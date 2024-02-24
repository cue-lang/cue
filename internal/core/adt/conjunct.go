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

import "fmt"

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
	// Explanation of switch statement:
	//
	// A Conjunct can be a leaf or, through a ConjunctGroup, a tree. The tree
	// reflects the history of how the conjunct was inserted in terms of
	// definitions and embeddings. This, in turn, is used to compute closedness.
	//
	// Once all conjuncts for a Vertex have been collected, this tree contains
	// all the information needed to trace its histroy: if a Vertex is
	// referenced in an expression, this tree can be used to insert the
	// conjuncts keeping closedness in mind.
	//
	// In the collection phase, however, this is not sufficient. CUE computes
	// conjuncts "out of band". This means that conjuncts accumulate in
	// different parts of the tree in an indeterminate order. closeContext is
	// used to account for this.
	//
	// Basically, if the closeContext associated with c belongs to n, we take
	// it that the conjunct needs to be inserted at the point in the tree
	// associated by this closeContext. If, on the other hand, the closeContext
	// is not defined or does not belong to this node, we take this conjunct
	// is inserted by means of a reference. In this case we assume that the
	// computation of the tree has completed and the tree can be used to reflect
	// the closedness structure.
	//
	// TODO: once the evaluator is done and all tests pass, consider having
	// two different entry points to account for these cases.
	switch cc := c.CloseInfo.cc; {
	case cc == nil || cc.src != n.node:
		// In this case, a Conjunct is inserted from another Arc. If the
		// conjunct represents an embedding or definition, we need to create a
		// new closeContext to represent this.
		if id.cc == nil {
			id.cc = n.node.rootCloseContext()
		}
		if id.cc == cc {
			panic("inconsistent state")
		}
		var t closeNodeType
		if c.CloseInfo.FromDef {
			t |= closeDef
		}
		if c.CloseInfo.FromEmbed {
			t |= closeEmbed
		}
		if t != 0 {
			id, _ = id.spawnCloseContext(t)
		}
		if !id.cc.done {
			id.cc.incDependent(DEFER, nil)
			defer id.cc.decDependent(n.ctx, DEFER, nil)
		}

		if id.cc.src != n.node {
			panic("inconsistent state")
		}
	default:

		// In this case, the conjunct is inserted as the result of an expansion
		// of a conjunct in place, not a reference. In this case, we must use
		// the cached closeContext.
		id.cc = cc

		// Note this subtlety: we MUST take the cycle info from c when this is
		// an in place evaluated node, otherwise we must take that of id.
		id.CycleInfo = c.CloseInfo.CycleInfo
	}

	if id.cc.needsCloseInSchedule != nil {
		dep := id.cc.needsCloseInSchedule
		id.cc.needsCloseInSchedule = nil
		defer id.cc.decDependent(n.ctx, EVAL, dep)
	}

	env := c.Env

	if id.cc.isDef {
		n.node.Closed = true
	}

	switch x := c.Elem().(type) {
	case *ConjunctGroup:
		for _, c := range *x {
			// TODO(perf): can be one loop

			cc := c.CloseInfo.cc
			if cc.src == n.node && cc.needsCloseInSchedule != nil {
				// We need to handle this specifically within the ConjunctGroup
				// loop, because multiple conjuncts may be using the same root
				// closeContext. This can be merged once Vertex.Conjuncts is an
				// interface, requiring any list to be a root conjunct.

				dep := cc.needsCloseInSchedule
				cc.needsCloseInSchedule = nil
				defer cc.decDependent(n.ctx, EVAL, dep)
			}
		}
		for _, c := range *x {
			n.scheduleConjunct(c, id)
		}

	case *Vertex:
		if x.IsData() {
			n.insertValueConjunct(env, x, id)
		} else {
			n.scheduleVertexConjuncts(c, x, id)
		}

	case Value:
		n.insertValueConjunct(env, x, id)

	case *BinaryExpr:
		if x.Op == AndOp {
			n.scheduleConjunct(MakeConjunct(env, x.X, id), id)
			n.scheduleConjunct(MakeConjunct(env, x.Y, id), id)
			return
		}
		// Even though disjunctions and conjunctions are excluded, the result
		// must may still be list in the case of list arithmetic. This could
		// be a scalar value only once this is no longer supported.
		n.scheduleTask(handleExpr, env, x, id)

	case *StructLit:
		n.scheduleStruct(env, x, id)

	case *ListLit:
		env := &Environment{
			Up:     env,
			Vertex: n.node,
		}
		n.scheduleTask(handleListLit, env, x, id)

	case *DisjunctionExpr:
		panic("unimplemented")
		// n.addDisjunction(env, x, id)

	case *Comprehension:
		// always a partial comprehension.
		n.insertComprehension(env, x, id)

	case Resolver:
		n.scheduleTask(handleResolver, env, x, id)

	case Evaluator:
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
	n.updateCyclicStatus(ci)

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

	// shouldClose := ci.cc.isDef || ci.cc.isClosedOnce
	// s.Init()

	// TODO: do we still need to AddStruct and do we still need to Disable?
	parent := n.node.AddStruct(s, childEnv, ci)
	parent.Disable = true // disable until processing is done.
	ci.IsClosed = false

	// TODO: precompile
loop1:
	for _, d := range s.Decls {
		switch d.(type) {
		case *Ellipsis:
			hasEllipsis = true
			break loop1
		}
	}

	// TODO(perf): precompile whether struct has embedding.
loop2:
	for _, d := range s.Decls {
		switch d.(type) {
		case *Comprehension, Expr:
			// No need to increment and decrement, as there will be at least
			// one entry.
			ci, _ = ci.spawnCloseContext(0)
			// Note: adding a count is not needed here, as there will be an
			// embed spawn below.
			hasEmbed = true
			break loop2
		}
	}

	// First add fixed fields and schedule expressions.
	for _, d := range s.Decls {
		switch x := d.(type) {
		case *Field:
			if x.Label.IsString() && x.ArcType == ArcMember {
				n.aStruct = s
				n.aStructID = ci
			}
			fc := MakeConjunct(childEnv, x, ci)
			// fc.CloseInfo.cc = nil // TODO: should we add this?
			n.insertArc(x.Label, x.ArcType, fc, ci, true)

		case *LetField:
			lc := MakeConjunct(childEnv, x, ci)
			n.insertArc(x.Label, ArcMember, lc, ci, true)

		case *Comprehension:
			ci, cc := ci.spawnCloseContext(closeEmbed)
			cc.incDependent(DEFER, nil)
			defer cc.decDependent(n.ctx, DEFER, nil)
			n.insertComprehension(childEnv, x, ci)
			hasEmbed = true

		case *Ellipsis:
			// Can be added unconditionally to patterns.
			ci.cc.isDef = false
			ci.cc.isClosed = false

		case *DynamicField:
			if x.ArcType == ArcMember {
				n.aStruct = s
				n.aStructID = ci
			}
			n.scheduleTask(handleDynamic, childEnv, x, ci)

		case *BulkOptionalField:

			// All do not depend on each other, so can be added at once.
			n.scheduleTask(handlePatternConstraint, childEnv, x, ci)

		case Expr:
			// TODO: perhaps special case scalar Values to avoid creating embedding.
			ci, cc := ci.spawnCloseContext(closeEmbed)

			// TODO: do we need to increment here?
			cc.incDependent(DEFER, nil) // decrement deferred below
			defer cc.decDependent(n.ctx, DEFER, nil)

			ec := MakeConjunct(childEnv, x, ci)
			n.scheduleConjunct(ec, ci)
			hasEmbed = true
		}
	}
	if hasEllipsis {
		ci.cc.hasEllipsis = true
	}
	if !hasEmbed {
		n.aStruct = s
		n.aStructID = ci
		ci.cc.hasNonTop = true
	}

	// TODO: probably no longer necessary.
	parent.Disable = false
}

// scheduleVertexConjuncts injects the conjuncst of src n. If src was not fully
// evaluated, it subscribes dst for future updates.
func (n *nodeContext) scheduleVertexConjuncts(c Conjunct, arc *Vertex, closeInfo CloseInfo) {
	// Don't add conjuncts if a node is referring to itself.
	if n.node == arc {
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
	key := arcKey{arc, ciKey}
	for _, k := range n.arcMap {
		if key == k {
			return
		}
	}
	n.arcMap = append(n.arcMap, key)

	if IsDef(c.Expr()) {
		// TODO: or should we always insert the wrapper (for errors)?
		ci, dc := closeInfo.spawnCloseContext(closeDef)
		closeInfo = ci

		dc.incDependent(DEFER, nil) // decrement deferred below
		defer dc.decDependent(n.ctx, DEFER, nil)
	}

	if state := arc.getState(n.ctx); state != nil {
		state.addNotify2(n.node, closeInfo)
	}

	for i := 0; i < len(arc.Conjuncts); i++ {
		c := arc.Conjuncts[i]

		// Note that we are resetting the tree here. We hereby assume that
		// closedness conflicts resulting from unifying the referenced arc were
		// already caught there and that we can ignore further errors here.
		// c.CloseInfo = closeInfo

		// We can use the original, but we know it will not be used

		n.scheduleConjunct(c, closeInfo)
	}
}

func (n *nodeContext) addNotify2(v *Vertex, c CloseInfo) []receiver {
	n.completeNodeTasks()

	// No need to do the notification mechanism if we are already complete.
	old := n.notify
	if n.meets(allAncestorsProcessed) {
		return old
	}

	// Create a "root" closeContext to reflect the entry point of the
	// reference into n.node relative to cc within v. After that, we can use
	// assignConjunct to add new conjuncts.

	// TODO: dedup: only add if t does not already exist. First check if this
	// is even possible by adding a panic.
	root := n.node.rootCloseContext()
	if root.isDecremented {
		return old
	}

	for _, r := range n.notify {
		if r.v == v && r.cc == c.cc {
			return old
		}
	}

	cc := c.cc

	if root.linkNotify(v, cc, c.CycleInfo) {
		n.notify = append(n.notify, receiver{v, cc})
		n.completeNodeTasks()
	}

	return old
}

// Literal conjuncts

func (n *nodeContext) insertValueConjunct(env *Environment, v Value, id CloseInfo) {
	n.updateCyclicStatus(id)

	ctx := n.ctx

	switch x := v.(type) {
	case *Vertex:
		if m, ok := x.BaseValue.(*StructMarker); ok {
			n.aStruct = x
			n.aStructID = id
			if m.NeedClose {
				// TODO: In the new evaluator this is used to mark a struct
				// as closed in the debug output. Once the old evaluator is
				// gone, we could simplify this.
				id.IsClosed = true
				if ctx.isDevVersion() {
					var cc *closeContext
					id, cc = id.spawnCloseContext(0)
					cc.isClosedOnce = true
				}
			}
		}

		if !x.IsData() {
			c := MakeConjunct(env, x, id)
			n.scheduleVertexConjuncts(c, x, id)
			return
		}

		// TODO: evaluate value?
		switch v := x.BaseValue.(type) {
		default:
			panic(fmt.Sprintf("invalid type %T", x.BaseValue))

		case *ListMarker:
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

		case Value:
			n.insertValueConjunct(env, v, id)
		}

		return

	case *Bottom:
		id.cc.hasNonTop = true
		n.addBottom(x)
		return

	case *Builtin:
		id.cc.hasNonTop = true
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
		n.addDisjunctionValue(env, x, id)

	case *Conjunction:
		for _, x := range x.Values {
			n.insertValueConjunct(env, x, id)
		}

	case *Top:
		n.hasTop = true
		id.cc.hasTop = true

	case *BasicType:
		id.cc.hasNonTop = true

	case *BoundValue:
		id.cc.hasNonTop = true
		switch x.Op {
		case LessThanOp, LessEqualOp:
			if y := n.upperBound; y != nil {
				n.upperBound = nil
				v := SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.upperBound)
					err.AddClosedPositions(id)
				}
				n.insertValueConjunct(env, v, id)
				return
			}
			n.upperBound = x

		case GreaterThanOp, GreaterEqualOp:
			if y := n.lowerBound; y != nil {
				n.lowerBound = nil
				v := SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.lowerBound)
					err.AddClosedPositions(id)
				}
				n.insertValueConjunct(env, v, id)
				return
			}
			n.lowerBound = x

		case EqualOp, NotEqualOp, MatchOp, NotMatchOp:
			// This check serves as simplifier, but also to remove duplicates.
			k := 0
			match := false
			for _, c := range n.checks {
				if y, ok := c.(*BoundValue); ok {
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
			if !match {
				n.checks = append(n.checks, x)
			}
			return
		}

	case Validator:
		// This check serves as simplifier, but also to remove duplicates.
		for i, y := range n.checks {
			if b := SimplifyValidator(ctx, x, y); b != nil {
				n.checks[i] = b
				return
			}
		}
		n.updateNodeType(x.Kind(), x, id)
		n.checks = append(n.checks, x)

	case *Vertex:
	// handled above.

	case Value: // *NullLit, *BoolLit, *NumLit, *StringLit, *BytesLit, *Builtin
		if y := n.scalar; y != nil {
			if b, ok := BinOp(ctx, EqualOp, x, y).(*Bool); !ok || !b.B {
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
