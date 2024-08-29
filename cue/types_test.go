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
	"bytes"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/tdtest"
)

func getValue(m *cuetdtest.M, body string) cue.Value {
	return m.CueContext().CompileString(body, cue.Filename("test"))
}

func mustCompile(t testing.TB, ctx *cue.Context, body string) cue.Value {
	t.Helper()

	val := ctx.CompileString(body, cue.Filename("test"))
	if err := val.Err(); err != nil {
		t.Fatal(err)
	}
	return val
}

func TestAPI(t *testing.T) {
	testCases := []struct {
		input string
		fun   func(i cue.Value) cue.Value
		want  string
		skip  bool
	}{{
		// Issue #567
		input: `
		#runSpec: {action: foo: int}

		v: {ction: foo: 1}
				`,
		fun: func(i cue.Value) cue.Value {
			runSpec := i.LookupDef("#runSpec")
			v := i.Lookup("v")
			res := runSpec.Unify(v)
			return res
		},
		want: "_|_ // #runSpec.ction: field not allowed",

		skip: true,
	}, {
		// Issue #567
		input: `
		#runSpec: {action: foo: int}

		v: {action: Foo: 1}
				`,
		fun: func(i cue.Value) cue.Value {
			runSpec := i.LookupDef("#runSpec")
			v := i.Lookup("v")
			res := runSpec.Unify(v)
			return res
		},
		want: "_|_ // #runSpec.action.Foo: field not allowed",
	}, {
		input: `
		#runSpec: v: {action: foo: int}

		w: {ction: foo: 1}
					`,
		fun: func(i cue.Value) cue.Value {
			runSpec := i.LookupDef("#runSpec")
			v := runSpec.Lookup("v")
			w := i.Lookup("w")
			res := w.Unify(v)
			return res
		},
		want: "_|_ // w.ction: field not allowed",
	}, {
		// Issue #1879
		input: `
		#Steps: {
			...
		}

		test: #Steps & {
			if true {
				test1: "test1"
			}
			if false {
				test2: "test2"
			}
		}
		`,

		fun: func(v cue.Value) (val cue.Value) {
			sub := v.LookupPath(cue.ParsePath("test"))
			st, err := sub.Struct()
			if err != nil {
				panic(err)
			}

			for i := 0; i < st.Len(); i++ {
				val = st.Field(i).Value
			}

			return val
		},
		want: `"test1"`,
	}}
	for _, tc := range testCases {
		if tc.skip {
			continue
		}
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			m.TODO_V3(t)

			ctx := m.CueContext()

			valIn := mustCompile(t, ctx, tc.input)
			valOut := tc.fun(valIn)
			got := fmt.Sprintf("%+v", valOut)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestValueType(t *testing.T) {
	testCases := []struct {
		value          string
		kind           cue.Kind
		incompleteKind cue.Kind
		json           string
		valid          bool
		concrete       bool
		closed         bool
		// pos            token.Pos
	}{{ // Not a concrete value.
		value:          `v: _`,
		kind:           cue.BottomKind,
		incompleteKind: cue.TopKind,
	}, {
		value:          `v: _|_`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BottomKind,
		concrete:       true,
	}, {
		value:          `v: 1&2`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BottomKind,
		concrete:       true,
	}, {
		value:          `v: b, b: 1&2`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BottomKind,
		concrete:       true,
	}, {
		value:          `v: (b[a]), b: 1, a: 1`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BottomKind,
		concrete:       true,
	}, { // TODO: should be error{
		value: `v: (b)
			b: bool`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BoolKind,
	}, {
		value:          `v: ([][b]), b: "d"`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BottomKind,
		concrete:       true,
	}, {
		value:          `v: null`,
		kind:           cue.NullKind,
		incompleteKind: cue.NullKind,
		concrete:       true,
	}, {
		value:          `v: true`,
		kind:           cue.BoolKind,
		incompleteKind: cue.BoolKind,
		concrete:       true,
	}, {
		value:          `v: false`,
		kind:           cue.BoolKind,
		incompleteKind: cue.BoolKind,
		concrete:       true,
	}, {
		value:          `v: bool`,
		kind:           cue.BottomKind,
		incompleteKind: cue.BoolKind,
	}, {
		value:          `v: 2`,
		kind:           cue.IntKind,
		incompleteKind: cue.IntKind,
		concrete:       true,
	}, {
		value:          `v: 2.0`,
		kind:           cue.FloatKind,
		incompleteKind: cue.FloatKind,
		concrete:       true,
	}, {
		value:          `v: 2.0Mi`,
		kind:           cue.IntKind,
		incompleteKind: cue.IntKind,
		concrete:       true,
	}, {
		value:          `v: 14_000`,
		kind:           cue.IntKind,
		incompleteKind: cue.IntKind,
		concrete:       true,
	}, {
		value:          `v: >=0 & <5`,
		kind:           cue.BottomKind,
		incompleteKind: cue.NumberKind,
	}, {
		value:          `v: float`,
		kind:           cue.BottomKind,
		incompleteKind: cue.FloatKind,
	}, {
		value:          `v: "str"`,
		kind:           cue.StringKind,
		incompleteKind: cue.StringKind,
		concrete:       true,
	}, {
		value:          "v: '''\n'''",
		kind:           cue.BytesKind,
		incompleteKind: cue.BytesKind,
		concrete:       true,
	}, {
		value:          "v: string",
		kind:           cue.BottomKind,
		incompleteKind: cue.StringKind,
	}, {
		value:          `v: {}`,
		kind:           cue.StructKind,
		incompleteKind: cue.StructKind,
		concrete:       true,
	}, {
		value:          `v: close({})`,
		kind:           cue.StructKind,
		incompleteKind: cue.StructKind,
		concrete:       true,
		closed:         true,
	}, {
		value:          `v: []`,
		kind:           cue.ListKind,
		incompleteKind: cue.ListKind,
		concrete:       true,
		closed:         true,
	}, {
		value:          `v: [...int]`,
		kind:           cue.BottomKind,
		incompleteKind: cue.ListKind,
		concrete:       false,
	}, {
		value:    `v: {a: int, b: [1][a]}.b`,
		kind:     cue.BottomKind,
		concrete: false,
	}, {
		value: `import "time"
		v: time.Time`,
		kind:           cue.BottomKind,
		incompleteKind: cue.StringKind,
		concrete:       false,
	}, {
		value: `import "time"
		v: {a: time.Time}.a`,
		kind:           cue.BottomKind,
		incompleteKind: cue.StringKind,
		concrete:       false,
	}, {
		value: `import "time"
			v: {a: time.Time & string}.a`,
		kind:           cue.BottomKind,
		incompleteKind: cue.StringKind,
		concrete:       false,
	}, {
		value: `import "strings"
			v: {a: strings.ContainsAny("D")}.a`,
		kind:           cue.BottomKind,
		incompleteKind: cue.StringKind,
		concrete:       false,
	}, {
		value: `import "struct"
		v: {a: struct.MaxFields(2) & {}}.a`,
		kind:           cue.StructKind, // Can determine a valid struct already.
		incompleteKind: cue.StructKind,
		concrete:       true,
	}, {
		value: `v: #Foo
		#Foo: {
			name: string,
			...
		}`,
		kind:           cue.StructKind,
		incompleteKind: cue.StructKind,
		concrete:       true,
	}, {
		value: `v: #Foo
			#Foo: {
				name: string,
			}`,
		kind:           cue.StructKind,
		incompleteKind: cue.StructKind,
		concrete:       true,
		closed:         true,
	}, {
		value: `v: #Foo | int
		#Foo: {
			name: string,
			}`,
		incompleteKind: cue.StructKind | cue.IntKind,
		// Hard to tell what is correct here, but For backwards compatibility,
		// this is false.
		closed: false,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			val := getValue(m, tc.value)
			v := val.Lookup("v")
			if got := v.Kind(); got != tc.kind {
				t.Errorf("Kind: got %x; want %x", int(got), int(tc.kind))
			}
			want := tc.incompleteKind | cue.BottomKind
			if got := v.IncompleteKind(); got != want {
				t.Errorf("IncompleteKind: got %x; want %x", int(got), int(want))
			}
			if got := v.IsConcrete(); got != tc.concrete {
				t.Errorf("IsConcrete: got %v; want %v", got, tc.concrete)
			}
			if got := v.IsClosed(); got != tc.closed {
				t.Errorf("IsClosed: got %v; want %v", got, tc.closed)
			}
		})
	}
}

func TestInt(t *testing.T) {
	testCases := []struct {
		value  string
		int    int64
		uint   uint64
		base   int
		err    string
		errU   string
		notInt bool
	}{{
		value: "1",
		int:   1,
		uint:  1,
	}, {
		value: "-1",
		int:   -1,
		uint:  0,
		errU:  cue.ErrAbove.Error(),
	}, {
		value: "-111222333444555666777888999000",
		int:   math.MinInt64,
		uint:  0,
		err:   cue.ErrAbove.Error(),
		errU:  cue.ErrAbove.Error(),
	}, {
		value: "111222333444555666777888999000",
		int:   math.MaxInt64,
		uint:  math.MaxUint64,
		err:   cue.ErrBelow.Error(),
		errU:  cue.ErrBelow.Error(),
	}, {
		value:  "1.0",
		err:    "cannot use value 1.0 (type float) as int",
		errU:   "cannot use value 1.0 (type float) as int",
		notInt: true,
	}, {
		value:  "int",
		err:    "non-concrete value int",
		errU:   "non-concrete value int",
		notInt: true,
	}, {
		value:  "_|_",
		err:    "explicit error (_|_ literal) in source",
		errU:   "explicit error (_|_ literal) in source",
		notInt: true,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			n := getValue(m, tc.value)
			base := 10
			if tc.base > 0 {
				base = tc.base
			}
			b, err := n.AppendInt(nil, base)
			if checkFailed(t, err, tc.err, "append") {
				want := tc.value
				if got := string(b); got != want {
					t.Errorf("append: got %v; want %v", got, want)
				}
			}

			vi, err := n.Int64()
			checkErr(t, err, tc.err, "Int64")
			if vi != tc.int {
				t.Errorf("Int64: got %v; want %v", vi, tc.int)
			}

			vu, err := n.Uint64()
			checkErr(t, err, tc.errU, "Uint64")
			if vu != uint64(tc.uint) {
				t.Errorf("Uint64: got %v; want %v", vu, tc.uint)
			}
		})
	}
}

func TestFloat(t *testing.T) {
	testCases := []struct {
		value   string
		float   string
		float64 float64
		mant    string
		exp     int
		fmt     byte
		prec    int
		kind    cue.Kind
		err     string
	}{{
		value:   "1",
		float:   "1",
		mant:    "1",
		exp:     0,
		float64: 1,
		fmt:     'g',
		kind:    cue.IntKind,
	}, {
		value:   "-1",
		float:   "-1",
		mant:    "-1",
		exp:     0,
		float64: -1,
		fmt:     'g',
		kind:    cue.IntKind,
	}, {
		value:   "0.0",
		float:   "0.0",
		mant:    "0",
		exp:     -1,
		float64: 0.0,
		fmt:     'g',
		kind:    cue.FloatKind,
	}, {
		value:   "1.0",
		float:   "1.0",
		mant:    "10",
		exp:     -1,
		float64: 1.0,
		fmt:     'g',
		kind:    cue.FloatKind,
	}, {
		value:   "2.6",
		float:   "2.6",
		mant:    "26",
		exp:     -1,
		float64: 2.6,
		fmt:     'g',
		kind:    cue.FloatKind,
	}, {
		value:   "20.600",
		float:   "20.60",
		mant:    "20600",
		exp:     -3,
		float64: 20.60,
		prec:    2,
		fmt:     'f',
		kind:    cue.FloatKind,
	}, {
		value:   "1/0",
		float:   "",
		float64: 0,
		prec:    2,
		fmt:     'f',
		err:     "division by zero",
		kind:    cue.BottomKind,
	}, {
		value:   "1.797693134862315708145274237317043567982e+308",
		float:   "1.8e+308",
		mant:    "1797693134862315708145274237317043567982",
		exp:     269,
		float64: math.Inf(1),
		prec:    2,
		fmt:     'g',
		err:     cue.ErrAbove.Error(),
		kind:    cue.FloatKind,
	}, {
		value:   "-1.797693134862315708145274237317043567982e+308",
		float:   "-1.8e+308",
		mant:    "-1797693134862315708145274237317043567982",
		exp:     269,
		float64: math.Inf(-1),
		prec:    2,
		fmt:     'g',
		kind:    cue.FloatKind,
		err:     cue.ErrBelow.Error(),
	}, {
		value:   "4.940656458412465441765687928682213723650e-324",
		float:   "4.941e-324",
		mant:    "4940656458412465441765687928682213723650",
		exp:     -363,
		float64: 0,
		prec:    4,
		fmt:     'g',
		kind:    cue.FloatKind,
		err:     cue.ErrBelow.Error(),
	}, {
		value:   "-4.940656458412465441765687928682213723650e-324",
		float:   "-4.940656458412465441765687928682213723650e-324",
		mant:    "-4940656458412465441765687928682213723650",
		exp:     -363,
		float64: 0,
		prec:    -1,
		fmt:     'g',
		kind:    cue.FloatKind,
		err:     cue.ErrAbove.Error(),
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			n := getValue(m, tc.value)
			if n.Kind() != tc.kind {
				t.Fatal("Not a number")
			}

			var mant big.Int
			exp, err := n.MantExp(&mant)
			mstr := ""
			if err == nil {
				mstr = mant.String()
			}
			if exp != tc.exp || mstr != tc.mant {
				t.Errorf("mantExp: got %s %d; want %s %d", mstr, exp, tc.mant, tc.exp)
			}

			b, _ := n.AppendFloat(nil, tc.fmt, tc.prec)
			want := tc.float
			if got := string(b); got != want {
				t.Errorf("append: got %v; want %v", got, want)
			}

			f, err := n.Float64()
			checkErr(t, err, tc.err, "Float64")
			if f != tc.float64 {
				t.Errorf("Float64: got %v; want %v", f, tc.float64)
			}
		})
	}
}

func TestString(t *testing.T) {
	testCases := []struct {
		value string
		str   string
		err   string
	}{{
		value: `""`,
		str:   ``,
	}, {
		value: `"Hello world!"`,
		str:   `Hello world!`,
	}, {
		value: `"Hello \(#world)!"
		#world: "world"`,
		str: `Hello world!`,
	}, {
		value: `string`,
		err:   "non-concrete value string",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			str, err := getValue(m, tc.value).String()
			checkFatal(t, err, tc.err, "init")
			if str != tc.str {
				t.Errorf("String: got %q; want %q", str, tc.str)
			}

			b, err := getValue(m, tc.value).Bytes()
			checkFatal(t, err, tc.err, "init")
			if got := string(b); got != tc.str {
				t.Errorf("Bytes: got %q; want %q", got, tc.str)
			}

			r, err := getValue(m, tc.value).Reader()
			checkFatal(t, err, tc.err, "init")
			b, _ = io.ReadAll(r)
			if got := string(b); got != tc.str {
				t.Errorf("Reader: got %q; want %q", got, tc.str)
			}
		})
	}
}

func TestError(t *testing.T) {
	testCases := []struct {
		value string
		err   string
	}{{
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `"Hello world!"`,
	}, {
		value: `string`,
		err:   "",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			err := getValue(m, tc.value).Err()
			checkErr(t, err, tc.err, "init")
		})
	}
}

func TestNull(t *testing.T) {
	testCases := []struct {
		value string
		err   string
	}{{
		value: `v: _|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `v: "str"`,
		err:   "cannot use value \"str\" (type string) as null",
	}, {
		value: `v: null`,
	}, {
		value: `v: _`,
		err:   "non-concrete value _",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, tc.value).Lookup("v")
			err := v.Null()
			checkErr(t, err, tc.err, "init")
			wantBool := err == nil
			gotBool := v.IsNull()
			if wantBool != gotBool {
				t.Fatalf("IsNull reported %v, but Null reported: %v", gotBool, err)
			}
		})
	}
}

func TestBool(t *testing.T) {
	testCases := []struct {
		value string
		bool  bool
		err   string
	}{{
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `"str"`,
		err:   "cannot use value \"str\" (type string) as bool",
	}, {
		value: `true`,
		bool:  true,
	}, {
		value: `false`,
	}, {
		value: `bool`,
		err:   "non-concrete value bool",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			got, err := getValue(m, tc.value).Bool()
			if checkErr(t, err, tc.err, "init") {
				if got != tc.bool {
					t.Errorf("got %v; want %v", got, tc.bool)
				}
			}
		})
	}
}

func TestList(t *testing.T) {
	testCases := []struct {
		value string
		res   string
		err   string
	}{{
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `"str"`,
		err:   "cannot use value \"str\" (type string) as list",
	}, {
		value: `[]`,
		res:   "[]",
	}, {
		value: `[1,2,3]`,
		res:   "[1,2,3,]",
	}, {
		value: `[for x in #y if x > 1 { x }]
		#y: [1,2,3]`,
		res: "[2,3,]",
	}, {
		value: `[int]`,
		err:   "cannot convert incomplete value",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			l, err := getValue(m, tc.value).List()
			checkFatal(t, err, tc.err, "init")

			buf := []byte{'['}
			for wantIdx := 0; l.Next(); wantIdx++ {
				// Ensure that we can get each index as well.
				if got := l.Selector().Index(); got != wantIdx {
					t.Errorf("Index got %v; want %v", got, wantIdx)
				}
				b, err := l.Value().MarshalJSON()
				checkFatal(t, err, tc.err, "list.Value")
				buf = append(buf, b...)
				buf = append(buf, ',')
			}
			buf = append(buf, ']')
			if got := string(buf); got != tc.res {
				t.Errorf("got %v; want %v", got, tc.res)
			}
		})
	}
}

func TestFields(t *testing.T) {
	testCases := []struct {
		value string
		res   string
		err   string
		opts  []cue.Option

		todoV3 bool
	}{{
		value: `{ #def: 1, _hidden: 2, opt?: 3, reg: 4 }`,
		res:   "{reg:4,}",
	}, {
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `"str"`,
		err:   "cannot use value \"str\" (type string) as struct",
	}, {
		value: `{}`,
		res:   "{}",
	}, {
		value: `{a:1,b:2,c:3}`,
		res:   "{a:1,b:2,c:3,}",
	}, {
		value: `{a:1,"_b":2,c:3,_d:4}`,
		res:   `{a:1,"_b":2,c:3,}`,
	}, {
		value: `{_a:"a"}`,
		res:   "{}",
	}, {
		value: `{ for k, v in #y if v > 1 {"\(k)": v} }
			#y: {a:1,b:2,c:3}`,
		res: "{b:2,c:3,}",
	}, {
		value: `{ #def: 1, _hidden: 2, opt?: 3, reg: 4 }`,
		res:   "{reg:4,}",
	}, {
		value: `{a:1,b:2,c:int}`,
		err:   "cannot convert incomplete value",
	}, {
		value: `
		step1: {}
		step2: {prefix: 3}
		if step2.value > 100 {
		   step3: {prefix: step2.value}
		}
		_hidden: 3`,
		res: `{step1:{},step2:{"prefix":3},}`,
	}, {
		opts: []cue.Option{cue.Final()},
		value: `
		step1: {}
		if step1.value > 100 {
		}`,
		err: "undefined field: value",
	}, {
		opts: []cue.Option{cue.Concrete(true)},
		value: `
		step1: {}
		if step1.value > 100 {
		}`,
		err: "undefined field: value",
	}, {
		value: `{a!: 1, b?: 2, c: 3}`,
		err:   "a: field is required but not present",
	}, {
		opts:  []cue.Option{cue.Hidden(true)},
		value: `1, _a: 2`,
		res:   `{_a:2,}`,
	}, {
		opts:  []cue.Option{cue.Definitions(true)},
		value: `1, #a: 2`,
		res:   `{#a:2,}`,
	}, {
		opts:  []cue.Option{cue.Optional(true)},
		value: `1, a?: 2`,
		err:   "cannot use value 1 (type int) as struct",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			obj := getValue(m, tc.value)

			iter, err := obj.Fields(tc.opts...)
			checkFatal(t, err, tc.err, "init")

			buf := []byte{'{'}
			for iter.Next() {
				buf = append(buf, iter.Selector().String()...)
				buf = append(buf, ':')
				b, err := iter.Value().MarshalJSON()
				checkFatal(t, err, tc.err, "Obj.At")
				buf = append(buf, b...)
				buf = append(buf, ',')
			}
			buf = append(buf, '}')
			if got := string(buf); got != tc.res {
				t.Errorf("got %v; want %v", got, tc.res)
			}

			iter, _ = obj.Fields(tc.opts...)
			for iter.Next() {
				want, err := iter.Value().MarshalJSON()
				checkFatal(t, err, tc.err, "Obj.At2")

				got, err := obj.LookupPath(cue.MakePath(iter.Selector())).MarshalJSON()
				checkFatal(t, err, tc.err, "Obj.At2")

				if !bytes.Equal(got, want) {
					t.Errorf("Lookup: got %q; want %q", got, want)
				}
			}
			v := obj.LookupPath(cue.MakePath(cue.Str("non-existing")))
			checkErr(t, v.Err(), "not found", "non-existing")
		})
	}
}

func TestAllFields(t *testing.T) {
	testCases := []struct {
		value string
		res   string
		err   string
	}{{
		value: `{a:1,"_b":2,c:3,_d:4}`,
		res:   `{a:1,"_b":2,c:3,_d:4,}`,
	}, {
		value: `{_a:"a"}`,
		res:   `{_a:"a",}`,
	}, {
		value: `{_a:"a", b?: "b", #c: 3}`,
		res:   `{_a:"a",b?:"b",#c:3,}`,
	}, {
		// Issue #1879
		value: `{a: 1, if false { b: 2 }}`,
		res:   `{a:1,}`,
	}, {
		value: `{a!:1,b?:2,c:3}`,
		res:   `{a!:1,b?:2,c:3,}`,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			obj := getValue(m, tc.value)

			var iter *cue.Iterator // Verify that the returned iterator is a pointer.
			iter, err := obj.Fields(cue.All())
			checkFatal(t, err, tc.err, "init")

			buf := []byte{'{'}
			for iter.Next() {
				buf = append(buf, iter.Selector().String()...)
				buf = append(buf, ':')
				b, err := iter.Value().MarshalJSON()
				checkFatal(t, err, tc.err, "Obj.At")
				buf = append(buf, b...)
				buf = append(buf, ',')
			}
			buf = append(buf, '}')
			if got := string(buf); got != tc.res {
				t.Errorf("got %v; want %v", got, tc.res)
			}
		})
	}
}

func TestFieldType(t *testing.T) {
	testCases := []struct {
		value string
		want  string
	}{{
		value: `{a:1,"_b":2,c:3,_d:4,#def: 1}`,
		want: `
		StringLabel
		StringLabel
		StringLabel
		HiddenLabel
		DefinitionLabel`,
	}, {
		value: `{a!:1,b?:2,c:3}`,
		want: `
		StringLabel|RequiredConstraint
		StringLabel|OptionalConstraint
		StringLabel`,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			obj := getValue(m, tc.value)

			iter, err := obj.Fields(cue.All())
			if err != nil {
				t.Fatal(err)
			}

			b := &strings.Builder{}
			for iter.Next() {
				fmt.Fprint(b, "\n\t\t", iter.FieldType())
			}
			if got := b.String(); got != tc.want {
				t.Errorf("got:%v\nwant:%v", got, tc.want)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		ctx := m.CueContext()
		val := mustCompile(t, ctx, `
#V: {
	x: int
}
#X: {
	[string]: int64
} & #V
v: #X

a: {
	b!: 1
	c: 2
}`)
		// expr, err := parser.ParseExpr("lookup.cue", `v`, parser.DeclarationErrors, parser.AllErrors)
		// if err != nil {
		// 	log.Fatalf("parseExpr: %v", err)
		// }
		// v := inst.Eval(expr)

		type testCase struct {
			ref    []string
			result string
			syntax string
		}
		testCases := []testCase{{
			ref: []string{"a"},
			result: `{
	b!: 1
	c:  2
}`,
			syntax: "{b!: 1, c: 2}",
		}, {
			// Allow descending into structs even if it has a required field error.
			ref:    []string{"a", "c"},
			result: "2",
			syntax: "2",
		}, {
			ref:    []string{"a", "b"},
			result: "_|_ // a.b: field is required but not present",
			syntax: "1",
		}, {
			ref:    []string{"v", "x"},
			result: "int64",
			syntax: "int64",
		}}
		for _, tc := range testCases {
			t.Run("", func(t *testing.T) {
				v := val.Lookup(tc.ref...)

				if got := fmt.Sprintf("%+v", v); got != tc.result {
					t.Errorf("got %v; want %v", got, tc.result)
				}

				got := fmt.Sprint(astinternal.DebugStr(v.Eval().Syntax()))
				if got != tc.syntax {
					t.Errorf("got %v; want %v", got, tc.syntax)
				}

				v = val.Lookup()
				for _, ref := range tc.ref {
					s, err := v.Struct()
					if err != nil {
						t.Fatal(err)
					}
					fi, err := s.FieldByName(ref, false)
					if err != nil {
						t.Fatal(err)
					}
					v = fi.Value

					// Struct gets all fields. Skip tests with optional fields,
					// as the result will differ.
					if cue.ValueVertex(v).ArcType != adt.ArcMember {
						return
					}
				}

				if got := fmt.Sprintf("%+v", v); got != tc.result {
					t.Errorf("got %v; want %v", got, tc.result)
				}

				got = fmt.Sprint(astinternal.DebugStr(v.Eval().Syntax()))
				if got != tc.syntax {
					t.Errorf("got %v; want %v", got, tc.syntax)
				}
			})
		}
	})
}

func goValue(v cue.Value) interface{} {
	var x interface{}
	err := v.Decode(&x)
	if err != nil {
		return err
	}
	return x
}

// TODO: Exporting of Vertex as Conjunct
func TestFill(t *testing.T) {
	// TODO: run with matrix.

	ctx := cuecontext.New()

	val := ctx.BuildExpr(ast.NewStruct("bar", ast.NewString("baz")))
	if err := val.Err(); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		in   string
		x    interface{}
		path string // comma-separated path
		out  string
	}{{
		in: `
		foo: int
		bar: foo
		`,
		x:    3,
		path: "foo",
		out: `
		foo: 3
		bar: 3
		`,
	}, {
		in: `
		string
		`,
		x:    "foo",
		path: "",
		out: `
		"foo"
		`,
	}, {
		in: `
		foo: _
		`,
		x:    val,
		path: "foo",
		out: `
		{foo: {bar: "baz"}}
		`,
	}}

	for _, tc := range testCases {
		var path []string
		if tc.path != "" {
			path = strings.Split(tc.path, ",")
		}

		v := mustCompile(t, ctx, tc.in)
		v = v.Fill(tc.x, path...)

		w := mustCompile(t, ctx, tc.out)

		if diff := cmp.Diff(goValue(v), goValue(w)); diff != "" {
			t.Error(diff)
			t.Errorf("\ngot:  %s\nwant: %s", v, w)
		}
	}
}

func TestFill2(t *testing.T) {
	// TODO: run with matrix.

	ctx := cuecontext.New()

	root := mustCompile(t, ctx, `
	#Provider: {
		ID: string
		notConcrete: bool
		a: int
		b: int
	}
	`)

	spec := root.LookupDef("#Provider")
	providerInstance := spec.Fill("12345", "ID")
	root = root.Fill(providerInstance, "providers", "myprovider")

	got := fmt.Sprintf("%#v", root)
	want := `#Provider: {
	ID:          string
	notConcrete: bool
	a:           int
	b:           int
}
providers: {
	myprovider: {
		ID:          "12345"
		notConcrete: bool
		a:           int
		b:           int
	}
}`
	if got != want {
		t.Errorf("got:  %s\nwant: %s", got, want)
	}
}

func TestFillPath(t *testing.T) {
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		ctx := m.CueContext()

		val := ctx.BuildExpr(ast.NewStruct("bar", ast.NewString("baz")))
		if err := val.Err(); err != nil {
			t.Fatal(err)
		}

		testCases := []struct {
			in   string
			x    interface{}
			path cue.Path
			out  string
		}{{
			in: `
		foo: int
		bar: foo
		`,
			x:    3,
			path: cue.ParsePath("foo"),
			out: `
		foo: 3
		bar: 3
		`,
		}, {
			in: `
		X="#foo": int
		bar: X
		`,
			x:    3,
			path: cue.ParsePath(`"#foo"`),
			out: `
		"#foo": 3
		bar: 3
		`,
		}, {
			in: `
		X="#foo": foo: int
		bar: X.foo
		`,
			x:    3,
			path: cue.ParsePath(`"#foo".foo`),
			out: `
		"#foo": foo: 3
		bar: 3
		`,
		}, {
			in: `
		foo: #foo: int
		bar: foo.#foo
		`,
			x:    3,
			path: cue.ParsePath("foo.#foo"),
			out: `
		foo: {
			#foo: 3
		}
		bar: 3
		`,
		}, {
			in: `
		foo: _foo: int
		bar: foo._foo
		`,
			x:    3,
			path: cue.MakePath(cue.Str("foo"), cue.Hid("_foo", "_")),
			out: `
		foo: {
			_foo: 3
		}
		bar: 3
		`,
		}, {
			in: `
		string
		`,
			x:    "foo",
			path: cue.ParsePath(""),
			out: `
		"foo"
		`,
		}, {
			in: `
		foo: _
		`,
			x:    val,
			path: cue.ParsePath("foo"),
			out: `
		{foo: {bar: "baz"}}
		`,
		}, {
			// Resolve to enclosing
			in: `
		foo: _
		x: 1
		`,
			x:    ast.NewIdent("x"),
			path: cue.ParsePath("foo"),
			out: `
		{foo: 1, x: 1}
		`,
		}, {
			in: `
		foo: {
			bar: _
			x: 1
		}
		`,
			x:    ast.NewIdent("x"),
			path: cue.ParsePath("foo.bar"),
			out: `
		{foo: {bar: 1, x: 1}}
		`,
		}, {
			// Resolve one scope up
			in: `
		x: 1
		foo: {
			bar: _
		}
		`,
			x:    ast.NewIdent("x"),
			path: cue.ParsePath("foo.bar"),
			out: `
		{foo: {bar: 1}, x: 1}
		`,
		}, {
			// Resolve within ast expression
			in: `
		foo: {
			bar: _
		}
		`,
			x: ast.NewStruct(
				ast.NewIdent("x"), ast.NewString("1"),
				ast.NewIdent("y"), ast.NewIdent("x"),
			),
			path: cue.ParsePath("foo.bar"),
			out: `
			{foo: {bar: {x: "1", y: "1"}}}
			`,
		}, {
			// Resolve in non-existing
			in: `
		foo: x: 1
		`,
			x:    ast.NewIdent("x"),
			path: cue.ParsePath("foo.bar.baz"),
			out: `
		{foo: {x: 1, bar: baz: 1}}
		`,
		}, {
			// empty path
			in: `
		_
		#foo: 1
		`,
			x:   ast.NewIdent("#foo"),
			out: `{1, #foo: 1}`,
		}, {
			in:   `[...int]`,
			x:    1,
			path: cue.ParsePath("0"),
			out:  `[1]`,
		}, {
			in:   `[1, ...int]`,
			x:    1,
			path: cue.ParsePath("1"),
			out:  `[1, 1]`,
		}, {
			in:   `a: {b: v: int, c: v: int}`,
			x:    1,
			path: cue.MakePath(cue.Str("a"), cue.AnyString, cue.Str("v")),
			out: `{
	a: {
		b: {
			v: 1
		}
		c: {
			v: 1
		}
	}
}`,
		}, {
			in:   `a: [_]`,
			x:    1,
			path: cue.MakePath(cue.Str("a"), cue.AnyIndex, cue.Str("b")),
			out: `{
	a: [{
		b: 1
	}]
}`,
		}, {
			in:   `a: 1`,
			x:    1,
			path: cue.MakePath(cue.Str("b").Optional()),
			out:  `{a: 1}`,
		}, {
			in:   `b: int`,
			x:    1,
			path: cue.MakePath(cue.Str("b").Optional()),
			out:  `{b: 1}`,
		}}

		for _, tc := range testCases {
			t.Run("", func(t *testing.T) {
				v := mustCompile(t, ctx, tc.in)
				v = v.FillPath(tc.path, tc.x)

				w := mustCompile(t, ctx, tc.out)

				if diff := cmp.Diff(goValue(v), goValue(w)); diff != "" {
					t.Error(diff)
					t.Error(cmp.Diff(goValue(v), goValue(w)))
					t.Errorf("\ngot:  %s\nwant: %s", v, w)
				}
			})
		}
	})
}

func TestFillPathError(t *testing.T) {
	testCases := []struct {
		in   string
		x    interface{}
		path cue.Path
		err  string
	}{{
		// unsupported type.
		in:  `_`,
		x:   make(chan int),
		err: "unsupported Go type (chan int)",
	}}

	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()
			v := mustCompile(t, ctx, tc.in)
			v = v.FillPath(tc.path, tc.x)

			err := v.Err()
			if err == nil {
				t.Errorf("unexpected success")

			} else if got := err.Error(); !strings.Contains(got, tc.err) {
				t.Errorf("\ngot:  %s\nwant: %s", got, tc.err)
			}
		})
	}
}

func TestAllows(t *testing.T) {
	testCases := []struct {
		desc  string
		in    string
		sel   cue.Selector
		allow bool

		todo_nosharing bool
	}{{
		desc: "allow new field in open struct",
		in: `
		x: {
			a: int
		}
		`,
		sel:   cue.Str("b"),
		allow: true,
	}, {
		desc: "disallow new field in definition",
		in: `
		x: #Def
		#Def: {
			a: int
		}
		`,
		sel: cue.Str("b"),
	}, {
		desc: "disallow new field in explicitly closed struct",
		in: `
		x: close({
			a: int
		})
		`,
		sel: cue.Str("b"),
	}, {
		desc: "allow field in pattern",
		in: `
				x: #X
				#X: [>"a"]: 1
				`,
		sel:   cue.Str("b"),
		allow: true,
	}, {
		desc: "allow index in open list",
		in: `
		x: [...int]
		`,
		sel:   cue.Index(100),
		allow: true,
	}, {
		desc: "disallow index in closed list",
		in: `
		x: []
		`,
		sel: cue.Index(0),
	}, {
		desc: "allow existing index in closed list",
		in: `
		x: [1]
		`,
		sel:   cue.Index(0),
		allow: true,
	}, {
		desc: "definition in non-def closed list",
		in: `
		x: [1]
		`,
		sel:   cue.Def("#foo"),
		allow: true,
	}, {
		// TODO(disallow)
		desc: "definition in def open list",
		in: `
		x: #Def
		x: [1]
		#Def: [...int]
		`,
		sel:   cue.Def("#foo"),
		allow: true,
	}, {
		desc: "field in def open list",
		in: `
		x: #Def
		x: [1]
		#Def: [...int]
		`,
		sel: cue.Str("foo"),
	}, {
		desc: "definition in open scalar",
		in: `
		x: 1
		`,
		sel:   cue.Def("#foo"),
		allow: true,
	}, {
		desc: "field in scalar",
		in: `
		x: #Def
		x: 1
		#Def: int
		`,
		sel: cue.Str("foo"),
	}, {
		desc: "any index in closed list",
		in: `
		x: [1]
		`,
		sel: cue.AnyIndex,
	}, {
		desc: "any index in open list",
		in: `
		x: [...int]
			`,
		sel:   cue.AnyIndex,
		allow: true,
	}, {
		desc: "definition in open scalar",
		in: `
		x: 1
		`,
		sel:   cue.AnyDefinition,
		allow: true,
	}, {
		desc: "field in open scalar",
		in: `
			x: 1
			`,
		sel: cue.AnyString,

		// TODO(v0.6.0)
		// }, {
		// 	desc: "definition in closed scalar",
		// 	in: `
		// 	x: #Def
		// 	x: 1
		// 	#Def: int
		// 	`,
		// 	sel:   cue.AnyDefinition,
		// 	allow: true,
	}, {
		desc: "allow field in any",
		in: `
			x: _
			`,
		sel:   cue.AnyString,
		allow: true,
	}, {
		desc: "allow index in any",
		in: `
		x: _
		`,
		sel:   cue.AnyIndex,
		allow: true,
	}, {
		desc: "allow index in disjunction",
		in: `
		x: [...int] | 1
		`,
		sel:   cue.AnyIndex,
		allow: true,
	}, {
		desc: "allow index in disjunction",
		in: `
		x: [] | [...int]
			`,
		sel:   cue.AnyIndex,
		allow: true,
	}, {
		desc: "disallow index in disjunction",
		in: `
		x: [1, 2] | [3, 2]
		`,
		sel: cue.AnyIndex,
	}, {
		desc: "disallow index in non-list disjunction",
		in: `
		x: "foo" | 1
		`,
		sel: cue.AnyIndex,
	}, {
		desc: "allow label in disjunction",
		in: `
		x: {} | 1
		`,
		sel:   cue.AnyString,
		allow: true,
	}, {
		desc: "allow label in disjunction",
		in: `
		x: #Def
		#Def: { a: 1 } | { b: 1, ... }
		`,
		sel:   cue.AnyString,
		allow: true,

		todo_nosharing: true,
	}, {
		desc: "disallow label in disjunction",
		in: `
		x: #Def
		#Def: { a: 1 } | { b: 1 }
		`,
		sel: cue.AnyString,
	}, {
		desc: "pattern constraint",
		in: `
		x: #PC
		#PC: [>"m"]: int
		`,
		sel: cue.Str(""),
	}, {
		desc: "pattern constraint",
		in: `
		x: #PC
		#PC: [>"m"]: int
		`,
		sel:   cue.Str("z"),
		allow: true,
	}, {
		desc: "any in pattern constraint",
		in: `
		x: #PC
		#PC: [>"m"]: int
		`,
		sel: cue.AnyString,
	}, {
		desc: "any in pattern constraint",
		in: `
		x: #PC
		#PC: [>" "]: int
		`,
		sel: cue.AnyString,
	}}

	path := cue.ParsePath("x")

	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.desc, func(t *testing.T, m *cuetdtest.M) {
			if tc.todo_nosharing {
				m.TODO_NoSharing(t)
			}
			ctx := m.CueContext()
			v := mustCompile(t, ctx, tc.in)
			v = v.LookupPath(path)

			got := v.Allows(tc.sel)
			if got != tc.allow {
				t.Errorf("got %v; want %v", got, tc.allow)
			}
		})
	}
}

func TestFillFloat(t *testing.T) {
	// This tests panics for issue #749

	want := `{
	x: 3.14
}`

	filltest := func(x interface{}) {
		ctx := cuecontext.New()
		val := mustCompile(t, ctx, `
	x: number
	`)
		val = val.Fill(x, "x")

		got := fmt.Sprint(val)
		if got != want {
			t.Errorf("got:  %s\nwant: %s", got, want)
		}
	}

	filltest(float32(3.14))
	filltest(float64(3.14))
	filltest(big.NewFloat(3.14))
}

func TestValue_LookupDef(t *testing.T) {
	testCases := []struct {
		in     string
		def    string // comma-separated path
		exists bool
		out    string
	}{{
		in:  `#foo: 3`,
		def: "#foo",
		out: `3`,
	}, {
		in:  `_foo: 3`,
		def: "_foo",
		out: `_|_ // field not found: #_foo`,
	}, {
		in:  `_#foo: 3`,
		def: "_#foo",
		out: `_|_ // field not found: _#foo`,
	}, {
		in:  `"foo", #foo: 3`,
		def: "#foo",
		out: `3`,
	}}

	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.def, func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()
			v := mustCompile(t, ctx, tc.in)
			v = v.LookupDef(tc.def)
			got := fmt.Sprint(v)

			if got != tc.out {
				t.Errorf("\ngot:  %s\nwant: %s", got, tc.out)
			}
		})
	}
}

// TODO: trim down to individual defaults?
func TestDefaults(t *testing.T) {
	testCases := []struct {
		value string
		def   string
		val   string
		ok    bool
	}{{
		value: `number | *1`,
		def:   "1",
		val:   "number",
		ok:    true,
	}, {
		value: `1 | 2 | *3`,
		def:   "3",
		val:   "1|2|3",
		ok:    true,
	}, {
		value: `*{a:1,b:2}|{a:1}|{b:2}`,
		def:   "{a:1,b:2}",
		val:   "{a: 1}|{b: 2}",
		ok:    true,
	}, {
		value: `{a:1}&{b:2}`,
		def:   `{a:1,b:2}`,
		val:   ``,
		ok:    false,
	}, {
		value: `*_|_ | (*"x" | string)`,
		def:   `"x" | string`,
		val:   `"x"|string`,
		ok:    false,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, "a: "+tc.value).Lookup("a")

			v = v.Eval()
			d, ok := v.Default()
			if ok != tc.ok {
				t.Errorf("hasDefault: got %v; want %v", ok, tc.ok)
			}

			if got := compactRawStr(d); got != tc.def {
				t.Errorf("default: got %v; want %v", got, tc.def)
			}

			op, val := d.Expr()
			if op != cue.OrOp {
				return
			}
			vars := []string{}
			for _, v := range val {
				vars = append(vars, fmt.Sprint(v))
			}
			if got := strings.Join(vars, "|"); got != tc.val {
				t.Errorf("value: got %v; want %v", got, tc.val)
			}
		})
	}
}

func TestLen(t *testing.T) {
	testCases := []struct {
		input  string
		length string
	}{{
		input:  "[1, 3]",
		length: "2",
	}, {
		input:  "[1, 3, ...]",
		length: "int & >=2",
	}, {
		input:  `"foo"`,
		length: "3",
	}, {
		input:  `'foo'`,
		length: "3",
		// TODO: Currently not supported.
		// }, {
		// 	input:  "{a:1, b:3, a:1, c?: 3, _hidden: 4}",
		// 	length: "2",
	}, {
		input:  "3",
		length: "_|_ // len not supported for type int",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.input, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, "a: "+tc.input).Lookup("a")

			length := v.Len()
			if got := fmt.Sprint(length); got != tc.length {
				t.Errorf("length: got %v; want %v", got, tc.length)
			}
		})
	}
}

func TestTemplate(t *testing.T) {
	testCases := []struct {
		value string
		path  []string
		want  string

		skip bool
	}{{
		value: `
		a: [Name=string]: Name
		`,
		path: []string{"a", ""},
		want: `"label"`,
	}, {
		value: `
		[Name=string]: { a: Name }
		`,
		path: []string{"", "a"},
		want: `"label"`,
	}, {
		value: `
		[Name=string]: { a: Name }
		`,
		path: []string{""},
		want: `{"a":"label"}`,
	}, {
		value: `
		a: [Foo=string]: [Bar=string]: { b: Foo+Bar }
		`,
		path: []string{"a", "", ""},
		want: `{"b":"labellabel"}`,
	}, {
		value: `
		a: [Foo=string]: b: [Bar=string]: { c: Foo+Bar }
		a: foo: b: [Bar=string]: { d: Bar }
		`,
		path: []string{"a", "foo", "b", ""},
		want: `{"c":"foolabel","d":"label"}`,

		skip: true, // TODO: reordering
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			if tc.skip {
				m.TODO_V3(t)
			}

			v := getValue(m, tc.value)
			for _, p := range tc.path {
				if p == "" {
					v = v.Template()("label")
				} else {
					v = v.Lookup(p)
				}
			}
			b, err := v.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if got := string(b); got != tc.want {
				t.Errorf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestElem(t *testing.T) {
	testCases := []struct {
		value string
		path  []string
		want  string
		skip  bool
	}{{
		value: `
		a: [...int]
		`,
		path: []string{"a", ""},
		want: `int`,
	}, {
		value: `
		[Name=string]: { a: Name }
		`,
		path: []string{"", "a"},
		want: `string`,
	}, {
		value: `
		[Name=string]: { a: Name }
		`,
		path: []string{""},
		want: "{\n\ta: string\n}",
	}, {
		value: `
		a: [Foo=string]: [Bar=string]: { b: Foo+Bar }
		`,
		path: []string{"a", "", ""},
		want: "{\n\tb: string + string\n}",
	}, {
		value: `
		a: [Foo=string]: b: [Bar=string]: { c: Foo+Bar }
		a: foo: b: [Bar=string]: { d: Bar }
		`,
		path: []string{"a", "foo", "b", ""},
		want: "{\n\tc: \"foo\" + string\n\td: string\n}",
		skip: true, // TODO(p3): Skip because this is just a reordering.
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			if tc.skip {
				m.TODO_V3(t)
			}

			v := getValue(m, tc.value)
			cue.ValueVertex(v).Finalize(cue.ValueCtx(v))
			for _, p := range tc.path {
				if p == "" {
					var ok bool
					v, ok = v.Elem()
					if !ok {
						t.Fatal("expected element")
					}
				} else {
					v = v.Lookup(p)
				}
			}
			got := fmt.Sprint(v)

			if got != tc.want {
				t.Errorf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestSubsume(t *testing.T) {
	a := cue.ParsePath("a")
	b := cue.ParsePath("b")
	testCases := []struct {
		value   string
		pathA   cue.Path
		pathB   cue.Path
		options []cue.Option
		want    bool
	}{{
		value: `4`,
		want:  true,
	}, {
		value: `a: string, b: "foo"`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: string, b: "foo"`,
		pathA: b,
		pathB: a,
		want:  false,
	}, {
		value: `a: {a: string, b: 4}, b: {a: "foo", b: 4}`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: [string,  4], b: ["foo", 4]`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: [...string], b: ["foo"]`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: [...int], b: ["foo"]`,
		pathA: a,
		pathB: b,
		want:  false,
	}, {
		// Issue #566
		// Closed struct subsuming open struct.
		value: `
		#Run: { action: "run", command: [...string] }
		b: { action: "run", command: ["echo", "hello"] }
		`,
		pathA: cue.ParsePath("#Run"),
		pathB: b,

		// NOTE: this is for v0.2 compatibility. Logically a closed struct
		// does not subsume an open struct. One could argue that the default
		// of an open struct is the closed struct with the minimal number
		// of fields that is an instance of it, though.
		want: true, // open struct is not subsumed by closed if not final.
	}, {
		// Issue #566
		// Closed struct subsuming open struct.
		value: `
			#Run: { action: "run", command: [...string] }
			b: { action: "run", command: ["echo", "hello"] }
			`,
		pathA:   cue.ParsePath("#Run"),
		pathB:   b,
		options: []cue.Option{cue.Final()},
		want:    true,
	}, {
		// default
		value: `
		a: <5
		b: *3 | int
		`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		// Disable default elimination.
		value: `
			a: <5
			b: *3 | int
			`,
		pathA:   a,
		pathB:   b,
		options: []cue.Option{cue.Raw()},
		want:    false,
	}, {
		value: `
			#A: {
				exact: string
			} | {
				regex: string
			}
			#B: {
				exact: string
			} | {
				regex: string
			}
			`,
		pathA:   cue.ParsePath("#A"),
		pathB:   cue.ParsePath("#B"),
		options: []cue.Option{},
		want:    true,
	}, {
		value: `
			import "time"
			a: time.Format(time.ANSIC)
			b: 1
			`,
		pathA: a,
		pathB: b,
		want:  false,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, tc.value)
			a := v.LookupPath(tc.pathA)
			b := v.LookupPath(tc.pathB)
			got := a.Subsume(b, tc.options...) == nil
			if got != tc.want {
				t.Errorf("got %v (%v); want %v (%v)", got, a, tc.want, b)
			}
		})
	}
}

func TestSubsumes(t *testing.T) {
	a := []string{"a"}
	b := []string{"b"}
	testCases := []struct {
		value string
		pathA []string
		pathB []string
		want  bool
	}{{
		value: `4`,
		want:  true,
	}, {
		value: `a: string, b: "foo"`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: string, b: "foo"`,
		pathA: b,
		pathB: a,
		want:  false,
	}, {
		value: `a: {a: string, b: 4}, b: {a: "foo", b: 4}`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: [string,  4], b: ["foo", 4]`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: [...string], b: ["foo"]`,
		pathA: a,
		pathB: b,
		want:  true,
	}, {
		value: `a: [...int], b: ["foo"]`,
		pathA: a,
		pathB: b,
		want:  false,
	}, {
		value: `
		a: { action: "run", command: [...string] }
		b: { action: "run", command: ["echo", "hello"] }
		`,
		pathA: a,
		pathB: b,
		want:  true,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, tc.value)
			a := v.Lookup(tc.pathA...)
			b := v.Lookup(tc.pathB...)
			got := a.Subsumes(b)
			if got != tc.want {
				t.Errorf("got %v (%v); want %v (%v)", got, a, tc.want, b)
			}
		})
	}
}

func TestUnify(t *testing.T) {
	a := "a"
	b := "b"
	type testCase struct {
		value string
		pathA string
		pathB string
		want  string
	}
	testCases := []testCase{{
		value: `4`,
		want:  `4`,
	}, {
		value: `a: string, b: "foo"`,
		pathA: a,
		pathB: b,
		want:  `"foo"`,
	}, {
		value: `a: string, b: "foo"`,
		pathA: b,
		pathB: a,
		want:  `"foo"`,
	}, {
		value: `a: {a: string, b: 4}, b: {a: "foo", b: 4}`,
		pathA: a,
		pathB: b,
		want:  `{"a":"foo","b":4}`,
	}, {
		value: `a: [string,  4], b: ["foo", 4]`,
		pathA: a,
		pathB: b,
		want:  `["foo",4]`,
	}, {
		value: `a: {a: string, _hidden: int, _#hidden: int}, b: close({a: "foo"})`,
		pathA: a,
		pathB: b,
		want:  `{"a":"foo"}`,
	}, {
		// Issue #2325: let should not result in a closedness error.
		value: `#T: {
			...
		}
		b: {
			let foobar = {}
			 _fb: foobar
		}`,
		pathA: "#T",
		pathB: b,
		want:  `{}`,
	}, {
		value: `
		a: #A: "foo"
		#B: {...}
		`,
		pathA: a,
		pathB: "#B",
		want:  `{}`,
	}}
	// TODO(tdtest): use cuetest.Run when supported.
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		tdtest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
			v := getValue(m, tc.value)
			x := v.LookupPath(cue.ParsePath(tc.pathA))
			y := v.LookupPath(cue.ParsePath(tc.pathB))
			b, err := x.Unify(y).MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			t.Equal(string(b), tc.want)
		})
	})
}

func TestUnifyAccept(t *testing.T) {
	type testCase struct {
		value string
		want  string
	}
	testCases := []testCase{{
		value: `#v: 4, #w: 4, #accept: int`,
		want:  `4`,
	}, {
		value: `#v: string, #w: "foo", #accept: string`,
		want:  `"foo"`,
	}, {
		value: `#v: {a: "foo"}, #w: {b: 4}, #accept: {a: string, b: int}`,
		want:  `{"a":"foo","b":4}`,
	}, {
		value: `#v: [string,  4], #w: ["foo", 4], #accept: [string, int, ...]`,
		want:  `["foo",4]`,
	}, {
		value: `#v: {a: string, b: 1, _#hidden: int}, #w: {a: "foo"}, #accept: {...}`,
		want:  `{"a":"foo","b":1}`,
	}, {
		// Issue #2325: let should not result in a closedness error.
		value: `#accept: {
			...
		}
		#v: {}
		#w: {
			let foobar = {}
			 _fb: foobar
		}`,
		want: `{}`,
	}, {
		value: `
		#v: #v: "foo"
		#w: {b:1}
		#accept: {...}
		`,
		want: `{"b":1}`,
	}}
	// TODO(tdtest): use cuetest.Run when supported.
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		tdtest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
			v := getValue(m, tc.value)
			x := v.LookupPath(cue.ParsePath("#v"))
			y := v.LookupPath(cue.ParsePath("#w"))
			a := v.LookupPath(cue.ParsePath("#accept"))
			b, err := x.UnifyAccept(y, a).MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			t.Equal(string(b), tc.want)
		})
	})
}

func TestEquals(t *testing.T) {
	testCases := []struct {
		a, b string
		want bool
	}{{
		`4`, `4`, true,
	}, {
		`"str"`, `2`, false,
	}, {
		`2`, `3`, false,
	}, {
		`[1]`, `[3]`, false,
	}, {
		`[{a: 1,...}]`, `[{a: 1,...}]`, true,
	}, {
		`[]`, `[]`, true,
	}, {
		`{
			a: b,
			b: a,
		}`,
		`{
			a: b,
			b: a,
		}`,
		true,
	}, {
		`{
			a: "foo",
			b: "bar",
		}`,
		`{
			a: "foo",
		}`,
		false,
	}, {
		// Ignore closedness
		`{ #Foo: { k: 1 }, a: #Foo }`,
		`{ #Foo: { k: 1 }, a: { k: 1 } }`,
		true,
	}, {
		// Ignore optional fields
		`{ #Foo: { k: 1 }, a: #Foo }`,
		`{ #Foo: { k: 1 }, a: { #Foo, i?: 1 } }`,
		true,
	}, {
		// Treat embedding as equal
		`{ a: 2, b: { 3 } }`,
		`{ a: { 2 }, b: 3 }`,
		true,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()

			a := mustCompile(t, ctx, tc.a)
			b := mustCompile(t, ctx, tc.b)
			got := a.Equals(b)
			if got != tc.want {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}

// TODO: options: disallow cycles.
func TestValidate(t *testing.T) {
	testCases := []struct {
		desc string
		in   string
		err  bool
		opts []cue.Option

		skip bool
	}{{
		desc: "issue #51",
		in: `
		a: [string]: foo
		a: b: {}
		`,
		err: true,
	}, {
		desc: "concrete",
		in: `
		a: 1
		b: { c: 2, d: 3 }
		c: d: e: f: 5
		g?: int
		`,
		opts: []cue.Option{cue.Concrete(true)},
	}, {
		desc: "definition error",
		in: `
			#b: 1 & 2
			`,
		opts: []cue.Option{},
		err:  true,
	}, {
		desc: "definition error okay if optional",
		in: `
			#b?: 1 & 2
			`,
		opts: []cue.Option{},
	}, {
		desc: "definition with optional",
		in: `
			#b: {
				a: int
				b?: >=0
			}
		`,
		opts: []cue.Option{cue.Concrete(true)},
	}, {
		desc: "disjunction",
		in:   `a: 1 | 2`,
	}, {
		desc: "disjunction concrete",
		in:   `a: 1 | 2`,
		opts: []cue.Option{cue.Concrete(true)},
		err:  true,
	}, {
		desc: "incomplete concrete",
		in:   `a: string`,
	}, {
		desc: "incomplete",
		in:   `a: string`,
		opts: []cue.Option{cue.Concrete(true)},
		err:  true,
	}, {
		desc: "list",
		in:   `a: [{b: string}, 3]`,
	}, {
		desc: "list concrete",
		in:   `a: [{b: string}, 3]`,
		opts: []cue.Option{cue.Concrete(true)},
		err:  true,
	}, {
		desc: "allow cycles",
		in: `
			a: b - 100
			b: a + 100
			c: [c[1], c[0]]
			`,
	}, {
		desc: "disallow cycles",
		in: `
			a: b - 100
			b: a + 100
			c: [c[1], c[0]]
			`,
		opts: []cue.Option{cue.DisallowCycles(true)},
		err:  true,

		// TODO: in the new evaluator these are not considered to be cycles
		// but rather incomplete errors. This is actually the correct behavior
		// and the old evaluator treats errors like this by default.
		// The option tested in this test was added for backwards compatibility
		// when the old evaluator was made to treat these kinds of errors
		// equally. With the new evaluator we can no longer distinguish these
		// errors. For now, at least. Consider what to do with this option.
		skip: true,
	}, {
		desc: "builtins are okay",
		in: `
		import "time"

		a: { b: time.Duration } | { c: time.Duration }
		`,
	}, {
		desc: "comprehension error",
		in: `
			a: { if b == "foo" { field: 2 } }
			`,
		err: true,
	}, {
		desc: "ignore optional in schema",
		in: `
		#Schema1: {
			a?: int
		}
		instance1: #Schema1
		`,
		opts: []cue.Option{cue.Concrete(true)},
	}, {
		desc: "issue324",
		in: `
		import "encoding/yaml"

		x: string
		a: b: c: *["\(x)"] | _
		d: yaml.Marshal(a.b)
		`,
	}, {
		desc: "allow non-concrete values for definitions",
		in: `
		variables: #variables

		{[!~"^[.]"]: #job}

		#variables: [string]: int | string

		#job: ({a: int} | {b: int}) & {
			"variables"?: #variables
		}
		`,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.desc, func(t *testing.T, m *cuetdtest.M) {
			if tc.skip {
				m.TODO_V3(t)
			}

			ctx := m.CueContext()
			val := ctx.CompileString(tc.in, cue.Filename("validate"))
			err := val.Validate(tc.opts...)
			if gotErr := err != nil; gotErr != tc.err {
				t.Errorf("got %v; want %v", err, tc.err)
			}
		})
	}
}

func TestPath(t *testing.T) {
	config := `
	a: b: c: 5
	b: {
		b1: 3
		b2: 4
		"b 3": 5
		"4b": 6
		l: [
			{a: 2},
			{c: 2},
		]
	}
	`
	mkpath := func(p ...string) []string { return p }
	testCases := [][]string{
		mkpath("a", "b", "c"),
		mkpath("b", "l", "1", "c"),
		mkpath("b", `"b 3"`),
		mkpath("b", `"4b"`),
	}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, strings.Join(tc, "."), func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()
			val := mustCompile(t, ctx, config)

			v := val.Lookup(tc[0])
			for _, e := range tc[1:] {
				if '0' <= e[0] && e[0] <= '9' {
					i, err := strconv.Atoi(e)
					if err != nil {
						t.Fatal(err)
					}
					iter, err := v.List()
					if err != nil {
						t.Fatal(err)
					}
					for c := 0; iter.Next(); c++ {
						if c == i {
							v = iter.Value()
							break
						}
					}
				} else if e[0] == '"' {
					v = v.Lookup(e[1 : len(e)-1])
				} else {
					v = v.Lookup(e)
				}
			}
			got := cue.PathToStrings(v.Path())
			if !reflect.DeepEqual(got, tc) {
				t.Errorf("got %v; want %v", got, tc)
			}
		})
	}
}

func TestValueLookup(t *testing.T) {
	config := `
		a: {
			a: 0
			b: 1
			c: 2
		}
		b: {
			d: a.a
			e: int
		}
	`

	strList := func(s ...string) []string { return s }

	testCases := []struct {
		config    string
		path      []string
		str       string
		notExists bool
	}{{
		config: "_|_",
		path:   strList(""),
		str:    "explicit error (_|_ literal) in source",
	}, {
		config: "_|_",
		path:   strList("a"),
		str:    "explicit error (_|_ literal) in source",
	}, {
		config: config,
		path:   strList(),
		str:    "{a:{a:0,b:1,c:2},b:{d:0,e:int}",
	}, {
		config: config,
		path:   strList("a", "a"),
		str:    "0",
	}, {
		config: config,
		path:   strList("a"),
		str:    "{a:0,b:1,c:2}",
	}, {
		config: config,
		path:   strList("b", "d"),
		str:    "0",
	}, {
		config:    config,
		path:      strList("c", "non-existing"),
		str:       "not found",
		notExists: true,
	}, {
		config: config,
		path:   strList("b", "d", "lookup in non-struct"),
		str:    "cannot use value 0 (type int) as struct",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.str, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, tc.config).Lookup(tc.path...)
			if got := !v.Exists(); got != tc.notExists {
				t.Errorf("exists: got %v; want %v", got, tc.notExists)
			}

			got := cue.ValueCtx(v).Str(cue.ValueVertex(v))
			if tc.str == "" {
				t.Fatalf("str empty, got %q", got)
			}
			if !strings.Contains(got, tc.str) {
				t.Errorf("\n got %v\nwant %v", got, tc.str)
			}
		})
	}
}

func cmpError(a, b error) bool {
	if a == nil {
		return b == nil
	}
	if b == nil {
		return a == nil
	}
	return a.Error() == b.Error()
}

// TODO: duplicate docs.
func TestValueDoc(t *testing.T) {
	const config = `
	// foobar defines at least foo.
	package foobar

	// A Foo fooses stuff.
	Foo: {
		// field1 is an int.
		field1: int

		field2: int

		// duplicate field comment
		dup3: int
	}

	// foos are instances of Foo.
	foos: [string]: Foo

	// My first little foo.
	foos: MyFoo: {
		// local field comment.
		field1: 0

		// Dangling comment.

		// other field comment.
		field2: 1

		// duplicate field comment
		dup3: int
	}

	bar: {
		// comment from bar on field 1
		field1: int
		// comment from bar on field 2
		field2: int // don't include this
	}

	baz: bar & {
		// comment from baz on field 1
		field1: int
		field2: int
	}
	`
	config2 := `
	// Another Foo.
	Foo: {}
	`

	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		ctx := m.CueContext()
		v1 := mustCompile(t, ctx, config)
		v2 := mustCompile(t, ctx, config2)
		both := v1.Unify(v2)

		testCases := []struct {
			val  cue.Value
			path string
			doc  string
			skip bool
		}{{
			val:  v1,
			path: "foos",
			doc:  "foos are instances of Foo.\n",
		}, {
			val:  v1,
			path: "foos MyFoo",
			doc:  "My first little foo.\n",
		}, {
			val:  v1,
			path: "foos MyFoo field1",
			doc: `local field comment.

field1 is an int.
`,
		}, {
			val:  v1,
			path: "foos MyFoo field2",
			doc:  "other field comment.\n",
		}, {
			// Duplicates are now removed.
			val:  v1,
			path: "foos MyFoo dup3",
			doc:  "duplicate field comment\n",
		}, {
			val:  v1,
			path: "bar field1",
			doc:  "comment from bar on field 1\n",
		}, {
			val:  v1,
			path: "baz field1",
			doc: `comment from bar on field 1

comment from baz on field 1
`,
			// New evaluaotor orders the comments differently (arguably better).
			skip: true,
		}, {
			val:  v1,
			path: "baz field2",
			doc:  "comment from bar on field 2\n",
		}, {
			val:  v2,
			path: "Foo",
			doc: `Another Foo.
`,
		}, {
			val:  both,
			path: "Foo",
			doc: `A Foo fooses stuff.

Another Foo.
`,
		}}
		for _, tc := range testCases {
			if tc.skip {
				m.TODO_V3(t)
			}
			t.Run("field:"+tc.path, func(t *testing.T) {
				v := tc.val.Lookup(strings.Split(tc.path, " ")...)
				doc := docStr(v.Doc())
				if doc != tc.doc {
					t.Errorf("doc: got:\n%vwant:\n%v", doc, tc.doc)
				}
			})
		}
		want := "foobar defines at least foo.\n"
		if got := docStr(v1.Doc()); got != want {
			t.Errorf("pkg: got:\n%vwant:\n%v", got, want)
		}
	})
}

func docStr(docs []*ast.CommentGroup) string {
	doc := ""
	for _, d := range docs {
		if doc != "" {
			doc += "\n"
		}
		doc += d.Text()
	}
	return doc
}

// TODO: unwrap marshal error
// TODO: improve error messages
func TestMarshalJSON(t *testing.T) {
	type testCase struct {
		value string
		json  string
		err   string
	}
	testCases := []testCase{{
		value: `""`,
		json:  `""`,
	}, {
		value: `null`,
		json:  `null`,
	}, {
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		value: `(a.b)
		a: {}`,
		err: "undefined field",
	}, {
		value: `true`,
		json:  `true`,
	}, {
		value: `false`,
		json:  `false`,
	}, {
		value: `bool`,
		err:   "cannot convert incomplete value",
	}, {
		value: `"str"`,
		json:  `"str"`,
	}, {
		value: `12_000`,
		json:  `12000`,
	}, {
		value: `12.000`,
		json:  `12.000`,
	}, {
		value: `12M`,
		json:  `12000000`,
	}, {
		value: `3.0e100`,
		json:  `3.0E+100`,
	}, {
		value: `0/0`,
		err:   "division undefined",
	}, {
		value: `[]`,
		json:  `[]`,
	}, {
		value: `[1, 2, 3]`,
		json:  `[1,2,3]`,
	}, {
		value: `[int]`,
		err:   `0: cannot convert incomplete value`,
	}, {
		value: `{}`,
		json:  `{}`,
	}, {
		value: `{a: 2, b: 3, c: ["A", "B"]}`,
		json:  `{"a":2,"b":3,"c":["A","B"]}`,
	}, {
		value: `{a: 2, b: 3, c: [string, "B"]}`,
		err:   `c.0: cannot convert incomplete value`,
	}, {
		value: `{a: [{b: [0, {c: string}] }] }`,
		err:   `a.0.b.1.c: cannot convert incomplete value`,
	}, {
		value: `{foo?: 1, bar?: 2, baz: 3}`,
		json:  `{"baz":3}`,
	}, {
		value: `{foo!: 1, bar: 2}`,
		err:   "cue: marshal error: foo: field is required but not present",
	}, {
		// Has an unresolved cycle, but should not matter as all fields involved
		// are optional
		value: `{foo?: bar, bar?: foo, baz: 3}`,
		json:  `{"baz":3}`,
	}, {
		// Issue #107
		value: `a: 1.0/1`,
		json:  `{"a":1.0}`,
	}, {
		// Issue #108
		value: `
		a: int
		a: >0
		a: <2

		b: int
		b: >=0.9
		b: <1.1

		c: int
		c: >1
		c: <=2

		d: int
		d: >=1
		d: <=1.5

		e: int
		e: >=1
		e: <=1.32

		f: >=1.1 & <=1.1
		`,
		json: `{"a":1,"b":1,"c":2,"d":1,"e":1,"f":1.1}`,
	}, {
		value: `
		#Task: {
			{
				op:          "pull"
				tag:         *"latest" | string
				tagInString: tag + "dd"
			} | {
				op: "scratch"
			}
		}

		foo: #Task & {"op": "pull"}
		`,
		json: `{"foo":{"op":"pull","tag":"latest","tagInString":"latestdd"}}`,
	}, {
		// Issue #326
		value: `x: "\(string)": "v"`,
		err:   `x: invalid interpolation`,
	}, {
		// Issue #326
		value: `x: "\(bool)": "v"`,
		err:   `invalid interpolation`,
	}, {
		// Issue #326
		value: `
		x: {
			for k, v in y {
				"\(k)": v
			}
		}
		y: {}
		`,
		json: `{"x":{},"y":{}}`,
	}, {
		// Issue #326
		value: `
		x: {
			for k, v in y {
				"\(k)": v
			}
		}
		y: _
		`,
		err: `x: cannot range over y (incomplete type _)`,
	}, {
		value: `
		package foo

		#SomeBaseType: {
			"a" | "b"
			#AUTO: "z"
		}

		V1: ("x" | "y") | *"z"
		V2: ("x" | "y") | *#SomeBaseType.#AUTO
		`,
		err: "cue: marshal error: V2: cannot convert incomplete value \"|((string){ \\\"x\\\" }, (string){ \\\"y\\\" })\" to JSON",
	}}
	for i, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, fmt.Sprintf("%d/%v", i, tc.value), func(t *testing.T, m *cuetdtest.M) {
			m.TODO_V3(t)

			val := getValue(m, tc.value)
			b, err := val.MarshalJSON()
			checkFatal(t, err, tc.err, "init")

			if got := string(b); got != tc.json {
				t.Errorf("\n got %v;\nwant %v", got, tc.json)
			}
		})
	}
}

func TestWalk(t *testing.T) {
	testCases := []struct {
		value string
		out   string
	}{{
		value: `""`,
		out:   `""`,
	}, {
		value: `null`,
		out:   `null`,
	}, {
		value: `_|_`,
		out:   "_|_(explicit error (_|_ literal) in source)",
	}, {
		value: `(a.b)
			a: {}`,
		out: `_|_(undefined field: b)`,
	}, {
		value: `true`,
		out:   `true`,
	}, {
		value: `false`,
		out:   `false`,
	}, {
		value: `bool`,
		out:   "bool",
	}, {
		value: `"str"`,
		out:   `"str"`,
	}, {
		value: `12_000`,
		out:   `12000`,
		// out:   `12_000`,
	}, {
		value: `12.000`,
		out:   `12.000`,
	}, {
		value: `12M`,
		out:   `12000000`,
		// out:   `12M`,
	}, {
		value: `3.0e100`,
		out:   `3.0e+100`,
		// out:   `3.0e100`,
	}, {
		value: `[]`,
		out:   `[]`,
	}, {
		value: `[1, 2, 3]`,
		out:   `[1,2,3]`,
	}, {
		value: `[int]`,
		out:   `[int]`,
	}, {
		value: `3 * [1, 2]`,
		out:   `_|_(Multiplication of lists is superseded by list.Repeat; see https://cuelang.org/e/v0.11-list-arithmetic)`,
	}, {
		value: `[7, 8] + [1, 2]`,
		out:   `_|_(Addition of lists is superseded by list.Concat; see https://cuelang.org/e/v0.11-list-arithmetic)`,
	}, {
		value: `{}`,
		out:   `{}`,
	}, {
		value: `{a: 2, b: 3, c: ["A", "B"]}`,
		out:   `{a:2,b:3,c:["A","B"]}`,
	}}
	for i, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, fmt.Sprintf("%d/%v", i, tc.value), func(t *testing.T, m *cuetdtest.M) {
			m.TODO_V3(t)

			val := getValue(m, tc.value)
			buf := []byte{}
			stripComma := func() {
				if n := len(buf) - 1; buf[n] == ',' {
					buf = buf[:n]
				}
			}
			val.Walk(func(v cue.Value) bool {
				v = v.Eval()
				if !cue.ValueVertex(v).Label.IsInt() {
					if k, ok := v.Label(); ok {
						buf = append(buf, k+":"...)
					}
				}
				switch v.Kind() {
				case cue.StructKind:
					buf = append(buf, '{')
				case cue.ListKind:
					buf = append(buf, '[')
				default:
					if b := cue.ValueVertex(v).Bottom(); b != nil {
						s := cue.DebugStr(cue.ValueCtx(v), b)
						buf = append(buf, fmt.Sprint(s, ",")...)
						return true
					}
					buf = append(buf, fmt.Sprint(v, ",")...)
				}
				return true
			}, func(v cue.Value) {
				switch v.Kind() {
				case cue.StructKind:
					stripComma()
					buf = append(buf, "},"...)
				case cue.ListKind:
					stripComma()
					buf = append(buf, "],"...)
				}
			})
			stripComma()
			if got := string(buf); got != tc.out {
				t.Errorf("\n got %v;\nwant %v", got, tc.out)
			}
		})
	}
}

func TestReferencePath(t *testing.T) {
	testCases := []struct {
		input          string
		want           string
		wantImportPath string
		alt            string
	}{{
		input: "v: w: x: _|_",
		want:  "",
	}, {
		input: "v: w: x: 2",
		want:  "",
	}, {
		input: "v: w: x: a, a: 1",
		want:  "a",
	}, {
		input: "v: w: x: a.b.c, a: b: c: 1",
		want:  "a.b.c",
	}, {
		input: "if true { v: w: x: a, a: 1 }",
		want:  "a",
	}, {
		input: "v: w: x: w.a.b.c, v: w: a: b: c: 1",
		want:  "v.w.a.b.c",
	}, {
		input: `v: w: x: w.a.b.c, v: w: a: b: c: 1, #D: 3, opt?: 3, "v\(#D)": 3, X: {a: 3}, X`,
		want:  "v.w.a.b.c",
	}, {
		input: `
		v: w: x: w.a[bb]["c"]
		v: w: a: b: c: 1
		bb: "b"`,
		want: "v.w.a.b.c",
	}, {
		input: `
		X="\(y)": 1
		v: w: x: X // TODO: Move up for crash
		y: "foo"`,
		want: "foo",
	}, {
		input: `
		v: w: _
		v: [X=string]: x: a[X]
		a: w: 1`,
		want: "a.w",
	}, {
		input: `v: {
			for t in src {
				w: "t\(t)": 1
				w: "\(t)": w["t\(t)"]
			}
		},
		src: ["x", "y"]`,
		want: "v.w.tx",
	}, {
		input: `
		v: w: x: a
		a: 1
		for i in [] {
		}
		`,
		want: "a",
	}, {
		input: `
		v: w: close({x: a})
		a: 1
		`,
		want: "a",
	}, {
		input: `
		import "math"

		v: w: x: math.Pi
		`,
		want:           "Pi",
		wantImportPath: "math",
		alt:            "3.14159265358979323846264338327950288419716939937510582097494459",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()

			val := ctx.CompileString(tc.input, cue.Filename("in"))
			v := val.Lookup("v", "w", "x")

			root, path := v.ReferencePath()
			if got := path.String(); got != tc.want {
				t.Errorf("\n got %v;\nwant %v", got, tc.want)
			}
			if tc.want != "" {
				want := "1"
				if tc.alt != "" {
					want = tc.alt
				}
				v := fmt.Sprint(root.LookupPath(path))
				if v != want {
					t.Errorf("path resolved to %s; want %s", v, want)
				}
				buildInst := root.BuildInstance()
				if buildInst == nil {
					t.Fatalf("no build instance found for reference path root")
				}
				if got, want := buildInst.ImportPath, tc.wantImportPath; got != want {
					t.Errorf("unexpected import path; got %q want %q", got, want)
				}
			}

			inst, a := v.Reference()
			if got := strings.Join(a, "."); got != tc.want {
				t.Errorf("\n got %v;\nwant %v", got, tc.want)
			}

			if tc.want != "" {
				want := "1"
				if tc.alt != "" {
					want = tc.alt
				}
				v := fmt.Sprint(inst.Lookup(a...))
				if v != want {
					t.Errorf("path resolved to %s; want %s", v, want)
				}
			}
		})
	}
}

func TestZeroValueBuildInstance(t *testing.T) {
	inst := cue.Value{}.BuildInstance()
	if inst != nil {
		t.Error("unexpected non-nil instance")
	}
}

func TestPos(t *testing.T) {
	testCases := []struct {
		value string
		pos   string
		skip  bool
	}{{
		value: `
a: string
a: "foo"`,
		pos: "3:4",
	}, {
		value: `
a: x: string
a: x: "x"`,
		pos: "2:4",

		// the position of the new evaluator is also correct, and actually
		// better.
		skip: true,
	}, {
		// Prefer struct conjuncts with actual fields.
		value: `
a: [string]: string
a: x: "x"`,
		pos: "3:4",
	}, {
		value: `
a: [string]: [string]: string
a: x: y: "x"`,
		pos: "3:4",
	}, {
		value: `
a: [string]: [string]: [string]: string
a: x: y: z: "x"`,
		pos: "3:4",
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			if tc.skip {
				m.TODO_V3(t)
			}

			c := m.CueContext()
			v := c.CompileString(tc.value)
			v = v.LookupPath(cue.ParsePath("a"))
			pos := v.Pos().String()
			if pos != tc.pos {
				t.Errorf("got %v; want %v", pos, tc.pos)
			}
		})
	}
}

func TestPathCorrection(t *testing.T) {
	testCases := []struct {
		input  string
		lookup func(i cue.Value) cue.Value
		want   string
		skip   bool
	}{{
		input: `
			a: b: {
				c: d: b
			}
			`,
		lookup: func(i cue.Value) cue.Value {
			op, a := i.Lookup("a", "b", "c", "d").Expr()
			_ = op
			return a[0] // structural cycle errors.
		},
		want: "a",
	}, {

		// TODO: embedding: have field operators.
		input: `
			a: {
				{x: c}
				c: 3
			}
			`,
		lookup: func(i cue.Value) cue.Value {
			op, a := i.Lookup("a").Expr()
			_ = op
			return a[0].Lookup("x")
		},
		want: "a.c",
	}, {

		// TODO: implement proper Elem()
		input: `
			a: b: [...T]
			a: b: [...T]
			T: int
			`,
		lookup: func(i cue.Value) cue.Value {
			v, _ := i.Lookup("a", "b").Elem()
			_, a := v.Expr()
			return a[0]
		},
		want: "T",
	}, {
		input: `
				#S: {
					b?: [...#T]
					b?: [...#T]
				}
				#T: int
				`,
		lookup: func(i cue.Value) cue.Value {
			v := i.LookupDef("#S")
			f, _ := v.LookupField("b")
			v, _ = f.Value.Elem()
			_, a := v.Expr()
			return a[0]
		},
		want: "#T",
	}, {
		input: `
		#S: {
			a?: [...#T]
			b?: [...#T]
		}
		#T: int
		`,
		lookup: func(i cue.Value) cue.Value {
			v := i.LookupDef("#S")
			f, _ := v.LookupField("a")
			x := f.Value
			f, _ = v.LookupField("b")
			y := f.Value
			u := x.Unify(y)
			v, _ = u.Elem()
			_, a := v.Expr()
			return a[0]
		},
		want: "#T",
	}, {
		input: `
		#a: {
			close({}) | close({c: #T}) | close({d: string})
			#T: {b: 3}
		}
		`,
		lookup: func(i cue.Value) cue.Value {
			f, _ := i.LookupField("#a")
			_, a := f.Value.Expr() // &
			_, a = a[0].Expr()     // |
			return a[1].Lookup("c")
		},
		want: "#a.#T",
	}, {
		input: `
		package foo

		#Struct: {
			#T: int

			{b?: #T}
		}`,
		want: "#Struct.#T",
		lookup: func(val cue.Value) cue.Value {
			// Locate Struct
			i, _ := val.Fields(cue.Definitions(true))
			if !i.Next() {
				t.Fatal("no fields")
			}
			// Locate b
			i, _ = i.Value().Fields(cue.Definitions(true), cue.Optional(true))
			if !(i.Next() && i.Next()) {
				t.Fatal("no fields")
			}
			v := i.Value()
			return v
		},
	}, {

		input: `
		package foo

		#A: #B: #T

		#T: {
			a: #S.#U
			#S: #U: {}
		}
		`,
		want: "#T.#S.#U",
		lookup: func(val cue.Value) cue.Value {
			f, _ := val.LookupField("#A")
			f, _ = f.Value.LookupField("#B")
			v := f.Value
			v = cue.Dereference(v)
			v = v.Lookup("a")
			return v
		},
	}, {

		// TODO: record additionalItems in list
		input: `
			package foo

			#A: #B: #T

			#T: {
				a: [...#S]
				#S: {}
			}
			`,
		want: "#T.#S",
		lookup: func(val cue.Value) cue.Value {
			f, _ := val.LookupField("#A")
			f, _ = f.Value.LookupField("#B")
			v := f.Value
			v = cue.Dereference(v)
			v, _ = v.Lookup("a").Elem()
			return v
		},
	}, {
		input: `
		#A: {
			b: #T
		}

		#T: {
			a: #S
			#S: {}
		}
		`,
		want: "#T.#S",
		lookup: func(val cue.Value) cue.Value {
			f, _ := val.LookupField("#A")
			v := f.Value.Lookup("b")
			v = cue.Dereference(v)
			v = v.Lookup("a")
			return v
		},
	}, {
		input: `
			#Tracing: {
				#T: { address?: string }
				#S: { ip?: string }

				close({}) | close({
					t: #T
				}) | close({
					s: #S
				})
			}
			#X: {}
			#X // Disconnect top-level struct from the one visible by close.
			`,
		want: "#Tracing.#T",
		lookup: func(val cue.Value) cue.Value {
			f, _ := val.LookupField("#Tracing")
			v := f.Value.Eval()
			_, args := v.Expr()
			v = args[1]
			v = v.Lookup("t")
			return v
		},
	}, {
		input: `
		x: { if true { v: a } }
		a: b
		b: 2
		`,
		want: "b",
		lookup: func(val cue.Value) cue.Value {
			v := val.LookupPath(cue.ParsePath("x.v"))
			v = cue.Dereference(v)
			return v
		},
	}, {
		input: `
		package foo

		#A:{ if true { #B: #T } }

		#T: {
			a: #S.#U
			#S: #U: {}
		}
		`,
		want: "#T.#S.#U",
		lookup: func(val cue.Value) cue.Value {
			f, _ := val.LookupField("#A")
			f, _ = f.Value.LookupField("#B")
			v := f.Value
			v = cue.Dereference(v)
			v = v.Lookup("a")
			return v
		},
	}}
	for _, tc := range testCases {
		if tc.skip {
			continue
		}
		cuetdtest.FullMatrix.Run(t, "", func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()

			val := ctx.CompileString(tc.input, cue.Filename("in"))
			v := tc.lookup(val)
			gotVal, ref := v.ReferencePath()
			if gotVal != val {
				t.Error("reference not in original instance")
			}
			gotPath := strings.Join(cue.PathToStrings(ref), ".")
			if gotPath != tc.want {
				t.Errorf("got path %s; want %s", gotPath, tc.want)
			}

			x, p := v.ReferencePath()
			if x != val {
				t.Error("reference not in original instance")
			}
			gotPath = p.String()
			if gotPath != tc.want {
				t.Errorf("got path %s; want %s", gotPath, tc.want)
			}

		})
	}
}

// func TestReferences(t *testing.T) {
// 	config1 := `
// 	a: {
// 		b: 3
// 	}
// 	c: {
// 		d: a.b
// 		e: c.d
// 		f: a
// 	}
// 	`
// 	config2 := `
// 	a: { c: 3 }
// 	b: { c: int, d: 4 }
// 	r: (a & b).c
// 	c: {args: s1 + s2}.args
// 	s1: string
// 	s2: string
// 	d: ({arg: b}).arg.c
// 	e: f.arg.c
// 	f: {arg: b}
// 	`
// 	testCases := []struct {
// 		config string
// 		in     string
// 		out    string
// 	}{
// 		{config1, "c.d", "a.b"},
// 		{config1, "c.e", "c.d"},
// 		{config1, "c.f", "a"},

// 		{config2, "r", "a.c b.c"},
// 		{config2, "c", "s1 s2"},
// 		// {config2, "d", "b.c"}, // TODO: make this work as well.
// 		{config2, "e", "f.arg.c"}, // TODO: should also report b.c.
// 	}
// 	for _, tc := range testCases {
// 		t.Run(tc.in, func(t *testing.T) {
// 			ctx, st := compileFile(t, tc.config)
// 			v := newValueRoot(ctx, st)
// 			for _, k := range strings.Split(tc.in, ".") {
// 				obj, err := v.structValFull(ctx)
// 				if err != nil {
// 					t.Fatal(err)
// 				}
// 				v = obj.Lookup(k)
// 			}
// 			got := []string{}
// 			for _, r := range v.References() {
// 				got = append(got, strings.Join(r, "."))
// 			}
// 			want := strings.Split(tc.out, " ")
// 			if !reflect.DeepEqual(got, want) {
// 				t.Errorf("got %v; want %v", got, want)
// 			}
// 		})
// 	}
// }

func checkErr(t *testing.T, err error, str, name string) bool {
	t.Helper()
	if err == nil {
		if str != "" {
			t.Errorf(`err:%s: got ""; want %q`, name, str)
		}
		return true
	}
	return checkFailed(t, err, str, name)
}

func checkFatal(t *testing.T, err error, str, name string) {
	t.Helper()
	if !checkFailed(t, err, str, name) {
		t.SkipNow()
	}
}

func checkFailed(t *testing.T, err error, str, name string) bool {
	t.Helper()
	if err != nil {
		got := err.Error()
		if str == "" {
			t.Fatalf(`err:%s: got %q; want ""`, name, got)
		}
		if !strings.Contains(got, str) {
			t.Errorf(`err:%s: got %q; want %q`, name, got, str)
		}
		return false
	}
	return true
}

func TestExpr(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{{
		input: "v: 3",
		want:  "3",
	}, {
		input: "v: 3 + 4",
		want:  "+(3 4)",
	}, {
		input: "v: !a, a: bool",
		want:  `!(.(〈〉 "a"))`,
	}, {
		input: "v: !a, a: 3", // TODO: Should still look up.
		want:  `!(.(〈〉 "a"))`,
	}, {
		input: "v: 1 | 2 | 3 | *4",
		want:  "|(1 2 3 4)",
	}, {
		input: "v: 2 & 5", // Allow even with error.
		want:  "&(2 5)",
	}, {
		input: "v: 2 | 5",
		want:  "|(2 5)",
	}, {
		input: "v: 2 && 5",
		want:  "&&(2 5)",
	}, {
		input: "v: 2 || 5",
		want:  "||(2 5)",
	}, {
		input: "v: 2 == 5",
		want:  "==(2 5)",
	}, {
		input: "v: !b, b: true",
		want:  `!(.(〈〉 "b"))`,
	}, {
		input: "v: 2 != 5",
		want:  "!=(2 5)",
	}, {
		input: "v: <5",
		want:  "<(5)",
	}, {
		input: "v: 2 <= 5",
		want:  "<=(2 5)",
	}, {
		input: "v: 2 > 5",
		want:  ">(2 5)",
	}, {
		input: "v: 2 >= 5",
		want:  ">=(2 5)",
	}, {
		input: "v: 2 =~ 5",
		want:  "=~(2 5)",
	}, {
		input: "v: 2 !~ 5",
		want:  "!~(2 5)",
	}, {
		input: "v: 2 + 5",
		want:  "+(2 5)",
	}, {
		input: "v: 2 - 5",
		want:  "-(2 5)",
	}, {
		input: "v: 2 * 5",
		want:  "*(2 5)",
	}, {
		input: "v: 2 / 5",
		want:  "/(2 5)",
	}, {
		input: "v: 2 quo 5",
		want:  "quo(2 5)",
	}, {
		input: "v: 2 rem 5",
		want:  "rem(2 5)",
	}, {
		input: "v: 2 div 5",
		want:  "div(2 5)",
	}, {
		input: "v: 2 mod 5",
		want:  "mod(2 5)",
	}, {
		input: "v: a.b, a: b: 4",
		want:  `.(.(〈〉 "a") "b")`,
	}, {
		input: `v: a["b"], a: b: 3 `,
		want:  `[](.(〈〉 "a") "b")`,
	}, {
		input: "v: a[2:5], a: [1, 2, 3, 4, 5]",
		want:  `[:](.(〈〉 "a") 2 5)`,
	}, {
		input: "v: len([])",
		want:  "()(len [])",
	}, {
		input: "v: a.b, a: { b: string }",
		want:  `.(.(〈〉 "a") "b")`,
	}, {
		input: `v: "Hello, \(x)! Welcome to \(place)", place: string, x: string`,
		want:  `\()("Hello, " .(〈〉 "x") "! Welcome to " .(〈〉 "place") "")`,
	}, {
		// Split out the reference, but ensure the split-off outer struct
		// remains valid.
		input: `v: { a, #b: 1 }, a: 2`,
		want:  `&(.(〈〉 "a") {int,#b:1})`,
	}, {
		// Result is an error, no need to split off.
		input: `v: { a, b: 1 }, a: 2`,
		want:  `&(.(〈〉 "a") {b:1})`,
	}, {
		// Don't split of concrete values.
		input: `v: { "foo", #def: 1 }`,
		want:  `{"foo",#def:1}`,
	}, {
		input: `v: { {} | { a: #A, b: #B}, #A: {} | { c: int} }, #B: int | bool`,
		want:  `&(|({} {a:#A,b:#B}) {#A:({}|{c:int})})`,
	}, {
		input: `v: { {c: a}, b: a }, a: int`,
		want:  `&({c:a} {b:a})`,
	}, {
		input: `v: [...number] | *[1, 2, 3]`,
		// Filter defaults that are subsumed by another value.
		want: `[...number]`,
	}, {
		input: `v: or([1, 2, 3])`,
		want:  `|(1 2 3)`,
	}, {
		input: `v: or([])`,
		want:  `_|_(empty list in call to or)`,
	}, {
		input: `v: and([1, 2, 3])`,
		want:  `&(1 2 3)`,
	}, {
		input: `v: and([])`,
		want:  `_`,
	}, {
		//Issue #1245
		input: `
				x: *4 | int
				v: x | *7
				`,
		want: `|(.(〈〉 "x") 7)`,
	}, {
		// Issue #1119
		// Unwrap single embedded values.
		input: `v: {>30}`,
		want:  `>(30)`,
	}, {
		input: `v: {>30, <40}`,
		want:  `&(>(30) <(40))`,
	}, {
		input: `a: string, if true { v: a }`,
		want:  `.(〈〉 "a")`,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.input, func(t *testing.T, m *cuetdtest.M) {
			v := getValue(m, tc.input).Lookup("v")
			got := exprStr(v)
			if got != tc.want {
				t.Errorf("\n got %v;\nwant %v", got, tc.want)
			}
		})
	}
}

func exprStr(v cue.Value) string {
	op, operands := v.Expr()
	if op == cue.NoOp {
		return compactRawStr(operands[0])
	}
	s := op.String()
	s += "("
	for i, v := range operands {
		if i > 0 {
			s += " "
		}
		s += exprStr(v)
	}
	s += ")"
	return s
}

func compactRawStr(v cue.Value) string {
	ctx := cue.ValueCtx(v)
	cfg := &debug.Config{Compact: true, Raw: true}
	return debug.NodeString(ctx, cue.ValueVertex(v), cfg)
}
