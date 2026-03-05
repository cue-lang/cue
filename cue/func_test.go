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
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	qt "github.com/go-quicktest/qt"
)

func TestPureFunc1(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.PureFunc1(func(x int) (int, error) {
		return x + 1, nil
	}))
	qt.Assert(t, qt.Equals(fmt.Sprint(v.LookupPath(cue.ParsePath("x"))), "4"))
}

func TestPureFunc1String(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#greet: _, x: #greet("world")`)
	v = v.FillPath(cue.ParsePath("#greet"), cue.PureFunc1(func(s string) (string, error) {
		return "hello, " + s, nil
	}))
	qt.Assert(t, qt.Equals(fmt.Sprint(v.LookupPath(cue.ParsePath("x"))), `"hello, world"`))
}

func TestPureFunc1Error(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.PureFunc1(func(x int) (int, error) {
		return 0, errors.New("something went wrong")
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*something went wrong.*`))
}

func TestPureFunc1Name(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.PureFunc1(func(x int) (int, error) {
		return 0, errors.New("bad value")
	}, cue.Name("myFunc")))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*bad value.*`))
}

func TestPureFunc2(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#add: _, x: #add(3, 4)")
	v = v.FillPath(cue.ParsePath("#add"), cue.PureFunc2(func(a, b int) (int, error) {
		return a + b, nil
	}))
	qt.Assert(t, qt.Equals(fmt.Sprint(v.LookupPath(cue.ParsePath("x"))), "7"))
}

func TestPureFuncExportError(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#f: _")
	v = v.FillPath(cue.ParsePath("#f"), cue.PureFunc1(func(x int) (int, error) {
		return x + 1, nil
	}, cue.Name("myFunc")))
	got := v.LookupPath(cue.ParsePath("#f"))
	qt.Assert(t, qt.Matches(fmt.Sprint(got), `.*cannot convert function "myFunc" to CUE.*`))
}

func TestPureFunc2WrongArgCount(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#add: _, x: #add(3)")
	v = v.FillPath(cue.ParsePath("#add"), cue.PureFunc2(func(a, b int) (int, error) {
		return a + b, nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*expected 2 argument\(s\).*`))
}

func TestValidatorFunc(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & "hello"`)
	v = v.FillPath(cue.ParsePath("#v"), cue.ValidatorFunc(func(s string) error {
		if len(s) < 3 {
			return fmt.Errorf("string too short")
		}
		return nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(got.Err()))
	qt.Assert(t, qt.Equals(fmt.Sprint(got), `"hello"`))
}

func TestValidatorFuncError(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & "hi"`)
	v = v.FillPath(cue.ParsePath("#v"), cue.ValidatorFunc(func(s string) error {
		if len(s) < 3 {
			return fmt.Errorf("string too short")
		}
		return nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*string too short.*`))
}

func TestValidatorFuncTypeMismatch(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & 42`)
	v = v.FillPath(cue.ParsePath("#v"), cue.ValidatorFunc(func(s string) error {
		return nil
	}))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*decoding value for validator.*`))
}

func TestValidatorFuncName(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`#v: _, x: #v & "hi"`)
	v = v.FillPath(cue.ParsePath("#v"), cue.ValidatorFunc(func(s string) error {
		return fmt.Errorf("bad value")
	}, cue.Name("myValidator")))
	got := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(got.Err()))
	qt.Assert(t, qt.ErrorMatches(got.Err(), `.*bad value.*`))
}

func TestValidatorFuncExportError(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString("#v: _")
	v = v.FillPath(cue.ParsePath("#v"), cue.ValidatorFunc(func(s string) error {
		return nil
	}, cue.Name("myValidator")))
	got := v.LookupPath(cue.ParsePath("#v"))
	qt.Assert(t, qt.Matches(fmt.Sprint(got), `.*cannot convert validator "myValidator" to CUE.*`))
}
