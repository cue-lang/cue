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

	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/export"
)

// Notes:
//   - Can optional fields of y can always be ignored here? Maybe not in the
//     schema case.
//   - Definitions of y can be ignored in data mode.
//
// TODO(perf): use merge sort where possible.
func (s *subsumer) vertices(x, y *adt.Vertex) bool {
	if s.ctx.Version == internal.DevVersion {
		return s.verticesDev(x, y)
	}
	if x == y {
		return true
	}
	if a, b := x.ArcType, y.ArcType; a < b {
		return false
	} else if s.BackwardsCompatibility && a != b {
		// See comments in verticesDev for rationale.
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

	default:
		panic(fmt.Sprintf("unexpected type %T", v))
	}

	xClosed := s.isClosedStruct(x)
	// TODO: this should not close for taking defaults. Do a more principled
	// makeover of this package before making it public, though.
	yClosed := s.Final || s.Defaults || s.isClosedStruct(y)

	if s.BackwardsCompatibility && s.isClosedStruct(x) && !s.isClosedStruct(y) {
		// TODO: this could still be true if there is a catch-all constraint
		// resolving to top. But there are probably other implications that
		// demand this is false, especially if we allow for reflection.
		s.errf("later version closes struct")
		return false
	}

	if xClosed && !yClosed && !final {
		return false
	}

	types := x.OptionalTypes()
	if !final && !s.IgnoreOptional && types&(adt.HasPattern|adt.HasAdditional) != 0 {
		// TODO: there are many cases where pattern constraints can be checked.
		s.inexact = true
		return false
	}

	// All arcs in x must exist in y and its values must subsume.
	xFeatures := export.VertexFeatures(s.ctx, x)
	for _, f := range xFeatures {
		if s.Final && !f.IsRegular() {
			continue
		}

		a := x.Lookup(f)
		aOpt := false
		if a == nil {
			// x.f is optional
			if s.IgnoreOptional {
				continue
			}

			a = &adt.Vertex{Label: f}
			s.matchAndInsert(x, a)

			// If field a is optional and has value top, neither the
			// omission of the field nor the field defined with any value
			// may cause unification to fail.
			if a.Kind() == adt.TopKind {
				continue
			}

			aOpt = true
		} else if a.IsConstraint() {
			if s.IgnoreOptional {
				continue
			}
			// If field a is optional and has value top, neither the
			// omission of the field nor the field defined with any value
			// may cause unification to fail.
			if a.Kind() == adt.TopKind {
				continue
			}
			aOpt = true
		}

		b := y.Lookup(f)
		if b == nil {
			// y.f is optional
			if !aOpt {
				s.errf("required field is optional in subsumed value: %v", f)
				return false
			}

			// If f is undefined for y and if y is closed, the field is
			// implicitly defined as _|_ and thus subsumed. Technically, this is
			// even true if a is not optional, but in that case it means that y
			// is invalid, so return false regardless
			if !y.Accept(ctx, f) || y.IsData() || s.Final {
				continue
			}

			b = &adt.Vertex{Label: f}
			s.matchAndInsert(y, b)
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

	yFeatures := export.VertexFeatures(s.ctx, y)
outer:
	for _, f := range yFeatures {
		if s.Final && !f.IsRegular() {
			continue
		}

		for _, g := range xFeatures {
			if g == f {
				// already validated
				continue outer
			}
		}

		b := y.Lookup(f)
		if b == nil {
			if s.IgnoreOptional || s.Final {
				continue
			}

			b = &adt.Vertex{Label: f}
			s.matchAndInsert(y, b)
		} else if b.IsConstraint() {
			if s.IgnoreOptional || s.Final {
				continue
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
		if !s.matchAndInsert(x, a) {
			continue
		}

		b.Finalize(ctx)

		if !s.vertices(a, b) {
			return false
		}
	}

	return true
}

func (s *subsumer) matchAndInsert(from, to *adt.Vertex) bool {
	to.ArcType = adt.ArcOptional
	from.MatchAndInsert(s.ctx, to)
	to.Finalize(s.ctx)
	return len(to.Conjuncts) > 0
}

// verticesDev replaces vertices with the implementation of the new evaluator.
func (s *subsumer) verticesDev(x, y *adt.Vertex) bool {
	if x == y {
		return true
	}
	if a, b := x.ArcType, y.ArcType; a < b {
		return false
	} else if s.BackwardsCompatibility && a != b {
		// For backwards compatibility we disallow any change of the arc type.
		// There are essentially three scenarios allowed by normal subsumption:
		//
		//         v1        -> v2
		//      1. foo: int  -> foo!: int
		//      2. foo: int  -> foo?: int
		//      3. foo!: int -> foo?: int
		//
		// Making a previously provided field required (1) is clearly a breaking
		// change in the general case. So we cannot allow this. Also making a
		// previously provided field optional (2) seems tenuous, as it may
		// change the semantics of an exported value. So we disallow this as
		// well. The only remaining option, which on the surface seems safe, is
		// to change a required field to an optional field (3). Such an API
		// change is generally discouraged, though (see guidelines in Protobuf
		// land, for instance), so we disallow this as well for concistency.
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

	default:
		panic(fmt.Sprintf("unexpected type %T", v))
	}

	xClosed := s.isClosedStruct(x)
	// TODO: this should not close for taking defaults. Do a more principled
	// makeover of this package before making it public, though.
	yClosed := s.Final || s.Defaults || s.isClosedStruct(y)

	if s.BackwardsCompatibility && s.isClosedStruct(x) && !s.isClosedStruct(y) {
		// TODO: this could still be true if there is a catch-all constraint
		// resolving to top. But there are probably other implications that
		// demand this is false, especially if we allow for reflection.
		s.errf("later version closes struct")
		return false
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
			s.matchAndInsert(y, b)
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
		if !s.matchAndInsert(x, a) {
			continue
		}

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
		if y.IsClosedList() || y.IsClosedStruct() || final {
			// This is a special case where know that any allowed optional field
			// in a must be bottom in y, which is strictly more specific.
			return true
		}
		// If all patterns are constraint constraints, we are done if we can
		// verify that the fields exist in b.
		for _, p := range apc.Pairs {
			ok, hasUnbounded := s.checkConcretePatterns(p.Pattern, p.Constraint, y)
			if !ok {
				if hasUnbounded {
					s.inexact = true
				}
				return false
			}
		}
		return true
	}
	if apc == nil {
		if x.IsClosedList() || x.IsClosedStruct() || final {
			// TODO: we should verify that there exist fields that match pattern
			// constraints in this set that are not already fields. If this is
			// not the case, we should mark this as inexact.
			s.inexact = true
			s.errf("pattern constraints in subsumed value not allowed")
			return false
		}
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
		ok, hasUnbounded := s.checkConcretePatterns(p.Pattern, p.Constraint, y)
		if ok {
			continue
		} else if !hasUnbounded {
			return false
		}
		for _, q := range bpc.Pairs {
			if adt.Equal(s.ctx, p.Pattern, q.Pattern, 0) {
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

// checkConcretePatterns checks whether the given concrete value of a pattern
// constraint exists in the given Vertex. Note that if the pattern value exists
// as an explicit field, we will already have checked this and we can safely
// assume it subsumes.
func (s *subsumer) checkConcretePatterns(pat adt.Value, constraint, y *adt.Vertex) (ok, hasUnbounded bool) {
	pat = adt.Unwrap(pat)
	switch x := pat.(type) {
	case *adt.String, *adt.Num:
		f := adt.LabelFromValue(s.ctx, nil, pat)
		var b *adt.Vertex
		b = y.Lookup(f)
		if b == nil {
			b = &adt.Vertex{Label: f}
			if !s.matchAndInsert(y, b) {
				return false, false
			}
		}
		return s.vertices(constraint, b), false

	case *adt.Disjunction:
		for _, a := range x.Values {
			ok, hasUnbounded = s.checkConcretePatterns(a, constraint, y)
			if !ok {
				return
			}
		}
		return true, false

	default:
		return false, true
	}
}

func (s *subsumer) isClosedStruct(v *adt.Vertex) bool {
	if s.IgnoreClosedness {
		return false
	}
	if v.Closed {
		return true
	}
	return v.IsClosedStruct()
}

func (s *subsumer) listVertices(x, y *adt.Vertex) bool {
	if !y.IsData() && x.IsClosedList() && !y.IsClosedList() {
		return false
	}

	xElems := x.Elems()
	yElems := y.Elems()

	switch {
	case len(xElems) == len(yElems):
	case len(xElems) > len(yElems):
		return false
	case x.IsClosedList():
		return false
	default:
		a := &adt.Vertex{Label: adt.AnyIndex}
		s.matchAndInsert(x, a)

		// x must be open
		for _, b := range yElems[len(xElems):] {
			if !s.vertices(a, b) {
				return false
			}
		}

		if !y.IsClosedList() {
			b := &adt.Vertex{Label: adt.AnyIndex}
			s.matchAndInsert(y, b)
		}
	}

	for i, a := range xElems {
		if !s.vertices(a, yElems[i]) {
			return false
		}
	}

	return true
}
