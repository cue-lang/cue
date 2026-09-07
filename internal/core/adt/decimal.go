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
	"cuelang.org/go/internal"
	"github.com/cockroachdb/apd/v3"
)

func (n *Num) Impl() *apd.Decimal {
	return &n.X
}

func (n *Num) Negative() bool {
	return n.X.Negative
}

func (a *Num) Cmp(b *Num) int {
	return a.X.Cmp(&b.X)
}

func (c *OpContext) Add(a, b *Num) Value {
	return numOp(c, (*internal.Context).Add, numKind(a, b), a, b)
}

func (c *OpContext) Sub(a, b *Num) Value {
	return numOp(c, (*internal.Context).Sub, numKind(a, b), a, b)
}

func (c *OpContext) Mul(a, b *Num) Value {
	return numOp(c, (*internal.Context).Mul, numKind(a, b), a, b)
}

func (c *OpContext) Quo(a, b *Num) Value {
	return numOp(c, (*internal.Context).Quo, FloatKind, a, b)
}

// numKind returns the kind of an arithmetic result over a and b: the kind they
// share, or a float if they share none.
func numKind(a, b *Num) Kind {
	k := a.Kind() & b.Kind()
	if k == 0 {
		k = FloatKind
	}
	return k
}

type numFunc func(c *internal.Context, z, x, y *apd.Decimal) (apd.Condition, error)

// numOp applies fn to x and y, giving the result kind k.
//
// An integer result is computed without rounding, so that it keeps the
// arbitrary precision doc/ref/spec.md requires. Only operations which are
// exact over the integers may yield one, which rules out division: a quotient
// such as 1/3 has no finite decimal representation, and apd refuses to divide
// at all in a context with no precision to round to.
func numOp(c *OpContext, fn numFunc, k Kind, x, y *Num) Value {
	ctx := internal.BaseContext
	if k&FloatKind == 0 {
		ctx = internal.ExactContext
	}

	var d apd.Decimal
	cond, err := fn(&ctx, &d, &x.X, &y.X)

	if err != nil {
		return c.NewErrf("failed arithmetic: %v", err)
	}

	if cond.DivisionByZero() {
		return c.NewErrf("division by zero")
	}

	return c.newNum(&d, k)
}

func (c *OpContext) IntDiv(a, b *Num) Value {
	return intDivOp(c, (*apd.BigInt).Div, a, b)
}

func (c *OpContext) IntMod(a, b *Num) Value {
	return intDivOp(c, (*apd.BigInt).Mod, a, b)
}

func (c *OpContext) IntQuo(a, b *Num) Value {
	return intDivOp(c, (*apd.BigInt).Quo, a, b)
}

func (c *OpContext) IntRem(a, b *Num) Value {
	return intDivOp(c, (*apd.BigInt).Rem, a, b)
}

type intFunc func(z, x, y *apd.BigInt) *apd.BigInt

func intDivOp(c *OpContext, fn intFunc, a, b *Num) Value {
	if b.X.IsZero() {
		return c.NewErrf("division by zero")
	}

	var x, y apd.Decimal
	_, _ = internal.BaseContext.RoundToIntegralValue(&x, &a.X)
	if x.Negative {
		x.Coeff.Neg(&x.Coeff)
	}
	_, _ = internal.BaseContext.RoundToIntegralValue(&y, &b.X)
	if y.Negative {
		y.Coeff.Neg(&y.Coeff)
	}

	var d apd.Decimal

	fn(&d.Coeff, &x.Coeff, &y.Coeff)

	if d.Coeff.Sign() < 0 {
		d.Coeff.Neg(&d.Coeff)
		d.Negative = true
	}

	return c.newNum(&d, IntKind)
}
