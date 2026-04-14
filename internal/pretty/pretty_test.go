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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/pretty"
)

func TestDocCombinators(t *testing.T) {
	listDoc := pretty.Group(pretty.Cats(
		pretty.Text("["),
		pretty.Nest(pretty.Cat(pretty.Line(""), pretty.Sep(pretty.Cats(pretty.Text(","), pretty.Line(" ")), pretty.Text("1"), pretty.Text("2")))),
		pretty.TrailingComma(),
		pretty.Line(""),
		pretty.Text("]"),
	))

	tests := []struct {
		name   string
		doc    pretty.Doc
		width  int
		indent string
		want   string
	}{
		{
			name: "nil",
			doc:  nil,
			want: "",
		},
		{
			name: "text",
			doc:  pretty.Text("hello"),
			want: "hello",
		},
		{
			name: "cat",
			doc:  pretty.Cat(pretty.Text("a"), pretty.Text("b")),
			want: "ab",
		},
		{
			name: "cats_with_nil",
			doc:  pretty.Cats(pretty.Text("a"), nil, pretty.Text("b"), nil, pretty.Text("c")),
			want: "abc",
		},
		{
			name: "sep",
			doc:  pretty.Sep(pretty.Text(", "), pretty.Text("a"), pretty.Text("b"), pretty.Text("c")),
			want: "a, b, c",
		},
		{
			name:  "group_fits",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.SoftLineSpace(), pretty.Text("b"))),
			width: 80,
			want:  "a b",
		},
		{
			name:  "group_breaks",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.SoftLineSpace(), pretty.Text("b"))),
			width: 2,
			want: `
a
b`[1:],
		},
		{
			name:  "nest_in_broken_group",
			doc:   pretty.Group(pretty.Cats(pretty.Text("{"), pretty.Nest(pretty.Cat(pretty.SoftLineSpace(), pretty.Text("x"))), pretty.SoftLineSpace(), pretty.Text("}"))),
			width: 3,
			want: `
{
	x
}`[1:],
		},
		{
			name: "hardline_forces_break",
			doc:  pretty.Group(pretty.Cats(pretty.Text("a"), pretty.HardLine(), pretty.Text("b"))),
			want: `
a
b`[1:],
		},
		{
			name:  "ifbreak_flat",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.IfBreak(pretty.Text("!"), pretty.Text("?")), pretty.Text("b"))),
			width: 80,
			want:  "a?b",
		},
		{
			name:  "ifbreak_broken",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.IfBreak(pretty.Text("!"), pretty.Text("?")), pretty.SoftLineSpace(), pretty.Text("b"))),
			width: 2,
			want: `
a!
b`[1:],
		},
		{
			name: "blank_line",
			doc:  pretty.Cats(pretty.Text("a"), pretty.BlankLine(), pretty.Text("b")),
			want: `
a

b`[1:],
		},
		{
			name:   "space_indent",
			doc:    pretty.Group(pretty.Cats(pretty.Text("{"), pretty.Nest(pretty.Cat(pretty.SoftLineSpace(), pretty.Text("x"))), pretty.SoftLineSpace(), pretty.Text("}"))),
			width:  3,
			indent: "    ",
			want: `
{
    x
}`[1:],
		},
		{
			name: "table_alignment",
			doc: pretty.Table([]pretty.Row{
				{Cells: []pretty.Doc{pretty.Text("foo:"), pretty.Text("1")}},
				{Cells: []pretty.Doc{pretty.Text("barbaz:"), pretty.Text("2")}, Sep: pretty.SoftLineComma()},
				{Cells: []pretty.Doc{pretty.Text("x:"), pretty.Text("3")}, Sep: pretty.SoftLineComma()},
			}),
			want: `
foo:    1
barbaz: 2
x:      3`[1:],
		},
		{
			name: "table_alignment_flat",
			doc: func() pretty.Doc {
				rows := []pretty.Row{
					{Cells: []pretty.Doc{pretty.Text("a:"), pretty.Text("1")}},
					{Cells: []pretty.Doc{pretty.Text("b:"), pretty.Text("2")}, Sep: pretty.SoftLineComma()},
				}
				return pretty.Group(pretty.Cats(
					pretty.Text("{"),
					pretty.Nest(pretty.Cat(pretty.Line(""), pretty.Table(rows))),
					pretty.Line(""),
					pretty.Text("}"),
				))
			}(),
			want: "{a: 1, b: 2}",
		},
		{
			name: "trailing_comma_flat",
			doc:  listDoc,
			want: "[1, 2]",
		},
		{
			name:  "trailing_comma_broken",
			doc:   listDoc,
			width: 5,
			want: `
[
	1,
	2,
]`[1:],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := pretty.Config{Width: tt.width, Indent: tt.indent}
			got := string(cfg.Render(tt.doc))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- txtar-based AST integration tests ---

// TestTxtar runs all txtar-based pretty-printer tests from testdata/.
// Each txtar file contains:
//   - An optional "config" section with width/indent settings
//   - An "in.cue" section with the input CUE source
//   - An "out/relpos" section with the expected output (RelPos honoured)
//   - An "out/norelpos" section with the expected output (RelPos stripped)
//
// Both outputs are checked for idempotency.
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
			if sec := findSection(ar, "config"); sec != nil {
				for line := range strings.SplitSeq(strings.TrimSpace(string(sec.Data)), "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
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
						n, err := strconv.Atoi(val)
						if err != nil {
							t.Fatalf("bad indent: %v", err)
						}
						indent = strings.Repeat(" ", n)
					}
				}
			}

			// Get sections.
			input := trimTrailingNewline(sectionData(t, ar, "in.cue"))
			wantRelPos := trimTrailingNewline(sectionData(t, ar, "out/relpos"))
			wantNoRelPos := trimTrailingNewline(sectionData(t, ar, "out/norelpos"))

			cfg := &pretty.Config{Width: width, Indent: indent}

			// Test with RelPos from parser.
			f, err := parser.ParseFile("test.cue", input, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := strings.TrimRight(string(cfg.Node(f)), "\n")
			if got != wantRelPos {
				t.Errorf("with RelPos:\ngot:\n%s\nwant:\n%s", got, wantRelPos)
			} else {
				checkIdempotent(t, got, cfg)
			}

			// Test without RelPos.
			f2, err := parser.ParseFile("test.cue", input, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			stripRelPos(f2)
			got2 := strings.TrimRight(string(cfg.Node(f2)), "\n")
			if got2 != wantNoRelPos {
				t.Errorf("without RelPos:\ngot:\n%s\nwant:\n%s", got2, wantNoRelPos)
			} else {
				checkIdempotent(t, got2, cfg)
			}
		})
	}
}

// findSection returns the first archive file with the given name, or nil.
func findSection(ar *txtar.Archive, name string) *txtar.File {
	for i := range ar.Files {
		if ar.Files[i].Name == name {
			return &ar.Files[i]
		}
	}
	return nil
}

// sectionData returns the data for the named section, fataling if not found.
func sectionData(t *testing.T, ar *txtar.Archive, name string) string {
	t.Helper()
	sec := findSection(ar, name)
	if sec == nil {
		t.Fatalf("missing section %q", name)
	}
	return string(sec.Data)
}

// trimTrailingNewline removes a single trailing newline if present.
// txtar sections always end with a newline, but the test expectations
// don't include it.
func trimTrailingNewline(s string) string {
	return strings.TrimRight(s, "\n")
}

// --- Helper functions ---

// checkIdempotent verifies that pretty-printing is idempotent:
// printing the output of a print-parse cycle produces the same result.
func checkIdempotent(t *testing.T, formatted string, cfg *pretty.Config) {
	t.Helper()
	f2, err := parser.ParseFile("idempotent.cue", formatted, parser.ParseComments)
	if err != nil {
		t.Errorf("idempotency: re-parse failed: %v\nformatted output was:\n%s", err, formatted)
		return
	}
	got2 := strings.TrimRight(string(cfg.Node(f2)), "\n")
	formatted = strings.TrimRight(formatted, "\n")
	if got2 != formatted {
		t.Errorf("idempotency failure:\nfirst:  %q\nsecond: %q", formatted, got2)
	}
}

// stripRelPos removes all RelPos information from every token.Pos field
// in the AST, so the printer must rely on its own layout decisions.
func stripRelPos(n ast.Node) {
	posType := reflect.TypeFor[token.Pos]()
	ast.Walk(n, func(node ast.Node) bool {
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
				if pos.HasRelPos() {
					f.Set(reflect.ValueOf(pos.WithRel(token.NoRelPos)))
				}
			}
		}
		return true
	}, nil)
}

// --- Programmatic AST tests ---
//
// These tests use programmatically constructed ASTs to test
// behaviours that can't be exercised via parsed CUE source
// (e.g. specific RelPos values, comments in unusual positions).
// All tests check idempotency to ensure the output is valid CUE.

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
			name: "relpos/elided",
			node: twoFieldFile(token.Elided),
			cfg:  &pretty.Config{Width: 80},
			want: "a: 1",
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
			cfg:  &pretty.Config{Width: 80},
			want: `
{a: 1
	b: 2}`[1:],
		},
		{
			name: "relpos/newline_in_struct_narrow",
			node: twoFieldStruct(token.Newline),
			cfg:  &pretty.Config{Width: 10},
			want: `
{a: 1
	b: 2}`[1:],
		},
		{
			name: "relpos/new_section_in_struct",
			node: twoFieldStruct(token.NewSection),
			cfg:  &pretty.Config{Width: 80},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := strings.TrimRight(string(tt.cfg.Node(tt.node)), "\n")
			want := strings.TrimRight(tt.want, "\n")
			if got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			checkIdempotent(t, got, tt.cfg)
		})
	}
}

// TestLargeFileFormatComparison times the new internal/pretty printer
// against the existing cue/format printer on a large CUE file. The
// file is located via the CUE_LARGE_FILE env var, or, if unset, at
// ../../large_file.cue relative to this package. The test is skipped
// if the file is not present. Run with `go test -v -run
// TestLargeFileFormatComparison ./internal/pretty` to see timings.
func TestLargeFileFormatComparison(t *testing.T) {
	path := os.Getenv("CUE_LARGE_FILE")
	if path == "" {
		path = filepath.Join("..", "..", "large_file.cue")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("large file not available at %s: %v", path, err)
	}

	parseStart := time.Now()
	f, err := parser.ParseFile(path, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	parseElapsed := time.Since(parseStart)

	// Warm up the allocator so the first formatter run doesn't pay a
	// disproportionate GC cost.
	runtime.GC()

	cfg := &pretty.Config{}
	prettyStart := time.Now()
	prettyOut := cfg.Node(f)
	prettyElapsed := time.Since(prettyStart)

	runtime.GC()

	formatStart := time.Now()
	formatOut, err := format.Node(f)
	if err != nil {
		t.Fatalf("cue/format: %v", err)
	}
	formatElapsed := time.Since(formatStart)

	t.Logf("input:            %d bytes", len(src))
	t.Logf("parse:            %v", parseElapsed)
	t.Logf("internal/pretty:  %v  (output: %d bytes)", prettyElapsed, len(prettyOut))
	t.Logf("cue/format:       %v  (output: %d bytes)", formatElapsed, len(formatOut))
	t.Logf("pretty / format:  %.2fx", float64(prettyElapsed)/float64(formatElapsed))
}
