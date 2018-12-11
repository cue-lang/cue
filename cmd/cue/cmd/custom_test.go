// Copyright 2018 The CUE Authors
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

package cmd

import (
	"strconv"
	"strings"
	"testing"
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
		tasks: "0",
		cycle: true,
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
	}}
	for _, tc := range testCases {
		t.Run(tc.tasks, func(t *testing.T) {
			deps := strings.Split(tc.tasks, ";")
			tasks := make([]*task, len(deps))
			for i := range tasks {
				tasks[i] = &task{index: i, dep: map[*task]bool{}}
			}
			for i, d := range deps {
				if d == "" {
					continue
				}
				for _, num := range strings.Split(d, ",") {
					num = strings.TrimSpace(num)
					if num == "" {
						continue
					}
					x, err := strconv.Atoi(num)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("%d -> %d", i, x)
					tasks[i].dep[tasks[x]] = true
				}
			}
			if got := isCyclic(tasks); got != tc.cycle {
				t.Errorf("got %v; want %v", got, tc.cycle)
			}
		})
	}
}
