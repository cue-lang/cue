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

package cue

//go:generate go run generate_func.go

import (
	"reflect"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/runtime"
)

// FuncOption configures a function created by PureFunc1, [PureFunc2], etc.
type FuncOption func(*funcConfig)

type funcConfig struct {
	name string
}

// Name sets the name of the function for error messages.
func Name(name string) FuncOption {
	return func(cfg *funcConfig) {
		cfg.name = name
	}
}

// ValidatorFunc returns a [Value] that acts as a CUE validator.
// When unified with a concrete value, it decodes that value as type T
// and calls f to validate it. If f returns a non-nil error, the
// unification fails with that error.
func ValidatorFunc[T any](f func(T) error, opts ...FuncOption) Value {
	var cfg funcConfig
	for _, o := range opts {
		o(&cfg)
	}
	ctx := (*Context)(runtime.New())
	return newValueRoot(ctx.runtime(), ctx.ctx(), &adt.FuncValidator{
		Name: cfg.name,
		Validate: func(opCtx *adt.OpContext, v adt.Value) *adt.Bottom {
			var t T
			cueVal := newValueRoot(ctx.runtime(), opCtx, v)
			if err := cueVal.Decode(&t); err != nil {
				return opCtx.NewErrf("decoding value for validator: %v", err)
			}
			if err := f(t); err != nil {
				return opCtx.NewErrf("%v", err)
			}
			return nil
		},
	})
}

func pureFunc[Args, R any](f func(Args) (R, error), opts []FuncOption) Value {
	var cfg funcConfig
	for _, o := range opts {
		o(&cfg)
	}
	ctx := (*Context)(runtime.New()) // Share the context between all values.
	argT := reflect.TypeFor[Args]()
	return newValueRoot(ctx.runtime(), ctx.ctx(), &adt.Func{
		Name: cfg.name,
		Func: func(opCtx *adt.OpContext, argValues []adt.Value) adt.Expr {
			numArgs := argT.NumField()
			if len(argValues) != numArgs {
				return opCtx.NewErrf("expected %d argument(s), got %d", numArgs, len(argValues))
			}
			var args Args
			dstArgs := reflect.ValueOf(&args).Elem()
			for i := range numArgs {
				argVal := newValueRoot(ctx.runtime(), opCtx, argValues[i])
				if err := argVal.Decode(dstArgs.Field(i).Addr().Interface()); err != nil {
					return opCtx.NewErrf("decoding argument %d: %v", i, err)
				}
			}
			result, err := f(args)
			if err != nil {
				return opCtx.NewErrf("%v", err)
			}
			return convert.FromGoValue(opCtx, result, false)
		},
	})
}
