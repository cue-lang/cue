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

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package math

// largest supported exponent
MaxExp: 2147483647

// smallest supported exponent
MinExp: -2147483648

// largest (theoretically) supported precision; likely memory-limited
MaxPrec: 4294967295

// == IEEE 754-2008 roundTiesToEven
ToNearestEven: 0

// == IEEE 754-2008 roundTiesToAway
ToNearestAway: 1

// == IEEE 754-2008 roundTowardZero
ToZero: 2

// no IEEE 754-2008 equivalent
AwayFromZero: 3

// == IEEE 754-2008 roundTowardNegative
ToNegativeInf: 4

// == IEEE 754-2008 roundTowardPositive
ToPositiveInf: 5

Below: -1

Exact: 0

Above: 1

// Jacobi returns the Jacobi symbol (x/y), either +1, -1, or 0.
// The y argument must be an odd integer.
Jacobi: func(x: int, y: int) -> int

// MaxBase is the largest number base accepted for string conversions.
MaxBase: 62

// Floor returns the greatest integer value less than or equal to x.
//
// It is an error if x is an infinity or NaN.
Floor: func(x: number) -> int

// Ceil returns the least integer value greater than or equal to x.
//
// It is an error if x is an infinity or NaN.
Ceil: func(x: number) -> int

// Trunc returns the integer value of x.
//
// It is an error if x is an infinity or NaN.
Trunc: func(x: number) -> int

// Round returns the nearest integer, rounding half away from zero.
//
// It is an error if x is an infinity or NaN.
Round: func(x: number) -> int

// RoundToEven returns the nearest integer, rounding ties to even.
//
// It is an error if x is an infinity or NaN.
RoundToEven: func(x: number) -> int

// MultipleOf reports whether x is a multiple of y.
MultipleOf: (func(y: number) -> validator(number)) | (func(x: number, y: number) -> bool)

// Abs returns the absolute value of x.
//
// Special case: Abs(±Inf) = +Inf
Abs: func(x: number) -> number

// Acosh returns the inverse hyperbolic cosine of x.
//
// Special cases are:
//
// 	Acosh(+Inf) = +Inf
// 	Acosh(x) = NaN if x < 1
// 	Acosh(NaN) = NaN
Acosh: func(x: number) -> number

// Asin returns the arcsine, in radians, of x.
//
// Special cases are:
//
// 	Asin(±0) = ±0
// 	Asin(x) = NaN if x < -1 or x > 1
Asin: func(x: number) -> number

// Acos returns the arccosine, in radians, of x.
//
// Special case is:
//
// 	Acos(x) = NaN if x < -1 or x > 1
Acos: func(x: number) -> number

// Asinh returns the inverse hyperbolic sine of x.
//
// Special cases are:
//
// 	Asinh(±0) = ±0
// 	Asinh(±Inf) = ±Inf
// 	Asinh(NaN) = NaN
Asinh: func(x: number) -> number

// Atan returns the arctangent, in radians, of x.
//
// Special cases are:
//
// 	Atan(±0) = ±0
// 	Atan(±Inf) = ±Pi/2
Atan: func(x: number) -> number

// Atan2 returns the arc tangent of y/x, using
// the signs of the two to determine the quadrant
// of the return value.
//
// Special cases are (in order):
//
// 	Atan2(y, NaN) = NaN
// 	Atan2(NaN, x) = NaN
// 	Atan2(+0, x>=0) = +0
// 	Atan2(-0, x>=0) = -0
// 	Atan2(+0, x<=-0) = +Pi
// 	Atan2(-0, x<=-0) = -Pi
// 	Atan2(y>0, 0) = +Pi/2
// 	Atan2(y<0, 0) = -Pi/2
// 	Atan2(+Inf, +Inf) = +Pi/4
// 	Atan2(-Inf, +Inf) = -Pi/4
// 	Atan2(+Inf, -Inf) = 3Pi/4
// 	Atan2(-Inf, -Inf) = -3Pi/4
// 	Atan2(y, +Inf) = 0
// 	Atan2(y>0, -Inf) = +Pi
// 	Atan2(y<0, -Inf) = -Pi
// 	Atan2(+Inf, x) = +Pi/2
// 	Atan2(-Inf, x) = -Pi/2
Atan2: func(y: number, x: number) -> number

// Atanh returns the inverse hyperbolic tangent of x.
//
// Special cases are:
//
// 	Atanh(1) = +Inf
// 	Atanh(±0) = ±0
// 	Atanh(-1) = -Inf
// 	Atanh(x) = NaN if x < -1 or x > 1
// 	Atanh(NaN) = NaN
Atanh: func(x: number) -> number

// Cbrt returns the cube root of x.
//
// Special cases are:
//
// 	Cbrt(±0) = ±0
// 	Cbrt(±Inf) = ±Inf
// 	Cbrt(NaN) = NaN
Cbrt: func(x: number) -> number

// https://oeis.org/A001113
E: 2.71828182845904523536028747135266249775724709369995957496696763

// https://oeis.org/A000796
Pi: 3.14159265358979323846264338327950288419716939937510582097494459

// https://oeis.org/A001622
Phi: 1.61803398874989484820458683436563811772030917980576286213544861

// https://oeis.org/A002193
Sqrt2: 1.41421356237309504880168872420969807856967187537694807317667974

// https://oeis.org/A019774
SqrtE: 1.64872127070012814684865078781416357165377610071014801157507931

// https://oeis.org/A002161
SqrtPi: 1.77245385090551602729816748334114518279754945612238712821380779

// https://oeis.org/A139339
SqrtPhi: 1.27201964951406896425242246173749149171560804184009624861664038

// https://oeis.org/A002162
Ln2: 0.693147180559945309417232121458176568075500134360255254120680009

Log2E: 1.442695040888963407359924681001892137426645954152985934135449408

// https://oeis.org/A002392
Ln10: 2.3025850929940456840179914546843642076011014886287729760333278

Log10E: 0.43429448190325182765112891891660508229439700580366656611445378

// Copysign returns a value with the magnitude
// of x and the sign of y.
Copysign: func(x: number, y: number) -> number

// Dim returns the maximum of x-y or 0.
//
// Special cases are:
//
// 	Dim(+Inf, +Inf) = NaN
// 	Dim(-Inf, -Inf) = NaN
// 	Dim(x, NaN) = Dim(NaN, x) = NaN
Dim: func(x: number, y: number) -> number

// Erf returns the error function of x.
//
// Special cases are:
//
// 	Erf(+Inf) = 1
// 	Erf(-Inf) = -1
// 	Erf(NaN) = NaN
Erf: func(x: number) -> number

// Erfc returns the complementary error function of x.
//
// Special cases are:
//
// 	Erfc(+Inf) = 0
// 	Erfc(-Inf) = 2
// 	Erfc(NaN) = NaN
Erfc: func(x: number) -> number

// Erfinv returns the inverse error function of x.
//
// Special cases are:
//
// 	Erfinv(1) = +Inf
// 	Erfinv(-1) = -Inf
// 	Erfinv(x) = NaN if x < -1 or x > 1
// 	Erfinv(NaN) = NaN
Erfinv: func(x: number) -> number

// Erfcinv returns the inverse of Erfc(x).
//
// Special cases are:
//
// 	Erfcinv(0) = +Inf
// 	Erfcinv(2) = -Inf
// 	Erfcinv(x) = NaN if x < 0 or x > 2
// 	Erfcinv(NaN) = NaN
Erfcinv: func(x: number) -> number

// Exp returns e**x, the base-e exponential of x.
//
// Special cases are:
//
// 	Exp(+Inf) = +Inf
// 	Exp(NaN) = NaN
//
// Very large values overflow to 0 or +Inf.
// Very small values underflow to 1.
Exp: func(x: number) -> number

// Exp2 returns 2**x, the base-2 exponential of x.
//
// Special cases are the same as Exp.
Exp2: func(x: number) -> number

// Expm1 returns e**x - 1, the base-e exponential of x minus 1.
// It is more accurate than Exp(x) - 1 when x is near zero.
//
// Special cases are:
//
// 	Expm1(+Inf) = +Inf
// 	Expm1(-Inf) = -1
// 	Expm1(NaN) = NaN
//
// Very large values overflow to -1 or +Inf.
Expm1: func(x: number) -> number

// Gamma returns the Gamma function of x.
//
// Special cases are:
//
// 	Gamma(+Inf) = +Inf
// 	Gamma(+0) = +Inf
// 	Gamma(-0) = -Inf
// 	Gamma(x) = NaN for integer x < 0
// 	Gamma(-Inf) = NaN
// 	Gamma(NaN) = NaN
Gamma: func(x: number) -> number

// Hypot returns Sqrt(p*p + q*q), taking care to avoid
// unnecessary overflow and underflow.
//
// Special cases are:
//
// 	Hypot(±Inf, q) = +Inf
// 	Hypot(p, ±Inf) = +Inf
// 	Hypot(NaN, q) = NaN
// 	Hypot(p, NaN) = NaN
Hypot: func(p: number, q: number) -> number

// J0 returns the order-zero Bessel function of the first kind.
//
// Special cases are:
//
// 	J0(±Inf) = 0
// 	J0(0) = 1
// 	J0(NaN) = NaN
J0: func(x: number) -> number

// Y0 returns the order-zero Bessel function of the second kind.
//
// Special cases are:
//
// 	Y0(+Inf) = 0
// 	Y0(0) = -Inf
// 	Y0(x < 0) = NaN
// 	Y0(NaN) = NaN
Y0: func(x: number) -> number

// J1 returns the order-one Bessel function of the first kind.
//
// Special cases are:
//
// 	J1(±Inf) = 0
// 	J1(NaN) = NaN
J1: func(x: number) -> number

// Y1 returns the order-one Bessel function of the second kind.
//
// Special cases are:
//
// 	Y1(+Inf) = 0
// 	Y1(0) = -Inf
// 	Y1(x < 0) = NaN
// 	Y1(NaN) = NaN
Y1: func(x: number) -> number

// Jn returns the order-n Bessel function of the first kind.
//
// Special cases are:
//
// 	Jn(n, ±Inf) = 0
// 	Jn(n, NaN) = NaN
Jn: func(n: int, x: number) -> number

// Yn returns the order-n Bessel function of the second kind.
//
// Special cases are:
//
// 	Yn(n, +Inf) = 0
// 	Yn(n ≥ 0, 0) = -Inf
// 	Yn(n < 0, 0) = +Inf if n is odd, -Inf if n is even
// 	Yn(n, x < 0) = NaN
// 	Yn(n, NaN) = NaN
Yn: func(n: int, x: number) -> number

// Ldexp is the inverse of Frexp.
// It returns frac × 2**exp.
//
// Special cases are:
//
// 	Ldexp(±0, exp) = ±0
// 	Ldexp(±Inf, exp) = ±Inf
// 	Ldexp(NaN, exp) = NaN
Ldexp: func(frac: number, exp: int) -> number

// Log returns the natural logarithm of x.
//
// Special cases are:
//
// 	Log(+Inf) = +Inf
// 	Log(0) = -Inf
// 	Log(x < 0) = NaN
// 	Log(NaN) = NaN
Log: func(x: number) -> number

// Log10 returns the decimal logarithm of x.
// The special cases are the same as for Log.
Log10: func(x: number) -> number

// Log2 returns the binary logarithm of x.
// The special cases are the same as for Log.
Log2: func(x: number) -> number

// Log1p returns the natural logarithm of 1 plus its argument x.
// It is more accurate than Log(1 + x) when x is near zero.
//
// Special cases are:
//
// 	Log1p(+Inf) = +Inf
// 	Log1p(±0) = ±0
// 	Log1p(-1) = -Inf
// 	Log1p(x < -1) = NaN
// 	Log1p(NaN) = NaN
Log1p: func(x: number) -> number

// Logb returns the binary exponent of x.
//
// Special cases are:
//
// 	Logb(±Inf) = +Inf
// 	Logb(0) = -Inf
// 	Logb(NaN) = NaN
Logb: func(x: number) -> number

// Ilogb returns the binary exponent of x as an integer.
//
// Special cases are:
//
// 	Ilogb(±Inf) = MaxInt32
// 	Ilogb(0) = MinInt32
// 	Ilogb(NaN) = MaxInt32
Ilogb: func(x: number) -> int

// Mod returns the floating-point remainder of x/y.
// The magnitude of the result is less than y and its
// sign agrees with that of x.
//
// Special cases are:
//
// 	Mod(±Inf, y) = NaN
// 	Mod(NaN, y) = NaN
// 	Mod(x, 0) = NaN
// 	Mod(x, ±Inf) = x
// 	Mod(x, NaN) = NaN
Mod: func(x: number, y: number) -> number

// Pow returns x**y, the base-x exponential of y.
//
// Special cases are (in order):
//
// 	Pow(x, ±0) = 1 for any x
// 	Pow(1, y) = 1 for any y
// 	Pow(x, 1) = x for any x
// 	Pow(NaN, y) = NaN
// 	Pow(x, NaN) = NaN
// 	Pow(±0, y) = ±Inf for y an odd integer < 0
// 	Pow(±0, -Inf) = +Inf
// 	Pow(±0, +Inf) = +0
// 	Pow(±0, y) = +Inf for finite y < 0 and not an odd integer
// 	Pow(±0, y) = ±0 for y an odd integer > 0
// 	Pow(±0, y) = +0 for finite y > 0 and not an odd integer
// 	Pow(-1, ±Inf) = 1
// 	Pow(x, +Inf) = +Inf for |x| > 1
// 	Pow(x, -Inf) = +0 for |x| > 1
// 	Pow(x, +Inf) = +0 for |x| < 1
// 	Pow(x, -Inf) = +Inf for |x| < 1
// 	Pow(+Inf, y) = +Inf for y > 0
// 	Pow(+Inf, y) = +0 for y < 0
// 	Pow(-Inf, y) = Pow(-0, -y)
// 	Pow(x, y) = NaN for finite x < 0 and finite non-integer y
Pow: func(x: number, y: number) -> number

// Pow10 returns 10**n, the base-10 exponential of n.
Pow10: func(n: int) -> number

// Remainder returns the IEEE 754 floating-point remainder of x/y.
//
// Special cases are:
//
// 	Remainder(±Inf, y) = NaN
// 	Remainder(NaN, y) = NaN
// 	Remainder(x, 0) = NaN
// 	Remainder(x, ±Inf) = x
// 	Remainder(x, NaN) = NaN
Remainder: func(x: number, y: number) -> number

// Signbit reports whether x is negative or negative zero.
Signbit: validator(number) | (func(x: number) -> bool)

// Cos returns the cosine of the radian argument x.
//
// Special cases are:
//
// 	Cos(±Inf) = NaN
// 	Cos(NaN) = NaN
Cos: func(x: number) -> number

// Sin returns the sine of the radian argument x.
//
// Special cases are:
//
// 	Sin(±0) = ±0
// 	Sin(±Inf) = NaN
// 	Sin(NaN) = NaN
Sin: func(x: number) -> number

// Sinh returns the hyperbolic sine of x.
//
// Special cases are:
//
// 	Sinh(±0) = ±0
// 	Sinh(±Inf) = ±Inf
// 	Sinh(NaN) = NaN
Sinh: func(x: number) -> number

// Cosh returns the hyperbolic cosine of x.
//
// Special cases are:
//
// 	Cosh(±0) = 1
// 	Cosh(±Inf) = +Inf
// 	Cosh(NaN) = NaN
Cosh: func(x: number) -> number

// Sqrt returns the square root of x.
//
// Special cases are:
//
// 	Sqrt(+Inf) = +Inf
// 	Sqrt(±0) = ±0
// 	Sqrt(x < 0) = NaN
// 	Sqrt(NaN) = NaN
Sqrt: func(x: number) -> number

// Tan returns the tangent of the radian argument x.
//
// Special cases are:
//
// 	Tan(±0) = ±0
// 	Tan(±Inf) = NaN
// 	Tan(NaN) = NaN
Tan: func(x: number) -> number

// Tanh returns the hyperbolic tangent of x.
//
// Special cases are:
//
// 	Tanh(±0) = ±0
// 	Tanh(±Inf) = ±1
// 	Tanh(NaN) = NaN
Tanh: func(x: number) -> number
