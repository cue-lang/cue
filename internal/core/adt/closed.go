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

// ShouldRecursivelyClose reports whether this vertex should be closed
// recursively using __reclose. This is to simulate compatibility mode
// with the semantics from before explicitOpen was introduced.
//
// This is the case if any of the embeddings marked with ... were recursively
// closed before opening them up with ....
func (v *Vertex) ShouldRecursivelyClose() bool {
	if v.state == nil {
		return false
	}
	return v.state.embedsRecursivelyClosed
}

type CloseInfo struct {
	// defID is a unique ID to track anything that gets inserted from this
	// Conjunct.
	opID           uint64 // generation of this conjunct, used for sanity check.
	defID          defID
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

	// Priority is used for default resolution. Higher values win. 0 means no
	// priority is assigned. Default handling may be more restrictive than
	// specified in the spec when a priority is assigned.
	Priority layer.Priority

	CycleInfo
}

func (c CloseInfo) Location(ctx *OpContext) Node {
	if c.opID != ctx.opID || c.defID == 0 {
		return nil
	}
	return ctx.containments[c.defID].n
}

// AncestorPositions returns an iterator over each parent of c,
// starting with the most immediate parent. This is used
// to add positions to errors that are associated with a CloseInfo.
func (c *CloseInfo) AncestorPositions(ctx *OpContext) iter.Seq[Node] {
	return func(yield func(Node) bool) {
		if c.opID != ctx.opID {
			return
		}
		for p := c.defID; p != 0; p = ctx.containments[p].id {
			if !yield(ctx.containments[p].n) {
				return
			}
		}
	}
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
