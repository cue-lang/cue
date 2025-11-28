// Copyright 2024 CUE Authors
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

package adt

import (
	"slices"
)

// This file implements a Vertex overlay. This is used by the disjunction
// algorithm to fork an existing Vertex value without modifying the original.
//
// At the moment, the forked value is a complete copy of the original.
// The copy points to the original to keep track of pointer equivalence.
// Conversely, while a copy is evaluated, the value of which it is a copy
// references the copy. Dereferencing will then take care that the copy is used
// during evaluation.
//
//   nodeContext (main)  <-
//   - deref               \
//     |                    \
//     |  nodeContext (d1)  | <-
//     \  - overlays -------/   \
//      \                        \
//       ->   nodeContext (d2)    |
//            - overlays --------/
//
// TODO: implement dereferencing
// TODO(perf): implement copy on write: instead of copying the entire tree, we
// could get by with only copying arcs to that are modified in the copy.

func newOverlayContext(ctx *OpContext) *overlayContext {
	return &overlayContext{
		ctx: ctx,
	}
}

// An overlayContext keeps track of copied vertices, closeContexts, and tasks.
// This allows different passes to know which of each were created, without
// having to walk the entire tree.
type overlayContext struct {
	ctx *OpContext

	// root is the root of the disjunct.
	root *Vertex

	// vertices holds the original, non-overlay vertices. The overlay for a
	// vertex v can be obtained by accessing v.overlay.
	vertices []*Vertex

	// confMap maps envComprehension values to the ones copied for this
	// overlayContext.
	compMap map[*envComprehension]*envComprehension
}

// overlayFrom is used to store overlay information in the OpContext. This
// is used for dynamic resolution of vertices, which prevents data structures
// from having to be copied in the overlay.
//
// TODO(perf): right now this is only used for resolving vertices in
// comprehensions. We could also use this for resolving environments, though.
//
// NOTE: using a stack globally in OpContext is not very principled, as we
// may be evaluating nested evaluations of different disjunctions. However,
// in practice this just results in more work: as the vertices should not
// overlap, there will be no cycles.
type overlayFrame struct {
	root *Vertex
}

type overlayEntry struct {
	overlay    *Vertex
	frameDepth int // depth at which this entry was added
}

func (c *OpContext) pushOverlay(v *Vertex, vertices []*Vertex) {
	depth := len(c.overlays)

	// Add new vertices to the shared map with current depth
	for _, orig := range vertices {
		if orig.overlay != nil {
			c.overlayVertexMap[orig] = overlayEntry{orig.overlay, depth}
		}
	}

	c.overlays = append(c.overlays, overlayFrame{v})
}

func (c *OpContext) popOverlay() {
	depth := len(c.overlays) - 1

	// Remove all entries added at this depth
	for key, entry := range c.overlayVertexMap {
		if entry.frameDepth == depth {
			delete(c.overlayVertexMap, key)
		}
	}

	c.overlays = c.overlays[:depth]
}

func (c *OpContext) deref(v *Vertex) *Vertex {
	// Check if v is the root of any active overlay frame
	for i := range c.overlays {
		if c.overlays[i].root == v {
			return v
		}
	}
	// Look up in the shared vertex map
	if entry, ok := c.overlayVertexMap[v]; ok {
		return entry.overlay
	}
	return v
}

// derefOverlay reports a replacement of v or v itself if such a replacement does not
// exist. It computes the transitive closure of the replacement graph by following
// the overlay chain.
// TODO(perf): it is probably sufficient to only replace one level. But we need
// to prove this to be sure. Until then, we keep the code as is.
//
// This function does a simple cycle check. As every overlayContext adds only
// new Vertex nodes and only entries from old to new nodes are created, this
// should never happen. But just in case we will panic instead of hang in such
// situations.
func derefOverlay(v *Vertex) *Vertex {
	if v == nil {
		return nil
	}
	const maxDepth = 1000 // Reasonable upper bound for overlay chain depth
	for i := 0; i < maxDepth; i++ {
		if v.overlay == nil {
			return v
		}
		v = v.overlay
	}
	panic("cycle detected in overlay chain")
}

// cloneRoot clones the Vertex in which disjunctions are defined to allow
// inserting selected disjuncts into a new Vertex.
func (ctx *overlayContext) cloneRoot(root *nodeContext) *nodeContext {
	// Clone all vertices that need to be cloned to support the overlay.
	v := ctx.cloneVertex(root.node)
	v.IsDisjunct = true
	ctx.root = v

	for _, v := range ctx.vertices {
		v = v.overlay

		n := v.state
		if n == nil {
			continue
		}

		for _, t := range n.tasks {
			ctx.rewriteComprehension(t)

			t.node = derefOverlay(t.node.node).state

			if t.blockedOn != nil {
				before := t.blockedOn.node.node
				after := derefOverlay(before)
				// Tasks that are blocked on nodes outside the current scope
				// of the disjunction should should be added to blocking queues.
				if before == after {
					continue
				}
				s := &after.state.scheduler
				t.blockedOn = s
				s.blocking = append(s.blocking, t)
				s.ctx.blocking = append(s.ctx.blocking, t)
				s.needs |= t.blockCondition
			}
		}
	}

	return v.state
}

// unlinkOverlay unlinks helper pointers. This should be done after the
// evaluation of a disjunct is complete. Keeping the linked pointers around
// will allow for dereferencing a vertex to its overlay, which, in turn,
// allows a disjunct to refer to parents vertices of the disjunct that
// recurse into the disjunct.
//
// TODO(perf): consider using generation counters.
func (ctx *overlayContext) unlinkOverlay() {
	for _, v := range ctx.vertices {
		v.overlay = nil
	}
}

// cloneVertex copies the contents of x into a new Vertex.
//
// It copies all Arcs, Conjuncts, and Structs, recursively.
//
// TODO(perf): it would probably be faster to copy vertices on demand. But this
// is more complicated and it would be worth measuring how much of a performance
// benefit this gives. More importantly, we should first implement the filter
// to eliminate disjunctions pre-copy based on discriminator fields and what
// have you. This is not unlikely to eliminate
func (ctx *overlayContext) cloneVertex(x *Vertex) *Vertex {
	if x.overlay != nil {
		return x.overlay
	}

	// TODO(mem-mgmt): use free list for Vertex allocation.
	v := &Vertex{}
	*v = *x
	x.overlay = v

	ctx.vertices = append(ctx.vertices, x)

	v.Conjuncts = slices.Clone(v.Conjuncts)

	if a := x.Arcs; len(a) > 0 {
		// TODO(perf): reuse buffer.
		v.Arcs = make([]*Vertex, len(a))
		for i, arc := range a {
			// TODO(perf): reuse when finalized.
			arc := ctx.cloneVertex(arc)
			v.Arcs[i] = arc
			arc.Parent = v
		}
	} else if cap(x.Arcs) > 0 {
		// If the original slice has any capacity, don't share it.
		v.Arcs = nil
	}

	v.Structs = slices.Clone(v.Structs)

	if pc := v.PatternConstraints; pc != nil {
		npc := &Constraints{Allowed: pc.Allowed}
		v.PatternConstraints = npc

		npc.Pairs = make([]PatternConstraint, len(pc.Pairs))
		for i, p := range pc.Pairs {
			npc.Pairs[i] = PatternConstraint{
				Pattern:    p.Pattern,
				Constraint: ctx.cloneVertex(p.Constraint),
			}
		}
	}

	if v.state != nil {
		v.state = ctx.cloneNodeContext(x.state)
		v.state.node = v

		ctx.cloneScheduler(v.state, x.state)
	}

	return v
}

func (ctx *overlayContext) cloneNodeContext(n *nodeContext) *nodeContext {
	n.node.getState(ctx.ctx) // ensure state is initialized.

	d := n.ctx.newNodeContext(n.node)
	d.underlying = n.underlying
	d.isDisjunct = true

	if n.underlying == nil {
		panic("unexpected nil underlying")
	}

	d.ctx = n.ctx
	d.node = n.node

	d.nodeContextState = n.nodeContextState

	d.arcMap = append(d.arcMap, n.arcMap...)
	d.checks = append(d.checks, n.checks...)
	d.sharedIDs = append(d.sharedIDs, n.sharedIDs...)

	d.reqDefIDs = append(d.reqDefIDs, n.reqDefIDs...)
	d.replaceIDs = append(d.replaceIDs, n.replaceIDs...)
	d.flatReplaceIDs = append(d.flatReplaceIDs, n.flatReplaceIDs...)
	d.minFlatReplaceIDTo = n.minFlatReplaceIDTo
	d.conjunctInfo = append(d.conjunctInfo, n.conjunctInfo...)

	// TODO: do we need to add cyclicConjuncts? Typically, cyclicConjuncts
	// gets cleared at the end of a unify call. There are cases, however, where
	// this is possible. We should decide whether cyclicConjuncts should be
	// forced to be processed in the parent node, or that we allow it to be
	// copied to the disjunction. By taking no action here, we assume it is
	// processed in the parent node. Investigate whether this always will lead
	// to correct results.
	// d.cyclicConjuncts = append(d.cyclicConjuncts, n.cyclicConjuncts...)

	// Do not clone cc in disjunctions, as it is identified by underlying.
	// We only need to clone the cc in disjunctCCs.
	d.disjunctions = append(d.disjunctions, n.disjunctions...)

	return d
}

func (ctx *overlayContext) cloneScheduler(dst, src *nodeContext) {
	ss := &src.scheduler
	ds := &dst.scheduler

	ds.state = ss.state
	ds.completed = ss.completed
	ds.needs = ss.needs
	ds.provided = ss.provided
	ds.counters = ss.counters

	ss.blocking = ss.blocking[:0]

	for _, t := range ss.tasks {
		switch t.state {
		case taskWAITING:
			t.defunct = true
			t := ctx.cloneTask(t, ds, ss)
			ds.tasks = append(ds.tasks, t)
			// We add this task to ds.blocking and ctx.ctx.blocking in
			// cloneRoot, after its node references have been rewritten.

		case taskREADY:
			t.defunct = true
			t := ctx.cloneTask(t, ds, ss)
			ds.tasks = append(ds.tasks, t)

		case taskRUNNING:
			if t.run == handleDisjunctions {
				continue
			}

			t.defunct = true
			t := ctx.cloneTask(t, ds, ss)
			t.state = taskREADY
			ds.tasks = append(ds.tasks, t)
		}
	}
}

func (ctx *overlayContext) cloneTask(t *task, dst, src *scheduler) *task {
	if t.node != src.node {
		panic("misaligned node")
	}

	id := t.id

	d := ctx.ctx.newTask()
	d.run = t.run
	d.state = t.state
	d.completes = t.completes
	d.unblocked = t.unblocked
	d.blockCondition = t.blockCondition
	d.blockedOn = t.blockedOn // will be rewritten later
	d.err = t.err
	d.env = t.env
	d.x = t.x
	d.id = id
	d.node = dst.node
	// These are rewritten after everything is cloned when all vertices are
	// known.
	d.comp = t.comp
	d.leaf = t.leaf

	return d
}

func (ctx *overlayContext) rewriteComprehension(t *task) {
	if t.comp != nil {
		t.comp = ctx.mapComprehensionContext(t.comp)
	}

	t.leaf = ctx.mapComprehension(t.leaf)
}

func (ctx *overlayContext) mapComprehension(c *Comprehension) *Comprehension {
	if c == nil {
		return nil
	}
	cc := *c
	cc.comp = ctx.mapComprehensionContext(cc.comp)
	cc.arc = ctx.ctx.deref(cc.arc)
	cc.parent = ctx.mapComprehension(cc.parent)
	return &cc
}

func (ctx *overlayContext) mapComprehensionContext(ec *envComprehension) *envComprehension {
	if ec == nil {
		return nil
	}

	if ctx.compMap == nil {
		ctx.compMap = make(map[*envComprehension]*envComprehension)
	}

	if ctx.compMap[ec] == nil {
		vertex := derefOverlay(ec.vertex)
		// Report the error at the root of the disjunction if otherwise the
		// error would be reported outside of the disjunction.
		if vertex == ec.vertex {
			vertex = ctx.root
		}
		x := &envComprehension{
			comp:    ec.comp,
			structs: ec.structs,
			vertex:  vertex,
		}
		ctx.compMap[ec] = x
		ec = x
	}

	return ec
}
