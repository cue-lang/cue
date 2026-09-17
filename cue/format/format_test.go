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
