@extern("wasm")
package p

add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64)")
mul: _ @extern("foo.wasm", abi=c, sig="func(float64, float64): []")
not: _ @extern("foo.wasm", abi=c, sig="func(*): bool")
