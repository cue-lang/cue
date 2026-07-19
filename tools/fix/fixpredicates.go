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

package fix

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
)

// fixPredicates rewrites comparisons against bottom to the isValid predicate
// builtin:
//
//	x != _|_   becomes   isValid(x)
//	x == _|_   becomes   !isValid(x)
//
// A parenthesized bottom operand is matched too, and redundant parentheses
// around the argument are dropped, since a call argument needs none. A
// comparison whose bottom side carries a comment is left alone, as the
// rewrite would drop the comment along with the bottom.
//
// isValid is the designated replacement for the comparison-to-bottom idiom.
// The two agree on concrete values (true), permanent errors (false),
// unresolved or absent references (false), composite values with
// non-concrete or unresolved members (true), and the unification guard
// (x & T) != _|_, which is the idiom's most common use.
//
// They diverge on one case, which this rewrite cannot detect and does not
// try to: a reference to a value that is declared but abstract. Given
// x: {a: int}, the comparison x.a != _|_ is false, because the comparison
// demands a concrete operand, whereas isValid(x.a) is true, because an
// abstract type is satisfiable. Code that used the idiom as a concreteness
// test therefore flips, and a guard written as
//
//	if x.a != _|_ { ... }
//
// starts taking its branch where it previously did not. Such code wants
// isConcrete, not isValid, but only the author knows which was meant, so the
// rewrite leaves the choice to them. (A bare abstract literal, as in
// int != _|_, is an incomplete error rather than false, so it does not arise
// in working configurations.)
//
// The rewrite emits the predeclared isValid identifier via
// [ast.NewPredeclared], so [astutil.Sanitize] renames it to the __isValid
// form wherever a user declaration would shadow the builtin.
func fixPredicates(f *ast.File) (result *ast.File, hasChanges bool) {
	result = astutil.Apply(f, nil, func(c astutil.Cursor) bool {
		n, ok := c.Node().(*ast.BinaryExpr)
		if !ok || (n.Op != token.NEQ && n.Op != token.EQL) {
			return true
		}
		xIsBottom := isBottom(n.X)
		yIsBottom := isBottom(n.Y)
		var arg ast.Expr
		switch {
		case yIsBottom && !xIsBottom:
			arg = n.X
		case xIsBottom && !yIsBottom:
			arg = n.Y
		default:
			// No bottom operand, or the degenerate _|_ == _|_: leave as is.
			return true
		}
		// Drop redundant parentheses around the retained argument; a call
		// argument needs none. Stop at parens carrying comments, which the
		// shared unparen would discard along with them.
		for {
			p, ok := arg.(*ast.ParenExpr)
			if !ok || len(ast.Comments(p)) > 0 {
				break
			}
			arg = p.X
		}

		// The comparison's position is that of its first token, even when
		// that is the bottom; read it before arg loses its own.
		pos := n.Pos()
		ast.SetRelPos(arg, token.NoSpace)
		var repl ast.Expr = ast.NewCall(ast.NewPredeclared("isValid"), arg)
		if n.Op == token.EQL {
			repl = &ast.UnaryExpr{Op: token.NOT, X: repl}
		}
		// Keep the line break and comma before the comparison, as in a list
		// element or a continued && chain.
		ast.SetPos(repl, pos)
		ast.SetComments(repl, ast.Comments(n))
		c.Replace(repl)
		hasChanges = true
		return true
	}).(*ast.File)

	return result, hasChanges
}

// isBottom reports whether x is a bottom literal, possibly parenthesized,
// without comments on the literal or its parentheses.
func isBottom(x ast.Expr) bool {
	for {
		if len(ast.Comments(x)) > 0 {
			return false
		}
		p, ok := x.(*ast.ParenExpr)
		if !ok {
			break
		}
		x = p.X
	}
	_, ok := x.(*ast.BottomLit)
	return ok
}
