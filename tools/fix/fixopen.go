// Copyright 2025 CUE Authors
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
	"cuelang.org/go/internal"
)

func todoComment(msg string) *ast.CommentGroup {
	return &ast.CommentGroup{
		Doc:  true,
		List: []*ast.Comment{{Text: "// TODO(cue-fix): " + msg}},
	}
}

// embedFlags tracks what kind of closing an embedding requires.
type embedFlags struct {
	def          bool // a definition was embedded
	other        bool // another embedding was modified (needs runtime check)
	forceReclose bool // disjunction has non-def operand; __closeAll would be wrong
	close        bool // close() was embedded and hoisted to wrapper level
}

func (a embedFlags) or(b embedFlags) embedFlags {
	return embedFlags{
		def:          a.def || b.def,
		other:        a.other || b.other,
		forceReclose: a.forceReclose || b.forceReclose,
		close:        a.close || b.close,
	}
}

type closeInfo struct {
	// Do not close enclosing structs if non-zero. This may be the case
	// for comprehensions, nested structs, etc.
	suspendReclose int

	// inComprehension tracks whether we are inside a comprehension value.
	inComprehension int

	embedFlags
}

func (c closeInfo) shouldReclose() bool {
	return c.suspendReclose == 0
}

func fixExplicitOpen(f *ast.File) (result *ast.File, hasChanges bool) {

	var info closeInfo
	recloseStack := []closeInfo{}
	result = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.Field:
			recloseStack = append(recloseStack, info)
			info = closeInfo{}
			if internal.IsDefinition(n.Label) {
				info.suspendReclose++
			}

		case *ast.BinaryExpr:
			if n.Op == token.AND || n.Op == token.OR {
				recloseStack = append(recloseStack, info)
				info = closeInfo{}
			}

		case *ast.Comprehension:
			info.suspendReclose++
			info.inComprehension++

		case *ast.EmbedDecl:
			info.suspendReclose++

			newExpr, exprChanged, flags := openEmbedExpr(n.Expr)
			info.embedFlags = info.embedFlags.or(flags)
			if exprChanged {
				if info.inComprehension > 0 {
					ast.AddComment(newExpr, todoComment(
						"... may not be intended inside a comprehension value; consider removing it."))
				}
				if flags.def && len(recloseStack) == 0 {
					ast.AddComment(newExpr, todoComment(
						"top-level definition embedding opened; if this is intended as a schema, remove the '...'."))
				}
				newEmbed := &ast.EmbedDecl{Expr: newExpr}
				// After is not called after a Replace: match nesting count.
				info.suspendReclose--
				c.Replace(newEmbed)
				hasChanges = true
			}
		}
		return true
	}, func(c astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Field:
			info = recloseStack[len(recloseStack)-1]
			recloseStack = recloseStack[:len(recloseStack)-1]
			c.ClearEnclosingModified()

		case *ast.BinaryExpr:
			if n.Op == token.AND || n.Op == token.OR {
				info = recloseStack[len(recloseStack)-1]
				recloseStack = recloseStack[:len(recloseStack)-1]
				c.ClearEnclosingModified()
			}

		case *ast.Comprehension:
			info.suspendReclose--
			info.inComprehension--

		case *ast.EmbedDecl:
			info.suspendReclose--

		case *ast.StructLit:
			if c.Modified() && info.shouldReclose() {
				hasChanges = true

				if isSingleEmbed(n) {
					// Single embedding: { expr } ≡ expr, so use the
					// expression directly without wrapping. Strip the
					// ... since it is not needed outside a wrapper.
					//
					// This is done in post-processing (after ... was
					// added) because at the EmbedDecl pre-visit level
					// we don't yet know the parent struct's element
					// count.
					//
					// TODO: this incorrectly fires for single-embed
					// structs inside close() at field-value level,
					// e.g. a: close({_repo}). Fix by tracking whether
					// we are inside a close() argument.
					embed := n.Elts[0].(*ast.EmbedDecl)
					pf := embed.Expr.(*ast.PostfixExpr)
					embed.Expr = pf.X
					c.ClearEnclosingModified()
					break
				}

				// Single close()-hoisted embed (bare embedding, no ...):
				// under old semantics, embedding close() opened up, so
				// a single close() embed produces an open struct — no
				// wrapping needed.
				if isSingleBareEmbed(n) {
					c.ClearEnclosingModified()
					break
				}

				ast.SetRelPos(n, token.NoSpace)
				var wrapper ast.Expr = n
				switch {
				case info.def && !info.forceReclose:
					wrapper = ast.NewCall(ast.NewIdent("__closeAll"), n)
				case info.other || info.forceReclose:
					wrapper = ast.NewCall(ast.NewIdent("__reclose"), n)
					if info.close {
						wrapper = ast.NewCall(ast.NewIdent("close"), wrapper)
					}
				case info.close:
					wrapper = ast.NewCall(ast.NewIdent("close"), n)
				}
				c.Replace(wrapper)
				c.ClearEnclosingModified()
			}
		}
		return true
	}).(*ast.File)

	return result, hasChanges
}

// collectEmbedFlags recurses into an expression to collect embedding flags
// without modifying the expression. Used for & and | where we add ... to the
// whole expression rather than individual operands.
func collectEmbedFlags(expr ast.Expr) (ast.Expr, bool, embedFlags) {
	switch x := expr.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.AND || x.Op == token.OR {
			_, _, xf := collectEmbedFlags(x.X)
			_, _, yf := collectEmbedFlags(x.Y)
			f := xf.or(yf)
			if x.Op == token.OR && (xf.other || yf.other) {
				f.forceReclose = true
			}
			return expr, false, f
		}
	case *ast.ParenExpr:
		return collectEmbedFlags(x.X)
	case *ast.Ident:
		if x.Name == "_" {
			return expr, false, embedFlags{}
		}
		if internal.IsDefinition(x) {
			return expr, false, embedFlags{def: true}
		}
		return expr, false, embedFlags{other: true}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "close":
				_, _, f := openCloseArg(x.Args[0])
				return expr, false, f.or(embedFlags{close: true})
			}
		}
	}
	return expr, false, embedFlags{}
}

// openEmbedExpr adds postfix ellipsis to embedded expressions. For & and |
// expressions, it adds ... to the whole expression. For other expressions, it
// adds ellipsis if needed based on the expression type.
func openEmbedExpr(expr ast.Expr) (result ast.Expr, changed bool, flags embedFlags) {
	switch x := expr.(type) {
	case *ast.PostfixExpr:
		// Already has ellipsis
		return expr, false, embedFlags{}

	case *ast.BinaryExpr:
		if x.Op == token.AND || x.Op == token.OR {
			// Collect flags from operands, then add ... to the
			// entire expression rather than each operand.
			_, _, xFlags := collectEmbedFlags(x.X)
			_, _, yFlags := collectEmbedFlags(x.Y)
			f := xFlags.or(yFlags)
			if x.Op == token.OR && (xFlags.other || yFlags.other) {
				f.forceReclose = true
			}
			return addEllipsis(expr), true, f
		}
		// Other binary ops (e.g. +, *) don't need ellipsis.
		return expr, false, embedFlags{}

	case *ast.ParenExpr:
		// Recurse through parens to collect flags, then add ...
		// to the whole parenthesized expression.
		_, _, f := collectEmbedFlags(x.X)
		return addEllipsis(expr), true, f

	case *ast.Ident:
		if x.Name == "_" {
			return expr, false, embedFlags{}
		}
		if internal.IsDefinition(x) {
			return addEllipsis(expr), true, embedFlags{def: true}
		}
		return addEllipsis(expr), true, embedFlags{other: true}

	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "close":
				// In the old semantics, embedding close() opened up
				// the embedding — the outer struct stayed open. Under
				// explicitopen, close() no longer opens up when embedded.
				// Hoist close() to wrapper level: return the processed
				// argument as the new embedding, and set the close flag
				// so the containing struct gets close() wrapping.
				if len(x.Args) == 1 {
					newArg, _, f := openCloseArg(x.Args[0])
					f.close = true
					astutil.CopyMeta(newArg, x)
					return newArg, true, f
				}
				return expr, true, embedFlags{close: true}
			case "and", "or":
				return addEllipsis(expr), true, embedFlags{other: true}
			}
		}
		return expr, false, embedFlags{}

	case *ast.ListLit, // Lists cannot be opened anyway (atm).
		*ast.StructLit, // Structs are open by default
		*ast.BasicLit,
		*ast.Interpolation,
		*ast.UnaryExpr:

		return expr, false, embedFlags{}
	}

	// Default: needs ellipsis (SelectorExpr, IndexExpr, etc.)
	return addEllipsis(expr), true, embedFlags{other: true}
}

// openCloseArg processes the argument of an embedded close() call,
// adding ... to any embeddings inside a struct literal. For non-struct
// arguments, it returns the flags without modifying the expression
// (adding ... to a bare identifier inside close() is not valid).
func openCloseArg(expr ast.Expr) (ast.Expr, bool, embedFlags) {
	s, ok := expr.(*ast.StructLit)
	if !ok {
		// Non-struct argument: process like a regular embedding so that
		// e.g. close(#A) → #A... when hoisted.
		newExpr, _, f := openEmbedExpr(expr)
		return newExpr, f.def || f.other || f.close, f
	}
	var f embedFlags
	var changed bool
	newElts := make([]ast.Decl, len(s.Elts))
	copy(newElts, s.Elts)
	for i, d := range newElts {
		embed, ok := d.(*ast.EmbedDecl)
		if !ok {
			continue
		}
		newExpr, exprChanged, ef := openEmbedExpr(embed.Expr)
		f = f.or(ef)
		if exprChanged {
			changed = true
			newElts[i] = &ast.EmbedDecl{Expr: newExpr}
		}
	}
	if !changed {
		return expr, false, f
	}
	newStruct := *s
	newStruct.Elts = newElts
	return &newStruct, true, f
}

// isSingleEmbed reports whether s contains exactly one declaration
// which is an embedding with postfix ellipsis (...).
func isSingleEmbed(s *ast.StructLit) bool {
	if len(s.Elts) != 1 {
		return false
	}
	embed, ok := s.Elts[0].(*ast.EmbedDecl)
	if !ok {
		return false
	}
	pf, ok := embed.Expr.(*ast.PostfixExpr)
	if !ok || pf.Op != token.ELLIPSIS {
		return false
	}
	return true
}

// isSingleBareEmbed reports whether s contains exactly one declaration
// which is an embedding without postfix ellipsis. This occurs when a
// close() call was hoisted and its struct argument became a bare embedding.
func isSingleBareEmbed(s *ast.StructLit) bool {
	if len(s.Elts) != 1 {
		return false
	}
	_, ok := s.Elts[0].(*ast.EmbedDecl)
	return ok
}

func addEllipsis(expr ast.Expr) *ast.PostfixExpr {
	return &ast.PostfixExpr{
		X:     expr,
		Op:    token.ELLIPSIS,
		OpPos: expr.End(),
	}
}
