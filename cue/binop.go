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
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd"
)

// binSrc returns a baseValue representing a binary expression of the given
// values.
func binSrc(pos token.Pos, op op, a, b value) baseValue {
	return baseValue{&computedSource{pos, op, a, b}}
}

func unify(ctx *context, src source, left, right evaluated) evaluated {
	return binOp(ctx, src, opUnify, left, right)
}

func binOp(ctx *context, src source, op op, left, right evaluated) (result evaluated) {
	if isBottom(left) {
		if op == opUnify && ctx.exprDepth == 0 && cycleError(left) != nil {
			ctx.cycleErr = true
			return right
		}
		return left
	}
	if isBottom(right) {
		if op == opUnify && ctx.exprDepth == 0 && cycleError(right) != nil {
			ctx.cycleErr = true
			return left
		}
		return right
	}

	leftKind := left.kind()
	rightKind := right.kind()
	kind, invert := matchBinOpKind(op, leftKind, rightKind)
	if kind == bottomKind {
		return ctx.mkIncompatible(src, op, left, right)
	}
	if kind.hasReferences() {
		panic("unexpected references in expression")
	}
	if invert {
		left, right = right, left
	}
	if op != opUnify {
		// Any operation other than unification or disjunction must be on
		// concrete types. Disjunction is handled separately.
		if !leftKind.isGround() || !rightKind.isGround() {
			return ctx.mkErr(src, codeIncomplete, "incomplete error")
		}
		ctx.exprDepth++
		v := left.binOp(ctx, src, op, right) // may return incomplete
		ctx.exprDepth--
		return v
	}

	// op == opUnify

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
		return distribute(ctx, src, dl, right)
	} else if dr, ok := right.(*disjunction); ok {
		return distribute(ctx, src, dr, left)
	}

	if _, ok := right.(*unification); ok {
		return right.binOp(ctx, src, opUnify, left)
	}

	// TODO: value may be incomplete if there is a cycle. Instead of an error
	// schedule an assert and return the atomic value, if applicable.
	v := left.binOp(ctx, src, op, right)
	if isBottom(v) {
		v := right.binOp(ctx, src, op, left)
		// Return the original failure if both fail, as this will result in
		// better error messages.
		if !isBottom(v) {
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
// unification operation. If allowCycle is true, references that resolve
// to a cycle are dropped.
func distribute(ctx *context, src source, x *disjunction, y evaluated) evaluated {
	return dist(ctx, src, x, mVal{y, false}).val
}

func dist(ctx *context, src source, dx *disjunction, y mVal) mVal {
	dn := &disjunction{src.base(), make([]dValue, 0, len(dx.values))}
	for _, dv := range dx.values {
		x := mVal{dv.val.evalPartial(ctx), dv.marked}
		src := binSrc(src.Pos(), opUnify, x.val, y.val)

		var v mVal
		if dy, ok := y.val.(*disjunction); ok {
			v = dist(ctx, src, dy, x)
		} else if ddv, ok := dv.val.(*disjunction); ok {
			v = dist(ctx, src, ddv, y)
		} else {
			v = mVal{binOp(ctx, src, opUnify, x.val, y.val), x.mark || y.mark}
		}
		dn.add(ctx, v.val, v.mark)
	}
	return dn.normalize(ctx, src)
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
	return ctx.mkErr(src, "binary operation on non-ground top value")
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
	msg := "%v not within bound %v"
	switch r.op {
	case opNeq, opNMat:
		msg = "%v excluded by %v"
	case opMat:
		msg = "%v does not match %v"
	}
	return ctx.mkErr(e, msg, debugStr(ctx, v), debugStr(ctx, r))
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
		k, _ := matchBinOpKind(opUnify, x.kind(), other.kind())
		if k == bottomKind {
			break
		}
		switch y := other.(type) {
		case *basicType:
			v := unify(ctx, src, xv, y)
			if v == xv {
				return x
			}
			return &bound{newSrc.base(), x.op, v}

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
						apd.BaseContext.Add(&d, d.SetInt64(1), &a.v)
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
					return ctx.mkErr(newSrc, "incompatible bounds %v and %v",
						debugStr(ctx, x), debugStr(ctx, y))
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
	if !ok || op != opUnify {
		return ctx.mkIncompatible(src, op, x, other)
	}

	// TODO: unify emit

	x = ctx.deref(x).(*structLit)
	y = ctx.deref(y).(*structLit)
	if x == y {
		return x
	}
	arcs := make(arcs, 0, len(x.arcs)+len(y.arcs))
	obj := &structLit{binSrc(src.Pos(), op, x, other), x.emit, nil, nil, arcs, nil}
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
			arc{a.feature, a.optional, cp, nil, a.attrs})
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
				return ctx.mkErr(x, "failed to unify: %v != %v", x.b, y.b)
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
				return ctx.mkErr(src, "failed to unify: %v != %v", x.str, str)
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, strings.Compare(x.str, str))
		case opAdd:
			src := binSrc(src.Pos(), op, x, other)
			return &stringLit{src, x.str + str}
		case opMat:
			b, err := regexp.MatchString(str, x.str)
			if err != nil {
				return ctx.mkErr(src, "error parsing regexp: %v", err)
			}
			return boolTonode(src, b)
		case opNMat:
			b, err := regexp.MatchString(str, x.str)
			if err != nil {
				return ctx.mkErr(src, "error parsing regexp: %v", err)
			}
			return boolTonode(src, !b)
		}
	case *numLit:
		switch op {
		case opMul:
			src := binSrc(src.Pos(), op, x, other)
			return &stringLit{src, strings.Repeat(x.str, y.intValue(ctx))}
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
				return ctx.mkErr(x, "failed to unify: %v != %v", x.b, b)
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, bytes.Compare(x.b, b))
		case opAdd:
			copy := append([]byte(nil), x.b...)
			copy = append(copy, b...)
			return &bytesLit{binSrc(src.Pos(), op, x, other), copy}
		}

	case *numLit:
		switch op {
		case opMul:
			src := binSrc(src.Pos(), op, x, other)
			return &bytesLit{src, bytes.Repeat(x.b, y.intValue(ctx))}
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

func (x *numLit) updateNumInfo(a, b *numLit) {
	x.numInfo = unifyNuminfo(a.numInfo, b.numInfo)
}

func (x *numLit) binOp(ctx *context, src source, op op, other evaluated) evaluated {
	switch y := other.(type) {
	case *basicType:
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
				return ctx.mkErr(src, "cannot unify numbers %v and %v", x.strValue(), y.strValue())
			}
			if k != x.k {
				n.v = x.v
				return n
			}
			return x
		case opLss, opLeq, opEql, opNeq, opGeq, opGtr:
			return cmpTonode(src, op, x.v.Cmp(&y.v))
		case opAdd:
			ctx.Add(&n.v, &x.v, &y.v)
		case opSub:
			ctx.Sub(&n.v, &x.v, &y.v)
		case opMul:
			ctx.Mul(&n.v, &x.v, &y.v)
		case opQuo:
			ctx.Quo(&n.v, &x.v, &y.v)
			ctx.Reduce(&n.v, &n.v)
			n.k = floatKind
		case opRem:
			ctx.Rem(&n.v, &x.v, &y.v)
			n.k = floatKind
		case opIDiv:
			intOp(ctx, n, (*big.Int).Div, x, y)
		case opIMod:
			intOp(ctx, n, (*big.Int).Mod, x, y)
		case opIQuo:
			intOp(ctx, n, (*big.Int).Quo, x, y)
		case opIRem:
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
	ctx.RoundToIntegralValue(&x, &a.v)
	if x.Negative {
		x.Coeff.Neg(&x.Coeff)
	}
	ctx.RoundToIntegralValue(&y, &b.v)
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
			ctx.Quo(&n.v, &n.v, d)
			return n
		case opRem:
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
		case opRem:
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

		n := unify(ctx, src, x.len.(evaluated), y.len.(evaluated))
		if isBottom(n) {
			src = mkBin(ctx, src.Pos(), op, x, other)
			return ctx.mkErr(src, "incompatible list lengths: %v", n)
		}
		var a, rest []value
		var rtyp value
		nx, ny := len(x.a), len(y.a)
		if nx < ny {
			a = make([]value, nx, ny)
			rest = y.a[nx:]
			rtyp = x.typ

		} else {
			a = make([]value, ny, nx)
			rest = x.a[ny:]
			rtyp = y.typ
		}
		typ := x.typ
		max, ok := n.(*numLit)
		if !ok || len(a)+len(rest) < max.intValue(ctx) {
			typ = unify(ctx, src, x.typ.(evaluated), y.typ.(evaluated))
			if isBottom(typ) {
				src = mkBin(ctx, src.Pos(), op, x, other)
				return ctx.mkErr(src, "incompatible list types: %v: ", typ)
			}
		}

		for i := range a {
			ai := unify(ctx, src, x.at(ctx, i).evalPartial(ctx), y.at(ctx, i).evalPartial(ctx))
			if isBottom(ai) {
				return ai
			}
			a[i] = ai
		}
		for _, n := range rest {
			an := unify(ctx, src, n.evalPartial(ctx), rtyp.(evaluated))
			if isBottom(an) {
				return an
			}
			a = append(a, an)
		}
		return &list{baseValue: binSrc(src.Pos(), op, x, other), a: a, typ: typ, len: n}

	case opEql, opNeq:
		y, ok := other.(*list)
		if !ok {
			break
		}
		if len(x.a) != len(y.a) {
			return boolTonode(src, false)
		}
		for i := range x.a {
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
		n.a = append(x.a, y.a...)
		switch v := y.len.(type) {
		case *numLit:
			// Closed list
			ln := &numLit{numBase: v.numBase}
			ln.v.SetInt64(int64(len(n.a)))
			n.len = ln
		default:
			// Open list
			n.len = y.len
		}
		return n

	case opMul:
		k := other.kind()
		if !k.isAnyOf(intKind) {
			panic("multiplication must be int type")
		}
		n := &list{baseValue: binSrc(src.Pos(), op, x, other), typ: x.typ}
		if len(x.a) > 0 {
			if !k.isGround() {
				// should never reach here.
				break
			}
			if ln := other.(*numLit).intValue(ctx); ln > 0 {
				for i := 0; i < ln; i++ {
					// TODO: copy values
					n.a = append(n.a, x.a...)
				}
			} else if ln < 0 {
				return ctx.mkErr(src, "negative number %d multiplies list", ln)
			}
		}
		switch v := x.len.(type) {
		case *numLit:
			// Closed list
			ln := &numLit{numBase: v.numBase}
			ln.v.SetInt64(int64(len(n.a)))
			n.len = ln
		default:
			// Open list
			n.len = x.len
		}
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
			return ctx.mkErr(src, "number of params of params should match in unification (%d != %d)", n, m)
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
