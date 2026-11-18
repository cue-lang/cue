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

// Parts of this file are derived from Go's crypto/sha256 package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package sha256

// The size of a SHA256 checksum in bytes.
Size: 32

// The size of a SHA224 checksum in bytes.
Size224: 28

// The blocksize of SHA256 and SHA224 in bytes.
BlockSize: 64

// Sum256 returns the SHA256 checksum of the data.
Sum256: func(data: bytes | string) -> bytes

// Sum224 returns the SHA224 checksum of the data.
Sum224: func(data: bytes | string) -> bytes
