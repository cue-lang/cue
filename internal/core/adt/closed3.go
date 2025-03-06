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

// TODO: Describe new closedness algorithm.

import (
	"slices"
)

type refInfo struct {
	v  *Vertex
	id defID
}

type defID uint32

type conjunctFlags uint8

const (
	cEllipsis conjunctFlags = 1 << iota
	cHasOpenValidator
)

type conjunctInfo struct {
	id    defID
	kind  Kind
	flags conjunctFlags
}

func (c conjunctInfo) hasEllipsis() bool {
	return c.flags&cEllipsis != 0
}

func (c conjunctInfo) isAny() bool {
	return c.kind&TopKind == TopKind || c.flags&cHasOpenValidator != 0
}

func rmDropped(a []refInfo, b ...defID) []refInfo {
	temp := a[:0]
outer:
	for _, e := range a {
		for _, x := range b {
			if e.id == x {
				continue outer
			}
		}
		temp = append(temp, e)
	}
	a = temp
	return a
}

func filterNonRecursive(a []refInfo) []refInfo {
	temp := a[:0]
	for _, e := range a {
		if e.v.ClosedRecursive && e.id != 0 {
			temp = append(temp, e)
		}
	}
	a = temp
	return a
}

func mergeCloseInfo(v, w *Vertex) {
	if w == nil {
		return
	}
	// Merge missing closeInfos
outer:
	for _, wci := range w.conjunctInfo {
		for _, vci := range v.conjunctInfo {
			if wci.id == vci.id {
				continue outer
			}
		}
		v.conjunctInfo = append(v.conjunctInfo, wci)
	}

outer2:
	for _, d := range w.dropDefIDs {
		for _, vd := range v.dropDefIDs {
			if d == vd {
				continue outer2
			}
		}
		v.dropDefIDs = append(v.dropDefIDs, d)
	}

	// Recurse for arcs
	for _, wa := range w.Arcs {
		for _, va := range v.Arcs {
			if va.Label == wa.Label {
				mergeCloseInfo(va, wa)
				break
			}
		}
	}
}

func appendRequired(a []refInfo, v *Vertex) []refInfo {
	if p := v.Parent; p != nil {
		a = appendRequired(a, p)
	}
	a = filterNonRecursive(a)

outer:
	for _, y := range v.reqDefIDs {
		for _, x := range a {
			if x.id == y.id {
				continue outer
			}
		}
		a = append(a, y)
	}

	// If 'v' is a hidden field, then all entries in 'a' for which there is no
	// corresponding entry in conjunctInfo should be removed from 'a'.
	if allowedInClosed(v.Label) {
		filtered := a[:0]
	outer2:
		for _, e := range a {
			for _, c := range v.conjunctInfo {
				if c.id == e.id {
					filtered = append(filtered, e)
					continue outer2
				}
			}
		}
		a = filtered
	}

	for _, c := range v.conjunctInfo {
		if c.isAny() || c.hasEllipsis() {
			a = rmDropped(a, c.id)
		}
	}
	a = rmDropped(a, v.dropDefIDs...)
	return a
}

func (n *nodeContext) removeRequired(id defID) {
	x := n.node
	// if i := slices.Index(n.reqDefIDs, id); i >= 0 {
	// 	n.reqDefIDs = slices.Delete(n.reqDefIDs, i, i+1)
	// }
	x.dropDefIDs = append(x.dropDefIDs, id)
}

func (n *nodeContext) updateConjunctInfo(k Kind, id CloseInfo, flags conjunctFlags) {
	if n.ctx.OpenDef {
		return
	}

	for i, c := range n.node.conjunctInfo {
		if c.id == id.defID {
			n.node.conjunctInfo[i].kind &= k
			n.node.conjunctInfo[i].flags |= flags
			return
		}
	}
	n.node.conjunctInfo = append(n.node.conjunctInfo, conjunctInfo{
		id: id.defID, kind: k,
	})
}

func (n *nodeContext) addType(v *Vertex, id CloseInfo) CloseInfo {
	if n.ctx.OpenDef {
		return id
	}

	switch {
	case id.FromEmbed:
		// n.embeddings = append(n.embeddings, v)
	case v.ClosedNonRecursive:
		id.IsClosed = true
		if id.defID != 0 {
			break
		}

		fallthrough

	default:
		// XXX: do not also add type candidates
		if !slices.Contains(n.typeCandidates, v) {
			n.typeCandidates = append(n.typeCandidates, v)
			// If this conjunct originates from another ID, we can safely
			// delete it, as the new definition necessarily constraints all
			// other fields.
			if id.defID != 0 {
				// openDebugGraph(n.ctx, n.node, "addType")
				n.removeRequired(id.defID)
			}
			n.ctx.nextDefID++
			id.defID = n.ctx.nextDefID
			n.node.reqDefIDs = append(n.node.reqDefIDs, refInfo{v: v, id: id.defID})
		}
	}
	return id
}

func (n *nodeContext) checkTypos() {
	c := n.ctx
	v := n.node

	if err := v.checkFields2(c, true, v.reqDefIDs...); err != nil {
		n.AddChildError(err)
	}
}

func (v *Vertex) checkFields2(ctx *OpContext, recursive bool, required ...refInfo) (err *Bottom) {
	if ctx.OpenDef {
		return nil

	}
	z := v
	_ = z
	v = v.DerefValue()

	required = appendRequired(nil, v)

	for _, c := range v.conjunctInfo {
		if c.isAny() {
			required = rmDropped(required, c.id)
		}
	}

	if len(required) == 0 {
		return nil
	}

outer:
	for _, a := range v.Arcs {
		f := a.Label
		if a.IsFromDisjunction() {
			continue // Already checked in disjuncts.
		}
		if a.IsShared {
			continue // Avoid exponential runtime. Assume this is checked already.
		}

		// do the right thing in appendRequired either way.
		filtered := rmDropped(required, a.dropDefIDs...)
		a = a.DerefValue()
		// TODO(perf): somehow prevent error generation of recursive structures,
		// or at least make it cheap. Right now if this field is a typo, likely
		// all descendents will be regarded as typos.
		if b, ok := a.BaseValue.(*Bottom); ok {
			if !b.CloseCheck {
				continue
			}
		}

		// openDebugGraph(ctx, v, "checkFields2: NOT FOUND")
		found := true
	outer2:
		for _, ri := range filtered {
			for _, c := range a.conjunctInfo {
				if c.id == ri.id {
					continue outer2
				}
			}
			found = false
			break
		}

		required = rmDropped(required, a.dropDefIDs...) // XXX: remove

		switch {
		case !recursive:
			continue
		case found:
			continue
		}

		if allowedInClosed(f) {
			continue
		}

		// TODO: do not descend on optional?

		if pc := v.PatternConstraints; pc != nil {
			for _, p := range pc.Pairs {
				if matchPattern(ctx, p.Pattern, f) {
					continue outer
				}
			}
		}

		// openDebugGraph(ctx, a, fmt.Sprintf("%p NOT ALLOWED", v))
		if b := ctx.notAllowedError(a); b != nil && a.ArcType <= ArcRequired {
			err = CombineErrors(nil, err, b)
		}
	}

	return err
}
