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

package literal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestQuote(t *testing.T) {
	testCases := []struct {
		form  Form
		in    string
		out   string
		lossy bool
	}{
		{form: String, in: "\x00", out: `"\u0000"`},
		{form: String, in: "abc\xffdef", out: `"abc�def"`, lossy: true},
		{form: Bytes, in: "abc\xffdef", out: `'abc\xffdef'`},
		{form: String.WithASCIIOnly(),
			in: "abc\xffdef", out: `"abc\ufffddef"`, lossy: true},
		{form: String, in: "\a\b\f\r\n\t\v", out: `"\a\b\f\r\n\t\v"`},
		{form: String, in: "\"", out: `"\""`},
		{form: String, in: "\\", out: `"\\"`},
		{form: String, in: "\u263a", out: `"☺"`},
		{form: String, in: "\U0010ffff", out: `"\U0010ffff"`},
		{form: String, in: "\x04", out: `"\u0004"`},
		{form: Bytes, in: "\x04", out: `'\x04'`},
		{form: String.WithASCIIOnly(), in: "\u263a", out: `"\u263a"`},
		{form: String.WithGraphicOnly(), in: "\u263a", out: `"☺"`},
		{
			form: String.WithASCIIOnly(),
			in:   "!\u00a0!\u2000!\u3000!",
			out:  `"!\u00a0!\u2000!\u3000!"`,
		},
		{form: String, in: "a\nb", out: `"a\nb"`},
		{form: String.WithTabIndent(3), in: "a\nb", out: `"""
			a
			b
			"""`},
		{form: String.WithTabIndent(3), in: "a", out: `"""
			a
			"""`},
		{form: String.WithTabIndent(3), in: "a\n", out: `"""
			a

			"""`},
		{form: String.WithTabIndent(3), in: "", out: `"""
			"""`},
		{form: String.WithTabIndent(3), in: "\n", out: `"""


			"""`},
		{form: String.WithTabIndent(3), in: "\n\n", out: `"""



			"""`},
		{form: String.WithOptionalTabIndent(3), in: "a", out: `"a"`},
		{form: String.WithOptionalTabIndent(3), in: "a\n", out: `"""
			a

			"""`},

		// Issue #541
		{form: String.WithTabIndent(3), in: "foo\n\"bar\"", out: `"""
			foo
			"bar"
			"""`},
		{form: String.WithTabIndent(3), in: "foo\n\"\"\"bar\"", out: `#"""
			foo
			"""bar"
			"""#`},
		{form: String.WithTabIndent(3), in: "foo\n\"\"\"\"\"###bar\"", out: `####"""
			foo
			"""""###bar"
			"""####`},
		{form: String.WithTabIndent(3), in: "foo\n\"\"\"\r\f\\", out: `#"""
			foo
			"""\#r\#f\#\
			"""#`},
		{form: Bytes.WithTabIndent(3), in: "foo'''\nhello", out: `#'''
			foo'''
			hello
			'''#`},
		{form: Bytes.WithTabIndent(3), in: "foo\n'''\r\f\\", out: `#'''
			foo
			'''\#r\#f\#\
			'''#`},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%q", tc.in), func(t *testing.T) {
			got := tc.form.Quote(tc.in)
			if got != tc.out {
				t.Errorf("Quote: %s", cmp.Diff(tc.out, got))
			}

			got = string(tc.form.Append(nil, tc.in))
			if got != tc.out {
				t.Errorf("Append: %s", cmp.Diff(tc.out, got))
			}

			str, err := Unquote(got)
			if err != nil {
				t.Errorf("Roundtrip error: %v", err)
			}

			if !tc.lossy && str != tc.in {
				t.Errorf("Quote: %s", cmp.Diff(tc.in, str))
			}
		})
	}
}

func TestAppendEscaped(t *testing.T) {
	testCases := []struct {
		form Form
		in   string
		out  string
	}{
		{String, "a", "a"},
		{String, "", ""},
		{String.WithTabIndent(2), "", ""},
		{String.WithTabIndent(2), "\n", "\n"},
		{String.WithTabIndent(2), "a\n", "a\n"},
		{String.WithTabIndent(2), "a\nb", "a\n\t\tb"},
	}
	for _, tc := range testCases {
		t.Run(tc.in, func(t *testing.T) {
			buf := tc.form.AppendEscaped(nil, tc.in)
			if got := string(buf); got != tc.out {
				t.Error(cmp.Diff(tc.out, got))
			}
		})
	}
}

func BenchmarkQuote(b *testing.B) {
	inputs := []string{
		"aaaa",
		"aaaaaaa\n\naaaaaa",
		strings.Repeat("aaaaaaaaaaa\n", 1000),
	}

	for _, f := range []Form{
		String,
		Bytes,
		String.WithTabIndent(3),
		String.WithOptionalTabIndent(3),
	} {
		b.Run("", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				for _, s := range inputs {
					f.Quote(s)
				}
			}
		})
	}
}
