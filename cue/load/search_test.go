// Copyright 2026 The CUE Authors
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

package load

import "testing"

func TestMatchPattern(t *testing.T) {
	testCases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"...", ".", true},
		{"...", "a", true},
		{"...", "a/b", true},

		// A trailing "/..." also matches the path before the slash.
		{"a/...", "a", true},
		{"a/...", "a/b", true},
		{"a/...", "a/b/c", true},
		{"a/...", ".", false},
		{"a/...", "b", false},
		{"a/...", "ab", false},

		// A wildcard in the middle must match at least one path element.
		{".../x", "x", false},
		{".../x", "a/x", true},
		{".../x", "a/b/x", true},
		{".../x", "a/y", false},
		{".../x", "a/x/y", false},
		{"a/.../x", "a/x", false},
		{"a/.../x", "a/b/x", true},
		{"a/.../x", "b/c/x", false},

		// A wildcard may appear within a path element.
		{"a.../x", "abc/x", true},
		{"a.../x", "b/x", false},

		{"../a/...", "../a", true},
		{"../a/...", "../a/b", true},
		{"../a/...", "../b", false},
	}
	for _, tc := range testCases {
		if got := matchPattern(tc.pattern)(tc.name); got != tc.want {
			t.Errorf("matchPattern(%q)(%q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}
