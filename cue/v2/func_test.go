// Copyright 2026 The CUE Authors
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
	"context"
	"errors"
	"fmt"
	"testing"

	cue "cuelang.org/go/cue/v2"
	"github.com/go-quicktest/qt"
)

func TestNewFunc1(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc1(func(ctx context.Context, x int) (int, error) {
		return x + 1, nil
	}))
	i, err := v.LookupPath(cue.ParsePath("x")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 4))
}

func TestNewFunc2(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#add: _, x: #add(3, 4)")
	v = v.FillPath(cue.ParsePath("#add"), cue.NewFunc2(func(ctx context.Context, a, b int) (int, error) {
		return a + b, nil
	}))
	i, err := v.LookupPath(cue.ParsePath("x")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 7))
}

func TestNewFunc3(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `#f: _, x: #f("a", "b", "c")`)
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc3(func(ctx context.Context, a, b, c string) (string, error) {
		return a + b + c, nil
	}))
	s, err := v.LookupPath(cue.ParsePath("x")).AsString(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(s, "abc"))
}

func TestNewFuncError(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc1(func(ctx context.Context, x int) (int, error) {
		return 0, errors.New("something went wrong")
	}))
	err := v.LookupPath(cue.ParsePath("x")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*something went wrong.*`))
}

func TestNewFuncWrongArgCount(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#add: _, x: #add(3)")
	v = v.FillPath(cue.ParsePath("#add"), cue.NewFunc2(func(ctx context.Context, a, b int) (int, error) {
		return a + b, nil
	}))
	err := v.LookupPath(cue.ParsePath("x")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*expected 2 argument\(s\), got 1.*`))
}

func TestNewFuncLowLevel(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `#f: _, x: #f(2, "ab")`)
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc("rep", func(call *cue.Call, args []cue.Value) (cue.Value, error) {
		if len(args) != 2 {
			return cue.Value{}, fmt.Errorf("expected 2 arguments, got %d", len(args))
		}
		n, err := args[0].Int64(call.Context())
		if err != nil {
			return cue.Value{}, err
		}
		s, err := args[1].AsString(call.Context())
		if err != nil {
			return cue.Value{}, err
		}
		out := ""
		for range n {
			out += s
		}
		return call.Value(out), nil
	}))
	s, err := v.LookupPath(cue.ParsePath("x")).AsString(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(s, "abab"))
}

func TestNewFuncReturningErrorf(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc("fail", func(call *cue.Call, args []cue.Value) (cue.Value, error) {
		return cue.Errorf("no value for you"), nil
	}))
	err := v.LookupPath(cue.ParsePath("x")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*no value for you.*`))
}

func TestNewValidator(t *testing.T) {
	ctx := context.Background()
	check := cue.NewValidator(func(ctx context.Context, s string) error {
		if len(s) < 3 {
			return fmt.Errorf("string too short")
		}
		return nil
	}, cue.Name("minLen"))

	v := compileValue(t, `#v: _, good: #v & "hello", bad: #v & "hi"`)
	v = v.FillPath(cue.ParsePath("#v"), check)

	good := v.LookupPath(cue.ParsePath("good"))
	qt.Assert(t, qt.IsNil(good.Err(ctx)))
	s, err := good.AsString(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(s, "hello"))

	err = v.LookupPath(cue.ParsePath("bad")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*string too short.*`))
}

func TestNewValidatorTypeMismatch(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `#v: _, x: #v & 42`)
	v = v.FillPath(cue.ParsePath("#v"), cue.NewValidator(func(ctx context.Context, s string) error {
		return nil
	}))
	err := v.LookupPath(cue.ParsePath("x")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*decoding value for validator.*`))
}

func TestCallFromContext(t *testing.T) {
	ctx := context.Background()

	var stashedCtx context.Context
	var stashedCall *cue.Call

	v := compileValue(t, "#f: _, x: #f(21)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc1(func(ctx context.Context, x int) (int, error) {
		// Code deep in the user function's call tree can recover the
		// in-flight call from the context alone.
		call, ok := cue.CallFromContext(ctx)
		if !ok {
			return 0, fmt.Errorf("no call in context")
		}
		// Values constructed via the recovered call participate in the
		// active evaluation.
		d, err := call.Value(x * 2).Int64(ctx)
		if err != nil {
			return 0, err
		}
		stashedCtx = ctx
		stashedCall = call
		return int(d), nil
	}))

	i, err := v.LookupPath(cue.ParsePath("x")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 42))

	// After the function returns, the call is invalid: it can no longer
	// be recovered from the stashed context...
	_, ok := cue.CallFromContext(stashedCtx)
	qt.Assert(t, qt.IsFalse(ok))

	// ...and using a stale Call fails loudly.
	qt.Assert(t, qt.PanicMatches(func() {
		stashedCall.Value(1)
	}, `cue.Call used outside its call`))
	qt.Assert(t, qt.PanicMatches(func() {
		stashedCall.Context()
	}, `cue.Call used outside its call`))
}

func TestCallErrorf(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#f: _, x: #f(3)")
	v = v.FillPath(cue.ParsePath("#f"), cue.NewFunc("f", func(call *cue.Call, args []cue.Value) (cue.Value, error) {
		return cue.Value{}, call.Errorf("rejected %d", 3)
	}))
	err := v.LookupPath(cue.ParsePath("x")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*rejected 3.*`))
}

func TestFillPathZeroFunc(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "#f: _")
	v = v.FillPath(cue.ParsePath("#f"), cue.Func{})
	err := v.LookupPath(cue.ParsePath("#f")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*uninitialized \(zero\) cue.Func.*`))
}
