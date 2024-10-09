// Copyright 2024 CUE Authors
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

package toposort_test

import (
	"slices"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/toposort"
)

func TestElementaryCycles(t *testing.T) {
	type TestCase struct {
		name     string
		inputs   [][]string
		expected [][]string
	}

	a, b, c, d, e, f, g := "a", "b", "c", "d", "e", "f", "g"

	testCases := []TestCase{
		{
			name:     "no cycles",
			inputs:   [][]string{{a, b, c}},
			expected: [][]string{},
		},
		{
			name:     "cycle of 2",
			inputs:   [][]string{{a, b}, {b, a}},
			expected: [][]string{{a, b}},
		},
		{
			name:     "cycle of 3",
			inputs:   [][]string{{a, b}, {b, c}, {c, a}},
			expected: [][]string{{a, b, c}},
		},
		{
			name:     "cycle of 3 and 4",
			inputs:   [][]string{{a, b, c, d}, {c, a}, {d, b}},
			expected: [][]string{{a, b, c}, {b, c, d}},
		},
		{
			name:     "unlinked cycles",
			inputs:   [][]string{{a, b, c, d}, {d, b}, {e, f, g}, {g, e}},
			expected: [][]string{{b, c, d}, {e, f, g}},
		},
		{
			name: "fully connected 4",
			inputs: [][]string{
				{a, b, c, d}, {d, c, b, a}, {b, d, a, c}, {c, a, d, b},
			},
			expected: [][]string{
				{a, b}, {a, b, c}, {a, b, c, d}, {a, b, d}, {a, b, d, c},
				{a, c}, {a, c, b}, {a, c, b, d}, {a, c, d}, {a, c, d, b},
				{a, d}, {a, d, b}, {a, d, b, c}, {a, d, c}, {a, d, c, b},
				{b, c}, {b, c, d},
				{b, d}, {b, d, c},
				{c, d},
			},
		},
		{
			name:     "nested cycles",
			inputs:   [][]string{{b, c}, {e, c, b, d}, {d, f, a, e}, {a, f}},
			expected: [][]string{{a, e, c, b, d, f}, {a, f}, {b, c}},
		},
	}

	index := runtime.New()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testAllPermutations(t, index, tc.inputs,
				func(t *testing.T, permutation [][]adt.Feature, graph *toposort.Graph) {
					var cyclesNames [][]string
					for _, scc := range graph.StronglyConnectedComponents() {
						for _, cycle := range scc.ElementaryCycles() {
							fs := cycle.Nodes.Features()
							names := make([]string, len(fs))
							for j, f := range fs {
								names[j] = f.StringValue(index)
							}
							cyclesNames = append(cyclesNames, names)
						}
					}
					for _, cycle := range cyclesNames {
						rotateToStartAt(cycle, slices.Min(cycle))
					}
					slices.SortFunc(cyclesNames, compareStringses)

					if !slices.EqualFunc(cyclesNames, tc.expected, slices.Equal) {
						t.Fatalf(`
For permutation: %v
       Expected: %v
            Got: %v`,
							permutationNames(index, permutation),
							tc.expected, cyclesNames)
					}
				})
		})
	}
}
