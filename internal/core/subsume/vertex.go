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

package subsume

import (
	"fmt"
	"slices"

	"cuelang.org/go/internal/core/adt"
)

// Notes:
//   - Can optional fields of y can always be ignored here? Maybe not in the
//     schema case.
//   - Definitions of y can be ignored in data mode.
//
// TODO(perf): use merge sort where possible.
func (s *subsumer) vertices(x, y *adt.Vertex) bool {
	if x == y {
		return true
	}
	// A vertex which shares another has its own arc type, so compare them
	// before dereferencing either.
	if a, b := x.ArcType, y.ArcType; a < b {
		return false
	}

	// A reference opened with the postfix ... operator opens the vertex it
	// shares at every level. Only the vertex as found records that, so it
	// is carried over to the vertices below the shared one.
	xOpened := s.xOpened || x.IsOpened()
	yOpened := s.yOpened || y.IsOpened()
	xFound, yFound := x, y

	if s.Defaults {
		y = s.defaultOf(y)
	}

	// x0 and y0 are the vertices as found, which may share x and y.
	x0, y0 := x, y
	x, y = x.DerefValue(), y.DerefValue()

	defer func(x, y bool) { s.xOpened, s.yOpened = x, y }(s.xOpened, s.yOpened)
	s.xOpened = s.xOpened || (xOpened && x != xFound)
	s.yOpened = s.yOpened || (yOpened && y != yFound)

	if b := y.Bottom(); b != nil {
		// If the value is incomplete, the error is not final. So either check
		// structural equivalence or return an error.
		return !b.IsIncomplete()
	}

	ctx := s.ctx

	final := y.IsData() || s.Final

	yStructClosed := !yOpened && isClosed(y0, y)
	xClosed := !xOpened && isClosed(x0, x) && !s.IgnoreClosedness
	// TODO: this should not close for taking defaults. Do a more principled
	// makeover of this package before making it public, though.
	yClosed := s.Final || s.Defaults ||
		(yStructClosed && !s.IgnoreClosedness)

	if x == y && (!xClosed || yClosed || final) {
		// x0 and y0 share one vertex, and x0 does not close it further.
		return true
	}

	switch v := x.BaseValue.(type) {
	case *adt.Bottom:
		return false

	case *adt.ListMarker:
		if !y.IsList() {
			s.errf("list does not subsume %v (type %s)", y, y.Kind())
			return false
		}
		if !s.listVertices(x, y, final) {
			return false
		}
		// TODO: allow other arcs alongside list arc.
		return true

	case *adt.StructMarker:
		_, ok := y.BaseValue.(*adt.StructMarker)
		if !ok {
			return false
		}

	case adt.Value:
		if !s.values(v, y.Value()) {
			return false
		}

		// Embedded scalars could still have arcs.
		if final {
			return true
		}

	case nil:
		return false

	default:
		panic(fmt.Sprintf("unexpected type %T", v))
	}

	if xClosed && !yClosed && !final {
		return false
	}

	// From here, verticesDev differs significantly from vertices.

	for _, a := range x.Arcs {
		f := a.Label
		if s.Final && !f.IsRegular() {
			continue
		}

		b := y.LookupRaw(f)
		isConstraint := false
		switch a.ArcType {
		case adt.ArcOptional:
			if s.IgnoreOptional {
				continue
			}

			if a.Kind() == adt.TopKind {
				continue
			}

			isConstraint = true

		case adt.ArcRequired:
			if s.IgnoreOptional {
				continue
			}
			// Whether y is open, closed, or data, it admits values without
			// f, none of which are instances of x.
			if b == nil {
				s.missing = f
				s.errf("required field %v not present in %v", f, y)
				return false
			}
			// A top value does not constrain a field that y has or
			// requires, even one whose value is incomplete.
			if a.Kind() == adt.TopKind && b.ArcType <= adt.ArcRequired {
				continue
			}
		}

		if b == nil {
			if !isConstraint {
				s.errf("regular field is constraint in subsumed value: %v", f)
				return false
			}

			// If f is undefined for y and if y is closed, the field is
			// implicitly defined as _|_, which an optional field subsumes.
			if (!yOpened && !y0.Accept(ctx, f)) || y.IsData() || s.Final {
				continue
			}

			// There is no explicit field, but the values of pattern constraints
			// may still be relevant.
			b = &adt.Vertex{Label: f}
			y.MatchAndInsert(ctx, b)
			b.Finalize(ctx)
		}

		if s.values(a, b) {
			continue
		}

		s.missing = f
		s.gt = a
		s.lt = y

		s.errf("field %v not present in %v", f, y)
		return false
	}

	if xClosed && !yClosed && !s.Final {
		s.errf("closed struct does not subsume open struct")
		return false
	}

outer:
	for _, b := range y.Arcs {
		f := b.Label

		if s.Final && !f.IsRegular() {
			continue
		}

		if b.IsConstraint() && (s.IgnoreOptional || s.Final) {
			continue
		}

		for _, a := range x.Arcs {
			g := a.Label
			if g == f {
				// already validated
				continue outer
			}
		}

		if !xOpened && !x0.Accept(ctx, f) {
			if s.Profile.IgnoreClosedness {
				continue
			}
			s.errf("field not allowed in closed struct: %v", f)
			return false
		}

		a := &adt.Vertex{Label: f}
		x.MatchAndInsert(ctx, a)
		if !a.HasConjuncts() {
			// It is accepted and has no further constraints, so all good.
			continue
		}

		a.Finalize(ctx)
		// x has no field f, so its patterns only constrain an optional one.
		// Setting this when creating a would be lost, as MatchAndInsert
		// adds a pattern's value as that of a regular field.
		a.ArcType = adt.ArcOptional

		if !s.vertices(a, b) {
			return false
		}
	}

	return s.patterns(x, y, xClosed, yStructClosed, final)
}

// isClosed reports whether the struct v allows no labels beyond its fields
// and patterns. v0 is the vertex as found, which may share v and be closed
// by itself.
func isClosed(v0, v *adt.Vertex) bool {
	return !v0.IsOpenStruct() || !v.IsOpenStruct()
}

// patterns compares the pattern constraints of x and y, given whether each
// is closed, meaning it allows no labels beyond its fields and patterns.
func (s *subsumer) patterns(x, y *adt.Vertex, xClosed, yClosed, final bool) bool {
	apc := x.PatternConstraints
	bpc := y.PatternConstraints
	if bpc == nil {
		if apc == nil {
			return true
		}
		if yClosed || final {
			// This is a special case where know that any allowed optional field
			// in a must be bottom in y, which is strictly more specific.
			return true
		}
		return false
	}
	var apairs []adt.PatternConstraint
	if apc != nil {
		apairs = apc.Pairs
	}
	switch {
	case len(apairs) > len(bpc.Pairs):
		// Theoretically it is still possible for a to subsume b, but it will
		// somewhat tricky and expensive to compute and it is probably not worth
		// it.
		s.inexact = true
		return false
	case len(apairs) < len(bpc.Pairs) && xClosed && !final:
		// A pattern of y without an equal one in x may admit labels that a
		// closed x does not.
		s.inexact = true
		return false
	}

outerConstraint:
	for i := range apairs {
		for j := range bpc.Pairs {
			if adt.Equal(s.ctx, apairs[i].Pattern, bpc.Pairs[j].Pattern, adt.CheckStructural) {
				if !s.constraints(x, y, x.PatternConstraint(s.ctx, i), y.PatternConstraint(s.ctx, j)) {
					return false
				}
				continue outerConstraint
			}
		}
		// We have a pattern in a that does not exist in b. Theoretically a
		// could still subsume b if the values of the patterns in b combined
		// subsume this value.
		// TODO: consider whether it is worth computing this.
		s.inexact = true
		return false
	}
	return true
}

// listVertices reports whether the list x subsumes the list y. A list is
// its listed elements followed by a tail: the constraint it places on any
// further elements, which for a closed list admits none. x subsumes y when
// x admits each element that y lists, by the element at the same position
// or by its tail once y lists more, and when x's tail admits y's tail.
func (s *subsumer) listVertices(x, y *adt.Vertex, final bool) bool {
	// A list without an ellipsis is open under the OpenLists experiment,
	// but as data, such as in Final mode, it admits no further elements.
	yClosed := final || y.IsClosedList()

	xElems := slices.Collect(x.Elems())
	yElems := slices.Collect(y.Elems())

	// Every element x lists must be matched by one that y lists, and a
	// closed x admits nothing beyond them.
	if len(xElems) > len(yElems) {
		return false
	}
	extra := yElems[len(xElems):]
	if x.IsClosedList() && (!yClosed || len(extra) > 0) {
		return false
	}
	for i, a := range xElems {
		if !s.vertices(a, yElems[i]) {
			return false
		}
	}
	if yClosed && len(extra) == 0 {
		return true
	}

	// x's tail must admit the further elements y lists, and then y's tail.
	tail := s.ctx.ListTail(x)
	for _, b := range extra {
		if !s.values(tail, b) {
			return false
		}
	}
	return yClosed || s.constraints(x, y, tail, s.ctx.ListTail(y))
}

// constraints reports whether a, a constraint of x, subsumes b, the matching
// constraint of y, such as two pattern constraints or list tails.
//
// The constraints of a recursive definition lead back to x and y, so they
// are taken to hold when reached again while being compared. The pair is
// that of x and y, as a list tail is a new vertex each time it is computed.
func (s *subsumer) constraints(x, y *adt.Vertex, a, b adt.Value) bool {
	pair := [2]*adt.Vertex{x.DerefValue(), y.DerefValue()}
	if slices.Contains(s.active, pair) {
		return true
	}
	s.active = append(s.active, pair)
	defer func() { s.active = s.active[:len(s.active)-1] }()
	return s.values(a, b)
}

// defaultOf returns the default of v, leaving an open list open, as closing
// it would make [...string] subsume [...int].
//
// A default may be a new vertex, so each one is kept: a vertex reached
// again, as a recursive definition is, must give the same default for
// [subsumer.constraints] to recognize it.
func (s *subsumer) defaultOf(v *adt.Vertex) *adt.Vertex {
	v = v.DerefValue()
	if d, ok := s.defaults[v]; ok {
		return d
	}
	d := v.DefaultDisjunct()
	if d != v {
		if s.defaults == nil {
			s.defaults = make(map[*adt.Vertex]*adt.Vertex)
		}
		s.defaults[v] = d
	}
	return d
}
