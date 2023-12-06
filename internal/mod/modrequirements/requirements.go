package modrequirements

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"cuelang.org/go/internal/mod/internal/par"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/mvs"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/internal/slices"
)

// Requirements holds a set of module requirements. It does not
// initially load the full module graph, as that can be expensive.
// Instead the [Registry.Graph] method can be used to lazily construct
// that.
type Requirements struct {
	registry          Registry
	mainModuleVersion module.Version

	// rootModules is the set of root modules of the graph, sorted and capped to
	// length. It may contain duplicates, and may contain multiple versions for a
	// given module path. The root modules are the main module's direct requirements.
	rootModules    []module.Version
	maxRootVersion map[string]string

	graphOnce sync.Once // guards writes to (but not reads from) graph
	graph     atomic.Pointer[cachedGraph]
}

// Registry holds the contents of a registry. It's expected that this will
// cache any results that it returns.
type Registry interface {
	CUEModSummary(ctx context.Context, m module.Version) (*ModFileSummary, error)
}

type ModFileSummary struct {
	Module  module.Version
	Require []module.Version
}

// A cachedGraph is a non-nil *ModuleGraph, together with any error discovered
// while loading that graph.
type cachedGraph struct {
	mg  *ModuleGraph
	err error // If err is non-nil, mg may be incomplete (but must still be non-nil).
}

// NewRequirements returns a new requirement set with the given root modules.
// The dependencies of the roots will be loaded lazily from the given
// Registry value at the first call to the Graph method.
//
// The rootModules slice must be sorted according to [module.Sort].
// The caller must not modify the rootModules slice or direct map after passing
// them to newRequirements.
//
// /home/rogpeppe/go/src/cmd/go/internal/modload/buildlist.go:117
func NewRequirements(mainModulePath string, reg Registry, rootModules []module.Version) *Requirements {
	mainModuleVersion := module.MustNewVersion(mainModulePath, "")
	// TODO add direct, so we can tell which modules are directly used by the
	// main module.
	for i, v := range rootModules {
		if v.Path() == mainModulePath {
			panic(fmt.Sprintf("NewRequirements called with untrimmed build list: rootModules[%v] is a main module", i))
		}
		if !v.IsValid() {
			panic("NewRequirements with invalid zero version")
		}
	}
	rs := &Requirements{
		registry:          reg,
		mainModuleVersion: mainModuleVersion,
		rootModules:       rootModules,
		maxRootVersion:    make(map[string]string, len(rootModules)),
	}
	for i, m := range rootModules {
		if i > 0 {
			prev := rootModules[i-1]
			if prev.Path() > m.Path() || (prev.Path() == m.Path() && semver.Compare(prev.Version(), m.Version()) > 0) {
				panic(fmt.Sprintf("NewRequirements called with unsorted roots: %v", rootModules))
			}
		}
		if v, ok := rs.maxRootVersion[m.Path()]; !ok || semver.Compare(v, m.Version()) < 0 {
			rs.maxRootVersion[m.Path()] = m.Version()
		}
	}
	return rs
}

// RootSelected returns the version of the root dependency with the given module
// path, or the zero module.Version and ok=false if the module is not a root
// dependency.
func (rs *Requirements) RootSelected(path string) (version string, ok bool) {
	if path == rs.mainModuleVersion.Path() {
		return "", true
	}
	if v, ok := rs.maxRootVersion[path]; ok {
		return v, true
	}
	return "", false
}

// rootModules returns the set of root modules of the graph, sorted and capped to
// length. It may contain duplicates, and may contain multiple versions for a
// given module path.
func (rs *Requirements) RootModules() []module.Version {
	return slices.Clip(rs.rootModules)
}

// Graph returns the graph of module requirements loaded from the current
// root modules (as reported by RootModules).
//
// Graph always makes a best effort to load the requirement graph despite any
// errors, and always returns a non-nil *ModuleGraph.
//
// If the requirements of any relevant module fail to load, Graph also
// returns a non-nil error of type *mvs.BuildListError.
func (rs *Requirements) Graph(ctx context.Context) (*ModuleGraph, error) {
	rs.graphOnce.Do(func() {
		mg, mgErr := rs.readModGraph(ctx)
		rs.graph.Store(&cachedGraph{mg, mgErr})
	})
	cached := rs.graph.Load()
	return cached.mg, cached.err
}

// GraphIsLoaded reports whether Graph has been called previously.
func (rs *Requirements) GraphIsLoaded() bool {
	return rs.graph.Load() != nil
}

// A ModuleGraph represents the complete graph of module dependencies
// of a main module.
//
// If the main module supports module graph pruning, the graph does not include
// transitive dependencies of non-root (implicit) dependencies.
type ModuleGraph struct {
	g *mvs.Graph[module.Version]

	buildListOnce sync.Once
	buildList     []module.Version
}

// cueModSummary returns a summary of the cue.mod/module.cue file for module m,
// taking into account any replacements for m, exclusions of its dependencies,
// and/or vendoring.
//
// m must be a version in the module graph, reachable from the Target module.
// cueModSummary must not be called for the Target module
// itself, as its requirements may change.
//
// The caller must not modify the returned summary.
func (rs *Requirements) cueModSummary(ctx context.Context, m module.Version) (*ModFileSummary, error) {
	// TODO account for replacements, exclusions, etc.
	return rs.registry.CUEModSummary(ctx, m)
}

// readModGraph reads and returns the module dependency graph starting at the
// given roots.
//
// readModGraph does not attempt to diagnose or update inconsistent roots.
func (rs *Requirements) readModGraph(ctx context.Context) (*ModuleGraph, error) {
	var (
		mu       sync.Mutex // guards mg.g and hasError during loading
		hasError bool
		mg       = &ModuleGraph{
			g: mvs.NewGraph[module.Version](module.Versions{}, cmpVersion, []module.Version{rs.mainModuleVersion}),
		}
	)

	mg.g.Require(rs.mainModuleVersion, rs.rootModules)

	var (
		loadQueue = par.NewQueue(runtime.GOMAXPROCS(0))
		loading   sync.Map // module.Version → nil; the set of modules that have been or are being loaded
		loadCache par.ErrCache[module.Version, *ModFileSummary]
	)

	// loadOne synchronously loads the explicit requirements for module m.
	// It does not load the transitive requirements of m.
	loadOne := func(m module.Version) (*ModFileSummary, error) {
		return loadCache.Do(m, func() (*ModFileSummary, error) {
			summary, err := rs.cueModSummary(ctx, m)

			mu.Lock()
			if err == nil {
				mg.g.Require(m, summary.Require)
			} else {
				hasError = true
			}
			mu.Unlock()

			return summary, err
		})
	}

	for _, m := range rs.rootModules {
		m := m
		if !m.IsValid() {
			panic("root module version is invalid")
		}
		if m.Version() == "none" {
			continue
		}

		if _, dup := loading.LoadOrStore(m, nil); dup {
			// m has already been enqueued for loading. Since unpruned loading may
			// follow cycles in the requirement graph, we need to return early
			// to avoid making the load queue infinitely long.
			continue
		}

		loadQueue.Add(func() {
			loadOne(m)
			// If there's an error, findError will report it later.
		})
	}
	<-loadQueue.Idle()

	if hasError {
		return mg, mg.findError(&loadCache)
	}
	return mg, nil
}

// RequiredBy returns the dependencies required by module m in the graph,
// or ok=false if module m's dependencies are pruned out.
//
// The caller must not modify the returned slice, but may safely append to it
// and may rely on it not to be modified.
func (mg *ModuleGraph) RequiredBy(m module.Version) (reqs []module.Version, ok bool) {
	return mg.g.RequiredBy(m)
}

// Selected returns the selected version of the module with the given path.
//
// If no version is selected, Selected returns version "none".
func (mg *ModuleGraph) Selected(path string) (version string) {
	return mg.g.Selected(path)
}

// WalkBreadthFirst invokes f once, in breadth-first order, for each module
// version other than "none" that appears in the graph, regardless of whether
// that version is selected.
func (mg *ModuleGraph) WalkBreadthFirst(f func(m module.Version)) {
	mg.g.WalkBreadthFirst(f)
}

// BuildList returns the selected versions of all modules present in the graph,
// beginning with the main modules.
//
// The order of the remaining elements in the list is deterministic
// but arbitrary.
//
// The caller must not modify the returned list, but may safely append to it
// and may rely on it not to be modified.
func (mg *ModuleGraph) BuildList() []module.Version {
	mg.buildListOnce.Do(func() {
		mg.buildList = slices.Clip(mg.g.BuildList())
	})
	return mg.buildList
}

func (mg *ModuleGraph) findError(loadCache *par.ErrCache[module.Version, *ModFileSummary]) error {
	errStack := mg.g.FindPath(func(m module.Version) bool {
		_, err := loadCache.Get(m)
		return err != nil && err != par.ErrCacheEntryNotFound
	})
	if len(errStack) > 0 {
		// TODO it seems that this stack can never be more than one
		// element long, becasuse readModGraph never goes more
		// than one depth level below the root requirements.
		// Given that the top of the stack will always be the main
		// module and that BuildListError elides the first element
		// in this case, is it really worth using FindPath?
		_, err := loadCache.Get(errStack[len(errStack)-1])
		var noUpgrade func(from, to module.Version) bool
		return mvs.NewBuildListError[module.Version](err, errStack, module.Versions{}, noUpgrade)
	}

	return nil
}

// cmpVersion implements the comparison for versions in the module loader.
//
// It is consistent with semver.Compare except that as a special case,
// the version "" is considered higher than all other versions.
// The main module (also known as the target) has no version and must be chosen
// over other versions of the same module in the module dependency graph.
func cmpVersion(v1, v2 string) int {
	if v2 == "" {
		if v1 == "" {
			return 0
		}
		return -1
	}
	if v1 == "" {
		return 1
	}
	return semver.Compare(v1, v2)
}
