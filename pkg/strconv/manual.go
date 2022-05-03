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

package strconv

import (
	"fmt"
	"strconv"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal"
)

// Unquote interprets s as a single-quoted, double-quoted,
// or backquoted CUE string literal, returning the string value
// that s quotes.
func Unquote(s string) (string, error) {
	return literal.Unquote(s)
}

// FormatFloat converts the floating-point number f to a string,
// according to the format fmt and precision prec. It rounds the
// result assuming that the original was obtained from a floating-point
// value of bitSize bits (32 for float32, 64 for float64).
//
// The format fmt a string: one of
// "b" (-ddddp±ddd, a binary exponent),
// "e" (-d.dddde±dd, a decimal exponent),
// "E" (-d.ddddE±dd, a decimal exponent),
// "f" (-ddd.dddd, no exponent),
// "g" ("e" for large exponents, "f" otherwise),
// "G" ("E" for large exponents, "f" otherwise),
// "x" (-0xd.ddddp±ddd, a hexadecimal fraction and binary exponent), or
// "X" (-0Xd.ddddP±ddd, a hexadecimal fraction and binary exponent).
//
// The precision prec controls the number of digits (excluding the exponent)
// printed by the "e", "E", "f", "g", "G", "x", and "X" formats.
// For "e", "E", "f", "x", and "X", it is the number of digits after the decimal point.
// For "g" and "G" it is the maximum number of significant digits (trailing
// zeros are removed).
// The special precision -1 uses the smallest number of digits
// necessary such that ParseFloat will return f exactly.
//
// For historical reasons, an integer (the ASCII code point of the
// a format character) is also accepted for the format.
func FormatFloat(f float64, fmtVal cue.Value, prec, bitSize int) (string, error) {
	// Note: none of the error cases below should ever happen
	// because the argument disjunction type fully enumerates all
	// the allowed values.
	var fmtByte byte
	switch k := fmtVal.Kind(); k {
	case cue.StringKind:
		s, err := fmtVal.String()
		if err != nil {
			return "", err
		}
		if len(s) != 1 {
			return "", fmt.Errorf("expected single character string")
		}
		fmtByte = s[0]
	case cue.IntKind:
		n, err := fmtVal.Int64()
		if err != nil {
			return "", err
		}
		// It might look like converts any arbitrary int mod 256, but
		// the disjunction allows only valid values, so it's OK.
		fmtByte = byte(n)
	default:
		return "", fmt.Errorf("unexpected kind %v", k)
	}
	return strconv.FormatFloat(f, fmtByte, prec, bitSize), nil
}

// TODO: replace parsing functions with parsing to apd

var formatCodes = &adt.Disjunction{
	Values: []*adt.Vertex{},
}

const formatFloatChars = "beEfgGxX"

// Use a var here rather than an init function so it's guaranteed
// to run before the call to internal.Register inside pkg.go.
var _ = func() (_ struct{}) {
	formatArg := &adt.Disjunction{
		Values: make([]*adt.Vertex, 0, len(formatFloatChars)*2),
	}
	// Create the disjunction in two loops rather than one so it's
	// formatted a little nicer ("g" | "e" | ... | 98 | 101 | ...)
	// rather than alternating strings and numbers.
	for i := range formatFloatChars {
		formatArg.Values = append(formatArg.Values, newStr(formatFloatChars[i:i+1]))
	}
	for i := range formatFloatChars {
		formatArg.Values = append(formatArg.Values, newInt(int(formatFloatChars[i])))
	}

	pkg.Native = append(pkg.Native, &internal.Builtin{
		Name: "FormatFloat",
		Params: []internal.Param{
			{Kind: adt.NumKind},
			{Kind: adt.IntKind | adt.StringKind, Value: formatArg},
			{Kind: adt.IntKind},
			{Kind: adt.IntKind},
		},
		Result: adt.StringKind,
		Func: func(c *internal.CallCtxt) {
			f, fmt, prec, bitSize := c.Float64(0), c.Value(1), c.Int(2), c.Int(3)
			if c.Do() {
				c.Ret, c.Err = FormatFloat(f, fmt, prec, bitSize)
			}
		},
	})
	return
}()

func newStr(s string) *adt.Vertex {
	v := &adt.Vertex{}
	v.SetValue(nil, adt.Finalized, &adt.String{Str: s})
	return v
}

func newInt(i int) *adt.Vertex {
	n := &adt.Num{K: adt.IntKind}
	n.X.SetInt64(int64(i))
	v := &adt.Vertex{}
	v.SetValue(nil, adt.Finalized, n)
	return v
}
