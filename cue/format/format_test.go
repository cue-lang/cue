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
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetxtar"
)

const debug = false

func TestFiles(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "format",
	}
	test.Run(t, func(t *cuetxtar.Test) {
		opts := []format.Option{format.TabIndent(true)}
		if t.HasTag("simplify") {
			opts = append(opts, format.Simplify())
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

			// TODO(mvdan): check that all files format in an idempotent way,
			// i.e. that formatting a golden file results in no changes.
		}
	})
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

// TestNodes tests nodes that are invalid CUE, but are accepted by
// format.
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
		name: "foo",
		in: func() ast.Node {
			st := ast.NewStruct("version", ast.NewString("foo"))
			st = ast.NewStruct("info", st)
			ast.AddComment(st.Elts[0], internal.NewComment(true, "FOO"))
			return st
		}(),
		out: `{
	// FOO
	info: {
		version: "foo"
	}
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
	b, err := format.Node(f1, format.UseSpaces(8))
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
