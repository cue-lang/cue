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

type Flag uint16

const (
	// IgnoreOptional allows optional information to be ignored. This only
	// applies when CheckStructural is given.
	IgnoreOptional Flag = 1 << iota

	// CheckStructural indicates that closedness information and pattern
	// constraints, including the element constraints of lists, should be
	// considered for equality. Equal may return false even when values are
	// equal.
	CheckStructural

	// RegularOnly indicates that only regular fields should be considered,
	// thus excluding hidden and definition fields.
	RegularOnly

	// IgnoreClosedness disregards whether structs and lists were closed by
	// close or by a definition. A list that cannot grow for lack of an
	// ellipsis remains distinct from one that can. This only applies when
	// CheckStructural is given.
	IgnoreClosedness

	// IgnoreVacuous disregards constraints that no field or element can
	// ever satisfy: the element constraints of a list that cannot grow,
	// and an optional field whose value is bottom where the other value
	// does not allow the field at all. This only applies when
	// CheckStructural is given.
	IgnoreVacuous

	// sharedByIdentity is set by [equalPatterns] to require that vertices
	// reached via structure sharing are the same vertex.
	sharedByIdentity
)

func Equal(ctx *OpContext, v, w Value, flags Flag) bool {
	if flags&CheckStructural == 0 {
		v = Default(v)
		w = Default(w)
	}
	if x, ok := v.(*Vertex); ok {
		return equalVertex(ctx, x, w, flags)
	}
	if y, ok := w.(*Vertex); ok {
		return equalVertex(ctx, y, v, flags)
	}
	return equalTerminal(ctx, v, w, flags)
}

func equalVertex(ctx *OpContext, x *Vertex, v Value, flags Flag) bool {
	y, ok := v.(*Vertex)
	if !ok {
		return false
	}

	// Note that the arc type of an originating node may be different than
	// the one we are sharing. So do this check before dereferencing.
	// For instance:
	//
	//    a?: #B  // ArcOptional
	//    #B: {}  // ArcMember
	if x.ArcType != y.ArcType {
		return false
	}

	x0, y0 := x, y
	x = x.DerefValue()
	y = y.DerefValue()

	if x == y {
		return true
	}
	if flags&sharedByIdentity != 0 && (x != x0 || y != y0) {
		return false
	}

	xk := x.Kind()
	yk := y.Kind()

	if xk != yk {
		// Numbers of a different kind may still be equal: when comparing an
		// integer with a floating-point number, the integer is first converted
		// to floating-point. Structural equality, by contrast, distinguishes
		// int from float.
		if flags&CheckStructural != 0 || xk&^NumberKind != 0 || yk&^NumberKind != 0 {
			return false
		}
	}

	maxArcType := ArcRequired
	if flags&CheckStructural != 0 {
		// Do not ignore optional fields
		// TODO(required): consider making this unconditional
		maxArcType = ArcOptional
	}

	// TODO: this really should be subsumption.
	if flags&CheckStructural != 0 {
		switch {
		case flags&IgnoreClosedness == 0:
			if x.IsClosedStruct() != y.IsClosedStruct() {
				return false
			}
			if x.IsClosedList() != y.IsClosedList() {
				return false
			}
		case isFixedLengthList(x) != isFixedLengthList(y):
			return false
		}
	}

	skipRegular := flags&RegularOnly != 0
	ignoreVacuous := flags&CheckStructural != 0 && flags&IgnoreVacuous != 0

loop1:
	for _, a := range x.Arcs {
		if (skipRegular && !a.Label.IsRegular()) || a.ArcType > maxArcType {
			continue
		}
		for _, b := range y.Arcs {
			if a.Label == b.Label {
				if a.ArcType != b.ArcType || !Equal(ctx, a, b, flags) {
					return false
				}
				continue loop1
			}
		}
		if ignoreVacuous && isVacuousArc(ctx, a, y) {
			continue
		}
		return false
	}

loop2:
	for _, b := range y.Arcs {
		if (skipRegular && !b.Label.IsRegular()) || b.ArcType > maxArcType {
			continue
		}
		for _, a := range x.Arcs {
			if a.Label == b.Label {
				if a.ArcType > maxArcType {
					// No need to continue: arc with label not found.
					break
				}
				// Label found. Equality was already tested in loop 1.
				continue loop2
			}
		}
		// Arc with same label not found.
		if ignoreVacuous && isVacuousArc(ctx, b, x) {
			continue
		}
		return false
	}

	if flags&CheckStructural != 0 && !equalPatterns(ctx, x, y, flags) {
		return false
	}

	v, ok1 := x.BaseValue.(Value)
	w, ok2 := y.BaseValue.(Value)
	if !ok1 && !ok2 {
		return true // both are struct or list.
	}

	return equalTerminal(ctx, v, w, flags)
}

// equalPatterns reports whether x and y have equal pattern constraints,
// in the same order.
//
// The constraints are compared recursively. Those of recursive values lead
// back to the vertices being compared via structure sharing, so shared
// vertices within a constraint must be the same vertex rather than equal
// ones, which keeps the recursion finite.
func equalPatterns(ctx *OpContext, x, y *Vertex, flags Flag) bool {
	var px, py []PatternConstraint
	if x.PatternConstraints != nil {
		px = x.PatternConstraints.Pairs
	}
	if y.PatternConstraints != nil {
		py = y.PatternConstraints.Pairs
	}
	if flags&IgnoreVacuous != 0 && isFixedLengthList(x) && isFixedLengthList(y) {
		// Neither list can gain an element, so their element constraints
		// are vacuous.
		return true
	}
	if len(px) != len(py) {
		return false
	}
	flags |= sharedByIdentity
	for i, p := range px {
		q := py[i]
		if !Equal(ctx, p.Pattern, q.Pattern, flags) {
			return false
		}
		p.Constraint.Finalize(ctx)
		q.Constraint.Finalize(ctx)
		if !Equal(ctx, p.Constraint, q.Constraint, flags) {
			return false
		}
	}
	return true
}

// isVacuousArc reports whether optional arc a, which has no counterpart in
// other, forbids its field just as other does: its value is bottom, so the
// field can never be present, and other does not allow the field at all.
// An incomplete error does not count, as it may yet resolve, nor does a
// structural cycle, as unifying a field with another value may yet give a
// useful result.
func isVacuousArc(ctx *OpContext, a, other *Vertex) bool {
	if a.ArcType != ArcOptional {
		return false
	}
	b := a.Bottom()
	if b == nil || b.IsIncomplete() || b.Code == StructuralCycleError {
		return false
	}
	return !other.Accept(ctx, a.Label)
}

// isFixedLengthList reports whether v is a list that cannot gain an element:
// a closed list, or one whose literals had no ellipsis.
func isFixedLengthList(v *Vertex) bool {
	m, ok := v.BaseValue.(*ListMarker)
	return ok && (!m.IsOpen || m.NoEllipsis)
}

func equalTerminal(ctx *OpContext, v, w Value, flags Flag) bool {
	if v == w {
		return true
	}

	switch x := v.(type) {
	case *Bottom:
		// All errors are logically the same.
		_, ok := w.(*Bottom)
		return ok

	case *Top:
		_, ok := w.(*Top)
		return ok

	case *Num, *String, *Bool, *Bytes, *Null:
		return BinOpBool(ctx, errOnDiffType, EqualOp, v, w)

	// TODO: for the remainder we are dealing with non-concrete values, so we
	// could also just not bother.

	case *BoundValue:
		if y, ok := w.(*BoundValue); ok {
			return x.Op == y.Op && Equal(ctx, x.Value, y.Value, flags)
		}

	case *BasicType:
		if y, ok := w.(*BasicType); ok {
			return x.K == y.K
		}

	case *Conjunction:
		y, ok := w.(*Conjunction)
		if !ok || len(x.Values) != len(y.Values) {
			return false
		}
		// always ordered the same
		for i, xe := range x.Values {
			if !Equal(ctx, xe, y.Values[i], flags) {
				return false
			}
		}
		return true

	case *Disjunction:
		// The best way to compute this is with subsumption, but even that won't
		// be too accurate. Assume structural equivalence for now.
		y, ok := w.(*Disjunction)
		if !ok || len(x.Values) != len(y.Values) {
			return false
		}
		for i, xe := range x.Values {
			if !Equal(ctx, xe, y.Values[i], flags) {
				return false
			}
		}
		return true

	case *BuiltinValidator:
		if y, ok := w.(*BuiltinValidator); ok {
			if x.Builtin != y.Builtin || len(x.Args) != len(y.Args) {
				return false
			}
			for i, xa := range x.Args {
				if !Equal(ctx, xa, y.Args[i], flags) {
					return false
				}
			}
			return true
		}

	case *FuncValue:
		if y, ok := w.(*FuncValue); ok {
			return equalFuncValues(ctx, x, y)
		}

	case *Builtin:
		// Identical builtins were already handled by the v == w comparison
		// above; this identifies tightened clones of the same builtin
		// carrying equal type constraints.
		if y, ok := w.(*Builtin); ok {
			return x.self() == y.self() && equalFuncTypes(x.Types, y.Types)
		}

	case *ExternalValidator:
		if y, ok := w.(*ExternalValidator); ok {
			return x == y
		}
	}

	return false
}
