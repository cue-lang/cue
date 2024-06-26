// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package embed

import "testing"

func TestIsHidden(t *testing.T) {
	// These test cases are the same for both Unix and Windows.
	testCases := []struct {
		path string
		want bool
	}{{
		path: "",
		want: false,
	}, {
		path: "foo",
		want: false,
	}, {
		path: ".foo",
		want: true,
	}, {
		path: "foo/bar",
		want: false,
	}, {
		path: "foo/.bar",
		want: true,
	}, {
		path: "x/.foo/bar",
		want: true,
	}}
	c := &compiler{dir: "/tmp"}
	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			got := c.isHidden(tc.path)
			if got != tc.want {
				t.Errorf("isHidden(%q) = %t; want %t", tc.path, got, tc.want)
			}
		})
	}
}
