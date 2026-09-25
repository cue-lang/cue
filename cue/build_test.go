// Copyright 2018 The CUE Authors
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

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/value"
)

func TestFromExpr(t *testing.T) {
	testCases := []struct {
		expr ast.Expr
		out  string
		err  string
	}{{
		expr: ast.NewString("Hello"),
		out:  `"Hello"`,
	}, {
		expr: ast.NewList(
			ast.NewString("Hello"),
			ast.NewString("World"),
		),
		out: `["Hello", "World"]`,
	}, {
		// Issue #2628; this would cause a panic in the evaluator.
		expr: ast.NewBinExpr(
			token.AND,
			ast.NewIdent("bool"),
			ast.NewBool(true),
		),
		out: `true`,
	}, {
		expr: ast.NewPredeclared("int"),
		out:  `int`,
	}, {
		expr: ast.NewPredeclared("__matchN"),
		out:  `matchN`,
	}, {
		expr: ast.NewPredeclared("nosuchbuiltin"),
		err:  `predeclared name "nosuchbuiltin" not found`,
	}, {
		expr: ast.NewIdent("int"),
		out:  `int`,
	}, {
		expr: ast.NewIdent("__matchN"),
		out:  `matchN`,
	}, {
		expr: ast.NewIdent("__nosuchbuiltin"),
		err:  `reference "__nosuchbuiltin" not found`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := cuecontext.New()
			v := r.BuildExpr(tc.expr)
			err := v.Err()
			if !checkErr(t, err, tc.err, "init") {
			} else if got := fmt.Sprint(v); got != tc.out {
				t.Errorf("\n got: %v; want %v", got, tc.out)
			}
		})
	}
}

// TestBuildExprExperiments checks which language experiments apply to an
// expression compiled on its own via [cue.Context.BuildExpr]: those of the
// expression's language version, defaulting to the current version whether
// the expression was parsed or assembled programmatically.
//
// See https://cuelang.org/issue/4297.
func TestBuildExprExperiments(t *testing.T) {
	ctx := cuecontext.New()
	scope := ctx.CompileString("x: 1")

	build := func(expr ast.Expr) (string, error) {
		v := ctx.BuildExpr(expr, cue.Scope(scope))
		if err := v.Err(); err != nil {
			return "", err
		}
		return fmt.Sprint(v), nil
	}

	// A parsed expression defaults to the current language version, where
	// aliasv2 and its 'self' identifier are stable.
	expr, err := parser.ParseExpr("test", "self & {y: 2}")
	qt.Assert(t, qt.IsNil(err))
	got, err := build(expr)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, `{
	x: 1
	y: 2
}`))

	// An expression assembled programmatically has no position to resolve a
	// language version from and gets the default experiments of the current
	// version, like one parsed at the default version.
	got, err = build(ast.NewIdent("self"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, `{x: 1}`))

	// At a language version where aliasv2 is not yet stable, 'self' is
	// rejected.
	expr, err = parser.ParseExpr("test", "self & {y: 2}", parser.Version("v0.17.0"))
	qt.Assert(t, qt.IsNil(err))
	_, err = build(expr)
	qt.Assert(t, qt.ErrorMatches(err, `predeclared identifier "self" requires @experiment\(aliasv2\)`))
}

func TestBuildExprSelfWithoutScope(t *testing.T) {
	// Without a scope there is no enclosing struct for self to refer to,
	// which must be an error rather than a panic.
	ctx := cuecontext.New()
	expr, err := parser.ParseExpr("test", "self & {y: 2}")
	qt.Assert(t, qt.IsNil(err))
	err = ctx.BuildExpr(expr).Err()
	qt.Assert(t, qt.ErrorMatches(err, `self has no enclosing struct`))
}

func TestBuildExprClose(t *testing.T) {
	// Issue #2680: BuildExpr should preserve the close constraint,
	// just like CompileString and BuildFile do.
	ctx := cuecontext.New()
	expr := ast.NewCall(ast.NewIdent("close"), ast.NewStruct())
	v := ctx.BuildExpr(expr)
	w := ctx.CompileString("a: 1")
	if err := v.Unify(w).Err(); err == nil {
		t.Error("close({}) unified with a:1 should fail, but succeeded")
	}
}

func TestBuildExprCloseList(t *testing.T) {
	// Closing a list in an expression without a file used to panic while
	// finalizing a structurally shared node. The list must also be closed.
	ctx := cuecontext.New()
	expr, err := parser.ParseExpr("test", "close([1])")
	qt.Assert(t, qt.IsNil(err))
	v := ctx.BuildExpr(expr)
	qt.Assert(t, qt.IsNil(v.Err()))
	w := ctx.CompileString("[1, 2]")
	if err := v.Unify(w).Err(); err == nil {
		t.Error("close([1]) unified with [1, 2] should fail, but succeeded")
	}
}

func TestBuild(t *testing.T) {
	files := func(s ...string) []string { return s }
	insts := func(i ...*bimport) []*bimport { return i }
	pkg1 := &bimport{
		"pkg1",
		files(`
		package pkg1

		Object: "World"
		`),
	}
	pkg2 := &bimport{
		"mod.test/foo/pkg2:pkg",
		files(`
		package pkg

		Number: 12
		`),
	}
	pkg3 := &bimport{
		"mod.test/foo/v1:pkg3",
		files(`
		package pkg3

		List: [1,2,3]
		`),
	}

	testCases := []struct {
		instances []*bimport
		emit      string
	}{{
		insts(&bimport{"", files(`test: "ok"`)}),
		`{test:"ok"}`,
	}, {
		insts(&bimport{"",
			files(
				`package test

				import "math"

				"Pi: \(math.Pi)!"`)}),
		`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
	}, {
		insts(&bimport{"",
			files(
				`package test

				import math2 "math"

				"Pi: \(math2.Pi)!"`)}),
		`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import "pkg1"

				"Hello \(pkg1.Object)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import "pkg1"

				"Hello \(pkg1.Object)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import pkg2 "pkg1"
				#pkg1: pkg2.Object

				"Hello \(#pkg1)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, pkg2, &bimport{"",
			files(
				`package test

				import bar "pkg1"
				import baz "mod.test/foo/pkg2:pkg"

				pkg1: Object: 3
				"Hello \(pkg1.Object)!"`),
		}),
		`imported and not used: "pkg1" as bar (and 1 more errors)`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

				import "mod.test/foo/pkg2:pkg"

				"Hello \(pkg2.Number)!"`),
		}),
		`imported and not used: "mod.test/foo/pkg2:pkg" (and 1 more errors)`,
		// `file0.cue:5:14: unresolved reference pkg2`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

				import "mod.test/foo/pkg2:pkg"

				"Hello \(pkg.Number)!"`),
		}),
		`"Hello 12!"`,
	}, {
		insts(pkg3, &bimport{"",
			files(
				`package test

				import "mod.test/foo/v1:pkg3"

				"Hello \(pkg3.List[1])!"`),
		}),
		`"Hello 2!"`,
	}, {
		insts(pkg3, &bimport{"",
			files(
				`package test

				import "mod.test/foo/v1:pkg3"

				pkg3: 3

				"Hello \(pkg3.List[1])!"`),
		}),
		`pkg3 redeclared as imported package name
	previous declaration at file0.cue:5:5`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			insts := cue.Build(makeInstances(tc.instances))
			var got string
			if err := insts[0].Err; err != nil {
				got = err.Error()
			} else {
				cfg := &debug.Config{Compact: true}
				r, v := value.ToInternal(insts[0].Value())
				got = debug.NodeString(r, v, cfg)
			}
			if got != tc.emit {
				t.Errorf("\n got: %s\nwant: %s", got, tc.emit)
			}
		})
	}
}

type builder struct {
	ctxt    *build.Context
	imports map[string]*bimport
}

func (b *builder) load(pos token.Pos, path string) *build.Instance {
	bi := b.imports[path]
	if bi == nil {
		return nil
	}
	return b.build(bi)
}

type bimport struct {
	path  string // "" means top-level
	files []string
}

func makeInstances(insts []*bimport) (instances []*build.Instance) {
	b := builder{
		imports: map[string]*bimport{},
	}
	b.ctxt = build.NewContext(build.Loader(b.load))

	for _, bi := range insts {
		if bi.path != "" {
			b.imports[bi.path] = bi
		}
	}
	for _, bi := range insts {
		if bi.path == "" {
			instances = append(instances, b.build(bi))
		}
	}
	return
}

func (b *builder) build(bi *bimport) *build.Instance {
	path := bi.path
	if path == "" {
		path = "dir"
	}
	p := b.ctxt.NewInstance(path, nil)
	for i, f := range bi.files {
		_ = p.AddFile(fmt.Sprintf("file%d.cue", i), f)
	}
	_ = p.Complete()
	return p
}
