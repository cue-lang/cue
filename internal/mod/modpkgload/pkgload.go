package modpkgload

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/par"
	"cuelang.org/go/mod/module"
)

// Registry represents a module registry, or at least this package's view of it.
type Registry interface {
	// Fetch returns the location of the contents for the given module
	// version, downloading it if necessary.
	Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error)
}

// Flags is a set of flags tracking metadata about a package.
type Flags int8

const (
	// PkgInAll indicates that the package is in the "all" package pattern,
	// regardless of whether we are loading the "all" package pattern.
	//
	// When the PkgInAll flag and PkgImportsLoaded flags are both set, the caller
	// who set the last of those flags must propagate the PkgInAll marking to all
	// of the imports of the marked package.
	PkgInAll Flags = 1 << iota

	// PkgIsRoot indicates that the package matches one of the root package
	// patterns requested by the caller.
	PkgIsRoot

	// PkgFromRoot indicates that the package is in the transitive closure of
	// imports starting at the roots. (Note that every package marked as PkgIsRoot
	// is also trivially marked PkgFromRoot.)
	PkgFromRoot

	// PkgImportsLoaded indicates that the Imports field of a
	// Pkg have been populated.
	PkgImportsLoaded
)

func (f Flags) String() string {
	var buf strings.Builder
	set := func(f1 Flags, s string) {
		if (f & f1) == 0 {
			return
		}
		if buf.Len() > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(s)
		f &^= f1
	}
	set(PkgInAll, "inAll")
	set(PkgIsRoot, "isRoot")
	set(PkgFromRoot, "fromRoot")
	set(PkgImportsLoaded, "importsLoaded")
	if f != 0 {
		set(f, fmt.Sprintf("extra%x", int(f)))
	}
	return buf.String()
}

// has reports whether all of the flags in cond are set in f.
func (f Flags) has(cond Flags) bool {
	return f&cond == cond
}

type Packages struct {
	mainModuleVersion module.Version
	mainModuleLoc     module.SourceLoc
	pkgCache          par.Cache[string, *Package]
	pkgs              []*Package
	rootPkgs          []*Package
	work              *par.Queue
	requirements      *modrequirements.Requirements
	registry          Registry
}

type Package struct {
	// Populated at construction time:
	path string // import path

	// Populated at construction time and updated by [loader.applyPkgFlags]:
	flags atomicLoadPkgFlags

	// Populated by [loader.load].
	mod          module.Version     // module providing package
	locs         []module.SourceLoc // location of source code directories
	err          error              // error loading package
	imports      []*Package         // packages imported by this one
	inStd        bool
	fromExternal bool
	altMods      []module.Version // modules that could have contained the package but did not

	// Populated by postprocessing in [Packages.buildStacks]:
	stack *Package // package importing this one in minimal import stack for this pkg
}

func (pkg *Package) ImportPath() string {
	return pkg.path
}

func (pkg *Package) FromExternalModule() bool {
	return pkg.fromExternal
}

func (pkg *Package) Locations() []module.SourceLoc {
	return pkg.locs
}

func (pkg *Package) Error() error {
	return pkg.err
}

func (pkg *Package) SetError(err error) {
	pkg.err = err
}

func (pkg *Package) HasFlags(flags Flags) bool {
	return pkg.flags.has(flags)
}

func (pkg *Package) Imports() []*Package {
	return pkg.imports
}

func (pkg *Package) Flags() Flags {
	return pkg.flags.get()
}

func (pkg *Package) Mod() module.Version {
	return pkg.mod
}

// LoadPackages loads information about all the given packages and the
// packages they import, recursively, using modules from the given
// requirements to determine which modules they might be obtained from,
// and reg to download module contents.
//
// rootPkgPaths should only contain canonical import paths.
func LoadPackages(
	ctx context.Context,
	mainModulePath string,
	mainModuleLoc module.SourceLoc,
	rs *modrequirements.Requirements,
	reg Registry,
	rootPkgPaths []string,
) *Packages {
	pkgs := &Packages{
		mainModuleVersion: module.MustNewVersion(mainModulePath, ""),
		mainModuleLoc:     mainModuleLoc,
		requirements:      rs,
		registry:          reg,
		work:              par.NewQueue(runtime.GOMAXPROCS(0)),
	}
	inRoots := map[*Package]bool{}
	pkgs.rootPkgs = make([]*Package, 0, len(rootPkgPaths))
	for _, p := range rootPkgPaths {
		// TODO the original logic didn't add PkgInAll here. Not sure why,
		// and that might be a lurking problem.
		if root := pkgs.addPkg(ctx, p, PkgIsRoot|PkgInAll); !inRoots[root] {
			pkgs.rootPkgs = append(pkgs.rootPkgs, root)
			inRoots[root] = true
		}
	}
	<-pkgs.work.Idle()
	pkgs.buildStacks()
	return pkgs
}

// buildStacks computes minimal import stacks for each package,
// for use in error messages. When it completes, packages that
// are part of the original root set have pkg.stack == nil,
// and other packages have pkg.stack pointing at the next
// package up the import stack in their minimal chain.
// As a side effect, buildStacks also constructs ld.pkgs,
// the list of all packages loaded.
func (pkgs *Packages) buildStacks() {
	for _, pkg := range pkgs.rootPkgs {
		pkg.stack = pkg // sentinel to avoid processing in next loop
		pkgs.pkgs = append(pkgs.pkgs, pkg)
	}
	for i := 0; i < len(pkgs.pkgs); i++ { // not range: appending to pkgs.pkgs in loop
		pkg := pkgs.pkgs[i]
		for _, next := range pkg.imports {
			if next.stack == nil {
				next.stack = pkg
				pkgs.pkgs = append(pkgs.pkgs, next)
			}
		}
	}
	for _, pkg := range pkgs.rootPkgs {
		pkg.stack = nil
	}
}

func (pkgs *Packages) Roots() []*Package {
	return slices.Clip(pkgs.rootPkgs)
}

func (pkgs *Packages) All() []*Package {
	return slices.Clip(pkgs.pkgs)
}

// Pkg obtains a given package given its canonical import path.
func (pkgs *Packages) Pkg(canonicalPkgPath string) *Package {
	pkg, _ := pkgs.pkgCache.Get(canonicalPkgPath)
	return pkg
}

func (pkgs *Packages) addPkg(ctx context.Context, pkgPath string, flags Flags) *Package {
	pkg := pkgs.pkgCache.Do(pkgPath, func() *Package {
		pkg := &Package{
			path: pkgPath,
		}
		pkgs.applyPkgFlags(pkg, flags)

		pkgs.work.Add(func() { pkgs.load(ctx, pkg) })
		return pkg
	})

	// Ensure the flags apply even if the package already existed.
	pkgs.applyPkgFlags(pkg, flags)
	return pkg
}

func (pkgs *Packages) load(ctx context.Context, pkg *Package) {
	if IsStdlibPackage(pkg.path) {
		pkg.inStd = true
		return
	}
	pkg.fromExternal = pkg.mod != pkgs.mainModuleVersion
	pkg.mod, pkg.locs, pkg.altMods, pkg.err = pkgs.importFromModules(ctx, pkg.path)
	if pkg.err != nil {
		return
	}
	if pkgs.mainModuleVersion.Path() == pkg.mod.Path() {
		pkgs.applyPkgFlags(pkg, PkgInAll)
	}
	pkgQual := module.ParseImportPath(pkg.path).Qualifier
	if pkgQual == "" {
		pkg.err = fmt.Errorf("cannot determine package name from import path %q", pkg.path)
		return
	}
	importsMap := make(map[string]bool)
	foundPackageFile := false
	for _, loc := range pkg.locs {
		// Layer an iterator whose yield function keeps track of whether we have seen
		// a single valid CUE file in the package directory.
		// Otherwise we would have to iterate twice, causing twice as many io/fs operations.
		pkgFileIter := func(yield func(modimports.ModuleFile, error) bool) {
			yield2 := func(mf modimports.ModuleFile, err error) bool {
				foundPackageFile = err == nil
				return yield(mf, err)
			}
			modimports.PackageFiles(loc.FS, loc.Dir, pkgQual)(yield2)
		}
		imports, err := modimports.AllImports(pkgFileIter)
		if err != nil {
			pkg.err = fmt.Errorf("cannot get imports: %v", err)
			return
		}
		for _, imp := range imports {
			importsMap[imp] = true
		}
	}
	if !foundPackageFile {
		pkg.err = fmt.Errorf("no files in package directory with package name %q", pkgQual)
		return
	}
	imports := make([]string, 0, len(importsMap))
	for imp := range importsMap {
		imports = append(imports, imp)
	}
	sort.Strings(imports) // Make the algorithm deterministic for tests.

	pkg.imports = make([]*Package, 0, len(imports))
	var importFlags Flags
	if pkg.flags.has(PkgInAll) {
		importFlags = PkgInAll
	}
	for _, path := range imports {
		pkg.imports = append(pkg.imports, pkgs.addPkg(ctx, path, importFlags))
	}
	pkgs.applyPkgFlags(pkg, PkgImportsLoaded)
}

// applyPkgFlags updates pkg.flags to set the given flags and propagate the
// (transitive) effects of those flags, possibly loading or enqueueing further
// packages as a result.
func (pkgs *Packages) applyPkgFlags(pkg *Package, flags Flags) {
	if flags == 0 {
		return
	}

	if flags.has(PkgInAll) {
		// This package matches a root pattern by virtue of being in "all".
		flags |= PkgIsRoot
	}
	if flags.has(PkgIsRoot) {
		flags |= PkgFromRoot
	}

	old := pkg.flags.update(flags)
	new := old | flags
	if new == old || !new.has(PkgImportsLoaded) {
		// We either didn't change the state of pkg, or we don't know anything about
		// its dependencies yet. Either way, we can't usefully load its test or
		// update its dependencies.
		return
	}

	if new.has(PkgInAll) && !old.has(PkgInAll|PkgImportsLoaded) {
		// We have just marked pkg with pkgInAll, or we have just loaded its
		// imports, or both. Now is the time to propagate pkgInAll to the imports.
		for _, dep := range pkg.imports {
			pkgs.applyPkgFlags(dep, PkgInAll)
		}
	}

	if new.has(PkgFromRoot) && !old.has(PkgFromRoot|PkgImportsLoaded) {
		for _, dep := range pkg.imports {
			pkgs.applyPkgFlags(dep, PkgFromRoot)
		}
	}
}

// An atomicLoadPkgFlags stores a loadPkgFlags for which individual flags can be
// added atomically.
type atomicLoadPkgFlags struct {
	bits atomic.Int32
}

// update sets the given flags in af (in addition to any flags already set).
//
// update returns the previous flag state so that the caller may determine which
// flags were newly-set.
func (af *atomicLoadPkgFlags) update(flags Flags) (old Flags) {
	for {
		old := af.bits.Load()
		new := old | int32(flags)
		if new == old || af.bits.CompareAndSwap(old, new) {
			return Flags(old)
		}
	}
}

func (af *atomicLoadPkgFlags) get() Flags {
	return Flags(af.bits.Load())
}

// has reports whether all of the flags in cond are set in af.
func (af *atomicLoadPkgFlags) has(cond Flags) bool {
	return Flags(af.bits.Load())&cond == cond
}

func IsStdlibPackage(pkgPath string) bool {
	firstElem, _, _ := strings.Cut(pkgPath, "/")
	return !strings.Contains(firstElem, ".")
}
