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

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
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
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()
			codec := New(ctx, nil)

			v, err := codec.ExtractType(tc.value)
			if err != nil {
				t.Fatal(err)
			}

			if tc.constraints != "" {
				v1 := ctx.CompileString(tc.constraints, cue.Filename(tc.name))
				if err := v1.Err(); err != nil {
					t.Fatal(err)
				}
				v = v.Unify(v1)
			}

			err = codec.Validate(v, tc.value)
			checkErr(t, err, tc.err)

			// Smoke test that it seems to work OK with deprecated *cue.Runtime argument
			r := &cue.Runtime{}
			codec = New(r, nil)
			if _, err := codec.ExtractType(tc.value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestComplete(t *testing.T) {
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
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()
			codec := New(ctx, nil)

			v, err := codec.ExtractType(tc.value)
			if err != nil {
				t.Fatal(err)
			}

			if tc.constraints != "" {
				c := ctx.CompileString(tc.constraints, cue.Filename(tc.name))
				if err := c.Err(); err != nil {
					t.Fatal(err)
				}
				v = v.Unify(c)
			}

			err = codec.Complete(v, tc.value)
			checkErr(t, err, tc.err)
			if diff := cmp.Diff(tc.value, tc.result); diff != "" {
				t.Error(diff)
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
	ctx := cuecontext.New()
	c := New(ctx, nil)

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			in := ctx.CompileString(tc.in, cue.Filename("test"))
			if err := in.Err(); err != nil {
				t.Fatal(err)
			}

			err := c.Encode(in, tc.dst)
			if err != nil {
				t.Fatal(err)
			}

			got := reflect.ValueOf(tc.dst).Elem().Interface()
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Error(diff)
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
	}, {
		in: func() interface{} {
			type T struct {
				B int
			}
			type S struct {
				A string
				T
			}
			return S{}
		}(),
		want: `{
	A: ""
	B: 0
}`,
	}, {
		in: func() interface{} {
			type T struct {
				B int
			}
			type S struct {
				A string
				T `json:"t"`
			}
			return S{}
		}(),
		want: `{
	A: ""
	t: {
		B: 0
	}
}`,
	}}
	c := New(cuecontext.New(), nil)

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

// For debugging purposes, do not remove.
func TestX(t *testing.T) {
	t.Skip()

	fail := "some error"
	// Not a typical constraint, but it is possible.
	var (
		name        = "string list incompatible lengths"
		value       = []string{"a", "b", "c"}
		constraints = `4*[string]`
		wantErr     = fail
	)

	ctx := cuecontext.New()
	codec := New(ctx, nil)

	v, err := codec.ExtractType(value)
	if err != nil {
		t.Fatal(err)
	}

	if constraints != "" {
		c := ctx.CompileString(constraints, cue.Filename(name))
		if err := c.Err(); err != nil {
			t.Fatal(err)
		}
		v = v.Unify(c)
	}

	err = codec.Validate(v, value)
	checkErr(t, err, wantErr)
}
