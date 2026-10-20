// Copyright 2020 CUE Authors
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
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/tools/flow"
)

func TestIsCyclic(t *testing.T) {
	testCases := []struct {
		// semi-colon-separated list of nodes with comma-separated list
		// of dependencies.
		tasks string
		cycle bool
	}{{
		tasks: "",
	}, {
		// flow ignores references from a task to itself.
		tasks: "0",
	}, {
		tasks: "1; 0",
		cycle: true,
	}, {
		tasks: "1; 2; 3; 4;",
	}, {
		tasks: "1; 2; ; 4; 5; ",
	}, {
		tasks: "1; 2; 3; 4; 0",
		cycle: true,
	}, {
		tasks: "1,2,3,4; 2,3,4; 3,4; 4;",
	}, {
		tasks: ";0;0,1;0,1,2;0,1,2,3;",
	}, {
		// A two-task cycle between t1 and t2.
		tasks: "1,2,3,4; 2,3,4; 1; 4;",
		cycle: true,
	}}
	for _, tc := range testCases {
		t.Run(tc.tasks, func(t *testing.T) {
			var b, wantDeps strings.Builder
			b.WriteString("root: {\n")
			for i, d := range strings.Split(tc.tasks, ";") {
				var after, deps []string
				for num := range strings.SplitSeq(d, ",") {
					if num = strings.TrimSpace(num); num != "" {
						after = append(after, "t"+num+".$id")
						if num != strconv.Itoa(i) {
							deps = append(deps, "root.t"+num)
						}
					}
				}
				fmt.Fprintf(&b, "\tt%d: {$id: \"valToOut\", $after: [%s]}\n", i, strings.Join(after, ", "))
				fmt.Fprintf(&wantDeps, "root.t%d: %s\n", i, strings.Join(deps, ", "))
			}
			b.WriteString("}\n")
			t.Log(b.String())

			v := cuecontext.New().CompileString(b.String())
			if err := v.Err(); err != nil {
				t.Fatal(err)
			}
			c := flow.New(&flow.Config{Root: cue.ParsePath("root")}, v, taskFunc)

			var gotDeps strings.Builder
			for _, task := range c.Tasks() {
				var deps []string
				for _, d := range task.Dependencies() {
					deps = append(deps, d.Path().String())
				}
				slices.Sort(deps)
				fmt.Fprintf(&gotDeps, "%s: %s\n", task.Path(), strings.Join(deps, ", "))
			}
			if got, want := gotDeps.String(), wantDeps.String(); got != want {
				t.Errorf("got dependencies:\n%s\nwant:\n%s", got, want)
			}

			err := c.Run(t.Context())
			if tc.cycle {
				if err == nil || !strings.Contains(err.Error(), "cyclic task dependency") {
					t.Errorf("got error %v; want a cyclic task dependency", err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
