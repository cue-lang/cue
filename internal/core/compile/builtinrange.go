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

package compile

import (
	"slices"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

// A BuiltinRange is a predeclared numeric type which bounds a kind,
// such as uint8, int128, or float64.
type BuiltinRange struct {
	Name string

	// Lo and Hi are the bounds, both of [adt.IntKind] or [adt.FloatKind],
	// with their sources as CUE number literals. Hi is nil if there is no
	// upper bound. They must not be modified.
	Lo, Hi *adt.Num
}

// BuiltinRanges returns all predeclared range types.
// The result must not be modified.
func BuiltinRanges() []BuiltinRange {
	return slices.Clip(builtinRanges())
}

// builtinRanges is built on first use, as parsing the bounds is costly
// at init time.
var builtinRanges = sync.OnceValue(func() []BuiltinRange {
	return []BuiltinRange{
		{"rune", rangeInt("0"), rangeInt("1114111")}, // 0x10FFFF
		{"int8", rangeInt("-128"), rangeInt("127")},
		{"int16", rangeInt("-32768"), rangeInt("32767")},
		{"int32", rangeInt("-2147483648"), rangeInt("2147483647")},
		{"int64", rangeInt("-9223372036854775808"), rangeInt("9223372036854775807")},
		{"int128",
			rangeInt("-170141183460469231731687303715884105728"),
			rangeInt("170141183460469231731687303715884105727")},

		// Do not include an alias for "byte", as it would be too easily confused
		// with the builtin "bytes".
		{"uint", rangeInt("0"), nil},
		{"uint8", rangeInt("0"), rangeInt("255")},
		{"uint16", rangeInt("0"), rangeInt("65535")},
		{"uint32", rangeInt("0"), rangeInt("4294967295")},
		{"uint64", rangeInt("0"), rangeInt("18446744073709551615")},
		{"uint128", rangeInt("0"), rangeInt("340282366920938463463374607431768211455")},

		// 2**127 * (2**24 - 1) / 2**23
		{"float32",
			rangeFloat("-3.40282346638528859811704183484516925440e+38"),
			rangeFloat("3.40282346638528859811704183484516925440e+38")},
		// 2**1023 * (2**53 - 1) / 2**52
		{"float64",
			rangeFloat("-1.797693134862315708145274237317043567981e+308"),
			rangeFloat("1.797693134862315708145274237317043567981e+308")},
	}
})

func rangeInt(s string) *adt.Num   { return rangeNum(adt.IntKind, token.INT, s) }
func rangeFloat(s string) *adt.Num { return rangeNum(adt.FloatKind, token.FLOAT, s) }

func rangeNum(k adt.Kind, tok token.Token, s string) *adt.Num {
	n := &adt.Num{Src: ast.NewLit(tok, s), K: k}
	if _, _, err := n.X.SetString(s); err != nil {
		panic(err)
	}
	return n
}

// expr returns the expression which the predeclared identifier
// [BuiltinRange.Name] stands for, as recognized by [MatchBuiltinRange].
func (r BuiltinRange) expr() *adt.Conjunction {
	var values []adt.Value
	if r.Lo.K == adt.IntKind {
		values = append(values, &adt.BasicType{Src: ast.NewIdent("__int"), K: adt.IntKind})
	}
	values = append(values, rangeBound(adt.GreaterEqualOp, r.Lo))
	if r.Hi != nil {
		values = append(values, rangeBound(adt.LessEqualOp, r.Hi))
	}
	srcs := make([]ast.Expr, len(values))
	for i, v := range values {
		srcs[i] = v.Source().(ast.Expr)
	}
	return &adt.Conjunction{Src: ast.NewBinExpr(token.AND, srcs...), Values: values}
}

func rangeBound(op adt.Op, n *adt.Num) *adt.BoundValue {
	src := &ast.UnaryExpr{Op: op.Token(), X: n.Src.(ast.Expr)}
	return &adt.BoundValue{Src: src, Op: op, Value: n}
}

// MatchBuiltinRange reports the name of the [BuiltinRange] that an
// [adt.Conjunction] precisely represents, such as "int16" or "float32",
// or the empty string if it does not represent any.
//
// It recognizes the shapes which the predeclared identifiers compile to:
// an int type for integer ranges, an inclusive lower bound [BuiltinRange.Lo],
// and an inclusive upper bound [BuiltinRange.Hi] if it is set, in any order.
func MatchBuiltinRange(c *adt.Conjunction) string {
	var hasInt bool
	var lo, hi *adt.Num
	for _, v := range c.Values {
		switch x := v.(type) {
		case *adt.BasicType:
			if x.K != adt.IntKind || hasInt {
				return ""
			}
			hasInt = true
		case *adt.BoundValue:
			n, ok := x.Value.(*adt.Num)
			if !ok {
				return ""
			}
			switch x.Op {
			case adt.GreaterEqualOp:
				if lo != nil {
					return ""
				}
				lo = n
			case adt.LessEqualOp:
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

	if lo == nil {
		return ""
	}
	for _, r := range builtinRanges() {
		if (r.Lo.K == adt.IntKind) == hasInt && (hi == nil) == (r.Hi == nil) &&
			lo.Cmp(r.Lo) == 0 && (hi == nil || hi.Cmp(r.Hi) == 0) {
			return r.Name
		}
	}
	return ""
}
