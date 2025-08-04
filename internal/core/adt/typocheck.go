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

	if len(c.containments) == 0 {
		// Our ID starts at 1. Create an extra element for the zero value.
		c.containments = make([]defID, 1, 16)
	}
	c.containments = append(c.containments, 0)

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
}

func (n *nodeContext) addReplacement(x replaceID) {
	if x.from == x.to {
		return
	}

	if x.from < x.to && n.ctx.containments[x.to] == 0 {
		n.ctx.containments[x.to] = x.from
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
	if id.defID != 0 && id.opID != n.ctx.opID {
		n.ctx.stats.MisalignedConjunct++
		return
	}
	for i, c := range n.conjunctInfo {
		if c.id == id.defID {
			n.conjunctInfo[i].kind &= k
			n.conjunctInfo[i].flags |= flags
			return
		}
	}
	n.ctx.stats.ConjunctInfos++
	n.conjunctInfo = append(n.conjunctInfo, conjunctInfo{
		id:    id.defID,
		embed: id.enclosingEmbed,
		kind:  k,
		flags: flags,
	})
	if len(n.conjunctInfo) > int(n.ctx.stats.MaxConjunctInfos) {
		n.ctx.stats.MaxConjunctInfos = int64(len(n.conjunctInfo))
	}
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

	if id.opID != 0 && id.opID != n.ctx.opID {
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
			n.addReplacement(replaceID{from: next, to: dstID})
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
	id.opID = n.ctx.opID
	id.defID = dstID

	n.addReplacement(replaceID{from: srcID, to: dstID})

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

// clearCloseCheck clears the CloseInfo for a node, so that it is not
// considered for typo checking.
func (id CloseInfo) clearCloseCheck() CloseInfo {
	id.opID = 0
	id.defID = 0
	id.enclosingEmbed = 0
	id.outerID = 0
	return id
}

func (n *nodeContext) newReq(id CloseInfo, kind defIDType) CloseInfo {
	if id.defID != 0 && id.opID != n.ctx.opID {
		return id.clearCloseCheck()
	}

	dstID := n.ctx.getNextDefID()
	n.addReplacement(replaceID{from: id.defID, to: dstID})

	parent := id.defID
	id.opID = n.ctx.opID
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
// This is called from UnifyAccept only.
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

	var err *Bottom
	for _, a := range v.Arcs {
		f := a.Label

		// TODO(mem): child states of uncompleted nodes must have a state.
		a = a.DerefDisjunct()
		na := a.state
		// TODO(refcount): remove: cache in Vertex?
		if na == nil {
			// A node may be evaluated twice, for instance when processing
			// validators. In this case, the closedness will already have been
			// checked and the nodeContext may already be nil.
			continue
		}

		required := baseRequired
		// If the field has its own rules, apply them as a delta.
		// This requires a copy to not pollute the base for the next iteration.
		if len(na.replaceIDs) > 0 {
			required = slices.Clone(required)
		}

		n.filterSets(&required, func(n *nodeContext, a *reqSet) bool {
			if id := hasParentEllipsis(n, a, n.conjunctInfo); id != 0 {
				a.removed = true
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

		if na.hasEvidenceForAll(required, na.conjunctInfo) {
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
	for i, rs := range a {
		if rs.ignored {
			continue
		}
		if rs.removed {
			continue
		}

		if !n.hasEvidenceForOne(a, uint32(i), conjuncts) {
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
func (n *nodeContext) hasEvidenceForOne(all reqSets, i uint32, conjuncts []conjunctInfo) bool {
	a := all[i]
	for _, x := range conjuncts {
		if n.containsDefID(a.id, x.id) {
			return true
		}
	}

	embedScope, ok := all.lookupSet(a.embed)

	if !ok {
		return false
	}

	outerScope, ok := all.lookupSet(a.parent)

	if ok && outerScope.removed {
		return true
	}

outer:
	for _, c := range conjuncts {
		if n.containsDefID(embedScope.id, c.embed) {
			// Within the scope of the embedding.
			continue outer
		}

		if !ok || a.parent == 0 {
			return true
		}

		// If this conjunct is within the outer struct, but outside the
		// embedding scope, this means it was "added" and we do not have
		// to verify it within the embedding scope.
		if n.containsDefID(outerScope.id, c.id) {
			return true
		}
	}
	return false
}

func (n *nodeContext) containsDefID(node, child defID) bool {
	// TODO(perf): cache result
	// TODO(perf): we could keep track of the minimum defID that could map so
	// that we can use this to bail out early.
	c := n.ctx
	c.redirectsBuf = c.redirectsBuf[:0]
	for p := n; p != nil; p = p.node.Parent.state {
		if p.opID != n.opID {
			break
		}
		c.redirectsBuf = append(c.redirectsBuf, p.replaceIDs...)
		if p.node.Parent == nil {
			break
		}
	}

	if int64(len(c.redirectsBuf)) > c.stats.MaxRedirect {
		c.stats.MaxRedirect = int64(len(c.redirectsBuf))
	}

	return n.containsDefIDRec(node, child, child)
}

func (n *nodeContext) containsDefIDRec(node, child, start defID) bool {
	c := n.ctx

	// NOTE: this loop is O(H)
	for p := child; p != 0; {
		if p == node {
			return true
		}

		// TODO(perf): can be binary search if we keep redirects sorted. Also, p
		// should be monotonically decreasing, so we could use this to direct
		// the binary search or-- at the very least--to only have to pass the
		// array once.
		for _, r := range c.redirectsBuf {
			if r.to == p && r.from != child {
				if n.containsDefIDRec(node, r.from, start) {
					return true
				}
			}
		}

		p = c.containments[p]
		if p == start {
			// We won't match node we haven't already after one cycle.
			return false
		}
	}

	return child == node
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
	embed  defID // TODO(flatclose): can be removed later.
	kind   defIDType

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
		if nw.ctx != nil {
			nw.ctx.stats.ConjunctInfos++
			if len(nw.conjunctInfo) > int(nw.ctx.stats.MaxConjunctInfos) {
				nw.ctx.stats.MaxConjunctInfos = int64(len(nw.conjunctInfo))
			}
		}
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

// getReqSets initializes, if necessary, and returns the reqSets for n.
func getReqSets(n *nodeContext) reqSets {
	if n == nil {
		return nil
	}

	if n.computedCloseInfo {
		return n.reqSets
	}

	a := n.reqSets
	v := n.node

	if p := v.Parent; p != nil && !n.dropParentRequirements {
		a = append(a, getReqSets(p.state)...)
		n.filterNonRecursive(&a)
	}

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
			kind:    y.kind,
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

	// If 'v' is a hidden field, then all reqSets in 'a' for which there is no
	// corresponding entry in conjunctInfo should be removed from 'a'.
	if allowedInClosed(v.Label) {
		n.filterSets(&a, func(n *nodeContext, a *reqSet) bool {
			for _, c := range n.conjunctInfo {
				if n.containsDefID(a.id, c.id) {
					return true // keep the set
				}
			}
			return false // discard the set
		})
	}

	var parentConjuncts []conjunctInfo
	if p := v.Parent; p != nil && p.state != nil {
		parentConjuncts = p.state.conjunctInfo
	}

	n.filterTop(&a, n.conjunctInfo, parentConjuncts)

	n.computedCloseInfo = true
	if int64(len(a)) > n.ctx.stats.MaxReqSets {
		n.ctx.stats.MaxReqSets = int64(len(a))
	}
	n.reqSets = a
	return a
}

// If there is a top or ellipsis for all supported conjuncts, we have
// evidence that this node can be dropped.
func (n *nodeContext) filterTop(a *reqSets, conjuncts, parentConjuncts []conjunctInfo) (openLevel bool) {
	n.filterSets(a, func(n *nodeContext, a *reqSet) bool {
		var f conjunctFlags
		hasAny := false

		for _, c := range conjuncts {
			if n.containsDefID(a.id, c.id) {
				hasAny = true
				flags := c.flags
				if c.id < a.id {
					flags &^= cHasStruct
				}
				f |= flags
			}
		}
		if (f.hasTop() && !f.hasStruct()) || f.forceOpen() {
			return false
		}

		if hasAny && a.kind != defStruct {
			// fast path.
			return true
		}

		switch id := hasParentEllipsis(n, a, parentConjuncts); {
		case id == 0:
		case !hasAny:
			a.removed = true
		case a.kind != defStruct:
			// The following logic should only apply to non-structs.
		default:
			hasAny = false
			for _, c := range conjuncts {
				if n.containsDefID(id, c.id) {
					hasAny = true
				}
			}
			if !hasAny {
				a.removed = true
			}
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
func hasParentEllipsis(n *nodeContext, a *reqSet, conjuncts []conjunctInfo) defID {
	for _, c := range conjuncts {
		if !c.flags.hasEllipsis() {
			continue
		}
		if n.containsDefID(a.id, c.id) {
			return c.id
		}
	}
	return 0
}

func (n *nodeContext) filterNonRecursive(a *reqSets) {
	n.filterSets(a, func(n *nodeContext, e *reqSet) bool {
		x := e
		if x.once { //  || x.id == 0
			e.ignored = true
		}
		return true // keep the entry
	})
}

// filter keeps all reqSets e in a for which f(e) and removes the rest.
func (n *nodeContext) filterSets(a *reqSets, f func(n *nodeContext, e *reqSet) bool) {
	temp := (*a)[:0]
	for i := range *a {
		set := (*a)[i]

		if f(n, &set) {
			temp = append(temp, set)
		}
	}
	*a = temp
}

// lookupSet returns the set in a with the given id or nil if no such set.
func (a reqSets) lookupSet(id defID) (reqSet, bool) {
	if id != 0 {
		for i := range a {
			if a[i].id == id {
				return a[i], true
			}
		}
	}
	return reqSet{}, false
}
