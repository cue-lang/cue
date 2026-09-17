// Copyright 2023 CUE Authors
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
	"fmt"
	"slices"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// This file contains functionality for processing conjuncts to insert the
// corresponding values in the Vertex.
//
// Conjuncts are divided into two classes:
// - literal values that need no evaluation: these are inserted directly into
//   the Vertex.
// - field or value expressions that need to be evaluated: these are inserted
//   as a task into the Vertex' associated scheduler for later evaluation.
//   The implementation of these tasks can be found in tasks.go.
//
// The main entrypoint is scheduleConjunct.

// scheduleConjunct splits c into parts to be incrementally processed and queues
// these parts up for processing. it will itself not cause recursive processing.
func (n *nodeContext) scheduleConjunct(c Conjunct, id CloseInfo) {
	n.assertInitialized()

	if c.CloseInfo.FromDef {
		n.node.ClosedRecursive = true
	}

	// TODO: consider setting this as a safety measure.
	// if c.CloseInfo.CycleType > id.CycleType {
	// 	id.CycleType = c.CloseInfo.CycleType
	// }
	// if c.CloseInfo.IsCyclic {
	// 	id.IsCyclic = true
	// }
	// default:
	// Note this subtlety: we MUST take the cycle info from c when this is
	// an in place evaluated node, otherwise we must take that of id.

	// TODO(evalv3): Why do we no longer need to do this?
	// id.CycleInfo = c.CloseInfo.CycleInfo

	env := c.Env

	// A function-call argument pins the caller's cycle-reference chain: when
	// the stored conjunct is re-scheduled from within the callee's body (an
	// argument is evaluated lazily, on first use of the parameter), the
	// body's ambient chain must not leak into the argument's evaluation. See
	// the [CycleInfo.IsFuncArg] documentation. The ambient CycleType is
	// deliberately kept: whether the consuming context is cyclic governs
	// evidence bookkeeping (markNonCyclic) and thus the bounding of genuine
	// recursion.
	if c.CloseInfo.IsFuncArg {
		id.Refs = c.CloseInfo.Refs
		id.IsFuncArg = true
	}

	n.markNonCyclic(id)

	switch x := c.Elem().(type) {
	case *ConjunctGroup:
		for _, c := range *x {
			n.scheduleConjunct(c, id)
		}

	case *Vertex:
		// TODO: move this logic to scheduleVertexConjuncts or at least ensure
		// that we can also share data Vertices?
		if x.IsData() {
			n.unshare()
			n.insertValueConjunct(env, x, id)
		} else {
			if x.ClosedNonRecursive {
				n.node.ClosedNonRecursive = true
				id = n.addResolver(x, x, id, false)
			}
			n.scheduleVertexConjuncts(c, x, id)
		}

	case Value:
		n.insertValueConjunct(env, x, id)

	case *OpenExpr:
		// This is not strictly necessary, but it ensures the same code path
		// is taken for references that now have been rewritten to have a ...
		// suffix.
		c.x = x.X
		c.CloseInfo.Opened = true // NOTE: seems unnecessary, but just to be sure.
		id.Opened = true
		n.scheduleConjunct(c, id)

	case *BinaryExpr:
		// NOTE: do not unshare: a conjunction could still allow structure
		// sharing, such as in the case of `ref & ref`.
		if x.Op == AndOp {
			// For (A & B)..., mark operands as ConjunctOpened instead of
			// Opened. This keeps each operand's close group active (for
			// mutual constraint checking between A and B) while still
			// suppressing closeOuter (so extra fields like d in
			// __closeAll({(#C1 & #C2)..., d: int}) are allowed).
			//
			// In ExplicitOpen mode, injectEmbedNode is a no-op, so no
			// embedding scope is created for the conjunction. We re-inject
			// it here so the evidence mechanism can distinguish between
			// fields from within the conjunction vs extra fields in the
			// enclosing struct.
			inner := id
			if inner.Opened {
				inner.Opened = false
				inner.ConjunctOpened = true
				inner.FromEmbed = true
				inner = n.newReq(x, inner, defEmbedding)
			}
			n.scheduleConjunct(MakeConjunct(env, x.X, inner), inner)
			n.scheduleConjunct(MakeConjunct(env, x.Y, inner), inner)
			return
		}

		n.unshare()
		// Even though disjunctions and conjunctions are excluded, the result
		// must may still be list in the case of list arithmetic. This could
		// be a scalar value only once this is no longer supported.
		n.scheduleTask(handleExpr, env, x, id)

	case *StructLit:
		n.unshare()
		n.scheduleStruct(env, x, id)

	case *ListLit:
		n.unshare()

		// At this point we known we have at least an empty list.
		n.updateCyclicStatus(id)

		env := &Environment{
			Up:     env,
			Vertex: n.node,
		}
		n.updateNodeType(ListKind, x, id)
		n.scheduleTask(handleListLit, env, x, id)

	case *DisjunctionExpr:
		n.unshare()
		id := id
		id.setOptional(n)

		n.ctx.holeID++
		// Reuse disjunctBuffer to avoid allocating a new slice for each disjunction.
		n.ctx.disjunctBuffer = n.ctx.disjunctBuffer[:0]
		for _, dv := range x.Values {
			n.ctx.disjunctBuffer = append(n.ctx.disjunctBuffer, disjunct{
				expr: dv.Val,
				mode: mode(x.HasDefaults, dv.Default),
			})
		}
		d := envDisjunct{
			env:       env,
			cloneID:   id,
			holeID:    n.ctx.holeID,
			src:       x,
			disjuncts: slices.Clone(n.ctx.disjunctBuffer),
		}
		n.scheduleDisjunction(d)
		n.updateConjunctInfo(TopKind, id, 0)

	case *Comprehension:
		// always a partial comprehension.
		n.insertComprehension(env, x, id)

	case Resolver:
		n.scheduleTask(handleResolver, env, x, id)

	case Evaluator:
		n.unshare()

		// Call expressions may end up in an infinite recursion if we do not
		// ensure that there is non-cyclic data to propagate the evaluation.
		// We therefore postpone call expressions until we have evidence that
		// such non-cyclic conjuncts exist.
		//
		// We only defer calls that contain references in their arguments, as
		// only those can cause infinite recursion. Calls with only literal
		// arguments (like `or(["a", "b"])`) are safe to evaluate immediately,
		// even when evaluated from within an unrelated cyclic context.
		//
		// TODO: this is rather hacky. We probably need a better solution
		// in a future iteration of the evaluator.
		if call, ok := x.(*CallExpr); ok && id.CycleType == IsCyclic && !n.hasNonCycle && !n.hasNonCyclic {
			if slices.ContainsFunc(call.Args, exprHasResolver) {
				n.hasAncestorCycle = true
				n.cyclicConjuncts = append(n.cyclicConjuncts, cyclicConjunct{c: c})
				return
			}
		}

		n.scheduleTask(handleExpr, env, x, id)

	default:
		panic("unreachable")
	}

	n.ctx.stats.Conjuncts++
}

// exprHasResolver checks if an expression contains a Resolver (reference).
func exprHasResolver(x Expr) bool {
	switch v := x.(type) {
	case Resolver:
		return true
	case Value:
		return false // Already evaluated, cannot contain references.
	case *BinaryExpr:
		return exprHasResolver(v.X) || exprHasResolver(v.Y)
	case *UnaryExpr:
		return exprHasResolver(v.X)
	case *CallExpr:
		return slices.ContainsFunc(v.Args, exprHasResolver)
	case *ListLit:
		for _, elem := range v.Elems {
			if expr, ok := elem.(Expr); ok && exprHasResolver(expr) {
				return true
			}
		}
		return false
	default:
		return true // Conservatively assume unknown types may have references.
	}
}

// scheduleStruct records all elements of this conjunct in the structure and
// then processes it. If an element needs to be inserted for evaluation,
// it may be scheduled.
func (n *nodeContext) scheduleStruct(env *Environment,
	s *StructLit,
	ci CloseInfo) {
	n.overrideCyclicConjuncts(ci)
	n.updateConjunctInfo(StructKind, ci, cHasStruct)

	// NOTE: This is a crucial point in the code:
	// Unification dereferencing happens here. The child nodes are set to
	// an Environment linked to the current node. Together with the De Bruijn
	// indices, this determines to which Vertex a reference resolves.

	childEnv := &Environment{
		Up:     env,
		Vertex: n.node,
	}

	hasEmbed := false
	hasEllipsis := false

	n.hasStruct = true

	// TODO: do we still need this?
	// shouldClose := ci.cc.isDef || ci.cc.isClosedOnce

	// Walk the env chain to find the comprehension firing whose body produced
	// this struct, if any. Toposort uses this to group sibling decls inserted
	// by the same comprehension body (see [StructInfo.CompID]).
	var compID uint32
	for e := env; e != nil; e = e.Up {
		if e.CompID != 0 {
			compID = e.CompID
			break
		}
	}
	// TODO: do we still need to AddStruct?
	n.node.AddStruct(s, compID)

	// TODO(perf): precompile whether struct has embedding.
loop1:
	for _, d := range s.Decls {
		switch d.(type) {
		case *Comprehension, Expr:
			hasEmbed = true
			break loop1
		}
	}

	// When inserting a replace that is a definition, flip the ignore.
	if hasEmbed { // only if more than one decl.
		ci = n.splitStruct(s, ci)
	}

	// Struct literals with static elements are common;
	// grow the capacity ahead of time to make space for their arcs.
	n.node.Arcs = slices.Grow(n.node.Arcs, len(s.Decls))

	// First add fixed fields and schedule expressions.
	for _, d := range s.Decls {
		switch x := d.(type) {
		case *Field:
			if x.Label.IsString() && x.ArcType == ArcMember {
				n.markStructLit(s, ci)
			}
			ci := n.ctx.subField(ci)
			if x.ArcType == ArcOptional {
				ci.setOptional(n)
			}

			fc := MakeConjunct(childEnv, x, ci)
			n.insertArc(x.Label, x.ArcType, fc, ci, true)

		case *LetField:
			ci := n.ctx.subField(ci)
			lc := MakeConjunct(childEnv, x, ci)
			n.insertArc(x.Label, ArcMember, lc, ci, true)

		case *Comprehension:
			ci := n.injectEmbedNode(x, ci)
			n.insertComprehension(childEnv, x, ci)
			hasEmbed = true

		case *Ellipsis:
			// Can be added unconditionally to patterns.
			hasEllipsis = true

		case *DynamicField:
			ci := n.ctx.subField(ci)
			if x.ArcType == ArcMember {
				n.markStructLit(s, ci)
			}
			n.scheduleTask(handleDynamic, childEnv, x, ci)

		case *BulkOptionalField:
			ci := n.ctx.subField(ci)
			ci.setOptional(n)

			// All do not depend on each other, so can be added at once.
			n.scheduleTask(handlePatternConstraint, childEnv, x, ci)

		case Expr:
			ci := n.injectEmbedNode(x, ci)
			ec := MakeConjunct(childEnv, x, ci)
			n.scheduleConjunct(ec, ci)
			hasEmbed = true
		}
	}
	if hasEllipsis {
		n.node.HasEllipsis = true
		n.updateConjunctInfo(TopKind, ci, cHasEllipsis)
	}
	if !hasEmbed {
		n.markStructLit(s, ci)
	}
}

// markStructLit records that struct literal s makes the node a struct: it has
// a regular field, or no embedding that could make it a scalar. Such a literal
// is a value of its own and so breaks a reference cycle through the node,
// unlike one whose kind is whatever its embeddings yield, as in `a: {a + 1}`.
// See [OpContext.evalStateCI].
func (n *nodeContext) markStructLit(s *StructLit, ci CloseInfo) {
	n.aStruct = true
	n.hasFieldValue = true
	n.updateNodeType(StructKind, s, ci)
}

// scheduleVertexConjuncts injects the conjuncst of src n. If src was not fully
// evaluated, it subscribes dst for future updates.
func (n *nodeContext) scheduleVertexConjuncts(c Conjunct, arc *Vertex, closeInfo CloseInfo) {
	// A function call reference carries its payload on the reference itself:
	// the anchor arc it resolves to exists only to give the structural cycle
	// detector a stable identity per function and deliberately has no
	// conjuncts. Dispatching here, rather than at the call sites of this
	// function, guarantees that every path consuming a scheduled or parked
	// (resolver, arc) pair — task processing as well as cyclic-conjunct
	// revival — schedules the call payload consistently. None of the
	// struct-oriented logic below (sharing, closedness, arc dedup,
	// notifications) applies to the conjunct-less anchor.
	if ref, ok := c.Elem().(*FuncCallRef); ok {
		n.scheduleFuncCall(ref, c.Env, closeInfo)
		return
	}

	// We should not "activate" an enclosing struct for typo checking if it is
	// derived from an embedded, inlined value:
	//
	//    #Schema: foo: { {embed: embedded: "foo"}.embed }
	//    #Schema: foo: { field: string }
	//
	// Even though the embedding is within a schema, it should not treat the
	// struct as closed if it itself does not refer to a schema, as it may still
	// be unified with another struct.
	//
	// We check this by checking if the result is not marked as Closed.
	// Alternativley, we could always disable this for inlined structs.
	//
	// TODO(#A...): this code could go if we had explicitly opened values.
	if !arc.ClosedRecursive &&
		!arc.ClosedNonRecursive &&
		closeInfo.enclosingEmbed != 0 {
		closeInfo.FromDef = false
	}
	if c.CloseInfo.Opened || c.CloseInfo.ConjunctOpened {
		n.setEmbedClosedness(arc)
	}

	// disjunctions, we need to dereference he underlying node.
	if deref(n.node) == deref(arc) {
		n.linkCyclicResolver(arc, closeInfo)
		if n.isShared {
			n.addShared(closeInfo)
		}
		return
	}

	if n.shareIfPossible(c, arc, closeInfo) {
		arc.getState(n.ctx)
		return
	}

	// Also check arc.Label: definitions themselves do not have the FromDef to
	// reflect their closedness. This means that if we are structure sharing, we
	// may end up with a Vertex that is a definition without the reference
	// reflecting that. We need to handle this case here. Note that if an
	// intermediate node refers to a definition, things are evaluated at least
	// once.
	//
	// A reference may also reach a recursively closed value without naming a
	// definition, by selecting from a regular field that holds one:
	//
	//	#D: a: {b: 1}
	//	x: #D
	//	y: x.a & {c: 2} // c is not allowed: x.a is closed by #D
	//
	// Such a value is closed like the definition it came from, so its
	// closedness must carry over to the referring node as well.
	//
	// The same holds for a field of such a shared definition, reached from
	// outside the definition through a regular field that holds it:
	//
	//	#D: s: {a: 1}
	//	d: #D
	//	h: d.s & {b: 2} // b is not allowed: d.s is #D.s
	//
	// The conjuncts of #D.s carry no FromDef either, so the reference must
	// supply it, as it does for #D.s. A reference from within the definition
	// itself, such as a sibling field referring to _a, does not close.
	switch isDef, _ := IsDef(c.Expr()); {
	case isDef || arc.Label.IsDef() || closeInfo.TopDef || crossesDefinition(c.Env, arc):
		if c.CloseInfo.Opened || c.CloseInfo.ConjunctOpened {
			// Definitions are always recursively closed, even if arc
			// doesn't have ClosedRecursive set yet at this point.
			n.embedClosedness = embedRecursivelyClosed
		}
		n.isDef = true
		// n.node.ClosedRecursive = true // TODO: should we set this here?
		closeInfo.FromDef = true
		closeInfo.TopDef = false

		closeInfo = n.addResolver(c.x, arc, closeInfo, false)
	default:
		closeInfo = n.addResolver(c.x, arc, closeInfo, true)
	}
	if closeInfo.defID != 0 && closeInfo.opID == n.ctx.opID {
		c.CloseInfo.opID = closeInfo.opID
		c.CloseInfo.defID = closeInfo.defID
		c.CloseInfo.outerID = closeInfo.outerID
		c.CloseInfo.enclosingEmbed = closeInfo.enclosingEmbed
	}

	// We need to ensure that each arc is only unified once (or at least) a
	// bounded time, witch each conjunct. Comprehensions, for instance, may
	// distribute a value across many values that get unified back into the
	// same value. If such a value is a disjunction, than a disjunction of N
	// disjuncts will result in a factor N more unifications for each
	// occurrence of such value, resulting in exponential running time. This
	// is especially common values that are used as a type.
	//
	// However, unification is idempotent, so each such conjunct only needs
	// to be unified once. This cache checks for this and prevents an
	// exponential blowup in such case.
	//
	// TODO(perf): this cache ensures the conjuncts of an arc at most once
	// per ID. However, we really need to add the conjuncts of an arc only
	// once total, and then add the close information once per close ID
	// (pointer can probably be shared). Aside from being more performant,
	// this is probably the best way to guarantee that conjunctions are
	// linear in this case.
	if slices.Contains(n.arcMap, arc) {
		return
	}
	n.arcMap = append(n.arcMap, arc)

	if arc.Parent != nil && (!n.node.nonRooted || n.node.IsDynamic) {
		// If the arc has a parent that for which the field conjuncts are not
		// fully known yet, we may not have collected all conjuncts yet. In that
		// case we need ot add n to the notification list of arc to ensure
		// we will get the notifications in the future.
		pState := arc.Parent.getState(n.ctx)
		state := arc.getBareState(n.ctx)
		if pState != nil && state != nil &&
			!pState.meets(allAncestorsProcessed|fieldConjunctsKnown) {
			state.addNotify2(n.node, closeInfo)
		}
	}

	// Use explicit index in case Conjuncts grows during iteration.
	for i := 0; i < len(arc.Conjuncts); i++ {
		c := arc.Conjuncts[i]
		n.scheduleConjunct(c, closeInfo)
	}

	if state := arc.getBareState(n.ctx); state != nil {
		n.toComplete = true
	}
}

func (n *nodeContext) addNotify2(v *Vertex, c CloseInfo) {
	// No need to do the notification mechanism if we are already complete.
	switch {
	case n.node.isFinal():
		return
	case !n.node.isInProgress():
	case n.meets(allAncestorsProcessed):
		return
	}

	for _, r := range n.notify {
		if r.v == v {
			// TODO: might need to add replacement here.
			return
		}
	}

	// TODO(mem): keeping track of notifications seems to be unnecessary. Still
	// consider it when we are reclaiming Vertex as well.
	//  s := v.getBareState(n.ctx) s.notifyCount++

	// TODO: it should not be necessary to register for notifications for
	// let expressions, so we could also filter for !n.node.Label.IsLet().
	// However, somehow this appears to result in slightly better error
	// messages.
	n.ctx.stats.Notifications++

	n.notify = append(n.notify, receiver{v, c})
}

// Literal conjuncts

// NoShareSentinel is a sentinel value that is used to disable sharing of
// nodes. We make this an error to make it clear that we discard the value.
var NoShareSentinel = &Bottom{
	Err: errors.Newf(token.NoPos, "no sharing"),
}

func (n *nodeContext) insertValueConjunct(env *Environment, v Value, id CloseInfo) {
	ctx := n.ctx

	switch x := v.(type) {
	case *Vertex:
		if x.ClosedNonRecursive {
			n.node.ClosedNonRecursive = true
		} else if x.ClosedRecursive {
			n.node.ClosedRecursive = true
		}
		// A vertex which holds a disjunction adds no resolver of its own:
		// its disjuncts do so as they are processed, each carrying its
		// closing. A disjunct closed by a builtin (see [Builtin.PerDisjunct])
		// carries that closing as a flag alone, so it needs the resolver
		// here like any other closed vertex.
		if (x.ClosedNonRecursive || x.ClosedRecursive) && disjunctionOf(x) == nil {
			id = n.addResolver(v, x, id, false)
		}
		if _, ok := x.BaseValue.(*StructMarker); ok {
			n.aStruct = true
			n.updateNodeType(StructKind, x, id)
		}

		if !x.IsData() {
			n.updateCyclicStatus(id)

			c := MakeConjunct(env, x, id)
			n.scheduleVertexConjuncts(c, x, id)
			return
		}

		// TODO: evaluate value?
		switch v := x.BaseValue.(type) {
		default:
			panic(fmt.Sprintf("invalid type %T", x.BaseValue))

		case *ListMarker:
			n.updateCyclicStatus(id)

			// TODO: arguably we know now that the type _must_ be a list.
			n.scheduleTask(handleListVertex, env, x, id)

			return

		case *StructMarker:
			for _, a := range x.Arcs {
				if a.ArcType != ArcMember {
					continue
				}
				// TODO(errors): report error when this is a regular field.
				c := MakeConjunct(nil, a, id)
				n.insertArc(a.Label, a.ArcType, c, id, true)
			}
			n.node.Structs = append(n.node.Structs, x.Structs...)

		case Value:
			n.insertValueConjunct(env, v, id)
		}

		return

	case *Bottom:
		n.unshare()
		if x == NoShareSentinel {
			return
		}
		n.addBottom(x)
		return

	case *Builtin:
		n.unshare()
		if v := x.BareValidator(); v != nil {
			n.insertValueConjunct(env, v, id)
			return
		}
	}

	if !n.updateNodeType(v.Kind(), v, id) {
		return
	}

	switch x := v.(type) {
	case *Disjunction:
		n.updateCyclicStatus(id)
		n.unshare()

		id := id
		id.setOptional(n)

		n.ctx.holeID++
		// Reuse disjunctBuffer to avoid allocating a new slice for each disjunction.
		n.ctx.disjunctBuffer = n.ctx.disjunctBuffer[:0]
		for i, dv := range x.Values {
			n.ctx.disjunctBuffer = append(n.ctx.disjunctBuffer, disjunct{
				expr: dv,
				mode: mode(x.HasDefaults, i < x.NumDefaults),
			})
		}
		d := envDisjunct{
			env:       env,
			cloneID:   id,
			holeID:    n.ctx.holeID,
			src:       x,
			disjuncts: slices.Clone(n.ctx.disjunctBuffer),
		}
		n.scheduleDisjunction(d)

	case *Conjunction:
		// TODO: consider sharing: conjunct could be `ref & ref`, for instance,
		// in which case ref could still be shared.

		for _, x := range x.Values {
			n.insertValueConjunct(env, x, id)
		}

	case *Top:
		n.updateCyclicStatus(id)

		n.hasTop = true
		n.updateConjunctInfo(TopKind, id, cHasTop)

	case *BasicType:
		n.unshare()
		n.updateCyclicStatus(id)
		if x.K != TopKind {
			n.updateConjunctInfo(TopKind, id, cHasTop)
		}

	case *BoundValue:
		n.unshare()
		n.updateCyclicStatus(id)

		switch x.Op {
		case LessThanOp, LessEqualOp, GreaterThanOp, GreaterEqualOp:
			bound := &n.upperBound
			if x.Op == GreaterThanOp || x.Op == GreaterEqualOp {
				bound = &n.lowerBound
			}
			if y := *bound; y != nil {
				if v := SimplifyBounds(ctx, n.kind, x, y); v != nil {
					*bound = nil
					n.insertValueConjunct(env, v, id)
					return
				}
			}
			*bound = x

		case EqualOp, NotEqualOp:
			// We treat equality as an open validator.
			n.updateConjunctInfo(TopKind, id, cHasOpenValidator|cHasTop)
			// Mark as top-like when the bound's kind overlaps with
			// composite types (e.g. !=null has kind TopKind &^ NullKind,
			// which includes StructKind). Without this, validateValue
			// prematurely marks the node as a concrete struct, causing
			// the constraint to be consumed and lost.
			if x.Kind()&CompositeKind != 0 {
				n.hasTop = true
			}
			fallthrough

		case MatchOp, NotMatchOp:
			// This check serves as simplifier, but also to remove duplicates.
			match := false
			n.checks = slices.DeleteFunc(n.checks, func(c Conjunct) bool {
				if y, ok := c.x.(*BoundValue); ok {
					switch SimplifyBounds(ctx, n.kind, x, y) {
					case y:
						match = true
					case x:
						return true
					}
				}
				return false
			})
			// TODO(perf): do an early check to be able to prune further
			// processing.
			if !match {
				n.checks = append(n.checks, MakeConjunct(env, x, id))
			}

			return
		}

	case Validator:
		n.unshare()
		// This check serves as simplifier, but also to remove duplicates.
		cx := MakeConjunct(env, x, id)
		kind := x.Kind()
		// A validator that is inserted in a closeContext should behave like top
		// in the sense that the closeContext should not be closed if no other
		// value is present that would erase top (cc.hasNonTop): if a field is
		// only associated with a validator, we leave it to the validator to
		// decide what fields are allowed.
		if kind&(ListKind|StructKind) != 0 {
			if b, ok := x.(*BuiltinValidator); ok && b.Builtin.NonConcrete {
				n.updateConjunctInfo(TopKind, id, cHasOpenValidator|cHasTop)
			} else {
				n.updateConjunctInfo(TopKind, id, cHasTop)
			}
		}

		for i, y := range n.checks {
			if b, ok := SimplifyValidator(ctx, cx, y); ok {
				// It is possible that simplification process triggered further
				// evaluation, finalizing this node and clearing the checks
				// slice. In that case it is safe to ignore the result.
				if len(n.checks) > 0 {
					n.checks[i] = b
				}
				return
			}
		}

		n.checks = append(n.checks, cx)

		// We use set the type of the validator argument here to ensure that
		// validation considers the ultimate value of embedded validators,
		// rather than assuming that the struct in which an expression is
		// embedded is always a struct.
		// TODO(validatorType): get rid of setting n.hasTop here.
		k := x.Kind()
		if k == TopKind {
			n.hasTop = true
			// TODO: should we set this here? Does not seem necessary.
			// n.updateConjunctInfo(TopKind, id, cHasTop)
		}
		n.updateNodeType(k, x, id)

	case *Vertex:
	// handled above.

	case *FuncValue:
		n.unshare()
		n.updateCyclicStatus(id)

		if y, ok := n.scalar.(*FuncValue); ok {
			z, b := mergeFuncValues(ctx, y, x)
			if b != nil {
				if err, ok := b.Err.(*ValueError); ok {
					err.AddPosition(y)
					err.AddPosition(x)
				}
				n.addBottom(b)
				break
			}
			if z == nil {
				// Distinct partial applications conflict, like any scalars.
				n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
				break
			}
			n.scalar = z
			break
		}
		if y, ok := n.scalar.(*Builtin); ok {
			// A builtin satisfies a compatible function type.
			z, b := mergeBuiltinFunc(ctx, y, x)
			if b != nil {
				if err, ok := b.Err.(*ValueError); ok {
					err.AddPosition(y)
					err.AddPosition(x)
				}
				n.addBottom(b)
				break
			}
			if z == nil {
				// A builtin conflicts with a proper function value.
				n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
				break
			}
			n.scalar = z
			break
		}
		if y := n.scalar; y != nil {
			// A function cannot unify with a scalar of any other kind.
			n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
			break
		}
		n.scalar = x
		n.scalarID = id.posInfo
		n.signal(scalarKnown)

	case *Builtin:
		n.unshare()
		n.updateCyclicStatus(id)

		switch y := n.scalar.(type) {
		case *FuncValue:
			// A builtin satisfies a compatible function type.
			z, b := mergeBuiltinFunc(ctx, x, y)
			if b != nil {
				if err, ok := b.Err.(*ValueError); ok {
					err.AddPosition(y)
					err.AddPosition(x)
				}
				n.addBottom(b)
				break
			}
			if z == nil {
				// A builtin conflicts with a proper function value.
				n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
				break
			}
			n.scalar = z

		case *Builtin:
			z, b := mergeBuiltins(ctx, x, y)
			if b != nil {
				if err, ok := b.Err.(*ValueError); ok {
					err.AddPosition(y)
					err.AddPosition(x)
				}
				n.addBottom(b)
				break
			}
			if z == nil {
				// Two distinct builtins conflict, like any scalars.
				n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
				break
			}
			n.scalar = z

		case nil:
			n.scalar = x
			n.scalarID = id.posInfo
			n.signal(scalarKnown)

		default:
			// A builtin cannot unify with a scalar of any other kind.
			n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
		}

	case Value: // *NullLit, *BoolLit, *NumLit, *StringLit, *BytesLit
		n.unshare()
		if p, isData := Pos(v).Priority(); isData {
			id.Priority = p
		}

		n.updateCyclicStatus(id)

		if y := n.scalar; y != nil {
			p1 := n.scalarID.Priority
			p2 := id.Priority
			if p1 != 0 && p2 != 0 {
				if p1 > p2 {
					// all good
					break
				} else if p1 < p2 {
					goto patchConjunct
				}
			}
			if !BinOpBool(ctx, errOnDiffType, EqualOp, x, y) {
				n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id.posInfo)
			}
			break
		}
	patchConjunct:
		n.scalar = x
		n.scalarID = id.posInfo
		// TODO: only set "scalarKnown" if there are no other high priority
		// conjuncts. Alternatively, we should process high priority conjuncts
		// in the scheduler first.
		n.signal(scalarKnown)

	default:
		panic(fmt.Sprintf("unknown value type %T", x))
	}

	if n.lowerBound != nil && n.upperBound != nil {
		if u := SimplifyBounds(ctx, n.kind, n.lowerBound, n.upperBound); u != nil {
			n.lowerBound = nil
			n.upperBound = nil
			n.insertValueConjunct(env, u, id)
		}
	}
}

// crossesDefinition reports whether arc lies within a definition that does
// not enclose the environment in which the reference to it was written: the
// reference then reaches the definition's value from outside, and must close
// like a reference to the definition. A reference written inside the
// definition, such as b in #D: {a: {}, b: a}, does not close, even if it is
// evaluated elsewhere because the definition's field was inlined.
//
// A reference evaluated through an inline struct, such as in.foo in
// (f & {in: #D}).out with f: {in: _, out: in.foo}, does not close either:
// selecting from an inline struct opens definition closedness, as it did in
// the previous evaluator. Only a value with no path from the root opens in
// this way: a comprehension body is written at a path and closes like the
// struct around it. See cue/testdata/eval/openinline.txtar,
// cue/testdata/eval/issue3672.txtar and cue/testdata/cycle/issue4228.txtar.
func crossesDefinition(env *Environment, arc *Vertex) bool {
	def := enclosingDef(arc.Parent)
	if def == nil {
		return false
	}
	for e := env; e != nil; e = e.Up {
		if e.Vertex == def {
			return false
		}
		// TODO(perf): IsDetached walks the parent chain of each vertex in
		// the environment chain, so this loop can go quadratic in deeply
		// nested values. Cache the result or bound the walk if this shows
		// up in profiles.
		if e.Vertex != nil && e.Vertex.IsDetached() {
			return false
		}
	}
	return true
}
