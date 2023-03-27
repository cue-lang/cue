@extern("wasm")
package p

add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")
sub: _ @extern("foo.wasm1", abi=c, sig="func(int64, int64): int64")

x: add(1, 2)
