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

// Parts of this file are derived from Go's encoding/base64 package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

// Package base64 implements base64 encoding as specified by RFC 4648.
package base64

// EncodedLen returns the length in bytes of the base64 encoding
// of an input buffer of length n. Encoding needs to be set to null
// as only StdEncoding is supported for now.
EncodedLen: func(encoding: null, n: int) -> int

// DecodedLen returns the maximum length in bytes of the decoded data
// corresponding to n bytes of base64-encoded data. Encoding needs to be set to
// null as only StdEncoding is supported for now.
DecodedLen: func(encoding: null, x: int) -> int

// Encode returns the base64 encoding of src. Encoding needs to be set to null
// as only StdEncoding is supported for now.
Encode: func(encoding: null, src: bytes | string) -> string

// Decode returns the bytes represented by the base64 string s. Encoding needs
// to be set to null as only StdEncoding is supported for now.
Decode: func(encoding: null, s: string) -> bytes
