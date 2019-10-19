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
	"math/big"
	"math/bits"
)

// And returns the bitwise and of a and b (a & b in Go).
//
func And(a, b *big.Int) *big.Int {
	wa := a.Bits()
	wb := b.Bits()
	n := len(wa)
	if len(wb) < n {
		n = len(wb)
	}
	w := make([]big.Word, n)
	for i := range w {
		w[i] = wa[i] & wb[i]
	}
	i := &big.Int{}
	i.SetBits(w)
	return i
}

// Or returns the bitwise or of a and b (a | b in Go).
//
func Or(a, b *big.Int) *big.Int {
	wa := a.Bits()
	wb := b.Bits()
	var w []big.Word
	n := len(wa)
	if len(wa) > len(wb) {
		w = append(w, wa...)
		n = len(wb)
	} else {
		w = append(w, wb...)
	}
	for i := 0; i < n; i++ {
		w[i] = wa[i] | wb[i]
	}
	i := &big.Int{}
	i.SetBits(w)
	return i
}

// Xor returns the bitwise xor of a and b (a ^ b in Go).
//
func Xor(a, b *big.Int) *big.Int {
	wa := a.Bits()
	wb := b.Bits()
	var w []big.Word
	n := len(wa)
	if len(wa) > len(wb) {
		w = append(w, wa...)
		n = len(wb)
	} else {
		w = append(w, wb...)
	}
	for i := 0; i < n; i++ {
		w[i] = wa[i] ^ wb[i]
	}
	i := &big.Int{}
	i.SetBits(w)
	return i
}

// Clear returns the bitwise and not of a and b (a &^ b in Go).
//
func Clear(a, b *big.Int) *big.Int {
	wa := a.Bits()
	wb := b.Bits()
	w := append([]big.Word(nil), wa...)
	for i, m := range wb {
		if i >= len(w) {
			break
		}
		w[i] = wa[i] &^ m
	}
	i := &big.Int{}
	i.SetBits(w)
	return i
}

// TODO: ShiftLeft, maybe trailing and leading zeros

// OnesCount returns the number of one bits ("population count") in x.
func OnesCount(x uint64) int {
	return bits.OnesCount64(x)
}

// Reverse returns the value of x with its bits in reversed order.
func Reverse(x uint64) uint64 {
	return bits.Reverse64(x)
}

// ReverseBytes returns the value of x with its bytes in reversed order.
func ReverseBytes(x uint64) uint64 {
	return bits.ReverseBytes64(x)
}

// Len returns the minimum number of bits required to represent x; the result is 0 for x == 0.
func Len(x uint64) int {
	return bits.Len64(x)
}
