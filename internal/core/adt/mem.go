// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt

// The goal of the memory management implemented here is to reuse as many of the
// allocated values as possible. It is currently mostly for nodeContext values,
// but we plan to collect other values as well.
//
// ### nodeContext
//
// One of the main complications with cleaning nodeContext values is that it
// holds closedness information, which is generally referred to by parent nodes
// and recursively by disjuncts (see [mergeCloseInfo]). This means we have to
// delay freeing the nodeContext until we are sure closedness information is no
// longer needed.
//
// Other than that, there are several other cases where nodeContext values
// cannot be freed when a nodeContext is involved in another computation that
// ends up recursively finalizing it. In this case, the nodeContext may only be
// freed when the last computation is finalized. This is tracked by the refCount
// field.
//
// Theoretically we should also not collect nodeContext values if they can still
// be notified about changes. But other mechanisms already preven this from
// being an issue.
//
// TODO(mem): Finally, we need to have a mechanism to collect temporary Vertex
// values that are not part of the output tree. This is done with the
// [nodeContext.toFree] field, which tracks Vertex values that can be cleaned up
// as soon as all processing for a nodeContext has been completed.

// TODO: Reuse
// - Vertex: Vertex values allocated for failed disjunctions could be reused.
// - tasks: perhaps make them "inline", so as to avoid the need for a free list.
// - Environment: not sure how. Perhaps using a mark and sweep approach.

func (n *nodeContext) retainProcess() *nodeContext {
	n.refCount++
	return n
}

func (n *nodeContext) releaseProcess() *nodeContext {
	n.refCount--
	// TODO: we could consider freeing the node here if it is marked as
	// freeable.
	return n
}

// A reclaimer is used to reclaim buffers of a Vertex and its children.
//
// It has several modes to be able to handle different cases.
type reclaimer struct {
	ctx *OpContext

	// guard is used to precent reclaiming disjuncts that are still "in flight",
	// or if nodeContext information, for instance, is still needed down the
	// line, even if a values is finalized. This is the case, for instance, if a
	// value is part of a disjunction that is still in flight or if another
	// operation is still modify a nodeContext.
	guard bool

	// recurse is set if reclaiming should be done for all child arcs
	// recursively.
	recurse bool

	// force allows a node to be freed if it is not finalized.
	//
	// TODO(mem): it is currently appears to be safe to free a nodeContext if it
	// is not finalized, even for nodes that are part of the output tree. We
	// should consider enforcing this though as it seems this could uncover some
	// bugs.
	force bool
}

// freeDisjunct frees a node that has been used as a disjunct. We cannot free a
// disjunct while unwinding the evaluation, because the closedness information
// of the result needs to be merged into other disjuncts when deduping.
// The additional advantage of handing disjuncts separately, though, is that
// we can reclaim Vertex nodes, of which many are overallocated.
func (n *nodeContext) freeDisjunct() {
	n.ctx.reclaimRecursive(n.node)
}

// reclaimRecursive reclaims all buffers of.v and its children. It forces
// buffers to be freed even if they are not finalized.
func (c *OpContext) reclaimRecursive(v *Vertex) {
	r := reclaimer{
		ctx:     c,
		force:   true,
		recurse: true,
	}
	r.reclaim(v)
}

// reclaimTempBuffers reclaims the nodeContext of itself and its children,
// if possible, along with other temporary buffers that are no longer needed.
func (c *OpContext) reclaimTempBuffers(v *Vertex) {
	r := reclaimer{
		ctx:   c,
		guard: true,
		force: true,
	}
	if !r.reclaim(v) {
		return
	}

	for _, arc := range v.Arcs {
		n := arc.state
		if n == nil || n.refCount > 0 {
			continue
		}

		if w := arc.DerefDisjunct(); arc != w {
			// Reclaim the fields that were already added before starting the
			// disjunction. The disjunct itself is reclaimed in [r.reclaim].
			c.reclaimRecursive(arc)
		}
		if n.node != nil {
			// TODO(mem): we could free recursively here, and this will release
			// some more nodes. But in this case it is rather rare, so we rather
			// prevent having to do a recursive traversal here.s
			c.freeNodeContext(n)
		}
	}
}

// reclaim is the core function that reclaims buffers for a Vertex and its
// children.
func (r reclaimer) reclaim(v *Vertex) bool {
	n := v.state
	if n != nil {
		for _, v := range n.toFree {
			r.ctx.reclaimRecursive(v)
		}
		n.toFree = n.toFree[:0]

		if !r.guard {
			r.reclaimBaseValueBuffers(v)
		} else if n.isDisjunct {
			// In guard mode we do not collect disjuncts. If this node is part
			// of a disjunct it is reclaimed later as part of [freeDisjunct].
			return false
		} else {
			r.reclaimBaseValueBuffers(v)

			if n.refCount > 0 || (v.Parent != nil && !v.Label.IsLet()) {
				goto skipRoot
			}
		}
		if n.ctx == r.ctx {
			// TODO(mem): it should be fine to just release the nodeContext into
			// c unconditionally. But the result is that it can result in
			// negative values for 'Leaks'. This is because loading imports
			// happens within a different context and currently this does not
			// correctly clean up.
			r.ctx.freeNodeContext(n)
		}
	}

	if w := v.DerefDisjunct(); v != w {
		r.ctx.reclaimRecursive(w)
	}

skipRoot:
	if v.PatternConstraints != nil {
		for _, p := range v.PatternConstraints.Pairs {
			if n := p.Constraint.state; n != nil {
				r.reclaim(p.Constraint)
			}
		}
	}

	if r.recurse {
		for _, arc := range v.Arcs {
			r.reclaim(arc)
		}
	}

	return true
}

func (r reclaimer) reclaimBaseValueBuffers(v *Vertex) {
	switch x := v.BaseValue.(type) {
	case *Disjunction:
		for _, d := range x.Values {
			if v, ok := d.(*Vertex); ok {
				r.ctx.reclaimRecursive(v)
			}
		}
	case *Conjunction:
		for _, d := range x.Values {
			if v, ok := d.(*Vertex); ok {
				r.ctx.reclaimRecursive(v)
			}
		}
	}
}

func (v *Vertex) clearArcs(c *OpContext) {
	for _, arc := range v.Arcs {
		c.reclaimRecursive(arc)
	}
	v.Arcs = v.Arcs[:0]
}
