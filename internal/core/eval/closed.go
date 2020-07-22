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

package eval

// The file implements the majority of the closed struct semantics.
// The data is recorded in the Closed field of a Vertex.
//
// Each vertex has a set of conjuncts that make up the values of the vertex.
// Each Conjunct may originate from various sources, like an embedding, field
// definition or regular value. For the purpose of computing the value, the
// source of the conjunct is irrelevant. The origin does matter, however, if
// for determining whether a field is allowed in a closed struct. The Closed
// field keeps track of the kind of origin for this purpose.
//
// More precisely, the CloseDef struct explains how the conjuncts of an arc
// were combined and define a logical expression on the field sets
// computed for each conjunct.
//
// While evaluating each conjunct, nodeContext keeps track what changes need to
// be made to ClosedDef based on the evaluation of the current conjuncts.
// For instance, if a field references a definition, all other previous
// checks are useless, as the newly referred to definitions define an upper
// bound and will contain all the information that is necessary to determine
// whether a field may be included.
//
// Most of the logic in this file concerns itself with the combination of
// multiple CloseDef values as well as traversing the structure to validate
// whether an arc is allowed. The actual fieldSet logic is in optional.go
// The overal control and use of the functionality in this file is used
// in eval.go.

import (
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

// acceptor implements adt.Acceptor.
//
// Note that it keeps track of whether it represents a closed struct. An
// acceptor is also used to associate an CloseDef with a Vertex, and not
// all CloseDefs represent a closed struct: a value that contains embeddings may
// eventually turn into a closed struct. Consider
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
//
type acceptor struct {
	tree     *CloseDef
	fields   []fieldSet
	isClosed bool
	isList   bool
	openList bool
}

func (a *acceptor) Accept(c *adt.OpContext, f adt.Feature) bool {
	if a.isList {
		return a.openList
	}
	if !a.isClosed {
		return true
	}
	if f == adt.InvalidLabel {
		return false
	}
	if f.IsInt() {
		return a.openList
	}
	return a.verifyArcAllowed(c, f) == nil
}

func (a *acceptor) MatchAndInsert(c *adt.OpContext, v *adt.Vertex) {
	for _, fs := range a.fields {
		fs.MatchAndInsert(c, v)
	}
}

func (a *acceptor) OptionalTypes() (mask adt.OptionalType) {
	for _, f := range a.fields {
		mask |= f.OptionalTypes()
	}
	return mask
}

// CloseDef defines how individual FieldSets (corresponding to conjuncts)
// combine to determine whether a field is contained in a closed set.
//
// Nodes with a non-empty List and IsAnd is false represent embeddings.
// The ID is the node that contained the embedding in that case.
//
// Nodes with a non-empty List and IsAnd is true represent conjunctions of
// definitions. In this case, a field must be contained in each definition.
//
// If a node has both conjunctions of definitions and embeddings, only the
// former are maintained. Conjunctions of definitions define an upper bound
// of the set of allowed fields in that case and the embeddings will not add
// any value.
type CloseDef struct {
	ID    uint32
	IsAnd bool
	List  []*CloseDef
}

// isOr reports whether this is a node representing embeddings.
func isOr(c *CloseDef) bool {
	return len(c.List) > 0 && !c.IsAnd
}

// updateClosed transforms c into a new node with all non-AND nodes with an
// ID matching one in replace substituted with the replace value.
//
// Vertex only keeps track of a flat list of conjuncts and does not keep track
// of the hierarchy of how these were derived. This function allows rewriting
// a CloseDef tree based on replacement information gathered during evaluation
// of this flat list.
//
func updateClosed(c *CloseDef, replace map[uint32]*CloseDef) *CloseDef { // used in eval.go
	switch {
	case c == nil:
		and := []*CloseDef{}
		for _, c := range replace {
			and = append(and, c)
		}
		switch len(and) {
		case 0:
		case 1:
			c = and[0]
		default:
			c = &CloseDef{IsAnd: true, List: and}
		}
		// needClose
	case len(replace) > 0:
		c = updateClosedRec(c, replace)
	}
	return c
}

func updateClosedRec(c *CloseDef, replace map[uint32]*CloseDef) *CloseDef {
	if c == nil {
		return nil
	}

	// If c is a leaf or AND node, replace it outright. If both are an OR node,
	// merge the lists.
	if len(c.List) == 0 || !c.IsAnd {
		if sub := replace[c.ID]; sub != nil {
			if isOr(sub) && isOr(c) {
				sub.List = append(sub.List, c.List...)
			}
			return sub
		}
	}

	changed := false
	buf := make([]*CloseDef, len(c.List))
	k := 0
	for _, c := range c.List {
		n := updateClosedRec(c, replace)
		changed = changed || n != c
		if n != nil {
			buf[k] = n
			k++
		}
	}
	if !changed {
		return c
	}

	if k == 1 {
		return buf[0]
	}

	return &CloseDef{ID: c.ID, IsAnd: c.IsAnd, List: buf[:k]}
}

// UpdateReplace is called after evaluating a conjunct at the top of the arc
// to update the replacement information with the gathered CloseDef info.
func (n *nodeContext) updateReplace(env *adt.Environment) { // used in eval.go
	if n.newClose == nil {
		return
	}

	if n.replace == nil {
		n.replace = make(map[uint32]*CloseDef)
	}

	id := uint32(0)
	if env != nil {
		id = env.CloseID
	}

	n.replace[id] = updateClose(n.replace[id], n.newClose)
	n.newClose = nil
}

// appendList creates a new CloseDef with the elements of the list of orig
// and updated appended. It will take the ID of orig. It does not alter
// either orig or update.
func appendLists(orig, update *CloseDef) *CloseDef {
	list := make([]*CloseDef, len(orig.List)+len(update.List))
	copy(list[copy(list, orig.List):], update.List)
	c := *orig
	c.List = list
	return &c
}

// updateClose merges update into orig without altering either.
//
// The merge takes into account whether it is an embedding node or not.
// Most notably, if an "And" node is combined with an embedding, the
// embedding information may be discarded.
func updateClose(orig, update *CloseDef) *CloseDef {
	switch {
	case orig == nil:
		return update
	case isOr(orig):
		if !isOr(update) {
			return update
		}
		return appendLists(orig, update)
	case isOr(update):
		return orig
	case len(orig.List) == 0 && len(update.List) == 0:
		return &CloseDef{IsAnd: true, List: []*CloseDef{orig, update}}
	case len(orig.List) == 0:
		update.List = append(update.List, orig)
		return update
	default: // isAnd(orig)
		return appendLists(orig, update)
	}
}

func (n *nodeContext) addAnd(c *CloseDef) { // used in eval.go
	switch {
	case n.newClose == nil:
		n.newClose = c
	case isOr(n.newClose):
		n.newClose = c
	case len(n.newClose.List) == 0:
		n.newClose = &CloseDef{
			IsAnd: true,
			List:  []*CloseDef{n.newClose, c},
		}
	default:
		n.newClose.List = append(n.newClose.List, c)
	}
}

func (n *nodeContext) addOr(parentID uint32, c *CloseDef) { // used in eval.go
	switch {
	case n.newClose == nil:
		d := &CloseDef{ID: parentID, List: []*CloseDef{{ID: parentID}}}
		if c != nil {
			d.List = append(d.List, c)
		}
		n.newClose = d
	case isOr(n.newClose):
		d := n.newClose
		if c != nil {
			d.List = append(d.List, c)
		}
	}
}

// verifyArcAllowed checks whether f is an allowed label within the current
// node. It traverses c considering the "or" semantics of embeddings and the
// "and" semantics of conjunctions. It generates an error if a field is not
// allowed.
func (n *acceptor) verifyArcAllowed(ctx *adt.OpContext, f adt.Feature) *adt.Bottom {
	filter := f.IsString() || f == adt.InvalidLabel
	if filter && !n.verifyArcRecursive(ctx, n.tree, f) {
		label := f.SelectorString(ctx)
		return &adt.Bottom{
			Err: errors.Newf(token.NoPos, "field `%s` not allowed", label),
		}
	}
	return nil
}

func (n *acceptor) verifyArcRecursive(ctx *adt.OpContext, c *CloseDef, f adt.Feature) bool {
	if len(c.List) == 0 {
		return n.verifyDefinition(ctx, c.ID, f)
	}
	if c.IsAnd {
		for _, c := range c.List {
			if !n.verifyArcRecursive(ctx, c, f) {
				return false
			}
		}
		return true
	}
	for _, c := range c.List {
		if n.verifyArcRecursive(ctx, c, f) {
			return true
		}
	}
	return false
}

// verifyDefinition reports whether f is a valid member for any of the fieldSets
// with the same closeID.
func (n *acceptor) verifyDefinition(ctx *adt.OpContext, closeID uint32, f adt.Feature) (ok bool) {
	for _, o := range n.fields {
		if o.env.CloseID != closeID {
			continue
		}

		if len(o.additional) > 0 || o.isOpen {
			return true
		}

		for _, g := range o.fields {
			if f == g.label {
				return true
			}
		}

		for _, b := range o.bulk {
			if b.check.Match(ctx, f) {
				return true
			}
		}
	}
	return false
}
