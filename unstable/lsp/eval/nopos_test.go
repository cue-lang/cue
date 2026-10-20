// Copyright 2026 CUE Authors
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

package eval_test

import (
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/unstable/lsp/eval"
)

// TestResolveWithoutPositions pins that [eval.Decl.Resolve] works on
// a fully programmatic AST carrying no position information: the
// element is matched by node identity, and the search is scoped to
// the declaration's own frames rather than navigated by offset.
func TestResolveWithoutPositions(t *testing.T) {
	xIdent := ast.NewIdent("x")
	f := &ast.File{
		Filename: "nopos.cue",
		Decls: []ast.Decl{
			&ast.Field{Label: ast.NewIdent("x"), Value: ast.NewLit(token.INT, "3")},
			&ast.Field{Label: ast.NewIdent("y"), Value: xIdent},
		},
	}
	ev := eval.New(eval.Config{
		IP: ast.ParseImportPath("example.test/p").Canonical(),
	}, f)

	x := ev.Root().Field("x")
	y := ev.Root().Field("y")
	qt.Assert(t, qt.IsNotNil(x))
	qt.Assert(t, qt.IsNotNil(y))

	var yDecl *eval.Decl
	for d := range y.Decls() {
		if d.Kind() == eval.DeclField {
			yDecl = d
		}
	}
	qt.Assert(t, qt.IsNotNil(yDecl))

	ns := yDecl.Resolve(xIdent)
	qt.Assert(t, qt.HasLen(ns, 1))
	qt.Assert(t, qt.Equals(ns[0], x))

	// An element that is not part of this declaration's own syntax
	// resolves to nothing.
	qt.Assert(t, qt.HasLen(yDecl.Resolve(ast.NewIdent("x")), 0))
}

// TestFuncArgsWithoutPositions pins that a reference resolves within
// a function literal built with the deprecated [ast.Func.Args] field,
// which the parser never produces: the anonymous parameters it
// synthesizes evaluate their constraints like any others.
func TestFuncArgsWithoutPositions(t *testing.T) {
	kRef := ast.NewIdent("k")
	f := &ast.File{
		Filename: "nopos.cue",
		Decls: []ast.Decl{
			&ast.Field{Label: ast.NewIdent("k"), Value: ast.NewIdent("int")},
			&ast.Field{Label: ast.NewIdent("f"), Value: &ast.Func{
				Args: []ast.Expr{kRef},
				Ret:  ast.NewIdent("int"),
			}},
		},
	}
	ev := eval.New(eval.Config{
		IP: ast.ParseImportPath("example.test/p").Canonical(),
	}, f)

	fNode := ev.Root().Field("f")
	qt.Assert(t, qt.IsNotNil(fNode))
	var fDecl *eval.Decl
	for d := range fNode.Decls() {
		if d.Kind() == eval.DeclField {
			fDecl = d
		}
	}
	qt.Assert(t, qt.IsNotNil(fDecl))

	ns := fDecl.Resolve(kRef)
	qt.Assert(t, qt.HasLen(ns, 1))
	qt.Assert(t, qt.Equals(ns[0], ev.Root().Field("k")))
}
