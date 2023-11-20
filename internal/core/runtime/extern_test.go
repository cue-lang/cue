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

package runtime_test

import (
	"fmt"
	"strconv"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/value"
)

func Test(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "testdata/",
		Name: "extern",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		interpreter := &interpreterFake{files: map[string]int{}}
		ctx := cuecontext.New(cuecontext.Interpreter(interpreter))

		b := t.Instance()
		v := ctx.BuildInstance(b)
		if err := v.Err(); err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
			return
		}

		fmt.Fprintf(t, "%v\n", v)
	})
}

type interpreterFake struct {
	files map[string]int
}

func (i *interpreterFake) Kind() string { return "test" }

func (i *interpreterFake) NewCompiler(b *build.Instance, r *runtime.Runtime) (runtime.Compiler, errors.Error) {
	switch b.PkgName {
	case "failinit":
		return nil, errors.Newf(token.NoPos, "TEST: fail initialization")
	case "nullinit":
		return nil, nil
	case "scopetest":
		return newCompilerFake(b, r)
	}
	return i, nil
}

func (i *interpreterFake) Compile(funcName string, _ adt.Value, a *internal.Attr) (adt.Expr, errors.Error) {
	if ok, _ := a.Flag(1, "fail"); ok {
		return nil, errors.Newf(token.NoPos, "TEST: fail compilation")
	}

	str, ok, err := a.Lookup(1, "err")
	if err != nil {
		return nil, errors.Promote(err, "test")
	}

	if ok {
		return nil, errors.Newf(token.NoPos, "%s", str)
	}

	if str, err = a.String(0); err != nil {
		return nil, errors.Promote(err, "test")
	}

	if _, ok := i.files[str]; !ok {
		i.files[str] = len(i.files) + 1
	}

	return &adt.Builtin{
		Name:   "impl" + funcName + strconv.Itoa(i.files[str]),
		Params: []adt.Param{{Value: &adt.BasicType{K: adt.IntKind}}},
		Result: adt.IntKind,
	}, nil
}

type compilerFake struct {
	runtime *runtime.Runtime
	b       *build.Instance
}

func newCompilerFake(b *build.Instance, r *runtime.Runtime) (runtime.Compiler, errors.Error) {
	return &compilerFake{
		runtime: r,
		b:       b,
	}, nil
}

func (c *compilerFake) Compile(name string, scope adt.Value, a *internal.Attr) (adt.Expr, errors.Error) {
	typStr, err := a.String(0)
	if err != nil {
		return nil, errors.Promote(err, "test")
	}

	call := &adt.CallExpr{Fun: &adt.Builtin{
		Result: adt.TopKind,
		Func: func(opctx *adt.OpContext, args []adt.Value) adt.Expr {
			cuectx := (*cue.Context)(c.runtime)
			scope := value.Make(opctx, scope)

			typ := cuectx.CompileString(typStr, cue.Scope(scope))
			_, ityp := value.ToInternal(typ)
			return ityp
		},
	}}
	return call, nil
}
