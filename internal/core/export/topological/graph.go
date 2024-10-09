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

package topological

import (
	"cmp"
	"math"
	"slices"

	"cuelang.org/go/internal/core/adt"
)

const (
	NODE_UNSORTED       = -1
	NODE_IN_CURRENT_SCC = -2
)

type Graph struct {
	nodes Nodes
}

type Node struct {
	Feature      adt.Feature
	Outgoing     Nodes
	Incoming     Nodes
	sccNodeState *sccNodeState
	ecNodeState  *ecNodeState
	name         string
	position     int
}

func (n *Node) Name(indexer adt.StringIndexer) string {
	if n.name == "" {
		n.name = n.Feature.RawString(indexer)
	}
	return n.name
}

func (n *Node) IsSorted() bool {
	return n.position >= 0
}

type Nodes []*Node

func (nodes Nodes) Features() []adt.Feature {
	features := make([]adt.Feature, len(nodes))
	for idx, node := range nodes {
		features[idx] = node.Feature
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

type indexerComparison struct{ adt.StringIndexer }

func (indexer *indexerComparison) compareNodeByName(a, b *Node) int {
	return cmp.Compare(a.Name(indexer), b.Name(indexer))
}

func (indexer *indexerComparison) compareNodesByNames(a, b Nodes) int {
	lim := min(len(a), len(b))
	for idx := 0; idx < lim; idx++ {
		if comparison := indexer.compareNodeByName(a[idx], b[idx]); comparison != 0 {
			return comparison
		}
	}
	return cmp.Compare(len(a), len(b))
}

func (indexer *indexerComparison) compareCyclesByNames(a, b *Cycle) int {
	return indexer.compareNodesByNames(a.Nodes, b.Nodes)
}

func (indexer *indexerComparison) compareComponentsByNodes(a, b *StronglyConnectedComponent) int {
	return indexer.compareNodesByNames(a.Nodes, b.Nodes)
}

func chooseCycle(indexerCmp *indexerComparison, unusedCycles []*Cycle) *Cycle {
	chosenCycleIdx := -1
	chosenCycleBrokenEdgeCount := math.MaxInt
	chosenCycleEnabledSince := math.MaxInt
	var chosenCycleEntryNode *Node

	for idx, cycle := range unusedCycles {
		if cycle == nil {
			continue
		}
		debug("cycle %d: %v\n", idx, cycle)
		cycleBrokenEdgeCount := 0
		cycleEnabledSince := math.MaxInt
		var cycleEntryNode *Node

		for _, cycleNode := range cycle.Nodes {
			if cycleNode.IsSorted() {
				continue
			}
		NEXT_CYCLE_NODE_INCOMING:
			for _, incoming := range cycleNode.Incoming {
				position := incoming.position

				if position < 0 {
					for _, cycleNode1 := range cycle.Nodes {
						if cycleNode1 == incoming {
							continue NEXT_CYCLE_NODE_INCOMING
						}
					}
					cycleBrokenEdgeCount++
					continue NEXT_CYCLE_NODE_INCOMING
				}

				if cycleEnabledSince == math.MaxInt || position < cycleEnabledSince {
					cycleEnabledSince = position
					cycleEntryNode = cycleNode
				}
			}
		}

		if cycleEntryNode == nil {
			cycleEntryNode = slices.MinFunc(cycle.Nodes, indexerCmp.compareNodeByName)
		}

		debug("cycle %v; edgeCount %v; enabledSince %v; entryNode %v\n", cycle, cycleBrokenEdgeCount, cycleEnabledSince, cycleEntryNode.Name(indexerCmp))

		cycleIsBetter := chosenCycleIdx == -1
		cycleIsBetter = cycleIsBetter || cycleBrokenEdgeCount < chosenCycleBrokenEdgeCount
		cycleIsBetter = cycleIsBetter || (cycleBrokenEdgeCount == chosenCycleBrokenEdgeCount &&
			((cycleEnabledSince < chosenCycleEnabledSince) || (cycleEnabledSince == chosenCycleEnabledSince &&
				(cycleEntryNode.Name(indexerCmp) < chosenCycleEntryNode.Name(indexerCmp) ||
					(cycleEntryNode == chosenCycleEntryNode && indexerCmp.compareCyclesByNames(cycle, unusedCycles[chosenCycleIdx]) < 0)))))
		if cycleIsBetter {
			chosenCycleIdx = idx
			chosenCycleBrokenEdgeCount = cycleBrokenEdgeCount
			chosenCycleEnabledSince = cycleEnabledSince
			chosenCycleEntryNode = cycleEntryNode
		}
	}

	if chosenCycleEntryNode == nil {
		return nil
	}

	debug("Chose cycle: %v; entering at node: %s\n", unusedCycles[chosenCycleIdx], chosenCycleEntryNode.Name(indexerCmp))
	cycle := unusedCycles[chosenCycleIdx]
	unusedCycles[chosenCycleIdx] = nil
	cycle.RotateToStartAt(chosenCycleEntryNode)
	return cycle
}

// Sort the features of the graph into a single slice.
//
// As far as possible, a topological sort is used.
//
// Whenever there is choice as to which feature should occur next, a
// lexicographical comparison is done, and minimum feature chosen.
//
// Whenever progress cannot be made due to needing to enter into
// cycles, the cycle to enter into, and the node of that cycle with
// which to start, is selected based on:
//
//  1. minimising the number of incoming edges that are violated
//  2. chosing a node which was reachable as early as possible
//  3. chosing a node with a smaller feature name (lexicographical)
func (self *Graph) Sort(indexer adt.StringIndexer) []adt.Feature {
	indexerCmp := &indexerComparison{indexer}

	nodesSorted := make(Nodes, 0, len(self.nodes))

	scc := self.StronglyConnectedComponents()
	var sccReady []*StronglyConnectedComponent
	for _, component := range scc {
		component.visited = false
		slices.SortFunc(component.Nodes, indexerCmp.compareNodeByName)
		if len(component.Incoming) == 0 {
			sccReady = append(sccReady, component)
		}
	}
	slices.SortFunc(sccReady, indexerCmp.compareComponentsByNodes)

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
		var cyclesCurrent []*Cycle

		var nodesReady Nodes
	NEXT_NODE:
		for _, node := range sccCurrent.Nodes {
			node.position = NODE_IN_CURRENT_SCC
			for _, required := range node.Incoming {
				if !required.IsSorted() {
					continue NEXT_NODE
				}
			}
			nodesReady = append(nodesReady, node)
		}
		slices.SortFunc(nodesReady, indexerCmp.compareNodeByName)

		requiredLen := len(nodesSorted) + len(sccCurrent.Nodes)
		for requiredLen != len(nodesSorted) {
			if len(nodesReady) == 0 {
				debug("Stuck after: %v\n", nodesSorted)
				if cyclesCurrent == nil {
					cyclesCurrent = sccCurrent.ElementaryCycles()
					debug("cycles current: %v\n", cyclesCurrent)
				}
				cycle := chooseCycle(indexerCmp, cyclesCurrent)
				if cycle == nil {
					panic("No cycle found.")
				}
				nodesSorted, nodesReady = appendNodes(indexerCmp, nodesSorted, cycle.Nodes, nodesReady)

			} else {
				nodesSorted, nodesReady = appendNodes(indexerCmp, nodesSorted, nodesReady[:1], nodesReady[1:])
			}
		}

		sccReadyNeedsSorting := false
	SCC_NEXT_OUTGOING:
		for _, next := range sccCurrent.Outgoing {
			for _, required := range next.Incoming {
				if !required.visited {
					continue SCC_NEXT_OUTGOING
				}
			}
			sccReady = append(sccReady, next)
			sccReadyNeedsSorting = true
		}
		if sccReadyNeedsSorting {
			slices.SortFunc(sccReady, indexerCmp.compareComponentsByNodes)
		}
	}

	return nodesSorted.Features()
}

func appendNodes(indexerCmp *indexerComparison, nodesSorted, nodesReady, nodesEnabled Nodes) (nodesSortedOut, nodesEnabledOut Nodes) {
	nodesReadyNeedsSorting := false
	for _, node := range nodesReady {
		if node.IsSorted() {
			continue
		}
		node.position = len(nodesSorted)
		nodesSorted = append(nodesSorted, node)

	NEXT_OUTGOING:
		for _, next := range node.Outgoing {
			if next.position != NODE_IN_CURRENT_SCC {
				continue
			}
			for _, required := range next.Incoming {
				if !required.IsSorted() {
					continue NEXT_OUTGOING
				}
			}
			debug("After %v, found new ready: %s\n", nodesSorted, next.Name(indexerCmp))
			nodesEnabled = append(nodesEnabled, next)
			nodesReadyNeedsSorting = true
		}
	}
	if nodesReadyNeedsSorting {
		slices.SortFunc(nodesEnabled, indexerCmp.compareNodeByName)
	}
	return nodesSorted, nodesEnabled
}

func debug(formatting string, args ...any) {
	//	fmt.Printf(formatting, args...)
}
