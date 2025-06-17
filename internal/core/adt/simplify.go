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
	"strings"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/internal"
)

// SimplifyBounds collapses bounds if possible. The bound values must be
// concrete. It returns nil if the bound values cannot be collapsed.
//
// k represents additional type constraints, such as `int`.
func SimplifyBounds(ctx *OpContext, k Kind, x, y *BoundValue) Value {
	xv := x.Value
	yv := y.Value

	cmp, xCat := opInfo(x.Op)
	_, yCat := opInfo(y.Op)

	// k := x.Kind() & y.Kind()

	switch {
	case xCat == yCat:
		switch x.Op {
		// NOTE: EqualOp should not happen, but include it defensively.
		// Maybe an API would use it, for instance.
		case EqualOp, NotEqualOp, MatchOp, NotMatchOp:
			if test(ctx, EqualOp, xv, yv) {
				return x
			}
			return nil // keep both bounds
		}

		// xCat == yCat && x.Op != NotEqualOp
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
		if test(ctx, cmp, xv, yv) {
			return x
		}
		return y

	case xCat == -yCat && k == StringKind:
		if xCat == -1 {
			x, y = y, x
			xv, yv = yv, xv
		}

		a, aOK := xv.(*String)
		b, bOK := yv.(*String)

		if !aOK || !bOK {
			break
		}

		switch diff := strings.Compare(a.Str, b.Str); diff {
		case -1:
		case 0:
			if x.Op == GreaterEqualOp && y.Op == LessEqualOp {
				if ctx.SimplifyValidators {
					return ctx.NewString(a.Str)
				}
				return nil
			}
			fallthrough

		case 1:
			return ctx.NewErrf("incompatible string bounds %v and %v", y, x)
		}

	case xCat == -yCat && k == BytesKind:
		if xCat == -1 {
			x, y = y, x
			xv, yv = yv, xv
		}

		a, aOK := xv.(*Bytes)
		b, bOK := yv.(*Bytes)

		if !aOK || !bOK {
			break
		}

		switch diff := bytes.Compare(a.B, b.B); diff {
		case -1:
		case 0:
			if x.Op == GreaterEqualOp && y.Op == LessEqualOp {
				if ctx.SimplifyValidators {
					return ctx.newBytes(a.B)
				}
				return nil
			}
			fallthrough

		case 1:
			return ctx.NewErrf("incompatible bytes bounds %v and %v", y, x)
		}

	case xCat == -yCat:
		if xCat == -1 {
			x, y = y, x
			xv, yv = yv, xv
		}
		a, aOK := xv.(*Num)
		b, bOK := yv.(*Num)

		if !aOK || !bOK {
			break
		}

		var d, lo, hi apd.Decimal
		lo.Set(&a.X)
		hi.Set(&b.X)
		if k&FloatKind == 0 {
			// Readjust bounds for integers.
			if x.Op == GreaterEqualOp {
				// >=3.4  ==>  >=4
				_, _ = internal.BaseContext.Ceil(&lo, &a.X)
			} else {
				// >3.4  ==>  >3
				_, _ = internal.BaseContext.Floor(&lo, &a.X)
			}
			if y.Op == LessEqualOp {
				// <=2.3  ==>  <= 2
				_, _ = internal.BaseContext.Floor(&hi, &b.X)
			} else {
				// <2.3   ==>  < 3
				_, _ = internal.BaseContext.Ceil(&hi, &b.X)
			}
		}

		cond, err := internal.BaseContext.Sub(&d, &hi, &lo)
		if cond.Inexact() || err != nil {
			break
		}

		// attempt simplification
		// numbers
		// >=a & <=b
		//     a   if a == b
		//     _|_ if b < a
		// >=a & <b
		//     _|_ if b <= a
		// >a  & <=b
		//     _|_ if b <= a
		// >a  & <b
		//     _|_ if b <= a

		// integers
		// >=a & <=b
		//     a   if b-a == 0
		//     _|_ if b < a
		// >=a & <b
		//     a   if b-a == 1
		//     _|_ if b <= a
		// >a  & <=b
		//     b   if b-a == 1
		//     _|_ if b <= a
		// >a  & <b
		//     a+1 if b-a == 2
		//     _|_ if b <= a

		if d.Negative {
			return errIncompatibleBounds(ctx, k, x, y)
		}
		// [apd.Decimal.Int64] on `d = hi - lo` will error if it overflows an int64.
		// This is pretty common with CUE bounds like int64, which expands to:
		//
		//     >=-9_223_372_036_854_775_808 & <=9_223_372_036_854_775_807
		//
		// Constructing that error is unfortunate as it allocates a few times
		// and stringifies the number too, which also has a cost.
		// Which is entirely unnecessary, as we don't use the error value at all.
		// If we know the integer will have more than one digit, give up early.
		if d.NumDigits() > 1 {
			break
		}
		switch diff, err := d.Int64(); {
		case diff == 1:
			if k&FloatKind == 0 {
				if x.Op == GreaterEqualOp && y.Op == LessThanOp {
					return newNum(ctx, &lo, k&NumberKind, x, y)
				}
				if x.Op == GreaterThanOp && y.Op == LessEqualOp {
					return newNum(ctx, &hi, k&NumberKind, x, y)
				}
				if x.Op == GreaterThanOp && y.Op == LessThanOp {
					return ctx.NewErrf("incompatible integer bounds %v and %v", x, y)
				}
			}

		case diff == 2:
			if k&FloatKind == 0 && x.Op == GreaterThanOp && y.Op == LessThanOp {
				_, _ = internal.BaseContext.Add(&d, d.SetInt64(1), &lo)
				return newNum(ctx, &d, k&NumberKind, x, y)
			}

		case diff == 0 && err == nil:
			if x.Op == GreaterEqualOp && y.Op == LessEqualOp {
				return newNum(ctx, &lo, k&NumberKind, x, y)
			}
			return errIncompatibleBounds(ctx, k, x, y)
		}

	case x.Op == NotEqualOp:
		if !test(ctx, y.Op, xv, yv) {
			return y
		}

	case y.Op == NotEqualOp:
		if !test(ctx, x.Op, yv, xv) {
			return x
		}
	}
	return nil
}

func errIncompatibleBounds(ctx *OpContext, k Kind, x, y *BoundValue) *Bottom {
	if k == IntKind {
		return ctx.NewErrf("incompatible integer bounds %v and %v", y, x)
	} else {
		return ctx.NewErrf("incompatible number bounds %v and %v", y, x)
	}
}
func newNum(ctx *OpContext, d *apd.Decimal, k Kind, sources ...Node) Value {
	if ctx.SimplifyValidators {
		return ctx.newNum(d, k, sources...)
	}
	return nil
}

func opInfo(op Op) (cmp Op, norm int) {
	switch op {
	case GreaterThanOp:
		return GreaterEqualOp, 1
	case GreaterEqualOp:
		return GreaterThanOp, 1
	case LessThanOp:
		return LessEqualOp, -1
	case LessEqualOp:
		return LessThanOp, -1
	case NotEqualOp:
		return NotEqualOp, 0
	case MatchOp:
		return MatchOp, 2
	case NotMatchOp:
		return NotMatchOp, 3
	}
	panic("cue: unreachable")
}

func test(ctx *OpContext, op Op, a, b Value) bool {
	if b, ok := BinOp(ctx, nil, op, a, b).(*Bool); ok {
		return b.B
	}
	return false
}

// SimplifyValidator simplifies non-bound validators.
//
// Currently this only checks for pure equality. In the future this can be used
// to simplify certain builtin validators analogously to how we simplify bounds
// now.
func SimplifyValidator(ctx *OpContext, v, w Conjunct) (c Conjunct, ok bool) {
	switch x := v.x.(type) {
	case *BuiltinValidator:
		switch y := w.x.(type) {
		case *BuiltinValidator:
			if x == y {
				return v, true
			}
			if x.Builtin != y.Builtin || len(x.Args) != len(y.Args) {
				return c, false
			}
			for i, a := range x.Args {
				b := y.Args[i]
				if v, ok := a.(*Vertex); ok {
					v.Finalize(ctx)
				}
				if v, ok := b.(*Vertex); ok {
					v.Finalize(ctx)
				}
				if !Equal(ctx, a, b, CheckStructural) {
					return c, false
				}
			}
			return v, true
		}
	}
	return c, false
}
