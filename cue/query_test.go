// Copyright 2021 CUE Authors
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
	"bytes"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/diff"
)

func TestLookupPath(t *testing.T) {
	r := &cue.Runtime{}

	testCases := []struct {
		in       string
		path     cue.Path
		out      string `test:"update"` // :nerdSnipe:
		notExist bool   `test:"update"` // :nerdSnipe:
	}{{
		in: `
		[Name=string]: { a: Name }
		`,
		path:     cue.MakePath(cue.Str("a")),
		notExist: true,
	}, {
		in: `
		#V: {
			x: int
		}
		#X: {
			[string]: int64
		} & #V
		v: #X
		`,
		path: cue.ParsePath("v.x"),
		out:  `int64`,
	}, {
		in: `
		a: [...int]
		`,
		path: cue.MakePath(cue.Str("a"), cue.AnyIndex),
		out:  `int`,
	}, {
		in: `
		[Name=string]: { a: Name }
		`,
		path: cue.MakePath(cue.AnyString, cue.Str("a")),
		out:  `string`,
	}, {
		in: `
		[Name=string]: { a: Name }
		`,
		path: cue.MakePath(cue.Str("b").Optional(), cue.Str("a")),
		out:  `"b"`,
	}, {
		in: `
		[Name=string]: { a: Name }
		`,
		path: cue.MakePath(cue.AnyString),
		out:  `{a: string}`,
	}, {
		in: `
		a: [Foo=string]: [Bar=string]: { b: Foo+Bar }
		`,
		path: cue.MakePath(cue.Str("a"), cue.Str("b"), cue.Str("c")).Optional(),
		out:  `{b: "bc"}`,
	}, {
		in: `
		a: [Foo=string]: b: [Bar=string]: { c: Foo }
		a: foo: b: [Bar=string]: { d: Bar }
		`,
		path: cue.MakePath(cue.Str("a"), cue.Str("foo"), cue.Str("b"), cue.AnyString),
		out:  `{c: "foo", d: string}`,
	}, {
		in: `
		[Name=string]: { a: Name }
		`,
		path:     cue.MakePath(cue.Str("a")),
		notExist: true,
	}}
	for _, tc := range testCases {
		t.Run(tc.path.String(), func(t *testing.T) {
			v := compileT(t, r, tc.in)

			v = v.LookupPath(tc.path)

			if exists := v.Exists(); exists != !tc.notExist {
				t.Fatalf("exists: got %v; want: %v", exists, !tc.notExist)
			} else if !exists {
				return
			}

			w := compileT(t, r, tc.out)

			if k, d := diff.Diff(v, w); k != diff.Identity {
				b := &bytes.Buffer{}
				diff.Print(b, d)
				t.Error(b)
			}
		})
	}
}

func compileT(t *testing.T, r *cue.Runtime, s string) cue.Value {
	t.Helper()
	inst, err := r.Compile("", s)
	if err != nil {
		t.Fatal(err)
	}
	return inst.Value()
}
