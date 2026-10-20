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

// sharedConjunct retains the original reference for replay if sharing stops.
// Different references may share one target when one includes the other.
type sharedConjunct struct {
	c   Conjunct
	arc *Vertex
	id  CloseInfo
}

func (n *nodeContext) unshare() {
	n.noSharing = true

	if !n.isShared {
		return
	}
	n.isShared = false
	n.node.IsShared = false
	n.node.OpenedShared = false

	// TODO: the use of cycle for BaseValue is getting increasingly outdated.
	// Find another mechanism once we get rid of the old evaluator.
	n.node.BaseValue = n.origBaseValue

	for _, x := range n.sharedConjuncts {
		n.scheduleVertexConjuncts(x.c, x.arc, x.id)
	}

	n.decSharedIDs()
}

// finalizeSharing should be called when it is known for sure a node can be
// shared.
func (n *nodeContext) finalizeSharing() {
	n.decSharedIDs()
	if !n.isShared {
		return
	}
	switch v := n.node.BaseValue.(type) {
	case *Vertex:
		if n.shareCycleType == NoCycle {
			v.Finalize(n.ctx)
			// See the TODO in unify.go for toFinalize.
			// } else if !v.isFinal() {
			// 	// TODO: ideally we just handle cycles in optional chains directly,
			// 	// rather than relying on this mechanism. This requires us to add
			// 	// a mechanism to detect that.
			// 	n.ctx.toFinalize = append(n.ctx.toFinalize, v)
		}
		// If state.parent is non-nil, we determined earlier that this Vertex
		// is not rooted and that it can safely be shared. Because it is
		// not-rooted, though, it will not have a path location, resulting in
		// bad error messages, and in some cases dropped errors. To avoid this,
		// we reset the parent and label of the Vertex so that its path reflects
		// its assigned location.
		if v.state != nil && v.state.parent != nil {
			v.Parent = v.state.parent
		}
	case *Bottom:
		// An error trumps sharing. We can leave it as is.
	default:
		panic("unreachable")
	}
}

func (n *nodeContext) addShared(c Conjunct, arc *Vertex, id CloseInfo) {
	if len(n.sharedConjuncts) == 0 || n.shareCycleType < id.CycleType {
		n.shareCycleType = id.CycleType
	}

	// At this point, the node may still be unshared at a later point. For this
	// purpose we need to keep the retain count above zero until all conjuncts
	// have been processed and it is clear that sharing is possible. Delaying
	// such a count should not hurt performance, as a shared node is completed
	// anyway.
	n.sharedConjuncts = append(n.sharedConjuncts, sharedConjunct{c, arc, id})
}

func (n *nodeContext) decSharedIDs() {
	if n.shareDecremented {
		return
	}
	n.shareDecremented = true
	for _, x := range n.sharedConjuncts {
		n.updateConjunctInfo(n.node.Kind(), x.id, 0)
	}
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
	n.addShared(c, arc, id)
	n.node.OpenedShared = id.Opened || id.ConjunctOpened
	if arc.cycleSensitive || arc.DerefValue().cycleSensitive {
		n.node.markCycleSensitive()
	}

	if arc.IsDetached() && arc.MayAttach() { // TODO: Second check necessary?
		// This node can safely be shared. Since it is not rooted, though, it
		// does not have a path location. Instead of setting the parent path
		// directly, though, we record the prospective parent in the state: as
		// the evaluator uses the Parent field during evaluation, setting the
		// field directly here can result in incorrect evaluation results.
		// Setting the parent in the state instead allows us to defer setting
		// Parent until it is safe to do so..
		if s := arc.getState(n.ctx); s != nil {
			s.parent = n.node
		}
	}
}

func (n *nodeContext) shareIfPossible(c Conjunct, arc *Vertex, id CloseInfo) bool {
	if !n.ctx.Sharing {
		return false
	}

	// We disallow sharing for any Arcs, even pending ones, to be defensive.
	// CUE currently does not always unwind sharing properly in the precense of
	// pending arcs. See, for instance:
	//
	// 		a: X
	// 		if true {
	// 			a: b: c: e: 1 // ensure 'e' is added
	// 		}
	// 		X: b: Y
	// 		Y: c: d: int
	//
	// TODO: allow sharing in the precense of pending or not present arcs.
	if len(n.node.Arcs) > 0 {
		return false
	}

	// See Issue #3801: structure sharing seems to be broken for non-rooted
	// values. We disable sharing for now.
	// TODO: make sharing work again for non-rooted structs.
	if arc.nonRooted || arc.IsDynamic {
		return false
	}

	// We do not allowing sharing if the conjunct has a cycle. Sharing is only
	// possible if there is a single conjunct. We want to further evaluate this
	// conjunct to force recognition of a structural cycle.
	if id.CycleType == IsCyclic && (n.node.nonRooted || n.node.IsDynamic) {
		return false
	}

	if n.noSharing || n.ctx.errs != nil {
		return false
	}

	// A forward reference may not have processed its definition yet, and so
	// may not be marked recursively closed. Complete it before deciding
	// whether it can be shared with the same definition at this destination.
	if n.node.ClosedRecursive && !arc.ClosedRecursive {
		if !n.completeSharingTarget(arc, id) {
			return false
		}
	}

	// Sharing let expressions is not supported and will result in unmarked
	// structural cycles. Processing will still terminate, but printing the
	// result will result in an infinite loop.
	//
	// TODO: allow this case.
	if arc.Label.IsLet() {
		return false
	}

	if n.isShared {
		// A value already constrained by a definition can be unified with
		// that definition again without unfolding either reference. In
		// particular, [...#T] need not copy a value declared as #T.
		if id.CycleType == IsCyclic || n.shareCycleType == IsCyclic ||
			id.Opened || id.ConjunctOpened || n.node.OpenedShared {
			return false
		}
		shared := n.node.BaseValue.(*Vertex)
		switch {
		case n.includesDefinition(shared, arc):
		// Detached targets require share's parent attachment bookkeeping.
		case !arc.IsDetached() && n.includesDefinition(arc, shared):
			n.node.BaseValue = arc
			n.shared = c
		default:
			return false
		}
		n.addShared(c, arc, id)
		return true
	}

	n.share(c, arc, id)
	return true
}

// completeSharingTarget completes an idle forward reference whose declaration
// and destination name the same definition. This only changes scheduling; the
// completed target still has to pass the normal sharing and containment checks.
func (n *nodeContext) completeSharingTarget(v *Vertex, id CloseInfo) bool {
	if len(n.ctx.overlays) > 0 || v.Parent == nil || v.Label.IsDef() || v.Label.IsLet() || id.IsCyclic() ||
		id.Opened || id.ConjunctOpened || !n.hasSameDefinitionConjunct(v) {
		return false
	}
	s := v.state
	if s == nil || s.ctx != n.ctx || s.opID != n.ctx.opID || s.refCount > 0 || s.isCompleting > 0 ||
		n.hasAncestor(v) || n.ctx.derefRoot(v) != v || !v.Parent.allChildConjunctsKnown(n.ctx) {
		return false
	}

	for _, t := range s.tasks {
		if t.state == taskRUNNING {
			return false
		}
	}

	owner := n.node.BaseValue
	v.Finalize(n.ctx)
	// Completing a dependency can run tasks on this node as well.
	return n.node.state == n && !n.noSharing && n.node.BaseValue == owner &&
		len(n.node.Arcs) == 0 && n.ctx.errs == nil && v.ClosedRecursive
}

// hasSameDefinitionConjunct finds a common direct definition reference without
// evaluating either vertex. It is a scheduling hint, not a containment proof.
func (n *nodeContext) hasSameDefinitionConjunct(v *Vertex) bool {
	for a := range n.node.LeafConjuncts() {
		if a.Env == nil || a.CloseInfo.Opened || a.CloseInfo.ConjunctOpened {
			continue
		}
		def := n.ctx.refTarget(a.Env, a.Expr())
		if def == nil || !def.Label.IsDef() {
			continue
		}
		for b := range v.LeafConjuncts() {
			if b.Env == nil || b.CloseInfo.Opened || b.CloseInfo.ConjunctOpened {
				continue
			}
			if n.ctx.refTarget(b.Env, b.Expr()) == def {
				return true
			}
		}
	}
	return false
}

// includesDefinition reports whether v already includes a closed reference to
// def. Only direct, resolved definition references are evidence: opened values
// and references inside embeddings have different closedness semantics.
// Missing, unfinished, stale, or cycle-dependent state cannot establish that
// the same constraint can be reused in a different evaluation context.
func (n *nodeContext) includesDefinition(v, def *Vertex) bool {
	if !def.Label.IsDef() || n.ctx.derefRoot(v) != v || n.ctx.derefRoot(def) != def {
		return false
	}
	v = v.DerefDisjunct()
	s := v.state
	if s == nil || s.ctx != n.ctx || s.opID != n.ctx.opID || n.ctx.derefRoot(v) != v {
		return false
	}
	if !v.isFinal() || disjunctionOf(v) != nil || v.cycleSensitive || v.DerefValue().cycleSensitive {
		return false
	}
	// Replaying erroneous values preserves diagnostics at the referring path.
	if v.Bottom() != nil || v.ChildErrors != nil || v.DerefValue().ChildErrors != nil {
		return false
	}
	for _, r := range s.reqDefIDs {
		if r.v == def && r.kind == defReference && !r.ignore &&
			!r.conjunctOpened && r.parent == 0 && r.embed == 0 {
			return true
		}
	}
	return false
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
