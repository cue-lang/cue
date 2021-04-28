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

package cue

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDecode(t *testing.T) {
	type fields struct {
		A int `json:"A"`
		B int `json:"B"`
		C int `json:"C"`
	}
	intList := func(ints ...int) *[]int {
		ints = append([]int{}, ints...)
		return &ints
	}
	testCases := []struct {
		value string
		dst   interface{}
		want  interface{}
		err   string
	}{{
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `"str"`,
		dst:   new(string),
		want:  "str",
	}, {
		value: `"str"`,
		dst:   new(int),
		err:   "cannot unmarshal string into Go value of type int",
	}, {
		value: `{}`,
		dst:   &fields{},
		want:  fields{},
	}, {
		value: `{a:1,b:2,c:3}`,
		dst:   &fields{},
		want:  fields{A: 1, B: 2, C: 3},
	}, {
		value: `{for k, v in y if v > 1 {"\(k)": v} }
		y: {a:1,b:2,c:3}`,
		dst:  &fields{},
		want: fields{B: 2, C: 3},
	}, {
		value: `{a:1,b:2,c:int}`,
		dst:   new(fields),
		err:   "cannot convert incomplete value",
	}, {
		value: `[]`,
		dst:   intList(),
		want:  *intList(),
	}, {
		value: `[1,2,3]`,
		dst:   intList(),
		want:  *intList(1, 2, 3),
	}, {
		value: `[for x in #y if x > 1 { x }]
				#y: [1,2,3]`,
		dst:  intList(),
		want: *intList(2, 3),
	}, {
		value: `[int]`,
		err:   "cannot convert incomplete value",
	}}
	for _, tc := range testCases {
		t.Run(tc.value, func(t *testing.T) {
			err := getInstance(t, tc.value).Value().Decode(tc.dst)
			checkFatal(t, err, tc.err, "init")

			got := reflect.ValueOf(tc.dst).Elem().Interface()
			if !cmp.Equal(got, tc.want) {
				t.Error(cmp.Diff(got, tc.want))
				t.Errorf("\n%#v\n%#v", got, tc.want)
			}
		})
	}
}
