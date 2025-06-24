// Copyright 2018 The CUE Authors
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

package load

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/mod/module"
)

// A match represents the result of matching a single package pattern.
type match struct {
	Pattern string // the pattern itself
	Literal bool   // whether it is a literal (no wildcards)
	Pkgs    []*build.Instance
	Err     errors.Error
}

// TODO: should be matched from module file only.
// The pattern is either "all" (all packages), "std" (standard packages),
// "cmd" (standard commands), or a path including "...".
func (l *loader) matchPackages(pattern, pkgName string) *match {
	// cfg := l.cfg
	m := &match{
		Pattern: pattern,
		Literal: false,
	}
	// match := func(string) bool { return true }
	// treeCanMatch := func(string) bool { return true }
	// if !isMetaPackage(pattern) {
	// 	match = matchPattern(pattern)
	// 	treeCanMatch = treeCanMatchPattern(pattern)
	// }

	// have := map[string]bool{
	// 	"builtin": true, // ignore pseudo-package that exists only for documentation
	// }

	// for _, src := range cfg.srcDirs() {
	// 	if pattern == "std" || pattern == "cmd" {
	// 		continue
	// 	}
	// 	src = filepath.Clean(src) + string(filepath.Separator)
	// 	root := src
	// 	if pattern == "cmd" {
	// 		root += "cmd" + string(filepath.Separator)
	// 	}
	// 	filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
	// 		if err != nil || path == src {
	// 			return nil
	// 		}

	// 		want := true
	// 		// Avoid .foo, _foo, and testdata directory trees.
	// 		_, elem := filepath.Split(path)
	// 		if strings.HasPrefix(elem, ".") || strings.HasPrefix(elem, "_") || elem == "testdata" {
	// 			want = false
	// 		}

	// 		name := filepath.ToSlash(path[len(src):])
	// 		if pattern == "std" && (!isStandardImportPath(name) || name == "cmd") {
	// 			// The name "std" is only the standard library.
	// 			// If the name is cmd, it's the root of the command tree.
	// 			want = false
	// 		}
	// 		if !treeCanMatch(name) {
	// 			want = false
	// 		}

	// 		if !fi.IsDir() {
	// 			if fi.Mode()&os.ModeSymlink != 0 && want {
	// 				if target, err := os.Stat(path); err == nil && target.IsDir() {
	// 					fmt.Fprintf(os.Stderr, "warning: ignoring symlink %s\n", path)
	// 				}
	// 			}
	// 			return nil
	// 		}
	// 		if !want {
	// 			return skipDir
	// 		}

	// 		if have[name] {
	// 			return nil
	// 		}
	// 		have[name] = true
	// 		if !match(name) {
	// 			return nil
	// 		}
	// 		pkg := l.importPkg(".", path)
	// 		if err := pkg.Error; err != nil {
	// 			if _, noGo := err.(*noCUEError); noGo {
	// 				return nil
	// 			}
	// 		}

	// 		// If we are expanding "cmd", skip main
	// 		// packages under cmd/vendor. At least as of
	// 		// March, 2017, there is one there for the
	// 		// vendored pprof tool.
	// 		if pattern == "cmd" && strings.HasPrefix(pkg.DisplayPath, "cmd/vendor") && pkg.PkgName == "main" {
	// 			return nil
	// 		}

	// 		m.Pkgs = append(m.Pkgs, pkg)
	// 		return nil
	// 	})
	// }
	return m
}

// matchPackagesInFS is like allPackages but is passed a pattern
// beginning ./ or ../, meaning it should scan the tree rooted
// at the given directory. There are ... in the pattern too.
// (See cue help inputs for pattern syntax.)
func (l *loader) matchPackagesInFS(pattern, pkgName string) *match {
	c := l.cfg
	m := &match{
		Pattern: pattern,
		Literal: false,
	}

	// Find directory to begin the scan.
	// Could be smarter but this one optimization
	// is enough for now, since ... is usually at the
	// end of a path.
	i := strings.Index(pattern, "...")
	dir, _ := path.Split(pattern[:i])

	root := l.abs(dir)

	// Find new module root from here or check there are no additional
	// cue.mod files between here and the next module.

	if !hasFilepathPrefix(root, c.ModuleRoot) {
		m.Err = errors.Newf(token.NoPos,
			"cue: pattern %s refers to dir %s, outside module root %s",
			pattern, root, c.ModuleRoot)
		return m
	}

	pkgDir := filepath.Join(root, modDir)

	_ = c.fileSystem.walk(root, func(path string, entry fs.DirEntry, err errors.Error) errors.Error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		if path == pkgDir {
			return skipDir
		}

		top := path == root

		// Avoid .foo, _foo, and testdata directory trees, but do not avoid "." or "..".
		_, elem := filepath.Split(path)
		dot := strings.HasPrefix(elem, ".") && elem != "." && elem != ".."
		if dot || strings.HasPrefix(elem, "_") || (elem == "testdata" && !top) {
			return skipDir
		}

		if !top {
			// Ignore other modules found in subdirectories.
			if _, err := c.fileSystem.stat(filepath.Join(path, modDir)); err == nil {
				return skipDir
			}
		}

		// name := prefix + filepath.ToSlash(path)
		// if !match(name) {
		// 	return nil
		// }

		// We keep the directory if we can import it, or if we can't import it
		// due to invalid CUE source files. This means that directories
		// containing parse errors will be built (and fail) instead of being
		// silently skipped as not matching the pattern.
		// Do not take root, as we want to stay relative
		// to one dir only.
		relPath, err2 := filepath.Rel(c.Dir, path)
		if err2 != nil {
			panic(err2) // Should never happen because c.Dir is absolute.
		}
		relPath = "./" + filepath.ToSlash(relPath)
		// TODO: consider not doing these checks here.
		inst := l.newRelInstance(token.NoPos, relPath, pkgName)
		pkgs := l.importPkg(token.NoPos, inst)
		for _, p := range pkgs {
			if err := p.Err; err != nil && (p == nil || len(p.InvalidFiles) == 0) {
				switch err.(type) {
				case *NoFilesError:
					if c.DataFiles && len(p.OrphanedFiles) > 0 {
						break
					}
					return nil
				default:
					m.Err = errors.Append(m.Err, err)
				}
			}
		}

		m.Pkgs = append(m.Pkgs, pkgs...)
		return nil
	})
	return m
}

// importPaths returns the matching paths to use for the given command line.
// It calls ImportPathsQuiet and then WarnUnmatched.
func (l *loader) importPaths(patterns []string) []*match {
	matches := l.importPathsQuiet(patterns)
	warnUnmatched(matches)
	return matches
}

// importPathsQuiet is like importPaths but does not warn about patterns with no matches.
func (l *loader) importPathsQuiet(patterns []string) []*match {
	var out []*match
	for _, a := range cleanPatterns(patterns) {
		if isMetaPackage(a) {
			out = append(out, l.matchPackages(a, l.cfg.Package))
			continue
		}

		orig := a
		pkgName := l.cfg.Package
		switch p := strings.IndexByte(a, ':'); {
		case p < 0:
		case p == 0:
			pkgName = a[1:]
			a = "."
		default:
			pkgName = a[p+1:]
			a = a[:p]
		}
		if pkgName == "*" {
			pkgName = ""
		}

		if strings.Contains(a, "...") {
			if isLocalImport(a) {
				out = append(out, l.matchPackagesInFS(a, pkgName))
			} else {
				out = append(out, l.matchPackages(a, pkgName))
			}
			continue
		}

		var p *build.Instance
		if isLocalImport(a) {
			p = l.newRelInstance(token.NoPos, a, pkgName)
		} else {
			p = l.newInstance(token.NoPos, importPath(orig))
		}

		pkgs := l.importPkg(token.NoPos, p)
		out = append(out, &match{Pattern: a, Literal: true, Pkgs: pkgs})
	}
	return out
}

type resolvedPackageArg struct {
	// The original field may be needed once we want to replace the original
	// package pattern matching code, as it is necessary to populate Instance.DisplayPath.
	original          string
	resolvedCanonical string
}

func expandPackageArgs(c *Config, pkgArgs []string, pkgQual string, tg *tagger) ([]resolvedPackageArg, error) {
	expanded := make([]resolvedPackageArg, 0, len(pkgArgs))
	for _, p := range pkgArgs {
		var err error
		expanded, err = appendExpandedPackageArg(c, expanded, p, pkgQual, tg)
		if err != nil {
			return nil, err
		}
	}
	return expanded, nil
}

// appendExpandedPackageArg appends all the package paths matched by p to pkgPaths
// and returns the result. It also cleans the paths and makes them absolute.
//
// pkgQual is used to determine which packages to match when wildcards are expanded.
// Its semantics follow those of [Config.Package].
func appendExpandedPackageArg(c *Config, pkgPaths []resolvedPackageArg, p string, pkgQual string, tg *tagger) ([]resolvedPackageArg, error) {
	origp := p
	if filepath.IsAbs(p) {
		return nil, fmt.Errorf("cannot use absolute directory %q as package path", p)
	}
	// Arguments are supposed to be import paths, but
	// as a courtesy to Windows developers, rewrite \ to /
	// in command-line arguments. Handles .\... and so on.
	p = filepath.ToSlash(p)

	ip := ast.ParseImportPath(p)
	if ip.Qualifier == "_" {
		return nil, fmt.Errorf("invalid import path qualifier _ in %q", origp)
	}

	isRel := strings.HasPrefix(ip.Path, "./")
	// Put argument in canonical form.
	ip.Path = path.Clean(ip.Path)
	if isRel && ip.Path != "." {
		// Preserve leading "./".
		ip.Path = "./" + ip.Path
	}
	isLocal := isLocalImport(ip.Path)
	// Note that when c.Module is empty, c.ModuleRoot is sometimes,
	// but not always, the same as c.Dir. Specifically it might point
	// to the directory containing a cue.mod directory even if that
	// directory doesn't actually contain a module.cue file.
	moduleRoot := c.ModuleRoot
	if isLocal {
		if c.Module != "" {
			// Make local import paths into absolute paths inside
			// the module root.
			absPath := path.Join(c.Dir, ip.Path)
			pkgPath, err := importPathFromAbsDir(c, absPath, origp)
			if err != nil {
				return nil, err
			}
			ip1 := ast.ParseImportPath(string(pkgPath))
			// Leave ip.Qualifier and ip.ExplicitQualifier intact.
			ip.Path = ip1.Path
			ip.Version = ip1.Version
		} else {
			// There's no module, so we can't make
			// the import path absolute.
			moduleRoot = c.Dir
		}
	}
	if !strings.Contains(ip.Path, "...") {
		if isLocal && !ip.ExplicitQualifier {
			// A package qualifier has not been explicitly specified for a local
			// import path so we need to walk the package directory to find the
			// packages in it. We have a special rule for local imports because it's
			// inconvenient always to have to specify a package qualifier when
			// there's only one package in the current directory but the last
			// component of its package path does not match its name.
			return appendExpandedUnqualifiedPackagePath(pkgPaths, origp, ip, pkgQual, module.SourceLoc{
				FS:  c.fileSystem.ioFS(moduleRoot, c.languageVersion()),
				Dir: ".",
			}, c.Module, tg)
		}
		return append(pkgPaths, resolvedPackageArg{origp, ip.Canonical().String()}), nil
	}
	// Strip the module prefix, leaving only the directory relative
	// to the module root.
	ip, ok := cutModulePrefix(ip, c.Module)
	if !ok {
		return nil, fmt.Errorf("pattern not allowed in external package path %q", origp)
	}
	return appendExpandedWildcardPackagePath(pkgPaths, ip, pkgQual, module.SourceLoc{
		FS:  c.fileSystem.ioFS(moduleRoot, c.languageVersion()),
		Dir: ".",
	}, c.Module, tg)
}

// appendExpandedUnqualifiedPackagePath expands the given import path,
// which is relative to the root of the module, into its resolved and
// qualified package paths according to the following rules (the first rule
// that applies is used)
//
//  1. if pkgQual is "*", it chooses all the packages present in the
//     package directory.
//  2. if pkgQual is "_", it looks for a package file with no package name.
//  3. if there's a package named after ip.Qualifier it chooses that
//  4. if there's exactly one package in the directory it will choose that.
//  5. if there's more than one package in the directory, it returns a MultiplePackageError.
//  6. if there are no package files in the directory, it just appends the import path as is, leaving it
//     to later logic to produce an error in this case.
func appendExpandedUnqualifiedPackagePath(pkgPaths []resolvedPackageArg, origp string, ip ast.ImportPath, pkgQual string, mainModRoot module.SourceLoc, mainModPath string, tg *tagger) ([]resolvedPackageArg, error) {
	ipRel, ok := cutModulePrefix(ip, mainModPath)
	if !ok {
		// Should never happen.
		return nil, fmt.Errorf("internal error: local import path %q in module %q has resulted in non-internal package %q", origp, mainModPath, ip)
	}
	dir := path.Join(mainModRoot.Dir, ipRel.Path)
	info, err := fs.Stat(mainModRoot.FS, dir)
	if err != nil {
		// The package directory doesn't exist.
		// Treat it like an empty directory and let later logic deal with it.
		return append(pkgPaths, resolvedPackageArg{origp, ip.String()}), nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is a file and not a package directory", origp)
	}
	iter := modimports.PackageFiles(mainModRoot.FS, dir, "*")

	// 1. if pkgQual is "*", it appends all the packages present in the package directory.
	if pkgQual == "*" {
		wasAdded := make(map[string]bool)
		for f, err := range iter {
			if err != nil {
				return nil, err
			}
			if err := shouldBuildFile(f.Syntax, tg.tagIsSet); err != nil {
				// Later build logic should pick up and report the same error.
				continue
			}
			pkgName := f.Syntax.PackageName()
			if wasAdded[pkgName] {
				continue
			}
			wasAdded[pkgName] = true
			ip := ip
			ip.Qualifier = pkgName
			p := ip.String()
			pkgPaths = append(pkgPaths, resolvedPackageArg{p, p})
		}
		return pkgPaths, nil
	}
	var files []modimports.ModuleFile
	foundQualifier := false
	for f, err := range iter {
		if err != nil {
			return nil, err
		}
		if err := shouldBuildFile(f.Syntax, tg.tagIsSet); err != nil {
			// Later build logic should pick up and report the same error.
			continue
		}
		pkgName := f.Syntax.PackageName()
		// 2. if pkgQual is "_", it looks for a package file with no package name.
		// 3. if there's a package named after ip.Qualifier it chooses that
		if (pkgName != "" && pkgName == ip.Qualifier) || (pkgQual == "_" && pkgName == "") {
			foundQualifier = true
			break
		}
		if pkgName != "" {
			files = append(files, f)
		}
	}
	if foundQualifier {
		// We found the actual package that was implied by the import path (or pkgQual == "_").
		// This takes precedence over anything else.
		return append(pkgPaths, resolvedPackageArg{origp, ip.String()}), nil
	}
	if len(files) == 0 {
		// 6. if there are no package files in the directory, it just appends the import path as is,
		// leaving it to later logic to produce an error in this case.
		return append(pkgPaths, resolvedPackageArg{origp, ip.String()}), nil
	}
	pkgName := files[0].Syntax.PackageName()
	for _, f := range files[1:] {
		// 5. if there's more than one package in the directory, it returns a MultiplePackageError.
		if pkgName1 := f.Syntax.PackageName(); pkgName1 != pkgName {
			return nil, &MultiplePackageError{
				Dir:      dir,
				Packages: []string{pkgName, pkgName1},
				Files: []string{
					path.Base(files[0].FilePath),
					path.Base(f.FilePath),
				},
			}
		}
	}
	// 4. if there's exactly one package in the directory it will choose that.
	ip.Qualifier = pkgName
	return append(pkgPaths, resolvedPackageArg{origp, ip.String()}), nil
}

// appendExpandedWildcardPackagePath expands the given pattern into any packages that it matches
// and appends the results to pkgPaths. It returns an error if the pattern matches nothing.
//
// Note:
// * We know that pattern contains "..."
// * We know that pattern is relative to the module root
func appendExpandedWildcardPackagePath(pkgPaths []resolvedPackageArg, pattern ast.ImportPath, pkgQual string, mainModRoot module.SourceLoc, mainModPath string, tg *tagger) ([]resolvedPackageArg, error) {
	modIpath := ast.ParseImportPath(mainModPath)
	// Find directory to begin the scan.
	// Could be smarter but this one optimization is enough for now,
	// since ... is usually at the end of a path.
	// TODO: strip package qualifier.
	i := strings.Index(pattern.Path, "...")
	dir, _ := path.Split(pattern.Path[:i])
	dir = path.Join(mainModRoot.Dir, dir)
	var isSelected func(string) bool
	switch pkgQual {
	case "_":
		isSelected = func(pkgName string) bool {
			return pkgName == ""
		}
	case "*":
		isSelected = func(pkgName string) bool {
			return true
		}
	case "":
		isSelected = func(pkgName string) bool {
			// The package ambiguity logic will be triggered if there's more than one
			// package in the same directory.
			return pkgName != ""
		}
	default:
		isSelected = func(pkgName string) bool {
			return pkgName == pkgQual
		}
	}

	var prevFile modimports.ModuleFile
	var prevImportPath ast.ImportPath
	for f, err := range modimports.AllModuleFiles(mainModRoot.FS, dir) {
		if err != nil {
			break
		}
		if err := shouldBuildFile(f.Syntax, tg.tagIsSet); err != nil {
			// Later build logic should pick up and report the same error.
			continue
		}
		pkgName := f.Syntax.PackageName()
		if !isSelected(pkgName) {
			continue
		}
		if pkgName == "" {
			pkgName = "_"
		}
		ip := ast.ImportPath{
			Path:      path.Join(modIpath.Path, path.Dir(f.FilePath)),
			Qualifier: pkgName,
			Version:   modIpath.Version,
		}
		if modIpath.Path == "" {
			// There's no module, so make sure that the path still looks like a relative import path.
			if !strings.HasPrefix(ip.Path, "../") {
				ip.Path = "./" + ip.Path
			}
		}
		if ip == prevImportPath {
			// TODO(rog): this isn't sufficient for full deduplication: we can get an alternation of different
			// package names within the same directory. We'll need to maintain a map.
		}
		if pkgQual == "" {
			// Note: we can look at the previous item only rather than maintaining a map
			// because modimports.AllModuleFiles guarantees that files in the same
			// package are always adjacent.
			if prevFile.FilePath != "" && prevImportPath.Path == ip.Path && ip.Qualifier != prevImportPath.Qualifier {
				// A wildcard isn't currently allowed to match multiple packages
				// in a single directory.
				return nil, &MultiplePackageError{
					Dir:      path.Dir(f.FilePath),
					Packages: []string{prevImportPath.Qualifier, ip.Qualifier},
					Files: []string{
						path.Base(prevFile.FilePath),
						path.Base(f.FilePath),
					},
				}
			}
		}
		pkgPaths = append(pkgPaths, resolvedPackageArg{ip.String(), ip.String()})
		prevFile, prevImportPath = f, ip
	}
	return pkgPaths, nil
}

// cutModulePrefix strips the given module path from p and reports whether p is inside mod.
// It returns a relative package path within m.
//
// If p does not contain a major version suffix but otherwise matches mod, it counts as a match.
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
