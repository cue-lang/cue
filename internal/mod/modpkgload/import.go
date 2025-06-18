package modpkgload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/module"
)

// importFromModules finds the module and source location in the dependency graph of
// pkgs containing the package with the given import path.
//
// The answer must be unique: importFromModules returns an error if multiple
// modules are observed to provide the same package.
//
// importFromModules can return a zero module version for packages in
// the standard library.
//
// If the package is not present in any module selected from the requirement
// graph, importFromModules returns an *ImportMissingError.
//
// If the package is present in exactly one module, importFromModules will
// return the module, its root directory, and a list of other modules that
// lexically could have provided the package but did not.
func (pkgs *Packages) importFromModules(ctx context.Context, pkgPath string) (
	m module.Version,
	mroot module.SourceLoc,
	pkgLocs []module.SourceLoc,
	err error,
) {
	fail := func(err error) (module.Version, module.SourceLoc, []module.SourceLoc, error) {
		return module.Version{}, module.SourceLoc{}, nil, err
	}
	failf := func(format string, args ...interface{}) (module.Version, module.SourceLoc, []module.SourceLoc, error) {
		return fail(fmt.Errorf(format, args...))
	}
	// Note: we don't care about the package qualifier at this point
	// because any directory with CUE files in counts as a possible
	// candidate, regardless of what packages are in it.
	pathParts := ast.ParseImportPath(pkgPath)
	pkgPathOnly := pathParts.Path

	if filepath.IsAbs(pkgPathOnly) || path.IsAbs(pkgPathOnly) {
		return failf("%q is not a package path", pkgPath)
	}
	// TODO check that the path isn't relative.
	// TODO check it's not a meta package name, such as "all".

	// Before any further lookup, check that the path is valid.
	if err := module.CheckImportPath(pkgPath); err != nil {
		return fail(err)
	}

	// Check each module on the build list.
	var locs []PackageLoc
	var mg *modrequirements.ModuleGraph
	versionForModule := func(ctx context.Context, prefix string) (module.Version, error) {
		var (
			v  string
			ok bool
		)
		pkgVersion := pathParts.Version
		if pkgVersion == "" {
			if pkgVersion, _ = pkgs.requirements.DefaultMajorVersion(prefix); pkgVersion == "" {
				return module.Version{}, nil
			}
		}
		prefixPath := prefix + "@" + pkgVersion
		// Note: mg is nil the first time around the loop.
		if mg == nil {
			v, ok = pkgs.requirements.RootSelected(prefixPath)
		} else {
			v, ok = mg.Selected(prefixPath), true
		}
		if !ok || v == "none" {
			// No possible module
			return module.Version{}, nil
		}
		m, err := module.NewVersion(prefixPath, v)
		if err != nil {
			// Not all package paths are valid module versions,
			// but a parent might be.
			return module.Version{}, nil
		}
		return m, nil
	}
	localPkgLocs, err := pkgs.findLocalPackage(pkgPathOnly)
	if err != nil {
		return fail(err)
	}
	if len(localPkgLocs) > 0 {
		locs = append(locs, PackageLoc{
			Module:     module.MustNewVersion("local", ""),
			ModuleRoot: pkgs.mainModuleLoc,
			Locs:       localPkgLocs,
		})
	}

	// Iterate over possible modules for the path, not all selected modules.
	// Iterating over selected modules would make the overall loading time
	// O(M × P) for M modules providing P imported packages, whereas iterating
	// over path prefixes is only O(P × k) with maximum path depth k. For
	// large projects both M and P may be very large (note that M ≤ P), but k
	// will tend to remain smallish (if for no other reason than filesystem
	// path limitations).
	//
	// We perform this iteration either one or two times.
	// Firstly we attempt to load the package using only the main module and
	// its root requirements. If that does not identify the package, then we attempt
	// to load the package using the full
	// requirements in mg.
	for {
		// Note: if fetch fails, we return an error:
		// we don't know for sure this module is necessary,
		// but it certainly _could_ provide the package, and even if we
		// continue the loop and find the package in some other module,
		// we need to look at this module to make sure the import is
		// not ambiguous.
		plocs, err := FindPackageLocations(ctx, pkgPath, versionForModule, pkgs.fetch)
		if err != nil {
			return fail(err)
		}
		locs = append(locs, plocs...)
		if len(locs) > 1 {
			// We produce the list of directories from longest to shortest candidate
			// module path, but the AmbiguousImportError should report them from
			// shortest to longest. Reverse them now.
			slices.Reverse(locs)
			return fail(&AmbiguousImportError{ImportPath: pkgPath, Locations: locs})
		}
		if len(locs) == 1 {
			// We've found the unique module containing the package.
			return locs[0].Module, locs[0].ModuleRoot, locs[0].Locs, nil
		}

		if mg != nil {
			// We checked the full module graph and still didn't find the
			// requested package.
			return fail(&ImportMissingError{Path: pkgPath})
		}

		// So far we've checked the root dependencies.
		// Load the full module graph and try again.
		mg, err = pkgs.requirements.Graph(ctx)
		if err != nil {
			// We might be missing one or more transitive (implicit) dependencies from
			// the module graph, so we can't return an ImportMissingError here — one
			// of the missing modules might actually contain the package in question,
			// in which case we shouldn't go looking for it in some new dependency.
			return fail(fmt.Errorf("cannot expand module graph: %v", err))
		}
	}
}

// PackageLoc holds a module version and the module root location, and a location of a package
// within that module.
type PackageLoc struct {
	Module     module.Version
	ModuleRoot module.SourceLoc
	// Locs holds the source locations of the package. There is always
	// at least one element; there can be more than one when the
	// module path is "local" (for exampe packages inside cue.mod/pkg).
	Locs []module.SourceLoc
}

// FindPackageLocations finds possible module candidates for a given import path.
//
// It tries each parent of the import path as a possible module location,
// using versionForModule to determine a version for that module
// and fetch to fetch the location for a given module version.
//
// versionForModule may indicate that there is no possible module
// for a given path by returning the zero version and a nil error.
//
// The fetch function also reports whether the location is "local"
// to the current module, allowing some checks to be skipped when false.
//
// It returns possible locations for the package. Each location may or may
// not contain the package itself, although it will hold some CUE files.
func FindPackageLocations(
	ctx context.Context,
	importPath string,
	versionForModule func(ctx context.Context, prefixPath string) (module.Version, error),
	fetch func(ctx context.Context, m module.Version) (loc module.SourceLoc, isLocal bool, err error),
) ([]PackageLoc, error) {
	ip := ast.ParseImportPath(importPath)
	var locs []PackageLoc
	for prefix := range pathAncestors(ip.Path) {
		v, err := versionForModule(ctx, prefix)
		if err != nil {
			return nil, err
		}
		if !v.IsValid() {
			continue
		}
		mloc, isLocal, err := fetch(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch %v: %w", v, err)
		}
		if mloc.FS == nil {
			// Not found but not an error.
			continue
		}
		loc, ok, err := locInModule(ip.Path, prefix, mloc, isLocal)
		if err != nil {
			return nil, fmt.Errorf("cannot find package: %v", err)
		}
		if ok {
			locs = append(locs, PackageLoc{
				Module:     v,
				ModuleRoot: mloc,
				Locs:       []module.SourceLoc{loc},
			})
		}
	}
	return locs, nil
}

// locInModule returns the location that would hold the package named by
// the given path, if it were in the module with module path mpath and
// root location mloc. If pkgPath is syntactically not within mpath, or
// if mdir is a local file tree (isLocal == true) and the directory that
// would hold path is in a sub-module (covered by a cue.mod below mdir),
// locInModule returns "", false, nil.
//
// Otherwise, locInModule returns the name of the directory where CUE
// source files would be expected, along with a boolean indicating
// whether there are in fact CUE source files in that directory. A
// non-nil error indicates that the existence of the directory and/or
// source files could not be determined, for example due to a permission
// error.
func locInModule(pkgPath, mpath string, mloc module.SourceLoc, isLocal bool) (loc module.SourceLoc, haveCUEFiles bool, err error) {
	loc.FS = mloc.FS

	// Determine where to expect the package.
	if pkgPath == mpath {
		loc = mloc
	} else if len(pkgPath) > len(mpath) && pkgPath[len(mpath)] == '/' && pkgPath[:len(mpath)] == mpath {
		loc.Dir = path.Join(mloc.Dir, pkgPath[len(mpath)+1:])
	} else {
		return module.SourceLoc{}, false, nil
	}

	// Check that there aren't other modules in the way.
	// This check is unnecessary inside the module cache.
	// So we only check local module trees
	// (the main module and, in the future, any directory trees pointed at by replace directives).
	if isLocal {
		for d := loc.Dir; d != mloc.Dir && len(d) > len(mloc.Dir); {
			_, err := fs.Stat(mloc.FS, path.Join(d, "cue.mod/module.cue"))
			// TODO should we count it as a module file if it's a directory?
			haveCUEMod := err == nil
			if haveCUEMod {
				return module.SourceLoc{}, false, nil
			}
			parent := path.Dir(d)
			if parent == d {
				// Break the loop, as otherwise we'd loop
				// forever if d=="." and mdir=="".
				break
			}
			d = parent
		}
	}

	// Are there CUE source files in the directory?
	// We don't care about build tags, not even "ignore".
	// We're just looking for a plausible directory.
	haveCUEFiles, err = isDirWithCUEFiles(loc)
	if err != nil {
		return module.SourceLoc{}, false, err
	}
	return loc, haveCUEFiles, err
}

var localPkgDirs = []string{"cue.mod/gen", "cue.mod/usr", "cue.mod/pkg"}

func (pkgs *Packages) findLocalPackage(pkgPath string) ([]module.SourceLoc, error) {
	var locs []module.SourceLoc
	for _, d := range localPkgDirs {
		loc := pkgs.mainModuleLoc
		loc.Dir = path.Join(loc.Dir, d, pkgPath)
		ok, err := isDirWithCUEFiles(loc)
		if err != nil {
			return nil, err
		}
		if ok {
			locs = append(locs, loc)
		}
	}
	return locs, nil
}

func isDirWithCUEFiles(loc module.SourceLoc) (bool, error) {
	// It would be nice if we could inspect the error returned from ReadDir to see
	// if it's failing because it's not a directory, but unfortunately that doesn't
	// seem to be something defined by the Go fs interface.
	// For now, catching fs.ErrNotExist seems to be enough.
	entries, err := fs.ReadDir(loc.FS, loc.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".cue") {
			continue
		}
		ftype := e.Type()
		// If the directory entry is a symlink, stat it to obtain the info for the
		// link target instead of the link itself.
		if ftype&fs.ModeSymlink != 0 {
			info, err := fs.Stat(loc.FS, filepath.Join(loc.Dir, e.Name()))
			if err != nil {
				continue // Ignore broken symlinks.
			}
			ftype = info.Mode()
		}
		if ftype.IsRegular() {
			return true, nil
		}
	}
	return false, nil
}

// fetch downloads the given module (or its replacement)
// and returns its location.
//
// The isLocal return value reports whether the replacement,
// if any, is within the local main module.
func (pkgs *Packages) fetch(ctx context.Context, mod module.Version) (loc module.SourceLoc, isLocal bool, err error) {
	if mod == pkgs.mainModuleVersion {
		return pkgs.mainModuleLoc, true, nil
	}
	loc, err = pkgs.registry.Fetch(ctx, mod)
	return loc, false, err
}

// pathAncestors returns an iterator over all the ancestors
// of p, including p itself.
func pathAncestors(p string) iter.Seq[string] {
	return func(yield func(s string) bool) {
		for {
			if !yield(p) {
				return
			}
			prev := p
			p = path.Dir(p)
			if p == "." || p == prev {
				return
			}
		}
	}
}

// An AmbiguousImportError indicates an import of a package found in multiple
// modules in the build list, or found in both the main module and its vendor
// directory.
type AmbiguousImportError struct {
	ImportPath string
	Locations  []PackageLoc
}

func (e *AmbiguousImportError) Error() string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "ambiguous import: found package %s in multiple locations:", e.ImportPath)

	for _, loc := range e.Locations {
		buf.WriteString("\n\t")
		buf.WriteString(loc.Module.Path())
		if v := loc.Module.Version(); v != "" {
			fmt.Fprintf(&buf, " %s", v)
		}
		// TODO work out how to present source locations in error messages.
		fmt.Fprintf(&buf, " (%s)", loc.Locs[0].Dir)
	}
	return buf.String()
}

// ImportMissingError is used for errors where an imported package cannot be found.
type ImportMissingError struct {
	Path string
}

func (e *ImportMissingError) Error() string {
	return "cannot find module providing package " + e.Path
}
