@extern("wasm")
package p

add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")
mul: _ @extern("foo.wasm", abi=c, sig="func(float64, float64): float64")
not: _ @extern("foo.wasm", abi=c, sig="func(bool): bool")

x0: add(1, 2)
x1: add(-1, 2)
x2: add(100, 1)

y0: mul(3.0, 5.0)
y1: mul(-2.5, 3.37)
y2: mul(1.234, 5.678)

z: not(true)
