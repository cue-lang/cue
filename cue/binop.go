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
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v2"

	"cuelang.org/go/cue/token"
)

// binSrc returns a baseValue representing a binary expression of the given
// values.
func binSrc(pos token.Pos, op op, a, b value) baseValue {
	return baseValue{&computedSource{pos, op, a, b}}
}

func binOp(ctx *context, src source, op op, left, right evaluated) (result evaluated) {
	_, isUnify := op.unifyType()
	if b, ok := left.(*bottom); ok {
		if isUnify && b.exprDepth == 0 && cycleError(b) != nil {
			ctx.cycleErr = true
			return right
		}
		return left
	}
	if b, ok := right.(*bottom); ok {
		if isUnify && b.exprDepth == 0 && cycleError(b) != nil {
			ctx.cycleErr = true
			return left
		}
		return right
	}

	left = convertBuiltin(left)
	right = convertBuiltin(right)

	leftKind := left.Kind()
	rightKind := right.Kind()
	kind, invert, msg := matchBinOpKind(op, leftKind, rightKind)
	if kind == bottomKind {
		simplify := func(v, orig value) value {
			switch x := v.(type) {
			case *disjunction:
				return orig
			case *binaryExpr:
				if x.Op == opDisjunction {
					return orig
				}
			default:
				return x
			}
			return v
		}
		var l, r value = left, right
		if x, ok := src.(*binaryExpr); ok {
			l = simplify(x.X, left)
			r = simplify(x.Y, right)
		}
		return ctx.mkErr(src, msg, op, ctx.str(l), ctx.str(r), leftKind, rightKind)
	}
	if kind.hasReferences() {
		panic("unexpected references in expression")
	}
	if invert {
		left, right = right, left
	}
	if !isUnify {
		// Any operation other than unification or disjunction must be on
		// concrete types. Disjunction is handled separately.
		if !leftKind.isGround() || !rightKind.isGround() {
			return ctx.mkErr(src, codeIncomplete, "incomplete error")
		}
		ctx.incEvalDepth()
		v := left.binOp(ctx, src, op, right) // may return incomplete
		ctx.decEvalDepth()
		return v
	}

	// isUnify

	// TODO: unify type masks.
	if left == right {
		return left
	}
	if isTop(left) {
		return right
	}
	if isTop(right) {
		return left
	}

	if dl, ok := left.(*disjunction); ok {
		return distribute(ctx, src, op, dl, right)
	} else if dr, ok := right.(*disjunction); ok {
		return distribute(ctx, src, op, dr, left)
	}

	if _, ok := right.(*unification); ok {
		return right.binOp(ctx, src, op, left)
	}

	// TODO: value may be incomplete if there is a cycle. Instead of an error
	// schedule an assert and return the atomic value, if applicable.
	v := left.binOp(ctx, src, op, right)
	if isBottom(v) {
		v := right.binOp(ctx, src, op, left)
		// Return the original failure if both fail, as this will result in
		// better error messages.
		if !isBottom(v) || isCustom(v) {
			return v
		}
	}
	return v
}

type mVal struct {
	val  evaluated
	mark bool
}

// distribute distributes a value over the element of a disjunction in a
// unification operation.
// TODO: this is an exponential algorithm. There is no reason to have to
// resolve this early. Revise this to only do early pruning but not a full
// evaluation.
func distribute(ctx *context, src source, op op, x, y evaluated) evaluated {
	dn := &disjunction{baseValue: src.base()}
	dist(ctx, dn, false, op, mVal{x, true}, mVal{y, true})
	return dn.normalize(ctx, src).val
}

func dist(ctx *context, d *disjunction, mark bool, op op, x, y mVal) {
	if dx, ok := x.val.(*disjunction); ok {
		if dx.HasDefaults {
			mark = true
			d.HasDefaults = true
		}
		for _, dxv := range dx.Values {
			m := dxv.Default || !dx.HasDefaults
			dist(ctx, d, mark, op, mVal{dxv.Val.evalPartial(ctx), m}, y)
		}
		return
	}
	if dy, ok := y.val.(*disjunction); ok {
		if dy.HasDefaults {
			mark = true
			d.HasDefaults = true
		}
		for _, dxy := range dy.Values {
			m := dxy.Default || !dy.HasDefaults
			dist(ctx, d, mark, op, x, mVal{dxy.Val.evalPartial(ctx), m})
		}
		return
	}
	src := binSrc(token.NoPos, op, x.val, y.val)
	d.add(ctx, binOp(ctx, src, op, x.val, y.val), mark && x.mark && y.mark)
}

func (x *disjunction) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	panic("unreachable: special-cased")
}

func (x *bottom) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	panic("unreachable: special-cased")
}

// add adds to a unification. Note that the value cannot be a struct and thus
// there is no need to distinguish between checked and unchecked unification.
func (x *unification) add(ctx *context, src source, v evaluated) evaluated {
	for progress := true; progress; {
		progress = false
		k := 0

		for i, vx := range x.Values {
			a := binOp(ctx, src, opUnify, vx, v)
			switch _, isUnify := a.(*unification); {
			case isBottom(a):
				if !isIncomplete(a) {
					return a
				}
				fallthrough
			case isUnify:
				x.Values[k] = x.Values[i]
				k++
				continue
			}
			// k will not be raised in this iteration. So the outer loop
			// will ultimately terminate as k reaches 0.
			// In practice it is seems unlikely that there will be more than
			// two iterations for any addition.
			// progress = true
			v = a
		}
		if k == 0 {
			return v
		}
		x.Values = x.Values[:k]
	}
	x.Values = append(x.Values, v)
	return nil
}

func (x *unification) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	if _, isUnify := op.unifyType(); isUnify {
		// Cannot be checked unification.
		u := &unification{baseValue: baseValue{src}}
		u.Values = append(u.Values, x.Values...)
		if y, ok := other.(*unification); ok {
			for _, vy := range y.Values {
				if v := u.add(ctx, src, vy); v != nil {
					return v
				}
			}
		} else if v := u.add(ctx, src, other); v != nil {
			return v
		}
		return u
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *top) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch op {
	case opUnify, opUnifyUnchecked:
		return other
	}
	src = mkBin(ctx, src.Pos(), op, x, other)
	return ctx.mkErr(src, codeIncomplete, "binary operation on (incomplete) top value")
}

func (x *basicType) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	k := unifyType(x.Kind(), other.Kind())
	switch y := other.(type) {
	case *basicType:
		switch op {
		// TODO: other types.
		case opUnify, opUnifyUnchecked:
			if k&typeKinds != bottomKind {
				return &basicType{binSrc(src.Pos(), op, x, other), k & typeKinds}
			}
		}

	case *bound:
		src = mkBin(ctx, src.Pos(), op, x, other)
		return ctx.mkErr(src, codeIncomplete, "%s with incomplete values", op)

	case *numLit:
		if op == opUnify || op == opUnifyUnchecked {
			if k == y.K {
				return y
			}
			return y.specialize(k)
		}
		src = mkBin(ctx, src.Pos(), op, x, other)
		return ctx.mkErr(src, codeIncomplete, "%s with incomplete values", op)

	default:
		if k&typeKinds != bottomKind {
			return other
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func checkBounds(ctx *context, src source, r *bound, op op, a, b evaluated) evaluated {
	v := binOp(ctx, src, op, a, b)
	if isBottom(v) || !v.(*boolLit).B {
		return errOutOfBounds(ctx, src.Pos(), r, a)
	}
	return nil
}

func errOutOfBounds(ctx *context, pos token.Pos, r *bound, v evaluated) *bottom {
	if pos == token.NoPos {
		pos = r.Pos()
	}
	e := mkBin(ctx, pos, opUnify, r, v)
	msg := "invalid value %v (out of bound %v)"
	switch r.Op {
	case opNeq, opNMat:
		msg = "invalid value %v (excluded by %v)"
	case opMat:
		msg = "invalid value %v (does not match %v)"
	}
	return ctx.mkErr(e, msg, ctx.str(v), ctx.str(r))
}

func opInfo(op op) (cmp op, norm int) {
	switch op {
	case opGtr:
		return opGeq, 1
	case opGeq:
		return opGtr, 1
	case opLss:
		return opLeq, -1
	case opLeq:
		return opLss, -1
	case opNeq:
		return opNeq, 0
	case opMat:
		return opMat, 2
	case opNMat:
		return opNMat, 3
	}
	panic("cue: unreachable")
}

func (x *bound) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	xv := x.Expr.(evaluated)

	newSrc := binSrc(src.Pos(), op, x, other)
	switch op {
	case opUnify, opUnifyUnchecked:
		k, _, msg := matchBinOpKind(opUnify, x.Kind(), other.Kind())
		if k == bottomKind {
			return ctx.mkErr(src, msg, opUnify, ctx.str(x), ctx.str(other), x.Kind(), other.Kind())
		}
		switch y := other.(type) {
		case *basicType:
			k := unifyType(x.k, y.Kind())
			if k == x.k {
				return x
			}
			return newBound(ctx, newSrc.base(), x.Op, k, xv)

		case *bound:
			yv := y.Expr.(evaluated)
			if !xv.Kind().isGround() || !yv.Kind().isGround() {
				return ctx.mkErr(newSrc, codeIncomplete, "cannot add incomplete values")
			}

			cmp, xCat := opInfo(x.Op)
			_, yCat := opInfo(y.Op)

			switch {
			case xCat == yCat:
				if x.Op == opNeq || x.Op == opMat || x.Op == opNMat {
					if test(ctx, x, opEql, xv, yv) {
						return x
					}
					break // unify the two bounds
				}

				// xCat == yCat && x.op != opNeq
				// > a & >= b
				//    > a   if a >= b
				//    >= b  if a <  b
				// > a & > b
				//    > a   if a >= b
				//    > b   if a <  b
				// >= a & > b
				//    >= a   if a > b
				//    > b    if a <= b
				// >= a & >= b
				//    >= a   if a > b
				//    >= b   if a <= b
				// inverse is true as well.

				// Tighten bound.
				if test(ctx, x, cmp, xv, yv) {
					return x
				}
				return y

			case xCat == -yCat:
				if xCat == -1 {
					x, y = y, x
				}
				a, aOK := x.Expr.(evaluated).(*numLit)
				b, bOK := y.Expr.(evaluated).(*numLit)

				if !aOK || !bOK {
					break
				}

				var d, lo, hi apd.Decimal
				lo.Set(&a.X)
				hi.Set(&b.X)
				if k&floatKind == 0 {
					// Readjust bounds for integers.
					if x.Op == opGeq {
						// >=3.4  ==>  >=4
						_, _ = apd.BaseContext.Ceil(&lo, &a.X)
					} else {
						// >3.4  ==>  >3
						_, _ = apd.BaseContext.Floor(&lo, &a.X)
					}
					if y.Op == opLeq {
						// <=2.3  ==>  <= 2
						_, _ = apd.BaseContext.Floor(&hi, &b.X)
					} else {
						// <2.3   ==>  < 3
						_, _ = apd.BaseContext.Ceil(&hi, &b.X)
					}
				}

				cond, err := apd.BaseContext.Sub(&d, &hi, &lo)
				if cond.Inexact() || err != nil {
					break
				}

				// attempt simplification
				// numbers
				// >=a & <=b
				//     a   if a == b
				//     _|_ if a < b
				// >=a & <b
				//     _|_ if b <= a
				// >a  & <=b
				//     _|_ if b <= a
				// >a  & <b
				//     _|_ if b <= a

				// integers
				// >=a & <=b
				//     a   if b-a == 0
				//     _|_ if a < b
				// >=a & <b
				//     a   if b-a == 1
				//     _|_ if b <= a
				// >a  & <=b
				//     b   if b-a == 1
				//     _|_ if b <= a
				// >a  & <b
				//     a+1 if b-a == 2
				//     _|_ if b <= a

				n := newNum(src, k&numKind, a.rep|b.rep)
				switch diff, err := d.Int64(); {
				case err != nil:

				case diff == 1:
					if k&floatKind == 0 {
						if x.Op == opGeq && y.Op == opLss {
							return n.set(&lo)
						}
						if x.Op == opGtr && y.Op == opLeq {
							return n.set(&hi)
						}
					}

				case diff == 2:
					if k&floatKind == 0 && x.Op == opGtr && y.Op == opLss {
						_, _ = apd.BaseContext.Add(&d, d.SetInt64(1), &lo)
						return n.set(&d)

					}

				case diff == 0:
					if x.Op == opGeq && y.Op == opLeq {
						return n.set(&lo)
					}
					fallthrough

				case d.Negative:
					return ctx.mkErr(newSrc, "conflicting bounds %v and %v",
						ctx.str(x), ctx.str(y))
				}

			case x.Op == opNeq:
				if !test(ctx, x, y.Op, xv, yv) {
					return y
				}

			case y.Op == opNeq:
				if !test(ctx, x, x.Op, yv, xv) {
					return x
				}
			}
			return &unification{newSrc, []evaluated{x, y}}

		case *numLit:
			if err := checkBounds(ctx, src, x, x.Op, y, xv); err != nil {
				return err
			}
			// Narrow down number type.
			if y.K != k {
				return y.specialize(k)
			}
			return other

		case *nullLit, *boolLit, *durationLit, *list, *structLit, *stringLit, *bytesLit:
			// All remaining concrete types. This includes non-comparable types
			// for comparison to null.
			if err := checkBounds(ctx, src, x, x.Op, y, xv); err != nil {
				return err
			}
			return y
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *customValidator) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	newSrc := binSrc(src.Pos(), op, x, other)
	switch op {
	case opUnify, opUnifyUnchecked:
		k, _, msg := matchBinOpKind(opUnify, x.Kind(), other.Kind())
		if k == bottomKind {
			return ctx.mkErr(src, msg, op, ctx.str(x), ctx.str(other), x.Kind(), other.Kind())
		}
		switch y := other.(type) {
		case *basicType:
			k := unifyType(x.Kind(), y.Kind())
			if k == x.Kind() {
				return x
			}
			return &unification{newSrc, []evaluated{x, y}}

		case *customValidator:
			return &unification{newSrc, []evaluated{x, y}}

		case *bound:
			return &unification{newSrc, []evaluated{x, y}}

		case *numLit:
			if err := x.check(ctx, y); err != nil {
				return err
			}
			// Narrow down number type.
			if y.K != k {
				return y.specialize(k)
			}
			return other

		case *nullLit, *boolLit, *durationLit, *list, *structLit, *stringLit, *bytesLit:
			// All remaining concrete types. This includes non-comparable types
			// for comparison to null.
			if err := x.check(ctx, y); err != nil {
				return err
			}
			return y
		}
	}
	return ctx.mkErr(src, "invalid operation %v and %v (operator not defined for custom validator)", ctx.str(x), ctx.str(other))
}

func (x *customValidator) check(ctx *context, v evaluated) evaluated {
	args := make([]evaluated, 1+len(x.Args))
	args[0] = v
	for i, v := range x.Args {
		args[1+i] = v.(evaluated)
	}
	res := x.Builtin.call(ctx, x, args...)
	if isBottom(res) {
		return res.(evaluated)
	}
	if b, ok := res.(*boolLit); !ok {
		// should never reach here
		return ctx.mkErr(x, "invalid custom validator")
	} else if !b.B {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%s.%s", ctx.LabelStr(x.Builtin.pkg), x.Builtin.Name)
		buf.WriteString("(")
		for _, a := range x.Args {
			buf.WriteString(ctx.str(a))
		}
		buf.WriteString(")")
		return ctx.mkErr(x, "invalid value %s (does not satisfy %s)", ctx.str(v), buf.String())
	}
	return nil
}

func evalLambda(ctx *context, a value, finalize bool) (l *lambdaExpr, err evaluated) {
	if a == nil {
		return nil, nil
	}
	// NOTE: the values of a lambda might still be a disjunction
	e := ctx.manifest(a)
	if isBottom(e) {
		return nil, e
	}
	l, ok := e.(*lambdaExpr)
	if !ok {
		return nil, ctx.mkErr(a, "value must be lambda")
	}
	lambda := ctx.deref(l).(*lambdaExpr)
	if finalize {
		lambda.value = wrapFinalize(ctx, lambda.value)
	}
	return lambda, nil
}

func (x *structLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	y, ok := other.(*structLit)
	unchecked, isUnify := op.unifyType()
	if !ok || !isUnify {
		return ctx.mkIncompatible(src, op, x, other)
	}

	// TODO: unify emit

	x = ctx.deref(x).(*structLit)
	y = ctx.deref(y).(*structLit)
	if x == y {
		return x
	}
	arcs := make(arcs, 0, len(x.arcs)+len(y.arcs))
	var base baseValue
	if src.computed() != nil {
		base = baseValue{src.computed()}
	} else {
		base = binSrc(src.Pos(), op, x, other)
	}
	obj := &structLit{
		base,                          // baseValue
		x.emit,                        // emit
		nil,                           // template
		x.closeStatus | y.closeStatus, // closeStatus
		nil,                           // comprehensions
		arcs,                          // arcs
		nil,                           // attributes
	}
	defer ctx.pushForwards(x, obj, y, obj).popForwards()

	optionals, err := unifyOptionals(ctx, src, op, x, y)
	if err != nil {
		return err
	}
	obj.optionals = optionals

	// If unifying with a closed struct that does not have a template,
	// we need to apply the template to all elements.

	sz := len(x.comprehensions) + len(y.comprehensions)
	obj.comprehensions = make([]compValue, sz)
	for i, c := range x.comprehensions {
		obj.comprehensions[i] = compValue{
			checked: c.checked || (!unchecked && y.isClosed()),
			comp:    ctx.copy(c.comp),
		}
	}
	for i, c := range y.comprehensions {
		obj.comprehensions[i+len(x.comprehensions)] = compValue{
			checked: c.checked || (!unchecked && x.isClosed()),
			comp:    ctx.copy(c.comp),
		}
	}

	for _, a := range x.arcs {
		found := false
		for _, b := range y.arcs {
			if a.feature == b.feature {
				found = true
				break
			}
		}
		if !unchecked && !found && !y.allows(ctx, a.feature) && !a.definition {
			if a.optional {
				continue
			}
			// TODO: pass position of key, not value. Currently does not have
			// a position.
			return ctx.mkErr(a.v, a.v, "field %q not allowed in closed struct",
				ctx.LabelStr(a.feature))
		}
		cp := ctx.copy(a.v)
		obj.arcs = append(obj.arcs,
			arc{a.feature, a.optional, a.definition, cp, nil, a.attrs, a.docs})
	}
outer:
	for _, a := range y.arcs {
		v := ctx.copy(a.v)
		found := false
		for i, b := range obj.arcs {
			if a.feature == b.feature {
				found = true
				if a.definition != b.definition {
					src := binSrc(x.Pos(), op, a.v, b.v)
					return ctx.mkErr(src, "field %q declared as definition and regular field",
						ctx.LabelStr(a.feature))
				}
				w := b.v
				if x.closeStatus.shouldFinalize() {
					w = wrapFinalize(ctx, w)
				}
				if y.closeStatus.shouldFinalize() {
					v = wrapFinalize(ctx, v)
				}
				v = mkBin(ctx, src.Pos(), op, w, v)
				obj.arcs[i].v = v
				obj.arcs[i].cache = nil
				obj.arcs[i].optional = a.optional && b.optional
				obj.arcs[i].docs = mergeDocs(a.docs, b.docs)
				attrs, err := unifyAttrs(ctx, src, a.attrs, b.attrs)
				if err != nil {
					return err
				}
				obj.arcs[i].attrs = attrs
				continue outer
			}
		}
		if !unchecked && !found && !x.allows(ctx, a.feature) && !a.definition {
			if a.optional {
				continue
			}
			// TODO: pass position of key, not value. Currently does not have a
			// position.
			return ctx.mkErr(a.v, x, "field %q not allowed in closed struct",
				ctx.LabelStr(a.feature))
		}
		a.setValue(v)
		obj.arcs = append(obj.arcs, a)
	}
	sort.Stable(obj)

	if unchecked && obj.optionals.isFull() {
		obj.closeStatus.unclose()
	}

	return obj
}

func (x *structLit) rewriteOpt(ctx *context) (*optionals, evaluated) {
	fn := func(v value) value {
		if l, ok := v.(*lambdaExpr); ok {
			l, err := evalLambda(ctx, l, x.closeStatus.shouldFinalize())
			if err != nil {
				return err
			}
			v = l
		}
		return ctx.copy(v)
	}
	c, err := x.optionals.rewrite(fn)
	if err != nil {
		return c, err
	}
	return c, nil
}

func unifyOptionals(ctx *context, src source, op op, x, y *structLit) (o *optionals, err evaluated) {
	if x.optionals == nil && y.optionals == nil {
		return nil, nil
	}
	left, err := x.rewriteOpt(ctx)
	if err != nil {
		return left, err
	}
	right, err := y.rewriteOpt(ctx)
	if err != nil {
		return right, err
	}

	closeStatus := x.closeStatus | y.closeStatus
	switch {
	case left.isDotDotDot() && right.isDotDotDot():

	case left == nil && (!x.closeStatus.isClosed() || op == opUnifyUnchecked):
		return right, nil

	case right == nil && (!y.closeStatus.isClosed() || op == opUnifyUnchecked):
		return left, nil

	case op == opUnify && closeStatus.isClosed(),
		left != nil && (left.left != nil || left.right != nil),
		right != nil && (right.left != nil || right.right != nil):
		return &optionals{closeStatus, op, left, right, nil}, nil
	}

	// opUnify where both structs are open or opUnifyUnchecked
	for _, f := range right.fields {
		left.add(ctx, f.key, f.value)
	}
	return left, nil
}

func (x *nullLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	// TODO: consider using binSrc instead of src.base() for better traceability.
	switch other.(type) {
	case *nullLit:
		switch op {
		case opEql:
			return &boolLit{baseValue: src.base(), B: true}
		case opNeq:
			return &boolLit{baseValue: src.base(), B: false}
		case opUnify, opUnifyUnchecked:
			return x
		}

	case *bound:
		// Not strictly necessary, but handling this results in better error
		// messages.
		if op == opUnify || op == opUnifyUnchecked {
			return other.binOp(ctx, src, opUnify, x)
		}

	default:
		switch op {
		case opEql:
			return &boolLit{baseValue: src.base(), B: false}
		case opNeq:
			return &boolLit{baseValue: src.base(), B: true}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *boolLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	case *basicType:
		// range math
		return x

	case *boolLit:
		switch op {
		case opUnify, opUnifyUnchecked:
			if x.B != y.B {
				return ctx.mkErr(x, "conflicting values %v and %v", x.B, y.B)
			}
			return x
		case opLand:
			return boolTonode(src, x.B && y.B)
		case opLor:
			return boolTonode(src, x.B || y.B)
		case opEql:
			return boolTonode(src, x.B == y.B)
		case opNeq:
			return boolTonode(src, x.B != y.B)
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *stringLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	// case *basicType:
	// 	return x

	// TODO: rangelit

	case *stringLit:
		str := other.strValue()
		switch op {
		case opUnify, opUnifyUnchecked:
			str := other.strValue()
			if x.Str != str {
				src := mkBin(ctx, src.Pos(), op, x, other)
				return ctx.mkErr(src, "conflicting values %v and %v",
					ctx.str(x), ctx.str(y))
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, strings.Compare(x.Str, str))
		case opAdd:
			src := binSrc(src.Pos(), op, x, other)
			return &stringLit{src, x.Str + str, nil}
		case opMat:
			if y.RE == nil {
				// This really should not happen, but leave in for safety.
				b, err := regexp.MatchString(str, x.Str)
				if err != nil {
					return ctx.mkErr(src, "error parsing regexp: %v", err)
				}
				return boolTonode(src, b)
			}
			return boolTonode(src, y.RE.MatchString(x.Str))
		case opNMat:
			if y.RE == nil {
				// This really should not happen, but leave in for safety.
				b, err := regexp.MatchString(str, x.Str)
				if err != nil {
					return ctx.mkErr(src, "error parsing regexp: %v", err)
				}
				return boolTonode(src, !b)
			}
			return boolTonode(src, !y.RE.MatchString(x.Str))
		}
	case *numLit:
		switch op {
		case opMul:
			src := binSrc(src.Pos(), op, x, other)
			return &stringLit{src, strings.Repeat(x.Str, y.intValue(ctx)), nil}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *bytesLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	// case *basicType:
	// 	return x

	// TODO: rangelit

	case *bytesLit:
		b := y.B
		switch op {
		case opUnify, opUnifyUnchecked:
			if !bytes.Equal(x.B, b) {
				return ctx.mkErr(x, "conflicting values %v and %v",
					ctx.str(x), ctx.str(y))
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, bytes.Compare(x.B, b))
		case opAdd:
			copy := append([]byte(nil), x.B...)
			copy = append(copy, b...)
			return &bytesLit{binSrc(src.Pos(), op, x, other), copy, nil}
		}

	case *numLit:
		switch op {
		case opMul:
			src := binSrc(src.Pos(), op, x, other)
			return &bytesLit{src, bytes.Repeat(x.B, y.intValue(ctx)), nil}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func test(ctx *context, src source, op op, a, b evaluated) bool {
	v := binOp(ctx, src, op, a, b)
	if isBottom(v) {
		return false
	}
	return v.(*boolLit).B
}

func leq(ctx *context, src source, a, b evaluated) bool {
	if isTop(a) || isTop(b) {
		return true
	}
	v := binOp(ctx, src, opLeq, a, b)
	if isBottom(v) {
		return false
	}
	return v.(*boolLit).B
}

// TODO: should these go?
func maxNum(v value) value {
	switch x := v.(type) {
	case *numLit:
		return x
	case *bound:
		switch x.Op {
		case opLeq:
			return x.Expr
		case opLss:
			return &binaryExpr{x.baseValue, opSub, x.Expr, one}
		}
		return &basicType{x.baseValue, intKind}
	}
	return v
}

func minNum(v value) value {
	switch x := v.(type) {
	case *numLit:
		return x
	case *bound:
		switch x.Op {
		case opGeq:
			return x.Expr
		case opGtr:
			return &binaryExpr{x.baseValue, opAdd, x.Expr, one}
		}
		return &basicType{x.baseValue, intKind}
	}
	return v
}

func cmpTonode(src source, op op, r int) evaluated {
	result := false
	switch op {
	case opLss:
		result = r == -1
	case opLeq:
		result = r != 1
	case opEql, opUnify, opUnifyUnchecked:
		result = r == 0
	case opNeq:
		result = r != 0
	case opGeq:
		result = r != -1
	case opGtr:
		result = r == 1
	}
	return boolTonode(src, result)
}

func (x *numLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	case *basicType, *bound, *customValidator: // for better error reporting
		if op == opUnify || op == opUnifyUnchecked {
			return y.binOp(ctx, src, op, x)
		}
	case *numLit:
		k, _, _ := matchBinOpKind(op, x.Kind(), y.Kind())
		if k == bottomKind {
			break
		}
		switch op {
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, x.X.Cmp(&y.X))
		}
		n := newNum(src.base(), k, x.rep|y.rep)
		switch op {
		case opUnify, opUnifyUnchecked:
			if x.X.Cmp(&y.X) != 0 {
				src = mkBin(ctx, src.Pos(), op, x, other)
				return ctx.mkErr(src, "conflicting values %v and %v",
					ctx.str(x), ctx.str(y))
			}
			if k != x.K {
				n.X = x.X
				return n
			}
			return x
		case opAdd:
			_, _ = ctx.Add(&n.X, &x.X, &y.X)
		case opSub:
			_, _ = ctx.Sub(&n.X, &x.X, &y.X)
		case opMul:
			_, _ = ctx.Mul(&n.X, &x.X, &y.X)
		case opQuo:
			cond, err := ctx.Quo(&n.X, &x.X, &y.X)
			if err != nil {
				return ctx.mkErr(src, err.Error())
			}
			if cond.DivisionByZero() {
				return ctx.mkErr(src, "division by zero")
			}
			n.K = floatKind
		case opIDiv:
			if y.X.IsZero() {
				return ctx.mkErr(src, "division by zero")
			}
			intOp(ctx, n, (*big.Int).Div, x, y)
		case opIMod:
			if y.X.IsZero() {
				return ctx.mkErr(src, "division by zero")
			}
			intOp(ctx, n, (*big.Int).Mod, x, y)
		case opIQuo:
			if y.X.IsZero() {
				return ctx.mkErr(src, "division by zero")
			}
			intOp(ctx, n, (*big.Int).Quo, x, y)
		case opIRem:
			if y.X.IsZero() {
				return ctx.mkErr(src, "division by zero")
			}
			intOp(ctx, n, (*big.Int).Rem, x, y)
		}
		return n

	case *durationLit:
		if op == opMul {
			fd := float64(y.d)
			// TODO: check range
			f, _ := x.X.Float64()
			d := time.Duration(f * fd)
			return &durationLit{binSrc(src.Pos(), op, x, other), d}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

type intFunc func(z, x, y *big.Int) *big.Int

func intOp(ctx *context, n *numLit, fn intFunc, a, b *numLit) {
	var x, y apd.Decimal
	_, _ = ctx.RoundToIntegralValue(&x, &a.X)
	if x.Negative {
		x.Coeff.Neg(&x.Coeff)
	}
	_, _ = ctx.RoundToIntegralValue(&y, &b.X)
	if y.Negative {
		y.Coeff.Neg(&y.Coeff)
	}
	fn(&n.X.Coeff, &x.Coeff, &y.Coeff)
	if n.X.Coeff.Sign() < 0 {
		n.X.Coeff.Neg(&n.X.Coeff)
		n.X.Negative = true
	}
	n.K = intKind
}

// TODO: check overflow

func (x *durationLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	case *basicType:
		// infinity math

	case *durationLit:
		switch op {
		case opUnify, opUnifyUnchecked:
			if x.d != y.d {
				return ctx.mkIncompatible(src, op, x, other)
			}
			return other
		case opLss:
			return boolTonode(src, x.d < y.d)
		case opLeq:
			return boolTonode(src, x.d <= y.d)
		case opEql:
			return boolTonode(src, x.d == y.d)
		case opNeq:
			return boolTonode(src, x.d != y.d)
		case opGeq:
			return boolTonode(src, x.d >= y.d)
		case opGtr:
			return boolTonode(src, x.d > y.d)
		case opAdd:
			return &durationLit{binSrc(src.Pos(), op, x, other), x.d + y.d}
		case opSub:
			return &durationLit{binSrc(src.Pos(), op, x, other), x.d - y.d}
		case opQuo:
			n := newFloat(src.base(), base10).setInt64(int64(x.d))
			d := apd.New(int64(y.d), 0)
			// TODO: check result if this code becomes undead.
			_, _ = ctx.Quo(&n.X, &n.X, d)
			return n
		case opIRem:
			n := newInt(src.base(), base10).setInt64(int64(x.d % y.d))
			n.X.Exponent = -9
			return n
		}

	case *numLit:
		switch op {
		case opMul:
			// TODO: check range
			f, _ := y.X.Float64()
			d := time.Duration(float64(x.d) * f)
			return &durationLit{binSrc(src.Pos(), op, x, other), d}
		case opQuo:
			// TODO: check range
			f, _ := y.X.Float64()
			d := time.Duration(float64(x.d) * f)
			return &durationLit{binSrc(src.Pos(), op, x, other), d}
		case opIRem:
			d := x.d % time.Duration(y.intValue(ctx))
			return &durationLit{binSrc(src.Pos(), op, x, other), d}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *list) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch op {
	case opUnify, opUnifyUnchecked:
		y, ok := other.(*list)
		if !ok {
			break
		}

		n := binOp(ctx, src, op, x.len.(evaluated), y.len.(evaluated))
		if isBottom(n) {
			src = mkBin(ctx, src.Pos(), op, x, other)
			return ctx.mkErr(src, "conflicting list lengths: %v", n)
		}
		sx := x.elem.arcs
		xa := sx
		sy := y.elem.arcs
		ya := sy
		for len(xa) < len(ya) {
			xa = append(xa, arc{feature: label(len(xa)), v: x.typ})
		}
		for len(ya) < len(xa) {
			ya = append(ya, arc{feature: label(len(ya)), v: y.typ})
		}

		typ := x.typ
		max, ok := n.(*numLit)
		if !ok || len(xa) < max.intValue(ctx) {
			typ = mkBin(ctx, src.Pos(), op, x.typ, y.typ)
		}

		// TODO: use forwarding instead of this mild hack.
		x.elem.arcs = xa
		y.elem.arcs = ya
		s := binOp(ctx, src, op, x.elem, y.elem).(*structLit)
		x.elem.arcs = sx
		y.elem.arcs = sy

		base := binSrc(src.Pos(), op, x, other)
		return &list{baseValue: base, elem: s, typ: typ, len: n}

	case opEql, opNeq:
		y, ok := other.(*list)
		if !ok {
			break
		}
		if len(x.elem.arcs) != len(y.elem.arcs) {
			return boolTonode(src, false)
		}
		for i := range x.elem.arcs {
			if !test(ctx, src, op, x.at(ctx, i), y.at(ctx, i)) {
				return boolTonode(src, false)
			}
		}
		return boolTonode(src, true)

	case opAdd:
		y, ok := other.(*list)
		if !ok {
			break
		}
		n := &list{baseValue: binSrc(src.Pos(), op, x, other), typ: y.typ}
		arcs := []arc{}
		for _, v := range x.elem.arcs {
			arcs = append(arcs, arc{feature: label(len(arcs)), v: v.v})
		}
		for _, v := range y.elem.arcs {
			arcs = append(arcs, arc{feature: label(len(arcs)), v: v.v})
		}
		switch v := y.len.(type) {
		case *numLit:
			// Closed list
			n.len = newInt(v.base(), v.rep).setInt(len(arcs))
		default:
			// Open list
			n.len = y.len // TODO: add length of x?
		}
		n.elem = &structLit{baseValue: n.baseValue, arcs: arcs}
		return n

	case opMul:
		k := other.Kind()
		if !k.isAnyOf(intKind) {
			panic("multiplication must be int type")
		}
		n := &list{baseValue: binSrc(src.Pos(), op, x, other), typ: x.typ}
		arcs := []arc{}
		if len(x.elem.arcs) > 0 {
			if !k.isGround() {
				// should never reach here.
				break
			}
			if ln := other.(*numLit).intValue(ctx); ln > 0 {
				for i := 0; i < ln; i++ {
					// TODO: copy values
					for _, a := range x.elem.arcs {
						arcs = append(arcs, arc{feature: label(len(arcs)), v: a.v})
					}
				}
			} else if ln < 0 {
				return ctx.mkErr(src, "negative number %d multiplies list", ln)
			}
		}
		switch v := x.len.(type) {
		case *numLit:
			// Closed list
			n.len = newInt(v.base(), v.rep).setInt(len(arcs))
		default:
			// Open list
			n.len = x.len // TODO: multiply length?
		}
		n.elem = &structLit{baseValue: n.baseValue, arcs: arcs}
		return n
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *lambdaExpr) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	if y, ok := other.(*lambdaExpr); ok && op == opUnify {
		x = ctx.deref(x).(*lambdaExpr)
		y = ctx.deref(y).(*lambdaExpr)
		n, m := len(x.params.arcs), len(y.params.arcs)
		if n != m {
			src = mkBin(ctx, src.Pos(), op, x, other)
			return ctx.mkErr(src, "number of params should match (%d != %d)", n, m)
		}
		arcs := make([]arc, len(x.arcs))
		lambda := &lambdaExpr{binSrc(src.Pos(), op, x, other), &params{arcs}, nil}
		defer ctx.pushForwards(x, lambda, y, lambda).popForwards()

		xVal := ctx.copy(x.value)
		yVal := ctx.copy(y.value)
		lambda.value = mkBin(ctx, src.Pos(), opUnify, xVal, yVal)

		for i := range arcs {
			xArg := ctx.copy(x.at(ctx, i)).(evaluated)
			yArg := ctx.copy(y.at(ctx, i)).(evaluated)
			v := binOp(ctx, src, op, xArg, yArg)
			if isBottom(v) {
				return v
			}
			arcs[i] = arc{feature: x.arcs[i].feature, v: v}
		}

		return lambda
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *builtin) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	if _, isUnify := op.unifyType(); isUnify && evaluated(x) == other {
		return x
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *feed) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *guard) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *yield) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	return ctx.mkIncompatible(src, op, x, other)
}

func (x *fieldComprehension) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	return ctx.mkIncompatible(src, op, x, other)
}
