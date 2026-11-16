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
	"math"

	"cuelang.org/go/internal/core/adt"
)

// exists reports whether its argument, which must be a field reference,
// selector, or index expression, refers to a regular member that is present
// when the call is evaluated. Presence is independent of the member's value:
// a present member with a broken value is still true. Declared optional or
// required-but-unset fields, undeclared fields on open parents,
// pattern-admitted labels, and open-list indices beyond the current length
// are all false: absent, but a legal configuration could add them. A member
// that can never exist — an undeclared label on a closed parent, an
// out-of-range index of a closed list, or any member of a value whose kind
// admits none — is a permanent error, as is a missing intermediate (with the
// incomplete flavor when the intermediate could still appear). The evaluator
// reports a cycle when the answer would be order-dependent, as the legacy
// `!= _|_` pattern did.
var exists = &adt.Builtin{
	Name:   "exists",
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

// existsN is a validator that passes when the number of present references
// among its arguments satisfies the count constraint, e.g.
//
//	existsN(<=1, a, b, c)
//
// for mutually exclusive fields (the protobuf oneof pattern). It is the
// presence-specialized form of countN: existsN(n, a, b, c) is equivalent to
// countN(n, [exists(a), exists(b), exists(c)]). It takes its references as
// variadic arguments because raw references are syntax, not values: they
// could not ride in a list literal without being evaluated. Each reference
// has exists semantics, including strictness: a reference that can never
// exist is an error, not a silently uncounted condition.
var existsN = &adt.Builtin{
	Name:            "existsN",
	Params:          []adt.Param{intParam},
	NonConcrete:     true,
	VarArgs:         true,
	ValidatorNoSelf: true, // Doesn't take "self": validates field presence.
	Result:          adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		ctx := call.OpContext()

		// With RawFunc, args are not pre-evaluated, so we access expressions
		// directly from call.Args and evaluate the bound manually.
		numArgs := call.NumArgs()
		if numArgs < 1 {
			return ctx.NewErrf("existsN requires at least 1 argument")
		}

		boundVal, b := evalBound(ctx, call.Expr(0), "existsN")
		if b != nil {
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
		if b := checkNum(ctx, "existsN", boundVal, count, possibleCount); b != nil {
			return b
		}
		return adt.StaticBoolTrue
	},
}

// countN is a validator that passes when the number of true elements in its
// list argument satisfies the count constraint, e.g.
//
//	countN(1, [exists(a), exists(b), exists(c)])
//
// for the "exactly one of" pattern; existsN is its presence-specialized
// shorthand, while the list form is what admits computed condition lists,
// e.g. countN(1, [for k in names {exists(s[k])}]). A permanent error in an
// element propagates; an element that is still incomplete at the
// configuration's fixpoint is an error rather than being silently counted as
// false. As for existsN, too few true conditions is incomplete, since an
// instance may still meet the unmet ones, while too many is permanent.
// isValid(countN(...)) provides the boolean form of either.
var countN = &adt.Builtin{
	Name:            "countN",
	Params:          []adt.Param{intParam, listParam},
	NonConcrete:     true,
	ValidatorNoSelf: true, // Doesn't take "self": validates its own arguments.
	Result:          adt.BoolKind,
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
		c := call.OpContext()

		if call.NumArgs() != 2 {
			return c.NewErrf("countN requires 2 arguments")
		}
		bound, b := evalBound(c, call.Expr(0), "countN")
		if b != nil {
			return b
		}
		list, _ := c.Evaluate(c.Env(0), call.Expr(1))
		if b, ok := list.(*adt.Bottom); ok {
			return b
		}
		// An open list reads as the list of its elements so far once its
		// default is taken below, so note that it is open first. For a
		// disjunction that is its default, which keeps an open list open.
		open := false
		if v, ok := adt.DefaultDisjunct(list).(*adt.Vertex); ok {
			m, _ := v.DerefValue().BaseValue.(*adt.ListMarker)
			open = m != nil && m.IsOpen && !m.NoEllipsis
		}
		// A defaulted list of conditions is its default, as it would be once
		// the configuration is finalized.
		lv, ok := adt.ConcreteDefault(list).(*adt.Vertex)
		if !ok || !lv.IsList() {
			if list.Kind()&adt.ListKind != 0 {
				// The argument may still become a list, as for a disjunction
				// without a default: an unformed list of conditions.
				return &adt.Bottom{
					Code: adt.IncompleteError,
					Err:  c.Newf("countN: second argument is not a resolved list"),
				}
			}
			return c.NewErrf("countN: second argument must be a list, found %s", list.Kind())
		}

		var count, unmet int64
		// pending is the first condition that is not settled yet. It is only
		// reported once all conditions are seen, so that a permanent error
		// in a later condition takes precedence wherever it sits in the list.
		var pending *adt.Bottom
		for e := range lv.Elems() {
			e.Finalize(c)
			// A defaulted condition counts as its default, as it would
			// once the configuration is finalized.
			v := adt.Unwrap(adt.ConcreteDefault(e.DerefValue()))
			if b, ok := v.(*adt.Bottom); ok {
				// Propagate by flavor: a permanent element error is a broken
				// condition; an incomplete one retries and, if still
				// unresolved at the fixpoint, is an unformed condition.
				if !b.IsIncomplete() {
					return b
				}
				if pending == nil {
					pending = b
				}
				continue
			}
			x, ok := v.(*adt.Bool)
			if !ok {
				if v != nil && v.Kind()&adt.BoolKind == 0 {
					return c.NewErrf("countN: element must be a boolean, found %s", v.Kind())
				}
				// A boolean that is not yet known, such as a field of type
				// bool in a schema: an instance may still settle it.
				if pending == nil {
					pending = &adt.Bottom{
						Code: adt.IncompleteError,
						Err:  c.Newf("countN: element %d is not a concrete boolean", e.Label.Index()),
					}
				}
				continue
			}
			if x.B {
				count++
			} else {
				unmet++
			}
		}
		if pending != nil {
			return pending
		}
		// A false condition, such as exists(a) for an optional a, may still
		// become true in an instance, as with existsN's absent references. So
		// a shortfall that the unmet conditions could make up is incomplete,
		// not permanent: a schema is not broken merely because its instances
		// are still to be filled in. Exceeding the bound is permanent.
		//
		// An open list may still gain conditions as well, as for a schema
		// that declares xs: [...bool]. Any shortfall is then incomplete. A
		// bound that the conditions so far meet passes, as it does for
		// existsN over fields that may still be added: the validator runs
		// again for each instance.
		maxCount := count + unmet
		if open {
			maxCount = math.MaxInt64
		}
		if b := checkNum(c, "countN", bound, count, maxCount); b != nil {
			return b
		}
		return adt.StaticBoolTrue
	},
}

// evalBound evaluates the count bound of existsN or countN, whose RawFunc
// evaluates its arguments by hand. A reference bound evaluates to the
// referenced vertex, so it is dereferenced to the bound value itself: only
// then can checkNum tell a settled violation from one that may still be
// resolved. A bound that is not a number is a permanent error, as it is for
// matchN, whose parameter check rejects it.
func evalBound(c *adt.OpContext, x adt.Expr, fn string) (adt.Value, *adt.Bottom) {
	v, _ := c.Evaluate(c.Env(0), x)
	v = adt.Unwrap(v)
	switch b := v.(type) {
	case nil:
		return nil, &adt.Bottom{
			Code: adt.IncompleteError,
			Err:  c.Newf("%s: unresolved bound", fn),
		}
	case *adt.Bottom:
		return nil, b
	}
	if k := v.Kind(); k&adt.IntKind == 0 {
		return nil, c.NewErrf("cannot use %s (type %s) as int in argument 1 to %s", v, k, fn)
	}
	return v, nil
}

// isValid reports whether the expression denotes a satisfiable value: false
// iff the expression's own denotation is a bottom of any flavor (an
// unresolved reference, an absent field, or a permanent error) or a permanent
// error is reachable in some instance of it; true otherwise. It is the one
// error-catching function: it never propagates errors arising from its
// argument.
var isValid = &adt.Builtin{
	Name:   "isValid",
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

// isConcrete reports whether the expression evaluates to a fully ground
// value. Anything clean but unfinished — an abstract type, an undefaulted
// disjunction, or an incomplete value, whether the whole argument or its
// interior — is false. A permanent error propagates: isConcrete is not an
// error catcher, and whether a broken value is concrete is not a meaningful
// question.
var isConcrete = &adt.Builtin{
	Name:   "isConcrete",
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

// checkExists checks if a reference path exists. Only the final component
// is tested for existence; all intermediate components must resolve without
// error. A missing final component that could still be added returns false;
// one that can never exist, or a missing intermediate, returns an error.
func checkExists(ctx *adt.OpContext, x adt.Expr, name string) (bool, *adt.Bottom) {
	return ctx.ResolveExists(x, name)
}

// checkValid reports whether x evaluates without a permanent error; the logic
// lives in [adt.OpContext.IsValidExpr], mirroring how the `!= _|_` validator
// determined validity. A post-check re-derives the answer once the enclosing
// node finalizes, so an answer that fed back into the configuration and
// changed is reported as a cycle.
func checkValid(ctx *adt.OpContext, x adt.Expr) (bool, *adt.Bottom) {
	valid, b := ctx.IsValidExpr(ctx.Env(0), x)
	if b != nil {
		return false, b
	}
	ctx.RegisterPostCheck(x, func(c *adt.OpContext, env *adt.Environment) bool {
		return c.RecheckValidExpr(env, x, valid)
	})
	return valid, nil
}

// checkConcrete evaluates the expression and reports whether it is fully
// ground. An expression that is incomplete at the configuration's fixpoint —
// an absent optional field, say — is definitively not ground and commits
// false; only permanent errors and cycles propagate. A post-check re-derives
// the committed answer at finalization.
func checkConcrete(ctx *adt.OpContext, x adt.Expr) (bool, *adt.Bottom) {
	v, b := ctx.EvaluateReflect(x, "isConcrete")
	if b != nil {
		return false, b
	}
	if v == nil {
		// Incomplete at the fixpoint: not a ground datum. EvaluateReflect
		// registered a post-check that x is still an error at finalization.
		// Reading x again returns its stale error if x was finalized while
		// a field it depends on was still absent, so also re-derive it: the
		// answer does not hold if x has become a ground value since.
		ctx.RegisterPostCheck(x, func(c *adt.OpContext, env *adt.Environment) bool {
			fresh := c.RederiveStaleExpr(env, x)
			if fresh == nil {
				return true
			}
			p, e := isFullyConcrete(c, fresh)
			return e != nil || !p
		})
		return false, nil
	}
	pass, err := isFullyConcrete(ctx, v)
	if err != nil {
		return false, err
	}
	ctx.RegisterPostCheck(x, func(c *adt.OpContext, env *adt.Environment) bool {
		p, e := checkConcreteFinal(c, x)
		if e != nil {
			// The configuration is independently broken at x; its own error
			// suffices without an additional cycle report.
			return true
		}
		return p == pass
	})
	return pass, nil
}

// checkConcreteFinal is the non-registering, non-suspending variant of
// checkConcrete used by its finalization post-check.
func checkConcreteFinal(ctx *adt.OpContext, x adt.Expr) (bool, *adt.Bottom) {
	v, _ := ctx.Evaluate(ctx.Env(0), x)
	if b, ok := v.(*adt.Bottom); ok {
		if b.Code == adt.IncompleteError {
			return false, nil
		}
		return false, b
	}
	return isFullyConcrete(ctx, v)
}

// isFullyConcrete reports whether v is fully ground: a struct or list is
// concrete only if all its regular members and elements are, an unset
// required field makes it non-concrete (matching what export needs), and a
// disjunction with a single default counts as concrete because finalization
// selects that default. Optional and pattern constraints, and open list
// tails, never block groundedness. The recursive check reuses [adt.Validate]
// — the same machinery behind cue.Value.Validate(cue.Concrete(true)).
func isFullyConcrete(ctx *adt.OpContext, v adt.Value) (bool, *adt.Bottom) {
	v = adt.ConcreteDefault(v)
	if v.Concreteness() != adt.Concrete {
		return false, nil
	}
	vertex, ok := v.(*adt.Vertex)
	if !ok {
		return true, nil // a concrete scalar
	}
	vertex.Finalize(ctx)
	// Validate first: a permanent error propagates even if the value is also
	// unfinished, so an unset required field must not mask it.
	b := adt.Validate(ctx, vertex, &adt.ValidateConfig{Concrete: true})
	if b != nil && !b.IsIncomplete() {
		return false, b
	}
	if b != nil || hasUnsetRequired(vertex) {
		return false, nil
	}
	return true, nil
}

// hasUnsetRequired reports whether v or any of its regular members contains an
// unset required field. Such a value is not a complete datum: export fails on
// it. The check walks arcs directly rather than using [adt.Validate] with its
// Final option, which would evaluate constraints on the shared vertex and leak
// their errors into the configuration.
func hasUnsetRequired(v *adt.Vertex) bool {
	// A member holding a disjunction with a single default counts as that
	// default, as for the value as a whole: its required fields are those
	// of the disjunct that finalization selects.
	for _, a := range v.ConcreteDefault().Arcs {
		if !a.Label.IsRegular() {
			continue
		}
		switch a.ArcType {
		case adt.ArcRequired:
			return true
		case adt.ArcMember:
			if hasUnsetRequired(a) {
				return true
			}
		}
	}
	return false
}
