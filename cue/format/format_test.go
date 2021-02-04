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
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

var (
	defaultConfig = newConfig([]Option{})
	Fprint        = defaultConfig.fprint
)

const (
	dataDir = "testdata"
)

type checkMode uint

const (
	_ checkMode = 1 << iota
	idempotent
	simplify
	sortImps
)

// format parses src, prints the corresponding AST, verifies the resulting
// src is syntactically correct, and returns the resulting src or an error
// if any.
func format(src []byte, mode checkMode) ([]byte, error) {
	// parse src
	opts := []Option{TabIndent(true)}
	if mode&simplify != 0 {
		opts = append(opts, Simplify())
	}
	if mode&sortImps != 0 {
		opts = append(opts, sortImportsOption())
	}

	res, err := Source(src, opts...)
	if err != nil {
		return nil, err
	}

	// make sure formatted output is syntactically correct
	if _, err := parser.ParseFile("", res, parser.AllErrors); err != nil {
		return nil, errors.Append(err.(errors.Error),
			errors.Newf(token.NoPos, "re-parse failed: %s", res))
	}

	return res, nil
}

// lineAt returns the line in text starting at offset offs.
func lineAt(text []byte, offs int) []byte {
	i := offs
	for i < len(text) && text[i] != '\n' {
		i++
	}
	return text[offs:i]
}

// diff compares a and b.
func diff(aname, bname string, a, b []byte) error {
	var buf bytes.Buffer // holding long error message

	// compare lengths
	if len(a) != len(b) {
		fmt.Fprintf(&buf, "\nlength changed: len(%s) = %d, len(%s) = %d", aname, len(a), bname, len(b))
	}

	// compare contents
	line := 1
	offs := 1
	for i := 0; i < len(a) && i < len(b); i++ {
		ch := a[i]
		if ch != b[i] {
			fmt.Fprintf(&buf, "\n%s:%d:%d: %s", aname, line, i-offs+1, lineAt(a, offs))
			fmt.Fprintf(&buf, "\n%s:%d:%d: %s", bname, line, i-offs+1, lineAt(b, offs))
			fmt.Fprintf(&buf, "\n\n")
			break
		}
		if ch == '\n' {
			line++
			offs = i + 1
		}
	}

	if buf.Len() > 0 {
		return errors.New(buf.String())
	}
	return nil
}

func runcheck(t *testing.T, source, golden string, mode checkMode) {
	src, err := ioutil.ReadFile(source)
	if err != nil {
		t.Error(err)
		return
	}

	res, err := format(src, mode)
	if err != nil {
		b := &bytes.Buffer{}
		errors.Print(b, err, nil)
		t.Error(b.String())
		return
	}

	// update golden files if necessary
	if cuetest.UpdateGoldenFiles {
		if err := ioutil.WriteFile(golden, res, 0644); err != nil {
			t.Error(err)
		}
		return
	}

	// get golden
	gld, err := ioutil.ReadFile(golden)
	if err != nil {
		t.Error(err)
		return
	}

	// formatted source and golden must be the same
	if err := diff(source, golden, res, gld); err != nil {
		t.Error(err)
		return
	}

	if mode&idempotent != 0 {
		// formatting golden must be idempotent
		// (This is very difficult to achieve in general and for now
		// it is only checked for files explicitly marked as such.)
		res, err = format(gld, mode)
		if err != nil {
			t.Fatal(err)
		}
		if err := diff(golden, fmt.Sprintf("format(%s)", golden), gld, res); err != nil {
			t.Errorf("golden is not idempotent: %s", err)
		}
	}
}

func check(t *testing.T, source, golden string, mode checkMode) {
	// run the test
	cc := make(chan int)
	go func() {
		runcheck(t, source, golden, mode)
		cc <- 0
	}()

	// wait with timeout
	select {
	case <-time.After(100000 * time.Second): // plenty of a safety margin, even for very slow machines
		// test running past time out
		t.Errorf("%s: running too slowly", source)
	case <-cc:
		// test finished within allotted time margin
	}
}

type entry struct {
	source, golden string
	mode           checkMode
}

// Set CUE_UPDATE=1 to create/update the respective golden files.
var data = []entry{
	{"comments.input", "comments.golden", simplify},
	{"simplify.input", "simplify.golden", simplify},
	{"expressions.input", "expressions.golden", 0},
	{"values.input", "values.golden", 0},
	{"imports.input", "imports.golden", sortImps},
}

func TestFiles(t *testing.T) {
	t.Parallel()
	for _, e := range data {
		source := filepath.Join(dataDir, e.source)
		golden := filepath.Join(dataDir, e.golden)
		mode := e.mode
		t.Run(e.source, func(t *testing.T) {
			t.Parallel()
			check(t, source, golden, mode)
			// TODO(gri) check that golden is idempotent
			//check(t, golden, golden, e.mode)
		})
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

// TestNodes tests nodes that are that are invalid CUE, but are accepted by
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

// Verify that the printer doesn't crash if the AST contains BadXXX nodes.
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
	// (//line comments must be interpreted even w/o syntax.ParseComments set)
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
	b, err := format([]byte(src), simplify)
	if err != nil {
		t.Error(err)
	}
	_ = b
	t.Error("\n", string(b))
}
