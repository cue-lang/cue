// Copyright 2024 CUE Authors
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

// This file contains logic regarding structure sharing.

// Notes
//
// TODO:
// We may want to consider tracking closedness in parallel to the Vertex
// structure, for instance in a CloseInfo or in a cue.Value itself.
//
//     reg: {}
//     #def: sub: reg
//
// By tracking closedness inside the CloseInfo, we can still share the
// structure and only have to change
//
// Maybe this is okay, though, as #Def itself can be shared, at least.

func (n *nodeContext) unshare() {
	n.noSharing = true

	if !n.isShared {
		return
	}
	n.isShared = false
	n.node.IsShared = false

	v := n.node.BaseValue.(*Vertex)

	// TODO: the use of cycle for BaseValue is getting increasingly outdated.
	// Find another mechanism once we get rid of the old evaluator.
	n.node.BaseValue = n.origBaseValue

	n.scheduleVertexConjuncts(n.shared, v, n.sharedID)
}

func (n *nodeContext) share(c Conjunct, arc *Vertex, id CloseInfo) {
	if n.isShared {
		panic("already sharing")
	}
	n.origBaseValue = n.node.BaseValue
	n.node.BaseValue = arc
	n.node.IsShared = true
	n.isShared = true
	n.shared = c
	n.sharedID = id
}

func (n *nodeContext) shareIfPossible(c Conjunct, arc *Vertex, id CloseInfo) bool {
	// TODO: have an experiment here to enable or disable structure sharing.
	// return false
	if !n.ctx.Sharing {
		return false
	}

	if n.noSharing || n.isShared || n.ctx.errs != nil {
		return false
	}

	// This line is to deal with this case:
	//
	//     reg: {}
	//     #def: sub: reg
	//
	// Ideally we find a different solution, like passing closedness
	// down elsewhere. In fact, as we do this in closeContexts, it probably
	// already works, it will just not be reflected in the debug output.
	// We could fix that by not printing structure shared nodes, which is
	// probably a good idea anyway.
	//
	// TODO: come up with a mechanism to allow this case.
	if n.node.Closed && !arc.Closed {
		return false
	}

	// Sharing let expressions is not supported and will result in unmarked
	// structural cycles. Processing will still terminate, but printing the
	// result will result in an infinite loop.
	//
	// TODO: allow this case.
	if n.node.Label.IsLet() {
		return false
	}

	// If an arc is a computed intermediate result and not part of a CUE output,
	// it should not be shared.
	if n.node.nonRooted || arc.nonRooted {
		return false
	}

	n.share(c, arc, id)
	return true
}

// Vertex values that are held in BaseValue will be wrapped in the following
// order:
//
//    disjuncts -> (shared | computed | data)
//
// DerefDisjunct
//   - get the current value under computation
//
// DerefValue
//   - get the value the node ultimately represents.
//

// DerefValue unrolls indirections of Vertex values. These may be introduced,
// for instance, by temporary bindings such as comprehension values.
// It returns v itself if v does not point to another Vertex.
func (v *Vertex) DerefValue() *Vertex {
	for {
		arc, ok := v.BaseValue.(*Vertex)
		if !ok {
			return v
		}
		v = arc
	}
}

// DerefDisjunct indirects a node that points to a disjunction.
func (v *Vertex) DerefDisjunct() *Vertex {
	for {
		arc, ok := v.BaseValue.(*Vertex)
		if !ok || !arc.IsDisjunct {
			return v
		}
		v = arc
	}
}

// DerefNonDisjunct indirects a node that points to a disjunction.
func (v *Vertex) DerefNonDisjunct() *Vertex {
	for {
		arc, ok := v.BaseValue.(*Vertex)
		if !ok || arc.IsDisjunct {
			return v
		}
		v = arc
	}
}

// DerefNonRooted indirects a node that points to a value that is not rooted.
// This includes structure-shared nodes that point to a let field: let fields
// may or may not be part of a struct, and thus should be treated as non-rooted.
func (v *Vertex) DerefNonRooted() *Vertex {
	for {
		arc, ok := v.BaseValue.(*Vertex)
		if !ok || arc.IsDisjunct || (v.IsShared && !arc.Label.IsLet()) {
			return v
		}
		v = arc
	}
}

// DerefNonShared finds the indirection of an arc that is not the result of
// structure sharing. This is especially relevant when indirecting disjunction
// values.
func (v *Vertex) DerefNonShared() *Vertex {
	if v.state != nil && v.state.isShared {
		return v
	}
	for {
		arc, ok := v.BaseValue.(*Vertex)
		if !ok {
			return v
		}
		v = arc
	}
}
