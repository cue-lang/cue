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

	"cuelang.org/go/cue/token"
)

// TODO: it probably makes sense to have only two modes left: subsuming a schema
// and subsuming a final value.

func subsumes(v, w Value, mode subsumeMode) error {
	ctx := v.ctx()
	gt := v.eval(ctx)
	lt := w.eval(ctx)
	s := subsumer{ctx: ctx, mode: mode}
	if !s.subsumes(gt, lt) {
		var b *bottom
		src := binSrc(token.NoPos, opUnify, gt, lt)
		if s.gt != nil && s.lt != nil {
			src := binSrc(token.NoPos, opUnify, s.gt, s.lt)
			var ok bool
			if s.missing != 0 {
				b = ctx.mkErr(src, "missing field %q", ctx.labelStr(s.missing))
			} else if b, ok = binOp(ctx, src, opUnify, s.gt, s.lt).(*bottom); !ok {
				b = ctx.mkErr(src, "value not an instance")
			}
		}
		if b == nil {
			b = ctx.mkErr(src, "value not an instance")
		} else {
			b = ctx.mkErr(src, b, "%v", b)
		}
		return w.toErr(b)
	}
	return nil
}

type subsumer struct {
	ctx  *context
	mode subsumeMode

	// recorded values where an error occurred.
	gt, lt  evaluated
	missing label
}

type subsumeMode int

const (
	// subChoose ensures values are elected before doing a subsumption. This
	// feature is on the conservative side and may result in false negatives.
	subChoose subsumeMode = 1 << iota

	// subNoOptional ignores optional fields for the purpose of subsumption.
	// This option is predominantly intended for implementing equality checks.
	// TODO: may be unnecessary now subFinal is available.
	subNoOptional

	// the subsumed value is final
	subFinal
)

// TODO: improve upon this highly inefficient implementation. There should
// be a dedicated equal function once the dust settles.
func equals(c *context, x, y value) bool {
	s := subsumer{ctx: c, mode: subNoOptional}
	return s.subsumes(x, y) && s.subsumes(y, x)
}

// subsumes checks gt subsumes lt. If any of the values contains references or
// unevaluated expressions, structural subsumption is performed. This means
// subsumption is conservative; it may return false when a guarantee for
// subsumption could be proven. For concreted values it returns the exact
// relation. It never returns a false positive.
func (s *subsumer) subsumes(gt, lt value) (result bool) {
	ctx := s.ctx
	var v, w evaluated
	if s.mode&subChoose == 0 {
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
		goto exit
		// TODO: consider not supporting references.
		// case (a|b)&(referenceKind) != 0:
		// 	// no resolution if references are in play.
		// 	return false, false
	}
	switch lt := lt.(type) {
	case *unification:
		if _, ok := gt.(*unification); !ok {
			for _, x := range lt.values {
				if s.subsumes(gt, x) {
					return true
				}
			}
			goto exit
		}

	case *disjunction:
		if _, ok := gt.(*disjunction); !ok {
			for _, x := range lt.values {
				if !s.subsumes(gt, x.val) {
					return false
				}
			}
			return true
		}
	}

	result = gt.subsumesImpl(s, lt)
exit:
	if !result && s.gt == nil && s.lt == nil {
		s.gt = v
		s.lt = w
	}
	return result
}

func (x *structLit) subsumesImpl(s *subsumer, v value) bool {
	ctx := s.ctx
	ignoreOptional := s.mode&subNoOptional != 0
	if o, ok := v.(*structLit); ok {
		if x.optionals != nil && !ignoreOptional {
			if s.mode&subFinal == 0 {
				// TODO: also cross-validate optional fields in the schema case.
				return false
			}
			for _, b := range o.arcs {
				if b.optional || b.definition {
					continue
				}
				name := ctx.labelStr(b.feature)
				arg := &stringLit{x.baseValue, name, nil}
				u, _ := x.optionals.constraint(ctx, arg)
				if u != nil && !s.subsumes(u, b.v) {
					return false
				}
			}
		}
		if len(x.comprehensions) > 0 {
			return false
		}
		if x.emit != nil {
			if o.emit == nil || !s.subsumes(x.emit, o.emit) {
				return false
			}
		}

		// all arcs in n must exist in v and its values must subsume.
		for _, a := range x.arcs {
			if a.optional && ignoreOptional {
				continue
			}
			b := o.lookup(ctx, a.feature)
			if !a.optional && b.optional {
				return false
			} else if b.val() == nil {
				if a.definition && s.mode&subFinal != 0 {
					continue
				}
				// if o is closed, the field is implicitly defined as _|_ and
				// thus subsumed. Technically, this is even true if a is not
				// optional, but in that case it means that o is invalid, so
				// return false regardless
				if a.optional && (o.closeStatus.shouldClose() || s.mode&subFinal != 0) {
					continue
				}
				// If field a is optional and has value top, neither the
				// omission of the field nor the field defined with any value
				// may cause unification to fail.
				if a.optional && isTop(a.v) {
					continue
				}
				s.missing = a.feature
				s.gt = a.val()
				s.lt = o
				return false
			} else if a.definition != b.definition {
				return false
			} else if !s.subsumes(a.v, b.val()) {
				return false
			}
		}
		// For closed structs, all arcs in b must exist in a.
		if x.closeStatus.shouldClose() {
			if !ignoreOptional && !o.closeStatus.shouldClose() && s.mode&subFinal == 0 {
				return false
			}
			ignoreOptional = ignoreOptional || s.mode&subFinal != 0
			for _, b := range o.arcs {
				if ignoreOptional && b.optional {
					continue
				}
				a := x.lookup(ctx, b.feature)
				if a.val() == nil {
					name := ctx.labelStr(b.feature)
					arg := &stringLit{x.baseValue, name, nil}
					u, _ := x.optionals.constraint(ctx, arg)
					if u == nil { // subsumption already checked
						s.lt = b.val()
						return false
					}
				}
			}
		}
	}
	return !isBottom(v)
}

func (*top) subsumesImpl(s *subsumer, v value) bool {
	return true
}

func (x *bottom) subsumesImpl(s *subsumer, v value) bool {
	// never called.
	return v.kind() == bottomKind
}

func (x *basicType) subsumesImpl(s *subsumer, v value) bool {
	return true
}

func (x *bound) subsumesImpl(s *subsumer, v value) bool {
	ctx := s.ctx
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

func (x *nullLit) subsumesImpl(s *subsumer, v value) bool {
	return true
}

func (x *boolLit) subsumesImpl(s *subsumer, v value) bool {
	return x.b == v.(*boolLit).b
}

func (x *stringLit) subsumesImpl(s *subsumer, v value) bool {
	return x.str == v.(*stringLit).str
}

func (x *bytesLit) subsumesImpl(s *subsumer, v value) bool {
	return bytes.Equal(x.b, v.(*bytesLit).b)
}

func (x *numLit) subsumesImpl(s *subsumer, v value) bool {
	b := v.(*numLit)
	return x.v.Cmp(&b.v) == 0
}

func (x *durationLit) subsumesImpl(s *subsumer, v value) bool {
	return x.d == v.(*durationLit).d
}

func (x *list) subsumesImpl(s *subsumer, v value) bool {
	switch y := v.(type) {
	case *list:
		if !s.subsumes(x.len, y.len) {
			return false
		}
		// TODO: need to handle case where len(x.elem) > len(y.elem) explicitly
		// if we introduce cap().
		if !s.subsumes(x.elem, y.elem) {
			return false
		}
		// TODO: assuming continuous indices, use merge sort if we allow
		// sparse arrays.
		for _, a := range y.elem.arcs[len(x.elem.arcs):] {
			if !s.subsumes(x.typ, a.v) {
				return false
			}
		}
		if y.isOpen() { // implies from first check that x.IsOpen.
			return s.subsumes(x.typ, y.typ)
		}
		return true
	}
	return isBottom(v)
}

func (x *params) subsumes(s *subsumer, y *params) bool {
	// structural equivalence
	// TODO: make agnostic to argument names.
	if len(y.arcs) != len(x.arcs) {
		return false
	}
	for i, a := range x.arcs {
		if !s.subsumes(a.v, y.arcs[i].v) {
			return false
		}
	}
	return true
}

func (x *lambdaExpr) subsumesImpl(s *subsumer, v value) bool {
	// structural equivalence
	if y, ok := v.(*lambdaExpr); ok {
		return x.params.subsumes(s, y.params) &&
			s.subsumes(x.value, y.value)
	}
	return isBottom(v)
}

func (x *unification) subsumesImpl(s *subsumer, v value) bool {
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
				if s.subsumes(vx, vy) {
					continue outer
				}
			}
			return false
		}
		return true
	}
	subsumed := true
	for _, vx := range x.values {
		subsumed = subsumed && s.subsumes(vx, v)
	}
	return subsumed
}

// subsumes for disjunction is logically precise. However, just like with
// structural subsumption, it should not have to be called after evaluation.
func (x *disjunction) subsumesImpl(s *subsumer, v value) bool {
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
				if (vx.marked || !vd.marked) && s.subsumes(vx.val, vd.val) {
					continue outer
				}
			}
			return false
		}
		return true
	}
	// v is subsumed if any value in x subsumes v.
	for _, vx := range x.values {
		if s.subsumes(vx.val, v) {
			return true
		}
	}
	return false
}

// Structural subsumption operations. Should never have to be called after
// evaluation.

// structural equivalence
func (x *nodeRef) subsumesImpl(s *subsumer, v value) bool {
	if r, ok := v.(*nodeRef); ok {
		return x.node == r.node
	}
	return isBottom(v)
}

// structural equivalence
func (x *selectorExpr) subsumesImpl(s *subsumer, v value) bool {
	if r, ok := v.(*selectorExpr); ok {
		return x.feature == r.feature && s.subsumes(x.x, r.x) // subChoose
	}
	return isBottom(v)
}

func (x *interpolation) subsumesImpl(s *subsumer, v value) bool {
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
			if !s.subsumes(p, v.parts[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// structural equivalence
func (x *indexExpr) subsumesImpl(s *subsumer, v value) bool {
	// TODO: what does it mean to subsume if the index value is not known?
	if r, ok := v.(*indexExpr); ok {
		// TODO: could be narrowed down if we know the exact value of the index
		// and referenced value.
		return s.subsumes(x.x, r.x) && s.subsumes(x.index, r.index)
	}
	return isBottom(v)
}

// structural equivalence
func (x *sliceExpr) subsumesImpl(s *subsumer, v value) bool {
	// TODO: what does it mean to subsume if the index value is not known?
	if r, ok := v.(*sliceExpr); ok {
		// TODO: could be narrowed down if we know the exact value of the index
		// and referenced value.
		return s.subsumes(x.x, r.x) &&
			s.subsumes(x.lo, r.lo) &&
			s.subsumes(x.hi, r.hi)
	}
	return isBottom(v)
}

// structural equivalence
func (x *customValidator) subsumesImpl(s *subsumer, v value) bool {
	y, ok := v.(*customValidator)
	if !ok {
		return isBottom(v)
	}
	if x.call != y.call {
		return false
	}
	for i, v := range x.args {
		if !s.subsumes(v, y.args[i]) {
			return false
		}
	}
	return true
}

// structural equivalence
func (x *callExpr) subsumesImpl(s *subsumer, v value) bool {
	if c, ok := v.(*callExpr); ok {
		if len(x.args) != len(c.args) {
			return false
		}
		for i, a := range x.args {
			if !s.subsumes(a, c.args[i]) {
				return false
			}
		}
		return s.subsumes(x.x, c.x)
	}
	return isBottom(v)
}

// structural equivalence
func (x *unaryExpr) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*unaryExpr); ok {
		return x.op == b.op && s.subsumes(x.x, b.x)
	}
	return isBottom(v)
}

// structural equivalence
func (x *binaryExpr) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*binaryExpr); ok {
		return x.op == b.op &&
			s.subsumes(x.left, b.left) &&
			s.subsumes(x.right, b.right)
	}
	return isBottom(v)
}

// structural equivalence
func (x *listComprehension) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*listComprehension); ok {
		return s.subsumes(x.clauses, b.clauses)
	}
	return isBottom(v)
}

// structural equivalence
func (x *structComprehension) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*structComprehension); ok {
		return s.subsumes(x.clauses, b.clauses)
	}
	return isBottom(v)
}

// structural equivalence
func (x *fieldComprehension) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*fieldComprehension); ok {
		return s.subsumes(x.key, b.key) &&
			s.subsumes(x.val, b.val) &&
			!x.opt && b.opt &&
			x.def == b.def
	}
	return isBottom(v)
}

// structural equivalence
func (x *yield) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*yield); ok {
		return s.subsumes(x.value, b.value)
	}
	return isBottom(v)
}

// structural equivalence
func (x *feed) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*feed); ok {
		return s.subsumes(x.source, b.source) &&
			s.subsumes(x.fn, b.fn)
	}
	return isBottom(v)
}

// structural equivalence
func (x *guard) subsumesImpl(s *subsumer, v value) bool {
	if b, ok := v.(*guard); ok {
		return s.subsumes(x.condition, b.condition) &&
			s.subsumes(x.value, b.value)
	}
	return isBottom(v)
}
