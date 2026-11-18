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

// Parts of this file are derived from Go's math/bits package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package bits

// Lsh sets and returns x shifted left by n bits.
Lsh: func(x: int, n: int) -> int

// Rsh sets and returns x shifted right by n bits.
Rsh: func(x: int, n: int) -> int

// At returns the value of the i'th bit of x.
At: func(x: int, i: int) -> int

// Set sets and returns x with x's i'th bit set to b (0 or 1).
// That is, if b is 1 Set returns x with its i'th bit set;
// if b is 0 Set returns x with its i'th bit cleared.
Set: func(x: int, i: int, bit: int) -> int

// And sets and returns a to the bitwise "and" of a and b.
And: func(a: int, b: int) -> int

// Or sets and returns a to the bitwise "or" of a and b (a | b in Go).
Or: func(a: int, b: int) -> int

// Xor sets and returns a to the bitwise xor of a and b (a ^ b in Go).
Xor: func(a: int, b: int) -> int

// Clear sets and returns a to the bitwise "and not" of a and b (a &^ b in Go).
Clear: func(a: int, b: int) -> int

// OnesCount returns the number of one bits ("population count") in x.
OnesCount: func(x: int) -> int

// Len returns the length of the absolute value of x in bits. The bit length
// of 0 is 0.
Len: func(x: int) -> int
