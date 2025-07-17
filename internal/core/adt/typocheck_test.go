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
