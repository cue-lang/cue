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

package adt

import (
	"fmt"
	"slices"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// This file implements unification of CUE function values and
// function types (bodyless function literals).
//
// A function type acts as a constraint on a function value: unifying the two
// yields the original value with the type recorded as an extra [FuncType]
// constraint. Static checks at unification time are limited to what is
// decidable from the compiled signatures alone:
//
//   - every parameter of the type must be declared by the value. Positional
//     parameters first align by ordinal. Their plain contract labels then
//     unify: equal labels agree, a label on only one side names the shared
//     slot, and two different labels conflict. A name-only parameter (a! or
//     a?, which can be passed by label alone) matches the parameter with its
//     label, positional or not; in a meet of two types, a labeled positional
//     parameter likewise matches a name-only parameter with its label. A
//     type parameter that the value does not declare is allowed only if the
//     parameter is omittable — optional (a?) or declaring a default
//     (a: T = d); callable function values are closed;
//   - requiredness combines as for struct fields: the strictest marking of
//     the matched parameters — plain, then required, then optional — decides
//     at call time whether the parameter may be omitted (see
//     [FuncParamArcType]);
//   - unless the type's signature is open, every parameter of the value must
//     be addressable through the type — by label, or positionally for the
//     value's positional parameters — or be omittable;
//   - defaults declared by both signatures for the same parameter must
//     unify (see [checkFuncDefaultsMeet]); a default declared by one side
//     only carries over to calls.
//
// Everything else is deferred to call time, where the type's parameter
// constraints, defaults for parameters the call leaves unbound, and result
// constraint are scheduled alongside those of the value (see
// [nodeContext.scheduleFuncCall]). In particular:
//
//   - the conjunction of a type's and a value's parameter constraints is
//     enforced when the function is called; that the value's declaration
//     admits what the type's requires is checked where the two meet (see
//     [checkFuncAdmission]);
//   - the type's parameter order does not replace the value's implementation
//     signature. A plain name can supply the contract label of an otherwise
//     unnamed matched slot, while positional calls and the function body's
//     local bindings remain those of the value.
//
// Unifying two function types checks the same parameter matching in both
// directions — a parameter present in only one signature is allowed only if
// the other signature is open or the parameter is omittable — and yields a
// type carrying both signatures. One rule thus governs extra parameters
// everywhere — for tightening in either direction, for type meets, for
// builtins, and for subsumption (see internal/core/subsume): an extra
// parameter is admitted against a closed signature iff it is omittable,
// that is, optional or declaring a default.
// The meet of the parameter constraints is not materialized: a value unified
// with the result is checked against, and calls are constrained by, each
// recorded signature separately. A parameter that is optional in one
// signature and plain in the other is plain (the stricter form) in effect,
// as calls bind against the value's signature.

// A FuncType records a function type that a function value or another
// function type has been unified with. The type's parameter constraints and
// result constraint must additionally be enforced on any call through the
// carrying [FuncValue]. A plain parameter name supplies the contract label of
// a matched positional slot when that slot was otherwise unnamed.
type FuncType struct {
	Fn  *Function
	Env *Environment
}

// IsFuncType reports whether v is a bodyless function literal, i.e. a
// function type rather than a callable function value. It is exported for
// use by internal/core/subsume.
func IsFuncType(v *FuncValue) bool {
	return v.Fn == nil || v.Fn.Body == nil
}

// MatchFuncParam returns the parameter of fn addressed by the given label,
// or, if label is InvalidLabel, the positional parameter of fn in position
// pos. The second return value is the index of the parameter in fn.Params. It
// is exported for use by internal/core/subsume.
func MatchFuncParam(fn *Function, label Feature, pos int) (FuncParam, int, bool) {
	if label != InvalidLabel {
		for j, q := range fn.Params {
			if q.Label == label {
				return q, j, true
			}
		}
		return FuncParam{}, -1, false
	}
	i := 0
	for j, q := range fn.Params {
		if !q.Positional {
			continue
		}
		if i == pos {
			return q, j, true
		}
		i++
	}
	return FuncParam{}, -1, false
}

// matchFuncParams maps every parameter of x to the parameter of y that it
// declares, or to -1 when y does not declare it. Positional parameters map by
// positional ordinal; name-only parameters map by label, to a positional or a
// name-only parameter of y. If byLabel is set, a labeled positional parameter
// of x that has no counterpart at its ordinal maps to the name-only parameter
// of y with its label. This is the rule for a meet of two function types:
// neither signature decides how the function is called, and a name-only and a
// positional parameter with the same label are the same parameter. When y is a
// function value, its own parameters decide how it can be called, so a
// positional parameter of x must find a positional parameter of y.
//
// A parameter of y can be matched at most once. This keeps positional and
// name-only matches from ever collapsing two declarations onto one slot.
func matchFuncParams(x, y *Function, byLabel bool) []int {
	matches := make([]int, len(x.Params))
	for i := range matches {
		matches[i] = -1
	}
	used := make([]bool, len(y.Params))
	pos := 0
	for i, p := range x.Params {
		var j int
		var ok bool
		if p.Positional {
			_, j, ok = MatchFuncParam(y, InvalidLabel, pos)
			pos++
			if !ok && byLabel && p.Label != InvalidLabel {
				var q FuncParam
				q, j, ok = MatchFuncParam(y, p.Label, -1)
				ok = ok && !q.Positional
			}
			if ok && used[j] {
				ok = false
			}
		} else {
			_, j, ok = MatchFuncParam(y, p.Label, -1)
			if ok && used[j] {
				ok = false
			}
		}
		if ok {
			matches[i] = j
			used[j] = true
		}
	}
	return matches
}

// conflictingPositionalLabels reports the first positional ordinal for which
// two signatures declare different contract labels. An absent label is not a
// conflict: a compatible signature may add a contract name to an otherwise
// unnamed positional slot.
func conflictingPositionalLabels(x, y *Function) (a, b Feature, pos int, ok bool) {
	xi, yi := 0, 0
	for pos := 0; ; pos++ {
		for xi < len(x.Params) && !x.Params[xi].Positional {
			xi++
		}
		for yi < len(y.Params) && !y.Params[yi].Positional {
			yi++
		}
		if xi == len(x.Params) || yi == len(y.Params) {
			break
		}
		a, b := x.Params[xi].Label, y.Params[yi].Label
		if a != InvalidLabel && b != InvalidLabel && a != b {
			return a, b, pos, true
		}
		xi++
		yi++
	}
	return InvalidLabel, InvalidLabel, -1, false
}

// checkParamsDeclared verifies that every parameter of x is declared by y.
// Positional parameters first align by ordinal. Equal plain contract labels
// agree, a label on only one side names the shared slot, and different labels
// conflict. A name-only parameter (one marked a! or a?, which can be passed by
// label alone) matches by label.
// A parameter of x that y does not declare is allowed only if y's signature
// is open or the parameter is omittable, that is, optional (a?) or declaring
// a default (a: T = d): such a parameter can never be bound by a call through
// y, but no call needs it either. Only x's own declaration counts: a default
// that another signature attached to x declares does not admit the
// parameter, so that the outcome does not depend on the order in which
// signatures are attached (see [ExtraFuncParam]). Requiredness is not
// checked here: it combines at call time (see [FuncParamArcType]). byLabel is
// passed to [matchFuncParams]. The strings xd and yd describe the two
// signatures in error messages.
func checkParamsDeclared(c *OpContext, x, y *Function, byLabel bool, xd, yd string) *Bottom {
	if a, b, pos, ok := conflictingPositionalLabels(x, y); ok {
		return c.NewErrf("conflicting parameter labels %s and %s for positional parameter %d in %s and %s",
			a.SelectorString(c), b.SelectorString(c), pos+1, xd, yd)
	}
	matches := matchFuncParams(x, y, byLabel)
	for i, p := range x.Params {
		if matches[i] < 0 {
			// An omittable parameter, optional or defaulted by x itself,
			// is admitted.
			if y.Open || p.ArcType == ArcOptional || p.Default != nil {
				continue
			}
			if p.Label != InvalidLabel {
				return c.NewErrf("parameter %s of %s not allowed by closed %s",
					p.Label.SelectorString(c), xd, yd)
			}
			return c.NewErrf("%s has more positional parameters than closed %s", xd, yd)
		}
	}
	return nil
}

// checkFuncTypeMeet verifies that two function types have unifiable
// signatures: positional parameters align by ordinal and unify their contract
// labels, name-only parameters match by label, and a parameter present in only
// one is allowed only if the other signature is open or the parameter is
// omittable, optional or declaring a default.
func checkFuncTypeMeet(c *OpContext, x, y *Function) *Bottom {
	if b := checkParamsDeclared(c, x, y, true, "function type", "function type"); b != nil {
		return b
	}
	return checkParamsDeclared(c, y, x, true, "function type", "function type")
}

// checkFuncTightening verifies that the function value val may be tightened
// by the function type typ: every non-omittable parameter of the type must
// be declared by the value, and, unless the type is open, every
// non-omittable parameter of the value must be addressable through the
// type. Whether a parameter of the value is omittable is decided by the
// value's own declaration alone, not by the signatures already attached to
// it: a closed type restricts the value it is attached to whatever else is
// attached, as a closed struct restricts its fields, so the result of a
// unification does not depend on the order in which its signatures are
// attached.
func checkFuncTightening(c *OpContext, typ, val *Function) *Bottom {
	if b := checkParamsDeclared(c, typ, val, false, "function type", "function"); b != nil {
		return b
	}
	if typ.Open {
		return nil
	}
	p, ok := ExtraFuncParam(typ, val, nil)
	if !ok {
		return nil
	}
	if p.Label != InvalidLabel {
		return c.NewErrf("parameter %s of function not allowed by closed function type",
			p.Label.SelectorString(c))
	}
	return c.NewErrf("function has more positional parameters than closed function type")
}

// checkFuncComposition verifies that two function values may be composed:
// each must declare every non-omittable parameter of the other, and, as calls
// bind positional arguments through either, a positional parameter of one
// must match a positional parameter of the other.
func checkFuncComposition(c *OpContext, x, y *Function) *Bottom {
	if b := checkParamsDeclared(c, x, y, false, "function", "function"); b != nil {
		return b
	}
	return checkParamsDeclared(c, y, x, false, "function", "function")
}

// checkFuncAdmission verifies declared compatibility between the
// implementations and the bodyless signatures among head and types. An
// implementation must declare, for every parameter it shares with a
// signature, a constraint admitting every value the signature's constraint
// admits, and a result constraint whose values the signature's result
// admits. Each implementation is checked against each signature on its own:
// neither another implementation nor the constraints enforced per call can
// stand in for a declaration that falls short, and a signature's constraints
// keep restricting calls regardless of what an implementation accepts.
//
// Incompatibility is established from complete evidence only: a constraint
// that does not yet evaluate to a complete value, or a comparison that no
// rule of [OpContext.admits] decides, does not reject the binding.
func checkFuncAdmission(c *OpContext, head FuncType, types []FuncType) *Bottom {
	for _, sig := range types {
		if sig.Fn.Body != nil {
			continue
		}
		if head.Fn.Body != nil {
			if b := c.checkDeclared(sig, head, types); b != nil {
				return b
			}
		}
		for _, impl := range types {
			if impl.Fn.Body == nil {
				continue
			}
			if b := c.checkDeclared(sig, impl, types); b != nil {
				return b
			}
		}
	}
	return nil
}

// checkDeclared verifies that the implementation impl satisfies the bodyless
// signature sig (see [checkFuncAdmission]). Parameters match as calls bind
// them, through the labels all of types contribute; a signature parameter
// that matches no parameter of the implementation is never bound through
// the signature.
func (c *OpContext) checkDeclared(sig, impl FuncType, types []FuncType) *Bottom {
	for i, j := range matchFuncParamsToValue(sig.Fn, impl.Fn, types) {
		if j < 0 {
			continue
		}
		sp, ip := &sig.Fn.Params[i], &impl.Fn.Params[j]
		if !c.covers(impl.Env, ip.Value, sig.Env, sp.Value) {
			return admissionErr(c, impl.Fn, sig.Fn,
				"parameter %s of function accepts %s, but its signature requires %s",
				funcParamName(c, i, sp), constraintString(c, ip.Value), constraintString(c, sp.Value))
		}
	}
	if !c.covers(sig.Env, sig.Fn.Ret, impl.Env, impl.Fn.Ret) {
		return admissionErr(c, impl.Fn, sig.Fn,
			"function returns %s, but its signature requires %s",
			constraintString(c, impl.Fn.Ret), constraintString(c, sig.Fn.Ret))
	}
	return nil
}

// checkEntitlement checks a function value supplied as the argument arg,
// compiled in argEnv, for parameter i of a call to x against the bodyless
// signatures that constrain that parameter. Such a signature S entitles the
// callee to call the argument with every value S's parameters admit. The
// supplied value's caller clauses, the bodyless signatures it was unified
// with, must permit those calls; otherwise a call the callee is entitled to
// would fail inside its body, or never be reported if the callee happens not
// to make it. Unifying S with the value cannot establish this: callers get
// the intersection of the clauses, so a narrower clause silently restricts
// what the callee was promised. The comparison is contravariant on the
// parameters and reuses the coverage relation of admission, so it admits
// wherever that relation is undecided; the value's own declaration is
// checked by that admission when S is unified with it.
func (c *OpContext) checkEntitlement(x *FuncValue, i int, arg Expr, argEnv *Environment) *Bottom {
	sigs := c.paramSignatures(x, i)
	if len(sigs) == 0 {
		return nil
	}
	return c.checkSuppliedEntitlement(sigs, c.suppliedFunc(argEnv, arg), funcParamName(c, i, &x.Fn.Params[i]))
}

// checkSuppliedEntitlement checks the supplied value v, a function value or
// builtin, against the bodyless signatures sigs that constrain the parameter
// named name (see [OpContext.checkEntitlement]). Any other value, including
// one not determined yet, is not checked here, nor is a partial application,
// whose clauses still describe the parameters it has bound: binding it to a
// signature fails regardless, as a partial application cannot be tightened.
func (c *OpContext) checkSuppliedEntitlement(sigs []FuncType, v Value, name string) *Bottom {
	// Parameters are matched through the labels of the value's own types and
	// of the signatures about to be unified with it: a label the latter
	// introduce binds a name-only clause parameter that matched nothing so
	// far, so that clause then constrains the slot.
	var clauses, types []FuncType
	var match func(*Function) []int
	if v != nil {
		v = Unwrap(v)
	}
	switch v := v.(type) {
	case *FuncValue:
		if v.Fn == nil || v.IsPartial() {
			return nil
		}
		clauses = v.Types
		if v.Fn.Body == nil {
			clauses = append([]FuncType{{Fn: v.Fn, Env: v.Env}}, clauses...)
		}
		types = append(append(types, v.Types...), sigs...)
		match = func(f *Function) []int { return matchFuncParamsToValue(f, v.Fn, types) }
	case *Builtin:
		clauses = v.Types
		types = append(append(types, v.Types...), sigs...)
		match = func(f *Function) []int { return matchBuiltinParamsWith(f, v, types) }
	default:
		return nil
	}
	for _, s := range sigs {
		ms := match(s.Fn)
		for _, t := range clauses {
			if t.Fn.Body != nil {
				continue
			}
			mt := match(t.Fn)
			for k, j := range ms {
				if j < 0 {
					continue
				}
				for m, jt := range mt {
					if jt != j {
						continue
					}
					sp, tp := &s.Fn.Params[k], &t.Fn.Params[m]
					if hasCall(sp.Value) || hasCall(tp.Value) {
						continue // left to the call, see hasCall
					}
					if !c.covers(t.Env, tp.Value, s.Env, sp.Value) {
						return admissionErr(c, t.Fn, s.Fn,
							"parameter %s of the function supplied for parameter %s permits only %s, but parameter %s permits %s",
							funcParamName(c, m, tp), name, constraintString(c, tp.Value), name, constraintString(c, sp.Value))
					}
				}
			}
		}
	}
	return nil
}

// checkDefaultEntitlement checks the default that supplies the unbound
// parameter i of x, whether declared by the function or by an attached
// signature, as [OpContext.checkEntitlement] checks an argument.
func (c *OpContext) checkDefaultEntitlement(x *FuncValue, i int) *Bottom {
	if d := x.Fn.Params[i].Default; d != nil {
		if b := c.checkEntitlement(x, i, d, x.Env); b != nil {
			return b
		}
	}
	for _, t := range x.Types {
		for m, j := range matchFuncParamsToValue(t.Fn, x.Fn, x.Types) {
			if d := t.Fn.Params[m].Default; j == i && d != nil {
				if b := c.checkEntitlement(x, i, d, t.Env); b != nil {
					return b
				}
			}
		}
	}
	return nil
}

// paramSignatures returns the bodyless signatures that constrain parameter i
// of x: those in the parameter's own constraint and in the constraints of
// the parameters of x's attached types that match it.
func (c *OpContext) paramSignatures(x *FuncValue, i int) []FuncType {
	sigs := c.funcTypeConstraints(x.Env, x.Fn.Params[i].Value)
	for _, t := range x.Types {
		for m, j := range matchFuncParamsToValue(t.Fn, x.Fn, x.Types) {
			if j == i {
				sigs = append(sigs, c.funcTypeConstraints(t.Env, t.Fn.Params[m].Value)...)
			}
		}
	}
	return sigs
}

// funcTypeConstraints returns the bodyless signatures that the constraint x,
// compiled in env, imposes: a function literal without a body, the
// signatures of a function type a reference resolves to, or those of the
// operands of a conjunction. Any other constraint, such as a call producing
// a signature, is not evaluated here and imposes none for this check; the
// call enforces it. Evaluating it here would run before the call's own
// cycle detection is in place.
func (c *OpContext) funcTypeConstraints(env *Environment, x Expr) []FuncType {
	switch x := x.(type) {
	case *Function:
		if x.Body == nil {
			return []FuncType{{Fn: x, Env: env}}
		}
	case *BinaryExpr:
		if x.Op == AndOp {
			return append(c.funcTypeConstraints(env, x.X), c.funcTypeConstraints(env, x.Y)...)
		}
	case Resolver:
		if callFree(x) {
			return funcTypeSigs(c.resolvedFunc(env, x))
		}
	}
	return nil
}

// hasCall reports whether evaluating the constraint x may evaluate a call.
// Such a constraint is not compared at the binding (see
// [OpContext.checkEntitlement]): evaluating it there would run ahead of the
// enclosing call's cycle detection, so a constraint calling the function
// itself would recurse without limit. The call enforces it regardless. Only
// forms known not to call count as call-free: literals, kinds, bounds,
// operators, interpolations, disjunctions, struct and list literals without
// comprehensions, nested signatures, and call-free references composed of
// such parts; any other form is treated as if it could call.
func hasCall(x Node) bool {
	return !callFreeExpr(x)
}

func callFreeExpr(x Node) bool {
	switch x := x.(type) {
	case nil, *BasicType, *Top, *Null, *Bool, *Num, *String, *Bytes, *BoundValue, *Vertex:
		return true
	case Resolver:
		return callFree(x)
	case *BoundExpr:
		return callFreeExpr(x.Expr)
	case *UnaryExpr:
		return callFreeExpr(x.X)
	case *BinaryExpr:
		return callFreeExpr(x.X) && callFreeExpr(x.Y)
	case *Interpolation:
		for _, p := range x.Parts {
			if !callFreeExpr(p) {
				return false
			}
		}
		return true
	case *DisjunctionExpr:
		for _, d := range x.Values {
			if !callFreeExpr(d.Val) {
				return false
			}
		}
		return true
	case *Function:
		for _, p := range x.Params {
			if !callFreeExpr(p.Value) || !callFreeExpr(p.Default) {
				return false
			}
		}
		return callFreeExpr(x.Ret)
	case *StructLit:
		for _, d := range x.Decls {
			var es []Expr
			switch d := d.(type) {
			case *Field:
				es = []Expr{d.Value}
			case *LetField:
				es = []Expr{d.Value}
			case *Ellipsis:
				es = []Expr{d.Value}
			case *BulkOptionalField:
				es = []Expr{d.Filter, d.Value}
			case *DynamicField:
				es = []Expr{d.Key, d.Value}
			case Expr:
				es = []Expr{d}
			default:
				return false // a comprehension, or a form not known here
			}
			for _, e := range es {
				if !callFreeExpr(e) {
					return false
				}
			}
		}
		return true
	case *ListLit:
		for _, el := range x.Elems {
			switch el := el.(type) {
			case *Ellipsis:
				if !callFreeExpr(el.Value) {
					return false
				}
			case Expr:
				if !callFreeExpr(el) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	return false
}

// callFree reports whether resolving the reference x evaluates no call: a
// field, label, let, import, or value reference, possibly selected or
// indexed by a literal or another such reference. A reference through a
// call, such as f().x, is not inspected at the binding: the call would run
// ahead of the enclosing call's cycle detection, so a default or argument
// that reaches the function itself would recurse without limit. A reference
// to a field shares that field's vertex, whose evaluation the evaluator's
// ordinary cycle handling guards.
func callFree(x Node) bool {
	switch x := x.(type) {
	case *FieldReference, *LabelReference, *LetReference, *ImportReference, *ValueReference:
		return true
	case *SelectorExpr:
		return callFree(x.X)
	case *IndexExpr:
		return callFree(x.X) && callFree(x.Index)
	case *Num, *String:
		return true
	}
	return false
}

// funcTypeSigs returns the bodyless signatures of v if it is a function
// value, and nothing otherwise.
func funcTypeSigs(v Value) []FuncType {
	fv, ok := v.(*FuncValue)
	if !ok || fv.Fn == nil {
		return nil
	}
	var sigs []FuncType
	if fv.Fn.Body == nil {
		sigs = append(sigs, FuncType{Fn: fv.Fn, Env: fv.Env})
	}
	for _, t := range fv.Types {
		if t.Fn.Body == nil {
			sigs = append(sigs, t)
		}
	}
	return sigs
}

// suppliedFunc returns the function value or builtin that the argument arg,
// compiled in argEnv, supplies, or nil if it supplies none or none can be
// determined here. Only a literal and a reference are inspected, without
// evaluating anything else: an argument computed by a call, a conjunction,
// or a disjunction is left to the call, which enforces the parameter's
// signatures regardless. Evaluating such an argument at the binding would
// run before the call's own cycle detection is in place, so a default that
// calls the function itself would recurse without limit.
func (c *OpContext) suppliedFunc(argEnv *Environment, arg Expr) Value {
	switch x := arg.(type) {
	case *Function:
		return &FuncValue{Src: x.Src, Fn: x, Env: argEnv}
	case Resolver:
		if callFree(x) {
			return c.resolvedFunc(argEnv, x)
		}
	}
	return nil
}

// resolvedFunc resolves the reference x in env and returns the function
// value or builtin it holds, or nil for any other value or for a reference
// that cannot be resolved. Errors the resolution raises are not retained.
// The value may still be provisional: its clauses can only grow, so a
// provisional view can at most admit a binding that the call would reject.
func (c *OpContext) resolvedFunc(env *Environment, x Resolver) Value {
	savedErrs := c.errs
	c.errs = nil
	defer func() { c.errs = savedErrs }()
	v, _ := c.resolveState(MakeConjunct(env, x, c.ci), x, Flags{
		status:    partial,
		condition: concreteKnown,
		mode:      yield,
	})
	if v == nil {
		return nil
	}
	switch f := Unwrap(v).(type) {
	case *FuncValue:
		return f
	case *Builtin:
		return f
	}
	return nil
}

// covers reports whether the constraint a, compiled in enva, admits every
// value that the constraint b, compiled in envb, admits. A nil constraint is
// top. It reports false only when this is refuted (see [OpContext.admits]).
func (c *OpContext) covers(enva *Environment, a Expr, envb *Environment, b Expr) bool {
	if a == nil {
		return true
	}
	if k, ok := kindOnlyConstraint(a); ok {
		return c.kindCovers(k, envb, b)
	}
	va, ok := c.evalConstraint(enva, a)
	if !ok {
		return true
	}
	var vb Value = &Top{}
	if b != nil {
		if vb, ok = c.evalConstraint(envb, b); !ok {
			return true
		}
	}
	return c.admits(va, vb)
}

// kindCovers reports whether a constraint admitting every value of the kinds
// k admits every value that the constraint b, compiled in env, admits. A
// kind-only b reveals its kinds without evaluation. Any other b is evaluated
// and compared as [OpContext.admits] does, as it need not admit a value of
// each of its kinds.
func (c *OpContext) kindCovers(k Kind, env *Environment, b Expr) bool {
	if k == TopKind {
		return true
	}
	if b == nil {
		return false
	}
	if kb, ok := kindOnlyConstraint(b); ok {
		return kb&^k == 0
	}
	v, ok := c.evalConstraint(env, b)
	return !ok || c.admits(kindValue(k), v)
}

// admits reports whether the constraint value a admits every value that b
// admits, that is, whether a subsumes b. Only a proof that b admits a value
// that a rejects, with unification failing on a definite conflict, makes it
// report false. The proofs are:
//
//   - a refutes a disjunct of b.
//   - b admits a value of a kind that a excludes. A basic type, a numeric
//     bound, a validator of a definite kind, or a function is taken to admit
//     some value of each of its kinds. Any other constraint must be shown to
//     by a witness, as below, or for a struct or list by its fields or
//     elements (see [OpContext.admitsKind]).
//   - For two structs, a refutes b on a regular field both declare that no
//     other part of b refers to (see [OpContext.correlatedFields]).
//   - For two lists, a refutes b on an element at the same index or on an
//     element that a's element constraint governs, or b admits a length
//     that a forbids.
//   - For other values, a witness: a concrete value of b's kinds that b
//     admits and a rejects. The candidates are a few fixed values and the
//     values a and b mention, with their neighbors; unifying a candidate
//     with a constraint decides whether the constraint admits it.
//
// Any comparison these do not decide admits, for instance on a field that
// only one side declares, which unification adds or supplies, a pattern, or
// closedness. The outcomes of comparing pairs of structs or lists are
// remembered for the duration of the outermost call, so that values shared
// along many paths are compared once (see [admissionMemo]).
func (c *OpContext) admits(a, b Value) bool {
	if c.admission == nil {
		c.admission = &admissionMemo{
			pairs:      map[[2]*Vertex]bool{},
			inhabited:  map[*Vertex]bool{},
			correlated: map[*Vertex]map[Feature]bool{},
		}
		defer func() { c.admission = nil }()
	}
	return !c.refutes(a, b)
}

// An admissionMemo holds what one outermost [OpContext.admits] call has
// established about the structs and lists it reached: for each pair compared
// whether the first refutes the second, with a pair still being compared
// taken not to; for each value whether it admits some value; and for each
// struct which of its fields another part of it refers to.
type admissionMemo struct {
	pairs      map[[2]*Vertex]bool
	inhabited  map[*Vertex]bool
	correlated map[*Vertex]map[Feature]bool
}

// refutes reports whether one of the proofs of [OpContext.admits] shows that
// b admits a value that a rejects.
func (c *OpContext) refutes(a, b Value) bool {
	if a == b {
		return false
	}
	switch y := b.(type) {
	case *Bottom:
		return false // b admits no value
	case *Disjunction:
		for _, d := range y.Values {
			if c.refutes(a, Unwrap(d)) {
				return true
			}
		}
		return false
	}
	if k := b.Kind() &^ a.Kind(); k != 0 && c.admitsKind(b, k) {
		return true
	}
	switch x := a.(type) {
	case *Top, *BasicType:
		return false // a admits every value of its kinds
	case *FuncValue:
		return false
	case *Vertex:
		if y, ok := b.(*Vertex); ok {
			return c.refutesComposite(x, y)
		}
		return false
	}
	if _, ok := b.(*Vertex); ok || Equal(c, a, b, CheckStructural) {
		return false
	}
	return c.refutesByWitness(a, b)
}

// admitsKind reports whether b admits some value of one of the kinds ks, all
// of which are among b's kinds. A basic type, a numeric bound, a validator of
// a definite kind, or a function admits a value of each of its kinds. A
// struct or list admits a value if its fields or elements do (see
// [OpContext.inhabited]), and a disjunction if a disjunct does. Any other
// constraint must be shown to by a witness: a conjunction may admit no value
// of a kind it names, as >0 & <1 admits no integer; a bound on strings or
// bytes may admit nothing, as <"" does, and so may a regular expression; and
// a validator that applies to every kind, such as matchN, admits what its
// arguments say.
func (c *OpContext) admitsKind(b Value, ks Kind) bool {
	switch x := b.(type) {
	case *Top, *BasicType, *FuncValue, *Builtin:
		return true
	case *Vertex:
		return c.inhabited(x)
	case *Disjunction:
		for _, d := range x.Values {
			if d := Unwrap(d); d.Kind()&ks != 0 && c.admitsKind(d, d.Kind()&ks) {
				return true
			}
		}
		return false
	case *BoundValue:
		if x.Op != MatchOp && x.Op != NotMatchOp && x.Value.Kind()&(StringKind|BytesKind) == 0 {
			return true
		}
	case *BuiltinValidator:
		if x.Kind() != TopKind {
			return true
		}
	}
	for _, w := range witnessCandidates(b, b) {
		if w.Kind()&ks != 0 {
			if ok, _ := c.admitsConcrete(b, w); ok {
				return true
			}
		}
	}
	if ks&StructKind != 0 {
		if ok, _ := c.admitsConcrete(b, &StructLit{}); ok {
			return true
		}
	}
	if ks&ListKind != 0 {
		if ok, _ := c.admitsConcrete(b, &ListLit{}); ok {
			return true
		}
	}
	return false
}

// admitsAny reports whether v admits some value (see [OpContext.admitsKind]).
func (c *OpContext) admitsAny(v Value) bool {
	if _, ok := v.(*Bottom); ok {
		return false
	}
	return c.admitsKind(v, v.Kind())
}

// inhabited reports whether the struct or list v admits some value: every
// field that its values must have, and every element, admits one. A value
// reached again while it is being examined, through a reference cycle,
// counts as inhabited.
func (c *OpContext) inhabited(v *Vertex) bool {
	if r, seen := c.admission.inhabited[v]; seen {
		return r
	}
	c.admission.inhabited[v] = true
	r := true
	for _, a := range v.Arcs {
		if v.IsList() && !a.Label.IsInt() || !v.IsList() && (!a.Label.IsRegular() || a.ArcType > ArcRequired) {
			continue
		}
		if !c.admitsAny(Unwrap(a)) {
			r = false
			break
		}
	}
	c.admission.inhabited[v] = r
	return r
}

// refutesComposite reports whether the struct or list a refutes the struct
// or list b (see [OpContext.admits]). Each pair is compared once per
// outermost comparison, so that values shared along many paths are not
// compared once per path; a pair met again while it is being compared counts
// as not refuted, which can only admit more.
func (c *OpContext) refutesComposite(a, b *Vertex) bool {
	key := [2]*Vertex{a, b}
	if r, seen := c.admission.pairs[key]; seen {
		return r
	}
	c.admission.pairs[key] = false
	var r bool
	switch {
	case a.IsList() && b.IsList():
		r = c.refutesElems(a, b)
	case !a.IsList() && !b.IsList():
		r = c.refutesFields(a, b)
	}
	c.admission.pairs[key] = r
	return r
}

// refutesFields reports whether the struct a refutes the struct b on a
// regular field that both declare (see [OpContext.admits]). A field that
// another part of b refers to is not compared: a value it admits on its own
// need not extend to a value of b (see [OpContext.correlatedFields]).
func (c *OpContext) refutesFields(a, b *Vertex) bool {
	correlated := c.correlatedFields(b)
	for _, y := range b.Arcs {
		if !y.Label.IsRegular() || y.ArcType == ArcNotPresent || correlated[y.Label] {
			continue
		}
		x := a.LookupRaw(y.Label)
		if x == nil || x.ArcType == ArcNotPresent {
			continue
		}
		if c.refutes(Unwrap(x), Unwrap(y)) {
			return true
		}
	}
	return false
}

// correlatedFields returns the fields of s that an expression within s refers
// to. Another field's value, a comprehension, or a pattern constraint of s
// that mentions a field may restrict it beyond its own constraint, as
// b: 0 & a restricts a in {a: int, b: 0 & a}, so a value such a field admits
// on its own need not extend to a value of s. References are found in the
// values of s's fields, which are resolved in their own environments, and in
// the literals that s was built from, whose declarations are resolved in an
// environment holding s, following references to the structs embedded in s.
// Expressions in nested scopes resolve in an environment with an empty
// vertex, so that only references to s or beyond resolve; the nested
// scopes' own correlations are found when their values are compared.
func (c *OpContext) correlatedFields(s *Vertex) map[Feature]bool {
	if m, ok := c.admission.correlated[s]; ok {
		return m
	}
	m := map[Feature]bool{}
	c.admission.correlated[s] = m
	mark := func(v *Vertex) {
		for w := v; w != nil; w = w.Parent {
			if w.Parent == s {
				m[w.Label] = true
				return
			}
		}
	}
	resolve := func(env *Environment, r Resolver) bool {
		savedErrs := c.errs
		c.errs = nil
		v, _ := c.resolveState(MakeConjunct(env, r, c.ci), r, Flags{
			status:    partial,
			condition: concreteKnown,
			mode:      yield,
		})
		c.errs = savedErrs
		if v == nil {
			return false
		}
		mark(v)
		return true
	}
	var walk func(env *Environment, x Node)
	var decls func(env *Environment, ds []Decl)
	comprehension := func(env *Environment, x *Comprehension) {
		if x.Fallback != nil {
			walk(env, x.Fallback)
		}
		for _, cl := range x.Clauses {
			switch cl := cl.(type) {
			case *ForClause:
				walk(env, cl.Src)
				env = &Environment{Up: env, Vertex: &Vertex{}}
			case *LetClause:
				walk(env, cl.Expr)
				env = &Environment{Up: env, Vertex: &Vertex{}}
			case *IfClause:
				walk(env, cl.Condition)
			}
		}
		walk(env, x.Value)
	}
	decls = func(env *Environment, ds []Decl) {
		for _, d := range ds {
			switch d := d.(type) {
			case *Field:
				walk(env, d.Value)
			case *LetField:
				walk(env, d.Value)
			case *Ellipsis:
				walk(env, d.Value)
			case *BulkOptionalField:
				walk(env, d.Filter)
				walk(env, d.Value)
			case *DynamicField:
				walk(env, d.Key)
				walk(env, d.Value)
			case *Comprehension:
				comprehension(env, d)
			case Expr:
				walk(env, d)
			}
		}
	}
	walk = func(env *Environment, x Node) {
		switch x := x.(type) {
		case nil:
		case *SelectorExpr:
			if !resolve(env, x) {
				walk(env, x.X)
			}
		case *IndexExpr:
			if !resolve(env, x) {
				walk(env, x.X)
				walk(env, x.Index)
			}
		case Resolver:
			resolve(env, x)
		case *SliceExpr:
			walk(env, x.X)
			walk(env, x.Lo)
			walk(env, x.Hi)
			walk(env, x.Stride)
		case *BoundExpr:
			walk(env, x.Expr)
		case *UnaryExpr:
			walk(env, x.X)
		case *BinaryExpr:
			walk(env, x.X)
			walk(env, x.Y)
		case *Interpolation:
			for _, p := range x.Parts {
				walk(env, p)
			}
		case *DisjunctionExpr:
			for _, d := range x.Values {
				walk(env, d.Val)
			}
		case *CallExpr:
			walk(env, x.Fun)
			for _, a := range x.Args {
				walk(env, a)
			}
		case *Function:
			for _, p := range x.Params {
				walk(env, p.Value)
				walk(env, p.Default)
			}
			walk(env, x.Ret)
			walk(&Environment{Up: env, Vertex: &Vertex{}}, x.Body)
		case *StructLit:
			decls(&Environment{Up: env, Vertex: &Vertex{}}, x.Decls)
		case *ListLit:
			inner := &Environment{Up: env, Vertex: &Vertex{}}
			for _, e := range x.Elems {
				switch e := e.(type) {
				case *Ellipsis:
					walk(inner, e.Value)
				case *Comprehension:
					comprehension(inner, e)
				case Expr:
					walk(inner, e)
				}
			}
		}
	}
	// The literals s was built from, directly or through the references
	// among its conjuncts, declare their fields in s.
	seen := map[*Vertex]bool{s: true}
	var literals func(v *Vertex)
	literals = func(v *Vertex) {
		for x := range v.LeafConjuncts() {
			switch e := x.Expr().(type) {
			case *StructLit:
				decls(&Environment{Up: x.Env, Vertex: s}, e.Decls)
			case Resolver:
				savedErrs := c.errs
				c.errs = nil
				r, _ := c.resolveState(MakeConjunct(x.Env, e, c.ci), e, Flags{
					status:    partial,
					condition: concreteKnown,
					mode:      yield,
				})
				c.errs = savedErrs
				if r = r.DerefValue(); r != nil && !seen[r] {
					seen[r] = true
					literals(r)
				}
			}
		}
	}
	literals(s)
	for _, a := range s.Arcs {
		for x := range a.LeafConjuncts() {
			walk(x.Env, x.Expr())
		}
	}
	return m
}

// refutesElems reports whether the list a refutes the list b (see
// [OpContext.admits]). The lengths refute it when b admits a list longer than
// a closed a admits, or when a closed b admits only lists shorter than a
// requires, provided that b admits a list at all. Otherwise an element of b
// is compared with the element of a at its index, or, beyond a's elements,
// with a's element constraint, which in turn is compared with b's element
// constraint beyond b's elements.
func (c *OpContext) refutesElems(a, b *Vertex) bool {
	xs := slices.Collect(a.Elems())
	ys := slices.Collect(b.Elems())
	for _, y := range ys {
		if !c.admitsAny(Unwrap(y)) {
			return false // b admits no list
		}
	}
	var tb Value // b's element constraint beyond its elements, if any
	if !b.IsClosedList() {
		if tb = c.listTail(b); !c.admitsAny(tb) {
			tb = nil // no further element is admitted
		}
	}
	switch {
	case tb == nil && len(ys) < len(xs):
		return true
	case a.IsClosedList():
		if len(ys) > len(xs) || tb != nil {
			return true
		}
	default:
		ta := c.listTail(a)
		for _, y := range ys[min(len(xs), len(ys)):] {
			if c.refutes(ta, Unwrap(y)) {
				return true
			}
		}
		if tb != nil && c.refutes(ta, tb) {
			return true
		}
	}
	for i := range min(len(xs), len(ys)) {
		if c.refutes(Unwrap(xs[i]), Unwrap(ys[i])) {
			return true
		}
	}
	return false
}

// listTail returns the constraint that the open list v places on elements
// beyond its own: top if it places none, and bottom if it admits none.
func (c *OpContext) listTail(v *Vertex) Value {
	w := &Vertex{Label: AnyIndex}
	v.MatchAndInsert(c, w)
	if !w.HasConjuncts() {
		return &Top{}
	}
	w.Finalize(c)
	return Unwrap(w)
}

// maxWitnesses bounds the number of candidate values that
// [OpContext.refutesByWitness] tries.
const maxWitnesses = 48

// refutesByWitness reports whether some concrete value of b's kinds is
// admitted by b and rejected by a (see [OpContext.admits]). A concrete b is
// its own only candidate.
func (c *OpContext) refutesByWitness(a, b Value) bool {
	if isConcreteScalar(b) {
		ok, decided := c.admitsConcrete(a, b)
		return !ok && decided
	}
	for _, w := range witnessCandidates(a, b) {
		if ok, decided := c.admitsConcrete(a, w); ok || !decided {
			continue
		}
		if ok, _ := c.admitsConcrete(b, w); ok {
			return true
		}
	}
	return false
}

// admitsConcrete reports whether the constraint value v admits the concrete
// value w, and whether that is decided: an incomplete unification is not. For
// a scalar w, a kind, a concrete value, or a disjunction of concrete values
// decides without unifying.
func (c *OpContext) admitsConcrete(v Value, w Expr) (ok, decided bool) {
	if w, scalar := w.(Value); scalar && isConcreteScalar(w) {
		switch x := v.(type) {
		case *Top:
			return true, true
		case *BasicType:
			return w.Kind()&^x.K == 0, true
		case *Disjunction:
			if !slices.ContainsFunc(x.Values, func(y Value) bool { return !isConcreteScalar(Unwrap(y)) }) {
				return slices.ContainsFunc(x.Values, func(y Value) bool {
					return Equal(c, Unwrap(y), w, CheckStructural)
				}), true
			}
		}
		if isConcreteScalar(v) {
			return Equal(c, v, w, CheckStructural), true
		}
	}
	savedErrs := c.errs
	c.errs = nil
	defer func() { c.errs = savedErrs }()
	n := c.newInlineVertex(nil, nil, MakeRootConjunct(nil, v), MakeRootConjunct(nil, w))
	if b := n.Err(c); b != nil {
		return false, !b.IsIncomplete()
	}
	return true, true
}

// isConcreteScalar reports whether v is a concrete value other than a struct,
// list, or function.
func isConcreteScalar(v Value) bool {
	switch v.(type) {
	case *Null, *Bool, *Num, *String, *Bytes:
		return true
	}
	return false
}

// witnessCandidates returns the concrete values of b's kinds that
// [OpContext.refutesByWitness] tries for a b that is not concrete: the
// numbers, strings, and bytes that a and b mention with their neighbors, and
// a few fixed values of each kind.
func witnessCandidates(a, b Value) []Value {
	k := b.Kind()
	var ws []Value
	seen := map[string]bool{}
	add := func(v Value) {
		var key string
		switch x := v.(type) {
		case *Num:
			key = x.K.String() + ":" + x.X.String()
		case *String:
			key = "s:" + x.Str
		case *Bytes:
			key = "b:" + string(x.B)
		case *Bool:
			key = fmt.Sprint(x.B)
		case *Null:
			key = "null"
		}
		if len(ws) < maxWitnesses && v.Kind()&^k == 0 && !seen[key] {
			seen[key] = true
			ws = append(ws, v)
		}
	}
	var nums []*Num
	var strs []string
	var bytes [][]byte
	var mention func(v Value)
	mention = func(v Value) {
		switch x := Unwrap(v).(type) {
		case *Num:
			nums = append(nums, x)
		case *String:
			strs = append(strs, x.Str)
		case *Bytes:
			bytes = append(bytes, x.B)
		case *BoundValue:
			mention(x.Value)
		case *Conjunction:
			for _, y := range x.Values {
				mention(y)
			}
		case *Disjunction:
			for _, y := range x.Values {
				mention(y)
			}
		case *BuiltinValidator:
			for _, y := range x.Args {
				mention(y)
			}
		}
	}
	mention(a)
	mention(b)
	one := &Num{K: IntKind, X: *apd.New(1, 0)}
	half := &Num{K: FloatKind, X: *apd.New(5, -1)}
	for _, n := range nums {
		if k&IntKind != 0 {
			var f apd.Decimal
			if _, err := internal.ExactContext.Floor(&f, &n.X); err == nil {
				lo := &Num{K: IntKind, X: f}
				add(lo)
				if v, err := NumSub(lo, one); err == nil {
					add(v)
				}
				if v, err := NumAdd(lo, one); err == nil {
					add(v)
					if v, err := NumAdd(v, one); err == nil {
						add(v)
					}
				}
			}
		}
		if k&FloatKind != 0 {
			f := &Num{K: FloatKind, X: n.X}
			add(f)
			if v, err := NumSub(f, half); err == nil {
				add(v)
			}
			if v, err := NumAdd(f, half); err == nil {
				add(v)
			}
		}
	}
	all := ""
	for _, s := range strs {
		add(&String{Str: s})
		add(&String{Str: s + "\x00"})
		if s != "" {
			add(&String{Str: s[:len(s)-1]})
		}
		all += s
	}
	if len(strs) > 0 {
		add(&String{Str: all + "_"})
	}
	var allBytes []byte
	for _, s := range bytes {
		add(&Bytes{B: s})
		add(&Bytes{B: append(slices.Clip(s), 0)})
		if len(s) > 0 {
			add(&Bytes{B: s[:len(s)-1]})
		}
		allBytes = append(allBytes, s...)
	}
	if len(bytes) > 0 {
		add(&Bytes{B: append(allBytes, '_')})
	}
	add(&Null{})
	add(&Bool{B: false})
	add(&Bool{B: true})
	for _, i := range []int64{0, 1, -1} {
		add(&Num{K: IntKind, X: *apd.New(i, 0)})
	}
	for _, f := range []int64{0, 5, -5} {
		add(&Num{K: FloatKind, X: *apd.New(f, -1)})
	}
	for _, s := range []string{"", "a", "b", "z", "A", "0", " ", "_"} {
		add(&String{Str: s})
		add(&Bytes{B: []byte(s)})
	}
	return ws
}

// builtinParamCovers reports whether the builtin parameter value pv admits
// every value that the signature constraint x, compiled in env, admits.
func (c *OpContext) builtinParamCovers(pv Value, env *Environment, x Expr) bool {
	if k, ok := kindOnlyConstraint(x); ok {
		return c.admits(pv, kindValue(k))
	}
	v, ok := c.evalConstraint(env, x)
	return !ok || c.admits(pv, v)
}

// builtinResultCovered reports whether the signature result ret, compiled in
// env, admits every value of a builtin's declared result rv (see
// [Builtin.DeclaredResult]). A builtin usually declares its result at the level
// of kinds only, so a structural constraint of a fitting kind, such as a list
// of strings over a builtin returning a list, is not refuted: the builtin's
// real result may well be narrower than its kind.
func (c *OpContext) builtinResultCovered(env *Environment, ret Expr, rv Value) bool {
	k := rv.Kind()
	if kr, ok := kindOnlyConstraint(ret); ok {
		return k&^kr == 0
	}
	v, ok := c.evalConstraint(env, ret)
	if !ok {
		return true
	}
	switch x := v.(type) {
	case *Top:
		return true
	case *BasicType:
		return k&^x.K == 0
	case *Vertex:
		return k&^x.Kind() == 0
	}
	return c.admits(v, rv)
}

// DeclaredResult returns the builtin's declared result: ResultValue when
// set, else the value admitting exactly the kind Result. It is exported for
// use by internal/core/subsume.
func (x *Builtin) DeclaredResult() Value {
	if x.ResultValue != nil {
		return x.ResultValue
	}
	return kindValue(x.Result)
}

// evalConstraint evaluates the signature constraint x in env to a complete
// value, with any struct fields or list elements evaluated, and reports
// whether it is free of errors. Errors it raises are not retained: a
// constraint that cannot be evaluated here is left to the call.
func (c *OpContext) evalConstraint(env *Environment, x Expr) (Value, bool) {
	if c.pendingConstraint(env, x) {
		return nil, false
	}
	savedErrs := c.errs
	c.errs = nil
	defer func() { c.errs = savedErrs }()
	s := c.PushState(env, x.Source())
	defer c.PopState(s)
	v := c.newInlineVertex(nil, nil, MakeConjunct(env, x, c.ci))
	v.Finalize(c)
	if v.Bottom() != nil || c.errs != nil {
		return nil, false
	}
	return Unwrap(v), true
}

// needsAdmission reports whether head and types combine an implementation
// with a bodyless signature, so that there is a declaration to check.
func needsAdmission(head FuncType, types []FuncType) bool {
	impl, sig := head.Fn.Body != nil, false
	for _, t := range types {
		if t.Fn.Body != nil {
			impl = true
		} else {
			sig = true
		}
	}
	return impl && sig
}

// pendingConstraint reports whether the constraint x, compiled in env, refers
// to a value that may still be refined, for instance by a comprehension whose
// condition depends on the function value being formed. Such a constraint
// leaves the check undone rather than rejecting on a provisional value;
// calls enforce the constraint regardless. References are found in the
// constraint itself, in the operands of its operators, bounds, disjunctions,
// interpolations, slices, and calls, in the signature and body of a function
// literal, such as that of a function the constraint calls, and in the
// fields, lets, and elements of struct and list
// literals, whose expressions are compiled in the literal's own scope: they
// are inspected in a scope whose vertex is empty, so that only references
// escaping the literal resolve. A comprehension in a literal counts as
// pending, as its clauses have scopes of their own. Whether a referenced value
// may still be refined is decided by [OpContext.unsettled].
func (c *OpContext) pendingConstraint(env *Environment, x Expr) bool {
	return c.pendingAt(env, x, map[*Vertex]bool{})
}

func (c *OpContext) pendingAt(env *Environment, x Expr, seen map[*Vertex]bool) bool {
	switch x := x.(type) {
	case Resolver:
		savedErrs := c.errs
		c.errs = nil
		v, _ := c.resolveState(MakeConjunct(env, x, c.ci), x, Flags{
			status:    partial,
			condition: concreteKnown,
			mode:      yield,
		})
		c.errs = savedErrs
		return v != nil && c.unsettled(v, seen)
	case *BinaryExpr:
		return c.pendingAt(env, x.X, seen) || c.pendingAt(env, x.Y, seen)
	case *UnaryExpr:
		return c.pendingAt(env, x.X, seen)
	case *BoundExpr:
		return c.pendingAt(env, x.Expr, seen)
	case *Interpolation:
		for _, p := range x.Parts {
			if c.pendingAt(env, p, seen) {
				return true
			}
		}
	case *SliceExpr:
		for _, e := range []Expr{x.X, x.Lo, x.Hi, x.Stride} {
			if e != nil && c.pendingAt(env, e, seen) {
				return true
			}
		}
	case *DisjunctionExpr:
		for _, d := range x.Values {
			if c.pendingAt(env, d.Val, seen) {
				return true
			}
		}
	case *CallExpr:
		if c.pendingAt(env, x.Fun, seen) {
			return true
		}
		for _, a := range x.Args {
			if c.pendingAt(env, a, seen) {
				return true
			}
		}
	case *StructLit:
		inner := &Environment{Up: env, Vertex: &Vertex{}}
		for _, d := range x.Decls {
			var es []Expr
			switch d := d.(type) {
			case *Field:
				es = []Expr{d.Value}
			case *LetField:
				es = []Expr{d.Value}
			case *Ellipsis:
				es = []Expr{d.Value}
			case *BulkOptionalField:
				es = []Expr{d.Filter, d.Value}
			case *DynamicField:
				es = []Expr{d.Key, d.Value}
			case *Comprehension:
				return true
			case Expr:
				es = []Expr{d}
			}
			for _, e := range es {
				if e != nil && c.pendingAt(inner, e, seen) {
					return true
				}
			}
		}
	case *ListLit:
		inner := &Environment{Up: env, Vertex: &Vertex{}}
		for _, el := range x.Elems {
			var e Expr
			switch el := el.(type) {
			case *Ellipsis:
				e = el.Value
			case *Comprehension:
				return true
			case Expr:
				e = el
			}
			if e != nil && c.pendingAt(inner, e, seen) {
				return true
			}
		}
	case *Function:
		for _, p := range x.Params {
			if p.Value != nil && c.pendingAt(env, p.Value, seen) ||
				p.Default != nil && c.pendingAt(env, p.Default, seen) {
				return true
			}
		}
		if x.Ret != nil && c.pendingAt(env, x.Ret, seen) {
			return true
		}
		return x.Body != nil && c.pendingAt(&Environment{Up: env, Vertex: &Vertex{}}, x.Body, seen)
	case *Vertex:
		return c.unsettled(x, seen)
	}
	return false
}

// unsettled reports whether the value of v may still be refined: v is not
// finalized, and conjuncts may still be added to it or one of its conjuncts
// refers to such a value. A value whose conjuncts are all known is settled
// however far its evaluation has progressed, so that whether a constraint is
// pending does not depend on the order in which fields are evaluated. seen
// records the values inspected so far, which a reference cycle reaches again.
func (c *OpContext) unsettled(v *Vertex, seen map[*Vertex]bool) bool {
	v = v.DerefValue()
	if v.status == finalized || seen[v] {
		return false
	}
	seen[v] = true
	if !conjunctsKnown(c, v) {
		return true
	}
	for x := range v.LeafConjuncts() {
		if c.pendingAt(x.Env, x.Expr(), seen) {
			return true
		}
	}
	return false
}

// conjunctsKnown reports whether no further conjuncts can be added to v: no
// comprehension that may add some to v or to one of its ancestors is pending,
// and the fields of v's parent have all their conjuncts (see
// [Vertex.allChildConjunctsKnown]).
func conjunctsKnown(c *OpContext, v *Vertex) bool {
	for w := v; w != nil; w = w.Parent {
		if s := w.state; s != nil && w.status != finalized && s.hasActiveParentTask() {
			return false
		}
	}
	return v.Parent.allChildConjunctsKnown(c)
}

// admissionErr reports a declared-compatibility failure at the position of
// the implementation's signature impl, adding that of the bodyless signature
// sig when given.
func admissionErr(c *OpContext, impl, sig Node, format string, args ...interface{}) *Bottom {
	err := c.NewPosf(Pos(impl), format, args...)
	if sig != nil {
		err.AddPosition(sig)
	}
	return &Bottom{Src: impl.Source(), Code: EvalError, Err: err}
}

// kindValue returns the value admitting exactly the values of kind k.
func kindValue(k Kind) Value {
	if k == TopKind {
		return &Top{}
	}
	return &BasicType{K: k}
}

func constraintString(c *OpContext, x Expr) string {
	if x == nil {
		return "_"
	}
	return c.Str(x).String()
}

// ExtraFuncParam returns the first non-omittable parameter of val that is
// not addressable through the closed signature typ — by label for name-only
// parameters or by ordinal for positional parameters — and reports whether
// such a parameter exists. An omittable parameter never qualifies: calls
// through typ can never bind it, but no call needs it either. A parameter
// is omittable when it is optional or declares a default itself; valTypes,
// if not nil, are signatures attached to val whose defaults count as well.
//
// The helper implements the closedness side of the one rule governing extra
// parameters (see the file comment) and is shared between function
// tightening and subsumption (internal/core/subsume). Both pass nil for
// valTypes: admission beyond a closed type depends on the value's own
// declaration, so that it does not depend on the order in which signatures
// are attached (see [checkFuncTightening]), and subsumption agrees with
// unification on it.
func ExtraFuncParam(typ, val *Function, valTypes []FuncType) (FuncParam, bool) {
	matched := make([]bool, len(val.Params))
	for _, j := range matchFuncParams(typ, val, false) {
		if j >= 0 {
			matched[j] = true
		}
	}
	for i, p := range val.Params {
		if matched[i] || p.ArcType == ArcOptional || FuncParamHasDefault(val, valTypes, i) {
			continue
		}
		return p, true
	}
	return FuncParam{}, false
}

// funcParamLabels returns the callable labels of a native function value.
// The value's own plain and name-only labels bind directly. A plain label of
// an attached signature names the otherwise unnamed value parameter that the
// signature matched, without changing the implementation's parameter list or
// local bindings. Compatibility checks ensure that a positional slot has at
// most one contract label.
//
// A label contributed for different parameter indexes is ambiguous and maps
// to -1. Such a label binds no argument, matching the treatment of conflicting
// contract labels on builtins in [bindBuiltinArgs].
func funcParamLabels(fn *Function, types []FuncType) (map[Feature]int, Feature) {
	byLabel := make(map[Feature]int)
	ambiguous := InvalidLabel
	add := func(label Feature, pos int) {
		if label == InvalidLabel {
			return
		}
		if prev, ok := byLabel[label]; ok && prev != pos {
			byLabel[label] = -1
			if ambiguous == InvalidLabel {
				ambiguous = label
			}
		} else if !ok {
			byLabel[label] = pos
		}
	}
	for i, p := range fn.Params {
		add(p.Label, i)
	}
	for _, t := range types {
		matches := matchFuncParams(t.Fn, fn, false)
		for i, p := range t.Fn.Params {
			if p.Positional && matches[i] >= 0 {
				add(p.Label, matches[i])
			}
		}
	}
	return byLabel, ambiguous
}

// matchFuncParamsToValue maps a signature's parameters to the concrete
// parameter indexes of a function value. Positional parameters map by
// ordinal. Name-only parameters map through the value's complete callable
// label surface, so a contract label contributed by a sibling attached
// signature also carries a matching name-only constraint to the same slot.
//
// As in [matchFuncParams], one destination parameter may be selected at most
// once by a given source signature.
func matchFuncParamsToValue(x, fn *Function, types []FuncType) []int {
	byLabel, _ := funcParamLabels(fn, types)
	matches := make([]int, len(x.Params))
	for i := range matches {
		matches[i] = -1
	}
	used := make([]bool, len(fn.Params))
	pos := 0
	for i, p := range x.Params {
		j := -1
		if p.Positional {
			_, j, _ = MatchFuncParam(fn, InvalidLabel, pos)
			pos++
		} else if p.Label != InvalidLabel {
			var ok bool
			j, ok = byLabel[p.Label]
			if !ok {
				j = -1
			}
		}
		if j < 0 || used[j] {
			continue
		}
		matches[i] = j
		used[j] = true
	}
	return matches
}

// MatchFuncValueParams maps every parameter of x to the concrete parameter
// index it selects on v, including through the contract label contributed by
// an attached signature. It is exported for structural subsumption checks.
func MatchFuncValueParams(x *Function, v *FuncValue) []int {
	return matchFuncParamsToValue(x, v.Fn, v.Types)
}

// funcPositionalLabelConflict reports the first violation of the one-to-one
// relation between plain contract labels and positional ordinals across fn and
// types. It scans signature ordinals rather than only parameters materialized
// by fn, so conflicts in extra positions of an open signature are rejected
// before a later concrete value supplies them.
func funcPositionalLabelConflict(fn *Function, types []FuncType) (a, b Feature, pos int) {
	byLabel := make(map[Feature]int)
	byPos := make(map[int]Feature)
	conflictA := InvalidLabel
	conflictB := InvalidLabel
	conflictPos := -1
	add := func(x *Function) {
		pos := 0
		for _, p := range x.Params {
			if !p.Positional {
				continue
			}
			if p.Label != InvalidLabel {
				if prev, ok := byLabel[p.Label]; ok && prev != pos {
					if conflictA == InvalidLabel {
						conflictA = p.Label
						conflictPos = -1
					}
				} else if !ok {
					byLabel[p.Label] = pos
				}
				if prev, ok := byPos[pos]; ok && prev != p.Label {
					if conflictA == InvalidLabel {
						conflictA = prev
						conflictB = p.Label
						conflictPos = pos
					}
				} else if !ok {
					byPos[pos] = p.Label
				}
			}
			pos++
		}
	}
	add(fn)
	for _, t := range types {
		add(t.Fn)
	}
	return conflictA, conflictB, conflictPos
}

// checkFuncParamLabels rejects a meet that would assign different contract
// labels to one positional parameter or one label to different parameters.
func checkFuncParamLabels(c *OpContext, fn *Function, types []FuncType) *Bottom {
	// Positional ordinals are representation-independent, including for a
	// meet of bodyless types whose chosen head depends on operand order. Always
	// validate both directions of the label-to-ordinal relation.
	a, b, pos := funcPositionalLabelConflict(fn, types)
	if b != InvalidLabel {
		return c.NewErrf("conflicting parameter labels %s and %s for positional parameter %d",
			a.SelectorString(c), b.SelectorString(c), pos+1)
	}
	ambiguous := a
	if fn.Body != nil {
		// A concrete function additionally has a materialized parameter list.
		// Check attached contract labels against all of its own labels,
		// including name-only parameters. A bodyless type cannot use this check:
		// an unmatched optional name-only parameter in its chosen head is not a
		// concrete slot, and treating its slice index as one makes type meets
		// operand-order dependent.
		_, concreteAmbiguous := funcParamLabels(fn, types)
		if concreteAmbiguous != InvalidLabel {
			ambiguous = concreteAmbiguous
		}
	}
	if ambiguous == InvalidLabel {
		return nil
	}
	return c.NewErrf("ambiguous parameter label %s refers to multiple positions",
		ambiguous.SelectorString(c))
}

// FuncParamLabelIndex reports the parameter index selected by label on v's
// effective callable interface, including a contract label contributed by an
// attached signature. It is exported for structural subsumption checks.
func FuncParamLabelIndex(v *FuncValue, label Feature) (int, bool) {
	byLabel, _ := funcParamLabels(v.Fn, v.Types)
	i, ok := byLabel[label]
	return i, ok && i >= 0
}

// anonParamLabel returns the label of the synthetic activation arc that
// binds the argument of the anonymous positional parameter at index i of a
// function's parameter list. Anonymous parameters are not referenceable
// from the function body — no compiled reference carries this label — so
// the label only needs to be unique within one activation: it is a string
// label, which no identifier reference can produce. The arc exists so that
// the parameter's own constraint and any type constraints matched to the
// parameter are enforced on the argument of every call; its name surfaces
// in error paths for a failing argument.
//
// Deriving the label interns a string through the runtime's global index
// lock, so the labels are cached on the OpContext, indexed by parameter
// position: they do not depend on which function is being called.
func anonParamLabel(c *OpContext, i int) Feature {
	for len(c.anonParamLabels) <= i {
		c.anonParamLabels = append(c.anonParamLabels,
			MakeStringLabel(c, fmt.Sprintf("arg%d", len(c.anonParamLabels))))
	}
	return c.anonParamLabels[i]
}

// staticKind returns the kind of a compiled constraint expression when it is
// statically decidable, i.e. when the expression is literally a basic type.
func staticKind(x Expr) (Kind, bool) {
	if t, ok := x.(*BasicType); ok {
		return t.K, true
	}
	return BottomKind, false
}

// kindOnlyConstraint reports the kind a constraint restricts its value to,
// when restricting the kind is all it does. A basic type qualifies by
// definition, and so does an open list whose element constraint is top, as it
// admits every list. Anything that restricts more than the kind — a bound, a
// struct, a list with element constraints or a fixed length — does not, and
// the caller must enforce it.
//
// This is what makes a signature constraint skippable at call time: a builtin
// already knows the kind of each parameter and of its result, and
// [CheckBuiltinTightening] verified those kinds against the type when the two
// were unified. Re-checking a kind-only constraint against the value can only
// succeed, but doing so materializes the value — which for a large list means
// building a vertex per element.
func kindOnlyConstraint(x Expr) (Kind, bool) {
	switch t := x.(type) {
	case *BasicType:
		return t.K, true

	case *Top:
		// `_` admits everything, so it constrains nothing at all.
		return TopKind, true

	case *DisjunctionExpr:
		// A disjunction of kind-only constraints without defaults, such as
		// `bytes | string`, restricts only to the union of the kinds.
		var k Kind
		for _, v := range t.Values {
			if v.Default {
				return BottomKind, false
			}
			vk, ok := kindOnlyConstraint(v.Val)
			if !ok {
				return BottomKind, false
			}
			k |= vk
		}
		return k, true

	case *ListLit:
		// `[...T]` restricts only the kind exactly when T admits everything:
		// `[..._]` and `[...]` do, `[...int]` does not.
		if len(t.Elems) != 1 {
			return BottomKind, false
		}
		e, ok := t.Elems[0].(*Ellipsis)
		if !ok {
			return BottomKind, false
		}
		switch v := e.Value.(type) {
		case nil:
			return ListKind, true
		case *Top:
			return ListKind, true
		case *BasicType:
			if v.K == TopKind {
				return ListKind, true
			}
		}

	case *StructLit:
		// `{...}` — an open struct with no other declarations — admits every
		// struct, so it restricts only the kind.
		if len(t.Decls) == 1 {
			if _, ok := t.Decls[0].(*Ellipsis); ok {
				return StructKind, true
			}
		}
	}
	return BottomKind, false
}

// CheckBuiltinTightening verifies that builtin b may be tightened by the
// function type typ. It is exported for use by internal/core/subsume, which
// uses the same static compatibility check as the first step of proving
// whether a function type subsumes a builtin.
//
// A builtin takes its arguments positionally, so a type's parameters align
// with the builtin's raw slots by position. A plain parameter such as
// `list: [..._]` is both named and positional, so its name supplies a contract
// label without changing the raw ABI slot. Compatible attached signatures
// must agree on that label. A parameter that can only be passed by label —
// one marked required (a!) or optional (a?) — does not supply a positional
// match itself; it is the builtin parameter that carries its label. The
// static checks are limited to what is decidable from the compiled signature
// and the builtin's shape:
//
//   - a name-only parameter of the type matches the builtin parameter with its
//     label, as named by the signatures already unified with the builtin. One
//     whose label names no builtin parameter is rejected unless it is
//     optional (a?): it then contributes no parameter or label, but no call
//     needs it either;
//   - a type parameter beyond the builtin's parameter count is rejected;
//   - unless the type is open, a defaultless builtin parameter that the type
//     covers neither by position nor by label is rejected, as calls through
//     the type could never provide it. A builtin parameter with a default of
//     its own is admitted instead;
//   - a parameter or result constraint that is literally a basic type
//     disjoint with the builtin's parameter or result kind is rejected.
//
// All other constraints are enforced dynamically, per call (see
// [Builtin.rawCall]).
func CheckBuiltinTightening(c *OpContext, typ *Function, b *Builtin) *Bottom {
	return checkBuiltinTightening(c, typ, b, b.Types)
}

// checkBuiltinTightening is [CheckBuiltinTightening] with the labels of the
// builtin's parameters taken from types, the signatures unified with it so far.
//
// The signatures of a builtin, including the one generated for a
// standard-library function, may be unified with it in any order. Until one of
// them labels the builtin's parameters, a name-only parameter cannot be
// resolved, and it is left pending rather than rejected; the check is repeated
// with all labels as further signatures arrive (see [checkBuiltinSignatures]).
func checkBuiltinTightening(c *OpContext, typ *Function, b *Builtin, types []FuncType) *Bottom {
	byLabel, _ := builtinParamLabels(types)
	labelsKnown := len(byLabel) > 0
	pending := false
	covered := make([]bool, len(b.Params))
	checkKind := func(p FuncParam, i int) *Bottom {
		if p.Value != nil {
			if k, ok := staticKind(p.Value); ok && k&b.Params[i].Kind() == BottomKind {
				return c.NewErrf("parameter %d of function type has kind %s, conflicting with kind %s of builtin %s",
					i+1, k, b.Params[i].Kind(), b.qualifiedName(c))
			}
		}
		return nil
	}
	pos := 0
	for _, p := range typ.Params {
		if !p.Positional {
			if j, ok := byLabel[p.Label]; ok && j >= 0 && j < len(b.Params) {
				// The builtin parameter with this label: the same parameter.
				if err := checkKind(p, j); err != nil {
					return err
				}
				covered[j] = true
				continue
			}
			if !labelsKnown {
				pending = true
				continue
			}
			if p.ArcType == ArcOptional {
				// An optional parameter that matches nothing is admitted:
				// no call needs it.
				continue
			}
			return c.NewErrf("parameter %s of function type not allowed by builtin %s: no parameter of the builtin has this label",
				p.Label.SelectorString(c), b.qualifiedName(c))
		}
		if pos >= len(b.Params) {
			return c.NewErrf("function type has more positional parameters than builtin %s",
				b.qualifiedName(c))
		}
		if err := checkKind(p, pos); err != nil {
			return err
		}
		covered[pos] = true
		pos++
	}
	if !typ.Open && !pending {
		for i := range b.Params {
			if !covered[i] && b.Params[i].Default() == nil {
				return c.NewErrf("builtin %s has more parameters than closed function type",
					b.qualifiedName(c))
			}
		}
	}
	if typ.Ret != nil && b.Result != BottomKind {
		if k, ok := staticKind(typ.Ret); ok && k&b.Result == BottomKind {
			return c.NewErrf("result of function type has kind %s, conflicting with result kind %s of builtin %s",
				k, b.Result, b.qualifiedName(c))
		}
	}
	return nil
}

// builtinParamLabels returns the parameter labels contributed by all attached
// builtin signatures. Labels name raw builtin positions. A label attached to
// more than one position is ambiguous and maps to -1; compatibility checks
// ensure that each position has at most one contract label.
func builtinParamLabels(types []FuncType) (map[Feature]int, Feature) {
	byLabel := make(map[Feature]int)
	ambiguous := InvalidLabel
	for _, t := range types {
		pos := 0
		for _, p := range t.Fn.Params {
			if !p.Positional {
				continue
			}
			if p.Label != InvalidLabel {
				if prev, ok := byLabel[p.Label]; ok && prev != pos {
					byLabel[p.Label] = -1
					if ambiguous == InvalidLabel {
						ambiguous = p.Label
					}
				} else if !ok {
					byLabel[p.Label] = pos
				}
			}
			pos++
		}
	}
	return byLabel, ambiguous
}

// matchBuiltinParams maps a signature's parameters to raw builtin positions.
// Positional parameters map by ordinal. A name-only parameter maps only when
// another attached positional signature exposes the same contract label for a
// raw position. One source signature cannot map two declarations to one raw
// position.
func matchBuiltinParams(x *Function, b *Builtin) []int {
	return matchBuiltinParamsWith(x, b, b.Types)
}

// matchBuiltinParamsWith is [matchBuiltinParams] with the labels taken from
// types instead of the signatures recorded on b.
func matchBuiltinParamsWith(x *Function, b *Builtin, types []FuncType) []int {
	byLabel, _ := builtinParamLabels(types)
	matches := make([]int, len(x.Params))
	for i := range matches {
		matches[i] = -1
	}
	used := make([]bool, len(b.Params))
	pos := 0
	for i, p := range x.Params {
		j := -1
		if p.Positional {
			j = pos
			pos++
		} else if p.Label != InvalidLabel {
			var ok bool
			j, ok = byLabel[p.Label]
			if !ok {
				j = -1
			}
		}
		if j < 0 || j >= len(b.Params) || used[j] {
			continue
		}
		matches[i] = j
		used[j] = true
	}
	return matches
}

// MatchBuiltinParams maps every parameter of x to the raw parameter position
// it selects on b, including name-only parameters resolved through contract
// labels contributed by b's attached signatures. It is exported for
// structural subsumption checks.
func MatchBuiltinParams(x *Function, b *Builtin) []int {
	return matchBuiltinParams(x, b)
}

func checkBuiltinParamLabels(c *OpContext, types []FuncType) *Bottom {
	if len(types) > 0 {
		a, b, pos := funcPositionalLabelConflict(types[0].Fn, types[1:])
		if b != InvalidLabel {
			return c.NewErrf("conflicting parameter labels %s and %s for positional parameter %d",
				a.SelectorString(c), b.SelectorString(c), pos+1)
		}
	}
	_, ambiguous := builtinParamLabels(types)
	if ambiguous == InvalidLabel {
		return nil
	}
	return c.NewErrf("ambiguous parameter label %s refers to multiple positions",
		ambiguous.SelectorString(c))
}

// BuiltinParamLabelIndex reports the raw parameter position selected by label
// on b's attached signatures. It is exported for structural subsumption
// checks.
func BuiltinParamLabelIndex(b *Builtin, label Feature) (int, bool) {
	byLabel, _ := builtinParamLabels(b.Types)
	i, ok := byLabel[label]
	return i, ok && i >= 0
}

// bindBuiltinArgs resolves a call's labeled arguments against the
// function types attached to a builtin, returning the call's arguments
// in positional order. A builtin takes its arguments by position; its
// declared signature names the positions, so a label selects the
// position of the like-named parameter. Positional arguments fill the
// remaining positions in order, exactly as in a call to a native
// function.
//
// A builtin may carry several attached types. Compatible signatures agree on
// at most one contract label per raw position. A label no type names does not
// bind. A label placed at different positions makes the attached signatures
// incompatible; the negative map entry remains a defensive check at the call
// boundary.
// A builtin with no attached types supports no labels at all — its
// parameters have no names — which keeps hand-registered packages such
// as path at the pre-existing error.
func bindBuiltinArgs(c *OpContext, b *Builtin, call *CallExpr) ([]Expr, bool) {
	if len(b.Types) == 0 {
		c.AddErrf("labeled arguments are not supported for builtin %s: it declares no parameter names",
			b.qualifiedName(c))
		return nil, false
	}

	byLabel, _ := builtinParamLabels(b.Types)

	bound := make([]Expr, len(b.Params))
	labeled := make([]bool, len(b.Params))
	n := 0 // number of positions bound
	high := -1
	bind := func(pos int, arg Expr, label Feature) bool {
		if pos >= len(bound) {
			c.AddErrf("too many arguments in call to %s", b.qualifiedName(c))
			return false
		}
		if bound[pos] != nil {
			if labeled[pos] {
				c.AddErrf("duplicate argument %s in call to %s",
					label.SelectorString(c), b.qualifiedName(c))
			} else {
				c.AddErrf("argument %s provided by position and label in call to %s",
					label.SelectorString(c), b.qualifiedName(c))
			}
			return false
		}
		bound[pos] = arg
		labeled[pos] = label != InvalidLabel
		n++
		high = max(high, pos)
		return true
	}

	nextPos := 0
	for i, arg := range call.Args {
		if i < len(call.ArgLabels) && call.ArgLabels[i] != InvalidLabel {
			label := call.ArgLabels[i]
			pos, ok := byLabel[label]
			if !ok || pos < 0 {
				c.AddErrf("unknown argument %s in call to %s",
					label.SelectorString(c), b.qualifiedName(c))
				return nil, false
			}
			if !bind(pos, arg, label) {
				return nil, false
			}
			continue
		}
		for nextPos < len(bound) && bound[nextPos] != nil {
			nextPos++
		}
		if !bind(nextPos, arg, InvalidLabel) {
			return nil, false
		}
	}

	// The bound positions must be contiguous from the start: a builtin
	// cannot leave a hole for a middle parameter.
	for i := 0; i <= high; i++ {
		if bound[i] == nil {
			c.AddErrf("missing argument %d in call to %s",
				i+1, b.qualifiedName(c))
			return nil, false
		}
	}
	return bound[:n], true
}

// mergeBuiltinFunc unifies builtin b with the function value or type f. A
// builtin unifies with a compatible function type, yielding the builtin
// carrying the type as an extra [FuncType] constraint that is additionally
// enforced on every ordinary full call. The legacy validator-constructor path
// remains separate. A Bottom return describes an incompatible
// signature. A nil, nil return indicates that b and f are conflicting
// values, to be reported like any other conflicting scalars: a builtin
// never unifies with a proper function value.
func mergeBuiltinFunc(c *OpContext, b *Builtin, f *FuncValue) (*Builtin, *Bottom) {
	if !IsFuncType(f) {
		return nil, nil
	}
	add := append([]FuncType{{Fn: f.Fn, Env: f.Env}}, f.Types...)
	types := b.Types
	added := false
	for _, t := range add {
		if slices.Contains(types, t) {
			continue
		}
		if err := checkBuiltinTightening(c, t.Fn, b, types); err != nil {
			return nil, err
		}
		// The attached types must agree among themselves as they would
		// without the builtin, defaults included: a builtin ignores the
		// defaults its types declare when it completes a call's arguments,
		// which does not make two conflicting declarations compatible.
		for _, u := range types {
			if err := checkFuncTypeMeet(c, t.Fn, u.Fn); err != nil {
				return nil, err
			}
			if err := checkFuncDefaultsMeet(c, t, u); err != nil {
				return nil, err
			}
		}
		if !added {
			types = slices.Clone(types)
			added = true
		}
		types = append(types, t)
	}
	if !added {
		return b, nil
	}
	if err := checkBuiltinParamLabels(c, types); err != nil {
		return nil, err
	}
	if err := checkBuiltinSignatures(c, b, types); err != nil {
		return nil, err
	}
	merged := *b
	merged.Types = types
	if merged.orig == nil {
		merged.orig = b
	}
	return &merged, nil
}

// checkBuiltinSignatures checks every signature in types against b with the
// parameter labels that all of them give the builtin, so that the result does
// not depend on the order in which the signatures were unified with it. A
// builtin's own defaults are part of its signature, like the defaults of the
// CUE signatures unified with it, so a declared default must also agree with
// the builtin's default for the same parameter. The builtin's parameter
// values and result kind are its declaration, which must admit what each
// signature requires, as an implementation's declaration must (see
// [checkFuncAdmission]).
func checkBuiltinSignatures(c *OpContext, b *Builtin, types []FuncType) *Bottom {
	for _, t := range types {
		if err := checkBuiltinTightening(c, t.Fn, b, types); err != nil {
			return err
		}
		for k, j := range matchBuiltinParamsWith(t.Fn, b, types) {
			tp := &t.Fn.Params[k]
			if j < 0 || tp.Default == nil {
				continue
			}
			if d := b.Params[j].Default(); d != nil {
				if err := c.checkBuiltinDefaultAgrees(t.Env, tp, k, d); err != nil {
					return err
				}
			}
		}
		if err := checkBuiltinAdmission(c, b, t, types); err != nil {
			return err
		}
	}
	return nil
}

// checkBuiltinAdmission verifies that builtin b's declaration, its parameter
// values and result kind, admits what the signature t requires, as an
// implementation's declaration must (see [checkFuncAdmission]). types are
// the signatures attached to b, which label its parameters; the check is
// repeated with every signature as they arrive, since a name-only parameter
// is matched only once a sibling signature has labeled the builtin's slot.
func checkBuiltinAdmission(c *OpContext, b *Builtin, t FuncType, types []FuncType) *Bottom {
	for k, j := range matchBuiltinParamsWith(t.Fn, b, types) {
		tp := &t.Fn.Params[k]
		if j < 0 || tp.Value == nil {
			continue
		}
		if !c.builtinParamCovers(b.Params[j].Value, t.Env, tp.Value) {
			return admissionErr(c, t.Fn, nil,
				"parameter %s of builtin %s accepts %s, but its signature requires %s",
				funcParamName(c, k, tp), b.qualifiedName(c), c.Str(b.Params[j].Value), c.Str(tp.Value))
		}
	}
	if t.Fn.Ret != nil && b.Result != BottomKind &&
		!c.builtinResultCovered(t.Env, t.Fn.Ret, b.DeclaredResult()) {
		return admissionErr(c, t.Fn, nil,
			"builtin %s returns %s, but its signature requires %s",
			b.qualifiedName(c), c.Str(b.DeclaredResult()), c.Str(t.Fn.Ret))
	}
	return nil
}

// mergeBuiltins unifies two occurrences of the same builtin, merging their
// recorded type constraints. It returns nil if a and b are distinct
// builtins, which conflict like any other distinct scalars.
func mergeBuiltins(c *OpContext, a, b *Builtin) (*Builtin, *Bottom) {
	if a.self() != b.self() {
		return nil, nil
	}
	if len(b.Types) == 0 {
		return a, nil
	}
	if len(a.Types) == 0 {
		return b, nil
	}
	if err := checkFuncTypeSetsMeet(c, a.Types, b.Types); err != nil {
		return nil, err
	}
	merged := *a
	merged.Types = mergeFuncTypes(a.Types, b.Types)
	if err := checkBuiltinParamLabels(c, merged.Types); err != nil {
		return nil, err
	}
	if err := checkBuiltinSignatures(c, &merged, merged.Types); err != nil {
		return nil, err
	}
	return &merged, nil
}

// BuiltinSubsumes reports whether builtin a subsumes builtin b: a builtin
// subsumes itself and any tightened clone of itself, as tightening only adds
// constraints. The two must share their identity — a tightened clone keeps
// the identity of the builtin it was derived from — and every type
// constraint of a must also constrain b. It is exported for use by
// internal/core/subsume.
func BuiltinSubsumes(a, b *Builtin) bool {
	if a.self() != b.self() {
		return false
	}
	for _, t := range a.Types {
		if !slices.Contains(b.Types, t) {
			return false
		}
	}
	return true
}

// mergeFuncValues unifies two function values, types, or a combination
// thereof. It returns the resulting value, or a Bottom describing why the
// signatures are incompatible. A nil, nil return indicates that a and b are
// conflicting partial applications, to be reported like any other
// conflicting scalars.
func mergeFuncValues(c *OpContext, a, b *FuncValue) (*FuncValue, *Bottom) {
	composing := !IsFuncType(a) && !IsFuncType(b)
	if composing && a.Fn == b.Fn && a.Env.Equal(c, b.Env) && equalFuncArgs(a.args, b.args) {
		// VALUE & VALUE of the same function: only the recorded types need
		// to be combined. Two partial applications are the same only when
		// they bound the same arguments, mirroring [equalTerminal].
		if err := checkFuncTypeSetsMeet(c, a.Types, b.Types); err != nil {
			return nil, err
		}
		merged := *a
		merged.Types = mergeFuncTypes(a.Types, b.Types)
		if err := checkFuncParamLabels(c, merged.Fn, merged.Types); err != nil {
			return nil, err
		}
		if err := checkAdmission(c, FuncType{Fn: a.Fn, Env: a.Env}, merged.Types); err != nil {
			return nil, err
		}
		return &merged, nil
	}

	// VALUE & VALUE of distinct functions composes them: the result is a
	// with b's implementation recorded among its signatures, and a call
	// evaluates all implementations and unifies their results (see
	// [nodeContext.scheduleFuncCall]). The bound arguments of a partial
	// application cannot be carried over to another implementation, so
	// distinct partial applications conflict like any other scalars.
	if composing && (a.IsPartial() || b.IsPartial()) {
		return nil, nil
	}

	// Otherwise, at least one of the two is a type. Make prim the value, if
	// any, so that the signatures of sec are added to it as constraints.
	prim, sec := a, b
	if IsFuncType(a) && !IsFuncType(b) {
		prim, sec = b, a
	}
	if !IsFuncType(prim) && prim.IsPartial() {
		// A partial application is callable over only its unbound
		// parameters, while prim.Fn still describes the original function.
		// Reject tightening until partial values carry a specialized
		// signature which can be matched soundly.
		return nil, c.NewErrf("cannot tighten a partially applied function")
	}

	head := FuncType{Fn: prim.Fn, Env: prim.Env}
	types := prim.Types
	added := false
	// The signatures to add are sec itself followed by its recorded types;
	// visit them in place instead of materializing a combined slice.
	for i := -1; i < len(sec.Types); i++ {
		t := FuncType{Fn: sec.Fn, Env: sec.Env}
		if i >= 0 {
			t = sec.Types[i]
		}
		if t == head || slices.Contains(types, t) {
			continue
		}
		if t.Fn.Body != nil && (sameFunc(c, t, head) || slices.ContainsFunc(types, func(u FuncType) bool {
			return sameFunc(c, t, u)
		})) {
			continue
		}
		if IsFuncType(prim) {
			if b := checkFuncTypeMeet(c, t.Fn, head.Fn); b != nil {
				return nil, b
			}
		} else if t.Fn.Body != nil {
			if b := checkFuncComposition(c, t.Fn, head.Fn); b != nil {
				return nil, b
			}
		} else {
			if b := checkFuncTightening(c, t.Fn, head.Fn); b != nil {
				return nil, b
			}
		}
		if b := checkFuncDefaultsMeet(c, t, head); b != nil {
			return nil, b
		}
		for _, u := range types {
			if b := checkFuncTypeMeet(c, t.Fn, u.Fn); b != nil {
				return nil, b
			}
			if b := checkFuncDefaultsMeet(c, t, u); b != nil {
				return nil, b
			}
		}
		if !added {
			types = slices.Clone(types)
			added = true
		}
		types = append(types, t)
	}
	if !added {
		return prim, nil
	}
	if b := checkFuncParamLabels(c, prim.Fn, types); b != nil {
		return nil, b
	}
	if b := checkAdmission(c, head, types); b != nil {
		return nil, b
	}
	merged := *prim
	merged.Types = types
	return &merged, nil
}

// checkAdmission runs the declared-compatibility check of the implementations
// among head and types against the bodyless signatures among them, if there
// are both; see [checkFuncAdmission].
func checkAdmission(c *OpContext, head FuncType, types []FuncType) *Bottom {
	if !needsAdmission(head, types) {
		return nil
	}
	return checkFuncAdmission(c, head, types)
}

// sameFunc reports whether two signatures with an implementation are the
// same function: the same literal evaluated in the same environment.
func sameFunc(c *OpContext, x, y FuncType) bool {
	return x.Fn == y.Fn && x.Env.Equal(c, y.Env)
}

// checkFuncDefaultsMeet reports a conflict between the defaults that two
// signatures declare for the same parameter. Defaults are part of a
// signature: a default declared on one side only carries over to calls, and
// defaults declared on both sides must unify, which is checked here when the
// signatures are unified, each default evaluated in its own closure
// environment. A default that is not yet resolvable is left to the call,
// where the two defaults meet in the parameter's arc. Parameter matching is
// directional (a positional parameter of a function value never matches a
// name-only parameter by label), so both directions are tried: the result
// must not depend on the operand order.
func checkFuncDefaultsMeet(c *OpContext, x, y FuncType) *Bottom {
	if b := checkFuncDefaultsMeetOneWay(c, x, y); b != nil {
		return b
	}
	return checkFuncDefaultsMeetOneWay(c, y, x)
}

func checkFuncDefaultsMeetOneWay(c *OpContext, x, y FuncType) *Bottom {
	matches := matchFuncParams(x.Fn, y.Fn, x.Fn.Body == nil && y.Fn.Body == nil)
	for i, j := range matches {
		if j < 0 {
			continue
		}
		xp, yp := &x.Fn.Params[i], &y.Fn.Params[j]
		if xp.Default == nil || yp.Default == nil {
			continue
		}
		if b := c.checkFuncDefaultsAgree(x.Env, xp, y.Env, yp, i); b != nil {
			return b
		}
	}
	return nil
}

// checkFuncDefaultsAgree unifies the defaults of two matched parameters, as
// a call omitting the parameter would, and reports a conflict at the first
// default's position with the conflict's own positions retained.
func (c *OpContext) checkFuncDefaultsAgree(xenv *Environment, xp *FuncParam, yenv *Environment, yp *FuncParam, i int) *Bottom {
	savedErrs := c.errs
	c.errs = nil
	defer func() { c.errs = savedErrs }()

	if !c.defaultResolvable(xenv, xp.Default) || !c.defaultResolvable(yenv, yp.Default) {
		return nil
	}
	return c.defaultsConflict(xenv, xp, i, MakeConjunct(yenv, yp.Default, c.ci))
}

// checkBuiltinDefaultAgrees unifies the default that a signature declares for
// parameter i with d, the builtin's own default for the parameter it matches,
// and reports a conflict as [OpContext.checkFuncDefaultsAgree] does.
func (c *OpContext) checkBuiltinDefaultAgrees(env *Environment, p *FuncParam, i int, d Value) *Bottom {
	savedErrs := c.errs
	c.errs = nil
	defer func() { c.errs = savedErrs }()

	if !c.defaultResolvable(env, p.Default) {
		return nil
	}
	return c.defaultsConflict(env, p, i, MakeRootConjunct(nil, d))
}

// defaultResolvable reports whether the default expression x evaluates, in
// env, to a value that is neither an error nor incomplete. A default that
// is not yet resolvable is left to the call.
func (c *OpContext) defaultResolvable(env *Environment, x Expr) bool {
	s := c.PushState(env, x.Source())
	v := c.evalState(x, Flags{status: partial, mode: yield})
	c.PopState(s)
	return v != nil && bottom(v) == nil && c.errs == nil
}

// defaultsConflict unifies the default of parameter i, declared as xp in
// xenv, with other, and reports a permanent conflict at the default's
// position with the conflict's own positions retained.
func (c *OpContext) defaultsConflict(xenv *Environment, xp *FuncParam, i int, other Conjunct) *Bottom {
	s := c.PushState(xenv, xp.Default.Source())
	defer c.PopState(s)
	v := c.newInlineVertex(nil, nil,
		MakeConjunct(xenv, xp.Default, c.ci),
		other)
	v.Finalize(c)
	b := v.Bottom()
	if b == nil {
		b = c.errs
	}
	if b == nil || b.IsIncomplete() || b.Code == CycleError || b.Code == StructuralCycleError {
		return nil
	}
	name := funcParamName(c, i, xp)
	src := xp.Default.Source()
	var at token.Pos
	if src != nil {
		at = src.Pos()
	}
	return &Bottom{
		Src:  src,
		Code: b.Code,
		Err:  errors.Wrapf(b.Err, at, "conflicting defaults for parameter %s", name),
	}
}

// checkFuncTypeSetsMeet verifies the cross-product needed when two already
// tightened values of the same identity are unified. Each side was checked
// while it was built, but constraints originating on opposite sides have not
// necessarily met before.
func checkFuncTypeSetsMeet(c *OpContext, a, b []FuncType) *Bottom {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				continue
			}
			if err := checkFuncTypeMeet(c, x.Fn, y.Fn); err != nil {
				return err
			}
			if err := checkFuncDefaultsMeet(c, x, y); err != nil {
				return err
			}
		}
	}
	return nil
}

// equalFuncTypes reports whether a and b record the same set of function
// type constraints. The lists are deduplicated on construction (see
// [mergeFuncTypes]) but ordered by encounter, so equal tightenings may
// record their types in a different order (T1 & T2 & f versus T2 & T1 & f);
// they must still compare equal.
func equalFuncTypes(a, b []FuncType) bool {
	if len(a) != len(b) {
		return false
	}
	for _, t := range a {
		if !slices.Contains(b, t) {
			return false
		}
	}
	return true
}

// equalFuncValues reports whether x and y are the same function value: they
// bound the same arguments and consist of the same functions and types,
// regardless of the order in which these were unified.
func equalFuncValues(c *OpContext, x, y *FuncValue) bool {
	if x.Fn == y.Fn && x.Env.Equal(c, y.Env) {
		return equalFuncTypes(x.Types, y.Types) && equalFuncArgs(x.args, y.args)
	}
	// Composed values: each side's functions must all occur on the other.
	// Bound arguments are indexed by the parameters of the head function, so
	// partial applications with different heads are not compared.
	if IsFuncType(x) || IsFuncType(y) || len(x.Types) != len(y.Types) || x.IsPartial() || y.IsPartial() {
		return false
	}
	contains := func(v *FuncValue, t FuncType) bool {
		same := func(u FuncType) bool {
			if t.Fn.Body == nil {
				return u == t
			}
			return sameFunc(c, u, t)
		}
		return same(FuncType{Fn: v.Fn, Env: v.Env}) || slices.ContainsFunc(v.Types, same)
	}
	for _, v := range [2][2]*FuncValue{{x, y}, {y, x}} {
		if !contains(v[1], FuncType{Fn: v[0].Fn, Env: v[0].Env}) {
			return false
		}
		for _, t := range v[0].Types {
			if !contains(v[1], t) {
				return false
			}
		}
	}
	return true
}

// equalFuncArgs reports whether two partial applications bound the same
// arguments to the same parameters. Arguments are compared conservatively by
// identity of their expression and environment: distinct bindings such as
// Add(5, ...) and Add(10, ...) are never treated as equal, while an expression
// evaluated in different environments is not assumed equal.
func equalFuncArgs(a, b []funcArg) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		if x.expr != b[i].expr || x.env != b[i].env {
			return false
		}
	}
	return true
}

// EqualArgs reports whether the function values x and y bound the same
// arguments to the same parameters by partial application (see
// [equalFuncArgs]). It is exported for use by internal/core/subsume:
// distinct partial applications differ on every call and never subsume one
// another.
func (x *FuncValue) EqualArgs(y *FuncValue) bool {
	return equalFuncArgs(x.args, y.args)
}

// IsPartial reports whether x carries any arguments bound by partial
// application. It is exported for structural subsumption: a partially applied
// value has a different callable interface from its original function type.
func (x *FuncValue) IsPartial() bool {
	for _, a := range x.args {
		if a.expr != nil {
			return true
		}
	}
	return false
}

// mergeFuncTypes returns the union of two lists of function type
// constraints, preserving the order of a.
func mergeFuncTypes(a, b []FuncType) []FuncType {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	types := slices.Clone(a)
	for _, t := range b {
		if !slices.Contains(types, t) {
			types = append(types, t)
		}
	}
	return types
}
