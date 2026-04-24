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

package convert_test

// TODO: generate tests from Go's json encoder.

import (
	"encoding"
	"encoding/json"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/runtime"

	_ "cuelang.org/go/pkg"
)

type recursiveA struct {
	Next *recursiveA
	Val  int
}

type crossRefA struct {
	Y string
	B *crossRefB
}

type crossRefB struct {
	X int
	A *crossRefA
}

type sharedS struct {
	Other string
}

type sharedT struct {
	S  *sharedS
	S2 *sharedS
}

func mkBigInt(a int64) (v apd.Decimal) { v.SetInt64(a); return }

type textMarshaller struct {
	b string
}

func (t *textMarshaller) MarshalText() (b []byte, err error) {
	return []byte(t.b), nil
}

var _ encoding.TextMarshaler = &textMarshaller{}

type jsonMarshaller struct {
	b string
}

func (j *jsonMarshaller) MarshalJSON() ([]byte, error) {
	return []byte(`"` + j.b + `"`), nil
}

var _ json.Marshaler = &jsonMarshaller{}

type ptrError struct {
	msg string
}

func (e *ptrError) Error() string { return e.msg }

var _ error = &ptrError{}

func TestConvert(t *testing.T) {
	type key struct {
		a int
	}
	type stringType string
	i34 := big.NewInt(34)
	d35 := mkBigInt(35)
	n36 := mkBigInt(-36)
	f37 := big.NewFloat(37.0000)
	testCases := []struct {
		goVal interface{}
		want  string
	}{{
		nil, "(_){ _ }",
	}, {
		true, "(bool){ true }",
	}, {
		false, "(bool){ false }",
	}, {
		errors.New("oh noes"), "(_|_){\n  // [eval] oh noes\n}",
	}, {
		"foo", `(string){ "foo" }`,
	}, {
		"\x80", `(string){ "�" }`,
	}, {
		3, "(int){ 3 }",
	}, {
		uint(3), "(int){ 3 }",
	}, {
		uint8(3), "(int){ 3 }",
	}, {
		uint16(3), "(int){ 3 }",
	}, {
		uint32(3), "(int){ 3 }",
	}, {
		uint64(3), "(int){ 3 }",
	}, {
		int8(-3), "(int){ -3 }",
	}, {
		int16(-3), "(int){ -3 }",
	}, {
		int32(-3), "(int){ -3 }",
	}, {
		int64(-3), "(int){ -3 }",
	}, {
		float64(3), "(float){ 3 }",
	}, {
		float64(3.1), "(float){ 3.1 }",
	}, {
		float32(3.1), "(float){ 3.1 }",
	}, {
		uintptr(3), "(int){ 3 }",
	}, {
		&i34, "(int){ 34 }",
	}, {
		&f37, "(float){ 37 }",
	}, {
		&d35, "(int){ 35 }",
	}, {
		&n36, "(int){ -36 }",
	}, {
		[]int{1, 2, 3, 4}, `(#list){
  0: (int){ 1 }
  1: (int){ 2 }
  2: (int){ 3 }
  3: (int){ 4 }
}`,
	}, {
		struct {
			A int
			B *int
		}{3, nil},
		"(struct){\n  A: (int){ 3 }\n}",
	}, {
		[]interface{}{}, "(#list){\n}",
	}, {
		[]interface{}{nil}, "(#list){\n  0: (_){ _ }\n}",
	}, {
		map[string]interface{}{"a": 1, "x": nil}, `(struct){
  a: (int){ 1 }
  x: (_){ _ }
}`,
	}, {
		map[string][]int{
			"a": {1},
			"b": {3, 4},
		}, `(struct){
  a: (#list){
    0: (int){ 1 }
  }
  b: (#list){
    0: (int){ 3 }
    1: (int){ 4 }
  }
}`,
	}, {
		map[bool]int{}, "(_|_){\n  // [eval] unsupported Go type for map key (bool)\n}",
	}, {
		map[struct{}]int{{}: 2}, "(_|_){\n  // [eval] unsupported Go type for map key (struct {})\n}",
	}, {
		map[int]int{1: 2}, `(struct){
  "1": (int){ 2 }
}`,
	}, {
		struct {
			a int
			b int
		}{3, 4},
		"(struct){\n}",
	}, {
		struct {
			A int
			B int `json:"-"`
			C int `json:",omitempty"`
		}{3, 4, 0},
		`(struct){
  A: (int){ 3 }
}`,
	}, {
		struct {
			A int
			B int
		}{3, 4},
		`(struct){
  A: (int){ 3 }
  B: (int){ 4 }
}`,
	}, {
		struct {
			A int `json:"a"`
			B int `yaml:"b"`
		}{3, 4},
		`(struct){
  a: (int){ 3 }
  b: (int){ 4 }
}`,
	}, {
		struct {
			A int `json:"" yaml:"" protobuf:"aa"`
			B int `yaml:"cc" json:"bb" protobuf:"aa"`
		}{3, 4},
		`(struct){
  aa: (int){ 3 }
  bb: (int){ 4 }
}`,
	}, {
		&struct{ A int }{3}, `(struct){
  A: (int){ 3 }
}`,
	}, {
		(*struct{ A int })(nil), "(_){ _ }",
	}, {
		time.Date(2019, 4, 1, 0, 0, 0, 0, time.UTC), `(string){ "2019-04-01T00:00:00Z" }`,
	}, {
		func() interface{} {
			type T struct {
				B int
			}
			type S struct {
				A string
				T
			}
			return S{}
		}(),
		`(struct){
  A: (string){ "" }
  B: (int){ 0 }
}`,
	},
		{map[key]string{{a: 1}: "foo"},
			"(_|_){\n  // [eval] unsupported Go type for map key (convert_test.key)\n}"},
		{map[*textMarshaller]string{{b: "bar"}: "foo"},
			"(struct){\n  \"&{bar}\": (string){ \"foo\" }\n}"},
		{map[int]string{1: "foo"},
			"(struct){\n  \"1\": (string){ \"foo\" }\n}"},
		{map[string]encoding.TextMarshaler{"foo": nil},
			"(struct){\n  foo: (_){ _ }\n}"},
		{map[string]encoding.TextMarshaler{"foo": &textMarshaller{b: "bar"}},
			"(struct){\n  foo: (string){ \"bar\" }\n}"},
		{make(chan int),
			"(_|_){\n  // [eval] unsupported Go type (chan int)\n}"},
		{[]interface{}{func() {}},
			"(_|_){\n  // [eval] unsupported Go type (func())\n}"},
		{[]encoding.TextMarshaler{nil},
			"(#list){\n  0: (_){ _ }\n}"},
		{[]encoding.TextMarshaler{&textMarshaller{b: "bar"}},
			"(#list){\n  0: (string){ \"bar\" }\n}"},
		{stringType("\x80"), `(string){ "�" }`},
		{jsonMarshaller{b: "bar"}, `(string){ "bar" }`},
		{&jsonMarshaller{b: "bar"}, `(string){ "bar" }`},
		{textMarshaller{b: "bar"}, `(string){ "bar" }`},
		{ptrError{msg: "bad"}, "(_|_){\n  // [eval] bad\n}"},
	}
	r := runtime.New()
	for _, tc := range testCases {
		ctx := adt.NewContext(r, &adt.Vertex{})
		t.Run("", func(t *testing.T) {
			v := convert.FromGoValue(ctx, tc.goVal, true)
			n, ok := v.(*adt.Vertex)
			if !ok {
				n = &adt.Vertex{BaseValue: v}
			}
			got := debug.NodeString(ctx, n, nil)
			if got != tc.want {
				t.Error(cmp.Diff(got, tc.want))
			}
		})
	}
}

func TestX(t *testing.T) {
	t.Skip()

	x := []string{}

	r := runtime.New()
	ctx := adt.NewContext(r, &adt.Vertex{})

	v := convert.FromGoValue(ctx, x, false)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	got := debug.NodeString(ctx, v, nil)
	t.Error(got)
}

func TestConvertType(t *testing.T) {
	testCases := []struct {
		goTyp       interface{}
		want        string
		expectError bool
	}{{
		goTyp: struct {
			A int      `cue:">=0&<100"`
			B *big.Int `cue:">=0"`
			C *big.Int
			D big.Int
			F *big.Float
		}{},
		// TODO: indicate that B is explicitly an int only.
		want: `(struct){
  A: (int){ &(>=0, <100, int) }
  B: (int){ &(>=0, int) }
  C?: (int){ int }
  D: (int){ int }
  F?: (number){ number }
}`,
	}, {
		goTyp: &struct {
			A int16 `cue:">=0&<100"`
			B error `json:"b"`
			C string
			D bool
			F float64
			L []byte
			T time.Time
			G func()
		}{},
		want: `((null|struct)){ |(*(null){ null }, (struct){
    A: (int){ &(>=0, <100, int) }
    b: (null){ null }
    C: (string){ string }
    D: (bool){ bool }
    F: (number){ number }
    L?: ((null|bytes)){ |(*(null){ null }, (bytes){ bytes }) }
    T: (_){ _ }
  }) }`,
	}, {
		goTyp: struct {
			A int `cue:"<"` // invalid
		}{},
		want:        "(_|_){// _|_(invalid tag \"<\" for field \"A\": expected operand, found 'EOF')\n}",
		expectError: true,
	}, {
		goTyp: struct {
			A int `json:"-"` // skip
			D *apd.Decimal
			P ***apd.Decimal
			I interface{ Foo() }
			T string `cue:""` // allowed
			h int
		}{},
		want: `(struct){
  D?: (number){ number }
  P?: ((null|number)){ |(*(null){ null }, (number){ number }) }
  I?: (_){ _ }
  T: (string){ string }
}`,
	}, {
		goTyp: struct {
			A int8 `cue:"C-B"`
			B int8 `cue:"C-A,opt"`
			C int8 `cue:"A+B"`
		}{},
		// TODO: should B be marked as optional?
		want: "(struct){\n  A: (_|_){\n    // [incomplete] A: non-concrete value int8 in operand to -:\n    //     <field:>:1:1\n    // A: cannot reference optional field: B:\n    //     <field:>:1:3\n  }\n  B?: (_|_){\n    // [incomplete] B: non-concrete value int8 in operand to -:\n    //     <field:>:1:1\n  }\n  C: (_|_){\n    // [incomplete] C: non-concrete value int8 in operand to +:\n    //     <field:>:1:1\n    // C: cannot reference optional field: B:\n    //     <field:>:1:3\n  }\n}",
	}, {
		goTyp: []string{},
		want: `((null|list)){ |(*(null){ null }, (list){
  }) }`,
	}, {
		goTyp: [4]string{},
		want: `(#list){
  0: (string){ string }
  1: (string){ string }
  2: (string){ string }
  3: (string){ string }
}`,
	}, {
		goTyp:       []func(){},
		want:        "(_|_){// _|_(unsupported Go type (func()))\n}",
		expectError: true,
	}, {
		goTyp: map[string]struct{ A map[string]uint }{},
		want: `((null|struct)){ |(*(null){ null }, (struct){
  }) }`,
	}, {
		goTyp:       map[float32]int{},
		want:        "(_|_){// _|_(unsupported Go type for map key (float32))\n}",
		expectError: true,
	}, {
		goTyp:       map[int]map[float32]int{},
		want:        "(_|_){// _|_(unsupported Go type for map key (float32))\n}",
		expectError: true,
	}, {
		goTyp:       map[int]func(){},
		want:        "(_|_){// _|_(unsupported Go type (func()))\n}",
		expectError: true,
	}, {
		goTyp:       time.Now, // a function
		want:        "(_|_){// _|_(unsupported Go type (func() time.Time))\n}",
		expectError: true,
	}, {
		goTyp: struct {
			Foobar string `cue:"\"foo,bar\",opt"`
		}{},
		want: `(struct){
  Foobar?: (string){ "foo,bar" }
}`,
	}, {
		goTyp: struct {
			Foobar string `cue:"\"foo,opt,bar\""`
		}{},
		want: `(struct){
  Foobar: (string){ "foo,opt,bar" }
}`,
	}}

	r := runtime.New()

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx := adt.NewContext(r, &adt.Vertex{})
			v, err := convert.FromGoType(ctx, tc.goTyp)
			got := debug.NodeString(ctx, v, nil)
			if got != tc.want {
				t.Errorf("\n got %q;\nwant %q", got, tc.want)
			}
			if tc.expectError && err == nil {
				t.Errorf("\n expected an error but didn't get one")
			} else if !tc.expectError && err != nil {
				t.Errorf("\n got unexpected error: %v", err)
			}
			if err == nil && !tc.expectError {
				val, _ := ctx.Evaluate(&adt.Environment{}, v)
				if bot, ok := val.(*adt.Bottom); ok {
					t.Errorf("\n unexpected error when evaluating result of conversion: %v", bot)
				}
			}
		})
	}
}

func TestFromGoTypeRecursive(t *testing.T) {
	r := runtime.New()

	testCases := []struct {
		name  string
		goTyp any
		want  string
	}{{
		name:  "self-recursive",
		goTyp: recursiveA{},
		want: `(struct){
  _recursiveA_0: (struct){
    Next?: (null){ null }
    Val: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
  }
  Next?: ((null|struct)){ |(*(null){ null }, (struct){
      Next?: ((null|struct)){ |(*(null){ null }, (struct){
          Next?: (null){ null }
          Val: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
        }) }
      Val: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
    }) }
  Val: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
}`,
	}, {
		name:  "mutually-recursive",
		goTyp: crossRefA{},
		want: `(struct){
  _crossRefA_0: (struct){
    Y: (string){ string }
    B?: ((null|struct)){ |(*(null){ null }, (struct){
        X: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
        A?: (null){ null }
      }) }
  }
  _crossRefB_0: (struct){
    X: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
    A?: ((null|struct)){ |(*(null){ null }, (struct){
        Y: (string){ string }
        B?: (null){ null }
      }) }
  }
  Y: (string){ string }
  B?: ((null|struct)){ |(*(null){ null }, (struct){
      X: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
      A?: ((null|struct)){ |(*(null){ null }, (struct){
          Y: (string){ string }
          B?: ((null|struct)){ |(*(null){ null }, (struct){
              X: (int){ &(>=-9223372036854775808, <=9223372036854775807, int) }
              A?: ((null|struct)){ |(*(null){ null }, (struct){
                  Y: (string){ string }
                  B?: (null){ null }
                }) }
            }) }
        }) }
    }) }
}`,
	}, {
		name:  "shared-type",
		goTyp: sharedT{},
		want: `(struct){
  _sharedT_0: (struct){
    S?: ((null|struct)){ |(*(null){ null }, (struct){
        Other: (string){ string }
      }) }
    S2?: ((null|struct)){ |(*(null){ null }, (struct){
        Other: (string){ string }
      }) }
  }
  _sharedS_0: (struct){
    Other: (string){ string }
  }
  S?: ((null|struct)){ |(*(null){ null }, (struct){
      Other: (string){ string }
    }) }
  S2?: ((null|struct)){ |(*(null){ null }, (struct){
      Other: (string){ string }
    }) }
}`,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := adt.NewContext(r, &adt.Vertex{})
			v, err := convert.FromGoType(ctx, tc.goTyp)
			if err != nil {
				t.Fatal(err)
			}
			got := debug.NodeString(ctx, v, nil)
			if got != tc.want {
				t.Errorf("\n got %q;\nwant %q", got, tc.want)
			}
			val, _ := ctx.Evaluate(&adt.Environment{}, v)
			if bot, ok := val.(*adt.Bottom); ok {
				t.Errorf("unexpected error when evaluating: %v", bot)
			}
		})
	}
}

func TestFromGoTypeConcurrent(t *testing.T) {
	// Note: there is a pre-existing race in compile.Expr which mutates
	// cached AST nodes. This test verifies that astFromGoType itself
	// (the AST construction) does not race, by checking that concurrent
	// calls complete without panicking or producing errors.
	// The -race flag may still detect the compile.Expr race which is
	// outside the scope of this fix.
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := runtime.New()
			ctx := adt.NewContext(r, &adt.Vertex{})
			_, err := convert.FromGoType(ctx, recursiveA{})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}
