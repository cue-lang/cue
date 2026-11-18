// SPDX-License-Identifier: Apache-2.0 AND BSD-3-Clause
// Copyright 2026 The CUE Authors
// Copyright The Go Authors.
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

// Parts of this file are derived from Go's strconv package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package strconv

// Unquote interprets s as a single-quoted, double-quoted,
// or backquoted CUE string literal, returning the string value
// that s quotes.
Unquote: func(s: string) -> string

// ParseBool returns the boolean value represented by the string.
// It accepts 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False.
// Any other value returns an error.
ParseBool: validator(string) | (func(str: string) -> bool)

// FormatBool returns "true" or "false" according to the value of b.
FormatBool: func(b: bool) -> string

// ParseFloat converts the string s to a floating-point number
// with the precision specified by bitSize: 32 for float32, or 64 for float64.
// When bitSize=32, the result still has type float64, but it will be
// convertible to float32 without changing its value.
//
// ParseFloat accepts decimal and hexadecimal floating-point number syntax.
// If s is well-formed and near a valid floating-point number,
// ParseFloat returns the nearest floating-point number rounded
// using IEEE754 unbiased rounding.
// (Parsing a hexadecimal floating-point value only rounds when
// there are more bits in the hexadecimal representation than
// will fit in the mantissa.)
//
// The errors that ParseFloat returns have concrete type *NumError
// and include err.Num = s.
//
// If s is not syntactically well-formed, ParseFloat returns err.Err = ErrSyntax.
//
// If s is syntactically well-formed but is more than 1/2 ULP
// away from the largest floating point number of the given size,
// ParseFloat returns f = ±Inf, err.Err = ErrRange.
//
// ParseFloat recognizes the strings "NaN", and the (possibly signed) strings "Inf" and "Infinity"
// as their respective special floating point values. It ignores case when matching.
ParseFloat: func(s: string, bitSize: int) -> number

// ParseNumber interprets s using the full CUE number literal syntax and returns
// the resulting value as an arbitrary-precision decimal. It accepts decimal
// and non-decimal bases, underscores as separators, fractional syntax, and
// the decimal or binary multiplier suffixes defined by CUE (for example "1Ki"
// and "10M").
//
// If s is not syntactically well-formed, ParseNumber returns a *strconv.NumError
// with Err containing detailed syntax information. Semantic errors, such as a
// multiplier that cannot be represented, are reported in the same way.
ParseNumber: func(s: string) -> number

// IntSize is the size in bits of an int or uint value.
IntSize: 64

// ParseUint is like [ParseInt] but for unsigned numbers.
ParseUint: func(s: string, base: int, bitSize: int) -> int

// ParseInt interprets a string s in the given base (0, 2 to 36) and
// bit size and returns the corresponding value i.
//
// If the base argument is 0, the true base is implied by the string's
// prefix: 2 for "0b", 8 for "0" or "0o", 16 for "0x", and 10 otherwise.
// Also, for argument base 0 only, underscore characters are permitted
// as defined by the Go syntax for integer literals.
//
// The bitSize argument specifies the integer type that the result must fit into.
// If bitSize is 0, the result is unconstrained (unlimited precision).
// If bitSize is positive, the result must fit in a signed integer of that many bits.
// If bitSize is negative, an error is returned.
ParseInt: func(s: string, base: int, bitSize: int) -> int

// Atoi is equivalent to ParseInt(s, 10, 0), converted to type int.
Atoi: func(s: string) -> int

// FormatFloat converts the floating-point number f to a string,
// according to the format fmt and precision prec. It rounds the
// result assuming that the original was obtained from a floating-point
// value of bitSize bits (32 for float32, 64 for float64).
//
// The format fmt is a string or integer: one of
// "b" (-ddddp±ddd, a binary exponent),
// "e" (-d.dddde±dd, a decimal exponent),
// "E" (-d.ddddE±dd, a decimal exponent),
// "f" (-ddd.dddd, no exponent),
// "g" ("e" for large exponents, "f" otherwise),
// "G" ("E" for large exponents, "f" otherwise),
// "x" (-0xd.ddddp±ddd, a hexadecimal fraction and binary exponent), or
// "X" (-0Xd.ddddP±ddd, a hexadecimal fraction and binary exponent).
//
// For historical reasons, an integer (the ASCII code point of a
// format character) is also accepted for the format.
//
// The precision prec controls the number of digits (excluding the exponent)
// printed by the "e", "E", "f", "g", "G", "x", and "X" formats.
// For "e", "E", "f", "x", and "X", it is the number of digits after the decimal point.
// For "g" and "G" it is the maximum number of significant digits (trailing
// zeros are removed).
// The special precision -1 uses the smallest number of digits
// necessary such that ParseFloat will return f exactly.
FormatFloat: func(f: number, fmtVal: _, prec: int, bitSize: int) -> string

// FormatUint returns the string representation of i in the given base,
// for 2 <= base <= 62. The result uses:
// For 10 <= digit values <= 35, the lower-case letters 'a' to 'z'
// For 36 <= digit values <= 61, the upper-case letters 'A' to 'Z'
FormatUint: func(i: int, base: int) -> string

// FormatInt returns the string representation of i in the given base,
// for 2 <= base <= 62. The result uses:
// For 10 <= digit values <= 35, the lower-case letters 'a' to 'z'
// For 36 <= digit values <= 61, the upper-case letters 'A' to 'Z'
FormatInt: func(i: int, base: int) -> string

// Quote returns a double-quoted Go string literal representing s. The
// returned string uses Go escape sequences (\t, \n, \xFF, \u0100) for
// control characters and non-printable characters as defined by
// IsPrint.
Quote: func(s: string) -> string

// QuoteToASCII returns a double-quoted Go string literal representing s.
// The returned string uses Go escape sequences (\t, \n, \xFF, \u0100) for
// non-ASCII characters and non-printable characters as defined by IsPrint.
QuoteToASCII: func(s: string) -> string

// QuoteToGraphic returns a double-quoted Go string literal representing s.
// The returned string leaves Unicode graphic characters, as defined by
// IsGraphic, unchanged and uses Go escape sequences (\t, \n, \xFF, \u0100)
// for non-graphic characters.
QuoteToGraphic: func(s: string) -> string

// QuoteRune returns a single-quoted Go character literal representing the
// rune. The returned string uses Go escape sequences (\t, \n, \xFF, \u0100)
// for control characters and non-printable characters as defined by IsPrint.
QuoteRune: func(r: int) -> string

// QuoteRuneToASCII returns a single-quoted Go character literal representing
// the rune. The returned string uses Go escape sequences (\t, \n, \xFF,
// \u0100) for non-ASCII characters and non-printable characters as defined
// by IsPrint.
QuoteRuneToASCII: func(r: int) -> string

// QuoteRuneToGraphic returns a single-quoted Go character literal representing
// the rune. If the rune is not a Unicode graphic character,
// as defined by IsGraphic, the returned string will use a Go escape sequence
// (\t, \n, \xFF, \u0100).
QuoteRuneToGraphic: func(r: int) -> string

// IsPrint reports whether the rune is defined as printable by Go, with
// the same definition as unicode.IsPrint: letters, numbers, punctuation,
// symbols and ASCII space.
IsPrint: validator(int) | (func(r: int) -> bool)

// IsGraphic reports whether the rune is defined as a Graphic by Unicode. Such
// characters include letters, marks, numbers, punctuation, symbols, and
// spaces, from categories L, M, N, P, S, and Zs.
IsGraphic: validator(int) | (func(r: int) -> bool)
