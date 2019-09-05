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

import (
	"bytes"
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
)

func TestCompile(t *testing.T) {
	testCases := []struct {
		in  string
		out string
	}{{
		in: `{
		  foo: 1,
		}`,
		out: "<0>{}", // emitted value, but no top-level fields
	}, {
		in: `
		foo: 1
		`,
		out: "<0>{foo: 1}",
	}, {
		in: `
		a: true
		b: 2K
		c: 4_5
		d: "abc"
		e: 3e2 // 3h1m2ss
		`,
		out: "<0>{a: true, b: 2000, c: 45, d: \"abc\", e: 3e+2}",
	}, {
		in: `
		a: null
		b: true
		c: false
		`,
		out: "<0>{a: null, b: true, c: false}",
	}, {
		in: `
		a: <1
		b: >= 0 & <= 10
		c: != null
		d: >100
		`,
		out: `<0>{a: <1, b: (>=0 & <=10), c: !=null, d: >100}`,
	}, {
		in: "" +
			`a: "\(4)",
			b: "one \(a) two \(  a + c  )",
			c: "one"`,
		out: `<0>{a: ""+4+"", b: "one "+<0>.a+" two "+(<0>.a + <0>.c)+"", c: "one"}`,
	}, {
		in: "" +
			`a: """
				multi
				""",
			b: '''
				hello world
				goodbye globe
				welcome back planet
				'''`,
		out: `<0>{a: "multi", b: 'hello world\ngoodbye globe\nwelcome back planet'}`,
	}, {
		in: "" +
			`a: """
				multi \(4)
				""",
			b: """
				hello \("world")
				goodbye \("globe")
				welcome back \("planet")
				"""`,
		out: `<0>{a: "multi "+4+"", b: "hello "+"world"+"\ngoodbye "+"globe"+"\nwelcome back "+"planet"+""}`,
	}, {
		in: `
		a: _
		b: int
		c: float
		d: bool
		e: duration
		f: string
		`,
		out: "<0>{a: _, b: int, c: float, d: bool, e: duration, f: string}",
	}, {
		in: `
		a: null
		b: true
		c: false
		`,
		out: "<0>{a: null, b: true, c: false}",
	}, {
		in: `
		null: null
		true: true
		false: false
		`,
		out: "<0>{null: null, true: true, false: false}",
	}, {
		in: `
		a: 1 + 2
		b: -2 - 3
		c: !d
		d: true
		`,
		out: "<0>{a: (1 + 2), b: (-2 - 3), c: !<0>.d, d: true}",
	}, {
		in: `
			l0: 3*[int]
			l0: [1, 2, 3]
			l1: <=5*[string]
			l1: ["a", "b"]
			l2: (<=5)*[{ a: int }]
			l2: [{a: 1}, {a: 2, b: 3}]
			l3: (<=10)*[int]
			l3: [1, 2, 3, ...]
			l4: [1, 2, ...]
			l4: [...int]
			l5: [1, ...int]

			s1: ((<=6)*[int])[2:3]
			s2: [0,2,3][1:2]

			e0: (>=2 & <=5)*[{}]
			e0: [{}]
			`,
		out: `<0>{l0: ((3 * [int]) & [1,2,3]), l1: ((<=5 * [string]) & ["a","b"]), l2: ((<=5 * [<1>{a: int}]) & [<2>{a: 1},<3>{a: 2, b: 3}]), l3: ((<=10 * [int]) & [1,2,3, ...]), l4: ([1,2, ...] & [, ...int]), l5: [1, ...int], s1: (<=6 * [int])[2:3], s2: [0,2,3][1:2], e0: (((>=2 & <=5) * [<4>{}]) & [<5>{}])}`,
	}, {
		in: `
		a: 5 | "a" | true
		aa: 5 | *"a" | true
		b c: {
			cc: { ccc: 3 }
		}
		d: true
		`,
		out: "<0>{a: (5 | \"a\" | true), aa: (5 | *\"a\" | true), b: <1>{c: <2>{cc: <3>{ccc: 3}}}, d: true}",
	}, {
		in: `
		a a: { b: a } // referencing ancestor nodes is legal.
		a b: a.a      // do lookup before merging of nodes
		b: a.a        // different node as a.a.b, as first node counts
		c: a          // same node as b, as first node counts
		d: a["a"]
		`,
		out: `<0>{a: (<1>{a: <2>{b: <2>}} & <3>{b: <3>.a}), b: <0>.a.a, c: <0>.a, d: <0>.a["a"]}`,
	}, {
		// bunch of aliases
		in: `
		a1 = a2
		a2 = 5
		b: a1
		a3 = d
		c: {
			d: {
				r: a3
			}
			r: a3
		}
		d: { e: 4 }
		`,
		out: `<0>{b: 5, c: <1>{d: <2>{r: <0>.d}, r: <0>.d}, d: <3>{e: 4}}`,
	}, {
		// aliases with errors
		in: `
		e1 = 1
		e1 = 2
		e1v: e1
		e2: "a"
		e2 = "a"
		`,
		out: "cannot have two aliases with the same name in the same scope:\n" +
			"    test:3:3\n" +
			"cannot have alias and non-alias with the same name:\n" +
			"    test:6:3\n" +
			"<0>{}",
	}, {
		in: `
		a = b
		b: {
			c: a // reference to own root.
		}
		`,
		out: `<0>{b: <1>{c: <0>.b}}`,
	}, {
		in: `
		a: {
			<name>: { n: name }
			k: 1
		}
		b: {
			<x>: { x: 0, y: 1 }
			v: {}
		}
		`,
		out: `<0>{a: <1>{<>: <2>(name: string)-><3>{n: <2>.name}, k: 1}, b: <4>{<>: <5>(x: string)-><6>{x: 0, y: 1}, v: <7>{}}}`,
	}, {
		in: `
		a: {
			for k, v in b if b.a < k {
				"\(k)": v
			}
		}
		b: {
			a: 1
			b: 2
			c: 3
		}
		`,
		out: `<0>{a: <1>{ <2>for k, v in <0>.b if (<0>.b.a < <2>.k) yield (""+<2>.k+""): <2>.v}, b: <3>{a: 1, b: 2, c: 3}}`,
	}, {
		in: `
			a: { for k, v in b {"\(v)": v} }
			b: { a: "aa", b: "bb", c: "cc" }
			`,
		out: `<0>{a: <1>{ <2>for k, v in <0>.b yield (""+<2>.v+""): <2>.v}, b: <3>{a: "aa", b: "bb", c: "cc"}}`,
	}, {
		in: `
			a: [ v for _, v in b ]
			b: { a: 1, b: 2, c: 3 }
			`,
		out: `<0>{a: [ <1>for _, v in <0>.b yield (*nil*): <1>.v ], b: <2>{a: 1, b: 2, c: 3}}`,
	}, {
		in: `
			a: >=1 & <=2
			b: >=1 & >=2 & <=3
			c: >="a" & <"b"
			d: >(2+3) & <(4+5)
			`,
		out: `<0>{a: (>=1 & <=2), b: ((>=1 & >=2) & <=3), c: (>="a" & <"b"), d: (>(2 + 3) & <(4 + 5))}`,
	}, {
		in: `
			a: *1,
			b: **1 | 2
		`,
		out: `<0>{a: _|_(preference mark not allowed at this position), ` +
			`b: (*_|_(preference mark not allowed at this position) | 2)}`,
	}, {
		in: `
			a: int @foo(1,"str")
		`,
		out: "<0>{a: int @foo(1,\"str\")}",
	}, {
		in: `
			a: int @b('' ,b) // invalid
		`,
		out: "attribute missing ')':\n    test:2:16\nmissing ',' in struct literal:\n    test:3:3\n<0>{}",
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx, root, err := compileFileWithErrors(t, tc.in)
			buf := &bytes.Buffer{}
			if err != nil {
				errors.Print(buf, err, nil)
			}
			buf.WriteString(debugStr(ctx, root))
			got := buf.String()
			if got != tc.out {
				t.Errorf("output differs:\ngot  %q\nwant %q", got, tc.out)
			}
		})
	}
}

func TestEmit(t *testing.T) {
	testCases := []struct {
		in  string
		out string
		rw  rewriteMode
	}{{
		in: `"\(hello), \(world)!"` + `
		hello: "Hello"
		world: "World"
		`,
		out: `""+<0>.hello+", "+<0>.world+"!"`,
		rw:  evalRaw,
	}, {
		in: `"\(hello), \(world)!"` + `
		hello: "Hello"
		world: "World"
		`,
		out: `"Hello, World!"`,
		rw:  evalPartial,
	}, {
		// Ambiguous disjunction must cary over to emit value.
		in: `baz

		baz: {
			a: 8000 | 7080
			a: 7080 | int
		}`,
		out: `<0>{a: (8000 | 7080)}`,
		rw:  evalFull,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx, root := compileFile(t, tc.in)
			v := testResolve(ctx, root.emit, tc.rw)
			if got := debugStr(ctx, v); got != tc.out {
				t.Errorf("output differs:\ngot  %q\nwant %q", got, tc.out)
			}
		})
	}
}

func TestEval(t *testing.T) {
	testCases := []struct {
		in   string
		expr string
		out  string
	}{{
		in: `
			hello: "Hello"
			world: "World"
			`,
		expr: `"\(hello), \(world)!"`,
		out:  `"Hello, World!"`,
	}, {
		in: `
			a: { b: 2, c: 3 }
			z: 1
			`,
		expr: `a.b + a.c + z`,
		out:  `6`,
	}, {
		in: `
			a: { b: 2, c: 3 }
			`,
		expr: `{ d: a.b + a.c }`,
		out:  `<0>{d: 5}`,
	}, {
		in: `
			a: "Hello World!"
			`,
		expr: `strings.ToUpper(a)`,
		out:  `"HELLO WORLD!"`,
	}, {
		in: `
			a: 0x8
			b: 0x1`,
		expr: `bits.Or(a, b)`, // package shorthand
		out:  `9`,
	}, {
		in: `
			a: 0x8
			b: 0x1`,
		expr: `math.Or(a, b)`,
		out:  `_|_(<0>.Or:undefined field "Or")`,
	}, {
		in:   `a: 0x8`,
		expr: `mathematics.Abs(a)`,
		out:  `_|_(reference "mathematics" not found)`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx, inst, errs := compileInstance(t, tc.in)
			if errs != nil {
				t.Fatal(errs)
			}
			expr, err := parser.ParseExpr("<test>", tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			evaluated := inst.evalExpr(ctx, expr)
			v := testResolve(ctx, evaluated, evalFull)
			if got := debugStr(ctx, v); got != tc.out {
				t.Errorf("output differs:\ngot  %q\nwant %q", got, tc.out)
			}
		})
	}
}

func TestResolution(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		err  string
	}{{
		name: "package name identifier should not resolve to anything",
		in: `package time

		import "time"

		a: time.Time
		`,
	}, {
		name: "duplicate_imports.cue",
		in: `
		import "time"
		import time "math"

		t: time.Time
		`,
		err: "time redeclared as imported package name",
	}, {
		name: "unused_import",
		in: `
			import "time"
			`,
		err: `imported and not used: "time"`,
	}, {
		name: "nonexisting import package",
		in:   `import "doesnotexist"`,
		err:  `package "doesnotexist" not found`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var r Runtime
			_, err := r.Compile(tc.name, tc.in)
			got := err == nil
			want := tc.err == ""
			if got != want {
				t.Fatalf("got %v; want %v", err, tc.err)
			}
			if err != nil {
				if s := err.Error(); !strings.Contains(s, tc.err) {
					t.Errorf("got %v; want %v", err, tc.err)
				}
			}
		})
	}
}
