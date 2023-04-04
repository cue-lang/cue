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

package cue_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/tdtest"
)

func TestPatch(t *testing.T) {
	type testCase struct {
		in    string
		path  string
		patch any

		outPath  string
		outValue string
		outRoot  string
	}
	tests := []testCase{{
		in: `
			a: [1, 2, 3]
			y: a
			z: y`,
		path:  "y",
		patch: []int{4, 5, 6},

		outPath:  `y`,
		outValue: `[4, 5, 6]`,
		outRoot: `
		{
			a: [1, 2, 3]
			y: [4, 5, 6]
			z: [1, 2, 3]
		}`,
	}, {
		in: `
			z: v.w.x
			a: [1, 2, 3]
			v: w: x: a
		`,
		path:  "v.w.x",
		patch: 4,

		outPath:  `v.w.x`,
		outValue: `4`,
		outRoot: `
		{
			z: [1, 2, 3]
			a: [1, 2, 3]
			v: {
				w: {
					x: 4
				}
			}
		}`,
	}, {
		in: `
			a: 1
			`,
		path:  "b.c.d",
		patch: 4,

		outPath:  `b.c.d`,
		outValue: `4`,
		outRoot: `
		{
			a: 1
			b: {
				c: {
					d: 4
				}
			}
		}`,
	}}

	ctx := cuecontext.New()

	// TODO: use cuetest.Run when supported.
	tdtest.Run(t, tests, func(t *cuetest.T, tc *testCase) {
		v := ctx.CompileString(tc.in)
		if err := v.Err(); err != nil {
			t.Fatal(err)
		}

		path := cue.ParsePath(tc.path)
		v = v.Patch(path, tc.patch)
		w := v.LookupPath(path)

		t.Equal(w.Path().String(), tc.outPath)
		t.Equal(tdtest.Indent(fmt.Sprint(w), 2), tc.outValue)
		t.Equal(tdtest.Indent(fmt.Sprint(v), 2), tc.outRoot)
	})
}
