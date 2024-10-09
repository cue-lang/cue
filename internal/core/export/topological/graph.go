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

func compareStructInfosByFilenameOffset(a, b *adt.StructInfo) int {
	aSrc, bSrc := a.Src, b.Src
	switch {
	case aSrc == nil && bSrc == nil:
		return 0
	case aSrc == nil:
		return 1
	case bSrc == nil:
		return -1
	}

	aPos, bPos := aSrc.Pos(), bSrc.Pos()
	if c := cmp.Compare(aPos.Filename(), bPos.Filename()); c != 0 {
		return c
	}
	return cmp.Compare(aPos.Offset(), bPos.Offset())
}

func structLitsInParent(v *adt.Vertex) map[*adt.StructLit]struct{} {
	var parentStructLits map[*adt.StructLit]struct{}
	if parent := v.Parent; parent != nil && v.Label != adt.InvalidLabel {
		for _, arc := range parent.Arcs {
			arc.VisitAllConjuncts(func(c adt.Conjunct, isLeaf bool) {
				expr, isStructInfo := c.Expr().(*adt.StructLit)
				field, isField := c.Field().(*adt.Field)
				if isStructInfo && isField && field.Label == v.Label {
					if parentStructLits == nil {
						parentStructLits = make(map[*adt.StructLit]struct{})
					}
					parentStructLits[expr] = struct{}{}
				}
			})
		}
	}
	return parentStructLits
}

func dynamicFieldsFeatures(v *adt.Vertex, builder *GraphBuilder) map[*adt.DynamicField][]adt.Feature {
	// Find all fields which have been created as a result of
	// successful evaluation of a dynamic field name.
	var dynamicFields map[*adt.DynamicField][]adt.Feature
	for _, arc := range v.Arcs {
		builder.EnsureNode(arc.Label)
		arc.VisitLeafConjuncts(func(c adt.Conjunct) bool {
			if dynField, ok := c.Field().(*adt.DynamicField); ok {
				if dynamicFields == nil {
					dynamicFields = make(map[*adt.DynamicField][]adt.Feature)
				}
				dynamicFields[dynField] = append(dynamicFields[dynField], arc.Label)
			}
			return true
		})
	}
	return dynamicFields
}

func VertexFeatures(indexer adt.StringIndexer, v *adt.Vertex) []adt.Feature {
	// The vertex v could be an implicit conjunction of several
	// declarations. Consider:
	//
	// x: c: _
	// x: b: _
	//
	// In this case, we want to guarantee the result is:
	//
	//	x: {
	//	   c: _
	//	   b: _
	//	}
	//
	// To detect this scenario, we need to look at the parent vertex's
	// arc's conjuncts and find any structLits that correspond to our
	// vertex's own label. We later sort all structLits by filename and
	// offset, and then add extra edges to the graph of features to
	// ensure this ordering is honoured.
	parentStructLits := structLitsInParent(v)
	builder := NewGraphBuilder()
	dynamicFields := dynamicFieldsFeatures(v, builder)

	structs := v.Structs
	outgoing := make(map[adt.Decl][]*adt.StructInfo)
	var roots []*adt.StructInfo
	for _, s := range structs {
		parentDecl := s.Decl
		if parentDecl == nil || parentDecl == v {
			roots = append(roots, s)
		} else {
			outgoing[parentDecl] = append(outgoing[parentDecl], s)
		}
	}
	slices.SortFunc(roots, compareStructInfosByFilenameOffset)

	var accumulated []adt.Feature
	for _, s := range roots {
		var previous []adt.Feature
		_, foundInParent := parentStructLits[s.StructLit]
		if foundInParent {
			previous = append(previous, accumulated...)
		}
		next := addEdges(builder, dynamicFields, outgoing, previous, s)
		if foundInParent {
			accumulated = append(accumulated, next...)
		}
	}

	return builder.Build().Sort(indexer)
}

func addEdges(builder *GraphBuilder, dynamicFields map[*adt.DynamicField][]adt.Feature, outgoing map[adt.Decl][]*adt.StructInfo, previous []adt.Feature, s *adt.StructInfo) []adt.Feature {
	var next []adt.Feature

	for _, decl := range s.Decls {
		if nextStructs := outgoing[decl]; len(nextStructs) != 0 {
			_, isBinary := decl.(*adt.BinaryExpr)
			for _, s1 := range nextStructs {
				edges := addEdges(builder, dynamicFields, outgoing, previous, s1)
				if isBinary {
					next = append(next, edges...)
				} else {
					previous = edges
				}
			}
			if isBinary {
				previous = next
				next = nil
			}
		}

		currentLabel := adt.InvalidLabel
		switch decl := decl.(type) {
		case *adt.Field:
			currentLabel = decl.Label
		case *adt.DynamicField:
			// This struct contains a dynamic field. If that dynamic
			// field was successfully evaluated into a field, then
			// insert that field into this chain.
			if labels := dynamicFields[decl]; len(labels) > 0 {
				currentLabel = labels[0]
				dynamicFields[decl] = labels[1:]
			}
		}
		if currentLabel != adt.InvalidLabel {
			builder.EnsureNode(currentLabel)
			next = append(next, currentLabel)
			for _, prevLabel := range previous {
				builder.AddEdge(prevLabel, currentLabel)
			}
			previous = next
			next = nil
		}
	}

	return previous
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
