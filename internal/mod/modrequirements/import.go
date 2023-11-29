package modrequirements

//
//import (
//	"context"
//	"errors"
//	"fmt"
//	"go/build"
//	"path/filepath"
//	"strings"
//
//	"golang.org/x/mod/module"
//)
//
//// importFromModules finds the module and directory in the dependency graph of
//// rs containing the package with the given import path. If mg is nil,
//// importFromModules attempts to locate the module using only the main module
//// and the roots of rs before it loads the full graph.
////
//// The answer must be unique: importFromModules returns an error if multiple
//// modules are observed to provide the same package.
////
//// importFromModules can return a module with an empty m.Path, for packages in
//// the standard library.
////
//// If the package is not present in any module selected from the requirement
//// graph, importFromModules returns an *ImportMissingError.
////
//// If the package is present in exactly one module, importFromModules will
//// return the module, its root directory, and a list of other modules that
//// lexically could have provided the package but did not.
//func importFromModules(ctx context.Context, path string, rs *Requirements, mg *ModuleGraph) (m module.Version, modroot, dir string, altMods []module.Version, err error) {
//	invalidf := func(format string, args ...interface{}) (module.Version, string, string, []module.Version, error) {
//		return module.Version{}, "", "", nil, &invalidImportError{
//			importPath: path,
//			err:        fmt.Errorf(format, args...),
//		}
//	}
//
//	if strings.Contains(path, "@") {
//		return invalidf("import path %q should not have @version", path)
//	}
//	if build.IsLocalImport(path) {
//		return invalidf("%q is relative, but relative import paths are not supported in module mode", path)
//	}
//	if filepath.IsAbs(path) {
//		return invalidf("%q is not a package path; see 'go help packages'", path)
//	}
//	// Before any further lookup, check that the path is valid.
//	if err := module.CheckImportPath(path); err != nil {
//		return module.Version{}, "", "", nil, &invalidImportError{importPath: path, err: err}
//	}
//
//	// Check each module on the build list.
//	var dirs, roots []string
//	var mods []module.Version
//
//	// Is the package in the standard library?
//	pathIsStd := search.IsStandardImportPath(path)
//	if pathIsStd && modindex.IsStandardPackage(cfg.GOROOT, cfg.BuildContext.Compiler, path) {
//		for _, mainModule := range MainModules.Versions() {
//			if MainModules.InGorootSrc(mainModule) {
//				if dir, ok, err := dirInModule(path, MainModules.PathPrefix(mainModule), MainModules.ModRoot(mainModule), true); err != nil {
//					return module.Version{}, MainModules.ModRoot(mainModule), dir, nil, err
//				} else if ok {
//					return mainModule, MainModules.ModRoot(mainModule), dir, nil, nil
//				}
//			}
//		}
//		dir := filepath.Join(cfg.GOROOTsrc, path)
//		modroot = cfg.GOROOTsrc
//		if str.HasPathPrefix(path, "cmd") {
//			modroot = filepath.Join(cfg.GOROOTsrc, "cmd")
//		}
//		dirs = append(dirs, dir)
//		roots = append(roots, modroot)
//		mods = append(mods, module.Version{})
//	}
//	// -mod=vendor is special.
//	// Everything must be in the main modules or the main module's or workspace's vendor directory.
//	if cfg.BuildMod == "vendor" {
//		var mainErr error
//		for _, mainModule := range MainModules.Versions() {
//			modRoot := MainModules.ModRoot(mainModule)
//			if modRoot != "" {
//				dir, mainOK, err := dirInModule(path, MainModules.PathPrefix(mainModule), modRoot, true)
//				if mainErr == nil {
//					mainErr = err
//				}
//				if mainOK {
//					mods = append(mods, mainModule)
//					dirs = append(dirs, dir)
//					roots = append(roots, modRoot)
//				}
//			}
//		}
//
//		if HasModRoot() {
//			vendorDir := VendorDir()
//			dir, vendorOK, _ := dirInModule(path, "", vendorDir, false)
//			if vendorOK {
//				readVendorList(vendorDir)
//				// TODO(#60922): It's possible for a package to manually have been added to the
//				// vendor directory, causing the dirInModule to succeed, but no vendorPkgModule
//				// to exist, causing an empty module path to be reported. Do better checking
//				// here.
//				mods = append(mods, vendorPkgModule[path])
//				dirs = append(dirs, dir)
//				roots = append(roots, vendorDir)
//			}
//		}
//
//		if len(dirs) > 1 {
//			return module.Version{}, "", "", nil, &AmbiguousImportError{importPath: path, Dirs: dirs}
//		}
//
//		if mainErr != nil {
//			return module.Version{}, "", "", nil, mainErr
//		}
//
//		if len(dirs) == 0 {
//			return module.Version{}, "", "", nil, &ImportMissingError{Path: path}
//		}
//
//		return mods[0], roots[0], dirs[0], nil, nil
//	}
//
//	// Iterate over possible modules for the path, not all selected modules.
//	// Iterating over selected modules would make the overall loading time
//	// O(M × P) for M modules providing P imported packages, whereas iterating
//	// over path prefixes is only O(P × k) with maximum path depth k. For
//	// large projects both M and P may be very large (note that M ≤ P), but k
//	// will tend to remain smallish (if for no other reason than filesystem
//	// path limitations).
//	//
//	// We perform this iteration either one or two times. If mg is initially nil,
//	// then we first attempt to load the package using only the main module and
//	// its root requirements. If that does not identify the package, or if mg is
//	// already non-nil, then we attempt to load the package using the full
//	// requirements in mg.
//	for {
//		var sumErrMods, altMods []module.Version
//		for prefix := path; prefix != "."; prefix = pathpkg.Dir(prefix) {
//			if gover.IsToolchain(prefix) {
//				// Do not use the synthetic "go" module for "go/ast".
//				continue
//			}
//			var (
//				v  string
//				ok bool
//			)
//			if mg == nil {
//				v, ok = rs.rootSelected(prefix)
//			} else {
//				v, ok = mg.Selected(prefix), true
//			}
//			if !ok || v == "none" {
//				continue
//			}
//			m := module.Version{Path: prefix, Version: v}
//
//			root, isLocal, err := fetch(ctx, m)
//			if err != nil {
//				if sumErr := (*sumMissingError)(nil); errors.As(err, &sumErr) {
//					// We are missing a sum needed to fetch a module in the build list.
//					// We can't verify that the package is unique, and we may not find
//					// the package at all. Keep checking other modules to decide which
//					// error to report. Multiple sums may be missing if we need to look in
//					// multiple nested modules to resolve the import; we'll report them all.
//					sumErrMods = append(sumErrMods, m)
//					continue
//				}
//				// Report fetch error.
//				// Note that we don't know for sure this module is necessary,
//				// but it certainly _could_ provide the package, and even if we
//				// continue the loop and find the package in some other module,
//				// we need to look at this module to make sure the import is
//				// not ambiguous.
//				return module.Version{}, "", "", nil, err
//			}
//			if dir, ok, err := dirInModule(path, m.Path, root, isLocal); err != nil {
//				return module.Version{}, "", "", nil, err
//			} else if ok {
//				mods = append(mods, m)
//				roots = append(roots, root)
//				dirs = append(dirs, dir)
//			} else {
//				altMods = append(altMods, m)
//			}
//		}
//
//		if len(mods) > 1 {
//			// We produce the list of directories from longest to shortest candidate
//			// module path, but the AmbiguousImportError should report them from
//			// shortest to longest. Reverse them now.
//			for i := 0; i < len(mods)/2; i++ {
//				j := len(mods) - 1 - i
//				mods[i], mods[j] = mods[j], mods[i]
//				roots[i], roots[j] = roots[j], roots[i]
//				dirs[i], dirs[j] = dirs[j], dirs[i]
//			}
//			return module.Version{}, "", "", nil, &AmbiguousImportError{importPath: path, Dirs: dirs, Modules: mods}
//		}
//
//		if len(sumErrMods) > 0 {
//			for i := 0; i < len(sumErrMods)/2; i++ {
//				j := len(sumErrMods) - 1 - i
//				sumErrMods[i], sumErrMods[j] = sumErrMods[j], sumErrMods[i]
//			}
//			return module.Version{}, "", "", nil, &ImportMissingSumError{
//				importPath: path,
//				mods:       sumErrMods,
//				found:      len(mods) > 0,
//			}
//		}
//
//		if len(mods) == 1 {
//			// We've found the unique module containing the package.
//			// However, in order to actually compile it we need to know what
//			// Go language version to use, which requires its go.mod file.
//			//
//			// If the module graph is pruned and this is a test-only dependency
//			// of a package in "all", we didn't necessarily load that file
//			// when we read the module graph, so do it now to be sure.
//			if !skipModFile && cfg.BuildMod != "vendor" && mods[0].Path != "" && !MainModules.Contains(mods[0].Path) {
//				if _, err := goModSummary(mods[0]); err != nil {
//					return module.Version{}, "", "", nil, err
//				}
//			}
//			return mods[0], roots[0], dirs[0], altMods, nil
//		}
//
//		if mg != nil {
//			// We checked the full module graph and still didn't find the
//			// requested package.
//			var queryErr error
//			if !HasModRoot() {
//				queryErr = ErrNoModRoot
//			}
//			return module.Version{}, "", "", nil, &ImportMissingError{Path: path, QueryErr: queryErr, isStd: pathIsStd}
//		}
//
//		// So far we've checked the root dependencies.
//		// Load the full module graph and try again.
//		mg, err = rs.Graph(ctx)
//		if err != nil {
//			// We might be missing one or more transitive (implicit) dependencies from
//			// the module graph, so we can't return an ImportMissingError here — one
//			// of the missing modules might actually contain the package in question,
//			// in which case we shouldn't go looking for it in some new dependency.
//			return module.Version{}, "", "", nil, err
//		}
//	}
//}
