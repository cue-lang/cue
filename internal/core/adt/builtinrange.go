// Copyright 2026 CUE Authors
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

import "github.com/cockroachdb/apd/v3"

type builtinRange struct {
	name   string
	lo, hi *apd.Decimal
}

func mustDec(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

var intBuiltinRanges = []builtinRange{
	{"int8", mustDec("-128"), mustDec("127")},
	{"int16", mustDec("-32768"), mustDec("32767")},
	{"int32", mustDec("-2147483648"), mustDec("2147483647")},
	{"int64", mustDec("-9223372036854775808"), mustDec("9223372036854775807")},
	{"int128",
		mustDec("-170141183460469231731687303715884105728"),
		mustDec("170141183460469231731687303715884105727")},

	{"uint8", mustDec("0"), mustDec("255")},
	{"uint16", mustDec("0"), mustDec("65535")},
	{"uint32", mustDec("0"), mustDec("4294967295")},
	{"uint64", mustDec("0"), mustDec("18446744073709551615")},
	{"uint128", mustDec("0"), mustDec("340282366920938463463374607431768211455")},
}

var floatBuiltinRanges = []builtinRange{
	// 2**127 * (2**24 - 1) / 2**23
	{"float32",
		mustDec("-3.40282346638528859811704183484516925440e+38"),
		mustDec("3.40282346638528859811704183484516925440e+38")},

	// 2**1023 * (2**53 - 1) / 2**52
	{"float64",
		mustDec("-1.797693134862315708145274237317043567981e+308"),
		mustDec("1.797693134862315708145274237317043567981e+308")},
}

// MatchBuiltinRange reports the predeclared identifier name (e.g. "int16",
// "uint8", "float32", "uint") that a Conjunction precisely represents, or the
// empty string if it does not represent any such type.
//
// It recognises the shapes produced by the predeclared compiler for ranged
// numeric types:
//
//   - sized int types: {BasicType{Int}, BoundValue{>=lo}, BoundValue{<=hi}}
//   - sized uint types: {BasicType{Int}, BoundValue{>=0}, BoundValue{<=hi}}
//   - sized float types: {BoundValue{>=lo}, BoundValue{<=hi}}
//   - uint: {BasicType{Int}, BoundValue{>=0}}
//
// The order of values is not significant.
func MatchBuiltinRange(c *Conjunction) string {
	var hasInt bool
	var lo, hi *Num
	for _, v := range c.Values {
		switch x := v.(type) {
		case *BasicType:
			if x.K != IntKind || hasInt {
				return ""
			}
			hasInt = true
		case *BoundValue:
			n, ok := x.Value.(*Num)
			if !ok {
				return ""
			}
			switch x.Op {
			case GreaterEqualOp:
				if lo != nil {
					return ""
				}
				lo = n
			case LessEqualOp:
				if hi != nil {
					return ""
				}
				hi = n
			default:
				return ""
			}
		default:
			return ""
		}
	}

	if lo != nil && hi == nil && hasInt && lo.X.IsZero() {
		return "uint"
	}
	if lo == nil || hi == nil {
		return ""
	}
	ranges := floatBuiltinRanges
	if hasInt {
		ranges = intBuiltinRanges
	}
	for _, r := range ranges {
		if lo.X.Cmp(r.lo) == 0 && hi.X.Cmp(r.hi) == 0 {
			return r.name
		}
	}
	return ""
}
