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

// Parts of this file are derived from Go's html package.

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

package html

// Escape escapes special characters like "<" to become "&lt;". It
// escapes only five such characters: <, >, &, ' and ".
// Unescape(Escape(s)) == s always holds, but the converse isn't
// always true.
Escape: func(s: string) -> string

// Unescape unescapes entities like "&lt;" to become "<". It unescapes a
// larger range of entities than Escape escapes. For example, "&aacute;"
// unescapes to "á", as does "&#225;" and "&#xE1;".
// Unescape(Escape(s)) == s always holds, but the converse isn't
// always true.
Unescape: func(s: string) -> string
