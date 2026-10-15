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
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"testing"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
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

func mustRat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("invalid rational: " + s)
	}
	return r
}

// TestEncodeBigRat covers that an integral [big.Rat] encodes as an integer
// whose decimal carries no exponent. Getting that wrong makes [cue.Value.Int64]
// report a value scaled by a power of ten and makes [cue.Value.Int] panic;
// see https://cuelang.org/issue/2649.
func TestEncodeBigRat(t *testing.T) {
	ctx := cuecontext.New()
	// A numerator of more than 34 digits does not fit the decimal context, and
	// one of more than 19 does not fit an int64.
	big35 := "100000000000000000000000000000000001"
	testCases := []struct {
		rat  *big.Rat
		kind cue.Kind
		out  string
		i64  int64
		// i64Err is what Int64 reports when out does not fit an int64.
		i64Err string
	}{
		{big.NewRat(8, 1), cue.IntKind, "8", 8, ""},
		{big.NewRat(100, 1), cue.IntKind, "100", 100, ""},
		{big.NewRat(-4000, 100), cue.IntKind, "-40", -40, ""},
		{big.NewRat(0, 5), cue.IntKind, "0", 0, ""},
		{big.NewRat(39, 2), cue.FloatKind, "19.5", 0, ""},
		{big.NewRat(-39, 2), cue.FloatKind, "-19.5", 0, ""},
		{big.NewRat(1, 3), cue.FloatKind, "0.3333333333333333333333333333333333", 0, ""},
		{mustRat(big35 + "/1"), cue.IntKind, big35, math.MaxInt64, "value was rounded down"},
		{mustRat("-" + big35 + "/1"), cue.IntKind, "-" + big35, math.MinInt64, "value was rounded up"},
	}
	for _, tc := range testCases {
		t.Run(tc.rat.RatString(), func(t *testing.T) {
			v := ctx.Encode(tc.rat)
			qt.Assert(t, qt.Equals(v.Kind(), tc.kind))
			qt.Assert(t, qt.Equals(fmt.Sprint(v), tc.out))
			if tc.kind != cue.IntKind {
				return
			}
			i64, err := v.Int64()
			if tc.i64Err == "" {
				qt.Assert(t, qt.IsNil(err))
			} else {
				qt.Assert(t, qt.ErrorMatches(err, tc.i64Err))
			}
			qt.Assert(t, qt.Equals(i64, tc.i64))
			i, err := v.Int(nil)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(i.String(), tc.out))
		})
	}
}

func TestEncodeType(t *testing.T) {
	type testCase struct {
		name    string
		x       interface{}
		wantErr string
		out     string
	}
	type linkedList struct {
		X    int         `json:"x"`
		Next *linkedList `json:"next"`
	}
	type multiref struct {
		L1 *linkedList `json:"l1"`
		L2 *linkedList `json:"l2"`
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
		name: "StructOmitZero",
		x: struct {
			A int       `json:"a,omitzero"`
			B time.Time `json:"b,omitzero"`
		}{},
		out: `{a?: int64, b?: _}`,
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
		out:  `*null|{[string]: int&>=-9223372036854775808&<=9223372036854775807}`,
	}, {
		name: "slice",
		x:    []int{},
		out:  `*null|[...int&>=-9223372036854775808&<=9223372036854775807]`,
	}, {
		name:    "chan",
		x:       chan int(nil),
		wantErr: `unsupported Go type \(chan int\)`,
	}, {
		name: "recursiveType",
		x:    new(linkedList),
		out:  `{*null|_linkedList_0, _linkedList_0: {x: int64, next: *null|_linkedList_0}}`,
	}, {
		name: "multiref",
		x:    new(multiref),
		out:  `{*null|_multiref_0, _multiref_0: {l1: *null|_linkedList_0, l2: *null|_linkedList_0}, _linkedList_0: {x: int64, next: *null|_linkedList_0}}`,
	}}
	tdtest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
		v := cuecontext.New().EncodeType(tc.x)
		if tc.wantErr != "" {
			qt.Assert(t, qt.ErrorMatches(v.Err(), tc.wantErr))
			return
		}
		qt.Assert(t, qt.IsNil(v.Err()))
		got := fmt.Sprint(astinternal.DebugStr(v.Syntax()))
		t.Equal(got, tc.out)
	})
}

func TestEncodeSyntax(t *testing.T) {
	type testCase struct {
		name string
		x    any
		out  string
	}
	testCases := []testCase{{
		// A string with newlines formats as a multiline literal.
		name: "StringStruct",
		x: struct {
			T string `json:"t"`
		}{T: "foo\nbar\nbaz\n"},
		out: `{
	t: """
		foo
		bar
		baz

		"""
}`,
	}, {
		// json.RawMessage goes through json.Marshaler; see #3578.
		name: "StringJSONRawMessage",
		x:    json.RawMessage(`{"t": "foo\nbar\nbaz\n"}`),
		out: `{
	t: """
		foo
		bar
		baz

		"""
}`,
	}, {
		// The omitzero json option omits zero values, consulting the
		// IsZero method when implemented; see #4429.
		name: "OmitZero",
		x: struct {
			A int       `json:"a,omitzero"`
			B time.Time `json:"b,omitzero"`
			C time.Time `json:"c,omitzero"`
			D string    `json:"d"`
		}{C: time.Date(2019, 4, 1, 0, 0, 0, 0, time.UTC)},
		out: `{
	c: "2019-04-01T00:00:00Z"
	d: ""
}`,
	}}
	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := ctx.Encode(tc.x)
			qt.Assert(t, qt.IsNil(v.Err()))
			b, err := format.Node(v.Syntax())
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(b), tc.out))
		})
	}
}

func TestContextCheck(t *testing.T) {
	qt.Assert(t, qt.PanicMatches(func() {
		var c cue.Context
		c.CompileString("1")
	}, `.*use cuecontext\.New.*`))
}

// TestHiddenFieldScope tests looking up hidden fields with [cue.Hid] across the
// ways in which a package scope can be established. The package scope must
// always be spelled as the identifier of the instance the hidden field was
// compiled as part of, as reported by [build.Instance.ID], which is not in
// general an import path.
func TestHiddenFieldScope(t *testing.T) {
	const src = `package b

_a: 42
`
	ctx := cuecontext.New()

	// loaded builds src as a package of the module mod.com@v0.
	loaded := func() cue.Value {
		a := txtar.Parse([]byte(`
-- cue.mod/module.cue --
module: "mod.com@v0"
language: version: "v0.14.0"
-- b.cue --
` + src))
		inst := cuetxtar.Load(a, t.TempDir())[0]
		qt.Assert(t, qt.IsNil(inst.Err))
		return ctx.BuildInstance(inst)
	}

	// bare builds src as an instance whose import path is a bare name, as
	// builtin packages such as "list" have.
	bare := func() cue.Value {
		inst := build.NewContext().NewInstance("b.cue", nil)
		inst.ImportPath = "b"
		//lint:ignore SA1019 AddFile is due to be renamed, not removed
		qt.Assert(t, qt.IsNil(inst.AddFile("b.cue", src)))
		return ctx.BuildInstance(inst)
	}

	testCases := []struct {
		desc string
		v    cue.Value
		pkg  string // the one package scope which matches
	}{{
		desc: "package clause",
		v:    ctx.CompileString(src),
		pkg:  ":b",
	}, {
		desc: "no package clause",
		v:    ctx.CompileString("_a: 42"),
		pkg:  "_",
	}, {
		desc: "ImportPath option",
		v:    ctx.CompileString(src, cue.ImportPath("b")),
		pkg:  "b",
	}, {
		desc: "bare import path",
		v:    bare(),
		pkg:  "b",
	}, {
		desc: "loaded from a module",
		v:    loaded(),
		pkg:  "mod.com@v0:b",
	}}
	// Each value must match its own package scope and no other's.
	var allPkgs []string
	for _, tc := range testCases {
		allPkgs = append(allPkgs, tc.pkg)
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			qt.Assert(t, qt.IsNil(tc.v.Err()))
			// The scope is always available from the value itself.
			qt.Assert(t, qt.Equals(tc.v.BuildInstance().ID(), tc.pkg))
			for _, pkg := range allPkgs {
				got := fmt.Sprint(tc.v.LookupPath(cue.MakePath(cue.Hid("_a", pkg))))
				want := "42"
				if pkg != tc.pkg {
					want = fmt.Sprintf("_|_ // field not found: _a in package %q; "+
						"hidden fields are scoped by package, and this value has _a in package %q",
						pkg, tc.pkg)
				}
				qt.Assert(t, qt.Equals(got, want), qt.Commentf("package scope %q", pkg))
			}
		})
	}
}
