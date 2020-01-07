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

	"github.com/stretchr/testify/assert"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
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
		{[]string{"// foo", "/* bar */"}, "foo\n bar\n"},
		{[]string{"//", "//", "//", "// foo", "//", "//", "//"}, "foo\n"},

		{[]string{"/**/"}, ""},
		{[]string{"/*   */"}, ""},
		{[]string{"/**/", "/**/", "/*   */"}, ""},
		{[]string{"/* Foo   */"}, " Foo\n"},
		{[]string{"/* Foo  Bar  */"}, " Foo  Bar\n"},
		{[]string{"/* Foo*/", "/* Bar*/"}, " Foo\n Bar\n"},
		{[]string{"/* Foo*/", "/**/", "/**/", "/**/", "// Bar"}, " Foo\n\nBar\n"},
		{[]string{"/* Foo*/", "/*\n*/", "//", "/*\n*/", "// Bar"}, " Foo\n\nBar\n"},
		{[]string{"/* Foo*/", "// Bar"}, " Foo\nBar\n"},
		{[]string{"/* Foo\n Bar*/"}, " Foo\n Bar\n"},
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
			assert.Equal(t, f.PackageName(), tc.pkg)
		})
	}
}
