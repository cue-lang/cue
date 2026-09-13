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

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
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
//     a?, which can be passed by label alone) matches by label. A type
//     parameter that the value does not declare is allowed only if the
//     parameter is omittable — optional (a?) or declaring a default
//     (a: T = d); callable function values are closed;
//   - a matched pair of parameters must agree on requiredness (a! matches
//     only a!);
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
//   - conflicts between a type's and a value's parameter constraints (e.g.
//     int versus string) surface when the function is called, not when it is
//     unified;
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
// positional ordinal; name-only parameters map by label.
//
// A parameter of y can be matched at most once. This keeps positional and
// name-only matches from ever collapsing two declarations onto one slot.
func matchFuncParams(x, y *Function) []int {
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
// signatures are attached (see [ExtraFuncParam]). Matched pairs must agree
// on requiredness. The strings xd and yd describe the two signatures in
// error messages.
func checkParamsDeclared(c *OpContext, x, y *Function, xd, yd string) *Bottom {
	if a, b, pos, ok := conflictingPositionalLabels(x, y); ok {
		return c.NewErrf("conflicting parameter labels %s and %s for positional parameter %d in %s and %s",
			a.SelectorString(c), b.SelectorString(c), pos+1, xd, yd)
	}
	matches := matchFuncParams(x, y)
	for i, p := range x.Params {
		j := matches[i]
		if j < 0 {
			if p.Positional && p.Label != InvalidLabel {
				// A same-named name-only parameter does not declare this
				// positional slot. Check it only to preserve the precise
				// requiredness diagnostic for plain a against a!.
				if q, _, ok := MatchFuncParam(y, p.Label, -1); ok && (p.ArcType == ArcRequired) != (q.ArcType == ArcRequired) {
					return c.NewErrf("parameter %s must be required in both %s and %s",
						p.Label.SelectorString(c), xd, yd)
				}
			}
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
		q := y.Params[j]
		if (p.ArcType == ArcRequired) != (q.ArcType == ArcRequired) {
			return c.NewErrf("parameter %s must be required in both %s and %s",
				p.Label.SelectorString(c), xd, yd)
		}
	}
	return nil
}

// checkFuncTypeMeet verifies that two function types have unifiable
// signatures: positional parameters align by ordinal and unify their contract
// labels, name-only parameters match by label, and a parameter present in only
// one is allowed only if the other signature is open or the parameter is
// optional.
func checkFuncTypeMeet(c *OpContext, x, y *Function) *Bottom {
	if b := checkParamsDeclared(c, x, y, "function type", "function type"); b != nil {
		return b
	}
	// The reverse direction only checks presence; requiredness of matched
	// pairs was already checked above.
	return checkParamsDeclared(c, y, x, "function type", "function type")
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
	if b := checkParamsDeclared(c, typ, val, "function type", "function"); b != nil {
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
	for _, j := range matchFuncParams(typ, val) {
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
		matches := matchFuncParams(t.Fn, fn)
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
// must agree on that label. Only a parameter that can *only* be passed by label —
// one marked required (a!) or optional (a?) — cannot itself supply a
// positional match. The static checks are limited to what is decidable from
// the compiled signature and the builtin's shape:
//
//   - a name-only parameter of the type is rejected, unless it is optional
//     (a?): an unmatched optional parameter contributes no slot or label, but
//     no call needs it either. If a sibling positional signature supplies the
//     same label, its constraint follows that sibling's slot;
//   - a type parameter beyond the builtin's parameter count is rejected;
//   - unless the type is open, a defaultless builtin parameter beyond the
//     type's parameter count is rejected, as calls through the type could
//     never provide it. Builtins carry no optional markers; a builtin
//     parameter with a default is admitted instead, since builtin defaults
//     — unlike function value defaults, which are dynamic and never probed
//     — are static metadata;
//   - a parameter or result constraint that is literally a basic type
//     disjoint with the builtin's parameter or result kind is rejected.
//
// All other constraints are enforced dynamically, per call (see
// [Builtin.rawCall]).
func CheckBuiltinTightening(c *OpContext, typ *Function, b *Builtin) *Bottom {
	pos := 0
	for _, p := range typ.Params {
		if !p.Positional {
			if p.ArcType == ArcOptional {
				// An extra parameter is admitted against a closed signature
				// iff it is optional. It contributes no position itself; a
				// sibling positional signature may nevertheless expose the same
				// label and carry this parameter's constraint to that slot.
				continue
			}
			return c.NewErrf("parameter %s of function type not allowed by builtin %s: it can only be passed by label, and a builtin takes its arguments positionally",
				p.Label.SelectorString(c), b.qualifiedName(c))
		}
		if pos >= len(b.Params) {
			return c.NewErrf("function type has more positional parameters than builtin %s",
				b.qualifiedName(c))
		}
		if p.Value != nil {
			if k, ok := staticKind(p.Value); ok && k&b.Params[pos].Kind() == BottomKind {
				return c.NewErrf("parameter %d of function type has kind %s, conflicting with kind %s of builtin %s",
					pos+1, k, b.Params[pos].Kind(), b.qualifiedName(c))
			}
		}
		pos++
	}
	if !typ.Open {
		for i := pos; i < len(b.Params); i++ {
			if b.Params[i].Default() == nil {
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
	byLabel, _ := builtinParamLabels(b.Types)
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
		if err := CheckBuiltinTightening(c, t.Fn, b); err != nil {
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
	merged := *b
	merged.Types = types
	if merged.orig == nil {
		merged.orig = b
	}
	return &merged, nil
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
// conflicting function values, to be reported like any other conflicting
// scalars.
func mergeFuncValues(c *OpContext, a, b *FuncValue) (*FuncValue, *Bottom) {
	if !IsFuncType(a) && !IsFuncType(b) {
		// VALUE & VALUE: a function value unifies only with itself. Two
		// partial applications are equal only when they bound the same
		// arguments, and a plain function differs from any partial
		// application of itself, mirroring [equalTerminal].
		if a.Fn != b.Fn || !a.Env.Equal(c, b.Env) || !equalFuncArgs(a.args, b.args) {
			return nil, nil
		}
		if err := checkFuncTypeSetsMeet(c, a.Types, b.Types); err != nil {
			return nil, err
		}
		merged := *a
		merged.Types = mergeFuncTypes(a.Types, b.Types)
		if err := checkFuncParamLabels(c, merged.Fn, merged.Types); err != nil {
			return nil, err
		}
		return &merged, nil
	}

	// At least one of the two is a type. Make prim the value, if any, so
	// that sec is always a type whose signatures are added as constraints.
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
		if IsFuncType(prim) {
			if b := checkFuncTypeMeet(c, t.Fn, head.Fn); b != nil {
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
	merged := *prim
	merged.Types = types
	return &merged, nil
}

// checkFuncDefaultsMeet reports a conflict between the defaults that two
// signatures declare for the same parameter. Defaults are part of a
// signature: a default declared on one side only carries over to calls, and
// defaults declared on both sides must unify, which is checked here when the
// signatures are unified, each default evaluated in its own closure
// environment. A default that is not yet resolvable is left to the call,
// where the two defaults meet in the parameter's arc. Parameter matching is
// not symmetric (a name-only parameter matches a positional one by label but
// not the reverse), so both directions are tried: the result must not
// depend on the operand order.
func checkFuncDefaultsMeet(c *OpContext, x, y FuncType) *Bottom {
	if b := checkFuncDefaultsMeetOneWay(c, x, y); b != nil {
		return b
	}
	return checkFuncDefaultsMeetOneWay(c, y, x)
}

func checkFuncDefaultsMeetOneWay(c *OpContext, x, y FuncType) *Bottom {
	matches := matchFuncParams(x.Fn, y.Fn)
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

	flags := Flags{status: partial, mode: yield}
	s := c.PushState(xenv, xp.Default.Source())
	xv := c.evalState(xp.Default, flags)
	c.PopState(s)
	if b := bottom(xv); b != nil || xv == nil || c.errs != nil {
		return nil
	}
	s = c.PushState(yenv, yp.Default.Source())
	yv := c.evalState(yp.Default, flags)
	c.PopState(s)
	if b := bottom(yv); b != nil || yv == nil || c.errs != nil {
		return nil
	}

	s = c.PushState(xenv, xp.Default.Source())
	defer c.PopState(s)
	v := c.newInlineVertex(nil, nil,
		MakeConjunct(xenv, xp.Default, c.ci),
		MakeConjunct(yenv, yp.Default, c.ci))
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
