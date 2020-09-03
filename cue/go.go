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

package cue

import (
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/eval"
)

func init() {
	internal.FromGoValue = func(runtime, x interface{}, nilIsTop bool) interface{} {
		r := runtime.(*Runtime)
		ctx := eval.NewContext(r.index().Runtime, nil)
		v := convert.GoValueToValue(ctx, x, nilIsTop)
		n := adt.ToVertex(v)
		return Value{r.idx, n}
	}

	internal.FromGoType = func(runtime, x interface{}) interface{} {
		r := runtime.(*Runtime)
		ctx := eval.NewContext(r.index().Runtime, nil)
		expr, err := convert.GoTypeToExpr(ctx, x)
		if err != nil {
			expr = &adt.Bottom{Err: err}
		}
		n := &adt.Vertex{}
		n.AddConjunct(adt.MakeRootConjunct(nil, expr))
		return Value{r.idx, n}

		// return convertType(runtime.(*Runtime), x)
	}
}
