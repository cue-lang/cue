// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mvs

import (
	"cmp"
	"fmt"
	"slices"
)

// Versions is an interface that should be provided by implementations
// to define the mvs algorithm in terms of their own version type V, where
// a version type holds a (module path, module version) pair.
type Versions[V any] interface {
	// New creates a new instance of V holding the
	// given module path and version.
	New(path, version string) (V, error)
	// Path returns the path part of V.
	Path(v V) string
	// Version returns the version part of V.
	Version(v V) string
}

// Graph implements an incremental version of the MVS algorithm, with the
// requirements pushed by the caller instead of pulled by the MVS traversal.
type Graph[V comparable] struct {
	v     Versions[V]
	cmp   func(v1, v2 string) int
	roots []V

	required map[V][]V

	isRoot   map[V]bool        // contains true for roots and false for reachable non-roots
	selected map[string]string // path → version
}

// NewGraph returns an incremental MVS graph containing only a set of root
// dependencies and using the given max function for version strings.
//
// The caller must ensure that the root slice is not modified while the Graph
// may be in use.
func NewGraph[V comparable](v Versions[V], cmp func(string, string) int, roots []V) *Graph[V] {
	g := &Graph[V]{
		v:        v,
		cmp:      cmp,
		roots:    slices.Clip(roots),
		required: make(map[V][]V),
		isRoot:   make(map[V]bool),
		selected: make(map[string]string),
	}

	for _, m := range roots {
		g.isRoot[m] = true
		if g.cmp(g.Selected(g.v.Path(m)), g.v.Version(m)) < 0 {
			g.selected[g.v.Path(m)] = g.v.Version(m)
		}
	}

	return g
}

// Require adds the information that module m requires all modules in reqs.
// The reqs slice must not be modified after it is passed to Require.
//
// m must be reachable by some existing chain of requirements from g's target,
// and Require must not have been called for it already.
//
// If any of the modules in reqs has the same path as g's target,
// the target must have higher precedence than the version in req.
func (g *Graph[V]) Require(m V, reqs []V) {
	// To help catch disconnected-graph bugs, enforce that all required versions
	// are actually reachable from the roots (and therefore should affect the
	// selected versions of the modules they name).
	if _, reachable := g.isRoot[m]; !reachable {
		panic(fmt.Sprintf("%v is not reachable from any root", m))
	}

	// Truncate reqs to its capacity to avoid aliasing bugs if it is later
	// returned from RequiredBy and appended to.
	reqs = slices.Clip(reqs)

	if _, dup := g.required[m]; dup {
		panic(fmt.Sprintf("requirements of %v have already been set", m))
	}
	g.required[m] = reqs

	for _, dep := range reqs {
		// Mark dep reachable, regardless of whether it is selected.
		if _, ok := g.isRoot[dep]; !ok {
			g.isRoot[dep] = false
		}

		if g.cmp(g.Selected(g.v.Path(dep)), g.v.Version(dep)) < 0 {
			g.selected[g.v.Path(dep)] = g.v.Version(dep)
		}
	}
}

// RequiredBy returns the slice of requirements passed to Require for m, if any,
// with its capacity reduced to its length.
// If Require has not been called for m, RequiredBy(m) returns ok=false.
//
// The caller must not modify the returned slice, but may safely append to it
// and may rely on it not to be modified.
func (g *Graph[V]) RequiredBy(m V) (reqs []V, ok bool) {
	reqs, ok = g.required[m]
	return reqs, ok
}

// Selected returns the selected version of the given module path.
//
// If no version is selected, Selected returns version "none".
func (g *Graph[V]) Selected(path string) (version string) {
	v, ok := g.selected[path]
	if !ok {
		return "none"
	}
	return v
}

// BuildList returns the selected versions of all modules present in the Graph,
// beginning with the selected versions of each module path in the roots of g.
//
// The order of the remaining elements in the list is deterministic
// but arbitrary.
func (g *Graph[V]) BuildList() []V {
	seenRoot := make(map[string]bool, len(g.roots))

	var list []V
	for _, r := range g.roots {
		if seenRoot[g.v.Path(r)] {
			// Multiple copies of the same root, with the same or different versions,
			// are a bit of a degenerate case: we will take the transitive
			// requirements of both roots into account, but only the higher one can
			// possibly be selected. However — especially given that we need the
			// seenRoot map for later anyway — it is simpler to support this
			// degenerate case than to forbid it.
			continue
		}

		if v := g.Selected(g.v.Path(r)); v != "none" {
			list = append(list, g.newVersion(g.v.Path(r), v))
		}
		seenRoot[g.v.Path(r)] = true
	}
	uniqueRoots := list

	for path, version := range g.selected {
		if !seenRoot[path] {
			list = append(list, g.newVersion(path, version))
		}
	}
	g.sortVersions(list[len(uniqueRoots):])
	return list
}

func (g *Graph[V]) sortVersions(vs []V) {
	slices.SortFunc(vs, func(a, b V) int {
		if c := cmp.Compare(g.v.Path(a), g.v.Path(b)); c != 0 {
			return c
		}
		return g.cmp(g.v.Version(a), g.v.Version(b))
	})
}

func (g *Graph[V]) newVersion(path string, vers string) V {
	v, err := g.v.New(path, vers)
	if err != nil {
		// Note: can't happen because all paths and versions passed to
		// g.newVersion have already come from valid paths and versions
		// returned from a Versions implementation.
		panic(err)
	}
	return v
}

// WalkBreadthFirst invokes f once, in breadth-first order, for each module
// version other than "none" that appears in the graph, regardless of whether
// that version is selected.
func (g *Graph[V]) WalkBreadthFirst(f func(m V)) {
	var queue []V
	enqueued := make(map[V]bool)
	for _, m := range g.roots {
		if g.v.Version(m) != "none" {
			queue = append(queue, m)
			enqueued[m] = true
		}
	}

	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]

		f(m)

		reqs, _ := g.RequiredBy(m)
		for _, r := range reqs {
			if !enqueued[r] && g.v.Version(r) != "none" {
				queue = append(queue, r)
				enqueued[r] = true
			}
		}
	}
}

// FindPath reports a shortest requirement path starting at one of the roots of
// the graph and ending at a module version m for which f(m) returns true, or
// nil if no such path exists.
func (g *Graph[V]) FindPath(f func(V) bool) []V {
	// firstRequires[a] = b means that in a breadth-first traversal of the
	// requirement graph, the module version a was first required by b.
	firstRequires := make(map[V]V)

	queue := g.roots
	for _, m := range g.roots {
		firstRequires[m] = *new(V)
	}

	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]

		if f(m) {
			// Construct the path reversed (because we're starting from the far
			// endpoint), then reverse it.
			path := []V{m}
			for {
				m = firstRequires[m]
				if g.v.Path(m) == "" {
					break
				}
				path = append(path, m)
			}

			i, j := 0, len(path)-1
			for i < j {
				path[i], path[j] = path[j], path[i]
				i++
				j--
			}

			return path
		}

		reqs, _ := g.RequiredBy(m)
		for _, r := range reqs {
			if _, seen := firstRequires[r]; !seen {
				queue = append(queue, r)
				firstRequires[r] = m
			}
		}
	}

	return nil
}
