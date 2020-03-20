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

package filetypes

import "testing"

func TestIsPackage(t *testing.T) {
	testCases := []struct {
		in  string
		out bool
	}{
		{".", true},
		{"..", true},
		{"../.../foo", true},
		{".../foo", true},
		{"./:foo", true},
		{"foo.bar/foo", true},

		// Not supported yet, but could be and isn't anything else valid.
		{":foo", true},

		{"foo.bar", false},
		{"foo:", false},
		{"foo:bar:baz", false},
		{"-", false},
		{"-:foo", false},
	}
	for _, tc := range testCases {
		t.Run(tc.in, func(t *testing.T) {
			got := IsPackage(tc.in)
			if got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
		})
	}
}
