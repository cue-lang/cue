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
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/diff"
	"golang.org/x/tools/txtar"
)

func TestLookupPath(t *testing.T) {
	testCases := []struct {
		in   string
		path cue.Path
		out  string `test:"update"` // :nerdSnipe:
		err  string `test:"update"` // :nerdSnipe:
	}{{
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
		in:   `#foo: 3`,
		path: cue.ParsePath("#foo"),
		out:  `3`,
	}, {
		in:   `_foo: 3`,
		path: cue.MakePath(cue.Def("_foo")),
		err:  `field not found: #_foo`,
	}, {
		in:   `_#foo: 3`,
		path: cue.MakePath(cue.Def("_#foo")),
		err:  `field not found: _#foo`,
	}, {
		in:   `"foo", #foo: 3`,
		path: cue.ParsePath("#foo"),
		out:  `3`,
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
		path: cue.MakePath(cue.Str("a")),
		err:  `field not found: a`,
	}, {
		in: `
		x: {
			[string]: int
		}
		y: x
		`,
		path: cue.MakePath(cue.Str("y"), cue.AnyString),
		out:  `int`,
	}, {
		in: `
		x: {
			[_]: int
		}
		y: x
		`,
		path: cue.MakePath(cue.Str("y"), cue.AnyString),
		out:  `int`,
	}, {
		in:   `t: {...}`,
		path: cue.MakePath(cue.Str("t"), cue.AnyString),
		out:  `_`,
	}, {
		in:   `t: [...]`,
		path: cue.MakePath(cue.Str("t"), cue.AnyIndex),
		out:  `_`,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.path.String(), func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()
			v := mustCompile(t, ctx, tc.in)

			v = v.LookupPath(tc.path)

			if err := v.Err(); err != nil || tc.err != "" {
				if got := err.Error(); got != tc.err {
					t.Errorf("error: got %v; want %v", got, tc.err)
				}
			}

			if exists := v.Exists(); exists != (tc.err == "") {
				t.Fatalf("exists: got %v; want: %v", exists, tc.err == "")
			} else if !exists {
				return
			}

			w := mustCompile(t, ctx, tc.out)

			if k, d := diff.Diff(v, w); k != diff.Identity {
				b := &bytes.Buffer{}
				diff.Print(b, d)
				t.Error(b)
			}
		})
	}
}

func TestLookupPathOnExprListElement(t *testing.T) {
	// Regression test for issue where LookupPath on list elements returned
	// from Expr() would panic due to unfinalized vertices.
	ctx := cuecontext.New()
	v := ctx.CompileString(`a: matchN(1, [_])`)
	op, args := v.LookupPath(cue.ParsePath("a")).Eval().Expr()
	if op != cue.CallOp || len(args) != 3 {
		t.Fatalf("unexpected expr results: %v %v", op, args)
	}

	// This should not panic - we're looking up an element in the list argument
	top := args[2].LookupPath(cue.MakePath(cue.Index(0)))
	if got := fmt.Sprint(top); got != "_" {
		t.Errorf("unexpected value for list element: got %q, want %q", got, "_")
	}
}

func TestHidden(t *testing.T) {
	in := `
-- cue.mod/module.cue --
module: "mod.test"
language: version: "v0.9.0"
-- in.cue --
import "mod.test/foo"

a: foo.C
b: _c
_c: 2
-- foo/foo.cue --
package foo

C: _d
_d: 3
		`

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	v := cuecontext.New().BuildInstance(instance)

	testCases := []struct {
		path cue.Path
		pkg  string
	}{{
		path: cue.ParsePath("a"),
		pkg:  "mod.test/foo",
	}, {
		path: cue.ParsePath("b"),
		pkg:  "_",
	}}
	for _, tc := range testCases {
		t.Run(tc.path.String(), func(t *testing.T) {
			v := v.LookupPath(tc.path)
			p := cue.Dereference(cue.Dereference(v)).Path().Selectors()
			if got := p[len(p)-1].PkgPath(); got != tc.pkg {
				t.Errorf("got %v; want %v", got, tc.pkg)
			}
		})
	}
}
