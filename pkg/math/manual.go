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

package math

import (
	"math/big"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/internal"
	"cuelang.org/go/internal/pkg"
)

func roundContext(rounder apd.Rounder) internal.Context {
	c := internal.BaseContext
	c.Rounding = rounder
	return c
}

// toInt converts an integral d to a big.Int. Callers must round d to an
// integral value first, so that no non-zero digits are dropped.
//
// TODO: converting to an int is how the desired type is conveyed, at the cost
// of expanding a number such as 1E10000. It would be better to unify the
// number types and let anything with an integer value pose as an integer.
func toInt(d *internal.Decimal) *big.Int {
	// Rounding to an integral value brings the exponent to zero, so that the
	// coefficient alone is the magnitude.
	var i internal.Decimal
	_, _ = internal.BaseContext.RoundToIntegralValue(&i, d)
	b := i.Coeff.MathBigInt()
	if i.Negative {
		b.Neg(b)
	}
	return b
}

// Floor returns the greatest integer value less than or equal to x.
//
// Special cases are:
//
//	Floor(±0) = ±0
//	Floor(±Inf) = ±Inf
//	Floor(NaN) = NaN
func Floor(x *internal.Decimal) (*big.Int, error) {
	var d internal.Decimal
	// apd truncates towards zero and then subtracts one, so an argument with
	// a fraction needs a context which will not round that step away.
	_, err := internal.ExactContext.Floor(&d, x)
	return toInt(&d), err
}

// Ceil returns the least integer value greater than or equal to x.
//
// Special cases are:
//
//	Ceil(±0) = ±0
//	Ceil(±Inf) = ±Inf
//	Ceil(NaN) = NaN
func Ceil(x *internal.Decimal) (*big.Int, error) {
	var d internal.Decimal
	// See the comment in [Floor]; Ceil adds one rather than subtracting it.
	_, err := internal.ExactContext.Ceil(&d, x)
	return toInt(&d), err
}

var roundTruncContext = roundContext(apd.RoundDown)

// Trunc returns the integer value of x.
//
// Special cases are:
//
//	Trunc(±0) = ±0
//	Trunc(±Inf) = ±Inf
//	Trunc(NaN) = NaN
func Trunc(x *internal.Decimal) (*big.Int, error) {
	var d internal.Decimal
	_, err := roundTruncContext.RoundToIntegralExact(&d, x)
	return toInt(&d), err
}

var roundUpContext = roundContext(apd.RoundHalfUp)

// Round returns the nearest integer, rounding half away from zero.
//
// Special cases are:
//
//	Round(±0) = ±0
//	Round(±Inf) = ±Inf
//	Round(NaN) = NaN
func Round(x *internal.Decimal) (*big.Int, error) {
	var d internal.Decimal
	_, err := roundUpContext.RoundToIntegralExact(&d, x)
	return toInt(&d), err
}

var roundEvenContext = roundContext(apd.RoundHalfEven)

// RoundToEven returns the nearest integer, rounding ties to even.
//
// Special cases are:
//
//	RoundToEven(±0) = ±0
//	RoundToEven(±Inf) = ±Inf
//	RoundToEven(NaN) = NaN
func RoundToEven(x *internal.Decimal) (*big.Int, error) {
	var d internal.Decimal
	_, err := roundEvenContext.RoundToIntegralExact(&d, x)
	return toInt(&d), err
}

// MultipleOf reports whether x is a multiple of y.
func MultipleOf(x, y *internal.Decimal) (pkg.Validator, error) {
	var d apd.Decimal

	// TODO: It would be preferable to use internal.BaseContext.Rem here, and directly
	//       check the result for 0. However, this currently fails with "division impossible".
	//       Fix this when https://github.com/cockroachdb/apd/issues/134 is resolved.
	_, err := internal.BaseContext.Quo(&d, x, y)
	if err != nil {
		return false, err
	}
	var frac apd.Decimal
	d.Modf(nil, &frac)
	return frac.IsZero(), nil
}
