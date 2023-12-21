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

func (v *Vertex) getState(c *OpContext) *nodeContext {
	if v.status == finalized { // TODO: use BaseValue != nil
		return nil
	}
	if v.state == nil {
		v.state = c.newNodeContext(v)
		v.state.initNode()
		v.state.refCount = 1
	}

	// An additional refCount for the current user.
	v.state.refCount += 1

	// TODO: see if we can get rid of ref counting after new evaluator is done:
	// the recursive nature of the new evaluator should make this unnecessary.

	return v.state
}

// initNode initializes a nodeContext for the evaluation of the given Vertex.
func (n *nodeContext) initNode() {
	v := n.node
	if v.Parent != nil && v.Parent.state != nil {
		v.state.depth = v.Parent.state.depth + 1
		n.blockOn(allAncestorsProcessed)
	}

	n.blockOn(scalarKnown | listTypeKnown | arcTypeKnown)

	if v.Label.IsDef() {
		v.Closed = true
	}

	if v.Parent != nil {
		if v.Parent.Closed {
			v.Closed = true
		}
	}

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

	root := n.node.rootCloseContext()
	root.incDependent(INIT, nil) // decremented below

	for _, c := range v.Conjuncts {
		ci := c.CloseInfo
		ci.cc = root
		n.scheduleConjunct(c, ci)
	}

	root.decDependent(ctx, INIT, nil)
}

func (v *Vertex) unify(c *OpContext, needs condition, mode runMode) bool {
	if Debug {
		c.nest++
		c.Logf(v, "Unify %v", fmt.Sprintf("%p", v))
		defer func() {
			c.Logf(v, "END Unify")
			c.nest--
		}()
	}

	if mode == ignore {
		return false
	}

	n := v.getState(c)
	if n == nil {
		return true // already completed
	}
	defer n.free()

	// Typically a node processes all conjuncts before processing its fields.
	// So this condition is very likely to trigger. If for some reason the
	// parent has not been processed yet, we could attempt to process more
	// of its tasks to increase the chances of being able to find the
	// information we are looking for here. For now we just continue as is,
	// though.
	// For dynamic nodes, the parent only exists to provide a path context.
	if v.Label.IsLet() || v.IsDynamic || v.Parent.allChildConjunctsKnown() {
		n.signal(allAncestorsProcessed)
	}

	defer c.PopArc(c.PushArc(v))

	nodeOnlyNeeds := needs &^ (subFieldsProcessed)
	n.process(nodeOnlyNeeds, mode)
	n.updateScalar()

	// First process all but the subfields.
	switch {
	case n.meets(nodeOnlyNeeds):
		// pass through next phase.
	case mode != finalize:
		return false
	}

	if isCyclePlaceholder(n.node.BaseValue) {
		n.node.BaseValue = nil
	}
	if n.aStruct != nil {
		n.updateNodeType(StructKind, n.aStruct, n.aStructID)
	}

	n.validateValue(finalized)

	if err, ok := n.node.BaseValue.(*Bottom); ok {
		for _, arc := range n.node.Arcs {
			if arc.Label.IsLet() {
				continue
			}
			c := MakeConjunct(nil, err, c.CloseInfo())
			if arc.state != nil {
				arc.state.scheduleConjunct(c, c.CloseInfo)
			}
		}
	}

	if n.node.Label.IsLet() || n.meets(allAncestorsProcessed) {
		if cc := v.rootCloseContext(); !cc.isDecremented { // TODO: use v.cc
			cc.decDependent(c, ROOT, nil) // REF(decrement:nodeDone)
			cc.isDecremented = true
		}
	}

	// At this point, no more conjuncts will be added, so we could decrement
	// the notification counters.

	switch {
	case n.completed&subFieldsProcessed != 0:
		// done

	case needs&subFieldsProcessed != 0:
		if DebugSort > 0 {
			DebugSortArcs(n.ctx, n.node)
		}

		switch {
		case assertStructuralCycle(n):
		case n.completeAllArcs(needs, mode):
		}

		n.signal(subFieldsProcessed)

		if v.BaseValue == nil {
			v.BaseValue = n.getValidators(finalized)
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
		n.evalArcTypes()
	}

	if err := n.getErr(); err != nil {
		n.errs = nil
		if b, _ := n.node.BaseValue.(*Bottom); b != nil {
			err = CombineErrors(nil, b, err)
		}
		n.node.BaseValue = err
	}

	if mask := n.completed & needs; mask != 0 {
		// TODO: phase3: validation
		n.signal(mask)
	}

	// validationCompleted
	if n.completed&(subFieldsProcessed) != 0 {
		n.node.updateStatus(finalized)

		for _, r := range n.node.cc.externalDeps {
			src := r.src
			a := &src.arcs[r.index]
			if a.decremented {
				continue
			}
			a.decremented = true
			if n := src.src.getState(n.ctx); n != nil {
				n.completeNodeConjuncts()
			}
			src.src.unify(n.ctx, needTasksDone, attemptOnly)
			a.cc.decDependent(c, a.kind, src) // REF(arcs)
		}

		if DebugDeps {
			RecordDebugGraph(n.ctx, n.node, "Finalize")
		}
	}

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
func (n *nodeContext) completeNodeConjuncts() {
	const conjunctsKnown = fieldConjunctsKnown | valueKnown // | fieldSetKnown

	if n.meets(conjunctsKnown) {
		return
	}

	if p := n.node.Parent; p != nil && p.state != nil {
		p.state.completeNodeConjuncts()
	}

	// This only attempts, but it ensures that all references are processed.
	n.process(conjunctsKnown, attemptOnly)
}

// Goal:
// - complete notifications
// - decrement reference counts for root and notify.
// NOT:
// - complete value. That is reserved for Unify.
func (n *nodeContext) completeNodeTasks() (ok bool) {
	v := n.node
	c := n.ctx

	if Debug {
		c.nest++
		defer func() {
			c.nest--
		}()
	}

	if p := v.Parent; p != nil && p.state != nil {
		if !v.IsDynamic && n.completed&allAncestorsProcessed == 0 {
			p.state.completeNodeTasks()
		}
	}

	if v.IsDynamic || v.Parent.allChildConjunctsKnown() {
		n.signal(allAncestorsProcessed)
	}

	if len(n.scheduler.tasks) != n.scheduler.taskPos {
		// TODO: do we need any more requirements here?
		const needs = valueKnown | fieldConjunctsKnown

		n.process(needs, attemptOnly)
		n.updateScalar()
	}

	// As long as ancestors are not processed, it is still possible for
	// conjuncts to be inserted. Until that time, it is not okay to decrement
	// theroot. It is not necessary to wait on tasks to complete, though,
	// as pending tasks will have their own dependencies on root, meaning it
	// is safe to decrement here.
	if !n.meets(allAncestorsProcessed) && !n.node.Label.IsLet() {
		return false
	}

	// At this point, no more conjuncts will be added, so we could decrement
	// the notification counters.

	if cc := v.rootCloseContext(); !cc.isDecremented { // TODO: use v.cc
		cc.isDecremented = true

		cc.decDependent(n.ctx, ROOT, nil) // REF(decrement:nodeDone)
	}

	return true
}

func (n *nodeContext) updateScalar() {
	// Set BaseValue to scalar, but only if it was not set before. Most notably,
	// errors should not be discarded.
	_, isErr := n.node.BaseValue.(*Bottom)
	if n.scalar != nil && (!isErr || isCyclePlaceholder(n.node.BaseValue)) {
		n.node.BaseValue = n.scalar
		n.signal(scalarKnown)
	}
}

func (n *nodeContext) completeAllArcs(needs condition, mode runMode) bool {
	if n.node.status == evaluatingArcs {
		// NOTE: this was an "incomplete" error pre v0.6. If this is a problem
		// we could make this a CycleError. Technically, this may be correct,
		// as it is possible to make the values exactly as the inserted
		// values. It seems more user friendly to just disallow this, though.
		// TODO: make uniform error messages
		// see compbottom2.cue:
		n.ctx.addErrf(CycleError, pos(n.node), "mutual dependency")
	}

	n.node.updateStatus(evaluatingArcs)

	// XXX(0.7): only set success if needs complete arcs.
	success := true
	// Visit arcs recursively to validate and compute error.
	for n.arcPos < len(n.node.Arcs) {
		a := n.node.Arcs[n.arcPos]
		n.arcPos++

		if !a.unify(n.ctx, needs, finalize) {
			success = false
		}

		// At this point we need to ensure that all notification cycles
		// for Arc a have been processed.

		if a.ArcType == ArcPending {
			// TODO: cancel tasks?
			a.ArcType = ArcNotPresent
			continue
		}

		// Errors are allowed in let fields. Handle errors and failure to
		// complete accordingly.
		if !a.Label.IsLet() && a.ArcType <= ArcRequired {
			if err, _ := a.BaseValue.(*Bottom); err != nil {
				n.node.AddChildError(err)
			}
			success = true // other arcs are irrelevant
		}

		// TODO: harmonize this error with "cannot combine"
		switch {
		case a.ArcType > ArcRequired, !a.Label.IsString():
		case n.kind&StructKind == 0:
			if !n.node.IsErr() {
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

	return success
}

func (n *nodeContext) evalArcTypes() {
	for _, a := range n.node.Arcs {
		if a.ArcType != ArcPending {
			continue
		}
		a.unify(n.ctx, arcTypeKnown, yield)
		// Ensure the arc is processed up to the desired level
		if a.ArcType == ArcPending {
			// TODO: cancel tasks?
			a.ArcType = ArcNotPresent
		}
	}
}

func (v *Vertex) lookup(c *OpContext, pos token.Pos, f Feature, flags combinedFlags) *Vertex {
	task := c.current()
	needs := flags.conditions()
	runMode := flags.runMode()

	c.Logf(c.vertex, "LOOKUP %v", f)

	state := v.getState(c)
	if state != nil {
		// If the scheduler associated with this vertex was already running,
		// it means we have encountered a cycle. In that case, we allow to
		// proceed with partial data, in which case a "pending" arc will be
		// created to be completed later.

		// Report error for now.
		if state.hasErr() {
			c.AddBottom(state.getErr())
		}
		state.completeNodeTasks()
	}

	// TODO: remove because unnecessary?
	if task.state != taskRUNNING {
		return nil // abort, task is blocked or terminated in a cycle.
	}

	// TODO: verify lookup types.

	arc := v.Lookup(f)
	// TODO: clean up this logic:
	// - signal arcTypeKnown when ArcMember or ArcNotPresent is set,
	//   similarly to scalarKnown.
	// - make it clear we want to yield if it is now known if a field exists.

	var arcState *nodeContext
	switch {
	case arc != nil:
		if arc.ArcType == ArcMember {
			return arc
		}
		arcState = arc.getState(c)

	case state == nil || state.meets(needTasksDone):
		// This arc cannot exist.
		v.reportFieldIndexError(c, pos, f)
		return nil

	default:
		arc = &Vertex{Parent: state.node, Label: f, ArcType: ArcPending}
		v.Arcs = append(v.Arcs, arc)
		arcState = arc.getState(c)
	}

	if arcState != nil && (!arcState.meets(needTasksDone) || !arcState.meets(arcTypeKnown)) {
		needs |= arcTypeKnown
		// If this arc is not ArcMember, which it is not at this point,
		// any pending arcs could influence the field set.
		for _, a := range arc.Arcs {
			if a.ArcType == ArcPending {
				needs |= fieldSetKnown
				break
			}
		}
		arcState.completeNodeTasks()

		// Child nodes, if pending and derived from a comprehension, may
		// still cause this arc to become not pending.
		if arc.ArcType != ArcMember {
			for _, a := range arcState.node.Arcs {
				if a.ArcType == ArcPending {
					a.unify(c, arcTypeKnown, runMode)
				}
			}
		}

		switch runMode {
		case ignore, attemptOnly:
			// TODO: should we avoid notifying ArcPending vertices here?
			arcState.addNotify2(task.node.node, task.id)
			return arc

		case yield:
			arcState.process(needs, yield)
			// continue processing, as successful processing may still result
			// in an invalid field.

		case finalize:
			// TODO: should we try to use finalize? Using it results in errors and this works. It would be more principled, though.
			arcState.process(needs, yield)
		}
	}

	switch arc.ArcType {
	case ArcMember:
		return arc

	case ArcOptional, ArcRequired:
		label := f.SelectorString(c.Runtime)
		b := &Bottom{
			Code: IncompleteError,
			Err: c.NewPosf(pos,
				"cannot reference optional field: %s", label),
		}
		c.AddBottom(b)
		// TODO: yield failure
		return nil

	case ArcNotPresent:
		v.reportFieldCycleError(c, pos, f)
		return nil

	case ArcPending:
		// should not happen.
		panic("unreachable")
	}

	v.reportFieldIndexError(c, pos, f)
	return nil
}
