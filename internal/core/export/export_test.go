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

package export_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/encoding/gocode/gocodec"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/rogpeppe/go-internal/txtar"
)

func TestDefinition(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "definition",
		Update: cuetest.UpdateGoldenFiles,
	}

	r := cue.NewRuntime()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, errs := compile.Files(nil, r, "", a[0].Files...)
		if errs != nil {
			t.Fatal(errs)
		}
		v.Finalize(eval.NewContext(r, v))

		// TODO: do we need to evaluate v? In principle not necessary.
		// v.Finalize(eval.NewContext(r, v))

		file, errs := export.Def(r, "", v)
		errors.Print(t, errs, nil)
		_, _ = t.Write(formatNode(t.T, file))
	})
}

func formatNode(t *testing.T, n ast.Node) []byte {
	t.Helper()

	b, err := format.Node(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestGenerated tests conversions of generated Go structs, which may be
// different from parsed or evaluated CUE, such as having Vertex values.
func TestGenerated(t *testing.T) {
	testCases := []struct {
		in  func(ctx *adt.OpContext) (adt.Expr, error)
		out string
	}{{
		in: func(ctx *adt.OpContext) (adt.Expr, error) {
			in := &C{
				Terminals: []*A{{Name: "Name", Description: "Desc"}},
			}
			return convert.GoValueToValue(ctx, in, false), nil
		},
		out: `{Terminals: [{Name: "Name", Description: "Desc"}]}`,
	}, {
		in: func(ctx *adt.OpContext) (adt.Expr, error) {
			in := &C{
				Terminals: []*A{{Name: "Name", Description: "Desc"}},
			}
			return convert.GoTypeToExpr(ctx, in)
		},
		out: `*null|{Terminals?: *null|[...*null|{Name: string, Description: string}]}`,
	}, {
		in: func(ctx *adt.OpContext) (adt.Expr, error) {
			in := []*A{{Name: "Name", Description: "Desc"}}
			return convert.GoValueToValue(ctx, in, false), nil
		},
		out: `[{Name: "Name", Description: "Desc"}]`,
	}, {
		in: func(ctx *adt.OpContext) (adt.Expr, error) {
			in := []*A{{Name: "Name", Description: "Desc"}}
			return convert.GoTypeToExpr(ctx, in)
		},
		out: `*null|[...*null|{Name: string, Description: string}]`,
	}, {
		in: func(ctx *adt.OpContext) (adt.Expr, error) {
			expr, err := parser.ParseExpr("test", `{
				x: Guide.#Terminal
				Guide: {}
			}`)
			if err != nil {
				return nil, err
			}
			c, err := compile.Expr(nil, ctx, "_", expr)
			if err != nil {
				return nil, err
			}
			root := &adt.Vertex{}
			root.AddConjunct(c)
			root.Finalize(ctx)

			// Simulate Value.Unify of Lookup("x") and Lookup("Guide").
			n := &adt.Vertex{}
			n.AddConjunct(adt.MakeRootConjunct(nil, root.Arcs[0]))
			n.AddConjunct(adt.MakeRootConjunct(nil, root.Arcs[1]))
			n.Finalize(ctx)

			return n, nil
		},
		out: `<[l2// x: undefined field #Terminal] _|_>`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := runtime.New()
			ctx := adt.NewContext(r, &adt.Vertex{})

			v, err := tc.in(ctx)
			if err != nil {
				t.Fatal("failed test case: ", err)
			}
			expr, err := export.Expr(ctx, "", v)
			if err != nil {
				t.Fatal("failed export: ", err)
			}
			got := astinternal.DebugStr(expr)
			if got != tc.out {
				t.Errorf("got:  %s\nwant: %s", got, tc.out)
			}
		})
	}
}

type A struct {
	Name        string
	Description string
}

type B struct {
	Image string
}

type C struct {
	Terminals []*A
}

// For debugging purposes. Do not delete.
func TestX(t *testing.T) {
	t.Skip()

	in := `
-- in.cue --
package test

// // Foo
// a: [X=string]: [Y=string]: {
// 	name: X+Y
// }

// [Y=string]: [X=string]: name: {Y+X}
// {
// 	name:  X.other + Y
// 	other: string
// }

// c: [X=string]: X

// #pkg1: Object

// "Hello \(#pkg1)!"


// Object: "World"

// // A Foo fooses stuff.
// foos are instances of Foo.
// foos: [string]: {}

// // // My first little foo.
// foos: MyFoo: {}
	`

	archive := txtar.Parse([]byte(in))
	a := cuetxtar.Load(archive, "/tmp/test")
	if err := a[0].Err; err != nil {
		t.Fatal(err)
	}

	// x := a[0].Files[0]
	// astutil.Sanitize(x)

	r := runtime.New()
	v, errs := compile.Files(nil, r, "", a[0].Files...)
	if errs != nil {
		t.Fatal(errs)
	}
	v.Finalize(eval.NewContext(r, v))

	file, errs := export.Def(r, "main", v)
	if errs != nil {
		t.Fatal(errs)
	}

	t.Error(string(formatNode(t, file)))
}

func TestFromGo(t *testing.T) {
	type Struct struct {
		A string
		B string
	}

	m := make(map[string]Struct)
	m["hello"] = Struct{
		A: "a",
		B: "b",
	}
	var r cue.Runtime
	codec := gocodec.New(&r, nil)
	v, err := codec.Decode(m)
	if err != nil {
		panic(err)
	}

	syn, _ := format.Node(v.Syntax())
	if got := string(syn); got != `{
	hello: {
		A: "a"
		B: "b"
	}
}` {
		t.Errorf("incorrect ordering: %s\n", got)
	}
}
