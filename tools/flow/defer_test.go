package flow_test

import (
	"context"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/task"
	"cuelang.org/go/tools/flow"
)

// TestStaticDeferredMixing verifies that static tasks (dependencies known at
// start) are NOT deferred even if they are dependencies of a task that is part
// of a cycle.
//
// The previous "sibling task" heuristic (waiting for tasks with 0 dependencies)
// would incorrectly defer independent tasks if they happened to be leaf nodes
// in the dependency graph, potentially causing them to never run if the
// controller was waiting for them to complete before starting the server.
func TestStaticDeferredMixing(t *testing.T) {
	ctx := cuecontext.New()

	// This configuration has:
	// 1. A server task (root) that excludes itself from cycles.
	// 2. A static task (setup) that runs immediately.
	// 3. A dynamic task (handler) that depends on the server's request (runtime coverage).
	// 4. A dependency from server to setup (server needs setup done).
	//
	// In a correct implementation:
	// - 'setup' should NOT be deferred because it has no runtime dependency.
	// - 'server' waits for 'setup'.
	// - 'handler' is deferred until valid request.
	val := ctx.CompileString(`
		// Server task (Service)
		root: {
			$id: "valStub"
			$exclude: true
			// Depends on static task
			dep: setup.done
		}

		// Static task - should run immediately
		setup: {
			$id: "valStub"
			done: true 
		}

		// Dynamic task - depends on runtime value from root
		handler: {
			$id: "valStub"
			// Depends on runtime field
			input: root.request.body
		}
	`)

	if err := val.Err(); err != nil {
		t.Fatal(err)
	}

	// Track which tasks ran
	ran := make(map[string]bool)

	// Register a stub runner
	runner := func(v cue.Value) (flow.Runner, error) {
		selectors := v.Path().Selectors()
		if len(selectors) == 0 {
			return nil, nil // Not a task
		}
		name := selectors[0].String()
		exclude := false
		if v.LookupPath(cue.ParsePath("$exclude")).Exists() {
			exclude = true
		}

		return &stubRunner{
			name:    name,
			exclude: exclude,
			runFunc: func() error {
				ran[name] = true
				return nil
			},
		}, nil
	}

	c := flow.New(nil, val, flow.TaskFunc(runner))

	// Run the flow
	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Flow run failed: %v", err)
	}

	// Verify 'setup' ran. It is a static dependency of 'root'.
	// If 'setup' was incorrectly deferred, 'root' would wait forever for it,
	// or if 'root' started, 'setup' wouldn't have run yet.
	if !ran["setup"] {
		t.Error("Static task 'setup' did not run. It may have been incorrectly deferred.")
	}

	// Verify 'root' ran (it waits for setup)
	if !ran["root"] {
		t.Error("Root task 'root' did not run.")
	}

	// Verify 'handler' did NOT run (it depends on runtime input)
	if ran["handler"] {
		t.Error("Dynamic task 'handler' ran prematurely. It should be deferred.")
	}
}

type stubRunner struct {
	name    string
	exclude bool
	runFunc func() error
}

func (r *stubRunner) Run(t *flow.Task, err error) error {
	return r.runFunc()
}

func (r *stubRunner) IsService() bool {
	return r.exclude
}

// Needed to implement task.Runner for registration (though strictly flow.Runner is interface)
func (r *stubRunner) RunTask(ctx *task.Context) (results interface{}, err error) {
	return nil, r.runFunc()
}
