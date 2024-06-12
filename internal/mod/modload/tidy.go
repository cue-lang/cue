package modload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"path"
	"runtime"
	"slices"

	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/internal/par"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

const logging = false // TODO hook this up to CUE_DEBUG

// Registry is modload's view of a module registry.
type Registry interface {
	modrequirements.Registry
	modpkgload.Registry
	// ModuleVersions returns all the versions for the module with the given path
	// sorted in semver order.
	// If mpath has a major version suffix, only versions with that major version will
	// be returned.
	ModuleVersions(ctx context.Context, mpath string) ([]string, error)
}

type loader struct {
	mainModule    module.Version
	mainModuleLoc module.SourceLoc
	registry      Registry
	checkTidy     bool
}

// CheckTidy checks that the module file in the given main module is considered tidy.
// A module file is considered tidy when:
// - it can be parsed OK by [modfile.ParseStrict].
// - it contains a language version in canonical semver form
// - it includes valid modules for all of its dependencies
// - it does not include any unnecessary dependencies.
func CheckTidy(ctx context.Context, fsys fs.FS, modRoot string, reg Registry) error {
	_, err := tidy(ctx, fsys, modRoot, reg, true)
	return err
}

// Tidy evaluates all the requirements of the given main module, using the given
// registry to download requirements and returns a resolved and tidied module file.
func Tidy(ctx context.Context, fsys fs.FS, modRoot string, reg Registry) (*modfile.File, error) {
	return tidy(ctx, fsys, modRoot, reg, false)
}

func tidy(ctx context.Context, fsys fs.FS, modRoot string, reg Registry, checkTidy bool) (*modfile.File, error) {
	mainModuleVersion, mf, err := readModuleFile(fsys, modRoot)
	if err != nil {
		return nil, err
	}
	// TODO check that module path is well formed etc
	origRs := modrequirements.NewRequirements(mf.QualifiedModule(), reg, mf.DepVersions(), mf.DefaultMajorVersions())
	rootPkgPaths, err := modimports.AllImports(modimports.AllModuleFiles(fsys, modRoot))
	if err != nil {
		return nil, err
	}
	ld := &loader{
		mainModule: mainModuleVersion,
		registry:   reg,
		mainModuleLoc: module.SourceLoc{
			FS:  fsys,
			Dir: modRoot,
		},
		checkTidy: checkTidy,
	}

	rs, pkgs, err := ld.resolveDependencies(ctx, rootPkgPaths, origRs)
	if err != nil {
		return nil, err
	}
	for _, pkg := range pkgs.All() {
		if pkg.Error() != nil {
			return nil, fmt.Errorf("failed to resolve %q: %v", pkg.ImportPath(), pkg.Error())
		}
	}
	// TODO check whether it's changed or not.
	rs, err = ld.tidyRoots(ctx, rs, pkgs)
	if err != nil {
		return nil, fmt.Errorf("cannot tidy requirements: %v", err)
	}
	if ld.checkTidy && !equalRequirements(origRs, rs) {
		// TODO: provide a reason, perhaps in structured form rather than a string
		return nil, &ErrModuleNotTidy{}
	}
	return modfileFromRequirements(mf, rs), nil
}

// ErrModuleNotTidy is returned by CheckTidy when a module is not tidy,
// such as when there are missing or unnecessary dependencies listed.
type ErrModuleNotTidy struct {
	// Reason summarizes why the module is not tidy.
	Reason string
}

func (e ErrModuleNotTidy) Error() string {
	if e.Reason == "" {
		return "module is not tidy"
	}
	return "module is not tidy: " + e.Reason
}

func equalRequirements(rs0, rs1 *modrequirements.Requirements) bool {
	// Note that rs1.RootModules may include the unversioned local module
	// if the current module imports any packages under cue.mod/*/.
	// In such a case we want to skip over the local module when comparing,
	// just like modfileFromRequirements does when filling [modfile.File.Deps].
	// Note that we clone the slice to not modify rs1's internal slice in-place.
	rs1RootMods := slices.DeleteFunc(slices.Clone(rs1.RootModules()), module.Version.IsLocal)
	return slices.Equal(rs0.RootModules(), rs1RootMods) &&
		maps.Equal(rs0.DefaultMajorVersions(), rs1.DefaultMajorVersions())
}

func readModuleFile(fsys fs.FS, modRoot string) (module.Version, *modfile.File, error) {
	modFilePath := path.Join(modRoot, "cue.mod/module.cue")
	data, err := fs.ReadFile(fsys, modFilePath)
	if err != nil {
		return module.Version{}, nil, fmt.Errorf("cannot read cue.mod file: %v", err)
	}
	mf, err := modfile.ParseNonStrict(data, modFilePath)
	if err != nil {
		return module.Version{}, nil, err
	}
	mainModuleVersion, err := module.NewVersion(mf.QualifiedModule(), "")
	if err != nil {
		return module.Version{}, nil, fmt.Errorf("invalid module path %q: %v", mf.QualifiedModule(), err)
	}
	return mainModuleVersion, mf, nil
}

func modfileFromRequirements(old *modfile.File, rs *modrequirements.Requirements) *modfile.File {
	// TODO it would be nice to have some way of automatically including new
	// fields by default when they're added to modfile.File, but we don't
	// want to just copy the entirety of old because that includes
	// private fields too.
	mf := &modfile.File{
		Module:   old.Module,
		Language: old.Language,
		Deps:     make(map[string]*modfile.Dep),
		Source:   old.Source,
	}
	defaults := rs.DefaultMajorVersions()
	for _, v := range rs.RootModules() {
		if v.IsLocal() {
			continue
		}
		mf.Deps[v.Path()] = &modfile.Dep{
			Version: v.Version(),
			Default: defaults[v.BasePath()] == semver.Major(v.Version()),
		}
	}
	return mf
}

func (ld *loader) resolveDependencies(ctx context.Context, rootPkgPaths []string, rs *modrequirements.Requirements) (*modrequirements.Requirements, *modpkgload.Packages, error) {
	for {
		logf("---- LOADING from requirements %q", rs.RootModules())
		pkgs := modpkgload.LoadPackages(ctx, ld.mainModule.Path(), ld.mainModuleLoc, rs, ld.registry, rootPkgPaths)
		if ld.checkTidy {
			for _, pkg := range pkgs.All() {
				err := pkg.Error()
				if err == nil {
					continue
				}
				missingErr := new(modpkgload.ImportMissingError)
				// "cannot find module providing package P" is confusing here,
				// as checkTidy simply points out missing dependencies without fetching them.
				if errors.As(err, &missingErr) {
					err = &ErrModuleNotTidy{Reason: fmt.Sprintf(
						"missing dependency providing package %s", missingErr.Path)}
				}
				return nil, nil, err
			}
			// All packages could be loaded OK so there are no new
			// dependencies to be resolved and nothing to do.
			// Specifically, if there are no packages in error, then
			// resolveMissingImports will never return any entries
			// in modAddedBy and the default major versions won't
			// change.
			return rs, pkgs, nil
		}

		// TODO the original code calls updateRequirements at this point.
		// /home/rogpeppe/go/src/cmd/go/internal/modload/load.go:1124

		modAddedBy, defaultMajorVersions := ld.resolveMissingImports(ctx, pkgs, rs)
		if !maps.Equal(defaultMajorVersions, rs.DefaultMajorVersions()) {
			rs = rs.WithDefaultMajorVersions(defaultMajorVersions)
		}
		if len(modAddedBy) == 0 {
			// The roots are stable, and we've resolved all of the missing packages
			// that we can.
			logf("dependencies are stable at %q", rs.RootModules())
			return rs, pkgs, nil
		}
		toAdd := make([]module.Version, 0, len(modAddedBy))
		// TODO use maps.Keys when we can.
		for m, p := range modAddedBy {
			logf("added: %v (by %v)", modAddedBy, p.ImportPath())
			toAdd = append(toAdd, m)
		}
		module.Sort(toAdd) // to make errors deterministic
		oldRs := rs
		var err error
		rs, err = ld.updateRoots(ctx, rs, pkgs, toAdd)
		if err != nil {
			return nil, nil, err
		}
		if slices.Equal(rs.RootModules(), oldRs.RootModules()) {
			// Something is deeply wrong. resolveMissingImports gave us a non-empty
			// set of modules to add to the graph, but adding those modules had no
			// effect — either they were already in the graph, or updateRoots did not
			// add them as requested.
			panic(fmt.Sprintf("internal error: adding %v to module graph had no effect on root requirements (%v)", toAdd, rs.RootModules()))
		}
		logf("after loading, requirements: %v", rs.RootModules())
	}
}

// updatePrunedRoots returns a set of root requirements that maintains the
// invariants of the cue.mod/module.cue file needed to support graph pruning:
//
//  1. The selected version of the module providing each package marked with
//     either pkgInAll or pkgIsRoot is included as a root.
//     Note that certain root patterns (such as '...') may explode the root set
//     to contain every module that provides any package imported (or merely
//     required) by any other module.
//  2. Each root appears only once, at the selected version of its path
//     (if rs.graph is non-nil) or at the highest version otherwise present as a
//     root (otherwise).
//  3. Every module path that appears as a root in rs remains a root.
//  4. Every version in add is selected at its given version unless upgraded by
//     (the dependencies of) an existing root or another module in add.
//
// The packages in pkgs are assumed to have been loaded from either the roots of
// rs or the modules selected in the graph of rs.
//
// The above invariants together imply the graph-pruning invariants for the
// go.mod file:
//
//  1. (The import invariant.) Every module that provides a package transitively
//     imported by any package or test in the main module is included as a root.
//     This follows by induction from (1) and (3) above. Transitively-imported
//     packages loaded during this invocation are marked with pkgInAll (1),
//     and by hypothesis any transitively-imported packages loaded in previous
//     invocations were already roots in rs (3).
//
//  2. (The argument invariant.) Every module that provides a package matching
//     an explicit package pattern is included as a root. This follows directly
//     from (1): packages matching explicit package patterns are marked with
//     pkgIsRoot.
//
//  3. (The completeness invariant.) Every module that contributed any package
//     to the build is required by either the main module or one of the modules
//     it requires explicitly. This invariant is left up to the caller, who must
//     not load packages from outside the module graph but may add roots to the
//     graph, but is facilitated by (3). If the caller adds roots to the graph in
//     order to resolve missing packages, then updatePrunedRoots will retain them,
//     the selected versions of those roots cannot regress, and they will
//     eventually be written back to the main module's go.mod file.
//
// (See https://golang.org/design/36460-lazy-module-loading#invariants for more
// detail.)
func (ld *loader) updateRoots(ctx context.Context, rs *modrequirements.Requirements, pkgs *modpkgload.Packages, add []module.Version) (*modrequirements.Requirements, error) {
	roots := rs.RootModules()
	rootsUpgraded := false

	spotCheckRoot := map[module.Version]bool{}

	// “The selected version of the module providing each package marked with
	// either pkgInAll or pkgIsRoot is included as a root.”
	needSort := false
	for _, pkg := range pkgs.All() {
		if !pkg.Mod().IsValid() || !pkg.FromExternalModule() {
			// pkg was not loaded from a module dependency, so we don't need
			// to do anything special to maintain that dependency.
			continue
		}

		switch {
		case pkg.HasFlags(modpkgload.PkgInAll):
			// pkg is transitively imported by a package or test in the main module.
			// We need to promote the module that maintains it to a root: if some
			// other module depends on the main module, and that other module also
			// uses a pruned module graph, it will expect to find all of our
			// transitive dependencies by reading just our go.mod file, not the go.mod
			// files of everything we depend on.
			//
			// (This is the “import invariant” that makes graph pruning possible.)

		case pkg.HasFlags(modpkgload.PkgIsRoot):
			// pkg is a root of the package-import graph. (Generally this means that
			// it matches a command-line argument.) We want future invocations of the
			// 'go' command — such as 'go test' on the same package — to continue to
			// use the same versions of its dependencies that we are using right now.
			// So we need to bring this package's dependencies inside the pruned
			// module graph.
			//
			// Making the module containing this package a root of the module graph
			// does exactly that: if the module containing the package supports graph
			// pruning then it should satisfy the import invariant itself, so all of
			// its dependencies should be in its go.mod file, and if the module
			// containing the package does not support pruning then if we make it a
			// root we will load all of its (unpruned) transitive dependencies into
			// the module graph.
			//
			// (This is the “argument invariant”, and is important for
			// reproducibility.)

		default:
			// pkg is a dependency of some other package outside of the main module.
			// As far as we know it's not relevant to the main module (and thus not
			// relevant to consumers of the main module either), and its dependencies
			// should already be in the module graph — included in the dependencies of
			// the package that imported it.
			continue
		}
		if _, ok := rs.RootSelected(pkg.Mod().Path()); ok {
			// It is possible that the main module's go.mod file is incomplete or
			// otherwise erroneous — for example, perhaps the author forgot to 'git
			// add' their updated go.mod file after adding a new package import, or
			// perhaps they made an edit to the go.mod file using a third-party tool
			// ('git merge'?) that doesn't maintain consistency for module
			// dependencies. If that happens, ideally we want to detect the missing
			// requirements and fix them up here.
			//
			// However, we also need to be careful not to be too aggressive. For
			// transitive dependencies of external tests, the go.mod file for the
			// module containing the test itself is expected to provide all of the
			// relevant dependencies, and we explicitly don't want to pull in
			// requirements on *irrelevant* requirements that happen to occur in the
			// go.mod files for these transitive-test-only dependencies. (See the test
			// in mod_lazy_test_horizon.txt for a concrete example).
			//
			// The “goldilocks zone” seems to be to spot-check exactly the same
			// modules that we promote to explicit roots: namely, those that provide
			// packages transitively imported by the main module, and those that
			// provide roots of the package-import graph. That will catch erroneous
			// edits to the main module's go.mod file and inconsistent requirements in
			// dependencies that provide imported packages, but will ignore erroneous
			// or misleading requirements in dependencies that aren't obviously
			// relevant to the packages in the main module.
			spotCheckRoot[pkg.Mod()] = true
		} else {
			roots = append(roots, pkg.Mod())
			rootsUpgraded = true
			// The roots slice was initially sorted because rs.rootModules was sorted,
			// but the root we just added could be out of order.
			needSort = true
		}
	}

	for _, m := range add {
		if !m.IsValid() {
			panic("add contains invalid module")
		}
		if v, ok := rs.RootSelected(m.Path()); !ok || semver.Compare(v, m.Version()) < 0 {
			roots = append(roots, m)
			rootsUpgraded = true
			needSort = true
		}
	}
	if needSort {
		module.Sort(roots)
	}

	// "Each root appears only once, at the selected version of its path ….”
	for {
		var mg *modrequirements.ModuleGraph
		if rootsUpgraded {
			// We've added or upgraded one or more roots, so load the full module
			// graph so that we can update those roots to be consistent with other
			// requirements.

			rs = modrequirements.NewRequirements(ld.mainModule.Path(), ld.registry, roots, rs.DefaultMajorVersions())
			var err error
			mg, err = rs.Graph(ctx)
			if err != nil {
				return rs, err
			}
		} else {
			// Since none of the roots have been upgraded, we have no reason to
			// suspect that they are inconsistent with the requirements of any other
			// roots. Only look at the full module graph if we've already loaded it;
			// otherwise, just spot-check the explicit requirements of the roots from
			// which we loaded packages.
			if rs.GraphIsLoaded() {
				// We've already loaded the full module graph, which includes the
				// requirements of all of the root modules — even the transitive
				// requirements, if they are unpruned!
				mg, _ = rs.Graph(ctx)
			} else if !ld.spotCheckRoots(ctx, rs, spotCheckRoot) {
				// We spot-checked the explicit requirements of the roots that are
				// relevant to the packages we've loaded. Unfortunately, they're
				// inconsistent in some way; we need to load the full module graph
				// so that we can fix the roots properly.
				var err error
				mg, err = rs.Graph(ctx)
				if err != nil {
					return rs, err
				}
			}
		}

		roots = make([]module.Version, 0, len(rs.RootModules()))
		rootsUpgraded = false
		inRootPaths := map[string]bool{
			ld.mainModule.Path(): true,
		}
		for _, m := range rs.RootModules() {
			if inRootPaths[m.Path()] {
				// This root specifies a redundant path. We already retained the
				// selected version of this path when we saw it before, so omit the
				// redundant copy regardless of its version.
				//
				// When we read the full module graph, we include the dependencies of
				// every root even if that root is redundant. That better preserves
				// reproducibility if, say, some automated tool adds a redundant
				// 'require' line and then runs 'go mod tidy' to try to make everything
				// consistent, since the requirements of the older version are carried
				// over.
				//
				// So omitting a root that was previously present may *reduce* the
				// selected versions of non-roots, but merely removing a requirement
				// cannot *increase* the selected versions of other roots as a result —
				// we don't need to mark this change as an upgrade. (This particular
				// change cannot invalidate any other roots.)
				continue
			}

			var v string
			if mg == nil {
				v, _ = rs.RootSelected(m.Path())
			} else {
				v = mg.Selected(m.Path())
			}
			mv, err := module.NewVersion(m.Path(), v)
			if err != nil {
				return nil, fmt.Errorf("internal error: cannot form module version from %q@%q", m.Path(), v)
			}
			roots = append(roots, mv)
			inRootPaths[m.Path()] = true
			if v != m.Version() {
				rootsUpgraded = true
			}
		}
		// Note that rs.rootModules was already sorted by module path and version,
		// and we appended to the roots slice in the same order and guaranteed that
		// each path has only one version, so roots is also sorted by module path
		// and (trivially) version.

		if !rootsUpgraded {
			// The root set has converged: every root going into this iteration was
			// already at its selected version, although we have have removed other
			// (redundant) roots for the same path.
			break
		}
	}

	if slices.Equal(roots, rs.RootModules()) {
		// The root set is unchanged and rs was already pruned, so keep rs to
		// preserve its cached ModuleGraph (if any).
		return rs, nil
	}
	return modrequirements.NewRequirements(ld.mainModule.Path(), ld.registry, roots, rs.DefaultMajorVersions()), nil
}

// resolveMissingImports returns a set of modules that could be added as
// dependencies in order to resolve missing packages from pkgs.
//
// It returns a map from each new module version to
// the first missing package that module would resolve.
func (ld *loader) resolveMissingImports(ctx context.Context, pkgs *modpkgload.Packages, rs *modrequirements.Requirements) (modAddedBy map[module.Version]*modpkgload.Package, defaultMajorVersions map[string]string) {
	type pkgMod struct {
		pkg          *modpkgload.Package
		needsDefault *bool
		mods         *[]module.Version
	}
	var pkgMods []pkgMod
	work := par.NewQueue(runtime.GOMAXPROCS(0))
	for _, pkg := range pkgs.All() {
		pkg := pkg
		if pkg.Error() == nil {
			continue
		}
		if !errors.As(pkg.Error(), new(*modpkgload.ImportMissingError)) {
			// Leave other errors to be reported outside of the module resolution logic.
			continue
		}
		logf("querying %q", pkg.ImportPath())
		var mods []module.Version // updated asynchronously.
		var needsDefault bool
		work.Add(func() {
			var err error
			mods, needsDefault, err = ld.queryImport(ctx, pkg.ImportPath(), rs)
			if err != nil {
				// pkg.err was already non-nil, so we can reasonably attribute the error
				// for pkg to either the original error or the one returned by
				// queryImport. The existing error indicates only that we couldn't find
				// the package, whereas the query error also explains why we didn't fix
				// the problem — so we prefer the latter.
				pkg.SetError(err)
			}

			// err is nil, but we intentionally leave pkg.err non-nil: we still haven't satisfied other invariants of a
			// successfully-loaded package, such as scanning and loading the imports
			// of that package. If we succeed in resolving the new dependency graph,
			// the caller can reload pkg and update the error at that point.
			//
			// Even then, the package might not be loaded from the version we've
			// identified here. The module may be upgraded by some other dependency,
			// or by a transitive dependency of mod itself, or — less likely — the
			// package may be rejected by an AllowPackage hook or rendered ambiguous
			// by some other newly-added or newly-upgraded dependency.
		})

		pkgMods = append(pkgMods, pkgMod{pkg: pkg, mods: &mods, needsDefault: &needsDefault})
	}
	<-work.Idle()

	modAddedBy = map[module.Version]*modpkgload.Package{}
	defaultMajorVersions = make(map[string]string)
	for m, v := range rs.DefaultMajorVersions() {
		defaultMajorVersions[m] = v
	}
	for _, pm := range pkgMods {
		pkg, mods, needsDefault := pm.pkg, *pm.mods, *pm.needsDefault
		for _, mod := range mods {
			// TODO support logging progress messages like this but without printing to stderr?
			logf("cue: found potential %s in %v", pkg.ImportPath(), mod)
			if modAddedBy[mod] == nil {
				modAddedBy[mod] = pkg
			}
			if needsDefault {
				defaultMajorVersions[mod.BasePath()] = semver.Major(mod.Version())
			}
		}
	}

	return modAddedBy, defaultMajorVersions
}

// tidyRoots returns a minimal set of root requirements that maintains the
// invariants of the cue.mod/module.cue file needed to support graph pruning for the given
// packages:
//
//  1. For each package marked with PkgInAll, the module path that provided that
//     package is included as a root.
//  2. For all packages, the module that provided that package either remains
//     selected at the same version or is upgraded by the dependencies of a
//     root.
//
// If any module that provided a package has been upgraded above its previous
// version, the caller may need to reload and recompute the package graph.
//
// To ensure that the loading process eventually converges, the caller should
// add any needed roots from the tidy root set (without removing existing untidy
// roots) until the set of roots has converged.
func (ld *loader) tidyRoots(ctx context.Context, old *modrequirements.Requirements, pkgs *modpkgload.Packages) (*modrequirements.Requirements, error) {
	var (
		roots      []module.Version
		pathIsRoot = map[string]bool{ld.mainModule.Path(): true}
	)
	// We start by adding roots for every package in "all".
	//
	// Once that is done, we may still need to add more roots to cover upgraded or
	// otherwise-missing test dependencies for packages in "all". For those test
	// dependencies, we prefer to add roots for packages with shorter import
	// stacks first, on the theory that the module requirements for those will
	// tend to fill in the requirements for their transitive imports (which have
	// deeper import stacks). So we add the missing dependencies for one depth at
	// a time, starting with the packages actually in "all" and expanding outwards
	// until we have scanned every package that was loaded.
	var (
		queue  []*modpkgload.Package
		queued = map[*modpkgload.Package]bool{}
	)
	for _, pkg := range pkgs.All() {
		if !pkg.HasFlags(modpkgload.PkgInAll) {
			continue
		}
		if pkg.FromExternalModule() && !pathIsRoot[pkg.Mod().Path()] {
			roots = append(roots, pkg.Mod())
			pathIsRoot[pkg.Mod().Path()] = true
		}
		queue = append(queue, pkg)
		queued[pkg] = true
	}
	module.Sort(roots)
	tidy := modrequirements.NewRequirements(ld.mainModule.Path(), ld.registry, roots, old.DefaultMajorVersions())

	for len(queue) > 0 {
		roots = tidy.RootModules()
		mg, err := tidy.Graph(ctx)
		if err != nil {
			return nil, err
		}

		prevQueue := queue
		queue = nil
		for _, pkg := range prevQueue {
			m := pkg.Mod()
			if m.Path() == "" {
				continue
			}
			for _, dep := range pkg.Imports() {
				if !queued[dep] {
					queue = append(queue, dep)
					queued[dep] = true
				}
			}
			if !pathIsRoot[m.Path()] {
				if s := mg.Selected(m.Path()); semver.Compare(s, m.Version()) < 0 {
					roots = append(roots, m)
					pathIsRoot[m.Path()] = true
				}
			}
		}

		if tidyRoots := tidy.RootModules(); len(roots) > len(tidyRoots) {
			module.Sort(roots)
			tidy = modrequirements.NewRequirements(ld.mainModule.Path(), ld.registry, roots, tidy.DefaultMajorVersions())
		}
	}

	if _, err := tidy.Graph(ctx); err != nil {
		return nil, err
	}

	// TODO the original code had some logic I don't properly understand,
	// related to https://go.dev/issue/60313, that _may_ be relevant only
	// to test-only dependencies, which we don't have, so leave it out for now.

	return tidy, nil
}

// spotCheckRoots reports whether the versions of the roots in rs satisfy the
// explicit requirements of the modules in mods.
func (ld *loader) spotCheckRoots(ctx context.Context, rs *modrequirements.Requirements, mods map[module.Version]bool) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	work := par.NewQueue(runtime.GOMAXPROCS(0))
	for m := range mods {
		m := m
		work.Add(func() {
			if ctx.Err() != nil {
				return
			}

			require, err := ld.registry.Requirements(ctx, m)
			if err != nil {
				cancel()
				return
			}

			for _, r := range require {
				if v, ok := rs.RootSelected(r.Path()); ok && semver.Compare(v, r.Version()) < 0 {
					cancel()
					return
				}
			}
		})
	}
	<-work.Idle()

	if ctx.Err() != nil {
		// Either we failed a spot-check, or the caller no longer cares about our
		// answer anyway.
		return false
	}

	return true
}

func logf(f string, a ...any) {
	if logging {
		log.Printf(f, a...)
	}
}
