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

package hover_test

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/lsp/hover"
	"cuelang.org/go/internal/pretty"
	"cuelang.org/go/unstable/lsp/eval"
)

// The cursor position within a test case's archive is indicated by
// the marker, which is stripped from the source before parsing.
const marker = "‸"

func TestValueForOffset(t *testing.T) {
	type testCase struct {
		name    string
		archive string
		// want is the expected rendering of the unified value at the
		// marker; "" means no value is expected.
		want string
		// tooBig indicates that rendering is expected to be abandoned
		// because the result would exceed the node budget.
		tooBig bool
	}

	var bigStruct strings.Builder
	bigStruct.WriteString("-- a.cue --\nx: {")
	for i := range 100 {
		fmt.Fprintf(&bigStruct, "f%d: %d, ", i, i)
	}
	bigStruct.WriteString("}\nx: ‸\n")

	testCases := []testCase{
		{
			name: "multiple decls, empty value",
			archive: `-- a.cue --
x: 5
x: int
x: ‸
`,
			want: "5 & int",
		},
		{
			name: "cursor within literal",
			archive: `-- a.cue --
x: 5‸
x: int
`,
			// The declaration containing the cursor renders last: the
			// user can already see it.
			want: "int & 5",
		},
		{
			name: "cursor declaration renders last",
			archive: `-- a.cue --
x: -‸5
x: int
`,
			want: "int & -5",
		},
		{
			name: "references inlined",
			archive: `-- a.cue --
y: 5
x: y
z: int
x: z
x: ‸
`,
			want: "5 & int",
		},
		{
			name: "references inlined throughout",
			archive: `-- a.cue --
x: y
y: {a: z, b: 4, c: z}
z: 3
x: ‸
`,
			want: "{a: 3, b: 4, c: 3}",
		},
		{
			name: "nested reference cycle stays unresolved",
			archive: `-- a.cue --
x: y
y: {a: z, b: 4, c: y}
z: 3
x: ‸
`,
			want: "{a: 3, b: 4, c: y}",
		},
		{
			name: "nested reference to the subject stays unresolved",
			archive: `-- a.cue --
x: y
y: {a: z, b: 4, c: x}
z: 3
x: ‸
`,
			want: "{a: 3, b: 4, c: x}",
		},
		{
			name: "references inlined within call arguments",
			archive: `-- a.cue --
a: 5
‸w: div(a, 2)
`,
			want: "div(5, 2)",
		},
		{
			name:    "too big",
			archive: bigStruct.String(),
			tooBig:  true,
		},
		{
			name: "references too deep stay unresolved",
			archive: `-- a.cue --
r1: 1
r2: 2
r3: 3
r4: 4
x: {x: r1, a: {x: r2, b: {x: r3, c: {x: r4}}}}
x: ‸
`,
			// r4 nests too deep in the output (see maxInlineDepth).
			// Note the printer renders the single-field struct at c
			// as a chain.
			want: "{x: 1, a: {x: 2, b: {x: 3, c: x: r4}}}",
		},
		{
			name: "inlining-created depth counts against the depth limit",
			archive: `-- a.cue --
y: {p: z}
z: {q: w}
w: {r: v}
v: {s: u}
u: 7
x: y
x: ‸
`,
			// Each inlined struct nests its contents one level
			// deeper: u would land at depth four. The printer
			// renders the single-field structs as a chain.
			want: "{p: q: r: s: u}",
		},
		{
			name: "reference chain",
			archive: `-- a.cue --
a: 5
y: a
x: y
x: ‸
`,
			want: "5",
		},
		{
			name: "key hover",
			archive: `-- a.cue --
x: 5
x: int
‸x: bool
`,
			want: "5 & int & bool",
		},
		{
			name: "reference hover",
			archive: `-- a.cue --
y: 5
x: y‸
`,
			// A reference on the value spine is just a position
			// within x's value: the subject is x, whose sole
			// (inlined) declaration is this one.
			want: "5",
		},
		{
			name: "selector reference hover",
			archive: `-- a.cue --
a: b: 5
x: a.b‸
x: int
`,
			// As above the subject is x, so x's other declarations
			// show too; the declaration containing the cursor
			// renders last.
			want: "int & 5",
		},
		{
			name: "disjunction preserved and parenthesized",
			archive: `-- a.cue --
a: 1
b: 2
x: a | b
x: int
x: ‸
`,
			want: "(1 | 2) & int",
		},
		{
			name: "default marker",
			archive: `-- a.cue --
x: *1 | 2
x: ‸
`,
			want: "*1 | 2",
		},
		{
			name: "cycle keeps reference",
			archive: `-- a.cue --
x: x & {a: 1}
x: ‸
`,
			want: "x & {a: 1}",
		},
		{
			name: "builtin stays unresolved",
			archive: `-- a.cue --
x: 5
x: in‸t
`,
			want: "5 & int",
		},
		{
			name: "unary operand hovers the field",
			archive: `-- a.cue --
x: int
x: -‸5
`,
			want: "int & -5",
		},
		{
			name: "arithmetic operand hovers the field",
			archive: `-- a.cue --
x: int
x: 1 + ‸2
`,
			want: "int & 1 + 2",
		},
		{
			name: "call paren interior yields nothing",
			archive: `-- a.cue --
x: int
x: len(‸)
`,
			want: "",
		},
		{
			name: "call argument literal yields nothing",
			archive: `-- a.cue --
x: int
x: len(1 + ‸2)
`,
			want: "",
		},
		{
			name: "call argument reference hovers the reference",
			archive: `-- a.cue --
a: 5
x: len(a‸)
`,
			want: "5",
		},
		{
			name: "conjunction operator hovers the field",
			archive: `-- a.cue --
x: int
x: {a: 1} ‸& {b: 2}
`,
			want: "int & {a: 1} & {b: 2}",
		},
		{
			name: "doc comments preserved",
			archive: `-- a.cue --
x: y
// comment 1
y: z: 3
// comment 2
y: {
	// comment 3
	a: int
}
x: ‸
`,
			// comment 3 is copied with the field it documents, and
			// comment 1, which documents a chained declaration,
			// moves onto the remainder of the chain; comment 2
			// documents the label y, which the rendering omits, so
			// it is dropped.
			want: `{
  // comment 1
  z: 3
} & {
  // comment 3
  a: int
}`,
		},
		{
			name: "implied unification",
			archive: `-- a.cue --
a: b: x: int
c: a & {b: x: ‸4}
`,
			want: "int & 4",
		},
		{
			name: "implied unification via multiple decls",
			archive: `-- a.cue --
y: b: int
x: y
x: b: ‸4
`,
			want: "int & 4",
		},
		{
			name: "list elements merge",
			archive: `-- a.cue --
l: [7]
l: [‸8, 9]
`,
			want: "7 & 8",
		},
		{
			name: "cross file decls",
			archive: `-- a.cue --
package p
x: 5
-- b.cue --
package p
x: ‸int
`,
			want: "5 & int",
		},
		{
			name: "comprehension body field",
			archive: `-- a.cue --
p: true
x: {if p {a: ‸1}}
x: {a: int}
`,
			// The a declared in the comprehension body and the a in
			// x's second declaration merge into one node; the
			// declaration containing the cursor renders last.
			want: "int & 1",
		},
		{
			name: "if condition yields nothing",
			archive: `-- a.cue --
x: {if ‸true {a: 1}}
`,
			want: "",
		},
		{
			name: "for source reference hovers the reference",
			archive: `-- a.cue --
l: [1]
x: {for v in l‸ {a: v}}
`,
			want: "[1]",
		},
		{
			name: "interpolation literal segment hovers the field",
			archive: `-- a.cue --
y: "b"
x: int
x: "a-\(y)-c‸"
`,
			// The reference within the interpolation is inlined too.
			want: `int & "a-\("b")-c"`,
		},
		{
			name: "interpolation expression reference hovers the reference",
			archive: `-- a.cue --
y: "b"
x: "a-\(y‸)-c"
`,
			want: `"b"`,
		},
		{
			name: "pattern constraint value yields nothing",
			archive: `-- a.cue --
x: {[string]: ‸1}
`,
			want: "",
		},
		{
			name: "let value yields nothing",
			archive: `-- a.cue --
x: {let y = ‸2, a: 1}
`,
			want: "",
		},
		{
			name: "reference within let expression hovers the reference",
			archive: `-- a.cue --
b: 3
x: {let y = b‸, a: 1}
`,
			want: "3",
		},
		{
			name: "single declaration",
			archive: `-- a.cue --
x: ‸5
`,
			want: "5",
		},
		{
			name: "selector prefix hovers the field",
			archive: `-- a.cue --
a: b: 5
x: a‸.b
x: int
`,
			want: "int & 5",
		},
		{
			name: "index with literal index inlined",
			archive: `-- a.cue --
l: [7]
x: l[0]‸
x: int
`,
			want: "int & 7",
		},
		{
			name: "index prefix hovers the field",
			archive: `-- a.cue --
l: [7]
x: l‸[0]
`,
			want: "7",
		},
		{
			name: "non-literal index stays a reference",
			archive: `-- a.cue --
i: 0
l: [7]
x: l[i‸]
`,
			// The index expression itself is not inlined, but the
			// references within it are.
			want: "[7][0]",
		},
		{
			name: "unary operator hovers the field",
			archive: `-- a.cue --
x: int
x: ‸-5
`,
			want: "int & -5",
		},
		{
			name: "paren interior hovers the field",
			archive: `-- a.cue --
x: int
x: (‸5)
`,
			want: "int & (5)",
		},
		{
			name: "paren itself hovers the field",
			archive: `-- a.cue --
x: int
x: ‸(5)
`,
			want: "int & (5)",
		},
		{
			name: "callee hovers the field",
			archive: `-- a.cue --
x: int
x: le‸n(x)
`,
			// The reference to x within the call argument is a cycle,
			// and stays as written.
			want: "int & len(x)",
		},
		{
			name: "struct interior whitespace hovers the field",
			archive: `-- a.cue --
x: int
x: {‸ a: 1}
`,
			want: "int & {a: 1}",
		},
		{
			name: "list brackets hover the field",
			archive: `-- a.cue --
l: [7]
l: ‸[8, 9]
`,
			want: "[7] & [8, 9]",
		},
		{
			name: "ellipsis type hovers the field",
			archive: `-- a.cue --
l: [...in‸t]
`,
			want: "[...int]",
		},
		{
			name: "ellipsis dots hover the field",
			archive: `-- a.cue --
l: [.‸..int]
`,
			want: "[...int]",
		},
		{
			name: "embedding inlined within a struct",
			archive: `-- a.cue --
y: 5
x: {y‸, a: 1}
`,
			want: "{5, a: 1}",
		},
		{
			name: "alias expression in a list element",
			archive: `-- a.cue --
l: [A=‸5, A]
`,
			want: "A=5",
		},
		{
			name: "alias whitespace in a list element",
			archive: `-- a.cue --
l: [A=‸ 5, A]
`,
			want: "A=5",
		},
		{
			name: "pattern constraint label yields nothing",
			archive: `-- a.cue --
x: {[str‸ing]: 1}
`,
			want: "",
		},
		{
			name: "pattern constraint whitespace yields nothing",
			archive: `-- a.cue --
x: {[string]: ‸ 1}
`,
			want: "",
		},
		{
			name: "comprehension clause keyword hovers the field",
			archive: `-- a.cue --
x: {‸if true {a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "comprehension body brace hovers the field",
			archive: `-- a.cue --
x: {if true ‸{a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "between comprehension clause and body hovers the field",
			archive: `-- a.cue --
x: {if true ‸ {a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "comprehension body whitespace hovers the field",
			archive: `-- a.cue --
x: {if true {‸ a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "fallback body whitespace hovers the field",
			archive: `-- a.cue --
@experiment(try)
x: {if true {a: 1} else {‸ b: 2}}
`,
			want: "{if true {a: 1} else {b: 2}}",
		},
		{
			name: "try expression reference hovers the reference",
			archive: `-- a.cue --
@experiment(try)
b: 5
x: {try v = b‸ {a: v}}
`,
			want: "5",
		},
		{
			name: "top level embedding reference hovers the reference",
			archive: `-- a.cue --
y: 5
‸y
`,
			// A top-level embedding has no enclosing field to serve
			// as the subject; the reference still resolves.
			want: "5",
		},
		{
			name: "cross file decl ordering",
			archive: `-- a.cue --
package p
x: true
-- b.cue --
package p
x: int
x: ‸
`,
			want: "true & int",
		},
		{
			name: "comprehension binding reference stays unresolved",
			archive: `-- a.cue --
l: [7]
x: {for v in l {a: v‸}}
`,
			// The for clause's binding declares no value of its own.
			want: "v",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ar := txtar.Parse([]byte(tc.archive))
			qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

			var files []*ast.File
			cursorFilename := ""
			cursorOffset := -1
			for _, fh := range ar.Files {
				data := fh.Data
				if i := bytes.Index(data, []byte(marker)); i >= 0 {
					qt.Assert(t, qt.Equals(cursorOffset, -1),
						qt.Commentf("multiple cursor markers found"))
					cursorFilename = fh.Name
					cursorOffset = i
					data = slices.Concat(data[:i], data[i+len(marker):])
				}
				// Parse errors are tolerated: several cases exercise
				// incomplete declarations such as `x: `.
				fileAst, _ := parser.ParseFile(fh.Name, data, parser.ParseComments)
				qt.Assert(t, qt.IsNotNil(fileAst))
				fileAst.Pos().File().SetContent(data)
				files = append(files, fileAst)
			}
			qt.Assert(t, qt.Not(qt.Equals(cursorOffset, -1)),
				qt.Commentf("no cursor marker found"))

			e := eval.New(eval.Config{}, files...)
			fe := e.ForFile(cursorFilename)
			qt.Assert(t, qt.IsNotNil(fe))

			expr, tooBig := hover.ValueForOffset(fe, cursorOffset)
			qt.Assert(t, qt.Equals(tooBig, tc.tooBig))
			got := ""
			if expr != nil {
				// The same config as [cache.Workspace.Hover] uses.
				b, err := (&pretty.Config{Indent: "  "}).Node(expr)
				qt.Assert(t, qt.IsNil(err))
				got = strings.TrimSpace(string(b))
			}
			qt.Assert(t, qt.Equals(got, tc.want))
		})
	}
}
