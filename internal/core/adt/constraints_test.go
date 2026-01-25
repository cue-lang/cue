// Copyright 2023 CUE Authors
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

package adt_test

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
)

func TestMatchPatternValue(t *testing.T) {
	type testCase struct {
		expr   string
		label  string
		index  int64
		value  string // overrides value from label
		result bool
	}

	r := runtime.New()
	ctx := eval.NewContext(r, nil)

	pv := func(t *testing.T, s string) adt.Value {
		switch s {
		case "_":
			return &adt.Top{}
		}
		cfg := compile.Config{
			Imports: func(x *ast.Ident) (pkgPath string) {
				return r.BuiltinPackagePath(x.Name)
			},
		}
		v, b := r.Compile(&runtime.Config{Config: cfg}, s)
		if b.Err != nil {
			t.Fatal(b.Err)
		}
		v.Finalize(ctx)
		ctx.Err() // clear errors
		return v.Value()
	}

	str := func(s string) (adt.Feature, adt.Value) {
		f := r.StrLabel(s)
		return f, ctx.NewString(s)
	}

	idx := func(i int64) adt.Feature {
		return adt.MakeIntLabel(adt.IntLabel, i)
	}

	testCases := []testCase{{
		expr:   "string",
		label:  "foo",
		result: true,
	}, {
		expr:   "_",
		label:  "foo",
		result: true,
	}, {
		expr:   "_|_",
		label:  "foo",
		result: false,
	}, {
		expr:   `<"h"`,
		label:  "foo",
		result: true,
	}, {
		expr:   `"foo"`,
		label:  "bar",
		result: false,
	}, {
		expr:   `"foo"`,
		label:  "foo",
		result: true,
	}, {
		expr:   `<4`,
		index:  5,
		result: false,
	}, {
		expr:   `>=4`,
		index:  5,
		result: true,
	}, {
		expr:   `5`,
		index:  5,
		result: true,
	}, {
		expr:   `5`,
		label:  "str",
		result: false,
	}, {
		expr:   `>1 & <10`,
		index:  5,
		result: true,
	}, {
		expr:   `>1 & <10`,
		index:  10,
		result: false,
	}, {
		expr:   `<1 | >10`,
		index:  0,
		result: true,
	}, {
		expr:   `<1 | >10`,
		index:  5,
		result: false,
	}, {
		expr:   `strings.HasPrefix("foo")`,
		label:  "foo",
		result: true,
	}, {
		expr:   `strings.HasPrefix("foo")`,
		label:  "bar",
		result: false,
	}}

	cuetest.Run(t, testCases, func(t *cuetest.T, tc *testCase) {
		expr := pv(t.T, tc.expr)

		var f adt.Feature
		if tc.label != "" {
			f, _ = str(tc.label)
		} else {
			f = idx(tc.index)
		}

		t.Equal(adt.MatchPatternValue(ctx, expr, f), tc.result)
	})
}
