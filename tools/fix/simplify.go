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

package fix

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
)

func simplify(f *ast.File) *ast.File {
	// Rewrite disjunctions with _ to _.
	f = astutil.Apply(f, nil, func(c astutil.Cursor) bool {
		if x, ok := c.Node().(ast.Expr); ok {
			if y := elideTop(x); x != y {
				c.Replace(y)
			}
		}
		return true
	}).(*ast.File)

	return f
}

func elideTop(x ast.Expr) ast.Expr {
	switch x := x.(type) {
	case *ast.BinaryExpr:
		switch x.Op {
		case token.OR:
			if isTop(x.X) {
				return x.X
			}
			if isTop(x.Y) {
				ast.SetRelPos(x.Y, token.NoRelPos)
				return x.Y
			}

		case token.AND:
			if isTop(x.X) {
				ast.SetRelPos(x.Y, token.NoRelPos)
				return x.Y
			}
			if isTop(x.Y) {
				return x.X
			}
		}

	case *ast.ParenExpr:
		switch x.X.(type) {
		case *ast.BinaryExpr, *ast.UnaryExpr:
		default:
			return x.X
		}
	}
	return x
}

func isTop(x ast.Expr) bool {
	v, ok := x.(*ast.Ident)
	return ok && v.Name == "_"
}
