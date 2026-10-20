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

package flow_test

import (
	"runtime"
	"testing"
	"weak"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/value"
	"cuelang.org/go/tools/flow"
)

// TestReleaseEarlierConfigurations runs a flow whose tasks each return a
// result, so that the configuration is recomputed after every task, and
// checks that each earlier configuration can be collected once replaced.
func TestReleaseEarlierConfigurations(t *testing.T) {
	// Independent tasks all run against the initial configuration. A task
	// which references the result of the previous one runs against the
	// configuration computed after that result, so each task runs against
	// a different configuration.
	for _, tc := range []struct{ name, field string }{
		{"independent", ""},
		{"chained", `in: command.run["t\(i-1)"].stdout`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := cuecontext.New().CompileString(`
				import "list"

				command: run: {
					t0: {$id: "task"}
					for i in list.Range(1, 10, 1) {
						"t\(i)": {$id: "task", ` + tc.field + `}
					}
				}
			`)
			if err := v.Err(); err != nil {
				t.Fatal(err)
			}

			// Each completed task leads to a new configuration, which the
			// completed task's value belongs to.
			var configs []weak.Pointer[adt.Vertex]
			cfg := &flow.Config{
				Root: cue.ParsePath("command.run"),
				UpdateFunc: func(_ *flow.Controller, ft *flow.Task) error {
					if ft != nil {
						configs = append(configs, weak.Make(rootOf(ft.Value())))
					}
					return nil
				},
			}
			c := flow.New(cfg, v, taskFunc)
			if err := c.Run(t.Context()); err != nil {
				t.Fatal(err)
			}
			// The controller keeps the last configuration.
			earlier := configs[:len(configs)-1]
			runtime.GC()
			retained := 0
			for _, wp := range earlier {
				if wp.Value() != nil {
					retained++
				}
			}
			runtime.KeepAlive(c)
			if retained != 0 {
				t.Fatalf("%d of %d earlier configurations stay reachable", retained, len(earlier))
			}
		})
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
