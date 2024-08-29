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

	// Rewrite integer division operations to use builtins.
	f = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch x := n.(type) {
		case *ast.BinaryExpr:
			switch x.Op {
			case token.IDIV, token.IMOD, token.IQUO, token.IREM:
				ast.SetRelPos(x.X, token.NoSpace)
				c.Replace(&ast.CallExpr{
					// Use the __foo version to prevent accidental shadowing.
					Fun:  ast.NewIdent("__" + x.Op.String()),
					Args: []ast.Expr{x.X, x.Y},
				})
			}
		}
		return true
	}, nil).(*ast.File)

	// Rewrite list addition to use list.Concat
	f = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.BinaryExpr:
			switch n.Op {
			case token.ADD:
				_, xIsList := n.X.(*ast.ListLit)
				_, yIsList := n.Y.(*ast.ListLit)
				if !(xIsList || yIsList) {
					break
				}
				ast.SetRelPos(n.X, token.NoSpace)
				pkg := c.Import("list")
				if pkg == nil {
					break
				}
				c.Replace(&ast.CallExpr{
					Fun:  ast.NewSel(pkg, "Concat"),
					Args: []ast.Expr{ast.NewList(n.X, n.Y)},
				})
			}
		}
		return true
	}, nil).(*ast.File)

	// Rewrite list multiplication to use list.Repeat
	f = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.BinaryExpr:
			switch n.Op {
			case token.MUL:
				x, y := n.X, n.Y
				_, xIsList := x.(*ast.ListLit)
				_, yIsList := y.(*ast.ListLit)
				if !(xIsList || yIsList) {
					break
				}
				if !xIsList {
					x, y = y, x
				}
				ast.SetRelPos(x, token.NoSpace)
				pkg := c.Import("list")
				if pkg == nil {
					break
				}
				c.Replace(&ast.CallExpr{
					Fun:  ast.NewSel(pkg, "Repeat"),
					Args: []ast.Expr{x, y},
				})
			}
		}
		return true
	}, nil).(*ast.File)

	if options.simplify {
		f = simplify(f)
	}

	return f
}
