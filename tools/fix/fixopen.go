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

// mayBeClosed reports whether the expression the flags were collected
// from may resolve to a closed value.
func (f embedFlags) mayBeClosed() bool {
	return f.def || f.other || f.close
}

type closeInfo struct {
	// Do not close enclosing structs if non-zero. This may be the case
	// for comprehensions, nested structs, etc.
	suspendReclose int

	// The scope is lexically inside a comprehension value. Unlike the
	// rest of closeInfo, it is inherited by nested scopes (see pushScope).
	inComprehension bool

	// The scope is the value of a comprehension, not a field nested
	// inside one. Only embeddings declared directly in a comprehension
	// value lose the old conditional closing and get a TODO comment.
	compValue bool

	embedFlags
}

func (c closeInfo) shouldReclose() bool {
	return c.suspendReclose == 0
}

func fixExplicitOpen(f *ast.File) (result *ast.File, hasChanges bool) {

	var info closeInfo
	recloseStack := []closeInfo{}
	// pushScope and popScope bracket the traversal of a node that starts
	// a new reclose scope: fields, conjunctions and disjunctions, and
	// comprehensions.
	pushScope := func(next closeInfo) {
		next.inComprehension = next.inComprehension || info.inComprehension
		recloseStack = append(recloseStack, info)
		info = next
	}
	popScope := func(c astutil.Cursor) {
		info = recloseStack[len(recloseStack)-1]
		recloseStack = recloseStack[:len(recloseStack)-1]
		c.ClearEnclosingModified()
	}
	// Each struct literal collects the embedFlags of its own embedded
	// declarations and decides its wrapper from exactly those: flags
	// must not leak to sibling literals in the same scope. Enclosing
	// literals re-collect them through collectEmbedFlags, which
	// descends into embedded literals.
	var flagsStack []embedFlags
	result = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.Field:
			next := closeInfo{}
			// Fields with definition labels reclose on their own. Fields
			// inside comprehensions never wrap: a wrapper would deny
			// fields that the old semantics allowed (see
			// openCompFieldValue).
			if internal.IsDefinition(n.Label) || info.inComprehension {
				next.suspendReclose = 1
			}
			pushScope(next)

		case *ast.BinaryExpr:
			if n.Op == token.AND || n.Op == token.OR {
				pushScope(closeInfo{})
			}

		case *ast.Comprehension:
			// Comprehensions are a scope boundary like conjunctions:
			// embedFlags collected inside the comprehension value must
			// not add a wrapper to the enclosing struct. No wrapper is
			// added to the comprehension value either (suspendReclose):
			// the old conditional closing cannot be expressed, as builtin
			// wrappers evaluate their argument without the enclosing
			// struct's fields, breaking self-referential guards. Opened
			// embeddings inside comprehension values are instead flagged
			// with a TODO comment.
			pushScope(closeInfo{
				suspendReclose:  1,
				inComprehension: true,
				compValue:       true,
			})

		case *ast.EmbedDecl:
			info.suspendReclose++

		case *ast.StructLit:
			flagsStack = append(flagsStack, info.embedFlags)
			info.embedFlags = embedFlags{}
		}
		return true
	}, func(c astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Field:
			popScope(c)

			// See openCompFieldValue: comprehension conjuncts did not
			// close their fields under the old semantics.
			if info.inComprehension {
				if newValue, changed := openCompFieldValue(n.Value); changed {
					n.Value = newValue
					hasChanges = true
				}
			}

		case *ast.BinaryExpr:
			if n.Op == token.AND || n.Op == token.OR {
				popScope(c)
			}

		case *ast.Comprehension:
			popScope(c)

		case *ast.EmbedDecl:
			info.suspendReclose--

			// Rewrite the embedding in the post-visit so that nested
			// embeddings (e.g. inside struct operands of a conjunction)
			// have already been processed; a Replace in the pre-visit
			// would prevent the children from being traversed at all.
			newExpr, exprChanged, flags := openEmbedExpr(n.Expr)
			info.embedFlags = info.embedFlags.or(flags)
			if exprChanged {
				if info.compValue {
					ast.AddComment(newExpr, todoComment(
						"the old semantics closed the enclosing struct when the comprehension fired; this is no longer the case."))
				}
				if flags.def && len(recloseStack) == 0 {
					ast.AddComment(newExpr, todoComment(
						"top-level definition embedding opened; if this is intended as a schema, remove the '...'."))
				}
				c.Replace(&ast.EmbedDecl{Expr: newExpr})
				hasChanges = true
			}

		case *ast.StructLit:
			flags := info.embedFlags
			info.embedFlags = flagsStack[len(flagsStack)-1]
			flagsStack = flagsStack[:len(flagsStack)-1]
			if c.Modified() && info.shouldReclose() {
				hasChanges = true

				// A hoisted close() carries its closing only in the close
				// flag, so the single-embed shortcuts below would drop it.
				// They still apply when the embedded expression guarantees
				// the closing by itself (a definition, with no operand
				// that needs a runtime check).
				closeSubsumed := flags.def && !flags.other && !flags.forceReclose

				// Single embedding: { expr } ≡ expr, so the wrapper can
				// often be omitted. This is decided in post-processing
				// (after ... was added) because at the EmbedDecl
				// pre-visit level we don't yet know the parent struct's
				// element count.
				if embed, ok := singleEmbed(n); ok {
					if pf, ok := embed.Expr.(*ast.PostfixExpr); ok && pf.Op == token.ELLIPSIS {
						if !flags.close || closeSubsumed {
							// Use the expression directly without
							// wrapping; strip the ... since it is not
							// needed outside a wrapper.
							//
							// TODO: this incorrectly fires for
							// single-embed structs inside close() at
							// field-value level, e.g. a: close({_repo}).
							// Fix by tracking whether we are inside a
							// close() argument.
							embed.Expr = pf.X
							c.ClearEnclosingModified()
							break
						}
					} else {
						// A bare embedding: the struct argument of a
						// hoisted close() call.
						if !flags.close {
							c.ClearEnclosingModified()
							break
						}
						// {close(X)} for a struct literal X: unwrap {X}
						// to X so that the wrapper below restores
						// close(X).
						if s, ok := embed.Expr.(*ast.StructLit); ok {
							n = s
						}
					}
				}

				ast.SetRelPos(n, token.NoSpace)
				var wrapper ast.Expr = n
				switch {
				case flags.def && !flags.forceReclose:
					wrapper = ast.NewCall(ast.NewIdent("__closeAll"), n)
				case flags.other || flags.forceReclose:
					wrapper = ast.NewCall(ast.NewIdent("__reclose"), n)
					if flags.close {
						wrapper = ast.NewCall(ast.NewIdent("close"), wrapper)
					}
				case flags.close:
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

// openCompFieldValue adds a postfix ellipsis to a field value inside a
// comprehension when the value may resolve to a closed struct. Under the
// old semantics, conjuncts inserted through comprehensions were treated
// like embeddings and did not close their fields.
func openCompFieldValue(expr ast.Expr) (ast.Expr, bool) {
	// PostfixExpr is excluded: the value already has an ellipsis.
	switch expr.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr,
		*ast.BinaryExpr, *ast.ParenExpr, *ast.CallExpr:
		if collectEmbedFlags(expr).mayBeClosed() {
			return addEllipsis(expr), true
		}
	}
	return expr, false
}

// collectEmbedFlags recurses into an expression to collect embedding flags
// without modifying the expression. It is the single classifier of what an
// embedded expression may resolve to; [openEmbedExpr] derives its rewrites
// from the flags it returns.
func collectEmbedFlags(expr ast.Expr) embedFlags {
	switch x := expr.(type) {
	case *ast.PostfixExpr:
		// Already has ellipsis (e.g. rewritten by a nested pass);
		// still collect flags from the underlying expression.
		if x.Op == token.ELLIPSIS {
			return collectEmbedFlags(x.X)
		}
	case *ast.BinaryExpr:
		if x.Op == token.AND || x.Op == token.OR {
			xf := collectEmbedFlags(x.X)
			yf := collectEmbedFlags(x.Y)
			f := xf.or(yf)
			if x.Op == token.OR && (xf.other || yf.other) {
				f.forceReclose = true
			}
			return f
		}
		// Other binary ops (e.g. +, *) cannot resolve to closed structs.
		return embedFlags{}
	case *ast.ParenExpr:
		return collectEmbedFlags(x.X)
	case *ast.Ident:
		if x.Name == "_" {
			return embedFlags{}
		}
		if internal.IsDefinition(x) {
			return embedFlags{def: true}
		}
		return embedFlags{other: true}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "close":
				f := embedFlags{close: true}
				if len(x.Args) == 1 {
					_, _, af := openCloseArg(x.Args[0])
					f = f.or(af)
				}
				return f
			case "and", "or":
				return embedFlags{other: true}
			}
		}
		return embedFlags{}
	case *ast.UnaryExpr:
		// The default marker *X takes on X's closedness, but the
		// disjunction it appears in may resolve to another branch,
		// so the closing always needs a runtime check.
		if x.Op == token.MUL && collectEmbedFlags(x.X).mayBeClosed() {
			return embedFlags{other: true}
		}
		return embedFlags{}
	case *ast.StructLit:
		// A struct literal is open by itself, but embeddings inside
		// it may still close it.
		var f embedFlags
		for _, d := range x.Elts {
			if e, ok := d.(*ast.EmbedDecl); ok {
				f = f.or(collectEmbedFlags(e.Expr))
			}
		}
		return f
	case *ast.ListLit, // Lists cannot be opened anyway (atm).
		*ast.BasicLit,
		*ast.Interpolation:
		return embedFlags{}
	}

	// Default: may resolve to a closed struct (SelectorExpr, IndexExpr, etc.)
	return embedFlags{other: true}
}

// openEmbedExpr adds postfix ellipsis to embedded expressions, classifying
// them via [collectEmbedFlags]. Conjunctions, disjunctions, and parenthesized
// expressions always get ... on the whole expression; embedded close() calls
// are hoisted; any other expression gets ... exactly when its flags indicate
// it may resolve to a closed value.
func openEmbedExpr(expr ast.Expr) (result ast.Expr, changed bool, flags embedFlags) {
	switch x := expr.(type) {
	case *ast.PostfixExpr:
		// Already has ellipsis; still collect flags from the underlying
		// expression, as they influence the wrapping of the enclosing
		// struct.
		return expr, false, collectEmbedFlags(x)

	case *ast.BinaryExpr:
		if x.Op != token.AND && x.Op != token.OR {
			// Other binary ops (e.g. +, *) don't need ellipsis.
			return expr, false, embedFlags{}
		}
		// Add ... to the entire expression rather than each operand,
		// even when no operand may resolve to a closed value.
		return addEllipsis(expr), true, collectEmbedFlags(x)

	case *ast.ParenExpr:
		// Add ... to the whole parenthesized expression.
		return addEllipsis(expr), true, collectEmbedFlags(x)

	case *ast.StructLit:
		// The literal itself needs no ellipsis — its inner embeddings
		// were already opened — but their flags still influence the
		// wrapping of the enclosing struct.
		return expr, false, collectEmbedFlags(x)

	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "close" {
			// Under the old semantics, embedding close(X) closed the
			// enclosing struct while still allowing its literal
			// fields; a strict embedding of close(X) would deny them.
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
		}
	}

	if f := collectEmbedFlags(expr); f.mayBeClosed() {
		return addEllipsis(expr), true, f
	}
	return expr, false, embedFlags{}
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
		return newExpr, f.mayBeClosed(), f
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

// singleEmbed returns the sole element of s if it is a single embedded
// declaration.
func singleEmbed(s *ast.StructLit) (*ast.EmbedDecl, bool) {
	if len(s.Elts) != 1 {
		return nil, false
	}
	e, ok := s.Elts[0].(*ast.EmbedDecl)
	return e, ok
}

func addEllipsis(expr ast.Expr) *ast.PostfixExpr {
	return &ast.PostfixExpr{
		X:     expr,
		Op:    token.ELLIPSIS,
		OpPos: expr.End(),
	}
}
