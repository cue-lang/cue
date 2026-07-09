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

package cue_test

import (
	"context"
	"testing"

	cue "cuelang.org/go/cue/v2"
	"github.com/go-quicktest/qt"
)

func TestDecodeScalars(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
b: true
i: 42
u: 7
f: 1.25
s: "hello"
by: 'abc'
n: null
`)
	var b bool
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("b")).Decode(ctx, &b)))
	qt.Assert(t, qt.IsTrue(b))

	var i int
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("i")).Decode(ctx, &i)))
	qt.Assert(t, qt.Equals(i, 42))

	var i8 int8
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("i")).Decode(ctx, &i8)))
	qt.Assert(t, qt.Equals(i8, 42))

	var u uint
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("u")).Decode(ctx, &u)))
	qt.Assert(t, qt.Equals(u, 7))

	var f float64
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("f")).Decode(ctx, &f)))
	qt.Assert(t, qt.Equals(f, 1.25))

	var s string
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("s")).Decode(ctx, &s)))
	qt.Assert(t, qt.Equals(s, "hello"))

	var by []byte
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("by")).Decode(ctx, &by)))
	qt.Assert(t, qt.DeepEquals(by, []byte("abc")))

	// null decodes into a nil pointer.
	p := &i
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("n")).Decode(ctx, &p)))
	qt.Assert(t, qt.IsNil(p))

	// Overflow is reported.
	var small int8
	err := v.LookupPath(cue.ParsePath("i")).Decode(ctx, &small)
	qt.Assert(t, qt.IsNil(err))
	v2 := compileValue(t, "big: 1000")
	err = v2.LookupPath(cue.ParsePath("big")).Decode(ctx, &small)
	qt.Assert(t, qt.ErrorMatches(err, `.*integer 1000 overflows int8.*`))
}

func TestDecodeStruct(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
name: "cue"
Version: 2
tags: ["a", "b"]
meta: size: 10
extra: "ignored"
`)
	type Meta struct {
		Size int `json:"size"`
	}
	type Info struct {
		Name    string `json:"name"`
		Version int    // matched case-insensitively
		Tags    []string
		Meta    Meta `json:"meta"`
	}
	var got Info
	qt.Assert(t, qt.IsNil(v.Decode(ctx, &got)))
	qt.Assert(t, qt.DeepEquals(got, Info{
		Name:    "cue",
		Version: 2,
		Tags:    []string{"a", "b"},
		Meta:    Meta{Size: 10},
	}))
}

func TestDecodeMapAndInterface(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
m: {a: 1, b: 2}
mixed: {s: "x", n: 3, l: [true], sub: {y: 1.5}, z: null}
`)
	var m map[string]int
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("m")).Decode(ctx, &m)))
	qt.Assert(t, qt.DeepEquals(m, map[string]int{"a": 1, "b": 2}))

	var x any
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("mixed")).Decode(ctx, &x)))
	qt.Assert(t, qt.DeepEquals(x, any(map[string]any{
		"s":   "x",
		"n":   int64(3),
		"l":   []any{true},
		"sub": map[string]any{"y": 1.5},
		"z":   nil,
	})))
}

func TestDecodeIntKeyedMap(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `m: "1": "one", m: "2": "two"`)
	var m map[int]string
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("m")).Decode(ctx, &m)))
	qt.Assert(t, qt.DeepEquals(m, map[int]string{1: "one", 2: "two"}))
}

func TestDecodeCueValueField(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `name: "x", constraint: >10`)
	type S struct {
		Name       string    `json:"name"`
		Constraint cue.Value `json:"constraint"`
	}
	var got S
	qt.Assert(t, qt.IsNil(v.Decode(ctx, &got)))
	qt.Assert(t, qt.Equals(got.Name, "x"))
	// The non-concrete part is preserved as a Value.
	qt.Assert(t, qt.IsTrue(got.Constraint.Exists(ctx)))
	qt.Assert(t, qt.Equals(got.Constraint.IncompleteKind(ctx), cue.NumberKind))
}

func TestDecodeIncomplete(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "a: int")
	var i int
	err := v.LookupPath(cue.ParsePath("a")).Decode(ctx, &i)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*non-concrete value.*`))
}

func TestDecodeError(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "a: 1 & 2")
	var i int
	err := v.LookupPath(cue.ParsePath("a")).Decode(ctx, &i)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*conflicting values.*`))
}

func TestDecodeDefaults(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `a: int | *5, s: string | *"dflt"`)
	var got struct {
		A int
		S string
	}
	qt.Assert(t, qt.IsNil(v.Decode(ctx, &got)))
	qt.Assert(t, qt.Equals(got.A, 5))
	qt.Assert(t, qt.Equals(got.S, "dflt"))
}
