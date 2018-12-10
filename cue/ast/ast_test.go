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

package ast

import (
	"testing"
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
		list := make([]*Comment, len(c.list))
		for i, s := range c.list {
			list[i] = &Comment{Text: s}
		}

		text := (&CommentGroup{List: list}).Text()
		if text != c.text {
			t.Errorf("case %d: got %q; expected %q", i, text, c.text)
		}
	}
}
