// Copyright 2023 CUE Authors
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

package updater

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func TestVisitPaths(t *testing.T) {
	sa := func(a ...string) string { return strings.Join(a, "\n") }
	testCases := []struct {
		in  string
		out string
	}{{
		in:  "1",
		out: sa(": 1"),
	}, {
		in:  "a: b: c: 1",
		out: sa("a.b.c: 1"),
	}, {
		in: `a: b: {
			c: 1,
			d: "q",
		}`,
		out: sa(
			"a.b.c: 1",
			`a.b.d: "q"`,
		),
	}, {
		in: `a: b: {
				4,
				#c: 1,
				#d: "q",
			}
			a: c: 5`,
		out: sa(
			"a.b: 4",
			"a.c: 5",
		),
	}}
	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			v := ctx.CompileString(tc.in)
			a := []string{}
			VisitPaths(v, func(p cue.Path, v cue.Value) {
				a = append(a, fmt.Sprintf("%v: %v", p, v))
			})
			got := sa(a...)
			if got != tc.out {
				t.Errorf("got %v, want %v", got, tc.out)
			}
		})
	}
}
