@extern("wasm")
package p

add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")
isPrime: _ @extern("bar.wasm", abi=c, name=is_prime, sig="func(uint64): bool")
fact: _ @extern("bar.wasm", abi=c, sig="func(uint64): uint64")

a0: neg32(42)

b1: isPrime(127)
b2: isPrime(128)

c1: fact(7)
c2: fact(9)
