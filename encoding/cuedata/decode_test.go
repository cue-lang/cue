package cuedata

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
)

// Auxiliary types for struct-related tests.
type simpleStruct struct {
	A int    `json:"a"`
	B string `json:"b"`
}

type EmbedBase struct {
	X int `json:"x"`
}

type embedStruct struct {
	EmbedBase
	Y string `json:"y"`
}

var decodeTests = []struct {
	testName string
	syntax   string
	into     any
	wantErr  error // unused but kept for completeness
}{
	{
		testName: "SimpleBool",
		syntax:   `true`,
		into:     new(bool),
	},
	{
		testName: "SimpleString",
		syntax:   `"hello"`,
		into:     new(string),
	},
	{
		testName: "SimpleInt",
		syntax:   `42`,
		into:     new(int),
	},
	{
		testName: "SimpleFloat",
		syntax:   `3.14`,
		into:     new(float64),
	},
	{
		testName: "BigInt",
		syntax:   `18446744073709551617`, // > 2^64 to ensure *big.Int path.
		into:     new(*big.Int),
	},
	// See https://cuelang.org/issue/3927.
	//	{
	//		testName: "BigFloat",
	//		syntax:   `1e100`,
	//		into:     new(*big.Float),
	//	},
	{
		testName: "ByteSlice",
		syntax:   `'\xff\x01'`,
		into:     new([]byte),
	},
	{
		testName: "IntSlice",
		syntax:   `[1, 2, 3]`,
		into:     new([]int),
	},
	{
		testName: "IntArray",
		syntax:   `[1, 2, 3]`,
		into:     new([3]int),
	},
	{
		testName: "MapStringInt",
		syntax:   `{a: 1, b: 2}`,
		into:     new(map[string]int),
	},
	{
		testName: "Struct",
		syntax:   `{a: 1, b: "x"}`,
		into:     new(simpleStruct),
	},
	{
		testName: "EmbeddedStruct",
		syntax:   `{x: 1, y: "y"}`,
		into:     new(embedStruct),
	},
	{
		testName: "NullSlice",
		syntax:   `null`,
		into:     new([]int),
	},
	{
		testName: "NullInterface",
		syntax:   `null`,
		into:     new(interface{}),
	},
	{
		testName: "TypeMismatchError",
		syntax:   `"foo"`, // cannot decode string into *int
		into:     new(int),
	},
	{
		testName: "NoncreteValue",
		syntax:   `int`,
		into:     new(int),
	},
}

func TestDecode(t *testing.T) {
	ctx := cuecontext.New()
	for _, test := range decodeTests {
		t.Run(test.testName, func(t *testing.T) {
			got := reflect.New(reflect.TypeOf(test.into).Elem())
			expr, err := parser.ParseExpr("", test.syntax)
			qt.Assert(t, qt.IsNil(err))
			gotErr := Decode(expr, got.Interface())

			want := reflect.New(reflect.TypeOf(test.into).Elem())
			v := ctx.BuildExpr(expr)
			qt.Assert(t, qt.IsNil(v.Err()))
			wantErr := v.Decode(want.Interface())
			if wantErr != nil {
				t.Logf("got error %v", gotErr)
				t.Logf("want error %v", wantErr)
				qt.Assert(t, qt.Not(qt.IsNil(gotErr)), qt.Commentf("CUE value error: %v", wantErr))
				return
			}
			qt.Assert(t, qt.IsNil(gotErr), qt.Commentf("got %#v", want.Elem()))
			qt.Assert(t, qt.CmpEquals(
				got.Elem().Interface(),
				want.Elem().Interface(),
				cmp.Comparer(cmp2eq((*big.Int).Cmp)),
				cmp.Comparer(cmp2eq((*big.Float).Cmp)),
			))
		})
	}
}

func cmp2eq[T any](cmp func(T, T) int) func(T, T) bool {
	return func(a, b T) bool {
		return cmp(a, b) == 0
	}
}
