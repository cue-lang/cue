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
	"slices"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/toposort"
)

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

func compareStringses(a, b []string) int {
	lim := min(len(a), len(b))
	for i := 0; i < lim; i++ {
		if comparison := cmp.Compare(a[i], b[i]); comparison != 0 {
			return comparison
		}
	}
	return cmp.Compare(len(a), len(b))
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
