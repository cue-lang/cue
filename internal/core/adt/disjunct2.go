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

// disjunctHole associates a closeContext copy representing a disjunct hole with
// the underlying closeContext from which it originally was branched.
// We could include this information in the closeContext itself, but since this
// is relatively rare, we keep it separate to avoid bloating the closeContext.
type disjunctHole struct {
	cc         *closeContext
	holeID     int
	underlying *closeContext
}

func (n *nodeContext) scheduleDisjunction(d envDisjunct) {
	if len(n.disjunctions) == 0 {
		// This processes all disjunctions in a single pass.
		n.scheduleTask(handleDisjunctions, nil, nil, CloseInfo{})
	}

	// ccHole is the closeContext in which the individual disjuncts are
	// scheduled.
	ccHole := d.cloneID.cc

	// This counter can be decremented after either a disjunct has been
	// scheduled in the clone. Note that it will not be closed in the original
	// as the result will either be an error, a single disjunct, in which
	// case mergeVertex will override the original value, or multiple disjuncts,
	// in which case the original is set to the disjunct itself.
	ccHole.incDisjunct(n.ctx, DISJUNCT)
	ccHole.holeID = d.holeID

	n.disjunctions = append(n.disjunctions, d)

	n.disjunctCCs = append(n.disjunctCCs, disjunctHole{
		cc:         ccHole, // this value is cloned in doDisjunct.
		holeID:     d.holeID,
		underlying: ccHole,
	})
}

func initArcs(ctx *OpContext, v *Vertex) bool {
	ok := true
	for _, a := range v.Arcs {
		s := a.getState(ctx)
		if s != nil && s.errs != nil {
			ok = false
			if a.ArcType == ArcMember {
				break
			}
		} else if !initArcs(ctx, a) {
			ok = false
		}
	}
	return ok
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

	a := n.disjunctions
	n.disjunctions = n.disjunctions[:0]

	holes := make([]disjunctHole, len(n.disjunctCCs))
	copy(holes, n.disjunctCCs)

	// Upon completion, decrement the DISJUNCT counters that were incremented
	// in scheduleDisjunction. Note that this disjunction may be a copy of the
	// original, in which case we need to decrement the copied disjunctCCs, not
	// the original.
	//
	// This is not strictly necessary, but it helps for balancing counters.
	// TODO: Consider disabling this when DebugDeps is not set.
	defer func() {
		// We add a "top" value to disable closedness checking for this
		// disjunction to avoid a spurious "field not allowed" error.
		// We return the errors below, which will, in turn, be reported as
		// the error.
		for i, d := range a {
			// TODO(perf: prove that holeIDs are always stored in increasing
			// order and allow for an incremental search to reduce cost.
			for _, h := range holes {
				if h.holeID != a[i].holeID {
					continue
				}
				cc := h.cc
				id := a[i].cloneID
				id.cc = cc
				c := MakeConjunct(d.env, top, id)
				n.scheduleConjunct(c, d.cloneID)
				cc.decDisjunct(n.ctx, DISJUNCT)
				break
			}
		}
	}()

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
			outerRunMode = p.state.runMode
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
		results = n.crossProduct(results, cross, d, mode, d.holeID)

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

		// switch up buffers.
		cross, results = results, cross[:0]
	}

	switch len(cross) {
	case 0:
		panic("unreachable: empty disjunction already handled above")

	case 1:
		d := cross[0].node
		n.setBaseValue(d)
		n.defaultMode = cross[0].defaultMode

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
func (n *nodeContext) crossProduct(dst, cross []*nodeContext, dn *envDisjunct, mode runMode, hole int) []*nodeContext {
	defer n.unmarkDepth(n.markDepth())
	defer n.unmarkOptional(n.markOptional())

	for i, p := range cross {
		ID := n.nextCrossProduct(i, len(cross), p)

		// TODO: use a partial unify instead
		// p.completeNodeConjuncts()
		initArcs(n.ctx, p.node)

		for j, d := range dn.disjuncts {
			ID.node.nextDisjunct(j, len(dn.disjuncts), d.expr)

			c := MakeConjunct(dn.env, d.expr, dn.cloneID)
			r, err := p.doDisjunct(c, d.mode, mode, hole)

			if err != nil {
				// TODO: store more error context
				dn.disjuncts[j].err = err
				continue
			}

			// Unroll nested disjunctions.
			switch len(r.disjuncts) {
			case 0:
				// r did not have a nested disjunction.
				dst = appendDisjunct(n.ctx, dst, r)

			case 1:
				panic("unexpected number of disjuncts")

			default:
				for _, x := range r.disjuncts {
					dst = appendDisjunct(n.ctx, dst, x)
				}
			}
		}
	}
	return dst
}

// collectErrors collects errors from a failed disjunctions.
func (n *nodeContext) collectErrors(dn *envDisjunct) (errs *Bottom) {
	code := EvalError
	for _, d := range dn.disjuncts {
		if b := d.err; b != nil {
			n.disjunctErrs = append(n.disjunctErrs, b)
			if b.Code > code {
				code = b.Code
			}
		}
	}

	b := &Bottom{
		Code: code,
		Err:  n.disjunctError(),
		Node: n.node,
	}
	return b
}

func (n *nodeContext) doDisjunct(c Conjunct, m defaultMode, mode runMode, hole int) (*nodeContext, *Bottom) {
	if c.CloseInfo.cc == nil {
		panic("nil closeContext during init")
	}

	ID := n.logDoDisjunct()
	_ = ID // Do not remove, used for debugging.

	oc := newOverlayContext(n.ctx)

	var ccHole *closeContext

	// TODO(perf): resuse buffer, for instance by keeping a buffer handy in oc
	// and then swapping it with disjunctCCs in the new nodeContext.
	holes := make([]disjunctHole, 0, len(n.disjunctCCs))

	// Complete as much of the pending work of this node and its parent before
	// copying. Note that once a copy is made, the disjunct is no longer able
	// to receive conjuncts from the original.
	n.completeNodeTasks(mode)
	// TODO: we may need to process incoming notifications for all arcs in
	// the copied disjunct, but only those notifications not coming from
	// within the arc itself.

	// Clone the closeContexts of all open disjunctions and dependencies.
	for _, d := range n.disjunctCCs {
		// TODO: remove filled holes.

		// Note that the root is already cloned as part of cloneVertex and that
		// a closeContext corresponding to a disjunction always has a parent.
		// We therefore do not need to check whether x.parent is nil.
		o := oc.allocCC(d.cc)
		if hole == d.holeID {
			ccHole = o
			if d.cc.conjunctCount == 0 {
				panic("unexpected zero conjunctCount")
			}
		}
		holes = append(holes, disjunctHole{o, d.holeID, d.underlying})
	}

	if ccHole == nil {
		panic("expected non-nil overlay closeContext")
	}

	n.scheduler.blocking = n.scheduler.blocking[:0]

	d := oc.cloneRoot(n)
	d.runMode = mode

	d.defaultMode = combineDefault(m, n.defaultMode)

	v := d.node

	defer n.setBaseValue(n.swapBaseValue(v))

	// Clear relevant scheduler states.
	// TODO: do something more principled: just ensure that a node that has
	// not all holes filled out yet is not finalized. This may require
	// a special mode, or evaluating more aggressively if finalize is not given.
	v.status = unprocessed

	d.overlays = n
	d.disjunctCCs = append(d.disjunctCCs, holes...)
	d.disjunct = c
	c.CloseInfo.cc = ccHole
	d.scheduleConjunct(c, c.CloseInfo)
	ccHole.decDisjunct(n.ctx, DISJUNCT)

	oc.unlinkOverlay()

	v.unify(n.ctx, allKnown, mode)

	if err := d.getErrorAll(); err != nil && !isCyclePlaceholder(err) {
		d.free()
		return nil, err
	}

	d = d.node.DerefDisjunct().state

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
	for _, x := range n.disjuncts {
		x.node.Conjuncts = nil
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
	n.setBaseValue(d)

	// The conjuncts will have too much information. Better have no
	// information than incorrect information.
	v.Arcs = nil
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
		xv := xn.node.DerefValue()
		if xv.status != finalized || nv.status != finalized {
			// Partial node

			// TODO: we could consider supporting an option here to disable
			// the filter. This way, if there is a bug, users could disable
			// it, trading correctness for performance.
			// If enabled, we would simply "continue" here.

			for i, h := range xn.disjunctCCs {
				// TODO(perf): only iterate over completed
				// TODO(evalv3): we now have a double loop to match the
				// disjunction holes. It should be possible to keep them
				// aligned and avoid the inner loop.
				for _, g := range x.disjunctCCs {
					if h.underlying == g.underlying {
						x, y := findIntersections(h.cc, x.disjunctCCs[i].cc)
						if !equalPartialNode(xn.ctx, x, y) {
							continue outer
						}
					}
				}
			}
			if len(xn.tasks) != xn.taskPos || len(x.tasks) != x.taskPos {
				if len(xn.tasks) != len(x.tasks) {
					continue
				}
			}
			for i, t := range xn.tasks[xn.taskPos:] {
				s := x.tasks[i]
				if s.x != t.x || s.id.cc != t.id.cc {
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
		// TODO: x.free()
		return a
	}

	return append(a, x)
}

// findIntersections reports the closeContext, relative to the two given
// disjunction holes, that should be used in comparing the arc set.
// x and y MUST both be originating from the same disjunct hole. This ensures
// that the depth of the parent chain is the same and that they have the
// same underlying closeContext.
//
// Currently, we just take the parent. We should investigate if that is always
// sufficient.
//
// Tradeoffs: if we do not go up enough, the two nodes may not be equal and we
// miss the opportunity to filter. On the other hand, if we go up too far, we
// end up comparing more arcs than potentially necessary.
//
// TODO: Add a unit test when this function is fully implemented.
func findIntersections(x, y *closeContext) (cx, cy *closeContext) {
	cx = x.parent
	cy = y.parent

	// TODO: why could this happen? Investigate. Note that it is okay to just
	// return x and y. In the worst case we will just miss some possible
	// deduplication.
	if cx == nil || cy == nil {
		return x, y
	}

	return cx, cy
}

func equalPartialNode(ctx *OpContext, x, y *closeContext) bool {
	nx := x.src.getState(ctx)
	ny := y.src.getState(ctx)

	if nx == nil && ny == nil {
		// Both nodes were finalized. We can compare them directly.
		return Equal(ctx, x.src, y.src, CheckStructural)
	}

	// TODO: process the nodes with allKnown, attemptOnly.

	if nx == nil || ny == nil {
		return false
	}

	if !isEqualNodeValue(nx, ny) {
		return false
	}

	if len(x.Patterns) != len(y.Patterns) {
		return false
	}
	// Assume patterns are in the same order.
	for i, p := range x.Patterns {
		if !Equal(ctx, p, y.Patterns[i], 0) {
			return false
		}
	}

	if !Equal(ctx, x.Expr, y.Expr, 0) {
		return false
	}

	if len(x.arcs) != len(y.arcs) {
		return false
	}

	// TODO(perf): use merge sort
outer:
	for _, a := range x.arcs {
		for _, b := range y.arcs {
			if a.root.src.Label != b.root.src.Label {
				continue
			}
			if !equalPartialNode(ctx, a.dst, b.dst) {
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
		if s.id.cc != t.id.cc {
			// FIXME: we should compare this too. For this to work we need to
			// have access to the underlying closeContext, which we do not
			// have at the moment.
			// return false
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
