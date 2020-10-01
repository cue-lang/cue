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

package literal

import (
	"testing"
)

func TestIndentTabs(t *testing.T) {
	testCases := []struct {
		in  string
		out string
	}{{
		in: `"""
		foo
		bar
		"""`,
		out: `"""
			foo
			bar
			"""`,
	}, {
		in: `"""
			foo
			bar
			"""`,
		out: `"""
			foo
			bar
			"""`,
	}, {
		in:  `""`,
		out: `""`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			got := IndentTabs(tc.in, 3)
			if got != tc.out {
				t.Errorf("got %s; want %s", got, tc.out)
			}
		})
	}
}
