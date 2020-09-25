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

// The file implements the majority of the closed struct semantics. The data is
// recorded in the Closed field of a Vertex.
//
// Each vertex has a set of conjuncts that make up the values of the vertex.
// Each Conjunct may originate from various sources, like an embedding, field
// definition or regular value. For the purpose of computing the value, the
// source of the conjunct is irrelevant. The origin does matter, however, for
// determining whether a field is allowed in a closed struct. The Closed field
// keeps track of the kind of origin for this purpose.
//
// More precisely, the CloseDef struct explains how the conjuncts of an arc were
// combined for instance due to a conjunction with closed struct or through an
// embedding. Each Vertex may be associated with a slice of CloseDefs. The
// position of a CloseDef in a file corresponds to an adt.ID.
//
// While evaluating each conjunct, new CloseDefs are added to indicate how a
// conjunct relates to its parent as needed. For instance, if a field references
// a definition, all other previous checks are useless, as the newly referred to
// definitions define an upper bound and will contain all the information that
// is necessary to determine whether a field may be included.
//
// Most of the logic in this file concerns itself with the combination of
// multiple CloseDef values as well as traversing the structure to validate
// whether an arc is allowed. The actual fieldSet logic is in optional.go The
// overall control and use of the functionality in this file is used in eval.go.

import (
	"fmt"

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
	Canopy []CloseDef
	Fields []*fieldSet

	// TODO: remove (unused as not fine-grained enough)
	// isClosed is now used as an approximate filter.
	isClosed bool
	isList   bool
	openList bool
}

func (a *acceptor) clone() *acceptor {
	canopy := make([]CloseDef, len(a.Canopy))
	copy(canopy, a.Canopy)
	for i := range canopy {
		canopy[i].IsClosed = false
	}
	return &acceptor{
		Canopy:   canopy,
		isClosed: a.isClosed,
	}
}

func (a *acceptor) Accept(c *adt.OpContext, f adt.Feature) bool {
	if a.isList {
		return a.openList
	}

	// TODO: remove these two checks and always pass InvalidLabel.
	if !a.isClosed {
		return true
	}
	if f == adt.InvalidLabel {
		return false
	}
	if f.IsInt() {
		return a.openList
	}
	return a.verifyArcAllowed(c, f, nil)
}

func (a *acceptor) MatchAndInsert(c *adt.OpContext, v *adt.Vertex) {
	a.visitAllFieldSets(func(fs *fieldSet) {
		fs.MatchAndInsert(c, v)
	})
}

func (a *acceptor) OptionalTypes() (mask adt.OptionalType) {
	a.visitAllFieldSets(func(f *fieldSet) {
		mask |= f.OptionalTypes()
	})
	return mask
}

// A disjunction acceptor represents a disjunction of all possible fields. Note
// that this is never used in evaluation as evaluation stops at incomplete nodes
// and a disjunction is incomplete. When the node is referenced, the original
// conjuncts are used instead.
//
// The value may be used in the API, though, where it may be an argument to
// UnifyAccept.
//
// TODO(perf): it would be sufficient to only implement the Accept method of an
// Acceptor. This could be implemented as an allocation-free wrapper type around
// a Disjunction. This will require a bit more API cleaning, though.
func newDisjunctionAcceptor(x *adt.Disjunction) adt.Acceptor {
	n := &acceptor{}

	for _, d := range x.Values {
		if a, _ := d.Closed.(*acceptor); a != nil {
			offset := n.InsertSubtree(0, nil, d, false)
			a.visitAllFieldSets(func(f *fieldSet) {
				g := *f
				g.id += offset
				n.insertFieldSet(g.id, &g)
			})
		}
	}

	return n
}

// CloseDef defines how individual fieldSets (corresponding to conjuncts)
// combine to determine whether a field is contained in a closed set.
//
// A CloseDef combines multiple conjuncts and embeddings. All CloseDefs are
// stored in slice. References to other CloseDefs are indices within this slice.
// Together they define the top of the tree of the expression tree of how
// conjuncts combine together (a canopy).
type CloseDef struct {
	Src adt.Node

	// And is used to track the IDs of a set of conjuncts. If IsDef or IsClosed
	// is true, a field is only allowed if at least one of the corresponding
	// fieldsets associated with this node or its embeddings allows it.
	//
	// And nodes are linked in a ring, meaning that the last node points back
	// to the first node. This allows a traversal of all and nodes to commence
	// at any point in the ring.
	And adt.ID

	// NextEmbed indicates the first ID for a linked list of embedded
	// expressions. The node corresponding to the actual embedding is at
	// position NextEmbed+1. The linked-list nodes all have a value of -1 for
	// And. NextEmbed is 0 for the last element in the list.
	NextEmbed adt.ID

	// IsDef indicates this node is associated with a definition and that all
	// expressions are recursively closed. This value is "sticky" when a child
	// node copies the closedness data from a parent node.
	IsDef bool

	// IsClosed indicates this node is associated with the result of close().
	// A child vertex should not "inherit" this value.
	IsClosed bool
}

func (n *CloseDef) isRequired() bool {
	return n.IsDef || n.IsClosed
}

const embedRoot adt.ID = -1

type Entry = fieldSet

func (c *acceptor) visitAllFieldSets(f func(f *fieldSet)) {
	for _, set := range c.Fields {
		for ; set != nil; set = set.next {
			f(set)
		}
	}
}

func (c *acceptor) visitAnd(id adt.ID, f func(id adt.ID, n CloseDef) bool) bool {
	for i := id; ; {
		x := c.Canopy[i]

		if !f(i, x) {
			return false
		}

		if i = x.And; i == id {
			break
		}
	}
	return true
}

func (c *acceptor) visitOr(id adt.ID, f func(id adt.ID, n CloseDef) bool) bool {
	if !f(id, c.Canopy[id]) {
		return false
	}
	return c.visitEmbed(id, f)
}

func (c *acceptor) visitEmbed(id adt.ID, f func(id adt.ID, n CloseDef) bool) bool {
	for i := c.Canopy[id].NextEmbed; i != 0; i = c.Canopy[i].NextEmbed {
		if id := i + 1; !f(id, c.Canopy[id]) {
			return false
		}
	}
	return true
}

func (c *acceptor) node(id adt.ID) *CloseDef {
	if len(c.Canopy) == 0 {
		c.Canopy = append(c.Canopy, CloseDef{})
	}
	return &c.Canopy[id]
}

func (c *acceptor) fieldSet(at adt.ID) *fieldSet {
	if int(at) >= len(c.Fields) {
		return nil
	}
	return c.Fields[at]
}

func (c *acceptor) insertFieldSet(at adt.ID, e *fieldSet) {
	c.node(0) // Ensure the canopy is at least length 1.
	if len(c.Fields) < len(c.Canopy) {
		a := make([]*fieldSet, len(c.Canopy))
		copy(a, c.Fields)
		c.Fields = a
	}
	e.next = c.Fields[at]
	c.Fields[at] = e
}

// InsertDefinition appends a new CloseDef to Canopy representing a reference to
// a definition at the given position. It returns the position of the new
// CloseDef.
func (c *acceptor) InsertDefinition(at adt.ID, src adt.Node) (id adt.ID) {
	if len(c.Canopy) == 0 {
		c.Canopy = append(c.Canopy, CloseDef{})
	}
	if int(at) >= len(c.Canopy) {
		panic(fmt.Sprintf("at >= len(canopy) (%d >= %d)", at, len(c.Canopy)))
	}
	// New there is a new definition, the parent location (invariant) is no
	// longer a required entry and could be dropped if there were no more
	// fields.
	//    #orig: #d     // only fields in #d are sufficient to check.
	//    #orig: {a: b}
	c.Canopy[at].IsDef = false

	id = adt.ID(len(c.Canopy))
	y := CloseDef{
		Src:       src,
		And:       c.Canopy[at].And,
		NextEmbed: 0,
		IsDef:     true,
	}
	c.Canopy[at].And = id
	c.Canopy = append(c.Canopy, y)

	return id
}

// InsertEmbed appends a new CloseDef to Canopy representing the use of an
// embedding at the given position. It returns the position of the new CloseDef.
func (c *acceptor) InsertEmbed(at adt.ID, src adt.Node) (id adt.ID) {
	if len(c.Canopy) == 0 {
		c.Canopy = append(c.Canopy, CloseDef{})
	}
	if int(at) >= len(c.Canopy) {
		panic(fmt.Sprintf("at >= len(canopy) (%d >= %d)", at, len(c.Canopy)))
	}

	id = adt.ID(len(c.Canopy))
	y := CloseDef{
		And:       -1,
		NextEmbed: c.Canopy[at].NextEmbed,
	}
	z := CloseDef{Src: src, And: id + 1}
	c.Canopy[at].NextEmbed = id
	c.Canopy = append(c.Canopy, y, z)

	return id + 1
}

// isComplexStruct reports whether the Closed information should be copied as a
// subtree into the parent node using InsertSubtree. If not, the conjuncts can
// just be inserted at the current ID.
func isComplexStruct(v *adt.Vertex) bool {
	m, _ := v.Value.(*adt.StructMarker)
	if m == nil {
		return true
	}
	a, _ := v.Closed.(*acceptor)
	if a == nil {
		return false
	}
	if a.isClosed {
		return true
	}
	switch len(a.Canopy) {
	case 0:
		return false
	case 1:
		// TODO: should we check for closedness?
		x := a.Canopy[0]
		return x.isRequired()
	}
	return true
}

// InsertSubtree inserts the closedness information of v into c as an embedding
// at the current position and inserts conjuncts of v into n (if not nil).
// It inserts it as an embedding and not and to cover either case. The idea is
// that one of the values were supposed to be closed, a separate node entry
// would already have been created.
func (c *acceptor) InsertSubtree(at adt.ID, n *nodeContext, v *adt.Vertex, cyclic bool) adt.ID {
	if len(c.Canopy) == 0 {
		c.Canopy = append(c.Canopy, CloseDef{})
	}
	if int(at) >= len(c.Canopy) {
		panic(fmt.Sprintf("at >= len(canopy) (%d >= %d)", at, len(c.Canopy)))
	}

	a := closedInfo(v)
	a.node(0)

	id := adt.ID(len(c.Canopy))
	y := CloseDef{
		And:       embedRoot,
		NextEmbed: c.Canopy[at].NextEmbed,
	}
	c.Canopy[at].NextEmbed = id

	c.Canopy = append(c.Canopy, y)
	id = adt.ID(len(c.Canopy))

	// First entry is at the embedded node location.
	c.Canopy = append(c.Canopy, a.Canopy...)

	// Shift all IDs for the new offset.
	for i, x := range c.Canopy[id:] {
		if x.And != -1 {
			c.Canopy[int(id)+i].And += id
		}
		if x.NextEmbed != 0 {
			c.Canopy[int(id)+i].NextEmbed += id
		}
	}

	if n != nil {
		for _, c := range v.Conjuncts {
			c = updateCyclic(c, cyclic, nil)
			c.CloseID += id
			n.addExprConjunct(c)
		}
	}

	return id
}

func (c *acceptor) verifyArc(ctx *adt.OpContext, f adt.Feature, v *adt.Vertex) (found bool, err *adt.Bottom) {

	defer ctx.ReleasePositions(ctx.MarkPositions())

	c.node(0) // ensure at least a size of 1.
	if c.verify(ctx, f) {
		return true, nil
	}

	// TODO: also disallow non-hidden definitions.
	if !f.IsString() && f != adt.InvalidLabel {
		return false, nil
	}

	if v != nil {
		for _, c := range v.Conjuncts {
			if pos := c.Field(); pos != nil {
				ctx.AddPosition(pos)
			}
		}
	}

	// collect positions from tree.
	for _, c := range c.Canopy {
		if c.Src != nil {
			ctx.AddPosition(c.Src)
		}
	}

	label := f.SelectorString(ctx)
	return false, ctx.NewErrf("field `%s` not allowed", label)
}

func (c *acceptor) verifyArcAllowed(ctx *adt.OpContext, f adt.Feature, v *adt.Vertex) bool {

	// TODO: also disallow non-hidden definitions.
	if !f.IsString() && f != adt.InvalidLabel {
		return true
	}

	defer ctx.ReleasePositions(ctx.MarkPositions())

	c.node(0) // ensure at least a size of 1.
	return c.verify(ctx, f)
}

func (c *acceptor) verify(ctx *adt.OpContext, f adt.Feature) bool {
	ok, required := c.verifyAnd(ctx, 0, f)
	return ok || (!required && !c.isClosed)
}

// verifyAnd reports whether f is contained in all closed conjuncts at id and,
// if not, whether the precense of at least one entry is required.
func (c *acceptor) verifyAnd(ctx *adt.OpContext, id adt.ID, f adt.Feature) (found, required bool) {
	for i := id; ; {
		x := c.Canopy[i]

		if ok, req := c.verifySets(ctx, i, f); ok {
			found = true
		} else if ok, isClosed := c.verifyEmbed(ctx, i, f); ok {
			found = true
		} else if req || x.isRequired() {
			// Not found for a closed entry so this indicates a failure.
			return false, true
		} else if isClosed {
			// The node itself isn't closed, but an embedding indicates it
			// should. See cue/testdata/definitions/embed.txtar.
			required = true
		}

		if i = x.And; i == id {
			break
		}
	}

	return found, required
}

// verifyEmbed reports whether any of the embeddings for the node at id allows f
// and, if not, whether the embeddings imply that the enclosing node should be
// closed. The latter is the case when embedded struct itself is closed.
func (c *acceptor) verifyEmbed(ctx *adt.OpContext, id adt.ID, f adt.Feature) (found, isClosed bool) {

	for i := c.Canopy[id].NextEmbed; i != 0; i = c.Canopy[i].NextEmbed {
		ok, req := c.verifyAnd(ctx, i+1, f)
		if ok {
			return true, false
		}
		if req {
			isClosed = true
		}
	}
	return false, isClosed
}

func (c *acceptor) verifySets(ctx *adt.OpContext, id adt.ID, f adt.Feature) (found, required bool) {
	o := c.fieldSet(id)
	if o == nil {
		return false, false
	}
	for isRegular := f.IsRegular(); o != nil; o = o.next {
		if isRegular && (len(o.additional) > 0 || o.isOpen) {
			return true, false
		}

		for _, g := range o.fields {
			if f == g.label {
				return true, false
			}
		}

		if !isRegular {
			continue
		}

		for _, b := range o.bulk {
			if b.check.Match(ctx, f) {
				return true, false
			}
		}
	}

	// TODO: this is the same location where code is registered as the old code,
	// but
	for o := c.Fields[id]; o != nil; o = o.next {
		if o.pos != nil {
			ctx.AddPosition(o.pos)
		}
	}
	return false, false
}

type info struct {
	referred bool
	up       adt.ID
	replace  adt.ID
	reverse  adt.ID
}

func (c *acceptor) Compact(all []adt.Conjunct) (compacted []CloseDef) {
	a := c.Canopy
	if len(a) == 0 {
		return nil
	}

	marked := make([]info, len(a))

	c.markParents(0, marked)

	// Mark all entries that cannot be dropped.
	for _, x := range all {
		c.markUsed(x.CloseID, marked)
	}

	// Compute compact numbers and reverse.
	k := adt.ID(0)
	for i, x := range marked {
		if x.referred {
			marked[i].replace = k
			marked[k].reverse = adt.ID(i)
			k++
		}
	}

	compacted = make([]CloseDef, k)

	for i := range compacted {
		orig := c.Canopy[marked[i].reverse]

		and := orig.And
		if and != embedRoot {
			and = marked[orig.And].replace
		}
		compacted[i] = CloseDef{
			Src:   orig.Src,
			And:   and,
			IsDef: orig.IsDef,
		}

		last := adt.ID(i)
		for or := orig.NextEmbed; or != 0; or = c.Canopy[or].NextEmbed {
			if marked[or].referred {
				compacted[last].NextEmbed = marked[or].replace
				last = marked[or].replace
			}
		}
	}

	// Update conjuncts
	for i, x := range all {
		all[i].CloseID = marked[x.ID()].replace
	}

	return compacted
}

func (c *acceptor) markParents(parent adt.ID, info []info) {
	// Ands are arranged in a ring, so check for parent, not 0.
	c.visitAnd(parent, func(i adt.ID, x CloseDef) bool {
		c.visitEmbed(i, func(j adt.ID, x CloseDef) bool {
			info[j-1].up = i
			info[j].up = i
			c.markParents(j, info)
			return true
		})
		return true
	})
}

func (c *acceptor) markUsed(id adt.ID, marked []info) {
	if marked[id].referred {
		return
	}

	if id > 0 && c.Canopy[id-1].And == embedRoot {
		marked[id-1].referred = true
	}

	for i := id; !marked[i].referred; i = c.Canopy[i].And {
		marked[i].referred = true
	}

	c.markUsed(marked[id].up, marked)
}
