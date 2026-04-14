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

package pretty_test

import (
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/pretty"
	"cuelang.org/go/internal/pretty/style"
)

// TestTxtar runs the txtar-based pretty-printer tests in testdata/. Each
// file holds an optional "config" section (width/indent settings), an
// "in.cue" input, and the expected outputs "out/relpos" (RelPos honoured)
// and "out/norelpos" (RelPos stripped). We check both outputs for
// idempotency.
func TestTxtar(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.txtar"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no txtar files found in testdata/")
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".txtar")
		t.Run(name, func(t *testing.T) {
			ar, err := txtar.ParseFile(file)
			if err != nil {
				t.Fatal(err)
			}

			// Parse config.
			width := 80
			indent := "\t"
			annotate := false
			if sec := findSection(t, ar, "config"); sec != nil {
				for line := range strings.SplitSeq(string(sec.Data), "\n") {
					key, val, ok := strings.Cut(line, ":")
					if !ok {
						continue
					}
					key = strings.TrimSpace(key)
					val = strings.TrimSpace(val)
					switch key {
					case "width":
						n, err := strconv.Atoi(val)
						if err != nil {
							t.Fatalf("bad width: %v", err)
						}
						width = n
					case "indent":
						indent, err = strconv.Unquote(val)
						if err != nil {
							t.Fatalf("bad indent: %v", err)
						}
					case "annotate":
						annotate, err = strconv.ParseBool(val)
						if err != nil {
							t.Fatalf("bad annotate: %v", err)
						}
					}
				}
			}

			// Get sections.
			input := trimTrailingNewline(sectionData(t, ar, "in.cue"))
			wantRelPos := trimTrailingNewline(sectionData(t, ar, "out/relpos"))
			wantNoRelPos := trimTrailingNewline(sectionData(t, ar, "out/norelpos"))

			cfg := &pretty.Config{Width: width, Indent: indent}
			// Annotate is opt-in. Tests that exercise rules owned by
			// the style package (e.g. A4's blank-line-before-doc
			// upgrades) set annotate: true in their config, so we run
			// the pre-pass - mirroring how cue/format invokes pretty.
			// Tests that target the pretty layer in isolation leave it
			// off, so we exercise the renderer against the raw AST.
			styleCfg := style.Config{RelPos: annotate}

			// Test with RelPos from parser.
			syntax, err := parser.ParseFile("in.cue", input, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			styleCfg.Annotate(syntax)

			gotRelPos := trimTrailingNewline(string(mustFormat(t, cfg, syntax)))
			if gotRelPos != wantRelPos {
				t.Errorf("with RelPos:\ngot:\n%s\nwant:\n%s", gotRelPos, wantRelPos)
			} else {
				checkIdempotent(t, gotRelPos, cfg)
			}
			// We run the preservation check on whatever the printer
			// produced, even if it diverges from the golden. Losing a
			// comment is a bug regardless of whether the layout matched.
			checkCommentsPreserved(t, "with RelPos", input, gotRelPos)

			// Test without RelPos.
			stripRelPos(syntax)
			styleCfg.Annotate(syntax)
			gotNoRelPos := trimTrailingNewline(string(mustFormat(t, cfg, syntax)))
			if gotNoRelPos != wantNoRelPos {
				t.Errorf("without RelPos:\ngot:\n%s\nwant:\n%s", gotNoRelPos, wantNoRelPos)
			} else {
				checkIdempotent(t, gotNoRelPos, cfg)
			}
			checkCommentsPreserved(t, "without RelPos", input, gotNoRelPos)
		})
	}
}

// sectionData returns the data for the named section. We fatal if it is
// not present.
func sectionData(t *testing.T, ar *txtar.Archive, name string) string {
	t.Helper()
	sec := findSection(t, ar, name)
	if sec == nil {
		t.Fatalf("missing section %q", name)
	}
	return string(sec.Data)
}

// findSection returns the first archive file with the given name, or
// nil.
func findSection(t *testing.T, ar *txtar.Archive, name string) *txtar.File {
	t.Helper()
	for i := range ar.Files {
		if ar.Files[i].Name == name {
			return &ar.Files[i]
		}
	}
	return nil
}

func trimTrailingNewline(s string) string {
	return strings.TrimRight(s, "\n")
}

// mustFormat formats n with cfg. We fail the test on a formatting error.
func mustFormat(tb testing.TB, cfg *pretty.Config, n ast.Node) []byte {
	tb.Helper()
	b, err := cfg.Node(n)
	if err != nil {
		tb.Fatalf("format error: %v", err)
	}
	return b
}

// checkIdempotent checks that, when we re-parse formatted and print it
// again with cfg, we get formatted back unchanged.
func checkIdempotent(t *testing.T, formatted string, cfg *pretty.Config) {
	t.Helper()
	syntax, err := parser.ParseFile("formatted.cue", formatted, parser.ParseComments)
	if err != nil {
		t.Errorf("idempotency: re-parse failed: %v\nformatted output was:\n%s", err, formatted)
		return
	}
	reformatted := trimTrailingNewline(string(mustFormat(t, cfg, syntax)))
	if reformatted != formatted {
		t.Errorf("idempotency failure:\nfirst:  %q\nsecond: %q", formatted, reformatted)
	}
}

// checkCommentsPreserved checks that every comment in input survives
// into output, catching comments the printer silently drops. We match by
// substring, i.e. each comment's exact Text must appear somewhere in
// output. Note that this catches total loss but not a comment that has
// moved across scope boundaries.
func checkCommentsPreserved(t *testing.T, label, input, output string) {
	t.Helper()
	syntax, err := parser.ParseFile("in.cue", input, parser.ParseComments)
	if err != nil {
		t.Fatalf("%s: reparse failed: %v", label, err)
	}
	var missing []string
	ast.Walk(syntax, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		for _, cg := range ast.Comments(n) {
			for _, c := range cg.List {
				if !strings.Contains(output, c.Text) {
					missing = append(missing, c.Text)
				}
			}
		}
		return true
	}, nil)
	if len(missing) > 0 {
		t.Errorf("%s: %d comment(s) lost in output:\n  %s\noutput was:\n%s",
			label, len(missing), strings.Join(missing, "\n  "), output)
	}
}

// stripRelPos strips all parser-derived layout intent from the AST, so
// that we simulate a programmatically-built tree: we reset every
// token.Pos's RelPos to NoRelPos with its comma and "scanned" bits
// cleared, and reset every CommentGroup's Line flag to false. We clear
// cg.Line because it is a redundant projection of Slash.RelPos = Blank;
// we clear the comma/scanned bits to keep [listOmitsCommas]'s
// source-preserving comma style faithful (i.e. lists keep their commas).
// Position and Doc are structural/semantic, so we leave them intact.
func stripRelPos(n ast.Node) {
	posType := reflect.TypeFor[token.Pos]()
	ast.Walk(n, func(node ast.Node) bool {
		if cg, ok := node.(*ast.CommentGroup); ok {
			cg.Line = false
		}
		v := reflect.ValueOf(node)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return true
		}
		for i := range v.NumField() {
			f := v.Field(i)
			if f.Type() == posType && f.CanSet() {
				pos := f.Interface().(token.Pos)
				newPos := pos.WithRel(token.NoRelPos).WithComma(false).WithScanned(false)
				if newPos != pos {
					f.Set(reflect.ValueOf(newPos))
				}
			}
		}
		return true
	}, nil)
}

// TestProgrammaticAST exercises behaviours we cannot reach through parsed
// CUE source - e.g. specific RelPos values, or comments in unusual
// positions - by building the ASTs programmatically. Every case also
// checks idempotency, so we know the output is valid CUE.
func TestProgrammaticAST(t *testing.T) {
	// twoFieldFile builds an ast.File with two fields, where the
	// second field's label has the given RelPos.
	twoFieldFile := func(rel token.RelPos) *ast.File {
		return &ast.File{
			Decls: []ast.Decl{
				&ast.Field{
					Label:    &ast.Ident{Name: "a"},
					TokenPos: token.Blank.Pos(),
					Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
				},
				&ast.Field{
					Label:    &ast.Ident{NamePos: rel.Pos(), Name: "b"},
					TokenPos: token.Blank.Pos(),
					Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
				},
			},
		}
	}

	// twoFieldStruct builds an ast.File containing a braced struct
	// with two fields, where the second field's label has the given
	// RelPos.
	twoFieldStruct := func(rel token.RelPos) *ast.File {
		s := &ast.StructLit{
			Lbrace: token.Blank.Pos(),
			Rbrace: token.Blank.Pos(),
			Elts: []ast.Decl{
				&ast.Field{
					Label:    &ast.Ident{Name: "a"},
					TokenPos: token.Blank.Pos(),
					Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
				},
				&ast.Field{
					Label:    &ast.Ident{NamePos: rel.Pos(), Name: "b"},
					TokenPos: token.Blank.Pos(),
					Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
				},
			},
		}
		return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: s}}}
	}

	// postfixWithComment builds a field whose value is a PostfixExpr
	// with a trailing comment on the operand.
	postfixWithComment := func() *ast.File {
		f := &ast.File{Decls: []ast.Decl{
			&ast.Field{
				Label:    &ast.Ident{Name: "x"},
				TokenPos: token.Blank.Pos(),
				Value: &ast.PostfixExpr{
					X:  &ast.Ident{Name: "value"},
					Op: token.OPTION,
				},
			},
		}}
		ast.AddComment(f.Decls[0].(*ast.Field).Value.(*ast.PostfixExpr).X,
			&ast.CommentGroup{
				Line:     true,
				Position: 3,
				List:     []*ast.Comment{{Text: "// trailing"}},
			})
		return f
	}

	tests := []struct {
		name string
		node ast.Node
		cfg  *pretty.Config
		want string
	}{
		{
			name: "relpos/newline",
			node: twoFieldFile(token.Newline),
			cfg:  &pretty.Config{Width: 80},
			want: `
a: 1
b: 2`[1:],
		},
		{
			name: "relpos/new_section",
			node: twoFieldFile(token.NewSection),
			cfg:  &pretty.Config{Width: 80},
			want: `
a: 1

b: 2`[1:],
		},
		{
			name: "relpos/no_relpos",
			node: func() *ast.File {
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{
						Label: &ast.Ident{Name: "a"},
						Value: &ast.BasicLit{Kind: token.INT, Value: "1"},
					},
					&ast.Field{
						Label: &ast.Ident{Name: "b"},
						Value: &ast.BasicLit{Kind: token.INT, Value: "2"},
					},
				}}
			}(),
			cfg:  &pretty.Config{Width: 80},
			want: "a: 1, b: 2",
		},
		{
			name: "relpos/nospace",
			node: twoFieldFile(token.NoSpace),
			cfg:  &pretty.Config{Width: 80},
			want: "a: 1, b: 2",
		},
		{
			name: "relpos/blank_in_struct",
			node: twoFieldStruct(token.Blank),
			cfg:  &pretty.Config{Width: 80},
			want: "{a: 1, b: 2}",
		},
		{
			name: "relpos/newline_in_struct_wide",
			node: twoFieldStruct(token.Newline),
			cfg:  &pretty.Config{Width: 80, Indent: "\t"},
			want: `
{a: 1
	b: 2}`[1:],
		},
		{
			name: "relpos/newline_in_struct_narrow",
			node: twoFieldStruct(token.Newline),
			cfg:  &pretty.Config{Width: 10, Indent: "\t"},
			want: `
{a: 1
	b: 2}`[1:],
		},
		{
			name: "relpos/new_section_in_struct",
			node: twoFieldStruct(token.NewSection),
			cfg:  &pretty.Config{Width: 80, Indent: "\t"},
			want: `
{a: 1

	b: 2}`[1:],
		},
		{
			name: "postfix_trailing_comment",
			node: postfixWithComment(),
			cfg:  &pretty.Config{Width: 80},
			want: "x: value? // trailing",
		},
		{
			// We exercise node()'s ast.Decl dispatch branch by passing a
			// bare Field decl directly, rather than wrapped in a File.
			name: "node_as_decl",
			node: &ast.Field{
				Label: &ast.Ident{Name: "a"},
				Value: &ast.BasicLit{Kind: token.INT, Value: "1"},
			},
			cfg:  &pretty.Config{Width: 80},
			want: "a: 1",
		},
		{
			// Per-subtree wrap: an authored StructLit (RelPos on its
			// inner fields) sits inside an otherwise programmatic
			// AST. The smallest wrap-eligible ancestor is the StructLit
			// itself, so only it gets the [infiniteWidth] wrap; the
			// surrounding File and sibling Fields go through
			// width-driven layout. The HardLines inside the authored
			// StructLit propagate up and break the outer Group, so each
			// top-level decl lands on its own line - but the inter-decl
			// separators stay soft (asInfiniteWidth does not convert them
			// to ", "), which proves the outer is not in authored-mode.
			name: "wrap_per_subtree_authored_struct_in_programmatic",
			node: func() *ast.File {
				inner := &ast.StructLit{
					Lbrace: token.Blank.Pos(),
					Rbrace: token.Newline.Pos(),
					Elts: []ast.Decl{
						&ast.Field{
							Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "a"},
							TokenPos: token.Blank.Pos(),
							Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
						},
						&ast.Field{
							Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "b"},
							TokenPos: token.Blank.Pos(),
							Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
						},
					},
				}
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{
						Label: &ast.Ident{Name: "before"},
						Value: &ast.BasicLit{Kind: token.INT, Value: "1"},
					},
					&ast.Field{
						Label: &ast.Ident{Name: "parsed"},
						Value: inner,
					},
					&ast.Field{
						Label: &ast.Ident{Name: "after"},
						Value: &ast.BasicLit{Kind: token.INT, Value: "3"},
					},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
before: 1
parsed: {
	a: 1
	b: 2
}
after: 3`[1:],
		},
		{
			// A synthesised StructLit (Lbrace.IsValid() == false) holding
			// fields with authored RelPos hints. [wrapEligibility]
			// reports the StructLit as eligible but not authored, so
			// [analyse] bubbles the inner RelPos past it to the
			// enclosing File and [maybeGroup] gives the StructLit a
			// [finiteWidth] wrap. That keeps the synthesised brace
			// opener/closer breaks soft, so they honour the body's
			// HardLines and put the closing brace on its own line.
			//
			// Were [wrapEligibility] to report authored == true for the
			// synthesised StructLit, [maybeGroup] would wrap it in
			// [infiniteWidth] and [asInfiniteWidth] would bake the
			// synthesised brace breaks to their flat alternative -
			// smashing the closer onto the last field's line.
			name: "wrap_synthesised_struct_keeps_closer_on_own_line",
			node: func() *ast.File {
				// We deliberately leave Lbrace/Rbrace as NoPos. The
				// parser never produces this shape; only programmatic AST
				// construction does.
				inner := &ast.StructLit{
					Elts: []ast.Decl{
						&ast.Field{
							Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "a"},
							TokenPos: token.Blank.Pos(),
							Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
						},
						&ast.Field{
							Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "b"},
							TokenPos: token.Blank.Pos(),
							Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
						},
					},
				}
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{
						Label: &ast.Ident{Name: "x"},
						Value: inner,
					},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
x: {
	a: 1
	b: 2
}`[1:],
		},
		{
			// Compact rendering: a programmatic ImportDecl with no
			// RelPos hints and unbounded width collapses to a single
			// `import (...)`. importDecl reads all break decisions
			// (opener, inter-spec, closer) from RelPos and wraps the
			// body in a Group, so with everything zero-valued the flat
			// branch wins. It is style.Config.Annotate's B3 that supplies
			// the Newline RelPos in the canonical multi-line shape.
			name: "compact_import_decl_no_relpos",
			node: &ast.ImportDecl{
				Lparen: token.Blank.Pos(),
				Rparen: token.Blank.Pos(),
				Specs: []*ast.ImportSpec{
					{Path: &ast.BasicLit{Kind: token.STRING, Value: `"strings"`}},
					{Path: &ast.BasicLit{Kind: token.STRING, Value: `"list"`}},
				},
			},
			cfg:  &pretty.Config{Width: math.MaxInt},
			want: `import ("strings", "list")`,
		},
		{
			// We lift same-line (Line=true) trailing comments attached
			// to a leaf value up to the field's trailing-cell column, so
			// they column-align with sibling fields' trailing comments.
			// Note that [internal/core/export] attaches error annotations
			// to the value (e.g. an *ast.BottomLit with Position=2,
			// Line=true) - structurally different from the parser, which
			// attaches to the field at Position>=3 - yet both should
			// render in the same aligned trailing column.
			//
			// This case also exercises co-existence with a doc comment
			// on the same leaf value: the doc comment must stay attached
			// to the value (and so renders before it), while we lift the
			// same-line comment.
			name: "field_value_line_comment_lifted_to_trailing_cell",
			node: func() *ast.File {
				// Field 1: short value (Ident), bare.
				short := &ast.Field{
					Label: &ast.Ident{Name: "ok"},
					Value: &ast.Ident{Name: "_"},
				}
				// Field 2: value carries BOTH a doc comment (Position=0,
				// Line=false) AND a same-line trailing comment
				// (Position=2, Line=true). Both must survive.
				val := &ast.BasicLit{Kind: token.INT, Value: "42"}
				ast.AddComment(val, &ast.CommentGroup{
					Position: 0,
					List:     []*ast.Comment{{Text: "// docs for 42"}},
				})
				ast.AddComment(val, &ast.CommentGroup{
					Position: 2,
					Line:     true,
					List:     []*ast.Comment{{Text: "// trailing"}},
				})
				doc := &ast.Field{
					Label: &ast.Ident{Name: "answer"},
					Value: val,
				}
				ast.SetPos(doc.Label, token.NoPos.WithRel(token.Newline))
				// Field 3: a BottomLit value with the same kind of
				// same-line comment that the exporter attaches to
				// error sentinels.
				bottom := &ast.BottomLit{}
				ast.AddComment(bottom, &ast.CommentGroup{
					Position: 2,
					Line:     true,
					List:     []*ast.Comment{{Text: "// boom"}},
				})
				err := &ast.Field{
					Label: &ast.Ident{Name: "err"},
					Value: bottom,
				}
				ast.SetPos(err.Label, token.NoPos.WithRel(token.Newline))
				return &ast.File{Decls: []ast.Decl{short, doc, err}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
ok: _
answer:
	// docs for 42
	42 // trailing
err: _|_ // boom`[1:],
		},
		{
			// When the field itself carries a same-line trailing comment
			// AND its leaf value also carries one, both must survive: we
			// join them into the same trailing-cell.
			name: "field_and_value_line_comments_both_survive",
			node: func() *ast.File {
				val := &ast.BasicLit{Kind: token.INT, Value: "1"}
				ast.AddComment(val, &ast.CommentGroup{
					Position: 2,
					Line:     true,
					List:     []*ast.Comment{{Text: "// on-value"}},
				})
				f := &ast.Field{
					Label: &ast.Ident{Name: "x"},
					Value: val,
				}
				ast.AddComment(f, &ast.CommentGroup{
					Position: 4, // PosTrailingMin or higher
					Line:     true,
					List:     []*ast.Comment{{Text: "// on-field"}},
				})
				return &ast.File{Decls: []ast.Decl{f}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
x: 1 // on-field
     // on-value`[1:],
		},
		{
			// We exercise node()'s ast.Expr dispatch branch by passing a
			// bare expression directly.
			name: "node_as_expr",
			node: &ast.BasicLit{Kind: token.INT, Value: "42"},
			cfg:  &pretty.Config{Width: 80},
			want: "42",
		},
		{
			// Per-subtree wrap, the inverse of the previous test: an
			// authored struct (RelPos on its existing fields) into which
			// we have spliced a no-RelPos field. We wrap the surrounding
			// StructLit in [infiniteWidth] so its authored children are
			// preserved as written, but the spliced field - which has no
			// RelPos descendants of its own - gets a [finiteWidth]
			// boundary that escapes the infinite-width and asInfiniteWidth
			// layers for its content. Width-driven Wadler-Lindig therefore
			// applies inside the spliced field: its wide chain breaks, and
			// render-time row segmentation isolates the broken row so the
			// surrounding fields no longer share alignment with it.
			//
			// The "programmatic" struct sitting alongside has the same
			// content but no RelPos anywhere - its StructLit is its own
			// programmatic-mode subtree. We get identical output: option
			// (B) treats programmatic content the same way regardless of
			// whether it is nested inside an [infiniteWidth] wrap or
			// stands alone.
			name: "wrap_per_subtree_programmatic_field_in_authored_struct",
			node: func() *ast.File {
				// progFile lets us synthesise valid Pos values that
				// carry no RelPos - which we need so that a StructLit's
				// Lbrace does not itself trip flagShouldWrap. (A
				// RelPos.Pos() always reports HasRelPos=true.)
				progFile := token.NewFile("prog", -1, 1024)
				progOffset := 1
				progPos := func() token.Pos {
					progOffset++
					return progFile.Pos(progOffset, token.NoRelPos)
				}
				chain := func() ast.Expr {
					return &ast.BinaryExpr{
						X:  &ast.BasicLit{Kind: token.STRING, Value: `"first"`},
						Op: token.OR,
						Y: &ast.BinaryExpr{
							X:  &ast.BasicLit{Kind: token.STRING, Value: `"second"`},
							Op: token.OR,
							Y:  &ast.BasicLit{Kind: token.STRING, Value: `"third"`},
						},
					}
				}
				relposField := func(name string, val ast.Expr) *ast.Field {
					return &ast.Field{
						Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: name},
						TokenPos: token.Blank.Pos(),
						Value:    val,
					}
				}
				progField := func(name string, val ast.Expr) *ast.Field {
					return &ast.Field{
						Label: &ast.Ident{Name: name},
						Value: val,
					}
				}
				parsedStruct := &ast.StructLit{
					Lbrace: token.Blank.Pos(),
					Rbrace: token.Newline.Pos(),
					Elts: []ast.Decl{
						relposField("a", &ast.BasicLit{Kind: token.INT, Value: "1"}),
						progField("long", chain()), // spliced in, no RelPos
						relposField("z", &ast.BasicLit{Kind: token.INT, Value: "9"}),
					},
				}
				progStruct := &ast.StructLit{
					Lbrace: progPos(),
					Rbrace: progPos(),
					Elts: []ast.Decl{
						progField("a", &ast.BasicLit{Kind: token.INT, Value: "1"}),
						progField("long", chain()),
						progField("z", &ast.BasicLit{Kind: token.INT, Value: "9"}),
					},
				}
				return &ast.File{Decls: []ast.Decl{
					progField("parsed", parsedStruct),
					progField("programmatic", progStruct),
				}}
			}(),
			cfg: &pretty.Config{Width: 30, Indent: "\t"},
			want: `
parsed: {
	a: 1
	long: "first" |
		"second" |
		"third"
	z: 9
}
programmatic: {
	a: 1
	long: "first" |
		"second" |
		"third"
	z: 9
}`[1:],
		},
		{
			// A trailing comment on a decl, in an [infiniteWidth] wrap,
			// must not be followed on the same line by the next decl.
			//
			// The blank line in the output comes from the existing rule
			// that puts a section break between a non-field decl (let)
			// and a doc-commented field. We extend that rule to fire when
			// prev's trailing `//` is going to migrate into the next
			// decl's doc on reparse, which is what keeps the parse/format
			// cycle idempotent.
			name: "comment_after_decl_does_not_swallow_next",
			node: func() *ast.File {
				let := &ast.LetClause{
					Ident: &ast.Ident{Name: "X"},
					Expr:  &ast.BasicLit{Kind: token.INT, Value: "1"},
				}
				ast.AddComment(let, &ast.CommentGroup{
					Position: 3,
					List: []*ast.Comment{
						{Slash: token.NoPos.WithRel(token.Newline), Text: "// c"},
					},
				})
				return &ast.File{Decls: []ast.Decl{
					let,
					&ast.Field{
						Label: &ast.Ident{Name: "bar"},
						Value: &ast.BasicLit{Kind: token.INT, Value: "2"},
					},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
let X = 1
// c

bar: 2`[1:],
		},
		{
			// A chain with a trailing comment whose Slash RelPos != Blank
			// (and cg.Line=false): we must not lose the comment.
			name: "chain_own_line_trailing_comment_preserved",
			node: func() *ast.File {
				bin := &ast.BinaryExpr{
					X:  &ast.Ident{Name: "a"},
					Op: token.OR,
					Y:  &ast.Ident{Name: "b"},
				}
				ast.AddComment(bin, &ast.CommentGroup{
					Position: 2, // PosSuffix: between op and right
					List:     []*ast.Comment{{Text: "// trailing"}},
				})
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{
						Label: &ast.Ident{Name: "x"},
						Value: bin,
					},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
x: a | // trailing
	b`[1:],
		},
		{
			// A programmatic list with a Scanned Lbrack but elements that
			// carry neither comma bits nor RelPos must NOT be treated as
			// comma-free: its elements need commas. This is the shape the
			// jsonschema encoder produces - it builds the list via
			// ast.NewList but copies Scanned positions from the source, so
			// the Lbrack reads as scanner-produced while the elements stay
			// position-less. The parser can never produce this (a
			// comma-less element must be preceded by a newline, i.e. carry
			// a Newline RelPos), so [listOmitsCommas] only treats a missing
			// comma as comma-free when the element is genuinely on its own
			// line; here it keeps the commas. Without that guard we emitted
			// `[number {...} ...]` - invalid CUE. We build the AST directly
			// since the parser can't produce it.
			name: "programmatic_scanned_list_keeps_commas",
			node: func() *ast.File {
				scanned := token.NoPos.WithScanned(true)
				innerStruct := &ast.StructLit{
					// Lbrace/Rbrace left NoPos, matching the encoder: the
					// struct is not authored, so its inner hard break
					// bubbles up to make the list authored.
					Elts: []ast.Decl{
						&ast.Field{
							Label:    &ast.Ident{Name: "suffixes"},
							TokenPos: token.Blank.Pos(),
							Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
						},
						&ast.Field{
							Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "more"},
							TokenPos: token.Blank.Pos(),
							Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
						},
					},
				}
				list := &ast.ListLit{
					Lbrack: scanned,
					Rbrack: scanned,
					Elts: []ast.Expr{
						&ast.Ident{Name: "number"},
						innerStruct,
						&ast.Ellipsis{},
					},
				}
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{Label: &ast.Ident{Name: "x"}, Value: list},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
x: [number, {
	suffixes: 1
	more:     2
}, ...]`[1:],
		},
		{
			// A genuine comma-free list (the v0.17.0+ syntax, only
			// reachable when an element starts on its own line) that mixes
			// styles: `a, b` share a line with an explicit comma, while `c`
			// sits on its own line with no comma. The list is authored (the
			// Newline RelPos on c), so it renders under [infiniteWidth],
			// where the group goes broken. The same-line `a, b` comma must
			// survive: in the comma-free style the comma travels with the
			// row separator, so a broken group cannot suppress it while the
			// separator still collapses `a` and `b` onto one line. The
			// parser won't accept comma-free lists without a v0.17.0+
			// version, so we build the AST directly.
			name: "comma_free_mixed_keeps_same_line_comma",
			node: func() *ast.File {
				scanned := token.NoPos.WithScanned(true)
				list := &ast.ListLit{
					Lbrack: scanned,
					Rbrack: token.Newline.Pos(),
					Elts: []ast.Expr{
						&ast.Ident{Name: "a"},
						&ast.Ident{NamePos: token.Blank.Pos().WithComma(true), Name: "b"},
						&ast.Ident{NamePos: token.Newline.Pos(), Name: "c"},
					},
				}
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{Label: &ast.Ident{Name: "x"}, Value: list},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
x: [a, b
	c
]`[1:],
		},
		{
			// A width-driven (programmatic, RelPos-free) list nested inside
			// an authored call that itself fits flat must indent its broken
			// body relative to the call, not to the left margin. The call
			// `matchN(1, [...])` is authored (the "1" carries a Blank RelPos,
			// so the CallExpr is marked authored) and renders flat under
			// [infiniteWidth]; its argument list carries no RelPos, so it
			// renders width-driven and breaks at width 80. A flat-mode table
			// emits no newlines itself, but it must still hand its cells the
			// current indent: otherwise the list's broken body anchors at
			// indent 0 and dedents below its own `matchN(1, [` opener. The
			// call sits at field-indent 1 (inside a multi-line struct), so
			// the list body must land at indent 2. We build the AST directly
			// since a parsed list always carries RelPos (never width-driven).
			name: "width_driven_list_in_flat_authored_call_indents",
			node: func() *ast.File {
				strList := &ast.ListLit{
					Elts: []ast.Expr{
						&ast.BasicLit{Kind: token.STRING, Value: `"never"`},
						&ast.BasicLit{Kind: token.STRING, Value: `"error-handling-correctness-only"`},
						&ast.BasicLit{Kind: token.STRING, Value: `"in-try-catch"`},
						&ast.BasicLit{Kind: token.STRING, Value: `"always"`},
					},
				}
				call := &ast.CallExpr{
					Fun: &ast.Ident{Name: "matchN"},
					Args: []ast.Expr{
						&ast.BasicLit{ValuePos: token.Blank.Pos(), Kind: token.INT, Value: "1"},
						strList,
					},
				}
				inner := &ast.StructLit{
					Lbrace: token.Blank.Pos(),
					Rbrace: token.Newline.Pos(),
					Elts: []ast.Decl{
						&ast.Field{
							Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "field"},
							TokenPos: token.Blank.Pos(),
							Value:    call,
						},
					},
				}
				return &ast.File{Decls: []ast.Decl{
					&ast.Field{Label: &ast.Ident{Name: "x"}, Value: inner},
				}}
			}(),
			cfg: &pretty.Config{Width: 80, Indent: "\t"},
			want: `
x: {
	field: matchN(1, [
		"never",
		"error-handling-correctness-only",
		"in-try-catch",
		"always",
	])
}`[1:],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := trimTrailingNewline(string(mustFormat(t, tt.cfg, tt.node)))
			want := trimTrailingNewline(tt.want)
			if got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			checkIdempotent(t, got, tt.cfg)
		})
	}
}
