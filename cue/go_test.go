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

package cue

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"github.com/cockroachdb/apd/v2"
)

func TestConvert(t *testing.T) {
	i34 := big.NewInt(34)
	d34 := mkBigInt(34)
	n34 := mkBigInt(-34)
	f34 := big.NewFloat(34.0000)
	testCases := []struct {
		goVal interface{}
		want  string
	}{{
		nil, "(null | _)",
	}, {
		true, "true",
	}, {
		false, "false",
	}, {
		errors.New("oh noes"), "_|_(oh noes)",
	}, {
		"foo", `"foo"`,
	}, {
		3, "3",
	}, {
		uint(3), "3",
	}, {
		uint8(3), "3",
	}, {
		uint16(3), "3",
	}, {
		uint32(3), "3",
	}, {
		uint64(3), "3",
	}, {
		int8(-3), "-3",
	}, {
		int16(-3), "-3",
	}, {
		int32(-3), "-3",
	}, {
		int64(-3), "-3",
	}, {
		float64(3.1), "3.1",
	}, {
		float32(3.1), "3.1",
	}, {
		uintptr(3), "3",
	}, {
		&i34, "34",
	}, {
		&f34, "34",
	}, {
		&d34, "34",
	}, {
		&n34, "-34",
	}, {
		[]int{1, 2, 3, 4}, "[1,2,3,4]",
	}, {
		[]interface{}{}, "[]",
	}, {
		map[string][]int{
			"a": []int{1},
			"b": []int{3, 4},
		}, "<0>{a: [1], b: [3,4]}",
	}, {
		map[bool]int{}, "_|_(unsupported Go type for map key (bool))",
	}, {
		map[struct{}]int{struct{}{}: 2}, "_|_(unsupported Go type for map key (struct {}))",
	}, {
		map[int]int{1: 2}, "<0>{1: 2}",
	}, {
		struct {
			a int
			b int
		}{3, 4},
		"<0>{}",
	}, {
		struct {
			A int
			B int
		}{3, 4},
		"<0>{A: 3, B: 4}",
	}, {
		struct {
			A int `json:"a"`
			B int `yaml:"b"`
		}{3, 4},
		"<0>{a: 3, b: 4}",
	}, {
		struct {
			A int `json:"" yaml:"" protobuf:"aa"`
			B int `yaml:"cc" json:"bb" protobuf:"aa"`
		}{3, 4},
		"<0>{aa: 3, bb: 4}",
	}, {
		&struct{ A int }{3}, "<0>{A: 3}",
	}, {
		(*struct{ A int })(nil), "(null | <0>{A: (int & >=-9223372036854775808 & int & <=9223372036854775807)})",
	}, {
		reflect.ValueOf(3), "3",
	}, {
		time.Date(2019, 4, 1, 0, 0, 0, 0, time.UTC), `"2019-04-01T00:00:00Z"`,
	}}
	inst := getInstance(t, "foo")
	b := ast.NewIdent("dummy")
	for _, tc := range testCases {
		ctx := inst.newContext()
		t.Run("", func(t *testing.T) {
			v := convert(ctx, newNode(b), true, tc.goVal)
			got := debugStr(ctx, v)
			if got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestConvertType(t *testing.T) {
	testCases := []struct {
		goTyp interface{}
		want  string
	}{{
		struct {
			A int      `cue:">=0&<100"`
			B *big.Int `cue:">=0"`
			C *big.Int
			D big.Int
			F *big.Float
		}{},
		// TODO: indicate that B is explicitly an int only.
		`<0>{A: ((int & >=-9223372036854775808 & int & <=9223372036854775807) & (>=0 & <100)), ` +
			`B: (int & >=0), ` +
			`C?: int, ` +
			`D: int, ` +
			`F?: number}`,
	}, {
		&struct {
			A int16 `cue:">=0&<100"`
			B error `json:"b,"`
			C string
			D bool
			F float64
			L []byte
			T time.Time
			G func()
		}{},
		`(*null | <0>{A: ((int & >=-32768 & int & <=32767) & (>=0 & <100)), ` +
			`C: string, ` +
			`D: bool, ` +
			`F: float, ` +
			`b: null, ` +
			`L?: (*null | bytes), ` +
			`T: _})`,
	}, {
		struct {
			A int `cue:"<"` // invalid
		}{},
		"_|_(invalid tag \"<\" for field \"A\": expected operand, found 'EOF' )",
	}, {
		struct {
			A int `json:"-"` // skip
			D *apd.Decimal
			P ***apd.Decimal
			I interface{ Foo() }
			T string `cue:""` // allowed
			h int
		}{},
		"<0>{D?: number, T: string, P?: (*null | number), I?: _}",
	}, {
		struct {
			A int8 `cue:"C-B"`
			B int8 `cue:"C-A,opt"`
			C int8 `cue:"A+B"`
		}{},
		// TODO: should B be marked as optional?
		`<0>{A: ((int & >=-128 & int & <=127) & (<0>.C - <0>.B)), ` +
			`B?: ((int & >=-128 & int & <=127) & (<0>.C - <0>.A)), ` +
			`C: ((int & >=-128 & int & <=127) & (<0>.A + <0>.B))}`,
	}, {
		[]string{},
		`(*null | [, ...string])`,
	}, {
		[4]string{},
		`4*[string]`,
	}, {
		[]func(){},
		"_|_(unsupported Go type (func()))",
	}, {
		map[string]struct{ A map[string]uint }{},
		`(*null | ` +
			`<0>{<>: <1>(_: string)-><2>{` +
			`A?: (*null | ` +
			`<3>{<>: <4>(_: string)->(int & >=0 & int & <=18446744073709551615), })}, })`,
	}, {
		map[float32]int{},
		`_|_(unsupported Go type for map key (float32))`,
	}, {
		map[int]map[float32]int{},
		`_|_(unsupported Go type for map key (float32))`,
	}, {
		map[int]func(){},
		`_|_(unsupported Go type (func()))`,
	}, {
		time.Now, // a function
		"_|_(unsupported Go type (func() time.Time))",
	}}
	inst := getInstance(t, "foo")

	for _, tc := range testCases {
		ctx := inst.newContext()
		t.Run("", func(t *testing.T) {
			v := goTypeToValue(ctx, true, reflect.TypeOf(tc.goTyp))
			got := debugStr(ctx, v)
			if got != tc.want {
				t.Errorf("\n got %q;\nwant %q", got, tc.want)
			}
		})
	}
}
