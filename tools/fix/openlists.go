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

// The openlists rewrite makes the files of a package keep their meaning
// under the openlists experiment.
//
// Without the experiment, a list literal without a trailing ellipsis is
// closed, and so is a list produced by slicing or by a builtin. Under the
// experiment, such lists are open outside definitions. A list in a
// definition is still closed by the definition but, as for a struct, a list
// with an ellipsis unified with it within the same definition opens it.
//
// A list literal is closed by writing it as a closed literal, #[...], which
// means what the list literal means without the experiment. It stays a list
// literal, so it keeps the closedness a definition gives its elements and can
// refer to its own elements; wrapping it in close would lose both. A list
// literal in a definition is closed by the definition already; the rewrite
// closes it anyway, so that the file reads the same with or without the
// experiment. A slice or a call to a builtin that returns a list is not a
// literal, and is wrapped in close instead, which closes one level, as the
// list is closed without the experiment.
//
// With the experiment, #[ starts a closed list, so an index or slice on a
// definition named # is written (#)[...].
//
// The rewrite cannot type the results of calls other than to builtins, and
// leaves such calls alone. Nor can it type the operand of a slice: it leaves
// a slice of a string or bytes literal alone and assumes that any other
// operand is a list, so that a slice of a string held in a field is wrapped
// in close, which then fails.

package fix

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
)

// rewriteOpenLists rewrites f and reports whether it changed. returnsList
// reports whether a call in f returns a list; it may be nil.
func rewriteOpenLists(f *ast.File, returnsList func(*ast.CallExpr) bool) bool {
	changed := false
	astutil.Apply(f, nil, func(c astutil.Cursor) bool {
		// An index or slice on a definition named # is parenthesized even
		// as the argument of close; the argument itself is not closed
		// again.
		switch n := c.Node().(type) {
		case *ast.ListLit:
			if !isCloseArg(c) && !n.Hash.IsValid() && !hasEllipsis(n) && !isLabel(c) {
				// The "#" takes the place of the bracket, which follows it
				// directly.
				n.Hash = n.Lbrack
				n.Lbrack = n.Lbrack.WithRel(token.NoSpace)
				changed = true
			}
		case *ast.IndexExpr:
			if parenthesizeHash(&n.X) {
				changed = true
			}
		case *ast.SliceExpr:
			if parenthesizeHash(&n.X) {
				changed = true
			}
			if isCloseArg(c) || isStringLit(n.X) {
				break
			}
			c.Replace(ast.NewCall(ast.NewIdent("close"), n))
			changed = true
		case *ast.CallExpr:
			if !isCloseArg(c) && returnsList != nil && returnsList(n) {
				c.Replace(ast.NewCall(ast.NewIdent("close"), n))
				changed = true
			}
		}
		return true
	})
	return changed
}

// hasEllipsis reports whether a list literal ends in an ellipsis, which
// keeps it open with or without the experiment.
func hasEllipsis(l *ast.ListLit) bool {
	if n := len(l.Elts); n > 0 {
		_, ok := l.Elts[n-1].(*ast.Ellipsis)
		return ok
	}
	return false
}

// parenthesizeHash wraps *x in parentheses if it is a reference to a
// definition named #, which must be written (#) when indexed.
func parenthesizeHash(x *ast.Expr) bool {
	if id, ok := (*x).(*ast.Ident); ok && id.Name == "#" {
		*x = &ast.ParenExpr{X: id}
		return true
	}
	return false
}

// isLabel reports whether the node at c is the label of a field, as for a
// pattern constraint [string]: T, possibly behind an alias.
func isLabel(c astutil.Cursor) bool {
	n := c.Node()
	p := c.Parent()
	if p == nil {
		return false
	}
	if a, ok := p.Node().(*ast.Alias); ok && a.Expr == n {
		n = a
		if p = p.Parent(); p == nil {
			return false
		}
	}
	f, ok := p.Node().(*ast.Field)
	return ok && f.Label == n
}

// isStringLit reports whether x is a string or bytes literal, whose slice is
// not a list.
func isStringLit(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING
	case *ast.Interpolation:
		return true
	}
	return false
}

// isCloseArg reports whether the node at c is already the argument of a call
// to close.
func isCloseArg(c astutil.Cursor) bool {
	p := c.Parent()
	if p == nil {
		return false
	}
	call, ok := p.Node().(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Args[0] != c.Node() {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "close"
}
