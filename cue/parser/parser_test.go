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

package parser

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/astinternal"
)

func TestParse(t *testing.T) {
	type testCase struct {
		desc    string
		version string
		in, out string
	}
	testCases := []testCase{
		{
			desc: "ellipsis in structs",
			in: `#Def: {
			b: "2"
			...
		}
		...

		#Def2: {
			...
			b: "2"
		}
		#Def3: {...
		_}
		...
		`,
			out: `#Def: {b: "2", ...}, ..., #Def2: {..., b: "2"}, #Def3: {..., _}, ...`,
		},
		{
			desc: "empty file",
		},
		{
			desc: "empty struct",
			in:   "{}",
			out:  "{}",
		},
		{
			desc: "empty structs",
			in:   "{},{},",
			out:  "{}, {}",
		},
		{
			desc: "empty structs; elided comma",
			in:   "{}\n{}",
			out:  "{}, {}",
		},
		{
			desc: "basic lits",
			in:   `"a","b", 3,3.4,5,2_3`,
			out:  `"a", "b", 3, 3.4, 5, 2_3`,
		},
		{
			desc: "keyword basic lits",
			in:   `true,false,null,for,in,if,let,if`,
			out:  `true, false, null, for, in, if, let, if`,
		},
		{
			desc: "keyword basic newline",
			in: `
		true
		false
		null
		for
		in
		if
		let
		if
		`,
			out: `true, false, null, for, in, if, let, if`,
		},
		{
			desc: "keywords as labels",
			in: `if: 0, for: 1, in: 2, where: 3, div: 4, quo: 5, func: 6
		for: if: func: let: 3
		`,
			out: `if: 0, for: 1, in: 2, where: 3, div: 4, quo: 5, func: 6, for: {if: {func: {let: 3}}}`,
		},
		{
			desc: "keywords as optional labels",
			in: `if?: 0, for?: 1, in?: 2, where?: 3, div?: 4, quo?: 5, func?: 6
		for?: if?: func?: let?: 3
		`,
			out: `if?: 0, for?: 1, in?: 2, where?: 3, div?: 4, quo?: 5, func?: 6, for?: {if?: {func?: {let?: 3}}}`,
		},
		{
			desc: "keywords as required labels",
			in: `if!: 0, for!: 1, in!: 2, where!: 3, div!: 4, quo!: 5, func!: 6
		for!: if!: func!: let!: 3
		`,
			out: `if!: 0, for!: 1, in!: 2, where!: 3, div!: 4, quo!: 5, func!: 6, for!: {if!: {func!: {let!: 3}}}`,
		},
		{
			desc: "keywords as alias",
			in: `if=foo: 0
		for=bar: 2
		let=bar: 3
		func=baz: 4
		`,
			out: `if=foo: 0, for=bar: 2, let=bar: 3, func=baz: 4`,
		},
		{
			desc: "keywords as selector",
			in: `a : {
			if: 0
			for: 1
			in: 2
			where: 3
			div: 4
			quo: 5
			func: 6
			float: 7
			null: if: func: let: 3
		}, b: [
			a.if,
			a.for,
			a.in,
			a.where,
			a.div,
			a.quo,
			a.func,
			a.float,
			a.null.if.func.let,
		]`,
			out: `a: {if: 0, for: 1, in: 2, where: 3, div: 4, quo: 5, func: 6, float: 7, null: {if: {func: {let: 3}}}}, b: [a.if, a.for, a.in, a.where, a.div, a.quo, a.func, a.float, a.null.if.func.let]`,
		},
		{
			desc: "json",
			in: `{
			"a": 1,
			"b": "2",
			"c": 3
		}`,
			out: `{"a": 1, "b": "2", "c": 3}`,
		},
		{
			desc: "json:extra comma",
			in: `{
			"a": 1,
			"b": "2",
			"c": 3,
		}`,
			out: `{"a": 1, "b": "2", "c": 3}`,
		},
		{
			desc: "json:simplified",
			in: `{
			a: 1
			b: "2"
			c: 3
		}`,
			out: `{a: 1, b: "2", c: 3}`,
		},
		{
			desc: "attributes",
			in: `a: 1 @xml(,attr)
		 b: 2 @foo(a,b=4) @go(Foo)
		 c: {
			 d: "x" @go(D) @json(,omitempty)
			 e: "y" @ts(,type=string,"str")
		 }`,
			out: `a: 1 @xml(,attr), b: 2 @foo(a,b=4) @go(Foo), c: {d: "x" @go(D) @json(,omitempty), e: "y" @ts(,type=string,"str")}`,
		},
		{
			desc: "not emitted",
			in: `a: true
		 b?: "2"
		 c?: 3

		 d!: 2
		 e: f!: 3

		 "g\("en")"?: 4
		 "h\("en")"!: 4
		`,
			out: `a: true, b?: "2", c?: 3, d!: 2, e: {f!: 3}, "g\("en")"?: 4, "h\("en")"!: 4`,
		},
		{
			desc: "definition",
			in: `#Def: {
			 b: "2"
			 c: 3

			 embedding
		}
		#Def: {}
		`,
			out: `#Def: {b: "2", c: 3, embedding}, #Def: {}`,
		},
		{
			desc: "one-line embedding",
			in:   `{ V1, V2 }`,
			out:  `{V1, V2}`,
		},
		{
			desc: "selectors",
			in:   `a.b. "str"`,
			out:  `a.b."str"`,
		},
		{
			desc: "selectors (dup)",
			in:   `a.b. "str"`,
			out:  `a.b."str"`,
		},
		{
			desc: "faulty bytes selector",
			in:   `a.b.'str'`,
			out:  "a.b._\nexpected selector, found 'STRING' 'str'",
		},
		{
			desc: "faulty multiline string selector",
			in: `a.b."""
			"""`,
			out: "a.b._\nexpected selector, found 'STRING' \"\"\"\n\t\t\t\"\"\"",
		},
		{
			desc: "expression embedding",
			in: `#Def: {
			a.b.c
			a > b < c
			-1<2

			foo: 2
		}`,
			out: `#Def: {a.b.c, a>b<c, -1<2, foo: 2}`,
		},
		{
			desc: "ellipsis in structs (dup)",
			in: `#Def: {
			b: "2"
			...
		}
		...

		#Def2: {
			...
			b: "2"
		}
		#Def3: {...
		_}
		...
		`,
			out: `#Def: {b: "2", ...}, ..., #Def2: {..., b: "2"}, #Def3: {..., _}, ...`,
		},
		{
			desc: "emitted referencing non-emitted",
			in: `a: 1
		 b: "2"
		 c: 3
		{ name: b, total: a + b }`,
			out: `a: 1, b: "2", c: 3, {name: b, total: a+b}`,
		},
		{
			desc: "package file",
			in: `package k8s
		 {}
		`,
			out: `package k8s, {}`,
		},
		{
			desc: "invalid package identifier: definition",
			in:   `package #x`,
			out: `package #x
invalid package name #x`,
		},
		{
			desc: "invalid package identifier: hidden definition",
			in:   `package _#x`,
			out: `package _#x
invalid package name _#x`,
		},
		{
			desc: "invalid import identifier: definition",
			in:   `import #x "foo"`,
			out: `import #x "foo"
cannot import package as definition identifier`,
		},
		{
			desc: "invalid import identifier: hidden definition",
			in:   `import _#x "foo"`,
			out: `import _#x "foo"
cannot import package as definition identifier`,
		},
		{
			desc: "imports group",
			in: `package k8s

		import (
			a "foo"
			"bar/baz"
		)
		`,
			out: `package k8s, import ( a "foo", "bar/baz" )`,
		},
		{
			desc: "imports single",
			in: `package k8s

		import a "foo"
		import "bar/baz"
			`,
			out: `package k8s, import a "foo", import "bar/baz"`,
		},
		{
			desc: "collapsed fields",
			in: `a: #b: c?: [Name=_]: d: 1
		"g\("en")"?: 4
		 // job foo { bar: 1 } // TODO error after foo
		 job: "foo": [_]: { bar: 1 }
		`,
			out: `a: {#b: {c?: {[Name=_]: {d: 1}}}}, "g\("en")"?: 4, job: {"foo": {[_]: {bar: 1}}}`,
		},
		{
			desc: "identifiers",
			in: `// 	$_: 1,
			a: {b: {c: d}}
			c: a
			d: a.b
			// e: a."b" // TODO: is an error
			e: a.b.c
			"f": f,
			[X=_]: X
		`,
			out: "a: {b: {c: d}}, c: a, d: a.b, e: a.b.c, \"f\": f, [X=_]: X",
		},
		{
			desc: "predeclared identifiers",
			in: `a:    __string
		__int: 2`,
			out: "a: __string, __int: 2\nidentifiers starting with '__' are reserved",
		},
		{
			desc: "reserved identifiers in let",
			in: `let __var = 42
		a: __var`,
			out: "let __var=42, a: __var\nidentifiers starting with '__' are reserved",
		},
		{
			desc: "reserved identifiers in comprehension",
			in:   `list: [for __x, y in [1] { __x }]`,
			out:  "list: [for __x: y in [1] {__x}]\nidentifiers starting with '__' are reserved",
		},
		{
			desc: "reserved identifiers in comprehension key-value",
			in:   `list: [for k, __v in {a: 1} { __v }]`,
			out:  "list: [for k: __v in {a: 1} {__v}]\nidentifiers starting with '__' are reserved",
		},
		{
			desc: "reserved identifiers in comprehension let",
			in:   `list: [for x in [1] let __temp = x { __temp }]`,
			out:  "list: [for x in [1] let __temp=x {__temp}]\nidentifiers starting with '__' are reserved",
		},
		{
			desc: "predeclared identifiers in struct embedding",
			in:   `a: { __int }`,
			out:  "a: {__int}",
		},
		{
			desc: "non-predeclared identifiers in struct embedding",
			in:   `a: { __myvar }`,
			out:  "a: {__myvar}",
		},
		{
			desc: "reserved identifiers in field alias",
			in:   `foo: __b=bar: {p: __b.baz}`,
			out:  "foo: {__b=bar: {p: __b.baz}}\nidentifiers starting with '__' are reserved",
		},
		{
			desc: "empty fields",
			in: `
		"": 3
		`,
			out: `"": 3`,
		},
		{
			desc: "expressions",
			in: `	a: (2 + 3) * 5
			b: (2 + 3) + 4
			c: 2 + 3 + 4
			d: -1
			e: !foo
			f: _|_
		`,
			out: "a: (2+3)*5, b: (2+3)+4, c: 2+3+4, d: -1, e: !foo, f: _|_",
		},
		{
			desc: "pseudo keyword expressions",
			in: `	a: (2 div 3) mod 5
			b: (2 quo 3) rem 4
			c: 2 div 3 div 4
		`,
			out: "a: (2 div 3) mod 5, b: (2 quo 3) rem 4, c: 2 div 3 div 4",
		},
		{
			desc: "ranges",
			in: `	a: >=1 & <=2
			b: >2.0  & <= 40.0
			c: >"a" & <="b"
			v: (>=1 & <=2) & <=(>=5 & <=10)
			w: >1 & <=2 & <=3
			d: >=3T & <=5M
		`,
			out: "a: >=1&<=2, b: >2.0&<=40.0, c: >\"a\"&<=\"b\", v: (>=1&<=2)&<=(>=5&<=10), w: >1&<=2&<=3, d: >=3T&<=5M",
		},
		{
			desc: "indices",
			in: `{
			a: b[2]
			b: c[1:2]
			c: "asdf"
			d: c ["a"]
		}`,
			out: `{a: b[2], b: c[1:2], c: "asdf", d: c["a"]}`,
		},
		{
			desc: "calls",
			in: `{
			a: b(a.b, c.d)
			b: a.b(c)
		}`,
			out: `{a: b(a.b, c.d), b: a.b(c)}`,
		},
		{
			desc: "lists",
			in: `{
			a: [ 1, 2, 3, b, c, ... ]
			b: [ 1, 2, 3, ],
			c: [ 1,
			 2,
			 3
			 ],
			d: [ 1+2, 2, 4,]
		}`,
			out: `{a: [1, 2, 3, b, c, ...], b: [1, 2, 3], c: [1, 2, 3], d: [1+2, 2, 4]}`,
		},
		{
			desc: "list types",
			in: `{
			a: 4*[int]
			b: <=5*[{a: 5}]
			c1: [...int]
			c2: [...]
			c3: [1, 2, ...int,]
		}`,
			out: `{a: 4*[int], b: <=5*[{a: 5}], c1: [...int], c2: [...], c3: [1, 2, ...int]}`,
		},
		{
			desc: "list comprehensions",
			in: `{
				y: [1,2,3]
				b: [for x in y if x == 1 { x }],
			}`,
			out: `{y: [1, 2, 3], b: [for x in y if x==1 {x}]}`,
		},
		{
			desc: "field comprehensions",
			in: `{
				y: { a: 1, b: 2}
				a: {
					for k, v in y if v > 2 {
						"\(k)": v
					}
				}
			 }`,
			out: `{y: {a: 1, b: 2}, a: {for k: v in y if v>2 {"\(k)": v}}}`,
		},
		{
			desc: "nested comprehensions",
			in: `{
			y: { a: 1, b: 2}
			a: {
				for k, v in y let x = v+2 if x > 2 {
					"\(k)": v
				}
			}
		}`,
			out: `{y: {a: 1, b: 2}, a: {for k: v in y let x=v+2 if x>2 {"\(k)": v}}}`,
		},
		{
			desc: "let declaration",
			in: `{
			let X = 42
			let Y = "42",
			let Z = 10 + 12
		}`,
			out: `{let X=42, let Y="42", let Z=10+12}`,
		},
		{
			desc: "duplicates allowed",
			in: `{
			a: b: 3
			a: { b: 3 }
		}`,
			out: "{a: {b: 3}, a: {b: 3}}",
		},
		{
			desc: "templates",
			in: `{
			[foo=_]: { a: int }
			a:     { a: 1 }
		}`,
			out: "{[foo=_]: {a: int}, a: {a: 1}}",
		},
		{
			desc: "value alias",
			in: `
		{
			a: X=foo
			b: Y={foo}
			c: d: e: X=5
		}
		`,
			out: `{a: X=foo, b: Y={foo}, c: {d: {e: X=5}}}`,
		},
		{
			desc: "dynamic labels",
			in: `{
			(x): a: int
			x:   "foo"
			a: {
				(a.b)
			}

			(x)?: 1
			y: (x)!: 2
		}`,
			out: `{(x): {a: int}, x: "foo", a: {(a.b)}, (x)?: 1, y: {(x)!: 2}}`,
		},
		{
			desc: "foo",
			in: `[
			[1],
			[1, 2],
			[1, 2, 3],
		]`,
			out: "[[1], [1, 2], [1, 2, 3]]",
		},
		{
			desc: "interpolation",
			in: `a: "foo \(ident)"
		 b: "bar \(bar)  $$$ "
		 c: "nest \(   { a: "\( nest ) "}.a ) \(5)"
		 m1: """
			 multi \(bar)
			 """
		 m2: '''
			 \(bar) multi
			 '''`,
			out: `a: "foo \(ident)", b: "bar \(bar)  $$$ ", c: "nest \({a: "\(nest) "}.a) \(5)", m1: """` + "\n\t\t\t multi \\(bar)\n\t\t\t \"\"\", m2: '''\n\t\t\t \\(bar) multi\n\t\t\t '''",
		},
		{
			desc: "file comments",
			in: `// foo

		// uni
		package foo // uniline

		// file.1
		// file.2

		`,
			out: "<[0// foo] <[d0// uni] [l3// uniline] [3// file.1 // file.2] package foo>>",
		},
		{
			desc: "line comments",
			in: `// doc
		 a: 5 // line
		 b: 6 // lineb
			  // next
			`,
			out: "<[d0// doc] [l5// line] a: 5>, <[l5// lineb] [5// next] b: 6>",
		},
		{
			desc: "alt comments",
			in: `// a ...
		a: 5 // line a

		// about a

		// b ...
		b: // lineb
		  6

		// about b

		c: 7

		// about c

		// about d
		d:
			// about e
			e: 3
		`,
			out: "<[d0// a ...] [l5// line a] [5// about a] a: 5>, " +
				"<[d0// b ...] [l2// lineb] [5// about b] b: 6>, " +
				"<[5// about c] c: 7>, " +
				"<[d0// about d] d: {<[d0// about e] e>: 3}>",
		},
		{
			desc: "expr comments",
			in: `
		a: 2 +  // 2 +
		   3 +  // 3 +
		   4    // 4
		   `,
			out: "<[l5// 4] a: <[l2// 3 +] <[l2// 2 +] 2+3>+4>>",
		},
		{
			desc: "composit comments",
			in: `a : {
			a: 1, b: 2, c: 3, d: 4
			// end
		}
		b: [
			1, 2, 3, 4, 5,
			// end
		]
		c: [ 1, 2, 3, 4, // here
			{ a: 3 }, // here
			5, 6, 7, 8 // and here
		]
		d: {
			a: 1 // Hello
			// Doc
			b: 2
		}
		e1: [
			// comment in list body
		]
		e2: {
			// comment in struct body
		}
		`,
			out: "a: {a: 1, b: 2, c: 3, <[d5// end] d: 4>}, " +
				"b: [1, 2, 3, 4, <[d2// end] 5>], " +
				"c: [1, 2, 3, <[l2// here] 4>, <[l4// here] {a: 3}>, 5, 6, 7, <[l2// and here] 8>], " +
				"d: {<[l5// Hello] a: 1>, <[d0// Doc] b: 2>}, " +
				"e1: <[d1// comment in list body] []>, " +
				"e2: <[d1// comment in struct body] {}>",
		},
		{
			desc: "attribute comments",
			in: `
		a: 1 @a() @b() // d
		`,
			out: `<[l5// d] a: 1 @a() @b()>`,
		},
		{
			desc: "attribute declarations",
			in: `
		@foo()

		package bar

		@bar()

		import "strings"

		@baz()
			`,
			out: `@foo(), package bar, @bar(), import "strings", @baz()`,
		},
		{
			desc: "comprehension comments",
			in: `
		if X {
			// Comment 1
			Field: 2
			// Comment 2
		}
		`,
			out: `if X <[d2// Comment 2] {<[d0// Comment 1] Field: 2>}>`,
		},
		{
			desc: "let comments",
			in:   `let X = foo // Comment 1`,
			out:  `<[5// Comment 1] let X=foo>`,
		},
		{
			desc: "emit comments",
			in: `// a comment at the beginning of the file

		// a second comment

		// comment
		a: 5

		{}

		// a comment at the end of the file
		`,
			out: "<[0// a comment at the beginning of the file] [0// a second comment] <[d0// comment] a: 5>, <[2// a comment at the end of the file] {}>>",
		},
		{
			desc: "composite comments 2",
			in: `
	{
// foo

// fooo
foo: 1

bar: 2
	}

[
	{"name": "value"}, // each element has a long
	{"name": "next"}   // optional next element
]
`,
			out: `{<[0// foo] [d0// fooo] foo: 1>, bar: 2}, [<[l4// each element has a long] {"name": "value"}>, <[l4// optional next element] {"name": "next"}>]`,
		},
		{
			desc: "field aliasing",
			in: `
		I="\(k)": v
		S="foo-bar": w
		L=foo: x
		X=[0]: {
			foo: X | null
		}
		[Y=string]: { name: Y }
		X1=[X2=<"d"]: { name: X2 }
		Y1=foo: Y2=bar: [Y1, Y2]
		`,
			out: `I="\(k)": v, S="foo-bar": w, L=foo: x, X=[0]: {foo: X|null}, [Y=string]: {name: Y}, X1=[X2=<"d"]: {name: X2}, Y1=foo: {Y2=bar: [Y1, Y2]}`,
		},
		{
			desc: "allow keyword in expression",
			in: `
		foo: in & 2
		`,
			out: "foo: in&2",
		},
		{
			desc: "dot import",
			in: `
		import . "foo"
		`,
			out: "import , \"foo\"\nexpected 'STRING', found '.'",
		},
		{
			desc: "attributes (2)",
			in: `
		package name

		@t1(v1)

		{
			@t2(v2)
		}
		a: {
			a: 1
			@t3(v3)
			@t4(v4)
			c: 2
		}
		`,
			out: "package name, @t1(v1), {@t2(v2)}, a: {a: 1, @t3(v3), @t4(v4), c: 2}",
		},
		{
			desc: "Issue #276",
			in: `
		a: int=>2
		`,
			out: "a: int=>2",
		},
		{
			desc: "struct comments",
			in: `
		struct: {
			// This is a comment

			// This is a comment

			// Another comment
			something: {
			}

			// extra comment
		}`,
			out: `struct: {<[0// This is a comment] [0// This is a comment] [d0// Another comment] [d5// extra comment] something: {}>}`,
		},
		{
			desc: "list comments",
			in: `
		list: [
			// Comment1

			// Comment2

			// Another comment
			{
			},

			// Comment 3
		]`,
			out: "list: [<[0// Comment1] [0// Comment2] [d0// Another comment] [d3// Comment 3] {}>]",
		},
		{
			desc: "call comments",
			in: `
		funcArg1: foo(
			{},

			// Comment1

			// Comment2
			{}

			// Comment3
		)`,
			out: "funcArg1: foo(<[1// Comment1] {}>, <[d0// Comment2] [d1// Comment3] {}>)",
		},
		{
			desc: "front-style commas",
			in: `
			frontStyle: { "key": "value"
				, "key2": "value2"
				, "foo" : bar
			}
			`,
			out: "frontStyle: {\"key\": \"value\", \"key2\": \"value2\", \"foo\": bar}",
		},
		{
			desc: "function types",
			in: `
			f0: func(): int
			f1: func(int): int
			f2: func(int, string): int
			f3: func({a: int, b: string}): bool
			f4: func(bool, func(int, string): int): string
			f5: func(int, int): func(bool, bool): bool
			f6: func(func(bool, bool): bool, func(string, string): string): func(int, func(int, string): int): func(int, string): int
		`,
			out: "f0: func(): int, f1: func(int): int, f2: func(int, string): int, f3: func({a: int, b: string}): bool, f4: func(bool, func(int, string): int): string, f5: func(int, int): func(bool, bool): bool, f6: func(func(bool, bool): bool, func(string, string): string): func(int, func(int, string): int): func(int, string): int",
		},
		{
			desc: "postfix ... operator with experiment",
			in: `@experiment(explicitopen)
		x: y...
		a: foo.bar...
		b: (c & d)...
		e: fn()...`,
			out: "@experiment(explicitopen), x: y..., a: foo.bar..., b: (c&d)..., e: fn()...",
		},
		{
			desc: "postfix ... operator with experiment missing",
			in: `
		x: y...
		`,
			out: "x: <*ast.BadExpr>\npostfix ... operator requires @experiment(explicitopen)",
		},
		{
			desc:    "postfix ... operator unsupported version",
			version: "v0.14.0",
			in: `@experiment(explicitopen)
		x: y...
		`,
			out: "\nparsing experiments for version \"v0.14.0\": cannot set experiment \"explicitopen\" before version v0.15.0",
		},
		{
			desc: "try clauses",
			in: `@experiment(try)
		{
			a: [for x in [1, 2] try { x }]
			b: [for x in [1, 2] try y = x { y }]
		}`,
			out: `@experiment(try), {a: [for x in [1, 2] try {x}], b: [for x in [1, 2] try y=x {y}]}`,
		}, {
			desc: "postfix ? operator",
			in: `{
			a: b?
			c: d.x?
		}`,
			out: `{a: b?, c: d.x?}`,
		}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			mode := []Option{AllErrors}
			if strings.Contains(tc.desc, "comments") {
				mode = append(mode, ParseComments)
			}
			if strings.Contains(tc.desc, "function") {
				mode = append(mode, ParseFuncs)
			}
			if tc.version != "" {
				mode = append(mode, Version(tc.version))
			}
			f, err := ParseFile("input", tc.in, mode...)
			got := astinternal.DebugStr(f)
			if err != nil {
				got += "\n" + err.Error()
			}
			if got != tc.out {
				t.Errorf("\ngot  %q;\nwant %q", got, tc.out)
			}
		})
	}
}

func TestStrict(t *testing.T) {
	testCases := []struct{ desc, in string }{
		{"block comments",
			`a: 1 /* a */`},
		{"space separator",
			`a b c: 2`},
		{"reserved identifiers",
			`__foo: 3`},
		{"old-style alias 1",
			`X=3`},
		{"old-style alias 2",
			`X={}`},

		// Not yet supported
		{"additional typed not yet supported",
			`{...int}`},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := ParseFile("input", tc.in, AllErrors, ParseComments)
			if err == nil {
				t.Errorf("unexpected success: %v", tc.in)
			}
		})
	}
}

// parseExprString is a convenience function for obtaining the AST of an
// expression x. The position information recorded in the AST is undefined. The
// filename used in error messages is the empty string.
func parseExprString(x string) (ast.Expr, error) {
	return ParseExpr("", []byte(x))
}

func TestParseExpr(t *testing.T) {
	// just kicking the tires:
	// a valid arithmetic expression
	src := "a + b"
	x, err := parseExprString(src)
	if err != nil {
		t.Errorf("ParseExpr(%q): %v", src, err)
	}
	// sanity check
	if _, ok := x.(*ast.BinaryExpr); !ok {
		t.Errorf("ParseExpr(%q): got %T, want *BinaryExpr", src, x)
	}

	// an invalid expression
	src = "a + *"
	if _, err := parseExprString(src); err == nil {
		t.Errorf("ParseExpr(%q): got no error", src)
	}

	// a comma is not permitted unless automatically inserted
	src = "a + b\n"
	if _, err := parseExprString(src); err != nil {
		t.Errorf("ParseExpr(%q): got error %s", src, err)
	}
	src = "a + b;"
	if _, err := parseExprString(src); err == nil {
		t.Errorf("ParseExpr(%q): got no error", src)
	}

	// check resolution
	src = "{ foo: bar, bar: foo }"
	x, err = parseExprString(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", src, err)
	}
	for _, d := range x.(*ast.StructLit).Elts {
		v := d.(*ast.Field).Value.(*ast.Ident)
		if v.Scope == nil {
			t.Errorf("ParseExpr(%q): scope of field %v not set", src, v.Name)
		}
		if v.Node == nil {
			t.Errorf("ParseExpr(%q): scope of node %v not set", src, v.Name)
		}
	}

	// various other stuff following a valid expression
	const validExpr = "a + b"
	const anything = "dh3*#D)#_"
	for _, c := range "!)]};," {
		src := validExpr + string(c) + anything
		if _, err := parseExprString(src); err == nil {
			t.Errorf("ParseExpr(%q): got no error", src)
		}
	}

	// ParseExpr must not crash
	for _, src := range valids {
		_, _ = parseExprString(src)
	}
}

func TestImports(t *testing.T) {
	var imports = map[string]bool{
		`"a"`:        true,
		`"a/b"`:      true,
		`"a.b"`:      true,
		`'m\x61th'`:  true,
		`"greek/αβ"`: true,
		`""`:         false,

		// Each of these pairs tests both #""# vs "" strings
		// and also use of invalid characters spelled out as
		// escape sequences and written directly.
		// For example `"\x00"` tests import "\x00"
		// while "`\x00`" tests import `<actual-NUL-byte>`.
		`#"a"#`:        true,
		`"\x00"`:       false,
		"'\x00'":       false,
		`"\x7f"`:       false,
		"`\x7f`":       false,
		`"a!"`:         false,
		"#'a!'#":       false,
		`"a b"`:        false,
		`#"a b"#`:      false,
		`"a\\b"`:       false,
		"#\"a\\b\"#":   false,
		"\"`a`\"":      false,
		"#'\"a\"'#":    false,
		`"\x80\x80"`:   false,
		"#'\x80\x80'#": false,
		`"\xFFFD"`:     false,
		"#'\xFFFD'#":   false,
	}
	for path, isValid := range imports {
		t.Run(path, func(t *testing.T) {
			src := fmt.Sprintf("package p, import %s", path)
			_, err := ParseFile("", src)
			switch {
			case err != nil && isValid:
				t.Errorf("ParseFile(%s): got %v; expected no error", src, err)
			case err == nil && !isValid:
				t.Errorf("ParseFile(%s): got no error; expected one", src)
			}
		})
	}
}

// TestIncompleteSelection ensures that an incomplete selector
// expression is parsed as a (blank) *SelectorExpr, not a
// *BadExpr.
func TestIncompleteSelection(t *testing.T) {
	for _, src := range []string{
		"{ a: fmt. }",         // at end of object
		"{ a: fmt.\n0.0: x }", // not at end of struct
	} {
		t.Run("", func(t *testing.T) {
			f, err := ParseFile("", src)
			if err == nil {
				t.Fatalf("ParseFile(%s) succeeded unexpectedly", src)
			}

			const wantErr = "expected selector"
			if !strings.Contains(err.Error(), wantErr) {
				t.Errorf("ParseFile returned wrong error %q, want %q", err, wantErr)
			}

			var sel *ast.SelectorExpr
			ast.Walk(f, func(n ast.Node) bool {
				if n, ok := n.(*ast.SelectorExpr); ok {
					sel = n
				}
				return true
			}, nil)
			if sel == nil {
				t.Fatalf("found no *SelectorExpr: %#v %s", f.Decls[0], astinternal.DebugStr(f))
			}
			const wantSel = "&{fmt _ {<nil>} {{}}}"
			if fmt.Sprint(sel) != wantSel {
				t.Fatalf("found selector %v, want %s", sel, wantSel)
			}
		})
	}
}

// Adapted from https://go-review.googlesource.com/c/go/+/559436
func TestIssue57490(t *testing.T) {
	src := `x: {a: int, b: int}
y: x.` // program not correctly terminated
	file, err := ParseFile("", src)
	if err == nil {
		t.Fatalf("syntax error expected, but no error reported")
	}

	// Because of the syntax error, the end position of the field decl
	// is past the end of the file's position range.
	funcEnd := file.Decls[1].End()

	tokFile := file.Pos().File()
	offset := tokFile.Offset(funcEnd)
	if offset != tokFile.Size() {
		t.Fatalf("offset = %d, want %d", offset, tokFile.Size())
	}
}

// For debugging, do not delete.
func TestX(t *testing.T) {
	t.Skip()

	f, err := ParseFile("input", `
	`, ParseComments)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	t.Error(astinternal.DebugStr(f))
}
