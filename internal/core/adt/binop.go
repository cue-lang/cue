// Copyright 2020 CUE Authors
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
	"bytes"
	"math/big"
	"strings"

	"github.com/cockroachdb/apd/v2"
)

var apdCtx apd.Context

func init() {
	apdCtx = apd.BaseContext
	apdCtx.Precision = 24
}

// BinOp handles all operations except AndOp and OrOp. This includes processing
// unary comparators such as '<4' and '=~"foo"'.
//
// BinOp returns nil if not both left and right are concrete.
func BinOp(c *OpContext, op Op, left, right Value) Value {
	leftKind := left.Kind()
	rightKind := right.Kind()

	const msg = "non-concrete value '%v' to operation '%s'"
	if left.Concreteness() > Concrete {
		return &Bottom{
			Code: IncompleteError,
			Err:  c.Newf(msg, c.Str(left), op),
		}
	}
	if right.Concreteness() > Concrete {
		return &Bottom{
			Code: IncompleteError,
			Err:  c.Newf(msg, c.Str(right), op),
		}
	}

	if err := CombineErrors(c.src, left, right); err != nil {
		return err
	}

	switch op {
	case EqualOp:
		switch {
		case leftKind == NullKind && rightKind == NullKind:
			return c.newBool(true)

		case leftKind == NullKind || rightKind == NullKind:
			return c.newBool(false)

		case leftKind == BoolKind:
			return c.newBool(c.BoolValue(left) == c.BoolValue(right))

		case leftKind == StringKind:
			// normalize?
			return cmpTonode(c, op, strings.Compare(c.StringValue(left), c.StringValue(right)))

		case leftKind == BytesKind:
			return cmpTonode(c, op, bytes.Compare(c.bytesValue(left, op), c.bytesValue(right, op)))

		case leftKind&NumKind != 0 && rightKind&NumKind != 0:
			// n := c.newNum()
			return cmpTonode(c, op, c.num(left, op).X.Cmp(&c.num(right, op).X))

		case leftKind == ListKind && rightKind == ListKind:
			x := c.Elems(left)
			y := c.Elems(right)
			if len(x) != len(y) {
				return c.newBool(false)
			}
			for i, e := range x {
				a, _ := c.Concrete(nil, e, op)
				b, _ := c.Concrete(nil, y[i], op)
				if !test(c, EqualOp, a, b) {
					return c.newBool(false)
				}
			}
			return c.newBool(true)
		}

	case NotEqualOp:
		switch {
		case leftKind == NullKind && rightKind == NullKind:
			return c.newBool(false)

		case leftKind == NullKind || rightKind == NullKind:
			return c.newBool(true)

		case leftKind == BoolKind:
			return c.newBool(c.boolValue(left, op) != c.boolValue(right, op))

		case leftKind == StringKind:
			// normalize?
			return cmpTonode(c, op, strings.Compare(c.StringValue(left), c.StringValue(right)))

		case leftKind == BytesKind:
			return cmpTonode(c, op, bytes.Compare(c.bytesValue(left, op), c.bytesValue(right, op)))

		case leftKind&NumKind != 0 && rightKind&NumKind != 0:
			// n := c.newNum()
			return cmpTonode(c, op, c.num(left, op).X.Cmp(&c.num(right, op).X))

		case leftKind == ListKind && rightKind == ListKind:
			x := c.Elems(left)
			y := c.Elems(right)
			if len(x) != len(y) {
				return c.newBool(false)
			}
			for i, e := range x {
				a, _ := c.Concrete(nil, e, op)
				b, _ := c.Concrete(nil, y[i], op)
				if !test(c, EqualOp, a, b) {
					return c.newBool(true)
				}
			}
			return c.newBool(false)
		}

	case LessThanOp, LessEqualOp, GreaterEqualOp, GreaterThanOp:
		switch {
		case leftKind == StringKind && rightKind == StringKind:
			// normalize?
			return cmpTonode(c, op, strings.Compare(c.stringValue(left, op), c.stringValue(right, op)))

		case leftKind == BytesKind && rightKind == BytesKind:
			return cmpTonode(c, op, bytes.Compare(c.bytesValue(left, op), c.bytesValue(right, op)))

		case leftKind&NumKind != 0 && rightKind&NumKind != 0:
			// n := c.newNum(left, right)
			return cmpTonode(c, op, c.num(left, op).X.Cmp(&c.num(right, op).X))
		}

	case BoolAndOp:
		return c.newBool(c.boolValue(left, op) && c.boolValue(right, op))

	case BoolOrOp:
		return c.newBool(c.boolValue(left, op) || c.boolValue(right, op))

	case MatchOp:
		// if y.re == nil {
		// 	// This really should not happen, but leave in for safety.
		// 	b, err := Regexp.MatchString(str, x.str)
		// 	if err != nil {
		// 		return c.Errf(Src, "error parsing Regexp: %v", err)
		// 	}
		// 	return boolTonode(Src, b)
		// }
		return c.newBool(c.regexp(right).MatchString(c.stringValue(left, op)))

	case NotMatchOp:
		return c.newBool(!c.regexp(right).MatchString(c.stringValue(left, op)))

	case AddOp:
		switch {
		case leftKind&NumKind != 0 && rightKind&NumKind != 0:
			return numOp(c, apdCtx.Add, left, right, AddOp)

		case leftKind == StringKind && rightKind == StringKind:
			return c.NewString(c.StringValue(left) + c.StringValue(right))

		case leftKind == BytesKind && rightKind == BytesKind:
			ba := c.bytesValue(left, op)
			bb := c.bytesValue(right, op)
			b := make([]byte, len(ba)+len(bb))
			copy(b, ba)
			copy(b[len(ba):], bb)
			return c.newBytes(b)

		case leftKind == ListKind && rightKind == ListKind:
			a := c.Elems(left)
			b := c.Elems(right)
			if err := c.Err(); err != nil {
				return err
			}
			n := c.newList(c.src, nil)
			if err := n.appendListArcs(a); err != nil {
				return err
			}
			if err := n.appendListArcs(b); err != nil {
				return err
			}
			// n.isList = true
			// n.IsClosed = true
			return n
		}

	case SubtractOp:
		return numOp(c, apdCtx.Sub, left, right, op)

	case MultiplyOp:
		switch {
		// float
		case leftKind&NumKind != 0 && rightKind&NumKind != 0:
			return numOp(c, apdCtx.Mul, left, right, op)

		case leftKind == StringKind && rightKind == IntKind:
			const as = "string multiplication"
			return c.NewString(strings.Repeat(c.stringValue(left, as), int(c.uint64(right, as))))

		case leftKind == IntKind && rightKind == StringKind:
			const as = "string multiplication"
			return c.NewString(strings.Repeat(c.stringValue(right, as), int(c.uint64(left, as))))

		case leftKind == BytesKind && rightKind == IntKind:
			const as = "bytes multiplication"
			return c.newBytes(bytes.Repeat(c.bytesValue(left, as), int(c.uint64(right, as))))

		case leftKind == IntKind && rightKind == BytesKind:
			const as = "bytes multiplication"
			return c.newBytes(bytes.Repeat(c.bytesValue(right, as), int(c.uint64(left, as))))

		case leftKind == ListKind && rightKind == IntKind:
			left, right = right, left
			fallthrough

		case leftKind == IntKind && rightKind == ListKind:
			a := c.Elems(right)
			n := c.newList(c.src, nil)
			// n.IsClosed = true
			index := int64(0)
			for i := c.uint64(left, "list multiplier"); i > 0; i-- {
				for _, a := range a {
					f, _ := MakeLabel(a.Source(), index, IntLabel)
					n.Arcs = append(n.Arcs, &Vertex{
						Parent:    n,
						Label:     f,
						Conjuncts: a.Conjuncts,
					})
					index++
				}
			}
			return n
		}

	case FloatQuotientOp:
		if leftKind&NumKind != 0 && rightKind&NumKind != 0 {
			v := numOp(c, apdCtx.Quo, left, right, op)
			if n, ok := v.(*Num); ok {
				n.K = FloatKind
			}
			return v
		}

	case IntDivideOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			y := c.num(right, op)
			if y.X.IsZero() {
				return c.NewErrf("division by zero")
			}
			return intOp(c, (*big.Int).Div, c.num(left, op), y)
		}

	case IntModuloOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			y := c.num(right, op)
			if y.X.IsZero() {
				return c.NewErrf("division by zero")
			}
			return intOp(c, (*big.Int).Mod, c.num(left, op), y)
		}

	case IntQuotientOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			y := c.num(right, op)
			if y.X.IsZero() {
				return c.NewErrf("division by zero")
			}
			return intOp(c, (*big.Int).Quo, c.num(left, op), y)
		}

	case IntRemainderOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			y := c.num(right, op)
			if y.X.IsZero() {
				return c.NewErrf("division by zero")
			}
			return intOp(c, (*big.Int).Rem, c.num(left, op), y)
		}
	}

	return c.NewErrf("invalid operands %s and %s to '%s' (type %s and %s)",
		c.Str(left), c.Str(right), op, left.Kind(), right.Kind())
}

func cmpTonode(c *OpContext, op Op, r int) Value {
	result := false
	switch op {
	case LessThanOp:
		result = r == -1
	case LessEqualOp:
		result = r != 1
	case EqualOp, AndOp:
		result = r == 0
	case NotEqualOp:
		result = r != 0
	case GreaterEqualOp:
		result = r != -1
	case GreaterThanOp:
		result = r == 1
	}
	return c.newBool(result)
}

type numFunc func(z, x, y *apd.Decimal) (apd.Condition, error)

func numOp(c *OpContext, fn numFunc, a, b Value, op Op) Value {
	var d apd.Decimal
	x := c.num(a, op)
	y := c.num(b, op)
	cond, err := fn(&d, &x.X, &y.X)
	if err != nil {
		return c.NewErrf("failed arithmetic: %v", err)
	}
	if cond.DivisionByZero() {
		return c.NewErrf("division by zero")
	}
	k := x.Kind() & y.Kind()
	if k == 0 {
		k = FloatKind
	}
	return c.newNum(&d, k)
}

type intFunc func(z, x, y *big.Int) *big.Int

func intOp(c *OpContext, fn intFunc, a, b *Num) Value {
	var d apd.Decimal

	var x, y apd.Decimal
	_, _ = apdCtx.RoundToIntegralValue(&x, &a.X)
	if x.Negative {
		x.Coeff.Neg(&x.Coeff)
	}
	_, _ = apdCtx.RoundToIntegralValue(&y, &b.X)
	if y.Negative {
		y.Coeff.Neg(&y.Coeff)
	}
	fn(&d.Coeff, &x.Coeff, &y.Coeff)
	if d.Coeff.Sign() < 0 {
		d.Coeff.Neg(&d.Coeff)
		d.Negative = true
	}
	return c.newNum(&d, IntKind)
}
