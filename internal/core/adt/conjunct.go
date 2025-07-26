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
			n.scheduleVertexConjuncts(c, x, id)
		}

	case Value:
		// TODO: perhaps some values could be shared.
		n.unshare()
		n.insertValueConjunct(env, x, id)

	case *BinaryExpr:
		// NOTE: do not unshare: a conjunction could still allow structure
		// sharing, such as in the case of `ref & ref`.
		if x.Op == AndOp {
			n.scheduleConjunct(MakeConjunct(env, x.X, id), id)
			n.scheduleConjunct(MakeConjunct(env, x.Y, id), id)
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
		n.updateCyclicStatusV3(id)

		env := &Environment{
			Up:     env,
			Vertex: n.node,
		}
		n.updateNodeType(ListKind, x, id)
		n.scheduleTask(handleListLit, env, x, id)

	case *DisjunctionExpr:
		n.unshare()
		id := id
		id.setOptionalV3(n)

		// TODO(perf): reuse envDisjunct values so that we can also reuse the
		// disjunct slice.
		n.ctx.holeID++
		d := envDisjunct{
			env:     env,
			cloneID: id,
			holeID:  n.ctx.holeID,
			src:     x,
			expr:    x,
		}
		for _, dv := range x.Values {
			d.disjuncts = append(d.disjuncts, disjunct{
				expr:      dv.Val,
				isDefault: dv.Default,
				mode:      mode(x.HasDefaults, dv.Default),
			})
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

		// Expressions that contain a call may end up in an infinite recursion
		// here if we do not ensure that there is non-cyclic data to propagate
		// the evaluation. We therefore postpone expressions until we have
		// evidence that such non-cyclic conjuncts exist.
		if id.CycleType == IsCyclic && !n.hasNonCycle && !n.hasNonCyclic {
			n.hasAncestorCycle = true
			n.cyclicConjuncts = append(n.cyclicConjuncts, cyclicConjunct{c: c})
			return
		}

		// Interpolation, UnaryExpr, CallExpr
		n.scheduleTask(handleExpr, env, x, id)

	default:
		panic("unreachable")
	}

	n.ctx.stats.Conjuncts++
}

// scheduleStruct records all elements of this conjunct in the structure and
// then processes it. If an element needs to be inserted for evaluation,
// it may be scheduled.
func (n *nodeContext) scheduleStruct(env *Environment,
	s *StructLit,
	ci CloseInfo) {
	n.updateCyclicStatusV3(ci)
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

	s.Init(n.ctx)

	// TODO: do we still need to AddStruct and do we still need to Disable?
	parent := n.node.AddStruct(s, childEnv, ci)
	parent.Disable = true // disable until processing is done.
	ci.IsClosed = false

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
	if hasEmbed && !s.isComprehension { // only if more than one decl.
		ci = n.splitStruct(s, ci)
	}

	// First add fixed fields and schedule expressions.
	for _, d := range s.Decls {
		switch x := d.(type) {
		case *Field:
			if x.Label.IsString() && x.ArcType == ArcMember {
				n.aStruct = s
				n.aStructID = ci
			}
			ci := n.ctx.subField(ci)
			if x.ArcType == ArcOptional {
				ci.setOptionalV3(n)
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
				n.aStruct = s
				n.aStructID = ci
			}
			n.scheduleTask(handleDynamic, childEnv, x, ci)

		case *BulkOptionalField:
			ci := n.ctx.subField(ci)
			ci.setOptionalV3(n)

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
		n.aStruct = s
		n.aStructID = ci
	}

	// TODO: probably no longer necessary.
	parent.Disable = false
}

// scheduleVertexConjuncts injects the conjuncst of src n. If src was not fully
// evaluated, it subscribes dst for future updates.
func (n *nodeContext) scheduleVertexConjuncts(c Conjunct, arc *Vertex, closeInfo CloseInfo) {
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

	// disjunctions, we need to dereference he underlying node.
	if deref(n.node) == deref(arc) {
		if n.isShared {
			n.addShared(closeInfo)
		}
		return
	}

	if n.shareIfPossible(c, arc, closeInfo) {
		arc.getState(n.ctx)
		return
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

	ciKey := closeInfo
	ciKey.Refs = nil
	ciKey.Inline = false
	if n.ctx.isDevVersion() {
		// No need to key on CloseInfo with evalv3.
		ciKey = CloseInfo{}
	}

	// Also check arc.Label: definitions themselves do not have the FromDef to
	// reflect their closedness. This means that if we are structure sharing, we
	// may end up with a Vertex that is a definition without the reference
	// reflecting that. We need to handle this case here. Note that if an
	// intermediate node refers to a definition, things are evaluated at least
	// once.
	switch isDef, _ := IsDef(c.Expr()); {
	case isDef || arc.Label.IsDef() || closeInfo.TopDef:
		n.isDef = true
		// n.node.ClosedRecursive = true // TODO: should we set this here?
		closeInfo.FromDef = true
		closeInfo.TopDef = false

		closeInfo = n.addResolver(arc, closeInfo, false)
	default:
		closeInfo = n.addResolver(arc, closeInfo, true)
	}
	if closeInfo.defID != 0 && closeInfo.opID == n.ctx.opID {
		c.CloseInfo.opID = closeInfo.opID
		c.CloseInfo.defID = closeInfo.defID
		c.CloseInfo.outerID = closeInfo.outerID
		c.CloseInfo.enclosingEmbed = closeInfo.enclosingEmbed
	}

	key := arcKey{arc, ciKey}
	for _, k := range n.arcMap {
		if key == k {
			return
		}
	}
	n.arcMap = append(n.arcMap, key)

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

// NoSharingSentinel is a sentinel value that is used to disable sharing of
// nodes. We make this an error to make it clear that we discard the value.
var NoShareSentinel = &Bottom{
	Err: errors.Newf(token.NoPos, "no sharing"),
}

func (n *nodeContext) insertValueConjunct(env *Environment, v Value, id CloseInfo) {
	ctx := n.ctx

	n.updateConjunctInfo(TopKind, id, 0)

	switch x := v.(type) {
	case *Vertex:
		if x.ClosedNonRecursive {
			n.node.ClosedNonRecursive = true

			// If this is a definition, it will be repeated in the evaluation.
			if !x.IsFromDisjunction() {
				id = n.addResolver(x, id, false)
			}
		}
		if _, ok := x.BaseValue.(*StructMarker); ok {
			n.aStruct = x
			n.aStructID = id
		}

		if !x.IsData() {
			n.updateCyclicStatusV3(id)

			c := MakeConjunct(env, x, id)
			n.scheduleVertexConjuncts(c, x, id)
			return
		}

		// TODO: evaluate value?
		switch v := x.BaseValue.(type) {
		default:
			panic(fmt.Sprintf("invalid type %T", x.BaseValue))

		case *ListMarker:
			n.updateCyclicStatusV3(id)

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
		if x == NoShareSentinel {
			n.unshare()
			return
		}
		n.addBottom(x)
		return

	case *Builtin:
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
		n.updateCyclicStatusV3(id)

		// TODO(perf): reuse envDisjunct values so that we can also reuse the
		// disjunct slice.
		id := id
		id.setOptionalV3(n)

		n.ctx.holeID++
		d := envDisjunct{
			env:     env,
			cloneID: id,
			holeID:  n.ctx.holeID,
			src:     x,
			value:   x,
		}
		for i, dv := range x.Values {
			d.disjuncts = append(d.disjuncts, disjunct{
				expr:      dv,
				isDefault: i < x.NumDefaults,
				mode:      mode(x.HasDefaults, i < x.NumDefaults),
			})
		}
		n.scheduleDisjunction(d)

	case *Conjunction:
		// TODO: consider sharing: conjunct could be `ref & ref`, for instance,
		// in which case ref could still be shared.

		for _, x := range x.Values {
			n.insertValueConjunct(env, x, id)
		}

	case *Top:
		n.updateCyclicStatusV3(id)

		n.hasTop = true
		n.updateConjunctInfo(TopKind, id, cHasTop)

	case *BasicType:
		n.updateCyclicStatusV3(id)
		if x.K != TopKind {
			n.updateConjunctInfo(TopKind, id, cHasTop)
		}

	case *BoundValue:
		n.updateCyclicStatusV3(id)

		switch x.Op {
		case LessThanOp, LessEqualOp:
			if y := n.upperBound; y != nil {
				v := SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.upperBound)
					err.AddClosedPositions(id)
				}
				n.upperBound = nil
				n.insertValueConjunct(env, v, id)
				return
			}
			n.upperBound = x

		case GreaterThanOp, GreaterEqualOp:
			if y := n.lowerBound; y != nil {
				v := SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.lowerBound)
					err.AddClosedPositions(id)
				}
				n.lowerBound = nil
				n.insertValueConjunct(env, v, id)
				return
			}
			n.lowerBound = x

		case EqualOp, NotEqualOp, MatchOp, NotMatchOp:
			// This check serves as simplifier, but also to remove duplicates.
			k := 0
			match := false
			for _, c := range n.checks {
				if y, ok := c.x.(*BoundValue); ok {
					switch z := SimplifyBounds(ctx, n.kind, x, y); {
					case z == y:
						match = true
					case z == x:
						continue
					}
				}
				n.checks[k] = c
				k++
			}
			n.checks = n.checks[:k]
			// TODO(perf): do an early check to be able to prune further
			// processing.
			if !match {
				n.checks = append(n.checks, MakeConjunct(env, x, id))
			}
			return
		}

	case Validator:
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

	case Value: // *NullLit, *BoolLit, *NumLit, *StringLit, *BytesLit, *Builtin
		n.updateCyclicStatusV3(id)

		if y := n.scalar; y != nil {
			if b, ok := BinOp(ctx, errOnDiffType, EqualOp, x, y).(*Bool); !ok || !b.B {
				n.reportConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id)
			}
			break
		}
		n.scalar = x
		n.scalarID = id
		n.signal(scalarKnown)

	default:
		panic(fmt.Sprintf("unknown value type %T", x))
	}

	if n.lowerBound != nil && n.upperBound != nil {
		if u := SimplifyBounds(ctx, n.kind, n.lowerBound, n.upperBound); u != nil {
			if err := valueError(u); err != nil {
				err.AddPosition(n.lowerBound)
				err.AddPosition(n.upperBound)
				err.AddClosedPositions(id)
			}
			n.lowerBound = nil
			n.upperBound = nil
			n.insertValueConjunct(env, u, id)
		}
	}
}
