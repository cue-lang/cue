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

// Parts of this file are derived from Go's encoding/json package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package json

// Valid reports whether data is a valid JSON encoding.
Valid: validator(bytes | string) | (func(data: bytes | string) -> bool)

// Compact generates the JSON-encoded src with insignificant space characters
// elided.
Compact: func(src: bytes | string) -> string

// Indent creates an indented form of the JSON-encoded src.
// Each element in a JSON object or array begins on a new,
// indented line beginning with prefix followed by one or more
// copies of indent according to the indentation nesting.
// The data appended to dst does not begin with the prefix nor
// any indentation, to make it easier to embed inside other formatted JSON data.
// Although leading space characters (space, tab, carriage return, newline)
// at the beginning of src are dropped, trailing space characters
// at the end of src are preserved and copied to dst.
// For example, if src has no trailing spaces, neither will dst;
// if src ends in a trailing newline, so will dst.
Indent: func(src: bytes | string, prefix: string, indent: string) -> string

// HTMLEscape returns the JSON-encoded src with <, >, &, U+2028 and
// U+2029 characters inside string literals changed to \u003c, \u003e, \u0026,
// \u2028, \u2029 so that the JSON will be safe to embed inside HTML <script>
// tags. For historical reasons, web browsers don't honor standard HTML escaping
// within <script> tags, so an alternative JSON encoding must be used.
HTMLEscape: func(src: bytes | string) -> string

// Marshal returns the JSON encoding of v.
Marshal: func(v: _) -> string

// MarshalStream turns a list into a stream of JSON objects.
MarshalStream: func(v: _) -> string

// UnmarshalStream parses the JSON to a CUE instance.
UnmarshalStream: func(data: bytes | string) -> _

// Unmarshal parses the JSON-encoded data.
Unmarshal: func(b: bytes | string) -> _

// Validate validates JSON and confirms it matches the constraints
// specified by v.
Validate: (func(v: _ @schema()) -> validator(bytes | string)) | (func(b: bytes | string, v: _ @schema()) -> bool)
