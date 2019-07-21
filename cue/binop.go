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

	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd/v2"
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

	leftKind := left.kind()
	rightKind := right.kind()
	kind, invert, msg := matchBinOpKind(op, leftKind, rightKind)
	if kind == bottomKind {
		simplify := func(v, orig value) value {
			switch x := v.(type) {
			case *disjunction:
				return orig
			case *binaryExpr:
				if x.op == opDisjunction {
					return orig
				}
			default:
				return x
			}
			return v
		}
		var l, r value = left, right
		if x, ok := src.(*binaryExpr); ok {
			l = simplify(x.left, left)
			r = simplify(x.right, right)
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
		if dx.hasDefaults {
			mark = true
			d.hasDefaults = true
		}
		for _, dxv := range dx.values {
			m := dxv.marked || !dx.hasDefaults
			dist(ctx, d, mark, op, mVal{dxv.val.evalPartial(ctx), m}, y)
		}
		return
	}
	if dy, ok := y.val.(*disjunction); ok {
		if dy.hasDefaults {
			mark = true
			d.hasDefaults = true
		}
		for _, dxy := range dy.values {
			m := dxy.marked || !dy.hasDefaults
			dist(ctx, d, mark, op, x, mVal{dxy.val.evalPartial(ctx), m})
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

func (x *unification) add(ctx *context, src source, v evaluated) evaluated {
	for progress := true; progress; {
		progress = false
		k := 0

		for i, vx := range x.values {
			a := binOp(ctx, src, opUnify, vx, v)
			switch _, isUnify := a.(*unification); {
			case isBottom(a):
				if !isIncomplete(a) {
					return a
				}
				fallthrough
			case isUnify:
				x.values[k] = x.values[i]
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
		x.values = x.values[:k]
	}
	x.values = append(x.values, v)
	return nil
}

func (x *unification) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	if op == opUnify {
		u := &unification{baseValue: baseValue{src}}
		u.values = append(u.values, x.values...)
		if y, ok := other.(*unification); ok {
			for _, vy := range y.values {
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
	case opUnify:
		return other
	}
	src = mkBin(ctx, src.Pos(), op, x, other)
	return ctx.mkErr(src, codeIncomplete, "binary operation on (incomplete) top value")
}

func (x *basicType) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	k := unifyType(x.kind(), other.kind())
	switch y := other.(type) {
	case *basicType:
		switch op {
		// TODO: other types.
		case opUnify:
			if k&typeKinds != bottomKind {
				return &basicType{binSrc(src.Pos(), op, x, other), k & typeKinds}
			}
		}

	case *bound:
		src = mkBin(ctx, src.Pos(), op, x, other)
		return ctx.mkErr(src, codeIncomplete, "%s with incomplete values", op)

	case *numLit:
		if op == opUnify {
			if k == y.k {
				return y
			}
			i := *y
			i.k = k
			return &i
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
	if isBottom(v) || !v.(*boolLit).b {
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
	switch r.op {
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
	xv := x.value.(evaluated)

	newSrc := binSrc(src.Pos(), op, x, other)
	switch op {
	case opUnify:
		k, _, msg := matchBinOpKind(opUnify, x.kind(), other.kind())
		if k == bottomKind {
			return ctx.mkErr(src, msg, opUnify, ctx.str(x), ctx.str(other), x.kind(), other.kind())
		}
		switch y := other.(type) {
		case *basicType:
			k := unifyType(x.k, y.kind())
			if k == x.k {
				return x
			}
			return newBound(ctx, newSrc.base(), x.op, k, xv)

		case *bound:
			yv := y.value.(evaluated)
			if !xv.kind().isGround() || !yv.kind().isGround() {
				return ctx.mkErr(newSrc, codeIncomplete, "cannot add incomplete values")
			}

			cmp, xCat := opInfo(x.op)
			_, yCat := opInfo(y.op)

			switch {
			case xCat == yCat:
				if x.op == opNeq || x.op == opMat || x.op == opNMat {
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
				a, aOK := x.value.(evaluated).(*numLit)
				b, bOK := y.value.(evaluated).(*numLit)

				if !aOK || !bOK {
					break
				}

				var d apd.Decimal
				cond, err := apd.BaseContext.Sub(&d, &b.v, &a.v)
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

				switch diff, err := d.Int64(); {
				case err != nil:

				case diff == 1:
					if k&floatKind == 0 {
						if x.op == opGeq && y.op == opLss {
							return a
						}
						if x.op == opGtr && y.op == opLeq {
							return b
						}
					}

				case diff == 2:
					if k&floatKind == 0 && x.op == opGtr && y.op == opLss {
						_, _ = apd.BaseContext.Add(&d, d.SetInt64(1), &a.v)
						n := *a
						n.k = k
						n.v = d
						return &n
					}

				case diff == 0:
					if x.op == opGeq && y.op == opLeq {
						return a
					}
					fallthrough

				case d.Negative:
					return ctx.mkErr(newSrc, "conflicting bounds %v and %v",
						ctx.str(x), ctx.str(y))
				}

			case x.op == opNeq:
				if !test(ctx, x, y.op, xv, yv) {
					return y
				}

			case y.op == opNeq:
				if !test(ctx, x, x.op, yv, xv) {
					return x
				}
			}
			return &unification{newSrc, []evaluated{x, y}}

		case *numLit:
			if err := checkBounds(ctx, src, x, x.op, y, xv); err != nil {
				return err
			}
			// Narrow down number type.
			if y.k != k {
				n := *y
				n.k = k
				return &n
			}
			return other

		case *nullLit, *boolLit, *durationLit, *list, *structLit, *stringLit, *bytesLit:
			// All remaining concrete types. This includes non-comparable types
			// for comparison to null.
			if err := checkBounds(ctx, src, x, x.op, y, xv); err != nil {
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
	case opUnify:
		k, _, msg := matchBinOpKind(opUnify, x.kind(), other.kind())
		if k == bottomKind {
			return ctx.mkErr(src, msg, op, ctx.str(x), ctx.str(other), x.kind(), other.kind())
		}
		switch y := other.(type) {
		case *basicType:
			k := unifyType(x.kind(), y.kind())
			if k == x.kind() {
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
			if y.k != k {
				n := *y
				n.k = k
				return &n
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
	args := make([]evaluated, 1+len(x.args))
	args[0] = v
	for i, v := range x.args {
		args[1+i] = v.(evaluated)
	}
	res := x.call.call(ctx, x, args...)
	if isBottom(res) {
		return res.(evaluated)
	}
	if b, ok := res.(*boolLit); !ok {
		// should never reach here
		return ctx.mkErr(x, "invalid custom validator")
	} else if !b.b {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%s.%s", ctx.labelStr(x.call.pkg), x.call.Name)
		buf.WriteString("(")
		for _, a := range x.args {
			buf.WriteString(ctx.str(a))
		}
		buf.WriteString(")")
		return ctx.mkErr(x, "invalid value %s (does not satisfy %s)", ctx.str(v), buf.String())
	}
	return nil
}

func evalLambda(ctx *context, a value) (l *lambdaExpr, err evaluated) {
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
	return ctx.deref(l).(*lambdaExpr), nil
}

func (x *structLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	y, ok := other.(*structLit)
	_, isUnify := op.unifyType()
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
	obj := &structLit{
		binSrc(src.Pos(), op, x, other), // baseValue
		x.emit,                          // emit
		nil,                             // template
		nil,                             // comprehensions
		arcs,                            // arcs
		nil,                             // attributes
	}
	defer ctx.pushForwards(x, obj, y, obj).popForwards()

	tx, ex := evalLambda(ctx, x.template)
	ty, ey := evalLambda(ctx, y.template)

	var t *lambdaExpr
	switch {
	case ex != nil:
		return ex
	case ey != nil:
		return ey
	case tx != nil:
		t = tx
	case ty != nil:
		t = ty
	}
	if tx != ty && tx != nil && ty != nil {
		v := binOp(ctx, src, opUnify, tx, ty)
		if isBottom(v) {
			return v
		}
		t = v.(*lambdaExpr)
	}
	if t != nil {
		obj.template = ctx.copy(t)
	}

	sz := len(x.comprehensions) + len(y.comprehensions)
	obj.comprehensions = make([]*fieldComprehension, sz)
	for i, c := range x.comprehensions {
		obj.comprehensions[i] = ctx.copy(c).(*fieldComprehension)
	}
	for i, c := range y.comprehensions {
		obj.comprehensions[i+len(x.comprehensions)] = ctx.copy(c).(*fieldComprehension)
	}

	for _, a := range x.arcs {
		cp := ctx.copy(a.v)
		obj.arcs = append(obj.arcs,
			arc{a.feature, a.optional, cp, nil, a.attrs, a.docs})
	}
outer:
	for _, a := range y.arcs {
		v := ctx.copy(a.v)
		for i, b := range obj.arcs {
			if a.feature == b.feature {
				v = mkBin(ctx, src.Pos(), opUnify, b.v, v)
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
		a.setValue(v)
		obj.arcs = append(obj.arcs, a)
	}
	sort.Stable(obj)

	return obj
}

func (x *nullLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	// TODO: consider using binSrc instead of src.base() for better traceability.
	switch other.(type) {
	case *nullLit:
		switch op {
		case opEql:
			return &boolLit{baseValue: src.base(), b: true}
		case opNeq:
			return &boolLit{baseValue: src.base(), b: false}
		case opUnify:
			return x
		}

	case *bound:
		// Not strictly necessary, but handling this results in better error
		// messages.
		if op == opUnify {
			return other.binOp(ctx, src, opUnify, x)
		}

	default:
		switch op {
		case opEql:
			return &boolLit{baseValue: src.base(), b: false}
		case opNeq:
			return &boolLit{baseValue: src.base(), b: true}
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
		case opUnify:
			if x.b != y.b {
				return ctx.mkErr(x, "conflicting values %v and %v", x.b, y.b)
			}
			return x
		case opLand:
			return boolTonode(src, x.b && y.b)
		case opLor:
			return boolTonode(src, x.b || y.b)
		case opEql:
			return boolTonode(src, x.b == y.b)
		case opNeq:
			return boolTonode(src, x.b != y.b)
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
		case opUnify:
			str := other.strValue()
			if x.str != str {
				src := mkBin(ctx, src.Pos(), op, x, other)
				return ctx.mkErr(src, "conflicting values %v and %v",
					ctx.str(x), ctx.str(y))
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, strings.Compare(x.str, str))
		case opAdd:
			src := binSrc(src.Pos(), op, x, other)
			return &stringLit{src, x.str + str, nil}
		case opMat:
			if y.re == nil {
				// This really should not happen, but leave in for safety.
				b, err := regexp.MatchString(str, x.str)
				if err != nil {
					return ctx.mkErr(src, "error parsing regexp: %v", err)
				}
				return boolTonode(src, b)
			}
			return boolTonode(src, y.re.MatchString(x.str))
		case opNMat:
			if y.re == nil {
				// This really should not happen, but leave in for safety.
				b, err := regexp.MatchString(str, x.str)
				if err != nil {
					return ctx.mkErr(src, "error parsing regexp: %v", err)
				}
				return boolTonode(src, !b)
			}
			return boolTonode(src, !y.re.MatchString(x.str))
		}
	case *numLit:
		switch op {
		case opMul:
			src := binSrc(src.Pos(), op, x, other)
			return &stringLit{src, strings.Repeat(x.str, y.intValue(ctx)), nil}
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
		b := y.b
		switch op {
		case opUnify:
			if !bytes.Equal(x.b, b) {
				return ctx.mkErr(x, "conflicting values %v and %v",
					ctx.str(x), ctx.str(y))
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, bytes.Compare(x.b, b))
		case opAdd:
			copy := append([]byte(nil), x.b...)
			copy = append(copy, b...)
			return &bytesLit{binSrc(src.Pos(), op, x, other), copy, nil}
		}

	case *numLit:
		switch op {
		case opMul:
			src := binSrc(src.Pos(), op, x, other)
			return &bytesLit{src, bytes.Repeat(x.b, y.intValue(ctx)), nil}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

func test(ctx *context, src source, op op, a, b evaluated) bool {
	v := binOp(ctx, src, op, a, b)
	if isBottom(v) {
		return false
	}
	return v.(*boolLit).b
}

func leq(ctx *context, src source, a, b evaluated) bool {
	if isTop(a) || isTop(b) {
		return true
	}
	v := binOp(ctx, src, opLeq, a, b)
	if isBottom(v) {
		return false
	}
	return v.(*boolLit).b
}

// TODO: should these go?
func maxNum(v value) value {
	switch x := v.(type) {
	case *numLit:
		return x
	case *bound:
		switch x.op {
		case opLeq:
			return x.value
		case opLss:
			return &binaryExpr{x.baseValue, opSub, x.value, one}
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
		switch x.op {
		case opGeq:
			return x.value
		case opGtr:
			return &binaryExpr{x.baseValue, opAdd, x.value, one}
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
	case opEql, opUnify:
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
		if op == opUnify {
			return y.binOp(ctx, src, op, x)
		}
	case *numLit:
		k := unifyType(x.kind(), y.kind())
		n := newNumBin(k, x, y)
		switch op {
		case opUnify:
			if x.v.Cmp(&y.v) != 0 {
				src = mkBin(ctx, src.Pos(), op, x, other)
				return ctx.mkErr(src, "conflicting values %v and %v",
					ctx.str(x), ctx.str(y))
			}
			if k != x.k {
				n.v = x.v
				return n
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, x.v.Cmp(&y.v))
		case opAdd:
			_, _ = ctx.Add(&n.v, &x.v, &y.v)
		case opSub:
			_, _ = ctx.Sub(&n.v, &x.v, &y.v)
		case opMul:
			_, _ = ctx.Mul(&n.v, &x.v, &y.v)
		case opQuo:
			cond, _ := ctx.Quo(&n.v, &x.v, &y.v)
			if cond.DivisionByZero() {
				return ctx.mkErr(src, "divide by zero")
			}
			_, _, _ = ctx.Reduce(&n.v, &n.v)
			n.k = floatKind
		case opIDiv:
			if y.v.IsZero() {
				return ctx.mkErr(src, "divide by zero")
			}
			intOp(ctx, n, (*big.Int).Div, x, y)
		case opIMod:
			if y.v.IsZero() {
				return ctx.mkErr(src, "divide by zero")
			}
			intOp(ctx, n, (*big.Int).Mod, x, y)
		case opIQuo:
			if y.v.IsZero() {
				return ctx.mkErr(src, "divide by zero")
			}
			intOp(ctx, n, (*big.Int).Quo, x, y)
		case opIRem:
			if y.v.IsZero() {
				return ctx.mkErr(src, "divide by zero")
			}
			intOp(ctx, n, (*big.Int).Rem, x, y)
		}
		return n

	case *durationLit:
		if op == opMul {
			fd := float64(y.d)
			// TODO: check range
			f, _ := x.v.Float64()
			d := time.Duration(f * fd)
			return &durationLit{binSrc(src.Pos(), op, x, other), d}
		}
	}
	return ctx.mkIncompatible(src, op, x, other)
}

type intFunc func(z, x, y *big.Int) *big.Int

func intOp(ctx *context, n *numLit, fn intFunc, a, b *numLit) {
	var x, y apd.Decimal
	_, _ = ctx.RoundToIntegralValue(&x, &a.v)
	if x.Negative {
		x.Coeff.Neg(&x.Coeff)
	}
	_, _ = ctx.RoundToIntegralValue(&y, &b.v)
	if y.Negative {
		y.Coeff.Neg(&y.Coeff)
	}
	fn(&n.v.Coeff, &x.Coeff, &y.Coeff)
	if n.v.Coeff.Sign() < 0 {
		n.v.Coeff.Neg(&n.v.Coeff)
		n.v.Negative = true
	}
	n.k = intKind
}

// TODO: check overflow

func (x *durationLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	case *basicType:
		// infinity math

	case *durationLit:
		switch op {
		case opUnify:
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
			n := &numLit{
				numBase: newNumBase(nil, newNumInfo(floatKind, 0, 10, false)),
			}
			n.v.SetInt64(int64(x.d))
			d := apd.New(int64(y.d), 0)
			// TODO: check result if this code becomes undead.
			_, _ = ctx.Quo(&n.v, &n.v, d)
			return n
		case opIRem:
			n := &numLit{
				numBase: newNumBase(nil, newNumInfo(intKind, 0, 10, false)),
			}
			n.v.SetInt64(int64(x.d % y.d))
			n.v.Exponent = -9
			return n
		}

	case *numLit:
		switch op {
		case opMul:
			// TODO: check range
			f, _ := y.v.Float64()
			d := time.Duration(float64(x.d) * f)
			return &durationLit{binSrc(src.Pos(), op, x, other), d}
		case opQuo:
			// TODO: check range
			f, _ := y.v.Float64()
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
	case opUnify:
		y, ok := other.(*list)
		if !ok {
			break
		}

		n := binOp(ctx, src, opUnify, x.len.(evaluated), y.len.(evaluated))
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
			src := mkBin(ctx, src.Pos(), op, x.typ, y.typ)
			typ = binOp(ctx, src, opUnify, x.typ.(evaluated), y.typ.(evaluated))
			if isBottom(typ) {
				return ctx.mkErr(src, "conflicting list element types: %v", typ)
			}
		}

		// TODO: use forwarding instead of this mild hack.
		x.elem.arcs = xa
		y.elem.arcs = ya
		s := binOp(ctx, src, opUnify, x.elem, y.elem).(*structLit)
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
			ln := &numLit{numBase: v.numBase}
			ln.v.SetInt64(int64(len(arcs)))
			n.len = ln
		default:
			// Open list
			n.len = y.len // TODO: add length of x?
		}
		n.elem = &structLit{baseValue: n.baseValue, arcs: arcs}
		return n

	case opMul:
		k := other.kind()
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
			ln := &numLit{numBase: v.numBase}
			ln.v.SetInt64(int64(len(arcs)))
			n.len = ln
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
	if op == opUnify && evaluated(x) == other {
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
