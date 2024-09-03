// Many test cases in this file were originally ported from
// github.com/go-yaml/yaml, also known as gopkg.in/yaml.v3.
//
// Copyright 2011-2016 Canonical Ltd.
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

package yaml_test

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/encoding/yaml"
)

var unmarshalTests = []struct {
	data string
	want string
}{
	{
		"",
		"null",
	},
	{
		"{}",
		"",
	}, {
		"v: hi",
		`v: "hi"`,
	}, {
		"v: hi",
		`v: "hi"`,
	}, {
		"v: true",
		"v: true",
	}, {
		"v: 10",
		"v: 10",
	}, {
		"v: 0b10",
		"v: 0b10",
	}, {
		"v: 0xA",
		"v: 0xA",
	}, {
		"v: 4294967296",
		"v: 4294967296",
	}, {
		"v: 0.1",
		"v: 0.1",
	}, {
		"v: .1",
		"v: 0.1",
	}, {
		"v: .Inf",
		"v: +Inf",
	}, {
		"v: -.Inf",
		"v: -Inf",
	}, {
		"v: -10",
		"v: -10",
	}, {
		"v: --10",
		`v: "--10"`,
	}, {
		"v: -.1",
		"v: -0.1",
	},

	// Simple values.
	{
		"123",
		"123",
	},

	// Floats from spec
	{
		"canonical: 6.8523e+5",
		"canonical: 6.8523e+5",
	}, {
		"expo: 685.230_15e+03",
		"expo: 685.230_15e+03",
	}, {
		"fixed: 685_230.15",
		"fixed: 685_230.15",
	}, {
		"neginf: -.inf",
		"neginf: -Inf",
	}, {
		"fixed: 685_230.15",
		"fixed: 685_230.15",
	},
	//{"sexa: 190:20:30.15", map[string]interface{}{"sexa": 0}}, // Unsupported
	//{"notanum: .NaN", map[string]interface{}{"notanum": math.NaN()}}, // Equality of NaN fails.

	// Bools from spec
	{
		"canonical: y",
		`canonical: "y"`,
	}, {
		"answer: n",
		`answer: "n"`,
	}, {
		"answer: NO",
		`answer: "NO"`,
	}, {
		"logical: True",
		"logical: true",
	}, {
		"option: on",
		`option: "on"`,
	}, {
		"answer: off",
		`answer: "off"`,
	},
	// Ints from spec
	{
		"canonical: 685230",
		"canonical: 685230",
	}, {
		"decimal: +685_230",
		"decimal: +685_230",
	}, {
		"octal_yaml11: 02472256",
		"octal_yaml11: 0o2472256",
	}, {
		"octal_yaml12: 0o2472256",
		"octal_yaml12: 0o2472256",
	}, {
		"not_octal_yaml11: 0123456789",
		`not_octal_yaml11: "0123456789"`,
	}, {
		"not_octal_yaml12: 0o123456789",
		`not_octal_yaml12: "0o123456789"`,
	}, {
		// TODO(mvdan): 01234 is not a valid numeric literal in CUE.
		"float_octal_yaml11: !!float 01234",
		"float_octal_yaml11: number & 01234",
	}, {
		"float_octal_yaml12: !!float 0o1234",
		"float_octal_yaml12: number & 0o1234",
	}, {
		"hexa: 0x_0A_74_AE",
		"hexa: 0x_0A_74_AE",
	}, {
		"bin: 0b1010_0111_0100_1010_1110",
		"bin: 0b1010_0111_0100_1010_1110",
	}, {
		"bin: -0b101010",
		"bin: -0b101010",
	}, {
		"bin: -0b1000000000000000000000000000000000000000000000000000000000000000",
		"bin: -0b1000000000000000000000000000000000000000000000000000000000000000",
	}, {
		"decimal: +685_230",
		"decimal: +685_230",
	},

	//{"sexa: 190:20:30", map[string]interface{}{"sexa": 0}}, // Unsupported

	// Nulls from spec
	{
		"empty:",
		"empty: null",
	}, {
		"canonical: ~",
		"canonical: null",
	}, {
		"english: null",
		"english: null",
	}, {
		"_foo: 1",
		`"_foo": 1`,
	}, {
		`"#foo": 1`,
		`"#foo": 1`,
	}, {
		"_#foo: 1",
		`"_#foo": 1`,
	}, {
		"~: null key",
		`"null": "null key"`,
	}, {
		`empty:
apple: "newline"`,
		`empty: null
apple: "newline"`,
	},

	// Flow sequence
	{
		"seq: [A,B]",
		`seq: ["A", "B"]`,
	}, {
		"seq: [A,B,C,]",
		`seq: ["A", "B", "C"]`,
	}, {
		"seq: [A,1,C]",
		`seq: ["A", 1, "C"]`,
	},
	// Block sequence
	{
		"seq:\n - A\n - B",
		`seq: [
	"A",
	"B",
]`,
	}, {
		"seq:\n - A\n - B\n - C",
		`seq: [
	"A",
	"B",
	"C",
]`,
	}, {
		"seq:\n - A\n - 1\n - C",
		`seq: [
	"A",
	1,
	"C",
]`,
	},

	// Literal block scalar
	{
		"scalar: | # Comment\n\n literal\n\n \ttext\n\n",
		`scalar: """

	literal

	\ttext

	""" // Comment`,
	},

	// Folded block scalar
	{
		"scalar: > # Comment\n\n folded\n line\n \n next\n line\n  * one\n  * two\n\n last\n line\n\n",
		`scalar: """

	folded line
	next line
	 * one
	 * two

	last line

	""" // Comment`,
	},

	// Structs
	{
		"a: {b: c}",
		`a: {b: "c"}`,
	},
	{
		"hello: world",
		`hello: "world"`,
	}, {
		"a:",
		"a: null",
	}, {
		"a: 1",
		"a: 1",
	}, {
		"a: 1.0",
		"a: 1.0",
	}, {
		"a: [1, 2]",
		"a: [1, 2]",
	}, {
		"a: y",
		`a: "y"`,
	}, {
		"{ a: 1, b: {c: 1} }",
		`a: 1, b: {c: 1}`,
	}, {
		`
True: 1
Null: 1
.Inf: 2
`,
		`"true": 1
"null": 1
"+Inf": 2`,
	},

	// Some cross type conversions
	{
		"v: 42",
		"v: 42",
	}, {
		"v: -42",
		"v: -42",
	}, {
		"v: 4294967296",
		"v: 4294967296",
	}, {
		"v: -4294967296",
		"v: -4294967296",
	},

	// int
	{
		"int_max: 2147483647",
		"int_max: 2147483647",
	},
	{
		"int_min: -2147483648",
		"int_min: -2147483648",
	},
	{
		"int_overflow: 9223372036854775808", // math.MaxInt64 + 1
		"int_overflow: 9223372036854775808", // math.MaxInt64 + 1
	},

	// int64
	{
		"int64_max: 9223372036854775807",
		"int64_max: 9223372036854775807",
	},
	{
		"int64_max_base2: 0b111111111111111111111111111111111111111111111111111111111111111",
		"int64_max_base2: 0b111111111111111111111111111111111111111111111111111111111111111",
	},
	{
		"int64_min: -9223372036854775808",
		"int64_min: -9223372036854775808",
	},
	{
		"int64_neg_base2: -0b111111111111111111111111111111111111111111111111111111111111111",
		"int64_neg_base2: -0b111111111111111111111111111111111111111111111111111111111111111",
	},
	{
		"int64_overflow: 9223372036854775808", // math.MaxInt64 + 1
		"int64_overflow: 9223372036854775808", // math.MaxInt64 + 1
	},

	// uint
	{
		"uint_max: 4294967295",
		"uint_max: 4294967295",
	},

	// uint64
	{
		"uint64_max: 18446744073709551615",
		"uint64_max: 18446744073709551615",
	},
	{
		"uint64_max_base2: 0b1111111111111111111111111111111111111111111111111111111111111111",
		"uint64_max_base2: 0b1111111111111111111111111111111111111111111111111111111111111111",
	},
	{
		"uint64_maxint64: 9223372036854775807",
		"uint64_maxint64: 9223372036854775807",
	},

	// float32
	{
		"float32_max: 3.40282346638528859811704183484516925440e+38",
		"float32_max: 3.40282346638528859811704183484516925440e+38",
	},
	{
		"float32_nonzero: 1.401298464324817070923729583289916131280e-45",
		"float32_nonzero: 1.401298464324817070923729583289916131280e-45",
	},
	{
		"float32_maxuint64: 18446744073709551615",
		"float32_maxuint64: 18446744073709551615",
	},
	{
		"float32_maxuint64+1: 18446744073709551616",
		`"float32_maxuint64+1": number & 18446744073709551616`,
	},

	// float64
	{
		"float64_max: 1.797693134862315708145274237317043567981e+308",
		"float64_max: 1.797693134862315708145274237317043567981e+308",
	},
	{
		"float64_nonzero: 4.940656458412465441765687928682213723651e-324",
		"float64_nonzero: 4.940656458412465441765687928682213723651e-324",
	},
	{
		"float64_maxuint64: 18446744073709551615",
		"float64_maxuint64: 18446744073709551615",
	},
	// TODO(mvdan): yaml.v3 uses strconv APIs like ParseUint to try to detect
	// whether a scalar should be considered a YAML integer or a float.
	// Integers in CUE aren't limited to 64 bits, so we should arguably not decode
	// large integers that don't fit in 64 bits as floats via `number &`.
	{
		"float64_maxuint64+1: 18446744073709551616",
		`"float64_maxuint64+1": number & 18446744073709551616`,
	},

	// Overflow cases.
	{
		"v: 4294967297",
		"v: 4294967297",
	}, {
		"v: 128",
		"v: 128",
	},

	// Quoted values.
	{
		"'1': '\"2\"'",
		`"1": "\"2\""`,
	}, {
		"v:\n- A\n- 'B\n\n  C'\n",
		`v: [
	"A",
	"""
		B
		C
		""",
]`,
	}, {
		`"\0"`,
		`"\u0000"`,
	},

	// Explicit tags.
	{
		"v: !!float '1.1'",
		"v: 1.1",
	}, {
		"v: !!float 0",
		"v: number & 0",
	}, {
		"v: !!float -1",
		"v: number & -1",
	}, {
		"v: !!null ''",
		"v: null",
	}, {
		"%TAG !y! tag:yaml.org,2002:\n---\nv: !y!int '1'",
		"v: 1",
	},

	// Non-specific tag (Issue #75)
	{
		`v: ! test`,
		`v: "test"`,
	},

	// Anchors and aliases.
	{
		"a: &x 1\nb: &y 2\nc: *x\nd: *y\n",
		`a: 1
b: 2
c: 1
d: 2`,
	}, {
		"a: &a {c: 1}\nb: *a",
		`a: {c: 1}
b: {
	c: 1
}`,
	}, {
		"a: &a [1, 2]\nb: *a",
		"a: [1, 2]\nb: [1, 2]", // TODO: a: [1, 2], b: a
	},

	{
		"foo: ''",
		`foo: ""`,
	}, {
		"foo: null",
		"foo: null",
	},

	// Support for ~
	{
		"foo: ~",
		"foo: null",
	},

	// Bug #1191981
	{
		"" +
			"%YAML 1.1\n" +
			"--- !!str\n" +
			`"Generic line break (no glyph)\n\` + "\n" +
			` Generic line break (glyphed)\n\` + "\n" +
			` Line separator\u2028\` + "\n" +
			` Paragraph separator\u2029"` + "\n",
		`"""
	Generic line break (no glyph)
	Generic line break (glyphed)
	Line separator\u2028Paragraph separator\u2029
	"""`,
	},

	// bug 1243827
	{
		"a: -b_c",
		`a: "-b_c"`,
	},
	{
		"a: +b_c",
		`a: "+b_c"`,
	},
	{
		"a: 50cent_of_dollar",
		`a: "50cent_of_dollar"`,
	},

	// issue #295 (allow scalars with colons in flow mappings and sequences)
	{
		"a: {b: https://github.com/go-yaml/yaml}",
		`a: {b: "https://github.com/go-yaml/yaml"}`,
	},
	{
		"a: [https://github.com/go-yaml/yaml]",
		`a: ["https://github.com/go-yaml/yaml"]`,
	},

	// Duration
	{
		"a: 3s",
		`a: "3s"`, // for now
	},

	// Issue #24.
	{
		"a: <foo>",
		`a: "<foo>"`,
	},

	// Base 60 floats are obsolete and unsupported.
	{
		"a: 1:1\n",
		`a: "1:1"`,
	},

	// Binary data.
	{
		"a: !!binary gIGC\n",
		`a: '\x80\x81\x82'`,
	}, {
		"a: !!binary |\n  " + strings.Repeat("kJCQ", 17) + "kJ\n  CQ\n",
		"a: '" + strings.Repeat(`\x90`, 54) + "'",
	}, {
		"a: !!binary |\n  " + strings.Repeat("A", 70) + "\n  ==\n",
		"a: '" + strings.Repeat(`\x00`, 52) + "'",
	},

	// Ordered maps.
	{
		"{b: 2, a: 1, d: 4, c: 3, sub: {e: 5}}",
		`b: 2, a: 1, d: 4, c: 3, sub: {e: 5}`,
	},

	// Spacing
	{
		`
a: {}
b: {
}
c: 1
d: [
]
e: []
`,
		// TODO(mvdan): keep the separated opening/closing tokens once yaml.v3 exposes end positions.
		`a: {}
b: {}
c: 1
d: []
e: []`,
	},

	{
		`
a:
  - { "a": 1, "b": 2 }
  - { "c": 1, "d": 2 }
`,
		`a: [{
	a: 1, b: 2
}, {
	c: 1, d: 2
}]`,
	},

	{
		"a:\n b:\n  c: d\n  e: f\n",
		`a: {
	b: {
		c: "d"
		e: "f"
	}
}`,
	},

	// Issue #39.
	{
		"a:\n b:\n  c: d\n",
		`a: {
	b: {
		c: "d"
	}
}`,
	},

	// Timestamps
	{
		// Date only.
		"a: 2015-01-01\n",
		`a: "2015-01-01"`,
	},
	{
		// RFC3339
		"a: 2015-02-24T18:19:39.12Z\n",
		`a: "2015-02-24T18:19:39.12Z"`,
	},
	{
		// RFC3339 with short dates.
		"a: 2015-2-3T3:4:5Z",
		`a: "2015-2-3T3:4:5Z"`,
	},
	{
		// ISO8601 lower case t
		"a: 2015-02-24t18:19:39Z\n",
		`a: "2015-02-24t18:19:39Z"`,
	},
	{
		// space separate, no time zone
		"a: 2015-02-24 18:19:39\n",
		`a: "2015-02-24 18:19:39"`,
	},
	// Some cases not currently handled. Uncomment these when
	// the code is fixed.
	//	{
	//		// space separated with time zone
	//		"a: 2001-12-14 21:59:43.10 -5",
	//		map[string]interface{}{"a": time.Date(2001, 12, 14, 21, 59, 43, .1e9, time.UTC)},
	//	},
	//	{
	//		// arbitrary whitespace between fields
	//		"a: 2001-12-14 \t\t \t21:59:43.10 \t Z",
	//		map[string]interface{}{"a": time.Date(2001, 12, 14, 21, 59, 43, .1e9, time.UTC)},
	//	},
	{
		// explicit string tag
		"a: !!str 2015-01-01",
		`a: "2015-01-01"`,
	},
	{
		// explicit timestamp tag on quoted string
		"a: !!timestamp \"2015-01-01\"",
		`a: "2015-01-01"`,
	},
	{
		// explicit timestamp tag on unquoted string
		"a: !!timestamp 2015-01-01",
		`a: "2015-01-01"`,
	},
	{
		// quoted string that's a valid timestamp
		"a: \"2015-01-01\"",
		"a: \"2015-01-01\"",
	},

	// Empty list
	{
		"a: []",
		"a: []",
	},

	// Floating comments.
	// TODO(mvdan): all empty lines separating comments should stay in place.
	// TODO(mvdan): avoid losing comments in empty lists and objects.
	{
		"# Start\n\na: 123\n\n# Middle\n\nb: 456\n\n# End",
		"// Start\na: 123\n\n// Middle\nb: 456\n// End",
	},
	{
		"a: [\n\t# Comment\n]",
		"a: []",
	},
	{
		"a: {\n\t# Comment\n}",
		"a: {}",
	},

	// Attached comments.
	{
		"start: 100\n\n# Before\na: 123 # Inline\n# After\n\nend: 200",
		"start: 100\n\n// Before\n// After\na:   123 // Inline\nend: 200",
	},
	{
		"# One\none: null\n\n# Two\ntwo: [2, 2]\n\n# Three\nthree: {val: 3}",
		"// One\none: null\n\n// Two\ntwo: [2, 2]\n\n// Three\nthree: {val: 3}",
	},

	// UTF-16-LE
	{
		"\xff\xfe\xf1\x00o\x00\xf1\x00o\x00:\x00 \x00v\x00e\x00r\x00y\x00 \x00y\x00e\x00s\x00\n\x00",
		`ñoño: "very yes"`,
	},
	// UTF-16-LE with surrogate.
	{
		"\xff\xfe\xf1\x00o\x00\xf1\x00o\x00:\x00 \x00v\x00e\x00r\x00y\x00 \x00y\x00e\x00s\x00 \x00=\xd8\xd4\xdf\n\x00",
		`ñoño: "very yes 🟔"`,
	},

	// UTF-16-BE
	{
		"\xfe\xff\x00\xf1\x00o\x00\xf1\x00o\x00:\x00 \x00v\x00e\x00r\x00y\x00 \x00y\x00e\x00s\x00\n",
		`ñoño: "very yes"`,
	},
	// UTF-16-BE with surrogate.
	{
		"\xfe\xff\x00\xf1\x00o\x00\xf1\x00o\x00:\x00 \x00v\x00e\x00r\x00y\x00 \x00y\x00e\x00s\x00 \xd8=\xdf\xd4\x00\n",
		`ñoño: "very yes 🟔"`,
	},

	// This *is* in fact a float number, per the spec. #171 was a mistake.
	{
		"a: 123456e1\n",
		`a: 123456e1`,
	}, {
		"a: 123456E1\n",
		`a: 123456e1`,
	},
	// Other float formats:
	{
		"x: .1",
		"x: 0.1",
	},
	{
		"x: .1e-3",
		"x: 0.1e-3",
	},
	{
		"x: 1.2E4",
		"x: 1.2e4",
	},
	{
		"x: 1.2E+4",
		"x: 1.2e+4",
	},
	// yaml-test-suite 3GZX: Spec Example 7.1. Alias Nodes
	{
		"First occurrence: &anchor Foo\nSecond occurrence: *anchor\nOverride anchor: &anchor Bar\nReuse anchor: *anchor\n",
		`"First occurrence":  "Foo"
"Second occurrence": "Foo"
"Override anchor":   "Bar"
"Reuse anchor":      "Bar"`,
	},
}

type M map[interface{}]interface{}

func cueStr(node ast.Node) string {
	if node == nil {
		return ""
	}
	b, _ := format.Node(internal.ToFile(node))
	return strings.TrimSpace(string(b))
}

func newDecoder(t *testing.T, data string) yaml.Decoder {
	t.Helper()
	t.Logf("  yaml:\n%s", data)
	return yaml.NewDecoder("test.yaml", []byte(data))
}

func callUnmarshal(t *testing.T, data string) (ast.Expr, error) {
	t.Helper()
	t.Logf("  yaml:\n%s", data)
	return yaml.Unmarshal("test.yaml", []byte(data))
}

func TestUnmarshal(t *testing.T) {
	for i, item := range unmarshalTests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Logf("test %d: %q", i, item.data)
			expr, err := callUnmarshal(t, item.data)
			if err != nil {
				t.Fatalf("expected error to be nil: %v", err)
			}
			if got := cueStr(expr); got != item.want {
				t.Errorf("\n    got:\n%v\n    want:\n%v", got, item.want)
			}
		})
	}
}

// For debug purposes: do not delete.
func TestX(t *testing.T) {
	y := `
`
	y = strings.TrimSpace(y)
	if len(y) == 0 {
		t.Skip()
	}

	expr, err := callUnmarshal(t, y)
	if err != nil {
		t.Fatal(err)
	}
	t.Error(cueStr(expr))
}

func TestDecoderSingleDocument(t *testing.T) {
	// Test that Decoder.Decode works as expected on
	// all the unmarshal tests.
	for i, item := range unmarshalTests {
		t.Run(fmt.Sprintf("test %d: %q", i, item.data), func(t *testing.T) {
			if item.data == "" {
				// Behaviour differs when there's no YAML.
				return
			}
			expr, err := newDecoder(t, item.data).Decode()
			if err != nil {
				t.Errorf("err should be nil, was %v", err)
			}
			if got := cueStr(expr); got != item.want {
				t.Errorf("\n    got:\n%v\n    want:\n%v", got, item.want)
			}
		})
	}
}

var decoderTests = []struct {
	data string
	want string
}{{
	"",
	"null",
}, {
	"a: b",
	`a: "b"`,
}, {
	"---\na: b\n...\n",
	`a: "b"`,
}, {
	"---\na: b\n---\n---\n",
	"a: \"b\"\nnull\nnull",
}, {
	"---\n---\n---\na: b\n",
	"null\nnull\na: \"b\"",
}, {
	"---\n'hello'\n...\n---\ngoodbye\n...\n",
	`"hello"` + "\n" + `"goodbye"`,
}}

func TestDecoder(t *testing.T) {
	for i, item := range decoderTests {
		t.Run(fmt.Sprintf("test %d: %q", i, item.data), func(t *testing.T) {
			var values []string
			dec := newDecoder(t, item.data)
			for {
				expr, err := dec.Decode()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("err should be nil, was %v", err)
				}
				values = append(values, cueStr(expr))
			}
			got := strings.Join(values, "\n")
			if got != item.want {
				t.Errorf("\n    got:\n%v\n    want:\n%v", got, item.want)
			}
		})
	}
}

func TestUnmarshalNaN(t *testing.T) {
	expr, err := callUnmarshal(t, "notanum: .NaN")
	if err != nil {
		t.Fatal("unexpected error", err)
	}
	got := cueStr(expr)
	want := "notanum: NaN"
	if got != want {
		t.Errorf("got %v; want %v", got, want)
	}
}

var unmarshalErrorTests = []struct {
	data, error string
}{
	{"\nv: !!float 'error'", `test.yaml:2: cannot decode "error" as !!float: illegal number start "error"`},
	{"\nv: !!int 'error'", `test.yaml:2: cannot decode "error" as !!int: illegal number start "error"`},
	{"\nv: !!int 123.456", `test.yaml:2: cannot decode "123.456" as !!int: not a literal number`},
	{"v: [A,", "test.yaml:1: did not find expected node content"},
	{"v:\n- [A,", "test.yaml:2: did not find expected node content"},
	{"a:\n- b: *,", "test.yaml:2: did not find expected alphabetic or numeric character"},
	{"a: *b\n", "test.yaml: unknown anchor 'b' referenced"},
	{"a: &a\n  b: *a\n", `test.yaml:2: anchor "a" value contains itself`},
	{"value: -", "test.yaml: block sequence entries are not allowed in this context"},
	{"a: !!binary ==", "test.yaml:1: !!binary value contains invalid base64 data"},
	{"{[.]}", `test.yaml:1: invalid map key: !!seq`},
	{"{{.}}", `test.yaml:1: invalid map key: !!map`},
	{"b: *a\na: &a {c: 1}", `test.yaml: unknown anchor 'a' referenced`},
	{"%TAG !%79! tag:yaml.org,2002:\n---\nv: !%79!int '1'", "test.yaml: did not find expected whitespace"},
}

func TestUnmarshalErrors(t *testing.T) {
	for i, item := range unmarshalErrorTests {
		t.Run(fmt.Sprintf("test %d: %q", i, item.data), func(t *testing.T) {
			expr, err := callUnmarshal(t, item.data)
			val := ""
			if expr != nil {
				val = cueStr(expr)
			}
			if err == nil || err.Error() != item.error {
				t.Errorf("got %v; want %v; (value %v)", err, item.error, val)
			}
		})
	}
}

func TestDecoderErrors(t *testing.T) {
	for i, item := range unmarshalErrorTests {
		t.Run(fmt.Sprintf("test %d: %q", i, item.data), func(t *testing.T) {
			_, err := newDecoder(t, item.data).Decode()
			if err == nil || err.Error() != item.error {
				t.Errorf("got %v; want %v", err, item.error)
			}
		})
	}
}

func TestFiles(t *testing.T) {
	files := []string{"merge"}
	for _, test := range files {
		t.Run(test, func(t *testing.T) {
			testname := fmt.Sprintf("testdata/%s.test", test)
			filename := fmt.Sprintf("testdata/%s.out", test)
			mergeTests, err := os.ReadFile(testname)
			if err != nil {
				t.Fatal(err)
			}
			expr, err := yaml.Unmarshal("test.yaml", mergeTests)
			if err != nil {
				t.Fatal(err)
			}
			got := cueStr(expr)
			if cuetest.UpdateGoldenFiles {
				os.WriteFile(filename, []byte(got), 0666)
				return
			}
			b, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if want := string(b); got != want {
				t.Error(cmp.Diff(want, got))
			}
		})
	}
}

func TestTrailingInput(t *testing.T) {
	for _, input := range []string{
		"---\nfirst\n...\n}invalid yaml",
		"---\nfirst\n---\nsecond\n",
	} {
		t.Run("", func(t *testing.T) {
			// Unmarshal should fail as it expects one value.
			wantErr := ".*expected a single YAML document.*"
			_, err := callUnmarshal(t, input)
			qt.Assert(t, qt.ErrorMatches(err, wantErr))

			// A single Decode call should succeed, no matter whether there is any valid or invalid trailing input.
			wantFirst := `"first"`
			dec := newDecoder(t, input)
			expr, err := dec.Decode()
			qt.Assert(t, qt.IsNil(err))
			gotFirst := cueStr(expr)
			qt.Assert(t, qt.Equals(gotFirst, wantFirst))
		})
	}

}
