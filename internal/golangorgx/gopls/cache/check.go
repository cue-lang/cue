// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/cache/typerefs"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/filecache"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/util/bug"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

// Various optimizations that should not affect correctness.
const (
	preserveImportGraph = true // hold on to the import graph for open packages
)

type unit = struct{}

// A typeCheckBatch holds data for a logical type-checking operation, which may
// type-check many unrelated packages.
//
// It shares state such as parsed files and imports, to optimize type-checking
// for packages with overlapping dependency graphs.
type typeCheckBatch struct {
	activePackageCache interface {
		getActivePackage(id PackageID) *Package
		setActivePackage(id PackageID, pkg *Package)
	}
	syntaxIndex map[PackageID]int // requested ID -> index in ids
	pre         preTypeCheck
	post        postTypeCheck
	handles     map[PackageID]*packageHandle
	parseCache  *parseCache
	fset        *token.FileSet // describes all parsed or imported files
	cpulimit    chan unit      // concurrency limiter for CPU-bound operations

	mu             sync.Mutex
	syntaxPackages map[PackageID]*futurePackage // results of processing a requested package; may hold (nil, nil)
	importPackages map[PackageID]*futurePackage // package results to use for importing
}

// A futurePackage is a future result of type checking or importing a package,
// to be cached in a map.
//
// The goroutine that creates the futurePackage is responsible for evaluating
// its value, and closing the done channel.
type futurePackage struct {
	done chan unit
	v    pkgOrErr
}

type pkgOrErr struct {
	pkg *types.Package
	err error
}

// An importGraph holds selected results of a type-checking pass, to be re-used
// by subsequent snapshots.
type importGraph struct {
	fset    *token.FileSet          // fileset used for type checking imports
	depKeys map[PackageID]file.Hash // hash of direct dependencies for this graph
	imports map[PackageID]pkgOrErr  // results of type checking
}

// Package visiting functions used by forEachPackage; see the documentation of
// forEachPackage for details.
type (
	preTypeCheck  = func(int, *packageHandle) bool // false => don't type check
	postTypeCheck = func(int, *Package)
)

// forEachPackage does a pre- and post- order traversal of the packages
// specified by ids using the provided pre and post functions.
//
// The pre func is optional. If set, pre is evaluated after the package
// handle has been constructed, but before type-checking. If pre returns false,
// type-checking is skipped for this package handle.
//
// post is called with a syntax package after type-checking completes
// successfully. It is only called if pre returned true.
//
// Both pre and post may be called concurrently.
func (s *Snapshot) forEachPackage(ctx context.Context, ids []PackageID, pre preTypeCheck, post postTypeCheck) error {
	ctx, done := event.Start(ctx, "cache.forEachPackage", tag.PackageCount.Of(len(ids)))
	defer done()

	return nil
}

// A packageHandle holds inputs required to compute a Package, including
// metadata, derived diagnostics, files, and settings. Additionally,
// packageHandles manage a key for these inputs, to use in looking up
// precomputed results.
//
// packageHandles may be invalid following an invalidation via snapshot.clone,
// but the handles returned by getPackageHandles will always be valid.
//
// packageHandles are critical for implementing "precise pruning" in gopls:
// packageHandle.key is a hash of a precise set of inputs, such as package
// files and "reachable" syntax, that may affect type checking.
//
// packageHandles also keep track of state that allows gopls to compute, and
// then quickly recompute, these keys. This state is split into two categories:
//   - local state, which depends only on the package's local files and metadata
//   - other state, which includes data derived from dependencies.
//
// Dividing the data in this way allows gopls to minimize invalidation when a
// package is modified. For example, any change to a package file fully
// invalidates the package handle. On the other hand, if that change was not
// metadata-affecting it may be the case that packages indirectly depending on
// the modified package are unaffected by the change. For that reason, we have
// two types of invalidation, corresponding to the two types of data above:
//   - deletion of the handle, which occurs when the package itself changes
//   - clearing of the validated field, which marks the package as possibly
//     invalid.
//
// With the second type of invalidation, packageHandles are re-evaluated from the
// bottom up. If this process encounters a packageHandle whose deps have not
// changed (as detected by the depkeys field), then the packageHandle in
// question must also not have changed, and we need not re-evaluate its key.
type packageHandle struct {
	mp *metadata.Package

	// loadDiagnostics memoizes the result of processing error messages from
	// go/packages (i.e. `go list`).
	//
	// These are derived from metadata using a snapshot. Since they depend on
	// file contents (for translating positions), they should theoretically be
	// invalidated by file changes, but historically haven't been. In practice
	// they are rare and indicate a fundamental error that needs to be corrected
	// before development can continue, so it may not be worth significant
	// engineering effort to implement accurate invalidation here.
	//
	// TODO(rfindley): loadDiagnostics are out of place here, as they don't
	// directly relate to type checking. We should perhaps move the caching of
	// load diagnostics to an entirely separate component, so that Packages need
	// only be concerned with parsing and type checking.
	// (Nevertheless, since the lifetime of load diagnostics matches that of the
	// Metadata, it is convenient to memoize them here.)
	loadDiagnostics []*Diagnostic

	// Local data:

	// localInputs holds all local type-checking localInputs, excluding
	// dependencies.
	localInputs typeCheckInputs
	// localKey is a hash of localInputs.
	localKey file.Hash
	// refs is the result of syntactic dependency analysis produced by the
	// typerefs package.
	refs map[string][]typerefs.Symbol

	// Data derived from dependencies:

	// validated indicates whether the current packageHandle is known to have a
	// valid key. Invalidated package handles are stored for packages whose
	// type information may have changed.
	validated bool
	// depKeys records the key of each dependency that was used to calculate the
	// key above. If the handle becomes invalid, we must re-check that each still
	// matches.
	depKeys map[PackageID]file.Hash
	// key is the hashed key for the package.
	//
	// It includes the all bits of the transitive closure of
	// dependencies's sources.
	key file.Hash
}

// clone returns a copy of the receiver with the validated bit set to the
// provided value.
func (ph *packageHandle) clone(validated bool) *packageHandle {
	copy := *ph
	copy.validated = validated
	return &copy
}

// A packageHandleBuilder computes a batch of packageHandles concurrently,
// sharing computed transitive reachability sets used to compute package keys.
type packageHandleBuilder struct {
	s *Snapshot

	// nodes are assembled synchronously.
	nodes map[typerefs.IndexID]*handleNode

	// transitiveRefs is incrementally evaluated as package handles are built.
	transitiveRefsMu sync.Mutex
	transitiveRefs   map[typerefs.IndexID]*partialRefs // see getTransitiveRefs
}

// A handleNode represents a to-be-computed packageHandle within a graph of
// predecessors and successors.
//
// It is used to implement a bottom-up construction of packageHandles.
type handleNode struct {
	mp              *metadata.Package
	idxID           typerefs.IndexID
	ph              *packageHandle
	err             error
	preds           []*handleNode
	succs           map[PackageID]*handleNode
	unfinishedSuccs int32
}

// partialRefs maps names declared by a given package to their set of
// transitive references.
//
// If complete is set, refs is known to be complete for the package in
// question. Otherwise, it may only map a subset of all names declared by the
// package.
type partialRefs struct {
	refs     map[string]*typerefs.PackageSet
	complete bool
}

// getTransitiveRefs gets or computes the set of transitively reachable
// packages for each exported name in the package specified by id.
//
// The operation may fail if building a predecessor failed. If and only if this
// occurs, the result will be nil.
func (b *packageHandleBuilder) getTransitiveRefs(pkgID PackageID) map[string]*typerefs.PackageSet {
	b.transitiveRefsMu.Lock()
	defer b.transitiveRefsMu.Unlock()

	idxID := b.s.pkgIndex.IndexID(pkgID)
	trefs, ok := b.transitiveRefs[idxID]
	if !ok {
		trefs = &partialRefs{
			refs: make(map[string]*typerefs.PackageSet),
		}
		b.transitiveRefs[idxID] = trefs
	}

	if !trefs.complete {
		trefs.complete = true
		ph := b.nodes[idxID].ph
		for name := range ph.refs {
			if ('A' <= name[0] && name[0] <= 'Z') || token.IsExported(name) {
				if _, ok := trefs.refs[name]; !ok {
					pkgs := b.s.pkgIndex.NewSet()
					for _, sym := range ph.refs[name] {
						pkgs.Add(sym.Package)
						otherSet := b.getOneTransitiveRefLocked(sym)
						pkgs.Union(otherSet)
					}
					trefs.refs[name] = pkgs
				}
			}
		}
	}

	return trefs.refs
}

// getOneTransitiveRefLocked computes the full set packages transitively
// reachable through the given sym reference.
//
// It may return nil if the reference is invalid (i.e. the referenced name does
// not exist).
func (b *packageHandleBuilder) getOneTransitiveRefLocked(sym typerefs.Symbol) *typerefs.PackageSet {
	assert(token.IsExported(sym.Name), "expected exported symbol")

	trefs := b.transitiveRefs[sym.Package]
	if trefs == nil {
		trefs = &partialRefs{
			refs:     make(map[string]*typerefs.PackageSet),
			complete: false,
		}
		b.transitiveRefs[sym.Package] = trefs
	}

	pkgs, ok := trefs.refs[sym.Name]
	if ok && pkgs == nil {
		// See below, where refs is set to nil before recursing.
		bug.Reportf("cycle detected to %q in reference graph", sym.Name)
	}

	// Note that if (!ok && trefs.complete), the name does not exist in the
	// referenced package, and we should not write to trefs as that may introduce
	// a race.
	if !ok && !trefs.complete {
		n := b.nodes[sym.Package]
		if n == nil {
			// We should always have IndexID in our node set, because symbol references
			// should only be recorded for packages that actually exist in the import graph.
			//
			// However, it is not easy to prove this (typerefs are serialized and
			// deserialized), so make this code temporarily defensive while we are on a
			// point release.
			//
			// TODO(rfindley): in the future, we should turn this into an assertion.
			bug.Reportf("missing reference to package %s", b.s.pkgIndex.PackageID(sym.Package))
			return nil
		}

		// Break cycles. This is perhaps overly defensive as cycles should not
		// exist at this point: metadata cycles should have been broken at load
		// time, and intra-package reference cycles should have been contracted by
		// the typerefs algorithm.
		//
		// See the "cycle detected" bug report above.
		trefs.refs[sym.Name] = nil

		pkgs := b.s.pkgIndex.NewSet()
		for _, sym2 := range n.ph.refs[sym.Name] {
			pkgs.Add(sym2.Package)
			otherSet := b.getOneTransitiveRefLocked(sym2)
			pkgs.Union(otherSet)
		}
		trefs.refs[sym.Name] = pkgs
	}

	return pkgs
}

// evaluatePackageHandle validates and/or computes the key of ph, setting key,
// depKeys, and the validated flag on ph.
//
// It uses prevPH to avoid recomputing keys that can't have changed, since
// their depKeys did not change.
//
// See the documentation for packageHandle for more details about packageHandle
// state, and see the documentation for the typerefs package for more details
// about precise reachability analysis.
func (b *packageHandleBuilder) evaluatePackageHandle(prevPH *packageHandle, n *handleNode) error {
	// Opt: if no dep keys have changed, we need not re-evaluate the key.
	if prevPH != nil {
		depsChanged := false
		assert(len(prevPH.depKeys) == len(n.succs), "mismatching dep count")
		for id, succ := range n.succs {
			oldKey, ok := prevPH.depKeys[id]
			assert(ok, "missing dep")
			if oldKey != succ.ph.key {
				depsChanged = true
				break
			}
		}
		if !depsChanged {
			return nil // key cannot have changed
		}
	}

	// Deps have changed, so we must re-evaluate the key.
	n.ph.depKeys = make(map[PackageID]file.Hash)

	// See the typerefs package: the reachable set of packages is defined to be
	// the set of packages containing syntax that is reachable through the
	// exported symbols in the dependencies of n.ph.
	reachable := b.s.pkgIndex.NewSet()
	for depID, succ := range n.succs {
		n.ph.depKeys[depID] = succ.ph.key
		reachable.Add(succ.idxID)
		trefs := b.getTransitiveRefs(succ.mp.ID)
		if trefs == nil {
			// A predecessor failed to build due to e.g. context cancellation.
			return fmt.Errorf("missing transitive refs for %s", succ.mp.ID)
		}
		for _, set := range trefs {
			reachable.Union(set)
		}
	}

	// Collect reachable handles.
	var reachableHandles []*packageHandle
	// In the presence of context cancellation, any package may be missing.
	// We need all dependencies to produce a valid key.
	missingReachablePackage := false
	reachable.Elems(func(id typerefs.IndexID) {
		dh := b.nodes[id]
		if dh == nil {
			missingReachablePackage = true
		} else {
			assert(dh.ph.validated, "unvalidated dependency")
			reachableHandles = append(reachableHandles, dh.ph)
		}
	})
	if missingReachablePackage {
		return fmt.Errorf("missing reachable package")
	}
	// Sort for stability.
	sort.Slice(reachableHandles, func(i, j int) bool {
		return reachableHandles[i].mp.ID < reachableHandles[j].mp.ID
	})

	// Key is the hash of the local key, and the local key of all reachable
	// packages.
	depHasher := sha256.New()
	depHasher.Write(n.ph.localKey[:])
	for _, rph := range reachableHandles {
		depHasher.Write(rph.localKey[:])
	}
	depHasher.Sum(n.ph.key[:0])

	return nil
}

// typerefData retrieves encoded typeref data from the filecache, or computes it on
// a cache miss.
func (s *Snapshot) typerefData(ctx context.Context, id PackageID, imports map[ImportPath]*metadata.Package, cgfs []file.Handle) ([]byte, error) {
	key := typerefsKey(id, imports, cgfs)
	if data, err := filecache.Get(typerefsKind, key); err == nil {
		return data, nil
	} else if err != filecache.ErrNotFound {
		bug.Reportf("internal error reading typerefs data: %v", err)
	}

	pgfs, err := s.view.parseCache.parseFiles(ctx, token.NewFileSet(), ParseFull&^parser.ParseComments, true, cgfs...)
	if err != nil {
		return nil, err
	}
	data := typerefs.Encode(pgfs, imports)

	// Store the resulting data in the cache.
	go func() {
		if err := filecache.Set(typerefsKind, key, data); err != nil {
			event.Error(ctx, fmt.Sprintf("storing typerefs data for %s", id), err)
		}
	}()

	return data, nil
}

// typerefsKey produces a key for the reference information produced by the
// typerefs package.
func typerefsKey(id PackageID, imports map[ImportPath]*metadata.Package, compiledGoFiles []file.Handle) file.Hash {
	hasher := sha256.New()

	fmt.Fprintf(hasher, "typerefs: %s\n", id)

	importPaths := make([]string, 0, len(imports))
	for impPath := range imports {
		importPaths = append(importPaths, string(impPath))
	}
	sort.Strings(importPaths)
	for _, importPath := range importPaths {
		imp := imports[ImportPath(importPath)]
		// TODO(rfindley): strength reduce the typerefs.Export API to guarantee
		// that it only depends on these attributes of dependencies.
		fmt.Fprintf(hasher, "import %s %s %s", importPath, imp.ID, imp.Name)
	}

	fmt.Fprintf(hasher, "compiledGoFiles: %d\n", len(compiledGoFiles))
	for _, fh := range compiledGoFiles {
		fmt.Fprintln(hasher, fh.Identity())
	}

	var hash [sha256.Size]byte
	hasher.Sum(hash[:0])
	return hash
}

// typeCheckInputs contains the inputs of a call to typeCheckImpl, which
// type-checks a package.
//
// Part of the purpose of this type is to keep type checking in-sync with the
// package handle key, by explicitly identifying the inputs to type checking.
type typeCheckInputs struct {
	id PackageID

	// Used for type checking:
	pkgPath                  PackagePath
	name                     PackageName
	goFiles, compiledGoFiles []file.Handle
	sizes                    types.Sizes
	depsByImpPath            map[ImportPath]PackageID
	goVersion                string // packages.Module.GoVersion, e.g. "1.18"

	// Used for type check diagnostics:
	// TODO(rfindley): consider storing less data in gobDiagnostics, and
	// interpreting each diagnostic in the context of a fixed set of options.
	// Then these fields need not be part of the type checking inputs.
	relatedInformation bool
	linkTarget         string
	moduleMode         bool
}

func (s *Snapshot) typeCheckInputs(ctx context.Context, mp *metadata.Package) (typeCheckInputs, error) {
	// Read both lists of files of this package.
	//
	// Parallelism is not necessary here as the files will have already been
	// pre-read at load time.
	//
	// goFiles aren't presented to the type checker--nor
	// are they included in the key, unsoundly--but their
	// syntax trees are available from (*pkg).File(URI).
	// TODO(adonovan): consider parsing them on demand?
	// The need should be rare.
	goFiles, err := readFiles(ctx, s, mp.GoFiles)
	if err != nil {
		return typeCheckInputs{}, err
	}
	compiledGoFiles, err := readFiles(ctx, s, mp.CompiledGoFiles)
	if err != nil {
		return typeCheckInputs{}, err
	}

	goVersion := ""
	if mp.Module != nil && mp.Module.GoVersion != "" {
		goVersion = mp.Module.GoVersion
	}

	return typeCheckInputs{
		id:              mp.ID,
		pkgPath:         mp.PkgPath,
		name:            mp.Name,
		goFiles:         goFiles,
		compiledGoFiles: compiledGoFiles,
		sizes:           mp.TypesSizes,
		depsByImpPath:   mp.DepsByImpPath,
		goVersion:       goVersion,

		relatedInformation: s.Options().RelatedInformationSupported,
		linkTarget:         s.Options().LinkTarget,
		moduleMode:         s.view.moduleMode(),
	}, nil
}

// readFiles reads the content of each file URL from the source
// (e.g. snapshot or cache).
func readFiles(ctx context.Context, fs file.Source, uris []protocol.DocumentURI) (_ []file.Handle, err error) {
	fhs := make([]file.Handle, len(uris))
	for i, uri := range uris {
		fhs[i], err = fs.ReadFile(ctx, uri)
		if err != nil {
			return nil, err
		}
	}
	return fhs, nil
}

// localPackageKey returns a key for local inputs into type-checking, excluding
// dependency information: files, metadata, and configuration.
func localPackageKey(inputs typeCheckInputs) file.Hash {
	hasher := sha256.New()

	// In principle, a key must be the hash of an
	// unambiguous encoding of all the relevant data.
	// If it's ambiguous, we risk collisions.

	// package identifiers
	fmt.Fprintf(hasher, "package: %s %s %s\n", inputs.id, inputs.name, inputs.pkgPath)

	// module Go version
	fmt.Fprintf(hasher, "go %s\n", inputs.goVersion)

	// import map
	importPaths := make([]string, 0, len(inputs.depsByImpPath))
	for impPath := range inputs.depsByImpPath {
		importPaths = append(importPaths, string(impPath))
	}
	sort.Strings(importPaths)
	for _, impPath := range importPaths {
		fmt.Fprintf(hasher, "import %s %s", impPath, string(inputs.depsByImpPath[ImportPath(impPath)]))
	}

	// file names and contents
	fmt.Fprintf(hasher, "compiledGoFiles: %d\n", len(inputs.compiledGoFiles))
	for _, fh := range inputs.compiledGoFiles {
		fmt.Fprintln(hasher, fh.Identity())
	}
	fmt.Fprintf(hasher, "goFiles: %d\n", len(inputs.goFiles))
	for _, fh := range inputs.goFiles {
		fmt.Fprintln(hasher, fh.Identity())
	}

	// types sizes
	wordSize := inputs.sizes.Sizeof(types.Typ[types.Int])
	maxAlign := inputs.sizes.Alignof(types.NewPointer(types.Typ[types.Int64]))
	fmt.Fprintf(hasher, "sizes: %d %d\n", wordSize, maxAlign)

	fmt.Fprintf(hasher, "relatedInformation: %t\n", inputs.relatedInformation)
	fmt.Fprintf(hasher, "linkTarget: %s\n", inputs.linkTarget)
	fmt.Fprintf(hasher, "moduleMode: %t\n", inputs.moduleMode)

	var hash [sha256.Size]byte
	hasher.Sum(hash[:0])
	return hash
}

// e.g. "go1" or "go1.2" or "go1.2.3"
var goVersionRx = regexp.MustCompile(`^go[1-9][0-9]*(?:\.(0|[1-9][0-9]*)){0,2}$`)

// An importFunc is an implementation of the single-method
// types.Importer interface based on a function value.
type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
