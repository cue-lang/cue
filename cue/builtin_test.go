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

package cue

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

func TestBuiltins(t *testing.T) {
	test := func(pkg, expr string) []*bimport {
		return []*bimport{&bimport{"",
			[]string{fmt.Sprintf("import %q\n(%s)", pkg, expr)},
		}}
	}
	testExpr := func(expr string) []*bimport {
		return []*bimport{&bimport{"",
			[]string{fmt.Sprintf("(%s)", expr)},
		}}
	}
	hexToDec := func(s string) string {
		var x big.Int
		x.SetString(s, 16)
		return x.String()
	}
	testCases := []struct {
		instances []*bimport
		emit      string
	}{{
		test("math", "math.Pi"),
		`3.14159265358979323846264338327950288419716939937510582097494459`,
	}, {
		test("math", "math.Floor(math.Pi)"),
		`3`,
	}, {
		test("math", "math.Pi(3)"),
		`_|_(cannot call non-function Pi (type float))`,
	}, {
		test("math", "math.Floor(3, 5)"),
		`_|_(too many arguments in call to math.Floor (have 2, want 1))`,
	}, {
		test("math", `math.Floor("foo")`),
		`_|_(cannot use "foo" (type string) as number in argument 1 to math.Floor)`,
	}, {
		test("crypto/sha256", `sha256.Sum256("hash me")`),
		`'\xeb \x1a\xf5\xaa\xf0\xd6\x06)\xd3Ҧ\x1eFl\xfc\x0f\xed\xb5\x17\xad\xd81\xec\xacR5\xe1کc\xd6'`,
	}, {
		test("crypto/md5", `len(md5.Sum("hash me"))`),
		`16`,
	}, {
		test("crypto/sha1", `len(sha1.Sum("hash me"))`),
		`20`,
	}, {
		test("crypto/sha256", `len(sha256.Sum256("hash me"))`),
		`32`,
	}, {
		test("crypto/sha256", `len(sha256.Sum224("hash me"))`),
		`28`,
	}, {
		test("crypto/sha512", `len(sha512.Sum512("hash me"))`),
		`64`,
	}, {
		test("crypto/sha512", `len(sha512.Sum384("hash me"))`),
		`48`,
	}, {
		test("crypto/sha512", `len(sha512.Sum512_224("hash me"))`),
		`28`,
	}, {
		test("crypto/sha512", `len(sha512.Sum512_256("hash me"))`),
		`32`,
	}, {
		test("encoding/base64", `base64.Encode(null, "foo")`),
		`"Zm9v"`,
	}, {
		test("encoding/base64", `base64.Decode(null, base64.Encode(null, "foo"))`),
		`'foo'`,
	}, {
		test("encoding/base64", `base64.Decode(null, "foo")`),
		`_|_(error in call to encoding/base64.Decode: illegal base64 data at input byte 0)`,
	}, {
		test("encoding/base64", `base64.Decode({}, "foo")`),
		`_|_(error in call to encoding/base64.Decode: base64: unsupported encoding: cannot use value {} (type struct) as null)`,
	}, {
		test("encoding/hex", `hex.Encode("foo")`),
		`"666f6f"`,
	}, {
		test("encoding/hex", `hex.Decode(hex.Encode("foo"))`),
		`'foo'`,
	}, {
		test("encoding/hex", `hex.Decode("foo")`),
		`_|_(error in call to encoding/hex.Decode: encoding/hex: invalid byte: U+006F 'o')`,
	}, {
		test("encoding/hex", `hex.Dump('foo')`),
		`"00000000  66 6f 6f                                          |foo|\n"`,
	}, {
		test("encoding/json", `json.Validate("{\"a\":10}", {b:string})`),
		`true`,
	}, {
		test("encoding/json", `json.Validate("{\"a\":10}", {a:<3})`),
		`_|_(error in call to encoding/json.Validate: invalid value 10 (out of bound <3))`,
	}, {
		test("encoding/yaml", `yaml.Validate("a: 2\n---\na: 4", {a:<3})`),
		`_|_(error in call to encoding/yaml.Validate: invalid value 4 (out of bound <3))`,
	}, {
		test("encoding/yaml", `yaml.Validate("a: 2\n---\na: 4", {a:<5})`),
		`true`,
	}, {
		test("strconv", `strconv.FormatUint(64, 16)`),
		`"40"`,
	}, {
		// Find a better alternative, as this call should go.
		test("strconv", `strconv.FormatFloat(3.02, 300, 4, 64)`),
		`_|_(int 300 overflows byte in argument 1 in call to strconv.FormatFloat)`,
	}, {
		// Find a better alternative, as this call should go.
		test("strconv", `strconv.FormatFloat(3.02, -1, 4, 64)`),
		`_|_(cannot use -1 (type int) as byte in argument 1 to strconv.FormatFloat)`,
	}, {
		// Find a better alternative, as this call should go.
		test("strconv", `strconv.FormatFloat(3.02, 1.0, 4, 64)`),
		`_|_(cannot use 1.0 (type float) as int in argument 2 to strconv.FormatFloat)`,
	}, {
		// Panics
		test("math", `math.Jacobi(1000, 2000)`),
		`_|_(error in call to math.Jacobi: big: invalid 2nd argument to Int.Jacobi: need odd integer but got 2000)`,
	}, {
		test("math", `math.Jacobi(1000, 201)`),
		`1`,
	}, {
		test("math", `math.Asin(2.0e400)`),
		`_|_(cannot use 2.0e+400 (type float) as float64 in argument 0 to math.Asin: value was rounded up)`,
	}, {
		test("math", `math.MultipleOf(4, 2)`), `true`,
	}, {
		test("math", `math.MultipleOf(5, 2)`), `false`,
	}, {
		test("math", `math.MultipleOf(5, 0)`),
		`_|_(error in call to math.MultipleOf: division by zero)`,
	}, {
		test("math", `math.MultipleOf(100, 1.00001)`), `false`,
	}, {
		test("math", `math.MultipleOf(1, 1)`), `true`,
	}, {
		test("math", `math.MultipleOf(5, 2.5)`), `true`,
	}, {
		test("math", `math.MultipleOf(100e100, 10)`), `true`,
	}, {
		test("encoding/csv", `csv.Decode("1,2,3\n4,5,6")`),
		`[["1","2","3"],["4","5","6"]]`,
	}, {
		test("strconv", `strconv.FormatBool(true)`),
		`"true"`,
	}, {
		test("strings", `strings.Join(["Hello", "World!"], " ")`),
		`"Hello World!"`,
	}, {
		test("strings", `strings.Join([1, 2], " ")`),
		`_|_(invalid list element 0 in argument 0 to strings.Join: cannot use value 1 (type int) as string)`,
	}, {
		test("strings", `strings.ByteAt("a", 0)`),
		strconv.Itoa('a'),
	}, {
		test("strings", `strings.ByteSlice("Hello", 2, 5)`),
		`'llo'`,
	}, {
		test("strings", `strings.Runes("Café")`),
		strings.Replace(fmt.Sprint([]rune{'C', 'a', 'f', 'é'}), " ", ",", -1),
	}, {
		test("math/bits", `bits.Or(0x8, 0x1)`),
		`9`,
	}, {
		testExpr(`len({})`),
		`0`,
	}, {
		testExpr(`len({a: 1, b: 2, <foo>: int, _c: 3})`),
		`2`,
	}, {
		testExpr(`len([1, 2, 3])`),
		`3`,
	}, {
		testExpr(`len("foo")`),
		`3`,
	}, {
		testExpr(`len('f\x20\x20')`),
		`3`,
	}, {
		testExpr(`and([string, "foo"])`),
		`"foo"`,
	}, {
		testExpr(`and([string, =~"fo"]) & "foo"`),
		`"foo"`,
	}, {
		testExpr(`and([])`),
		`_`,
	}, {
		testExpr(`or([1, 2, 3]) & 2`),
		`2`,
	}, {
		testExpr(`or([])`),
		`_|_(empty list in call to or)`,
	}, {
		test("encoding/csv", `csv.Encode([[1,2,3],[4,5],[7,8,9]])`),
		`"1,2,3\n4,5\n7,8,9\n"`,
	}, {
		test("encoding/csv", `csv.Encode([["a", "b"], ["c"]])`),
		`"a,b\nc\n"`,
	}, {
		test("encoding/json", `json.Valid("1")`),
		`true`,
	}, {
		test("encoding/json", `json.Compact("[1, 2]")`),
		`"[1,2]"`,
	}, {
		test("encoding/json", `json.Indent(#"{"a": 1, "b": 2}"#, "", "  ")`),
		`"{\n  \"a\": 1,\n  \"b\": 2\n}"`,
	}, {
		test("encoding/json", `json.Unmarshal("1")`),
		`1`,
	}, {
		test("encoding/json", `json.MarshalStream([{a: 1}, {b: 2}])`),
		`"{\"a\":1}\n{\"b\":2}\n"`,
	}, {
		test("encoding/yaml", `yaml.MarshalStream([{a: 1}, {b: 2}])`),
		`"a: 1\n---\nb: 2\n"`,
	}, {
		test("net", `net.FQDN & "foo.bar."`),
		`"foo.bar."`,
	}, {
		test("net", `net.FQDN("foo.bararararararararararararararararararararararararararararararararara")`),
		`false`,
	}, {
		test("net", `net.SplitHostPort("[::%lo0]:80")`),
		`["::%lo0","80"]`,
	}, {
		test("net", `net.JoinHostPort("example.com", "80")`),
		`"example.com:80"`,
	}, {
		test("net", `net.JoinHostPort("2001:db8::1", 80)`),
		`"[2001:db8::1]:80"`,
	}, {
		test("net", `net.JoinHostPort([192,30,4,2], 80)`),
		`"192.30.4.2:80"`,
	}, {
		test("net", `net.JoinHostPort([192,30,4], 80)`),
		`_|_(error in call to net.JoinHostPort: invalid host [192,30,4])`,
	}, {
		test("net", `net.IP("23.23.23.23")`),
		`true`,
	}, {
		test("net", `net.IPv4 & "23.23.23.2333"`),
		`_|_(invalid value "23.23.23.2333" (does not satisfy net.IPv4()))`,
	}, {
		test("net", `net.IP("23.23.23.23")`),
		`true`,
	}, {
		test("net", `net.IP("2001:db8::1")`),
		`true`,
	}, {
		test("net", `net.IPv4("2001:db8::1")`),
		`false`,
	}, {
		test("net", `net.IPv4() & "ff02::1:3"`),
		`_|_(invalid value "ff02::1:3" (does not satisfy net.IPv4()))`,
	}, {
		test("net", `net.LoopbackIP([127, 0, 0, 1])`),
		`true`,
	}, {
		test("net", `net.LoopbackIP("127.0.0.1")`),
		`true`,
	}, {
		test("net", `net.ToIP4("127.0.0.1")`),
		`[127,0,0,1]`,
	}, {
		test("net", `net.ToIP16("127.0.0.1")`),
		`[0,0,0,0,0,0,0,0,0,0,255,255,127,0,0,1]`,
	}, {
		test("strings", `strings.ToCamel("AlphaBeta")`),
		`"alphaBeta"`,
	}, {
		test("strings", `strings.ToTitle("alpha")`),
		`"Alpha"`,
	}, {
		test("strings", `strings.MaxRunes(3) & "foo"`),
		`"foo"`,
	}, {
		test("strings", `strings.MaxRunes(3) & "quux"`),
		`_|_(invalid value "quux" (does not satisfy strings.MaxRunes(3)))`,
	}, {
		test("strings", `strings.MinRunes(1) & "e"`),
		`"e"`,
	}, {
		test("strings", `strings.MaxRunes(0) & "e"`),
		`_|_(invalid value "e" (does not satisfy strings.MaxRunes(0)))`,
	}, {
		test("strings", `strings.MaxRunes(0) & ""`),
		`""`,
	}, {
		test("strings", `strings.MinRunes(3) & "hello"`),
		`"hello"`,
	}, {
		test("strings", `strings.MaxRunes(10) & "hello"`),
		`"hello"`,
	}, {
		test("strings", `strings.MaxRunes(3) & "hello"`),
		`_|_(invalid value "hello" (does not satisfy strings.MaxRunes(3)))`,
	}, {
		test("strings", `strings.MinRunes(10) & "hello"`),
		`_|_(invalid value "hello" (does not satisfy strings.MinRunes(10)))`,
	}, {
		test("struct", `struct.MinFields(0) & ""`),
		`_|_(conflicting values MinFields (0) and "" (mismatched types struct and string))`,
	}, {
		test("struct", `struct.MinFields(0) & {a: 1}`),
		`{a: 1}`,
	}, {
		test("struct", `struct.MinFields(2) & {a: 1}`),
		`_|_(invalid value {a: 1} (does not satisfy struct.MinFields(2)))`,
	}, {
		test("struct", `struct.MaxFields(0) & {a: 1}`),
		`_|_(invalid value {a: 1} (does not satisfy struct.MaxFields(0)))`,
	}, {
		test("struct", `struct.MaxFields(2) & {a: 1}`),
		`{a: 1}`,
	}, {
		test("math/bits", `bits.And(0x10000000000000F0E, 0xF0F7)`), `6`,
	}, {
		test("math/bits", `bits.Or(0x100000000000000F0, 0x0F)`),
		hexToDec("100000000000000FF"),
	}, {
		test("math/bits", `bits.Xor(0x10000000000000F0F, 0xFF0)`),
		hexToDec("100000000000000FF"),
	}, {
		test("math/bits", `bits.Xor(0xFF0, 0x10000000000000F0F)`),
		hexToDec("100000000000000FF"),
	}, {
		test("math/bits", `bits.Clear(0xF, 0x100000000000008)`), `7`,
	}, {
		test("math/bits", `bits.Clear(0x1000000000000000008, 0xF)`),
		hexToDec("1000000000000000000"),
	}, {
		test("text/tabwriter", `tabwriter.Write("""
			a\tb\tc
			aaa\tbb\tvv
			""")`),
		`"a   b  c\naaa bb vv"`,
	}, {
		test("text/tabwriter", `tabwriter.Write([
				"a\tb\tc",
				"aaa\tbb\tvv"])`),
		`"a   b  c\naaa bb vv\n"`,
	}, {
		test("text/template", `template.Execute("{{.}}-{{.}}", "foo")`),
		`"foo-foo"`,
	}, {
		test("time", `time.Time & "1937-01-01T12:00:27.87+00:20"`),
		`"1937-01-01T12:00:27.87+00:20"`,
	}, {
		test("time", `time.Time & "no time"`),
		`_|_(error in call to time.Time: invalid time "no time")`,
	}, {
		test("time", `time.Unix(1500000000, 123456)`),
		`"2017-07-14T02:40:00.000123456Z"`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			insts := Build(makeInstances(tc.instances))
			if err := insts[0].Err; err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(fmt.Sprintf("%s\n", insts[0].Value()))
			if got != tc.emit {
				t.Errorf("\n got: %s\nwant: %s", got, tc.emit)
			}
		})
	}
}
