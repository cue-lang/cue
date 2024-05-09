// Copyright 2024 The CUE Authors
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

package toml_test

import (
	"io"
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/toml"
	"github.com/go-quicktest/qt"
)

func TestDecoder(t *testing.T) {
	t.Parallel()
	// Note that we use backquoted Go string literals with indentation for readability.
	// The whitespace doesn't affect the input TOML, and we cue/format on the "want" CUE source,
	// so the added newlines and tabs don't change the test behavior.
	tests := []struct {
		name  string
		input string
		want  string
	}{{
		name:  "Empty",
		input: "",
		want:  "null",
	}, {
		name: "LoneComment",
		input: `
			# Just a comment
		`,
		want: "null",
	}, {
		name: "RootKeysOne",
		input: `
			key = "value"
		`,
		want: `
			key: "value"
		`,
	}, {
		name: "RootKeysMany",
		input: `
			key1 = "value1"
			key2 = "value2"
			key3 = "value3"
		`,
		want: `
			key1: "value1"
			key2: "value2"
			key3: "value3"
		`,
	}, {
		name: "RootKeysDots",
		input: `
			a1       = "A"
			b1.b2    = "B"
			c1.c2.c3 = "C"
		`,
		want: `
			a1: "A"
			b1: b2: "B"
			c1: c2: c3: "C"
		`,
	}, {
		name: "RootKeysCharacters",
		input: `
			a-b = "dashes"
			a_b = "underscores"
			123 = "numbers"
		`,
		want: `
			"a-b": "dashes"
			a_b:   "underscores"
			"123": "numbers"
		`,
	}, {
		name: "RootKeysQuoted",
		input: `
			"1.2.3" = "quoted dots"
			"foo bar" = "quoted space"
			'foo "bar"' = "nested quotes"
		`,
		want: `
			"1.2.3":       "quoted dots"
			"foo bar":     "quoted space"
			"foo \"bar\"": "nested quotes"
		`,
	}, {
		name: "RootKeysMixed",
		input: `
			site."foo.com".title = "foo bar"
		`,
		want: `
			site: "foo.com": title: "foo bar"
		`,
	}, {
		// TODO(mvdan): the TOML spec says that defining a key multiple times is invalid,
		// we should error even though this can be OK in CUE as long as the values unify.
		name: "RootKeysDuplicate",
		input: `
			foo = "same value"
			foo = "same value"
		`,
		want: `
			foo: "same value"
			foo: "same value"
		`,
	}, {
		name: "BasicStrings",
		input: `
			escapes = "foo \"bar\" \n\t\\ baz"
			unicode = "foo \u00E9"
		`,
		want: `
			escapes: "foo \"bar\" \n\t\\ baz"
			unicode: "foo é"
		`,
	}, {
		// Leading tabs do matter in this test.
		// TODO: use our own multiline strings where it gives better results.
		name: "MultilineBasicStrings",
		input: `
nested = """ can contain "" quotes """
four   = """"four""""
double = """
line one
line two"""
double_indented = """
	line one
	line two
	"""
escaped = """\
line one \
line two.\
"""
		`,
		want: `
			nested:           " can contain \"\" quotes "
			four:             "\"four\""
			double:           "line one\nline two"
			double_indented:  "\tline one\n\tline two\n\t"
			escaped:          "line one line two."
		`,
	}, {
		// TODO: we can probably do better in many cases, e.g. #""
		name: "LiteralStrings",
		input: `
			winpath  = 'C:\Users\nodejs\templates'
			winpath2 = '\\ServerX\admin$\system32\'
			quoted   = 'Tom "Dubs" Preston-Werner'
			regex    = '<\i\c*\s*>'
		`,
		want: `
			winpath:  "C:\\Users\\nodejs\\templates"
			winpath2: "\\\\ServerX\\admin$\\system32\\"
			quoted:   "Tom \"Dubs\" Preston-Werner"
			regex:    "<\\i\\c*\\s*>"
		`,
	}, {
		// Leading tabs do matter in this test.
		// TODO: use our own multiline strings where it gives better results.
		name: "MultilineLiteralStrings",
		input: `
nested = ''' can contain '' quotes '''
four   = ''''four''''
double = '''
line one
line two'''
double_indented = '''
	line one
	line two
	'''
escaped = '''\
line one \
line two.\
'''
		`,
		want: `
			nested:           " can contain '' quotes "
			four:             "'four'"
			double:           "line one\nline two"
			double_indented:  "\tline one\n\tline two\n\t"
			escaped:          "\\\nline one \\\nline two.\\\n"
		`,
	}, {
		name: "Integers",
		input: `
			zero        = 0
			positive    = 123
			plus        = +40
			minus       = -40
			underscores = 1_002_003
			hexadecimal = 0xdeadBEEF
			octal       = 0o755
			binary      = 0b11010110
		`,
		want: `
			zero:        0
			positive:    123
			plus:        +40
			minus:       -40
			underscores: 1_002_003
			hexadecimal: 0xdeadBEEF
			octal:       0o755
			binary:      0b11010110
		`,
	}, {
		name: "Floats",
		input: `
			pi             = 3.1415
			plus           = +1.23
			minus          = -4.56
			exponent       = 1e067
			exponent_plus  = 5e+20
			exponent_minus = -2E-4
			exponent_dot   = 6.789e-30
		`,
		want: `
			pi:             3.1415
			plus:           +1.23
			minus:          -4.56
			exponent:       1e067
			exponent_plus:  5e+20
			exponent_minus: -2E-4
			exponent_dot:   6.789e-30
		`,
	}, {
		name: "Bools",
		input: `
			positive = true
			negative = false
		`,
		want: `
			positive: true
			negative: false
		`,
	}, {
		name: "Arrays",
		input: `
			integers      = [ 1, 2, 3 ]
			colors        = [ "red", "yellow", "green" ]
			nested_ints   = [ [ 1, 2 ], [3, 4, 5] ]
			nested_mixed  = [ [ 1, 2 ], ["a", "b", "c"] ]
			strings       = [ "all", 'strings', """are the same""", '''type''' ]
			mixed_numbers = [ 0.1, 0.2, 0.5, 1, 2, 5 ]
		`,
		want: `
			integers:      [1, 2, 3]
			colors:        ["red", "yellow", "green"]
			nested_ints:   [[1, 2], [3, 4, 5]]
			nested_mixed:  [[1, 2], ["a", "b", "c"]]
			strings:       ["all", "strings", "are the same", "type"]
			mixed_numbers: [0.1, 0.2, 0.5, 1, 2, 5]
		`,
	}}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dec := toml.NewDecoder(strings.NewReader(test.input))

			node, err := dec.Decode()
			qt.Assert(t, qt.IsNil(err))

			node2, err := dec.Decode()
			qt.Assert(t, qt.IsNil(node2))
			qt.Assert(t, qt.Equals(err, io.EOF))

			wantFormatted, err := format.Source([]byte(test.want))
			qt.Assert(t, qt.IsNil(err))

			formatted, err := format.Node(node)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(formatted), string(wantFormatted)))

			// TODO(mvdan): validate that the decoded CUE values are equivalent
			// to the Go values that a direct TOML unmarshal would produce.
			// For example, compare JSON equality between the CUE encoded as JSON
			// and the TOML decoded into `any` and encoded as JSON.
		})
	}
}
