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
	"reflect"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/pretty"
)

func TestDocCombinators(t *testing.T) {
	tests := []struct {
		name  string
		doc   *pretty.Doc
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
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.SoftLine(), pretty.Text("b"))),
			width: 80,
			want:  "a b",
		},
		{
			name:  "group_breaks",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.SoftLine(), pretty.Text("b"))),
			width: 2,
			want: `a
b`,
		},
		{
			name:  "nest_in_broken_group",
			doc:   pretty.Group(pretty.Cats(pretty.Text("{"), pretty.Nest(1, pretty.Cat(pretty.SoftLine(), pretty.Text("x"))), pretty.SoftLine(), pretty.Text("}"))),
			width: 3,
			want: `{
	x
}`,
		},
		{
			name: "hardline_forces_break",
			doc:  pretty.Group(pretty.Cats(pretty.Text("a"), pretty.HardLine(), pretty.Text("b"))),
			want: `a
b`,
		},
		{
			name:  "ifbreak_flat",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.IfBreak(pretty.Text("!"), pretty.Text("?")), pretty.Text("b"))),
			width: 80,
			want:  "a?b",
		},
		{
			name:  "ifbreak_broken",
			doc:   pretty.Group(pretty.Cats(pretty.Text("a"), pretty.IfBreak(pretty.Text("!"), pretty.Text("?")), pretty.SoftLine(), pretty.Text("b"))),
			width: 2,
			want: `a!
b`,
		},
		{
			name: "blank_line",
			doc:  pretty.Cats(pretty.Text("a"), pretty.BlankLine(), pretty.Text("b")),
			want: `a

b`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width := tt.width
			if width == 0 {
				width = 80
			}
			got := string(pretty.Render(width, "\t", tt.doc))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDocSpaceIndent(t *testing.T) {
	doc := pretty.Group(pretty.Cats(
		pretty.Text("{"),
		pretty.Nest(1, pretty.Cat(pretty.SoftLine(), pretty.Text("x"))),
		pretty.SoftLine(),
		pretty.Text("}"),
	))

	got := string(pretty.Render(3, "    ", doc))
	want := `{
    x
}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTableAlignment(t *testing.T) {
	rows := []pretty.Row{
		{Key: pretty.Text("foo:"), Val: pretty.Text("1")},
		{Key: pretty.Text("barbaz:"), Val: pretty.Text("2")},
		{Key: pretty.Text("x:"), Val: pretty.Text("3")},
	}

	// In broken mode (top-level render), values should be aligned.
	got := string(pretty.Render(80, "\t", pretty.Table(rows)))
	want := `foo:    1
barbaz: 2
x:      3`
	if got != want {
		t.Errorf("table alignment:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableAlignmentFlat(t *testing.T) {
	rows := []pretty.Row{
		{Key: pretty.Text("a:"), Val: pretty.Text("1")},
		{Key: pretty.Text("b:"), Val: pretty.Text("2")},
	}

	// In a group that fits, table should be inline.
	doc := pretty.Group(pretty.Cats(
		pretty.Text("{"),
		pretty.Nest(1, pretty.Cat(pretty.Line(""), pretty.Table(rows))),
		pretty.Line(""),
		pretty.Text("}"),
	))

	got := string(pretty.Render(80, "\t", doc))
	want := "{a: 1, b: 2}"
	if got != want {
		t.Errorf("flat table:\ngot %q\nwant %q", got, want)
	}
}

func TestTrailingComma(t *testing.T) {
	doc := pretty.Group(pretty.Cats(
		pretty.Text("["),
		pretty.Nest(1, pretty.Cat(pretty.Line(""), pretty.Sep(pretty.Cats(pretty.Text(","), pretty.Line(" ")), pretty.Text("1"), pretty.Text("2")))),
		pretty.TrailingComma(),
		pretty.Line(""),
		pretty.Text("]"),
	))

	// Flat: no trailing comma.
	got := string(pretty.Render(80, "\t", doc))
	if want := "[1, 2]"; got != want {
		t.Errorf("flat list: got %q, want %q", got, want)
	}

	// Broken: has trailing comma.
	got = string(pretty.Render(5, "\t", doc))
	want := `[
	1,
	2,
]`
	if got != want {
		t.Errorf("broken list:\ngot %q\nwant %q", got, want)
	}
}

// --- AST integration tests ---

// testPretty parses input, formats it, and checks against two expectations:
//   - want: expected output with RelPos from the parser honoured
//   - wantNoRelPos: expected output after stripping all RelPos from the AST
//
// Both are also checked for idempotency.
func testPretty(t *testing.T, input string, width int, indent string, want, wantNoRelPos string) {
	t.Helper()
	f, err := parser.ParseFile("test.cue", input, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := &pretty.Config{Width: width, Indent: indent}

	// Test with RelPos from parser.
	got := strings.TrimRight(string(cfg.Node(f)), "\n")
	want = strings.TrimRight(want, "\n")
	if got != want {
		t.Errorf("with RelPos:\ninput: %s\ngot:\n%s\nwant:\n%s", input, got, want)
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
	wantNoRelPos = strings.TrimRight(wantNoRelPos, "\n")
	if got2 != wantNoRelPos {
		t.Errorf("without RelPos:\ninput: %s\ngot:\n%s\nwant:\n%s", input, got2, wantNoRelPos)
	} else {
		checkIdempotent(t, got2, cfg)
	}
}

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

func TestSimpleStructFlat(t *testing.T) {
	// Single-line input: parser gives Blank RelPos. Fits on one line.
	// Without RelPos: same — fits on one line.
	input := `{a: 1, b: 2}`
	testPretty(t, input, 80, "\t", `{a: 1, b: 2}`, `{a: 1, b: 2}`)
}

func TestSimpleStructMultiline(t *testing.T) {
	// Multi-line input: parser gives Newline RelPos.
	// With RelPos: Newline is honoured, stays multi-line even at width 80.
	// Without RelPos: fits on one line.
	input := `{
	a: 1
	b: 2
}`
	testPretty(t, input, 80, "\t",
		`{
	a: 1
	b: 2
}`,
		`{a: 1, b: 2}`)
}

func TestSimpleStructNarrow(t *testing.T) {
	// Single-line input at narrow width: must break regardless of RelPos.
	input := `{a: 1, b: 2}`
	testPretty(t, input, 8, "\t",
		`{
	a: 1
	b: 2
}`,
		`{
	a: 1
	b: 2
}`)
}

func TestSimpleListFlat(t *testing.T) {
	// Single-line input: fits on one line.
	input := `[1, 2, 3]`
	testPretty(t, input, 80, "\t", `[1, 2, 3]`, `[1, 2, 3]`)
}

func TestSimpleListMultiline(t *testing.T) {
	// Multi-line input: parser gives Newline RelPos.
	// With RelPos: Newline is honoured, stays multi-line even at width 80.
	// Without RelPos: fits on one line.
	input := `[
	1,
	2,
	3,
]`
	testPretty(t, input, 80, "\t",
		`[
	1,
	2,
	3,
]`,
		`[1, 2, 3]`)
}

func TestSimpleListNarrow(t *testing.T) {
	// Single-line input at narrow width: must break regardless of RelPos.
	input := `[1, 2, 3]`
	testPretty(t, input, 6, "\t",
		`[
	1,
	2,
	3,
]`,
		`[
	1,
	2,
	3,
]`)
}

func TestPackageDecl(t *testing.T) {
	input := `package foo`
	testPretty(t, input, 80, "\t", `package foo`, `package foo`)
}

func TestFieldWithConstraint(t *testing.T) {
	// Multi-line input so Newline RelPos is honoured.
	input := `{
	foo?: 1
}`
	testPretty(t, input, 80, "\t",
		`{
	foo?: 1
}`,
		`{foo?: 1}`)
}

func TestFieldWithAttribute(t *testing.T) {
	// Multi-line input so Newline RelPos is honoured.
	input := `{
	foo: 1 @go(Foo)
}`
	testPretty(t, input, 80, "\t",
		`{
	foo: 1 @go(Foo)
}`,
		`{foo: 1 @go(Foo)}`)
}

func TestBinaryExpr(t *testing.T) {
	// Multi-line input so Newline RelPos is honoured.
	input := `{
	x: 1 + 2
}`
	testPretty(t, input, 80, "\t",
		`{
	x: 1 + 2
}`,
		`{x: 1 + 2}`)
}

func TestBinaryPrecedenceSpacing(t *testing.T) {
	// Precedence-based spacing: spaces around lower-precedence operators,
	// no spaces around higher-precedence operators when mixed.
	testPretty(t, `x: a*b + c*d`, 80, "\t",
		`x: a*b + c*d`, `x: a*b + c*d`)

	// Single precedence level in normal mode: spaces around all operators.
	testPretty(t, `x: a * b * c`, 80, "\t",
		`x: a * b * c`, `x: a * b * c`)

	// Compact mode (inside comparison): no spaces around arithmetic.
	testPretty(t, `x: a+b == c+d`, 80, "\t",
		`x: a+b == c+d`, `x: a+b == c+d`)

}

func TestUnaryExpr(t *testing.T) {
	input := `{
	x: !true
}`
	testPretty(t, input, 80, "\t",
		`{
	x: !true
}`,
		`{x: !true}`)
}

func TestSelectorExpr(t *testing.T) {
	input := `{
	x: a.b.c
}`
	testPretty(t, input, 80, "\t",
		`{
	x: a.b.c
}`,
		`{x: a.b.c}`)
}

func TestCallExpr(t *testing.T) {
	input := `{
	x: strings.Join(a, ",")
}`
	testPretty(t, input, 80, "\t",
		`{
	x: strings.Join(a, ",")
}`,
		`{x: strings.Join(a, ",")}`)
}

func TestLetClause(t *testing.T) {
	input := `{
	let x = 1
}`
	testPretty(t, input, 80, "\t",
		`{
	let x = 1
}`,
		`{let x = 1}`)
}

func TestEllipsis(t *testing.T) {
	input := `{
	...
}`
	testPretty(t, input, 80, "\t",
		`{
	...
}`,
		`{...}`)
}

func TestSpaceIndent(t *testing.T) {
	// Multi-line input: Newline RelPos honoured.
	// Without RelPos at width 8: doesn't fit, so breaks anyway.
	input := `{
  a: 1
  b: 2
}`
	testPretty(t, input, 8, "  ",
		`{
  a: 1
  b: 2
}`,
		`{
  a: 1
  b: 2
}`)
}

func TestTableAlignmentAST(t *testing.T) {
	input := `{
	foo: 1
	barbaz: 2
	x: 3
}`
	testPretty(t, input, 20, "\t",
		`{
	foo:    1
	barbaz: 2
	x:      3
}`,
		`{
	foo:    1
	barbaz: 2
	x:      3
}`)
}

func TestMixedSimpleAndComplexFields(t *testing.T) {
	// Simple fields x, longname, baz are aligned across the struct.
	// nested (struct value) is interspersed but does not affect alignment.
	input := `{
	x: 1
	longname: "hello"
	nested: {
		a: 1
	}
	baz: true
}`
	testPretty(t, input, 20, "\t",
		`{
	x:        1
	longname: "hello"
	nested: {
		a: 1
	}
	baz:      true
}`,
		// Without RelPos, the inner struct {a: 1} flattens.
		`{
	x:        1
	longname: "hello"
	nested: {a: 1}
	baz:      true
}`)
}

func TestImportDecl(t *testing.T) {
	input := `import "strings"`
	testPretty(t, input, 80, "\t", `import "strings"`, `import "strings"`)
}

func TestImportGroup(t *testing.T) {
	input := `import (
	"strings"
	"list"
)`
	testPretty(t, input, 80, "\t",
		`import (
	"strings"
	"list"
)`,
		`import (
	"strings"
	"list"
)`)
}

func TestDocComment(t *testing.T) {
	input := `// A comment.
x: 1`
	testPretty(t, input, 80, "\t",
		`// A comment.
x: 1`,
		`// A comment.
x: 1`)
}

func TestLineComment(t *testing.T) {
	input := `x: 1 // a comment`
	testPretty(t, input, 80, "\t",
		`x: 1 // a comment`,
		`x: 1 // a comment`)
}

func TestLineCommentAlignment(t *testing.T) {
	// A line comment on a short-key field should not affect alignment.
	// The // comment forces the struct to break (even without RelPos)
	// because a line comment would swallow subsequent tokens in flat mode.
	input := `{
	x: 1 // comment
	longname: 2
}`
	testPretty(t, input, 80, "\t",
		`{
	x:        1 // comment
	longname: 2
}`,
		`{
	x:        1 // comment
	longname: 2
}`)
}

func TestCommentColumnAlignment(t *testing.T) {
	// Trailing // comments on consecutive fields are vertically aligned.
	input := `{
	x: "hello"   // greeting
	y: "goodbye" // farewell
	z: "hi"      // short
}`
	testPretty(t, input, 80, "\t",
		`{
	x: "hello"   // greeting
	y: "goodbye" // farewell
	z: "hi"      // short
}`,
		`{
	x: "hello"   // greeting
	y: "goodbye" // farewell
	z: "hi"      // short
}`)
}

func TestCommentColumnAlignmentMixed(t *testing.T) {
	// Only rows with comments participate in comment alignment;
	// rows without comments don't get extra padding.
	input := `{
	a: 1 // has comment
	b: 2
	c: 3 // also has comment
}`
	testPretty(t, input, 80, "\t",
		`{
	a: 1 // has comment
	b: 2
	c: 3 // also has comment
}`,
		`{
	a: 1 // has comment
	b: 2
	c: 3 // also has comment
}`)
}

func TestDocCommentAlignment(t *testing.T) {
	// A doc comment on a field should not disable padding for that field.
	// The comment appears before the field, and the field still aligns.
	// The doc comment forces the struct to break (so it's not lost).
	input := `{
	// doc comment
	x: 1
	longname: 2
}`
	testPretty(t, input, 80, "\t",
		`{
	// doc comment
	x:        1
	longname: 2
}`,
		`{
	// doc comment
	x:        1
	longname: 2
}`)
}

func TestDocCommentAlignmentMultiple(t *testing.T) {
	// Multiple fields with doc comments, all aligned.
	// Doc comments force the struct to break.
	input := `{
	// comment on a
	a: 1
	// comment on longname
	longname: 2
	b: 3
}`
	testPretty(t, input, 80, "\t",
		`{
	// comment on a
	a:        1
	// comment on longname
	longname: 2
	b:        3
}`,
		`{
	// comment on a
	a:        1
	// comment on longname
	longname: 2
	b:        3
}`)
}

func TestBlankLineBeforeComment(t *testing.T) {
	// A blank line before a doc comment block is preserved.
	input := `x: 5

// a comment
y: 6`
	testPretty(t, input, 80, "\t",
		`x: 5

// a comment
y: 6`,
		// Without RelPos, the comment's NewSection RelPos is stripped
		// so the blank line is not preserved.
		"x: 5\n// a comment\ny: 6")
}

func TestBlankLineBeforeCommentInStruct(t *testing.T) {
	// Blank line before a doc comment inside a struct.
	// The doc comment forces the struct to break.
	input := `{
	x: 5

	// a comment
	y: 6
}`
	// The blank line has indentation on it (BlankLine = HardLine+HardLine,
	// each emitting newline+indent).
	testPretty(t, input, 80, "\t",
		"{\n\tx: 5\n\t\n\t// a comment\n\ty: 6\n}",
		// Without RelPos the blank line is lost (NewSection stripped)
		// but the struct still breaks because the doc comment forces it.
		"{\n\tx: 5\n\t// a comment\n\ty: 6\n}")
}

func TestTrailingComment(t *testing.T) {
	// A comment at the end of a file, separated by a blank line,
	// is preserved with the blank line.
	input := `x: true

// a comment`
	testPretty(t, input, 80, "\t",
		`x: true

// a comment`,
		// Without RelPos the comment's NewSection is stripped,
		// so it falls back to a same-line trailing comment.
		`x: true // a comment`)
}

func TestTrailingCommentNewline(t *testing.T) {
	// A trailing comment with Newline (not NewSection) RelPos
	// goes on the next line, not the same line.
	input := `x: true
// a comment`
	testPretty(t, input, 80, "\t",
		`x: true
// a comment`,
		// Without RelPos, falls back to same-line.
		`x: true // a comment`)
}

func TestDocCommentOnValue(t *testing.T) {
	// A doc comment on a field's value expression is preserved.
	// The comment and value are indented one level from the key.
	input := `x:
	// doc on value
	1`
	testPretty(t, input, 80, "\t",
		`x:
	// doc on value
	1`,
		`x:
	// doc on value
	1`)
}

func TestDocCommentOnListElem(t *testing.T) {
	// A doc comment on a list element is preserved.
	input := `[
	// doc on elem
	1,
	2,
]`
	testPretty(t, input, 80, "\t",
		`[
	// doc on elem
	1,
	2,
]`,
		`[
	// doc on elem
	1,
	2,
]`)
}

func TestTrailingCommentOnBinaryRHS(t *testing.T) {
	// A trailing comment on a binary expression's RHS is preserved.
	// The // comment forces the struct to break.
	input := `{
	x: 1 + // trailing on plus
		2
}`
	testPretty(t, input, 80, "\t",
		`{
	x: 1 +
	2 // trailing on plus
}`,
		// Without RelPos, the binary expr can flatten (1 + 2)
		// but the struct still breaks because the // comment would
		// swallow the closing brace in flat mode.
		`{
	x: 1 + 2 // trailing on plus
}`)
}

func TestMultiLineCommentGroup(t *testing.T) {
	input := `// line one
// line two
// line three
x: 1`
	testPretty(t, input, 80, "\t",
		`// line one
// line two
// line three
x: 1`,
		`// line one
// line two
// line three
x: 1`)
}

func TestCommentOnComplexField(t *testing.T) {
	input := `{
	// doc on struct field
	x: {
		a: 1
	}
}`
	testPretty(t, input, 80, "\t",
		`{
	// doc on struct field
	x: {
		a: 1
	}
}`,
		`{
	// doc on struct field
	x: {a: 1}
}`)
}

func TestCommentOnListField(t *testing.T) {
	input := `{
	// doc on list field
	x: [1, 2, 3]
}`
	testPretty(t, input, 80, "\t",
		`{
	// doc on list field
	x: [1, 2, 3]
}`,
		`{
	// doc on list field
	x: [1, 2, 3]
}`)
}

func TestMultipleCommentsOnField(t *testing.T) {
	input := `{
	// first doc
	// second doc
	x: 1 // trailing
}`
	testPretty(t, input, 80, "\t",
		`{
	// first doc
	// second doc
	x: 1 // trailing
}`,
		`{
	// first doc
	// second doc
	x: 1 // trailing
}`)
}

func TestCommentBetweenListElems(t *testing.T) {
	input := `[
	1,
	// between
	2,
	3,
]`
	testPretty(t, input, 80, "\t",
		`[
	1,
	// between
	2,
	3,
]`,
		`[
	1,
	// between
	2,
	3,
]`)
}

func TestTrailingCommentOnListElem(t *testing.T) {
	// Trailing // comments on list elements: comma comes before comment.
	input := `[
	1, // first
	2, // second
	3, // third
]`
	testPretty(t, input, 80, "\t",
		`[
	1, // first
	2, // second
	3, // third
]`,
		`[
	1, // first
	2, // second
	3, // third
]`)
}

func TestCommentOnlyFile(t *testing.T) {
	input := `// just a comment
`
	testPretty(t, input, 80, "\t",
		`// just a comment`,
		`// just a comment`)
}

func TestCommentAfterPackage(t *testing.T) {
	// Blank line before doc-commented field after package clause
	// (a non-field decl) is always inserted by the heuristic,
	// even without RelPos.
	input := `package foo

// a comment
x: 1`
	testPretty(t, input, 80, "\t",
		`package foo

// a comment
x: 1`,
		`package foo

// a comment
x: 1`)
}

func TestCommentOnLet(t *testing.T) {
	input := `{
	// doc on let
	let x = 1
	y: x
}`
	testPretty(t, input, 80, "\t",
		`{
	// doc on let
	let x = 1
	y: x
}`,
		`{
	// doc on let
	let x = 1
	y: x
}`)
}

func TestCommentOnEllipsis(t *testing.T) {
	input := `{
	x: 1
	// doc on ellipsis
	...
}`
	testPretty(t, input, 80, "\t",
		`{
	x: 1
	// doc on ellipsis
	...
}`,
		`{
	x: 1
	// doc on ellipsis
	...
}`)
}

func TestComprehension(t *testing.T) {
	input := `{
	for k, v in x {(k): v}
}`
	testPretty(t, input, 80, "\t",
		`{
	for k, v in x {(k): v}
}`,
		`{for k, v in x {(k): v}}`)
}

func TestComprehensionMultiClause(t *testing.T) {
	// Clauses on separate lines: RelPos Newline between them is honoured.
	input := `{
	for a in [1, 10]
	let x = {
		value: a + 1
	}
	if x.value > 5 {
		b: x
	}
}`
	testPretty(t, input, 80, "\t",
		`{
	for a in [1, 10]
	let x = {
		value: a + 1
	}
	if x.value > 5 {
		b: x
	}
}`,
		`{for a in [1, 10] let x = {value: a + 1} if x.value > 5 {b: x}}`)
}

func TestLetDecl(t *testing.T) {
	input := `{
	let X = foo
}`
	testPretty(t, input, 80, "\t",
		`{
	let X = foo
}`,
		`{let X = foo}`)
}

func TestEmptyStruct(t *testing.T) {
	input := `{}`
	testPretty(t, input, 80, "\t", `{}`, `{}`)
}

func TestEmptyList(t *testing.T) {
	input := `{
	x: []
}`
	testPretty(t, input, 80, "\t",
		`{
	x: []
}`,
		`{x: []}`)
}

func TestNestedStructs(t *testing.T) {
	input := `{
	a: {b: {c: 1}}
}`
	testPretty(t, input, 80, "\t",
		`{
	a: {b: {c: 1}}
}`,
		`{a: {b: {c: 1}}}`)
}

func TestBottomLit(t *testing.T) {
	input := `{
	x: _|_
}`
	testPretty(t, input, 80, "\t",
		`{
	x: _|_
}`,
		`{x: _|_}`)
}

func TestDefinition(t *testing.T) {
	input := `{
	#Foo: {bar: string}
}`
	testPretty(t, input, 80, "\t",
		`{
	#Foo: {bar: string}
}`,
		`{#Foo: {bar: string}}`)
}

func TestBlankLineAfterDefinition(t *testing.T) {
	// A doc-commented field after a definition gets a blank line
	// before it, even if the RelPos doesn't already specify one.
	// The blank line has indentation (BlankLine = HardLine+HardLine).
	// Fields are table-aligned: #Foo: and x: share the same key width.
	input := `{
	#Foo: string
	// doc comment
	x: #Foo
}`
	testPretty(t, input, 80, "\t",
		"{\n\t#Foo: string\n\t\n\t// doc comment\n\tx:    #Foo\n}",
		"{\n\t#Foo: string\n\t\n\t// doc comment\n\tx:    #Foo\n}")
}

func TestNoBlankLineFieldToField(t *testing.T) {
	// Between two regular fields (no definitions), a doc comment does
	// NOT trigger a blank line upgrade. The doc comment forces the
	// struct to break (so the comment isn't lost).
	input := `{
	a: 1
	// doc comment
	b: 2
}`
	testPretty(t, input, 80, "\t",
		`{
	a: 1
	// doc comment
	b: 2
}`,
		`{
	a: 1
	// doc comment
	b: 2
}`)
}

func TestDisjunction(t *testing.T) {
	input := `{
	x: 1 | 2 | 3
}`
	testPretty(t, input, 80, "\t",
		`{
	x: 1 | 2 | 3
}`,
		`{x: 1 | 2 | 3}`)
}

func TestDisjunctionComments(t *testing.T) {
	// Comments on disjunction arms are preserved. Each // comment
	// appears after the "|" on the same line as the preceding arm.
	// Continuation disjuncts are indented one level.
	input := `out: "A" | // first letter
    "B" | // second letter
    "C" | // third letter
    "D" // fourth letter
`
	testPretty(t, input, 80, "\t",
		"out: \"A\" | // first letter\n"+
			"\t\"B\" | // second letter\n"+
			"\t\"C\" | // third letter\n"+
			"\t\"D\" // fourth letter",
		"out: \"A\" | // first letter\n"+
			"\t\"B\" | // second letter\n"+
			"\t\"C\" | // third letter\n"+
			"\t\"D\" // fourth letter")
}

func TestConjunction(t *testing.T) {
	input := `{
	x: int & >0
}`
	testPretty(t, input, 80, "\t",
		`{
	x: int & >0
}`,
		`{x: int & >0}`)
}

func TestIndexExpr(t *testing.T) {
	input := `{
	x: a[0]
}`
	testPretty(t, input, 80, "\t",
		`{
	x: a[0]
}`,
		`{x: a[0]}`)
}

func TestParenExpr(t *testing.T) {
	input := `{
	x: (1 + 2)
}`
	testPretty(t, input, 80, "\t",
		`{
	x: (1 + 2)
}`,
		`{x: (1 + 2)}`)
}

// --- RelPos on expressions ---

func TestCallMultiline(t *testing.T) {
	// Multi-line call args: RelPos Newline honoured, args stay on separate lines.
	input := `x: strings.Join(
	a,
	",",
)`
	testPretty(t, input, 80, "\t",
		`x: strings.Join(
	a,
	",",
)`,
		`x: strings.Join(a, ",")`)
}

func TestIndexMultiline(t *testing.T) {
	// Multi-line index: RelPos Newline honoured on the index expression.
	// No newline before ']' (auto-comma insertion would break it).
	input := `x: a[
	0]`
	testPretty(t, input, 80, "\t",
		`x: a[
	0]`,
		`x: a[0]`)
}

func TestParenMultiline(t *testing.T) {
	// Multi-line paren: RelPos Newline honoured on the inner expression.
	// No newline before ')' (auto-comma insertion would break it).
	input := `x: (
	1 + 2)`
	testPretty(t, input, 80, "\t",
		`x: (
	1 + 2)`,
		`x: (1 + 2)`)
}

func TestConjunctionChain(t *testing.T) {
	// Conjunction chain flattened and indented like disjunctions.
	input := `x: int &
	>0 &
	<100`
	testPretty(t, input, 80, "\t",
		`x: int &
	>0 &
	<100`,
		`x: int & >0 & <100`)
}

// --- Table alignment tests ---

func TestAlignmentVaryingKeyLengths(t *testing.T) {
	// Fields with varying key lengths: values should be aligned.
	input := `{
	a: 1
	bb: 2
	ccc: 3
	dddd: 4
}`
	// Without RelPos the struct can try to flatten, but at width 20 it
	// doesn't fit, so the output is the same.
	testPretty(t, input, 20, "\t",
		`{
	a:    1
	bb:   2
	ccc:  3
	dddd: 4
}`,
		`{
	a:    1
	bb:   2
	ccc:  3
	dddd: 4
}`)
}

func TestAlignmentWithConstraints(t *testing.T) {
	// Optional/required markers are part of the key column.
	input := `{
	name: string
	age?: int
	id!: string
}`
	// Key widths: "name:" = 5, "age?:" = 5, "id!:" = 4. Max = 5.
	testPretty(t, input, 20, "\t",
		`{
	name: string
	age?: int
	id!:  string
}`,
		`{
	name: string
	age?: int
	id!:  string
}`)
}

func TestAlignmentBrokenByStructValue(t *testing.T) {
	// A struct-valued field is interspersed among simple fields but does
	// not participate in column width calculation. All simple field values
	// are aligned to the widest key across the whole struct.
	input := `{
	short: 1
	longname: 2
	mid: {inner: true}
	a: 3
	bc: 4
}`
	testPretty(t, input, 25, "\t",
		`{
	short:    1
	longname: 2
	mid: {inner: true}
	a:        3
	bc:       4
}`,
		`{
	short:    1
	longname: 2
	mid: {inner: true}
	a:        3
	bc:       4
}`)
}

func TestAlignmentBrokenByListValue(t *testing.T) {
	// A list-valued field is interspersed among simple fields.
	// Simple fields x and y are aligned (both have key width 2).
	// items (list value) is a raw row and does not affect alignment.
	input := `{
	x: 1
	items: [1, 2, 3]
	y: 2
}`
	testPretty(t, input, 20, "\t",
		`{
	x: 1
	items: [1, 2, 3]
	y: 2
}`,
		`{
	x: 1
	items: [1, 2, 3]
	y: 2
}`)
}

func TestAlignmentWithAttributes(t *testing.T) {
	// Attributes are part of the value column.
	// Keys are padded so values align.
	input := `{
	name: string @go(Name)
	longField: int @go(LongField)
}`
	testPretty(t, input, 40, "\t",
		`{
	name:      string @go(Name)
	longField: int @go(LongField)
}`,
		`{
	name:      string @go(Name)
	longField: int @go(LongField)
}`)
}

func TestAlignmentSingleField(t *testing.T) {
	// A single simple field: no alignment padding needed.
	// The input is parsed with Newline RelPos (multi-line source),
	// so it stays multi-line regardless of width.
	// Without RelPos, the struct is free to flatten at width 80.
	input := `{
	x: 1
}`
	testPretty(t, input, 80, "\t",
		`{
	x: 1
}`,
		`{x: 1}`)
	testPretty(t, input, 3, "\t",
		`{
	x: 1
}`,
		`{
	x: 1
}`)

	// An inline struct stays inline because Blank RelPos is honoured.
	// Without RelPos, same result — fits on one line.
	testPretty(t, `{x: 1}`, 80, "\t", `{x: 1}`, `{x: 1}`)
}

func TestAlignmentAllComplex(t *testing.T) {
	// All fields have struct/list values — no table alignment at all.
	input := `{
	a: {x: 1}
	b: [1, 2]
}`
	testPretty(t, input, 20, "\t",
		`{
	a: {x: 1}
	b: [1, 2]
}`,
		`{
	a: {x: 1}
	b: [1, 2]
}`)
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
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1
b: 2`)
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
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1

b: 2`)
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
	// Fields without any RelPos: top-level context defaults to pretty.HardLine.
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
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1
b: 2`)
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
	// invalid syntax (a: 1b: 2), so the printer falls back to the
	// default separator (pretty.HardLine at top level).
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
	testAST(t, f, &pretty.Config{Width: 80}, `a: 1
b: 2`)
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
	testAST(t, f, &pretty.Config{Width: 80}, `{
	a: 1
	b: 2
}`)
	testAST(t, f, &pretty.Config{Width: 10}, `{
	a: 1
	b: 2
}`)
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
	// The blank line between fields has indentation on it because
	// BlankLine is pretty.Cat(pretty.HardLine, pretty.HardLine) and each pretty.HardLine emits
	// newline + indent.
	testAST(t, f, &pretty.Config{Width: 80}, "{"+
		"\n\ta: 1"+
		"\n\t"+
		"\n\tb: 2"+
		"\n}")
}

// --- Multi-line string tests ---

func TestMultiLineString(t *testing.T) {
	input := `x: """
	hello
	world
	"""`
	testPretty(t, input, 80, "\t",
		`x: """
	hello
	world
	"""`,
		`x: """
	hello
	world
	"""`)
}

func TestMultiLineStringInStruct(t *testing.T) {
	input := `{
	description: """
		This is a long
		description field
		"""
	name: "foo"
}`
	// Multi-line string value is a BasicLit, so the field participates
	// in table alignment. name: is padded to match description:.
	testPretty(t, input, 80, "\t",
		`{
	description: """
		This is a long
		description field
		"""
	name:        "foo"
}`,
		`{
	description: """
		This is a long
		description field
		"""
	name:        "foo"
}`)
}

func TestRawString(t *testing.T) {
	input := `{
	x: #"a raw string"#
}`
	testPretty(t, input, 80, "\t",
		`{
	x: #"a raw string"#
}`,
		`{x: #"a raw string"#}`)
}

func TestRawMultiLineString(t *testing.T) {
	input := `z: #"""
	a raw multi-line
	string
	"""#`
	testPretty(t, input, 80, "\t",
		`z: #"""
	a raw multi-line
	string
	"""#`,
		`z: #"""
	a raw multi-line
	string
	"""#`)
}

func TestMultiLineStringNarrow(t *testing.T) {
	// Even at narrow width, multi-line string content is never reflowed.
	input := `{
	x: """
		hello
		world
		"""
	y: 1
}`
	testPretty(t, input, 20, "\t",
		`{
	x: """
		hello
		world
		"""
	y: 1
}`,
		`{
	x: """
		hello
		world
		"""
	y: 1
}`)
}

func TestMultiLineStringMultipleFields(t *testing.T) {
	// Two multi-line string fields and a simple field.
	input := `{
	a: """
		first
		"""
	b: """
		second
		"""
	c: 42
}`
	testPretty(t, input, 80, "\t",
		`{
	a: """
		first
		"""
	b: """
		second
		"""
	c: 42
}`,
		`{
	a: """
		first
		"""
	b: """
		second
		"""
	c: 42
}`)
}

func TestEpic(t *testing.T) {
	src := `r: _|_ @test(err, code=incomplete, contains="issue3437.r: undefined field: a", pos=[5:9])
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
`
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
