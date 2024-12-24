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
	"strings"
	"testing"

	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

// These states used for this test. Each has a suffix of their corresponding
// name in states.go. Debuggers, when resolving constants, will often only
// how the debug constants. Adding the suffix clarifies which states these
// correspond to in the main program.
//
// We could also use the states in the main program directly, but states may
// shift, so this way we ensures that we separate these concerns.
const (
	c1AllAncestorsProcessed condition = 1 << iota
	c2ArcTypeKnown
	c3ValueKnown
	c4ScalarKnown

	// autoFieldConjunctsKnown is a condition that is automatically set by the simulator.
	autoFieldConjunctsKnown
)

func TestStateNames(t *testing.T) {
	if c1AllAncestorsProcessed != allAncestorsProcessed {
		t.Error("inconsistent state name for allAncestorsProcessed")
	}
	if c2ArcTypeKnown != arcTypeKnown {
		t.Error("inconsistent state name for arcTypeKnown")
	}
	if c3ValueKnown != valueKnown {
		t.Error("inconsistent state name for valueKnown")
	}
	if c4ScalarKnown != scalarKnown {
		t.Error("inconsistent state name for scalarKnown")
	}
	if autoFieldConjunctsKnown != fieldConjunctsKnown {
		t.Error("inconsistent state name for fieldConjunctsKnown")
	}
}

// TestScheduler tests the non-CUE specific scheduler functionality.
func TestScheduler(t *testing.T) {
	ctx := &OpContext{
		Version: internal.DefaultVersion,
		taskContext: taskContext{
			counterMask: c1AllAncestorsProcessed | c2ArcTypeKnown | c3ValueKnown | c4ScalarKnown,
			complete:    func(s *scheduler) condition { return 0 },
		},
	}

	// shared state
	nodeID := 0
	w := &strings.Builder{}
	nodes := []*nodeContext{}

	node := func(parent *nodeContext) *nodeContext {
		if nodeID == 0 {
			if parent != nil {
				t.Fatal("root node must be created first")
			}
		} else {
			if parent == nil {
				t.Fatal("non-root node must have parent")
			}
		}

		n := &nodeContext{scheduler: scheduler{ctx: ctx}, refCount: nodeID}
		nodeID++
		nodes = append(nodes, n)
		return n
	}

	// dep encodes a dependency on a node uncovered while running a task. It
	// corresponds to a single evaluation of a top-level expression within a
	// task.
	type dep struct {
		node  *nodeContext
		needs condition
	}

	// process simulates the running of a task with the given dependencies on
	// other nodes/ schedulers.
	//
	// Note that tasks indicate their dependencies at runtime, and that these
	// are not statically declared at the time of task creation. This is because
	// dependencies may only be known after evaluating some CUE. As a
	// consequence, it may be possible for a tasks to be started before one of
	// its dependencies is run. Blocking only occurs if there is a mutual
	// dependency that cannot be resolved without first blocking the task and
	// coming back to it later.
	process := func(name string, t *task, deps ...dep) (ok bool) {
		fmt.Fprintf(w, "\n\t\t    running task %s", name)
		ok = true
		for _, d := range deps {
			func() {
				defer func() {
					if x := recover(); x != nil {
						fmt.Fprintf(w, "\n\t\t        task %s waiting for v%d meeting %x", name, d.node.refCount, d.needs)
						fmt.Fprint(w, ": BLOCKED")
						panic(x)
					}
				}()
				if !d.node.process(d.needs, yield) {
					ok = false
				}
			}()
		}
		return ok
	}

	// success creates a task that will succeed.
	success := func(name string, n *nodeContext, completes, needs condition, deps ...dep) *task {
		t := &task{
			run: &runner{
				f: func(ctx *OpContext, t *task, mode runMode) {
					process(name, t, deps...)
				},
				completes: completes,
				needs:     needs,
			},
			node: n,
			x:    &String{Str: name}, // Set name for debugging purposes.
		}
		n.insertTask(t)
		return t
	}

	// signal is a task that unconditionally sets a completion bit.
	signal := func(name string, n *nodeContext, completes condition, deps ...dep) *task {
		t := &task{
			run: &runner{
				f: func(ctx *OpContext, t *task, mode runMode) {
					if process(name, t, deps...) {
						n.scheduler.signal(completes)
					}
				},
				completes: completes,
			},
			node: n,
			x:    &String{Str: name}, // Set name for debugging purposes.
		}
		n.insertTask(t)
		return t
	}

	// completes creates a task that completes some state in another node.
	completes := func(name string, n, other *nodeContext, completes condition, deps ...dep) *task {
		other.scheduler.incrementCounts(completes)
		t := &task{
			run: &runner{
				f: func(ctx *OpContext, t *task, mode runMode) {
					if process(name, t, deps...) {
						other.scheduler.decrementCounts(completes)
					}
				},
				completes: completes,
			},
			node: n,
			x:    &String{Str: name}, // Set name for debugging purposes.
		}
		n.insertTask(t)
		return t
	}

	// fail creates a task that will fail.
	fail := func(name string, n *nodeContext, completes, needs condition, deps ...dep) *task {
		t := &task{

			run: &runner{
				f: func(ctx *OpContext, t *task, mode runMode) {
					fmt.Fprintf(w, "\n\t\t    running task %s:", name)
					t.err = &Bottom{}
					fmt.Fprint(w, " FAIL")
				},
				completes: completes,
				needs:     needs,
			},
			node: n,
			x:    &String{Str: name}, // Set name for debugging purposes.
		}
		n.insertTask(t)
		return t
	}

	type testCase struct {
		name string
		init func()

		log   string // A lot
		state string // A textual representation of the task state
	}

	cases := []testCase{{
		name: "empty scheduler",
		init: func() {
			node(nil)
		},
		log: ``,

		state: `
			v0 (SUCCESS):`,
	}, {
		name: "node with one task",
		init: func() {
			v0 := node(nil)
			success("t1", v0, c1AllAncestorsProcessed, 0)
		},
		log: `
		    running task t1`,

		state: `
			v0 (SUCCESS):
			    task:    t1: SUCCESS`,
	}, {
		name: "node with two tasks",
		init: func() {
			v0 := node(nil)
			success("t1", v0, c1AllAncestorsProcessed, 0)
			success("t2", v0, c2ArcTypeKnown, 0)
		},
		log: `
		    running task t1
		    running task t2`,

		state: `
			v0 (SUCCESS):
			    task:    t1: SUCCESS
			    task:    t2: SUCCESS`,
	}, {
		name: "node failing task",
		init: func() {
			v0 := node(nil)
			fail("t1", v0, c1AllAncestorsProcessed, 0)
		},
		log: `
		    running task t1: FAIL`,

		state: `
			v0 (SUCCESS):
			    task:    t1: FAILED`,
	}, {
		// Tasks will have to be run in order according to their dependencies.
		// Note that the tasks will be run in order, as they all depend on the
		// same node, in which case the order must be and will be strictly
		// enforced.
		name: "dependency chain on nodes within scheduler",
		init: func() {
			v0 := node(nil)
			success("third", v0, c3ValueKnown, c2ArcTypeKnown)
			success("fourth", v0, c4ScalarKnown, c3ValueKnown)
			success("second", v0, c2ArcTypeKnown, c1AllAncestorsProcessed)
			success("first", v0, c1AllAncestorsProcessed, 0)
		},
		log: `
		    running task first
		    running task second
		    running task third
		    running task fourth`,

		state: `
			v0 (SUCCESS):
			    task:    third: SUCCESS
			    task:    fourth: SUCCESS
			    task:    second: SUCCESS
			    task:    first: SUCCESS`,
	}, {
		// If a task depends on a state completion for which there is no task,
		// it should be considered as completed, because essentially all
		// information is known about that state.
		name: "task depends on state for which there is no task",
		init: func() {
			v0 := node(nil)
			success("t1", v0, c2ArcTypeKnown, c1AllAncestorsProcessed)
		},
		log: `
		    running task t1`,
		state: `
			v0 (SUCCESS):
			    task:    t1: SUCCESS`,
	}, {
		// Same as previous, but now for another node.
		name: "task depends on state of other node for which there is no task",
		init: func() {
			v0 := node(nil)
			v1 := node(v0)
			v2 := node(v0)
			success("t1", v1, c1AllAncestorsProcessed, 0, dep{node: v2, needs: c2ArcTypeKnown})
		},
		log: `
		    running task t1`,
		state: `
			v0 (SUCCESS):
			v1 (SUCCESS):
			    task:    t1: SUCCESS
			v2 (SUCCESS):`,
	}, {
		name: "tasks depend on multiple other tasks within same scheduler",
		init: func() {
			v0 := node(nil)
			success("before1", v0, c2ArcTypeKnown, 0)
			success("last", v0, c4ScalarKnown, c1AllAncestorsProcessed|c2ArcTypeKnown|c3ValueKnown)
			success("block", v0, c3ValueKnown, c1AllAncestorsProcessed|c2ArcTypeKnown)
			success("before2", v0, c1AllAncestorsProcessed, 0)
		},
		log: `
		    running task before1
		    running task before2
		    running task block
		    running task last`,

		state: `
			v0 (SUCCESS):
			    task:    before1: SUCCESS
			    task:    last: SUCCESS
			    task:    block: SUCCESS
			    task:    before2: SUCCESS`,
	}, {
		// In this test we simulate dynamic reference that are dependent
		// on each other in a chain to form the fields. Task t0 would not be
		// a task in the regular evaluator, but it is included there as a
		// task in absence of the ability to simulate static elements.
		//
		//	v0: {
		//		(v0.baz): "bar" // task t1
		//		(v0.foo): "baz" // task t2
		//		baz: "foo"      // task t0
		//	}
		//
		name: "non-cyclic dependencies between nodes p1",
		init: func() {
			v0 := node(nil)
			baz := node(v0)
			success("t0", baz, c1AllAncestorsProcessed, 0)
			foo := node(v0)

			completes("t1:bar", v0, foo, c2ArcTypeKnown, dep{node: baz, needs: c1AllAncestorsProcessed})
			success("t2:baz", v0, c1AllAncestorsProcessed, 0, dep{node: foo, needs: c2ArcTypeKnown})
		},
		log: `
		    running task t1:bar
		    running task t0
		    running task t2:baz`,
		state: `
			v0 (SUCCESS):
			    task:    t1:bar: SUCCESS
			    task:    t2:baz: SUCCESS
			v1 (SUCCESS):
			    task:    t0: SUCCESS
			v2 (SUCCESS):`,
	}, {
		// Like the previous test, but different order of execution.
		//
		//	v0: {
		//		(v0.foo): "baz" // task t2
		//		(v0.baz): "bar" // task t1
		//		baz: "foo"      // task t0
		//	}
		//
		name: "non-cyclic dependencies between nodes p2",
		init: func() {
			v0 := node(nil)
			baz := node(v0)
			success("foo", baz, c1AllAncestorsProcessed, 0)
			foo := node(v0)

			success("t2:baz", v0, c1AllAncestorsProcessed, 0, dep{node: foo, needs: c2ArcTypeKnown})
			completes("t1:bar", v0, foo, c2ArcTypeKnown, dep{node: baz, needs: c1AllAncestorsProcessed})
		},
		log: `
		    running task t2:baz
		    running task t1:bar
		    running task foo`,
		state: `
			v0 (SUCCESS):
			    task:    t2:baz: SUCCESS
			    task:    t1:bar: SUCCESS
			v1 (SUCCESS):
			    task:    foo: SUCCESS
			v2 (SUCCESS):`,
	}, {
		//	    b: a - 10
		//	    a: b + 10
		name: "cycle in mutually referencing expressions",
		init: func() {
			v0 := node(nil)
			v1 := node(v0)
			v2 := node(v0)
			success("a-10", v1, c1AllAncestorsProcessed|c2ArcTypeKnown, 0, dep{node: v2, needs: c1AllAncestorsProcessed})
			success("b+10", v2, c1AllAncestorsProcessed|c2ArcTypeKnown, 0, dep{node: v1, needs: c1AllAncestorsProcessed})
		},
		log: `
		    running task a-10
		    running task b+10
		        task b+10 waiting for v1 meeting 1: BLOCKED
		        task a-10 waiting for v2 meeting 1: BLOCKED
		    running task b+10
		    running task a-10`,
		state: `
			v0 (SUCCESS):
			v1 (SUCCESS): (frozen)
			    task:    a-10: SUCCESS (unblocked)
			v2 (SUCCESS): (frozen)
			    task:    b+10: SUCCESS (unblocked)`,
	}, {
		//	    b: a - 10
		//	    a: b + 10
		//	    a: 5
		name: "broken cyclic reference in expressions",
		init: func() {
			v0 := node(nil)
			v1 := node(v0)
			v2 := node(v0)
			success("a-10", v1, c1AllAncestorsProcessed|c2ArcTypeKnown, 0, dep{node: v2, needs: c1AllAncestorsProcessed})
			success("b+10", v2, c1AllAncestorsProcessed|c2ArcTypeKnown, 0, dep{node: v1, needs: c1AllAncestorsProcessed})

			// NOTE: using success("5", v2, c1, 0) here would cause the cyclic
			// references to block, as they would both provide and depend on
			// v1 and v2 becoming scalars. Once a field is known to be a scalar,
			// it can safely be signaled as unification cannot make it more
			// concrete. Further unification could result in an error, but that
			// will be caught by completing the unification.
			signal("5", v2, c1AllAncestorsProcessed)
		},
		log: `
		    running task a-10
		    running task b+10
		        task b+10 waiting for v1 meeting 1: BLOCKED
		    running task 5
		    running task b+10`,
		state: `
			v0 (SUCCESS):
			v1 (SUCCESS):
			    task:    a-10: SUCCESS
			v2 (SUCCESS):
			    task:    b+10: SUCCESS
			    task:    5: SUCCESS`,
	}, {
		// This test simulates a case where a comprehension projects
		// onto itself. The cycle is broken by allowing a required state
		// to be dropped upon detecting a cycle. For comprehensions,
		// for instance, one usually would define that it provides fields in
		// the vertex in which it is defined. However, for self-projections
		// this results in a cycle. By dropping the requirement that all fields
		// need be specified the cycle is broken. However, this means the
		// comprehension may no longer add new fields to the vertex.
		//
		//	x: {
		//		for k, v in x {
		//			(k): v
		//		}
		//		foo: 5
		//	}
		name: "self cyclic",
		init: func() {
			x := node(nil)
			foo := node(x)
			success("5", foo, c1AllAncestorsProcessed, 0)
			success("comprehension", x, c1AllAncestorsProcessed, 0, dep{node: x, needs: c1AllAncestorsProcessed})
		},
		log: `
		    running task comprehension
		        task comprehension waiting for v0 meeting 1: BLOCKED
		    running task comprehension
		    running task 5`,
		state: `
			v0 (SUCCESS): (frozen)
			    task:    comprehension: SUCCESS (unblocked)
			v1 (SUCCESS):
			    task:    5: SUCCESS`,
	}, {
		// This test simulates a case where comprehensions are not allowed to
		// project on themselves. CUE allows this, but it is to test that
		// similar constructions where this is not allowed do not cause
		// infinite loops.
		//
		//	x: {
		//		for k, v in x {
		//			(k+"X"): v
		//		}
		//		foo: 5
		//	}
		// TODO: override freeze.
		name: "self cyclic not allowed",
		init: func() {
			x := node(nil)
			foo := node(x)
			success("5", foo, c1AllAncestorsProcessed, 0)
			success("comprehension", x, c1AllAncestorsProcessed, 0, dep{node: x, needs: c1AllAncestorsProcessed})
		},
		log: `
		    running task comprehension
		        task comprehension waiting for v0 meeting 1: BLOCKED
		    running task comprehension
		    running task 5`,
		state: `
			v0 (SUCCESS): (frozen)
			    task:    comprehension: SUCCESS (unblocked)
			v1 (SUCCESS):
			    task:    5: SUCCESS`,
	}, {
		// This test simulates a case where comprehensions mutually project
		// on each other.
		//
		//	x: {
		//		for k, v in y {
		//			(k): v
		//		}
		//	}
		//	y: {
		//		for k, v in x {
		//			(k): v
		//		}
		//	}
		name: "mutually cyclic projection",
		init: func() {
			v0 := node(nil)
			x := node(v0)
			y := node(v0)

			success("comprehension", x, c1AllAncestorsProcessed, 0, dep{node: y, needs: c1AllAncestorsProcessed})
			success("comprehension", y, c1AllAncestorsProcessed, 0, dep{node: x, needs: c1AllAncestorsProcessed})

		},
		log: `
		    running task comprehension
		    running task comprehension
		        task comprehension waiting for v1 meeting 1: BLOCKED
		        task comprehension waiting for v2 meeting 1: BLOCKED
		    running task comprehension
		    running task comprehension`,
		state: `
			v0 (SUCCESS):
			v1 (SUCCESS): (frozen)
			    task:    comprehension: SUCCESS (unblocked)
			v2 (SUCCESS): (frozen)
			    task:    comprehension: SUCCESS (unblocked)`,
	}, {
		// This test simulates a case where comprehensions are not allowed to
		// project on each other cyclicly. CUE allows this, but it is to test
		// that similar constructions where this is not allowed do not cause
		// infinite loops.
		//
		//	x: {
		//		for k, v in y {
		//			(k): v
		//		}
		//	}
		//	y: {
		//		for k, v in x {
		//			(k): v
		//		}
		//		foo: 5
		//	}
		name: "disallowed mutually cyclic projection",
		init: func() {
			v0 := node(nil)
			x := node(v0)
			y := node(v0)
			foo := node(y)
			success("5", foo, c1AllAncestorsProcessed, 0)

			success("comprehension", x, c1AllAncestorsProcessed, 0, dep{node: y, needs: c1AllAncestorsProcessed})
			success("comprehension", y, c1AllAncestorsProcessed, 0, dep{node: x, needs: c1AllAncestorsProcessed})

		},
		log: `
		    running task comprehension
		    running task comprehension
		        task comprehension waiting for v1 meeting 1: BLOCKED
		        task comprehension waiting for v2 meeting 1: BLOCKED
		    running task comprehension
		    running task comprehension
		    running task 5`,
		state: `
			v0 (SUCCESS):
			v1 (SUCCESS): (frozen)
			    task:    comprehension: SUCCESS (unblocked)
			v2 (SUCCESS): (frozen)
			    task:    comprehension: SUCCESS (unblocked)
			v3 (SUCCESS):
			    task:    5: SUCCESS`,
	}}

	cuetest.Run(t, cases, func(t *cuetest.T, tc *testCase) {
		// t.Update(true)
		// t.Select("non-cyclic_dependencies_between_nodes_p2")

		nodeID = 0
		nodes = nodes[:0]
		w.Reset()

		// Create and run root scheduler.
		tc.init()
		for _, n := range nodes {
			n.provided |= autoFieldConjunctsKnown
			n.signalDoneAdding()
		}
		for _, n := range nodes {
			n.finalize(autoFieldConjunctsKnown)
		}

		t.Equal(w.String(), tc.log)

		w := &strings.Builder{}
		for _, n := range nodes {
			fmt.Fprintf(w, "\n\t\t\tv%d (%v):", n.refCount, n.state)
			if n.scheduler.isFrozen {
				fmt.Fprint(w, " (frozen)")
			}
			for _, t := range n.tasks {
				fmt.Fprintf(w, "\n\t\t\t    task:    %s: %v", t.x.(*String).Str, t.state)
				if t.unblocked {
					fmt.Fprint(w, " (unblocked)")
				}
			}
			for _, t := range n.blocking {
				if t.blockedOn != nil {
					fmt.Fprintf(w, "\n\t\t\t    blocked: %s: %v", t.x.(*String).Str, t.state)
				}
			}
		}

		t.Equal(w.String(), tc.state)
	})
}
