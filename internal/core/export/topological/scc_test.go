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

package topological_test

import (
	"slices"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/export/topological"
	"cuelang.org/go/internal/core/runtime"
)

func TestStronglyConnectedComponents(t *testing.T) {
	type TestCase struct {
		name     string
		inputs   [][]string
		expected [][]string
	}

	a, b, c, d, e, f, g := "a", "b", "c", "d", "e", "f", "g"

	testCases := []TestCase{
		{
			name:     "one",
			inputs:   [][]string{{a}},
			expected: [][]string{{a}},
		},
		{
			name:     "independent",
			inputs:   [][]string{{a}, {b}, {c}},
			expected: [][]string{{a}, {b}, {c}},
		},
		{
			name:     "simple chain two",
			inputs:   [][]string{{c, b}, {d, a}},
			expected: [][]string{{a}, {b}, {c}, {d}},
		},
		{
			name:     "simple chain three",
			inputs:   [][]string{{c, b}, {d, a}, {f, e}},
			expected: [][]string{{a}, {b}, {c}, {d}, {e}, {f}},
		},
		{
			name:     "smallest cycle",
			inputs:   [][]string{{g, f}, {f, g}},
			expected: [][]string{{f, g}},
		},
		{
			name:     "smallest cycle with prefix",
			inputs:   [][]string{{a, b, g, f}, {f, g}},
			expected: [][]string{{a}, {b}, {f, g}},
		},
		{
			name:     "smallest cycle with suffix",
			inputs:   [][]string{{g, f, a, b}, {f, g}},
			expected: [][]string{{a}, {b}, {f, g}},
		},
		{
			name:     "smallest cycle with prefices",
			inputs:   [][]string{{a, b, g, f}, {c, d, f, g}},
			expected: [][]string{{a}, {b}, {c}, {d}, {f, g}},
		},
		{
			name:     "smallest cycle with suffices",
			inputs:   [][]string{{g, f, a, b}, {f, g, c, d}},
			expected: [][]string{{a}, {b}, {c}, {d}, {f, g}},
		},
		{
			name:     "smallest cycle with prefices and sufficies",
			inputs:   [][]string{{a, g, f}, {b, f}, {g, c}, {f, g, d}},
			expected: [][]string{{a}, {b}, {c}, {d}, {f, g}},
		},
		{
			name:     "nested cycles",
			inputs:   [][]string{{b, c}, {e, c, b, d}, {d, f, a, e}, {a, f}},
			expected: [][]string{{a, b, c, d, e, f}},
		},
		{
			name:     "cycles through common node",
			inputs:   [][]string{{a, b, c}, {c, a}, {f, b, g}, {g, f}},
			expected: [][]string{{a, b, c, f, g}},
		},
		{
			name:     "split",
			inputs:   [][]string{{a, b, c}, {a, d, e}, {c, b}, {e, d}},
			expected: [][]string{{a}, {b, c}, {d, e}},
		},
	}

	indexer := runtime.New()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testAllPermutations(t, indexer, testCase.inputs, func(t *testing.T, permutation [][]adt.Feature, graph *topological.Graph) {
				components := graph.StronglyConnectedComponents()

				componentsNames := make([][]string, len(components))
				for idx, component := range components {
					features := component.Nodes.Features()
					names := make([]string, len(features))
					for idy, feature := range features {
						names[idy] = feature.StringValue(indexer)
					}
					slices.Sort(names)
					componentsNames[idx] = names
				}
				slices.SortFunc(componentsNames, compareStringses)

				if !slices.EqualFunc(componentsNames, testCase.expected, slices.Equal) {
					t.Fatalf("\nFor permutation: %v\n       Expected: %v\n            Got: %v", permutationNames(indexer, permutation), testCase.expected, componentsNames)
				}

				seen := make(map[*topological.StronglyConnectedComponent]struct{})
				// by definition, the graph of components (the "condensation graph") must be a DAG. I.e. no cycles
				// the components are already sorted in a topological ordering
				for _, component := range components {
					if _, found := seen[component]; found {
						t.Fatalf("\nFor permutation: %v\n       List of components contains the same component twice: %v", permutationNames(indexer, permutation), component)
					}
					seen[component] = struct{}{}
					for _, next := range component.Outgoing {
						if _, found := seen[next]; found {
							t.Fatalf("\nFor permutation: %v\n       Either the list of components is not topologically sorted, or the condensation graph is not a DAG!\n       Component: %v\n       Outgoing: %v",
								permutationNames(indexer, permutation), component.Nodes, next.Nodes)
						}
					}
				}
			})
		})
	}
}
