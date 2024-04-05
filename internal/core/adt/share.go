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

func (n *nodeContext) canShare() bool {
	return !n.noSharing && !n.isShared && n.ctx.errs == nil
}

func (n *nodeContext) unshare() {
	n.noSharing = true

	if !n.isShared {
		return
	}
	n.isShared = false

	v := n.node.BaseValue.(*Vertex)
	n.node.BaseValue = cycle

	n.scheduleVertexConjuncts(n.shared, v, n.sharedID)
}

func (n *nodeContext) share(c Conjunct, arc *Vertex, id CloseInfo) {
	if n.isShared {
		panic("already sharing")
	}
	n.node.BaseValue = arc
	n.isShared = true
	n.shared = c
	n.sharedID = id
}

func (n *nodeContext) shareIfPossible(c Conjunct, arc *Vertex, id CloseInfo) bool {
	// TODO: have an experiment here to enable or disable structure sharing.
	// return false

	if !n.canShare() {
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
	if n.node.Closed && !arc.Closed {
		return false
	}

	// Sharing let expressions is not supported and will result in unmarked
	// structural cycles. Processing will still terminate, but printing the
	// result will result in an infinite loop.
	if n.node.Label.IsLet() {
		return false
	}

	n.share(c, arc, id)
	return true
}
