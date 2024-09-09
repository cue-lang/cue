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
	"bytes"
	"encoding/json"
	"io"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	gotoml "github.com/pelletier/go-toml/v2"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/toml"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetxtar"
)

func TestDecoder(t *testing.T) {
	t.Parallel()
	// Note that we use backquoted Go string literals with indentation for readability.
	// The whitespace doesn't affect the input TOML, and we cue/format on the "want" CUE source,
	// so the added newlines and tabs don't change the test behavior.
	tests := []struct {
		name    string
		input   string
		wantCUE string
		wantErr string
	}{{
		name:    "Empty",
		input:   "",
		wantCUE: "",
	}, {
		name: "LoneComment",
		input: `
			# Just a comment
			`,
		wantCUE: "",
	}, {
		name: "RootKeyMissing",
		input: `
			# A comment to verify that parser positions work.
			= "no key name"
			`,
		wantErr: `
			invalid character at start of key: =:
			    test.toml:2:1
			`,
	}, {
		name: "RootKeysOne",
		input: `
			key = "value"
			`,
		wantCUE: `
			key: "value"
			`,
	}, {
		name: "RootMultiple",
		input: `
			key1 = "value1"
			key2 = "value2"
			key3 = "value3"
			`,
		wantCUE: `
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
		wantCUE: `
			a1: "A"
			b1: b2: "B"
			c1: c2: c3: "C"
			`,
	}, {
		name: "RootKeysCharacters",
		input: `
			a-b = "dashes"
			a_b = "underscore unquoted"
			_   = "underscore quoted"
			_ab = "underscore prefix quoted"
			123 = "numbers"
			x._.y._ = "underscores quoted"
			`,
		wantCUE: `
			"a-b": "dashes"
			a_b:   "underscore unquoted"
			"_":   "underscore quoted"
			"_ab": "underscore prefix quoted"
			"123": "numbers"
			x: "_": y: "_": "underscores quoted"
			`,
	}, {
		name: "RootKeysQuoted",
		input: `
			"1.2.3" = "quoted dots"
			"foo bar" = "quoted space"
			'foo "bar"' = "nested quotes"
			`,
		wantCUE: `
			"1.2.3":       "quoted dots"
			"foo bar":     "quoted space"
			"foo \"bar\"": "nested quotes"
			`,
	}, {
		name: "RootKeysMixed",
		input: `
			site."foo.com".title = "foo bar"
			`,
		wantCUE: `
			site: "foo.com": title: "foo bar"
			`,
	}, {
		name: "KeysDuplicateSimple",
		input: `
			foo = "same key"
			foo = "same key"
			`,
		wantErr: `
			duplicate key: foo:
			    test.toml:2:1
			`,
	}, {
		name: "KeysDuplicateQuoted",
		input: `
			"foo" = "same key"
			foo = "same key"
			`,
		wantErr: `
			duplicate key: foo:
			    test.toml:2:1
			`,
	}, {
		name: "KeysDuplicateWhitespace",
		input: `
			foo . bar = "same key"
			foo.bar = "same key"
			`,
		wantErr: `
			duplicate key: foo.bar:
			    test.toml:2:1
			`,
	}, {
		name: "KeysDuplicateDots",
		input: `
			foo."bar.baz".zzz = "same key"
			foo."bar.baz".zzz = "same key"
			`,
		wantErr: `
			duplicate key: foo."bar.baz".zzz:
			    test.toml:2:1
			`,
	}, {
		name: "KeysNotDuplicateDots",
		input: `
			foo."bar.baz" = "different key"
			"foo.bar".baz = "different key"
			`,
		wantCUE: `
			foo: "bar.baz": "different key"
			"foo.bar": baz: "different key"
			`,
	}, {
		name: "BasicStrings",
		input: `
			escapes = "foo \"bar\" \n\t\\ baz"
			unicode = "foo \u00E9"
			`,
		wantCUE: `
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
		wantCUE: `
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
		wantCUE: `
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
		wantCUE: `
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
		wantCUE: `
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
		wantCUE: `
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
		wantCUE: `
			positive: true
			negative: false
			`,
	}, {
		name: "DateTimes",
		input: `
			offsetDateTime1 = 1979-05-27T07:32:00Z
			offsetDateTime2 = 1979-05-27T00:32:00-07:00
			offsetDateTime3 = 1979-05-27T00:32:00.999999-07:00
			localDateTime1 = 1979-05-27T07:32:00
			localDateTime2 = 1979-05-27T00:32:00.999999
			localDate1 = 1979-05-27
			localTime1 = 07:32:00
			localTime2 = 00:32:00.999999

			inlineArray = [1979-05-27, 07:32:00]

			notActuallyDate = "1979-05-27"
			notActuallyTime = "07:32:00"
			inlineArrayNotActually = ["1979-05-27", "07:32:00"]
			`,
		wantCUE: `
			import "time"

			offsetDateTime1: "1979-05-27T07:32:00Z" & time.Format(time.RFC3339)
			offsetDateTime2: "1979-05-27T00:32:00-07:00" & time.Format(time.RFC3339)
			offsetDateTime3: "1979-05-27T00:32:00.999999-07:00" & time.Format(time.RFC3339)
			localDateTime1: "1979-05-27T07:32:00" & time.Format("2006-01-02T15:04:05")
			localDateTime2: "1979-05-27T00:32:00.999999" & time.Format("2006-01-02T15:04:05")
			localDate1: "1979-05-27" & time.Format(time.RFC3339Date)
			localTime1: "07:32:00" & time.Format("15:04:05")
			localTime2: "00:32:00.999999" & time.Format("15:04:05")
			inlineArray: ["1979-05-27" & time.Format(time.RFC3339Date), "07:32:00" & time.Format("15:04:05")]
			notActuallyDate: "1979-05-27"
			notActuallyTime: "07:32:00"
			inlineArrayNotActually: ["1979-05-27", "07:32:00"]
			`,
	}, {
		name: "Arrays",
		input: `
			integers      = [1, 2, 3]
			colors        = ["red", "yellow", "green"]
			nested_ints   = [[1, 2], [3, 4, 5]]
			nested_mixed  = [[1, 2], ["a", "b", "c"], {extra = "keys"}]
			strings       = ["all", 'strings', """are the same""", '''type''']
			mixed_numbers = [0.1, 0.2, 0.5, 1, 2, 5]
			`,
		wantCUE: `
			integers:      [1, 2, 3]
			colors:        ["red", "yellow", "green"]
			nested_ints:   [[1, 2], [3, 4, 5]]
			nested_mixed:  [[1, 2], ["a", "b", "c"], {extra: "keys"}]
			strings:       ["all", "strings", "are the same", "type"]
			mixed_numbers: [0.1, 0.2, 0.5, 1, 2, 5]
			`,
	}, {
		name: "InlineTables",
		input: `
			empty  = {}
			point  = {x = 1, y = 2}
			animal = {type.name = "pug"}
			deep   = {l1 = {l2 = {l3 = "leaf"}}}
			`,
		wantCUE: `
			empty:  {}
			point:  {x: 1, y: 2}
			animal: {type: name: "pug"}
			deep:   {l1: {l2: {l3: "leaf"}}}
			`,
	}, {
		name: "InlineTablesDuplicate",
		input: `
			point = {x = "same key", x = "same key"}
			`,
		wantErr: `
			duplicate key: point.x:
			    test.toml:1:26
			`,
	}, {
		name: "ArrayInlineTablesDuplicate",
		input: `
			point = [{}, {}, {x = "same key", x = "same key"}]
			`,
		wantErr: `
			duplicate key: point.2.x:
			    test.toml:1:35
			`,
	}, {
		name: "InlineTablesNotDuplicateScoping",
		input: `
			repeat = {repeat = {repeat = "leaf"}}
			struct1 = {sibling = "leaf"}
			struct2 = {sibling = "leaf"}
			arrays = [{sibling = "leaf"}, {sibling = "leaf"}]
			`,
		wantCUE: `
			repeat: {repeat: {repeat: "leaf"}}
			struct1: {sibling: "leaf"}
			struct2: {sibling: "leaf"}
			arrays: [{sibling: "leaf"}, {sibling: "leaf"}]
			`,
	}, {
		name: "TablesEmpty",
		input: `
			[foo]
			[bar]
			`,
		wantCUE: `
			foo: {}
			bar: {}
			`,
	}, {
		name: "TablesOne",
		input: `
			[foo]
			single = "single"
			`,
		wantCUE: `
			foo: {
				single: "single"
			}
			`,
	}, {
		name: "TablesMultiple",
		input: `
			root1 = "root1 value"
			root2 = "root2 value"
			[foo]
			foo1 = "foo1 value"
			foo2 = "foo2 value"
			[bar]
			bar1 = "bar1 value"
			bar2 = "bar2 value"
			`,
		wantCUE: `
			root1: "root1 value"
			root2: "root2 value"
			foo: {
				foo1: "foo1 value"
				foo2: "foo2 value"
			}
			bar: {
				bar1: "bar1 value"
				bar2: "bar2 value"
			}
			`,
	}, {
		// A lot of these edge cases are covered by RootKeys tests already.
		name: "TablesKeysComplex",
		input: `
			[foo.bar . "baz.zzz zzz"]
			one = "1"
			[123-456]
			two = "2"
			`,
		wantCUE: `
			foo: bar: "baz.zzz zzz": {
				one: "1"
			}
			"123-456": {
				two: "2"
			}
			`,
	}, {
		name: "TableKeysDuplicateSimple",
		input: `
			[foo]
			[foo]
			`,
		wantErr: `
			duplicate key: foo:
			    test.toml:2:2
			`,
	}, {
		name: "TableKeysDuplicateOverlap",
		input: `
			[foo]
			bar = "leaf"
			[foo.bar]
			baz = "second leaf"
			`,
		wantErr: `
			duplicate key: foo.bar:
			    test.toml:3:2
			`,
	}, {
		name: "TableInnerKeysDuplicateSimple",
		input: `
			[foo]
			bar = "same key"
			bar = "same key"
			`,
		wantErr: `
			duplicate key: foo.bar:
			    test.toml:3:1
			`,
	}, {
		name: "TablesNotDuplicateScoping",
		input: `
			[repeat]
			repeat.repeat = "leaf"
			[struct1]
			sibling = "leaf"
			[struct2]
			sibling = "leaf"
			`,
		wantCUE: `
			repeat: {
				repeat: repeat: "leaf"
			}
			struct1: {
				sibling: "leaf"
			}
			struct2: {
				sibling: "leaf"
			}
			`,
	}, {
		name: "ArrayTablesEmpty",
		input: `
			[[foo]]
			`,
		wantCUE: `
			foo: [
				{},
			]
			`,
	}, {
		name: "ArrayTablesOne",
		input: `
			[[foo]]
			single = "single"
			`,
		wantCUE: `
			foo: [
				{
					single: "single"
				},
			]
			`,
	}, {
		name: "ArrayTablesMultiple",
		input: `
			root = "root value"
			[[foo]]
			foo1 = "foo1 value"
			foo2 = "foo2 value"
			[[foo]]
			foo3 = "foo3 value"
			foo4 = "foo4 value"
			[[foo]]
			[[foo]]
			single = "single"
			`,
		wantCUE: `
			root: "root value"
			foo: [
				{
					foo1: "foo1 value"
					foo2: "foo2 value"
				},
				{
					foo3: "foo3 value"
					foo4: "foo4 value"
				},
				{},
				{
					single: "single"
				},
			]
			`,
	}, {
		name: "ArrayTablesSeparate",
		input: `
			root = "root value"
			[[foo]]
			foo1 = "foo1 value"
			[[bar]]
			bar1 = "bar1 value"
			[[baz]]
			`,
		wantCUE: `
			root: "root value"
			foo: [
				{
					foo1: "foo1 value"
				},
			]
			bar: [
				{
					bar1: "bar1 value"
				},
			]
			baz: [
				{},
			]
			`,
	}, {
		name: "ArrayTablesSubtable",
		input: `
			[[foo]]
			foo1 = "foo1 value"
			[foo.subtable1]
			sub1 = "sub1 value"
			[foo.subtable2]
			sub2 = "sub2 value"
			[foo.subtable2.deeper]
			sub2d = "sub2d value"
			[[foo]]
			foo2 = "foo2 value"
			`,
		wantCUE: `
			foo: [
				{
					foo1: "foo1 value"
					subtable1: {
						sub1: "sub1 value"
					}
					subtable2: {
						sub2: "sub2 value"
					}
					subtable2: deeper: {
						sub2d: "sub2d value"
					}
				},
				{
					foo2: "foo2 value"
				},
			]
			`,
	}, {
		name: "ArrayTablesNested",
		input: `
			[[foo]]
			foo1 = "foo1 value"
			[[foo.nested1]]
			nest1a = "nest1a value"
			[[foo.nested1]]
			nest1b = "nest1b value"
			[[foo.nested2]]
			nest2 = "nest2 value"
			[[foo.nested2.deeper]]
			nest2d = "nest2d value"
			[[foo.nested3.directly.deeper]]
			nest3d = "nest3d value"
			[[foo]]
			foo2 = "foo2 value"
			`,
		wantCUE: `
			foo: [
				{
					foo1: "foo1 value"
					nested1: [
						{
							nest1a: "nest1a value"
						},
						{
							nest1b: "nest1b value"
						},
					]
					nested2: [
						{
							nest2: "nest2 value"
							deeper: [
								{
									nest2d: "nest2d value"
								}
							]
						},
					]
					nested3: directly: deeper: [
						{
							nest3d: "nest3d value"
						},
					]
				},
				{
					foo2: "foo2 value"
				},
			]
			`,
	}, {
		name: "RedeclareKeyAsTableArray",
		input: `
			foo = "foo value"
			[middle]
			middle = "to ensure we don't rely on the last key"
			[[foo]]
			baz = "baz value"
			`,
		wantErr: `
			cannot redeclare key "foo" as a table array:
			    test.toml:4:3
			`,
	}, {
		name: "RedeclareTableAsTableArray",
		input: `
			[foo]
			bar = "bar value"
			[middle]
			middle = "to ensure we don't rely on the last key"
			[[foo]]
			baz = "baz value"
			`,
		wantErr: `
			cannot redeclare key "foo" as a table array:
			    test.toml:5:3
			`,
	}, {
		name: "RedeclareArrayAsTableArray",
		input: `
			foo = ["inline array"]
			[middle]
			middle = "to ensure we don't rely on the last key"
			[[foo]]
			baz = "baz value"
			`,
		wantErr: `
			cannot redeclare key "foo" as a table array:
			    test.toml:4:3
			`,
	}, {
		name: "RedeclareTableArrayAsKey",
		input: `
			[[foo.foo2]]
			bar = "bar value"
			[middle]
			middle = "to ensure we don't rely on the last key"
			[foo]
			foo2 = "redeclaring"
			`,
		wantErr: `
			cannot redeclare table array "foo.foo2" as a table:
			    test.toml:6:1
			`,
	}, {
		name: "RedeclareTableArrayAsTable",
		input: `
			[[foo]]
			bar = "bar value"
			[middle]
			middle = "to ensure we don't rely on the last key"
			[foo]
			baz = "baz value"
			`,
		wantErr: `
			cannot redeclare table array "foo" as a table:
			    test.toml:5:2
			`,
	}, {
		name: "KeysNotDuplicateTableArrays",
		input: `
			[[foo]]
			bar = "foo.0.bar"
			[[foo]]
			bar = "foo.1.bar"
			[[foo]]
			bar = "foo.2.bar"
			[[foo.nested]]
			bar = "foo.2.nested.0.bar"
			[[foo.nested]]
			bar = "foo.2.nested.1.bar"
			[[foo.nested]]
			bar = "foo.2.nested.2.bar"
			`,
		wantCUE: `
			foo: [
				{
					bar: "foo.0.bar"
				},
				{
					bar: "foo.1.bar"
				},
				{
					bar: "foo.2.bar"
					nested: [
						{
							bar: "foo.2.nested.0.bar"
						},
						{
							bar: "foo.2.nested.1.bar"
						},
						{
							bar: "foo.2.nested.2.bar"
						},
					]
				},
			]
			`,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := unindentMultiline(test.input)
			dec := toml.NewDecoder("test.toml", strings.NewReader(input))

			node, err := dec.Decode()
			if test.wantErr != "" {
				gotErr := strings.TrimSuffix(errors.Details(err, nil), "\n")
				wantErr := unindentMultiline(test.wantErr)

				qt.Assert(t, qt.Equals(gotErr, wantErr))
				qt.Assert(t, qt.IsNil(node))
				// We don't continue, so we can't expect any decoded CUE.
				qt.Assert(t, qt.Equals(test.wantCUE, ""))

				// Validate that go-toml's Unmarshal also rejects this input.
				err = gotoml.Unmarshal([]byte(input), new(any))
				qt.Assert(t, qt.IsNotNil(err))
				return
			}
			qt.Assert(t, qt.IsNil(err))

			file, err := astutil.ToFile(node)
			qt.Assert(t, qt.IsNil(err))

			node2, err := dec.Decode()
			qt.Assert(t, qt.IsNil(node2))
			qt.Assert(t, qt.Equals(err, io.EOF))

			wantFormatted, err := format.Source([]byte(test.wantCUE))
			qt.Assert(t, qt.IsNil(err), qt.Commentf("wantCUE:\n%s", test.wantCUE))

			formatted, err := format.Node(file)
			qt.Assert(t, qt.IsNil(err))
			t.Logf("CUE:\n%s", formatted)
			qt.Assert(t, qt.Equals(string(formatted), string(wantFormatted)))

			// Ensure that the CUE node can be compiled into a cue.Value and validated.
			ctx := cuecontext.New()
			val := ctx.BuildFile(file)
			qt.Assert(t, qt.IsNil(val.Err()))
			qt.Assert(t, qt.IsNil(val.Validate()))

			// Validate that the decoded CUE value is equivalent
			// to the Go value that go-toml's Unmarshal produces.
			// We use JSON equality as some details such as which integer types are used
			// are not actually relevant to an "equal data" check.
			var unmarshalTOML any
			err = gotoml.Unmarshal([]byte(input), &unmarshalTOML)
			qt.Assert(t, qt.IsNil(err))
			jsonTOML, err := json.Marshal(unmarshalTOML)
			qt.Assert(t, qt.IsNil(err))
			t.Logf("json.Marshal via go-toml:\t%s\n", jsonTOML)

			jsonCUE, err := json.Marshal(val)
			qt.Assert(t, qt.IsNil(err))
			t.Logf("json.Marshal via CUE:\t%s\n", jsonCUE)
			qt.Assert(t, qt.JSONEquals(jsonCUE, unmarshalTOML))

			// Ensure that the decoded CUE can be re-encoded as TOML,
			// and the resulting TOML is still JSON-equivalent.
			t.Run("reencode", func(t *testing.T) {
				sb := new(strings.Builder)
				enc := toml.NewEncoder(sb)

				err := enc.Encode(val)
				qt.Assert(t, qt.IsNil(err))
				cueTOML := sb.String()
				t.Logf("reencoded TOML:\n%s", cueTOML)

				var unmarshalCueTOML any
				err = gotoml.Unmarshal([]byte(cueTOML), &unmarshalCueTOML)
				qt.Assert(t, qt.IsNil(err))
				qt.Assert(t, qt.JSONEquals(jsonCUE, unmarshalCueTOML))
			})
		})
	}
}

// unindentMultiline mimics CUE's behavior with `"""` multi-line strings,
// where a leading newline is omitted, and any whitespace preceding the trailing newline
// is removed from the start of all lines.
func unindentMultiline(s string) string {
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		// Not a multi-line string.
		return s
	}
	trim := s[i:]
	s = strings.ReplaceAll(s, trim, "\n")
	s = strings.TrimPrefix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	return s
}

var (
	typNode = reflect.TypeFor[ast.Node]()
	typPos  = reflect.TypeFor[token.Pos]()
)

func TestDecoderTxtar(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "testdata",
		Name: "decode",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		for _, file := range t.Archive.Files {
			if strings.HasPrefix(file.Name, "out/") {
				continue
			}
			dec := toml.NewDecoder(file.Name, bytes.NewReader(file.Data))
			node, err := dec.Decode()
			qt.Assert(t, qt.IsNil(err))

			// Show all valid node positions.
			out := astinternal.AppendDebug(nil, node, astinternal.DebugConfig{
				OmitEmpty: true,
				Filter: func(v reflect.Value) bool {
					t := v.Type()
					return t.Implements(typNode) || t.Kind() == reflect.Slice || t == typPos
				},
			})
			t.Writer(path.Join(file.Name, "positions")).Write(out)
		}
	})
}
