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
	bigStruct.WriteString("-- a.cue --\nx: {\n")
	for i := range 100 {
		fmt.Fprintf(&bigStruct, "  f%d: %d\n", i, i)
	}
	bigStruct.WriteString("}\nx: ‸\n")

	testCases := []testCase{
		{
			name: "multiple_decls_empty_value",
			archive: `-- a.cue --
x: 5
x: int
x: ‸
`,
			want: "5 & int",
		},
		{
			name: "cursor_within_literal",
			archive: `-- a.cue --
x: 5‸
x: int
`,
			// The declaration containing the cursor renders last: the
			// user can already see it.
			want: "int & 5",
		},
		{
			name: "cursor_declaration_renders_last",
			archive: `-- a.cue --
x: -‸5
x: int
`,
			want: "int & -5",
		},
		{
			name: "references_inlined",
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
			name: "references_inlined_throughout",
			archive: `-- a.cue --
x: y
y: {a: z, b: 4, c: z}
z: 3
x: ‸
`,
			want: "{a: 3, b: 4, c: 3}",
		},
		{
			name: "nested_reference_cycle_stays_unresolved",
			archive: `-- a.cue --
x: y
y: {a: z, b: 4, c: y}
z: 3
x: ‸
`,
			want: "{a: 3, b: 4, c: y}",
		},
		{
			name: "nested_reference_to_the_subject_stays_unresolved",
			archive: `-- a.cue --
x: y
y: {a: z, b: 4, c: x}
z: 3
x: ‸
`,
			want: "{a: 3, b: 4, c: x}",
		},
		{
			name: "references_inlined_within_call_arguments",
			archive: `-- a.cue --
a: 5
‸w: div(a, 2)
`,
			want: "div(5, 2)",
		},
		{
			name:    "too_big",
			archive: bigStruct.String(),
			tooBig:  true,
		},
		{
			name: "references_too_deep_stay_unresolved",
			archive: `-- a.cue --
r1: 1
r2: 2
r3: 3
r4: 4
x: {x: r1, a: {x: r2, b: {x: r3, c: {x: r4}}}}
x: ‸
`,
			// r4 nests too deep in the output (see maxInlineDepth).
			// Note the printer renders the single-field struct at c as a
			// chain.
			want: "{x: 1, a: {x: 2, b: {x: 3, c: x: r4}}}",
		},
		{
			name: "inlining-created_depth_counts_against_the_depth_limit",
			archive: `-- a.cue --
y: {p: z}
z: {q: w}
w: {r: v}
v: {s: u}
u: 7
x: y
x: ‸
`,
			// Each inlined struct nests its contents one level deeper: u
			// would land at depth four. The printer renders the
			// single-field structs as a chain.
			want: "{p: q: r: s: u}",
		},
		{
			name: "reference_chain",
			archive: `-- a.cue --
a: 5
y: a
x: y
x: ‸
`,
			want: "5",
		},
		{
			name: "key_hover",
			archive: `-- a.cue --
x: 5
x: int
‸x: bool
`,
			want: "5 & int & bool",
		},
		{
			name: "reference_hover",
			archive: `-- a.cue --
y: 5
x: y‸
`,
			// A reference on the value spine is just a position within
			// x's value: the subject is x, whose sole (inlined)
			// declaration is this one.
			want: "5",
		},
		{
			name: "selector_reference_hover",
			archive: `-- a.cue --
a: b: 5
x: a.b‸
x: int
`,
			// As above the subject is x, so x's other declarations show
			// too; the declaration containing the cursor renders last.
			want: "int & 5",
		},
		{
			name: "disjunction_preserved_and_parenthesized",
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
			name: "default_marker",
			archive: `-- a.cue --
x: *1 | 2
x: ‸
`,
			want: "*1 | 2",
		},
		{
			name: "cycle_keeps_reference",
			archive: `-- a.cue --
x: x & {a: 1}
x: ‸
`,
			want: "x & {a: 1}",
		},
		{
			name: "builtin_stays_unresolved",
			archive: `-- a.cue --
x: 5
x: in‸t
`,
			want: "5 & int",
		},
		{
			name: "unary_operand_hovers_the_field",
			archive: `-- a.cue --
x: int
x: -‸5
`,
			want: "int & -5",
		},
		{
			name: "arithmetic_operand_hovers_the_field",
			archive: `-- a.cue --
x: int
x: 1 + ‸2
`,
			want: "int & 1 + 2",
		},
		{
			name: "call_paren_interior_yields_nothing",
			archive: `-- a.cue --
x: int
x: len(‸)
`,
			want: "",
		},
		{
			name: "call_argument_literal_yields_nothing",
			archive: `-- a.cue --
x: int
x: len(1 + ‸2)
`,
			want: "",
		},
		{
			name: "call_argument_reference_hovers_the_reference",
			archive: `-- a.cue --
a: 5
x: len(a‸)
`,
			want: "5",
		},
		{
			name: "conjunction_operator_hovers_the_field",
			archive: `-- a.cue --
x: int
x: {a: 1} ‸& {b: 2}
`,
			want: "int & {a: 1} & {b: 2}",
		},
		{
			name: "doc_comments_preserved",
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
			// comment 1, which documents a chained declaration, moves
			// onto the remainder of the chain; comment 2 documents the
			// label y, which the rendering omits, so it is dropped.
			want: `{
  // comment 1
  z: 3
} & {
  // comment 3
  a: int
}`,
		},
		{
			name: "implied_unification",
			archive: `-- a.cue --
a: b: x: int
c: a & {b: x: ‸4}
`,
			want: "int & 4",
		},
		{
			name: "implied_unification_via_multiple_decls",
			archive: `-- a.cue --
y: b: int
x: y
x: b: ‸4
`,
			want: "int & 4",
		},
		{
			name: "list_elements_merge",
			archive: `-- a.cue --
l: [7]
l: [‸8, 9]
`,
			want: "7 & 8",
		},
		{
			name: "cross_file_decls",
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
			name: "comprehension_body_field",
			archive: `-- a.cue --
p: true
x: {if p {a: ‸1}}
x: {a: int}
`,
			// The a declared in the comprehension body and the a in x's
			// second declaration merge into one node; the declaration
			// containing the cursor renders last.
			want: "int & 1",
		},
		{
			name: "if_condition_yields_nothing",
			archive: `-- a.cue --
x: {if ‸true {a: 1}}
`,
			want: "",
		},
		{
			name: "for_source_reference_hovers_the_reference",
			archive: `-- a.cue --
l: [1]
x: {for v in l‸ {a: v}}
`,
			want: "[1]",
		},
		{
			name: "interpolation_literal_segment_hovers_the_field",
			archive: `-- a.cue --
y: "b"
x: int
x: "a-\(y)-c‸"
`,
			// The reference within the interpolation is inlined too.
			want: `int & "a-\("b")-c"`,
		},
		{
			name: "interpolation_expression_reference_hovers_the_reference",
			archive: `-- a.cue --
y: "b"
x: "a-\(y‸)-c"
`,
			want: `"b"`,
		},
		{
			name: "pattern_constraint_value_yields_nothing",
			archive: `-- a.cue --
x: {[string]: ‸1}
`,
			want: "",
		},
		{
			name: "let_value_yields_nothing",
			archive: `-- a.cue --
x: {let y = ‸2, a: 1}
`,
			want: "",
		},
		{
			name: "reference_within_let_expression_hovers_the_reference",
			archive: `-- a.cue --
b: 3
x: {let y = b‸, a: 1}
`,
			want: "3",
		},
		{
			name: "single_declaration",
			archive: `-- a.cue --
x: ‸5
`,
			want: "5",
		},
		{
			name: "selector_prefix_hovers_the_field",
			archive: `-- a.cue --
a: b: 5
x: a‸.b
x: int
`,
			want: "int & 5",
		},
		{
			name: "index_with_literal_index_inlined",
			archive: `-- a.cue --
l: [7]
x: l[0]‸
x: int
`,
			want: "int & 7",
		},
		{
			name: "index_prefix_hovers_the_field",
			archive: `-- a.cue --
l: [7]
x: l‸[0]
`,
			want: "7",
		},
		{
			name: "non-literal_index_stays_a_reference",
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
			name: "unary_operator_hovers_the_field",
			archive: `-- a.cue --
x: int
x: ‸-5
`,
			want: "int & -5",
		},
		{
			name: "paren_interior_hovers_the_field",
			archive: `-- a.cue --
x: int
x: (‸5)
`,
			want: "int & (5)",
		},
		{
			name: "paren_itself_hovers_the_field",
			archive: `-- a.cue --
x: int
x: ‸(5)
`,
			want: "int & (5)",
		},
		{
			name: "callee_hovers_the_field",
			archive: `-- a.cue --
x: int
x: le‸n(x)
`,
			// The reference to x within the call argument is a cycle,
			// and stays as written.
			want: "int & len(x)",
		},
		{
			name: "struct_interior_whitespace_hovers_the_field",
			archive: `-- a.cue --
x: int
x: {‸ a: 1}
`,
			want: "int & {a: 1}",
		},
		{
			name: "list_brackets_hover_the_field",
			archive: `-- a.cue --
l: [7]
l: ‸[8, 9]
`,
			want: "[7] & [8, 9]",
		},
		{
			name: "ellipsis_type_hovers_the_field",
			archive: `-- a.cue --
l: [...in‸t]
`,
			want: "[...int]",
		},
		{
			name: "ellipsis_dots_hover_the_field",
			archive: `-- a.cue --
l: [.‸..int]
`,
			want: "[...int]",
		},
		{
			name: "embedding_inlined_within_a_struct",
			archive: `-- a.cue --
y: 5
x: {y‸, a: 1}
`,
			want: "{5, a: 1}",
		},
		{
			name: "alias_expression_in_a_list_element",
			archive: `-- a.cue --
l: [A=‸5, A]
`,
			want: "A=5",
		},
		{
			name: "alias_whitespace_in_a_list_element",
			archive: `-- a.cue --
l: [A=‸ 5, A]
`,
			want: "A=5",
		},
		{
			name: "pattern_constraint_label_yields_nothing",
			archive: `-- a.cue --
x: {[str‸ing]: 1}
`,
			want: "",
		},
		{
			name: "pattern_constraint_whitespace_yields_nothing",
			archive: `-- a.cue --
x: {[string]: ‸ 1}
`,
			want: "",
		},
		{
			name: "comprehension_clause_keyword_hovers_the_field",
			archive: `-- a.cue --
x: {‸if true {a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "comprehension_body_brace_hovers_the_field",
			archive: `-- a.cue --
x: {if true ‸{a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "between_comprehension_clause_and_body_hovers_the_field",
			archive: `-- a.cue --
x: {if true ‸ {a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "comprehension_body_whitespace_hovers_the_field",
			archive: `-- a.cue --
x: {if true {‸ a: 1}}
`,
			want: "{if true {a: 1}}",
		},
		{
			name: "fallback_body_whitespace_hovers_the_field",
			archive: `-- a.cue --
@experiment(try)
x: {if true {a: 1} else {‸ b: 2}}
`,
			want: "{if true {a: 1} else {b: 2}}",
		},
		{
			name: "try_expression_reference_hovers_the_reference",
			archive: `-- a.cue --
@experiment(try)
b: 5
x: {try v = b‸ {a: v}}
`,
			want: "5",
		},
		{
			name: "top_level_embedding_reference_hovers_the_reference",
			archive: `-- a.cue --
y: 5
‸y
`,
			// A top-level embedding has no enclosing field to serve as
			// the subject; the reference still resolves.
			want: "5",
		},
		{
			name: "cross_file_decl_ordering",
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
			name: "comprehension_binding_reference_stays_unresolved",
			archive: `-- a.cue --
l: [7]
x: {for v in l {a: v‸}}
`,
			// The for clause's binding declares no value of its own.
			want: "v",
		},
		{
			name: "reference_through_import_inlined",
			archive: `-- a.cue --
package a

import "b"

x: b.y
x: ‸
-- b.cue --
package b

y: 5
y: int
`,
			want: "5 & int",
		},
		{
			name: "implied_unification_through_import",
			archive: `-- a.cue --
package a

import "b"

x: b.#S
x: {n: ‸4}
-- b.cue --
package b

#S: {n: int}
`,
			// x's n is unified with #S's n through the import; the
			// declaration containing the cursor renders last.
			want: "int & 4",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ar := txtar.Parse([]byte(tc.archive))
			qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

			filesByPkg := make(map[string][]*ast.File)
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
				pkgName := fileAst.PackageName()
				filesByPkg[pkgName] = append(filesByPkg[pkgName], fileAst)
			}
			qt.Assert(t, qt.Not(qt.Equals(cursorOffset, -1)), qt.Commentf("no cursor marker found"))

			// Each package in the archive is importable by the name
			// of its package clause. Cross-package resolution is
			// lazy, so it does not matter that the evaluators are
			// created in arbitrary (map) order.
			evalByPkgName := make(map[string]*eval.Evaluator)
			evalByFilename := make(map[string]*eval.Evaluator)
			importCanonicalisation := make(map[string]ast.ImportPath)
			forPackage := func(importPath ast.ImportPath) *eval.Evaluator {
				return evalByPkgName[importPath.String()]
			}
			for pkgName, pkgFiles := range filesByPkg {
				importCanonicalisation[pkgName] = ast.ImportPath{Path: pkgName}.Canonical()
				e := eval.New(eval.Config{
					IP:                     importCanonicalisation[pkgName],
					ImportCanonicalisation: importCanonicalisation,
					ForPackage:             forPackage,
				}, pkgFiles...)
				evalByPkgName[pkgName] = e
				for _, f := range pkgFiles {
					evalByFilename[f.Filename] = e
				}
			}

			fe := evalByFilename[cursorFilename].ForFile(cursorFilename)
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
