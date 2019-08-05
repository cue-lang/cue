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

package gocodec

import (
	"fmt"
	"reflect"
	"testing"

	"cuelang.org/go/cue"
	"github.com/google/go-cmp/cmp"
)

type Sum struct {
	A int `cue:"C-B" json:",omitempty"`
	B int `cue:"C-A" json:",omitempty"`
	C int `cue:"A+B & >=5" json:",omitempty"`
}

func checkErr(t *testing.T, got error, want string) {
	t.Helper()
	if (got == nil) != (want == "") {
		t.Errorf("error: got %v; want %v", got, want)
	}
}
func TestValidate(t *testing.T) {
	fail := "some error"
	testCases := []struct {
		name        string
		value       interface{}
		constraints string
		err         string
	}{{
		name:        "*Sum: nil disallowed by constraint",
		value:       (*Sum)(nil),
		constraints: "!=null",
		err:         fail,
	}, {
		name:  "Sum",
		value: Sum{A: 1, B: 4, C: 5},
	}, {
		name:  "*Sum",
		value: &Sum{A: 1, B: 4, C: 5},
	}, {
		name:  "*Sum: incorrect sum",
		value: &Sum{A: 1, B: 4, C: 6},
		err:   fail,
	}, {
		name:  "*Sum: field C is too low",
		value: &Sum{A: 1, B: 3, C: 4},
		err:   fail,
	}, {
		name:  "*Sum: nil value",
		value: (*Sum)(nil),
	}, {
		// Not a typical constraint, but it is possible.
		name:        "string list",
		value:       []string{"a", "b", "c"},
		constraints: `[_, "b", ...]`,
	}, {
		// Not a typical constraint, but it is possible.
		name:        "string list incompatible lengths",
		value:       []string{"a", "b", "c"},
		constraints: `4*[string]`,
		err:         fail,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &cue.Runtime{}
			codec := New(r, nil)

			v, err := codec.ExtractType(tc.value)
			if err != nil {
				t.Fatal(err)
			}

			if tc.constraints != "" {
				inst, err := r.Compile(tc.name, tc.constraints)
				if err != nil {
					t.Fatal(err)
				}
				v = v.Unify(inst.Value())
				fmt.Println("XXX", v)
				fmt.Println("XXX", inst.Value())
				fmt.Println("UUU", v)
			}

			err = codec.Validate(v, tc.value)
			checkErr(t, err, tc.err)
		})
	}
}

func TestComplete(t *testing.T) {
	type updated struct {
		A []*int `cue:"[...int|*1]"` // arbitrary length slice with values 1
		B []int  `cue:"3*[int|*1]"`  // slice of length 3, defaults to [1,1,1]

		// TODO: better errors if the user forgets to quote.
		M map[string]int `cue:",opt"`
	}
	type sump struct {
		A *int `cue:"C-B"`
		B *int `cue:"C-A"`
		C *int `cue:"A+B"`
	}
	one := 1
	two := 2
	fail := "some error"
	_ = fail
	_ = one
	testCases := []struct {
		name        string
		value       interface{}
		result      interface{}
		constraints string
		err         string
	}{{
		name:   "*Sum",
		value:  &Sum{A: 1, B: 4, C: 5},
		result: &Sum{A: 1, B: 4, C: 5},
	}, {
		name:   "*Sum",
		value:  &Sum{A: 1, B: 4},
		result: &Sum{A: 1, B: 4, C: 5},
	}, {
		name:   "*sump",
		value:  &sump{A: &one, B: &one},
		result: &sump{A: &one, B: &one, C: &two},
	}, {
		name:   "*Sum: backwards",
		value:  &Sum{B: 4, C: 8},
		result: &Sum{A: 4, B: 4, C: 8},
	}, {
		name:   "*Sum: sum too low",
		value:  &Sum{A: 1, B: 3},
		result: &Sum{A: 1, B: 3}, // Value should not be updated
		err:    fail,
	}, {
		name:   "*Sum: sum underspecified",
		value:  &Sum{A: 1},
		result: &Sum{A: 1}, // Value should not be updated
		err:    fail,
	}, {
		name:   "Sum: cannot modify",
		value:  Sum{A: 3, B: 4, C: 7},
		result: Sum{A: 3, B: 4, C: 7},
		err:    fail,
	}, {
		name:   "*Sum: cannot update nil value",
		value:  (*Sum)(nil),
		result: (*Sum)(nil),
		err:    fail,
	}, {
		name:   "cannot modify slice",
		value:  []string{"a", "b", "c"},
		result: []string{"a", "b", "c"},
		err:    fail,
	}, {
		name: "composite values update",
		// allocate a slice with uninitialized values and let Update fill
		// out default values.
		value: &updated{A: make([]*int, 3)},
		result: &updated{
			A: []*int{&one, &one, &one},
			B: []int{1, 1, 1},
			M: map[string]int(nil),
		},
	}, {
		name:   "composite values update with unsatisfied map constraints",
		value:  &updated{},
		result: &updated{},

		constraints: ` { M: {foo: bar, bar: foo} } `,
		err:         fail, // incomplete values
	}, {
		name:        "composite values update with map constraints",
		value:       &updated{M: map[string]int{"foo": 1}},
		constraints: ` { M: {foo: bar, bar: foo} } `,
		result: &updated{
			// TODO: would be better if this is nil, but will not matter for
			// JSON output: if omitempty is false, an empty list will be
			// printed regardless, and if it is true, it will be omitted
			// regardless.
			A: []*int{},
			B: []int{1, 1, 1},
			M: map[string]int{"bar": 1, "foo": 1},
		},
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &cue.Runtime{}
			codec := New(r, nil)

			v, err := codec.ExtractType(tc.value)
			if err != nil {
				t.Fatal(err)
			}

			if tc.constraints != "" {
				inst, err := r.Compile(tc.name, tc.constraints)
				if err != nil {
					t.Fatal(err)
				}
				v = v.Unify(inst.Value())
			}

			err = codec.Complete(v, tc.value)
			checkErr(t, err, tc.err)
			if !reflect.DeepEqual(tc.value, tc.result) {
				t.Errorf("value:\n got: %#v;\nwant: %#v", tc.value, tc.result)
			}
		})
	}
}

func TestEncode(t *testing.T) {
	testCases := []struct {
		in   string
		dst  interface{}
		want interface{}
	}{{
		in:   "4",
		dst:  new(int),
		want: 4,
	}}
	r := &cue.Runtime{}
	c := New(r, nil)

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			inst, err := r.Compile("test", tc.in)
			if err != nil {
				t.Fatal(err)
			}

			err = c.Encode(inst.Value(), tc.dst)
			if err != nil {
				t.Fatal(err)
			}

			got := reflect.ValueOf(tc.dst).Elem().Interface()
			if !cmp.Equal(got, tc.want) {
				t.Error(cmp.Diff(got, tc.want))
			}
		})
	}
}

func TestDecode(t *testing.T) {
	testCases := []struct {
		in   interface{}
		want string
	}{{
		in:   "str",
		want: `"str"`,
	}}
	c := New(&cue.Runtime{}, nil)

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			v, err := c.Decode(tc.in)
			if err != nil {
				t.Fatal(err)
			}

			got := fmt.Sprint(v)
			if got != tc.want {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}
