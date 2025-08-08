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

// # Overview
//
// This files contains the disjunction algorithm of the CUE evaluator. It works
// in unison with the code in overlay.go.
//
// In principle, evaluating disjunctions is a matter of unifying each disjunct
// with the non-disjunct values, eliminate those that fail and see what is left.
// In case of multiple disjunctions it is a simple cross product of disjuncts.
// The key is how to do this efficiently.
//
// # Classification of disjunction performance
//
// The key to an efficient disjunction algorithm is to minimize the impact of
// taking cross product of disjunctions. This is especially pertinent if
// disjunction expressions can be unified with themselves, as can be the case in
// recursive definitions, as this can lead to exponential time complexity.
//
// We identify the following categories of importance for performance
// optimization:
//
//  - Eliminate duplicates
//      - For completed disjunctions
//      - For partially computed disjuncts
//  - Fail early / minimize work before failure
//      - Filter disjuncts before unification (TODO)
//          - Based on discriminator field
//          - Based on a non-destructive unification of the disjunct and
//            the current value computed so far
//      - During the regular destructive unification
//          - Traverse arcs where failure may occur
//          - Copy on write (TODO)
//
// We discuss these aspects in more detail below.
//
// # Eliminating completed duplicates
//
// Eliminating completed duplicates can be achieved by comparing them for
// equality. A disjunct can only be considered completed if all disjuncts have
// been selected and evaluated, or at any time if processing fails.
//
// The following values should be recursively considered for equality:
//
//  - the value of the node,
//  - the value of its arcs,
//  - the key and value of the pattern constraints, and
//  - the expression of the allowed fields.
//
// In some of these cases it may not be possible to detect if two nodes are
// equal. For instance, two pattern constraints with two different regular
// expressions as patterns, but that define an identical language, should be
// considered equal. In practice, however, this is hard to distinguish.
//
// In the end this is mostly a matter of performance. As we noted, the biggest
// concern is to avoid a combinatorial explosion when disjunctions are unified
// with itself. The hope is that we can at least catch these cases, either
// because they will evaluate to the same values, or because we can identify
// that the underlying expressions are the same, or both.
//
// # Eliminating partially-computed duplicates
//
// We start with some observations and issues regarding partially evaluated
// nodes.
//
// ## Issue: Closedness
//
// Two identical CUE values with identical field, values, and pattern
// constraints, may still need to be consider as different, as they may exhibit
// different closedness behavior. Consider, for instance, this example:
//
//  #def: {
//      {} | {c: string} // D1
//      {} | {a: string} // D2
//  }
//  x: #def
//  x: c: "foo"
//
// Now, consider the case of the cross product that unifies the two empty
// structs for `x`. Note that `x` already has a field `c`. After unifying the
// first disjunction with `x`, both intermediate disjuncts will have the value
// `{c: "foo"}`:
//
//         {c: "foo"} & ({} | {c: string})
//       =>
//         {c: "foo"} | {c: "foo"}
//
// One would think that one of these disjuncts can be eliminated. Nonetheless,
// there is a difference. The second disjunct, which resulted from unifying
//  `{c: "foo"}` with `{c: string}`, will remain valid. The first disjunct,
// however, will fail after it is unified and completed with the `{}` of the
// second disjunctions (D2): only at this point is it known that x was unified
// with an empty closed struct, and that field `c` needs to be rejected.
//
// One possible solution would be to fully compute the cross product of `#def`
// and use this expanded disjunction for unification, as this would mean that
// full knowledge of closedness information is available.
//
// Although this is possible in some cases and can be a useful performance
// optimization, it is not always possible to use the fully evaluated disjuncts
// in such a precomputed cross product. For instance, if a disjunction relies on
// a comprehension or a default value, it is not possible to fully evaluate the
// disjunction, as merging it with another value may change the inputs for such
// expressions later on. This means that we can only rely on partial evaluation
// in some cases.
//
// ## Issue: Outstanding tasks in partial results
//
// Some tasks may not be completed until all conjuncts are known. For cross
// products of disjunctions this may mean that such tasks cannot be completed
// until all cross products are done. For instance, it is typically not possible
// to evaluate a tasks that relies on taking a default value that may change as
// more disjuncts are added. A similar argument holds for comprehensions on
// values that may still be changed as more disjunctions come in.
//
// ## Evaluating equality of partially evaluated nodes
//
// Because unevaluated expressions may depend on results that have yet to be
// computed, we cannot reliably compare the results of a Vertex to determine
// equality. We need a different strategy.
//
// The strategy we take is based on the observation that at the start of a cross
// product, the base conjunct is the same for all disjuncts. We can factor these
// inputs out and focus on the differences between the disjuncts. In other
// words, we can focus solely on the differences that manifest at the insertion
// points (or "disjunction holes") of the disjuncts.
//
// In short, two disjuncts are equal if:
//
//  1. the disjunction holes that were already processed are equal, and
//  2. they have either no outstanding tasks, or the outstanding tasks are equal
//
// Coincidentally, analyzing the differences as discussed in this section is
// very similar in nature to precomputing a disjunct and using that. The main
// difference is that we potentially have more information to prematurely
// evaluate expressions and thus to prematurely filter values. For instance, the
// mixed in value may have fixed a value that previously was not fixed. This
// means that any expression referencing this value may be evaluated early and
// can cause a disjunct to fail and be eliminated earlier.
//
// A disadvantage of this approach, however, is that it is not fully precise: it
// may not filter some disjuncts that are logically identical. There are
// strategies to further optimize this. For instance, if all remaining holes do
// not contribute to closedness, which can be determined by walking up the
// closedness parent chain, we may be able to safely filter disjuncts with equal
// results.
//
// # Invariants
//
// We use the following assumptions in the below implementation:
//
//  - No more conjuncts are added to a disjunct after its processing begins.
//    If a disjunction results in a value that causes more fields to be added
//    later, this may not influence the result of the disjunction, i.e., those
//    changes must be idempotent.
//  - TODO: consider if any other assumptions are made.
//
// # Algorithm
//
// The evaluator accumulates all disjuncts of a Vertex in the nodeContext along
// with the closeContext at which each was defined. A single task is scheduled
// to process them all at once upon the first encounter of a disjunction.
//
// The algorithm is as follows:
//  - Initialize the current Vertex n with the result evaluated so far as a
//    list of "previous disjuncts".
//  - Iterate over each disjunction
//    - For each previous disjunct x
//      - For each disjunct y in the current disjunctions
//        - Unify
//        - Discard if error, store in the list of current disjunctions if
//          it differs from all other disjunctions in this list.
//  - Set n to the result of the disjunction.
//
// This algorithm is recursive: if a disjunction is encountered in a disjunct,
// it is processed as part of the evaluation of that disjunct.
//

// A disjunct is the expanded form of the disjuncts of either an Disjunction or
// a DisjunctionExpr.
//
// TODO(perf): encode ADT structures in the correct form so that we do not have to
// compute these each time.
type disjunct struct {
	expr Expr
	err  *Bottom

	isDefault bool
	mode      defaultMode
}

func (n *nodeContext) scheduleDisjunction(d envDisjunct) {
	if len(n.disjunctions) == 0 {
		// This processes all disjunctions in a single pass.
		n.scheduleTask(handleDisjunctions, nil, nil, CloseInfo{})
	}

	n.disjunctions = append(n.disjunctions, d)
	n.hasDisjunction = true
}

func initArcs(ctx *OpContext, v *Vertex) bool {
	for _, a := range v.Arcs {
		s := a.getState(ctx)
		if s != nil && s.errs != nil {
			if a.ArcType == ArcMember {
				return false
			}
		} else if !initArcs(ctx, a) {
			return false
		}
	}
	return true
}

func (n *nodeContext) processDisjunctions() *Bottom {
	ID := n.pushDisjunctionTask()
	defer ID.pop()

	defer func() {
		// TODO:
		// Clear the buffers.
		// TODO: we may want to retain history of which disjunctions were
		// processed. In that case we can set a disjunction position to end
		// of the list and schedule new tasks if this position equals the
		// disjunction list length.
	}()

	// TODO(perf): check scalar errors so far to avoid unnecessary work.

	// TODO: during processing disjunctions, new disjunctions may be added.
	// We copy the slice to prevent the original slice from being overwritten.
	// TODO(perf): use some pre-existing buffer or use a persising position
	// so that disjunctions can be processed incrementally.
	a := slices.Clone(n.disjunctions)
	n.disjunctions = n.disjunctions[:0]

	if !initArcs(n.ctx, n.node) {
		return n.getError()
	}

	// If the disjunct of an enclosing disjunction operation has an attemptOnly
	// runMode, this disjunct should have this also and may not finalize.
	// Finalization may cause incoming dependencies to be broken. If an outer
	// disjunction still has open holes, this means that more conjuncts may be
	// incoming and that finalization would prematurely prevent those from being
	// added. In practice, this may result in the infamous "already closed"
	// panic.
	var outerRunMode runMode
	for p := n.node; p != nil; p = p.Parent {
		if p.IsDisjunct {
			if p.state == nil {
				outerRunMode = finalize
			} else {
				outerRunMode = p.state.runMode
			}
			break
		}
	}

	// TODO(perf): single pass for quick filter on all disjunctions.
	// n.node.unify(n.ctx, allKnown, attemptOnly)

	// Initially we compute the cross product of a disjunction with the
	// nodeContext as it is processed so far.
	cross := []*nodeContext{n}
	results := []*nodeContext{} // TODO: use n.disjuncts as buffer.

	// Slow path for processing all disjunctions. Do not use `range` in case
	// evaluation adds more disjunctions.
	for i := 0; i < len(a); i++ {
		d := &a[i]
		n.nextDisjunction(i, len(a), d.holeID)

		// We need to only finalize the last series of disjunctions. However,
		// disjunctions can be nested.
		mode := attemptOnly
		switch {
		case outerRunMode != 0:
			mode = outerRunMode
			if i < len(a)-1 {
				mode = attemptOnly
			}
		case i == len(a)-1:
			mode = finalize
		}

		// Mark no final in nodeContext and observe later.
		results = n.crossProduct(results, cross, d, mode)

		// TODO: do we unwind only at the end or also intermittently?
		switch len(results) {
		case 0:
			// TODO: now we have disjunct counters, do we plug holes at all?

			// Empty intermediate result. Further processing will not result in
			// any new result, so we can terminate here.
			// TODO(errors): investigate remaining disjunctions for errors.
			return n.collectErrors(d)

		case 1:
			// TODO: consider injecting the disjuncts into the main nodeContext
			// here. This would allow other values that this disjunctions
			// depends on to be evaluated. However, we should investigate
			// whether this might lead to a situation where the order of
			// evaluating disjunctions matters. So to be safe, we do not allow
			// this for now.
		}

		if i > 0 {
			for _, n := range cross {
				n.freeDisjunct()
			}
		}

		// switch up buffers.
		cross, results = results, cross[:0]
	}

	switch len(cross) {
	case 0:
		panic("unreachable: empty disjunction already handled above")

	case 1:
		d := cross[0].node
		n.setBaseValue(d)
		if n.defaultMode == maybeDefault {
			n.defaultMode = cross[0].defaultMode
		}
		if n.defaultAttemptInCycle != nil && n.defaultMode != isDefault {
			c := n.ctx
			path := c.PathToString(n.defaultAttemptInCycle.Path())

			index := c.MarkPositions()
			c.AddPosition(n.defaultAttemptInCycle)
			err := c.Newf("ambiguous default elimination by referencing %v", path)
			c.ReleasePositions(index)

			b := &Bottom{Code: CycleError, Err: err}
			n.setBaseValue(b)
			return b
		}

	default:
		// append, rather than assign, to allow reusing the memory of
		// a pre-existing slice.
		n.disjuncts = append(n.disjuncts, cross...)
	}

	var completed condition
	numDefaults := 0
	if len(n.disjuncts) == 1 {
		completed = n.disjuncts[0].completed
	}
	for _, d := range n.disjuncts {
		if d.defaultMode == isDefault {
			numDefaults++
			completed = d.completed
		}
	}
	if numDefaults == 1 || len(n.disjuncts) == 1 {
		n.signal(completed)
	}

	return nil
}

// crossProduct computes the cross product of the disjuncts of a disjunction
// with an existing set of results.
func (n *nodeContext) crossProduct(dst, cross []*nodeContext, dn *envDisjunct, mode runMode) []*nodeContext {
	defer n.unmarkDepth(n.markDepth())
	defer n.unmarkOptional(n.markOptional())

	// TODO(perf): use a pre-allocated buffer in n.ctx. Note that the actual
	// buffer may grow and has a max size of len(cross) * len(dn.disjuncts).
	tmp := make([]*nodeContext, 0, len(cross))

	leftDropsDefault := true
	rightDropsDefault := true

	for i, p := range cross {
		ID := n.nextCrossProduct(i, len(cross), p)

		// TODO: use a partial unify instead
		// p.completeNodeConjuncts()
		initArcs(n.ctx, p.node)

		for j, d := range dn.disjuncts {
			ID.node.nextDisjunct(j, len(dn.disjuncts), d.expr)

			c := MakeConjunct(dn.env, d.expr, dn.cloneID)
			r, err := p.doDisjunct(c, d.mode, mode, n.node)

			if err != nil {
				// TODO: store more error context
				dn.disjuncts[j].err = err
				continue
			}

			tmp = append(tmp, r)
			if p.defaultMode == isDefault || p.origDefaultMode == isDefault {
				leftDropsDefault = false
			}
			if d.mode == isDefault {
				rightDropsDefault = false
			}
		}
	}

	hasNonMaybe := false
	for _, r := range tmp {
		// Unroll nested disjunctions.
		switch len(r.disjuncts) {
		case 0:
			r.defaultMode = combineDefault2(r.defaultMode, r.origDefaultMode, leftDropsDefault, rightDropsDefault)
			// r did not have a nested disjunction.
			dst = appendDisjunct(n.ctx, dst, r)

		case 1:
			panic("unexpected number of disjuncts")

		default:
			for _, x := range r.disjuncts {
				m := combineDefault(r.origDefaultMode, x.defaultMode)

				// TODO(defaults): using rightHasDefault instead of false here
				// is not according to the spec, but may result in better user
				// ergononmics. See Issue #1304.
				x.defaultMode = combineDefault2(r.defaultMode, m, leftDropsDefault, false)
				if x.defaultMode != maybeDefault {
					hasNonMaybe = true
				}
				dst = appendDisjunct(n.ctx, dst, x)
			}
		}
	}

	if hasNonMaybe {
		for _, r := range dst {
			if r.defaultMode == maybeDefault {
				r.defaultMode = notDefault
			}
		}
	}

	return dst
}

func combineDefault2(a, b defaultMode, dropsDefaultA, dropsDefaultB bool) defaultMode {
	if dropsDefaultA {
		a = maybeDefault
	}
	if dropsDefaultB {
		b = maybeDefault
	}
	return combineDefault(a, b)
}

// collectErrors collects errors from a failed disjunctions.
func (n *nodeContext) collectErrors(dn *envDisjunct) (errs *Bottom) {
	code := EvalError
	hasUserError := false
	for _, d := range dn.disjuncts {
		if b := d.err; b != nil {
			if b.Code > code {
				code = b.Code
			}
			switch {
			case b.Code == UserError:
				if !hasUserError {
					n.disjunctErrs = n.disjunctErrs[:0]
				}
				hasUserError = true

			case hasUserError:
				continue
			}
			n.disjunctErrs = append(n.disjunctErrs, b)
		}
	}

	b := &Bottom{
		Code: code,
		Err:  n.disjunctError(),
		Node: n.node,
	}
	return b
}

// doDisjunct computes a single disjunct. n is the current disjunct that is
// augmented, whereas orig is the original node where disjunction processing
// started. orig is used to clean up Environments.
func (n *nodeContext) doDisjunct(c Conjunct, m defaultMode, mode runMode, orig *Vertex) (*nodeContext, *Bottom) {
	ID := n.logDoDisjunct()
	_ = ID // Do not remove, used for debugging.

	oc := newOverlayContext(n.ctx)

	// Complete as much of the pending work of this node and its parent before
	// copying. Note that once a copy is made, the disjunct is no longer able
	// to receive conjuncts from the original.
	n.completeNodeTasks(mode)

	// TODO: we may need to process incoming notifications for all arcs in
	// the copied disjunct, but only those notifications not coming from
	// within the arc itself.

	n.scheduler.blocking = n.scheduler.blocking[:0]

	// TODO(perf): do not set to nil, but rather maintain an index to unwind
	// to avoid allocting new arrays.
	// TODO: ideally, we move unresolved tasks to the original vertex for
	// disambiguated disjuncts.
	saved := n.ctx.blocking
	n.ctx.blocking = nil
	defer func() { n.ctx.blocking = saved }()

	// We forward the original base value to the disjunct. This allows for
	// lookups with the disjunct to the original value.
	var savedBase BaseValue
	if !orig.IsDisjunct {
		savedBase = orig.BaseValue
		defer func() { orig.BaseValue = savedBase }()
	}

	d := oc.cloneRoot(n)

	// This mechanism only works if the original is not a disjunct.
	if !orig.IsDisjunct {
		orig.BaseValue = d.node
	}

	n.ctx.pushOverlay(n.node, oc.vertexMap)
	defer n.ctx.popOverlay()

	d.runMode = mode
	c.Env = oc.derefDisjunctsEnv(c.Env)

	v := d.node

	defer n.setBaseValue(n.swapBaseValue(v))

	// Clear relevant scheduler states.
	// TODO: do something more principled: just ensure that a node that has
	// not all holes filled out yet is not finalized. This may require
	// a special mode, or evaluating more aggressively if finalize is not given.
	v.status = unprocessed

	d.scheduleConjunct(c, c.CloseInfo)

	oc.unlinkOverlay()

	d.defaultMode = n.defaultMode
	d.origDefaultMode = m

	v.unify(n.ctx, allKnown, mode, true)

	if err := d.getErrorAll(); err != nil && !isCyclePlaceholder(err) {
		d.freeDisjunct()
		return nil, err
	}

	d.node.DerefDisjunct().state.origDefaultMode = d.origDefaultMode
	d = d.node.DerefDisjunct().state // TODO: maybe do not unroll at all.

	return d, nil
}

func (n *nodeContext) finalizeDisjunctions() {
	if len(n.disjuncts) == 0 {
		return
	}

	// TODO: we clear the Conjuncts to be compatible with the old evaluator.
	// This is especially relevant for the API. Ideally, though, we should
	// update Conjuncts to reflect the actual conjunct that went into the
	// disjuncts.
	numErrs := 0
	for _, x := range n.disjuncts {
		x.node.Conjuncts = nil

		if b := x.getErr(); b != nil {
			n.disjunctErrs = append(n.disjunctErrs, b)
			numErrs++
			continue
		}
	}

	if len(n.disjuncts) == numErrs {
		n.makeError()
		return
	}

	a := make([]Value, len(n.disjuncts))
	p := 0
	hasDefaults := false
	for i, x := range n.disjuncts {
		switch x.defaultMode {
		case isDefault:
			a[i] = a[p]
			a[p] = x.node
			p++
			hasDefaults = true

		case notDefault:
			hasDefaults = true
			fallthrough
		case maybeDefault:
			a[i] = x.node
		}
	}

	d := &Disjunction{
		Values:      a,
		NumDefaults: p,
		HasDefaults: hasDefaults,
	}

	v := n.node

	if n.defaultAttemptInCycle == nil || d.NumDefaults == 1 {
		n.setBaseValue(d)
	} else {
		c := n.ctx
		path := c.PathToString(n.defaultAttemptInCycle.Path())

		index := c.MarkPositions()
		c.AddPosition(n.defaultAttemptInCycle)
		err := c.Newf("cycle across unresolved disjunction referenced by %v", path)
		c.ReleasePositions(index)

		b := &Bottom{Code: CycleError, Err: err}
		n.setBaseValue(b)
	}

	// The conjuncts will have too much information. Better have no
	// information than incorrect information.
	v.clearArcs(n.ctx)
	v.ChildErrors = nil
}

func (n *nodeContext) getErrorAll() *Bottom {
	err := n.getError()
	if err != nil {
		return err
	}
	for _, a := range n.node.Arcs {
		if a.ArcType > ArcRequired || a.Label.IsLet() {
			return nil
		}
		n := a.getState(n.ctx)
		if n != nil {
			if err := n.getErrorAll(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *nodeContext) getError() *Bottom {
	if b := n.node.Bottom(); b != nil && !isCyclePlaceholder(b) {
		return b
	}
	if n.node.ChildErrors != nil {
		return n.node.ChildErrors
	}
	if errs := n.errs; errs != nil {
		return errs
	}
	if n.ctx.errs != nil {
		return n.ctx.errs
	}
	return nil
}

// appendDisjunct appends a disjunct x to a, if it is not a duplicate.
func appendDisjunct(ctx *OpContext, a []*nodeContext, x *nodeContext) []*nodeContext {
	if x == nil {
		return a
	}

	nv := x.node.DerefValue()
	nx := nv.BaseValue
	if nx == nil || isCyclePlaceholder(nx) {
		nx = x.getValidators(finalized)
	}

	// check uniqueness
	// TODO: if a node is not finalized, we could check that the parent
	// (overlayed) closeContexts are identical.
outer:
	for _, xn := range a {
		// TODO: for some reason, r may already have been added to dst in some
		// cases, so we need to check for this.
		if xn == x {
			return a
		}
		xv := xn.node.DerefValue()
		if xv.status != finalized || nv.status != finalized {
			// Partial node

			if !equalPartialNode(xn.ctx, x.node, xn.node) {
				continue outer
			}
			if len(xn.tasks) != xn.taskPos || len(x.tasks) != x.taskPos {
				if len(xn.tasks) != len(x.tasks) {
					continue
				}
			}
			for i, t := range xn.tasks[xn.taskPos:] {
				s := x.tasks[i]
				if s.x != t.x {
					continue outer
				}
			}
			vx, okx := nx.(Value)
			ny := xv.BaseValue
			if ny == nil || isCyclePlaceholder(ny) {
				ny = x.getValidators(finalized)
			}
			vy, oky := ny.(Value)
			if okx && oky && !Equal(ctx, vx, vy, CheckStructural) {
				continue outer

			}
		} else {
			// Complete nodes.
			if !Equal(ctx, xn.node.DerefValue(), x.node.DerefValue(), CheckStructural) {
				continue outer
			}
		}

		// free vertex
		if x.defaultMode == isDefault {
			xn.defaultMode = isDefault
		}
		mergeCloseInfo(xn, x)
		x.freeDisjunct()
		return a
	}

	return append(a, x)
}

func equalPartialNode(ctx *OpContext, x, y *Vertex) bool {
	nx := x.state
	ny := y.state

	if nx == nil && ny == nil {
		// Both nodes were finalized. We can compare them directly.
		return Equal(ctx, x, y, CheckStructural)
	}

	// TODO: process the nodes with allKnown, attemptOnly.

	if nx == nil || ny == nil {
		return false
	}

	if !isEqualNodeValue(nx, ny) {
		return false
	}

	switch cx, cy := x.PatternConstraints, y.PatternConstraints; {
	case cx == nil && cy == nil:
	case cx == nil || cy == nil:
		return false
	case len(cx.Pairs) != len(cy.Pairs):
		return false
	default:
		// Assume patterns are in the same order.
		for i, p := range cx.Pairs {
			p.Constraint.Finalize(ctx)
			cy.Pairs[i].Constraint.Finalize(ctx)
			if !Equal(ctx, p.Constraint, cy.Pairs[i].Constraint, CheckStructural) {
				return false
			}
		}
	}

	if len(x.Arcs) != len(y.Arcs) {
		return false
	}

	// TODO(perf): use merge sort
outer:
	for _, a := range x.Arcs {
		for _, b := range y.Arcs {
			if a.Label != b.Label {
				continue
			}
			if !equalPartialNode(ctx, a, b) {
				return false
			}
			continue outer
		}
		return false
	}
	return true
}

// isEqualNodeValue reports whether the two nodes are of the same type and have
// the same value.
//
// TODO: this could be done much more cleanly if we are more deligent in early
// evaluation.
func isEqualNodeValue(x, y *nodeContext) bool {
	xk := x.kind
	yk := y.kind

	// If a node is mid evaluation, the kind might not be actual if the type is
	// a struct, as whether a struct is a struct kind or an embedded type is
	// determined later. This is just a limitation of the current
	// implementation, we should update the kind more directly so that this code
	// is not necessary.
	// TODO: verify that this is still necessary and if so fix it so that this
	// can be removed.
	if x.aStruct != nil {
		xk &= StructKind
	}
	if y.aStruct != nil {
		yk &= StructKind
	}

	if xk != yk {
		return false
	}
	if x.hasTop != y.hasTop {
		return false
	}
	if !isEqualValue(x.ctx, x.scalar, y.scalar) {
		return false
	}

	// Do some quick checks first.
	if len(x.checks) != len(y.checks) {
		return false
	}
	if len(x.tasks) != x.taskPos || len(y.tasks) != y.taskPos {
		if len(x.tasks) != len(y.tasks) {
			return false
		}
	}

	if !isEqualValue(x.ctx, x.lowerBound, y.lowerBound) {
		return false
	}
	if !isEqualValue(x.ctx, x.upperBound, y.upperBound) {
		return false
	}

	// Assume that checks are added in the same order.
	for i, c := range x.checks {
		d := y.checks[i]
		if !Equal(x.ctx, c.x.(Value), d.x.(Value), CheckStructural) {
			return false
		}
	}

	for i, t := range x.tasks[x.taskPos:] {
		s := y.tasks[i]
		if s.x != t.x {
			return false
		}
	}

	return true
}

type ComparableValue interface {
	comparable
	Value
}

func isEqualValue[P ComparableValue](ctx *OpContext, x, y P) bool {
	var zero P

	if x == y {
		return true
	}
	if x == zero || y == zero {
		return false
	}

	return Equal(ctx, x, y, CheckStructural)
}

// IsFromDisjunction reports whether any conjunct of v was a disjunction.
// There are three cases:
//  1. v is a disjunction itself. This happens when the result is an
//     unresolved disjunction.
//  2. v is a disjunct. This happens when only a single disjunct remains. In this
//     case there will be a forwarded node that is marked with IsDisjunct.
//  3. the disjunction was erroneous and none of the disjuncts failed.
//
// TODO(evalv3): one case that is not covered by this is erroneous disjunctions.
// This is not the worst, but fixing it may lead to better error messages.
func (v *Vertex) IsFromDisjunction() bool {
	_, ok := v.BaseValue.(*Disjunction)
	return ok || v.isDisjunct()
}

// TODO: export this instead of IsDisjunct
func (v *Vertex) isDisjunct() bool {
	for {
		if v.IsDisjunct {
			return true
		}
		arc, ok := v.BaseValue.(*Vertex)
		if !ok {
			return false
		}
		v = arc
	}
}
