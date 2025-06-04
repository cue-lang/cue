// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt

import (
	"slices"
	"testing"
)

func TestReplaceIDs(t *testing.T) {
	tests := []struct {
		name     string
		reqSets  reqSets
		replace  []replaceID
		expected reqSets
	}{{
		name: "replace single set",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: 2},
		},
		// The group was already added as a requirement, so the original group
		// should be deleted.
		expected: reqSets{},
	}, {
		name: "empty result",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: deleteID, add: true},
		},
	}, {
		name: "replace first set",
		reqSets: reqSets{
			{id: 1, size: 2},
			{id: 2},
			{id: 3, size: 2},
			{id: 4},
		},
		replace: []replaceID{
			{from: 1, to: 5},
		},
		expected: reqSets{
			{id: 3, size: 2},
			{id: 4},
		},
	}, {
		name: "replace last set",
		reqSets: reqSets{
			{id: 1, size: 2},
			{id: 2},
			{id: 3, size: 2},
			{id: 4},
		},
		replace: []replaceID{
			{from: 3, to: 5},
		},
		expected: reqSets{
			{id: 1, size: 2},
			{id: 2},
		},
	}, {
		name: "replace multiple ids",
		reqSets: reqSets{
			{id: 1, size: 1},
			{id: 2, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: 3},
			{from: 2, to: 4},
		},
		expected: reqSets{},
	}, {
		name: "replace with zero id",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: deleteID},
		},
		expected: reqSets{},
	}, {
		name: "replace equivalent",
		reqSets: reqSets{
			{id: 1, size: 2},
			{id: 2}, // e.g. from embedding
		},
		replace: []replaceID{
			{from: 2, to: 3}, // replacing an embedding is additive.
		},
		expected: reqSets{
			{id: 1, size: 3},
			{id: 2},
			{id: 3},
		},
	}, {
		name: "no replacement",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		replace: []replaceID{},
		expected: reqSets{
			{id: 1, size: 1},
		},
	}, {
		name: "remove multiple from equivalence set",
		reqSets: reqSets{
			{id: 1, size: 4},
			{id: 2},
			{id: 3},
			{id: 4},
			{id: 5, size: 1},
		},
		replace: []replaceID{
			{from: 4, to: deleteID},
			{from: 2, to: deleteID},
		},
		expected: reqSets{
			{id: 1, size: 2},
			{id: 3},
			{id: 5, size: 1},
		},
	}, {
		name: "add new id to existing set",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: 2, add: true},
		},
		expected: reqSets{
			{id: 1, size: 2},
			{id: 2},
		},
	}, {
		name: "add new id to multiple sets",
		reqSets: reqSets{
			{id: 1, size: 1},
			{id: 3, size: 1},
		},
		replace: []replaceID{
			{from: 3, to: 4, add: true},
			{from: 1, to: 2, add: true},
		},
		expected: reqSets{
			{id: 1, size: 2},
			{id: 2},
			{id: 3, size: 2},
			{id: 4},
		},
	}, {
		name:    "add new id to empty set",
		reqSets: reqSets{},
		replace: []replaceID{
			{from: 0, to: 1, add: true},
		},
		expected: reqSets{},
	}, {
		name: "add new id to non-existent set",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		replace: []replaceID{
			{from: 2, to: 3, add: true},
		},
		expected: reqSets{
			{id: 1, size: 1},
		},
	}, {
		name: "add then delete",
		reqSets: reqSets{
			{id: 1, size: 1}, // delete this
			{id: 3, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: 2, add: true},
			{from: 1, to: deleteID},
		},
		expected: reqSets{
			{id: 3, size: 1},
		},
	}, {
		name: "delete then add",
		reqSets: reqSets{
			{id: 1, size: 1}, // delete this
			{id: 3, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: deleteID},
			{from: 1, to: 2, add: true},
		},
		expected: reqSets{
			{id: 3, size: 1},
		},
	}, {
		name: "fixed point",
		reqSets: reqSets{
			{id: 1, size: 1},
			{id: 4, size: 2},
			{id: 1},
		},
		replace: []replaceID{
			{from: 1, to: 2, add: true},
			{from: 2, to: 3, add: true},
			{from: 3, to: 4, add: true},
		},
		expected: reqSets{
			{id: 1, size: 4},
			{id: 2},
			{id: 3},
			{id: 4},
			{id: 4, size: 4},
			{id: 1},
			{id: 2},
			{id: 3},
		},
	}, {
		name: "fixed point with jumps",
		reqSets: reqSets{
			{id: 4, size: 1},
			{id: 1, size: 1},
		},
		replace: []replaceID{
			{from: 1, to: 3, add: true},
			{from: 2, to: 1, add: true},
			{from: 3, to: 2, add: true},
		},
		expected: reqSets{
			{id: 4, size: 1},
			{id: 1, size: 3},
			{id: 3}, // TODO: maybe order?
			{id: 2},
		},
	}, {
		name: "fixed idempotent",
		reqSets: reqSets{
			{id: 1, size: 3},
			{id: 3},
			{id: 2},
			{id: 4, size: 2},
			{id: 1},
		},
		replace: []replaceID{
			{from: 1, to: 3, add: true},
			{from: 2, to: 1, add: true},
			{from: 3, to: 2, add: true},
		},
		expected: reqSets{
			{id: 1, size: 3},
			{id: 3},
			{id: 2},
			{id: 4, size: 4},
			{id: 1},
			{id: 3},
			{id: 2},
		},
	}, {
		name: "add and drop",
		reqSets: reqSets{
			// A main group needs to be fully deleted in case of a replacement.
			// This corresponds to that #B can be dropped as a requirement
			// for `c` in `#B: c: #A` when replacing it with #A.
			{id: 1, size: 1}, // add to this set.
			{id: 2, size: 2}, // drop this set.
			// A replacement of an equivalent id should just add the new id.
			// This corresponds to embeddings being additive.
			{id: 1},
		},
		replace: []replaceID{
			// A main group needs to be fully deleted in case of a replacement.
			// This corresponds to that #B can be dropped as a requirement
			// for `c` in `#B: c: #A` when replacing it with #A.
			{from: 1, to: 3, add: true},
			// A replacement of an equivalent id should just add the new id.
			// This corresponds to embeddings being additive.
			{from: 2, to: 3},
		},
		expected: reqSets{
			{id: 1, size: 2},
			{id: 3},
		},
	}, {
		name: "drop and add",
		reqSets: reqSets{
			{id: 1, size: 1},
			{id: 2, size: 2},
			{id: 1},
		},
		replace: []replaceID{
			{from: 1, to: 3},
			{from: 1, to: 3, add: true},
		},
		expected: reqSets{
			{id: 2, size: 3},
			{id: 1},
			{id: 3},
		},
	}, {
		name: "cycle",
		reqSets: []reqSet{
			{id: 1, size: 1},
			{id: 2, size: 1},
			{id: 3, size: 1, embed: 2},
			{id: 4, size: 1, embed: 2},
		},
		replace: []replaceID{
			{from: 1, to: 2, add: true}, // , headOnly: true},
			{from: 2, to: 3, add: true},
			{from: 2, to: 4, add: true},
			{from: 3, to: 1, add: true},
			{from: 4, to: 1, add: true},
		},
		expected: reqSets{
			{id: 1, size: 4},
			{id: 2},
			{id: 3},
			{id: 4},
			{id: 2, size: 4},
			{id: 3},
			{id: 1},
			{id: 4},
			{id: 3, size: 2, embed: 2},
			{id: 1},
			{id: 4, size: 2, embed: 2},
			{id: 1},
		},
	}, {
		name: "exclude 1",
		reqSets: []reqSet{
			{id: 3, size: 1, embed: 2},
		},
		replace: []replaceID{
			{from: 1, to: 2, add: true},
			{from: 2, to: 3, add: true},
			{from: 3, to: 1, add: true},
		},
		expected: reqSets{
			{id: 3, size: 2, embed: 2},
			{id: 1},
		},
	}, {
		name: "exclude 2",
		reqSets: []reqSet{
			{id: 5, size: 1},
			{id: 6, size: 1},
			{id: 7, size: 1, embed: 6},
			{id: 8, size: 1, embed: 6},
		},
		replace: []replaceID{
			{from: 5, to: 6, add: true},
			{from: 6, to: 7, add: true},
			{from: 6, to: 8, add: true},
			{from: 7, to: 0, add: true},
			{from: 8, to: 0, add: true},
		},
		expected: reqSets{
			{id: 5, size: 4},
			{id: 6},
			{id: 7},
			{id: 8},
			{id: 6, size: 3},
			{id: 7},
			{id: 8},
			{id: 7, size: 2, embed: 6},
			{id: 0},
			{id: 8, size: 2, embed: 6},
			{id: 0},
		},
	}, {
		name: "exclude 3",
		reqSets: []reqSet{
			{id: 5, size: 1},
			{id: 8, size: 1, embed: 7},
			{id: 9, size: 1, embed: 7},
		},
		replace: []replaceID{
			{from: 5, to: 6, add: true},
			{from: 6, to: 7, add: true},
			{from: 7, to: 8, add: true},
			{from: 7, to: 9, add: true},
			{from: 8, to: 6, add: true},
			{from: 9, to: 6, add: true},
		},
		expected: reqSets{
			{id: 5, size: 5},
			{id: 6},
			{id: 7},
			{id: 8},
			{id: 9},
			{id: 8, size: 2, embed: 7},
			{id: 6},
			{id: 9, size: 2, embed: 7},
			{id: 6},
		},
	}, {
		name: "exclude 4",
		// represents
		// #a: [>="k"]: int // 11
		// #b: [<="m"]: int // 12
		// #c: [>="w"]: int // 13
		// #d: [<="y"]: int // 14
		// X: { // 8
		// 		#a & #b // 9
		// 		#c & #d // 10
		// }
		// ignored groups (9, 10), are omitted.
		reqSets: []reqSet{
			{id: 8, size: 1},
			{id: 11, size: 1, embed: 9},
			{id: 12, size: 1, embed: 9},
			{id: 13, size: 1, embed: 10},
			{id: 14, size: 1, embed: 10},
		},
		replace: []replaceID{
			{from: 8, to: 9, add: true},
			{from: 8, to: 10, add: true},
			{from: 9, to: 11, add: true},
			{from: 11, to: 8, add: true},
			{from: 9, to: 12, add: true},
			{from: 12, to: 8, add: true},
			{from: 10, to: 13, add: true},
			{from: 13, to: 8, add: true},
			{from: 10, to: 14, add: true},
			{from: 14, to: 8, add: true},
		},
		expected: reqSets{
			{id: 8, size: 7},
			{id: 9},
			{id: 11},
			{id: 12},
			{id: 10},
			{id: 13},
			{id: 14},
			{id: 11, size: 5, embed: 9},
			{id: 8},
			{id: 10},
			{id: 13},
			{id: 14},
			{id: 12, size: 5, embed: 9},
			{id: 8},
			{id: 10},
			{id: 13},
			{id: 14},
			{id: 13, size: 5, embed: 10},
			{id: 8},
			{id: 9},
			{id: 11},
			{id: 12},
			{id: 14, size: 5, embed: 10},
			{id: 8},
			{id: 9},
			{id: 11},
			{id: 12},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name != "exclude1" {
				// return
			}
			tt.reqSets.assert()
			tt.expected.assert()

			tt.reqSets.replaceIDs(&OpContext{}, tt.replace...)
			if !slices.Equal(tt.reqSets, tt.expected) {
				t.Errorf("got: \n%v, want:\n%v", tt.reqSets, tt.expected)
			}
		})
	}
}

func TestHasEvidence(t *testing.T) {
	tests := []struct {
		name      string
		reqSets   reqSets
		conjuncts []conjunctInfo
		want      bool
	}{{
		name: "single match",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		conjuncts: []conjunctInfo{
			{id: 1},
		},
		want: true,
	}, {
		name: "no match",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		conjuncts: []conjunctInfo{
			{id: 2},
		},
		want: false,
	}, {
		name: "no conjuncts",
		reqSets: reqSets{
			{id: 1, size: 1},
		},
		conjuncts: []conjunctInfo{},
		want:      false,
	}, {
		name:    "no requirements",
		reqSets: reqSets{},
		conjuncts: []conjunctInfo{
			{id: 2},
		},
		want: true,
	}, {
		name:      "no requirements, no conjuncts",
		reqSets:   reqSets{},
		conjuncts: []conjunctInfo{},
		want:      true,
	}, {
		name: "multiple, all match",
		reqSets: reqSets{
			{id: 1, size: 1},
			{id: 2, size: 1},
		},
		conjuncts: []conjunctInfo{
			{id: 1},
			{id: 2},
		},
		want: true,
	}, {
		name: "multiple, one does not match",
		reqSets: reqSets{
			{id: 1, size: 1},
			{id: 2, size: 1},
		},
		conjuncts: []conjunctInfo{
			{id: 2},
		},
		want: false,
	}, {
		name: "multiset match",
		reqSets: reqSets{
			{id: 1, size: 2},
			{id: 2},
		},
		conjuncts: []conjunctInfo{
			{id: 2},
		},
		want: true,
	}, {
		name: "multiset no match",
		reqSets: reqSets{
			{id: 1, size: 2},
			{id: 2},
		},
		conjuncts: []conjunctInfo{
			{id: 3},
		},
		want: false,
	}}

	n := &nodeContext{}
	n.ctx = &OpContext{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.reqSets.assert()

			if got := n.hasEvidenceForAll(tt.reqSets, tt.conjuncts); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
func TestMergeCloseInfo(t *testing.T) {
	tests := []struct {
		name     string
		nv       *nodeContext
		nw       *nodeContext
		expected *nodeContext
	}{{
		name: "merge with no conflicts",
		nv: &nodeContext{
			node: &Vertex{
				Arcs: []*Vertex{
					{Label: 1, state: &nodeContext{}},
				},
			},
			conjunctInfo: []conjunctInfo{
				{id: 1},
			},
			replaceIDs: []replaceID{
				{from: 1, to: 2},
			},
		},
		nw: &nodeContext{
			node: &Vertex{
				Arcs: []*Vertex{
					{Label: 1, state: &nodeContext{}},
				},
			},
			conjunctInfo: []conjunctInfo{
				{id: 2},
			},
			replaceIDs: []replaceID{
				{from: 2, to: 3},
			},
		},
		expected: &nodeContext{
			conjunctInfo: []conjunctInfo{
				{id: 1},
				{id: 2},
			},
			replaceIDs: []replaceID{
				{from: 1, to: 2},
				{from: 2, to: 3},
			},
		},
	}, {
		name: "merge with conflicts",
		nv: &nodeContext{
			node: &Vertex{
				Arcs: []*Vertex{
					{Label: 1, state: &nodeContext{}},
				},
			},
			conjunctInfo: []conjunctInfo{
				{id: 1},
			},
			replaceIDs: []replaceID{
				{from: 1, to: 2},
			},
		},
		nw: &nodeContext{
			node: &Vertex{
				Arcs: []*Vertex{
					{Label: 2, state: &nodeContext{}},
				},
			},
			conjunctInfo: []conjunctInfo{
				{id: 1},
			},
			replaceIDs: []replaceID{
				{from: 1, to: 3},
			},
		},
		expected: &nodeContext{
			conjunctInfo: []conjunctInfo{
				{id: 1},
			},
			replaceIDs: []replaceID{
				{from: 1, to: 2},
				{from: 1, to: 3},
			},
		},
	},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeCloseInfo(tt.nv, tt.nw)
			if !slices.Equal(tt.nv.conjunctInfo, tt.expected.conjunctInfo) {
				t.Errorf("conjunctInfo got %v, want %v", tt.nv.conjunctInfo, tt.expected.conjunctInfo)
			}
			if !slices.Equal(tt.nv.replaceIDs, tt.expected.replaceIDs) {
				t.Errorf("replaceIDs got %v, want %v", tt.nv.replaceIDs, tt.expected.replaceIDs)
			}
		})
	}
}
