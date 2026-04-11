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

package compile

import (
	"cuelang.org/go/internal/core/adt"
)

// exists reports whether its argument, which must be a field reference,
// selector, or index expression, refers to a regular member that is present
// when the call is evaluated. Optional or required-but-unset fields, fields not
// declared on a closed parent, and out-of-range closed-list indices are all
// false; any non-reference argument is an error. The evaluator reports a cycle
// when the answer would be order-dependent, as the legacy `!= _|_` pattern did.
var exists = &adt.Builtin{
	Name:   "exists",
	Added:  "v0.17.0",
	Params: []adt.Param{valueParam},
	Result: adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()
		arg := call.Expr(0)

		pass, err := checkExists(ctx, arg, "exists")
		if err != nil {
			return err
		}
		return &adt.Bool{Src: arg.Source(), B: pass}
	},
}

// existsN is a validator that passes when the number of existing references
// among its arguments satisfies the count constraint, e.g. existsN(<=1, a, b, c).
// Unlike matchN, it tests presence without evaluating the references.
var existsN = &adt.Builtin{
	Name:            "existsN",
	Added:           "v0.17.0",
	Params:          []adt.Param{intParam},
	NonConcrete:     true,
	VarArgs:         true,
	ValidatorNoSelf: true, // Doesn't take "self" - validates field presence
	Result:          adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()

		// With RawFunc, args are not pre-evaluated, so we access expressions
		// directly from call.Args and evaluate the bound manually.
		numArgs := call.NumArgs()
		if numArgs < 1 {
			return ctx.NewErrf("existsN requires at least 1 argument")
		}

		// Evaluate the bound (first argument) manually
		boundExpr := call.Expr(0)
		boundVal, _ := ctx.Evaluate(ctx.Env(0), boundExpr)
		if b, ok := boundVal.(*adt.Bottom); ok {
			return b
		}

		var count int64
		numRefs := numArgs - 1
		for i := 1; i <= numRefs; i++ {
			expr := call.Expr(i)
			exists, err := checkExists(ctx, expr, "existsN")
			if err != nil {
				return err
			}
			if exists {
				count++
			}
		}
		possibleCount := int64(numRefs)
		b := checkNum(ctx, "existsN", boundVal, count, possibleCount)
		if b != nil {
			return b
		}
		return adt.StaticBoolTrue
	},
}

// isValid reports whether the expression evaluates without error. A reference
// that does not exist yields an error rather than false, distinguishing "path
// absent" from "path present but invalid".
var isValid = &adt.Builtin{
	Name:   "isValid",
	Added:  "v0.17.0",
	Params: []adt.Param{valueParam},
	Result: adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()
		arg := call.Expr(0)

		pass, err := checkValid(ctx, arg)
		if err != nil {
			return err
		}
		return &adt.Bool{Src: arg.Source(), B: pass}
	},
}

// validN is a validator that passes when the number of valid (non-error)
// arguments satisfies the count constraint, without propagating their errors.
var validN = &adt.Builtin{
	Name:            "validN",
	Added:           "v0.17.0",
	Params:          []adt.Param{intParam},
	NonConcrete:     true,
	VarArgs:         true,
	ValidatorNoSelf: true, // Doesn't take "self"
	Result:          adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()

		// With RawFunc, args are not pre-evaluated, so we access expressions
		// directly and evaluate the bound manually.
		numArgs := call.NumArgs()
		if numArgs < 1 {
			return ctx.NewErrf("validN requires at least 1 argument")
		}

		// Evaluate the bound (first argument) manually
		boundExpr := call.Expr(0)
		boundVal, _ := ctx.Evaluate(ctx.Env(0), boundExpr)
		if b, ok := boundVal.(*adt.Bottom); ok {
			return b
		}

		var count, permInvalid int64
		numExprs := numArgs - 1
		for i := 1; i <= numExprs; i++ {
			v, _ := ctx.Evaluate(ctx.Env(0), call.Expr(i))
			if b, ok := v.(*adt.Bottom); ok {
				if !b.IsIncomplete() {
					permInvalid++ // permanently invalid, can never become valid
				}
			} else {
				count++
			}
		}
		possibleCount := int64(numExprs) - permInvalid
		b := checkNum(ctx, "validN", boundVal, count, possibleCount)
		if b != nil {
			return b
		}
		return adt.StaticBoolTrue
	},
}

// isConcrete reports whether the expression evaluates to a concrete value.
// It waits for finalization before returning a result. An erroneous argument,
// including an incomplete one, propagates as an error rather than reporting
// false: whether an erroneous value is concrete is not a meaningful question.
var isConcrete = &adt.Builtin{
	Name:   "isConcrete",
	Added:  "v0.17.0",
	Params: []adt.Param{valueParam},
	Result: adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()
		arg := call.Expr(0)

		pass, err := checkConcrete(ctx, arg)
		if err != nil {
			return err
		}
		return &adt.Bool{Src: arg.Source(), B: pass}
	},
}

// concreteN is a validator that passes when the number of concrete arguments
// satisfies the count constraint.
var concreteN = &adt.Builtin{
	Name:            "concreteN",
	Added:           "v0.17.0",
	Params:          []adt.Param{intParam},
	NonConcrete:     true,
	VarArgs:         true,
	ValidatorNoSelf: true, // Doesn't take "self"
	Result:          adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()

		// With RawFunc, args are not pre-evaluated, so we access expressions
		// directly and evaluate the bound manually.
		numArgs := call.NumArgs()
		if numArgs < 1 {
			return ctx.NewErrf("concreteN requires at least 1 argument")
		}

		// Evaluate the bound (first argument) manually
		boundExpr := call.Expr(0)
		boundVal, _ := ctx.Evaluate(ctx.Env(0), boundExpr)
		if b, ok := boundVal.(*adt.Bottom); ok {
			return b
		}

		var count int64
		numExprs := numArgs - 1
		for i := 1; i <= numExprs; i++ {
			// An erroneous argument is simply not counted as concrete; unlike
			// isConcrete, the count validator does not propagate the error.
			if c, _ := checkConcrete(ctx, call.Expr(i)); c {
				count++
			}
		}
		possibleCount := int64(numExprs)
		b := checkNum(ctx, "concreteN", boundVal, count, possibleCount)
		if b != nil {
			return b
		}
		return adt.StaticBoolTrue
	},
}

// checkExists checks if a reference path exists. Only the final component
// is tested for existence; all intermediate components must resolve without
// error. A missing final component returns false; a missing intermediate
// returns an error.
func checkExists(ctx *adt.OpContext, x adt.Expr, name string) (bool, *adt.Bottom) {
	return ctx.ResolveExists(x, name)
}

// checkValid reports whether x evaluates without error; any evaluation error,
// including a missing field, yields false. The logic lives in
// [adt.OpContext.IsValidExpr], mirroring how the `!= _|_` validator determined
// validity.
func checkValid(ctx *adt.OpContext, x adt.Expr) (bool, *adt.Bottom) {
	return ctx.IsValidExpr(ctx.Env(0), x), nil
}

// checkConcrete evaluates the expression and reports whether it is fully
// concrete. For structs and lists, this means all fields/elements must also be
// concrete. If the expression evaluates to an error, including an incomplete
// one, that error is returned so callers can choose to propagate it.
func checkConcrete(ctx *adt.OpContext, x adt.Expr) (bool, *adt.Bottom) {
	v, _ := ctx.Evaluate(ctx.Env(0), x)
	if b, ok := v.(*adt.Bottom); ok {
		return false, b
	}
	return isFullyConcrete(ctx, v)
}

// isFullyConcrete reports whether v is fully concrete: a struct or list is
// concrete only if all its members are, and a disjunction with a single default
// counts as concrete because finalization selects that default. The recursive
// check reuses [adt.Validate] — the same machinery behind
// cue.Value.Validate(cue.Concrete(true)).
func isFullyConcrete(ctx *adt.OpContext, v adt.Value) (bool, *adt.Bottom) {
	v = adt.Default(v)
	if v.Concreteness() != adt.Concrete {
		return false, nil
	}
	vertex, ok := v.(*adt.Vertex)
	if !ok {
		return true, nil // a concrete scalar
	}
	vertex.Finalize(ctx)
	b := adt.Validate(ctx, vertex, &adt.ValidateConfig{Concrete: true})
	if b == nil {
		return true, nil
	}
	if b.IsIncomplete() {
		return false, nil
	}
	return false, b
}
