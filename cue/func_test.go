// Copyright 2025 The CUE Authors
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
	"errors"
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	qt "github.com/go-quicktest/qt"
)

func TestNewPureFunc1(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewPureFunc1(func(x int) (int, error) {
		return x + 1, nil
	}))
	qt.Assert(t, qt.Equals(fmt.Sprint(v.LookupPath(cue.ParsePath("x"))), "4"))
}

func TestNewPureFunc1String(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#greet: _, x: #greet("world")`)
	v = v.FillPath(cue.ParsePath("#greet"), cue.NewPureFunc1(func(s string) (string, error) {
		return "hello, " + s, nil
	}))
	qt.Assert(t, qt.Equals(fmt.Sprint(v.LookupPath(cue.ParsePath("x"))), `"hello, world"`))
}

func TestNewPureFunc1Error(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewPureFunc1(func(x int) (int, error) {
		return 0, errors.New("something went wrong")
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*something went wrong.*`))
}

func TestNewPureFunc1Name(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewPureFunc1(func(x int) (int, error) {
		return 0, errors.New("bad value")
	}, cue.Name("myFunc")))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*bad value.*`))
}

func TestNewPureFunc2(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#add: _, x: #add(3, 4)")
	v = v.FillPath(cue.ParsePath("#add"), cue.NewPureFunc2(func(a, b int) (int, error) {
		return a + b, nil
	}))
	qt.Assert(t, qt.Equals(fmt.Sprint(v.LookupPath(cue.ParsePath("x"))), "7"))
}

func TestNewPureFuncExportError(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewPureFunc1(func(x int) (int, error) {
		return x + 1, nil
	}, cue.Name("myFunc")))
	got := v.LookupPath(cue.ParsePath("#f"))
	qt.Assert(t, qt.Matches(fmt.Sprint(got), `.*cannot convert function "myFunc" to CUE.*`))
}

func TestNewPureFunc2WrongArgCount(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#add: _, x: #add(3)")
	v = v.FillPath(cue.ParsePath("#add"), cue.NewPureFunc2(func(a, b int) (int, error) {
		return a + b, nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*expected 2 argument\(s\).*`))
}

func TestNewPureValidatorFunc(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & "hello"`)
	v = v.FillPath(cue.ParsePath("#v"), cue.NewPureValidatorFunc(func(s string) error {
		if len(s) < 3 {
			return fmt.Errorf("string too short")
		}
		return nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(got.Err()))
	qt.Assert(t, qt.Equals(fmt.Sprint(got), `"hello"`))
}

func TestNewPureValidatorFuncError(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & "hi"`)
	v = v.FillPath(cue.ParsePath("#v"), cue.NewPureValidatorFunc(func(s string) error {
		if len(s) < 3 {
			return fmt.Errorf("string too short")
		}
		return nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*string too short.*`))
}

func TestNewPureValidatorFuncTypeMismatch(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & 42`)
	v = v.FillPath(cue.ParsePath("#v"), cue.NewPureValidatorFunc(func(s string) error {
		return nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*decoding value for validator.*`))
}

func TestNewPureValidatorFuncName(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & "hi"`)
	v = v.FillPath(cue.ParsePath("#v"), cue.NewPureValidatorFunc(func(s string) error {
		return fmt.Errorf("bad value")
	}, cue.Name("myValidator")))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*bad value.*`))
}

func TestNewPureValidatorFuncExportError(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#v: _")
	v = v.FillPath(cue.ParsePath("#v"), cue.NewPureValidatorFunc(func(s string) error {
		return nil
	}, cue.Name("myValidator")))
	got := v.LookupPath(cue.ParsePath("#v"))
	qt.Assert(t, qt.Matches(fmt.Sprint(got), `.*cannot convert validator "myValidator" to CUE.*`))
}

// joinTag is a tag function for a tagged string interpolation that
// reassembles the literal fragments and string operands, wrapping each
// operand in angle brackets so the split between fragments and operands
// is observable.
var joinTag = cue.NewPureFunc2(func(strs, vals []string) (string, error) {
	var sb strings.Builder
	for i, s := range strs {
		sb.WriteString(s)
		if i < len(vals) {
			sb.WriteString("<" + vals[i] + ">")
		}
	}
	return sb.String(), nil
})

func TestTaggedInterpolation(t *testing.T) {
	testCases := []struct {
		name    string
		src     string
		tag     cue.Value
		want    string
		wantErr string
	}{{
		name: "string operands",
		src:  `#tag: _, x: #tag "a\("one")b\("two")c"`,
		tag:  joinTag,
		want: `"a<one>b<two>c"`,
	}, {
		name: "single operand",
		src:  `#tag: _, x: #tag "hello \("world")"`,
		tag:  joinTag,
		want: `"hello <world>"`,
	}, {
		// Operands keep their original CUE type rather than being
		// coerced to string, so the tag function receives them as ints.
		name: "int operands",
		src:  `#tag: _, x: #tag "a\(1)b\(2)c"`,
		tag: cue.NewPureFunc2(func(strs []string, vals []int) (string, error) {
			var sb strings.Builder
			for i, s := range strs {
				sb.WriteString(s)
				if i < len(vals) {
					sb.WriteString(fmt.Sprintf("<%d>", vals[i]))
				}
			}
			return sb.String(), nil
		}),
		want: `"a<1>b<2>c"`,
	}, {
		// A tagged literal with no interpolations still invokes the tag,
		// which receives a single fragment and no operands.
		name: "no interpolations",
		src:  `#tag: _, x: #tag "plain"`,
		tag: cue.NewPureFunc2(func(strs, vals []string) (string, error) {
			return fmt.Sprintf("%d fragment(s) %d operand(s): %q", len(strs), len(vals), strs), nil
		}),
		want: `"1 fragment(s) 0 operand(s): [\"plain\"]"`,
	}, {
		name: "error from tag",
		src:  `#tag: _, x: #tag "a\("b")c"`,
		tag: cue.NewPureFunc2(func(strs, vals []string) (string, error) {
			return "", errors.New("tag failed")
		}),
		wantErr: `.*tag failed.*`,
	}, {
		// An abstract operand cannot be passed to the tag function.
		name:    "abstract operand",
		src:     `#tag: _, y: string, x: #tag "a\(y)b"`,
		tag:     joinTag,
		wantErr: `.*cannot convert non-concrete value string.*`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()
			v := ctx.CompileString(tc.src)
			v = v.FillPath(cue.ParsePath("#tag"), tc.tag)
			got := v.LookupPath(cue.ParsePath("x"))
			if tc.wantErr != "" {
				qt.Assert(t, qt.IsNotNil(got.Err()))
				qt.Assert(t, qt.ErrorMatches(got.Err(), tc.wantErr))
				return
			}
			qt.Assert(t, qt.IsNil(got.Err()))
			qt.Assert(t, qt.Equals(fmt.Sprint(got), tc.want))
		})
	}
}
