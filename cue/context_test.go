// Copyright 2021 CUE Authors
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

package cue_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/tdtest"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestNewList(t *testing.T) {
	ctx := cuecontext.New()

	intList := ctx.CompileString("[...int]")

	l123 := ctx.NewList(
		ctx.Encode(1),
		ctx.Encode(2),
		ctx.Encode(3),
	)

	testCases := []struct {
		desc string
		v    cue.Value
		out  string
	}{{
		v:   ctx.NewList(),
		out: `[]`,
	}, {
		v:   l123,
		out: `[1, 2, 3]`,
	}, {
		v:   l123.Unify(intList),
		out: `[1, 2, 3]`,
	}, {
		v:   l123.Unify(intList).Unify(l123),
		out: `[1, 2, 3]`,
	}, {
		v:   intList.Unify(ctx.NewList(ctx.Encode("string"))),
		out: `_|_ // 0: conflicting values "string" and int (mismatched types string and int)`,
	}, {
		v:   ctx.NewList().Unify(l123),
		out: `_|_ // incompatible list lengths (0 and 3)`,
	}, {
		v: ctx.NewList(
			intList,
			intList,
		).Unify(ctx.NewList(
			ctx.NewList(
				ctx.Encode(1),
				ctx.Encode(3),
			),
			ctx.NewList(
				ctx.Encode(5),
				ctx.Encode(7),
			),
		)),
		out: `[[1, 3], [5, 7]]`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := fmt.Sprint(tc.v)
			if got != tc.out {
				t.Errorf(" got: %v\nwant: %v", got, tc.out)
			}
		})
	}
}

func TestBuildInstancesSuccess(t *testing.T) {
	in := `
-- foo.cue --
package foo

foo: [{a: "b", c: "d"}, {a: "e", g: "f"}]
bar: [
	for f in foo
	if (f & {c: "b"}) != _|_
	{f}
]
`

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	_, err := cuecontext.New().BuildInstances([]*build.Instance{instance})
	if err != nil {
		t.Fatalf("BuildInstances() = %v", err)
	}
}

func TestBuildInstancesError(t *testing.T) {
	in := `
-- foo.cue --
package foo

foo: [{a: "b", c: "d"}, {a: "e", g: "f"}]
bar: [
	for f in foo
	if f & {c: "b") != _|_   // NOTE: ')' instead of '}'
	{f}
]
`

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]

	// Normally, this should be checked, however, this is explicitly
	// testing the path where this was NOT checked.
	// if instance.Err != nil {
	// 	t.Fatal(instance.Err)
	// }

	vs, err := cuecontext.New().BuildInstances([]*build.Instance{instance})
	if err == nil {
		t.Fatalf("BuildInstances() = %#v, wanted error", vs)
	}
}

func TestEncodeType(t *testing.T) {
	type testCase struct {
		name    string
		x       interface{}
		wantErr string
		out     string
	}
	testCases := []testCase{{
		name: "Struct",
		x: struct {
			A int    `json:"a"`
			B string `json:"b,omitempty"`
			C []bool
		}{},
		out: `{a: int64, b?: string, C?: *null|[...bool]}`,
	}, {
		name: "CUEValue#1",
		x: struct {
			A cue.Value `json:"a"`
		}{},
		out: `{a: _}`,
	}, {
		name: "CUEValue#2",
		x:    cue.Value{},
		out:  `_`,
	}, {
		// TODO this looks like a shortcoming of EncodeType.
		name: "map",
		x:    map[string]int{},
		out:  `*null|{}`,
	}, {
		name: "slice",
		x:    []int{},
		out:  `*null|[...int64]`,
	}, {
		name:    "chan",
		x:       chan int(nil),
		wantErr: `unsupported Go type \(chan int\)`,
	}}
	tdtest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
		v := cuecontext.New().EncodeType(tc.x)
		if tc.wantErr != "" {
			qt.Assert(t, qt.ErrorMatches(v.Err(), tc.wantErr))
			return
		}
		qt.Assert(t, qt.IsNil(v.Err()))
		got := fmt.Sprint(astinternal.DebugStr(v.Eval().Syntax()))
		t.Equal(got, tc.out)
	})
}

func TestContextCheck(t *testing.T) {
	qt.Assert(t, qt.PanicMatches(func() {
		var c cue.Context
		c.CompileString("1")
	}, `.*use cuecontext\.New.*`))
}
