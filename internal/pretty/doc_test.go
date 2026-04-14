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

package pretty

import (
	"testing"
)

func TestDocCombinators(t *testing.T) {
	listDoc := group(cats(
		stringLit("["),
		nest(cat(lineBreakSoft(""), sep(cats(stringLit(","), lineBreakSoft(" ")), stringLit("1"), stringLit("2")))),
		commaWhenBroken,
		lineBreakSoft(""),
		stringLit("]"),
	))

	a := stringLit("a")
	b := stringLit("b")
	c := stringLit("c")
	x := stringLit("x")
	lBrace := stringLit("{")
	rBrace := stringLit("}")

	tests := []struct {
		name        string
		doc         doc
		width       int
		indent      string
		indentWidth int
		want        string
	}{
		{
			name: "nil",
			doc:  nil,
			want: "",
		},
		{
			name: "text",
			doc:  stringLit("hello"),
			want: "hello",
		},
		{
			name: "cat",
			doc:  cat(a, b),
			want: "ab",
		},
		{
			name: "cats_with_nil",
			doc:  cats(a, nil, b, nil, c),
			want: "abc",
		},
		{
			name: "sep",
			doc:  sep(stringLit(", "), a, b, c),
			want: "a, b, c",
		},
		{
			name:  "group_fits",
			doc:   group(cats(a, lineBreakOrSpace, b)),
			width: 80,
			want:  "a b",
		},
		{
			name:  "group_breaks",
			doc:   group(cats(a, lineBreakOrSpace, b)),
			width: 2,
			want: `
a
b`[1:],
		},
		{
			name:   "nest_in_broken_group",
			doc:    group(cats(lBrace, nest(cat(lineBreakOrSpace, x)), lineBreakOrSpace, rBrace)),
			width:  3,
			indent: "\t",
			want: `
{
	x
}`[1:],
		},
		{
			name: "hardline_forces_break",
			doc:  group(cats(a, lineBreakHard, b)),
			want: `
a
b`[1:],
		},
		{
			name:  "switch_mode_flat",
			doc:   group(cats(a, switchMode(stringLit("!"), stringLit("?")), b)),
			width: 80,
			want:  "a?b",
		},
		{
			name:  "switch_mode_broken",
			doc:   group(cats(a, switchMode(stringLit("!"), stringLit("?")), lineBreakOrSpace, b)),
			width: 2,
			want: `
a!
b`[1:],
		},
		{
			name: "blank_line",
			doc:  cats(a, blankLine, b),
			want: `
a

b`[1:],
		},
		{
			name:   "space_indent",
			doc:    group(cats(lBrace, nest(cat(lineBreakOrSpace, x)), lineBreakOrSpace, rBrace)),
			width:  3,
			indent: "    ",
			want: `
{
    x
}`[1:],
		},
		{
			name: "table_alignment",
			doc: table([]row{
				{cells: []doc{stringLit("foo:"), a}},
				{cells: []doc{stringLit("barbaz:"), b}, sep: lineBreakOrComma},
				{cells: []doc{stringLit("x:"), c}, sep: lineBreakOrComma},
			}),
			want: `
foo:    a
barbaz: b
x:      c`[1:],
		},
		{
			name: "table_alignment_flat",
			doc: func() doc {
				rows := []row{
					{cells: []doc{stringLit("foo:"), a}},
					{cells: []doc{stringLit("barbaz:"), b}, sep: lineBreakOrComma},
					{cells: []doc{stringLit("x:"), c}, sep: lineBreakOrComma},
				}
				return group(cats(
					lBrace,
					nest(cat(lineBreakSoft(""), table(rows))),
					lineBreakSoft(""),
					rBrace,
				))
			}(),
			want: "{foo: a, barbaz: b, x: c}",
		},
		{
			name: "trailing_comma_flat",
			doc:  listDoc,
			want: "[1, 2]",
		},
		{
			name:   "trailing_comma_broken",
			doc:    listDoc,
			width:  5,
			indent: "\t",
			want: `
[
	1,
	2,
]`[1:],
		},
		{
			// listDoc inside one nest level: column tracking adds
			// IndentWidth per nest level. With Indent="\t" the inferred
			// IndentWidth is 4, so a [docGroup] at one indent level renders
			// with effective remaining width = width - 4. Width 10
			// gives 6 remaining; "[1, 2]" fits flat at 6.
			name:   "tab_indent_infers_width_4",
			doc:    nest(cat(lineBreakHard, listDoc)),
			width:  10,
			indent: "\t",
			want: `
	[1, 2]`,
		},
		{
			// Same doc, width 10 - but explicit IndentWidth=8 leaves
			// only 2 remaining columns at one indent level, so "[1, 2]"
			// (6) doesn't fit flat and the [docGroup] breaks.
			name:        "explicit_indent_width_overrides_inference",
			doc:         nest(cat(lineBreakHard, listDoc)),
			width:       10,
			indent:      "\t",
			indentWidth: 8,
			want: `
	[
		1,
		2,
	]`,
		},
		{
			// Empty Indent yields IndentWidth=0 by inference, so newlines
			// emit no indentation at all.
			name:   "empty_indent_no_width",
			doc:    group(cats(lBrace, nest(cat(lineBreakHard, x)), lineBreakHard, rBrace)),
			width:  10,
			indent: "",
			want: `
{
x
}`[1:],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Width: tt.width, Indent: tt.indent, IndentWidth: tt.indentWidth}
			got := string(cfg.Render(tt.doc))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
