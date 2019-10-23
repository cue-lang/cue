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
	"testing"
)

var traceOn = flag.Bool("debug", false, "enable tracing")

func compileFileWithErrors(t *testing.T, body string) (*context, *structLit, error) {
	t.Helper()
	ctx, inst, err := compileInstance(t, body)
	return ctx, inst.rootValue.evalPartial(ctx).(*structLit), err
}

func compileFile(t *testing.T, body string) (*context, *structLit) {
	t.Helper()
	ctx, inst, errs := compileInstance(t, body)
	if errs != nil {
		t.Fatal(errs)
	}
	return ctx, inst.rootValue.evalPartial(ctx).(*structLit)
}

func compileInstance(t *testing.T, body string) (*context, *Instance, error) {
	var r Runtime
	inst, err := r.Parse("test", body)

	if err != nil {
		x := newIndex(sharedIndex).newInstance(nil)
		ctx := x.newContext()
		return ctx, x, err
	}

	return r.index().newContext(), inst, nil
}

func rewriteHelper(t *testing.T, cases []testCase, r rewriteMode) {
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, obj := compileFile(t, tc.in)
			ctx.trace = *traceOn
			root := testResolve(ctx, obj, r)

			got := debugStr(ctx, root)

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
		desc: "regexp",
		in: `
			c1: "a" =~ "a"
			c2: "foo" =~ "[a-z]{3}"
			c3: "foo" =~ "[a-z]{4}"
			c4: "foo" !~ "[a-z]{4}"

			b1: =~ "a"
			b1: "a"
			b2: =~ "[a-z]{3}"
			b2: "foo"
			b3: =~ "[a-z]{4}"
			b3: "foo"
			b4: !~ "[a-z]{4}"
			b4: "foo"

			s1: != "b" & =~"c"      // =~"c"
			s2: != "b" & =~"[a-z]"  // != "b" & =~"[a-z]"

			e1: "foo" =~ 1
			e2: "foo" !~ true
			e3: != "a" & <5
		`,
		out: `<0>{c1: true, ` +
			`c2: true, ` +
			`c3: false, ` +
			`c4: true, ` +

			`b1: "a", ` +
			`b2: "foo", ` +
			`b3: _|_((=~"[a-z]{4}" & "foo"):invalid value "foo" (does not match =~"[a-z]{4}")), ` +
			`b4: "foo", ` +

			`s1: =~"c", ` +
			`s2: (!="b" & =~"[a-z]"), ` +

			`e1: _|_(("foo" =~ 1):invalid operation "foo" =~ 1 (mismatched types string and int)), ` +
			`e2: _|_(("foo" !~ true):invalid operation "foo" !~ true (mismatched types string and bool)), ` +
			`e3: _|_((!="a" & <5):conflicting values !="a" and <5 (mismatched types string and number))}`,
	}, {
		desc: "arithmetic",
		in: `
			i1: 1 & int
			i2: 2 & int

			sum: -1 + +2        // 1
			div1: 2.0 / 3 * 6   // 4
			div2: 2 / 3 * 6     // 4
			divZero: 1.0 / 0
			div00: 0 / 0
			b: 1 != 4

			idiv00: 0 div 0
			imod00: 0 mod 0
			iquo00: 0 quo 0
			irem00: 0 rem 0

			v1: 1.0T/2.0
			v2: 2.0 == 2
			v3: 2.0/3.0
			v5: i1 div i2

			e0: 2 + "a"
			// these are now all alloweed
			// e1: 2.0 / i1
			// e2: i1 / 2.0
			// e3: 3.0 % i2
			// e4: i1 % 2.0
			e5: 1.0 div 2
			e6: 2 rem 2.0
			e7: 2 quo 2.0
			e8: 1.0 mod 1
			`,
		out: `<0>{i1: 1, i2: 2, ` +
			`sum: 1, ` +
			`div1: 4.00000000000000000000000, ` +
			`div2: 4.00000000000000000000000, ` +
			`divZero: _|_((1.0 / 0):division by zero), ` +
			`div00: _|_((0 / 0):division undefined), ` +
			`b: true, ` +
			`idiv00: _|_((0 div 0):division by zero), ` +
			`imod00: _|_((0 mod 0):division by zero), ` +
			`iquo00: _|_((0 quo 0):division by zero), ` +
			`irem00: _|_((0 rem 0):division by zero), ` +
			`v1: 5e+11, ` +
			`v2: true, ` +
			`v3: 0.666666666666666666666667, ` +
			`v5: 0, ` +

			`e0: _|_((2 + "a"):invalid operation 2 + "a" (mismatched types int and string)), ` +
			// `e1: _|_((2.0 / 1):unsupported op /(float, int)), ` +
			// `e2: _|_((1 / 2.0):unsupported op /(int, float)), ` +
			// `e3: _|_((3.0 % 2):unsupported op %(float, int)), ` +
			// `e4: _|_((1 % 2.0):unsupported op %(int, float)), ` +
			`e5: _|_((1.0 div 2):invalid operation 1.0 div 2 (mismatched types float and int)), ` +
			`e6: _|_((2 rem 2.0):invalid operation 2 rem 2.0 (mismatched types int and float)), ` +
			`e7: _|_((2 quo 2.0):invalid operation 2 quo 2.0 (mismatched types int and float)), ` +
			`e8: _|_((1.0 mod 1):invalid operation 1.0 mod 1 (mismatched types float and int))}`,
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
			`,
		out: `<0>{q1: 2, q2: -2, q3: -2, q4: 2, ` +
			`qe1: _|_((2.0 quo 1):invalid operation 2.0 quo 1 (mismatched types float and int)), ` +
			`qe2: _|_((2 quo 1.0):invalid operation 2 quo 1.0 (mismatched types int and float)), ` +
			`r1: 1, r2: 1, r3: -1, r4: -1, ` +
			`re1: _|_((2.0 rem 1):invalid operation 2.0 rem 1 (mismatched types float and int)), ` +
			`re2: _|_((2 rem 1.0):invalid operation 2 rem 1.0 (mismatched types int and float)), ` +
			`d1: 2, d2: -2, d3: -3, d4: 3, ` +
			`de1: _|_((2.0 div 1):invalid operation 2.0 div 1 (mismatched types float and int)), ` +
			`de2: _|_((2 div 1.0):invalid operation 2 div 1.0 (mismatched types int and float)), ` +
			`m1: 1, m2: 1, m3: 1, m4: 1, ` +
			`me1: _|_((2.0 mod 1):invalid operation 2.0 mod 1 (mismatched types float and int)), ` +
			`me2: _|_((2 mod 1.0):invalid operation 2 mod 1.0 (mismatched types int and float))}`,
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
		out: "<0>{t: true, f: false, e: _|_(true:conflicting values true and false)}",
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
		out: "<0>{a: true, b: true, c: false, d: true, e: true, f: _|_(true:conflicting values true and false)}",
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
		out: `<0>{a: 1, b: 1, c: 1.0, d: _|_((int & float):conflicting values int and float (mismatched types int and float)), e: "4", f: true}`, // TODO: eliminate redundancy
	}, {
		desc: "strings and bytes",
		in: `
			s0: "foo" + "bar"
			s1: 3 * "abc"
			s2: "abc" * 2

			b0: 'foo' + 'bar'
			b1: 3 * 'abc'
			b2: 'abc' * 2

			// TODO: consider the semantics of this and perhaps allow this.
			e0: "a" + ''
			e1: 'b' + "c"
		`,
		out: `<0>{` +
			`s0: "foobar", ` +
			`s1: "abcabcabc", ` +
			`s2: "abcabc", ` +
			`b0: 'foobar', ` +
			`b1: 'abcabcabc', ` +
			`b2: 'abcabc', ` +

			`e0: _|_(("a" + ''):invalid operation "a" + '' (mismatched types string and bytes)), ` +
			`e1: _|_(('b' + "c"):invalid operation 'b' + "c" (mismatched types bytes and string))` +
			`}`,
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
			e4: [1, 2, ...>=4 & <=5] & [1, 2, 4, 8]
			e5: [1, 2, 4, 8] & [1, 2, ...>=4 & <=5]
			`,
		out: `<0>{list: [1,2,3], index: 2, unify: [1,2,3], e: _|_(([] & 4):conflicting values [] and 4 (mismatched types list and int)), e2: _|_("d":invalid list index "d" (type string)), e3: _|_(-1:invalid list index -1 (index must be non-negative)), e4: [1,2,4,_|_((<=5 & 8):invalid value 8 (out of bound <=5))], e5: [1,2,4,_|_((<=5 & 8):invalid value 8 (out of bound <=5))]}`,
	}, {
		desc: "list arithmetic",
		in: `
			list: [1,2,3]
			mul0: list*0
			mul1: list*1
			mul2: 2*list
			list1: [1]
		    mul1_0: list1*0
			mul1_1: 1*list1
			mul1_2: list1*2
			e: list*-1
			`,
		out: `<0>{list: [1,2,3], ` +
			`mul0: [], ` +
			`mul1: [1,2,3], ` +
			`mul2: [1,2,3,1,2,3], ` +
			`list1: [1], ` +
			`mul1_0: [], ` +
			`mul1_1: [1], ` +
			`mul1_2: [1,1], ` +
			`e: _|_((<1>.list * -1):negative number -1 multiplies list)}`,
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
		out: `<0>{obj: <1>{a: 1, b: 2}, index: 2, mulidx: 3, e: _|_(4:invalid struct index 4 (type int)), f: <2>{a: 1}.b, g: <3>{a: 1}["b"], h: _|_([3]:invalid operation: [3].b (type list does not support selection))}`,
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
		out: "<0>{o1: <1>{a: 1, b: 2}, o2: <2>{a: 1, b: 2}, o3: <3>{a: 1, b: 2}, o4: <4>{a: 1, b: 2}, e: _|_((1 & <5>{a: 3}):conflicting values 1 and {a: 3} (mismatched types int and struct))}",
	}, {
		desc: "disjunctions",
		in: `
			o1: 1 | 2 | 3
			o2: (1 | 2 | 3) & 1
			o3: 2 & (1 | *2 | 3)
			o4: (1 | *2 | 3) & (1 | 2 | *3)
			o5: (1 | *2 | 3) & (3 | *2 | 1)
			o6: (1 | 2 | 3) & (3 | 1 | 2)
			o7: (1 | 2 | 3) & (2 | 3)
			o8: (1 | 2 | 3) & (3 | 2)
			o9: (2 | 3) & (1 | 2 | 3)
			o10: (3 | 2) & (1 | *2 | 3)

			m1: (*1 | (*2 | 3)) & (>=2 & <=3)
			m2: (*1 | (*2 | 3)) & (2 | 3)
			m3: (*1 | *(*2 | 3)) & (2 | 3)
			m4: (2 | 3) & (*2 | 3)
			m5: (*2 | 3) & (2 | 3)

			// (*2 | 3) & (2 | 3)
			// (2 | 3) & (*2 | 3)
			// 2&(*2 | 3) | 3&(*2 | 3)
			// (*1 | (*2 | 3)) & (2 | 3)
			// *1& (2 | 3) | (*2 | 3)&(2 | 3)
			// *2&(2 | 3) | 3&(2 | 3)

			// (2 | 3)&(*1 | (*2 | 3))
			// 2&(*1 | (*2 | 3)) | 3&(*1 | (*2 | 3))
			// *1&2 | (*2 | 3)&2 | *1&3 | (*2 | 3)&3
			// (*2 | 3)&2 | (*2 | 3)&3
			// *2 | 3


			// All errors are treated the same as per the unification model.
			i1: [1, 2][3] | "c"
			`,
		out: `<0>{o1: (1 | 2 | 3), o2: 1, o3: 2, o4: (1 | 2 | 3 | *_|_), o5: (1 | *2 | 3), o6: (1 | 2 | 3), o7: (2 | 3), o8: (2 | 3), o9: (2 | 3), o10: (3 | *2), m1: (*2 | 3), m2: (*2 | 3), m3: (*2 | 3), m4: (*2 | 3), m5: (*2 | 3), i1: "c"}`,
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
		out: `<0>{i: int, j: 3, s: string, t: "s", e: _|_((int & string):conflicting values int and string (mismatched types int and string)), e2: _|_((1 & string):conflicting values 1 and string (mismatched types int and string)), b: _|_(!int:invalid operation !int (! int)), p: _|_(+true:invalid operation +true (+ bool)), m: _|_(-false:invalid operation -false (- bool))}`,
	}, {
		desc: "comparison",
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
		out: `<0>{lss: true, leq: true, eql: true, neq: true, gtr: true, geq: true, seq: true, err: _|_((2 == "s"):invalid operation 2 == "s" (mismatched types int and string))}`,
	}, {
		desc: "null",
		in: `
			eql: null == null
			neq: null != null
			unf: null & null

			// errors
			eq1: null == 1
			eq2: 1 == null
			ne1: "s" != null
			call: null()
		`,
		out: `<0>{eql: true, neq: false, unf: null, eq1: false, eq2: false, ne1: true, call: _|_(null:cannot call non-function null (type null))}`,
	}, {
		desc: "self-reference cycles",
		in: `
			a: b - 100
			b: a + 100

			c: [c[1], c[0]]
		`,
		out: `<0>{a: (<1>.b - 100), ` +
			`b: (<1>.a + 100), ` +
			`c: [<1>.c[1],<1>.c[0]]}`,
	}, {
		desc: "resolved self-reference cycles",
		in: `
			a: b - 100
			b: a + 100
			b: 200

			c: [c[1], a]

			s1: s2 & {a: 1}
			s2: s3 & {b: 2}
			s3: s1 & {c: 3}
		`,
		out: `<0>{a: 100, b: 200, c: [100,100], s1: <1>{a: 1, b: 2, c: 3}, s2: <2>{a: 1, b: 2, c: 3}, s3: <3>{a: 1, b: 2, c: 3}}`,
	}, {
		desc: "resolved self-reference cycles: Issue 19",
		in: `
			// CUE knows how to resolve the following:
			x: y + 100
			y: x - 100
			x: 200

			z1: z2 + 1
			z2: z3 + 2
			z3: z1 - 3
			z3: 8

			// TODO: extensive tests with disjunctions.
		`,
		out: `<0>{x: 200, y: 100, z1: 11, z2: 10, z3: 8}`,
	}, {
		desc: "delayed constraint failure",
		in: `
			a: b - 100
			b: a + 110
			b: 200

			x: 100
			x: x + 1
		`,
		out: `<0>{` +
			`a: _|_((210 & 200):conflicting values 210 and 200), ` +
			`b: _|_((210 & 200):conflicting values 210 and 200), ` +
			`x: _|_((100 & 101):conflicting values 100 and 101)}`,
		// TODO: find a way to mark error in data.
	}}
	rewriteHelper(t, testCases, evalPartial)
}

func TestChooseDefault(t *testing.T) {
	testCases := []testCase{{
		desc: "pick first",
		in: `
		a: *5 | "a" | true
		b c: *{
			a: 2
		} | {
			a : 3
		}
		`,
		out: "<0>{a: 5, b: <1>{c: <2>{a: 2}}}",
	}, {
		// In this test, default results to bottom, meaning that the non-default
		// value remains.
		desc: "simple disambiguation conflict",
		in: `
			a: *"a" | "b"
			b: *"b" | "a"
			c: a & b
			`,
		out: `<0>{a: "a", b: "b", c: ("a" | "b")}`,
	}, {
		desc: "associativity of defaults",
		in: `
			a: *"a" | ("b" | "c")
			b: (*"a" | "b") | "c"
			c: *"a" | (*"b" | "c")
			x: a & b
			y: b & c
			`,
		out: `<0>{a: "a", b: "a", c: (*"a" | *"b"), x: "a", y: (*"a" | *"b")}`,
	}}
	rewriteHelper(t, testCases, evalFull)
}

func TestResolve(t *testing.T) {
	testCases := []testCase{{
		desc: "convert _ to top",
		in:   `a: { <_>: _ }`,
		out:  `<0>{a: <1>{...}}`,
	}, {
		in: `
			a: b.c.d
			b c: { d: 3 }
			c: { c: d.d, }
			d: { d: 2 }
			`,
		out: "<0>{a: 3, b: <1>{c: <2>{d: 3}}, c: <3>{c: 2}, d: <4>{d: 2}}",
	}, {
		in:  "`foo-bar`: 3\n x: `foo-bar`,",
		out: `<0>{foo-bar: 3, x: 3}`,
	}, {
		desc: "resolution of quoted identifiers",
		in: `
		package foo

` + "`foo-bar`" + `: 2
"baz":     ` + "`foo-bar`" + `

a: {
	qux:        3
	` + "`qux-quux`" + `: qux
	"qaz":      ` + "`qux-quux`" + `
}`,
		out: "<0>{foo-bar: 2, baz: 2, a: <1>{qux: 3, qux-quux: 3, qaz: 3}}",
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
				v1: 1.0T/2.0
				v2: 2.0 == 2
				n1: 1
				v5: 2.0 / n1
				e2: int & 4.0/2.0
				`,
		out: `<0>{v1: 5e+11, v2: true, n1: 1, v5: 2, e2: _|_((int & (4.0 / 2.0)):conflicting values int and (4.0 / 2.0) (mismatched types int and float))}`,
	}, {
		desc: "inequality",
		in: `
			a: 1 != 2
			b: 1 != null
			c: true == null
			d: null != {}
			e: null == []
			f: 0 == 0.0    // types are unified first TODO: make this consistent
		`,
		out: `<0>{a: true, b: true, c: false, d: true, e: false, f: true}`,
	}, {
		desc: "attributes",
		in: `
			a: { foo: 1 @foo() @baz(1) }
			b: { foo: 1 @bar() @foo() }
			c: a & b

			e: a & { foo: 1 @foo(other) }
		`,
		out: `<0>{a: <1>{foo: 1 @baz(1) @foo()}, ` +
			`b: <2>{foo: 1 @bar() @foo()}, ` +
			`c: <3>{foo: 1 @bar() @baz(1) @foo()}, ` +
			`e: _|_((<4>.a & <5>{foo: 1 @foo(other)}):conflicting attributes for key "foo")}`,
	}, {
		desc: "optional field unification",
		in: `
			a: { foo?: string }
			b: { foo: "foo" }
			c: a & b
			d: a & { "foo"?: "bar" }

			g1: 1
			"g\(1)"?: 1
			"g\(2)"?: 2
		`,
		out: `<0>{a: <1>{foo?: string}, ` +
			`b: <2>{foo: "foo"}, ` +
			`c: <3>{foo: "foo"}, ` +
			`d: <4>{foo?: "bar"}, ` +
			`g1: 1, ` +
			`g2?: 2}`,
	}, {
		desc: "optional field resolves to incomplete",
		in: `
		r: {
			a?: 3
			b: a
			c: r["a"]
		}
	`,
		out: `<0>{r: <1>{a?: 3, b: <2>.a, c: <3>.r["a"]}}`,
		// TODO(#152): should be
		// out: `<0>{r: <1>{a?: 3, b: <2>.a, c: <2>["a"]}}`,
	}, {
		desc: "bounds",
		in: `
			i1: >1 & 5
			i2: (>=0 & <=10) & 5
			i3: !=null & []
			i4: !=2 & !=4


			s1: >=0 & <=10 & !=1        // no simplification
			s2: >=0 & <=10 & !=11       // >=0 & <=10
			s3: >5 & !=5                // >5
			s4: <10 & !=10              // <10
			s5: !=2 & !=2

			// TODO: could change inequality
			s6: !=2 & >=2
			s7: >=2 & !=2

			s8: !=5 & >5

			s10: >=0 & <=10 & <12 & >1   // >1  & <=10
			s11: >0 & >=0 & <=12 & <12   // >0  & <12

			s20: >=10 & <=10             // 10

			s22:  >5 & <=6               // no simplification
			s22a: >5 & (<=6 & int)       // 6
			s22b: (int & >5) & <=6       // 6
			s22c: >=5 & (<6 & int)       // 5
			s22d: (int & >=5) & <6       // 5
			s22e: (>=5 & <6) & int       // 5
			s22f: int & (>=5 & <6)       // 5

			s23: >0 & <2                 // no simplification
			s23a: (>0 & <2) & int        // int & 1
			s23b: int & (>0 & <2)        // int & 1
			s23c: (int & >0) & <2        // int & 1
			s23d: >0 & (int & <2)        // int & 1
			s23e: >0.0 & <2.0            // no simplification

			s30: >0 & int

			e1: null & !=null
			e2: !=null & null
			e3: >1 & 1
			e4: <0 & 0
			e5: >1 & <0
			e6: >11 & <11
			e7: >=11 & <11
			e8: >11 & <=11
			e9: >"a" & <1
		`,
		out: `<0>{i1: 5, i2: 5, i3: [], i4: (!=2 & !=4), ` +

			`s1: (>=0 & <=10 & !=1), ` +
			`s2: (>=0 & <=10), ` +
			`s3: >5, ` +
			`s4: <10, ` +
			`s5: !=2, ` +

			`s6: (!=2 & >=2), ` +
			`s7: (>=2 & !=2), ` +

			`s8: >5, ` +

			`s10: (<=10 & >1), ` +
			`s11: (>0 & <12), ` +

			`s20: 10, ` +

			`s22: (>5 & <=6), ` +
			`s22a: 6, ` +
			`s22b: 6, ` +
			`s22c: 5, ` +
			`s22d: 5, ` +
			`s22e: 5, ` +
			`s22f: 5, ` +

			`s23: (>0 & <2), ` +
			`s23a: 1, ` +
			`s23b: 1, ` +
			`s23c: 1, ` +
			`s23d: 1, ` +
			`s23e: (>0.0 & <2.0), ` +

			`s30: int & >0, ` +

			`e1: _|_((!=null & null):invalid value null (excluded by !=null)), ` +
			`e2: _|_((!=null & null):invalid value null (excluded by !=null)), ` +
			`e3: _|_((>1 & 1):invalid value 1 (out of bound >1)), ` +
			`e4: _|_((<0 & 0):invalid value 0 (out of bound <0)), ` +
			`e5: _|_(conflicting bounds >1 and <0), ` +
			`e6: _|_(conflicting bounds >11 and <11), ` +
			`e7: _|_(conflicting bounds >=11 and <11), ` +
			`e8: _|_(conflicting bounds >11 and <=11), ` +
			`e9: _|_((>"a" & <1):conflicting values >"a" and <1 (mismatched types string and number))}`,
	}, {
		desc: "bound conversions",
		in: `
		r1: int & >0.1 & <1.9
		r2: int & >=0.1 & <1.9
		r3: int & >=-1.9 & <=-0.1
		r4: int & >-1.9 & <=-0.1

		r5: >=1.1 & <=1.1
		r6: r5 & 1.1

		c1: (1.2 & >1.3) & <2
		c2: 1.2 & (>1.3 & <2)

		c3: 1.2 & (>=1 & <2)
		c4: 1.2 & (>=1 & <2 & int)
		`,
		out: `<0>{` +
			`r1: 1, ` +
			`r2: 1, ` +
			`r3: -1, ` +
			`r4: -1, ` +
			`r5: 1.1, ` +
			`r6: 1.1, ` +
			`c1: _|_((>1.3 & 1.2):invalid value 1.2 (out of bound >1.3)), ` +
			`c2: _|_((>1.3 & 1.2):invalid value 1.2 (out of bound >1.3)), ` +
			`c3: 1.2, ` +
			`c4: _|_((1.2 & ((>=1 & <2) & int)):conflicting values 1.2 and ((>=1 & <2) & int) (mismatched types float and int))}`,
	}, {
		desc: "custom validators",
		in: `
		import "strings"

		a: strings.ContainsAny("ab")
		a: "after"

		b: strings.ContainsAny("c")
		b: "dog"

		c: strings.ContainsAny("d") & strings.ContainsAny("g")
		c: "dog"
		`,
		out: `<0>{` +
			`a: "after", ` +
			`b: _|_(strings.ContainsAny ("c"):invalid value "dog" (does not satisfy strings.ContainsAny("c"))), ` +
			`c: "dog"` +
			`}`,
	}, {
		desc: "null coalescing",
		in: `
			a: null
			b: a.x | "b"
			c: a["x"] | "c"
			`,
		out: `<0>{a: null, b: "b", c: "c"}`,
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
			c: (*l|{"3":3})["3"]
			d: (*[]|[1])[0]
			l: []
			e1: [2][""]
			e2: 2[2]
			e3: [][true]
			e4: [1,2,3][3]
			e5: [1,2,3][-1]
			e6: (*[]|{})[1]
			def: {
				a: 1
				b :: 3
			}
			e7: def["b"]
		`,
		out: `<0>{a: 2, b: "bar", c: _|_("3":invalid list index "3" (type string)), l: [], d: _|_([]:index 0 out of bounds), e1: _|_("":invalid list index "" (type string)), e2: _|_(2:invalid operation: 2[2] (type int does not support indexing)), e3: _|_(true:invalid list index true (type bool)), e4: _|_([1,2,3]:index 3 out of bounds), e5: _|_(-1:invalid list index -1 (index must be non-negative)), e6: _|_([]:index 1 out of bounds), def: <1>{a: 1, b :: 3}, e7: _|_(<2>.def["b"]:field "b" is a definition)}`,
		// }, {
		// NOTE: string indexing no longer supported.
		// Keeping it around until this is no longer an experiment.
		// 	desc: "string index",
		// 	in: `
		// 		a0: "abc"[0]
		// 		a1: "abc"[1]
		// 		a2: "abc"[2]
		// 		a3: "abc"[3]
		// 		a4: "abc"[-1]

		// 		b: "zoëven"[2]
		// 	`,
		// 	out: `<0>{a0: "a", a1: "b", a2: "c", a3: _|_("abc":index 3 out of bounds), a4: _|_(-1:invalid string index -1 (index must be non-negative)), b: "ë"}`,
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
		out: `<0>{a: [], b: [], e1: _|_(1:slice bounds out of range), e2: _|_([0]:negative slice index), e3: _|_([0]:invalid slice index: 1 > 0), e4: _|_(2:slice bounds out of range), e5: _|_(4:cannot slice 4 (type int)), e6: _|_("":invalid slice index "" (type string)), e7: _|_("9":invalid slice index "9" (type string))}`,
		// }, {
		// NOTE: string indexing no longer supported.
		// Keeping it around until this is no longer an experiment.
		// 	desc: "string slice",
		// 	in: `
		// 		a0: ""[0:0]
		// 		a1: ""[:]
		// 		a2: ""[0:]
		// 		a3: ""[:0]
		// 		b0: "abc"[0:0]
		// 		b1: "abc"[0:1]
		// 		b2: "abc"[0:2]
		// 		b3: "abc"[0:3]
		// 		b4: "abc"[3:3]
		// 		b5: "abc"[1:]
		// 		b6: "abc"[:2]

		// 		// TODO: supported extended graphemes, instead of just runes.
		// 		u: "Spaß"[3:4]
		// 	`,
		// 	out: `<0>{a0: "", a1: "", a2: "", a3: "", b0: "", b1: "a", b2: "ab", b3: "abc", b4: "", b5: "bc", b6: "ab", u: "ß"}`,
	}, {
		desc: "list types",
		in: `
			l0: 3*[int]
			l0: [1, 2, 3]
			l2: [...{ a: int }]
			l2: [{a: 1}, {a: 2, b: 3}]

			// TODO: work out a decent way to specify length ranges of lists.
			// l3: <=10*[int]
			// l3: [1, 2, 3, ...]

			s1: (6*[int])[2:3]
			s2: [0,2,3][1:2]

			i1: (6*[int])[2]
			i2: [0,2,3][2]

			t0: [...{a: 8}]
			t0: [{}]
			t1: [...]
			t1: [...int]

			e0: 2*[{}]
			e0: [{}]
			e1: [...int]
			e1: [...float]
			`,
		out: `<0>{` +
			`l0: [1,2,3], ` +
			`l2: [<1>{a: 1},<2>{a: 2, b: 3}], ` +
			`s1: [int], ` +
			`s2: [2], ` +
			`i1: int, ` +
			`i2: 3, ` +
			`t0: [<3>{a: 8}], ` +
			`t1: [, ...int], ` +
			`e0: _|_(([<4>{},<4>{}] & [<5>{}]):conflicting list lengths: conflicting values 2 and 1), ` +
			`e1: _|_((int & float):conflicting list element types: conflicting values int and float (mismatched types int and float))` +
			`}`,
	}, {
		// TODO: consider removing list arithmetic altogether. It is no longer
		// needed to indicate the allowed capacity of a list and that didn't
		// work anyway.
		desc: "list arithmetic",
		in: `
			l0: 3*[1, 2, 3]
			l1: 0*[1, 2, 3]
			l2: 10*[]
			l3: <=2*[]
			l4: <=2*[int]
			l5: <=2*(int*[int])
			l6: 3*[...int]
			l7: 3*[1, ...int]
			l8: 3*[1, 2, ...int]

			s0: [] + []
			s1: [1] + []
			s2: [] + [2]
			s3: [1] + [2]
			s4: [1,2] + []
			s5: [] + [1,2]
			s6: [1] + [1,2]
			s7: [1,2] + [1]
			s8: [1,2] + [1,2]
			s9: [] + [...]
			s10: [1] + [...]
			s11: [] + [2, ...]
			s12: [1] + [2, ...]
			s13: [1,2] + [...]
			s14: [] + [1,2, ...]
			s15: [1] + [1,2, ...]
			s16: [1,2] + [1, ...]
			s17: [1,2] + [1,2, ...]

			s18: [...] + []
			s19: [1, ...] + []
			s20: [...] + [2]
			s21: [1, ...] + [2]
			s22: [1,2, ...] + []
			s23: [...] + [1,2]
			s24: [1, ...] + [1,2]
			s25: [1,2, ...] + [1]
			s26: [1,2, ...] + [1,2]
			s27: [...] + [...]
			s28: [1, ...] + [...]
			s29: [...] + [2, ...]
			s30: [1, ...] + [2, ...]
			s31: [1,2, ...] + [...]
			s32: [...] + [1,2, ...]
			s33: [1, ...] + [1,2, ...]
			s34: [1,2, ...] + [1, ...]
			s35: [1,2, ...] + [1,2, ...]
			`,
		out: `<0>{l0: [1,2,3,1,2,3,1,2,3], ` +
			`l1: [], ` +
			`l2: [], ` +
			`l3: (<=2 * []), ` +
			`l4: (<=2 * [int]), ` +
			`l5: (<=2 * (int * [int])), ` +
			`l6: [], ` +
			`l7: [1,1,1], ` +
			`l8: [1,2,1,2,1,2], ` +

			`s0: [], ` +
			`s1: [1], ` +
			`s2: [2], ` +
			`s3: [1,2], ` +
			`s4: [1,2], ` +
			`s5: [1,2], ` +
			`s6: [1,1,2], ` +
			`s7: [1,2,1], ` +
			`s8: [1,2,1,2], ` +
			`s9: [], ` +
			`s10: [1], ` +
			`s11: [2], ` +
			`s12: [1,2], ` +
			`s13: [1,2], ` +
			`s14: [1,2], ` +
			`s15: [1,1,2], ` +
			`s16: [1,2,1], ` +
			`s17: [1,2,1,2], ` +

			`s18: [], ` +
			`s19: [1], ` +
			`s20: [2], ` +
			`s21: [1,2], ` +
			`s22: [1,2], ` +
			`s23: [1,2], ` +
			`s24: [1,1,2], ` +
			`s25: [1,2,1], ` +
			`s26: [1,2,1,2], ` +
			`s27: [], ` +
			`s28: [1], ` +
			`s29: [2], ` +
			`s30: [1,2], ` +
			`s31: [1,2], ` +
			`s32: [1,2], ` +
			`s33: [1,1,2], ` +
			`s34: [1,2,1], ` +
			`s35: [1,2,1,2]` +

			`}`,
	}, {
		desc: "list equality",
		in: `
		eq0: [] == []
		eq1: [...] == []
		eq2: [] == [...]
		eq3: [...] == [...]

		eq4: [1] == [1]
		eq5: [1, ...] == [1]
		eq6: [1] == [1, ...]
		eq7: [1, ...] == [1, ...]

		eq8: [1, 2] == [1, 2]
		eq9: [1, 2, ...] == [1, 2]
		eq10: [1, 2] == [1, 2, ...]
		eq11: [1, 2, ...] == [1, 2, ...]

		ne0: [] != []
		ne1: [...] != []
		ne2: [] != [...]
		ne3: [...] != [...]

		ne4: [1] != [1]
		ne5: [1, ...] != [1]
		ne6: [1] != [1, ...]
		ne7: [1, ...] != [1, ...]

		ne8: [1, 2] != [1, 2]
		ne9: [1, 2, ...] != [1, 2]
		ne10: [1, 2] != [1, 2, ...]
		ne11: [1, 2, ...] != [1, 2, ...]

		feq0: [] == [1]
		feq1: [...] == [1]
		feq2: [] == [1, ...]
		feq3: [...] == [1, ...]

		feq4: [1] == []
		feq5: [1, ...] == []
		feq6: [1] == [...]
		feq7: [1, ...] == [...]

		feq8: [1, 2] == [1]
		feq9: [1, ...] == [1, 2]
		feq10: [1, 2] == [1, ...]
		feq11: [1, ...] == [1, 2, ...]

		fne0: [] != [1]
		fne1: [...] != [1]
		fne2: [] != [1, ...]
		fne3: [1, ...] != [1, ...]

		fne4: [1] != []
		fne5: [1, ...] != []
		fne6: [1] != [...]
		fne7: [1, ...] != [...]

		fne8: [1, 2] != [1]
		fne9: [1, ...] != [1, 2]
		fne10: [1, 2] != [1, ...]
		fne11: [1, ...] != [1, 2, ...]
		`,
		out: `<0>{` +
			`eq0: true, eq1: true, eq2: true, eq3: true, eq4: true, eq5: true, eq6: true, eq7: true, eq8: true, eq9: true, eq10: true, eq11: true, ` +
			`ne0: true, ne1: true, ne2: true, ne3: true, ne4: false, ne5: false, ne6: false, ne7: false, ne8: false, ne9: false, ne10: false, ne11: false, ` +
			`feq0: false, feq1: false, feq2: false, feq3: false, feq4: false, feq5: false, feq6: false, feq7: false, feq8: false, feq9: false, feq10: false, feq11: false, ` +
			`fne0: false, fne1: false, fne2: false, fne3: false, fne4: false, fne5: false, fne6: false, fne7: false, fne8: false, fne9: false, fne10: false, fne11: false}`,
	}, {
		desc: "list unification",
		in: `
		a: { l: ["foo", v], v: l[1] }
		b: a & { l: [_, "bar"] }
		`,
		out: `<0>{` +
			`a: <1>{l: ["foo",<2>.v], ` +
			`v: <2>.l[1]}, ` +
			`b: <3>{l: ["foo","bar"], v: "bar"}}`,
	}, {
		desc: "correct error messages",
		// Tests that it is okay to partially evaluate structs.
		in: `
			a: "a" & 1
			`,
		out: `<0>{a: _|_(("a" & 1):conflicting values "a" and 1 (mismatched types string and int))}`,
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
		desc: "definitions",
		in: `
			Foo :: {
				field: int
				recursive: {
					field: string
				}
			}

			// Allowed
			Foo1 :: { field: int }
			Foo1 :: { field2: string }

			foo: Foo
			foo: { feild: 2 }

			foo1: Foo
			foo1: {
				field: 2
				recursive: {
					feild: 2 // Not caught as per spec. TODO: change?
				}
			}

			Bar :: {
				field: int
				<A>:   int
			}
			bar: Bar
			bar: { feild: 2 }

			Mixed :: string
			Mixed: string

			mixedRec: { Mixed :: string }
			mixedRec: { Mixed: string }
			`,
		out: `<0>{` +
			`Foo :: <1>C{field: int, recursive: <2>C{field: string}}, ` +
			`Foo1 :: <3>C{field: int, field2: string}, ` +
			`foo: _|_(2:field "feild" not allowed in closed struct), ` +
			`foo1: <4>C{field: 2, recursive: _|_(2:field "feild" not allowed in closed struct)}, ` +
			`Bar :: <5>{<>: <6>(A: string)->int, field: int}, ` +
			`bar: <7>{<>: <8>(A: string)->int, field: int, feild: 2}, ` +
			`Mixed: _|_(field "Mixed" declared as definition and regular field), ` +
			`mixedRec: _|_(field "Mixed" declared as definition and regular field)}`,
	}, {
		desc: "combined definitions",
		in: `
			// Allow combining of structs within a definition
			D1 :: {
				env a: "A"
				env b: "B"
				def :: {a: "A"}
				def :: {b: "B"}
			}

			d1: D1 & { env c: "C" }

			D2 :: {
				a: int
			}
			D2 :: {
				b: int
			}

			D3 :: {
				env a: "A"
			}
			D3 :: {
				env b: "B"
			}

			D4 :: {
				env: DC
				env b: int
			}

			DC :: { a: int }
					`,
		out: `<0>{` +
			`D1 :: <1>C{env: <2>C{a: "A", b: "B"}, def :: <3>C{a: "A", b: "B"}}, ` +
			`d1: <4>C{env: _|_("C":field "c" not allowed in closed struct), def :: <5>C{a: "A", b: "B"}}, ` +
			`D2 :: <6>C{a: int, b: int}, ` +
			`D3 :: <7>C{env: <8>C{a: "A", b: "B"}}, ` +
			`D4 :: <9>C{env: _|_(int:field "b" not allowed in closed struct)}, ` +
			`DC :: <10>C{a: int}` +
			`}`,
	}, {
		desc: "recursive closing starting at non-definition",
		in: `
			z a: {
				B:: {
					c d: 1
					c f: 1
				}
			}
			A: z & { a: { B :: { c e: 2 } } }
			`,
		out: `<0>{z: <1>{a: <2>{B :: <3>C{c: <4>C{d: 1, f: 1}}}}, A: <5>{a: <6>{B :: <7>C{c: _|_(2:field "e" not allowed in closed struct)}}}}`,
	}, {
		desc: "non-closed definition carries over closedness to enclosed template",
		in: `
		S :: {
			[string]: { a: int }
		}
		a: S & {
			v: { b: int }
		}
		Q :: {
			[string]: { a: int } | { b: int }
		}
		b: Q & {
			w: { c: int }
		}
		R :: {
			[string]: [{ a: int }, { b: int }]
		}
		c: R & {
			w: [{ d: int }, ...]
		}
		`,
		out: `<0>{` +
			`S :: <1>{<>: <2>(_: string)-><3>C{a: int}, }, ` +
			`a: <4>{<>: <5>(_: string)-><6>C{a: int}, v: _|_(int:field "b" not allowed in closed struct)}, ` +
			`b: <7>{<>: <8>(_: string)->(<9>C{a: int} | <10>C{b: int}), w: _|_(int:empty disjunction: field "c" not allowed in closed struct)}, ` +
			`Q :: <11>{<>: <12>(_: string)->(<13>C{a: int} | <14>C{b: int}), }, ` +
			`c: <15>{<>: <16>(_: string)->[<17>C{a: int},<18>C{b: int}], w: [_|_(int:field "d" not allowed in closed struct),<19>C{b: int}]}, ` +
			`R :: <20>{<>: <21>(_: string)->[<22>C{a: int},<23>C{b: int}], }}`,
	}, {
		desc: "definitions with disjunctions",
		in: `
			Foo :: {
				field: int

				{ a: 1 } |
				{ b: 2 }
			}

			foo: Foo
			foo: { a: 1 }

			bar: Foo
			bar: { c: 2 }

			baz: Foo
			baz: { b: 2 }
			`,
		out: `<0>{` +
			`Foo :: (<1>C{field: int, a: 1} | <2>C{field: int, b: 2}), ` +
			`foo: <3>C{field: int, a: 1}, ` +
			`bar: _|_(2:empty disjunction: field "c" not allowed in closed struct), ` +
			`baz: <4>C{field: int, b: 2}}`,
	}, {
		desc: "definitions with disjunctions recurisive",
		in: `
			Foo :: {
				x: {
					field: int

					{ a: 1 } |
					{ b: 2 }
				}
				x c: 3
			}
					`,
		out: `<0>{` +
			`Foo :: <1>C{x: (<2>C{field: int, a: 1, c: 3} | <3>C{field: int, b: 2, c: 3})}` +
			`}`,
	}, {
		desc: "definitions with embedding",
		in: `
		E :: {
			a: { b: int }
		}

		S :: {
			E
			a: { c: int }
			b: 3
		}

		// adding a field to a nested struct that is closed.
		e1 :: S & { a d: 4 }
		// literal struct not closed until after unification.
		v1 :: S & { a c: 4 }
		`,
		out: `<0>{` +
			`E :: <1>C{a: <2>C{b: int}}, ` +
			`S :: <3>C{a: <4>C{b: int, c: int}, b: 3}, ` +
			`e1 :: <5>C{a: _|_(4:field "d" not allowed in closed struct), b: 3}, ` +
			`v1 :: <6>C{a: <7>C{b: int, c: 4}, b: 3}}`,
	}, {
		desc: "top-level definition with struct and disjunction",
		in: `
		def :: {
			Type: string
			Text: string
			Size: int
		}

		def :: {
			Type: "B"
			Size: 0
		} | {
			Type: "A"
			Size: 1
		}`,
		out: `<0>{` +
			`def :: (<1>C{Size: (0 & int), Type: ("B" & string), Text: string} | ` +
			`<2>C{Size: (1 & int), Type: ("A" & string), Text: string})` +
			`}`,
	}, {
		desc: "closing structs",
		in: `
		op: {x: int}             // {x: int}
		ot: {x: int, ...}        // {x: int, ...}
		cp: close({x: int})      // closed({x: int})
		ct: close({x: int, ...}) // {x: int, ...}

		opot: op & ot  // {x: int, ...}
		otop: ot & op  // {x: int, ...}
		opcp: op & cp  // closed({x: int})
		cpop: cp & op  // closed({x: int})
		opct: op & ct  // {x: int, ...}
		ctop: ct & op  // {x: int, ...}
		otcp: ot & cp  // closed({x: int})
		cpot: cp & ot  // closed({x: int})
		otct: ot & ct  // {x: int, ...}
		ctot: ct & ot  // {x: int, ...}
		cpct: cp & ct  // closed({x: int})
		ctcp: ct & cp  // closed({x: int})
		ctct: ct & ct  // {x: int, ...}
		`,
		out: `<0>{` +
			`op: <1>{x: int}, ` +
			`ot: <2>{x: int, ...}, ` +
			`cp: <3>C{x: int}, ` +
			`ct: <4>{x: int, ...}, ` +
			`opot: <5>{x: int, ...}, ` +
			`otop: <6>{x: int, ...}, ` +
			`opcp: <7>C{x: int}, ` +
			`cpop: <8>C{x: int}, ` +
			`opct: <9>{x: int, ...}, ` +
			`ctop: <10>{x: int, ...}, ` +
			`otcp: <11>C{x: int}, ` +
			`cpot: <12>C{x: int}, ` +
			`otct: <13>{x: int, ...}, ` +
			`ctot: <14>{x: int, ...}, ` +
			`cpct: <15>C{x: int}, ` +
			`ctcp: <16>C{x: int}, ` +
			`ctct: <17>{x: int, ...}}`,
	}, {
		desc: "excluded embedding from closing",
		in: `
		S :: {
			a: { c: int }
			{
				c: { d: int }
			}
			B = { open: int }
			b: B
		}
		V: S & {
			c e: int
			b extra: int
		}
		`,
		out: `<0>{` +
			`S :: <1>C{` +
			`a: <2>C{c: int}, ` +
			`c: <3>{d: int}, ` +
			`b: <4>{open: int}}, ` +
			`V: <5>C{` +
			`a: <6>C{c: int}, ` +
			`c: <7>{d: int, e: int}, ` +
			`b: <8>{open: int, extra: int}}}`,
	}, {
		desc: "closing with failed optional",
		in: `
		k1 :: {a: int, b?: int} & A // closed({a: int})
		k2 :: A & {a: int, b?: int} // closed({a: int})

		o1: {a?: 3} & {a?: 4} // {a?: _|_}

		// Optional fields with error values can be elimintated when closing
		o2 :: {a?: 3} & {a?: 4} // close({})

		d1 :: {a?: 2, b: 4} | {a?: 3, c: 5}
		v1: d1 & {a?: 3, b: 4}  // close({b: 4})

		A :: {a: int}
		`,
		out: `<0>{` +
			`k1 :: <1>C{a: int}, ` +
			`A :: <2>C{a: int}, ` +
			`k2 :: <3>C{a: int}, ` +
			`o1: <4>{a?: _|_((3 & 4):conflicting values 3 and 4)}, ` +
			`o2 :: <5>C{a?: _|_((3 & 4):conflicting values 3 and 4)}, ` +
			`d1 :: (<6>C{a?: 2, b: 4} | <7>C{a?: 3, c: 5}), ` +
			`v1: <8>C{a?: _|_((2 & 3):conflicting values 2 and 3), b: 4}` +
			`}`,
	}, {
		desc: "closing with comprehensions",
		in: `
		A :: {f1: int, f2: int}

		for k, v in {f3 : int} {
			a: A & { "\(k)": v }
		}

		B :: {
			for k, v in {f1: int} {
				"\(k)": v
			}
		}

		C :: {
			f1: _
			for k, v in {f1: int} {
				"\(k)": v
			}
		}

		D :: {
			for k, v in {f1: int} {
				"\(k)": v
			}
			...
		}

		E :: A & {
			for k, v in { f3: int } {
				"\(k)": v
			}
		}
		`,
		out: `<0>{` +
			`E :: _|_(int:field "f3" not allowed in closed struct), ` +
			`A :: <1>C{f1: int, f2: int}, ` +
			`a: _|_(int:field "f3" not allowed in closed struct), ` +
			`B :: <2>C{f1: int}, ` +
			`C :: <3>C{f1: int}, ` +
			`D :: <4>{f1: int, ...}` +
			`}`,
	}, {
		desc: "incomplete comprehensions",
		in: `
		A: {
			for v in src {
				"\(v)": v
			}
			src: _
			if true {
				baz: "baz"
			}
		}
		B: A & {
			src: ["foo", "bar"]
		}
		`,
		out: `<0>{` +
			`A: <1>{src: _, baz: "baz" <2>for _, v in <3>.src yield <4>{""+<2>.v+"": <2>.v}}, ` +
			`B: <5>{src: ["foo","bar"], baz: "baz", foo: "foo", bar: "bar"}}`,
	}, {
		desc: "reference to root",
		in: `
			a: { b: int }
			c: a & {
				b: 100
				d: a.b + 3 // do not resolve as c != a.
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
		out: `<0>{a: <1>{b: int}, c: <2>{b: 100, d: (<3>.a.b + 3)}, x: <4>{b: int, c: (<5>.b + 5)}, y: <6>{b: 100, c: 105}, v: <7>{b: int, c: (<3>.v.b + 5)}, w: <8>{b: 100, c: (<3>.v.b + 5)}, wp: <9>{b: 100, c: (<3>.v.b + 5)}}`,
		// TODO(#152): should be
		// out: `<0>{a: <1>{b: int}, c: <2>{b: 100, d: (<3>.a.b + 3)}, x: <4>{b: int, c: (<5>.b + 5)}, y: <6>{b: 100, c: 105}, v: <7>{b: int, c: (<8>.b + 5)}, w: <9>{b: 100, c: 105}, wp: <10>{b: 100, c: 105}}`,
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
		desc: "multiline interpolation",
		in: `
			a1: """
			before
			\(4)
			after
			"""
			a2: """
			before
			\(4)

			"""
			a3: """

			\(4)
			after
			"""
			a4: """

			\(4)

			"""
			m1: """
			before
			\(
				4)
			after
			"""
			m2: """
			before
			\(
	4)

			"""
			m3: """

			\(

				4)
			after
			"""
			m4: """

			\(
	4)

			"""
			`,
		out: `<0>{` +
			`a1: "before\n4\nafter", a2: "before\n4\n", a3: "\n4\nafter", a4: "\n4\n", ` +
			`m1: "before\n4\nafter", m2: "before\n4\n", m3: "\n4\nafter", m4: "\n4\n"` +
			`}`,
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
				<X>: { x: 0, y: *1 | int }
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
		out: `<0>{a: <1>{<>: <2>(name: string)->int, k: 1}, b: <3>{<>: <4>(X: string)->(<5>{x: 0, y: (*1 | int)} & <6>{}), v: <7>{x: 0, y: (*1 | int)}, w: <8>{x: 0, y: (*1 | int)}}, c: <9>{<>: <10>(Name: string)-><11>{name: <10>.Name, y: 1}, foo: <12>{name: "foo", y: 1}, bar: <13>{name: "bar", y: 1}}}`,
	}, {
		desc: "range unification",
		in: `
			// with concrete values
			a1: >=1 & <=5 & 3
			a2: >=1 & <=5 & 1
			a3: >=1 & <=5 & 5
			a4: >=1 & <=5 & 6
			a5: >=1 & <=5 & 0

			a6: 3 & >=1 & <=5
			a7: 1 & >=1 & <=5
			a8: 5 & >=1 & <=5
			a9: 6 & >=1 & <=5
			a10: 0 & >=1 & <=5

			// with ranges
			b1: >=1 & <=5 & >=1 & <=5
			b2: >=1 & <=5 & >=1 & <=1
			b3: >=1 & <=5 & >=5 & <=5
			b4: >=1 & <=5 & >=2 & <=3
			b5: >=1 & <=5 & >=3 & <=9
			b6: >=1 & <=5 & >=5 & <=9
			b7: >=1 & <=5 & >=6 & <=9

			b8: >=1 & <=5 & >=1 & <=5
			b9: >=1 & <=1 & >=1 & <=5
			b10: >=5 & <=5 & >=1 & <=5
			b11: >=2 & <=3 & >=1 & <=5
			b12: >=3 & <=9 & >=1 & <=5
			b13: >=5 & <=9 & >=1 & <=5
			b14: >=6 & <=9 & >=1 & <=5

			// ranges with more general types
			c1: int & >=1 & <=5
			c2: >=1 & <=5 & int
			c3: string & >=1 & <=5
			c4: >=1 & <=5 & string

			// other types
			s1: >="d" & <="z" & "e"
			s2: >="d" & <="z" & "ee"

			n1: number & >=1 & <=2
			n2: int & >=1.1 & <=1.3
			n3: >=1.0 & <=3.0 & 2
			n4: >=0.0 & <=0.1 & 0.09999
			n5: >=1 & <=5 & 2.5
			`,
		out: `<0>{` +
			`a1: 3, ` +
			`a2: 1, ` +
			`a3: 5, ` +
			`a4: _|_((<=5 & 6):invalid value 6 (out of bound <=5)), ` +
			`a5: _|_((>=1 & 0):invalid value 0 (out of bound >=1)), ` +
			`a6: 3, ` +
			`a7: 1, ` +
			`a8: 5, ` +

			`a9: _|_((<=5 & 6):invalid value 6 (out of bound <=5)), ` +
			`a10: _|_((>=1 & 0):invalid value 0 (out of bound >=1)), ` +

			`b1: (>=1 & <=5), ` +
			`b2: 1, ` +
			`b3: 5, ` +
			`b4: (>=2 & <=3), ` +
			`b5: (>=3 & <=5), ` +
			`b6: 5, ` +
			`b7: _|_(conflicting bounds >=6 and <=5), ` +
			`b8: (>=1 & <=5), ` +
			`b9: 1, ` +
			`b10: 5, ` +
			`b11: (>=2 & <=3), ` +
			`b12: (>=3 & <=5), ` +
			`b13: 5, ` +
			`b14: _|_(conflicting bounds >=6 and <=5), ` +
			`c1: (int & >=1 & <=5), ` +
			`c2: (<=5 & int & >=1), ` +
			`c3: _|_((string & >=1):conflicting values string and >=1 (mismatched types string and number)), ` +
			`c4: _|_(((>=1 & <=5) & string):conflicting values (>=1 & <=5) and string (mismatched types number and string)), ` +
			`s1: "e", ` +
			`s2: "ee", ` +
			`n1: (>=1 & <=2), ` +
			`n2: _|_(conflicting bounds int & >=1.1 and <=1.3), ` +
			`n3: 2, ` +
			`n4: 0.09999, ` +
			`n5: 2.5}`,
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
			`e1: _|_((int & <=32767 & 100000):invalid value 100000 (out of bound int & <=32767))}`,
	}, {
		desc: "struct comprehensions",
		in: `
			obj foo a: "bar"
			obj <Name>: {
				a: *"dummy" | string
				if true {
					sub as: a
				}
			}

			for k, v in { def :: 1, opt?: 2, _hid: 3, reg: 4 } {
				"\(k)": v
			}
		`,
		out: `<0>{obj: <1>{<>: <2>(Name: string)-><3>{a: (*"dummy" | string) if true yield <4>{sub: <5>{as: <3>.a}}}, foo: <6>{a: "bar", sub: <7>{as: "bar"}}}, reg: 4}`,
	}, {
		desc: "builtins",
		in: `
		a1: {
			a: and([b, c])
			b: =~"oo"
			c: =~"fo"
		}
		a2: a1 & { a: "foo" }
		a3: a1 & { a: "bar" }

		o1: {
			a: or([b, c])
			b: string
			c: "bar"
		}
		o2: o1 & { a: "foo" }
		o3: o1 & { a: "foo", b: "baz" }
		`,
		out: `<0>{` +
			`a1: <1>{a: (=~"oo" & =~"fo"), b: =~"oo", c: =~"fo"}, ` +
			`a2: <2>{a: "foo", b: =~"oo", c: =~"fo"}, ` +
			`a3: <3>{a: _|_((=~"oo" & "bar"):invalid value "bar" (does not match =~"oo")), b: =~"oo", c: =~"fo"}, ` +
			`o1: <4>{a: string, b: string, c: "bar"}, ` +
			`o2: <5>{a: "foo", b: string, c: "bar"}, ` +
			`o3: <6>{a: _|_(("baz" & "foo"):empty disjunction: conflicting values "baz" and "foo";("bar" & "foo"):empty disjunction: conflicting values "bar" and "foo"), b: "baz", c: "bar"}}`,
	}, {
		desc: "self-reference cycles conflicts with strings",
		in: `
			a: {
				x: y+"?"
				y: x+"!"
			}
			a x: "hey"
		`,
		out: `<0>{a: <1>{x: _|_(("hey!?" & "hey"):conflicting values "hey!?" and "hey"), y: "hey!"}}`,
	}, {
		desc: "resolved self-reference cycles with disjunctions",
		in: `
			a: b&{x:1} | {y:1}  // {x:1,y:3,z:2} | {y:1}
			b: {x:2} | c&{z:2}  // {x:2} | {x:1,y:3,z:2}
			c: a&{y:3} | {z:3}  // {x:1,y:3,z:2} | {z:3}
		`,
		out: `<0>{a: (<1>{x: 1, y: 3, z: 2} | <2>{y: 1}), b: (<3>{x: 2} | <4>{x: 1, y: 3, z: 2}), c: (<5>{x: 1, y: 3, z: 2} | <6>{z: 3})}`,
	}, {
		// We take a very conservative stance on delaying arithmetic
		// expressions within disjunctions. It should remain resolvable, though,
		// once the user specifies one.
		desc: "resolved self-reference cycles with disjunction",
		in: `
			// The second disjunct in xa1 is not resolvable and can be
			// eliminated:
			//   xa4 & 9
			//   (xa2 + 2) & 9
			//   ((xa3 + 2) + 2) & 9
			//   (((6 & xa1-2) + 2) + 2) & 9
			//   ((6 + 2) + 2) & 9 // 6 == xa1-2
			//   10 & 9 => _|_
			// The remaining values resolve.
			xa1: (xa2 & 8) | (xa4 & 9)
			xa2: xa3 + 2
			xa3: 6 & xa1-2
			xa4: xa2 + 2

			// The second disjunct in xb4 can be eliminated as both disjuncts
			// of xb3 result in an incompatible sum when substituted.
			xb1: (xb2 & 8) | (xb4 & 9)
			xb2: xb3 + 2
			xb3: (6 & (xb1-2)) | (xb4 & 9)
			xb4: xb2 + 2

			// Another variant with more disjunctions. xc1 remains with two
			// possibilities. Technically, only the first value is valid.
			// However, to fully determine that, all options of the remaining
			// disjunction will have to be evaluated algebraically, which is
			// not done.
			xc1: xc2 & 8 | xc4 & 9 | xc5 & 9
			xc2: xc3 + 2
			xc3: 6 & xc1-2
			xc4: xc2 + 1
			xc5: xc2 + 2

			// The above is resolved by setting xd1 explicitly.
			xd1: xd2 & 8 | xd4 & 9 | xd5 & 9
			xd2: xd3 + 2
			xd3: 6 & xd1-2
			xd4: xd2 + 1
			xd5: xd2 + 2
			xd1: 8

			// The above is resolved by setting xd1 explicitly to the wrong
			// value, resulting in an error.
			xe1: xe2 & 8 | xe4 & 9 | xe5 & 9
			xe2: xe3 + 2
			xe3: 6 & xe1-2
			xe4: xe2 + 1
			xe5: xe2 + 2
			xe1: 9

			// Only one solution.
			xf1: xf2 & 8 | xf4 & 9
			xf2: xf3 + 2
			xf3: 6 & xf1-2 | xf4 & 9
			xf4: xf2 + 2

			z1: z2 + 1 | z3 + 5
			z2: z3 + 2
			z3: z1 - 3
			z3: 8
		`,
		out: `<0>{` +
			`xa1: 8, ` +
			`xa2: 8, ` +
			`xa4: 10, ` +
			`xa3: 6, ` +

			`xb1: 8, ` +
			`xb2: 8, ` +
			`xb4: 10, ` +
			`xb3: 6, ` +

			`xc1: ((<1>.xc2 & 8) | (<1>.xc4 & 9) | (<1>.xc5 & 9)), ` +
			`xc2: (<1>.xc3 + 2), ` +
			`xc4: (<1>.xc2 + 1), ` +
			`xc5: (<1>.xc2 + 2), ` +
			`xc3: (6 & (<1>.xc1 - 2)), ` +

			`xd1: 8, ` +
			`xd2: 8, ` +
			`xd4: 9, ` +
			`xd5: 10, ` +
			`xd3: 6, ` +

			`xe1: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe2: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe4: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe5: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe3: _|_((6 & 7):conflicting values 6 and 7), ` +

			`xf1: 8, ` +
			`xf2: 8, ` +
			`xf4: 10, ` +
			`xf3: 6, ` +

			`z1: ((<1>.z2 + 1) | (<1>.z3 + 5)), ` +
			`z2: (<1>.z3 + 2), ` +
			`z3: ((<1>.z1 - 3) & 8)}`,
	}, {
		// Defaults should not alter the result of the above disjunctions.
		// The results may differ, but errors and resolution should be roughly
		// the same.
		desc: "resolved self-reference cycles with disjunction with defaults",
		in: `
			// The disjunction in xa could be resolved, but as disjunctions
			// are not resolved for expression, it remains unresolved.
			xa1: (xa2 & 8) | *(xa4 & 9)
			xa2: xa3 + 2
			xa3: 6 & xa1-2
			xa4: xa2 + 2

			// As xb3 is a disjunction, xb2 cannot be resolved and evaluating
			// the cycle completely is broken. However, it is not an error
			// as the user might still resolve the disjunction.
			xb1: *(xb2 & 8) | (xb4 & 9)
			xb2: xb3 + 2
			xb3: *(6 & (xb1-2)) | (xb4 & 9)
			xb4: xb2 + 2

			// Another variant with more disjunctions. xc1 remains with two
			// possibilities. Technically, only the first value is valid.
			// However, to fully determine that, all options of the remaining
			// disjunction will have to be evaluated algebraically, which is
			// not done.
			xc1: *(xc2 & 8) | (xc4 & 9) | (xc5 & 9)
			xc2: xc3 + 2
			xc3: 6 & xc1-2
			xc4: xc2 + 1
			xc5: xc2 + 2

			// The above is resolved by setting xd1 explicitly.
			xd1: *(xd2 & 8) | xd4 & 9 | xd5 & 9
			xd2: xd3 + 2
			xd3: 6 & xd1-2
			xd4: xd2 + 1
			xd5: xd2 + 2

			// The above is resolved by setting xd1 explicitly to the wrong
			// value, resulting in an error.
			xe1: *(xe2 & 8) | xe4 & 9 | xe5 & 9
			xe2: xe3 + 2
			xe3: 6 & xe1-2
			xe4: xe2 + 1
			xe5: xe2 + 2
			xe1: 9

			z1: *(z2 + 1) | z3 + 5
			z2: z3 + 2
			z3: z1 - 3
			z3: 8
		`,
		out: `<0>{` +
			`xa1: 8, ` +
			`xa2: 8, ` +
			`xa4: 10, ` +
			`xa3: 6, ` +

			`xb1: 8, ` +
			`xb2: 8, ` +
			`xb4: 10, ` +
			`xb3: 6, ` +

			`xc1: (*8 | 9), ` + // not resolved because we use evalPartial
			`xc2: 8, ` +
			`xc4: 9, ` +
			`xc5: 10, ` +
			`xc3: 6, ` +

			`xd1: (*8 | 9), ` + // TODO: eliminate 9?
			`xd2: 8, ` +
			`xd4: 9, ` +
			`xd5: 10, ` +
			`xd3: 6, ` +

			`xe1: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe2: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe4: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe5: _|_((6 & 7):conflicting values 6 and 7), ` +
			`xe3: _|_((6 & 7):conflicting values 6 and 7), ` +

			`z1: (*11 | 13), ` + // 13 is eliminated with evalFull
			`z2: 10, ` +
			`z3: 8}`,
	}}
	rewriteHelper(t, testCases, evalPartial)
}

func TestFullEval(t *testing.T) {
	testCases := []testCase{{
		desc: "detect conflicting value",
		in: `
				a: 8000.9
				a: 7080 | int`,
		out: `<0>{a: _|_((8000.9 & (int | int)):conflicting values 8000.9 and int (mismatched types float and int))}`, // TODO: fix repetition
	}, {
		desc: "conflicts in optional fields are okay ",
		in: `
			d: {a: 1, b?: 3} | {a: 2}

			// the following conjunction should not eliminate any disjuncts
			c: d & {b?:4}
		`,
		out: `<0>{d: (<1>{a: 1, b?: 3} | <2>{a: 2}), c: (<3>{a: 1, b?: (3 & 4)} | <4>{a: 2, b?: 4})}`,
	}, {
		desc: "resolve all disjunctions",
		in: `
			service <Name>: {
				name: string | *Name
				port: int | *7080
			}
			service foo: _
			service bar: { port: 8000 }
			service baz: { name: "foobar" }
			`,
		out: `<0>{service: <1>{<>: <2>(Name: string)-><3>{name: (string | *<2>.Name), port: (int | *7080)}, foo: <4>{name: "foo", port: 7080}, bar: <5>{name: "bar", port: 8000}, baz: <6>{name: "foobar", port: 7080}}}`,
	}, {
		desc: "field templates",
		in: `
			a: {
				<name>: int
				k: 1
			}
			b: {
				<X>: { x: 0, y: *1 | int }
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
		out: `<0>{a: <1>{<>: <2>(name: string)->int, k: 1}, b: <3>{<>: <4>(X: string)->(<5>{x: 0, y: (*1 | int)} & <6>{}), v: <7>{x: 0, y: 1}, w: <8>{x: 0, y: 0}}, c: <9>{<>: <10>(Name: string)-><11>{name: <10>.Name, y: 1}, foo: <12>{name: "foo", y: 1}, bar: <13>{name: "bar", y: 1}}}`,
	}, {
		desc: "field comprehension",
		in: `
			a: {
				for k, v in b
				if k < "d"
				if v > b.a {
					"\(k)": v
				}
			}
			b: {
				a: 1
				b: 2
				c: 3
				d: 4
			}
			c: {
				for k, v in b
				if k < "d"
				if v > b.a {
					"\(k)": v
				}
			}
			`,
		out: `<0>{a: <1>{b: 2, c: 3}, b: <2>{a: 1, b: 2, c: 3, d: 4}, c: <3>{b: 2, c: 3}}`,
	}, {
		desc: "conditional field",
		in: `
			if b {
				a: "foo"
			}
			b: true
			c: {
				a: 3
				if a > 1 {
					a: 3
				}
			}
			d: {
				a: int
				if a > 1 {
					a: 3
				}
			}
		`,
		// NOTE: the node numbers are not correct here, but this is an artifact
		// of the testing code.
		out: `<0>{b: true, a: "foo", c: <1>{a: 3}, d: <2>{a: int if (<3>.a > 1) yield <4>{a: 3}}}`,
	}, {
		desc: "referencing field in field comprehension",
		in: `
		a: { b c: 4 }
		a: {
			b d: 5
			for k, v in b {
				"\(k)": v
			}
		}
		`,
		out: `<0>{a: <1>{b: <2>{c: 4, d: 5}, c: 4, d: 5}}`,
	}, {
		desc: "different labels for templates",
		in: `
		a <X>: { name: X }
		a <Name>: { name: Name }
		a foo: {}
		`,
		out: `<0>{a: <1>{<>: <2>(X: string)->(<3>{name: <2>.X} & <4>{name: <2>.X}), foo: <5>{name: "foo"}}}`,
	}, {
		// TODO: rename EE and FF to E and F to check correct ordering.

		desc: "nested templates in one field",
		in: `
			a <A> b <B>: {
				name: A
				kind: B
			}
			a "A" b "B": _
			a "C" b "D": _
			a "EE" b "FF": { c: "bar" }
		`,
		out: `<0>{a: <1>{<>: <2>(A: string)-><3>{b: <4>{<>: <5>(B: string)-><6>{name: <2>.A, kind: <5>.B}, }}, ` +
			`A: <7>{b: <8>{<>: <9>(B: string)-><10>{name: <11>.A, kind: <9>.B}, ` +
			`B: <12>{name: "A", kind: "B"}}}, ` +
			`C: <13>{b: <14>{<>: <15>(B: string)-><16>{name: <17>.A, kind: <15>.B}, ` +
			`D: <18>{name: "C", kind: "D"}}}, ` +
			`EE: <19>{b: <20>{<>: <21>(B: string)-><22>{name: <23>.A, kind: <21>.B}, ` +
			`FF: <24>{name: "EE", kind: "FF", c: "bar"}}}}}`,
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
			`E: <5>{name: "E", kind: "E", c: "bar"}, ` +
			`A: <6>{name: "A", kind: "A"}, ` +
			`C: <7>{name: "C", kind: "C"}}}`,
	}, {
		desc: "field comprehensions with multiple keys",
		in: `
			for x in [
				{a: "A", b: "B" },
				{a: "C", b: "D" },
				{a: "E", b: "F" },
			] {
				a "\(x.a)" b "\(x.b)": x
			}

			for x in [
				{a: "A", b: "B" },
				{a: "C", b: "D" },
				{a: "E", b: "F" },
			] {
				"\(x.a)" "\(x.b)": x
			}
			`,
		out: `<0>{E: <1>{F: <2>{a: "E", b: "F"}}, ` +
			`a: <3>{` +
			`E: <4>{b: <5>{F: <6>{a: "E", b: "F"}}}, ` +
			`A: <7>{b: <8>{B: <9>{a: "A", b: "B"}}}, ` +
			`C: <10>{b: <11>{D: <12>{a: "C", b: "D"}}}}, ` +
			`A: <13>{B: <14>{a: "A", b: "B"}}, ` +
			`C: <15>{D: <16>{a: "C", b: "D"}}}`,
		// TODO: this order would be desirable.
		// out: `<0>{a: <1>{` +
		// 	`A: <2>{b: <3>{B: <4>{a: "A", b: "B"}}}, ` +
		// 	`C: <5>{b: <6>{D: <7>{a: "C", b: "D"}}}, ` +
		// 	`E: <8>{b: <9>{F: <10>{a: "E", b: "F"}}}}, ` +
		// 	`A: <11>{B: <12>{a: "A", b: "B"}}, ` +
		// 	`C: <13>{D: <14>{a: "C", b: "D"}}, ` +
		// 	`E: <15>{F: <16>{a: "E", b: "F"}}}`,
	}, {
		desc: "field comprehensions with templates",
		in: `
			num: 1
			a: {
				if num < 5 {
					<A> <B>: {
						name: A
						kind: B
					}
				}
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
			l: *[ int, int ] | [ string, string ]

			l1: [ "a", "b" ]
			l2: l & [ "c", "d" ]
			`,
		out: `<0>{l: [int,int], l1: ["a","b"], l2: ["c","d"]}`,
	}, {
		desc: "normalization",
		in: `
			a: string | string
			b: *1 | *int
			c: *1.0 | *float
		`,
		out: `<0>{a: string, b: int, c: float}`,
	}, {
		desc: "default disambiguation and elimination",
		in: `
		a: *1 | int
		b: *3 | int
		c: a & b
		d: b & a

		e: *1 | *1
		`,
		out: `<0>{a: 1, b: 3, c: int, d: int, e: 1}`,
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
				name: *Name | string
				type: "service"
				port: *7080 | int
			}
			service foo: {}
			service bar: { port: 8000 }
			service baz: { name: "foobar" }
			`,
		out: `<0>{result: [` +
			`<1>{name: "foo", type: "service", port: 7080},` +
			`<2>{name: "bar", type: "service", port: 8000},` +
			`<3>{name: "foobar", type: "service", port: 7080}], ` +

			`service: <4>{` +
			`<>: <5>(Name: string)-><6>{name: (*<5>.Name | string), type: "service", port: (*7080 | int)}, ` +
			`foo: <7>{name: "foo", type: "service", port: 7080}, ` +
			`bar: <8>{name: "bar", type: "service", port: 8000}, ` +
			`baz: <9>{name: "foobar", type: "service", port: 7080}}}`,
	}, {
		desc: "resolutions in struct comprehension keys",
		in: `
			a: { for _, b in ["c"] { "\(b + ".")": "a" } }
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
		out: `<0>{` +
			`l: [<1>{c: "t", d: "t"}], ` +
			`a: <2>{c: "t", d: "t"}, ` +
			`b: <3>{c: string, d: string}, ` +
			`l1: [<4>{c: "t", d: "st"}], ` +
			`a1: <5>{c: "t", d: "st"}, ` +
			`b1: <6>{c: string, d: ("s" + <7>.c)}}`,
	}, {
		desc: "ips",
		in: `
		IP: 4*[ uint8 ]

		Private:
			*[ 192, 168, uint8, uint8 ] |
			[ 10, uint8, uint8, uint8] |
			[ 172, >=16 & <=32, uint8, uint8 ]

		Inst: Private & [ _, 10, ... ]

		MyIP: Inst & [_, _, 10, 10 ]
		`,
		out: `<0>{` +
			`IP: [(int & >=0 & int & <=255),(int & >=0 & int & <=255),(int & >=0 & int & <=255),(int & >=0 & int & <=255)], ` +
			`Private: [192,168,(int & >=0 & int & <=255),(int & >=0 & int & <=255)], ` +
			`Inst: [10,10,(int & >=0 & int & <=255),(int & >=0 & int & <=255)], ` +
			`MyIP: [10,10,10,10]` +
			`}`,
	}, {
		desc: "complex interaction of groundness",
		in: `
			res: [ y & { d: "b" } for x in a for y in x ]
			res: [ a.b.c & { d: "b" } ]

			a b <C>: { d: string, s: "a" + d }
			a b c d: string
		`,
		// TODO(perf): unification should catch shared node.
		out: `<0>{res: [<1>{d: "b", s: "ab"}], ` +
			`a: <2>{b: <3>{<>: <4>(C: string)-><5>{d: string, s: ("a" + <5>.d)}, c: <6>{d: string, s: ("a" + <7>.d)}}}}`,
	}, {
		desc: "complex groundness 2",
		in: `
			r1: f1 & { y: "c" }

			f1: { y: string, res: a.b.c & { d: y } }

			a b c: { d: string, s: "a" + d }
			a b <C>: { d: string, s: "a" + d }
			a b c d: string
		`,
		out: `<0>{r1: <1>{y: "c", res: <2>{d: "c", s: "ac"}}, f1: <3>{y: string, res: <4>{d: string, s: (("a" + <5>.d) & ("a" + <5>.d))}}, a: <6>{b: <7>{<>: <8>(C: string)-><9>{d: string, s: ("a" + <9>.d)}, c: <10>{d: string, s: (("a" + <11>.d) & ("a" + <11>.d))}}}}`,
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
	}, {
		// TODO: A nice property for CUE to have would be that evaluation time
		// is proportional to the number of output nodes (note that this is
		// not the same as saying that the running time is O(n)).
		// We should probably disallow shenanigans like the one below. But until
		// this is allowed, it should at least be correct. At least we are not
		// making reentrant coding easy.
		desc: "reentrance",
		in: `
		// This indirection is needed to avoid binding references to fib
		// within fib to the instantiated version.
		fibRec: {nn: int, out: (fib & {n: nn}).out}
		fib: {
			n: int

			if n >= 2 {
				out: (fibRec & {nn: n - 2}).out + (fibRec & {nn: n - 1}).out
			}
			if n < 2 {
				out: n
			}
		}
		fib2: (fib & {n: 2}).out
		fib7: (fib & {n: 7}).out
		fib12: (fib & {n: 12}).out
		`,
		out: `<0>{` +
			`fibRec: <1>{` +
			`nn: int, ` +
			`out: (<2>.fib & <3>{n: <4>.nn}).out}, ` +
			// NOTE: the node numbers are not correct here, but this is an artifact
			// of the testing code.
			`fib: <5>{n: int if (<6>.n >= 2) yield <7>{out: ((<2>.fibRec & <8>{nn: (<6>.n - 2)}).out + (<2>.fibRec & <9>{nn: (<6>.n - 1)}).out)},  if (<6>.n < 2) yield <10>{out: <6>.n}}, ` +
			`fib2: 1, ` +
			`fib7: 13, ` +
			`fib12: 144}`,
	}, {
		desc: "Issue #23",
		in: `
			x: {a:1}|{a:2}
			y: x & {a:3}
		`,
		out: `<0>{x: (<1>{a: 1} | <2>{a: 2}), y: _|_((1 & 3):empty disjunction: conflicting values 1 and 3;(2 & 3):empty disjunction: conflicting values 2 and 3)}`,
	}, {
		desc: "cannot resolve references that would be ambiguous",
		in: `
		a1: *0 | 1
		a1: a3 - a2
		a2: *0 | 1
		a2: a3 - a1
		a3: 1

		b1: (*0 | 1) & b2
		b2: (0 | *1) & b1

		c1: (*{a:1} | {b:1}) & c2
		c2: (*{a:2} | {b:2}) & c1
		`,
		out: `<0>{` +
			`a1: ((*0 | 1) & (<1>.a3 - <1>.a2)), ` +
			`a3: 1, ` +
			`a2: ((*0 | 1) & (<1>.a3 - <1>.a1)), ` +
			`b1: (0 | 1), ` +
			`b2: (0 | 1), ` +
			`c1: (<2>{a: 1, b: 2} | <3>{a: 2, b: 1}), ` +
			`c2: (<4>{a: 2, b: 1} | <5>{a: 1, b: 2})}`,
	}, {
		desc: "don't convert incomplete errors to non-incomplete",
		in: `
		import "strings"

		n1: {min: <max, max: >min}
		n2: -num
		n3: +num
		n4: num + num
		n5: num - num
		n6: num * num
		n7: num / num

		b1: !is

		s1: "\(str)"
		s2: strings.ContainsAny("dd")
		s3: strings.ContainsAny(str, "dd")

		str: string
		num: <4
		is:  bool
		`,
		out: `<0>{` +
			`n1: <1>{min: <<2>.max, max: ><2>.min}, ` +
			`n2: -<3>.num, num: <4, ` +
			`n3: +<3>.num, ` +
			`n4: (<3>.num + <3>.num), ` +
			`n5: (<3>.num - <3>.num), ` +
			`n6: (<3>.num * <3>.num), ` +
			`n7: (<3>.num / <3>.num), ` +
			`b1: !<3>.is, ` +
			`is: bool, ` +
			`s1: ""+<3>.str+"", ` +
			`str: string, ` +
			`s2: strings.ContainsAny ("dd"), ` +
			`s3: <4>.ContainsAny (<3>.str,"dd")}`,
	}, {
		desc: "len of incomplete types",
		in: `
		args: *[] | [...string]
		v1: len(args)
		v2: len([])
		v3: len({})
		v4: len({a: 3})
		v5: len({a: 3} | {a: 4})
		v6: len('sf' | 'dd')
		v7: len([2] | *[1, 2])
		v8: len([2] | [1, 2])
		v9: len("😂")
		v10: len("")
		`,
		out: `<0>{` +
			`args: [], ` +
			`v1: 0, ` +
			`v2: 0, ` +
			`v3: 0, ` +
			`v4: 1, ` +
			`v5: len ((<1>{a: 3} | <2>{a: 4})), ` +
			`v6: len (('sf' | 'dd')), ` +
			`v7: 2, ` +
			`v8: len (([2] | [1,2])), ` +
			`v9: 4, ` +
			`v10: 0}`,
	}, {
		desc: "slice rewrite bug",
		in: `
		fn: {
			arg: [...int] & [1]
			out: arg[1:]
		}
		fn1: fn & {arg: [1]}
		`,
		out: `<0>{fn: <1>{arg: [1], out: []}, fn1: <2>{arg: [1], out: []}}`,
	}, {
		desc: "Issue #94",
		in: `
		foo: {
			opt?: 1
			"txt": 2
			def :: 3
			regular: 4
			_hidden: 5
		}
		comp: { for k, v in foo { "\(k)": v } }
		select: {
			opt: foo.opt
			"txt": foo.txt
			def :: foo.def
			regular: foo.regular
			_hidden: foo._hidden
		}
		index: {
			opt: foo["opt"]
			"txt": foo["txt"]
			def :: foo["def"]
			regular: foo["regular"]
			_hidden: foo["_hidden"]
		}
		`,
		out: `<0>{` +
			`foo: <1>{opt?: 1, txt: 2, def :: 3, regular: 4, _hidden: 5}, ` +
			`comp: <2>{txt: 2, regular: 4}, ` +
			`select: <3>{opt: <4>.foo.opt, txt: 2, def :: 3, regular: 4, _hidden: 5}, ` +
			`index: <5>{opt: <4>.foo["opt"], txt: 2, def :: _|_(<4>.foo["def"]:field "def" is a definition), regular: 4, _hidden: <4>.foo["_hidden"]}}`,
	}, {
		desc: "retain references with interleaved embedding",
		in: `
		a d: {
			base
			info :: {...}
			Y: info.X
		}

		base :: {
			info :: {...}
		}

		a <Name>: { info :: {
			X: "foo"
		}}
		`,
		out: `<0>{a: <1>{<>: <2>(Name: string)-><3>{info :: <4>C{X: "foo"}}, d: <5>C{info :: <6>C{X: "foo"}, Y: "foo"}}, base :: <7>C{info :: <8>{...}}}`,
	}, {
		desc: "comparison against bottom",
		in: `
		a: _|_ == _|_
		b: err == 1&2 // not a literal error, so not allowed
		c: err == _|_ // allowed
		d: err != _|_ // allowed
		e: err != 1&3
		// z: err == err // TODO: should infer to be true?

		err: 1 & 2
		`,
		out: `<0>{a: true, b: _|_((1 & 2):conflicting values 1 and 2), err: _|_((1 & 2):conflicting values 1 and 2), c: true, d: false, e: _|_((1 & 2):conflicting values 1 and 2)}`,
	}, {
		desc: "or builtin should not fail on non-concrete empty list",
		in: `
		Workflow :: {
			jobs: {
				<jobID>: {
				}
			}
			JobID :: or([ k for k, _ in jobs ])
		}

		foo: Workflow & {
			jobs foo: {
			}
		}
		`,
		out: `<0>{Workflow :: <1>C{jobs: <2>{<>: <3>(jobID: string)-><4>C{}, }, JobID :: or ([ <5>for k, _ in <6>.jobs yield <5>.k ])}, foo: <7>C{jobs: <8>{<>: <9>(jobID: string)-><10>C{}, foo: <11>C{}}, JobID :: "foo"}}`,
	}, {
		desc: "Issue #153",
		in: `
		Foo: {
			listOfCloseds: [...Closed]
		}
		
		Closed :: {
			a: int | *0
		}
		
		Junk: {
			b: 2
		}
		
		Foo & {
			listOfCloseds: [{
				for k, v in Junk {
					"\(k)": v
				}
			 }]
		}
		`,
		out: `<0>{<1>{listOfCloseds: [_|_(2:field "b" not allowed in closed struct)]}, Foo: <2>{listOfCloseds: []}, Closed :: <3>C{a: 0}, Junk: <4>{b: 2}}`,
	}, {
		in: `
		p: [ID=string]: { name: ID }
		A="foo=bar": "str"
		a: A
		B=bb: 4
		b1: B
		b1: bb
		C="\(a)": 5
		c: C
		`,
		out: `<0>{` +
			`p: <1>{<>: <2>(ID: string)-><3>{name: <2>.ID}, }, ` +
			`foo=bar: "str", ` +
			`a: "str", ` +
			`bb: 4, ` +
			`b1: 4, ` +
			`c: 5, ` +
			`str: 5}`,
	}}
	rewriteHelper(t, testCases, evalFull)
}

func TestX(t *testing.T) {
	t.Skip()

	// Don't remove. For debugging.
	testCases := []testCase{{
		in: `
		`,
	}}
	rewriteHelper(t, testCases, evalFull)
}
