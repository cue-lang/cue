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

package cue_test

import (
	"context"
	"testing"

	"cuelang.org/go/cue/stats"
	cue "cuelang.org/go/cue/v2"
	"github.com/go-quicktest/qt"
)

func TestLookupPathMissingFieldIsLazy(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "a: 1")

	// Constructing the lookup does not evaluate and reports no error.
	w := v.LookupPath(cue.ParsePath("nosuchfield"))

	// The error surfaces only when forced.
	err := w.Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*nosuchfield.*`))
	qt.Assert(t, qt.IsFalse(w.Exists(ctx)))

	// The base value is unaffected.
	qt.Assert(t, qt.IsNil(v.Err(ctx)))
	qt.Assert(t, qt.IsTrue(v.Exists(ctx)))
}

func TestLookupPath(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
a: b: c: 42
list: [10, 20, 30]
#Def: x: "hello"
"quoted-name": true
`)
	i, err := v.LookupPath(cue.ParsePath("a.b.c")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 42))

	i, err = v.LookupPath(cue.ParsePath("list[1]")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 20))

	s, err := v.LookupPath(cue.ParsePath("#Def.x")).AsString(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(s, "hello"))

	b, err := v.LookupPath(cue.MakePath(cue.Str("quoted-name"))).Bool(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(b))

	// An empty path returns the value itself.
	qt.Assert(t, qt.IsTrue(v.LookupPath(cue.Path{}).Exists(ctx)))
}

func TestUnify(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "a: int, b: 2")
	w := compileValue(t, "a: 1, c: 3")

	qt.Assert(t, qt.PanicMatches(func() {
		v.Unify(w).Err(ctx)
	}, ".*different loaders.*"))

	root := compileValue(t, `
x: {a: int, b: 2}
y: {a: 1, c: 3}
conflict: {a: 4}
`)
	u := root.LookupPath(cue.ParsePath("x")).Unify(root.LookupPath(cue.ParsePath("y")))
	var got struct{ A, B, C int }
	qt.Assert(t, qt.IsNil(u.Decode(ctx, &got)))
	qt.Assert(t, qt.Equals(got.A, 1))
	qt.Assert(t, qt.Equals(got.B, 2))
	qt.Assert(t, qt.Equals(got.C, 3))
}

func TestUnifyConflictSurfacesOnForce(t *testing.T) {
	ctx := context.Background()
	root := compileValue(t, `
x: a: 1
y: a: 2
`)
	// Constructing the conflicting unification is not an error.
	u := root.LookupPath(cue.ParsePath("x")).Unify(root.LookupPath(cue.ParsePath("y")))

	// The conflict is observed at the failing field when forced.
	err := u.LookupPath(cue.ParsePath("a")).Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*conflicting values.*`))

	// Validate sees the conflict from the root.
	qt.Assert(t, qt.IsNotNil(u.Validate(ctx)))
}

func TestFillPath(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "a: b: int, c: a.b+1")

	w := v.FillPath(cue.ParsePath("a.b"), 41)
	i, err := w.LookupPath(cue.ParsePath("c")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 42))

	// Fill with a Value from the same runtime.
	root := compileValue(t, "in: 10, out: {b: int}")
	w2 := root.LookupPath(cue.ParsePath("out")).
		FillPath(cue.ParsePath("b"), root.LookupPath(cue.ParsePath("in")))
	i, err = w2.LookupPath(cue.ParsePath("b")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 10))

	// Fill a list element.
	lv := compileValue(t, "l: [...int]")
	w3 := lv.FillPath(cue.ParsePath("l[0]"), 7)
	i, err = w3.LookupPath(cue.ParsePath("l[0]")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 7))

	// Fill with an invalid path reports an error on force.
	bad := v.FillPath(cue.ParsePath("a[x]"), 1)
	qt.Assert(t, qt.IsNotNil(bad.Err(ctx)))
}

func TestFillPathGoValues(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "x: _")
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	w := v.FillPath(cue.ParsePath("x"), point{X: 1, Y: 2})
	var got map[string]int
	qt.Assert(t, qt.IsNil(w.LookupPath(cue.ParsePath("x")).Decode(ctx, &got)))
	qt.Assert(t, qt.DeepEquals(got, map[string]int{"x": 1, "y": 2}))

	w = v.FillPath(cue.ParsePath("x"), []string{"p", "q"})
	var l []string
	qt.Assert(t, qt.IsNil(w.LookupPath(cue.ParsePath("x")).Decode(ctx, &l)))
	qt.Assert(t, qt.DeepEquals(l, []string{"p", "q"}))
}

func TestEvalAndDefault(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
x: y
y: 5
d: int | *3
`)
	// Eval resolves references; the result is still 5.
	ev := v.LookupPath(cue.ParsePath("x")).Eval()
	i, err := ev.Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 5))

	// Eval with options is not supported in this preview.
	qt.Assert(t, qt.IsNotNil(v.Eval(cue.Final()).Err(ctx)))

	// Default selects the default of a disjunction.
	d := v.LookupPath(cue.ParsePath("d"))
	_, err = d.Int64(ctx)
	qt.Assert(t, qt.IsNil(err)) // scalar accessors default implicitly
	qt.Assert(t, qt.Equals(d.Kind(ctx), cue.BottomKind))
	i, err = d.Default().Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 3))
	qt.Assert(t, qt.Equals(d.Default().Kind(ctx), cue.IntKind))
}

func TestErrExistsKind(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
good: 4
bad: 1 & 2
str: "hi"
open: int
st: {a: 1}
li: [1, 2]
by: 'bytes'
fl: 1.5
bo: true
nu: null
`)
	for _, tc := range []struct {
		path   string
		exists bool
		kind   cue.Kind
		incomp cue.Kind
	}{
		{"good", true, cue.IntKind, cue.IntKind},
		{"bad", false, cue.BottomKind, cue.BottomKind},
		{"str", true, cue.StringKind, cue.StringKind},
		{"open", true, cue.BottomKind, cue.IntKind},
		{"st", true, cue.StructKind, cue.StructKind},
		{"li", true, cue.ListKind, cue.ListKind},
		{"by", true, cue.BytesKind, cue.BytesKind},
		{"fl", true, cue.FloatKind, cue.FloatKind},
		{"bo", true, cue.BoolKind, cue.BoolKind},
		{"nu", true, cue.NullKind, cue.NullKind},
	} {
		w := v.LookupPath(cue.ParsePath(tc.path))
		qt.Check(t, qt.Equals(w.Exists(ctx), tc.exists), qt.Commentf("path %s", tc.path))
		qt.Check(t, qt.Equals(w.Kind(ctx), tc.kind), qt.Commentf("path %s", tc.path))
		qt.Check(t, qt.Equals(w.IncompleteKind(ctx), tc.incomp), qt.Commentf("path %s", tc.path))
		if tc.exists {
			qt.Check(t, qt.IsNil(w.Err(ctx)), qt.Commentf("path %s", tc.path))
		} else {
			qt.Check(t, qt.IsNotNil(w.Err(ctx)), qt.Commentf("path %s", tc.path))
		}
	}
}

func TestValidate(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
concrete: {a: 1, b: "x"}
incomplete: {a: int}
`)
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("concrete")).Validate(ctx)))
	qt.Assert(t, qt.IsNil(v.LookupPath(cue.ParsePath("concrete")).Validate(ctx, cue.Concrete(true))))

	inc := v.LookupPath(cue.ParsePath("incomplete"))
	qt.Assert(t, qt.IsNil(inc.Validate(ctx)))
	err := inc.Validate(ctx, cue.Concrete(true))
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorMatches(err, `.*incomplete value int.*`))

	// Options not supported by Validate are reported, not ignored.
	err = v.Validate(ctx, cue.Docs(true))
	qt.Assert(t, qt.ErrorMatches(err, `.*Docs not supported by Validate.*`))
}

func TestScalarAccessors(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
b: true
i: 42
f: 1.25
s: "hello"
by: 'abc'
def: int | *7
`)
	b, err := v.LookupPath(cue.ParsePath("b")).Bool(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(b))

	i, err := v.LookupPath(cue.ParsePath("i")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 42))

	f, err := v.LookupPath(cue.ParsePath("f")).Float64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(f, 1.25))

	s, err := v.LookupPath(cue.ParsePath("s")).AsString(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(s, "hello"))

	by, err := v.LookupPath(cue.ParsePath("by")).Bytes(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(by, []byte("abc")))

	// Accessors take the default implicitly.
	i, err = v.LookupPath(cue.ParsePath("def")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 7))

	// Kind mismatches report an error.
	_, err = v.LookupPath(cue.ParsePath("i")).Bool(ctx)
	qt.Assert(t, qt.ErrorMatches(err, `.*cannot use value 42 \(type int\) as bool.*`))
}

func TestFields(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `
a: 1
b: "two"
c?: int
#d: 4
_e: 5
`)
	collect := func(opts ...cue.Option) map[string]bool {
		m := map[string]bool{}
		for sel, w := range v.Fields(ctx, opts...) {
			m[sel.String()] = w.Exists(ctx)
		}
		return m
	}

	// By default only regular members are enumerated.
	qt.Assert(t, qt.DeepEquals(collect(), map[string]bool{
		"a": true, "b": true,
	}))

	// Definitions, hidden and optional fields can be included.
	got := collect(cue.Definitions(true), cue.Hidden(true), cue.Optional(true))
	qt.Assert(t, qt.DeepEquals(got, map[string]bool{
		"a": true, "b": true, "c": true, "#d": true, "_e": true,
	}))

	// Member values are decodable.
	for sel, w := range v.Fields(ctx) {
		if sel.Unquoted() == "b" {
			s, err := w.AsString(ctx)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(s, "two"))
		}
	}

	// A non-struct yields no fields.
	n := 0
	for range v.LookupPath(cue.ParsePath("a")).Fields(ctx) {
		n++
	}
	qt.Assert(t, qt.Equals(n, 0))

	// Unsupported options panic.
	qt.Assert(t, qt.PanicMatches(func() {
		v.Fields(ctx, cue.Docs(true))
	}, `.*Docs not supported by Fields.*`))
}

func TestItems(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, `l: ["a", "b", "c"], notList: 1`)

	var got []string
	for i, e := range v.LookupPath(cue.ParsePath("l")).Items(ctx) {
		s, err := e.AsString(ctx)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(i, len(got)))
		got = append(got, s)
	}
	qt.Assert(t, qt.DeepEquals(got, []string{"a", "b", "c"}))

	n := 0
	for range v.LookupPath(cue.ParsePath("notList")).Items(ctx) {
		n++
	}
	qt.Assert(t, qt.Equals(n, 0))
}

func TestErrorf(t *testing.T) {
	ctx := context.Background()
	e := cue.Errorf("it went %s", "wrong")

	err := e.Err(ctx)
	qt.Assert(t, qt.ErrorMatches(err, `it went wrong`))
	qt.Assert(t, qt.IsFalse(e.Exists(ctx)))
	qt.Assert(t, qt.Equals(e.Kind(ctx), cue.BottomKind))

	// An error value adopts the runtime of a value it is combined with,
	// and poisons the result when forced.
	v := compileValue(t, "a: 1")
	u := v.Unify(e)
	qt.Assert(t, qt.ErrorMatches(u.Err(ctx), `.*it went wrong.*`))

	// Filling an error value places the error at the path.
	w := v.FillPath(cue.ParsePath("b"), e)
	qt.Assert(t, qt.IsNil(w.LookupPath(cue.ParsePath("a")).Err(ctx)))
	qt.Assert(t, qt.ErrorMatches(w.LookupPath(cue.ParsePath("b")).Err(ctx), `.*it went wrong.*`))
}

func TestForcingIsMemoized(t *testing.T) {
	v := compileValue(t, "a: 1, b: a+1")

	r1 := &stats.Recorder{}
	err := v.Validate(stats.WithRecorder(context.Background(), r1))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(r1.Counts().Unifications > 0))

	// A second force of the same value is answered from the memoized
	// result: no further evaluation statistics accrue for it.
	r2 := &stats.Recorder{}
	_, err = v.LookupPath(cue.ParsePath("b")).Int64(stats.WithRecorder(context.Background(), r2))
	qt.Assert(t, qt.IsNil(err))

	r3 := &stats.Recorder{}
	err = v.Validate(stats.WithRecorder(context.Background(), r3))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(r3.Counts().Unifications, 0))
}

func TestCancellation(t *testing.T) {
	v := compileValue(t, "a: 1, b: a+1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := v.Err(ctx)
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.ErrorIs(err, context.Canceled))
	qt.Assert(t, qt.IsFalse(v.Exists(ctx)))

	_, err = v.LookupPath(cue.ParsePath("b")).Int64(ctx)
	qt.Assert(t, qt.ErrorIs(err, context.Canceled))

	// A canceled force is not memoized: with a live context the value
	// evaluates normally afterwards.
	i, err := v.LookupPath(cue.ParsePath("b")).Int64(context.Background())
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 2))
	qt.Assert(t, qt.IsNil(v.Err(context.Background())))
}

func TestValueString(t *testing.T) {
	v := compileValue(t, "a: b: 1")

	qt.Assert(t, qt.Equals(v.String(), "value"))
	w := v.LookupPath(cue.ParsePath("a.b")).Unify(cue.Errorf("boom"))
	qt.Assert(t, qt.Equals(w.String(), `unify(lookup(value, a.b), error("boom"))`))

	f := v.FillPath(cue.ParsePath("a.b"), 1)
	qt.Assert(t, qt.Equals(f.String(), "fill(value, a.b, 1)"))
	qt.Assert(t, qt.Equals(v.Eval().String(), "eval(value)"))
	qt.Assert(t, qt.Equals(v.Default().String(), "default(value)"))

	var zero cue.Value
	qt.Assert(t, qt.Equals(zero.String(), "<invalid>"))
}

func TestValuePath(t *testing.T) {
	ctx := context.Background()
	v := compileValue(t, "a: b: c: 1")

	w := v.LookupPath(cue.ParsePath("a.b"))
	qt.Assert(t, qt.Equals(w.Path().String(), "a.b"))
	qt.Assert(t, qt.Equals(w.LookupPath(cue.ParsePath("c")).Path().String(), "a.b.c"))

	// Fields report their vertex path.
	for _, f := range v.Fields(ctx) {
		qt.Assert(t, qt.Equals(f.Path().String(), "a"))
	}
}

func TestZeroValue(t *testing.T) {
	ctx := context.Background()
	var zero cue.Value

	qt.Assert(t, qt.IsFalse(zero.Exists(ctx)))
	qt.Assert(t, qt.ErrorMatches(zero.Err(ctx), `.*undefined value.*`))
	qt.Assert(t, qt.Equals(zero.Kind(ctx), cue.BottomKind))

	v := compileValue(t, "a: 1")
	// Unifying with the zero value is the identity.
	i, err := v.Unify(zero).LookupPath(cue.ParsePath("a")).Int64(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, 1))
}

func TestKindString(t *testing.T) {
	qt.Assert(t, qt.Equals(cue.BottomKind.String(), "_|_"))
	qt.Assert(t, qt.Equals(cue.IntKind.String(), "int"))
	qt.Assert(t, qt.Equals(cue.NumberKind.String(), "number"))
	qt.Assert(t, qt.Equals(cue.TopKind.String(), "_"))
	qt.Assert(t, qt.Equals((cue.StringKind|cue.BoolKind).String(), "(bool|string)"))
}
