package modpkgload

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/par"
	"cuelang.org/go/mod/module"
)

// Registry represents a module registry, or at least this package's view of it.
type Registry interface {
	// Fetch returns the location of the contents for the given module
	// version, downloading it if necessary.
	// It returns an error that satisfies [errors.Is]([modregistry.ErrNotFound]) if the
	// module is not present in the store at this version.
	Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error)
}

// CachedRegistry is optionally implemented by a registry that
// implements a cache.
type CachedRegistry interface {
	// FetchFromCache looks up the given module in the cache.
	// It returns an error that satisfies [errors.Is]([modregistry.ErrNotFound]) if the
	// module is not present in the cache at this version or if there
	// is no cache.
	FetchFromCache(mv module.Version) (module.SourceLoc, error)
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
	mainModuleVersion    module.Version
	mainModuleLoc        module.SourceLoc
	shouldIncludePkgFile func(pkgPath string, mod module.Version, fsys fs.FS, mf modimports.ModuleFile) bool
	pkgCache             par.Cache[string, *Package]
	pkgs                 []*Package
	rootPkgs             []*Package
	work                 *par.Queue
	requirements         *modrequirements.Requirements
	registry             Registry
}

type Package struct {
	// Populated at construction time:
	path string // import path

	// Populated at construction time and updated by [loader.applyPkgFlags]:
	flags atomicLoadPkgFlags

	// Populated by [loader.load].
	mod          module.Version   // module providing package
	modRoot      module.SourceLoc // root location of module
	files        []modimports.ModuleFile
	locs         []module.SourceLoc // location of source code directories
	err          error              // error loading package
	imports      []*Package         // packages imported by this one
	inStd        bool
	fromExternal bool

	// Populated by postprocessing in [Packages.buildStacks]:
	stack *Package // package importing this one in minimal import stack for this pkg
}

func (pkg *Package) ImportPath() string {
	return pkg.path
}

func (pkg *Package) FromExternalModule() bool {
	return pkg.fromExternal
}

func (pkg *Package) IsStdlibPackage() bool {
	return pkg.inStd
}

func (pkg *Package) Locations() []module.SourceLoc {
	return pkg.locs
}

func (pkg *Package) Files() []modimports.ModuleFile {
	return pkg.files
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

func (pkg *Package) ModRoot() module.SourceLoc {
	return pkg.modRoot
}

// LoadPackages loads information about all the given packages and the
// packages they import, recursively, using modules from the given
// requirements to determine which modules they might be obtained from,
// and reg to download module contents.
//
// rootPkgPaths should only contain canonical import paths.
//
// The shouldIncludePkgFile function is used to determine whether a
// given file in a package should be considered to be part of the build.
// If it returns true for a package, the file's imports will be followed.
// A nil value corresponds to a function that always returns true.
// It may be called concurrently.
func LoadPackages(
	ctx context.Context,
	mainModulePath string,
	mainModuleLoc module.SourceLoc,
	rs *modrequirements.Requirements,
	reg Registry,
	rootPkgPaths []string,
	shouldIncludePkgFile func(pkgPath string, mod module.Version, fsys fs.FS, mf modimports.ModuleFile) bool,
) *Packages {
	if shouldIncludePkgFile == nil {
		shouldIncludePkgFile = func(pkgPath string, mod module.Version, fsys fs.FS, mf modimports.ModuleFile) bool { return true }
	}
	pkgs := &Packages{
		mainModuleVersion:    module.MustNewVersion(mainModulePath, ""),
		mainModuleLoc:        mainModuleLoc,
		shouldIncludePkgFile: shouldIncludePkgFile,
		requirements:         rs,
		registry:             reg,
		work:                 par.NewQueue(runtime.GOMAXPROCS(0)),
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
	pkg.mod, pkg.modRoot, pkg.locs, pkg.err = pkgs.importFromModules(ctx, pkg.path)
	if pkg.err != nil {
		return
	}
	pkg.fromExternal = pkg.mod != pkgs.mainModuleVersion
	if pkgs.mainModuleVersion.Path() == pkg.mod.Path() {
		pkgs.applyPkgFlags(pkg, PkgInAll)
	}
	ip := ast.ParseImportPath(pkg.path)
	pkgQual := ip.Qualifier
	switch pkgQual {
	case "":
		// If we are tidying a module which imports "foo.com/bar-baz@v0",
		// a qualifier is needed as no valid package name can be derived from the path.
		// Don't fail here, however, as tidy can simply ensure that bar-baz is a dependency,
		// much like how `cue mod get foo.com/bar-baz` works just fine to add a module.
		// Any command which later attempts to actually import bar-baz without a qualifier
		// will result in a helpful error which the user can resolve at that point.
		return
	case "_":
		pkg.err = fmt.Errorf("_ is not a valid import path qualifier in %q", pkg.path)
		return
	}
	importsMap := make(map[string]bool)
	foundPackageFile := false
	excludedPackageFiles := 0
	var files []modimports.ModuleFile
	for _, loc := range pkg.locs {
		// Layer an iterator whose yield function keeps track of whether we have seen
		// a single valid CUE file in the package directory.
		// Otherwise we would have to iterate twice, causing twice as many io/fs operations.
		pkgFileIter := func(yield func(modimports.ModuleFile, error) bool) {
			modimports.PackageFiles(loc.FS, loc.Dir, pkgQual)(func(mf modimports.ModuleFile, err error) bool {
				if err != nil {
					return yield(mf, err)
				}
				ip1 := ip
				ip1.Qualifier = mf.Syntax.PackageName()
				if !pkgs.shouldIncludePkgFile(ip1.String(), pkg.mod, loc.FS, mf) {
					excludedPackageFiles++
					return true
				}
				foundPackageFile = true
				files = append(files, mf)
				return yield(mf, err)
			})
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
		if excludedPackageFiles > 0 {
			pkg.err = fmt.Errorf("no files in package directory with package name %q (%d files were excluded)", pkgQual, excludedPackageFiles)
		} else {
			pkg.err = fmt.Errorf("no files in package directory with package name %q", pkgQual)
		}
		return
	}
	pkg.files = files
	// Make the algorithm deterministic for tests.
	imports := slices.Sorted(maps.Keys(importsMap))

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
