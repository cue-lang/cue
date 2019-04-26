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
		`_|_(<0>.Pi:cannot call non-function 3.14159265358979323846264338327950288419716939937510582097494459 (type float))`,
	}, {
		test("math", "math.Floor(3, 5)"),
		`_|_(<0>.Floor (3,5):number of arguments does not match (1 vs 2))`,
	}, {
		test("math", `math.Floor("foo")`),
		`_|_(<0>.Floor ("foo"):argument 1 requires type number, found string)`,
	}, {
		test("encoding/hex", `hex.Encode("foo")`),
		`"666f6f"`,
	}, {
		test("encoding/hex", `hex.Decode(hex.Encode("foo"))`),
		`'foo'`,
	}, {
		test("encoding/hex", `hex.Decode("foo")`),
		`_|_(<0>.Decode ("foo"):call error: encoding/hex: invalid byte: U+006F 'o')`,
	}, {
		test("strconv", `strconv.FormatUint(64, 16)`),
		`"40"`,
	}, {
		// Find a better alternative, as this call should go.
		test("strconv", `strconv.FormatFloat(3.02, 300, 4, 64)`),
		`_|_(<0>.FormatFloat (3.02,300,4,64):argument 1 out of range: has 9 > 8 bits)`,
	}, {
		// Find a better alternative, as this call should go.
		test("strconv", `strconv.FormatFloat(3.02, -1, 4, 64)`),
		`_|_(<0>.FormatFloat (3.02,-1,4,64):argument 1 must be a positive integer)`,
	}, {
		// Find a better alternative, as this call should go.
		test("strconv", `strconv.FormatFloat(3.02, 1.0, 4, 64)`),
		`_|_(<0>.FormatFloat (3.02,1.0,4,64):argument 2 requires type int, found float)`,
	}, {
		// Panics
		test("math", `math.Jacobi(1000, 2000)`),
		`_|_(<0>.Jacobi (1000,2000):call error: big: invalid 2nd argument to Int.Jacobi: need odd integer but got 2000)`,
	}, {
		test("math", `math.Jacobi(1000, 201)`),
		`1`,
	}, {
		test("math", `math.Asin(2.0e400)`),
		`_|_(<0>.Asin (2.0e+400):invalid argument 0: cue: value was rounded up)`,
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
		`_|_(<0>.Join ([1,2]," "):list element 1: not of right kind (number vs string))`,
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
		`_|_(builtin:or:empty or)`,
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
