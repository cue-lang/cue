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
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestLookupPath(t *testing.T) {
	testCases := []struct {
		in   string
		path cue.Path
		out  string `test:"update"` // :nerdSnipe:
		err  string `test:"update"` // :nerdSnipe:
		// exists is stated for every case, and is independent of err: a
		// path exists when the configuration holds a value at it, even a
		// value that fails to evaluate. A conflict or an explicit error
		// therefore exists, while an absent, optional, or required field
		// does not. out is only compared when err is empty.
		exists bool
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
		path:   cue.ParsePath("v.x"),
		out:    `int64`,
		exists: true,
	}, {
		in:     `#foo: 3`,
		path:   cue.ParsePath("#foo"),
		out:    `3`,
		exists: true,
	}, {
		in:     `_foo: 3`,
		path:   cue.MakePath(cue.Def("_foo")),
		err:    `field not found: #_foo`,
		exists: false,
	}, {
		in:     `_#foo: 3`,
		path:   cue.MakePath(cue.Def("_#foo")),
		err:    `field not found: _#foo`,
		exists: false,
	}, {
		in:     `"foo", #foo: 3`,
		path:   cue.ParsePath("#foo"),
		out:    `3`,
		exists: true,
	}, {
		in: `
		a: [...int]
		`,
		path:   cue.MakePath(cue.Str("a"), cue.AnyIndex),
		out:    `int`,
		exists: true,
	}, {
		in: `
		[string]~(Name,_): { a: Name }
		`,
		path:   cue.MakePath(cue.AnyString, cue.Str("a")),
		out:    `string`,
		exists: true,
	}, {
		in: `
		[string]~(Name,_): { a: Name }
		`,
		path:   cue.MakePath(cue.Str("b").Optional(), cue.Str("a")),
		out:    `"b"`,
		exists: true,
	}, {
		in: `
		[string]~(Name,_): { a: Name }
		`,
		path:   cue.MakePath(cue.AnyString),
		out:    `{a: string}`,
		exists: true,
	}, {
		in: `
		a: [string]~(Foo,_): [string]~(Bar,_): { b: Foo+Bar }
		`,
		path:   cue.MakePath(cue.Str("a"), cue.Str("b"), cue.Str("c")).Optional(),
		out:    `{b: "bc"}`,
		exists: true,
	}, {
		in: `
		a: [string]~(Foo,_): b: [string]~(Bar,_): { c: Foo }
		a: foo: b: [string]~(Bar,_): { d: Bar }
		`,
		path:   cue.MakePath(cue.Str("a"), cue.Str("foo"), cue.Str("b"), cue.AnyString),
		out:    `{c: "foo", d: string}`,
		exists: true,
	}, {
		in: `
		[string]~(Name,_): { a: Name }
		`,
		path:   cue.MakePath(cue.Str("a")),
		err:    `field not found: a`,
		exists: false,
	}, {
		in: `
		x: {
			[string]: int
		}
		y: x
		`,
		path:   cue.MakePath(cue.Str("y"), cue.AnyString),
		out:    `int`,
		exists: true,
	}, {
		in: `
		x: {
			[_]: int
		}
		y: x
		`,
		path:   cue.MakePath(cue.Str("y"), cue.AnyString),
		out:    `int`,
		exists: true,
	}, {
		in:     `t: {...}`,
		path:   cue.MakePath(cue.Str("t"), cue.AnyString),
		out:    `_`,
		exists: true,
	}, {
		in:     `t: [...]`,
		path:   cue.MakePath(cue.Str("t"), cue.AnyIndex),
		out:    `_`,
		exists: true,
	}, {
		// A required field holds no value of its own, so looking it up is
		// indistinguishable from looking up an undeclared field.
		in:     `x!: int`,
		path:   cue.ParsePath("x"),
		err:    `field not found: x`,
		exists: false,
	}, {
		// A reference to a required field is itself declared, so it
		// resolves to a value which exists and carries an incomplete
		// error.
		in: `
		x!: int
		z:  x
		`,
		path:   cue.ParsePath("z"),
		err:    `z: required field missing: x`,
		exists: true,
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

			if exists := v.Exists(); exists != tc.exists {
				t.Fatalf("exists: got %v; want: %v", exists, tc.exists)
			}
			if tc.err != "" {
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

// TestLookupPathErroredParent checks that LookupPath steps into a struct
// which is an error because of one of its fields, returning the values of
// its other fields as if the struct were not an error.
func TestLookupPathErroredParent(t *testing.T) {
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		ctx := m.CueContext()
		v := ctx.CompileString(`
			a: {
				x: "test"
				_y: 1 & 2
			}
			`, cue.Filename("test"))
		qt.Assert(t, qt.ErrorMatches(v.Err(), `a._y: conflicting values 2 and 1`))

		a := v.LookupPath(cue.ParsePath("a"))
		qt.Assert(t, qt.ErrorMatches(a.Err(), `a._y: conflicting values 2 and 1`))

		x := a.LookupPath(cue.ParsePath("x"))
		qt.Assert(t, qt.IsNil(x.Err()))
		s, err := x.String()
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(s, "test"))

		// Evaluating the same path as a CUE expression does fail,
		// as selecting from an error results in that error.
		e := ctx.CompileString(`a.x`, cue.Scope(v))
		qt.Assert(t, qt.ErrorMatches(e.Err(), `a._y: conflicting values 2 and 1`))
	})
}

func TestLookupPathConstraintSelector(t *testing.T) {
	// A required or optional field is a constraint rather than a value of its
	// own, so a plain selector does not reach it until a value is supplied. A
	// selector made with Selector.Required or Selector.Optional matches the
	// constraint itself, which exists either way.
	testCases := []struct {
		in     string
		sel    cue.Selector
		exists bool
	}{{
		in:     `x!: int`,
		sel:    cue.Str("x"),
		exists: false,
	}, {
		in:     `x!: int`,
		sel:    cue.Str("x").Required(),
		exists: true,
	}, {
		in:     `x?: int`,
		sel:    cue.Str("x"),
		exists: false,
	}, {
		in:     `x?: int`,
		sel:    cue.Str("x").Optional(),
		exists: true,
	}, {
		// A constraint selector also matches an ordinary field.
		in:     `x: 1`,
		sel:    cue.Str("x").Required(),
		exists: true,
	}, {
		// It does not conjure a field which was never declared.
		in:     `y: 1`,
		sel:    cue.Str("x").Required(),
		exists: false,
	}}
	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.in+"/"+tc.sel.String(), func(t *testing.T) {
			v := mustCompile(t, ctx, tc.in)
			w := v.LookupPath(cue.MakePath(tc.sel))
			if got := w.Exists(); got != tc.exists {
				t.Errorf("exists: got %v; want %v", got, tc.exists)
			}
		})
	}
}

func TestLookupPathDanglingReference(t *testing.T) {
	// A field whose value is a dangling reference is unlike any other failing
	// lookup: the configuration does not compile, so the field is declared yet
	// LookupPath reports it as absent.
	//
	// TODO: a is declared, so it should exist. Reporting the root's error
	// here is not the fix: a compile error leaves the root vertex without
	// arcs, so that makes every lookup exist, including a genuinely absent
	// one, and callers which use Exists to test for a field then never stop
	// looking. Value.Lookup below is no guide either, reporting absent
	// fields as existing for the same reason. Retaining the arcs of a vertex
	// which failed to compile is the place to start.
	ctx := cuecontext.New()
	v := ctx.CompileString(`a: b`)
	if got, want := fmt.Sprint(v.Err()), `a: reference "b" not found`; got != want {
		t.Errorf("root:\n got %v\nwant %v", got, want)
	}

	w := v.LookupPath(cue.ParsePath("a"))
	if w.Exists() {
		t.Error("LookupPath: got exists true; want false")
	}
	if got, want := fmt.Sprint(w.Err()), `field not found: a`; got != want {
		t.Errorf("LookupPath:\n got %v\nwant %v", got, want)
	}

	w = v.Lookup("a")
	if !w.Exists() {
		t.Error("Lookup: got exists false; want true")
	}
	if got, want := fmt.Sprint(w.Err()), `a: reference "b" not found`; got != want {
		t.Errorf("Lookup:\n got %v\nwant %v", got, want)
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
