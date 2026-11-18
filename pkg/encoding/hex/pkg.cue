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

// Parts of this file are derived from Go's encoding/hex package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package hex

// EncodedLen returns the length of an encoding of n source bytes.
// Specifically, it returns n * 2.
EncodedLen: func(n: int) -> int

// DecodedLen returns the length of a decoding of x source bytes.
// Specifically, it returns x / 2.
DecodedLen: func(x: int) -> int

// Decode returns the bytes represented by the hexadecimal string s.
//
// Decode expects that s contains only hexadecimal
// characters and that s has even length.
// If the input is malformed, Decode returns
// the bytes decoded before the error.
Decode: func(s: string) -> bytes

// Dump returns a string that contains a hex dump of the given data. The format
// of the hex dump matches the output of `hexdump -C` on the command line.
Dump: func(data: bytes | string) -> string

// Encode returns the hexadecimal encoding of src.
Encode: func(src: bytes | string) -> string
