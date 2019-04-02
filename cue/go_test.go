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
	"go/ast"
	"math/big"
	"reflect"
	"testing"
	"time"

	"cuelang.org/go/cue/errors"
)

func TestConvert(t *testing.T) {
	i34 := big.NewInt(34)
	d34 := mkBigInt(34)
	f34 := big.NewFloat(34.0000)
	testCases := []struct {
		goVal interface{}
		want  string
	}{{
		nil, "null",
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
		&i34, "34",
	}, {
		&f34, "34",
	}, {
		&d34, "34",
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
		map[int]int{}, "_|_(builtin map key not a string, but unsupported type int)",
	}, {
		map[int]int{1: 2}, "_|_(builtin map key not a string, but unsupported type int)",
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
			A int `json:",bb" yaml:"" protobuf:"aa"`
			B int `yaml:"cc" json:"bb" protobuf:"aa"`
		}{3, 4},
		"<0>{aa: 3, bb: 4}",
	}, {
		&struct{ A int }{3}, "<0>{A: 3}",
	}, {
		(*struct{ A int })(nil), "null",
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
			v := convert(ctx, newNode(b), tc.goVal)
			got := debugStr(ctx, v)
			if got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}
