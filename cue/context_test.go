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
	"runtime"
	"slices"
	"testing"
	"time"
	"weak"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	cueload "cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/core/adt"
	cueruntime "cuelang.org/go/internal/core/runtime"
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

func parseExpr(t testing.TB, src string) ast.Expr {
	t.Helper()
	x, err := parser.ParseExpr("test", src)
	qt.Assert(t, qt.IsNil(err))
	return x
}

func TestEncodeSyntax(t *testing.T) {
	type testCase struct {
		name string
		x    any
		out  string
		err  string
	}
	file := func(src string) *ast.File {
		f, err := parser.ParseFile("", src)
		qt.Assert(t, qt.IsNil(err))
		return f
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
	}, {
		// CUE syntax nested in a Go value is evaluated in full.
		name: "ExprInMap",
		x:    map[string]any{"a": parseExpr(t, `close({}) & {b: 2}`)},
		err:  `b: field not allowed`,
	}, {
		name: "ExprInSlice",
		x:    []any{parseExpr(t, `{x: 1} & {x: 2}`)},
		err:  `x: conflicting values 2 and 1`,
	}, {
		name: "FileInMap",
		x:    map[string]any{"f": file(`x: close({}) & {b: 2}`)},
		err:  `x.b: field not allowed`,
	}}
	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := ctx.Encode(tc.x)
			if tc.err != "" {
				qt.Assert(t, qt.ErrorMatches(v.Validate(), tc.err))
				return
			}
			qt.Assert(t, qt.IsNil(v.Validate()))
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

// TestBuildInstanceOptions checks that a build with options is not shared
// with any other build of the same instance nor with the packages importing
// it, and that the options do not reach the packages it imports.
func TestBuildInstanceOptions(t *testing.T) {
	fsys, err := txtar.FS(txtar.Parse([]byte(`
-- cue.mod/module.cue --
module: "mod.test"
language: version: "v0.16.0"
-- scoped/scoped.cue --
package scoped

x: y
-- usescoped/usescoped.cue --
package usescoped

import "mod.test/scoped"

out: scoped.x
-- usescoped2/usescoped2.cue --
package usescoped2

import "mod.test/scoped"

out: scoped.x
-- hid/hid.cue --
package hid

_h: 1
x:  _h
-- usehid/usehid.cue --
package usehid

import "mod.test/hid"

out: hid.x
-- infer/infer.cue --
package infer

x: strings.ToUpper("a")
-- inferhid/inferhid.cue --
package inferhid

import "mod.test/hid"

x: strings.ToUpper("a")
y: hid.x
-- scopedhid/scopedhid.cue --
package scopedhid

_h: y
x:  _h
`)))
	qt.Assert(t, qt.IsNil(err))

	ctx := cuecontext.New()
	scope := func(y int) cue.BuildOption {
		return cue.Scope(ctx.CompileString(fmt.Sprintf("y: %d", y)))
	}
	// noY is a scope which does not resolve y.
	noY := cue.Scope(ctx.CompileString("z: 1"))
	both := func(opts ...cue.BuildOption) cue.BuildOption {
		return func(o *cueruntime.Config) {
			for _, opt := range opts {
				opt(o)
			}
		}
	}
	const (
		scopedErr = `x: reference "y" not found`
		inferErr  = `x: reference "strings" not found`
		otherPath = "mod.test/other"
	)
	type step struct {
		pkg      string          // the package to build, or "" for the previous value's
		opt      cue.BuildOption // may be nil
		want     string          // the value built, or its error
		wantPath string          // the import path of the value's build instance, and its ID if different
	}
	// The builds of each test run in order, and are checked once they have
	// all run, so that each is also checked against the later ones.
	for _, test := range []struct {
		name   string
		builds []step
	}{{
		name: "ScopeNotAppliedToImports",
		builds: []step{
			{"usescoped", scope(1), scopedErr, ""},
		},
	}, {
		name: "ImportPathNotAppliedToImports",
		builds: []step{
			{"usehid", cue.ImportPath(otherPath), `{out: 1}`, otherPath},
			{"hid", nil, `{x: 1}`, "mod.test/hid (mod.test/hid@v0)"},
		},
	}, {
		name: "ScopeNotReused",
		builds: []step{
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
			{"scoped", scope(2), `{x: 2}`, "mod.test/scoped (mod.test/scoped@v0)"},
		},
	}, {
		name: "ScopeNotSharedWithPlainBuild",
		builds: []step{
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
			{"scoped", nil, scopedErr, ""},
		},
	}, {
		name: "ScopeNotSharedWithPlainBuildOfItsInstance",
		builds: []step{
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
			{"", nil, scopedErr, ""},
		},
	}, {
		name: "ScopeNotSharedWithImporter",
		builds: []step{
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
			{"usescoped", nil, scopedErr, ""},
		},
	}, {
		name: "InferBuiltinsNotSharedWithPlainBuild",
		builds: []step{
			{"infer", cue.InferBuiltins(true), `{x: "A"}`, "mod.test/infer@v0"},
			{"infer", nil, inferErr, ""},
		},
	}, {
		name: "ImportPathAfterPlainBuild",
		builds: []step{
			{"hid", nil, `{x: 1}`, "mod.test/hid (mod.test/hid@v0)"},
			{"hid", cue.ImportPath(otherPath), `{x: 1}`, otherPath},
		},
	}, {
		name: "ImportPathBeforePlainBuild",
		builds: []step{
			{"hid", cue.ImportPath(otherPath), `{x: 1}`, otherPath},
			{"hid", nil, `{x: 1}`, "mod.test/hid (mod.test/hid@v0)"},
		},
	}, {
		name: "ImportPathOfItsInstance",
		builds: []step{
			{"hid", cue.ImportPath("mod.test/hid"), `{x: 1}`, "mod.test/hid (mod.test/hid@v0)"},
		},
	}, {
		name: "ImportPathWithQualifier",
		builds: []step{
			{"hid", cue.ImportPath(otherPath + ":other"), `{x: 1}`, otherPath + ":other"},
		},
	}, {
		name: "ImportPathNotSharedWithImporter",
		builds: []step{
			{"usehid", nil, `{out: 1}`, "mod.test/usehid@v0"},
			{"hid", cue.ImportPath(otherPath), `{x: 1}`, otherPath},
			{"usehid", nil, `{out: 1}`, "mod.test/usehid@v0"},
		},
	}, {
		name: "ScopeAfterFailedScope",
		builds: []step{
			{"scoped", noY, scopedErr, ""},
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
			{"scoped", nil, scopedErr, ""},
		},
	}, {
		name: "InferBuiltinsWithImport",
		builds: []step{
			{"inferhid", cue.InferBuiltins(true), "{\n\tx: \"A\"\n\ty: 1\n}", "mod.test/inferhid@v0"},
			{"hid", nil, `{x: 1}`, "mod.test/hid (mod.test/hid@v0)"},
			{"inferhid", nil, inferErr, ""},
		},
	}, {
		name: "ScopeAndImportPath",
		builds: []step{
			{"scopedhid", both(scope(1), cue.ImportPath(otherPath)), `{x: 1}`, otherPath},
			{"scopedhid", scope(2), `{x: 2}`, "mod.test/scopedhid@v0"},
			{"scopedhid", nil, `_h: reference "y" not found`, ""},
		},
	}, {
		name: "ImportPathNotSharedWithPlainBuildOfItsInstance",
		builds: []step{
			{"hid", cue.ImportPath(otherPath), `{x: 1}`, otherPath},
			// Building the instance it reports keeps the import path.
			{"", nil, `{x: 1}`, otherPath},
			{"usehid", nil, `{out: 1}`, "mod.test/usehid@v0"},
		},
	}, {
		name: "ScopeAfterFailedPlainBuild",
		builds: []step{
			{"scoped", nil, scopedErr, ""},
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
			{"scoped", nil, scopedErr, ""},
		},
	}, {
		name: "ScopeAfterFailedImporter",
		builds: []step{
			{"usescoped", nil, scopedErr, ""},
			{"scoped", scope(1), `{x: 1}`, "mod.test/scoped (mod.test/scoped@v0)"},
		},
	}, {
		name: "InferBuiltinsAfterFailedPlainBuild",
		builds: []step{
			{"infer", nil, inferErr, ""},
			{"infer", cue.InferBuiltins(true), `{x: "A"}`, "mod.test/infer@v0"},
		},
	}, {
		name: "FailedImportOfTwoImporters",
		builds: []step{
			{"usescoped", nil, scopedErr, ""},
			{"usescoped2", nil, scopedErr, ""},
		},
	}} {
		t.Run(test.name, func(t *testing.T) {
			// Importers must share the instances of their imports.
			insts := make(map[string]*build.Instance)
			for _, inst := range cueload.Instances([]string{"./..."}, &cueload.Config{FS: fsys}) {
				qt.Assert(t, qt.IsNil(inst.Err))
				insts[inst.PkgName] = inst
			}
			for _, inst := range insts {
				for _, imp := range inst.Imports {
					insts[imp.PkgName] = imp
				}
			}

			var values []cue.Value
			for _, b := range test.builds {
				var opts []cue.BuildOption
				if b.opt != nil {
					opts = append(opts, b.opt)
				}
				inst := insts[b.pkg]
				if b.pkg == "" {
					inst = values[len(values)-1].BuildInstance()
				}
				values = append(values, ctx.BuildInstance(inst, opts...))
			}
			for i, b := range test.builds {
				v := values[i]
				got, gotPath := fmt.Sprint(v), ""
				if err := v.Err(); err != nil {
					got = err.Error()
				}
				if inst := v.BuildInstance(); inst != nil {
					gotPath = inst.ImportPath
					id := inst.ID()
					if id != gotPath {
						gotPath += " (" + id + ")"
					}
					// The hidden fields are scoped by the identifier.
					iter, _ := v.Fields(cue.Hidden(true))
					for iter.Next() {
						if sel := iter.Selector(); sel.LabelType() == cue.HiddenLabel {
							qt.Check(t, qt.Equals(sel.PkgPath(), id), qt.Commentf("build %d: %v", i, sel))
						}
					}
				}
				qt.Check(t, qt.Equals(got, b.want), qt.Commentf("build %d", i))
				qt.Check(t, qt.Equals(gotPath, b.wantPath), qt.Commentf("build %d", i))
			}
		})
	}
}

// TestBuildInstanceErr checks that a failed build is reported by its value
// alone: the build instances keep only their load errors, so that a later
// build of them, even in another Context, does not fail because of it.
func TestBuildInstanceErr(t *testing.T) {
	fsys, err := txtar.FS(txtar.Parse([]byte(`
-- cue.mod/module.cue --
module: "mod.test"
language: version: "v0.16.0"
-- scoped/scoped.cue --
package scoped

x: y
-- usescoped/usescoped.cue --
package usescoped

import "mod.test/scoped"

out: scoped.x
`)))
	qt.Assert(t, qt.IsNil(err))
	usescoped := cueload.Instances([]string{"./usescoped"}, &cueload.Config{FS: fsys})[0]
	qt.Assert(t, qt.IsNil(usescoped.Err))
	scoped := usescoped.Imports[0]

	errString := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	v := cuecontext.New().BuildInstance(usescoped)
	qt.Assert(t, qt.Equals(errString(v.Err()), `x: reference "y" not found`))
	qt.Check(t, qt.Equals(errString(usescoped.Err), ""))
	qt.Check(t, qt.Equals(errString(scoped.Err), ""))

	ctx := cuecontext.New()
	v = ctx.BuildInstance(scoped, cue.Scope(ctx.CompileString("y: 1")))
	qt.Check(t, qt.Equals(errString(v.Err()), ""))
}

// TestCompileRetention checks what keeps alive the build instances of values
// compiled from source or built with options: a live value, its Context, or a
// root which outlives its Context via the values of another.
func TestCompileRetention(t *testing.T) {
	ctx := cuecontext.New()
	parse := func(src string) *ast.File {
		f, err := parser.ParseFile("x.cue", src)
		qt.Assert(t, qt.IsNil(err))
		return f
	}
	newInst := func(src string) *build.Instance {
		inst := build.NewContext().NewInstance("", nil)
		qt.Assert(t, qt.IsNil(inst.AddSyntax(parse(src))))
		return inst
	}
	compile := []func(ctx *cue.Context, src string) cue.Value{
		func(ctx *cue.Context, src string) cue.Value { return ctx.CompileString(src) },
		func(ctx *cue.Context, src string) cue.Value { return ctx.CompileBytes([]byte(src)) },
		func(ctx *cue.Context, src string) cue.Value { return ctx.BuildFile(parse(src)) },
		// A build with options builds a copy of its build instance.
		func(ctx *cue.Context, src string) cue.Value {
			return ctx.BuildInstance(newInst(src), cue.InferBuiltins(true))
		},
		func(ctx *cue.Context, src string) cue.Value {
			return ctx.BuildInstance(newInst(src), cue.Scope(ctx.CompileString("s: 1")))
		},
		func(ctx *cue.Context, src string) cue.Value {
			return ctx.BuildInstance(newInst(src), cue.ImportPath("example.com/p"))
		},
	}
	var insts []weak.Pointer[build.Instance]
	var v cue.Value
	for i := range 2 * len(compile) {
		v = compile[i%len(compile)](ctx, fmt.Sprintf("a: %d", i))
		// Formatting looks up the value's instance, which may cache it.
		qt.Assert(t, qt.Equals(fmt.Sprint(v), fmt.Sprintf("{a: %d}", i)))
		inst := v.BuildInstance()
		qt.Assert(t, qt.IsNotNil(inst))
		insts = append(insts, weak.Make(inst))
		// As a loader's functions might, lead the instance back to the value.
		self := v
		inst.CanonicalImportPath = func(path string) string {
			runtime.KeepAlive(self)
			return path
		}
	}
	runtime.GC()
	last := len(insts) - 1
	for i, inst := range insts[:last] {
		qt.Assert(t, qt.IsNil(inst.Value()), qt.Commentf("value %d", i))
	}
	qt.Assert(t, qt.Equals(insts[last].Value(), v.BuildInstance()))

	var foreign []cue.Value
	var foreignInsts []weak.Pointer[build.Instance]
	for i, f := range compile {
		built := f(cuecontext.New(), fmt.Sprintf("a: %d", i))
		foreignInsts = append(foreignInsts, weak.Make(built.BuildInstance()))
		foreign = append(foreign, ctx.CompileString("_").Unify(built))
	}
	// Freeing what a Context held takes collections and cleanups, which can
	// lag on a busy machine.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		runtime.GC()
		if !slices.ContainsFunc(foreignInsts, func(p weak.Pointer[build.Instance]) bool {
			return p.Value() != nil
		}) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i, inst := range foreignInsts {
		// TODO: a root which outlives its Context should not keep its build
		// instance alive, as nothing can reach the instance through it.
		qt.Check(t, qt.IsNotNil(inst.Value()), qt.Commentf("foreign value %d", i))
		qt.Check(t, qt.Equals(fmt.Sprint(foreign[i]), fmt.Sprintf("{a: %d}", i)))
	}
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

// TestContextStats checks that each [cue.Context] method which evaluates
// a value adds that evaluation to the global stats.
//
// The stats are global, so this test and its subtests cannot run in parallel.
func TestContextStats(t *testing.T) {
	const src = `{a: 1, b: a + 1, c: [a, b]}`
	type goType struct {
		A int    `json:"a" cue:">0"`
		B string `json:"b"`
	}
	tests := []struct {
		name         string
		value        func(t *testing.T, ctx *cue.Context) cue.Value
		unifications int64
	}{{
		name: "CompileString",
		value: func(t *testing.T, ctx *cue.Context) cue.Value {
			return ctx.CompileString(src)
		},
		unifications: 6,
	}, {
		name: "BuildExpr",
		value: func(t *testing.T, ctx *cue.Context) cue.Value {
			return ctx.BuildExpr(parseExpr(t, src))
		},
		unifications: 6,
	}, {
		name: "EncodeExpr",
		value: func(t *testing.T, ctx *cue.Context) cue.Value {
			return ctx.Encode(parseExpr(t, src))
		},
		unifications: 6,
	}, {
		name: "EncodeType",
		value: func(t *testing.T, ctx *cue.Context) cue.Value {
			return ctx.EncodeType(goType{})
		},
		unifications: 6,
	}, {
		// A type which was converted before is not evaluated again.
		name: "EncodeTypeTwice",
		value: func(t *testing.T, ctx *cue.Context) cue.Value {
			ctx.EncodeType(goType{})
			return ctx.EncodeType(goType{})
		},
		unifications: 6,
	}, {
		// The two elements count as one unification each.
		name: "NewList",
		value: func(t *testing.T, ctx *cue.Context) cue.Value {
			return ctx.NewList(ctx.CompileString(`1`), ctx.CompileString(`"x"`))
		},
		unifications: 5,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()
			adt.ResetStats()
			v := tc.value(t, ctx)
			qt.Assert(t, qt.IsNil(v.Err()))
			qt.Assert(t, qt.Equals(adt.TotalStats().Unifications, tc.unifications))
		})
	}
}
