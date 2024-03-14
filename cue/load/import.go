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
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/mod/module"
)

// importPkg returns details about the CUE package named by the import path,
// interpreting local import paths relative to l.cfg.Dir.
// If the path is a local import path naming a package that can be imported
// using a standard import path, the returned package will set p.ImportPath
// to that path.
//
// In the directory and ancestor directories up to including one with a
// cue.mod file, all .cue files are considered part of the package except for:
//
//   - files starting with _ or . (likely editor temporary files)
//   - files with build constraints not satisfied by the context
//
// If an error occurs, importPkg sets the error in the returned instance,
// which then may contain partial information.
//
// pkgName indicates which packages to load. It supports the following
// values:
//
//	""      the default package for the directory, if only one
//	        is present.
//	_       anonymous files (which may be marked with _)
//	*       all packages
func (l *loader) importPkg(pos token.Pos, p *build.Instance) []*build.Instance {
	retErr := func(errs errors.Error) []*build.Instance {
		// XXX: move this loop to ReportError
		for _, err := range errors.Errors(errs) {
			p.ReportError(err)
		}
		return []*build.Instance{p}
	}

	for _, item := range l.stk {
		if item == p.ImportPath {
			return retErr(&PackageError{Message: errors.NewMessagef("package import cycle not allowed")})
		}
	}
	l.stk.Push(p.ImportPath)
	defer l.stk.Pop()

	cfg := l.cfg
	ctxt := &cfg.fileSystem

	if p.Err != nil {
		return []*build.Instance{p}
	}

	fp := newFileProcessor(cfg, p, l.tagger)

	if p.PkgName == "" {
		if l.cfg.Package == "*" {
			fp.ignoreOther = true
			fp.allPackages = true
			p.PkgName = "_"
		} else {
			p.PkgName = l.cfg.Package
		}
	}
	if p.PkgName != "" {
		// If we have an explicit package name, we can ignore other packages.
		fp.ignoreOther = true
	}

	var dirs [][2]string
	genDir := GenPath(cfg.ModuleRoot)
	if strings.HasPrefix(p.Dir, genDir) {
		dirs = append(dirs, [2]string{genDir, p.Dir})
		// && p.PkgName != "_"
		for _, sub := range []string{"pkg", "usr"} {
			rel, err := filepath.Rel(genDir, p.Dir)
			if err != nil {
				// should not happen
				return retErr(errors.Wrapf(err, token.NoPos, "invalid path"))
			}
			base := filepath.Join(cfg.ModuleRoot, modDir, sub)
			dir := filepath.Join(base, rel)
			dirs = append(dirs, [2]string{base, dir})
		}
	} else {
		dirs = append(dirs, [2]string{cfg.ModuleRoot, p.Dir})
	}

	found := false
	for _, d := range dirs {
		info, err := ctxt.stat(d[1])
		if err == nil && info.IsDir() {
			found = true
			break
		}
	}

	if !found {
		return retErr(
			&PackageError{
				Message: errors.NewMessagef("cannot find package %q", p.DisplayPath),
			})
	}

	// This algorithm assumes that multiple directories within cue.mod/*/
	// have the same module scope and that there are no invalid modules.
	inModule := false // if pkg == "_"
	for _, d := range dirs {
		if l.cfg.findRoot(d[1]) != "" {
			inModule = true
			break
		}
	}

	for _, d := range dirs {
		for dir := filepath.Clean(d[1]); ctxt.isDir(dir); {
			files, err := ctxt.readDir(dir)
			if err != nil && !os.IsNotExist(err) {
				return retErr(errors.Wrapf(err, pos, "import failed reading dir %v", dirs[0][1]))
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				if f.Name() == "-" {
					if _, err := cfg.fileSystem.stat("-"); !os.IsNotExist(err) {
						continue
					}
				}
				file, err := filetypes.ParseFile(f.Name(), filetypes.Input)
				if err != nil {
					p.UnknownFiles = append(p.UnknownFiles, &build.File{
						Filename:      f.Name(),
						ExcludeReason: errors.Newf(token.NoPos, "unknown filetype"),
					})
					continue // skip unrecognized file types
				}
				fp.add(dir, file, importComment)
			}

			if p.PkgName == "" || !inModule || l.cfg.isRoot(dir) || dir == d[0] {
				break
			}

			// From now on we just ignore files that do not belong to the same
			// package.
			fp.ignoreOther = true

			parent, _ := filepath.Split(dir)
			parent = filepath.Clean(parent)

			if parent == dir || len(parent) < len(d[0]) {
				break
			}
			dir = parent
		}
	}

	all := []*build.Instance{}

	for _, p := range fp.pkgs {
		impPath, err := addImportQualifier(importPath(p.ImportPath), p.PkgName)
		p.ImportPath = string(impPath)
		if err != nil {
			p.ReportError(err)
		}

		all = append(all, p)
		rewriteFiles(p, cfg.ModuleRoot, false)
		if errs := fp.finalize(p); errs != nil {
			p.ReportError(errs)
			return all
		}

		l.addFiles(cfg.ModuleRoot, p)
		_ = p.Complete()
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Dir < all[j].Dir
	})
	return all
}

// _loadFunc is the method used for the value of l.loadFunc.
func (l *loader) _loadFunc(pos token.Pos, path string) *build.Instance {
	impPath := importPath(path)
	if isLocalImport(path) {
		return l.cfg.newErrInstance(errors.Newf(pos, "relative import paths not allowed (%q)", path))
	}

	// is it a builtin?
	if strings.IndexByte(strings.Split(path, "/")[0], '.') == -1 {
		if l.cfg.StdRoot != "" {
			p := l.newInstance(pos, impPath)
			_ = l.importPkg(pos, p)
			return p
		}
		return nil
	}

	p := l.newInstance(pos, impPath)
	_ = l.importPkg(pos, p)
	return p
}

// newRelInstance returns a build instance from the given
// relative import path.
func (l *loader) newRelInstance(pos token.Pos, path, pkgName string) *build.Instance {
	if !isLocalImport(path) {
		panic(fmt.Errorf("non-relative import path %q passed to newRelInstance", path))
	}

	var err errors.Error
	dir := path

	p := l.cfg.Context.NewInstance(path, l.loadFunc)
	p.PkgName = pkgName
	p.DisplayPath = filepath.ToSlash(path)
	// p.ImportPath = string(dir) // compute unique ID.
	p.Root = l.cfg.ModuleRoot
	p.Module = l.cfg.Module

	dir = filepath.Join(l.cfg.Dir, filepath.FromSlash(path))

	if path != cleanImport(path) {
		err = errors.Append(err, l.errPkgf(nil,
			"non-canonical import path: %q should be %q", path, pathpkg.Clean(path)))
	}

	if importPath, e := l.importPathFromAbsDir(fsPath(dir), path); e != nil {
		// Detect later to keep error messages consistent.
	} else {
		p.ImportPath = string(importPath)
	}

	p.Dir = dir

	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		err = errors.Append(err, errors.Newf(pos,
			"absolute import path %q not allowed", path))
	}
	if err != nil {
		p.Err = errors.Append(p.Err, err)
		p.Incomplete = true
	}

	return p
}

func (l *loader) importPathFromAbsDir(absDir fsPath, key string) (importPath, errors.Error) {
	if l.cfg.ModuleRoot == "" {
		return "", errors.Newf(token.NoPos,
			"cannot determine import path for %q (root undefined)", key)
	}

	dir := filepath.Clean(string(absDir))
	if !strings.HasPrefix(dir, l.cfg.ModuleRoot) {
		return "", errors.Newf(token.NoPos,
			"cannot determine import path for %q (dir outside of root)", key)
	}

	pkg := filepath.ToSlash(dir[len(l.cfg.ModuleRoot):])
	switch {
	case strings.HasPrefix(pkg, "/cue.mod/"):
		pkg = pkg[len("/cue.mod/"):]
		if pkg == "" {
			return "", errors.Newf(token.NoPos,
				"invalid package %q (root of %s)", key, modDir)
		}

	case l.cfg.Module == "":
		return "", errors.Newf(token.NoPos,
			"cannot determine import path for %q (no module)", key)
	default:
		pkg = l.cfg.Module + pkg
	}

	name := l.cfg.Package
	switch name {
	case "_", "*":
		name = ""
	}

	return addImportQualifier(importPath(pkg), name)
}

func (l *loader) newInstance(pos token.Pos, p importPath) *build.Instance {
	dir, name, err := l.absDirFromImportPath(pos, p)
	i := l.cfg.Context.NewInstance(dir, l.loadFunc)
	i.Dir = dir
	i.PkgName = name
	i.DisplayPath = string(p)
	i.ImportPath = string(p)
	i.Root = l.cfg.ModuleRoot
	i.Module = l.cfg.Module
	i.Err = errors.Append(i.Err, err)

	return i
}

// absDirFromImportPath converts a giving import path to an absolute directory
// and a package name. The root directory must be set.
//
// The returned directory may not exist.
func (l *loader) absDirFromImportPath(pos token.Pos, p importPath) (absDir, name string, err errors.Error) {
	if l.cfg.ModuleRoot == "" {
		return "", "", errors.Newf(pos, "cannot import %q (root undefined)", p)
	}
	origp := p
	// Extract the package name.
	parts := module.ParseImportPath(string(p))
	name = parts.Qualifier
	p = importPath(parts.Unqualified().String())
	if name == "" {
		err = errors.Newf(pos, "empty package name in import path %q", p)
	} else if strings.IndexByte(name, '.') >= 0 {
		err = errors.Newf(pos,
			"cannot determine package name for %q (set explicitly with ':')", p)
	} else if !ast.IsValidIdent(name) {
		err = errors.Newf(pos,
			"implied package identifier %q from import path %q is not valid", name, p)
	}
	if l.cfg.Registry != nil {
		if l.pkgs == nil {
			return "", name, errors.Newf(pos, "imports are unavailable because there is no cue.mod/module.cue file")
		}
		// TODO predicate registry-aware lookup on module.cue-declared CUE version?

		// Note: use the original form of the import path because
		// that's the form passed to modpkgload.LoadPackages
		// and hence it's available by that name via Pkg.
		pkg := l.pkgs.Pkg(string(origp))
		if pkg == nil {
			return "", name, errors.Newf(pos, "no dependency found for package %q", p)
		}
		if err := pkg.Error(); err != nil {
			return "", name, errors.Newf(pos, "cannot find package %q: %v", p, err)
		}
		if mv := pkg.Mod(); mv.IsLocal() {
			// It's a local package that's present inside one or both of the gen, usr or pkg
			// directories. Even though modpkgload tells us exactly what those directories
			// are, the rest of the cue/load logic expects only a single directory for now,
			// so just use that.
			absDir = filepath.Join(GenPath(l.cfg.ModuleRoot), parts.Path)
		} else {
			locs := pkg.Locations()
			if len(locs) > 1 {
				return "", "", errors.Newf(pos, "package %q unexpectedly found in multiple locations", p)
			}
			var err error
			absDir, err = absPathForSourceLoc(locs[0])
			if err != nil {
				return "", name, errors.Newf(pos, "cannot determine source directory for package %q: %v", p, err)
			}
		}
		return absDir, name, nil
	}

	// Determine the directory without using the registry.

	sub := filepath.FromSlash(string(p))
	switch hasPrefix := strings.HasPrefix(string(p), l.cfg.Module); {
	case hasPrefix && len(sub) == len(l.cfg.Module):
		absDir = l.cfg.ModuleRoot

	case hasPrefix && p[len(l.cfg.Module)] == '/':
		absDir = filepath.Join(l.cfg.ModuleRoot, sub[len(l.cfg.Module)+1:])

	default:
		absDir = filepath.Join(GenPath(l.cfg.ModuleRoot), sub)
	}
	return absDir, name, err
}

func absPathForSourceLoc(loc module.SourceLoc) (string, error) {
	osfs, ok := loc.FS.(module.OSRootFS)
	if !ok {
		return "", fmt.Errorf("cannot get absolute path for FS of type %T", loc.FS)
	}
	osPath := osfs.OSRoot()
	if osPath == "" {
		return "", fmt.Errorf("cannot get absolute path for FS of type %T", loc.FS)
	}
	return filepath.Join(osPath, loc.Dir), nil
}
