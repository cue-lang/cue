// Copyright 2018 The CUE Authors
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

package format_test

// TODO: port more of the tests of go/printer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/tdtest"
)

const debug = false

func TestFiles(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "format",
	}
	test.Run(t, func(t *cuetxtar.Test) {
		var opts []format.Option
		if t.HasTag("simplify") {
			opts = append(opts, format.Simplify())
		}
		if version, ok := t.Value("version"); ok {
			opts = append(opts, format.Version(version))
		}
		// TODO(mvdan): note that this option is not exposed in the API,
		// nor does it seem to be actually tested in any of the txtar testdata files.
		// if t.HasTag("sort-imports") {
		// 	opts = append(opts, format.sortImportsOption())
		// }

		for _, f := range t.Archive.Files {
			if !strings.HasSuffix(f.Name, ".input") {
				continue
			}
			res, err := format.Source(f.Data, opts...)
			qt.Assert(t, qt.IsNil(err))

			// make sure formatted output is syntactically correct
			_, err = parser.ParseFile("", res, parser.AllErrors)
			qt.Assert(t, qt.IsNil(err))

			goldenFile := strings.TrimSuffix(f.Name, ".input") + ".golden"
			t.Writer(goldenFile).Write(res)

			// Formatting is idempotent: re-formatting the golden output
			// leaves it unchanged.
			again, err := format.Source(res, opts...)
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.Equals(string(again), string(res)),
				qt.Commentf("%s is not formatted idempotently", goldenFile))
		}
	})
}

// TestPostfixSpread checks that both formatters keep the postfix ...
// operator after every primary expression form the parser accepts, and
// that a comment beside the operator survives. The archive
// testdata/spread.txtar covers the same forms in context, but only for the
// formatter which the formatv2 experiment selects.
func TestPostfixSpread(t *testing.T) {
	// Each source is already in canonical form, so formatting it must be a
	// no-op for either formatter.
	sources := []string{
		"a: {b...}\n",
		"a: {b.c...}\n",
		"a: {b[0]...}\n",
		"a: {b[1:2]...}\n",
		"a: {f()...}\n",
		"a: {(b & c)...}\n",
		"a: {(b | c)...}\n",
		"a: {{x: 1}...}\n",
		"a: {[1, 2]...}\n",
		"a: {\"\\(b)\"...}\n",
		"a: {b......}\n",
		"a: {b... // note\n}\n",
	}

	qt.Assert(t, qt.IsNil(cueexperiment.Init()))
	// Init is guarded by sync.Once, so overriding Flags directly here is not
	// undone by the format functions calling cueexperiment.Init again.
	defer func(orig bool) { cueexperiment.Flags.FormatV2 = orig }(cueexperiment.Flags.FormatV2)

	for _, v2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("formatv2=%v", v2), func(t *testing.T) {
			cueexperiment.Flags.FormatV2 = v2
			for _, src := range sources {
				t.Run(strings.TrimSuffix(src, "\n"), func(t *testing.T) {
					got, err := format.Source([]byte(src))
					qt.Assert(t, qt.IsNil(err))
					qt.Check(t, qt.Equals(string(got), src))
				})
			}
		})
	}
}

// TestFuncParamComments checks that both formatters keep a comment placed
// after a parameter's "=" or ":", or on its own line before the default or
// the constraint, and that the result parses back and is stable. The
// parser attaches the own-line comments to the expression that follows,
// not to the parameter, and a comment after the last parameter would
// swallow the closing parenthesis on a shared line.
func TestFuncParamComments(t *testing.T) {
	const head = "@experiment(functions)\n\n"
	sources := []struct{ src, comment string }{{
		src:     head + "f: func(a: int = // after the bind token\n\t2) -> int: a\n",
		comment: "// after the bind token",
	}, {
		src:     head + "f: func(a: int =\n\t// before the default\n\t2) -> int: a\n",
		comment: "// before the default",
	}, {
		src:     head + "f: func(a:\n\t// before the constraint\n\tint = 2, b: int) -> int: a\n",
		comment: "// before the constraint",
	}, {
		src:     head + "f: func(a: int = 2 // after the last parameter\n\t) -> int: a\n",
		comment: "// after the last parameter",
	}, {
		src:     head + "f: func(a: int, // after the first parameter\n\tb: int = 2) -> int: a\n",
		comment: "// after the first parameter",
	}}

	qt.Assert(t, qt.IsNil(cueexperiment.Init()))
	// Init is guarded by sync.Once, so overriding Flags directly here is not
	// undone by the format functions calling cueexperiment.Init again.
	defer func(orig bool) { cueexperiment.Flags.FormatV2 = orig }(cueexperiment.Flags.FormatV2)

	for _, v2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("formatv2=%v", v2), func(t *testing.T) {
			cueexperiment.Flags.FormatV2 = v2
			for _, tc := range sources {
				t.Run(tc.comment, func(t *testing.T) {
					got, err := format.Source([]byte(tc.src))
					qt.Assert(t, qt.IsNil(err))
					_, err = parser.ParseFile("", got, parser.AllErrors)
					qt.Assert(t, qt.IsNil(err), qt.Commentf("output:\n%s", got))
					qt.Check(t, qt.StringContains(string(got), tc.comment))

					again, err := format.Source(got)
					qt.Assert(t, qt.IsNil(err))
					qt.Check(t, qt.Equals(string(again), string(got)))
				})
			}
		})
	}
}

// TestClosingBracketComma checks that a bracketed list whose first element
// starts on its own line closes on its own line too, after a trailing comma,
// even when the source closes the list on the line of its last element.
// Without the comma, formatting the output again would add one.
func TestClosingBracketComma(t *testing.T) {
	const head = "@experiment(functions)\n\n"
	tests := []struct{ name, in, want string }{{
		name: "list",
		in:   head + "x: [\n\t1, 2]\n",
		want: head + "x: [\n\t1, 2,\n]\n",
	}, {
		name: "call",
		in:   head + "x: f(\n\t1, 2)\n",
		want: head + "x: f(\n\t1, 2,\n)\n",
	}, {
		name: "named call",
		in:   head + "x: f(\n\ta: 1, b: 2)\n",
		want: head + "x: f(\n\ta: 1, b: 2,\n)\n",
	}, {
		name: "func",
		in:   head + "x: func(\n\ta: int, b: int) -> int\n",
		want: head + "x: func(\n\ta: int, b: int,\n) -> int\n",
	}, {
		// The list opens on the line of its first element, so it
		// closes on the line of its last one, without a comma.
		name: "open inline",
		in:   head + "x: [1,\n\t2]\n",
		want: head + "x: [1,\n\t2]\n",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := format.Source([]byte(tc.in))
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.Equals(string(got), tc.want))
		})
	}
}

// TestCommentPlacement checks where the formatter places comments which
// the parser attaches to nodes in unusual positions, such as inside a list
// element, a label, or an operand. Each comment must stay where it parses
// back to the same node, without swallowing the tokens which follow it on
// its line.
func TestCommentPlacement(t *testing.T) {
	const head = "@experiment(functions)\n\n"
	tests := []struct{ name, in, want string }{{
		// A `//` comment ending the row of a list element, a call
		// argument, or a struct element is followed by a line break, so
		// that it does not swallow the next element or the closer. Such
		// a comment may have moved there from inside the element, which
		// then ends on another line than in the source.
		name: "comprehension",
		in:   head + "x: [\n\tif a // c\n\t{\n\t\t\"a\"\n\t}, \"b\",\n]\n",
		want: head + "x: [\n\tif a // c\n\t{\n\t\t\"a\"\n\t}, \"b\",\n]\n",
	}, {
		name: "last comprehension",
		in:   head + "x: [if a // c\n\t{b}]\n",
		want: head + "x: [if a // c\n{b}]\n",
	}, {
		name: "unary",
		in:   head + "x: [- // c\n\ty, z]\n",
		want: head + "x: [- // c\n\ty, z]\n",
	}, {
		name: "index",
		in:   head + "x: [y[0 // c\n], z]\n",
		want: head + "x: [y[0 // c\n], z]\n",
	}, {
		name: "before ellipsis",
		in:   head + "x: [1, ... // c\n]\n",
		want: head + "x: [1, // c\n\t...,\n]\n",
	}, {
		name: "ellipsis type",
		in:   head + "x: [1, ...- // c\n\tint]\n",
		want: head + "x: [1, ...- // c\n\tint]\n",
	}, {
		name: "operand",
		in:   head + "x: [a & - // c\n\tb]\n",
		want: head + "x: [a & - // c\n\tb]\n",
	}, {
		name: "call unary",
		in:   head + "x: f(a, - // c\n\ty)\n",
		want: head + "x: f(a, - // c\n\ty)\n",
	}, {
		name: "call index",
		in:   head + "x: f(a, y[0 // c\n])\n",
		want: head + "x: f(a, y[0 // c\n])\n",
	}, {
		name: "named call unary",
		in:   head + "x: f(a: - // c\n\ty)\n",
		want: head + "x: f(a: - // c\n\ty)\n",
	}, {
		name: "struct operand",
		in:   head + "x: {a & - // c\n\tb}\n",
		want: head + "x: {a & - // c\n\tb}\n",
	}, {
		// A comment on its own line after the last call argument stays
		// there, rather than moving into the argument.
		name: "after struct argument",
		in:   head + "x: f(\n\t{a: 1},\n\t// c\n)\n",
		want: head + "x: f(\n\t{a: 1},\n\t// c\n)\n",
	}, {
		name: "after list argument",
		in:   head + "x: f(\n\t[1],\n\t// c\n)\n",
		want: head + "x: f(\n\t[1],\n\t// c\n)\n",
	}, {
		name: "after empty struct argument",
		in:   head + "x: f(\n\t{},\n\t// c\n)\n",
		want: head + "x: f(\n\t{},\n\t// c\n)\n",
	}, {
		name: "after binary argument",
		in:   head + "x: f(\n\ta & b,\n\t// c\n)\n",
		want: head + "x: f(\n\ta & b,\n\t// c\n)\n",
	}, {
		name: "after hugged struct argument",
		in:   head + "x: f({a: 1}\n\t// c\n)\n",
		want: head + "x: f({a: 1},\n\t// c\n)\n",
	}, {
		// The parser attaches a doc comment before the inner label of a
		// braceless chain to the label, which has no comment slot of its
		// own, so the chain gains braces to hold the comment.
		name: "chain label doc",
		in:   "a:\n\t// comment\n\tc: 1\n",
		want: "a: {\n\t// comment\n\tc: 1\n}\n",
	}, {
		name: "chain label doc nested",
		in:   "a: b:\n\t// comment\n\tc: 1\n",
		want: "a: b: {\n\t// comment\n\tc: 1\n}\n",
	}, {
		name: "chain string label doc",
		in:   "a:\n\t// comment\n\t\"b\": 1\n",
		want: "a: {\n\t// comment\n\t\"b\": 1\n}\n",
	}, {
		name: "chain pattern label doc",
		in:   "a:\n\t// comment\n\t[string]: 1\n",
		want: "a: {\n\t// comment\n\t[string]: 1\n}\n",
	}, {
		name: "chain paren label doc",
		in:   "a:\n\t// comment\n\t(k): 1\n",
		want: "a: {\n\t// comment\n\t(k): 1\n}\n",
	}, {
		name: "chain interpolation label doc",
		in:   "a:\n\t// comment\n\t\"\\(k)\": 1\n",
		want: "a: {\n\t// comment\n\t\"\\(k)\": 1\n}\n",
	}, {
		// The parser attaches a comment beside a `\(` or `)` delimiter to
		// the string fragment ending or starting with it, so the comment
		// forces a line break inside a single-line string.
		name: "interpolation open",
		in:   "a: \"\\( // c\n\tx)\"\n",
		want: "a: \"\\( // c\n\tx)\"\n",
	}, {
		name: "interpolation close",
		in:   "a: \"\\(x // c\n\t)\"\n",
		want: "a: \"\\(x // c\n)\"\n",
	}, {
		name: "interpolation label",
		in:   "a: {\n\t\"\\( // c\n\t\tk)\": 1\n}\n",
		want: "a: {\n\t\"\\( // c\n\t\tk)\": 1\n}\n",
	}, {
		name: "interpolation doc",
		in:   "a: \"x\\(\n// c\ny)z\"\n",
		want: "a: \"x\\(\n\t// c\n\ty)z\"\n",
	}, {
		// A line comment after the expression of a pattern label puts
		// the closing bracket on the next line.
		name: "pattern label line",
		in:   "a: {\n\t[string // c\n\t]: 1\n}\n",
		want: "a: {\n\t[string // c\n\t]: 1\n}\n",
	}, {
		name: "pattern label line inline struct",
		in:   "a: {[string // c\n]: 1}\n",
		want: "a: {[string // c\n]: 1}\n",
	}, {
		name: "pattern label line chain",
		in:   "a: [string // c\n]: 1\n",
		want: "a:\n\t[string // c\n\t]: 1\n",
	}, {
		// Comments inside the brackets of an index expression stay there,
		// as they would swallow a postfix operator after the brackets.
		name: "index line before close",
		in:   "a: b.c[d // c\n][1:2]\n",
		want: "a: b.c[d // c\n][1:2]\n",
	}, {
		name: "index line after open",
		in:   "a: b[ // c\n\td].e\n",
		want: "a: b[ // c\n\td].e\n",
	}, {
		name: "for value doc",
		in:   "a: [\n\tfor k,\n\t// c\n\tv in y {k},\n]\n",
		want: "a: [\n\tfor k,\n\t\t// c\n\t\tv in y {k},\n]\n",
	}, {
		name: "selector doc",
		in:   "a: b.\n\t// c\n\tc\n",
		want: "a: b.\n\t// c\n\tc\n",
	}, {
		// A doc comment before an operand which follows a token on the
		// same line stays on a line of its own, after that token.
		name: "let doc",
		in:   "let X =\n// c\n1\n",
		want: "let X =\n\t// c\n\t1\n",
	}, {
		name: "paren label doc",
		in:   "a: {\n\t(\n\t// c\n\tk): 1\n}\n",
		want: "a: {\n\t(\n\t\t// c\n\t\tk): 1\n}\n",
	}, {
		name: "nested paren label doc",
		in:   "a: {\n\t(\n\t// c\n\t(k)): 1\n}\n",
		want: "a: {\n\t(\n\t\t// c\n\t\t(k)): 1\n}\n",
	}, {
		name: "nested paren doc",
		in:   "a: (\n\t// c\n\t(k))\n",
		want: "a: (\n\t// c\n\t(k))\n",
	}, {
		name: "pattern label doc",
		in:   "a: {\n\t[\n\t// c\n\tstring]: 1\n}\n",
		want: "a: {\n\t[\n\t\t// c\n\t\tstring]: 1\n}\n",
	}, {
		name: "pattern label unary doc",
		in:   "a: {\n\t[!\n\t// c\n\tx]: 1\n}\n",
		want: "a: {\n\t[!\n\t\t// c\n\t\tx]: 1\n}\n",
	}, {
		name: "unary doc",
		in:   "a: !\n\t// c\n\tx\n",
		want: "a: !\n\t// c\n\tx\n",
	}, {
		name: "slice low doc",
		in:   "a: x[\n\t// c\n\t1:2]\n",
		want: "a: x[\n\t// c\n\t1:2]\n",
	}, {
		name: "slice high doc",
		in:   "a: x[1:\n\t// c\n\t2]\n",
		want: "a: x[1:\n\t// c\n\t2]\n",
	}, {
		// A comment between a unary operator and its operand stays there,
		// with the operand on the next line, rather than moving after the
		// operand, where it could swallow the tokens which follow.
		name: "unary operand binary",
		in:   "a: - // c\n\tx + y\n",
		want: "a: - // c\n\tx + y\n",
	}, {
		name: "unary operand default disjunct",
		in:   "a: * // c\n\t1 | 2\n",
		want: "a: * // c\n\t1 | 2\n",
	}, {
		name: "comprehension comment after clause in inline list",
		in:   "a: [for x in y // c\n\tif x > 1 {x}]\n",
		want: "a: [for x in y // c\nif x > 1 {x}]\n",
	}, {
		name: "comprehension comment after clause in list",
		in:   "a: [\n\tfor x in y // c\n\tif x > 1 {x},\n]\n",
		want: "a: [\n\tfor x in y // c\n\tif x > 1 {x},\n]\n",
	}, {
		name: "comprehension comments after clauses",
		in:   "a: {\n\tfor x in y // c1\n\tlet z = x // c2\n\tif z {\n\t\tb: z\n\t}\n}\n",
		want: "a: {\n\tfor x in y // c1\n\tlet z = x // c2\n\tif z {\n\t\tb: z\n\t}\n}\n",
	}, {
		name: "comprehension comment after body brace",
		in:   "a: {if x { // c\n\tb: 1\n}, c: 2}\n",
		want: "a: {if x { // c\n\tb: 1\n}, c: 2}\n",
	}, {
		name: "comprehension comments after body",
		in:   "a: {\n\tif x {\n\t\tb: 1\n\t} // c1\n\t// c2\n}\n",
		want: "a: {\n\tif x {\n\t\tb: 1\n\t} // c1\n\t// c2\n}\n",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := format.Source([]byte(tc.in))
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.Equals(string(got), tc.want))
		})
	}
}

// TestClosedLiterals checks that both formatters keep the "#" of closed
// literals, which the openlists experiment enables, for parsed and for
// constructed nodes.
func TestClosedLiterals(t *testing.T) {
	const src = "@experiment(openlists)\n\n" +
		"a: #[1, 2]\n" +
		"b: #{c: 1}\n" +
		"c: #[#{d: 1}, #[]]\n" +
		"d: #{}\n" +
		"e: (#)[0]\n"

	list := &ast.ListLit{Hash: token.NoSpace.Pos(), Elts: []ast.Expr{ast.NewLit(token.INT, "1")}}
	strct := &ast.StructLit{Hash: token.NoSpace.Pos(), Elts: []ast.Decl{
		&ast.Field{Label: ast.NewIdent("a"), Value: ast.NewLit(token.INT, "1")},
	}}

	qt.Assert(t, qt.IsNil(cueexperiment.Init()))
	defer func(orig bool) { cueexperiment.Flags.FormatV2 = orig }(cueexperiment.Flags.FormatV2)

	for _, v2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("formatv2=%v", v2), func(t *testing.T) {
			cueexperiment.Flags.FormatV2 = v2

			got, err := format.Source([]byte(src))
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.Equals(string(got), src))

			b, err := format.Node(list)
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.Equals(string(b), "#[1]"))

			b, err = format.Node(strct)
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.StringContains(string(b), "#{"))
			qt.Check(t, qt.StringContains(string(b), "a: 1"))
		})
	}
}

// Verify that the printer can be invoked during initialization.
func init() {
	const name = "foobar"
	b, err := format.Node(&ast.Ident{Name: name})
	if err != nil {
		panic(err) // error in test
	}
	// in debug mode, the result contains additional information;
	// ignore it
	if s := string(b); !debug && s != name {
		panic("got " + s + ", want " + name)
	}
}

// TestNodes tests manually constructed AST nodes,
// which would not be produced by cue/parser but are accepted by cue/format.
func TestNodes(t *testing.T) {
	testCases := []struct {
		name string
		in   ast.Node
		out  string
	}{{
		name: "old-style octal numbers",
		in:   ast.NewLit(token.INT, "0123"),
		out:  "0o123",
	}, {
		name: "labels with multi-line strings",
		in: &ast.Field{
			Label: ast.NewLit(token.STRING,
				`"""
					foo
					bar
					"""`,
			),
			Value: ast.NewIdent("goo"),
		},
		out: `"foo\nbar": goo`,
	}, {
		// Issue #4296: struct fields with relative positions caused fields
		// with different nesting to be aligned, when they should not be.
		name: "field alignment with mixed nesting depths",
		in: &ast.StructLit{
			Lbrace: token.NoSpace.Pos(),
			Rbrace: token.NoSpace.Pos(),
			Elts: []ast.Decl{&ast.StructLit{
				Lbrace: token.NoSpace.Pos(),
				Rbrace: token.NoSpace.Pos(),
				Elts: []ast.Decl{
					&ast.Field{
						Label: &ast.Ident{NamePos: token.Newline.Pos(), Name: "veryLongLabel"},
						Value: ast.NewString("1"),
					},
					&ast.Field{
						Label: &ast.Ident{NamePos: token.Newline.Pos(), Name: "x"},
						Value: ast.NewString("2"),
					},
					&ast.Field{
						Label: &ast.Ident{NamePos: token.Newline.Pos(), Name: "a"},
						Value: &ast.StructLit{Elts: []ast.Decl{
							&ast.Field{Label: ast.NewIdent("b"), Value: ast.NewString("3")},
						}},
					},
				}}},
		},
		out: `{{
	veryLongLabel: "1"
	x:             "2"
	a: b: "3"
}}`,
	}, {
		name: "foo",
		in: func() ast.Node {
			st := ast.NewStruct("version", ast.NewString("foo"))
			st = ast.NewStruct("info", st)
			ast.AddComment(st.Elts[0], internal.NewComment(true, "FOO"))
			return st
		}(),
		out: `{
	// FOO
	info: {version: "foo"}
}`,
	}, {
		// A line comment runs to the end of the line, so an operator
		// following an operand which ends with one must go before it.
		name: "line comment ending a left operand",
		in: func() ast.Node {
			a := ast.NewIdent("a")
			ast.AddComment(a, internal.NewComment(false, "c"))
			return &ast.Field{
				Label: ast.NewIdent("x"),
				Value: ast.NewBinExpr(token.SUB, a,
					ast.NewBinExpr(token.SUB, ast.NewIdent("b"), ast.NewIdent("c"))),
			}
		}(),
		out: "x: a - // c\n\t(b - c)",
	}, {
		// As above, for a disjunct, such as the commented default
		// error which cue def exports for `*_|_ | int | string`.
		name: "line comment ending a disjunct",
		in: func() ast.Node {
			bottom := &ast.BottomLit{}
			ast.AddComment(bottom, internal.NewComment(false, "c"))
			return &ast.Field{
				Label: ast.NewIdent("x"),
				Value: ast.NewBinExpr(token.OR,
					&ast.UnaryExpr{Op: token.MUL, X: bottom},
					ast.NewIdent("int"), ast.NewIdent("string")),
			}
		}(),
		out: "x: *_|_ | // c\n\tint |\n\tstring",
	}, {
		// As above, for a conjunct ending with a nested binary
		// expression.
		name: "line comment ending a conjunct",
		in: func() ast.Node {
			b := ast.NewIdent("b")
			ast.AddComment(b, internal.NewComment(false, "c"))
			return &ast.Field{
				Label: ast.NewIdent("x"),
				Value: ast.NewBinExpr(token.AND,
					ast.NewBinExpr(token.ADD, ast.NewIdent("a"), b),
					ast.NewIdent("d")),
			}
		}(),
		out: "x: a + b & // c\n\td",
	}, {
		// A comment between a clause and the body, without a position,
		// sits on its own line.
		name: "comprehension clause comment without position",
		in: func() ast.Node {
			c := &ast.Comprehension{
				Clauses: []ast.Clause{&ast.ForClause{Value: ast.NewIdent("x"), Source: ast.NewIdent("y")}},
				Value:   ast.NewStruct("a", ast.NewIdent("x")),
			}
			ast.AddComment(c, &ast.CommentGroup{Position: 2, List: []*ast.Comment{{Text: "// c"}}})
			return &ast.Field{Label: ast.NewIdent("f"), Value: ast.NewList(c)}
		}(),
		out: "f: [\n\tfor x in y\n\t// c\n\t{\n\t\t\"a\": x\n\t}\n]",
	}, {
		name: "struct ending in a comment",
		in: func() ast.Node {
			v := ast.NewLit(token.INT, "1")
			ast.AddComment(v, internal.NewComment(false, "c"))
			return ast.NewStruct("a", v)
		}(),
		out: "{\n\ta: 1 // c\n}",
	}, {
		name: "disjunction of structs ending in a comment",
		in: func() ast.Node {
			v := &ast.BottomLit{}
			ast.AddComment(v, internal.NewComment(false, "c"))
			return ast.NewBinExpr(token.OR,
				ast.NewStruct(&ast.Field{Label: ast.NewIdent("a"), Constraint: token.OPTION, Value: v}),
				ast.NewStruct(&ast.Field{Label: ast.NewIdent("b"), Value: ast.NewLit(token.INT, "1")}),
			)
		}(),
		out: "{\n\ta?: _|_ // c\n} | {b: 1}",
	}, {
		// As the YAML decoder builds a flow sequence with a comment.
		name: "list ending in a comment",
		in: func() ast.Node {
			v := &ast.BasicLit{ValuePos: token.Blank.Pos(), Kind: token.STRING, Value: `"a"`}
			ast.AddComment(v, internal.NewComment(false, "c"))
			return &ast.ListLit{Lbrack: token.Blank.Pos(), Rbrack: token.Blank.Pos(), Elts: []ast.Expr{v}}
		}(),
		out: "[\"a\", // c\n]",
	}, {
		// As decoders build a list with an interior comment, which
		// stays inside the brackets.
		name: "list with an interior comment",
		in: func() ast.Node {
			l := ast.NewList(ast.NewIdent("x"), ast.NewIdent("y"))
			ast.AddComment(l, &ast.CommentGroup{Position: 1, List: []*ast.Comment{{Text: "// interior"}}})
			return &ast.Field{Label: ast.NewIdent("f"), Value: l}
		}(),
		out: "f: [\n\t// interior\n\tx,\n\ty,\n]",
	}, {
		// A comment ending the key or value of a for clause goes after
		// the source, as it would swallow `in` otherwise.
		name: "for clause value comment",
		in: func() ast.Node {
			v := ast.NewIdent("v")
			ast.AddComment(v, &ast.CommentGroup{Line: true, Position: 2, List: []*ast.Comment{{Text: "// c"}}})
			return &ast.Field{Label: ast.NewIdent("f"), Value: &ast.StructLit{Elts: []ast.Decl{&ast.Comprehension{
				Clauses: []ast.Clause{&ast.ForClause{Key: ast.NewIdent("k"), Value: v, Source: ast.NewIdent("src")}},
				Value:   ast.NewStruct("a", ast.NewIdent("v")),
			}}}}
		}(),
		out: "f: {for k, v in src // c\n{\n\t\"a\": v\n}}",
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := format.Node(tc.in, format.Simplify())
			if err != nil {
				t.Fatal(err)
			}
			if got := string(b); got != tc.out {
				t.Errorf("\ngot:  %v; want: %v", got, tc.out)
			}
		})
	}

}

// Verify that the printer doesn't crash if the AST contains Bad... nodes.
func TestBadNodes(t *testing.T) {
	const src = "package p\n("
	const res = "package p\n\n(_|_)\n"
	f, err := parser.ParseFile("", src, parser.ParseComments)
	if err == nil {
		t.Error("expected illegal program") // error in test
	}
	b, _ := format.Node(f)
	if string(b) != res {
		t.Errorf("got %q, expected %q", string(b), res)
	}
}
func TestPackage(t *testing.T) {
	f := &ast.File{
		Decls: []ast.Decl{
			&ast.Package{Name: ast.NewIdent("foo")},
			&ast.EmbedDecl{
				Expr: &ast.BasicLit{
					Kind:     token.INT,
					ValuePos: token.NoSpace.Pos(),
					Value:    "1",
				},
			},
		},
	}
	b, err := format.Node(f)
	if err != nil {
		t.Fatal(err)
	}
	const want = "package foo\n\n1\n"
	if got := string(b); got != want {
		t.Errorf("got %q, expected %q", got, want)
	}
}

// idents is an iterator that returns all idents in f via the result channel.
func idents(f *ast.File) <-chan *ast.Ident {
	v := make(chan *ast.Ident)
	go func() {
		ast.Walk(f, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				v <- ident
			}
			return true
		}, nil)
		close(v)
	}()
	return v
}

// identCount returns the number of identifiers found in f.
func identCount(f *ast.File) int {
	n := 0
	for range idents(f) {
		n++
	}
	return n
}

// Verify that the SourcePos mode emits correct //line comments
// by testing that position information for matching identifiers
// is maintained.
func TestSourcePos(t *testing.T) {
	const src = `package p

import (
	"go/printer"
	"math"
	"regexp"
)

let pi = 3.14
let xx = 0
t: {
	x: int
	y: int
	z: int
	u: number
	v: number
	w: number
}
e: a*t.x + b*t.y

// two extra lines here // ...
e2: c*t.z
`

	// parse original
	f1, err := parser.ParseFile("src", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	// pretty-print original
	b, err := format.Node(f1, format.IndentWidth(8))
	if err != nil {
		t.Fatal(err)
	}

	// parse pretty printed original
	// (//line comments must be interpreted even w/o parser.ParseComments set)
	f2, err := parser.ParseFile("", b, parser.AllErrors, parser.ParseComments)
	if err != nil {
		t.Fatalf("%s\n%s", err, b)
	}

	// At this point the position information of identifiers in f2 should
	// match the position information of corresponding identifiers in f1.

	// number of identifiers must be > 0 (test should run) and must match
	n1 := identCount(f1)
	n2 := identCount(f2)
	if n1 == 0 {
		t.Fatal("got no idents")
	}
	if n2 != n1 {
		t.Errorf("got %d idents; want %d", n2, n1)
	}

	// verify that all identifiers have correct line information
	i2range := idents(f2)
	for i1 := range idents(f1) {
		i2 := <-i2range

		if i2 == nil || i1 == nil {
			t.Fatal("non nil identifiers")
		}
		if i2.Name != i1.Name {
			t.Errorf("got ident %s; want %s", i2.Name, i1.Name)
		}

		l1 := i1.Pos().Line()
		l2 := i2.Pos().Line()
		if l2 != l1 {
			t.Errorf("got line %d; want %d for %s", l2, l1, i1.Name)
		}
	}

	if t.Failed() {
		t.Logf("\n%s", b)
	}
}

var decls = []string{
	"package p\n\n" + `import "fmt"`,
	"package p\n\n" + "let pi = 3.1415\nlet e = 2.71828\n\nlet x = pi",
}

func TestDeclLists(t *testing.T) {
	for _, src := range decls {
		file, err := parser.ParseFile("", src, parser.ParseComments)
		if err != nil {
			panic(err) // error in test
		}

		b, err := format.Node(file) // only print declarations
		if err != nil {
			panic(err) // error in test
		}

		out := strings.TrimSpace(string(b))

		if out != src {
			t.Errorf("\ngot : %q\nwant: %q\n", out, src)
		}
	}
}

func TestIncorrectIdent(t *testing.T) {
	testCases := []struct {
		ident string
		out   string
	}{
		{"foo", "foo"},
		{"a.b.c", `"a.b.c"`},
		{"for", "for"},
	}
	for _, tc := range testCases {
		t.Run(tc.ident, func(t *testing.T) {
			b, _ := format.Node(&ast.Field{Label: ast.NewIdent(tc.ident), Value: ast.NewIdent("A")})
			if got, want := string(b), tc.out+`: A`; got != want {
				t.Errorf("got %q; want %q", got, want)
			}
		})
	}
}

func TestSourceOptions(t *testing.T) {
	// Input with a nested struct, aligned fields,
	// and a quoted label that could be simplified to a plain identifier.
	src := `
"foo": {
	a:         1
	longField: 2
}
`[1:]
	type testCase struct {
		name    string
		options []format.Option
		want    string
	}
	testCases := []testCase{
		// No options: default behavior uses tabs for indentation and spaces for alignment.
		// The input source remains as-is.
		{
			name: "Defaults",
			want: src,
		},
		// Setting options to their default values is also a no-op.
		{
			name:    "DefaultValues",
			options: []format.Option{format.TabIndent(true), format.UseSpaces(8)},
			want:    src,
		},
		// UseSpaces setting a different tabWidth makes no difference unless we indent with spaces.
		{
			name:    "UseSpaces=2",
			options: []format.Option{format.UseSpaces(2)},
			want:    src,
		},

		// Simplify removes unnecessary quotes from "foo".
		{
			name:    "Simplify",
			options: []format.Option{format.Simplify()},
			want:    "foo: {\n	a:         1\n	longField: 2\n}\n",
		},
		// TabIndent(false) makes the indentation use a tabWidth number of spaces.
		// Note that this exposes the default tabWidth value of 4.
		{
			name:    "TabIndent=false",
			options: []format.Option{format.TabIndent(false)},
			want:    "\"foo\": {\n    a:         1\n    longField: 2\n}\n",
		},
		// TabIndent(false) with a custom number of spaces.
		{
			name:    "TabIndent=false,UseSpaces=2",
			options: []format.Option{format.TabIndent(false), format.UseSpaces(2)},
			want:    "\"foo\": {\n  a:         1\n  longField: 2\n}\n",
		},
		// IndentPrefix indents every line as a prefix.
		{
			name:    "IndentPrefix(3)",
			options: []format.Option{format.IndentPrefix(3)},
			want: `			"foo": {
				a:         1
				longField: 2
			}
`},
		// IndentPrefix follows the indentation string.
		{
			name:    "IndentPrefix(2),Indent(2 spaces)",
			options: []format.Option{format.IndentPrefix(2), format.Indent("  ")},
			want: `    "foo": {
      a:         1
      longField: 2
    }
`},
	}
	tdtest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
		t.Update(cuetest.UpdateGoldenFiles())
		got, err := format.Source([]byte(src), tc.options...)
		qt.Assert(t, qt.IsNil(err))
		t.Equal(string(got), tc.want)
	})
}

// TestFormatV2Smoke sanity-checks that the formatv2 experiment is on by
// default, and that the pre-v2 formatter remains reachable by disabling it.
//
// The input formats differently between the two: v2 aligns the field values
// even though one of them is a list, whereas v1 does not align in that case.
func TestFormatV2Smoke(t *testing.T) {
	const src = "a: 1\nbbbb: [1, 2, 3]\n"
	const wantV2 = "a:    1\nbbbb: [1, 2, 3]\n"
	const wantV1 = "a: 1\nbbbb: [1, 2, 3]\n"

	qt.Assert(t, qt.IsNil(cueexperiment.Init()))

	// The experiment is on by default.
	qt.Assert(t, qt.IsTrue(cueexperiment.Flags.FormatV2))

	// Init is guarded by sync.Once, so overriding Flags directly here is not
	// undone by the format functions calling cueexperiment.Init again.
	defer func(orig bool) { cueexperiment.Flags.FormatV2 = orig }(cueexperiment.Flags.FormatV2)

	cueexperiment.Flags.FormatV2 = true
	got, err := format.Source([]byte(src))
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(got), wantV2))

	cueexperiment.Flags.FormatV2 = false
	got, err = format.Source([]byte(src))
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(got), wantV1))
}

// TestV2Options exercises the options that only the formatv2 formatter
// implements. Each of them selects that formatter, so we run the whole
// test with the formatv2 experiment disabled: the layouts below are out
// of reach of the pre-v2 formatter, which has no notion of a target
// width and indents with tabs only.
func TestV2Options(t *testing.T) {
	// Clearing the authored positions leaves the layout entirely to the
	// width-driven heuristics, which is what these options steer.
	const src = "a: {x: 1, b: [1, 2, 3, 4, 5, 6]}\n"

	testCases := []struct {
		name string
		opts []format.Option
		want string
	}{{
		name: "width",
		opts: []format.Option{format.LineWidth(25)},
		want: "a: {\n	x: 1\n	b: [1, 2, 3, 4, 5, 6]\n}\n",
	}, {
		name: "narrowWidth",
		opts: []format.Option{format.LineWidth(15)},
		want: "a: {\n	x: 1\n	b: [\n		1,\n		2,\n		3,\n		4,\n		5,\n		6,\n	]\n}\n",
	}, {
		name: "indent",
		opts: []format.Option{format.LineWidth(25), format.Indent("  ")},
		want: "a: {\n  x: 1\n  b: [1, 2, 3, 4, 5, 6]\n}\n",
	}, {
		name: "noIndent",
		opts: []format.Option{format.LineWidth(25), format.Indent("")},
		want: "a: {\nx: 1\nb: [1, 2, 3, 4, 5, 6]\n}\n",
	}, {
		// A wider indent leaves less room on the line, so the nested
		// list no longer fits and breaks.
		name: "indentWidth",
		opts: []format.Option{format.LineWidth(25), format.IndentWidth(10)},
		want: "a: {\n	x: 1\n	b: [\n		1,\n		2,\n		3,\n		4,\n		5,\n		6,\n	]\n}\n",
	}}

	qt.Assert(t, qt.IsNil(cueexperiment.Init()))
	// Init is guarded by sync.Once, so overriding Flags directly here is not
	// undone by the format functions calling cueexperiment.Init again.
	defer func(orig bool) { cueexperiment.Flags.FormatV2 = orig }(cueexperiment.Flags.FormatV2)
	cueexperiment.Flags.FormatV2 = false

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("in", src, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))
			format.ASTStyle{ClearPositions: true}.Apply(f)

			got, err := format.Node(f, tc.opts...)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(got), tc.want))
		})
	}
}

// TestKeepRelPos checks that KeepRelPos leaves the layout of the AST
// alone, so that the width-driven heuristics see the positions as they
// are rather than the ones the conventional layout would impose.
func TestKeepRelPos(t *testing.T) {
	const src = "a: {x: 1, b: [1, 2, 3]}\nc: \"str\"\n"

	testCases := []struct {
		name string
		opts []format.Option
		want string
	}{{
		// The conventional layout puts every field of a struct on its
		// own line, undoing the compaction that clearing the positions
		// asks for.
		name: "default",
		want: "a: {\n	x: 1\n	b: [1, 2, 3]\n}\nc: \"str\"\n",
	}, {
		name: "keepRelPos",
		opts: []format.Option{format.KeepRelPos()},
		want: "a: {x: 1, b: [1, 2, 3]}, c: \"str\"\n",
	}}

	withoutFormatV2(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := parseCleared(t, src)
			got, err := format.Node(f, tc.opts...)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(got), tc.want))
		})
	}
}

// TestKeepRelPosSimplify checks that KeepRelPos does not hold back the
// rewrites that Simplify asks for.
func TestKeepRelPosSimplify(t *testing.T) {
	withoutFormatV2(t)

	f, err := parser.ParseFile("in", "\"a\": {\n	b: 1\n}\n", parser.ParseComments)
	qt.Assert(t, qt.IsNil(err))
	got, err := format.Node(f, format.KeepRelPos(), format.Simplify())
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(got), "a: b: 1\n"))
}

// TestNodeInPlace checks that [format.Node] never mutates the AST it
// is given, and that [format.NodeInPlace] does so instead of copying.
func TestNodeInPlace(t *testing.T) {
	// The AST is reformatted with KeepRelPos afterwards to observe
	// whether the default style was applied to it in place: the layout
	// hints it adds survive in the tree, so a compact reformat of a
	// mutated tree comes back expanded.
	const src = "a: {x: 1, b: [1, 2, 3]}\nc: \"str\"\n"
	const compact = "a: {x: 1, b: [1, 2, 3]}, c: \"str\"\n"
	const expanded = "a: {\n	x: 1\n	b: [1, 2, 3]\n}\nc: \"str\"\n"

	t.Run("immutable", func(t *testing.T) {
		f := parseCleared(t, src)
		got, err := format.Node(f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(got), expanded))

		after, err := format.Node(f, format.KeepRelPos())
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(after), compact))
	})

	t.Run("mutable", func(t *testing.T) {
		f := parseCleared(t, src)
		got, err := format.NodeInPlace(f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(got), expanded))

		after, err := format.Node(f, format.KeepRelPos())
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(after), expanded))
	})

	// The rewrites that Simplify asks for are just as much off limits
	// to Node, whichever formatter is in use.
	for _, v2 := range []bool{true, false} {
		t.Run(fmt.Sprintf("simplify-v2=%v", v2), func(t *testing.T) {
			if !v2 {
				withoutFormatV2(t)
			}
			const src = "\"a\": 1\n"
			f, err := parser.ParseFile("in", src, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))
			got, err := format.Node(f, format.Simplify())
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(got), "a: 1\n"))
			qt.Assert(t, qt.Satisfies(f.Decls[0].(*ast.Field).Label, func(l ast.Label) bool {
				_, ok := l.(*ast.BasicLit)
				return ok
			}))

			f, err = parser.ParseFile("in", src, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))
			got, err = format.NodeInPlace(f, format.Simplify())
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(got), "a: 1\n"))
			qt.Assert(t, qt.Satisfies(f.Decls[0].(*ast.Field).Label, func(l ast.Label) bool {
				_, ok := l.(*ast.Ident)
				return ok
			}))
		})
	}
}

// TestCompact checks that Compact discards the authored layout and the
// comments, laying the result out on as few lines as the syntax allows.
func TestCompact(t *testing.T) {
	const src = `package p

// doc comment
a: {
	x: 1 // line comment
	b: [1, 2, 3]
}

c: """
	multi
	line
	"""
`

	testCases := []struct {
		name string
		opts []format.Option
		want string
	}{{
		name: "compact",
		opts: []format.Option{format.Compact()},
		want: `package p
a: {x: 1, b: [1, 2, 3]}
c: """
	multi
	line
	"""
`,
	}, {
		// A width given after Compact still applies; the layout is
		// broken up only as far as that width requires.
		name: "compactThenWideWidth",
		opts: []format.Option{format.Compact(), format.LineWidth(120)},
		want: `package p
a: {x: 1, b: [1, 2, 3]}
c: """
	multi
	line
	"""
`,
	}, {
		name: "compactThenNarrowWidth",
		opts: []format.Option{format.Compact(), format.LineWidth(22)},
		want: `package p
a: {
	x: 1
	b: [1, 2, 3]
}
c: """
	multi
	line
	"""
`,
	}, {
		name: "compactThenNarrowerWidth",
		opts: []format.Option{format.Compact(), format.LineWidth(15)},
		want: `package p
a: {
	x: 1
	b: [
		1,
		2,
		3,
	]
}
c: """
	multi
	line
	"""
`,
	}, {
		// Compact sets the width itself, so a width given before it is
		// overridden by the effectively unbounded one that it implies.
		name: "widthThenCompact",
		opts: []format.Option{format.LineWidth(15), format.Compact()},
		want: `package p
a: {x: 1, b: [1, 2, 3]}
c: """
	multi
	line
	"""
`,
	}, {
		// A width repeated after Compact takes effect again.
		name: "widthThenCompactThenWidth",
		opts: []format.Option{format.LineWidth(15), format.Compact(), format.LineWidth(15)},
		want: `package p
a: {
	x: 1
	b: [
		1,
		2,
		3,
	]
}
c: """
	multi
	line
	"""
`,
	}}

	withoutFormatV2(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("in", src, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))
			got, err := format.Node(f, tc.opts...)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(got), tc.want))

			// Node leaves the comments and positions it was given alone.
			again, err := format.Node(f)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.StringContains(string(again), "// doc comment"))
		})
	}

	// Compact does not hold back the rewrites that Simplify asks for.
	t.Run("simplify", func(t *testing.T) {
		f, err := parser.ParseFile("in", `"a": {
	b: 1
}
`, parser.ParseComments)
		qt.Assert(t, qt.IsNil(err))
		got, err := format.Node(f, format.Compact(), format.Simplify())
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(got), "a: b: 1\n"))
	})

	// Programmatic ASTs may hold comments as standalone declarations,
	// which are discarded like any other comment.
	t.Run("commentDecls", func(t *testing.T) {
		f := &ast.File{Decls: []ast.Decl{
			internal.NewComment(true, "Generated."),
			&ast.Field{Label: ast.NewIdent("a"), Value: ast.NewLit(token.INT, "1")},
			&ast.Field{Label: ast.NewIdent("b"), Value: ast.NewStruct(
				internal.NewComment(true, "Inner."),
				ast.NewIdent("c"), ast.NewLit(token.INT, "2"),
				ast.NewIdent("d"), ast.NewLit(token.INT, "3"),
			)},
		}}
		got, err := format.Node(f, format.Compact())
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(got), "a: 1, b: {c: 2, d: 3}\n"))

		// Node leaves the comment declarations it was given alone.
		again, err := format.Node(f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.StringContains(string(again), "// Generated."))
		qt.Assert(t, qt.StringContains(string(again), "// Inner."))
	})
}

// parseCleared parses src and clears the positions it carries, leaving
// the layout entirely to the width-driven heuristics.
func parseCleared(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile("in", src, parser.ParseComments)
	qt.Assert(t, qt.IsNil(err))
	format.ASTStyle{ClearPositions: true}.Apply(f)
	return f
}

// withoutFormatV2 disables the formatv2 experiment for the duration of
// the test, so that a formatter selected by an option is selected by
// that option alone.
func withoutFormatV2(t *testing.T) {
	t.Helper()
	qt.Assert(t, qt.IsNil(cueexperiment.Init()))
	// Init is guarded by sync.Once, so overriding Flags directly here is not
	// undone by the format functions calling cueexperiment.Init again.
	orig := cueexperiment.Flags.FormatV2
	t.Cleanup(func() { cueexperiment.Flags.FormatV2 = orig })
	cueexperiment.Flags.FormatV2 = false
}

// TextX is a skeleton test that can be filled in for debugging one-off cases.
// Do not remove.
func TestX(t *testing.T) {
	t.Skip()
	const src = `

`
	b, err := format.Source([]byte(src), format.Simplify())
	if err != nil {
		t.Error(err)
	}
	_ = b
	t.Error("\n", string(b))
}
