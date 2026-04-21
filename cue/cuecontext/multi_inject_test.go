// Copyright 2026 CUE Authors
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

package cuecontext

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"

	qt "github.com/go-quicktest/qt"
)

func TestMultiInjectionUnifiesValues(t *testing.T) {
	inj1 := &constInjection{
		kind:  "test",
		value: &adt.BasicType{K: adt.StringKind},
	}
	inj2 := &constInjection{
		kind:  "test",
		value: &adt.String{Str: "hello"},
	}
	ctx := New(WithInjection(inj1), WithInjection(inj2))

	v := ctx.CompileString(`
		@extern(test)

		package foo

		x: _ @test()
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(x.Err()))

	got, err := x.String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "hello"))
}

func TestMultiInjectionThreeInjectors(t *testing.T) {
	inj1 := &constInjection{
		kind:  "test",
		value: &adt.BasicType{K: adt.StringKind},
	}
	inj2 := &constInjection{
		kind:  "test",
		value: &adt.String{Str: "hello"},
	}
	inj3 := &constInjection{
		kind:  "test",
		value: &adt.BasicType{K: adt.StringKind},
	}
	ctx := New(WithInjection(inj1), WithInjection(inj2), WithInjection(inj3))

	v := ctx.CompileString(`
		@extern(test)

		package foo

		x: _ @test()
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(x.Err()))

	got, err := x.String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "hello"))
}

func TestMultiInjectionInjectedValueError(t *testing.T) {
	inj1 := &constInjection{
		kind:  "test",
		value: &adt.String{Str: "hello"},
	}
	inj2 := &constInjection{
		kind:     "test",
		valueErr: errors.Newf(token.NoPos, "injector2 failed"),
	}
	ctx := New(WithInjection(inj1), WithInjection(inj2))

	v := ctx.CompileString(`
		@extern(test)

		package foo

		x: _ @test()
	`)
	qt.Assert(t, qt.IsNotNil(v.Err()))
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*injector2 failed.*`))
}

func TestMultiInjectionInstanceError(t *testing.T) {
	inj1 := &constInjection{
		kind:  "test",
		value: &adt.String{Str: "hello"},
	}
	inj2 := &constInjection{
		kind:        "test",
		instanceErr: errors.Newf(token.NoPos, "instance init failed"),
	}
	ctx := New(WithInjection(inj1), WithInjection(inj2))

	v := ctx.CompileString(`
		@extern(test)

		package foo

		x: _ @test()
	`)
	qt.Assert(t, qt.IsNotNil(v.Err()))
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*instance init failed.*`))
}

type constInjection struct {
	kind        string
	value       adt.Expr
	valueErr    errors.Error
	instanceErr errors.Error
}

func (i *constInjection) Kind() string { return i.kind }

func (i *constInjection) InjectorForInstance(_ *build.Instance, _ *runtime.Runtime) (runtime.Injector, errors.Error) {
	if i.instanceErr != nil {
		return nil, i.instanceErr
	}
	return &constInjector{
		value: i.value,
		err:   i.valueErr,
	}, nil
}

type constInjector struct {
	value adt.Expr
	err   errors.Error
}

func (j *constInjector) InjectedValue(_ *runtime.ExternAttr, _ *adt.Vertex) (adt.Expr, errors.Error) {
	return j.value, j.err
}
