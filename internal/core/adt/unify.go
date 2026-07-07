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

	"cuelang.org/go/cue/token"
)

// TODO(mpvl): perhaps conjunctsProcessed is a better name for this.
func (v *Vertex) isInitialized() bool {
	return v.status == finalized || (v.state != nil && v.state.isInitialized)
}

func (n *nodeContext) assertInitialized() {
	if n != nil {
		if n.node == nil {
			// Can happen for unit tests.
			return
		}
		if v := n.node; !v.isInitialized() {
			panic(fmt.Sprintf("vertex %p not initialized", v))
		}
	}
}

// isInProgress reports whether v is in the midst of being evaluated. This means
// that conjuncts have been scheduled, but that it has not been finalized.
func (v *Vertex) isInProgress() bool {
	return v.status != finalized && v.state != nil && v.state.isInitialized
}

func (v *Vertex) getBareState(c *OpContext) *nodeContext {
	if v.status == finalized { // TODO: use BaseValue != nil
		return nil
	}
	if v.state == nil {
		v.state = c.newNodeContext(v)
		v.state.initBare()
	}

	// TODO: see if we can get rid of ref counting after new evaluator is done:
	// the recursive nature of the new evaluator should make this unnecessary.

	return v.state
}

func (v *Vertex) getState(c *OpContext) *nodeContext {
	s := v.getBareState(c)
	if s != nil && !s.isInitialized {
		s.scheduleConjuncts()
	}
	return s
}

// initNode initializes a nodeContext for the evaluation of the given Vertex.
func (n *nodeContext) initBare() {
	v := n.node
	if v.Parent != nil && v.Parent.state != nil {
		v.state.depth = v.Parent.state.depth + 1
		n.blockOn(allAncestorsProcessed)
	}

	n.blockOn(scalarKnown | arcTypeKnown)

	if v.Label.IsDef() {
		v.ClosedRecursive = true
	}

	if v.Parent != nil {
		if v.Parent.ClosedRecursive {
			v.ClosedRecursive = true
		}
	}
}

func (n *nodeContext) scheduleConjuncts() {
	n.isInitialized = true

	v := n.node
	ctx := n.ctx

	ctx.stats.Unifications++

	// Set the cache to a cycle error to ensure a cyclic reference will result
	// in an error if applicable. A cyclic error may be ignored for
	// non-expression references. The cycle error may also be removed as soon
	// as there is evidence what a correct value must be, but before all
	// validation has taken place.
	//
	// TODO(cycle): having a more recursive algorithm would make this
	// special cycle handling unnecessary.
	v.BaseValue = cycle

	defer ctx.PopArc(ctx.PushArc(v))

	for i, c := range v.Conjuncts {
		_ = i // for debugging purposes
		ci := c.CloseInfo
		ci = ctx.combineCycleInfo(ci)
		n.scheduleConjunct(c, ci)
	}
}

// flushDeferredCyclicConjuncts re-schedules cyclic conjuncts that
// [nodeContext.scheduleConjunct] postponed but that cannot themselves
// trigger infinite recursion — currently those whose resolver arguments
// are all [LabelReference]. The CallExpr deferment is a conservative
// cycle break that waits for a non-cyclic conjunct to confirm the node
// can terminate; when none arrives, the deferred ones would otherwise
// stay parked in [nodeContext.cyclicConjuncts] and the node resolves to _.
//
// This construct is necessary after the pushdown algorithm moved from pushing
// down fields to pushing down dependencies.
func (n *nodeContext) flushDeferredCyclicConjuncts() {
	if len(n.cyclicConjuncts) == 0 || len(n.scheduler.tasks) > 0 {
		return
	}

	// Suppress further deferment on this node so dispatched conjuncts
	// cannot re-append into n.cyclicConjuncts.
	n.hasNonCyclic = true

	// In-place two-pointer compaction: dispatch safe entries, keep
	// unsafe ones at the front of the slice.
	write := 0
	for read := 0; read < len(n.cyclicConjuncts); read++ {
		cc := n.cyclicConjuncts[read]
		if !isSafeToFlushCyclic(cc.c.Elem()) {
			if write != read {
				n.cyclicConjuncts[write] = cc
			}
			write++
			continue
		}
		ci := cc.c.CloseInfo
		ci.CycleType = NoCycle
		if cc.arc != nil {
			// Unreachable today: safe entries are Evaluators, not
			// arc-bearing Resolvers. Kept for forward compatibility.
			n.scheduleVertexConjuncts(cc.c, cc.arc, ci)
		} else {
			c := cc.c
			c.CloseInfo = ci
			n.scheduleConjunct(c, ci)
		}
	}
	n.cyclicConjuncts = n.cyclicConjuncts[:write]
}

// isSafeToFlushCyclic reports whether a conjunct's expression contains
// only resolvers that cannot trigger a vertex re-entry — currently just
// [LabelReference], which reads a label from the surrounding env. Other
// resolvers (FieldReference, SelectorExpr, etc.) may indirectly re-enter
// the same vertex being evaluated and so must remain deferred.
func isSafeToFlushCyclic(x Elem) bool {
	switch v := x.(type) {
	case *LabelReference:
		return true
	case Value:
		return true
	case *BinaryExpr:
		return isSafeToFlushCyclic(v.X) && isSafeToFlushCyclic(v.Y)
	case *UnaryExpr:
		return isSafeToFlushCyclic(v.X)
	case *CallExpr:
		for _, a := range v.Args {
			if !isSafeToFlushCyclic(a) {
				return false
			}
		}
		return true
	case *ListLit:
		for _, e := range v.Elems {
			if expr, ok := e.(Expr); ok && !isSafeToFlushCyclic(expr) {
				return false
			}
		}
		return true
	case *Interpolation:
		for _, p := range v.Parts {
			if !isSafeToFlushCyclic(p) {
				return false
			}
		}
		return true
	default:
		// Conservatively assume anything else (Resolver, embeddings,
		// comprehensions, etc.) may re-enter.
		return false
	}
}

// TODO(evalv3): consider not returning a result at all.
//
//	func (v *Vertex) unify@(c *OpContext, needs condition, mode runMode) bool {
//		return v.unifyC(c, needs, mode, true)
//	}
func (v *Vertex) unify(c *OpContext, flags Flags) bool {
	needs := flags.condition
	mode := flags.mode
	checkTypos := flags.checkTypos

	if c.checkCanceled() {
		// The operation was interrupted: mark the vertex with the
		// cancellation error and report it as done, so that the whole
		// evaluation winds down instead of retrying. The resulting
		// value state is unspecified; the operation as a whole reports
		// the error recorded by [OpContext.Canceled].
		if v.status != finalized {
			v.setValue(c, finalized, c.canceled)
		}
		return true
	}

	if c.LogEval > 0 {
		defer c.Un(c.Indentf(v, "UNIFY(%x, %v)", needs, mode))
	}

	// TODO: investigate whether we still need this mechanism.
	//
	// This has been disabled to fix Issue #3709. This was added in lieu of a
	// proper depth detecting mechanism. This has been implemented now, but
	// we keep this around to investigate certain edge cases, such as
	// depth checking across inline vertices.
	//
	// if c.evalDepth == 0 {
	// 	defer func() {
	// 		// This loop processes nodes that need to be evaluated, but should be
	// 		// evaluated outside of the stack to avoid structural cycle detection.
	// 		// See comment at toFinalize.
	// 		a := c.toFinalize
	// 		c.toFinalize = c.toFinalize[:0]
	// 		for _, x := range a {
	// 			x.Finalize(c)
	// 		}
	// 	}()
	// }

	if mode == ignore {
		return false
	}

	if n := v.state; n != nil && n.ctx.opID != c.opID {
		// TODO: we could clear the closedness information.
		// v.state = nil
		// v.status = finalized
		// for _, c := range v.Conjuncts {
		// 	c.CloseInfo.defID = 0
		// 	c.CloseInfo.enclosingEmbed = 0
		// 	c.CloseInfo.outerID = 0
		// }
		c.stats.GenerationMismatch++

		// A different OpContext is finalizing this vertex (e.g.
		// json.Marshal re-entering via [value.Make] mid-evaluation).
		// Arc conjunctInfo defIDs belong to the original context, so
		// the typo check would falsely reject sanctioned fields here.
		checkTypos = false
	}

	// Note that the state of a node can be removed before the node is.
	// This happens with the close builtin, for instance.
	// See TestFromAPI in pkg export.
	// TODO(evalv3): find something more principled.
	n := v.getState(c)
	if n == nil {
		return true // already completed
	}

	n.retainProcess()
	defer func() {
		n.releaseProcess()
		if v.state != nil && v.status == finalized {
			n.ctx.reclaimTempBuffers(v)
		}
	}()

	// TODO(perf): reintroduce freeing once we have the lifetime under control.
	// Right now this is not managed anyway, so we prevent bugs by disabling it.
	// defer n.free()

	// Typically a node processes all conjuncts before processing its fields.
	// So this condition is very likely to trigger. If for some reason the
	// parent has not been processed yet, we could attempt to process more
	// of its tasks to increase the chances of being able to find the
	// information we are looking for here. For now we just continue as is.
	//
	// For dynamic nodes, the parent only exists to provide a path context.
	//
	// Note that if mode is final, we will guarantee that the conditions for
	// this if clause are met down the line. So we assume this is already the
	// case and set the signal accordingly if so.
	if !v.Rooted() || v.Parent.allChildConjunctsKnown(c) || mode == finalize {
		n.signal(allAncestorsProcessed)
	}

	nodeOnlyNeeds := needs &^ (subFieldsProcessed)

	if v.BaseValue == nil {
		v.BaseValue = cycle
	}
	n.updateScalar()
	if nodeOnlyNeeds == (scalarKnown|arcTypeKnown) && n.meets(nodeOnlyNeeds) {
		return true
	}

	// Detect a self-reference: if this node is under evaluation at the same
	// evaluation depth, this means that we have a self-reference, possibly
	// through an expression. As long as there is no request to process arcs or
	// finalize the value, we can and should stop processing here to avoid
	// spurious cycles.

	if v.status == evaluating && v.state.evalDepth == c.evalDepth {
		switch mode {
		case finalize:
			// We will force completion below.
		case yield:
			// TODO: perhaps add to queue in some condition.
		default:
			if needs&fieldSetKnown == 0 {
				return false
			}
		}
	}

	v.status = evaluating

	defer n.unmarkDepth(n.markDepth())

	// Recover from over-conservative CallExpr deferment that would otherwise
	// leave the node's value unresolved.
	n.flushDeferredCyclicConjuncts()

	n.process(nodeOnlyNeeds, mode)

	if n.node.ArcType != ArcPending &&
		n.meets(allAncestorsProcessed) &&
		allTasksFinished(n) {
		n.signal(arcTypeKnown)
	}

	defer c.PopArc(c.PushArc(v))

	w := v.DerefDisjunct()
	if w != v {
		// Should resolve with dereference.
		v.ClosedRecursive = w.ClosedRecursive
		v.status = w.status
		v.ChildErrors = CombineErrors(nil, v.ChildErrors, w.ChildErrors)
		v.clearArcs(c)
		if w.status == finalized {
			return true
		}
		return w.state.meets(needs)
	}
	n.updateScalar()

	// First process all but the subfields.
	switch {
	case n.meets(nodeOnlyNeeds):
		// pass through next phase.
	case mode != finalize:
		// TODO: disjunctions may benefit from evaluation as much prematurely
		// as possible, as this increases the chances of premature failure.
		// We should consider doing a recursive "attemptOnly" evaluation here.
		return false
	}

	if n.isShared {
		if isCyclePlaceholder(n.origBaseValue) {
			n.origBaseValue = nil
		}
	} else if isCyclePlaceholder(n.node.BaseValue) {
		n.node.BaseValue = nil
	}
	if !n.isShared {
		// TODO(sharewithval): allow structure sharing if we only have validator
		// and references.
		// TODO: rewrite to use mode when we get rid of old evaluator.
		state := finalized
		n.validateValue(state)
	}

	if v, ok := n.node.BaseValue.(*Vertex); ok && n.shareCycleType == NoCycle {
		if n.ctx.hasDepthCycle(v) {
			n.reportCycleError()
			return true
		}
		// We unify here to proactively detect cycles. We do not need to,
		// nor should we, if have have already found one.
		v.unify(n.ctx, Flags{condition: needs, mode: mode, checkTypos: checkTypos})
	}

	// At this point, no more conjuncts will be added, so we could decrement
	// the notification counters.

	switch {
	case n.completed&subFieldsProcessed != 0:
		// done

	case needs&subFieldsProcessed != 0:
		switch {
		case assertStructuralCycle(n):

		case n.node.status == finalized:
			// There is no need to recursively process if the node is already
			// finalized. This can happen if there was an error, for instance.
			// This may drop a structural cycle error, but as long as the node
			// already is erroneous, that is fine. It is probably possible to
			// skip more processing if the node is already finalized.

		// TODO: consider bailing on error if n.errs != nil.
		// At the very least, no longer propagate typo errors if this node
		// is erroneous.
		case n.kind == BottomKind:
		case n.completeAllArcs(needs, mode, checkTypos):
		}

		if mode == finalize {
			n.signal(subFieldsProcessed)
		}

		if v := n.node.Value(); v != nil && IsConcrete(v) {
			// Ensure that checks are not run again when this value is used
			// in a validator.
			checks := n.checks
			n.checks = n.checks[:0]
			for _, v := range checks {
				// TODO(errors): make Validate return bottom and generate
				// optimized conflict message. Also track and inject IDs
				// to determine origin location.s
				if b := c.Validate(v, n.node); b != nil {
					n.addBottom(b)
				}
			}
		}

	// TODO(pushdown): remove
	case needs&fieldSetKnown != 0:
		n.evalArcTypes(mode)
	}

	if err := n.getErr(); err != nil {
		n.errs = nil
		if b := n.node.Bottom(); b != nil {
			err = CombineErrors(nil, b, err)
		}
		n.setBaseValue(err)
	}

	n.finalizeDisjunctions()

	if mode == attemptOnly {
		return n.meets(needs)
	}

	if mask := n.completed & needs; mask != 0 {
		// TODO: phase3: validation
		n.signal(mask)
	}

	w = v.DerefValue() // Dereference anything, including shared nodes.
	if w != v {
		// Clear value fields that are now referred to in the dereferenced
		// value (w).
		v.clearArcs(c)
		v.ChildErrors = nil

		if n.completed&(subFieldsProcessed) == 0 {
			// Ensure the shared node is processed to the requested level. This is
			// typically needed for scalar values.
			if w.status == unprocessed {
				w.unify(c, Flags{condition: needs, mode: mode, checkTypos: false})
			}

			return n.meets(needs)
		}

		// Set control fields that are referenced without dereferencing.
		if w.ClosedRecursive {
			v.ClosedRecursive = true
		}
		// NOTE: setting ClosedNonRecursive is not necessary, as it is
		// handled by scheduleValue.
		if w.HasEllipsis {
			v.HasEllipsis = true
		}

		v.status = w.status

		n.finalizeSharing()

		// TODO: find a more principled way to catch this cycle and avoid this
		// check.
		if n.hasAncestor(w) {
			n.reportCycleError()
			return true
		}

		// Report the cycle on n (the sharing vertex, typically a
		// disjunct's view of w) so the failure is scoped to this
		// evaluation context rather than polluting w with a structural
		// cycle. See sharedTargetHasInProgressCycle for the trigger.
		if sharedTargetHasInProgressCycle(c, w) {
			n.reportCycleError()
			return true
		}

		// Ensure that shared nodes comply to the same requirements as we
		// need for the current node.
		w.unify(c, Flags{condition: needs, mode: mode, checkTypos: checkTypos})

		return true
	}

	if n.completed&(subFieldsProcessed) == 0 {
		return n.meets(needs)
	}

	// TODO: adding this is wrong, but it should not cause the snippet below
	// to hang. Investigate.
	// v.Closed = v.cc.isClosed
	//
	// This hangs:
	// issue1940: {
	// 	#T: ["a", #T] | ["c", #T] | ["d", [...#T]]
	// 	#A: t: #T
	// 	#B: x: #A
	// 	#C: #B
	// 	#C: x: #A
	// }

	// validationCompleted
	// The next piece of code used to address the following case
	// (order matters)
	//
	// 		c1: c: [string]: f2
	// 		f2: c1
	// 		Also: cycle/issue990
	//
	// However, with recent changes, it no longer matters. Simultaneously,
	// this causes a hang in the following case:
	//
	// 		_self: x: [...and(x)]
	// 		_self
	// 		x: [1]
	//
	// For this reason we disable it now. It may be the case that we need
	// to enable it for computing disjunctions.
	//
	n.incDepth()
	defer n.decDepth()

	// TODO: find more strategic place to set ClosedRecursive and get rid
	// of helper fields.
	blockClose := n.hasTop
	if n.hasStruct {
		blockClose = false
	}
	if n.isDef && !blockClose {
		n.node.ClosedRecursive = true
	}

	if checkTypos {
		n.checkTypos()
	}

	// After this we no longer need the defIDs of the conjuncts. By clearing
	// them we ensure that we do not have rogue index values into the
	// [OpContext.containments].
	// for i := range n.node.Conjuncts {
	// 	// Consider if this is necessary now we have generations.
	// 	c := &n.node.Conjuncts[i]
	// 	c.CloseInfo.defID = 0
	// 	c.CloseInfo.enclosingEmbed = 0
	// 	c.CloseInfo.outerID = 0
	// }

	v.updateStatus(finalized)

	return n.meets(needs)
}

// completeNodeTasks advances the scheduler for this node, processing any
// outstanding tasks up to the given mode. It is called after a task completes
// when toComplete was set, indicating the node had an in-progress scheduler
// that needs further processing. The isCompleting guard prevents re-entrant
// calls.
func (n *nodeContext) completeNodeTasks(mode runMode) {
	if n.ctx.LogEval > 0 {
		defer n.ctx.Un(n.ctx.Indentf(n.node, "(%v)", mode))
	}

	// TODO(pushdown): can be removed remove
	// In attemptOnly mode, don't assert initialization to allow processing
	// of partially initialized vertices
	if mode != attemptOnly {
		n.assertInitialized()
	} else if n.node != nil && !n.node.isInitialized() {
		// In attemptOnly mode, skip processing if vertex is not initialized
		return
	}

	// TODO(pushdown): okay to remove, but results in different default
	// behavior. Verify.
	if n.isCompleting > 0 {
		return
	}
	n.isCompleting++
	defer func() {
		n.isCompleting--
	}()

	// Needed to not have nil pointer exceptions in some builtin calls.
	v := n.node
	if v.IsDynamic || v.Label.IsLet() || v.Parent.allChildConjunctsKnown(n.ctx) {
		n.signal(allAncestorsProcessed)
	}

	if !allTasksStarted(n) {
		const needs = valueKnown | fieldConjunctsKnown

		n.process(needs, mode)
		n.updateScalar()
	}
}

func (n *nodeContext) updateScalar() {
	// Set BaseValue to scalar, but only if it was not set before. Most notably,
	// errors should not be discarded.
	if n.scalar != nil && (!n.node.IsErr() || isCyclePlaceholder(n.node.BaseValue)) {
		if v, ok := n.node.BaseValue.(*Vertex); !ok || !v.IsDisjunct {
			n.setBaseValue(n.scalar)
		}
		n.signal(scalarKnown)
	}
}

func (n *nodeContext) completeAllArcs(needs condition, mode runMode, checkTypos bool) bool {
	if n.underlying != nil {
		// References within the disjunct may end up referencing the layer that
		// this node overlays. Also for these nodes we want to be able to detect
		// structural cycles early. For this reason, we also set the
		// evaluatingArcs status in the underlying layer.
		//
		// TODO: for now, this seems not necessary. Moreover, this will cause
		// benchmarks/cycle to display a spurious structural cycle. But it
		// shortens some of the structural cycle depths. So consider using this.
		//
		// status := n.underlying.status
		// n.underlying.updateStatus(evaluatingArcs) defer func() {
		// n.underlying.status = status }()
	}

	// Ensure remaining tasks (e.g., comprehensions that add arcs) run before
	// visiting arcs. Without this, arcs from comprehensions may be missing
	// when completeAllArcs iterates over them.
	n.completeNodeTasks(finalize)

	n.incDepth()
	defer n.decDepth()

	// TODO: do something more principled here.s
	if n.hasDisjunction {
		checkTypos = false
	}

	// XXX(0.7): only set success if needs complete arcs.
	success := true
	// Visit arcs recursively to validate and compute error. Use index instead
	// of range in case the Arcs grows during processing.
	for arcPos := 0; arcPos < len(n.node.Arcs); arcPos++ {
		a := n.node.Arcs[arcPos]
		// TODO: Consider skipping lets.

		if !a.unify(n.ctx, Flags{condition: needs, mode: mode, checkTypos: checkTypos}) {
			success = false
		}

		// At this point we need to ensure that all notification cycles
		// for Arc a have been processed.

		if a.ArcType == ArcPending {
			// TODO: cancel tasks?
			// TODO: is this ever run? Investigate once new evaluator work is
			// complete.
			a.updateArcType(ArcNotPresent)
			continue
		}

		// TODO: harmonize this error with "cannot combine"
		switch {
		case a.ArcType > ArcRequired, !a.Label.IsString():
		case n.kind&StructKind == 0:
			if !n.node.IsErr() && !a.IsErr() {
				n.reportFieldMismatch(Pos(a.Value()), nil, a.Label, n.node.Value())
			}
			// case !wasVoid:
			// case n.kind == TopKind:
			// 	// Theoretically it may be possible that a "void" arc references
			// 	// this top value where it really should have been a struct. One
			// 	// way to solve this is to have two passes over the arcs, where
			// 	// the first pass additionally analyzes whether comprehensions
			// 	// will yield values and "un-voids" an arc ahead of the rest.
			// 	//
			// 	// At this moment, though, I fail to see a possibility to create
			// 	// faulty CUE using this mechanism, though. At most error
			// 	// messages are a bit unintuitive. This may change once we have
			// 	// functionality to reflect on types.
			// 	if _, ok := n.node.BaseValue.(*Bottom); !ok {
			// 		n.node.BaseValue = &StructMarker{}
			// 		n.kind = StructKind
			// 	}
		}
	}

	n.node.Arcs = slices.DeleteFunc(n.node.Arcs, func(a *Vertex) bool {
		return a.ArcType == ArcNotPresent
	})

	for _, a := range n.node.Arcs {
		// Errors are allowed in let fields. Handle errors and failure to
		// complete accordingly.
		if !a.Label.IsLet() && a.ArcType <= ArcRequired {
			a := a.DerefValue()
			if err := a.Bottom(); err != nil {
				n.AddChildError(err)
			}
			success = true // other arcs are irrelevant
		}
	}

	// TODO: perhaps this code can go once we have builtins for comparing to
	// bottom.
	for _, c := range n.postChecks {
		ctx := n.ctx
		f := ctx.PushState(c.env, c.expr.Source())

		v := ctx.evalState(c.expr, Flags{
			status:    finalized,
			condition: allKnown,
			mode:      ignore,
		})
		v, _ = ctx.getDefault(v)
		v = Unwrap(v)

		switch _, isError := v.(*Bottom); {
		case isError == c.expectError:
		default:
			n.node.AddErr(ctx, &Bottom{
				Src:  c.expr.Source(),
				Code: CycleError,
				Node: n.node,
				Err: ctx.NewPosf(Pos(c.expr),
					"circular dependency in evaluation of conditionals: %v changed after evaluation",
					c.expr),
			})
		}

		ctx.PopState(f)
	}

	// This should be called after all arcs have been processed, because
	// whether sharing is possible or not may depend on how arcs with type
	// ArcPending will resolve.
	n.finalizeSharing()

	// Strip struct literals that were not initialized and are not part
	// of the output.
	//
	// TODO(perf): we could keep track if any such structs exist and only
	// do this removal if there is a change of shrinking the list.
	n.node.Structs = slices.DeleteFunc(n.node.Structs, func(s StructInfo) bool {
		return !s.initialized
	})

	// TODO: This seems to be necessary, but enables structural cycles.
	// Evaluator whether we still need this.
	//
	// pc := n.node.PatternConstraints
	// if pc == nil {
	// 	return success
	// }
	// for _, c := range pc.Pairs {
	// 	c.Constraint.Finalize(n.ctx)
	// }

	return success
}

func (n *nodeContext) evalArcTypes(mode runMode) {
	for _, a := range n.node.Arcs {
		if a.ArcType != ArcPending {
			continue
		}
		a.unify(n.ctx, Flags{condition: arcTypeKnown, mode: mode, checkTypos: false})
		// Ensure the arc is processed up to the desired level
		if a.ArcType == ArcPending {
			// TODO: cancel tasks?
			a.updateArcType(ArcNotPresent)
		}
	}
}

func root(v *Vertex) *Vertex {
	for v.Parent != nil {
		v = v.Parent
	}
	return v
}

func (v *Vertex) lookup(c *OpContext, pos token.Pos, f Feature, flags Flags) *Vertex {
	needs := flags.condition
	runMode := flags.mode

	v = v.DerefValue()

	if c.LogEval > 0 {
		c.Logf(c.vertex, "LOOKUP %v", f)
	}

	state := v.getState(c)
	if state != nil {
		// If the scheduler associated with this vertex was already running,
		// it means we have encountered a cycle. In that case, we allow to
		// proceed with partial data, in which case a "pending" arc will be
		// created to be completed later.

		// Propagate error if the error is from a different package. This
		// compensates for the fact that we do not fully evaluate the package.
		if state.hasErr() {
			err := state.getErr()
			if err != nil && err.Node != nil && root(err.Node) != root(v) {
				c.AddBottom(err)
			}
		}

		// A lookup counts as new structure. See the commend in Section
		// "Lookups in inline cycles" in cycle.go.
		// TODO: this seems no longer necessary and setting this will cause some
		// hangs. Investigate.
		// state.hasNonCycle = true

		v := state.node
		if v.IsDynamic || v.Label.IsLet() || v.Parent.allChildConjunctsKnown(c) {
			state.signal(allAncestorsProcessed)
		}

		// Drive the lookup target forward when its scheduler has not yet
		// started everything; the !allTasksStarted guard keeps us out of
		// nodes that are already mid-execution.
		if !allTasksStarted(state) {
			state.process(valueKnown|fieldConjunctsKnown|allTasksCompleted, attemptOnly)
			state.updateScalar()
		}
	}

	// TODO: verify lookup types.

	// Re-deref v: state.process may have caused v to share with another
	// vertex (e.g. via scheduleVertexConjuncts in a resolver task), so
	// further lookup must follow the new BaseValue.
	if v2 := v.DerefValue(); v2 != v {
		v = v2
		// TODO: might result in better error message if not set.
		state = v.getState(c)
	}

	arc := v.LookupRaw(f)
	// We leave further dereferencing to the caller, but we do dereference for
	// the remainder of this function to be able to check the status.
	arcReturn := arc
	if arc != nil {
		arc = arc.DerefNonRooted()
		// TODO(perf): NonRooted is the minimum, but consider doing more.
		// arc = arc.DerefValue()
	}

	// TODO: clean up this logic:
	// - signal arcTypeKnown when ArcMember or ArcNotPresent is set,
	//   similarly to scalarKnown.
	// - make it clear we want to yield if it is now known if a field exists.

	var arcState *nodeContext
	switch {
	case arc != nil:
		if arc.ArcType == ArcMember {
			return arcReturn
		}
		arcState = arc.getState(c)

	case state == nil || state.meets(needTasksDone):
		// This arc cannot exist.
		v.reportFieldIndexError(c, pos, f)
		return nil

	default:
		// If the vertex is known to be closed and does not accept the field,
		// the field can never exist. Report a hard error immediately rather
		// than creating a phantom pending arc, which would only produce an
		// incomplete error later. This handles the case of a lookup in a
		// closed struct (e.g., via the close() builtin) where the struct is
		// already known to be closed but its ancestor evaluation is still
		// pending.
		if !v.IsOpenStruct() && !v.Accept(c, f) {
			v.reportFieldIndexError(c, pos, f)
			return nil
		}
		arc = &Vertex{Parent: state.node, Label: f, ArcType: ArcPending}
		if runMode != finalize && runMode != ignore {
			v.Arcs = append(v.Arcs, arc)
		}
		arcState = arc.getState(c) // TODO: consider using getBareState.
	}

	if arcState != nil && (!arcState.meets(needTasksDone) || !arcState.meets(arcTypeKnown)) {
		needs |= arcTypeKnown

		switch runMode {
		case ignore, attemptOnly:
			// TODO(cycle): ideally, we should be able to require that the
			// arcType be known at this point, but that does not seem to work.
			// Revisit once we have the structural cycle detection in place.

			if arc.ArcType == ArcPending {
				// In ignore mode (used for comprehension clause evaluation),
				// always return the pending arc optimistically to avoid
				// prematurely blocking comprehension expansion.
				if runMode == ignore {
					return arcReturn
				}

				// In attemptOnly mode, return the pending arc if the container
				// vertex still has work that may produce this field:
				//
				//   - it has an active parent task (e.g., a comprehension that
				//     may dynamically add this field), OR
				//   - it has registered tasks for allTasksCompleted that have
				//     not yet all completed (e.g., a builtin function whose
				//     result may include this field).
				//
				// If neither condition holds, the field can never become a
				// member, so treat it as ArcNotPresent. This ensures lookups
				// of truly absent fields produce an incomplete error rather
				// than returning _.
				if state != nil {
					if state.hasActiveParentTask() {
						c.lookupPendingParent = true
						return arcReturn
					}
					if state.provided&allTasksCompleted != 0 &&
						state.counters[allTasksCompletedIdx] > 0 {
						return arcReturn
					}
				}
				// Check the arc's own parent tasks (e.g.,
				// comprehensions pushed down by pushDownDeps) — they
				// may still produce this field.
				if arcState.hasActiveParentTask() {
					c.lookupPendingParent = true
					return arcReturn
				}
				arc.ArcType = ArcNotPresent
			}

		case yield:
			arcState.process(needs, yield)
			// continue processing, as successful processing may still result
			// in an invalid field.

		case finalize:
			// TODO: we should try to always use finalize? Using it results in
			// errors. For now we only use it for let values. Let values are
			// not normally finalized (they may be cached) and as such might
			// not trigger the usual unblocking. Force unblocking may cause
			// some values to be remain unevaluated.

			// If this arc is pending or optional and a parent task that may
			// create or upgrade it is active, check whether the current task
			// is that parent. If not (i.e. we are a different comprehension
			// depending on a field another comprehension may produce), yield
			// and wait for the arc type to be resolved. This avoids a
			// spurious CycleError: without this, the lookup would return an
			// IncompleteError for an optional arc, causing
			// verifyNonMonotonicResult to add a postCheck expecting the field
			// to remain absent. But the field becomes present once the parent
			// task finishes, triggering the postCheck as a false cycle.
			//
			// This applies to both ArcPending and ArcOptional because:
			// - ArcPending: a comprehension may yet create this arc as a
			//   member.
			// - ArcOptional: a comprehension (in parentTasks) may upgrade the
			//   optional arc to a member arc (e.g. `if raises == _|_ { ret:
			//   a: 1 }` upgrades `ret?: {}` to a regular member).
			if arc.ArcType == ArcPending || arc.ArcType == ArcOptional {
				cur := c.current()
				shouldYield := false
				for _, pt := range arcState.parentTasks {
					if pt.state == taskRUNNING && pt != cur {
						shouldYield = true
						break
					}
				}
				if shouldYield {
					// A parent task is actively running in the current call
					// chain (triggered via processAncestors), but is not our
					// own task. Yield and wait for the arc type to be
					// determined. This prevents a spurious CycleError that
					// would otherwise be stored when arc.unify below sets
					// arc.status = evaluating while the arc's type is unknown.
					arcState.process(arcTypeKnown, yield)
					break
				}
			}

			switch {
			case needs == arcTypeKnown|fieldSetKnown:
				arc.unify(c, Flags{condition: needs, mode: finalize, checkTypos: false})
			default:
				// Now we can't finalize, at least try to get as far as we
				// can and only yield if we really have to.
				needs := needs | arcTypeKnown
				if !arc.unify(c, Flags{condition: needs, mode: attemptOnly, checkTypos: false}) {
					arcState.process(needs, attemptOnly)
				}
			}
			if arc.ArcType == ArcPending {
				// Only mark arc as not present if no parent task is still
				// running. If a parent task (e.g. a comprehension) is still
				// in progress, it may yet add this arc.
				if !arcState.hasActiveParentTask() {
					// updateArcType is the normal path, but for an arc
					// currently at ArcPending it returns early without
					// assigning because ArcNotPresent is "more restrictive"
					// than ArcPending in the enum (see updateArcType). We
					// force the transition here because no parent task is
					// going to materialize this arc.
					arc.ArcType = ArcNotPresent
				}
			}
		}
	}

	switch arc.ArcType {
	case ArcRequired:
		label := f.SelectorString(c.Runtime)
		b := &Bottom{
			Code: IncompleteError,
			Err:  c.NewPosf(pos, "required field missing: %s", label),
			Node: v,
		}
		// TODO: yield failure
		c.AddBottom(b) // TODO: unify error mechanism.
		return arcReturn
	case ArcMember:
		return arcReturn

	case ArcOptional:
		// Technically, this failure also applies to required fields. We assume
		// however, that if a reference field that is made regular will already
		// result in an error, so that piling up another error is not strictly
		// necessary. Note that the spec allows for eliding an error if it is
		// guaranteed another error is generated elsewhere. This does not
		// properly cover the case where a reference is made directly within the
		// definition, but this is fine for the purpose it serves.
		// TODO(refRequired): revisit whether referencing required fields should
		// fail.
		label := f.SelectorString(c.Runtime)
		b := &Bottom{
			Code: IncompleteError,
			Node: v,
			Err: c.NewPosf(pos,
				"cannot reference optional field: %s", label),
		}
		c.AddBottom(b)
		// TODO: yield failure
		return nil

	case ArcNotPresent:
		v.reportFieldIndexError(c, pos, f)
		return nil

	case ArcPending:
		// The arc is still pending after finalization. If a parent task
		// (comprehension) is still active (RUNNING, WAITING) or failed
		// (e.g. due to a cycle error in a mutual comprehension
		// dependency), report as CycleError so that bottom comparisons
		// propagate the cycle rather than treating the field as absent.
		//
		// arcState may be nil here: the if block above only ran when
		// arcState was non-nil, so a finalized arc whose ArcType remained
		// ArcPending (e.g. left over from a sibling traversal) reaches this
		// point without state. With no parent tasks to inspect, fall
		// through to the generic absent-field error.
		if arcState != nil {
			for _, pt := range arcState.parentTasks {
				if pt.state == taskRUNNING || pt.state == taskWAITING || pt.state == taskFAILED {
					label := f.SelectorString(c.Runtime)
					b := &Bottom{
						Code: CycleError,
						Err:  c.NewPosf(pos, "cyclic reference to field %s", label),
						Node: v,
					}
					c.AddBottom(b)
					return nil
				}
			}
		}
		v.reportFieldIndexError(c, pos, f)
		return nil
	}

	v.reportFieldIndexError(c, pos, f)
	return nil
}

// accept reports whether the given feature is allowed by the pattern
// constraints.
func (v *Vertex) accept(ctx *OpContext, f Feature) bool {
	// TODO: this is already handled by callers at the moment, but it may be
	// better design to move this here.
	// if v.LookupRaw(f) != nil {
	// 	return true, true
	// }

	v = v.DerefValue()

	pc := v.PatternConstraints
	if pc == nil {
		return false
	}

	// TODO: parhaps use matchPattern again if we have an allowed.
	if matchPattern(ctx, pc.Allowed, f) {
		return true
	}

	// TODO: fall back for now to just matching any pattern.
	for _, c := range pc.Pairs {
		if matchPattern(ctx, c.Pattern, f) {
			return true
		}
	}

	return false
}
