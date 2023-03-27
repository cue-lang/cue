@extern("wasm")
package p

add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")

x0: add(1, 2)
x1: add(-1, 2)
x2: add(100, 1)
