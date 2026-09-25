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

package ast_test

import (
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/tdtest"
)

func TestCommentText(t *testing.T) {
	testCases := []struct {
		list []string
		text string
	}{
		{[]string{"//"}, ""},
		{[]string{"//   "}, ""},
		{[]string{"//", "//", "//   "}, ""},
		{[]string{"// foo   "}, "foo\n"},
		{[]string{"//", "//", "// foo"}, "foo\n"},
		{[]string{"// foo  bar  "}, "foo  bar\n"},
		{[]string{"// foo", "// bar"}, "foo\nbar\n"},
		{[]string{"// foo", "//", "//", "//", "// bar"}, "foo\n\nbar\n"},
		{[]string{"//", "//", "//", "// foo", "//", "//", "//"}, "foo\n"},
	}

	for i, c := range testCases {
		list := make([]*ast.Comment, len(c.list))
		for i, s := range c.list {
			list[i] = &ast.Comment{Text: s}
		}

		text := (&ast.CommentGroup{List: list}).Text()
		if text != c.text {
			t.Errorf("case %d: got %q; expected %q", i, text, c.text)
		}
	}
}

// TestClosedLiteralPos checks that a closed literal starts at its "#" when
// the position of the "#" is known, and at its bracket otherwise.
func TestClosedLiteralPos(t *testing.T) {
	f, err := parser.ParseFile("in", "@experiment(openlists)\nx: #[1]\ny: #{a: 1}\n")
	qt.Assert(t, qt.IsNil(err))
	l := f.Decls[1].(*ast.Field).Value.(*ast.ListLit)
	s := f.Decls[2].(*ast.Field).Value.(*ast.StructLit)
	qt.Check(t, qt.Equals(l.Pos(), l.Hash))
	qt.Check(t, qt.Equals(s.Pos(), s.Hash))

	// Without a position for its "#" the literal is open, and starts at
	// its bracket.
	l.Hash, s.Hash = token.NoPos, token.NoPos
	qt.Check(t, qt.Equals(l.Pos(), l.Lbrack))
	qt.Check(t, qt.Equals(s.Pos(), s.Lbrace))
}

func TestPackageName(t *testing.T) {
	testCases := []struct {
		input string
		pkg   string
	}{{
		input: `
		package foo
		`,
		pkg: "foo",
	}, {
		input: `
		a: 2
		`,
	}, {
		input: `
		// Comment

		// Package foo ...
		package foo
		`,
		pkg: "foo",
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			f, err := parser.ParseFile("test", tc.input)
			if err != nil {
				t.Fatal(err)
			}
			qt.Assert(t, qt.Equals(f.PackageName(), tc.pkg))
		})
	}
}

func TestNewStruct(t *testing.T) {
	type testCase struct {
		input []any
		want  string
	}
	testCases := []testCase{{
		input: []any{
			internal.NewComment(true, "foo"),
			&ast.Ellipsis{},
		},
		want: `{
	// foo

	...
}`,
	}, {
		input: []any{
			&ast.LetClause{Ident: ast.NewIdent("foo"), Expr: ast.NewIdent("bar")},
			ast.Label(ast.NewString("bar")), ast.NewString("baz"),
			&ast.Field{
				Label: ast.NewString("a"),
				Value: ast.NewString("b"),
			},
		},
		want: `{
	let foo = bar
	"bar": "baz"
	"a":   "b"
}`,
	}, {
		input: []any{
			ast.NewIdent("opt"), token.OPTION, ast.NewString("foo"),
			ast.NewIdent("req"), token.NOT, ast.NewString("bar"),
		},
		want: `{
	opt?: "foo"
	req!: "bar"
}`,
	}, {
		input: []any{ast.Embed(ast.NewBool(true))},
		want:  `{true}`,
	}}
	// TODO(tdtest): use cuetest.Run when supported.
	tdtest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
		s := ast.NewStruct(tc.input...)
		b, err := format.Node(s)
		if err != nil {
			t.Fatal(err)
		}
		t.Equal(string(b), tc.want)
	})
}

func TestFuncParamType(t *testing.T) {
	var _ ast.Decl = (*ast.Field)(nil)

	p := ast.FuncParam(ast.Field{}) // Requires identical underlying types.
	var n ast.Node = &p
	if _, ok := n.(*ast.Field); ok {
		t.Fatal("FuncParam is not a distinct node type")
	}
	if _, ok := n.(ast.Decl); ok {
		t.Fatal("FuncParam implements ast.Decl")
	}
}

func TestFileExperiments(t *testing.T) {
	forVersion := func(t *testing.T, version string) *cueexperiment.File {
		t.Helper()
		exp, err := cueexperiment.NewFile(version)
		qt.Assert(t, qt.IsNil(err))
		return exp
	}
	// version is the language version f reports through its experiments.
	version := func(f *ast.File) string {
		exp := f.Experiment()
		return exp.LanguageVersion()
	}
	newFile := func() *ast.File {
		return &ast.File{Decls: []ast.Decl{
			&ast.Field{Label: ast.NewIdent("a"), Value: ast.NewString("b")},
		}}
	}

	// A file assembled rather than parsed has no source to resolve
	// experiments from, so it carries them once told which apply.
	f := newFile()
	qt.Assert(t, qt.Equals(version(f), ""))
	f.SetExperiments(forVersion(t, "v0.14.0"))
	qt.Assert(t, qt.Equals(version(f), "v0.14.0"))

	// Recording a second set replaces the first.
	f.SetExperiments(forVersion(t, "v0.15.0"))
	qt.Assert(t, qt.Equals(version(f), "v0.15.0"))

	// A parsed file resolves them from its position instead.
	parsed, err := parser.ParseFile("p.cue", "a: 1\n", parser.Version("v0.17.0"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(version(parsed), "v0.17.0"))

	// Declarations assembled from elsewhere keep the positions they were
	// parsed with, which report the version of the file they came from. What
	// the file itself carries wins, being what it is assembled for.
	built := &ast.File{Decls: parsed.Decls}
	qt.Assert(t, qt.Equals(version(built), "v0.17.0"))
	built.SetExperiments(forVersion(t, "v0.14.0"))
	qt.Assert(t, qt.Equals(version(built), "v0.14.0"))

	// Setting them leaves the file's position alone: it is the experiments
	// that are carried, not a source location the file does not have.
	qt.Assert(t, qt.Equals(built.Pos(), parsed.Decls[0].Pos()))

	// Experiments a source enabled by attribute travel with the set, which a
	// language version on its own could not express.
	attr, err := parser.ParseFile("attr.cue", "@experiment(aliasv2)\na: 1\n",
		parser.Version("v0.17.0"))
	qt.Assert(t, qt.IsNil(err))
	carried := attr.Experiment()
	qt.Assert(t, qt.IsTrue(carried.AliasV2))
	g := newFile()
	g.SetExperiments(&carried)
	qt.Assert(t, qt.Equals(version(g), "v0.17.0"))
	exp := g.Experiment()
	qt.Assert(t, qt.IsTrue(exp.AliasV2))
}
