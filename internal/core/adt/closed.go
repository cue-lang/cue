// Copyright 2020 CUE Authors
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

package adt

import (
	"iter"

	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/layer"
)

// This file implements the closedness algorithm.

// Outline of algorithm
//
// To compute closedness each Vertex is associated with a tree which has
// leaf nodes with sets of allowed labels, and interior nodes that describe
// how these sets may be combines: Or, for embedding, or And for definitions.
//
// Each conjunct of a Vertex is associated with such a leaf node. Each
// conjunct that evaluates to a struct is added to the list of Structs, which
// in the end forms this tree. If a conjunct is embedded, or references another
// struct or definition, it adds interior node to reflect this.
//
// To test whether a feature is allowed, it must satisfy the resulting
// expression tree.
//
// In order to avoid having to copy the tree for each node, the tree is linked
// from leaf node to root, rather than the other way around. This allows
// parent nodes to be shared as the tree grows and ensures that the growth
// of the tree is bounded by the number of conjuncts. As a consequence, this
// requires a two-pass algorithm:
//
//    - walk up to mark which nodes are required and count the number of
//      child nodes that need to be satisfied.
//    - verify fields in leaf structs and mark parent leafs as satisfied
//      when appropriate.
//
// A label is allowed if all required root nodes are marked as accepted after
// these two passes.
//

// A note on embeddings: it is important to keep track which conjuncts originate
// from an embedding, as an embedded value may eventually turn into a closed
// struct. Consider
//
//    a: {
//       b
//       d: e: int
//    }
//    b: d: {
//       #A & #B
//    }
//
// At the point of evaluating `a`, the struct is not yet closed. However,
// descending into `d` will trigger the inclusion of definitions which in turn
// causes the struct to be closed. At this point, it is important to know that
// `b` originated from an embedding, as otherwise `e` may not be allowed.

// TODO(perf):
// - less nodes
// - disable StructInfo nodes that can no longer pass a feature
// - sort StructInfos active ones first.

// TODO(errors): return a dedicated ConflictError that can track original
// positions on demand.

// IsRecursivelyClosed returns true if this value is either a definition or unified
// with a definition.
func (v *Vertex) IsRecursivelyClosed() bool {
	return v.ClosedRecursive
}

// closing returns how v is closed, as a builtin marked [Builtin.Closes]
// closes its argument.
func (v *Vertex) closing() Closing {
	switch {
	case v.ClosedRecursive:
		return ClosesRecursively
	case v.ClosedNonRecursive:
		return ClosesOnce
	}
	return NoClosing
}

// markClosed marks v as closed by a call to a builtin.
func (v *Vertex) markClosed(closes Closing) {
	switch closes {
	case ClosesOnce:
		v.ClosedNonRecursive = true
	case ClosesRecursively:
		v.ClosedRecursive = true
	}
}

// embedClosed records that a value closed as closes is embedded in the node
// through a spread from the scope id. The node is closed as strongly as any
// value it embeds: [Vertex.ApplyEmbedClosedness] applies that to the result
// of the __reclose builtin, and [nodeContext.recloseEmbedded] to a __reclose
// literal enclosing the spread.
func (n *nodeContext) embedClosed(id CloseInfo, closes Closing) {
	n.embedClosedness = max(n.embedClosedness, closes)
	n.recloseEmbedded(id, closes)
}

// recloseEmbedded closes the __reclose literal enclosing the scope id, if
// any, as a value embedded in it through a spread is closed: the literal is
// closed as strongly as any value it embeds. A spread of a close call or of
// another __reclose literal opens that scope and passes the closing on to
// the __reclose literal enclosing it in turn, while a closed scope which is
// not opened bounds the embedding.
func (n *nodeContext) recloseEmbedded(id CloseInfo, closes Closing) {
	if closes == NoClosing || n.ctx.OpenDef || id.opID != n.ctx.opID {
		return
	}
	// The containment chain leads from the scope of the spread through the
	// scopes enclosing it, the groups of values whose conjuncts are
	// scheduled here included, so that a definition named by a value
	// spread into the literal closes it however the value is reached: a
	// let or a field bound to the definition is spread before its own
	// closedness is known.
	for p := id.defID; p != 0; p = n.ctx.containments[p].id {
		i := n.reqIndexByID(p)
		if i < 0 {
			return
		}
		r := &n.reqDefIDs[i]
		if r.kind != defStruct {
			continue
		}
		if !r.reclose {
			if r.ignore {
				continue
			}
			return
		}
		if r.ignore || (closes == ClosesRecursively && !r.isRecursive) {
			n.invalidateReqSets()
		}
		r.ignore = false
		if closes == ClosesRecursively {
			r.isRecursive = true
		}
		n.node.markClosed(closes)
		return
	}
}

// ApplyEmbedClosedness closes v as strongly as any value embedded in it
// through a spread operator is closed. This is used by __reclose.
func (v *Vertex) ApplyEmbedClosedness() {
	if v.state != nil {
		v.markClosed(v.state.embedClosedness)
	}
}

// posInfo is a compact representation of position information for error reporting.
// It stores only the essential fields needed for tracking positions and priority,
// saving significant memory compared to the full CloseInfo struct.
// This is used for scalarID and kindID fields in nodeContext.
type posInfo struct {
	// opID is the generation of this conjunct, used for sanity check.
	opID uint64

	// defID is a unique ID to track anything that gets inserted from this
	// Conjunct.
	defID defID

	// Priority is used for default resolution. Higher values win. 0 means no
	// priority is assigned. Default handling may be more restrictive than
	// specified in the spec when a priority is assigned.
	Priority layer.Priority
}

// AncestorPositions returns an iterator over each parent of p,
// starting with the most immediate parent. This is used
// to add positions to errors that are associated with position info.
func (p posInfo) AncestorPositions(ctx *OpContext) iter.Seq[token.Pos] {
	return func(yield func(token.Pos) bool) {
		if p.opID != ctx.opID {
			return
		}
		for id := p.defID; id != 0; id = ctx.containments[id].id {
			pos := ctx.positionTable[ctx.containments[id].posIndex]
			if !yield(pos) {
				return
			}
		}
	}
}

type CloseInfo struct {
	// Embedded posInfo provides opID, defID, and priority fields.
	// These are the core fields needed for position tracking and priority comparison.
	posInfo

	enclosingEmbed defID // Tracks an embedding within a struct.
	outerID        defID // Tracks the {} that should be closed after unifying.

	// FromEmbed indicates whether this conjunct was inserted because of an
	// embedding.  This flag is sticky: it will be set for conjuncts created
	// from fields defined by this conjunct.
	// NOTE: only used when using closeContext.
	FromEmbed bool

	// FromDef indicates whether this conjunct was inserted because of a
	// definition. This flag is sticky: it will be set for conjuncts created
	// from fields defined by this conjunct.
	// NOTE: only used when using closeContext.
	FromDef bool

	// Like FromDef, but used by APIs to force FromDef to be true.
	TopDef bool

	// This conjunct was opened by the ... postfix operator.
	Opened bool

	// ConjunctOpened indicates that this conjunct is an operand of a
	// conjunction that was opened with ... (e.g. (#A & #B)...). The operand
	// itself is NOT opened (its close group stays active for mutual constraint
	// checking), but closeOuter is suppressed so that extra fields in the
	// enclosing struct are still allowed.
	//
	// TODO: consolidate with Opened. Since (#A & {})... is equivalent to
	// #A..., ConjunctOpened could replace Opened entirely: for single
	// references the pre-scan in hasEvidenceForOne produces the same result
	// as ignoring the resolver. This would require creating an embedding
	// scope for single opened references (not just conjunctions).
	ConjunctOpened bool

	CycleInfo
}

func (c CloseInfo) Location(ctx *OpContext) token.Pos {
	if c.opID != ctx.opID || c.defID == 0 {
		return token.NoPos
	}
	return ctx.positionTable[ctx.containments[c.defID].posIndex]
}

// IsDef reports whether an expressions is a reference that references a
// definition anywhere in its selection path.
//
// TODO(performance): this should be merged with resolve(). But for now keeping
// this code isolated makes it easier to see what it is for.
func IsDef(x Expr) (isDef bool, depth int) {
	switch r := x.(type) {
	case *FieldReference:
		isDef = r.Label.IsDef()

	case *SelectorExpr:
		isDef, depth = IsDef(r.X)
		depth++
		if r.Sel.IsDef() {
			isDef = true
		}

	case *IndexExpr:
		isDef, depth = IsDef(r.X)
		depth++
	}
	return isDef, depth
}

// isClosed reports whether v is closed at this level (so not recursively).
func isClosed(v *Vertex) bool {
	// We could have used IsRecursivelyClosed here, but (effectively)
	// implementing it again here allows us to only have to iterate over
	// Structs once.
	if v.ClosedRecursive || v.ClosedNonRecursive {
		return true
	}
	return false
}

// Accept determines whether f is allowed in n. It uses the OpContext for
// caching administrative fields.
func Accept(ctx *OpContext, n *Vertex, f Feature) (found, required bool) {
	return n.accept(ctx, f), true
}
