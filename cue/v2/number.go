// Copyright 2026 The CUE Authors
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
	"context"
	"errors"
	"math"
	"math/big"

	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"github.com/cockroachdb/apd/v3"
)

var (
	// ErrBelow indicates that a value was rounded down in a conversion.
	ErrBelow = errors.New("value was rounded down")

	// ErrAbove indicates that a value was rounded up in a conversion.
	ErrAbove = errors.New("value was rounded up")
)

// num returns the underlying number if v is a number of a kind in k.
func (v Value) num(ctx context.Context, k adt.Kind) (*adt.Num, error) {
	w, err := v.forceDefault(ctx)
	if err != nil {
		return nil, err
	}
	if n, _ := w.BaseValue.(*adt.Num); n != nil && k&n.Kind() != adt.BottomKind {
		return n, nil
	}
	return nil, v.checkKindErr(w, k)
}

// Int64 converts the underlying integral number to an int64. It reports
// an error if the underlying value is not an integer type or cannot be
// represented as an int64. The result is (math.MinInt64, ErrAbove) for
// x < math.MinInt64, and (math.MaxInt64, ErrBelow) for
// x > math.MaxInt64.
func (v Value) Int64(ctx context.Context) (int64, error) {
	n, err := v.num(ctx, adt.IntKind)
	if err != nil {
		return 0, err
	}
	if !n.X.Coeff.IsInt64() {
		if n.X.Negative {
			return math.MinInt64, ErrAbove
		}
		return math.MaxInt64, ErrBelow
	}
	i := n.X.Coeff.Int64()
	if n.X.Negative {
		i = -i
	}
	return i, nil
}

// uint64Val converts the underlying integral number to a uint64,
// following the conventions of v1's Value.Uint64.
func (v Value) uint64Val(ctx context.Context) (uint64, error) {
	n, err := v.num(ctx, adt.IntKind)
	if err != nil {
		return 0, err
	}
	if n.X.Negative {
		return 0, ErrAbove
	}
	if !n.X.Coeff.IsUint64() {
		return math.MaxUint64, ErrBelow
	}
	return n.X.Coeff.Uint64(), nil
}

// bigInt converts the underlying integral number to a big.Int.
func (v Value) bigInt(ctx context.Context) (*big.Int, error) {
	n, err := v.num(ctx, adt.IntKind)
	if err != nil {
		return nil, err
	}
	return n.BigInt(nil), nil
}

// bigFloat converts the underlying number to a big.Float.
func (v Value) bigFloat(ctx context.Context) (*big.Float, error) {
	n, err := v.num(ctx, adt.NumberKind)
	if err != nil {
		return nil, err
	}
	f := &big.Float{}
	f, _, err = f.Parse(n.X.String(), 0)
	return f, err
}

var (
	smallestPosFloat64 *apd.Decimal
	smallestNegFloat64 *apd.Decimal
	maxPosFloat64      *apd.Decimal
	maxNegFloat64      *apd.Decimal
)

func init() {
	const (
		// math.SmallestNonzeroFloat64: 1 / 2**(1023 - 1 + 52)
		smallest = "4.940656458412465441765687928682213723651e-324"
		// math.MaxFloat64: 2**1023 * (2**53 - 1) / 2**52
		max = "1.797693134862315708145274237317043567981e+308"
	)
	ctx := internal.BaseContext.WithPrecision(40)

	var err error
	smallestPosFloat64, _, err = ctx.NewFromString(smallest)
	if err != nil {
		panic(err)
	}
	smallestNegFloat64, _, err = ctx.NewFromString("-" + smallest)
	if err != nil {
		panic(err)
	}
	maxPosFloat64, _, err = ctx.NewFromString(max)
	if err != nil {
		panic(err)
	}
	maxNegFloat64, _, err = ctx.NewFromString("-" + max)
	if err != nil {
		panic(err)
	}
}

// Float64 returns the float64 value nearest to x. It reports an error
// if v is not a number. If x is too small to be represented by a
// float64 (|x| < math.SmallestNonzeroFloat64), the result is
// (0, ErrBelow) or (-0, ErrAbove), respectively, depending on the sign
// of x. If x is too large to be represented by a float64
// (|x| > math.MaxFloat64), the result is (+Inf, ErrAbove) or
// (-Inf, ErrBelow), depending on the sign of x.
func (v Value) Float64(ctx context.Context) (float64, error) {
	n, err := v.num(ctx, adt.NumberKind)
	if err != nil {
		return 0, err
	}
	if n.X.IsZero() {
		return 0.0, nil
	}
	if n.X.Negative {
		if n.X.Cmp(smallestNegFloat64) == 1 {
			return -0, ErrAbove
		}
		if n.X.Cmp(maxNegFloat64) == -1 {
			return math.Inf(-1), ErrBelow
		}
	} else {
		if n.X.Cmp(smallestPosFloat64) == -1 {
			return 0, ErrBelow
		}
		if n.X.Cmp(maxPosFloat64) == 1 {
			return math.Inf(1), ErrAbove
		}
	}
	f, _ := n.X.Float64()
	return f, nil
}
