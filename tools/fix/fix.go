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
				_, xIsConcat := concatCallArgs(x)
				_, yIsConcat := concatCallArgs(y)

				if n.Op == token.ADD {
					if !(xIsList || xIsConcat || yIsList || yIsConcat) {
						break
					}
					// Rewrite list addition to use list.Concat
					exprs := expandConcats(x, y)
					ast.SetRelPos(x, token.NoSpace)
					c.Replace(ast.NewCall(
						ast.NewSel(&ast.Ident{
							Name: "list",
							Node: ast.NewImport(nil, "list"),
						}, "Concat"), ast.NewList(exprs...)),
					)

				} else {
					if !(xIsList || yIsList) {
						break
					}
					// Rewrite list multiplication to use list.Repeat
					if !xIsList {
						x, y = y, x
					}
					ast.SetRelPos(x, token.NoSpace)
					c.Replace(ast.NewCall(
						ast.NewSel(&ast.Ident{
							Name: "list",
							Node: ast.NewImport(nil, "list"),
						}, "Repeat"), x, y),
					)
				}
			}
		}
		return true
	}).(*ast.File)

	if options.simplify {
		f = simplify(f)
	}

	err := astutil.Sanitize(f)
	// TODO: this File method is public, and its signature was fixed
	// before we started calling Sanitize. Ideally, we want to return
	// this error, but that would require deprecating this File method,
	// and creating a new one, which might happen in due course if we
	// also discover that we need to be a bit more flexible than just
	// accepting a File.
	if err != nil {
		panic(err)
	}
	return f
}

func expandConcats(exprs ...ast.Expr) (result []ast.Expr) {
	for _, expr := range exprs {
		list, ok := concatCallArgs(expr)
		if ok {
			result = append(result, expandConcats(list.Elts...)...)
		} else {
			result = append(result, expr)
		}
	}
	return result
}

func concatCallArgs(expr ast.Expr) (*ast.ListLit, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	name, ok := sel.X.(*ast.Ident)
	if !ok || name.Name != "list" {
		return nil, false
	}
	name, ok = sel.Sel.(*ast.Ident)
	if !ok || name.Name != "Concat" {
		return nil, false
	}
	if len(call.Args) != 1 {
		return nil, false
	}
	list, ok := call.Args[0].(*ast.ListLit)
	if !ok {
		return nil, false
	}
	return list, true
}
