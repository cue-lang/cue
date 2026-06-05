// Copyright 2022 CUE Authors
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
	"cuelang.org/go/internal/cueexperiment"
)

// allows reports whether a given label is permitted on a container.
//
//	allows(s, "x")  // true iff "x" may appear as a field of s
//	allows(xs, 3)   // true iff index 3 is in range for list xs
//
// The first argument is a struct or list; the second is a string (for
// struct fields) or an integer (for list indices). A closed struct
// rejects undeclared field names; a closed list rejects out-of-range
// indices; open containers admit any label. The check delegates to
// [adt.Vertex.Accept] — the same predicate the evaluator consults
// when deciding whether a conjunct is permitted on a closed value.
//
// Use this builtin to ask a structural question — "may this field
// ever exist here" — distinct from [exists], which asks the runtime
// question "is this field present right now". The two compose:
// `allows(s, "k") && exists(s.k)` is the strict "permitted and
// present" check.
var allows = &adt.Builtin{
	Name:       "allows",
	Experiment: cueexperiment.Reflect,
	Params: []adt.Param{
		{Value: &adt.BasicType{K: adt.StructKind | adt.ListKind}},
		{Value: &adt.BasicType{K: adt.StringKind | adt.IntKind}},
	},
	Result: adt.BoolKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()

		v, ok := call.Value(0).(*adt.Vertex)
		if !ok {
			return c.NewErrf("allows: first argument must be a struct or list")
		}

		key := call.Value(1)
		sel := c.Label(call.Expr(1), key)
		if c.HasErr() {
			return nil
		}

		return &adt.Bool{Src: call.Expr(0).Source(), B: v.Accept(c, sel)}
	},
}

// exists reports whether a referenced field is present as a regular
// member at the moment the call is evaluated.
//
//	exists(a)         // is field a present in the enclosing scope?
//	exists(s.x)       // is x present on struct s?
//	exists(xs[3])     // does list xs have an element at index 3?
//	exists(s["foo"])  // is "foo" present on s, indexed dynamically?
//
// The argument must be a reference: a field reference, a selector
// expression, or an index expression. Any other shape (a literal,
// an arithmetic expression, indexing on a non-container) is an error
// — the call is asking a question that cannot be answered.
//
// The result is true only when the referenced arc exists *and* is a
// regular member. All other cases collapse to false, including:
//
//   - The field is declared optional (a?: …) and has not been
//     supplied.
//   - The field is declared required (a!: …) and has not been
//     supplied.
//   - The field is not declared on a closed parent and so could
//     never appear.
//   - The index is out of range on a closed list.
//
// exists does not error on "permissible but absent" — use [allows] for
// the structural question "may this field exist here at all". The two
// builtins compose: `allows(s, "k") && exists(s.k)` is the strict
// check.
//
// exists is non-monotonic in the unification lattice — a sibling
// comprehension that later supplies the field can flip the answer
// from false to true. The evaluator records such checks and reports
// a cycle when the answer would be order-dependent (the same
// machinery the legacy `!= _|_` pattern relied on).
var exists = &adt.Builtin{
	Name:       "exists",
	Experiment: cueexperiment.Reflect,
	Params:     []adt.Param{valueParam},
	Result:     adt.BoolKind,
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

// existsN is a validator that passes when the count of existing references
// matches the constraint. Example: existsN(<=1, a, b, c) requires at most one
// of a, b, c to exist.
//
// This validator is typically embedded in a struct to constrain field presence:
//
//	foo: {
//	    a?: int
//	    b?: int
//	    existsN(<=1, a, b)  // at most one can be present
//	}
//
// Unlike matchN which evaluates arguments against self, existsN checks whether
// references exist without evaluating them. This requires using RawFunc to
// access unevaluated expressions.
var existN = &adt.Builtin{
	Name:            "existsN",
	Experiment:      cueexperiment.Reflect,
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

// isValid reports whether the expression evaluates without error.
// For reference expressions, it first checks that the reference exists;
// if the reference does not exist, isValid returns an error rather than false.
// This distinguishes "path doesn't exist" from "path exists but value is invalid".
var isValid = &adt.Builtin{
	Name:       "isValid",
	Experiment: cueexperiment.Reflect,
	Params:     []adt.Param{valueParam},
	Result:     adt.BoolKind,
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

// validN is a validator that passes when the count of valid (non-error)
// expressions matches the constraint.
//
// This is useful for checking expressions that may or may not unify successfully
// without propagating the errors.
var validN = &adt.Builtin{
	Name:            "validN",
	Experiment:      cueexperiment.Reflect,
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
// It waits for finalization before returning a result.
var isConcrete = &adt.Builtin{
	Name:       "isConcrete",
	Experiment: cueexperiment.Reflect,
	Params:     []adt.Param{valueParam},
	Result:     adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()
		arg := call.Expr(0)

		pass := checkConcrete(ctx, arg)
		return &adt.Bool{Src: arg.Source(), B: pass}
	},
}

// concreteN is a validator that passes when the count of concrete expressions
// matches the constraint.
//
// This validator is typically embedded in a struct:
//
//	foo: {
//	    a: int | *1    // has default, so concrete
//	    b: string      // type, not concrete
//	    concreteN(>=1, a, b)  // at least one must be concrete
//	}
var concreteN = &adt.Builtin{
	Name:            "concreteN",
	Experiment:      cueexperiment.Reflect,
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
			if checkConcrete(ctx, call.Expr(i)) {
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

// checkValid evaluates the expression and returns (true, nil) if it does not
// result in an error, (false, nil) otherwise. Any evaluation error—including
// missing fields—returns false. This is consistent with isValid({x: expr})
// which also returns false when expr errors.
//
// The actual logic — including the finalization needed to surface
// permanent ChildErrors on inline structs like
// `{} & {s: [for y in xc.undefined {}]}` — lives in
// [adt.OpContext.IsValidExpr2], which mirrors how the `!= _|_` validator
// determined validity (see [OpContext.validate]).
func checkValid(ctx *adt.OpContext, x adt.Expr) (bool, *adt.Bottom) {
	return ctx.IsValidExpr(ctx.Env(0), x), nil
}

// checkConcrete evaluates the expression and returns true if it is fully concrete.
// For structs and lists, this means all fields/elements must also be concrete.
func checkConcrete(ctx *adt.OpContext, x adt.Expr) bool {
	v, _ := ctx.Evaluate(ctx.Env(0), x)
	if _, isErr := v.(*adt.Bottom); isErr {
		return false
	}
	return isFullyConcrete(ctx, v)
}

// isFullyConcrete recursively checks if a value is fully concrete.
// A struct is concrete only if all its fields are concrete.
// A list is concrete only if all its elements are concrete.
// A disjunction with a single unambiguous default is considered concrete,
// because finalization will select that default.
func isFullyConcrete(ctx *adt.OpContext, v adt.Value) bool {
	v = adt.Default(v)
	// Check top-level concreteness first
	if v.Concreteness() != adt.Concrete {
		return false
	}

	// For vertices (structs and lists), recursively check children
	vertex, ok := v.(*adt.Vertex)
	if !ok {
		return true // Scalar values are concrete if Concreteness() == Concrete
	}

	// Ensure the vertex is fully evaluated
	vertex.CompleteArcsOnly(ctx)

	// Check all arcs (fields for structs, elements for lists)
	for _, arc := range vertex.Arcs {
		// Skip optional fields that aren't present
		if arc.ArcType != adt.ArcMember {
			continue
		}
		arc.CompleteArcsOnly(ctx)
		if !isFullyConcrete(ctx, arc) {
			return false
		}
	}

	return true
}
