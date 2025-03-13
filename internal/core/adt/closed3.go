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

// XXX: rename close3.go to typocheck.go

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
// 		#A: b: {a: int}
// 		a: #A
//
// 		// Trigger typo check
// 		r1: #A
// 		r2: a
//
// 		// Do NOT trigger typo check
// 		r3: a.b
//
// In the case of r3, no typo check is triggered as the inserted value does
// not reference a definition directly. This choice is somewhat arbitrary. The
// main reason to pick this semantics is to be compatible with V2.
//
// ### Inline structs
//
// ### Close builtin
//
// ## Tracking Evidence
//
// ### Specializing definitions
//
// If a field that is inserted as a result of a typo-checked definition
// refers itself to a definition, then from that field forward it suffices
// to only track that definition, while tracking the originating typo-checked
// definition is no longer necessary. This is because the new definition
// is, by definition, strictly more specific than the original definition.
// This is a key optimization to avoid tracking too much information.
//
// ### Evidence sets and multiple insertions
//
// The same struct may be referred to within a node multiple times. If it is
// not itself a typo-checked definition, it may provide evidence for
// multiple other typo-checked definitions. To avoid having to process the
// conjuncts of such structs multiple times, we will still assign a unique
// identifier to such structs, and will annotate each typo-checked definition
// to which this applies that it is satisfied by this struct.
//
// In other words, each typo-checked struct may be associated with multiple
// unique identifiers that provide evidence for an allowed field.
//
// ### Embeddings
//
// Embeddings are a special case of structs that are not typo checked, but may
// provide evidence for other typo-checked definitions. As a struct associated
// with an embedding may be referred to multiple times, we will also assign a
// unique identifier to embeddings. This identifier is then also added to the
// evidence set of the typo-checked definition.
//
// ### Early terminate
//
// For definitions, typo checking proceeds recursively. However, typo checking
// is disabled for certain fields, even when a node still has subfields.
// Typo checking is disabled if a field has:
//
// - a "naked" top `_` (so not, for instance `{_}`),
// - an ellipsis `...`, or
// - it has a "non-concrete" validator, like matchn.
//
// In the latter case, the builtin takes over the responsibility of checking
// the type.

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
)

const deleteID defID = 0xffff

type defID uint32

type refInfo struct {
	v  *Vertex
	id defID
	// exclude
	// It would probably be more efficient to track a separate allow and deny
	// list, rather than tracking the entire tree except for the excluded.
	exclude defID

	ignore      bool
	placeholder bool
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
	kind  Kind
	flags conjunctFlags
}

func (c conjunctFlags) hasPlainTop() bool {
	return c&(cHasTop) != 0
}

func (c conjunctFlags) hasStruct() bool {
	return c&(cHasStruct) != 0
}

func (c conjunctFlags) forceOpen() bool {
	return c&(cHasEllipsis|cHasOpenValidator) != 0
}

type replaceID struct {
	from     defID
	to       defID
	add      bool
	headOnly bool
}

func (n *nodeContext) addMapping(x replaceID) {
	if x.from == x.to {
		return
	}
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
		id: id.defID, kind: k, flags: flags,
	})
}

func (n *nodeContext) addType(v *Vertex, id CloseInfo, forceIgnore bool) CloseInfo {
	if n.ctx.OpenDef {
		return id
	}

	// If this comes from an embedding we are ignoring it as a requirement.
	// We may still add an idea to track it as an equivalence, though.
	ignore := forceIgnore || id.FromEmbed || (!id.FromDef && !id.IsClosed)
	// TODO: this is more accurate, but is more restrictive than V2 in some
	// cases.
	// ignore := (id.FromEmbed || !id.FromDef) && !id.IsClosed
	if id.enclosingEmbed != 0 || !id.hasOuter {
		ignore = forceIgnore || (!id.FromDef && !id.IsClosed)
	}

	srcID := id.defID
	dstID := defID(0)
	for _, x := range n.reqDefIDs {
		if x.v == v {
			dstID = x.id
			break
		}
	}

	if dstID == 0 {
		n.ctx.nextDefID++
		dstID = n.ctx.nextDefID
		n.reqDefIDs = append(n.reqDefIDs, refInfo{
			v:       v,
			id:      dstID,
			exclude: id.enclosingEmbed,
			ignore:  ignore,
		})
	}
	id.defID = dstID

	if ignore && id.defID == 0 {
		// We have reserved the number. We know there is no requirement that
		// needs an addition ore replacement, though.
		return id
	}

	if id.FromDef || id.IsClosed {
		for i, x := range n.reqDefIDs {
			if x.id == id.outerID && id.hasOuter {
				n.reqDefIDs[i].ignore = false
				break
			}
		}
	}
	// if !ignore && id.enclosingEmbed != 0 {
	// 	for i, x := range n.reqDefIDs {
	// 		if x.id == id.enclosingEmbed {
	// 			n.reqDefIDs[i].ignore = false
	// 		}
	// 	}
	// }

	switch {
	case srcID == 0:
	case id.enclosingEmbed != 0:
		ph := id.outerID
		n.addMapping(replaceID{from: srcID, to: dstID, add: true})
		n.addMapping(replaceID{from: dstID, to: ph, add: true})
		id.enclosingEmbed = 0
	default:
		n.addMapping(replaceID{from: srcID, to: dstID, add: ignore})
	}

	return id
}

func (n *nodeContext) injectEmbedNode(id CloseInfo) CloseInfo {
	srcID := id.defID

	n.ctx.nextDefID++
	dstID := n.ctx.nextDefID
	n.reqDefIDs = append(n.reqDefIDs, refInfo{
		v:      emptyNode,
		id:     dstID,
		ignore: true,
	})
	id.defID = dstID
	id.enclosingEmbed = dstID

	n.addMapping(replaceID{from: srcID, to: dstID, add: true})

	return id
}

func (n *nodeContext) splitDefID(s *StructLit, id CloseInfo) CloseInfo {
	if n.ctx.OpenDef {
		return id
	}

	if s != nil { // TODO: move to caller
		if _, ok := s.Src.(*ast.File); ok {
			return id
		}
	}

	srcID := id.defID

	n.ctx.nextDefID++
	dstID := n.ctx.nextDefID
	n.reqDefIDs = append(n.reqDefIDs, refInfo{
		v:      emptyNode,
		id:     dstID,
		ignore: true,
	})
	id.defID = dstID

	if !id.hasOuter {
		id.hasOuter = true
		id.outerID = dstID
	}

	n.addMapping(replaceID{from: srcID, to: dstID, add: true})

	return id
}

func (n *nodeContext) checkTypos() {
	ctx := n.ctx
	if ctx.OpenDef {
		return

	}
	v := n.node
	z := v // keep around for debugging.
	_ = z
	v = v.DerefValue()

	if ctx.logID > 84 {
		// openDebugGraph(ctx, z, fmt.Sprintf("CHECK TYPOS"))
	}
	if x := n.node.Label; x == 1633 && n.node.Parent.Label == 1649 {
		// openDebugGraph(ctx, z, fmt.Sprintf("CHECK TYPOS"))
	}

	required := appendRequired(nil, n)

	if len(required) == 0 {
		return
	}
	// Avoid unnecessary errors.
	if b, ok := v.BaseValue.(*Bottom); ok && !b.CloseCheck {
		return
	}

	var err *Bottom
	// outer:
	for _, a := range v.Arcs {
		f := a.Label
		if a.IsFromDisjunction() {
			continue // Already checked in disjuncts.
		}

		// TODO(mem): child states of uncompleted nodes must have a state.
		na := a.state

		// TODO: this should not be necessary?
		required := slices.Clone(required) // TODO(perf): use buffer
		// do the right thing in appendRequired either way.
		required.replaceIDs(na.replaceIDs...)

		a = a.DerefDisjunct()
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

		if hasEvidenceForAll(required, na.conjunctInfo) {
			continue
		}

		// TODO: do not descend on optional?

		openDebugGraph(ctx, a, fmt.Sprintf("%p NOT ALLOWED", v))
		if b := ctx.notAllowedError(a); b != nil && a.ArcType <= ArcRequired {
			err = CombineErrors(nil, err, b)
		}
	}

	if err != nil {
		n.AddChildError(err)
	}
}

// hasEvidenceForAll reports whether there is evidence in a set of
// conjuncts for each of the typo-checked structs represented by
// the reqSets.
func hasEvidenceForAll(a reqSets, conjuncts []conjunctInfo) bool {
	for i := uint32(0); int(i) < len(a); i += a[i].size {
		if a[i].size == 0 {
			panic("unexpected set length")
		}
		if i+a[i].size > uint32(len(a)) {
			_ = i
		}
		if !hasEvidenceForOne(a[i:i+a[i].size], conjuncts) {
			// println("DENIED DENIED DENIED DENIED DENIED ", a[i].id)
			return false
		}
	}
	return true
}

// hasEvidenceForOne reports whether a single typo-checked set has evidence for
// any of its defIDs.
func hasEvidenceForOne(a reqSets, conjuncts []conjunctInfo) bool {
	for _, c := range conjuncts {
		for _, ri := range a {
			if c.id == ri.id {
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
// elements, next points to the next head element. All elements up to the next
// head element represent defIDs that satisfy the requirement of the head and
// have next set to 0.
type reqSets []reqSet

// A single reqID might be satisfied by multiple defIDs, if the definition
// associated with the reqID embeds other definitions, for instance. In this
// case we keep a linked list of defIDs that may also be satisfied.
//
// This type is used in [nodeContext.equivalences] as follows:
//   - if an embedding is used by a definition, it is inserted in the list
//     pointed to by its refInfo. Similarly,
//     refInfo is added to the list
type reqSet struct {
	id defID
	// size is the number of elements in the set. This is only set for the head.
	// Entries with equivalence IDs have size set to 0.
	size uint32
	del  defID // TODO(flatclose): can be removed later.
	once bool
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

// replaceIDs replaces defIDs mappings in the receiving reqSets in place.
//
// The following rules apply:
//
//   - Mapping a typo-checked definition to a new typo-checked definition replaces
//     the set from the "from" definition in its entirety. It is assumed that the
//     set is already added to the requirements.
//
//   - A mapping from a typo-checked definition to 0 removes the set from the
//     requirements. This typically comes from _ or ....
//
//   - A mapping from an equivalence (non-head) value to 0 removes the
//     equivalence. This does not change the outcome of typo checking, but
//     reduces the size of the equivalence list, which helps performance.
//
//   - A mapping from an equivalence (non-head) value to a new value widens the
//     allowed values for the respective set by adding the new value to the
//     equivalence list.
//
// In words;
// - Definition: if not in embed, create new group
// - If in active definition, replace old definition
// - If in embed, replace embed in respective sets. definition starts new group
// - child definition replaces parent definition
func (a *reqSets) replaceIDs(b ...replaceID) {
	temp := *a
	temp = temp[:0]
	var buf reqSets
outer:
	for i := 0; i < len(*a); {
		e := (*a)[i]
		if e.size != 0 {
			// If the head is dropped, the entire group is deleted.
			for _, x := range b {
				if e.id == x.from && !x.add {
					i += int(e.size)
					continue outer
				}
			}
			if len(buf) > 0 {
				if len(temp)+len(buf) > i {
					*a = slices.Replace(*a, len(temp), i, buf...)
					i = len(temp) + len(buf)
					temp = (*a)[:i]
				} else {
					temp = append(temp, buf...)
				}
				buf = buf[:0] // TODO: perf use OpContext buffer.
			}
		}

		buf = transitiveMapping(buf, e, b)
		buf[0].size = uint32(len(buf))

		i++
	}
	if len(buf) > 0 {
		temp = append(temp, buf...)
	}
	*a = temp
}

func transitiveMapping(buf reqSets, x reqSet, b []replaceID) reqSets {
	// Trim subtree for embedded conjunctions.
	if len(buf) > 0 && buf[0].del == x.id {
		return buf
	}

	// do not add duplicates
	for _, y := range buf {
		if x.id == y.id {
			return buf
		}
	}

	buf = append(buf, x)

	for _, y := range b {
		if x.id == y.from {
			if y.to == deleteID { // do we need this?
				buf = buf[:len(buf)-1]
				return buf
			}
			buf = transitiveMapping(buf, reqSet{id: y.to, once: x.once}, b)
		}
	}
	return buf
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

// appendRequired
func appendRequired(a reqSets, n *nodeContext) reqSets {
	_ = reqSet{}
	if n == nil {
		return a
	}
	v := n.node
	if !n.dropParentRequirements {
		if p := v.Parent; p != nil {
			a = appendRequired(a, p.state)
		}
	}
	a.filterNonRecursive()

outer:
	for _, y := range n.reqDefIDs {
		if y.ignore {
			continue
		}
		for _, x := range a {
			if x.id == y.id {
				continue outer
			}
		}
		once := false
		if y.v != nil {
			once = !y.v.ClosedRecursive
		}

		a = append(a, reqSet{
			id:   y.id,
			once: once,
			del:  y.exclude,
			size: 1,
		})
	}

	a.replaceIDs(n.replaceIDs...)

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

	a.filterTop(n.conjunctInfo)

	return a
}

// If there is a top or ellipsis for all supported conjuncts, we have
// evidence that this node can be dropped.
func (a *reqSets) filterTop(conjuncts []conjunctInfo) {
	a.filterSets(func(a []reqSet) bool {
		var f conjunctFlags
		for _, e := range a {
			for _, c := range conjuncts {
				if e.id != c.id {
					continue
				}
				f |= c.flags
			}
		}
		if (f.hasPlainTop() && !f.hasStruct()) || f.forceOpen() {
			return false
		}
		return true
	})
}

func (a *reqSets) filterNonRecursive() {
	a.filterSets(func(e []reqSet) bool {
		x := e[0]
		if x.once { //  || x.id == 0
			return false // discard the entry
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

// filter keeps all elements e in a for which f(e) and removes the rest.
// If f(e) is false for a group head, the entire group is removed.
func (a *reqSets) filter(f func(e reqSet) bool) {
	temp := (*a)[:0]
	lastHead := -1
	for i := 0; i < len(*a); {
		e := (*a)[i]
		switch {
		case f(e):
			if e.size != 0 {
				lastHead = len(temp)
			}
			temp = append(temp, e)
			i++

		case e.size != 0:
			// force crash if an equivalence is removed before a new head is
			// found.
			lastHead = -1
			i += int(e.size)

		default:
			temp[lastHead].size--
			i++
		}
	}
	*a = temp
}
