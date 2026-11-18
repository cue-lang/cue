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

// Parts of this file are derived from Go's crypto/sha512 package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package sha512

// Size is the size, in bytes, of a SHA-512 checksum.
Size: 64

// Size224 is the size, in bytes, of a SHA-512/224 checksum.
Size224: 28

// Size256 is the size, in bytes, of a SHA-512/256 checksum.
Size256: 32

// Size384 is the size, in bytes, of a SHA-384 checksum.
Size384: 48

// BlockSize is the block size, in bytes, of the SHA-512/224,
// SHA-512/256, SHA-384 and SHA-512 hash functions.
BlockSize: 128

// Sum512 returns the SHA512 checksum of the data.
Sum512: func(data: bytes | string) -> bytes

// Sum384 returns the SHA384 checksum of the data.
Sum384: func(data: bytes | string) -> bytes

// Sum512_224 returns the Sum512/224 checksum of the data.
Sum512_224: func(data: bytes | string) -> bytes

// Sum512_256 returns the Sum512/256 checksum of the data.
Sum512_256: func(data: bytes | string) -> bytes
