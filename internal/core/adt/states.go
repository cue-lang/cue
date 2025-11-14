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

// TODO: clean up following notes:

// Used in expr.go:
// - ctx.value  (uses: noncrete scalar allowed, concrete scalar, concrete composite)
//   - evalState
// - ctx.node (need to know all fields)
// - ctx.lookup
// - ctx.concrete
//
// - ctx.relLabel
//     OK: always exists
// - ctx.relNode (upcount + unify(partial))
//     OK: node always exists.
//
// - ctx.evalState (in validation of comparison against bottom)
//
// - arc.Finalize (finalized)
// - CompleteArcs (conjuncts)
//

// lookup in p1
//    - process remaining field todos

// lookup:
// if node is currently processing, just look up directly and create
// field with notification.
//
// if node has not been processed, process once.
//
// Any dynamic fields should have been triggered by the existence of a new
// arc. This will either cascade the evaluation or not.

// p1: {
// 	(p1.baz): "bar" // t1
// 	(p1.foo): "baz" // t2
// 	baz: "foo"
// }
//
// <p1, fieldsKnown> -> t1 -> <p1.baz, scalar>
// <p1.foo, scalar> -> t1
// <p1, fieldsKnown> -> t2 -> <p1.foo, scalar>

// p2: {
// 	(p2[p2.baz]): "bar"
// 	(p2.foo): "baz"
// 	baz: "qux"
// 	qux: "foo"
// }

// b -> a - > b: detected cycle in b:
//
//		xxx register expression (a-10) being processed as a post constraint.
//		add task to pending.
//		register value as waiting for scalar to be completed later.
//		return with cycle/ in complete error.
//
//	  - in b:
//	    xxx register expression (b+10) as post constraint.
//	    add task to pending
//	    register value as waiting for scalar to be completed later.
//	    5 is processed and set
//	    this completes the task in b
//	    this sets a scalar in b
//	    this completes the expression in a
//
//	    b: a - 10
//	    a: b + 10
//	    a: 5
//
//	    a: a
//	    a: 5
//

// These are the condition types of the CUE evaluator. A scheduler
// is associated with a single Vertex. So when these states refer to a Vertex,
// it is the Vertex associated with the scheduler.
//
// There are core conditions and condition sets. The core conditions are
// determined during operation as conditions are met. The condition sets are
// used to indicate a set of required or provided conditions.
//
// Core conditions can be signal conditions or counter conditions. A counter
// condition is triggered if all conjuncts that contribute to the computation
// of this condition have been met. A signal condition is triggered as soon as
// evidence is found that this condition is met. Unless otherwise specified,
// conditions are counter conditions.
const (
	// allAncestorsProcessed indicates that all conjuncts that could be added
	// to the Vertex by any of its ancestors have been added. In other words,
	// all ancestors schedulers have reached the state fieldConjunctsKnown.
	//
	// This is a signal condition. It is explicitly set in unify when a
	// parent meets fieldConjunctsKnown|allAncestorsProcessed.
	allAncestorsProcessed condition = 1 << iota

	// Really: all ancestor subfield tasks processed.

	// arcTypeKnown means that the ArcType value of a Vertex is fully
	// determined. The ArcType of all fields of a Vertex need to be known
	// before the complete set of fields of this Vertex can be known.
	arcTypeKnown

	// valueKnown means that it is known what the "type" of the value would be
	// if present.
	valueKnown

	// scalarKnown indicates that a Vertex has either a concrete scalar value or
	// that it is known that it will never have a scalar value.
	//
	// This is a signal condition that is reached when:
	//    - a node is set to a concrete scalar value
	//    - a node is set to an error
	//    - or if ...state is reached.
	//
	// TODO: rename to something better?
	scalarKnown

	// fieldConjunctsKnown means that all the conjuncts of all fields are
	// known.
	fieldConjunctsKnown

	// fieldSetKnown means that all fields of this node are known. This is true
	// if all tasks that can add a field have been processed and if
	// all pending arcs have been resolved.
	fieldSetKnown

	// // allConjunctsKnown means that all conjuncts have been registered as a
	// // task. allParentsProcessed must be true for this to be true.
	// allConjunctsKnown

	// allTasksCompleted means that all tasks of a Vertex have been completed
	// with the exception of validation tasks. A Vertex may still not be
	// finalized.
	allTasksCompleted

	// subFieldsProcessed means that all tasks of a Vertex, including those of
	// its arcs have been completed.
	//
	// This is a signal condition that is met if all arcs have reached the
	// the state finalStateKnown.
	//
	subFieldsProcessed

	// pendingKnown means that this task is relevant for resolving whether an
	// arc is present or not. This implies actTypeKnown.
	pendingKnown

	// disjunctionTask indicates that this task is a disjunction. This is
	// used to trigger finalization of disjunctions.
	disjunctionTask

	childConjunctsDone

	leftOfMaxCoreCondition

	finalStateKnown condition = leftOfMaxCoreCondition - 1

	preValidation condition = finalStateKnown //&^ validationCompleted

	conditionsUsingCounters = arcTypeKnown |
		valueKnown |
		fieldConjunctsKnown |
		allTasksCompleted |
		// TODO: not adding this improves error message for issue3691 in
		// eval/comprehensions.txtar. But without this, TestVisit of dep
		// panics. Investigate.
		pendingKnown |
		disjunctionTask

	// The xConjunct condition sets indicate a conjunct MAY contribute the to
	// final result. For some conjuncts it may not be known what the
	// contribution will be. In such a cases the set that reflects all possible
	// contributions should be used. For instance, an embedded reference may
	// resolve to a scalar or struct.
	//
	// All conjunct states include allTasksCompleted.

	// a genericConjunct is one for which the contributions to the states
	// are not known in advance. For instance, an embedded reference can be
	// anything. In such case, all conditions are included.
	genericConjunct = allTasksCompleted |
		scalarKnown |
		valueKnown |
		fieldConjunctsKnown

	// genericDisjunction is used to record processDisjunction tasks.
	genericDisjunction = genericConjunct | disjunctionTask

	// a fieldConjunct is on that only adds a new field to the struct.
	fieldConjunct = allTasksCompleted |
		fieldConjunctsKnown

	// a scalarConjunct is one that is guaranteed to result in a scalar or
	// list value.
	scalarConjunct = allTasksCompleted |
		scalarKnown |
		valueKnown |
		disjunctionTask

	// a scalarValue is one that is guaranteed to result in a scalar.
	// TODO: use more widely instead of scalarKnown.
	scalarValue = scalarKnown | disjunctionTask

	// needsX condition sets are used to indicate which conditions need to be
	// met.

	needFieldConjunctsKnown = fieldConjunctsKnown |
		allAncestorsProcessed

	needFieldSetKnown = fieldSetKnown |
		allAncestorsProcessed

	needTasksDone = allAncestorsProcessed | allTasksCompleted

	// concreteKnown means that we know whether a value is concrete or not.
	// At the moment this is equal to 'scalarKnown'.
	concreteKnown = scalarKnown

	// fieldConjunctsKnownIdx is the bit-position index of
	// fieldConjunctsKnown, for use as a scheduler.counters index.
	// It must equal bits.TrailingZeros16(uint16(fieldConjunctsKnown)).
	fieldConjunctsKnownIdx = 4
)

// schedConfig configures a taskContext with the states needed for the
// CUE evaluator. It is used in OpContext.New as a template for creating
// new taskContexts.
var schedConfig = taskContext{
	counterMask: conditionsUsingCounters,
	autoUnblock: scalarKnown | arcTypeKnown,
	complete:    stateCompletions,
}

// handleParents checks whether needs is already met and, if not, triggers
// ancestor processing to propagate conjuncts downward. It reports whether
// all ancestors have completed processing.
func (s *scheduler) handleParents(needs condition, mode runMode) (done bool) {
	if s.meets(needs) {
		return true
	}

	return s.node.processAncestors(mode)
}

// processAncestors walks up the parent chain, recursively processing each
// ancestor's pending conjuncts until fieldConjunctsKnown is reached. It
// returns true when all ancestors have finished processing.
func (n *nodeContext) processAncestors(mode runMode) (done bool) {
	if n == nil {
		return // Some tests do not set node.
	}
	v := n.node

	if n.meets(allAncestorsProcessed) {
		return true
	}

	parentsDone := true
	p := n.node.Parent
	switch {
	case p != nil:
		n := p.state
		// p.state is nil when the parent vertex exists but has not yet
		// entered evaluation (no nodeContext has been created for it).
		// Two known trigger paths:
		//   1. completePending → lookup → process → handleParents: a
		//      SelectorExpr inside an IfClause fires in a comprehension
		//      while its parent struct is still lazy.
		//   2. unifyNode passes arcTypeKnown|fieldSetKnown to process,
		//      so handleParents no longer returns early once arcTypeKnown
		//      is met, reaching processAncestors for nodes whose parents
		//      were never unified. (Reproducer: TestScript/cmd_typocheck)
		// Without this guard the nil receiver panics at n.meets(...).
		if n == nil {
			break
		}

		// p.state is nil when the parent vertex exists but has not yet
		// entered evaluation (no nodeContext has been created for it).
		// Two known trigger paths:
		//   1. completePending → lookup → process → handleParents: a
		//      SelectorExpr inside an IfClause fires in a comprehension
		//      while its parent struct is still lazy.
		//   2. unifyNode passes arcTypeKnown|fieldSetKnown to process,
		//      so handleParents no longer returns early once arcTypeKnown
		//      is met, reaching processAncestors for nodes whose parents
		//      were never unified. (Reproducer: TestScript/cmd_typocheck)
		// Without this guard the nil receiver panics at n.meets(...).
		if n == nil {
			return false
		}

		if n.meets(childConjunctsDone) {
			break
		}

		parentsDone = n.processAncestors(mode)

		if n.counters[fieldConjunctsKnownIdx] > 0 {
			n.process(fieldConjunctsKnown, mode)
		}

		if parentsDone && n.counters[fieldConjunctsKnownIdx] == 0 {
			n.completed |= childConjunctsDone
		}
	}

	if done || v.IsDynamic || v.Label.IsLet() ||
		v.Parent.allChildConjunctsKnown(n.ctx) {
		n.signal(allAncestorsProcessed)
	}

	return parentsDone
}

// stateCompletions indicates the completion of conditions based on the
// completions of other conditions.
func stateCompletions(s *scheduler) condition {
	x := s.completed
	v := s.node.node
	if s.node.ctx.LogEval > 0 {
		s.node.Logf("=== stateCompletions: %v  %v", v.Label, s.completed)
	}
	if x.meets(allAncestorsProcessed) {
		x |= conditionsUsingCounters &^ s.provided
		// If we have a pending or constraint arc, a sub arc may still cause the
		// arc to become a member. For instance, if 'a' is pending in the
		// following
		//   if x != _!_ {
		//       a: b: 1
		//   }
		// it may still become not pending if 'b' becomes a regular arc.
		if s.counters[arcTypeKnown] == 0 && x.meets(subFieldsProcessed) {
			x |= arcTypeKnown
		}
	}
	switch {
	case v.ArcType == ArcMember, v.ArcType == ArcNotPresent:
		x |= arcTypeKnown
	case x&arcTypeKnown != 0 && v.ArcType == ArcPending:
		v.ArcType = ArcNotPresent
	}

	if x.meets(valueKnown) {
		// NOTE: in this case, scalarKnown is not the same as concreteKnown,
		// especially if this arc is Pending, as it may still become concrete.
		// We probably want to separate this out.
		if v.ArcType == ArcMember || v.ArcType == ArcNotPresent {
			x |= scalarKnown
		}
	}

	if x.meets(needFieldConjunctsKnown | needTasksDone) {
		switch {
		case x.meets(subFieldsProcessed):
			x |= fieldSetKnown
		default:
			for _, a := range v.Arcs {
				if a.ArcType == ArcPending {
					return x
				}
			}
			x |= fieldSetKnown
		}
	}
	return x
}

// allChildConjunctsKnown indicates that all conjuncts have been added by
// the parents and every conjunct that may add fields to subfields have been
// processed.
func (v *Vertex) allChildConjunctsKnown(ctx *OpContext) bool {
	if v == nil {
		return true
	}

	n := v.getState(ctx)
	if n == nil {
		return true
	}
	return n.meets(fieldConjunctsKnown | allAncestorsProcessed)
}

func (n *nodeContext) scheduleTask(r *runner, env *Environment, x Node, ci CloseInfo) *task {
	t := n.ctx.newTask()
	t.run = r
	t.node = n
	t.env = env
	t.id = ci
	t.x = x
	n.insertTask(t)
	return t
}
