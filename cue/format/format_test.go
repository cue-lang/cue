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

package format

// TODO: port more of the tests of go/printer

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

var (
	defaultConfig = newConfig([]Option{})
	Fprint        = defaultConfig.fprint
)

func TestFiles(t *testing.T) {
	txtarFiles, err := filepath.Glob("testdata/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, txtarFile := range txtarFiles {
		ar, err := txtar.ParseFile(txtarFile)
		qt.Assert(t, qt.IsNil(err))

		opts := []Option{TabIndent(true)}
		for _, word := range strings.Fields(string(ar.Comment)) {
			switch word {
			case "simplify":
				opts = append(opts, Simplify())
			case "sort-imports":
				opts = append(opts, sortImportsOption())
			}
		}

		tfs, err := txtar.FS(ar)
		qt.Assert(t, qt.IsNil(err))
		inputFiles, err := fs.Glob(tfs, "*.input")
		qt.Assert(t, qt.IsNil(err))

		for _, inputFile := range inputFiles {
			goldenFile := strings.TrimSuffix(inputFile, ".input") + ".golden"
			t.Run(path.Join(txtarFile, inputFile), func(t *testing.T) {
				src, err := fs.ReadFile(tfs, inputFile)
				qt.Assert(t, qt.IsNil(err))

				res, err := Source(src, opts...)
				qt.Assert(t, qt.IsNil(err))

				// make sure formatted output is syntactically correct
				_, err = parser.ParseFile("", res, parser.AllErrors)
				qt.Assert(t, qt.IsNil(err))

				// update golden files if necessary
				// TODO(mvdan): deduplicate this code with UpdateGoldenFiles on txtar files?
				if cuetest.UpdateGoldenFiles {
					for i := range ar.Files {
						file := &ar.Files[i]
						if file.Name == goldenFile {
							file.Data = res
							return
						}
					}
					ar.Files = append(ar.Files, txtar.File{
						Name: goldenFile,
						Data: res,
					})
					return
				}

				// get golden
				gld, err := fs.ReadFile(tfs, goldenFile)
				qt.Assert(t, qt.IsNil(err))

				// formatted source and golden must be the same
				qt.Assert(t, qt.Equals(string(res), string(gld)))

				// TODO(mvdan): check that all files format in an idempotent way,
				// i.e. that formatting a golden file results in no changes.
			})
		}
		if cuetest.UpdateGoldenFiles {
			err = os.WriteFile(txtarFile, txtar.Format(ar), 0o666)
			qt.Assert(t, qt.IsNil(err))
		}
	}
}

// Verify that the printer can be invoked during initialization.
func init() {
	const name = "foobar"
	b, err := Fprint(&ast.Ident{Name: name})
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
			b, err := Node(tc.in, Simplify())
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
	b, _ := Fprint(f)
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
	b, err := Node(f)
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
	b, err := (&config{UseSpaces: true, Tabwidth: 8}).fprint(f1)
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

		b, err := Fprint(file.Decls) // only print declarations
		if err != nil {
			panic(err) // error in test
		}

		out := string(b)

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
			b, _ := Node(&ast.Field{Label: ast.NewIdent(tc.ident), Value: ast.NewIdent("A")})
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
	b, err := Source([]byte(src), Simplify())
	if err != nil {
		t.Error(err)
	}
	_ = b
	t.Error("\n", string(b))
}
