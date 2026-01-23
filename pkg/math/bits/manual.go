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

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bits

import (
	"fmt"
	"math"
	"math/big"
	"math/bits"
)

// Lsh sets and returns x shifted left by n bits.
func Lsh(x *big.Int, n uint) *big.Int {
	return x.Lsh(x, n)
}

// Rsh sets and returns x shifted right by n bits.
func Rsh(x *big.Int, n uint) *big.Int {
	return x.Rsh(x, n)
}

// At returns the value of the i'th bit of x.
func At(x *big.Int, i uint) (uint, error) {
	if i > math.MaxInt32 {
		return 0, fmt.Errorf("bit index too large")
	}
	return x.Bit(int(i)), nil
}

// Set sets and returns x with x's i'th bit set to b (0 or 1).
// That is, if b is 1 Set returns x with its i'th bit set;
// if b is 0 Set returns x with its i'th bit cleared.
func Set(x *big.Int, i int, bit uint) *big.Int {
	return x.SetBit(x, i, bit)
}

// And sets and returns a to the bitwise "and" of a and b.
func And(a, b *big.Int) *big.Int {
	return a.And(a, b)
}

// Or sets and returns a to the bitwise "or" of a and b (a | b in Go).
func Or(a, b *big.Int) *big.Int {
	return a.Or(a, b)
}

// Xor sets and returns a to the bitwise xor of a and b (a ^ b in Go).
func Xor(a, b *big.Int) *big.Int {
	return a.Xor(a, b)
}

// Clear sets and returns a to the bitwise "and not" of a and b (a &^ b in Go).
func Clear(a, b *big.Int) *big.Int {
	return a.AndNot(a, b)
}

// OnesCount returns the number of one bits ("population count") in x.
func OnesCount(x *big.Int) int {
	var count int
	for _, w := range x.Bits() {
		count += bits.OnesCount64(uint64(w))
	}
	return count
}

// TODO: Reverse, ReverseBytes?
// Not entirely sure what that means for infinite precision.
// Reverse returns the value of x with its bits in reversed order.
// func Reverse(x uint64) uint64 {
// 	return bits.Reverse64(x)
// }

// // ReverseBytes returns the value of x with its bytes in reversed order.
// func ReverseBytes(x uint64) uint64 {
// 	return bits.ReverseBytes64(x)
// }

// Len returns the length of the absolute value of x in bits. The bit length
// of 0 is 0.
func Len(x *big.Int) int {
	return x.BitLen()
}
