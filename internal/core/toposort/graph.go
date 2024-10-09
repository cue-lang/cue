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

package toposort

import (
	"cuelang.org/go/internal/core/adt"
)

const (
	NODE_UNSORTED = -1
)

type Graph struct {
	nodes Nodes
}

type Node struct {
	Feature  adt.Feature
	Outgoing Nodes
	Incoming Nodes
	// temporary state for calculating the Strongly Connected
	// Components of a graph.
	sccNodeState *sccNodeState
	// temporary state for calculating the Elementary Cycles of a
	// graph.
	ecNodeState *ecNodeState
	position    int
}

func (n *Node) IsSorted() bool {
	return n.position >= 0
}

type Nodes []*Node

func (nodes Nodes) Features() []adt.Feature {
	features := make([]adt.Feature, len(nodes))
	for i, node := range nodes {
		features[i] = node.Feature
	}
	return features
}

type edge struct {
	from adt.Feature
	to   adt.Feature
}

type GraphBuilder struct {
	edgesSet       map[edge]struct{}
	nodesByFeature map[adt.Feature]*Node
}

func NewGraphBuilder() *GraphBuilder {
	return &GraphBuilder{
		edgesSet:       make(map[edge]struct{}),
		nodesByFeature: make(map[adt.Feature]*Node),
	}
}

// Adds an edge between the two features. Nodes for the features will
// be created if they don't already exist. This method is idempotent:
// multiple calls with the same arguments will not create multiple
// edges, nor error.
func (builder *GraphBuilder) AddEdge(from, to adt.Feature) {
	edge := edge{from: from, to: to}
	if _, found := builder.edgesSet[edge]; found {
		return
	}

	builder.edgesSet[edge] = struct{}{}
	fromNode := builder.EnsureNode(from)
	toNode := builder.EnsureNode(to)
	fromNode.Outgoing = append(fromNode.Outgoing, toNode)
	toNode.Incoming = append(toNode.Incoming, fromNode)
}

// Ensure that a node for this feature exists. This is necessary for
// features that are not necessarily connected to any other feature.
func (builder *GraphBuilder) EnsureNode(feature adt.Feature) *Node {
	node, found := builder.nodesByFeature[feature]
	if !found {
		node = &Node{Feature: feature, position: NODE_UNSORTED}
		builder.nodesByFeature[feature] = node
	}
	return node
}

func (builder *GraphBuilder) Build() *Graph {
	nodesByFeature := builder.nodesByFeature
	nodes := make(Nodes, 0, len(nodesByFeature))
	for _, node := range nodesByFeature {
		nodes = append(nodes, node)
	}
	return &Graph{nodes: nodes}
}
