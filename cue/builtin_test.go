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

package cue_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"

	_ "cuelang.org/go/pkg"
)

func TestBuiltins(t *testing.T) {
	test := func(pkg, expr string) []*bimport {
		return []*bimport{{"",
			[]string{fmt.Sprintf("import %q\n(%s)", pkg, expr)},
		}}
	}
	testExpr := func(expr string) []*bimport {
		return []*bimport{{"",
			[]string{fmt.Sprintf("(%s)", expr)},
		}}
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
		`_|_ // cannot call non-function math.Pi (type float)`,
	}, {
		test("math", "math.Floor(3, 5)"),
		`_|_ // too many arguments in call to math.Floor (have 2, want 1)`,
	}, {
		test("math", `math.Floor("foo")`),
		`_|_ // cannot use "foo" (type string) as number in argument 1 to math.Floor`,
	}, {
		test("crypto/sha256", `sha256.Sum256("hash me")`),
		`'\xeb \x1a\xf5\xaa\xf0\xd6\x06)\xd3Ҧ\x1eFl\xfc\x0f\xed\xb5\x17\xad\xd81\xec\xacR5\xe1کc\xd6'`,
	}, {
		test("crypto/md5", `len(md5.Sum("hash me"))`),
		`16`,
	}, {
		test("encoding/yaml", `yaml.Validate("a: 2\n---\na: 4", {a:<3})`),
		`_|_ // error in call to encoding/yaml.Validate: a: invalid value 4 (out of bound <3)`,
	}, {
		test("encoding/yaml", `yaml.Validate("a: 2\n---\na: 4", {a:<5})`),
		`true`,
	}, {
		test("encoding/yaml", `yaml.Validate("a: 2\n", {a:<5, b:int})`),
		`_|_ // error in call to encoding/yaml.Validate: b: incomplete value int`,
	}, {
		test("strconv", `strconv.FormatUint(64, 16)`),
		`"40"`,
	}, {
		test("regexp", `regexp.Find(#"f\w\w"#, "afoot")`),
		`"foo"`,
	}, {
		test("regexp", `regexp.Find(#"f\w\w"#, "bar")`),
		`_|_ // error in call to regexp.Find: no match`,
	}, {
		testExpr(`len([1, 2, 3])`),
		`3`,
	}, {
		testExpr(`len("foo")`),
		`3`,
	}, {
		test("encoding/json", `json.MarshalStream([{a: 1}, {b: 2}])`),
		`"""` + "\n\t{\"a\":1}\n\t{\"b\":2}\n\n\t" + `"""`,
	}, {
		test("encoding/json", `{
			x: int
			y: json.Marshal({a: x})
		}`),
		`{
	x: int
	y: _|_ // cannot convert incomplete value "int" to JSON
}`,
	}, {
		test("encoding/yaml", `yaml.MarshalStream([{a: 1}, {b: 2}])`),
		`"""` + "\n\ta: 1\n\t---\n\tb: 2\n\n\t" + `"""`,
	}, {
		test("struct", `struct.MinFields(0) & ""`),
		`_|_ // conflicting values struct.MinFields(0) and "" (mismatched types struct and string)`,
	}, {
		test("struct", `struct.MinFields(0) & {a: 1}`),
		`{
	a: 1
}`,
	}, {
		test("struct", `struct.MinFields(2) & {a: 1}`),
		// TODO: original value may be better.
		// `_|_ // invalid value {a:1} (does not satisfy struct.MinFields(2))`,
		`_|_ // invalid value {a:1} (does not satisfy struct.MinFields(2)): len(fields) < MinFields(2) (1 < 2)`,
	}, {
		test("time", `time.Time & "1937-01-01T12:00:27.87+00:20"`),
		`"1937-01-01T12:00:27.87+00:20"`,
	}, {
		test("time", `time.Time & "no time"`),
		`_|_ // invalid value "no time" (does not satisfy time.Time): error in call to time.Time: invalid time "no time"`,
	}, {
		test("time", `time.Unix(1500000000, 123456)`),
		`"2017-07-14T02:40:00.000123456Z"`,
	}}
	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			insts := cue.Build(makeInstances(tc.instances))
			if err := insts[0].Err; err != nil {
				t.Fatal(err)
			}
			v := insts[0].Value()
			got := fmt.Sprintf("%+v", v)
			if got != tc.emit {
				t.Errorf("\n got: %q\nwant: %q", got, tc.emit)
			}
		})
	}
}

// For debugging purposes. Do not remove.
func TestSingleBuiltin(t *testing.T) {
	t.Skip("error message")

	test := func(pkg, expr string) []*bimport {
		return []*bimport{{"",
			[]string{fmt.Sprintf("import %q\n(%s)", pkg, expr)},
		}}
	}
	testCases := []struct {
		instances []*bimport
		emit      string
	}{{
		test("list", `list.Sort([{a:1}, {b:2}], list.Ascending)`),
		`_|_ // error in call to list.Sort: less: invalid operands {b:2} and {a:1} to '<' (type struct and struct)`,
	}}
	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			insts := cue.Build(makeInstances(tc.instances))
			if err := insts[0].Err; err != nil {
				t.Fatal(err)
			}
			v := insts[0].Value()
			got := fmt.Sprint(v)
			if got != tc.emit {
				t.Errorf("\n got: %s\nwant: %s", got, tc.emit)
			}
		})
	}
}
