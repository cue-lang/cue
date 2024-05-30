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
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

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
		if l.cfg.findModRoot(d[1]) != "" {
			inModule = true
			break
		}
	}

	// Walk the parent directories up to the module root to add their files as well,
	// since a package foo/bar/baz inherits from parent packages foo/bar and foo.
	// See https://cuelang.org/docs/concept/modules-packages-instances/#instances.
	for _, d := range dirs {
		dir := filepath.Clean(d[1])
		for {
			sd, ok := l.dirCachedBuildFiles[dir]
			if !ok {
				sd = l.scanDir(dir)
				l.dirCachedBuildFiles[dir] = sd
			}
			if err := sd.err; err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					break
				}
				return retErr(errors.Wrapf(err, token.NoPos, "import failed reading dir %v", dir))
			}
			p.UnknownFiles = append(p.UnknownFiles, sd.unknownFiles...)
			for _, f := range sd.buildFiles {
				bf := *f
				fp.add(dir, &bf, importComment)
			}

			if p.PkgName == "" || !inModule || l.cfg.isModRoot(dir) || dir == d[0] {
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
			p.ReportError(errors.Promote(err, ""))
		}

		all = append(all, p)
		rewriteFiles(p, cfg.ModuleRoot, false)
		if errs := fp.finalize(p); errs != nil {
			p.ReportError(errs)
			return all
		}

		l.addFiles(p)
		_ = p.Complete()
	}
	slices.SortFunc(all, func(a, b *build.Instance) int {
		// Instances may share the same directory but have different package names.
		// Sort by directory first, then by package name.
		if c := cmp.Compare(a.Dir, b.Dir); c != 0 {
			return c
		}

		return cmp.Compare(a.PkgName, b.PkgName)
	})
	return all
}

func (l *loader) scanDir(dir string) cachedFileFiles {
	sd := cachedFileFiles{}
	files, err := l.cfg.fileSystem.readDir(dir)
	if err != nil {
		sd.err = err
		return sd
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if f.Name() == "-" {
			if _, err := l.cfg.fileSystem.stat("-"); !os.IsNotExist(err) {
				continue
			}
		}
		file, err := filetypes.ParseFile(f.Name(), filetypes.Input)
		if err != nil {
			sd.unknownFiles = append(sd.unknownFiles, &build.File{
				Filename:      f.Name(),
				ExcludeReason: errors.Newf(token.NoPos, "unknown filetype"),
			})
			continue // skip unrecognized file types
		}
		sd.buildFiles = append(sd.buildFiles, file)
	}
	return sd
}

func setFileSource(cfg *Config, f *build.File) error {
	if f.Source != nil {
		return nil
	}
	fullPath := f.Filename
	if fullPath == "-" {
		b, err := io.ReadAll(cfg.stdin())
		if err != nil {
			return errors.Newf(token.NoPos, "read stdin: %v", err)
		}
		f.Source = b
		return nil
	}
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(cfg.Dir, fullPath)
		// Ensure that encoding.NewDecoder will work correctly.
		f.Filename = fullPath
	}
	if fi := cfg.fileSystem.getOverlay(fullPath); fi != nil {
		if fi.file != nil {
			f.Source = fi.file
		} else {
			f.Source = fi.contents
		}
	}
	return nil
}

func (l *loader) loadFunc(pos token.Pos, path string) *build.Instance {
	impPath := importPath(path)
	if isLocalImport(path) {
		return l.cfg.newErrInstance(errors.Newf(pos, "relative import paths not allowed (%q)", path))
	}

	if isStdlibPackage(path) {
		// It looks like a builtin.
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

	p := l.cfg.Context.NewInstance(path, l.loadFunc)
	p.PkgName = pkgName
	p.DisplayPath = filepath.ToSlash(path)
	// p.ImportPath = string(dir) // compute unique ID.
	p.Root = l.cfg.ModuleRoot
	p.Module = l.cfg.Module

	var err errors.Error
	if path != cleanImport(path) {
		err = errors.Append(err, l.errPkgf(nil,
			"non-canonical import path: %q should be %q", path, pathpkg.Clean(path)))
	}

	dir := filepath.Join(l.cfg.Dir, filepath.FromSlash(path))
	if pkgPath, e := importPathFromAbsDir(l.cfg, dir, path); e != nil {
		// Detect later to keep error messages consistent.
	} else {
		// Add package qualifier if the configuration requires it.
		name := l.cfg.Package
		switch name {
		case "_", "*":
			name = ""
		}
		pkgPath, e := addImportQualifier(pkgPath, name)
		if e != nil {
			// Detect later to keep error messages consistent.
		} else {
			p.ImportPath = string(pkgPath)
		}
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

func importPathFromAbsDir(c *Config, absDir string, origPath string) (importPath, error) {
	if c.ModuleRoot == "" {
		return "", fmt.Errorf("cannot determine import path for %q (root undefined)", origPath)
	}

	dir := filepath.Clean(absDir)
	if !strings.HasPrefix(dir, c.ModuleRoot) {
		return "", fmt.Errorf("cannot determine import path for %q (dir outside of root)", origPath)
	}

	pkg := filepath.ToSlash(dir[len(c.ModuleRoot):])
	switch {
	case strings.HasPrefix(pkg, "/cue.mod/"):
		pkg = pkg[len("/cue.mod/"):]
		if pkg == "" {
			return "", fmt.Errorf("invalid package %q (root of %s)", origPath, modDir)
		}

	case c.Module == "":
		return "", fmt.Errorf("cannot determine import path for %q (no module)", origPath)
	default:
		impPath := module.ParseImportPath(c.Module)
		impPath.Path += pkg
		impPath.Qualifier = ""
		pkg = impPath.String()
	}
	return importPath(pkg), nil
}

func (l *loader) newInstance(pos token.Pos, p importPath) *build.Instance {
	dir, err := l.absDirFromImportPath(pos, p)
	i := l.cfg.Context.NewInstance(dir, l.loadFunc)
	i.Err = errors.Append(i.Err, err)
	i.Dir = dir

	parts := module.ParseImportPath(string(p))
	i.PkgName = parts.Qualifier
	if i.PkgName == "" {
		i.Err = errors.Append(i.Err, l.errPkgf([]token.Pos{pos}, "cannot determine package name for %q; set it explicitly with ':'", p))
	}
	i.DisplayPath = string(p)
	i.ImportPath = string(p)
	i.Root = l.cfg.ModuleRoot
	i.Module = l.cfg.Module

	return i
}

// absDirFromImportPath converts a giving import path to an absolute directory
// and a package name. The root directory must be set.
//
// The returned directory may not exist.
func (l *loader) absDirFromImportPath(pos token.Pos, p importPath) (string, errors.Error) {
	dir, err := l.absDirFromImportPath1(pos, p)
	if err != nil {
		// Any error trying to determine the package location
		// is a PackageError.
		return "", l.errPkgf([]token.Pos{pos}, "%s", err.Error())
	}
	return dir, nil
}

func (l *loader) absDirFromImportPath1(pos token.Pos, p importPath) (absDir string, err error) {
	if p == "" {
		return "", fmt.Errorf("empty import path")
	}
	if l.cfg.ModuleRoot == "" {
		return "", fmt.Errorf("cannot import %q (root undefined)", p)
	}
	if isStdlibPackage(string(p)) {
		return "", fmt.Errorf("standard library import path %q cannot be imported as a CUE package", p)
	}
	// Extract the package name.
	parts := module.ParseImportPath(string(p))
	unqualified := parts.Unqualified().String()
	if l.cfg.Registry != nil {
		if l.pkgs == nil {
			return "", fmt.Errorf("imports are unavailable because there is no cue.mod/module.cue file")
		}
		// TODO predicate registry-aware lookup on module.cue-declared CUE version?

		// Note: use the canonical form of the import path because
		// that's the form passed to [modpkgload.LoadPackages]
		// and hence it's available by that name via Pkg.
		pkg := l.pkgs.Pkg(parts.Canonical().String())
		// TODO(mvdan): using "unqualified" for the errors below doesn't seem right,
		// should we not be using either the original path or the canonical path?
		// The unqualified import path should only be used for filepath.FromSlash further below.
		if pkg == nil {
			return "", fmt.Errorf("no dependency found for package %q", unqualified)
		}
		if err := pkg.Error(); err != nil {
			return "", fmt.Errorf("cannot find package %q: %v", unqualified, err)
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
				return "", fmt.Errorf("package %q unexpectedly found in multiple locations", unqualified)
			}
			if len(locs) == 0 {
				return "", fmt.Errorf("no location found for package %q", unqualified)
			}
			var err error
			absDir, err = absPathForSourceLoc(locs[0])
			if err != nil {
				return "", fmt.Errorf("cannot determine source directory for package %q: %v", unqualified, err)
			}
		}
		return absDir, nil
	}

	// Determine the directory without using the registry.

	sub := filepath.FromSlash(unqualified)
	switch hasPrefix := strings.HasPrefix(unqualified, l.cfg.Module); {
	case hasPrefix && len(sub) == len(l.cfg.Module):
		absDir = l.cfg.ModuleRoot

	case hasPrefix && unqualified[len(l.cfg.Module)] == '/':
		absDir = filepath.Join(l.cfg.ModuleRoot, sub[len(l.cfg.Module)+1:])

	default:
		absDir = filepath.Join(GenPath(l.cfg.ModuleRoot), sub)
	}
	return absDir, err
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

// isStdlibPackage reports whether pkgPath looks like
// an import from the standard library.
func isStdlibPackage(pkgPath string) bool {
	firstElem, _, _ := strings.Cut(pkgPath, "/")
	if firstElem == "" {
		return false // absolute paths like "/foo/bar"
	}
	// Paths like ".foo/bar", "./foo/bar", or "foo.com/bar" are not standard library import paths.
	return strings.IndexByte(firstElem, '.') == -1
}
