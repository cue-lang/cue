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

package cue

import (
	"context"
	"fmt"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/runtime"
)

// A Func describes a host-provided function or validator that can be
// called from CUE. A Func is independent of any loader or runtime: it
// becomes a value when placed into a configuration, via
// [Value.FillPath], value injection (@inject), or a package resolver
// (cueload.Config.Resolve).
//
// Functions must be pure: calling one with the same arguments must always
// produce the same result. The evaluator is free to call them multiple
// times, in any order, or not at all, and may cache their results.
// The contract for validators that perform I/O is an open question
// tracked in the proposal.
type Func struct {
	name string

	// Exactly one of call and validate is set.
	call     func(call *Call, args []Value) (Value, error)
	validate func(call *Call, v Value) error
}

// FuncOption configures a Func.
type FuncOption func(*funcConfig)

type funcConfig struct {
	name string
}

// Name sets the function's name for error messages and debug output.
func Name(name string) FuncOption {
	return func(cfg *funcConfig) {
		cfg.name = name
	}
}

func applyFuncOptions(opts []FuncOption) funcConfig {
	var cfg funcConfig
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// NewFunc returns the low-level form of a host-provided function: fn
// receives the in-flight evaluation as a [Call] together with its
// (concrete) arguments, and returns the result value.
//
// fn runs synchronously on the evaluator's goroutine. Values it derives
// from args, and values it constructs via the call, participate in the
// active evaluation.
func NewFunc(name string, fn func(call *Call, args []Value) (Value, error)) Func {
	return Func{name: name, call: fn}
}

// NewValidatorFunc is the low-level validator form, receiving the
// in-flight evaluation and the value being validated.
func NewValidatorFunc(name string, fn func(call *Call, v Value) error) Func {
	return Func{name: name, validate: fn}
}

// NewFunc1 returns a function of one argument with Go-typed argument and
// result handling: the argument is decoded as by [Value.Decode] and the
// result converted as by [Value.FillPath]. The context is derived from
// the calling evaluation (see [Call.Context]).
//
// NewFunc2 and NewFunc3 are the same for higher arities.
//
// TODO(generate): generate NewFunc4 through NewFunc10.
func NewFunc1[A0, R any](fn func(ctx context.Context, a0 A0) (R, error), opts ...FuncOption) Func {
	cfg := applyFuncOptions(opts)
	return NewFunc(cfg.name, func(call *Call, args []Value) (Value, error) {
		if err := checkArgCount(args, 1); err != nil {
			return Value{}, err
		}
		a0, err := decodeArg[A0](call, args, 0)
		if err != nil {
			return Value{}, err
		}
		r, err := fn(call.Context(), a0)
		if err != nil {
			return Value{}, err
		}
		return call.Value(r), nil
	})
}

// NewFunc2 is like [NewFunc1] but for functions of two arguments.
func NewFunc2[A0, A1, R any](fn func(ctx context.Context, a0 A0, a1 A1) (R, error), opts ...FuncOption) Func {
	cfg := applyFuncOptions(opts)
	return NewFunc(cfg.name, func(call *Call, args []Value) (Value, error) {
		if err := checkArgCount(args, 2); err != nil {
			return Value{}, err
		}
		a0, err := decodeArg[A0](call, args, 0)
		if err != nil {
			return Value{}, err
		}
		a1, err := decodeArg[A1](call, args, 1)
		if err != nil {
			return Value{}, err
		}
		r, err := fn(call.Context(), a0, a1)
		if err != nil {
			return Value{}, err
		}
		return call.Value(r), nil
	})
}

// NewFunc3 is like [NewFunc1] but for functions of three arguments.
func NewFunc3[A0, A1, A2, R any](fn func(ctx context.Context, a0 A0, a1 A1, a2 A2) (R, error), opts ...FuncOption) Func {
	cfg := applyFuncOptions(opts)
	return NewFunc(cfg.name, func(call *Call, args []Value) (Value, error) {
		if err := checkArgCount(args, 3); err != nil {
			return Value{}, err
		}
		a0, err := decodeArg[A0](call, args, 0)
		if err != nil {
			return Value{}, err
		}
		a1, err := decodeArg[A1](call, args, 1)
		if err != nil {
			return Value{}, err
		}
		a2, err := decodeArg[A2](call, args, 2)
		if err != nil {
			return Value{}, err
		}
		r, err := fn(call.Context(), a0, a1, a2)
		if err != nil {
			return Value{}, err
		}
		return call.Value(r), nil
	})
}

// NewValidator returns a validator: a value that constrains whatever it
// is unified with rather than computing a result. The argument is the
// value being validated, decoded into T.
func NewValidator[T any](fn func(ctx context.Context, v T) error, opts ...FuncOption) Func {
	cfg := applyFuncOptions(opts)
	return NewValidatorFunc(cfg.name, func(call *Call, v Value) error {
		var t T
		if err := v.Decode(call.Context(), &t); err != nil {
			return fmt.Errorf("decoding value for validator: %v", err)
		}
		return fn(call.Context(), t)
	})
}

func checkArgCount(args []Value, n int) error {
	if len(args) != n {
		return fmt.Errorf("expected %d argument(s), got %d", n, len(args))
	}
	return nil
}

func decodeArg[T any](call *Call, args []Value, i int) (T, error) {
	var t T
	if err := args[i].Decode(call.Context(), &t); err != nil {
		return t, fmt.Errorf("decoding argument %d: %v", i, err)
	}
	return t, nil
}

// adtValue converts the Func to its evaluator representation, bound to
// the given runtime. This happens when the value the Func was filled
// into is realized.
func (fn Func) adtValue(rt *runtime.Runtime) (adt.Value, errors.Error) {
	switch {
	case fn.call != nil:
		return &adt.Func{
			Name: fn.name,
			Func: func(opCtx *adt.OpContext, args []adt.Value) adt.Expr {
				call := newCall(opCtx, rt)
				defer call.invalidate()
				vals := make([]Value, len(args))
				for i, a := range args {
					vals[i] = call.wrapValue(a)
				}
				res, err := fn.call(call, vals)
				if err != nil {
					return opCtx.NewErrf("%v", err)
				}
				return call.resultExpr(res)
			},
		}, nil
	case fn.validate != nil:
		return &adt.FuncValidator{
			Name: fn.name,
			Validate: func(opCtx *adt.OpContext, x adt.Value) *adt.Bottom {
				call := newCall(opCtx, rt)
				defer call.invalidate()
				if err := fn.validate(call, call.wrapValue(x)); err != nil {
					return opCtx.NewErrf("%v", err)
				}
				return nil
			},
		}, nil
	}
	return nil, errors.Newf(token.NoPos, "cannot use uninitialized (zero) cue.Func")
}

// A Call represents an in-flight call from the evaluator into a
// host-provided function. It is valid only until the function returns
// and only on the goroutine the function was invoked on; retaining or
// sharing it fails loudly.
type Call struct {
	opCtx *adt.OpContext
	rt    *runtime.Runtime
	goCtx context.Context

	// valid is set to false when the function the Call was created
	// for returns. The Call may only be used on the goroutine the
	// function was invoked on, so no synchronization is needed.
	valid bool
}

// callCtxKey is the context key under which the in-flight Call is
// recoverable from the context returned by [Call.Context].
type callCtxKey struct{}

func newCall(opCtx *adt.OpContext, rt *runtime.Runtime) *Call {
	c := &Call{
		opCtx: opCtx,
		rt:    rt,
		valid: true,
	}
	c.goCtx = context.WithValue(opCtx.Context(), callCtxKey{}, c)
	return c
}

func (c *Call) invalidate() {
	c.valid = false
}

func (c *Call) check() {
	if !c.valid {
		panic("cue.Call used outside its call")
	}
}

// Context returns the context of the evaluation that made the call. It
// carries the cancellation and stats recorder of the operation that
// triggered evaluation, and the call itself (see [CallFromContext]).
func (c *Call) Context() context.Context {
	c.check()
	return c.goCtx
}

// Value converts a Go value to a CUE value within the active evaluation,
// following [Value.FillPath] conventions.
func (c *Call) Value(x any) Value {
	c.check()
	expr := convert.FromGoValue(c.opCtx, x, true)
	if w, ok := expr.(*adt.Vertex); ok {
		w.Finalize(c.opCtx)
		return newForcedValue(c.rt, w)
	}
	w := &adt.Vertex{}
	w.AddConjunct(adt.MakeRootConjunct(nil, expr))
	w.Finalize(c.opCtx)
	return newForcedValue(c.rt, w)
}

// Errorf returns an error carrying the position of the call site.
func (c *Call) Errorf(format string, args ...any) error {
	c.check()
	return errors.Newf(c.opCtx.Pos(), format, args...)
}

// wrapValue wraps an evaluator value as a Value participating in the
// active evaluation.
func (c *Call) wrapValue(x adt.Value) Value {
	w, ok := x.(*adt.Vertex)
	if !ok {
		w = &adt.Vertex{}
		w.AddConjunct(adt.MakeRootConjunct(nil, x))
	}
	w.Finalize(c.opCtx)
	return newForcedValue(c.rt, w)
}

// resultExpr converts the result value of a user function to an
// expression to hand back to the evaluator.
func (c *Call) resultExpr(res Value) adt.Expr {
	if res.op == nil {
		return c.opCtx.NewErrf("user function returned invalid (zero) cue.Value")
	}
	if res.rt != nil && res.rt != c.rt {
		return c.opCtx.NewErrf("user function returned value created by a different loader")
	}
	w, err := res.op.realize(&forcer{rt: c.rt, goCtx: c.goCtx, opCtx: c.opCtx})
	if err != nil {
		return c.opCtx.NewErrf("%v", err)
	}
	return w
}

// CallFromContext recovers the in-flight call from a context returned by
// [Call.Context]. It allows code that only receives a context — deep in a
// user function's call tree — to re-enter the evaluator. It returns
// false if ctx does not derive from an active call.
func CallFromContext(ctx context.Context) (*Call, bool) {
	c, ok := callFromContext(ctx)
	if !ok || !c.valid {
		return nil, false
	}
	return c, true
}

func callFromContext(ctx context.Context) (*Call, bool) {
	c, ok := ctx.Value(callCtxKey{}).(*Call)
	return c, ok
}
