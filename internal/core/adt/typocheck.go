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

// This file holds CUE's algorithm to detect misspelled field names.
//
// ## Outline
//
// Typo checking MAY be enabled whenever a node is unified with a definition or
// a struct returned from the close builtin. Each distinct unification of such a
// struct triggers a separate "typo check", which is associated with a unique
// identifier. This identifier is passed along all values that result from
// unification with this struct.
//
// Once the processing of a node is complete, it is checked that for all its
// fields there is supportive evidence that the field is allowed for all structs
// for which typo checking is enabled.
//
// ## Selecting which Structs to Typo Check
//
// The algorithm is quite general and allows many types of heuristics to be
// applied. The initial goal is to mimic V2 semantics as closely as possible to
// facilitate migration to V3.
//
// ### Embeddings
//
// Embeddings provide evidence allowing fields for their enclosing scopes. Even
// when an embedding references a definition, it will not start its own typo
// check.
//
// ### Definitions
//
// By default, all references to non-embedded definitions are typo checked. The
// following situations qualify.
//
//      #A: b: {a: int}
//      a: #A
//
//      // Trigger typo check
//      r1: #A
//      r2: a
//
//      // Do NOT trigger typo check
//      r3: a.b
//
// In the case of r3, no typo check is triggered as the inserted value does not
// reference a definition directly. This choice is somewhat arbitrary. The main
// reason to pick this semantics is to be compatible with V2.
//
// ### Inline structs
//
// Like in V2, inline structs are generally not typo checked. We can add this
// later if necessary.
//
// ## Tracking Evidence
//
// The basic principle of this algorithm is that each (dynamic) reference is
// associated with a unique identifier. When a node is unified with a definition
// all its descendant nodes are tagged with this number if any of the conjuncts
// of this definition end up being unified with such nodes. Once a node finished
// processing, it is checked, recursively, that all its fields adhere to this
// schema by checking that there is supportive evidence that this schema allows
// such fields.
//
// Consider, for instance, the following CUE. Here, the reference to #Schema
// will be assigned the unique identifier 7.
//
// 	foo: #Schema & {
// 		a: 1  // marked with 7 as #Schema has a field `a`.
// 		b: 1  // marked with 7 as the pattern of #Schema allows `b`.
// 		c: 1  // not marked with 7 as #Schema does not have a field `c`.
// 	}
//
// 	#Schema: {
// 		a?:      int
// 		[<="b"]: int
// 	}
//
// Details of this algorithm are included below.
//
// ### Evidence sets and multiple insertions
//
// The same struct may be referred to within a node multiple times. If it is not
// itself a typo-checked definition, it may provide evidence for multiple other
// typo-checked definitions. To avoid having to process the conjuncts of such
// structs multiple times, we will still assign a unique identifier to such
// structs, and will annotate each typo-checked definition to which this applies
// that it is satisfied by this struct.
//
// In other words, each typo-checked struct may be associated with multiple
// unique identifiers that provide evidence for an allowed field. See the
// section below for a more detailed example.
//
// ### Embeddings
//
// Embeddings are a special case of structs that are not typo checked, but may
// provide evidence for other typo-checked definitions. As a struct associated
// with an embedding may be referred to multiple times, we will also assign a
// unique identifier to embeddings. This identifier is then also added to the
// evidence set of the typo-checked definition.
//
// Note that if a definition is embedded within a struct, that struct is
// considered closed after unifying all embeddings. We need to track this
// separately. This is discussed in the code below where appropriate. Ideally,
// we would get rid of this behavior by only allowing "open" or "closed"
// unifications, without considering any of the other context.
//
// Consider the following example:
//
// 	a: #A & { #A, b: int, c: int }
//  #A: #B
//  #B: { b: int }
//
// In this case, for `a`, we need find evidence that all fields of `#A` are
// allowed. If `a` were to be referenced by another field, we would also
// need to check that the struct with the embedded definition, which is
// closed after unification, is valid.
//
// So, for `a` we track two requirements, say `1`, which corresponds to the
// reference to `#A` and `2`, which corresponds to the literal struct.
// During evaluation, we additionally assign `3` to `#B`.
//
// The algorithm now proceeds as follows. We start with the requirement sets
// `{1}` and `{2}`. For the first, while evaluating definition `#A`, we find
// that this redirects to `#B`. In this case, as `#B` is at least as strict
// as `#A`, we can rewrite the set as `{3}`. This means we need to find
// evidence that each field of `a` is marked with a `3`.
// For the second case, `{2}`, we find that `#A` is embedded. As this is not
// a further restriction, we add the ID for `#A` to the set, resulting in
// `{2, 1}`. Subsequently, `#A` maps to `#B` again, but in this case, as `#A`
// is embedded, it is also additive, resulting in `{2, 1, 3}`, where each field
// in `a` needs to be marked with at least one of these values.
//
// After `a` is fully unified, field `b` will be marked with `3` and thus has
// evidence for both requirements. Field `c`, however, is marked with `2`, which
// is only supports the second requirement, and thus will result in a typo
// error.
//
// NOTE 1: that the second set is strictly more permissive then the first
// requirement and could be elided.
// NOTE 2: a reference to the same node within one field is only assigned a
// single ID and only needs to be processed once per node.
//
// ### Pruning of sub nodes
//
// For definitions, typo checking proceeds recursively. However, typo checking
// is disabled for certain fields, even when a node still has subfields. Typo
// checking is disabled if a field has:
//
// - a "naked" top `_` (so not, for instance `{_}`), - an ellipsis `...`, or -
// it has a "non-concrete" validator, like matchn.
//
// In the latter case, the builtin takes over the responsibility of checking the
// type.
//
import (
	"math"
	"slices"

	"cuelang.org/go/cue/ast"
)

type defID uint32

type defIDType int8

const (
	// defIDTypeUnknown indicates that the ID is not a definition.
	defIDTypeUnknown defIDType = iota

	defEmbedding
	defReference
	defStruct
)

func (d defIDType) String() string {
	switch d {
	case defEmbedding:
		return "E"
	case defReference:
		return "D"
	case defStruct:
		return "S"
	default:
		return "*"
	}
}

const deleteID defID = math.MaxUint32

func (c *OpContext) getNextDefID() defID {
	c.stats.NumCloseIDs++
	c.nextDefID++
	return c.nextDefID
}

type refInfo struct {
	v  *Vertex
	id defID

	// parent is used for enclosing structs and embedding relations.
	parent defID

	// embed defines the scope of the embedding in which id is defined.
	embed defID

	// ignore defines whether we should not do typo checking for this defID.
	ignore bool

	// kind explains the type of defID.
	kind defIDType

	// isRecursive indicates this is recursively closed.
	isRecursive bool
}

type conjunctFlags uint8

const (
	cHasEllipsis conjunctFlags = 1 << iota
	cHasTop
	cHasStruct
	cHasOpenValidator
)

type conjunctInfo struct {
	id    defID
	embed defID
	kind  Kind
	flags conjunctFlags
}

func (c conjunctFlags) hasTop() bool {
	return c&(cHasTop) != 0
}

func (c conjunctFlags) hasStruct() bool {
	return c&(cHasStruct) != 0
}

func (c conjunctFlags) forceOpen() bool {
	return c&(cHasOpenValidator) != 0
}

func (c conjunctFlags) hasEllipsis() bool {
	return c&(cHasEllipsis) != 0
}

type replaceID struct {
	from defID
	to   defID
	add  bool // If true, add to the set. If false, replace from with to.
}

func (n *nodeContext) addReplacement(x replaceID) {
	if x.from == x.to {
		return
	}
	// TODO: we currently may compute n.reqSets too early in some rare
	// circumstances. We clear the set if it needs to be recomputed.
	n.computedCloseInfo = false
	n.reqSets = n.reqSets[:0]

	n.replaceIDs = append(n.replaceIDs, x)
}

func (n *nodeContext) updateConjunctInfo(k Kind, id CloseInfo, flags conjunctFlags) {
	if n.ctx.OpenDef {
		return
	}

	for i, c := range n.conjunctInfo {
		if c.id == id.defID {
			n.conjunctInfo[i].kind &= k
			n.conjunctInfo[i].flags |= flags
			return
		}
	}
	n.conjunctInfo = append(n.conjunctInfo, conjunctInfo{
		id:    id.defID,
		embed: id.enclosingEmbed,
		kind:  k,
		flags: flags,
	})
}

// addResolver adds a resolver to typo checking. Both definitions and
// non-definitions should typically be added: non-definitions may be added
// multiple times to a single node. As we only want to insert each conjunct
// once, we need to ensure that within all contexts a single ID assigned to such
// a resolver is tracked.
func (n *nodeContext) addResolver(v *Vertex, id CloseInfo, forceIgnore bool) CloseInfo {
	if n.ctx.OpenDef {
		return id
	}

	closeOuter := (id.FromDef && id.FromEmbed) || v.ClosedNonRecursive

	if closeOuter && !forceIgnore {
		// Walk up the parent chain of the outer structs to "activate" them.
		outerID := id.outerID
		for i := len(n.reqDefIDs) - 1; i >= 0; i-- {
			x := n.reqDefIDs[i]
			if x.id == outerID && outerID != 0 {
				n.reqDefIDs[i].ignore = false
				if v.ClosedRecursive {
					n.reqDefIDs[i].isRecursive = true
				}
				outerID = x.parent
			}
		}
	}

	var ignore bool
	switch {
	case forceIgnore:
		// Special mode to always ignore the outer enclosing group.
		// This is the case, for instance, if a resolver resolves to a
		// non-definition.
		ignore = true
		// TODO: Consider resetting FromDef.
		// id.FromDef = false
	case id.enclosingEmbed != 0 || id.outerID == 0:
		// We have a reference within an inner embedding group. If this is
		// a definition, or otherwise typo checked struct, we need to track
		// the embedding for mutual compatibility.
		// is a definition:
		// 		a: {
		// 			// Even though #A and #B do not constraint `a` individually,
		// 			// they need to be checked for mutual consistency within
		// 			// the embedding.
		// 			#A & #B
		// 		}
		isClosed := id.FromDef || v.ClosedNonRecursive
		ignore = !isClosed
	default:
		// In the default case we can disable typo checking this type if it is
		// an embedding.
		ignore = id.FromEmbed
	}

	dstID := defID(0)
	for _, x := range n.reqDefIDs {
		if x.v == v {
			dstID = x.id
			break
		}
	}

	if dstID == 0 || id.enclosingEmbed != 0 {
		next := n.ctx.getNextDefID()
		if dstID != 0 {
			// If we need to activate an enclosing embed group, and the added
			// resolver was already before, we need to allocate a new ID and
			// add the original ID to the set of the new one.
			n.addReplacement(replaceID{from: next, to: dstID, add: true})
		}
		dstID = next

		n.reqDefIDs = append(n.reqDefIDs, refInfo{
			v:           v,
			id:          dstID,
			parent:      id.outerID,
			ignore:      ignore,
			kind:        defReference,
			embed:       id.enclosingEmbed,
			isRecursive: v.ClosedRecursive,
		})
	}
	srcID := id.defID
	id.defID = dstID

	n.addReplacement(replaceID{from: srcID, to: dstID, add: true})

	return id
}

// subField updates a CloseInfo for subfields of a struct.
func (c *OpContext) subField(ci CloseInfo) CloseInfo {
	// TODO: we mostly signal here that we need a new scope if a subfield has
	// another embedding. IOW, we are overloading this field. This seems fine
	// as, at this point, it seems to be only used for debugging. We may
	// want to consider having a separate field for this, though.
	ci.FromEmbed = false
	return ci
}

func (n *nodeContext) newReq(id CloseInfo, kind defIDType) CloseInfo {
	dstID := n.ctx.getNextDefID()
	n.addReplacement(replaceID{from: id.defID, to: dstID, add: true})

	parent := id.defID
	id.defID = dstID

	switch kind {
	case defEmbedding:
		id.enclosingEmbed = dstID

	case defStruct:
		id.outerID = dstID

	default:
		panic("unknown kind")
	}

	// TODO: consider only adding when record || OpenGraph
	n.reqDefIDs = append(n.reqDefIDs, refInfo{
		v:           emptyNode,
		id:          dstID,
		parent:      parent,
		embed:       id.enclosingEmbed,
		ignore:      true,
		kind:        kind,
		isRecursive: id.FromDef,
	})

	return id
}

// AddOpenConjunct adds w as a conjunct of v and disables typo checking for w,
// even if it is a definition.
func (v *Vertex) AddOpenConjunct(ctx *OpContext, w *Vertex) {
	n := v.getBareState(ctx)
	ci := n.injectEmbedNode(w, CloseInfo{})
	c := MakeConjunct(nil, w, ci)
	v.AddConjunct(c)
}

// injectEmbedNode is used to track typo checking within an embedding.
// Consider, for instance:
//
//	#A: {a: int}
//	#B: {b: int}
//	#C: {
//		#A & #B // fails
//		c: int
//	}
//
// In this case, even though #A and #B are both embedded, they are intended
// to be mutually exclusive. We track this by introducing a separate defID
// for the embedding. Suppose that the embedding #A&#B is assigned defID 2,
// where its parent is defID 1. Then #A is assigned 3 and #B is assigned 4.
//
// We can then say that requirement 3 (node A) holds if all fields contain
// either label 3, or any field within 1 that is not 2.
func (n *nodeContext) injectEmbedNode(x Decl, id CloseInfo) CloseInfo {
	id.FromEmbed = true

	// Filter cases where we do not need to track the definition.
	switch x := x.(type) {
	case *BinaryExpr:
		if x.Op != AndOp {
			return id
		}
	}

	return n.newReq(id, defEmbedding)
}

// splitStruct is used to mark the outer struct of a field in which embeddings
// occur. The significance is that a reference to this node expects a node
// to be closed, even if it only has embeddings. Consider for instance:
//
//	// A is closed  and allows the fields of #B plus c.
//	A: { { #B }, c: int }
//
// TODO(flatclose): this is a temporary solution to handle the case where a
// definition is embedded within a struct. It can be removed if we implement
// the #A vs #A... semantics.
func (n *nodeContext) splitStruct(s *StructLit, id CloseInfo) CloseInfo {
	if n.ctx.OpenDef {
		return id
	}

	if id.outerID != 0 {
		// This is not strictly necessary, but it reduces the counters a bit.
		return id
	}
	if id.FromEmbed {
		// If we already had a struct within this field we can simply use it.
		return id
	}

	if _, ok := s.Src.(*ast.File); ok {
		// If this is not a file, the struct indicates the scope/
		// boundary at which closedness should apply. This is not true
		// for files.
		// We should also not spawn if this is a nested Comprehension,
		// where the spawn is already done as it may lead to spurious
		// field not allowed errors. We can detect this with a nil s.Src.
		// TODO(evalv3): use a more principled detection mechanism.
		// TODO: set this as a flag in StructLit so as to not have to
		// do the somewhat dangerous cast here.
		return id
	}

	return n.splitScope(id)
}

func (n *nodeContext) splitScope(id CloseInfo) CloseInfo {
	return n.newReq(id, defStruct)
}

func (n *nodeContext) checkTypos() {
	ctx := n.ctx
	if ctx.OpenDef {
		return
	}
	noDeref := n.node // noDeref retained for debugging purposes.
	v := noDeref.DerefValue()

	// Stop early, avoiding the work in appendRequired below, if we have no arcs to check.
	if len(v.Arcs) == 0 {
		return
	}

	// Avoid unnecessary errors.
	if b, ok := v.BaseValue.(*Bottom); ok && !b.CloseCheck {
		return
	}

	baseRequired := getReqSets(n)
	if len(baseRequired) == 0 {
		return
	}

	// Get all replacement rules from the parent level and above,
	// and pre-calculate the result of applying these common rules once.
	if replacements := n.getReplacements(); len(replacements) > 0 {
		baseRequired.replaceIDs(ctx, replacements...)
	}

	var err *Bottom
	for _, a := range v.Arcs {
		f := a.Label

		// TODO(mem): child states of uncompleted nodes must have a state.
		a = a.DerefDisjunct()
		na := a.state

		required := baseRequired
		// If the field has its own rules, apply them as a delta.
		// This requires a copy to not pollute the base for the next iteration.
		if len(na.replaceIDs) > 0 {
			required = slices.Clone(required)
			required.replaceIDs(ctx, na.replaceIDs...)
		}

		required.filterSets(func(a []reqSet) bool {
			if hasParentEllipsis(a, n.conjunctInfo) {
				a[0].removed = true
			}
			return true
		})
		// TODO(perf): somehow prevent error generation of recursive structures,
		// or at least make it cheap. Right now if this field is a typo, likely
		// all descendents will be regarded as typos.
		if b, ok := a.BaseValue.(*Bottom); ok {
			if !b.CloseCheck {
				continue
			}
		}

		if allowedInClosed(f) {
			continue
		}

		if n.hasEvidenceForAll(required, na.conjunctInfo) {
			continue
		}

		// TODO: do not descend on optional?

		// openDebugGraph(ctx, a, "NOT ALLOWED") // Uncomment for debugging.

		if b := ctx.notAllowedError(a); b != nil && a.ArcType <= ArcRequired {
			err = CombineErrors(nil, err, b)
		}
	}

	if err != nil {
		n.AddChildError(err) // TODO: should not be necessary.
	}
}

// hasEvidenceForAll reports whether there is evidence in a set of
// conjuncts for each of the typo-checked structs represented by
// the reqSets.
func (n *nodeContext) hasEvidenceForAll(a reqSets, conjuncts []conjunctInfo) bool {
	for i := uint32(0); int(i) < len(a); i += a[i].size {
		if a[i].size == 0 {
			panic("unexpected set length")
		}
		if a[i].ignored {
			continue
		}
		if a[i].removed {
			continue
		}

		if !hasEvidenceForOne(a, i, conjuncts) {
			if n.ctx.LogEval > 0 {
				n.Logf("DENIED BY %d", a[i].id)
			}
			return false
		}
	}
	return true
}

// hasEvidenceForOne reports whether a single typo-checked set has evidence for
// any of its defIDs.
func hasEvidenceForOne(all reqSets, i uint32, conjuncts []conjunctInfo) bool {
	a := all[i : i+all[i].size]

	for _, c := range conjuncts {
		for _, ri := range a {
			if c.id == ri.id {
				return true
			}
		}
	}

	embedScope := all.lookupSet(a[0].embed)

	if len(embedScope) == 0 {
		return false
	}

	outerScope := all.lookupSet(a[0].parent)

	if len(outerScope) > 0 && outerScope[0].removed {
		return true
	}

outer:
	for _, c := range conjuncts {
		for _, x := range embedScope {
			if x.id == c.embed {
				// Within the scope of the embedding.
				continue outer
			}
		}

		if len(outerScope) == 0 || a[0].parent == 0 {
			return true
		}

		// If this conjunct is within the outer struct, but outside the
		// embedding scope, this means it was "added" and we do not have
		// to verify it within the embedding scope.
		for _, x := range outerScope {
			if x.id == c.id {
				return true
			}
		}
	}
	return false
}

// reqSets defines a set of sets of defIDs that can each satisfy a required id.
//
// A reqSet holds a sequence of a defID "representative", or "head" for a
// requirement, followed by all defIDs that satisfy this requirement. For head
// elements, size indicates the number of entries in the set, including the
// head. For non-head elements, size is 0.
type reqSets []reqSet

// A single reqID might be satisfied by multiple defIDs, if the definition
// associated with the reqID embeds other definitions, for instance. In this
// case we keep a list of defIDs that may also be satisfied.
//
// This type is used in [nodeContext.equivalences] as follows:
//   - if an embedding is used by a definition, it is inserted in the list
//     pointed to by its refInfo. Similarly,
//     refInfo is added to the list
type reqSet struct {
	id     defID
	parent defID
	// size is the number of elements in the set. This is only set for the head.
	// Entries with equivalence IDs have size set to 0.
	size  uint32
	embed defID // TODO(flatclose): can be removed later.
	// once indicates that a reqSet closes only one level, i.e. closedness
	// is the result of a close()
	once bool
	// ignored indicates whether this reqSet should be used to check closedness.
	// A group can be ignored while still needed to determine the scope of
	// an outer struct or embedding group.
	// The value of ignored may flip from true to false (e.g. with an embedded
	// definition) or false to true (e.g. getting out of scope of a close()
	// builtin).
	ignored bool
	// removed is like ignore, but is permanent. Once removed, a reqSet cannot
	// be "unremoved". In many cases we cannot actually remove a reqSet as
	// we still need to track the group memberships for embeddings or enclosing
	// structs.
	removed bool
}

// assert checks the invariants of a reqSets. It can be used for debugging.
func (a reqSets) assert() {
	for i := 0; i < len(a); {
		e := a[i]
		if e.size == 0 {
			panic("head element with 0 size")
		}
		if i+int(e.size) > len(a) {
			panic("set extends beyond end of slice")
		}
		for j := 1; j < int(e.size); j++ {
			if a[i+j].size != 0 {
				panic("non-head element with non-zero size")
			}
		}

		i += int(e.size)
	}
}

type replaceInfo struct {
	skip, delete bool
	replacements []defID
}

// replaceIDs transitively applies replacement rules to the requirement sets,
// modifying the receiver in place.
//
// The algorithm uses an efficient, non-recursive Breadth-First Search (BFS)
// to expand equivalences for each requirement group. It first pre-processes
// the list of rules into structures for quick lookups:
//
//  1. Head Deletion: A rule with `add: false` marks a group for deletion if
//     its `from` ID matches the group's head.
//  2. ID Removal: A rule with `to: deleteID` marks the `from` ID to be
//     excluded from any group.
//  3. Graph Traversal: All other rules build a replacement graph. The BFS
//     traverses this graph, starting from a group's initial members, to find
//     all reachable IDs. Traversal is pruned for branches leading to a removed
//     ID or into an embedding's scope.
//
// Finally, a new group is reconstructed from all the valid IDs found.
func (a *reqSets) replaceIDs(ctx *OpContext, b ...replaceID) {
	if len(b) == 0 {
		return
	}

	// TODO(mvdan): can we build this map directly instead of a slice?
	index := ctx.replaceIDsIndex
	if index == nil {
		index = make(map[defID]replaceInfo, len(b))
	}
	for _, rule := range b {
		info := index[rule.from]
		if !rule.add {
			info.skip = true
		}
		if rule.to == deleteID {
			info.delete = true
		} else {
			// All non-delete rules, regardless of the `add` flag, contribute
			// to the replacement graph.
			info.replacements = append(info.replacements, rule.to)
		}
		index[rule.from] = info
	}

	origSets := append(ctx.replaceIDsOrig[:0], *a...)
	newSets := (*a)[:0]
	queue := ctx.replaceIDsQueue
	visited := ctx.replaceIDsVisited
	if visited == nil {
		visited = make(map[defID]bool)
	}

	for i := 0; i < len(origSets); {
		head := origSets[i]
		currentGroup := origSets[i : i+int(head.size)]
		i += int(head.size)

		if index[head.id].skip {
			continue
		}

		queue = queue[:0]
		clear(visited)

		for _, set := range currentGroup {
			if !index[set.id].delete && !visited[set.id] {
				visited[set.id] = true
				queue = append(queue, set)
			}
		}

		for qIdx := 0; qIdx < len(queue); qIdx++ {
			currentSet := queue[qIdx]
			ctx.stats.CloseIDElems++

			for _, nextID := range index[currentSet.id].replacements {
				if !index[nextID].delete && !visited[nextID] {
					// Trim subtree for embedded conjunctions.
					if head.embed == nextID {
						continue
					}
					visited[nextID] = true
					queue = append(queue, reqSet{id: nextID, once: currentSet.once})
				}
			}
		}

		if len(queue) > 0 {
			queue[0].size = uint32(len(queue))
			newSets = append(newSets, queue...)
		}
	}

	// to be reused later on
	clear(index)
	ctx.replaceIDsIndex = index
	ctx.replaceIDsOrig = origSets[:0]
	ctx.replaceIDsQueue = queue[:0]
	clear(visited)
	ctx.replaceIDsVisited = visited

	*a = newSets
}

// mergeCloseInfo merges the conjunctInfo of nw that is missing from nv into nv.
//
// This is used to merge conjunctions from a disjunct to the Vertex that
// originated the disjunct.
// TODO: consider whether we can do without. We usually aim to not check
// such nodes, but sometimes we do.
func mergeCloseInfo(nv, nw *nodeContext) {
	v := nv.node
	w := nw.node
	if w == nil {
		return
	}
	// Merge missing closeInfos
outer:
	for _, wci := range nw.conjunctInfo {
		for _, vci := range nv.conjunctInfo {
			if wci.id == vci.id {
				continue outer
			}
		}
		nv.conjunctInfo = append(nv.conjunctInfo, wci)
	}

outer2:
	for _, d := range nw.replaceIDs {
		for _, vd := range nv.replaceIDs {
			if d == vd {
				continue outer2
			}
		}
		nv.replaceIDs = append(nv.replaceIDs, d)
	}

	for _, wa := range w.Arcs {
		for _, va := range v.Arcs {
			if va.Label == wa.Label {
				mergeCloseInfo(va.state, wa.state)
				break
			}
		}
	}
}

func (n *nodeContext) getReplacements() (a []replaceID) {
	for p := n.node; p != nil && p.state != nil; p = p.Parent {
		a = append(a, p.state.replaceIDs...)
	}
	return a
}

// getReqSets initializes, if necessary, and returns the reqSets for n.
func getReqSets(n *nodeContext) reqSets {
	if n == nil {
		return nil
	}

	if n.computedCloseInfo {
		return n.reqSets
	}
	n.reqSets = n.reqSets[:0]
	n.computedCloseInfo = true

	a := n.reqSets
	v := n.node

	if p := v.Parent; p != nil {
		aReq := getReqSets(p.state)
		if !n.dropParentRequirements {
			a = append(a, aReq...)
		}
	}
	a.filterNonRecursive()

	last := len(a) - 1

outer:
	for _, y := range n.reqDefIDs {
		// A defReference is never "reactivated" once it is ignored.
		// Embeddings we need to keep around to compute the embedding scope,
		// even when the embedding itself is ignored.
		if y.ignore && y.kind == defReference {
			continue
		}

		for _, x := range a {
			if x.id == y.id {
				continue outer
			}
		}
		once := false
		if y.v != nil && y.kind != defEmbedding {
			once = y.v.ClosedNonRecursive
			if !y.ignore && !y.isRecursive {
				once = true
			}
		}

		a = append(a, reqSet{
			id:      y.id,
			parent:  y.parent,
			once:    once,
			ignored: y.ignore,
			embed:   y.embed,
			size:    1,
		})

		if y.parent != 0 && !y.ignore {
			// Enable outer structs for checking.
			outerID := y.parent
			for i := last; i >= 0 && outerID != 0; i-- {
				x := a[i]
				if x.id == outerID {
					if a[i].ignored {
						a[i].once = !y.isRecursive
					} else {
						a[i].once = a[i].once && !y.isRecursive
					}
					a[i].ignored = false
					outerID = x.parent
				}
			}
		}
	}

	a.replaceIDs(n.ctx, n.replaceIDs...)

	// If 'v' is a hidden field, then all reqSets in 'a' for which there is no
	// corresponding entry in conjunctInfo should be removed from 'a'.
	if allowedInClosed(v.Label) {
		a.filterSets(func(a []reqSet) bool {
			for _, e := range a {
				for _, c := range n.conjunctInfo {
					if c.id == e.id {
						return true // keep the set
					}
				}
			}
			return false // discard the set
		})
	}

	var parentConjuncts []conjunctInfo
	if p := v.Parent; p != nil && p.state != nil {
		parentConjuncts = p.state.conjunctInfo
	}

	a.filterTop(n.conjunctInfo, parentConjuncts)

	n.reqSets = a
	return a
}

// If there is a top or ellipsis for all supported conjuncts, we have
// evidence that this node can be dropped.
func (a *reqSets) filterTop(conjuncts, parentConjuncts []conjunctInfo) (openLevel bool) {
	a.filterSets(func(a []reqSet) bool {
		var f conjunctFlags
		hasAny := false
		for _, e := range a {
			for _, c := range conjuncts {
				if e.id != c.id {
					continue
				}
				hasAny = true
				flags := c.flags
				if c.id < a[0].id {
					flags &^= cHasStruct
				}
				f |= flags
			}
		}
		if (f.hasTop() && !f.hasStruct()) || f.forceOpen() {
			return false
		}
		if !hasAny && hasParentEllipsis(a, parentConjuncts) {
			a[0].removed = true
		}
		return true
	})
	return openLevel
}

// hasParentEllipsis reports if the parent has any conjuncts from an ellipsis
// matching any of the ids in a.
//
// TODO: this is currently called twice. Consider an approach where we only need
// to filter this once for each node. Luckily we can avoid quadratic checks
// for any conjunct that is not an ellipsis, which is most.
func hasParentEllipsis(a reqSets, conjuncts []conjunctInfo) bool {
	for _, c := range conjuncts {
		if !c.flags.hasEllipsis() {
			continue
		}
		for _, e := range a {
			if e.id == c.id {
				return true
			}
		}
	}
	return false
}

func (a *reqSets) filterNonRecursive() {
	a.filterSets(func(e []reqSet) bool {
		x := e[0]
		if x.once { //  || x.id == 0
			e[0].ignored = true
		}
		return true // keep the entry
	})
}

// filter keeps all reqSets e in a for which f(e) and removes the rest.
func (a *reqSets) filterSets(f func(e []reqSet) bool) {
	temp := (*a)[:0]
	for i := 0; i < len(*a); {
		e := (*a)[i]
		set := (*a)[i : i+int(e.size)]

		if f(set) {
			temp = append(temp, set...)
		}

		i += int(e.size)
	}
	*a = temp
}

// lookupSet returns the set in a with the given id or nil if no such set.
func (a reqSets) lookupSet(id defID) reqSets {
	if id != 0 {
		for i := uint32(0); int(i) < len(a); i += a[i].size {
			if a[i].id == id {
				return a[i : i+a[i].size]
			}
		}
	}
	return nil
}
