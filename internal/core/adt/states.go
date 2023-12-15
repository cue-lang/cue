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

// These are the condition types that of the CUE evaluator. A scheduler
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

	valueKnown

	// scalarKnown indicates that a Vertex has either a concrete scalar value or
	// that it is known that it will never have a scalar value.
	//
	// This is a signal condition that is reached when:
	//    - a node is set to a concrete scalar value
	//    - a node is set to an error
	//    - or if XXXstate is reached.
	//
	// TODO: rename to something better?
	scalarKnown

	// listTypeKnown indicates that it is known that lists unified with this
	// Vertex should be interpreted as integer indexed lists, as associative
	// lists, or an error.
	//
	// This is a signal condition that is reached when:
	//    - allFieldsKnown is reached (all expressions have )
	//    - it is unified with an associative list type
	listTypeKnown

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

	// validationCompleted

	leftOfMaxCoreCondition

	finalStateKnown condition = leftOfMaxCoreCondition - 1

	preValidation condition = finalStateKnown //&^ validationCompleted

	conditionsUsingCounters = arcTypeKnown |
		valueKnown |
		// fieldSetKnown |
		fieldConjunctsKnown |
		allTasksCompleted

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
		// fieldSetKnown

	// a fieldConjunct is on that only adds a new field to the struct.
	fieldConjunct = allTasksCompleted |
		fieldConjunctsKnown
		// fieldSetKnown

	// a scalarConjunct is one that is guaranteed to result in a scalar or
	// list value.
	scalarConjunct = allTasksCompleted |
		scalarKnown |
		valueKnown

	// a selfReferentialComprehensionConjunct is one where the source of a for
	// comprehension is a self-reference (direct or indirect). Such a
	// comprehension is legal, but may not create new fields. It still may
	// add conjuncts to sub fields.
	//
	// This conjunct is only here for reference. Self-referential comprehensions
	// are only uncovered during evaluation.
	// selfReferentialComprehensionConjunct = allTasksCompleted |
	// 	scalarKnown |
	// 	valueKnown |
	// 	fieldConjunctsKnown
	// _ = selfReferentialComprehensionConjunct

	// A pendingConjunct is one that influences the ArcType of a Vertex.
	// For instance, if a "pushed down" comprehension may turn a pending field
	// into a field constraint or regular field.
	pendingConjunct = genericConjunct | arcTypeKnown

	// needsX condition sets are used to indicate which conditions need to be
	// met.

	// needConjunctsKnown = allAncestorsProcessed | allTasksCompleted

	needFieldConjunctsKnown = // fieldSetKnown |
	fieldConjunctsKnown |
		allAncestorsProcessed

	needFieldSetKnown = fieldSetKnown |
		allAncestorsProcessed

	// concreteKnown = scalarKnown // isFinal // more like final
	// once struct is known, this should become fieldSetKnown.
	concreteKnown = scalarKnown // isFinal // more like final

	needTasksDone = allAncestorsProcessed | allTasksCompleted

	// fieldConjunctsKnown = fieldSetKnown
)
