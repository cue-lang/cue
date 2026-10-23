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
)

// schedTest builds scheduler nodes and tasks directly, so that the tests below
// can drive the scheduler without going through evaluation.
type schedTest struct {
	ctx *OpContext
}

func newSchedTest() *schedTest {
	return &schedTest{ctx: &OpContext{
		Version: internal.EvalV3,
		taskContext: taskContext{
			complete: func(*scheduler) condition { return 0 },
		},
	}}
}

// node returns an initialized node. inTry makes it the inline vertex
// pre-evaluating a try clause body.
func (st *schedTest) node(inTry bool) *nodeContext {
	n := &nodeContext{}
	n.isInitialized = true
	n.node = &Vertex{state: n, nonRooted: inTry}
	n.scheduler = scheduler{
		ctx: st.ctx, node: n, running: true,
		completed: allAncestorsProcessed,
	}
	if inTry {
		n.tryBody = &tryBody{}
	}
	return n
}

// task adds a task to owner which runs f.
func (st *schedTest) task(owner *nodeContext, f func()) *task {
	x := st.ctx.newTask()
	x.node = owner
	x.run = &runner{completes: valueKnown, f: func(*OpContext, *task, runMode) { f() }}
	owner.insertTask(x)
	return x
}

// wait adds a task to owner which runs f once dependency completes cond.
func (st *schedTest) wait(owner, dependency *nodeContext, cond condition, f func()) *task {
	t := st.task(owner, f)
	dependency.blockOn(cond)
	t.waitFor(&dependency.scheduler, cond)
	return t
}

// TestSchedulerReentrantFinalize exercises queue ownership independently of
// CUE evaluation. In particular, a nested regular finalize must not force an
// enclosing try body's deferred waits, even when a task address is reused.
func TestSchedulerReentrantFinalize(t *testing.T) {
	for _, auto := range []bool{false, true} {
		for _, reuse := range []string{"unchanged", "reblocked", "recycled", "cleared"} {
			t.Run(fmt.Sprintf("auto=%v/%s", auto, reuse), func(t *testing.T) {
				st := newSchedTest()
				ctx := st.ctx
				if auto {
					ctx.autoUnblock = scalarKnown
				}

				outer := st.node(true)
				nested := st.node(false)
				dependency := st.node(false)
				owner := st.node(false)
				var runs, addedRuns int
				// Put the deferred wait before the trigger: a live-length loop
				// with in-place compaction could move it behind its cursor.
				deferred := st.wait(owner, dependency, valueKnown, func() { runs++ })
				st.wait(outer, st.node(false), scalarKnown, func() {
					switch reuse {
					case "reblocked":
						dependency.signal(valueKnown)
						dependency = st.node(false)
						dependency.blockOn(valueKnown)
						deferred.waitFor(&dependency.scheduler, valueKnown)
					case "recycled", "cleared":
						owner.clear()
						// Clearing freed memory must not affect enclosing queues.
						*deferred = task{}
						if reuse == "recycled" {
							if x := st.wait(st.node(false), dependency, valueKnown, func() { runs++ }); x != deferred {
								t.Fatal("test did not reuse the freed task")
							}
						}
					}

					// New waits belong to this nested try finalize. Its own
					// task can run, but its external wait must be returned.
					innerTry := st.node(true)
					st.wait(st.node(false), st.node(false), valueKnown, func() { addedRuns++ })
					innerRuns := 0
					st.wait(innerTry, st.node(false), scalarKnown, func() {
						innerRuns++
						nested.process(allKnown, finalize)
					})
					innerTry.process(allKnown, finalize)
					if innerRuns != 1 {
						t.Fatalf("inner task ran %d times, want 1", innerRuns)
					}
				})

				outer.process(allKnown, finalize)
				// The nested try owns newly registered waits, so its nested
				// regular finalize cannot see or force those waits either.
				wantRuns := 0
				if reuse == "reblocked" {
					// This run was justified by an explicit signal, before reblocking.
					wantRuns = 1
				}
				wantAdded := 0
				wantFrozen := false
				wantPending := 2
				if reuse == "cleared" {
					wantPending = 1
				}
				if runs != wantRuns || addedRuns != wantAdded {
					t.Fatalf("runs: original=%d (want %d), added=%d (want %d)",
						runs, wantRuns, addedRuns, wantAdded)
				}
				if got := dependency.frozen&valueKnown != 0; got != wantFrozen {
					t.Fatalf("deferred dependency frozen = %v, want %v", got, wantFrozen)
				}
				if len(ctx.blocking) != wantPending {
					t.Fatalf("retained %d waits, want %d", len(ctx.blocking), wantPending)
				}

				// A subsequent regular finalize must drain every deferred wait.
				nested.process(allKnown, finalize)
				if reuse != "cleared" {
					wantRuns++
				}
				wantAdded = 1
				if runs != wantRuns || addedRuns != wantAdded || len(ctx.blocking) != 0 {
					t.Fatalf("lost waits: original=%d (want %d), added=%d (want %d), pending=%d",
						runs, wantRuns, addedRuns, wantAdded, len(ctx.blocking))
				}
			})
		}
	}
}

// TestSchedulerReentrantFinalizeRetainsTasks covers a regular outer finalize.
// A nested try finalize used to compact the shared queue and move an unvisited
// task behind the outer cursor. Resetting the queue then silently lost it.
func TestSchedulerReentrantFinalizeRetainsTasks(t *testing.T) {
	for _, newReady := range []bool{false, true} {
		t.Run(fmt.Sprintf("newReady=%v", newReady), func(t *testing.T) {
			st := newSchedTest()
			ctx := st.ctx
			waiting := func(f func()) {
				st.wait(st.node(false), st.node(false), valueKnown, f)
			}
			outer := st.node(false)
			var ran []string
			waiting(func() {
				ran = append(ran, "trigger")
				waiting(func() { ran = append(ran, "nested") })
				st.node(true).process(allKnown, finalize)
				if newReady {
					// A new runnable task must precede the next batch of forced waits.
					st.task(outer, func() { ran = append(ran, "ready") })
				}
			})
			waiting(func() { ran = append(ran, "first") })
			waiting(func() { ran = append(ran, "second") })
			outer.process(allKnown, finalize)
			want := "trigger,first,second,nested"
			if newReady {
				want = "trigger,first,second,ready,nested"
			}
			if got := strings.Join(ran, ","); got != want {
				t.Fatalf("task execution = %s, want %s", got, want)
			}
			if len(ctx.blocking) != 0 {
				t.Fatalf("%d waits left after regular finalize", len(ctx.blocking))
			}
		})
	}
}

// TestSchedulerDeferredAcrossTaskLoop covers waits deferred by a try body
// finalize while it returns to its task loop: a regular finalize started by
// one of the tasks it runs there must not force them either.
func TestSchedulerDeferredAcrossTaskLoop(t *testing.T) {
	st := newSchedTest()
	outer := st.node(true)
	nested := st.node(false)
	dependency := st.node(false)
	var runs int
	st.wait(st.node(false), dependency, valueKnown, func() { runs++ })
	st.wait(outer, st.node(false), scalarKnown, func() {
		// A task added here sends the try body's finalize back to its task
		// loop with the deferred wait still outstanding.
		st.task(outer, func() { nested.process(allKnown, finalize) })
	})

	outer.process(allKnown, finalize)
	wantRuns := 0
	wantFrozen := false
	wantPending := 1
	if runs != wantRuns {
		t.Fatalf("deferred wait ran %d times, want %d", runs, wantRuns)
	}
	if got := dependency.frozen&valueKnown != 0; got != wantFrozen {
		t.Fatalf("deferred dependency frozen = %v, want %v", got, wantFrozen)
	}
	if len(st.ctx.blocking) != wantPending {
		t.Fatalf("retained %d waits, want %d", len(st.ctx.blocking), wantPending)
	}

	// A subsequent regular finalize must drain the deferred wait.
	nested.process(allKnown, finalize)
	if runs != 1 || len(st.ctx.blocking) != 0 {
		t.Fatalf("lost wait: runs=%d (want 1), pending=%d", runs, len(st.ctx.blocking))
	}
}

// TestSchedulerRecycledTaskWait covers the per-scheduler blocking list, which
// keeps a task after it ran. Once the task is freed and recycled for a wait on
// another scheduler, the stale entry must not act on that new wait.
func TestSchedulerRecycledTaskWait(t *testing.T) {
	for _, op := range []string{"signal", "clear"} {
		t.Run(op, func(t *testing.T) {
			st := newSchedTest()
			var runs int
			waitOn := func(owner, dependency *nodeContext) *task {
				return st.wait(owner, dependency, valueKnown, func() { runs++ })
			}

			// The task waits on dep, runs once dep completes, and stays in
			// dep's blocking list afterwards.
			owner, dep := st.node(false), st.node(false)
			x := waitOn(owner, dep)
			dep.signal(valueKnown)
			if runs != 1 {
				t.Fatalf("task ran %d times, want 1", runs)
			}

			// Freeing the task returns it to the pool, from where it is
			// handed out again for a wait on another scheduler.
			owner.clear()
			other := st.node(false)
			if y := waitOn(st.node(false), other); y != x {
				t.Fatal("test did not reuse the freed task")
			}

			// The entry dep kept for the freed task no longer says anything
			// about the recycled task: only the scheduler it now waits on
			// may run it.
			wantForced, wantFinal := 1, 2
			switch op {
			case "signal":
				dep.signal(scalarKnown)
				if runs != wantForced {
					t.Fatalf("after unrelated signal: runs=%d, want %d", runs, wantForced)
				}
			case "clear":
				dep.clear()
			}
			other.signal(valueKnown)
			if runs != wantFinal {
				t.Fatalf("after the awaited signal: runs=%d, want %d", runs, wantFinal)
			}
		})
	}
}
