// Copyright 2018 The CUE Authors
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

package cue

import "testing"

func TestStripTemplates(t *testing.T) {
	testCases := []testCase{{
		desc: "basic",
		in: `
		foo <Name>: { name: Name }
		foo bar:  { units: 5 }
		`,
		out: `<0>{foo: <1>{bar: <2>{name: "bar", units: 5}}}`,
	}, {
		desc: "top-level template",
		in: `
		<Name>: { name: Name }
		bar:  { units: 5 }
		`,
		out: `<0>{bar: <1>{name: "bar", units: 5}}`,
	}, {
		desc: "with reference",
		in: `
		before: foo.bar
		foo <Name>: { name: Name }
		foo bar:  { units: 5 }
		after: foo.bar
		`,
		out: `<0>{before: <1>{name: "bar", units: 5}, foo: <2>{bar: <3>{name: "bar", units: 5}}, after: <4>{name: "bar", units: 5}}`,
	}, {
		desc: "nested",
		in: `
			<X1> foo <X2> bar <X3>: { name: X1+X2+X3 }
			a foo b bar c: { morning: true }
			`,
		out: `<0>{a: <1>{foo: <2>{b: <3>{bar: <4>{c: <5>{name: "abc", morning: true}}}}}}`}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx, obj := compileFile(t, tc.in)

			v := stripTemplates(ctx, obj)

			if got := debugStr(ctx, v); got != tc.out {
				t.Errorf("output differs:\ngot  %s\nwant %s", got, tc.out)
			}
		})
	}
}
