package p

add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")

x00: add(1, 2)
