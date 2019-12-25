// Copyright 2019 CUE Authors
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

package json

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
)

func TestExtract(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		out  string
	}{{
		name: "no expand as JSON is not compact",
		in:   `{"a": 32}`,
		out:  `{a: 32}`,
	}, {
		name: "break across new lines",
		in:   `{"a":32,"b":[1,2],"c-d":"foo-bar-baz"}`,
		out: `{
	a: 32
	b: [1, 2]
	"c-d": "foo-bar-baz"
}`,
	}, {
		name: "multiline string",
		in:   `"a\nb\uD803\uDE6D\nc\\\t\nd\/"`,
		out: `"""
	a
	b` + "\U00010E6D" + `
	c\\\t
	d/
	"""`,
	}, {
		name: "multiline string indented",
		in:   `{"x":{"y":"a\nb\nc\nd"}}`,
		out: `{
	x: {
		y: """
			a
			b
			c
			d
			"""
	}
}`,
	}, {
		name: "don't create multiline string for label",
		in:   `{"foo\nbar\nbaz\n": 2}`,
		out:  `{"foo\nbar\nbaz\n": 2}`,
	}, {
		name: "don't cap indentation",
		in:   `{"a":{"b":{"c":{"d":"a\nb\nc\nd"}}}}`,
		out: `{
	a: {
		b: {
			c: {
				d: """
					a
					b
					c
					d
					"""
			}
		}
	}
}`,
	}, {
		name: "keep list formatting",
		in: `[1,2,
	3]`,
		out: "[1, 2,\n\t3]",
	}, {
		// TODO: format.Node doesn't break large lists, it probably should.
		name: "large list",
		in:   `[11111111111,2222222222,3333333333,4444444444,5555555555,6666666666]`,
		out:  "[11111111111, 2222222222, 3333333333, 4444444444, 5555555555, 6666666666]",
	}, {
		name: "reflow large values unconditionally",
		in:   `{"a": "11111111112222222222333333333344444444445555555555"}`,
		out:  "{\n\ta: \"11111111112222222222333333333344444444445555555555\"\n}",
	}, {
		name: "invalid JSON",
		in:   `[3_]`,
		out:  "invalid JSON for file \"invalid JSON\": invalid character '_' after array element",
	}, {
		name: "numeric keys: Issue #219",
		in:   `{"20": "a"}`,
		out:  `{"20": "a"}`,
	}, {
		name: "legacy: hidden fields",
		in:   `{"_legacy": 1}`,
		out:  `{"_legacy": 1}`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			e, err := Extract(tc.name, []byte(tc.in))
			toString(out, e, err)
			assert.Equal(t, tc.out, out.String())

			out = &bytes.Buffer{}
			d := NewDecoder(nil, tc.name, strings.NewReader(tc.in))
			for {
				e, err := d.Extract()
				if err == io.EOF {
					break
				}
				toString(out, e, err)
				if err != nil {
					break
				}
			}
			assert.Equal(t, tc.out, out.String())
		})
	}
}

func toString(w *bytes.Buffer, e ast.Expr, err error) {
	if err != nil {
		fmt.Fprint(w, err)
		return
	}
	b, err := format.Node(e)
	if err != nil {
		fmt.Fprint(w, err)
		return
	}
	fmt.Fprint(w, string(b))
}
