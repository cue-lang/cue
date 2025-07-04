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

// TODO: Reuse
// - tasks: perhaps make them "inline", so as to avoid the need for a free list.
// - Environment: not sure how. Perhaps using a mark and sweep approach.

func (v *Vertex) free() {
	n := v.state
	if n == nil {
		return
	}

	if n.isDisjunct {
		return
	}

	for _, arc := range v.Arcs {
		// Recursive results in minimal more recovery. Ignore this for now.
		n := arc.state
		if n == nil || n.isDisjunct || n.refCount > 0 {
			continue
		}
		n.ctx.freeNodeContext(n)
	}

	// Don't free the state if there are still dependants.
	if n.refCount > 0 {
		return
	}

	n.ctx.freeNodeContext(n)

	if v.PatternConstraints == nil {
		return
	}

	for _, p := range v.PatternConstraints.Pairs {
		n := p.Constraint.state
		if n == nil {
			continue
		}
		n.ctx.freeNodeContext(n)
	}
}

// freeDisjunct frees a node that has been used as a disjunct. We cannot free a
// disjunct while unwinding the evaluation, because the closedness information
// of the result needs to be merged into disjunct other disjuncts when
// deduping.
// The additional advantage of handing disjuncts separately, though, is that
// we can reclaim Vertex nodes, of which many are overallocated.
func (n *nodeContext) freeDisjunct() {
	n.ctx.freeVertex(n.node)

	// v := n.node

	// if w := v.DerefDisjunct(); v != w && w.state != nil {
	// 	w.state.freeDisjunct()
	// }

	// for _, arc := range v.Arcs {
	// 	if arc.state != nil {
	// 		arc.state.freeDisjunct()
	// 	}
	// }
	// n.ctx.freeNodeContext(n)
}

// func (c *OpContext) newVertex() (v *Vertex) {
// 	if v = c.freeListVertex; v != nil {
// 		// c.stats.Reused++
// 		c.freeListVertex = v.Parent
// 		v.Parent = nil
// 	} else {
// 		// c.stats.Allocs++
// 		v = &Vertex{}
// 	}
// 	return v
// }

func (c *OpContext) freeVertex(v *Vertex) {
	if w := v.DerefDisjunct(); v != w {
		c.freeVertex(w)
	}

	for _, arc := range v.Arcs {
		c.freeVertex(arc)
	}

	if n := v.state; n != nil {
		c.freeNodeContext(n)
	}

	if v.PatternConstraints == nil {
		return
	}

	for _, p := range v.PatternConstraints.Pairs {
		n := p.Constraint
		c.freeVertex(n)
	}

	// *v = Vertex{
	// 	Structs:   v.Structs[:0],
	// 	Arcs:      v.Arcs[:0],
	// 	Conjuncts: v.Conjuncts[:0],
	// }

	// v.Parent = c.freeListVertex
	// c.freeListVertex = v
	// c.stats.Retained++
}
