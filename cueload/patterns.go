// Copyright 2026 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cueload

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/buildattr"
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/mod/module"
)

// expandPattern expands one package pattern into the canonical import
// paths it denotes. Patterns are import paths, relative directories
// (./foo, .), or wildcards containing "..."; a wildcard matching no
// packages is an error.
func (l *Loader) expandPattern(pattern string) ([]string, error) {
	ip := ast.ParseImportPath(pattern)
	if ip.Qualifier == "_" && ip.ExplicitQualifier {
		return nil, fmt.Errorf("cueload: invalid import path qualifier _ in %q", pattern)
	}
	if path.IsAbs(ip.Path) {
		return nil, fmt.Errorf("cueload: absolute pattern %q is not supported; use a path relative to the loader directory", pattern)
	}

	if i := strings.Index(ip.Path, "..."); i >= 0 {
		if rest := ip.Path[i+3:]; rest != "" {
			return nil, fmt.Errorf("cueload: pattern %q: text after ... is not supported", pattern)
		}
		return l.expandWildcard(pattern, ip, ip.Path[:i])
	}

	isRel := ip.Path == "." || ip.Path == ".." ||
		strings.HasPrefix(ip.Path, "./") || strings.HasPrefix(ip.Path, "../")
	if isRel {
		return l.expandRelDir(pattern, ip)
	}

	// A plain import path.
	return []string{l.canonicalKey(pattern)}, nil
}

// expandWildcard expands a "..." pattern. before is the pattern's path
// up to (but excluding) the "...".
func (l *Loader) expandWildcard(pattern string, ip ast.ImportPath, before string) ([]string, error) {
	if l.module == nil {
		return nil, fmt.Errorf("cueload: no CUE module found: cannot expand pattern %q", pattern)
	}
	modIP := ast.ParseImportPath(l.module.path)

	var startDir string
	switch {
	case before == "" || before == "./" || strings.HasPrefix(before, "./") || strings.HasPrefix(before, "../"):
		// A directory-relative wildcard such as ./... or ./x/...
		dir, _ := path.Split(before)
		abs := absJoin(l.dir, dir)
		rel, err := l.moduleRelDir(abs, pattern)
		if err != nil {
			return nil, err
		}
		startDir = rel
	default:
		// A module-prefixed wildcard such as foo.com/bar/...
		rel, ok := cutModulePrefix(ip, l.module.path)
		if !ok {
			return nil, fmt.Errorf("cueload: pattern not allowed in external package path %q", pattern)
		}
		dir, _ := path.Split(strings.TrimSuffix(rel.Path, "..."))
		startDir = path.Clean(dir)
	}

	qual := ""
	if ip.ExplicitQualifier {
		qual = ip.Qualifier
	}
	paths, err := expandModuleFiles(l, l.module.loc, modIP, startDir, qual)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("cueload: no packages matched pattern %q", pattern)
	}
	return paths, nil
}

// expandModuleFiles walks the CUE files of the module at loc, starting
// at startDir (relative to the module root), and returns the canonical
// import paths of the packages found. qual selects a package name;
// when empty, any named package matches, and two differently-named
// packages in the same directory are an error.
func expandModuleFiles(l *Loader, loc module.SourceLoc, modIP ast.ImportPath, startDir, qual string) ([]string, error) {
	startDir = path.Clean(path.Join(loc.Dir, startDir))
	type dirPkg struct {
		dir, name string
	}
	seen := make(map[dirPkg]bool)
	nameInDir := make(map[string]string)
	var out []string
	for mf, err := range modimports.AllModuleFiles(loc.FS, startDir) {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && len(out) == 0 {
				// A missing start directory simply matches nothing.
				return nil, nil
			}
			return nil, err
		}
		if !l.matchModuleFile(mf) {
			continue
		}
		pkgName := mf.Syntax.PackageName()
		if pkgName == "" {
			continue
		}
		if qual != "" && pkgName != qual {
			continue
		}
		dir := path.Dir(mf.FilePath)
		if qual == "" {
			if prev, ok := nameInDir[dir]; ok && prev != pkgName {
				return nil, fmt.Errorf("cueload: multiple packages (%s and %s) in directory %q", prev, pkgName, dir)
			}
			nameInDir[dir] = pkgName
		}
		key := dirPkg{dir, pkgName}
		if seen[key] {
			continue
		}
		seen[key] = true
		rel := dir
		if loc.Dir != "." {
			switch {
			case dir == loc.Dir:
				rel = "."
			case strings.HasPrefix(dir, loc.Dir+"/"):
				rel = dir[len(loc.Dir)+1:]
			}
		}
		pip := ast.ImportPath{
			Path:      modIP.Path,
			Qualifier: pkgName,
			Version:   modIP.Version,
		}
		if rel != "." {
			pip.Path = path.Join(modIP.Path, rel)
		}
		out = append(out, pip.Canonical().String())
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// matchModuleFile reports whether a module file takes part in pattern
// expansion, applying the _test/_tool rules and build attributes.
func (l *Loader) matchModuleFile(mf modimports.ModuleFile) bool {
	if mf.Syntax == nil {
		return false
	}
	if !l.cfg.IncludeTools && strings.HasSuffix(mf.FilePath, "_tool.cue") {
		return false
	}
	if !l.cfg.IncludeTests && strings.HasSuffix(mf.FilePath, "_test.cue") {
		return false
	}
	ok, _, err := buildattr.ShouldBuildFile(mf.Syntax, func(key string) bool {
		return l.buildTagSet[key]
	})
	return err == nil && ok
}

// expandRelDir expands a relative directory pattern such as ./foo into
// a canonical import path, discovering the package qualifier from the
// directory contents when the pattern does not make it explicit.
func (l *Loader) expandRelDir(pattern string, ip ast.ImportPath) ([]string, error) {
	if l.module == nil {
		return nil, fmt.Errorf("cueload: no CUE module found: cannot resolve relative package pattern %q", pattern)
	}
	abs := absJoin(l.dir, ip.Path)
	rel, err := l.moduleRelDir(abs, pattern)
	if err != nil {
		return nil, err
	}
	modIP := ast.ParseImportPath(l.module.path)
	pip := ast.ImportPath{
		Path:              modIP.Path,
		Version:           modIP.Version,
		Qualifier:         ip.Qualifier,
		ExplicitQualifier: ip.ExplicitQualifier,
	}
	if rel != "." {
		pip.Path = path.Join(modIP.Path, rel)
	}
	if !ip.ExplicitQualifier {
		// Discover the package name from the directory: the directory
		// must hold exactly one package (ignoring nameless files), or a
		// package named after the directory.
		name, err := l.dirPackageName(rel, path.Base(pip.Path))
		if err != nil {
			return nil, fmt.Errorf("cueload: pattern %q: %v", pattern, err)
		}
		if name != "" {
			pip.Qualifier = name
		}
	}
	return []string{pip.Canonical().String()}, nil
}

// dirPackageName determines the package name to load from the given
// directory (relative to the module root): implied if a package named
// after the directory exists, the single package present otherwise.
// It returns "" when the directory has no named packages, leaving the
// error to package loading.
func (l *Loader) dirPackageName(rel, implied string) (string, error) {
	dir := path.Join(l.module.loc.Dir, rel)
	var names []string
	for mf, err := range modimports.PackageFiles(l.module.loc.FS, dir, "*") {
		if err != nil {
			// Leave I/O problems (such as a missing directory) to
			// package loading, which reports them consistently.
			return "", nil
		}
		if !l.matchModuleFile(mf) {
			continue
		}
		name := mf.Syntax.PackageName()
		if name == implied {
			return name, nil
		}
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	switch len(names) {
	case 0:
		return "", nil
	case 1:
		return names[0], nil
	}
	slices.Sort(names)
	return "", fmt.Errorf("multiple packages (%s) in directory; add an explicit package qualifier", strings.Join(names, ", "))
}

// moduleRelDir converts an absolute directory to a path relative to
// the module root, requiring the directory to be inside the module.
func (l *Loader) moduleRelDir(abs, pattern string) (string, error) {
	root := l.module.rootDir
	switch {
	case abs == root:
		return ".", nil
	case strings.HasPrefix(abs, root+"/"):
		return abs[len(root)+1:], nil
	}
	return "", fmt.Errorf("cueload: pattern %q refers to directory %s outside module root %s", pattern, abs, root)
}

// cutModulePrefix strips the module path from p and reports whether p
// is inside the module, returning a relative package path. A p without
// a major version suffix that otherwise matches mod counts as a match.
// Ported from cue/load.
func cutModulePrefix(p ast.ImportPath, mod string) (ast.ImportPath, bool) {
	if mod == "" {
		return p, true
	}
	modPath, modVers, _ := ast.SplitPackageVersion(mod)
	if !strings.HasPrefix(p.Path, modPath) {
		return ast.ImportPath{}, false
	}
	if p.Path == modPath {
		p.Path = "."
		return p, true
	}
	if p.Path[len(modPath)] != '/' {
		return ast.ImportPath{}, false
	}
	if p.Version != "" && modVers != "" && p.Version != modVers {
		return ast.ImportPath{}, false
	}
	p.Path = "." + p.Path[len(modPath):]
	p.Version = ""
	return p, true
}
