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
	"fmt"
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
)

func TestDocCombinators(t *testing.T) {
	tests := []struct {
		name  string
		doc   pretty.Doc
		width int
		want  string
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
	}

	var cfg pretty.Config
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.Width = tt.width
			got := string(cfg.Render(tt.doc))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDocSpaceIndent(t *testing.T) {
	doc := pretty.Group(pretty.Cats(
		pretty.Text("{"),
		pretty.Nest(pretty.Cat(pretty.SoftLineSpace(), pretty.Text("x"))),
		pretty.SoftLineSpace(),
		pretty.Text("}"),
	))

	got := string(pretty.Config{Width: 3, Indent: "    "}.Render(doc))
	want := `
{
    x
}`[1:]
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTableAlignment(t *testing.T) {
	rows := []pretty.Row{
		{Cells: []pretty.Doc{pretty.Text("foo:"), pretty.Text("1")}},
		{Cells: []pretty.Doc{pretty.Text("barbaz:"), pretty.Text("2")}, Sep: pretty.SoftLineComma()},
		{Cells: []pretty.Doc{pretty.Text("x:"), pretty.Text("3")}, Sep: pretty.SoftLineComma()},
	}

	// In broken mode (top-level render), values should be aligned.
	got := string(pretty.Config{}.Render(pretty.Table(rows)))
	want := `
foo:    1
barbaz: 2
x:      3`[1:]
	if got != want {
		t.Errorf("table alignment:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableAlignmentFlat(t *testing.T) {
	rows := []pretty.Row{
		{Cells: []pretty.Doc{pretty.Text("a:"), pretty.Text("1")}},
		{Cells: []pretty.Doc{pretty.Text("b:"), pretty.Text("2")}, Sep: pretty.SoftLineComma()},
	}

	// In a group that fits, table should be inline.
	doc := pretty.Group(pretty.Cats(
		pretty.Text("{"),
		pretty.Nest(pretty.Cat(pretty.Line(""), pretty.Table(rows))),
		pretty.Line(""),
		pretty.Text("}"),
	))

	got := string(pretty.Config{}.Render(doc))
	want := "{a: 1, b: 2}"
	if got != want {
		t.Errorf("flat table:\ngot %q\nwant %q", got, want)
	}
}

func TestTrailingCommaDoc(t *testing.T) {
	doc := pretty.Group(pretty.Cats(
		pretty.Text("["),
		pretty.Nest(pretty.Cat(pretty.Line(""), pretty.Sep(pretty.Cats(pretty.Text(","), pretty.Line(" ")), pretty.Text("1"), pretty.Text("2")))),
		pretty.TrailingComma(),
		pretty.Line(""),
		pretty.Text("]"),
	))

	// Flat: no trailing comma.
	got := string(pretty.Config{}.Render(doc))
	if want := "[1, 2]"; got != want {
		t.Errorf("flat list: got %q, want %q", got, want)
	}

	// Broken: has trailing comma.
	got = string(pretty.Config{Width: 5}.Render(doc))
	want := `
[
	1,
	2,
]`[1:]
	if got != want {
		t.Errorf("broken list:\ngot %q\nwant %q", got, want)
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
				for _, line := range strings.Split(strings.TrimSpace(string(sec.Data)), "\n") {
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
	posType := reflect.TypeOf(token.Pos{})
	ast.Walk(n, func(node ast.Node) bool {
		v := reflect.ValueOf(node)
		if v.Kind() == reflect.Ptr {
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

// --- RelPos tests using programmatically constructed ASTs ---
//
// All programmatic tests check idempotency to ensure the output is valid CUE.

func testAST(t *testing.T, n ast.Node, cfg *pretty.Config, want string) {
	t.Helper()
	got := strings.TrimRight(string(cfg.Node(n)), "\n")
	want = strings.TrimRight(want, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	checkIdempotent(t, got, cfg)
}

func TestRelPosNewline(t *testing.T) {
	// Newline RelPos between top-level fields produces a hard line break.
	f := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label:    &ast.Ident{Name: "a"},
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
	testAST(t, f, &pretty.Config{Width: 80}, `
a: 1
b: 2`[1:])
}

func TestRelPosNewSection(t *testing.T) {
	// NewSection RelPos produces a blank line separator.
	f := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label:    &ast.Ident{Name: "a"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
			},
			&ast.Field{
				Label:    &ast.Ident{NamePos: token.NewSection.Pos(), Name: "b"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	testAST(t, f, &pretty.Config{Width: 80}, `
a: 1

b: 2`[1:])
}

func TestRelPosBlank(t *testing.T) {
	// Blank RelPos in a struct context: between declarations a bare space
	// is not valid CUE, so the printer uses comma separation instead.
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
				Label:    &ast.Ident{NamePos: token.Blank.Pos(), Name: "b"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	f := &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: s}}}
	testAST(t, f, &pretty.Config{Width: 80}, `{a: 1, b: 2}`)
}

func TestRelPosNoRelPos(t *testing.T) {
	// Fields without any RelPos: the file-level Group allows
	// flattening, so at width 80 they fit on one line.
	f := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label: &ast.Ident{Name: "a"},
				Value: &ast.BasicLit{Kind: token.INT, Value: "1"},
			},
			&ast.Field{
				Label: &ast.Ident{Name: "b"},
				Value: &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1, b: 2`)
}

func TestRelPosElided(t *testing.T) {
	// Elided RelPos: the declaration is skipped entirely because
	// including it without a separator would produce invalid syntax.
	f := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label:    &ast.Ident{Name: "a"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
			},
			&ast.Field{
				Label:    &ast.Ident{NamePos: token.Elided.Pos(), Name: "b"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1`)
}

func TestRelPosNoSpace(t *testing.T) {
	// NoSpace RelPos between declarations: honouring it would produce
	// invalid syntax (a: 1b: 2), so the printer upgrades to
	// SoftLineComma. At width 80 the file-level Group flattens this.
	f := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label:    &ast.Ident{Name: "a"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "1"},
			},
			&ast.Field{
				Label:    &ast.Ident{NamePos: token.NoSpace.Pos(), Name: "b"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1, b: 2`)
}

func TestRelPosNewlineInStruct(t *testing.T) {
	// Newline RelPos in a struct: forces multi-line even at wide widths.
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
				Label:    &ast.Ident{NamePos: token.Newline.Pos(), Name: "b"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	f := &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: s}}}
	testAST(t, f, &pretty.Config{Width: 80}, `
{
	a: 1
	b: 2
}`[1:])
	testAST(t, f, &pretty.Config{Width: 10}, `
{
	a: 1
	b: 2
}`[1:])
}

func TestRelPosNewSectionInStruct(t *testing.T) {
	// NewSection RelPos between struct fields: produces a blank line,
	// forcing the struct to break and visually separating groups of fields.
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
				Label:    &ast.Ident{NamePos: token.NewSection.Pos(), Name: "b"},
				TokenPos: token.Blank.Pos(),
				Value:    &ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		},
	}
	f := &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: s}}}
	testAST(t, f, &pretty.Config{Width: 80}, `
{
	a: 1

	b: 2
}`[1:])
}

func TestEpic(t *testing.T) {
	src := `
r: _|_ @test(err, code=incomplete, contains="issue3437.r: undefined field: a", pos=[5:9])
d: a: a: a: b: 4
out: {
	a: {
		a: {
			a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
			b: 4
		} | {
			a: {
				a: a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
				b: 4
			} | {
				a: a: {
					a: {b: 4} | {a: a: a: b: 4}
					b: 4
				} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
					b: 4
				} | {a: a: {a: b: 4
					b: 4
				} | {a: {a: a: b: 4
					b: 4
				}
				}}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
		b: 4
	} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
		b: 4
	} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}
		b: 4
	} | {a: {a: {a: b: 4
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	}}
		b: 4
	} | {a: {a: {a: b: 4
		b: 4
	}
		b: 4
	} | {a: {a: {a: b: 4
		b: 4
	}
		b: 4
	}}}}}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: b: 4
	b: 4
} | {a: {a: a: b: 4
	b: 4
}}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: b: 4
	b: 4
} | {a: {a: a: b: 4
	b: 4
}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}
	b: 4
} | {a: {a: {a: b: 4
	b: 4
} | {a: {a: a: b: 4
	b: 4
}}
	b: 4
} | {a: {a: {a: b: 4
	b: 4
}
	b: 4
} | {a: {a: {a: b: 4
	b: 4
}
	b: 4
}}}}}}}}}}
`[1:]
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &pretty.Config{Width: 80, Indent: "  "}

	got := string(cfg.Node(f))
	fmt.Printf("=== epic ===\n%s\n", got)

	checkIdempotent(t, got, cfg)

	stripRelPos(f)
	got = string(cfg.Node(f))
	fmt.Printf("=== epic-no-relpos ===\n%s\n", got)
}
