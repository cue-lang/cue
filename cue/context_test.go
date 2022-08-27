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
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/internal/cuetxtar"
	"golang.org/x/tools/txtar"
)

func TestNewList(t *testing.T) {
	ctx := cuecontext.New()

	intList := ctx.CompileString("[...int]")

	l123 := ctx.NewList(
		ctx.Encode(1),
		ctx.Encode(2),
		ctx.Encode(3),
	)

	testCases := []struct {
		desc string
		v    cue.Value
		out  string
	}{{
		v:   ctx.NewList(),
		out: `[]`,
	}, {
		v:   l123,
		out: `[1, 2, 3]`,
	}, {
		v:   l123.Unify(intList),
		out: `[1, 2, 3]`,
	}, {
		v:   l123.Unify(intList).Unify(l123),
		out: `[1, 2, 3]`,
	}, {
		v:   intList.Unify(ctx.NewList(ctx.Encode("string"))),
		out: `_|_ // 0: conflicting values "string" and int (mismatched types string and int)`,
	}, {
		v:   ctx.NewList().Unify(l123),
		out: `_|_ // incompatible list lengths (0 and 3)`,
	}, {
		v: ctx.NewList(
			intList,
			intList,
		).Unify(ctx.NewList(
			ctx.NewList(
				ctx.Encode(1),
				ctx.Encode(3),
			),
			ctx.NewList(
				ctx.Encode(5),
				ctx.Encode(7),
			),
		)),
		out: `[[1, 3], [5, 7]]`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := fmt.Sprint(tc.v)
			if got != tc.out {
				t.Errorf(" got: %v\nwant: %v", got, tc.out)
			}
		})
	}
}

func TestBuildInstancesSuccess(t *testing.T) {
	in := `
-- foo.cue --
package foo

foo: [{a: "b", c: "d"}, {a: "e", g: "f"}]
bar: [
	for f in foo
	if (f & {c: "b"}) != _|_
	{f}
]
`

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	_, err := cuecontext.New().BuildInstances([]*build.Instance{instance})
	if err != nil {
		t.Fatalf("BuildInstances() = %v", err)
	}
}

func TestBuildInstancesError(t *testing.T) {
	in := `
-- foo.cue --
package foo

foo: [{a: "b", c: "d"}, {a: "e", g: "f"}]
bar: [
	for f in foo
	if f & {c: "b") != _|_   // NOTE: ')' instead of '}'
	{f}
]
`

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]

	// Normally, this should be checked, however, this is explicitly
	// testing the path where this was NOT checked.
	// if instance.Err != nil {
	// 	t.Fatal(instance.Err)
	// }

	vs, err := cuecontext.New().BuildInstances([]*build.Instance{instance})
	if err == nil {
		t.Fatalf("BuildInstances() = %#v, wanted error", vs)
	}
}

func TestAddCounts(t *testing.T) {
	config := `{
	a: 1
	b: 1 & a
	c: 3 | 4
}`

	archive := txtar.Parse([]byte("-- foo.cue --\n" + config))

	want := stats.Counts{
		Unifications: 4,
		Disjuncts:    6,
		Conjuncts:    9,
	}

	counts := &stats.Counts{}
	accrueOpt := cue.AccrueCounts(counts)

	c := cuecontext.New()

	testCases := []struct {
		name string
		f    func(t *testing.T)

		// Some operations may have a slight deviating count.
		adjust stats.Counts
	}{{
		name: "BuildExpr",
		f: func(t *testing.T) {
			x, err := parser.ParseExpr("", config)
			if err != nil {
				t.Fatal(err)
			}
			c.BuildExpr(x, accrueOpt)
		},
		adjust: stats.Counts{
			Conjuncts: 1, // BuildExpr uses one less conjunct.
		},
	}, {
		name: "BuildFile",
		f: func(t *testing.T) {
			x, _ := parser.ParseFile("", config)
			c.BuildFile(x, accrueOpt)
		},
	}, {
		name: "BuildInstance",
		f: func(t *testing.T) {
			// Don't reuse load as the result of computing the package may be
			// cached.
			instance := cuetxtar.Load(archive, t.TempDir())[0]
			if instance.Err != nil {
				t.Fatal(instance.Err)
			}
			c.BuildInstance(instance, accrueOpt)
		},
	}, {
		name: "BuildInstances",
		f: func(t *testing.T) {
			instance := cuetxtar.Load(archive, t.TempDir())[0]
			if instance.Err != nil {
				t.Fatal(instance.Err)
			}
			c.BuildInstances([]*build.Instance{instance}, accrueOpt)
		},
	}, {
		name: "CompileBytes",
		f:    func(t *testing.T) { c.CompileBytes([]byte(config), accrueOpt) },
	}, {
		name: "CompileString",
		f:    func(t *testing.T) { c.CompileString(config, accrueOpt) },
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			*counts = stats.Counts{}
			tc.f(t)
			got := stats.Counts{
				Unifications: counts.Unifications,
				Disjuncts:    counts.Disjuncts,
				Conjuncts:    counts.Conjuncts,
			}
			got.Add(tc.adjust)

			if got != want {
				t.Errorf("\ngot:\n%v;\n\nwant:\n%v", got, want)
			}
		})
	}
}
