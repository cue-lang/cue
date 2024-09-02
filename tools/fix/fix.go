// Copyright 2019 CUE Authors
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

// Package fix contains functionality for writing CUE files with legacy
// syntax to newer ones.
//
// Note: the transformations that are supported in this package will change
// over time.
package fix

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
)

type Option func(*options)

type options struct {
	simplify bool
}

// Simplify enables fixes that simplify the code, but are not strictly
// necessary.
func Simplify() Option {
	return func(o *options) { o.simplify = true }
}

// File applies fixes to f and returns it. It alters the original f.
func File(f *ast.File, o ...Option) *ast.File {
	var options options
	for _, f := range o {
		f(&options)
	}

	// Make sure we use the "after" function, and not the "before",
	// because "before" will stop recursion early which creates
	// problems with nested expressions.
	f = astutil.Apply(f, nil, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.BinaryExpr:
			switch n.Op {
			case token.IDIV, token.IMOD, token.IQUO, token.IREM:
				// Rewrite integer division operations to use builtins.
				ast.SetRelPos(n.X, token.NoSpace)
				c.Replace(&ast.CallExpr{
					// Use the __foo version to prevent accidental shadowing.
					Fun:  ast.NewIdent("__" + n.Op.String()),
					Args: []ast.Expr{n.X, n.Y},
				})

			case token.ADD, token.MUL:
				// The fix here only works when at least one argument is a
				// literal list. It would be better to be able to use CUE
				// to infer type information, and then apply the fix to
				// all places where we infer a list argument.
				x, y := n.X, n.Y
				_, xIsList := x.(*ast.ListLit)
				_, yIsList := y.(*ast.ListLit)
				if !(xIsList || yIsList) {
					break
				}
				pkg := c.Import("list")
				if pkg == nil {
					break
				}
				if n.Op == token.ADD {
					// Rewrite list addition to use list.Concat
					ast.SetRelPos(x, token.NoSpace)
					c.Replace(&ast.CallExpr{
						Fun:  ast.NewSel(pkg, "Concat"),
						Args: []ast.Expr{ast.NewList(x, y)},
					})
				} else {
					// Rewrite list multiplication to use list.Repeat
					if !xIsList {
						x, y = y, x
					}
					ast.SetRelPos(x, token.NoSpace)
					c.Replace(&ast.CallExpr{
						Fun:  ast.NewSel(pkg, "Repeat"),
						Args: []ast.Expr{x, y},
					})
				}
			}
		}
		return true
	}).(*ast.File)

	if options.simplify {
		f = simplify(f)
	}

	return f
}
