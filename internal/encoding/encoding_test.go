// Copyright 2020 CUE Authors
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

package encoding

import (
	"path"
	"strings"
	"testing"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		form build.Form
		in   string
		err  string
	}{{
		form: "data",
		in: `
		// Foo
		a: 2
		"b-b": 3
		s: -2
		a: +2
		`,
	}, {
		form: "graph",
		in: `
		X=3
		a: X
		"b-b": 3
		s: a
		`,
	},

		{form: "data", err: "imports", in: `import "foo" `},
		{form: "data", err: "references", in: `a: a`},
		{form: "data", err: "expressions", in: `a: 1 + 3`},
		{form: "data", err: "expressions", in: `a: 1 + 3`},
		{form: "data", err: "definitions", in: `a :: 1`},
		{form: "data", err: "constraints", in: `a: <1`},
		{form: "data", err: "expressions", in: `a: !true`},
		{form: "data", err: "expressions", in: `a: 1 | 2`},
		{form: "data", err: "expressions", in: `a: 1 | *2`},
		{form: "data", err: "references", in: `X=3, a: X`},
		{form: "data", err: "expressions", in: `2+2`},
		{form: "data", err: "expressions", in: `"\(3)"`},
		{form: "data", err: "expressions", in: `for x in [2] { a: 2 }`},
		{form: "data", err: "expressions", in: `a: len([])`},
		{form: "data", err: "ellipsis", in: `a: [...]`},
	}
	for _, tc := range testCases {
		t.Run(path.Join(string(tc.form), tc.in), func(t *testing.T) {
			f, err := parser.ParseFile("", tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			d := Decoder{cfg: &Config{}}
			d.validate(f, &build.File{
				Filename: "foo.cue",
				Encoding: build.CUE,
				Form:     tc.form,
			})
			if (tc.err == "") != (d.err == nil) {
				t.Errorf("error: got %v; want %v", tc.err == "", d.err == nil)
			}
			if d.err != nil && !strings.Contains(d.err.Error(), tc.err) {
				t.Errorf("error message did not contain %q: %v", tc.err, d.err)
			}
		})
	}
}
