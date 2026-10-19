// Copyright 2026 CUE Authors
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

package task_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"weak"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/task"
	"cuelang.org/go/internal/value"
	"cuelang.org/go/tools/flow"
)

type testRunner struct {
	service bool
	run     func()
}

func (r testRunner) Run(t *flow.Task, err error) error {
	r.run()
	return nil
}

func (r testRunner) IsService() bool { return r.service }

func TestStartBackgroundWhenReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		v := cuecontext.New().CompileString(`
			serve: $id: "serve"
			slowServe: $id: "slowServe"
			client: $id: "client"
			waitingServe: {
				$id:    "waitingServe"
				$after: client
			}
		`)
		started := make(chan struct{})
		serveDone := make(chan struct{})
		releaseSlowServe := make(chan struct{})
		runners := map[string]testRunner{
			"serve": {service: true, run: func() {
				(&task.Context{}).BackgroundTask(func() { close(started) })
			}},
			"slowServe":    {service: true, run: func() { <-releaseSlowServe }},
			"client":       {run: func() { <-t.Context().Done() }},
			"waitingServe": {service: true, run: func() {}},
		}
		cfg := &flow.Config{
			UpdateFunc: func(c *flow.Controller, ft *flow.Task) error {
				err := task.StartBackgroundWhenReady(c, ft)
				if ft != nil && ft.Path().String() == "serve" {
					close(serveDone)
				}
				return err
			},
		}
		c := flow.New(cfg, v, func(v cue.Value) (flow.Runner, error) {
			id, err := v.LookupPath(cue.ParsePath("$id")).String()
			if err != nil {
				return nil, nil
			}
			return runners[id], nil
		})
		go c.Run(t.Context())

		// A service which is still running holds back background work.
		<-serveDone
		synctest.Wait()
		select {
		case <-started:
			t.Error("background work started while a service was still running")
		default:
		}

		// A service waiting on another task, which might be a client
		// of the services which already ran, does not hold it back.
		close(releaseSlowServe)
		<-started
	})
}

type resultRunner struct{}

func (resultRunner) Run(ctx *task.Context) (any, error) {
	return map[string]any{"success": true}, nil
}

// TestReleaseEarlierConfigurations runs a chain of tasks through the
// runners registered with this package. Each task runs against the
// configuration computed after the previous task's result. Only the
// runners are kept alive once the flow is done: a runner outlives its
// Run calls, and must not keep the configuration it ran against.
func TestReleaseEarlierConfigurations(t *testing.T) {
	task.Register("test/result", func(v cue.Value) (task.Runner, error) {
		return resultRunner{}, nil
	})
	v := cuecontext.New().CompileString(`
		import "list"

		command: run: {
			t0: {$id: "test/result", path: "x-0", success?: bool}
			for i in list.Range(1, 10, 1) {
				"t\(i)": {
					$id:      "test/result"
					path:     "x-\(i)-\(command.run["t\(i-1)"].success)"
					success?: bool
				}
			}
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	// Each completed task leads to a new configuration, which the
	// completed task's value belongs to. The initial configuration
	// is left out, as v keeps it alive regardless.
	initial := rootOf(v)
	var configs []weak.Pointer[adt.Vertex]
	cfg := &flow.Config{
		Root: cue.ParsePath("command.run"),
		UpdateFunc: func(_ *flow.Controller, ft *flow.Task) error {
			if ft != nil {
				if root := rootOf(ft.Value()); root != initial {
					configs = append(configs, weak.Make(root))
				}
			}
			return nil
		},
	}
	ctx := task.Context{
		TaskKey: func(v cue.Value) (string, error) {
			id := v.LookupPath(cue.ParsePath("$id"))
			if !id.Exists() {
				return "", nil
			}
			return id.String()
		},
	}
	newTask := ctx.TaskFunc(new(atomic.Bool))
	var runners []flow.Runner
	taskFunc := func(v cue.Value) (flow.Runner, error) {
		r, err := newTask(v)
		if r != nil {
			runners = append(runners, r)
		}
		return r, err
	}
	// The controller is not kept alive, as it holds the latest
	// configuration itself. Run in a bubble, so that the goroutines
	// of the finished tasks have exited before the garbage collection.
	synctest.Test(t, func(t *testing.T) {
		if err := flow.New(cfg, v, taskFunc).Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	if len(configs) != 10 {
		t.Fatalf("collected %d configurations, want 10", len(configs))
	}
	runtime.GC()
	retained := 0
	for _, wp := range configs {
		if wp.Value() != nil {
			retained++
		}
	}
	runtime.KeepAlive(runners)
	if retained != 0 {
		t.Fatalf("%d of %d configurations stay reachable", retained, len(configs))
	}
}

// rootOf returns the root of the configuration v belongs to.
func rootOf(v cue.Value) *adt.Vertex {
	root := value.Vertex(v)
	for root.Parent != nil {
		root = root.Parent
	}
	return root
}
