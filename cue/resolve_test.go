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
	"flag"
	"fmt"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

var traceOn = flag.Bool("debug", false, "enable tracing")

func compileFileWithErrors(t *testing.T, body string) (*context, *structLit, errors.List) {
	t.Helper()
	ctx, inst, errs := compileInstance(t, body)
	return ctx, inst.rootValue.evalPartial(ctx).(*structLit), errs
}

func compileFile(t *testing.T, body string) (*context, *structLit) {
	t.Helper()
	ctx, inst, errs := compileInstance(t, body)
	if errs != nil {
		t.Fatal(errs)
	}
	return ctx, inst.rootValue.evalPartial(ctx).(*structLit)
}

func compileInstance(t *testing.T, body string) (*context, *Instance, errors.List) {
	t.Helper()

	fset := token.NewFileSet()
	x := newIndex(fset).NewInstance(nil)
	f, err := parser.ParseFile(fset, "test", body, parser.ParseLambdas)
	ctx := x.newContext()

	switch errs := err.(type) {
	case nil:
		x.insertFile(f)
	case errors.List:
		return ctx, x, errs
	default:
		t.Fatal(err)
	}
	return ctx, x, nil
}

func rewriteHelper(t *testing.T, cases []testCase, r rewriteMode) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Helper()
			ctx, obj := compileFile(t, tc.in)
			ctx.trace = *traceOn
			root := testResolve(ctx, obj, r)

			got := debugStr(ctx, root)
			if v := ctx.processDelayedConstraints(); v != nil {
				got += fmt.Sprintf("\n%s", debugStr(ctx, v))
			}

			// Copy the result
			if got != tc.out {
				fn := t.Errorf
				if tc.skip {
					fn = t.Skipf
				}
				fn("output differs:\ngot  %s\nwant %s", got, tc.out)
			}
		})
	}
}

type testCase struct {
	desc string
	in   string
	out  string
	skip bool
}

func TestBasicRewrite(t *testing.T) {
	testCases := []testCase{{
		desc: "errors",
		in: `
			a: _|_ & _|_
			b: null & _|_
			c: b.a == _|_
			d: _|_ != b.a
			e: _|_ == _|_
			`,
		out: `<0>{a: _|_(from source), b: _|_(from source), c: true, d: false, e: true}`,
	}, {
		desc: "arithmetic",
		in: `
			sum: -1 + +2        // 1
			str: "foo" + "bar"  // "foobar"
			div1: 2.0 / 3 * 6   // 4
			div2: 2 / 3 * 6     // 4
			rem: 2 % 3          // 2
			e: 2 + "a"          // _|_: unsupported op +(int, string))
			b: 1 != 4
			`,
		out: `<0>{sum: 1, str: "foobar", div1: 4.00000000000000000000000, div2: 4.00000000000000000000000, rem: 2, e: _|_((2 + "a"):unsupported op +(number, string)), b: true}`,
	}, {
		desc: "integer-specific arithmetic",
		in: `
			q1: 5 quo 2    // 2
			q2: 5 quo -2   // -2
			q3: -5 quo 2   // -2
			q4: -5 quo -2  // 2
			qe1: 2.0 quo 1
			qe2: 2 quo 1.0

			r1: 5 rem 2    // 1
			r2: 5 rem -2   // 1
			r3: -5 rem 2   // -1
			r4: -5 rem -2  // -1
			re1: 2.0 rem 1
			re2: 2 rem 1.0

			d1: 5 div 2    // 2
			d2: 5 div -2   // -2
			d3: -5 div 2   // -3
			d4: -5 div -2  // 3
			de1: 2.0 div 1
			de2: 2 div 1.0

			m1: 5 mod 2    // 1
			m2: 5 mod -2   // 1
			m3: -5 mod 2   // 1
			m4: -5 mod -2  // 1
			me1: 2.0 mod 1
			me2: 2 mod 1.0

			// TODO: handle divide by zero
			`,
		out: `<0>{q1: 2, q2: -2, q3: -2, q4: 2, ` +
			`qe1: _|_((2.0 quo 1):unsupported op quo(float, number)), ` +
			`qe2: _|_((2 quo 1.0):unsupported op quo(number, float)), ` +
			`r1: 1, r2: 1, r3: -1, r4: -1, re1: ` +
			`_|_((2.0 rem 1):unsupported op rem(float, number)), ` +
			`re2: _|_((2 rem 1.0):unsupported op rem(number, float)), ` +
			`d1: 2, d2: -2, d3: -3, d4: 3, ` +
			`de1: _|_((2.0 div 1):unsupported op div(float, number)), ` +
			`de2: _|_((2 div 1.0):unsupported op div(number, float)), ` +
			`m1: 1, m2: 1, m3: 1, m4: 1, ` +
			`me1: _|_((2.0 mod 1):unsupported op mod(float, number)), ` +
			`me2: _|_((2 mod 1.0):unsupported op mod(number, float))}`,
	}, {
		desc: "booleans",
		in: `
			t: true
			t: !false
			f: false
			f: !t
			e: true
			e: !true
			`,
		out: "<0>{t: true, f: false, e: _|_(true:failed to unify: true != false)}",
	}, {
		desc: "boolean arithmetic",
		in: `
			a: true && true
			b: true || false
			c: false == true
			d: false != true
			e: true & true
			f: true & false
			`,
		out: "<0>{a: true, b: true, c: false, d: true, e: true, f: _|_(true:failed to unify: true != false)}",
	}, {
		desc: "basic type",
		in: `
			a: 1 & int
			b: number & 1
			c: 1.0
			c: float
			d: int & float // _|_
			e: "4" & string
			f: true
			f: bool
			`,
		out: `<0>{a: 1, b: 1, c: 1.0, d: _|_((int & float):unsupported op &((int)*, (float)*)), e: "4", f: true}`,
	}, {
		desc: "escaping",

		in: `
			a: "foo\nbar",
			b: a,

			// TODO: mimic http://exploringjs.com/es6/ch_template-literals.html#sec_introduction-template-literals
		`,
		out: `<0>{a: "foo\nbar", b: "foo\nbar"}`,
		// out: `<0>{a: "foo\nbar", b: <0>.a}`,
	}, {
		desc: "reference",
		in: `
			a: b
			b: 2
			d: {
				d: 3
				e: d
			}
			e: {
				e: {
					v: 1
				}
				f: {
					v: e.v
				}
			}
			`,
		out: "<0>{a: 2, b: 2, d: <1>{d: 3, e: 3}, e: <2>{e: <3>{v: 1}, f: <4>{v: 1}}}",
	}, {
		desc: "lists",
		in: `
			list: [1,2,3]
			index: [1,2,3][1]
			unify: [1,2,3] & [_,2,3]
			e: [] & 4
			e2: [3]["d"]
			e3: [3][-1]
			e4: [1, 2, ...4..5] & [1, 2, 4, 8]
			e5: [1, 2, 4, 8] & [1, 2, ...4..5]
			`,
		out: `<0>{list: [1,2,3], index: 2, unify: [1,2,3], e: _|_(([] & 4):unsupported op &(list, number)), e2: _|_("d":invalid list index "d" (type string)), e3: _|_(-1:invalid list index -1 (index must be non-negative)), e4: _|_(((4..5) & 8):value 8 not in range (4..5)), e5: _|_(((4..5) & 8):value 8 not in range (4..5))}`,
	}, {
		desc: "selecting",
		in: `
			obj: {a: 1, b: 2}
			index: {a: 1, b: 2}["b"]
			mulidx: {a: 1, b: {a:1, b: 3}}["b"]["b"]
			e: {a: 1}[4]
			f: {a: 1}.b
			g: {a: 1}["b"]
			h: [3].b
			`,
		out: `<0>{obj: <1>{a: 1, b: 2}, index: 2, mulidx: 3, e: _|_(4:invalid struct index 4 (type number)), f: _|_(<2>{a: 1}.b:undefined field "b"), g: _|_(<3>{a: 1}["b"]:undefined field "b"), h: _|_([3]:invalid operation: [3].b (type list does not support selection))}`,
	}, {
		desc: "obj unify",
		in: `
			o1: {a: 1 } & { b: 2}      // {a:1,b:2}
			o2: {a: 1, b:2 } & { b: 2} // {a:1,b:2}
			o3: {a: 1 } & { a:1, b: 2} // {a:1,b:2}
			o4: {a: 1 } & { b: 2}      // {a:1,b:2}
			o4: {a: 1, b:2 } & { b: 2}
			o4: {a: 1 } & { a:1, b: 2}
			e: 1                       // 1 & {a:3}
			e: {a:3}
			`,
		out: "<0>{o1: <1>{a: 1, b: 2}, o2: <2>{a: 1, b: 2}, o3: <3>{a: 1, b: 2}, o4: <4>{a: 1, b: 2}, e: _|_((1 & <5>{a: 3}):unsupported op &(number, struct))}",
	}, {
		desc: "disjunctions",
		in: `
			o1: 1 | 2 | 3
			o2: (1 | 2 | 3) & 1
			o3: 2 & (1 | 2 | 3)
			o4: (1 | 2 | 3) & (1 | 2 | 3)
			o5: (1 | 2 | 3) & (3 | 2 | 1)
			o6: (1 | 2 | 3) & (3 | 1 | 2)
			o7: (1 | 2 | 3) & (2 | 3)
			o8: (1 | 2 | 3) & (3 | 2)
			o9: (2 | 3) & (1 | 2 | 3)
			o10: (3 | 2) & (1 | 2 | 3)

			// All errors are treated the same as per the unification model.
			i1: [1, 2][3] | "c"
			`,
		out: `<0>{o1: (1 | 2 | 3), o2: 1, o3: 2, o4: (1 | 2 | 3), o5: (1! | 2! | 3!), o6: (1! | 2! | 3!), o7: (2 | 3), o8: (2! | 3!), o9: (2 | 3), o10: (3! | 2!), i1: "c"}`,
	}, {
		desc: "lambda",
		in: `
			o1(A:1, B:2) -> { a: A, b: B }
			oe() -> { a: 1, b: 2 }
			l1: (A:1, B:2) -> { a: A, b: B }
			c1: ((A:int, B:int) -> {a:A, b:B})(1, 2)
			`,
		// TODO(P1): don't let values refer to themselves.
		out: "<0>{o1: <1>(A: 1, B: 2)-><2>{a: <1>.A, b: <1>.B}, oe: <3>()-><4>{a: 1, b: 2}, l1: <5>(A: 1, B: 2)-><6>{a: <5>.A, b: <5>.B}, c1: <7>{a: 1, b: 2}}",
	}, {
		desc: "types",
		in: `
			i: int
			j: int & 3
			s: string
			t: "s" & string
			e: int & string
			e2: 1 & string
			b: !int
			p: +true
			m: -false
		`,
		out: `<0>{i: int, j: 3, s: string, t: "s", e: _|_((int & string):unsupported op &((int)*, (string)*)), e2: _|_((1 & string):unsupported op &(number, (string)*)), b: _|_(!int:unary '!' requires bool value, found (int)*), p: _|_(+true:unary '+' requires numeric value, found bool), m: _|_(-false:unary '-' requires numeric value, found bool)}`,
	}, {
		desc: "comparisson",
		in: `
			lss: 1 < 2
			leq: 1 <= 1.0
			leq: 2.0 <= 3
			eql: 1 == 1.0
			neq: 1.0 == 1
			gtr: !(2 > 3)
			geq: 2.0 >= 2
			seq: "a" + "b" == "ab"
			err: 2 == "s"
		`,
		out: `<0>{lss: true, leq: true, eql: true, neq: true, gtr: true, geq: true, seq: true, err: _|_((2 == "s"):unsupported op ==(number, string))}`,
	}, {
		desc: "null",
		in: `
			eql: null == null
			neq: null != null
			unf: null & null

			// errors
			eqe1: null == 1
			eqe2: 1 == null
			nee1: "s" != null
			call: null()
		`,
		out: `<0>{eql: true, neq: false, unf: null, eqe1: _|_((null == 1):unsupported op ==(null, number)), eqe2: _|_((1 == null):unsupported op ==(number, null)), nee1: _|_(("s" != null):unsupported op !=(string, null)), call: _|_(null:cannot call non-function null (type null))}`,
	}, {
		desc: "self-reference cycles",
		in: `
			a: b - 100
			b: a + 100

			c: [c[1], c[0]]
		`,
		out: `<0>{a: _|_(cycle detected), b: _|_(cycle detected), c: _|_(cycle detected)}`,
		// }, {
		// 	desc: "resolved self-reference cycles",
		// 	in: `
		// 		a: b - 100
		// 		b: a + 100
		// 		b: 200

		// 		c: [c[1], a] // TODO: should be allowed

		// 		s1: s2 & {a: 1}
		// 		s2: s3 & {b: 2}
		// 		s3: s1 & {c: 3}
		// 	`,
		// 	out: `<0>{a: 100, b: 200, c: _|_(cycle detected)}`,
	}, {
		desc: "delayed constraint failure",
		in: `
			a: b - 100
			b: a + 110
			b: 200
		`,
		out: `<0>{a: 100, b: 200}
_|_(((<1>.a + 110) & 200):constraint violated: _|_((210 & 200):cannot unify numbers 210 and 200))`,
		// TODO: find a way to mark error in data.
	}}
	rewriteHelper(t, testCases, evalPartial)
}

func TestChooseFirst(t *testing.T) {
	testCases := []testCase{{
		desc: "pick first",
		in: `
		a: 5 | "a" | true
		b c: {
			a: 2
		} | {
			a : 3
		}
		`,
		out: "<0>{a: 5, b: <1>{c: <2>{a: 2}}}",
	}, {
		desc: "simple disambiguation conflict",
		in: `
			a: "a" | "b"
			b: "b" | "a"
			c: a & b
			`,
		out: `<0>{a: "a", b: "b", c: _|_(("a"! | "b"!):ambiguous disjunction)}`,
	}, {
		desc: "disambiguation non-conflict",
		in: `
			a: "a" | ("b" | "c")
			b: ("a" | "b") | "c"
			c: a & b
			`,
		out: `<0>{a: "a", b: "a", c: "a"}`,
	}}
	rewriteHelper(t, testCases, evalFull)
}

func TestResolve(t *testing.T) {
	testCases := []testCase{{
		in: `
			a: b.c.d
			b c: { d: 3 }
			c: { c: d.d, }
			d: { d: 2 }
			`,
		out: "<0>{a: 3, b: <1>{c: <2>{d: 3}}, c: <3>{c: 2}, d: <4>{d: 2}}",
	}, {
		in: `
			a: _
			b: a
			a: { d: 1, d: _ }
			b: _
			`,
		out: `<0>{a: <1>{d: 1}, b: <2>{d: 1}}`,
	}, {
		desc: "JSON",
		in: `
			"a": 3
			b: a
			o: { "a\nb": 2 } // TODO: use $ for root?
			c: o["a\nb"]
		`,
		out: `<0>{a: 3, b: 3, o: <1>{"a\nb": 2}, c: 2}`,
	}, {
		desc: "arithmetic",
		in: `
				v1: 1.0T/2.0  //
				v2: 2.0 == 2
				i1: 1
				v5: 2.0 / i1  // TODO: should probably fail
				e1: 2.0 % 3
				e2: int & 4.0/2.0
				`,
		out: `<0>{v1: 5e+11, v2: true, i1: 1, v5: 2, e1: _|_((2.0 % 3):unsupported op %(float, number)), e2: _|_((int & 2):unsupported op &((int)*, float))}`,
		// }, {
		// 	desc: "null coalescing",
		// 	in: `
		// 		a: null
		// 		b: a.x
		// 		c: a["x"]
		// 	`,
		// 	out: ``,
	}, {
		desc: "call",
		in: `
			a: { a: (P, Q) -> {p:P, q:Q} }
			b: a // reference different nodes
			c: a.a(1, 2)
			`,
		out: "<0>{a: <1>{a: <2>(P: _, Q: _)-><3>{p: <2>.P, q: <2>.Q}}, b: <4>{a: <2>(P: _, Q: _)-><3>{p: <2>.P, q: <2>.Q}}, c: <5>{p: 1, q: 2}}",
	}, {
		desc: "call of lambda",
		in: `
			a(P, Q) -> {p:P, q:Q}
			a(P, Q) -> {p:P, q:Q}
			ai(P, Q) -> {p:Q, q:P}
			b: a(1,2)
			c: (a | b)(1)
			d: ([] | (a) -> 3)(2)
			e1: a(1)
			`,
		out: "<1>{a: <2>(P: _, Q: _)->(<3>{p: <2>.P, q: <2>.Q} & <4>{p: <2>.P, q: <2>.Q}), ai: <5>(P: _, Q: _)-><6>{p: <5>.Q, q: <5>.P}, b: <7>{p: 1, q: 2}, c: _|_((<8>.a | <8>.b) (1):number of arguments does not match (2 vs 1)), d: _|_(([] | <0>(a: _)->3):cannot call non-function [] (type list)), e1: _|_(<8>.a (1):number of arguments does not match (2 vs 1))}",
	}, {
		desc: "reference across tuples and back",
		// Tests that it is okay to partially evaluate structs.
		in: `
			a: { c: b.e, d: b.f }
			b: { e: 3, f: a.c }
			`,
		out: "<0>{a: <1>{c: 3, d: 3}, b: <2>{e: 3, f: 3}}",
	}, {
		desc: "index",
		in: `
			a: [2][0]
			b: {foo:"bar"}["foo"]
			c: (l|{"3":3})["3"]
			d: ([]|[1])[0]
			l: []
			e1: [2][""]
			e2: 2[2]
			e3: [][true]
			e4: [1,2,3][3]
			e5: [1,2,3][-1]
			e6: ([]|{})[1]
		`,
		out: `<0>{a: 2, b: "bar", c: _|_("3":invalid list index "3" (type string)), l: [], d: _|_([]:index 0 out of bounds), e1: _|_("":invalid list index "" (type string)), e2: _|_(2:invalid operation: 2[2] (type number does not support indexing)), e3: _|_(true:invalid list index true (type bool)), e4: _|_([1,2,3]:index 3 out of bounds), e5: _|_(-1:invalid list index -1 (index must be non-negative)), e6: _|_([]:index 1 out of bounds)}`,
	}, {
		desc: "string index",
		in: `
			a0: "abc"[0]
			a1: "abc"[1]
			a2: "abc"[2]
			a3: "abc"[3]
			a4: "abc"[-1]

			b: "zoëven"[2]
		`,
		out: `<0>{a0: "a", a1: "b", a2: "c", a3: _|_("abc":index 3 out of bounds), a4: _|_(-1:invalid string index -1 (index must be non-negative)), b: "ë"}`,
	}, {
		desc: "disjunctions of lists",
		in: `
			l: [ int, int ] | [ string, string ]

			l1: [ "a", "b" ]
			l2: l & [ "c", "d" ]
			`,
		out: `<0>{l: ([int,int] | [string,string]), l1: ["a","b"], l2: ["c","d"]}`,
	}, {
		desc: "slice",
		in: `
			a: [2][0:0]
			b: [0][1:1]
			e1: [][1:1]
			e2: [0][-1:0]
			e3: [0][1:0]
			e4: [0][1:2]
			e5: 4[1:2]
			e6: [2]["":]
			e7: [2][:"9"]

		`,
		out: `<0>{a: [], b: [], e1: _|_(1:slice bounds out of range), e2: _|_([0]:negative slice index), e3: _|_([0]:invalid slice index: 1 > 0), e4: _|_(2:slice bounds out of range), e5: _|_(4:cannot slice 4 (type number)), e6: _|_("":invalid slice index "" (type string)), e7: _|_("9":invalid slice index "9" (type string))}`,
	}, {
		desc: "string slice",
		in: `
			a0: ""[0:0]
			a1: ""[:]
			a2: ""[0:]
			a3: ""[:0]
			b0: "abc"[0:0]
			b1: "abc"[0:1]
			b2: "abc"[0:2]
			b3: "abc"[0:3]
			b4: "abc"[3:3]
			b5: "abc"[1:]
			b6: "abc"[:2]

			// TODO: supported extended graphemes, instead of just runes.
			u: "Spaß"[3:4]
		`,
		out: `<0>{a0: "", a1: "", a2: "", a3: "", b0: "", b1: "a", b2: "ab", b3: "abc", b4: "", b5: "bc", b6: "ab", u: "ß"}`,
	}, {
		desc: "list types",
		in: `
			l0: 3*[int]
			l0: [1, 2, 3]
			l1:(0..5)*[string]
			l1: ["a", "b"]
			l2: (0..5)*[{ a: int }]
			l2: [{a: 1}, {a: 2, b: 3}]
			l3: (0..10)*[int]
			l3: [1, 2, 3, ...]

			s1: ((0..6)*[int])[2:3] // TODO: simplify 1*[int] to [int]
			s2: [0,2,3][1:2]

			i1: ((0..6)*[int])[2]
			i2: [0,2,3][2]

			t0: [...{a: 8}]
			t0: [{}]

			e0: (2..5)*[{}]
			e0: [{}]

			e1: 0.._*[...int]
			`,
		out: `<0>{l0: [1,2,3], l1: ["a","b"], l2: [<1>{a: 1},<2>{a: 2, b: 3}], l3: (3..10)*[int]([1,2,3, ...int]), s1: 1*[int], s2: [2], i1: int, i2: 3, t0: [<3>{a: 8}], e0: _|_(((2..5)*[<4>{}] & [<5>{}]):incompatible list lengths: value 1 not in range (2..5)), e1: [, ...int]}`,
	}, {
		desc: "list arithmetic",
		in: `
			l0: 3*[1, 2, 3]
			l1: 0*[1, 2, 3]
			l2: 10*[]
			l3: (0..2)*[]
			l4: (0..2)*[int]
			l5: (0..2)*(int*[int])
			l6: 3*((3..4)*[int])
		`,
		out: `<0>{l0: [1,2,3,1,2,3,1,2,3], l1: [], l2: [], l3: [], l4: (0..2)*[int], l5: (0..2)*[int], l6: (9..12)*[int]}`,
	}, {
		desc: "correct error messages",
		// Tests that it is okay to partially evaluate structs.
		in: `
			a: "a" & 1
			`,
		out: `<0>{a: _|_(("a" & 1):unsupported op &(string, number))}`,
	}, {
		desc: "structs",
		in: `
			a: t & { c: 5 }             // {c:5,d:15}
			b: ti & { c: 7 }            // {c:7,d:21}
			t: { c: number, d: c * 3 }  // {c:number,d:number*3}
			ti: t & { c: int }
			`,
		out: `<0>{a: <1>{c: 5, d: 15}, t: <2>{c: number, d: (<3>.c * 3)}, b: <4>{c: 7, d: 21}, ti: <5>{c: int, d: (<6>.c * 3)}}`,
	}, {
		desc: "reference to root",
		in: `
			a: { b: int }
			c: a & {
				b: 100
				d: a.b + 3 // do not resolve as c != a.

				// TODO(crash)
				// e: int; e < 100 // where clause can be different.
			}
			x: {
				b: int
				c: b + 5
			}
			y: x & {
				b: 100
				// c should resolve to 105
			}
			v: {
				b: int
				c: v.b + 5 // reference starting from copied node.
			}
			w: v & { b: 100 }
			wp: v & { b: 100 }
			`,
		out: `<0>{a: <1>{b: int}, c: <2>{b: 100, d: (<3>.a.b + 3)}, x: <4>{b: int, c: (<5>.b + 5)}, y: <6>{b: 100, c: 105}, v: <7>{b: int, c: (<8>.b + 5)}, w: <9>{b: 100, c: 105}, wp: <10>{b: 100, c: 105}}`,
	}, {
		desc: "references from template to concrete",
		in: `
			res: [t]
			t <X>: {
				a: c + b.str
				b str: string
				c: "X"
			}
			t x: { b str: "DDDD" }
			`,
		out: `<0>{res: [<1>{<>: <2>(X: string)-><3>{a: (<3>.c + <3>.b.str), c: "X", b: <4>{str: string}}, x: <5>{a: "XDDDD", c: "X", b: <6>{str: "DDDD"}}}], t: <7>{<>: <2>(X: string)-><3>{a: (<3>.c + <3>.b.str), c: "X", b: <4>{str: string}}, x: <8>{a: "XDDDD", c: "X", b: <9>{str: "DDDD"}}}}`,
	}, {
		desc: "interpolation",
		in: `
			a: "\(4)"
			b: "one \(a) two \(  a + c  )"
			c: "one"
			d: "\(r)"
			u: "\(_)"
			r: _
			e: "\([])"`,
		out: `<0>{a: "4", b: "one 4 two 4one", c: "one", d: ""+<1>.r+"", r: _, u: ""+_+"", e: _|_([]:expression in interpolation must evaluate to a number kind or string (found list))}`,
	}, {
		desc: "diamond-shaped constraints",
		in: `
		S: {
			A: {
				a: 1,
			},
			B: A & {
				b: 2,
			}
		},
		T: S & { // S == { A: { a:1 }, B: { a:1, b:2 } }
			A: {
				c: 3,
			},
			B: { // S.B & A
				d: 4, // Combines constraints S.A, S.B, T.A, and T.B
			}
		}`,
		out: "<0>{S: <1>{A: <2>{a: 1}, B: <3>{a: 1, b: 2}}, T: <4>{A: <5>{a: 1, c: 3}, B: <6>{a: 1, b: 2, c: 3, d: 4}}}",
	}, {
		desc: "field templates",
		in: `
			a: {
				<name>: int
				k: 1
			}
			b: {
				<x>: { x: 0, y: 1 | int }
				v: {}
				w: { x: 0 }
			}
			b: { <y>: {} } // TODO: allow different name
			c: {
				<Name>: { name: Name, y: 1 }
				foo: {}
				bar: _
			}
			`,
		out: `<0>{a: <1>{<>: <2>(name: string)->int, k: 1}, b: <3>{<>: <4>(x: string)->(<5>{x: 0, y: (1 | int)} & <6>{}), v: <7>{x: 0, y: (1 | int)}, w: <8>{x: 0, y: (1 | int)}}, c: <9>{<>: <10>(Name: string)-><11>{name: <10>.Name, y: 1}, foo: <12>{name: "foo", y: 1}, bar: <13>{name: "bar", y: 1}}}`,
	}, {
		desc: "simple ranges",
		in: `
			a: 1..2
			c: "a".."b"
			d: (2+3)..(4+5)  // 5..9

			s1: 1..1       // 1
			s2: 1..2..3    // simplify (1..2)..3 to 1..3
			s3: (1..10)..5 // This is okay!
			s4: 5..(1..10) // This is okay!
			s5: (0..(5..6))..(1..10)
			`,
		out: `<0>{a: (1..2), c: ("a".."b"), d: (5..9), s1: 1, s2: (1..3), s3: (1..5), s4: (5..10), s5: (0..10)}`,
	}, {
		desc: "range unification",
		in: `
			// with concrete values
			a1: 1..5 & 3
			a2: 1..5 & 1
			a3: 1..5 & 5
			a4: 1..5 & 6
			a5: 1..5 & 0

			a6: 3 & 1..5
			a7: 1 & 1..5
			a8: 5 & 1..5
			a9: 6 & 1..5
			a10: 0 & 1..5

			// with ranges
			b1: 1..5 & 1..5
			b2: 1..5 & 1..1
			b3: 1..5 & 5..5
			b4: 1..5 & 2..3
			b5: 1..5 & 3..9
			b6: 1..5 & 5..9
			b7: 1..5 & 6..9

			b8: 1..5 & 1..5
			b9: 1..1 & 1..5
			b10: 5..5 & 1..5
			b11: 2..3 & 1..5
			b12: 3..9 & 1..5
			b13: 5..9 & 1..5
			b14: 6..9 & 1..5

			// ranges with more general types
			c1: int & 1..5
			c2: 1..5 & int
			c3: string & 1..5
			c4: 1..5 & string

			// other types
			s1: "d" .. "z" & "e"
			s2: "d" .. "z" & "ee"

			n1: number & 1..2
			n2: int & 1.1 .. 1.3
			n3: 1.0..3.0 & 2
			n4: 0.0..0.1 & 0.09999
			n5: 1..5 & 2.5
			`,
		out: `<0>{a1: 3, a2: 1, a3: 5, a4: _|_(((1..5) & 6):value 6 not in range (1..5)), a5: _|_(((1..5) & 0):value 0 not in range (1..5)), a6: 3, a7: 1, a8: 5, a9: _|_(((1..5) & 6):value 6 not in range (1..5)), a10: _|_(((1..5) & 0):value 0 not in range (1..5)), b1: (1..5), b2: 1, b3: 5, b4: (2..3), b5: (3..5), b6: 5, b7: _|_(((1..5) & (6..9)):non-overlapping ranges (1..5) and (6..9)), b8: (1..5), b9: 1, b10: 5, b11: (2..3), b12: (3..5), b13: 5, b14: _|_(((6..9) & (1..5)):non-overlapping ranges (6..9) and (1..5)), c1: (1..5), c2: (1..5), c3: _|_((string & (1..5)):unsupported op &((string)*, (number)*)), c4: _|_(((1..5) & string):unsupported op &((number)*, (string)*)), s1: "e", s2: "ee", n1: (1..2), n2: _|_((int & (1.1..1.3)):unsupported op &((int)*, (float)*)), n3: 2, n4: 0.09999, n5: 2.5}`,
	}, {
		desc: "range arithmetic",
		in: `
			r0: (1..2) * (4..5)
			r1: (1..2) * (-1..2)
			r2: (1.0..2.0) * (-0.5..1.0)
			r3: (1..2) + (4..5)

			i0: (1..2) * 2
			i1: (2..3) * -2
			i2: (1..2) * 2
			i3: (2..3) * -2

			t0: int * (1..2) // TODO: should be int
			t1: (1..2) * int
			t2: (1..2) * (0..int)
			t3: (1..int) * (0..2)
			t4: (1..int) * (-1..2)
			t5: _ * (1..2)  // TODO: should be int

			s0: (1..2) - (3..5)
			s1: (1..2) - 1

			str0: ("ab".."cd") + "ef"
			str1: ("ab".."cd") + ("ef".."gh")
			str2: ("ab".."cd") + string

		`,
		out: `<0>{r0: (4..10), r1: (-2..4), r2: (-1.00..2.00), r3: (5..7), i0: (2..4), i1: (-6..-4), i2: (2..4), i3: (-6..-4), t0: (int * (1..2)), t1: int, t2: (0..int), t3: (0..int), t4: int, t5: _|_((_ * (1..2)):binary operation on non-ground top value), s0: (-4..-1), s1: (0..1), str0: ("abef".."cdef"), str1: ("abef".."cdgh"), str2: ("ab".."cd")}`,
	}, {
		desc: "predefined ranges",
		in: `
			k1: int8
			k1: 44

			k2: int64
			k2: -8_000_000_000

			e1: int16
			e1: 100_000
		`,
		out: `<0>{k1: 44, k2: -8000000000, ` +
			`e1: _|_(((-32768..32767) & 100000):value 100000 not in range (-32768..32767))}`,
		// TODO(P3): if two fields are evaluating to the same field, their
		// values could be bound.
		// nodes:   use a shared node
		// other:   change to where clause
		// unknown: change to where clause.
		// in: `
		// 		a: b
		// 		b: a
		// 		a: { d: 1 }
		// 		`,
		// out: `<0>{a: {d:1}, b: {d:1}}`,

		// TODO(P2): circular references in expressions can be resolved when
		// unified with complete values.
		// in: `
		// 		a: 20
		// 		a: b + 10  // 20 & b+10  ==> 20; where a == b+10
		// 		b: a - 10  // 10-10 = 10  ==>         20 == 10+10
		// `
		// out: `<0>{a_0:20, b_1:10}`
	}}
	rewriteHelper(t, testCases, evalPartial)
}

func TestFullEval(t *testing.T) {
	testCases := []testCase{{
		desc: "detect conflicting value",
		in: `
				a: 8000.9
				a: 7080 | int`,
		out: `<0>{a: _|_(empty disjunction after evaluation)}`,
	}, {
		desc: "resolve all disjunctions",
		in: `
			service <Name>: {
				name: Name | string
				port: 7080 | int
			}
			service foo: _
			service bar: { port: 8000 }
			service baz: { name: "foobar" }
			`,
		out: `<0>{service: <1>{<>: <2>(Name: string)-><3>{name: (<2>.Name | string), port: (7080 | int)}, foo: <4>{name: "foo", port: 7080}, bar: <5>{name: "bar", port: 8000}, baz: <6>{name: "foobar", port: 7080}}}`,
	}, {
		desc: "field templates",
		in: `
			a: {
				<name>: int
				k: 1
			}
			b: {
				<x>: { x: 0, y: 1 | int }
				v: {}
				w: { y: 0 }
			}
			b: { <y>: {} } // TODO: allow different name
			c: {
				<Name>: { name: Name, y: 1 }
				foo: {}
				bar: _
			}
			`,
		out: `<0>{a: <1>{<>: <2>(name: string)->int, k: 1}, b: <3>{<>: <4>(x: string)->(<5>{x: 0, y: (1 | int)} & <6>{}), v: <7>{x: 0, y: 1}, w: <8>{x: 0, y: 0}}, c: <9>{<>: <10>(Name: string)-><11>{name: <10>.Name, y: 1}, foo: <12>{name: "foo", y: 1}, bar: <13>{name: "bar", y: 1}}}`,
	}, {
		desc: "field comprehension",
		in: `
			a: { "\(k)": v for k, v in b if k < "d" if v > b.a }
			b: {
				a: 1
				b: 2
				c: 3
				d: 4
			}
			c: {
				"\(k)": v <-
					for k, v in b
					if k < "d"
					if v > b.a
			}
			// TODO: Propagate error:
			// e: { "\(k)": v for k, v in b if k < "d" if v > b.a }
			`,
		out: `<0>{a: <1>{b: 2, c: 3}, b: <2>{a: 1, b: 2, c: 3, d: 4}, c: <3>{b: 2, c: 3}}`,
	}, {
		desc: "nested templates in one field",
		in: `
			a <A> b <B>: {
				name: A
				kind: B
			}
			a "A" b "B": _
			a "C" b "D": _
			a "E" b "F": { c: "bar" }
		`,
		out: `<0>{a: <1>{<>: <2>(A: string)-><3>{b: <4>{<>: <5>(B: string)-><6>{name: <2>.A, kind: <5>.B}, }}, A: <7>{b: <8>{<>: <9>(B: string)-><10>{name: <11>.A, kind: <9>.B}, B: <12>{name: "A", kind: "B"}}}, C: <13>{b: <14>{<>: <15>(B: string)-><16>{name: <17>.A, kind: <15>.B}, D: <18>{name: "C", kind: "D"}}}, E: <19>{b: <20>{<>: <21>(B: string)-><22>{name: <23>.A, kind: <21>.B}, F: <24>{name: "E", kind: "F", c: "bar"}}}}}`,
	}, {
		desc: "template unification within one struct",
		in: `
			a: {
				<A>: { name: A }
				<A>: { kind: A }
			}
			a "A": _
			a "C": _
			a "E": { c: "bar" }
		`,
		out: `<0>{a: <1>{<>: <2>(A: string)->(<3>{name: <2>.A} & <4>{kind: <2>.A}), ` +
			`A: <5>{name: "A", kind: "A"}, ` +
			`C: <6>{name: "C", kind: "C"}, ` +
			`E: <7>{name: "E", kind: "E", c: "bar"}}}`,
	}, {
		desc: "field comprehensions with multiple keys",
		in: `
			a "\(x.a)" b "\(x.b)": x for x in [
				{a: "A", b: "B" },
				{a: "C", b: "D" },
				{a: "E", b: "F" },
			]

			"\(x.a)" "\(x.b)": x for x in [
				{a: "A", b: "B" },
				{a: "C", b: "D" },
				{a: "E", b: "F" },
			]`,
		out: `<0>{a: <1>{` +
			`A: <2>{b: <3>{B: <4>{a: "A", b: "B"}}}, ` +
			`C: <5>{b: <6>{D: <7>{a: "C", b: "D"}}}, ` +
			`E: <8>{b: <9>{F: <10>{a: "E", b: "F"}}}}, ` +
			`A: <11>{B: <12>{a: "A", b: "B"}}, ` +
			`C: <13>{D: <14>{a: "C", b: "D"}}, ` +
			`E: <15>{F: <16>{a: "E", b: "F"}}}`,
	}, {
		desc: "field comprehensions with templates",
		in: `
			num: 1
			a: {
				<A> <B>: {
					name: A
					kind: B
				} if num < 5

			}
			a b c d: "bar"
			`,
		out: `<0>{num: 1, a: <1>{<>: <2>(A: string)-><3>{<>: <4>(B: string)-><5>{name: <2>.A, kind: <4>.B}, }, ` +
			`b: <6>{<>: <7>(B: string)-><8>{name: <9>.A, kind: <7>.B}, ` +
			`c: <10>{name: "b", kind: "c", ` +
			`d: "bar"}}}}`,
	}, {
		desc: "disjunctions of lists",
		in: `
			l: [ int, int ] | [ string, string ]

			l1: [ "a", "b" ]
			l2: l & [ "c", "d" ]
			`,
		out: `<0>{l: [int,int], l1: ["a","b"], l2: ["c","d"]}`,
	}, {
		desc: "list comprehension",
		in: `
			// a: [ k for k: v in b if k < "d" if v > b.a ] // TODO test error using common iso colon
			a: [ k for k, v in b if k < "d" if v > b.a ]
			b: {
				a: 1
				b: 2
				c: 3
				d: 4
			}
			c: [ x for _, x in b for _, y in b  if x < y ]
			d: [ x for x, _ in a ]
			`,
		out: `<0>{a: ["b","c"], b: <1>{a: 1, b: 2, c: 3, d: 4}, c: [1,1,1,2,2,3], d: [0,1]}`,
	}, {
		desc: "struct comprehension with template",
		in: `
			result: [ v for _, v in service ]

			service <Name>: {
				type: "service"
				name: Name | string
				port: 7080 | int
			}
			service foo: {}
			service bar: { port: 8000 }
			service baz: { name: "foobar" }
			`,
		out: `<0>{result: [` +
			`<1>{type: "service", name: "foo", port: 7080},` +
			`<2>{type: "service", name: "bar", port: 8000},` +
			`<3>{type: "service", name: "foobar", port: 7080}], ` +

			`service: <4>{` +
			`<>: <5>(Name: string)-><6>{type: "service", name: (<5>.Name | string), port: (7080 | int)}, ` +
			`foo: <7>{type: "service", name: "foo", port: 7080}, ` +
			`bar: <8>{type: "service", name: "bar", port: 8000}, ` +
			`baz: <9>{type: "service", name: "foobar", port: 7080}}}`,
	}, {
		desc: "resolutions in struct comprehension keys",
		in: `
			a: { "\(b + ".")": "a" for _, b in ["c"] }
			`,
		out: `<0>{a: <1>{c.: "a"}}`,
	}, {
		desc: "recursive evaluation within list",
		in: `
			l: [a]
			a: b & { c: "t" }
			b: {
				d: c
				c: string
			}
			l1: [a1]
			a1: b1 & { c: "t" }
			b1: {
				d: "s" + c
				c:  string
			}
		`,
		out: `<0>{l: [<1>{c: "t", d: "t"}], a: <2>{c: "t", d: "t"}, b: <3>{c: string, d: string}, l1: [<4>{c: "t", d: "st"}], a1: <5>{c: "t", d: "st"}, b1: <6>{c: string, d: _|_(("s" + string):unsupported op +(string, (string)*))}}`,
	}, {
		desc: "ips",
		in: `
		IP: 4*[ 0..255 ]

		Private:
			[ 192, 168, 0..255, 0..255 ] |
			[ 10, 0..255, 0..255, 0..255] |
			[ 172, 16..32, 0..255, 0..255 ]

		Inst: Private & [ _, 10, ... ]

		MyIP: Inst & [_, _, 10, 10 ]
		`,
		out: `<0>{IP: 4*[(0..255)], Private: [192,168,(0..255),(0..255)], Inst: [10,10,(0..255),(0..255)], MyIP: [10,10,10,10]}`,
	}, {
		desc: "complex interaction of groundness",
		in: `
			res: [ y & { d: "b" } for x in a for y in x ]
			res: [ a.b.c & { d: "b" } ]

			a b <C>: { d: string, s: "a" + d }
			a b c d: string
		`,
		// TODO(perf): unification should catch shared node.
		out: `<0>{res: [<1>{d: "b", s: "ab"}], a: <2>{b: <3>{<>: <4>(C: string)-><5>{d: string, s: ("a" + <5>.d)}, c: <6>{d: string, s: _|_(("a" + string):unsupported op +(string, (string)*))}}}}`,
	}, {
		desc: "complex groundness 2",
		in: `
			r1: f1 & { y: "c" }

			f1: { y: string, res: a.b.c & { d: y } }

			a b c: { d: string, s: "a" + d }
			a b <C>: { d: string, s: "a" + d }
			a b c d: string
		`,
		out: `<0>{r1: <1>{y: "c", res: <2>{d: "c", s: "ac"}}, f1: <3>{y: string, res: <4>{d: string, s: _|_(("a" + string):unsupported op +(string, (string)*))}}, a: <5>{b: <6>{<>: <7>(C: string)-><8>{d: string, s: ("a" + <8>.d)}, c: <9>{d: string, s: _|_(("a" + string):unsupported op +(string, (string)*))}}}}`,
	}, {
		desc: "references from template to concrete",
		in: `
				res: [t]
				t <X>: {
					a: c + b.str
					b str: string
					c: "X"
				}
				t x: { b str: "DDDD" }
				`,
		out: `<0>{res: [<1>{<>: <2>(X: string)-><3>{a: (<3>.c + <3>.b.str), c: "X", b: <4>{str: string}}, x: <5>{a: "XDDDD", c: "X", b: <6>{str: "DDDD"}}}], ` +
			`t: <7>{<>: <2>(X: string)-><3>{a: (<3>.c + <3>.b.str), c: "X", b: <4>{str: string}}, x: <8>{a: "XDDDD", c: "X", b: <9>{str: "DDDD"}}}}`,
	}}
	rewriteHelper(t, testCases, evalFull)
}
