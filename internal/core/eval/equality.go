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

import "cuelang.org/go/internal/core/adt"

func Equal(ctx *adt.OpContext, v, w adt.Value) bool {
	if x, ok := v.(*adt.Vertex); ok {
		return equalVertex(ctx, x, w)
	}
	if y, ok := w.(*adt.Vertex); ok {
		return equalVertex(ctx, y, v)
	}
	return equalTerminal(ctx, v, w)
}

func equalVertex(ctx *adt.OpContext, x *adt.Vertex, v adt.Value) bool {
	y, ok := v.(*adt.Vertex)
	if !ok {
		return false
	}
	if x == y {
		return true
	}
	if len(x.Arcs) != len(y.Arcs) {
		return false
	}
	if len(x.Arcs) == 0 && len(y.Arcs) == 0 {
		return equalTerminal(ctx, x.Value, y.Value)
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

	return equalTerminal(ctx, x.Value, y.Value)
}

func equalTerminal(ctx *adt.OpContext, v, w adt.Value) bool {
	if v == w {
		return true
	}
	switch x := v.(type) {
	case *adt.Num, *adt.String, *adt.Bool, *adt.Bytes:
		if b, ok := adt.BinOp(ctx, adt.EqualOp, v, w).(*adt.Bool); ok {
			return b.B
		}
		return false

	// TODO: for the remainder we are dealing with non-concrete values, so we
	// could also just not bother.

	case *adt.BoundValue:
		if y, ok := w.(*adt.BoundValue); ok {
			return x.Op == y.Op && Equal(ctx, x.Value, y.Value)
		}

	case *adt.BasicType:
		if y, ok := w.(*adt.BasicType); ok {
			return x.K == y.K
		}

	case *adt.Conjunction:
		y, ok := w.(*adt.Conjunction)
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

	case *adt.Disjunction:
		// The best way to compute this is with subsumption, but even that won't
		// be too accurate. Assume structural equivalence for now.
		y, ok := w.(*adt.Disjunction)
		if !ok || len(x.Values) != len(y.Values) {
			return false
		}
		for i, xe := range x.Values {
			if !Equal(ctx, xe, y.Values[i]) {
				return false
			}
		}
		return true

	case *adt.ListMarker:
		_, ok := w.(*adt.ListMarker)
		return ok

	case *adt.StructMarker:
		_, ok := w.(*adt.StructMarker)
		return ok

	case *adt.BuiltinValidator:
	}

	return false
}
