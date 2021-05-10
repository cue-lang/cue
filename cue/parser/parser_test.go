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
)

func TestParse(t *testing.T) {
	testCases := []struct{ desc, in, out string }{{

		"ellipsis in structs",
		`#Def: {
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
		`#Def: {b: "2", ...}, ..., #Def2: {..., b: "2"}, #Def3: {..., _}, ...`,
	}, {

		"empty file", "", "",
	}, {
		"empty struct", "{}", "{}",
	}, {
		"empty structs", "{},{},", "{}, {}",
	}, {
		"empty structs; elided comma", "{}\n{}", "{}, {}",
	}, {
		"basic lits", `"a","b", 3,3.4,5,2_3`, `"a", "b", 3, 3.4, 5, 2_3`,
	}, {
		"keyword basic lits", `true,false,null,for,in,if,let,if`, `true, false, null, for, in, if, let, if`,
	}, {
		"keyword basic newline", `
		true
		false
		null
		for
		in
		if
		let
		if
		`, `true, false, null, for, in, if, let, if`,
	}, {
		"keywords as labels",
		`if: 0, for: 1, in: 2, where: 3, div: 4, quo: 5
		for: if: let: 3
		`,
		`if: 0, for: 1, in: 2, where: 3, div: 4, quo: 5, for: {if: {let: 3}}`,
	}, {
		"keywords as alias",
		`if=foo: 0
		for=bar: 2
		let=bar: 3
		`,
		`if=foo: 0, for=bar: 2, let=bar: 3`,
	}, {
		"json",
		`{
			"a": 1,
			"b": "2",
			"c": 3
		}`,
		`{"a": 1, "b": "2", "c": 3}`,
	}, {
		"json:extra comma",
		`{
			"a": 1,
			"b": "2",
			"c": 3,
		}`,
		`{"a": 1, "b": "2", "c": 3}`,
	}, {
		"json:simplified",
		`{
			a: 1
			b: "2"
			c: 3
		}`,
		`{a: 1, b: "2", c: 3}`,
	}, {
		"attributes",
		`a: 1 @xml(,attr)
		 b: 2 @foo(a,b=4) @go(Foo)
		 c: {
			 d: "x" @go(D) @json(,omitempty)
			 e: "y" @ts(,type=string,"str")
		 }`,
		`a: 1 @xml(,attr), b: 2 @foo(a,b=4) @go(Foo), c: {d: "x" @go(D) @json(,omitempty), e: "y" @ts(,type=string,"str")}`,
	}, {
		"not emitted",
		`a: true
		 b?: "2"
		 c?: 3

		 "g\("en")"?: 4
		`,
		`a: true, b?: "2", c?: 3, "g\("en")"?: 4`,
	}, {
		"definition",
		`#Def: {
			 b: "2"
			 c: 3

			 embedding
		}
		#Def: {}
		`,
		`#Def: {b: "2", c: 3, embedding}, #Def: {}`,
	}, {
		"one-line embedding",
		`{ V1, V2 }`,
		`{V1, V2}`,
	}, {
		"selectors",
		`a.b. "str"`,
		`a.b."str"`,
	}, {
		"selectors",
		`a.b. "str"`,
		`a.b."str"`,
	}, {
		"faulty bytes selector",
		`a.b.'str'`,
		"a.b._\nexpected selector, found 'STRING' 'str'",
	}, {
		"faulty multiline string selector",
		`a.b."""
			"""`,
		"a.b._\nexpected selector, found 'STRING' \"\"\"\n\t\t\t\"\"\"",
	}, {
		"expression embedding",
		`#Def: {
			a.b.c
			a > b < c
			-1<2

			foo: 2
		}`,
		`#Def: {a.b.c, a>b<c, -1<2, foo: 2}`,
	}, {
		"ellipsis in structs",
		`#Def: {
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
		`#Def: {b: "2", ...}, ..., #Def2: {..., b: "2"}, #Def3: {..., _}, ...`,
	}, {
		"emitted referencing non-emitted",
		`a: 1
		 b: "2"
		 c: 3
		{ name: b, total: a + b }`,
		`a: 1, b: "2", c: 3, {name: b, total: a+b}`,
	}, {
		"package file",
		`package k8s
		 {}
		`,
		`package k8s, {}`,
	}, {
		"imports group",
		`package k8s

		import (
			a "foo"
			"bar/baz"
		)
		`,
		`package k8s, import ( a "foo", "bar/baz" )`,
	}, {
		"imports single",
		`package k8s

		import a "foo"
		import "bar/baz"
			`,
		`package k8s, import a "foo", import "bar/baz"`,
	}, {
		"collapsed fields",
		`a: b:: c?: [Name=_]: d: 1
		"g\("en")"?: 4
		 // job foo { bar: 1 } // TODO error after foo
		 job: "foo": [_]: { bar: 1 }
		`,
		`a: {b :: {c?: {[Name=_]: {d: 1}}}}, "g\("en")"?: 4, job: {"foo": {[_]: {bar: 1}}}`,
	}, {
		"identifiers",
		`// 	$_: 1,
			a: {b: {c: d}}
			c: a
			d: a.b
			// e: a."b" // TODO: is an error
			e: a.b.c
			"f": f,
			[X=_]: X
		`,
		"a: {b: {c: d}}, c: a, d: a.b, e: a.b.c, \"f\": f, [X=_]: X",
	}, {
		"empty fields",
		`
		"": 3
		`,
		`"": 3`,
	}, {
		"expressions",
		`	a: (2 + 3) * 5
			b: (2 + 3) + 4
			c: 2 + 3 + 4
			d: -1
			e: !foo
			f: _|_
		`,
		"a: (2+3)*5, b: (2+3)+4, c: 2+3+4, d: -1, e: !foo, f: _|_",
	}, {
		"pseudo keyword expressions",
		`	a: (2 div 3) mod 5
			b: (2 quo 3) rem 4
			c: 2 div 3 div 4
		`,
		"a: (2 div 3) mod 5, b: (2 quo 3) rem 4, c: 2 div 3 div 4",
	}, {
		"ranges",
		`	a: >=1 & <=2
			b: >2.0  & <= 40.0
			c: >"a" & <="b"
			v: (>=1 & <=2) & <=(>=5 & <=10)
			w: >1 & <=2 & <=3
			d: >=3T & <=5M
		`,
		"a: >=1&<=2, b: >2.0&<=40.0, c: >\"a\"&<=\"b\", v: (>=1&<=2)&<=(>=5&<=10), w: >1&<=2&<=3, d: >=3T&<=5M",
	}, {
		"indices",
		`{
			a: b[2]
			b: c[1:2]
			c: "asdf"
			d: c ["a"]
		}`,
		`{a: b[2], b: c[1:2], c: "asdf", d: c["a"]}`,
	}, {
		"calls",
		`{
			a: b(a.b, c.d)
			b: a.b(c)
		}`,
		`{a: b(a.b, c.d), b: a.b(c)}`,
	}, {
		"lists",
		`{
			a: [ 1, 2, 3, b, c, ... ]
			b: [ 1, 2, 3, ],
			c: [ 1,
			 2,
			 3
			 ],
			d: [ 1+2, 2, 4,]
		}`,
		`{a: [1, 2, 3, b, c, ...], b: [1, 2, 3], c: [1, 2, 3], d: [1+2, 2, 4]}`,
	}, {
		"list types",
		`{
			a: 4*[int]
			b: <=5*[ {a: 5} ]
			c1: [...int]
			c2: [...]
			c3: [1, 2, ...int,]
		}`,
		`{a: 4*[int], b: <=5*[{a: 5}], c1: [...int], c2: [...], c3: [1, 2, ...int]}`,
	}, {
		"list comprehensions",
		`{
				y: [1,2,3]
				b: [ for x in y if x == 1 { x } ],
			}`,
		`{y: [1, 2, 3], b: [for x in y if x==1 {x}]}`,
	}, {
		"field comprehensions",
		`{
				y: { a: 1, b: 2}
				a: {
					for k, v in y if v > 2 {
						"\(k)": v
					}
				}
			 }`,
		`{y: {a: 1, b: 2}, a: {for k: v in y if v>2 {"\(k)": v}}}`,
	}, {
		"nested comprehensions",
		`{
			y: { a: 1, b: 2}
			a: {
				for k, v in y let x = v+2 if x > 2 {
					"\(k)": v
				}
			}
		}`,
		`{y: {a: 1, b: 2}, a: {for k: v in y let x=v+2 if x>2 {"\(k)": v}}}`,
	}, {
		"let declaration",
		`{
			let X = 42
			let Y = "42",
			let Z = 10 + 12
		}`,
		`{let X=42, let Y="42", let Z=10+12}`,
	}, {
		"duplicates allowed",
		`{
			a: b: 3
			a: { b: 3 }
		}`,
		"{a: {b: 3}, a: {b: 3}}",
	}, {
		"templates", // TODO: remove
		`{
			[foo=_]: { a: int }
			a:     { a: 1 }
		}`,
		"{[foo=_]: {a: int}, a: {a: 1}}",
	}, {
		"value alias",
		`
		{
			a: X=foo
			b: Y={foo}
			c: d: e: X=5
		}
		`,
		`{a: X=foo, b: Y={foo}, c: {d: {e: X=5}}}`,
	}, {
		"dynamic labels",
		`{
			(x): a: int
			x:   "foo"
			a: {
				(a.b)
			}
		}`,
		`{(x): {a: int}, x: "foo", a: {(a.b)}}`,
	}, {
		"foo",
		`[
			[1],
			[1, 2],
			[1, 2, 3],
		]`,
		"[[1], [1, 2], [1, 2, 3]]",
	}, {
		"interpolation",
		`a: "foo \(ident)"
		 b: "bar \(bar)  $$$ "
		 c: "nest \(   { a: "\( nest ) "}.a ) \(5)"
		 m1: """
			 multi \(bar)
			 """
		 m2: '''
			 \(bar) multi
			 '''`,
		`a: "foo \(ident)", b: "bar \(bar)  $$$ ", c: "nest \({a: "\(nest) "}.a) \(5)", ` + "m1: \"\"\"\n\t\t\t multi \\(bar)\n\t\t\t \"\"\", m2: '''\n\t\t\t \\(bar) multi\n\t\t\t '''",
	}, {
		"file comments",
		`// foo

		// uni
		package foo // uniline

		// file.1
		// file.2

		`,
		"<[0// foo] <[d0// uni] [l3// uniline] [3// file.1 // file.2] package foo>>",
	}, {
		"line comments",
		`// doc
		 a: 5 // line
		 b: 6 // lineb
			  // next
			`, // next is followed by EOF. Ensure it doesn't move to file.
		"<[d0// doc] [l5// line] a: 5>, " +
			"<[l5// lineb] [5// next] b: 6>",
	}, {
		"alt comments",
		`// a ...
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
		"<[d0// a ...] [l5// line a] [5// about a] a: 5>, " +
			"<[d0// b ...] [l2// lineb] [5// about b] b: 6>, " +
			"<[5// about c] c: 7>, " +
			"<[d0// about d] d: {<[d0// about e] e>: 3}>",
	}, {
		"expr comments",
		`
		a: 2 +  // 2 +
		   3 +  // 3 +
		   4    // 4
		   `,
		"<[l5// 4] a: <[l2// 3 +] <[l2// 2 +] 2+3>+4>>",
	}, {
		"composit comments",
		`a : {
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
		"a: {a: 1, b: 2, c: 3, <[d5// end] d: 4>}, " +
			"b: [1, 2, 3, 4, <[d2// end] 5>], " +
			"c: [1, 2, 3, <[l2// here] 4>, <[l4// here] {a: 3}>, 5, 6, 7, <[l2// and here] 8>], " +
			"d: {<[l5// Hello] a: 1>, <[d0// Doc] b: 2>}, " +
			"e1: <[d1// comment in list body] []>, " +
			"e2: <[d1// comment in struct body] {}>",
	}, {
		"attribute comments",
		`
		a: 1 @a() @b() // d
		`,
		`<[l5// d] a: 1 @a() @b()>`,
	}, {
		"attribute declarations",
		`
		@foo()

		package bar

		@bar()

		import "strings"

		@baz()
			`,
		`@foo(), package bar, @bar(), import "strings", @baz()`,
	}, {
		"comprehension comments",
		`
		if X {
			// Comment 1
			Field: 2
			// Comment 2
		}
		`,
		`if X <[d2// Comment 2] {<[d0// Comment 1] Field: 2>}>`,
	}, {
		"let comments",
		`let X = foo // Comment 1`,
		`<[5// Comment 1] let X=foo>`,
	}, {
		"emit comments",
		`// a comment at the beginning of the file

		// a second comment

		// comment
		a: 5

		{}

		// a comment at the end of the file
		`,
		"<[0// a comment at the beginning of the file] [0// a second comment] <[d0// comment] a: 5>, <[2// a comment at the end of the file] {}>>",
	}, {
		"composite comments 2",
		`
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
		`{<[0// foo] [d0// fooo] foo: 1>, bar: 2}, [<[l4// each element has a long] {"name": "value"}>, <[l4// optional next element] {"name": "next"}>]`,
	}, {
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
		out: `I="\(k)": v, ` +
			`S="foo-bar": w, ` +
			`L=foo: x, ` +
			`X=[0]: {foo: X|null}, ` +
			`[Y=string]: {name: Y}, ` +
			`X1=[X2=<"d"]: {name: X2}, ` +
			`Y1=foo: {Y2=bar: [Y1, Y2]}`,
	}, {
		desc: "allow keyword in expression",
		in: `
		foo: in & 2
		`,
		out: "foo: in&2",
	}, {
		desc: "dot import",
		in: `
		import . "foo"
		`,
		out: "import , \"foo\"\nexpected 'STRING', found '.'",
	}, {
		desc: "attributes",
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
	}, {
		desc: "Issue #276",
		in: `
		a: int=>2
		`,
		out: "a: int=>2",
	}, {
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
	}, {
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
	}, {
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
	}, {
		desc: "front-style commas",
		in: `
			frontStyle: { "key": "value"
				, "key2": "value2"
				, "foo" : bar
			}
			`,
		out: "frontStyle: {\"key\": \"value\", \"key2\": \"value2\", \"foo\": bar}",
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			mode := []Option{AllErrors}
			if strings.Contains(tc.desc, "comments") {
				mode = append(mode, ParseComments)
			}
			f, err := ParseFile("input", tc.in, mode...)
			got := debugStr(f)
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
		{"old-style definition",
			`foo :: 3`},
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
			mode := []Option{AllErrors, ParseComments, FromVersion(Latest)}
			_, err := ParseFile("input", tc.in, mode...)
			if err == nil {
				t.Errorf("unexpected success: %v", tc.in)
			}
		})
	}
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
				t.Fatalf("found no *SelectorExpr: %#v %s", f.Decls[0], debugStr(f))
			}
			const wantSel = "&{fmt _ {<nil>} {{}}}"
			if fmt.Sprint(sel) != wantSel {
				t.Fatalf("found selector %v, want %s", sel, wantSel)
			}
		})
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
	t.Error(debugStr(f))
}
