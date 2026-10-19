// Copyright 2026 The CUE Authors
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
	"slices"
	"testing"

	"cuelang.org/go/cue/token"
)

func TestMergeBuiltinsRejectsConflictingContractLabels(t *testing.T) {
	raw := &Builtin{Params: []Param{{Value: &Top{}}, {Value: &Top{}}}}
	tightened := func(labels ...Feature) *Builtin {
		params := make([]FuncParam, len(labels))
		for i, label := range labels {
			params[i] = FuncParam{Label: label, Positional: true}
		}
		return &Builtin{
			orig:   raw,
			Params: raw.Params,
			Types: []FuncType{{
				Fn: &Function{Params: params, Open: true},
			}},
		}
	}
	one := MakeIntLabel(IntLabel, 1)
	two := MakeIntLabel(IntLabel, 2)

	t.Run("unnamed and named", func(t *testing.T) {
		got, bottom := mergeBuiltins(&OpContext{}, tightened(InvalidLabel), tightened(one))
		if bottom != nil || got == nil {
			t.Fatalf("merge rejected a label for an unnamed slot: %v", bottom)
		}
	})
	t.Run("same label", func(t *testing.T) {
		got, bottom := mergeBuiltins(&OpContext{}, tightened(one), tightened(one))
		if bottom != nil || got == nil {
			t.Fatalf("merge rejected equal contract labels: %v", bottom)
		}
	})
	t.Run("different labels", func(t *testing.T) {
		_, bottom := mergeBuiltins(&OpContext{}, tightened(one), tightened(two))
		if bottom == nil {
			t.Fatal("merge accepted different contract labels for one raw position")
		}
	})
	t.Run("one label at different positions", func(t *testing.T) {
		_, bottom := mergeBuiltins(&OpContext{},
			tightened(one, InvalidLabel),
			tightened(InvalidLabel, one),
		)
		if bottom == nil {
			t.Fatal("merge accepted one contract label at different raw positions")
		}
	})
}

// An error moved to the site of a call keeps positions of its own: a position
// added to the moved copy or to the original afterwards does not overwrite one
// of the other's, even while both are held in the original's bootstrap array.
func TestMoveToCallSiteCopiesPositions(t *testing.T) {
	f := token.NewFile("x.cue", -1, 10)
	pos := func(offset int) token.Pos { return f.Pos(offset, token.NoRelPos) }
	args, site := &Vertex{}, &Vertex{}
	e := &ValueError{baseError: baseError{v: &Vertex{Parent: args}}}
	e.auxpos = e.auxposBootstrap[:0]
	e.AddPos(pos(1))

	moved, ok := (&OpContext{}).moveToCallSite(e, site, args, nil)
	if !ok {
		t.Fatal("error in the arguments was not moved to the call site")
	}
	m := moved.(*ValueError)
	m.AddPos(pos(2))
	e.AddPos(pos(3))

	if got, want := e.InputPositions(), []token.Pos{pos(1), pos(3)}; !slices.Equal(got, want) {
		t.Errorf("original positions: got %v, want %v", got, want)
	}
	if got, want := m.InputPositions(), []token.Pos{pos(1), pos(2)}; !slices.Equal(got, want) {
		t.Errorf("moved positions: got %v, want %v", got, want)
	}
}
