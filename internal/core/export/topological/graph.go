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

	"cuelang.org/go/cue/token"
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
	File         string
	StructInfo   *adt.StructInfo
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

func comparePos(aPos, bPos token.Pos) int {
	if c := cmp.Compare(aPos.Filename(), bPos.Filename()); c != 0 {
		return c
	}
	return cmp.Compare(aPos.Offset(), bPos.Offset())
}

func exprsInParent(v *adt.Vertex) map[adt.Node]token.Pos {
	var parentExprs map[adt.Node]token.Pos
	if parent := v.Parent; parent != nil && v.Label != adt.InvalidLabel {
		for _, arc := range parent.Arcs {
			debug("parent arc %p\n", arc)
			arc.VisitAllConjuncts(func(c adt.Conjunct, isLeaf bool) {
				debug(" parent arc conjunct: expr: %p :: %T; field: %p :: %T\n", c.Expr(), c.Expr(), c.Field(), c.Field())
				field, isField := c.Field().(*adt.Field)
				if isField && field.Label == v.Label {
					debug("  matches self\n")
				}
				if bin, ok := c.Expr().(*adt.BinaryExpr); ok {
					debug("  binary x: %p :: %T; y: %p :: %T\n", bin.X, bin.X, bin.Y, bin.Y)
				}
				if isField && field.Label == v.Label {
					if parentExprs == nil {
						parentExprs = make(map[adt.Node]token.Pos)
					}
					pos := token.NoPos
					if src := c.Source(); src != nil {
						pos = src.Pos()
					}
					parentExprs[c.Expr()] = pos
				}
			})
		}
	}
	return parentExprs
}

func dynamicFieldsFeatures(v *adt.Vertex, parentExprs map[adt.Node]token.Pos, builder *GraphBuilder) map[*adt.DynamicField][]adt.Feature {
	// Find all fields which have been created as a result of
	// successful evaluation of a dynamic field name.
	var dynamicFields map[*adt.DynamicField][]adt.Feature
	for _, arc := range v.Arcs {
		debug("self arc %p\n", arc)
		builder.EnsureNode(arc.Label)
		arc.VisitLeafConjuncts(func(c adt.Conjunct) bool {
			debug(" self arc conjunct: expr: %p :: %T; field: %p :: %T\n", c.Expr(), c.Expr(), c.Field(), c.Field())
			if _, found := parentExprs[c.Field()]; !found {
				for refs := c.CloseInfo.CycleInfo.Refs; refs != nil; refs = refs.Next {
					debug("  %p :: %T  =  %v (%v)\n", refs.Ref, refs.Ref, refs.Ref, refs.Ref.Source().Pos())
					if _, found = parentExprs[refs.Ref]; found {
						parentExprs[c.Field()] = refs.Ref.Source().Pos()
						break
					}
				}
			}
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
	debug("\n*** V (%s %v %p) ***\n", v.Label.RawString(indexer), v.Label, v)
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
	parentExprs := exprsInParent(v)
	builder := NewGraphBuilder()
	dynamicFields := dynamicFieldsFeatures(v, parentExprs, builder)

	structs := v.Structs
	outgoing := make(map[adt.Decl][]*adt.StructInfo)
	nonRoots := make(map[adt.Decl]struct{})
	for idx, s := range structs {
		parentDecl := s.Decl
		debug(" %d from %p; %d children\n", idx, parentDecl, len(s.Decls))
		outgoing[parentDecl] = append(outgoing[parentDecl], s)
		for _, child := range s.Decls {
			nonRoots[child] = struct{}{}
		}
	}

	var explictUnifications []*adt.StructInfo
	var implicitUnifications []*adt.StructInfo
	for _, s := range structs {
		if _, found := nonRoots[s.Decl]; found {
			continue
		}
		if _, found := parentExprs[s.StructLit]; found {
			implicitUnifications = append(implicitUnifications, s)
		} else {
			allNotFound := true
			for _, decl := range s.Decls {
				if _, found := parentExprs[decl]; found {
					allNotFound = false
					break
				}
			}
			if allNotFound {
				explictUnifications = append(explictUnifications, s)
			} else {
				implicitUnifications = append(implicitUnifications, s)
			}
		}
	}
	slices.SortFunc(implicitUnifications, func(a, b *adt.StructInfo) int {
		aPos, found := parentExprs[a]
		if !found && len(a.Decls) != 0 {
			aPos, found = parentExprs[a.Decls[0]]
		}
		if !found && a.Src != nil {
			aPos = a.Src.Pos()
		}

		bPos, found := parentExprs[b]
		if !found && len(b.Decls) != 0 {
			bPos, found = parentExprs[b.Decls[0]]
		}
		if !found && b.Src != nil {
			bPos = b.Src.Pos()
		}

		return comparePos(aPos, bPos)
	})

	debug("structs: %v\n", structs)
	debug("implicitly unified: %v\n", implicitUnifications)
	debug("explicitly unified: %v\n", explictUnifications)
	debug("outgoing:  %v\n", outgoing)

	for _, s := range explictUnifications {
		debug("starting explict root\n")
		addEdges(builder, dynamicFields, outgoing, nil, false, s)
	}

	currentFilename := ""
	var accumulated []adt.Feature
	for _, s := range implicitUnifications {
		var previous []adt.Feature
		if s.Src == nil && currentFilename != "" {
			currentFilename = ""
			accumulated = accumulated[:0]
		} else if s.Src != nil {
			if fileName := s.Src.Pos().Filename(); fileName != currentFilename {
				currentFilename = fileName
				accumulated = accumulated[:0]
			}
		}
		previous = append(previous, accumulated...)
		debug("starting implicit root\n")
		accumulated = append(accumulated, addEdges(builder, dynamicFields, outgoing, previous, true, s)...)
	}
	debug("edges: %v\n", builder.edgesSet)
	return builder.Build().Sort(indexer)
}

func addEdges(builder *GraphBuilder, dynamicFields map[*adt.DynamicField][]adt.Feature, outgoing map[adt.Decl][]*adt.StructInfo, previous []adt.Feature, skipExistingFeatures bool, s *adt.StructInfo) []adt.Feature {
	debug("--- S %p (%p :: %T) (sl: %p) (skip? %v) ---\n", s, s.Decl, s.Decl, s.StructLit, skipExistingFeatures)
	debug(" previous: %v\n", previous)
	var next []adt.Feature

	filename := ""
	if src := s.Src; src != nil {
		filename = src.Pos().Filename()
	}
	debug(" filename: %s (%v)\n", filename, s.Src)

	for idx, decl := range s.Decls {
		debug(" %p / %d: d (%p :: %T)\n", s, idx, decl, decl)

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
			debug("  label %v\n", currentLabel)

			node, exists := builder.nodesByFeature[currentLabel]
			if exists && node.StructInfo == s {
				debug("    skipping 1\n")
			} else if exists && skipExistingFeatures && filename != "" && node.File == filename {
				debug("    skipping 2\n")
			} else {
				debug("    %v %v\n", node, exists)
				node = builder.EnsureNode(currentLabel)
				if filename != "" {
					node.File = filename
				}
				node.StructInfo = s
				next = append(next, currentLabel)
				for _, prevLabel := range previous {
					builder.AddEdge(prevLabel, currentLabel)
				}
				previous = next
				next = nil
			}
		}

		if nextStructs := outgoing[decl]; len(nextStructs) != 0 {
			debug("  nextStructs: %v\n", nextStructs)
			_, isBinary := decl.(*adt.BinaryExpr)
			for _, s1 := range nextStructs {
				edges := addEdges(builder, dynamicFields, outgoing, previous, skipExistingFeatures, s1)
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
	// fmt.Printf(formatting, args...)
}
