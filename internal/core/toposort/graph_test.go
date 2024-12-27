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
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/toposort"
)

func TestSort(t *testing.T) {
	type MergeTestCase struct {
		name     string
		inputs   [][]string
		expected []string
	}

	a, b, c, d, e, f, g, h := "a", "b", "c", "d", "e", "f", "g", "h"

	testCases := []MergeTestCase{
		{
			name:     "simple two",
			inputs:   [][]string{{c, b}, {d, a}},
			expected: []string{c, b, d, a},
		},
		{
			name:     "simple three",
			inputs:   [][]string{{c, b}, {d, a}, {f, e}},
			expected: []string{c, b, d, a, f, e},
		},
		{
			name:     "linked linear two",
			inputs:   [][]string{{b, c}, {c, a}},
			expected: []string{b, c, a},
		},
		{
			name:     "linked linear two multiple",
			inputs:   [][]string{{b, c, f, d, g}, {c, a, e, d}},
			expected: []string{b, c, a, e, f, d, g},
		},
		{
			name:     "linked linear three",
			inputs:   [][]string{{b, c}, {c, d, a, f}, {a, f, e}},
			expected: []string{b, c, d, a, f, e},
		},
		{
			name:     "simple cycle",
			inputs:   [][]string{{h, b, a}, {a, b}, {h, c, d}, {d, c}},
			expected: []string{h, b, a, c, d},
		},
		{
			name:     "nested cycles",
			inputs:   [][]string{{g, b, c}, {e, c, b, d}, {d, f, a, e}, {a, h, f}},
			expected: []string{g, b, d, f, a, e, c, h},
		},
		{
			name: "fully connected 4",
			inputs: [][]string{
				{a, b, c, d}, {d, c, b, a}, {b, d, a, c}, {c, a, d, b},
			},
			expected: []string{a, b, c, d},
		},
	}

	index := runtime.New()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testAllPermutations(t, index, tc.inputs,
				func(t *testing.T, perm [][]adt.Feature, graph *toposort.Graph) {
					sortedNames := featuresNames(index, graph.Sort(index))
					if !slices.Equal(sortedNames, tc.expected) {
						t.Fatalf(`
For permutation: %v
       Expected: %v
            Got: %v`,
							permutationNames(index, perm), tc.expected, sortedNames)
					}
				})
		})
	}
}

func TestSortRandom(t *testing.T) {
	seed := rand.Int63()
	if str := os.Getenv("SEED"); str != "" {
		num, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			t.Fatalf("Could not parse SEED env var %q: %v", str, err)
			return
		}
		seed = num
	}
	t.Log("Seed", seed)
	rng := rand.New(rand.NewSource(seed))

	names := strings.Split("abcdefghijklm", "")
	index := runtime.New()

	for n := 0; n < 100; n++ {
		inputs := make([][]string, 2+rng.Intn(4))
		for i := range inputs {
			names := slices.Clone(names)
			rng.Shuffle(len(names),
				func(i, j int) { names[i], names[j] = names[j], names[i] })
			inputs[i] = names[:2+rng.Intn(4)]
		}

		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Log("inputs:", inputs)

			var expected []string
			testAllPermutations(t, index, inputs,
				func(t *testing.T, perm [][]adt.Feature, graph *toposort.Graph) {
					sortedNames := featuresNames(index, graph.Sort(index))
					if expected == nil {
						expected = sortedNames
						t.Log("First result:", expected)
						usedNames := make(map[string]struct{}, len(expected))
						for _, name := range expected {
							usedNames[name] = struct{}{}
						}
						for _, input := range inputs {
							for _, name := range input {
								if _, found := usedNames[name]; !found {
									t.Fatalf(`
Input %v contains name %q, but that does not appear in the output: %v`,
										input, name, expected)
								}
							}
						}
					} else if !slices.Equal(sortedNames, expected) {
						t.Fatalf(`
For permutation: %v
       Expected: %v
            Got: %v`,
							permutationNames(index, perm), expected, sortedNames)
					}
				})
		})
	}
}

func makeFeatures(index adt.StringIndexer, inputs [][]string) [][]adt.Feature {
	result := make([][]adt.Feature, len(inputs))
	for i, names := range inputs {
		features := make([]adt.Feature, len(names))
		for j, name := range names {
			features[j] = adt.MakeStringLabel(index, name)
		}
		result[i] = features
	}
	return result
}

// Consider that names are nodes in a cycle, we want to rotate the
// slice so that it starts at the given node name. This modifies the
// names slice in-place.
func rotateToStartAt(names []string, start string) {
	if start == names[0] {
		return
	}
	for i, node := range names {
		if start == node {
			prefix := slices.Clone(names[:i])
			copy(names, names[i:])
			copy(names[len(names)-i:], prefix)
			break
		}
	}
}

func allPermutations(featureses [][]adt.Feature) [][][]adt.Feature {
	nonNilIdx := -1
	var results [][][]adt.Feature
	for i, features := range featureses {
		if features == nil {
			continue
		}
		nonNilIdx = i
		featureses[i] = nil
		for _, result := range allPermutations(featureses) {
			results = append(results, append(result, features))
		}
		featureses[i] = features
	}
	if len(results) == 0 && nonNilIdx != -1 {
		return [][][]adt.Feature{{featureses[nonNilIdx]}}
	}
	return results
}

func permutationNames(index adt.StringIndexer, permutation [][]adt.Feature) [][]string {
	permNames := make([][]string, len(permutation))
	for i, features := range permutation {
		permNames[i] = featuresNames(index, features)
	}
	return permNames
}

func featuresNames(index adt.StringIndexer, features []adt.Feature) []string {
	names := make([]string, len(features))
	for i, feature := range features {
		names[i] = feature.StringValue(index)
	}
	return names
}

func buildGraphFromPermutation(permutation [][]adt.Feature) *toposort.Graph {
	builder := toposort.NewGraphBuilder(true)

	for _, chain := range permutation {
		if len(chain) == 0 {
			continue
		}

		prev := chain[0]
		builder.EnsureNode(prev)
		for _, cur := range chain[1:] {
			builder.AddEdge(prev, cur)
			prev = cur
		}
	}
	return builder.Build()
}

func testAllPermutations(t *testing.T, index adt.StringIndexer, inputs [][]string, fun func(*testing.T, [][]adt.Feature, *toposort.Graph)) {
	features := makeFeatures(index, inputs)
	for i, permutation := range allPermutations(features) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			graph := buildGraphFromPermutation(permutation)
			fun(t, permutation, graph)
		})
	}
}

func TestAllPermutations(t *testing.T) {
	a, b, c, d := []string{"a"}, []string{"b"}, []string{"c"}, []string{"d"}

	type PermutationTestCase struct {
		name     string
		inputs   [][]string
		expected [][][]string
	}

	testCases := []PermutationTestCase{
		{
			name: "empty",
		},
		{
			name:     "one",
			inputs:   [][]string{a},
			expected: [][][]string{{a}},
		},
		{
			name:     "two",
			inputs:   [][]string{a, b},
			expected: [][][]string{{b, a}, {a, b}},
		},
		{
			name:   "three",
			inputs: [][]string{a, b, c},
			expected: [][][]string{
				{c, b, a}, {b, c, a}, {c, a, b}, {a, c, b}, {b, a, c}, {a, b, c},
			},
		},
		{
			name:   "four",
			inputs: [][]string{a, b, c, d},
			expected: [][][]string{
				{d, c, b, a}, {c, d, b, a}, {d, b, c, a}, {b, d, c, a}, {c, b, d, a}, {b, c, d, a},
				{d, c, a, b}, {c, d, a, b}, {d, a, c, b}, {a, d, c, b}, {c, a, d, b}, {a, c, d, b},
				{d, b, a, c}, {b, d, a, c}, {d, a, b, c}, {a, d, b, c}, {b, a, d, c}, {a, b, d, c},
				{c, b, a, d}, {b, c, a, d}, {c, a, b, d}, {a, c, b, d}, {b, a, c, d}, {a, b, c, d},
			},
		},
	}

	index := runtime.New()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs := makeFeatures(index, tc.inputs)
			permutations := allPermutations(fs)
			permutationsNames := make([][][]string, len(permutations))
			for i, permutation := range permutations {
				permutationsNames[i] = permutationNames(index, permutation)
			}

			if !slices.EqualFunc(permutationsNames, tc.expected,
				func(gotPerm, expectedPerm [][]string) bool {
					return slices.EqualFunc(gotPerm, expectedPerm, slices.Equal)
				}) {
				t.Fatalf(`
For inputs: %v
  Expected: %v
       Got: %v`,
					tc.inputs, tc.expected, permutations)
			}
		})
	}
}
