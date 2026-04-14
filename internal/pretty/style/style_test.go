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

package style_test

import (
	"reflect"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/pretty/style"
)

// leadingRel returns the effective leading RelPos of n: the first
// doc-position (Position==0) comment's Slash RelPos if any, otherwise
// n.Pos().RelPos(). We mirror pretty.LeadingRelPos here so the tests
// read the same position the renderer does.
func leadingRel(n ast.Node) token.RelPos {
	for _, cg := range ast.Comments(n) {
		if cg.Position == 0 {
			return cg.Pos().RelPos()
		}
	}
	return n.Pos().RelPos()
}

// AST builders. We deliberately leave token positions at their zero
// value in these programmatic constructions, so we can observe the
// heuristic rules acting on otherwise-blank inputs.

func ident(name string) *ast.Ident { return &ast.Ident{Name: name} }

func field(name, value string) *ast.Field {
	return &ast.Field{Label: ident(name), Value: ident(value)}
}

func definitionField(name, value string) *ast.Field {
	if !strings.HasPrefix(name, "#") {
		panic("definitionField: name must start with #")
	}
	return field(name, value)
}

func docComment(text string) *ast.CommentGroup {
	return &ast.CommentGroup{
		Doc:  true,
		List: []*ast.Comment{{Text: "// " + text}},
	}
}

func withDoc(d ast.Decl, text string) ast.Decl {
	ast.AddComment(d, docComment(text))
	return d
}

func letClause(name, value string) *ast.LetClause {
	return &ast.LetClause{Ident: ident(name), Expr: ident(value)}
}

func embedDecl(value string) *ast.EmbedDecl {
	return &ast.EmbedDecl{Expr: ident(value)}
}

func alias(name, value string) *ast.Alias {
	return &ast.Alias{Ident: ident(name), Expr: ident(value)}
}

func packageDecl(name string) *ast.Package {
	return &ast.Package{Name: ident(name)}
}

func importDecl(specs ...*ast.ImportSpec) *ast.ImportDecl {
	d := &ast.ImportDecl{Specs: specs}
	if len(specs) > 1 {
		// Multi-spec imports always come with parens, so we give Lparen
		// a valid position to make the renderer treat it as a block.
		d.Lparen = token.Blank.Pos()
	}
	return d
}

func importSpec(path string) *ast.ImportSpec {
	return &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: path}}
}

func bracedStruct(elts ...ast.Decl) *ast.StructLit {
	return &ast.StructLit{
		Lbrace: token.Blank.Pos(),
		Rbrace: token.Blank.Pos(),
		Elts:   elts,
	}
}

func bracelessStruct(elts ...ast.Decl) *ast.StructLit {
	// No Lbrace/Rbrace: we mirror the synthesised structs that
	// internal/core/export emits (e.adt builds a bare
	// &ast.StructLit{}).
	return &ast.StructLit{Elts: elts}
}

func embedStruct(s *ast.StructLit) *ast.EmbedDecl {
	return &ast.EmbedDecl{Expr: s}
}

func stringLit(s string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: `"` + s + `"`}
}

func stringLabelField(label, value string) *ast.Field {
	return &ast.Field{Label: stringLit(label), Value: ident(value)}
}

// TestAnnotateRelPos exercises the [style.Config.RelPos] flag: here we
// check the layout heuristics (the A- and B-group rules) one at a time.
func TestAnnotateRelPos(t *testing.T) {
	cfg := style.Config{RelPos: true}

	t.Run("A1_BlankAfterPackage", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{
			packageDecl("p"),
			field("x", "int"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("A2_BlankAfterImport", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{
			importDecl(importSpec(`"list"`)),
			field("x", "int"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("A3_BlankAfterCommentGroup", func(t *testing.T) {
		// A standalone CommentGroup decl gets a blank line after it,
		// matching what the old formatter did.
		cg := &ast.CommentGroup{List: []*ast.Comment{{Text: "// header"}}}
		f := &ast.File{Decls: []ast.Decl{
			cg,
			field("x", "int"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("A4_BlankBeforeDocAfterDefinition", func(t *testing.T) {
		// A doc-commented decl following a Definition Field (#X:)
		// gets a blank line.
		f := &ast.File{Decls: []ast.Decl{
			definitionField("#D", "int"),
			withDoc(field("x", "int"), "x doc"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
		// Specifically, we check that the doc comment's Slash carries
		// the RelPos - that is what pretty.LeadingRelPos reads - rather
		// than the decl's own Pos.
		docs := ast.Comments(f.Decls[1])
		if len(docs) == 0 {
			t.Fatal("decl[1] lost its doc comment")
		}
		if got, want := docs[0].Pos().RelPos(), token.NewSection; got != want {
			t.Errorf("doc comment RelPos = %v, want %v", got, want)
		}
	})

	t.Run("A4_BlankBeforeDocAfterNonField", func(t *testing.T) {
		// A doc-commented decl following any non-Field decl (here,
		// an EmbedDecl) gets a blank line.
		f := &ast.File{Decls: []ast.Decl{
			embedDecl("foo"),
			withDoc(field("x", "int"), "x doc"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("A4_NoBlankBeforeDocAfterRegularField", func(t *testing.T) {
		// A doc-commented decl following a regular (non-Definition)
		// Field gets only a Newline, not a blank line.
		f := &ast.File{Decls: []ast.Decl{
			field("a", "int"),
			withDoc(field("b", "int"), "b doc"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.Newline; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("B1_NewlineBetweenDecls", func(t *testing.T) {
		// The default rule: we put every decl after the first on its
		// own line.
		f := &ast.File{Decls: []ast.Decl{
			field("a", "int"),
			field("b", "int"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := leadingRel(f.Decls[1]), token.Newline; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	for _, tc := range []struct {
		name string
		decl ast.Decl
	}{
		{"LetClause", letClause("x", "int")},
		{"EmbedDecl", embedDecl("foo")},
		{"Comprehension", &ast.Comprehension{
			Clauses: []ast.Clause{&ast.IfClause{Condition: ident("c")}},
			Value:   bracedStruct(field("a", "int")),
		}},
		{"Alias", alias("x", "y")},
	} {
		t.Run("B2_NewlineBefore"+tc.name, func(t *testing.T) {
			// LetClause / EmbedDecl / Comprehension / Alias each
			// start on their own line by default. We make the first
			// decl a Field too, so the previous-decl context pulls in
			// no A-group rule; B2 / B1 should then produce Newline.
			f := &ast.File{Decls: []ast.Decl{
				field("a", "int"),
				tc.decl,
			}}
			if !cfg.Annotate(f) {
				t.Fatal("expected Annotate to report a change")
			}
			if got, want := leadingRel(f.Decls[1]), token.Newline; got != want {
				t.Errorf("decl[1] (%s) leading RelPos = %v, want %v",
					tc.name, got, want)
			}
		})
	}

	t.Run("B3_NewlineBeforeImportRparen", func(t *testing.T) {
		// A multi-spec ImportDecl with a valid Lparen has its
		// closing Rparen pushed to its own line.
		d := importDecl(importSpec(`"a"`), importSpec(`"b"`))
		f := &ast.File{Decls: []ast.Decl{d}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if got, want := d.Rparen.RelPos(), token.Newline; got != want {
			t.Errorf("Rparen RelPos = %v, want %v", got, want)
		}
	})

	t.Run("Conservative_PreservesStrongerRelPos", func(t *testing.T) {
		// B1 would write Newline, but the decl already carries
		// NewSection on its leading position - so we leave it alone.
		f := &ast.File{Decls: []ast.Decl{
			field("a", "int"),
			field("b", "int"),
		}}
		ast.SetRelPos(f.Decls[1], token.NewSection)
		cfg.Annotate(f)
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v (must not weaken)",
				got, want)
		}
	})

	t.Run("Conservative_PreservesDocCommentRelPos", func(t *testing.T) {
		// Same conservativeness rule, but here the strong RelPos lives
		// on the doc comment's Slash (where pretty.LeadingRelPos reads
		// it from). Annotate must not overwrite it via the decl's own
		// Pos.
		d := withDoc(field("b", "int"), "b doc")
		ast.SetRelPos(ast.Comments(d)[0], token.NewSection)
		f := &ast.File{Decls: []ast.Decl{
			field("a", "int"),
			d,
		}}
		cfg.Annotate(f)
		if got, want := leadingRel(d), token.NewSection; got != want {
			t.Errorf("decl[1] leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("ReturnsFalseWhenNoChanges", func(t *testing.T) {
		// The file already carries the exact RelPos values Annotate
		// would write, so we expect it to report no change and to leave
		// the tree untouched.
		f1 := field("a", "int")
		f2 := field("b", "int")
		ast.SetRelPos(f2, token.Newline)
		f := &ast.File{Decls: []ast.Decl{f1, f2}}
		if cfg.Annotate(f) {
			t.Error("expected Annotate to return false on already-canonical input")
		}
	})

	t.Run("DescendsIntoNestedStruct", func(t *testing.T) {
		// The catalogue applies at every struct boundary, not just
		// file scope, so we expect the inner StructLit's decls to
		// receive their own annotations.
		inner := bracedStruct(
			definitionField("#D", "int"),
			withDoc(field("y", "int"), "y doc"),
		)
		f := &ast.File{Decls: []ast.Decl{
			&ast.Field{Label: ident("x"), Value: inner},
		}}
		cfg.Annotate(f)
		if got, want := leadingRel(inner.Elts[1]), token.NewSection; got != want {
			t.Errorf("inner decl[1] leading RelPos = %v, want %v (A4 inside nested struct)",
				got, want)
		}
	})

	t.Run("LeavesFirstDeclAlone", func(t *testing.T) {
		// The first decl of a body has no predecessor; its leading
		// position describes placement relative to leading comments /
		// start of body, which is out of scope for the heuristics.
		// Annotate must not modify it.
		first := field("a", "int")
		ast.SetRelPos(first, token.Blank)
		f := &ast.File{Decls: []ast.Decl{first, field("b", "int")}}
		cfg.Annotate(f)
		if got, want := first.Pos().RelPos(), token.Blank; got != want {
			t.Errorf("decl[0] RelPos changed to %v, want %v", got, want)
		}
	})

	t.Run("FirstEltInMultiStructBody", func(t *testing.T) {
		// {a: 1, b: 2} - inside a struct body of 2+ elts with no
		// explicit "stay inline" hint, we upgrade the first elt's
		// leading RelPos to Newline so the body breaks uniformly across
		// lines after the opening brace.
		inner := bracedStruct(field("a", "int"), field("b", "int"))
		outer := &ast.Field{Label: ident("X"), Value: inner}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if got, want := leadingRel(inner.Elts[0]), token.Newline; got != want {
			t.Errorf("first elt leading RelPos = %v, want %v", got, want)
		}
	})

	t.Run("FirstEltInMultiStructBodyInlineHint", func(t *testing.T) {
		// When any decl carries Blank RelPos (the explicit "stay on the
		// same line" hint), we suppress openFirst and the body keeps
		// its compact inline shape.
		first := field("a", "int")
		second := field("b", "int")
		ast.SetRelPos(second.Label, token.Blank)
		inner := bracedStruct(first, second)
		outer := &ast.Field{Label: ident("X"), Value: inner}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if got := leadingRel(first); got >= token.Newline {
			t.Errorf("first elt leading RelPos = %v, want < Newline", got)
		}
	})

	t.Run("FirstEltInMultiStructBodyWithDoc", func(t *testing.T) {
		// {// doc\n a: 1, b: 2} - we expect the Newline target to land
		// on the first elt's doc comment Slash, not the decl's Pos,
		// matching what pretty.LeadingRelPos reads.
		first := withDoc(field("a", "int"), "doc")
		inner := bracedStruct(first, field("b", "int"))
		outer := &ast.Field{Label: ident("X"), Value: inner}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		// The doc comment's Slash should now carry Newline RelPos.
		docs := ast.Comments(first)
		if len(docs) == 0 {
			t.Fatal("first elt lost its doc comment")
		}
		if got, want := docs[0].Pos().RelPos(), token.Newline; got != want {
			t.Errorf("doc comment Slash RelPos = %v, want %v", got, want)
		}
	})

	t.Run("FirstEltInSingleStructBodyUntouched", func(t *testing.T) {
		// {a: 1} (a single-elt body) - the chain / hug paths in pretty
		// want this kept tight, so we leave the first elt's RelPos
		// alone.
		first := field("a", "int")
		inner := bracedStruct(first)
		outer := &ast.Field{Label: ident("X"), Value: inner}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if got := leadingRel(first); got >= token.Newline {
			t.Errorf("first elt of single-elt body got RelPos = %v, expected < Newline", got)
		}
	})

	t.Run("FirstDeclInFileUntouched", func(t *testing.T) {
		// File body: the first decl's leading RelPos describes BOF
		// placement, not bracket placement, so we must not upgrade it.
		first := field("a", "int")
		f := &ast.File{Decls: []ast.Decl{first, field("b", "int")}}
		cfg.Annotate(f)
		if got := leadingRel(first); got >= token.Newline {
			t.Errorf("first decl of File got RelPos = %v, expected < Newline", got)
		}
	})

	t.Run("RuleStrengthOrdering_A1WinsOverB2", func(t *testing.T) {
		// Package -> EmbedDecl: A1 (NewSection) outranks B2 (Newline),
		// so the embed's leading position should get NewSection.
		f := &ast.File{Decls: []ast.Decl{
			packageDecl("p"),
			embedDecl("foo"),
		}}
		cfg.Annotate(f)
		if got, want := leadingRel(f.Decls[1]), token.NewSection; got != want {
			t.Errorf("EmbedDecl after Package: leading RelPos = %v, want %v",
				got, want)
		}
	})
}

// TestAnnotateRelPosBracelessEmbed checks the rule that a leading
// RelPos destined for a braceless embedded struct attaches to a
// materialised opening brace, rather than leaking onto the struct's
// first element. We assert both halves: the brace appears and the
// first element stays put.
func TestAnnotateRelPosBracelessEmbed(t *testing.T) {
	cfg := style.Config{RelPos: true}

	// topField wraps a braceless container struct (as export emits it)
	// holding the given embeds in a File, and annotates it for us.
	topField := func(embeds ...ast.Decl) *ast.File {
		f := &ast.File{Decls: []ast.Decl{
			&ast.Field{Label: ident("top"), Value: bracelessStruct(embeds...)},
		}}
		cfg.Annotate(f)
		return f
	}

	t.Run("SingleFieldEmbed", func(t *testing.T) {
		// top: { {a1: true} {a2: true} } - the second embed's brace is
		// materialised with Newline; its field does not absorb it.
		first := bracelessStruct(field("a1", "true"))
		second := bracelessStruct(field("a2", "true"))
		topField(embedStruct(first), embedStruct(second))

		if !second.Lbrace.IsValid() || second.Lbrace.RelPos() != token.Newline {
			t.Errorf("second embed Lbrace = (valid=%v, rel=%v), want (true, Newline)",
				second.Lbrace.IsValid(), second.Lbrace.RelPos())
		}
		if got := leadingRel(second.Elts[0]); got >= token.Newline {
			t.Errorf("second embed's field absorbed RelPos = %v, want < Newline (no leak)", got)
		}
		// The first embed is the anchor, so we apply no upgrade: it
		// stays braceless and renders flat with no materialised brace.
		if first.Lbrace.IsValid() {
			t.Errorf("first embed Lbrace unexpectedly materialised")
		}
	})

	t.Run("BinaryConjunction", func(t *testing.T) {
		// top: { {a1: true} {b: true} & {c: true} } - we expect the
		// leading operand of the conjunction to carry the brace +
		// Newline, while the trailing operand, not at the leading edge,
		// is left alone.
		anchor := bracelessStruct(field("a1", "true"))
		left := bracelessStruct(field("b", "true"))
		right := bracelessStruct(field("c", "true"))
		conj := &ast.EmbedDecl{Expr: &ast.BinaryExpr{X: left, Op: token.AND, Y: right}}
		topField(embedStruct(anchor), conj)

		if !left.Lbrace.IsValid() || left.Lbrace.RelPos() != token.Newline {
			t.Errorf("conjunction left operand Lbrace = (valid=%v, rel=%v), want (true, Newline)",
				left.Lbrace.IsValid(), left.Lbrace.RelPos())
		}
		if got := leadingRel(left.Elts[0]); got >= token.Newline {
			t.Errorf("left operand's field absorbed RelPos = %v, want < Newline (no leak)", got)
		}
		if right.Lbrace.IsValid() {
			t.Errorf("conjunction right operand Lbrace materialised, want untouched")
		}
	})
}

// TestAnnotateEllipsis exercises the [style.Config.Ellipsis] flag: we
// check that the various "open" markers (`...`, `[string]: _`, `[_]:
// _`) collapse to a single trailing `...` and that their comments
// follow.
func TestAnnotateEllipsis(t *testing.T) {
	cfg := style.Config{Ellipsis: true}

	t.Run("LiteralEllipsisMovedToEnd", func(t *testing.T) {
		// {..., a: 1} -> {a: 1, ...}
		ell := &ast.Ellipsis{}
		f := &ast.File{Decls: []ast.Decl{
			ell,
			field("a", "int"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if len(f.Decls) != 2 {
			t.Fatalf("len(Decls) = %d, want 2", len(f.Decls))
		}
		if _, ok := f.Decls[0].(*ast.Field); !ok {
			t.Errorf("Decls[0] = %T, want *ast.Field", f.Decls[0])
		}
		if _, ok := f.Decls[1].(*ast.Ellipsis); !ok {
			t.Errorf("Decls[1] = %T, want *ast.Ellipsis", f.Decls[1])
		}
	})

	t.Run("MultipleEllipsesCollapseToOne", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{
			&ast.Ellipsis{},
			field("a", "int"),
			&ast.Ellipsis{},
			field("b", "int"),
			&ast.Ellipsis{},
		}}
		cfg.Annotate(f)
		if len(f.Decls) != 3 {
			t.Fatalf("len(Decls) = %d, want 3 (a, b, ...)", len(f.Decls))
		}
		if _, ok := f.Decls[2].(*ast.Ellipsis); !ok {
			t.Errorf("trailing Decls[2] = %T, want *ast.Ellipsis", f.Decls[2])
		}
	})

	t.Run("StringPatternCollapses", func(t *testing.T) {
		// [string]: _ in a body becomes a trailing ...
		patternField := &ast.Field{
			Label: &ast.ListLit{Elts: []ast.Expr{ident("string")}},
			Value: ident("_"),
		}
		f := &ast.File{Decls: []ast.Decl{
			patternField,
			field("a", "int"),
		}}
		cfg.Annotate(f)
		if len(f.Decls) != 2 {
			t.Fatalf("len(Decls) = %d, want 2", len(f.Decls))
		}
		if _, ok := f.Decls[1].(*ast.Ellipsis); !ok {
			t.Errorf("Decls[1] = %T, want *ast.Ellipsis", f.Decls[1])
		}
	})

	t.Run("UnderscorePatternCollapses", func(t *testing.T) {
		// [_]: _ also becomes a trailing ...
		patternField := &ast.Field{
			Label: &ast.ListLit{Elts: []ast.Expr{ident("_")}},
			Value: ident("_"),
		}
		f := &ast.File{Decls: []ast.Decl{
			patternField,
		}}
		cfg.Annotate(f)
		if len(f.Decls) != 1 {
			t.Fatalf("len(Decls) = %d, want 1", len(f.Decls))
		}
		if _, ok := f.Decls[0].(*ast.Ellipsis); !ok {
			t.Errorf("Decls[0] = %T, want *ast.Ellipsis", f.Decls[0])
		}
	})

	t.Run("RemovedCommentsAllTransfer", func(t *testing.T) {
		first := &ast.Ellipsis{}
		ast.AddComment(first, docComment("first"))
		last := &ast.Ellipsis{}
		ast.AddComment(last, docComment("last"))
		f := &ast.File{Decls: []ast.Decl{
			first,
			field("a", "int"),
			last,
		}}
		cfg.Annotate(f)
		if len(f.Decls) != 2 {
			t.Fatalf("len(Decls) = %d, want 2", len(f.Decls))
		}
		trailing, ok := f.Decls[1].(*ast.Ellipsis)
		if !ok {
			t.Fatalf("Decls[1] = %T, want *ast.Ellipsis", f.Decls[1])
		}
		// We expect the new trailing Ellipsis to carry the comments of
		// every removed marker, in source order. mergeDocComments then
		// folds the two transferred doc groups into one, joined by an
		// empty `//` line.
		var got []string
		for _, cg := range ast.Comments(trailing) {
			for _, c := range cg.List {
				got = append(got, c.Text)
			}
		}
		want := []string{"// first", "//", "// last"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("trailing comments = %v, want %v", got, want)
		}
	})

	t.Run("NoEllipsisIsNoOp", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{
			field("a", "int"),
			field("b", "int"),
		}}
		if cfg.Annotate(f) {
			t.Error("expected Annotate to report no change on body without ellipses")
		}
		if len(f.Decls) != 2 {
			t.Errorf("len(Decls) = %d, want 2", len(f.Decls))
		}
	})

	t.Run("ZeroConfigIsNoOp", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{
			&ast.Ellipsis{},
			field("a", "int"),
		}}
		if (style.Config{}).Annotate(f) {
			t.Error("zero Config returned true")
		}
		if _, ok := f.Decls[0].(*ast.Ellipsis); !ok {
			t.Error("Ellipsis was moved despite zero Config")
		}
	})

	t.Run("AppliesToNestedStruct", func(t *testing.T) {
		// Ellipsis inside a nested struct should also be deferred.
		inner := bracedStruct(
			&ast.Ellipsis{},
			field("a", "int"),
		)
		f := &ast.File{Decls: []ast.Decl{
			&ast.Field{Label: ident("outer"), Value: inner},
		}}
		cfg.Annotate(f)
		if len(inner.Elts) != 2 {
			t.Fatalf("len(inner.Elts) = %d, want 2", len(inner.Elts))
		}
		if _, ok := inner.Elts[1].(*ast.Ellipsis); !ok {
			t.Errorf("inner trailing = %T, want *ast.Ellipsis", inner.Elts[1])
		}
	})
}

// TestAnnotateInlineStructs exercises the [style.Config.InlineStructs]
// flag: we check that a single-field StructLit value collapses to the
// chain shape when that is safe, and that we hold off whenever attrs
// or comments would otherwise be lost or misplaced.
func TestAnnotateInlineStructs(t *testing.T) {
	cfg := style.Config{InlineStructs: true}

	t.Run("StripsSingleFieldStruct", func(t *testing.T) {
		// outer: {inner: 1} -> outer: inner: 1
		// We invalidate the struct's Lbrace; pretty's braceless chain
		// logic then renders the chain shape.
		inner := field("inner", "int")
		s := bracedStruct(inner)
		outer := &ast.Field{Label: ident("outer"), Value: s}
		f := &ast.File{Decls: []ast.Decl{outer}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		if s.Lbrace.IsValid() || s.Rbrace.IsValid() {
			t.Errorf("expected Lbrace/Rbrace stripped: Lbrace=%v Rbrace=%v",
				s.Lbrace, s.Rbrace)
		}
	})

	t.Run("RecursesIntoNestedStruct", func(t *testing.T) {
		// outer: {mid: {leaf: 1}} -> outer: mid: leaf: 1
		inner := bracedStruct(field("leaf", "int"))
		mid := &ast.Field{Label: ident("mid"), Value: inner}
		outerSL := bracedStruct(mid)
		outer := &ast.Field{Label: ident("outer"), Value: outerSL}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if outerSL.Lbrace.IsValid() {
			t.Error("outer struct Lbrace still valid")
		}
		if inner.Lbrace.IsValid() {
			t.Error("inner struct Lbrace still valid (recursion didn't fire)")
		}
	})

	t.Run("PreservesWhenOuterHasAttrs", func(t *testing.T) {
		// outer @attr: {inner: 1} - chain form would attach @attr
		// to the inner field; keep braces.
		s := bracedStruct(field("inner", "int"))
		outer := &ast.Field{
			Label: ident("outer"),
			Value: s,
			Attrs: []*ast.Attribute{{Text: "@attr()"}},
		}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if !s.Lbrace.IsValid() {
			t.Error("outer struct's Lbrace stripped despite outer.Attrs")
		}
	})

	t.Run("PreservesWhenInnerHasAttrs", func(t *testing.T) {
		// outer: {inner: 1 @attr} - chain form would re-bind @attr
		// from inner to the leaf; keep braces.
		inner := &ast.Field{
			Label: ident("inner"),
			Value: ident("int"),
			Attrs: []*ast.Attribute{{Text: "@attr()"}},
		}
		s := bracedStruct(inner)
		outer := &ast.Field{Label: ident("outer"), Value: s}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if !s.Lbrace.IsValid() {
			t.Error("Lbrace stripped despite inner.Attrs")
		}
	})

	t.Run("PreservesWhenInnerHasComments", func(t *testing.T) {
		// outer: {// doc\n inner: 1} - chain form would squeeze the
		// inner doc between outer_key: and inner_key:.
		inner := withDoc(field("inner", "int"), "inner doc")
		s := bracedStruct(inner)
		outer := &ast.Field{Label: ident("outer"), Value: s}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if !s.Lbrace.IsValid() {
			t.Error("Lbrace stripped despite inner doc comment")
		}
	})

	t.Run("PreservesWhenAuthoredBracesAndOuterDoc", func(t *testing.T) {
		// // outer doc
		// outer: {inner: 1}
		// The user wrote both the doc and the braces, so we keep the
		// braces. The only path to a non-NoPos Lbrace with a real file
		// position is via parser.ParseFile, so that is what we use
		// here.
		f, err := parser.ParseFile("x.cue", "// outer doc\nouter: {inner: 1}\n", parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		outer := f.Decls[0].(*ast.Field)
		s := outer.Value.(*ast.StructLit)
		cfg.Annotate(f)
		if !s.Lbrace.IsValid() {
			t.Error("authored Lbrace stripped under outer doc")
		}
	})

	t.Run("RefusesMultiElementStruct", func(t *testing.T) {
		// outer: {a: 1, b: 2} - two-element body cannot become a
		// chain.
		s := bracedStruct(field("a", "int"), field("b", "int"))
		outer := &ast.Field{Label: ident("outer"), Value: s}
		f := &ast.File{Decls: []ast.Decl{outer}}
		cfg.Annotate(f)
		if !s.Lbrace.IsValid() {
			t.Error("Lbrace stripped on multi-element struct")
		}
	})

	t.Run("ZeroConfigIsNoOp", func(t *testing.T) {
		s := bracedStruct(field("inner", "int"))
		outer := &ast.Field{Label: ident("outer"), Value: s}
		f := &ast.File{Decls: []ast.Decl{outer}}
		if (style.Config{}).Annotate(f) {
			t.Error("zero Config returned true")
		}
		if !s.Lbrace.IsValid() {
			t.Error("Lbrace stripped under zero Config")
		}
	})
}

// TestAnnotateLabels exercises the [style.Config.Labels] flag: we
// check that a quoted string label unquotes to an identifier only when
// no in-scope reference would then bind to a different value.
func TestAnnotateLabels(t *testing.T) {
	cfg := style.Config{Labels: true}

	t.Run("SafeBasicLitBecomesIdent", func(t *testing.T) {
		// "foo": 1 - no other reference in scope - becomes foo: 1.
		f := &ast.File{Decls: []ast.Decl{
			stringLabelField("foo", "int"),
		}}
		if !cfg.Annotate(f) {
			t.Fatal("expected Annotate to report a change")
		}
		fld := f.Decls[0].(*ast.Field)
		if _, ok := fld.Label.(*ast.Ident); !ok {
			t.Errorf("label = %T, want *ast.Ident", fld.Label)
		}
	})

	t.Run("StringLabelStaysWhenReferenced", func(t *testing.T) {
		// foo: "foo": 1 - a field whose value references the label
		// inhibits unquoting, because the bare ident `foo` would
		// resolve to that field rather than the string-keyed one.
		f := &ast.File{Decls: []ast.Decl{
			&ast.Field{Label: ident("foo"), Value: ident("foo")},
			stringLabelField("foo", "int"),
		}}
		cfg.Annotate(f)
		fld := f.Decls[1].(*ast.Field)
		if _, ok := fld.Label.(*ast.BasicLit); !ok {
			t.Errorf("label = %T, want *ast.BasicLit (preserved)", fld.Label)
		}
	})

	t.Run("InvalidIdentifierStays", func(t *testing.T) {
		// "foo bar": 1 - the string is not a valid identifier
		// (contains space), so it cannot be unquoted.
		f := &ast.File{Decls: []ast.Decl{
			stringLabelField("foo bar", "int"),
		}}
		cfg.Annotate(f)
		fld := f.Decls[0].(*ast.Field)
		if _, ok := fld.Label.(*ast.BasicLit); !ok {
			t.Errorf("label = %T, want *ast.BasicLit (invalid ident)", fld.Label)
		}
	})

	t.Run("ScopeNesting", func(t *testing.T) {
		// outer struct { "foo": 1 } - inner reference to foo in a
		// nested scope still inhibits unquoting at the outer level.
		f := &ast.File{Decls: []ast.Decl{
			stringLabelField("foo", "int"),
			&ast.Field{Label: ident("inner"), Value: bracedStruct(
				&ast.Field{Label: ident("x"), Value: ident("foo")},
			)},
		}}
		cfg.Annotate(f)
		// We expect foo at file scope to stay quoted: the inner
		// reference to `foo` would otherwise bind to it.
		fld := f.Decls[0].(*ast.Field)
		if _, ok := fld.Label.(*ast.BasicLit); !ok {
			t.Errorf("outer label = %T, want *ast.BasicLit", fld.Label)
		}
	})

	t.Run("ZeroConfigIsNoOp", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{stringLabelField("foo", "int")}}
		if (style.Config{}).Annotate(f) {
			t.Error("zero Config should be a no-op, returned true")
		}
		fld := f.Decls[0].(*ast.Field)
		if _, ok := fld.Label.(*ast.BasicLit); !ok {
			t.Errorf("label changed under zero Config: %T", fld.Label)
		}
	})
}
