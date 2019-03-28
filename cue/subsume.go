// Copyright 2018 The CUE Authors
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
	"bytes"
)

type valueSubsumer interface {
	subsumesImpl(ctx *context, v value) bool
}

type subsumeMode int

const (
	// subChoose ensures values are elected before doing a subsumption. This
	// feature is on the conservative side and may result in false negatives.
	subChoose subsumeMode = 1 << iota
)

// subsumes checks gt subsumes lt. If any of the values contains references or
// unevaluated expressions, structural subsumption is performed. This means
// subsumption is conservative; it may return false when a guarantee for
// subsumption could be proven. For concreted values it returns the exact
// relation. It never returns a false positive.
func subsumes(ctx *context, gt, lt value, mode subsumeMode) bool {
	var v, w value
	if mode&subChoose == 0 {
		v = gt.evalPartial(ctx)
		w = lt.evalPartial(ctx)
	} else {
		v = ctx.manifest(gt)
		w = ctx.manifest(lt)
	}
	if !isIncomplete(v) && !isIncomplete(w) {
		gt = v
		lt = w
	}
	a := gt.kind()
	b := lt.kind()
	switch {
	case b == bottomKind:
		return true
	case b&^(a&b) != 0:
		// a does not have strictly more bits. This implies any ground kind
		// subsuming a non-ground type.
		return false
		// TODO: consider not supporting references.
		// case (a|b)&(referenceKind) != 0:
		// 	// no resolution if references are in play.
		// 	return false, false
	}
	switch lt := lt.(type) {
	case *unification:
		if _, ok := gt.(*unification); !ok {
			for _, x := range lt.values {
				if subsumes(ctx, gt, x, mode) {
					return true
				}
			}
			return false
		}

	case *disjunction:
		if _, ok := gt.(*disjunction); !ok {
			for _, x := range lt.values {
				if !subsumes(ctx, gt, x.val, mode) {
					return false
				}
			}
			return true
		}
	}

	return gt.subsumesImpl(ctx, lt, mode)
}

func (x *structLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if o, ok := v.(*structLit); ok {
		// TODO: consider what to do with templates. Perhaps we should always
		// do subsumption on fully evaluated structs.
		if len(x.comprehensions) > 0 { //|| x.template != nil {
			return false
		}

		// all arcs in n must exist in v and its values must subsume.
		for _, a := range x.arcs {
			b := o.lookup(ctx, a.feature)
			if !a.optional && b.optional {
				return false
			} else if b.val() == nil {
				// If field a is optional and has value top, neither the
				// omission of the field nor the field defined with any value
				// may cause unification to fail.
				return a.optional && isTop(a.v)
			} else if !subsumes(ctx, a.v, b.val(), mode) {
				return false
			}
		}
	}
	return !isBottom(v)
}

func (*top) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return true
}

func (x *bottom) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	// never called.
	return v.kind() == bottomKind
}

func (x *basicType) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return true
}

func (x *bound) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if isBottom(v) {
		return true
	}
	kx := x.value.kind()
	if !kx.isDone() || !kx.isGround() {
		return false
	}

	switch y := v.(type) {
	case *bound:
		if ky := y.value.kind(); ky.isDone() && ky.isGround() {
			if (kx&ky)&^kx != 0 {
				return false
			}
			// x subsumes y if
			// x: >= a, y: >= b ==> a <= b
			// x: >= a, y: >  b ==> a <= b
			// x: >  a, y: >  b ==> a <= b
			// x: >  a, y: >= b ==> a < b
			//
			// x: <= a, y: <= b ==> a >= b
			//
			// x: != a, y: != b ==> a != b
			//
			// false if types or op direction doesn't match

			xv := x.value.(evaluated)
			yv := y.value.(evaluated)
			switch x.op {
			case opGtr:
				if y.op == opGeq {
					return test(ctx, x, opLss, xv, yv)
				}
				fallthrough
			case opGeq:
				if y.op == opGtr || y.op == opGeq {
					return test(ctx, x, opLeq, xv, yv)
				}
			case opLss:
				if y.op == opLeq {
					return test(ctx, x, opGtr, xv, yv)
				}
				fallthrough
			case opLeq:
				if y.op == opLss || y.op == opLeq {
					return test(ctx, x, opGeq, xv, yv)
				}
			case opNeq:
				switch y.op {
				case opNeq:
					return test(ctx, x, opEql, xv, yv)
				case opGeq:
					return test(ctx, x, opLss, xv, yv)
				case opGtr:
					return test(ctx, x, opLeq, xv, yv)
				case opLss:
					return test(ctx, x, opGeq, xv, yv)
				case opLeq:
					return test(ctx, x, opGtr, xv, yv)
				}

			case opMat, opNMat:
				// these are just approximations
				if y.op == x.op {
					return test(ctx, x, opEql, xv, yv)
				}

			default:
				// opNeq already handled above.
				panic("cue: undefined bound mode")
			}
		}
		// structural equivalence
		return false

	case *numLit, *stringLit, *durationLit, *boolLit:
		return test(ctx, x, x.op, y.(evaluated), x.value.(evaluated))
	}
	return false
}

func (x *nullLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return true
}

func (x *boolLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return x.b == v.(*boolLit).b
}

func (x *stringLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return x.str == v.(*stringLit).str
}

func (x *bytesLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return bytes.Equal(x.b, v.(*bytesLit).b)
}

func (x *numLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	b := v.(*numLit)
	return x.v.Cmp(&b.v) == 0
}

func (x *durationLit) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return x.d == v.(*durationLit).d
}

func (x *list) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	switch y := v.(type) {
	case *list:
		if !subsumes(ctx, x.len, y.len, mode) {
			return false
		}
		n := len(x.a)
		if len(y.a) < n {
			n = len(y.a)
		}
		for i, a := range x.a[:n] {
			if !subsumes(ctx, a, y.a[i], mode) {
				return false
			}
		}
		if y.isOpen() {
			return subsumes(ctx, x.typ, y.typ, 0)
		}
		for i := range y.a[n:] {
			if !subsumes(ctx, x.typ, y.a[i], mode) {
				return false
			}
		}
		return true
	}
	return isBottom(v)
}

func (x *params) subsumes(ctx *context, y *params, mode subsumeMode) bool {
	// structural equivalence
	// TODO: make agnostic to argument names.
	if len(y.arcs) != len(x.arcs) {
		return false
	}
	for i, a := range x.arcs {
		if !subsumes(ctx, a.v, y.arcs[i].v, 0) {
			return false
		}
	}
	return true
}

func (x *lambdaExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	// structural equivalence
	if y, ok := v.(*lambdaExpr); ok {
		return x.params.subsumes(ctx, y.params, 0) &&
			subsumes(ctx, x.value, y.value, 0)
	}
	return isBottom(v)
}

func (x *unification) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if y, ok := v.(*unification); ok {
		// A unification subsumes another unification if for all values a in x
		// there is a value b in y such that a subsumes b.
		//
		// This assumes overlapping ranges in disjunctions are merged.If this is
		// not the case, subsumes will return a false negative, which is
		// allowed.
	outer:
		for _, vx := range x.values {
			for _, vy := range y.values {
				if subsumes(ctx, vx, vy, mode) {
					continue outer
				}
			}
			return false
		}
		return true
	}
	subsumed := true
	for _, vx := range x.values {
		subsumed = subsumed && subsumes(ctx, vx, v, mode)
	}
	return subsumed
}

// subsumes for disjunction is logically precise. However, just like with
// structural subsumption, it should not have to be called after evaluation.
func (x *disjunction) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	// A disjunction subsumes another disjunction if all values of v are
	// subsumed by any of the values of x, and default values in v are subsumed
	// by the default values of x.
	//
	// This assumes that overlapping ranges in x are merged. If this is not the
	// case, subsumes will return a false negative, which is allowed.
	if d, ok := v.(*disjunction); ok {
		// at least one value in x should subsume each value in d.
	outer:
		for _, vd := range d.values {
			// v is subsumed if any value in x subsumes v.
			for _, vx := range x.values {
				if (vx.marked || !vd.marked) && subsumes(ctx, vx.val, vd.val, 0) {
					continue outer
				}
			}
			return false
		}
		return true
	}
	// v is subsumed if any value in x subsumes v.
	for _, vx := range x.values {
		if subsumes(ctx, vx.val, v, 0) {
			return true
		}
	}
	return false
}

// Structural subsumption operations. Should never have to be called after
// evaluation.

// structural equivalence
func (x *nodeRef) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if r, ok := v.(*nodeRef); ok {
		return x.node == r.node
	}
	return isBottom(v)
}

// structural equivalence
func (x *selectorExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if r, ok := v.(*selectorExpr); ok {
		return x.feature == r.feature && subsumes(ctx, x.x, r.x, subChoose)
	}
	return isBottom(v)
}

func (x *interpolation) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	switch v := v.(type) {
	case *stringLit:
		// Be conservative if not ground.
		return false

	case *interpolation:
		// structural equivalence
		if len(x.parts) != len(v.parts) {
			return false
		}
		for i, p := range x.parts {
			if !subsumes(ctx, p, v.parts[i], 0) {
				return false
			}
		}
		return true
	}
	return false
}

// structural equivalence
func (x *indexExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	// TODO: what does it mean to subsume if the index value is not known?
	if r, ok := v.(*indexExpr); ok {
		// TODO: could be narrowed down if we know the exact value of the index
		// and referenced value.
		return subsumes(ctx, x.x, r.x, mode) && subsumes(ctx, x.index, r.index, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *sliceExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	// TODO: what does it mean to subsume if the index value is not known?
	if r, ok := v.(*sliceExpr); ok {
		// TODO: could be narrowed down if we know the exact value of the index
		// and referenced value.
		return subsumes(ctx, x.x, r.x, 0) &&
			subsumes(ctx, x.lo, r.lo, 0) &&
			subsumes(ctx, x.hi, r.hi, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *callExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if c, ok := v.(*callExpr); ok {
		if len(x.args) != len(c.args) {
			return false
		}
		for i, a := range x.args {
			if !subsumes(ctx, a, c.args[i], 0) {
				return false
			}
		}
		return subsumes(ctx, x.x, c.x, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *unaryExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*unaryExpr); ok {
		return x.op == b.op && subsumes(ctx, x.x, b.x, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *binaryExpr) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*binaryExpr); ok {
		return x.op == b.op &&
			subsumes(ctx, x.left, b.left, 0) &&
			subsumes(ctx, x.right, b.right, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *listComprehension) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*listComprehension); ok {
		return subsumes(ctx, x.clauses, b.clauses, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *fieldComprehension) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*fieldComprehension); ok {
		return subsumes(ctx, x.clauses, b.clauses, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *yield) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*yield); ok {
		return subsumes(ctx, x.key, b.key, 0) &&
			subsumes(ctx, x.value, b.value, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *feed) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*feed); ok {
		return subsumes(ctx, x.source, b.source, 0) &&
			subsumes(ctx, x.fn, b.fn, 0)
	}
	return isBottom(v)
}

// structural equivalence
func (x *guard) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if b, ok := v.(*guard); ok {
		return subsumes(ctx, x.condition, b.condition, 0) &&
			subsumes(ctx, x.value, b.value, 0)
	}
	return isBottom(v)
}
