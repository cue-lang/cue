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
	"cmp"
	"slices"

	"cuelang.org/go/internal/core/adt"
)

const (
	NodeUnsorted = -1
)

type Graph struct {
	nodes Nodes
}

type Node struct {
	Feature    adt.Feature
	Outgoing   Nodes
	Incoming   Nodes
	structMeta *structMeta
	// temporary state for calculating the Strongly Connected
	// Components of a graph.
	sccNodeState *sccNodeState
	position     int
}

func (n *Node) IsSorted() bool {
	return n.position >= 0
}

// SafeName returns a string useful for debugging, regardless of the
// type of the feature. So for IntLabels, you'll get back `1`, `10`
// etc; for identifiers, you may get back a string with quotes in it,
// eg `"runs-on"`. So this is not useful for comparisons, but it is
// useful (and safe) for debugging.
func (n *Node) SafeName(index adt.StringIndexer) string {
	return n.Feature.SelectorString(index)
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
	allowEdges     bool
	edgesSet       map[edge]struct{}
	nodesByFeature map[adt.Feature]*Node
}

// NewGraphBuilder is the constructor for GraphBuilder.
//
// If you disallow edges, then nodes can still be added to the graph,
// and the [GraphBuilder.AddEdge] method will not error, but edges
// will never be added between nodes. This has the effect that
// topological ordering is not possible.
func NewGraphBuilder(allowEdges bool) *GraphBuilder {
	return &GraphBuilder{
		allowEdges:     allowEdges,
		edgesSet:       make(map[edge]struct{}),
		nodesByFeature: make(map[adt.Feature]*Node),
	}
}

// Adds an edge between the two features. Nodes for the features will
// be created if they don't already exist. This method is idempotent:
// multiple calls with the same arguments will not create multiple
// edges, nor error.
func (builder *GraphBuilder) AddEdge(from, to adt.Feature) {
	if !builder.allowEdges {
		builder.EnsureNode(from)
		builder.EnsureNode(to)
		return
	}

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
		node = &Node{Feature: feature, position: NodeUnsorted}
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

type indexComparison struct{ adt.StringIndexer }

func (index *indexComparison) compareNodeByName(a, b *Node) int {
	aFeature, bFeature := a.Feature, b.Feature
	aIsInt, bIsInt := aFeature.Typ() == adt.IntLabel, bFeature.Typ() == adt.IntLabel

	switch {
	case aIsInt && bIsInt:
		return cmp.Compare(aFeature.Index(), bFeature.Index())
	case aIsInt:
		return -1
	case bIsInt:
		return 1
	default:
		return cmp.Compare(aFeature.RawString(index), bFeature.RawString(index))
	}
}

func (index *indexComparison) compareComponentsByNodes(a, b *StronglyConnectedComponent) int {
	return slices.CompareFunc(a.Nodes, b.Nodes, index.compareNodeByName)
}

// Sort the features of the graph into a single slice.
//
// As far as possible, a topological sort is used. We first calculate
// the strongly-connected-components (SCCs) of the graph. If the graph
// has no cycles then there will be 1 SCC per graph node, which we
// then walk topologically. When there is a choice as to which SCC to
// enter into next, a lexicographical comparison is done, and minimum
// feature chosen.
//
// If the graph has cycles, then there will be at least one SCC
// containing several nodes. When we choose to enter this SCC, we use
// a lexicographical ordering of its nodes. This avoids the need for
// expensive and complex analysis of cycles: the maximum possible
// number of cycles rises with the factorial of the number of nodes in
// a component.
func (graph *Graph) Sort(index adt.StringIndexer) []adt.Feature {
	indexCmp := &indexComparison{index}

	nodesSorted := make(Nodes, 0, len(graph.nodes))

	scc := graph.StronglyConnectedComponents()
	var sccReady []*StronglyConnectedComponent
	for _, component := range scc {
		component.visited = false
		slices.SortFunc(component.Nodes, indexCmp.compareNodeByName)
		if len(component.Incoming) == 0 {
			sccReady = append(sccReady, component)
		}
	}
	slices.SortFunc(sccReady, indexCmp.compareComponentsByNodes)

	sccVisitedCount := 0
	for sccVisitedCount != len(scc) {
		sccCurrent := sccReady[0]
		sccReady = sccReady[1:]
		if sccCurrent.visited {
			continue
		}
		sccCurrent.visited = true
		sccVisitedCount++
		debug("scc current: %p %v\n", sccCurrent, sccCurrent)

		nodesSorted = appendNodes(nodesSorted, sccCurrent.Nodes)

		sccReadyNeedsSorting := false
	SccNextOutgoing:
		for _, next := range sccCurrent.Outgoing {
			for _, required := range next.Incoming {
				if !required.visited {
					continue SccNextOutgoing
				}
			}
			sccReady = append(sccReady, next)
			sccReadyNeedsSorting = true
		}
		if sccReadyNeedsSorting {
			slices.SortFunc(sccReady, indexCmp.compareComponentsByNodes)
		}
	}

	return nodesSorted.Features()
}

func appendNodes(nodesSorted, nodesReady Nodes) Nodes {
	for i, node := range nodesReady {
		node.position = len(nodesSorted) + i
	}
	nodesSorted = append(nodesSorted, nodesReady...)
	return nodesSorted
}

func debug(formatting string, args ...any) {
	//	fmt.Printf(formatting, args...)
}
