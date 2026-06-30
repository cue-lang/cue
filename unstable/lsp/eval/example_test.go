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
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/unstable/lsp/eval"
)

// ExampleNode_Expand walks a three-branch disjunction: recovering
// the branches from the parse tree, matching each branch to the
// [eval.Decl] that models it, and resolving and expanding reference
// branches to reach the fields they include.
func ExampleNode_Expand() {
	const src = `
package p

x: small | large | {manual: true}

small: size: 1
large: size: 100
`

	file, err := parser.ParseFile("x.cue", src, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	ev := eval.New(eval.Config{
		IP: ast.ParseImportPath("example.test/p").Canonical(),
	}, file)

	// Exploring the graph provokes evaluation as needed: there is no
	// explicit evaluation step.
	x := ev.Root().Field("x")

	// x has one Decl of kind DeclField, whose Value is the whole
	// disjunction expression, and one Decl of kind DeclDisjunct per
	// operand of the parsed tree - including the interior `small |
	// large` node (the disjunction is parsed as a nested
	// [ast.BinaryExpr]). The disjunct Decls' Values are shared with
	// the field Decl's parse tree, so indexing them by identity lets
	// us match each branch to its Decl.
	var fieldDecl *eval.Decl
	disjuncts := make(map[ast.Node]*eval.Decl)
	for d := range x.Decls() {
		switch d.Kind() {
		case eval.DeclField:
			fieldDecl = d
		case eval.DeclDisjunct:
			disjuncts[d.Value()] = d
		}
	}

	// The AST is authoritative for the disjunction's shape: flatten
	// the parsed tree (`a | b | c` parses as `(a | b) | c`) into its
	// leaf branches.
	var branches func(ast.Expr) []ast.Expr
	branches = func(e ast.Expr) []ast.Expr {
		if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.OR {
			return append(branches(bin.X), branches(bin.Y)...)
		}
		return []ast.Expr{e}
	}

	for _, branch := range branches(fieldDecl.Value().(ast.Expr)) {
		d := disjuncts[branch]

		// Fields declared literally within the branch come from the
		// branch's own Decl...
		var fields []string
		for name := range d.Fields() {
			fields = append(fields, name)
		}

		// ...and fields reached through references come from
		// resolving the branch expression and expanding the result.
		// A struct-literal branch is not a path element: it resolves
		// to nothing, and expanding the empty set yields nothing.
		for name := range d.Resolve(branch).Expand().Fields() {
			fields = append(fields, name)
		}

		branchSrc, _ := format.Node(branch)
		fmt.Printf("branch %s has fields %v\n", branchSrc, fields)
	}

	// Output:
	// branch small has fields [size]
	// branch large has fields [size]
	// branch {manual: true} has fields [manual]
}
