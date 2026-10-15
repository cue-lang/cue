// Copyright 2020 CUE Authors
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

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package path_test

import (
	"errors"
	"strings"
	"testing"

	"cuelang.org/go/pkg/path"
)

type MatchTest struct {
	pattern, s string
	match      bool
	err        error
}

var matchTests = []MatchTest{
	{"abc", "abc", true, nil},
	{"*", "abc", true, nil},
	{"*c", "abc", true, nil},
	{"a*", "a", true, nil},
	{"a*", "abc", true, nil},
	{"a*", "ab/c", false, nil},
	{"a*/b", "abc/b", true, nil},
	{"a*/b", "a/c/b", false, nil},
	{"a*b*c*d*e*/f", "axbxcxdxe/f", true, nil},
	{"a*b*c*d*e*/f", "axbxcxdxexxx/f", true, nil},
	{"a*b*c*d*e*/f", "axbxcxdxe/xxx/f", false, nil},
	{"a*b*c*d*e*/f", "axbxcxdxexxx/fff", false, nil},
	{"a*b?c*x", "abxbbxdbxebxczzx", true, nil},
	{"a*b?c*x", "abxbbxdbxebxczzy", false, nil},
	{"ab[c]", "abc", true, nil},
	{"ab[b-d]", "abc", true, nil},
	{"ab[e-g]", "abc", false, nil},
	{"ab[^c]", "abc", false, nil},
	{"ab[^b-d]", "abc", false, nil},
	{"ab[^e-g]", "abc", true, nil},
	{"a\\*b", "a*b", true, nil},
	{"a\\*b", "ab", false, nil},
	{"a?b", "a☺b", true, nil},
	{"a[^a]b", "a☺b", true, nil},
	{"a???b", "a☺b", false, nil},
	{"a[^a][^a][^a]b", "a☺b", false, nil},
	{"[a-ζ]*", "α", true, nil},
	{"*[a-ζ]", "A", false, nil},
	{"a?b", "a/b", false, nil},
	{"a*b", "a/b", false, nil},
	{"[\\]a]", "]", true, nil},
	{"[\\-]", "-", true, nil},
	{"[x\\-]", "x", true, nil},
	{"[x\\-]", "-", true, nil},
	{"[x\\-]", "z", false, nil},
	{"[\\-x]", "x", true, nil},
	{"[\\-x]", "-", true, nil},
	{"[\\-x]", "a", false, nil},
	{"[]a]", "]", false, path.ErrBadPattern},
	{"[-]", "-", false, path.ErrBadPattern},
	{"[x-]", "x", false, path.ErrBadPattern},
	{"[x-]", "-", false, path.ErrBadPattern},
	{"[x-]", "z", false, path.ErrBadPattern},
	{"[-x]", "x", false, path.ErrBadPattern},
	{"[-x]", "-", false, path.ErrBadPattern},
	{"[-x]", "a", false, path.ErrBadPattern},
	{"\\", "a", false, path.ErrBadPattern},
	{"[a-b-c]", "a", false, path.ErrBadPattern},
	{"[", "a", false, path.ErrBadPattern},
	{"[^", "a", false, path.ErrBadPattern},
	{"[^bc", "a", false, path.ErrBadPattern},
	{"a[", "a", false, path.ErrBadPattern},
	{"a[", "ab", false, path.ErrBadPattern},
	{"a[", "x", false, path.ErrBadPattern},
	{"a/b[", "x", false, path.ErrBadPattern},
	{"*x", "xxx", true, nil},
	{"**", "ab/c", false, errStarStarDisallowed},
	{"**/c", "ab/c", false, errStarStarDisallowed},
	{"a/b/**", "", false, errStarStarDisallowed},
	{"\\**", "*ab", true, nil},
	{"[x**y]", "*", true, nil},
}

// errStarStarDisallowed has the message of the unexported error which
// [path.Match] returns for patterns containing "**".
var errStarStarDisallowed = errors.New("'**' is not supported in patterns as of yet")

func errp(e error) string {
	if e == nil {
		return "<nil>"
	}
	return e.Error()
}

func TestMatch(t *testing.T) {
	testEachOS(t, []path.OS{path.Unix, path.Windows, path.Plan9}, func(t *testing.T, os path.OS) {
		for _, tt := range matchTests {
			pattern := tt.pattern
			s := tt.s
			if os == path.Windows {
				if strings.Contains(pattern, "\\") {
					// no escape allowed on windows.
					continue
				}
				pattern = path.Clean(pattern, os)
				s = path.Clean(s, os)
			}
			ok, err := path.Match(pattern, s, os)
			// The "**" error is unexported, so compare messages,
			// as well as identity for ErrBadPattern.
			if ok != tt.match || errp(err) != errp(tt.err) ||
				(tt.err == path.ErrBadPattern && err != tt.err) {
				t.Errorf("Match(%#q, %#q, %q) = %v, %q want %v, %q",
					pattern, s, os, ok, errp(err), tt.match, errp(tt.err))
			}
		}
	})
}
