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

package ast_test

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/go/packages"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

// cloneSrc holds the constructs which the parser accepts without
// enabling any experiment.
const cloneSrc = `
// a package comment
package p

import "list"

// a is a number
a: b: 1 + 2 @attr(x)

c: [for x, y in a if y > 1 {"\(x)": y}]

d: [1, 2, ...int]
e: list.Concat([[a], [d[1:2]]])
f: {g: _, h?: !=3, ...}
i: *1 | 2
let j = a
k: (j)
m: {n: 1} & {o: 2}
p: d[0]
q: {a}
s: _|_
t: [for x in d {try y = x, {v: y}}]

@fileAttr()
`

// cloneExperimentSrc holds the constructs which are only available
// with an experiment enabled.
const cloneExperimentSrc = `
@experiment(aliasv2,try,functions)
package p

a~v: {b: 1}
c: [for x in [1] {v: x} otherwise {w: 1}]
d: {e?: int}
f: d.e?
g: {h: 1}...
h: func(_~x: int @attr(), y?: string) -> bool: x > 0
i: h(1, y: "x")
`

// cloneBadExprSrc and cloneBadDeclSrc do not parse; they exercise the
// error nodes which the parser leaves behind.
const (
	cloneBadExprSrc = "a: 1 +\n"
	cloneBadDeclSrc = "X=b\n"
)

// cloneOldAliasSrc holds the prefix alias form, which only parses below
// language version v0.18.0, where @experiment(aliasv2) became stable.
// It is also what leaves an *ast.BadDecl behind in cloneBadDeclSrc, so
// both are parsed at the older version.
const cloneOldAliasSrc = "r: X=d\n"

// oldAliasVersion is the last language version accepting cloneOldAliasSrc
// and cloneBadDeclSrc.
const oldAliasVersion = "v0.17.0"

func TestClone(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		version string
		file    func() *ast.File
	}{{
		name: "plain",
		src:  cloneSrc,
	}, {
		name: "experiments",
		src:  cloneExperimentSrc,
	}, {
		name: "badExpr",
		src:  cloneBadExprSrc,
	}, {
		name:    "badDecl",
		src:     cloneBadDeclSrc,
		version: oldAliasVersion,
	}, {
		name:    "oldAlias",
		src:     cloneOldAliasSrc,
		version: oldAliasVersion,
	}, {
		name: "func",
		file: funcFile,
	}}

	// seen accumulates the node types reached across all the test cases.
	seen := make(map[string]bool)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var f *ast.File
			if test.file != nil {
				f = test.file()
			} else {
				// Parse errors are expected for some cases; the parser
				// still returns a usable file.
				f, _ = parser.ParseFile("clone.cue", test.src,
					parser.ParseComments, parser.Version(test.version))
				qt.Assert(t, qt.IsNotNil(f))
				astutil.Resolve(f, func(token.Pos, string, ...any) {})
			}
			testClone(t, f, seen)
		})
	}

	// Every node type in the package must be reached by one of the
	// cases above, so that a node type added later cannot slip through
	// Clone untested.
	qt.Assert(t, qt.DeepEquals(slices.Sorted(maps.Keys(seen)), nodeTypeNames(t)))
}

// nodeTypeNames returns the name of every exported type declared in the
// ast package whose pointer type implements [ast.Node], sorted.
func nodeTypeNames(t *testing.T) []string {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes,
	}, ".")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(len(pkgs), 1))
	qt.Assert(t, qt.HasLen(pkgs[0].Errors, 0))
	pkg := pkgs[0].Types
	scope := pkg.Scope()

	node := scope.Lookup("Node").Type().Underlying().(*types.Interface)
	var names []string
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		typ, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		if _, isInterface := typ.Underlying().(*types.Interface); isInterface {
			continue
		}
		if types.Implements(types.NewPointer(typ), node) {
			names = append(names, "*ast."+name)
		}
	}
	slices.Sort(names)
	return names
}

// testClone checks that cloning f yields an independent but equivalent
// tree, and records the node types it encounters in seen.
func testClone(t *testing.T, f *ast.File, seen map[string]bool) {
	want := formatNode(t, f)

	f1 := ast.Clone(f)

	// The clone formats identically to the original, and cloning it
	// has not changed the original.
	qt.Assert(t, qt.Equals(formatNode(t, f1), want))
	qt.Assert(t, qt.Equals(formatNode(t, f), want))

	// The clone shares no nodes with the original.
	orig := nodeSet(f)
	for n := range nodeSet(f1) {
		seen[fmt.Sprintf("%T", n)] = true
		qt.Assert(t, qt.IsFalse(orig[n]), qt.Commentf("node %T is shared with the original", n))
	}

	// References inside the cloned tree point to the cloned nodes.
	ast.Walk(f1, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if id.Node != nil {
				qt.Check(t, qt.IsFalse(orig[id.Node]), qt.Commentf("identifier %v refers to original node %T", id.Name, id.Node))
			}
			if id.Scope != nil {
				qt.Check(t, qt.IsFalse(orig[id.Scope]), qt.Commentf("identifier %v is scoped to original node %T", id.Name, id.Scope))
			}
		}
		return true
	}, nil)
	for _, id := range f1.Unresolved {
		qt.Check(t, qt.IsFalse(orig[ast.Node(id)]), qt.Commentf("unresolved identifier %v is shared with the original", id.Name))
	}

	// Mutating the clone does not affect the original.
	f1.Decls = nil
	f1.Unresolved = nil
	ast.SetComments(f1, nil)
	qt.Assert(t, qt.Equals(formatNode(t, f), want))
}

// funcFile returns a file holding an [ast.Func], which the parser
// never produces.
func funcFile() *ast.File {
	return &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label: ast.NewIdent("f"),
				Value: &ast.Func{
					Args: []ast.Expr{ast.NewIdent("int"), ast.NewIdent("string")},
					Ret:  ast.NewIdent("bool"),
				},
			},
		},
	}
}

func TestCloneSharedComment(t *testing.T) {
	f, err := parser.ParseFile("clone.cue", "// doc\na: b: 1\n", parser.ParseComments)
	qt.Assert(t, qt.IsNil(err))

	// The doc comment is attached to the chain root and documents its leaf,
	// so it is reachable twice; it must be cloned only once.
	field := f.Decls[0].(*ast.Field)
	leaf := field.Value.(*ast.StructLit).Elts[0].(*ast.Field)
	qt.Assert(t, qt.Equals(len(ast.Comments(field)), 1))
	qt.Assert(t, qt.Equals(len(ast.DocComments(leaf)), 1))

	f1 := ast.Clone(f)
	field1 := f1.Decls[0].(*ast.Field)
	leaf1 := field1.Value.(*ast.StructLit).Elts[0].(*ast.Field)
	qt.Assert(t, qt.Equals(ast.DocComments(leaf1)[0], ast.Comments(field1)[0]))
	qt.Assert(t, qt.Not(qt.Equals(ast.Comments(field1)[0], ast.Comments(field)[0])))
}

func TestCloneSubtree(t *testing.T) {
	f, err := parser.ParseFile("clone.cue", "a: 1\nb: a\n", parser.ParseComments)
	qt.Assert(t, qt.IsNil(err))
	astutil.Resolve(f, func(_ token.Pos, msg string, args ...any) {
		t.Errorf(msg, args...)
	})

	// The value of b refers to a, which lies outside the cloned subtree,
	// so the clone keeps referring to the original nodes.
	value := f.Decls[1].(*ast.Field).Value.(*ast.Ident)
	qt.Assert(t, qt.IsNotNil(value.Node))
	qt.Assert(t, qt.IsNotNil(value.Scope))

	value1 := ast.Clone(value)
	qt.Assert(t, qt.Not(qt.Equals(value1, value)))
	qt.Assert(t, qt.Equals(value1.Node, value.Node))
	qt.Assert(t, qt.Equals(value1.Scope, value.Scope))
}

func TestCloneNil(t *testing.T) {
	qt.Assert(t, qt.IsNil(ast.Clone[*ast.File](nil)))
	qt.Assert(t, qt.IsNil(ast.Clone[ast.Expr](nil)))
	qt.Assert(t, qt.IsNil(ast.Clone[ast.Node](nil)))

	// A nil child of a non-nil node stays nil.
	x := ast.Clone(&ast.SliceExpr{X: ast.NewIdent("a")})
	qt.Assert(t, qt.IsNil(x.Low))
	qt.Assert(t, qt.IsNil(x.High))
}

// unknownNode is a node type that [Clone] does not know about.
type unknownNode struct {
	ast.Expr
}

func TestCloneUnknownNode(t *testing.T) {
	qt.Assert(t, qt.PanicMatches(func() {
		ast.Clone(&ast.ParenExpr{X: &unknownNode{}})
	}, `Clone: unexpected node type \*ast_test.unknownNode`))
}

func TestCloneExpr(t *testing.T) {
	x, err := parser.ParseExpr("clone.cue", `{a: 1, b: a + 1}`)
	qt.Assert(t, qt.IsNil(err))
	x1 := ast.Clone(x)
	qt.Assert(t, qt.Equals(formatNode(t, x1), formatNode(t, x)))
	qt.Assert(t, qt.Not(qt.Equals(x1, x)))

	// The parser never sets the default expression of a field, but a
	// constructed one is cloned like any other child.
	f := &ast.Field{
		Label:   ast.NewIdent("a"),
		Value:   ast.NewIdent("int"),
		Default: ast.NewIdent("d"),
	}
	f1 := ast.Clone(f)
	qt.Assert(t, qt.Not(qt.Equals(f1.Default, f.Default)))
}

func TestClonePredeclared(t *testing.T) {
	id := ast.NewPredeclared("self")
	id1 := ast.Clone(id)
	qt.Assert(t, qt.Not(qt.Equals(id1, id)))
	qt.Assert(t, qt.IsTrue(id1.IsPredeclared()))

	// The same holds for a predeclared identifier inside a larger tree,
	// whose sentinel node must not be remapped.
	f := &ast.File{Decls: []ast.Decl{
		&ast.Field{Label: ast.NewIdent("a"), Value: ast.NewPredeclared("self")},
	}}
	f1 := ast.Clone(f)
	qt.Assert(t, qt.IsTrue(f1.Decls[0].(*ast.Field).Value.(*ast.Ident).IsPredeclared()))
}

// nodeSet returns the set of all nodes in the tree rooted at n,
// including its comments.
func nodeSet(n ast.Node) map[ast.Node]bool {
	m := make(map[ast.Node]bool)
	add := func(n ast.Node) {
		m[n] = true
	}
	ast.Walk(n, func(n ast.Node) bool {
		add(n)
		for _, cg := range slices.Concat(ast.Comments(n), ast.DocComments(n)) {
			add(cg)
			for _, c := range cg.List {
				add(c)
			}
		}
		return true
	}, nil)
	return m
}

func formatNode(t *testing.T, n ast.Node) string {
	t.Helper()
	b, err := format.Node(n)
	qt.Assert(t, qt.IsNil(err))
	return string(b)
}
