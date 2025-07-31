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

	"cuelang.org/go/cue/token"
)

// TODO(mpvl): perhaps conjunctsProcessed is a better name for this.
func (v *Vertex) isInitialized() bool {
	return v.status == finalized || (v.state != nil && v.state.isInitialized)
}

func (n *nodeContext) assertInitialized() {
	if n != nil && n.ctx.isDevVersion() {
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

	n.blockOn(scalarKnown | listTypeKnown | arcTypeKnown)

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

// TODO(evalv3): consider not returning a result at all.
//
//	func (v *Vertex) unify@(c *OpContext, needs condition, mode runMode) bool {
//		return v.unifyC(c, needs, mode, true)
//	}
func (v *Vertex) unify(c *OpContext, needs condition, mode runMode, checkTypos bool) bool {
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
	if !v.Rooted() || v.Parent.allChildConjunctsKnown() || mode == finalize {
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

	if n.node.ArcType == ArcPending {
		// forcefully do an early recursive evaluation to decide the state
		// of the arc. See https://cuelang.org/issue/3621.
		n.process(pendingKnown, attemptOnly)
		if n.node.ArcType == ArcPending {
			for _, a := range n.node.Arcs {
				a.unify(c, needs, attemptOnly, checkTypos)
			}
		}
		// TODO(evalv3): do we need this? Error messages are slightly better,
		// but adding leads to Issue #3941.
		// n.completePending(yield)
	}

	n.process(nodeOnlyNeeds, mode)

	if n.node.ArcType != ArcPending &&
		n.meets(allAncestorsProcessed) &&
		len(n.tasks) == n.taskPos {
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

	if n.aStruct != nil {
		n.updateNodeType(StructKind, n.aStruct, n.aStructID)
	}

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
		v.unify(n.ctx, needs, mode, checkTypos)
	}

	// At this point, no more conjuncts will be added, so we could decrement
	// the notification counters.

	switch {
	case n.completed&subFieldsProcessed != 0:
		// done

	case needs&subFieldsProcessed != 0:
		switch {
		case assertStructuralCycleV3(n):

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

		if v.BaseValue == nil {
			// TODO: this seems to not be possible. Possibly remove.
			state := finalized
			v.BaseValue = n.getValidators(state)
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
				w.unify(c, needs, mode, false)
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
		if n.hasAncestorV3(w) {
			n.reportCycleError()
			return true
		}

		// Ensure that shared nodes comply to the same requirements as we
		// need for the current node.
		w.unify(c, needs, mode, checkTypos)

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
	if n.hasOpenValidator {
		blockClose = true
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

// Once returning, all arcs plus conjuncts that can be known are known.
//
// Proof:
//   - if there is a cycle, all completeNodeConjuncts will be called
//     repeatedly for all nodes in this cycle, and all tasks on the cycle
//     will have run at least once.
//   - any tasks that were blocking on values on this circle to be completed
//     will thus have to be completed at some point in time if they can.
//   - any tasks that were blocking on values outside of this ring will have
//     initiated its own execution, which is either not cyclic, and thus
//     completes, or is on a different cycle, in which case it completes as
//     well.
//
// Goal:
// - complete notifications
// - decrement reference counts for root and notify.
// NOT:
// - complete value. That is reserved for Unify.
func (n *nodeContext) completeNodeTasks(mode runMode) {
	if n.ctx.LogEval > 0 {
		defer n.ctx.Un(n.ctx.Indentf(n.node, "(%v)", mode))
	}

	n.assertInitialized()

	if n.isCompleting > 0 {
		return
	}
	n.isCompleting++
	defer func() {
		n.isCompleting--
	}()

	v := n.node

	if !v.Label.IsLet() {
		if p := v.Parent; p != nil && p.state != nil {
			if !v.IsDynamic && n.completed&allAncestorsProcessed == 0 {
				p.state.completeNodeTasks(mode)
			}
		}
	}

	if v.IsDynamic || v.Label.IsLet() || v.Parent.allChildConjunctsKnown() {
		n.signal(allAncestorsProcessed)
	}

	if len(n.scheduler.tasks) != n.scheduler.taskPos {
		// TODO: do we need any more requirements here?
		const needs = valueKnown | fieldConjunctsKnown

		n.process(needs, mode)
		n.updateScalar()
	}

	// As long as ancestors are not processed, it is still possible for
	// conjuncts to be inserted. Until that time, it is not okay to decrement
	// theroot. It is not necessary to wait on tasks to complete, though,
	// as pending tasks will have their own dependencies on root, meaning it
	// is safe to decrement here.
	if !n.meets(allAncestorsProcessed) && !n.node.Label.IsLet() && mode != finalize {
		return
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

	// TODO: this should only be done if n is not currently running tasks.
	// Investigate how to work around this.
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

		if !a.unify(n.ctx, needs, mode, checkTypos) {
			success = false
		}

		// At this point we need to ensure that all notification cycles
		// for Arc a have been processed.

		if a.ArcType == ArcPending {
			// TODO: cancel tasks?
			// TODO: is this ever run? Investigate once new evaluator work is
			// complete.
			a.ArcType = ArcNotPresent
			continue
		}

		// TODO: harmonize this error with "cannot combine"
		switch {
		case a.ArcType > ArcRequired, !a.Label.IsString():
		case n.kind&StructKind == 0:
			if !n.node.IsErr() && !a.IsErr() {
				n.reportFieldMismatch(pos(a.Value()), nil, a.Label, n.node.Value())
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

	k := 0
	for _, a := range n.node.Arcs {
		if a.ArcType != ArcNotPresent {
			n.node.Arcs[k] = a
			k++
		}
	}
	n.node.Arcs = n.node.Arcs[:k]

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

		v := ctx.evalState(c.expr, combinedFlags{
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
				Err: ctx.NewPosf(pos(c.expr),
					"circular dependency in evaluation of conditionals: %v changed after evaluation",
					ctx.Str(c.expr)),
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
	k = 0
	for _, s := range n.node.Structs {
		if s.initialized {
			n.node.Structs[k] = s
			k++
		}
	}
	n.node.Structs = n.node.Structs[:k]

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

// completePending determines if n is pending. In order to do so, it must
// recursively find any descendents with unresolved comprehensions. Note that
// it is currently possible for arcs with unresolved comprehensions to not be
// marked as pending. Consider this example (from issue 3708):
//
//	out: people.bob.kind
//	people: [string]: {
//		kind:  "person"
//		name?: string
//	}
//	if true {
//		people: bob: name: "Bob"
//	}
//
// In this case, the pattern constraint inserts fields into 'bob', which then
// marks 'name' as not pending. However, for 'people' to become non-pending,
// the comprehension associated with field 'name' still needs to be evaluated.
//
// For this reason, this method does not check whether 'n' is pending.
//
// TODO(evalv4): consider making pending not an arc state, but rather a
// separate mode. This will allow us to descend with more precision to only
// visit arcs that still need to be resolved.
func (n *nodeContext) completePending(mode runMode) {
	for _, a := range n.node.Arcs {
		state := a.getState(n.ctx)
		if state != nil {
			state.completePending(mode)
		}
	}
	n.process(pendingKnown, mode)
}

func (n *nodeContext) evalArcTypes(mode runMode) {
	for _, a := range n.node.Arcs {
		if a.ArcType != ArcPending {
			continue
		}
		a.unify(n.ctx, arcTypeKnown, mode, false)
		// Ensure the arc is processed up to the desired level
		if a.ArcType == ArcPending {
			// TODO: cancel tasks?
			a.ArcType = ArcNotPresent
		}
	}
}

func root(v *Vertex) *Vertex {
	for v.Parent != nil {
		v = v.Parent
	}
	return v
}

func (v *Vertex) lookup(c *OpContext, pos token.Pos, f Feature, flags combinedFlags) *Vertex {
	task := c.current()
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

		// TODO: ideally this should not be run at this point. Consider under
		// which circumstances this is still necessary, and at least ensure
		// this will not be run if node v currently has a running task.
		state.completeNodeTasks(attemptOnly)
	}

	// TODO: remove because unnecessary?
	if task != nil && task.state != taskRUNNING {
		return nil // abort, task is blocked or terminated in a cycle.
	}

	// TODO: verify lookup types.

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
		arc = &Vertex{Parent: state.node, Label: f, ArcType: ArcPending}
		v.Arcs = append(v.Arcs, arc)
		arcState = arc.getState(c) // TODO: consider using getBareState.
	}

	if arcState != nil && (!arcState.meets(needTasksDone) || !arcState.meets(arcTypeKnown)) {
		arcState.completePending(attemptOnly)

		arcState.completeNodeTasks(yield)

		needs |= arcTypeKnown

		switch runMode {
		case ignore, attemptOnly:
			// TODO(cycle): ideally, we should be able to require that the
			// arcType be known at this point, but that does not seem to work.
			// Revisit once we have the structural cycle detection in place.

			// TODO: should we avoid notifying ArcPending vertices here?
			if task != nil {
				arcState.addNotify2(task.node.node, task.id)
			}
			if arc.ArcType == ArcPending {
				return arcReturn
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
			switch {
			case needs == arcTypeKnown|fieldSetKnown:
				arc.unify(c, needs, finalize, false)
			default:
				// Now we can't finalize, at least try to get as far as we
				// can and only yield if we really have to.
				if !arc.unify(c, needs, attemptOnly, false) {
					arcState.process(needs, yield)
				}
			}
			if arc.ArcType == ArcPending {
				arc.ArcType = ArcNotPresent
			}
		}
	}

	switch arc.ArcType {
	case ArcMember, ArcRequired:
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
		// should not happen.
		panic("unreachable")
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
