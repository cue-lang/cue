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

func Equal(ctx *OpContext, v, w Value) bool {
	if x, ok := v.(*Vertex); ok {
		return equalVertex(ctx, x, w)
	}
	if y, ok := w.(*Vertex); ok {
		return equalVertex(ctx, y, v)
	}
	return equalTerminal(ctx, v, w)
}

func equalVertex(ctx *OpContext, x *Vertex, v Value) bool {
	y, ok := v.(*Vertex)
	if !ok {
		return false
	}
	if x == y {
		return true
	}
	xk := x.Kind()
	yk := y.Kind()

	if xk != yk {
		return false
	}

	if len(x.Arcs) != len(y.Arcs) {
		return false
	}

	// TODO: this really should be subsumption.
	if x.IsClosed(ctx) != y.IsClosed(ctx) {
		return false
	}
	if !equalOptional(ctx, x, y) {
		return false
	}

loop1:
	for _, a := range x.Arcs {
		for _, b := range y.Arcs {
			if a.Label == b.Label {
				if !Equal(ctx, a, b) {
					return false
				}
				continue loop1
			}
		}
		return false
	}

	// We do not need to do the following check, because of the pigeon-hole principle.
	// loop2:
	// 	for _, b := range y.Arcs {
	// 		for _, a := range x.Arcs {
	// 			if a.Label == b.Label {
	// 				continue loop2
	// 			}
	// 		}
	// 		return false
	// 	}

	v, ok1 := x.BaseValue.(Value)
	w, ok2 := y.BaseValue.(Value)
	if !ok1 && !ok2 {
		return true // both are struct or list.
	}

	return equalTerminal(ctx, v, w)
}

// equalOptional tests if x and y have the same set of close information.
// Right now this just checks if it has the same source structs that
// define optional fields.
// TODO: the following refinements are possible:
// - unify optional fields and equate the optional fields
// - do the same for pattern constraints, where the pattern constraints
//   are collated by pattern equality.
// - a further refinement would collate patterns by ranges.
//
// For all these refinements it would be necessary to have well-working
// structure sharing so as to not repeatedly recompute optional arcs.
func equalOptional(ctx *OpContext, x, y *Vertex) bool {
	return verifyStructs(x, y) && verifyStructs(y, x)
}

func verifyStructs(x, y *Vertex) bool {
outer:
	for _, s := range x.Structs {
		if !s.StructLit.HasOptional() {
			continue
		}
		for _, t := range y.Structs {
			if s.StructLit == t.StructLit {
				continue outer
			}
		}
		return false
	}
	return true
}

func equalTerminal(ctx *OpContext, v, w Value) bool {
	if v == w {
		return true
	}

	switch x := v.(type) {
	case *Num, *String, *Bool, *Bytes:
		if b, ok := BinOp(ctx, EqualOp, v, w).(*Bool); ok {
			return b.B
		}
		return false

	// TODO: for the remainder we are dealing with non-concrete values, so we
	// could also just not bother.

	case *BoundValue:
		if y, ok := w.(*BoundValue); ok {
			return x.Op == y.Op && Equal(ctx, x.Value, y.Value)
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
			if !Equal(ctx, xe, y.Values[i]) {
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
			if !Equal(ctx, xe, y.Values[i]) {
				return false
			}
		}
		return true

	case *BuiltinValidator:
	}

	return false
}
