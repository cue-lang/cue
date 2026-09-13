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

package subsume

import (
	"slices"

	"cuelang.org/go/internal/core/adt"
)

// This file implements subsumption of CUE function values and
// function types (bodyless function literals).
//
// Subsumption is structural, following the signature matching rules of
// function tightening and function type meets: positional parameters align by
// ordinal and their contract labels must be equal when both are present;
// name-only parameters match by label; and matched pairs must agree on
// requiredness. A plain label promised by the subsuming signature must already
// select the matched slot on the candidate; merely being able to add it
// through future tightening is not enough.
// Directionally, everything the subsumed signature
// declares beyond the subsuming signature is admitted only against an open
// subsuming signature (or when the extra parameter is optional and thus not
// needed for a call to succeed), while the subsumed signature must declare
// every non-optional parameter the subsuming signature declares: an extra
// parameter is admitted against a closed signature iff it is optional, the
// same rule the unification paths apply. Each matched parameter constraint
// of the subsuming signature must subsume the corresponding constraint of
// the subsumed one, and so must the result constraint.
//
// A function type subsumes a (tightened) function value whose signature
// satisfies it. A function value subsumes itself and a tightening of
// itself: tightening only adds constraints. A function type also subsumes a
// (tightened) builtin that its signatures admit, applying the same static
// check the evaluator uses when tightening a builtin; a builtin subsumes
// itself and a tightening of itself (see [adt.BuiltinSubsumes]).

// funcValues reports whether the function value or type a subsumes the
// function value or type b.
func (s *subsumer) funcValues(a, b *adt.FuncValue) bool {
	if adt.IsFuncType(a) {
		// Partial application removes already bound parameters from the
		// callable surface. Until partial values expose a specialized
		// signature, conservatively reject type-to-partial subsumption.
		if !adt.IsFuncType(b) && b.IsPartial() {
			return false
		}
		// Every signature constraint of a — its own signature and any
		// types it was met with — must admit b.
		if !s.funcSignature(a.Fn, a.Env, b) {
			return false
		}
		for _, t := range a.Types {
			if !s.funcSignature(t.Fn, t.Env, b) {
				return false
			}
		}
		return true
	}

	// A function value subsumes itself and a tightening of itself:
	// tightening only adds constraints, so every type constraint of a must
	// also constrain b. Distinct partial applications differ on every call
	// and never subsume one another: a and b must have bound the same
	// arguments.
	if adt.IsFuncType(b) || a.Fn != b.Fn || !a.Env.Equal(s.ctx, b.Env) || !a.EqualArgs(b) {
		return false
	}
	for _, t := range a.Types {
		if !slices.Contains(b.Types, t) {
			return false
		}
	}
	return true
}

// funcBuiltin reports whether the function value or type a subsumes the
// builtin b. Only a function type can subsume a builtin: a proper function
// value never equals one. The type subsumes b if its own signature and every
// type it was met with admit b. This starts with the evaluator's static
// tightening check (see [adt.CheckBuiltinTightening]) and additionally proves
// that b already exposes every promised plain label and enforces every
// non-kind constraint.
func (s *subsumer) funcBuiltin(a *adt.FuncValue, b *adt.Builtin) bool {
	if !adt.IsFuncType(a) {
		return false
	}
	if !s.builtinSignature(a.Fn, a.Env, b) {
		return false
	}
	for _, t := range a.Types {
		if !s.builtinSignature(t.Fn, t.Env, b) {
			return false
		}
	}
	return true
}

// builtinSignature reports whether b already satisfies one function-type
// signature. Static compatibility alone is insufficient: tightening may add
// callable labels and non-kind constraints which a bare builtin does not yet
// expose or enforce.
func (s *subsumer) builtinSignature(fn *adt.Function, env *adt.Environment, b *adt.Builtin) bool {
	if slices.Contains(b.Types, adt.FuncType{Fn: fn, Env: env}) {
		return true
	}
	if adt.CheckBuiltinTightening(s.ctx, fn, b) != nil {
		return false
	}

	matches := adt.MatchBuiltinParams(fn, b)
	for i, p := range fn.Params {
		j := matches[i]
		if j < 0 {
			if p.ArcType == adt.ArcOptional {
				continue
			}
			return false
		}
		if p.Positional && p.Label != adt.InvalidLabel {
			got, ok := adt.BuiltinParamLabelIndex(b, p.Label)
			if !ok || got != j {
				return false
			}
		}
		if p.Value != nil && !s.builtinParamConstraint(env, p.Value, b, j) {
			return false
		}
	}
	return s.builtinResultConstraint(env, fn.Ret, b)
}

// builtinParamConstraint proves a signature constraint from either the raw
// builtin parameter or one attached signature constraint on the same slot.
// A single such factor is sufficient because the builtin enforces the meet
// of all factors. Returning false when no individual factor proves the
// constraint is conservative.
func (s *subsumer) builtinParamConstraint(env *adt.Environment, x adt.Expr, b *adt.Builtin, pos int) bool {
	if s.funcConstraint(env, x, nil, b.Params[pos].Value) {
		return true
	}
	for _, t := range b.Types {
		matches := adt.MatchBuiltinParams(t.Fn, b)
		for i, p := range t.Fn.Params {
			if matches[i] == pos && p.Value != nil && s.funcConstraint(env, x, t.Env, p.Value) {
				return true
			}
		}
	}
	return false
}

func (s *subsumer) builtinResultConstraint(env *adt.Environment, x adt.Expr, b *adt.Builtin) bool {
	if x == nil {
		return true
	}
	// A builtin which always returns bottom satisfies every result
	// constraint: bottom is an instance of every value.
	if b.Result == adt.BottomKind {
		return true
	}
	if s.funcConstraint(env, x, nil, &adt.BasicType{K: b.Result}) {
		return true
	}
	for _, t := range b.Types {
		if t.Fn.Ret != nil && s.funcConstraint(env, x, t.Env, t.Fn.Ret) {
			return true
		}
	}
	return false
}

// funcSignature reports whether the function type signature (fn, env)
// admits the function value or type b.
func (s *subsumer) funcSignature(fn *adt.Function, env *adt.Environment, b *adt.FuncValue) bool {
	// A signature b carries as a recorded type constraint, or that is b's
	// own signature, is satisfied by construction.
	if slices.Contains(b.Types, adt.FuncType{Fn: fn, Env: env}) {
		return true
	}
	if b.Fn == fn && b.Env.Equal(s.ctx, env) {
		return true
	}

	if adt.IsFuncType(b) && len(b.Types) > 0 {
		// A composite bodyless type has no concrete implementation signature:
		// whichever constituent happened to become Fn is an artifact of meet
		// order. Until subsumption constructs the effective meet of all
		// parameter constraints, require the proof to hold with every
		// constituent as the head. This is conservative, but sound and
		// representation-independent; in particular, an optional name-only
		// parameter in one head cannot masquerade as a concrete positional
		// slot, nor can a weaker head hide a stricter retained constraint.
		factors := make([]adt.FuncType, 1, len(b.Types)+1)
		factors[0] = adt.FuncType{Fn: b.Fn, Env: b.Env}
		factors = append(factors, b.Types...)
		for i, head := range factors {
			view := *b
			view.Fn = head.Fn
			view.Env = head.Env
			view.Types = make([]adt.FuncType, 0, len(factors)-1)
			view.Types = append(view.Types, factors[:i]...)
			view.Types = append(view.Types, factors[i+1:]...)
			if !s.funcSignatureHead(fn, env, &view) {
				return false
			}
		}
		return true
	}
	return s.funcSignatureHead(fn, env, b)
}

// funcSignatureHead proves one signature against b with its current Fn as
// the concrete or selected head. funcSignature handles the additional
// representation-invariance requirement for composite bodyless types.
func (s *subsumer) funcSignatureHead(fn *adt.Function, env *adt.Environment, b *adt.FuncValue) bool {
	// Every parameter of the signature must be declared by b. Positional
	// parameters align by ordinal and their contract labels must agree when
	// both are present; name-only (a!, a?) parameters match by label. Every
	// plain label must already select its matched slot on b. Matched pairs must
	// agree on requiredness, and the signature's parameter constraint must
	// subsume b's.
	matches := adt.MatchFuncValueParams(fn, b)
	for i, p := range fn.Params {
		j := matches[i]
		if j < 0 {
			if p.ArcType == adt.ArcOptional || p.Default != nil {
				// An extra parameter is admitted against a closed signature
				// iff it is omittable — optional or defaulted — mirroring
				// the tightening and type-meet rules: calls through the
				// signature never need to bind it.
				continue
			}
			return false
		}
		if p.Positional && p.Label != adt.InvalidLabel {
			got, ok := adt.FuncParamLabelIndex(b, p.Label)
			if !ok || got != j {
				return false
			}
		}
		q := b.Fn.Params[j]
		if (p.ArcType == adt.ArcRequired) != (q.ArcType == adt.ArcRequired) {
			return false
		}
		// Omittability is part of the contract: a parameter that the
		// signature lets a call omit must be omittable through b as well,
		// by its own default or by one declared by a signature attached
		// to b. The value of a default does not participate, so signatures
		// that differ only in a default's value subsume each other.
		if p.Default != nil && q.ArcType != adt.ArcOptional && !adt.FuncParamHasDefault(b.Fn, b.Types, j) {
			return false
		}
		if !s.funcConstraint(env, p.Value, b.Env, q.Value) {
			return false
		}
	}

	// Everything b declares beyond the signature's parameters is admitted
	// only against an open signature. An optional or defaulted extra
	// parameter is not needed for a call to succeed and is always admitted;
	// the check is the same one the tightening rules apply, and like them it
	// consults each declaration on its own, not the defaults of the other
	// signatures attached to b, so that subsumption agrees with unification.
	// A composite function type retains every contributing signature, so
	// inspect them all rather than only its chosen head. An effectively open
	// function type also admits future declarations and therefore cannot be
	// an instance of a closed one.
	if !fn.Open {
		if adt.IsFuncType(b) && effectiveFuncTypeOpen(b) {
			return false
		}
		if _, ok := adt.ExtraFuncParam(fn, b.Fn, nil); ok {
			return false
		}
		for _, t := range b.Types {
			if _, ok := adt.ExtraFuncParam(fn, t.Fn, nil); ok {
				return false
			}
		}
	}

	// The signature's result constraint must subsume b's.
	return s.funcConstraint(env, fn.Ret, b.Env, b.Fn.Ret)
}

// effectiveFuncTypeOpen reports whether every signature contributing to a
// composite function type is open. A meet is open only when all of its
// retained signatures are open.
func effectiveFuncTypeOpen(v *adt.FuncValue) bool {
	if !v.Fn.Open {
		return false
	}
	for _, t := range v.Types {
		if !t.Fn.Open {
			return false
		}
	}
	return true
}

// funcConstraint reports whether the parameter or result constraint xa,
// compiled in environment envA, subsumes constraint xb compiled in envB. A
// missing constraint is top. Constraints that do not evaluate to a complete
// value are conservatively reported as not subsuming.
func (s *subsumer) funcConstraint(envA *adt.Environment, xa adt.Expr, envB *adt.Environment, xb adt.Expr) bool {
	if xa == nil {
		// Top subsumes everything.
		return true
	}
	va, ok := s.evalFuncConstraint(envA, xa)
	if !ok {
		return false
	}
	if _, isTop := va.(*adt.Top); isTop {
		return true
	}
	var vb adt.Value = &adt.Top{}
	if xb != nil {
		if vb, ok = s.evalFuncConstraint(envB, xb); !ok {
			return false
		}
	}
	return s.values(va, vb)
}

// evalFuncConstraint evaluates a compiled signature constraint in its
// closure environment.
func (s *subsumer) evalFuncConstraint(env *adt.Environment, x adt.Expr) (adt.Value, bool) {
	v, complete := s.ctx.Evaluate(env, x)
	if !complete {
		return nil, false
	}
	if _, ok := v.(*adt.Bottom); ok {
		return nil, false
	}
	return v, true
}
