// Copyright 2019 CUE Authors
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

package cue

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
)

// TODO: this code could be generated, but currently isn't.

type valueSorter struct {
	a   []Value
	cmp Value
	err error
}

func (s *valueSorter) ret() ([]Value, error) {
	if s.err != nil {
		return nil, s.err
	}
	// The input slice is already a copy and that we can modify it safely.
	return s.a, nil
}

func (s *valueSorter) Len() int      { return len(s.a) }
func (s *valueSorter) Swap(i, j int) { s.a[i], s.a[j] = s.a[j], s.a[i] }
func (s *valueSorter) Less(i, j int) bool {
	ctx := s.cmp.ctx()
	x := fill(ctx, s.cmp.v, s.a[i], "x")
	x = fill(ctx, x, s.a[j], "y")
	ctx.opCtx.Unify(ctx.opCtx, x, adt.Finalized) // TODO: remove.
	v := Value{s.cmp.idx, x}
	isLess, err := v.Lookup("less").Bool()
	if err != nil && s.err == nil {
		s.err = err
		return true
	}
	return isLess
}

// fill creates a new value with the old value unified with the given value.
// TODO: consider making this a method on Value.
func fill(ctx *context, v *adt.Vertex, x interface{}, path ...string) *adt.Vertex {
	for i := len(path) - 1; i >= 0; i-- {
		x = map[string]interface{}{path[i]: x}
	}
	value := convertVal(ctx, v, false, x)

	w := adt.ToVertex(value)
	n := &adt.Vertex{Label: v.Label}
	n.AddConjunct(adt.MakeConjunct(nil, v))
	n.AddConjunct(adt.MakeConjunct(nil, w))

	// n.Add(v)
	// n.Add(w)
	return n
}

func convertVal(ctx *context, src source, nullIsTop bool, x interface{}) adt.Value {
	return convert.GoValueToValue(ctx.opCtx, x, nullIsTop)
}
