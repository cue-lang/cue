// SPDX-License-Identifier: Apache-2.0 AND BSD-3-Clause
// Copyright 2020 The CUE Authors
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

// Parts of this file are derived from Go's regexp package.

//cue:pure
package regexp

import (
	"regexp"

	"cuelang.org/go/internal/core/adt"
)

// Match reports whether the string s
// contains any match of the regular expression pattern.
func Match(pattern string, s string) (matched bool, err error) {
	re, err := adt.CompileRegexp(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(s), nil
}

// QuoteMeta returns a string that escapes all regular expression metacharacters
// inside the argument text; the returned string is a regular expression matching
// the literal text.
func QuoteMeta(s string) string {
	return regexp.QuoteMeta(s)
}
