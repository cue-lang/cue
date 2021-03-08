// Copyright 2020 CUE Authors
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

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
)

func TestOptionalTypes(t *testing.T) {
	testCases := []struct {
		in  string
		out adt.OptionalType
	}{{
		in: `
		...
		`,
		out: adt.IsOpen,
	}, {
		in: `
		[string]: int
		`,
		// adt.IsOpen means fully defined in this context, which this is not.
		out: adt.HasPattern,
	}, {
		in: `
		bar: 3        // Not counted, as it is not optional.
		[string]: int // embedded into end result.
		"\(bar)": int
		`,
		out: adt.HasPattern | adt.HasDynamic,
	}, {
		in: `
		bar?: 3
		`,
		out: adt.HasField,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx := eval.NewContext(runtime.New(), nil)
			f, err := parser.ParseFile("opt", tc.in)
			if err != nil {
				t.Fatal(err)
			}

			v, errs := compile.Files(nil, ctx, "", f)
			if errs != nil {
				t.Fatal(errs)
			}

			v.Finalize(ctx)

			got := v.OptionalTypes()
			if got != tc.out {
				t.Errorf("got %x; want %x", got, tc.out)
			}
		})
	}
}
