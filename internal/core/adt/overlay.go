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

import "slices"

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
	return &overlayContext{ctx: ctx}
}

// An overlayContext keeps track of copied vertices, closeContexts, and tasks.
// This allows different passes to know which of each were created, without
// having to walk the entire tree.
type overlayContext struct {
	ctx *OpContext

	// vertices holds the original, non-overlay vertices. The overlay for a
	// vertex v can be obtained by looking up v.cc.overlay.src.
	vertices []*Vertex
}

// cloneRoot clones the Vertex in which disjunctions are defined to allow
// inserting selected disjuncts into a new Vertex.
func (ctx *overlayContext) cloneRoot(root *nodeContext) *nodeContext {
	// Clone all vertices that need to be cloned to support the overlay.
	v := ctx.cloneVertex(root.node)
	v.IsDisjunct = true

	for _, v := range ctx.vertices {
		n := v.state
		if n == nil || n.closeParent == nil {
			continue
		}

		if p := n.closeParent.node; p.overlay != nil {
			// Use the new nodeContext if the node was cloned. Otherwise it
			// is fine to use the old one.
			n.closeParent = p.state
			if p.state == nil {
				panic("unexpected nil nodeContext")
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

	v := &Vertex{}
	*v = *x

	x.overlay = v

	ctx.vertices = append(ctx.vertices, x)

	// The group of the root closeContext should point to the Conjuncts field
	// of the Vertex. As we already allocated the group, we use that allocation,
	// but "move" it to v.Conjuncts.
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
	if n.underlying == nil {
		panic("unexpected nil underlying")
	}

	d.refCount++

	d.ctx = n.ctx
	d.node = n.node

	d.nodeContextState = n.nodeContextState

	d.arcMap = append(d.arcMap, n.arcMap...)
	d.checks = append(d.checks, n.checks...)
	d.sharedIDs = append(d.sharedIDs, n.sharedIDs...)

	d.reqDefIDs = append(d.reqDefIDs, n.reqDefIDs...)
	d.replaceIDs = append(d.replaceIDs, n.replaceIDs...)
	d.conjunctInfo = append(d.conjunctInfo, n.conjunctInfo...)

	// TODO: do we need to add cyclicConjuncts? Typically, cyclicConjuncts
	// gets cleared at the end of a unify call. There are cases, however, where
	// this is possible. We should decide whether cyclicConjuncts should be
	// forced to be processed in the parent node, or that we allow it to be
	// copied to the disjunction. By taking no action here, we assume it is
	// processed in the parent node. Investigate whether this always will lead
	// to correct results.
	// d.cyclicConjuncts = append(d.cyclicConjuncts, n.cyclicConjuncts...)

	if len(n.disjunctions) > 0 {
		// Do not clone cc in disjunctions, as it is identified by underlying.
		// We only need to clone the cc in disjunctCCs.
		d.disjunctions = append(d.disjunctions, n.disjunctions...)
	}

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
			// Do not unblock previously blocked tasks, unless they are
			// associated with this node.
			// TODO: an edge case is when a task is blocked on another node
			// within the same disjunction. We could solve this by associating
			// each nodeContext with a unique ID (like a generation counter) for
			// the disjunction.
			if t.node != src || t.blockedOn != ss {
				break
			}
			t.defunct = true
			t := ctx.cloneTask(t, ds, ss)
			ds.tasks = append(ds.tasks, t)
			ds.blocking = append(ds.blocking, t)
			ctx.ctx.blocking = append(ctx.ctx.blocking, t)

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

	// TODO(perf): alloc from buffer.
	d := &task{
		run:            t.run,
		state:          t.state,
		completes:      t.completes,
		unblocked:      t.unblocked,
		blockCondition: t.blockCondition,
		err:            t.err,
		env:            t.env,
		x:              t.x,
		id:             id,

		node: dst.node,

		// TODO: need to copy closeContexts?
		comp: t.comp,
		leaf: t.leaf,
	}

	if t.blockedOn != nil {
		if t.blockedOn != src {
			panic("invalid scheduler")
		}
		d.blockedOn = dst
	}

	return d
}
