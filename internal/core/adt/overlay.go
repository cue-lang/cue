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
	"maps"
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

		// TODO(perf): take a map from a pool of maps and reuse.
		vertexMap: make(map[*Vertex]*Vertex),
	}
}

// An overlayContext keeps track of copied vertices, closeContexts, and tasks.
// This allows different passes to know which of each were created, without
// having to walk the entire tree.
type overlayContext struct {
	ctx *OpContext

	// vertices holds the original, non-overlay vertices. The overlay for a
	// vertex v can be obtained by looking up v.cc.overlay.src.
	vertices []*Vertex

	// vertexMap maps Vertex values of an originating node to the ones copied
	// for this overlayContext. This is used to update the Vertex values in
	// Environment values.
	vertexMap vertexMap

	// confMap maps envComprehension values to the ones copied for this
	// overlayContext.
	compMap map[*envComprehension]*envComprehension
}

type vertexMap map[*Vertex]*Vertex

// overlayFrom is used to store overlay information in the OpContext. This
// is used for dynamic resolution of vertices, which prevents data structures
// from having to be copied in the overlay.
//
// TODO(perf): right now this is only used for resolving vertices in
// comprehensions. We could also use this for resolving environments, though.
// Furthermore, we could used the "cleared" vertexMaps on this stack to avoid
// allocating memory.
//
// NOTE: using a stack globally in OpContext is not very principled, as we
// may be evaluating nested evaluations of different disjunctions. However,
// in practice this just results in more work: as the vertices should not
// overlap, there will be no cycles.
type overlayFrame struct {
	vertexMap vertexMap
	root      *Vertex
}

func (c *OpContext) pushOverlay(v *Vertex, m vertexMap) {
	c.overlays = append(c.overlays, overlayFrame{m, v})
}

func (c *OpContext) popOverlay() {
	c.overlays = c.overlays[:len(c.overlays)-1]
}

func (c *OpContext) deref(v *Vertex) *Vertex {
	for i := len(c.overlays) - 1; i >= 0; i-- {
		f := c.overlays[i]
		if f.root == v {
			continue
		}
		if x, ok := f.vertexMap[v]; ok {
			return x
		}
	}
	return v
}

// deref reports a replacement of v or v itself if such a replacement does not
// exists. It computes the transitive closure of the replacement graph.
// TODO(perf): it is probably sufficient to only replace one level. But we need
// to prove this to be sure. Until then, we keep the code as is.
//
// This function does a simple cycle check. As every overlayContext adds only
// new Vertex nodes and only entries from old to new nodes are created, this
// should never happen. But just in case we will panic instead of hang in such
// situations.
func (m vertexMap) deref(v *Vertex) *Vertex {
	for i := 0; ; i++ {
		x, ok := m[v]
		if !ok {
			break
		}
		v = x

		if i > len(m) {
			panic("cycle detected in vertexMap")
		}
	}
	return v
}

// cloneRoot clones the Vertex in which disjunctions are defined to allow
// inserting selected disjuncts into a new Vertex.
func (ctx *overlayContext) cloneRoot(root *nodeContext) *nodeContext {
	maps.Copy(ctx.vertexMap, root.vertexMap)

	// Clone all vertices that need to be cloned to support the overlay.
	v := ctx.cloneVertex(root.node)
	v.IsDisjunct = true
	v.state.vertexMap = ctx.vertexMap

	for _, v := range ctx.vertices {
		v = v.overlay

		n := v.state
		if n == nil {
			continue
		}

		// The group of the root closeContext should point to the Conjuncts field
		// of the Vertex. As we already allocated the group, we use that allocation,
		// but "move" it to v.Conjuncts.
		// TODO: Is this ever necessary? It is certainly necessary to rewrite
		// environments from inserted disjunction values, but expressions that
		// were already added will typically need to be recomputed and recreated
		// anyway. We add this in to be a bit defensive and reinvestigate once we
		// have more aggressive structure sharing implemented
		for i, c := range v.Conjuncts {
			v.Conjuncts[i].Env = ctx.derefDisjunctsEnv(c.Env)
		}

		for _, t := range n.tasks {
			ctx.rewriteComprehension(t)

			t.node = ctx.vertexMap.deref(t.node.node).state

			if t.blockedOn != nil {
				before := t.blockedOn.node.node
				after := ctx.vertexMap.deref(before)
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
	ctx.vertexMap[x] = v
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

// derefDisjunctsEnv creates a new env for each Environment in the Up chain with
// each Environment where Vertex is "from" to one where Vertex is "to".
//
// TODO(perf): we could, instead, just look up the mapped vertex in
// OpContext.Up. This would avoid us having to copy the Environments for each
// disjunct. This requires quite a bit of plumbing, though, so we leave it as
// is until this proves to be a performance issue.
func (ctx *overlayContext) derefDisjunctsEnv(env *Environment) *Environment {
	if env == nil {
		return nil
	}
	up := ctx.derefDisjunctsEnv(env.Up)
	to := ctx.vertexMap.deref(env.Vertex)
	if up != env.Up || env.Vertex != to {
		env = &Environment{
			Up:           up,
			Vertex:       to,
			DynamicLabel: env.DynamicLabel,
		}
	}
	return env
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
		for _, x := range n.disjunctions {
			x.env = ctx.derefDisjunctsEnv(x.env)
			d.disjunctions = append(d.disjunctions, x)
		}
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

	env := ctx.derefDisjunctsEnv(t.env)

	// TODO(perf): alloc from buffer.
	d := &task{
		run:            t.run,
		state:          t.state,
		completes:      t.completes,
		unblocked:      t.unblocked,
		blockCondition: t.blockCondition,
		blockedOn:      t.blockedOn, // will be rewritten later
		err:            t.err,
		env:            env,
		x:              t.x,
		id:             id,

		node: dst.node,

		// These are rewritten after everything is cloned when all vertices are
		// known.
		comp: t.comp,
		leaf: t.leaf,
	}

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
		x := &envComprehension{
			comp:    ec.comp,
			structs: ec.structs,
			vertex:  ctx.ctx.deref(ec.vertex),
		}
		ctx.compMap[ec] = x
		ec = x
	}

	return ec
}
