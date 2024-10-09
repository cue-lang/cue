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
	"cmp"
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
			name:     "fully connected 4",
			inputs:   [][]string{{a, b, c, d}, {d, c, b, a}, {b, d, a, c}, {c, a, d, b}},
			expected: []string{a, b, c, d},
		},
	}

	indexer := runtime.New()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testAllPermutations(t, indexer, testCase.inputs, func(t *testing.T, permutation [][]adt.Feature, graph *topological.Graph) {
				sortedNames := featuresNames(indexer, graph.Sort(indexer))
				if !slices.Equal(sortedNames, testCase.expected) {
					t.Fatalf("\nFor permutation: %v\n       Expected: %v\n            Got: %v", permutationNames(indexer, permutation), testCase.expected, sortedNames)
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
	indexer := runtime.New()

	for n := 0; n < 100; n++ {
		inputs := make([][]string, 2+rng.Intn(4))
		for idx := range inputs {
			names := slices.Clone(names)
			rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
			inputs[idx] = names[:2+rng.Intn(4)]
		}

		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Log("inputs:", inputs)

			var expected []string
			testAllPermutations(t, indexer, inputs, func(t *testing.T, permutation [][]adt.Feature, graph *topological.Graph) {
				sortedNames := featuresNames(indexer, graph.Sort(indexer))
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
								t.Fatalf("\nInput %v contains name %q, but that does not appear in the output: %v", input, name, expected)
							}
						}
					}
				} else if !slices.Equal(sortedNames, expected) {
					t.Fatalf("\nFor permutation: %v\n       Expected: %v\n            Got: %v", permutationNames(indexer, permutation), expected, sortedNames)
				}
			})
		})
	}
}

func makeFeatures(indexer adt.StringIndexer, inputs [][]string) [][]adt.Feature {
	result := make([][]adt.Feature, len(inputs))
	for idx, names := range inputs {
		features := make([]adt.Feature, len(names))
		for idy, name := range names {
			features[idy] = adt.MakeStringLabel(indexer, name)
		}
		result[idx] = features
	}
	return result
}

func compareStringses(a, b []string) int {
	lim := min(len(a), len(b))
	for idx := 0; idx < lim; idx++ {
		if comparison := cmp.Compare(a[idx], b[idx]); comparison != 0 {
			return comparison
		}
	}
	return cmp.Compare(len(a), len(b))
}

// Consider that names are nodes in a cycle, we want to rotate the
// slice so that it starts at the given node name. This modifies the
// names slice in-place.
func rotateToStartAt(names []string, start string) {
	if start != names[0] {
		for idx, node := range names {
			if start == node {
				prefix := slices.Clone(names[:idx])
				copy(names, names[idx:])
				copy(names[len(names)-idx:], prefix)
				break
			}
		}
	}
}

func allPermutations(featureses [][]adt.Feature) [][][]adt.Feature {
	nonNilIdx := -1
	var results [][][]adt.Feature
	for idx, feature := range featureses {
		if feature == nil {
			continue
		}
		nonNilIdx = idx
		featureses[idx] = nil
		for _, result := range allPermutations(featureses) {
			results = append(results, append(result, feature))
		}
		featureses[idx] = feature
	}
	if len(results) == 0 && nonNilIdx != -1 {
		return [][][]adt.Feature{{featureses[nonNilIdx]}}
	}
	return results
}

func permutationNames(indexer adt.StringIndexer, permutation [][]adt.Feature) [][]string {
	permNames := make([][]string, len(permutation))
	for idx, features := range permutation {
		permNames[idx] = featuresNames(indexer, features)
	}
	return permNames
}

func featuresNames(indexer adt.StringIndexer, features []adt.Feature) []string {
	names := make([]string, len(features))
	for idy, feature := range features {
		names[idy] = feature.StringValue(indexer)
	}
	return names
}

func buildGraphFromPermutation(permutation [][]adt.Feature) *toposort.Graph {
	builder := toposort.NewGraphBuilder()

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

func testAllPermutations(t *testing.T, indexer adt.StringIndexer, inputs [][]string, fun func(*testing.T, [][]adt.Feature, *toposort.Graph)) {
	features := makeFeatures(indexer, inputs)
	for permIdx, permutation := range allPermutations(features) {
		t.Run(fmt.Sprint(permIdx), func(t *testing.T) {
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
			name:     "three",
			inputs:   [][]string{a, b, c},
			expected: [][][]string{{c, b, a}, {b, c, a}, {c, a, b}, {a, c, b}, {b, a, c}, {a, b, c}},
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

	indexer := runtime.New()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			features := makeFeatures(indexer, testCase.inputs)
			permutations := allPermutations(features)
			permutationsNames := make([][][]string, len(permutations))
			for idx, permutation := range permutations {
				permutationsNames[idx] = permutationNames(indexer, permutation)
			}

			if !slices.EqualFunc(permutationsNames, testCase.expected, func(gotPerm, expectedPerm [][]string) bool {
				return slices.EqualFunc(gotPerm, expectedPerm, slices.Equal)
			}) {
				t.Fatalf("\nFor inputs: %v\n  Expected: %v\n       Got: %v", testCase.inputs, testCase.expected, permutations)
			}
		})
	}
}
