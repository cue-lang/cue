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
	return s.verticesDev(x, y)
}

// verticesDev replaces vertices with the implementation of the new evaluator.
func (s *subsumer) verticesDev(x, y *adt.Vertex) bool {
	if x == y {
		return true
	}
	if a, b := x.ArcType, y.ArcType; a < b {
		return false
	}

	if s.Defaults {
		y = y.Default()
	}

	if b := y.Bottom(); b != nil {
		// If the value is incomplete, the error is not final. So either check
		// structural equivalence or return an error.
		return !b.IsIncomplete()
	}

	ctx := s.ctx

	final := y.IsData() || s.Final

	switch v := x.BaseValue.(type) {
	case *adt.Bottom:
		return false

	case *adt.ListMarker:
		if !y.IsList() {
			s.errf("list does not subsume %v (type %s)", y, y.Kind())
			return false
		}
		if !s.listVertices(x, y) {
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

	xClosed := x.IsClosedStruct() && !s.IgnoreClosedness
	// TODO: this should not close for taking defaults. Do a more principled
	// makeover of this package before making it public, though.
	yClosed := s.Final || s.Defaults ||
		(y.IsClosedStruct() && !s.IgnoreClosedness)

	if xClosed && !yClosed && !final {
		return false
	}

	// From here, verticesDev differs significantly from vertices.

	for _, a := range x.Arcs {
		f := a.Label
		if s.Final && !f.IsRegular() {
			continue
		}

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
			// TODO: what to do with required fields. Logically they should be
			// ignored if subsuming at the value level. OTOH, they represent an
			// (incomplete) error at the value level.
			// Mimic the old evaluator for now.
			if s.IgnoreOptional {
				continue
			}
			// If field a is optional and has value top, neither the
			// omission of the field nor the field defined with any value
			// may cause unification to fail.
			if a.Kind() == adt.TopKind {
				continue
			}

			isConstraint = true
		}

		b := y.Lookup(f)
		if b == nil {
			if !isConstraint {
				s.errf("regular field is constraint in subsumed value: %v", f)
				return false
			}

			// If f is undefined for y and if y is closed, the field is
			// implicitly defined as _|_ and thus subsumed. Technically, this is
			// even true if a is not optional, but in that case it means that y
			// is invalid, so return false regardless
			if !y.Accept(ctx, f) || y.IsData() || s.Final {
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

		if !x.Accept(ctx, f) {
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

		if !s.vertices(a, b) {
			return false
		}
	}

	// Now compare pattern constraints.
	apc := x.PatternConstraints
	bpc := y.PatternConstraints
	if bpc == nil {
		if apc == nil {
			return true
		}
		if y.IsClosedStruct() || y.IsClosedList() || final {
			// This is a special case where know that any allowed optional field
			// in a must be bottom in y, which is strictly more specific.
			return true
		}
		return false
	}
	if apc == nil {
		return true
	}
	if len(apc.Pairs) > len(bpc.Pairs) {
		// Theoretically it is still possible for a to subsume b, but it will
		// somewhat tricky and expensive to compute and it is probably not worth
		// it.
		s.inexact = true
		return false
	}

outerConstraint:
	for _, p := range apc.Pairs {
		for _, q := range bpc.Pairs {
			if adt.Equal(s.ctx, p.Pattern, q.Pattern, 0) {
				p.Constraint.Finalize(s.ctx)
				q.Constraint.Finalize(s.ctx)
				if !s.values(p.Constraint, q.Constraint) {
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

func (s *subsumer) listVertices(x, y *adt.Vertex) bool {
	ctx := s.ctx

	if !y.IsData() && x.IsClosedList() && !y.IsClosedList() {
		return false
	}

	xElems := slices.Collect(x.Elems())
	yElems := slices.Collect(y.Elems())

	switch {
	case len(xElems) == len(yElems):
	case len(xElems) > len(yElems):
		return false
	case x.IsClosedList():
		return false
	default:
		a := &adt.Vertex{Label: adt.AnyIndex}
		x.MatchAndInsert(ctx, a)
		a.Finalize(ctx)

		// x must be open
		for _, b := range yElems[len(xElems):] {
			if !s.vertices(a, b) {
				return false
			}
		}

		if !y.IsClosedList() {
			b := &adt.Vertex{Label: adt.AnyIndex}
			y.MatchAndInsert(ctx, b)
			b.Finalize(ctx)
		}
	}

	for i, a := range xElems {
		if !s.vertices(a, yElems[i]) {
			return false
		}
	}

	return true
}
