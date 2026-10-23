// Copyright 2021 CUE Authors
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
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/cuetdtest"
	"github.com/cockroachdb/apd/v3"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
)

func TestDecode(t *testing.T) {
	type Nested struct {
		P *int `json:"P"`
	}
	type fields struct {
		A int `json:"A"`
		B int `json:"B"`
		C int `json:"C"`
		M map[string]any
		*Nested
	}
	type Untagged struct{ X int }
	type Tagged struct {
		X int `json:"X"`
	}
	// A tagged field dominates an untagged one at the same depth.
	type embedTagged struct {
		Untagged
		Tagged
	}
	intList := func(ints ...int) *[]int {
		ints = append([]int{}, ints...)
		return &ints
	}
	testCases := []struct {
		value  string
		newDst func() any // a new destination for each run
		want   any
		err    string
	}{{
		value:  `1`,
		newDst: func() any { return nil },
		err:    "cannot decode into unsettable value",
	}, {
		newDst: func() any { return new(any) },
		value:  `_|_`,
		err:    "explicit error (_|_ literal) in source",
	}, {
		// clear pointer
		value:  `null`,
		newDst: func() any { return &[]int{1} },
		want:   []int(nil),
	}, {
		// clear pointer
		value:  `[null]`,
		newDst: func() any { return &[]*int{new(1)} },
		want:   []*int{nil},
	}, {
		value:  `true`,
		newDst: func() any { return new(bool) },
		want:   true,
	}, {
		value:  `false`,
		newDst: func() any { return new(bool) },
		want:   false,
	}, {
		value:  `bool`,
		newDst: func() any { return new(bool) },
		err:    "cannot convert non-concrete value bool",
	}, {
		value:  `_`,
		newDst: func() any { return new([]int) },
		want:   []int(nil),
	}, {
		value:  `"str"`,
		newDst: func() any { return new(string) },
		want:   "str",
	}, {
		value:  `"str"`,
		newDst: func() any { return new(int) },
		err:    "cannot use value \"str\" (type string) as int",
	}, {
		value:  `'bytes'`,
		newDst: func() any { return new([]byte) },
		want:   []byte("bytes"),
	}, {
		value:  `'bytes'`,
		newDst: func() any { return &[3]byte{} },
		want:   [3]byte{0x62, 0x79, 0x74},
	}, {
		value:  `1`,
		newDst: func() any { return new(float32) },
		want:   float32(1),
	}, {
		value:  `500`,
		newDst: func() any { return new(uint8) },
		err:    "integer 500 overflows uint8",
	}, {
		value:  `5`,
		newDst: func() any { return new(uintptr) },
		want:   uintptr(5),
	}, {
		value:  `501`,
		newDst: func() any { return new(int8) },
		err:    "integer 501 overflows int8",
	}, {
		value:  `{}`,
		newDst: func() any { return &fields{} },
		want:   fields{},
	}, {
		value:  `{A:1,b:2,c:3}`,
		newDst: func() any { return &fields{} },
		want:   fields{A: 1, B: 2, C: 3},
	}, {
		// allocate map
		value:  `{a:1,m:{a: 3}}`,
		newDst: func() any { return &fields{} },
		want: fields{A: 1,
			M: map[string]any{"a": int64(3)}},
	}, {
		// indirect int
		value:  `{p: 1}`,
		newDst: func() any { return &fields{} },
		want:   fields{Nested: &Nested{P: new(1)}},
	}, {
		value:  `{X: 1}`,
		newDst: func() any { return &embedTagged{} },
		want:   embedTagged{Tagged: Tagged{X: 1}},
	}, {
		value: `{for k, v in y if v > 1 {"\(k)": v} }
		y: {a:1,b:2,c:3}`,
		newDst: func() any { return &fields{} },
		want:   fields{B: 2, C: 3},
	}, {
		value:  `{a:1,b:2,c:int}`,
		newDst: func() any { return new(fields) },
		err:    "c: cannot convert non-concrete value int",
	}, {
		value:  `[]`,
		newDst: func() any { return intList() },
		want:   *intList(),
	}, {
		value:  `[1,2,3]`,
		newDst: func() any { return intList() },
		want:   *intList(1, 2, 3),
	}, {
		// shorten list
		value:  `[1,2,3]`,
		newDst: func() any { return intList(1, 2, 3, 4) },
		want:   *intList(1, 2, 3),
	}, {
		// shorter array
		value:  `[1,2,3]`,
		newDst: func() any { return &[2]int{} },
		want:   [2]int{1, 2},
	}, {
		// longer array
		value:  `[1,2,3]`,
		newDst: func() any { return &[4]int{} },
		want:   [4]int{1, 2, 3, 0},
	}, {
		value: `[for x in #y if x > 1 { x }]
				#y: [1,2,3]`,
		newDst: func() any { return intList() },
		want:   *intList(2, 3),
	}, {
		value:  `[int]`,
		newDst: func() any { return intList() },
		err:    "0: cannot convert non-concrete value int",
	}, {
		value:  `{a: 1, b: 2, c: 3}`,
		newDst: func() any { return &map[string]int{} },
		want:   map[string]int{"a": 1, "b": 2, "c": 3},
	}, {
		value:  `{"1": 1, "-2": 2, "3": 3}`,
		newDst: func() any { return &map[int]int{} },
		want:   map[int]int{1: 1, -2: 2, 3: 3},
	}, {
		value:  `{"1": 1, "2": 2, "3": 3}`,
		newDst: func() any { return &map[uint]int{} },
		want:   map[uint]int{1: 1, 2: 2, 3: 3},
	}, {
		value:  `{a: 1, b: 2, c: true, d: e: 2}`,
		newDst: func() any { return &map[string]any{} },
		want: map[string]any{
			"a": int64(1), "b": int64(2), "c": true,
			"d": map[string]any{"e": int64(2)}},
	}, {
		value:  `{a: b: *2 | int}`,
		newDst: func() any { return &map[string]any{} },
		want:   map[string]any{"a": map[string]any{"b": int64(2)}},
	}, {
		value:  `{a: 1, b: 2, c: true}`,
		newDst: func() any { return &map[string]int{} },
		err:    "c: cannot use value true (type bool) as int",
	}, {
		value:  `{"300": 3}`,
		newDst: func() any { return &map[int8]int{} },
		err:    "key integer 300 overflows int8",
	}, {
		value:  `{"300": 3}`,
		newDst: func() any { return &map[uint8]int{} },
		err:    "key integer 300 overflows uint8",
	}, {
		// Issue #1401
		value:  `a: b: _ | *[0, ...]`,
		newDst: func() any { return &map[string]any{} },
		want: map[string]any{
			"a": map[string]any{
				"b": []any{int64(0)},
			},
		},
	}, {
		// Issue #1466
		value:  `{myDuration: "1s"}`,
		newDst: func() any { return &Types{} },
		want:   Types{DurationStruct: MyDuration{D: 1000000000}},
	}, {
		// Issue #1466
		value:  `{myDuration: '1s'}`,
		newDst: func() any { return &Types{} },
		want:   Types{DurationStruct: MyDuration{D: 1000000000}},
	}, {
		// Issue #1466
		value:  `{myDuration: 1}`,
		newDst: func() any { return &Types{} },
		err:    "Decode: myDuration: cannot use value 1 (type int) as (string|bytes)",
	}, {
		value:  `{intPtr: null}`,
		newDst: func() any { return &Types{IntPtr: new(999)} }, // the original value is wiped
		want:   Types{},
	}, {
		value:  `{intPtr: 123}`,
		newDst: func() any { return &Types{} },
		want:   Types{IntPtr: new(123)},
	}, {
		value:  `{intPtr: int}`,
		newDst: func() any { return &Types{} },
		err:    "intPtr: cannot convert non-concrete value int",
	}, {
		value:  `[]`,
		newDst: func() any { return new(any) },
		want:   []any{},
	}, {
		// large integer which doesn't fit into an int32 or int on 32-bit platforms
		value:  `8000000000`,
		newDst: func() any { return new(any) },
		want:   int64(8000000000),
	}, {
		// same as the above, but negative
		value:  `-8000000000`,
		newDst: func() any { return new(any) },
		want:   int64(-8000000000),
	}, {
		// even larger integer which doesn't fit into an int64
		value:  `9500000000000000000`,
		newDst: func() any { return new(any) },
		want:   bigInt("9500000000000000000"),
	}, {
		// same as the above, but negative
		value:  `-9500000000000000000`,
		newDst: func() any { return new(any) },
		want:   bigInt("-9500000000000000000"),
	}, {
		value:  `9500000000000000000`,
		newDst: func() any { return new(*big.Int) },
		want:   bigInt("9500000000000000000"),
	}, {
		value:  `-9500000000000000000`,
		newDst: func() any { return new(*big.Int) },
		want:   bigInt("-9500000000000000000"),
	}, {
		// TODO: should this work?
		value:  `9500000000000000000`,
		newDst: func() any { return new(*apd.Decimal) },
		err:    "Decode: cannot use value 9500000000000000000 (type int) as (string|bytes)",
	}, {
		// TODO: should this work?
		value:  `-9500000000000000000`,
		newDst: func() any { return new(*apd.Decimal) },
		err:    "Decode: cannot use value -9500000000000000000 (type int) as (string|bytes)",
	}, {
		// large float which doesn't fit into a float32
		value:  `1.797693134e+308`,
		newDst: func() any { return new(any) },
		want:   float64(1.797693134e+308),
	}, {
		value:  `1.99769313499e+508`,
		newDst: func() any { return new(any) },
		want:   bigFloat(`1.99769313499e+508`),
	}, {
		value:  `-1.99769313499e+508`,
		newDst: func() any { return new(any) },
		want:   bigFloat(`-1.99769313499e+508`),
	}, {
		value:  `1.99769313499e+508`,
		newDst: func() any { return new(*big.Float) },
		want:   bigFloat(`1.99769313499e+508`),
	}, {
		value:  `-1.99769313499e+508`,
		newDst: func() any { return new(*big.Float) },
		want:   bigFloat(`-1.99769313499e+508`),
	}, {
		value:  `{RawMessage: 123.0}`,
		newDst: func() any { return &Unmarshalers{} },
		want:   Unmarshalers{RawMessage: json.RawMessage("123.0")},
	}, {
		value:  `{RawMessage: null}`,
		newDst: func() any { return &Unmarshalers{} },
		want:   Unmarshalers{RawMessage: json.RawMessage("null")},
	}, {
		value:  `{NetIP: "127.0.0.1"}`,
		newDst: func() any { return &Unmarshalers{} },
		want:   Unmarshalers{NetIP: net.IP{127, 0, 0, 1}},
	}, {
		value:  `{NetIP: null}`,
		newDst: func() any { return &Unmarshalers{} },
		want:   Unmarshalers{NetIP: nil},
	}, {
		value:  `{Value: 1.99769313499e+508}`,
		newDst: func() any { return new(StructWithFloat) },
		want:   StructWithFloat{bigFloat("1.99769313499e+508")},
	}, {
		value:  "{name: \"John Doe\"}",
		newDst: func() any { return new(Custom) },
		want: Custom{
			UnmarshalCalled: true,
			Name:            "John Doe",
		},
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			dst := tc.newDst()
			err := getValue(m, tc.value).Decode(dst)
			if !checkErr(t, err, tc.err, "init") {
				return
			}

			got := reflect.ValueOf(dst).Elem().Interface()
			qt.Assert(t, qt.CmpEquals(got, tc.want,
				cmp.Comparer(func(a, b *big.Int) bool {
					return a.Cmp(b) == 0
				}),
				cmp.Comparer(func(a, b *big.Float) bool {
					return a.Cmp(b) == 0
				}),
			))
		})
	}
}

func bigInt(s string) *big.Int {
	n, _ := big.NewInt(0).SetString(s, 10)
	return n
}

func bigFloat(s string) *big.Float {
	f := &big.Float{}
	f, _, _ = f.Parse(s, 0)
	return f
}

func TestDecodeIntoCUEValue(t *testing.T) {
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		// We should be able to decode into a CUE value so we can
		// decode partially incomplete values into Go.
		// This test doesn't fit within the table used by TestDecode
		// because cue values aren't easily comparable with cmp.Diff.
		var st struct {
			X cue.Value `json:"x"`
		}
		err := getValue(m, `x: string`).Decode(&st)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fmt.Sprint(st.X), "string"))

		// Check we can decode into a top level value.
		var v cue.Value
		err = getValue(m, `int`).Decode(&v)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fmt.Sprint(v), "int"))
	})
}

func TestDecodeIncompleteValueIntoCUEValue(t *testing.T) {
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		// We should be able to decode into a CUE value so we can
		// decode partially incomplete values into Go.
		// This test doesn't fit within the table used by TestDecode
		// because cue values aren't easily comparable with cmp.Diff.
		var st struct {
			Field cue.Value `json:"field"`
		}
		err := getValue(m, `
field: {
	#x: _
	a: #x.b
}
field: string | {
	#x!: {...}
	...
}
`).Decode(&st)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fmt.Sprintf("%#v", st.Field), `(string | {
	#x!: {...}
	...
}) & {
	#x: _
	a:  #x.b
}`))
	})
}

type StructWithFloat struct {
	Value *big.Float
}

type Custom struct {
	UnmarshalCalled bool
	Name            string
}

func (c *Custom) UnmarshalCUE(v cue.Value) error {
	name, err := v.LookupPath(cue.ParsePath("name")).String()
	if err == nil {
		c.UnmarshalCalled = true
		c.Name = name
	}

	return err
}

type MyDuration struct {
	D time.Duration
}
type Types struct {
	DurationStruct MyDuration `json:"myDuration"`
	IntPtr         *int       `json:"intPtr"`
}

type Unmarshalers struct {
	RawMessage json.RawMessage // [json.Unmarshaler]
	NetIP      net.IP          // [encoding.TextUnmarshaler]
}

func (d *MyDuration) UnmarshalText(data []byte) error {
	v, err := time.ParseDuration(string(data))
	if err != nil {
		return err
	}
	d.D = v
	return nil
}

func (d *MyDuration) MarshalText() ([]byte, error) {
	return []byte(d.D.String()), nil
}
