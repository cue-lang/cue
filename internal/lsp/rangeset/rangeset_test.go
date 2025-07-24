// Copyright 2025 CUE Authors
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

package rangeset_test

import (
	"testing"

	"cuelang.org/go/internal/lsp/rangeset"
	"github.com/go-quicktest/qt"
)

func TestAdd(t *testing.T) {
	testCases := []struct {
		name     string
		add      []rangeset.Range
		expected []rangeset.Range
	}{
		// --- Basic Cases ---
		{
			name:     "Add to empty set",
			add:      []rangeset.Range{{10, 20}},
			expected: []rangeset.Range{{10, 20}},
		},
		{
			name:     "Add non-overlapping range after",
			add:      []rangeset.Range{{10, 20}, {30, 40}},
			expected: []rangeset.Range{{10, 20}, {30, 40}},
		},
		{
			name:     "Add non-overlapping range before",
			add:      []rangeset.Range{{30, 40}, {10, 20}},
			expected: []rangeset.Range{{10, 20}, {30, 40}},
		},
		{
			name:     "Add non-overlapping range in a gap",
			add:      []rangeset.Range{{10, 20}, {50, 60}, {30, 40}},
			expected: []rangeset.Range{{10, 20}, {30, 40}, {50, 60}},
		},

		// --- Merging and Extending ---
		{
			name:     "Extend existing range to the right (overlap)",
			add:      []rangeset.Range{{10, 20}, {15, 25}},
			expected: []rangeset.Range{{10, 25}},
		},
		{
			name:     "Extend existing range to the left (overlap)",
			add:      []rangeset.Range{{10, 20}, {5, 15}},
			expected: []rangeset.Range{{5, 20}},
		},
		{
			name:     "Extend existing range to the right (touching)",
			add:      []rangeset.Range{{10, 20}, {20, 30}},
			expected: []rangeset.Range{{10, 30}},
		},
		{
			name:     "Extend existing range to the left (touching)",
			add:      []rangeset.Range{{10, 20}, {0, 10}},
			expected: []rangeset.Range{{0, 20}},
		},
		{
			name:     "Bridge two existing ranges",
			add:      []rangeset.Range{{10, 20}, {30, 40}, {18, 32}},
			expected: []rangeset.Range{{10, 40}},
		},
		{
			name:     "Bridge two existing ranges by touching",
			add:      []rangeset.Range{{10, 20}, {30, 40}, {20, 30}},
			expected: []rangeset.Range{{10, 40}},
		},
		{
			name:     "Merge three ranges",
			add:      []rangeset.Range{{10, 20}, {30, 40}, {50, 60}, {15, 55}},
			expected: []rangeset.Range{{10, 60}},
		},

		// --- Containment Cases ---
		{
			name:     "Add range fully contained within an existing range",
			add:      []rangeset.Range{{10, 50}, {20, 30}},
			expected: []rangeset.Range{{10, 50}},
		},
		{
			name:     "Add range that fully contains an existing range",
			add:      []rangeset.Range{{20, 30}, {10, 40}},
			expected: []rangeset.Range{{10, 40}},
		},
		{
			name:     "Add range that fully contains multiple existing ranges",
			add:      []rangeset.Range{{10, 20}, {30, 40}, {5, 45}},
			expected: []rangeset.Range{{5, 45}},
		},

		// --- Edge Cases ---
		{
			name:     "Add an identical range",
			add:      []rangeset.Range{{10, 20}, {10, 20}},
			expected: []rangeset.Range{{10, 20}},
		},
		{
			name:     "Add an invalid range (start >= end)",
			add:      []rangeset.Range{{10, 20}, {30, 30}},
			expected: []rangeset.Range{{10, 20}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rangeset.NewRangeSet()

			for _, r := range tc.add {
				got.Add(r.Start, r.End)
			}

			want := rangeset.NewRangeSet()
			for _, r := range tc.expected {
				want.Add(r.Start, r.End)
			}

			qt.Assert(t, qt.DeepEquals(got.Ranges(), want.Ranges()))
		})
	}
}

func TestContains(t *testing.T) {
	rs := rangeset.NewRangeSet()
	rs.Add(10, 20)
	rs.Add(30, 40)
	rs.Add(50, 100)

	testCases := []struct {
		name     string
		offset   int
		expected bool
	}{
		// --- Outside ranges ---
		{name: "Point before all ranges", offset: 5, expected: false},
		{name: "Point after all ranges", offset: 101, expected: false},
		{name: "Point in a gap", offset: 25, expected: false},
		{name: "Point in another gap", offset: 45, expected: false},

		// --- Inside ranges ---
		{name: "Point inside first range", offset: 15, expected: true},
		{name: "Point inside second range", offset: 33, expected: true},
		{name: "Point inside third range", offset: 75, expected: true},

		// --- Boundary conditions ---
		{name: "Point on start boundary of first range", offset: 10, expected: true},
		{name: "Point on end boundary of first range", offset: 20, expected: false},
		{name: "Point on start boundary of middle range", offset: 30, expected: true},
		{name: "Point on end boundary of middle range", offset: 40, expected: false},
		{name: "Point on start boundary of last range", offset: 50, expected: true},
		{name: "Point on end boundary of last range", offset: 100, expected: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rs.Contains(tc.offset)

			qt.Assert(t, qt.Equals(got, tc.expected))
		})
	}
}
