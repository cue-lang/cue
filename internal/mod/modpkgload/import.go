package modpkgload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

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
func (pkgs *Packages) importFromModules(ctx context.Context, pkgPath string) (m module.Version, pkgLocs []module.SourceLoc, altMods []module.Version, err error) {
	fail := func(err error) (module.Version, []module.SourceLoc, []module.Version, error) {
		return module.Version{}, []module.SourceLoc(nil), nil, err
	}
	failf := func(format string, args ...interface{}) (module.Version, []module.SourceLoc, []module.Version, error) {
		return fail(fmt.Errorf(format, args...))
	}
	// Note: we don't care about the package qualifier at this point
	// because any directory with CUE files in counts as a possible
	// candidate, regardless of what packages are in it.
	pathParts := module.ParseImportPath(pkgPath)
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
	var locs [][]module.SourceLoc
	var mods []module.Version
	var mg *modrequirements.ModuleGraph
	localPkgLocs, err := pkgs.findLocalPackage(pkgPathOnly)
	if err != nil {
		return fail(err)
	}
	if len(localPkgLocs) > 0 {
		mods = append(mods, module.MustNewVersion("local", ""))
		locs = append(locs, localPkgLocs)
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
		var altMods []module.Version
		// TODO we could probably do this loop concurrently.

		for prefix := pkgPathOnly; prefix != "."; prefix = path.Dir(prefix) {
			var (
				v  string
				ok bool
			)
			pkgVersion := pathParts.Version
			if pkgVersion == "" {
				if pkgVersion, _ = pkgs.requirements.DefaultMajorVersion(prefix); pkgVersion == "" {
					continue
				}
			}
			prefixPath := prefix + "@" + pkgVersion
			if mg == nil {
				v, ok = pkgs.requirements.RootSelected(prefixPath)
			} else {
				v, ok = mg.Selected(prefixPath), true
			}
			if !ok || v == "none" {
				continue
			}
			m, err := module.NewVersion(prefixPath, v)
			if err != nil {
				// Not all package paths are valid module versions,
				// but a parent might be.
				continue
			}
			mloc, isLocal, err := pkgs.fetch(ctx, m)
			if err != nil {
				// Report fetch error.
				// Note that we don't know for sure this module is necessary,
				// but it certainly _could_ provide the package, and even if we
				// continue the loop and find the package in some other module,
				// we need to look at this module to make sure the import is
				// not ambiguous.
				return fail(fmt.Errorf("cannot fetch %v: %v", m, err))
			}
			if loc, ok, err := locInModule(pkgPathOnly, prefix, mloc, isLocal); err != nil {
				return fail(fmt.Errorf("cannot find package: %v", err))
			} else if ok {
				mods = append(mods, m)
				locs = append(locs, []module.SourceLoc{loc})
			} else {
				altMods = append(altMods, m)
			}
		}

		if len(mods) > 1 {
			// We produce the list of directories from longest to shortest candidate
			// module path, but the AmbiguousImportError should report them from
			// shortest to longest. Reverse them now.
			slices.Reverse(mods)
			slices.Reverse(locs)
			return fail(&AmbiguousImportError{ImportPath: pkgPath, Locations: locs, Modules: mods})
		}

		if len(mods) == 1 {
			// We've found the unique module containing the package.
			return mods[0], locs[0], altMods, nil
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

// locInModule returns the location that would hold the package named by the given path,
// if it were in the module with module path mpath and root location mloc.
// If pkgPath is syntactically not within mpath,
// or if mdir is a local file tree (isLocal == true) and the directory
// that would hold path is in a sub-module (covered by a go.mod below mdir),
// locInModule returns "", false, nil.
//
// Otherwise, locInModule returns the name of the directory where
// CUE source files would be expected, along with a boolean indicating
// whether there are in fact CUE source files in that directory.
// A non-nil error indicates that the existence of the directory and/or
// source files could not be determined, for example due to a permission error.
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
	// It would be nice if we could inspect the error returned from
	// ReadDir to see if it's failing because it's not a directory,
	// but unfortunately that doesn't seem to be something defined
	// by the Go fs interface.
	fi, err := fs.Stat(loc.FS, loc.Dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		return false, nil
	}
	if !fi.IsDir() {
		return false, nil
	}
	entries, err := fs.ReadDir(loc.FS, loc.Dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".cue") && e.Type().IsRegular() {
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

// An AmbiguousImportError indicates an import of a package found in multiple
// modules in the build list, or found in both the main module and its vendor
// directory.
type AmbiguousImportError struct {
	ImportPath string
	Locations  [][]module.SourceLoc
	Modules    []module.Version // Either empty or 1:1 with Dirs.
}

func (e *AmbiguousImportError) Error() string {
	locType := "modules"
	if len(e.Modules) == 0 {
		locType = "locations"
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "ambiguous import: found package %s in multiple %s:", e.ImportPath, locType)

	for i, loc := range e.Locations {
		buf.WriteString("\n\t")
		if i < len(e.Modules) {
			m := e.Modules[i]
			buf.WriteString(m.Path())
			if m.Version() != "" {
				fmt.Fprintf(&buf, " %s", m.Version())
			}
			// TODO work out how to present source locations in error messages.
			fmt.Fprintf(&buf, " (%s)", loc[0].Dir)
		} else {
			buf.WriteString(loc[0].Dir)
		}
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
