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
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	cueload "cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
)

func TestSyntax(t *testing.T) {
	o := func(opts ...cue.Option) []cue.Option {
		return opts
	}
	_ = o
	testCases := []struct {
		name    string
		in      string
		path    string
		options []cue.Option
		out     string
	}{{
		name: "preseve docs",
		in: `
		// Aloha
		hello: "world"
		// Aloha2
		if true {
			// Aloha3
			if true {
				// Aloha4
				hello2: "world"
			}
		}
		`,
		options: o(cue.Docs(true)),
		out: `
{

	// Aloha
	hello: "world"
	// Aloha2
	if true {
		// Aloha3
		if true {
			// Aloha4
			hello2: "world"
		}
	}
}`,
	}, {
		name: "partially resolvable",
		in: `
		x: {}
		t: {name: string}
		output: [ ... {t & x.value}]
		`,
		out: `
{
	x: {}
	t: name: string
	output: [...t & x.value]
}`,
	}, {
		name: "issue867",
		path: "output",
		in: `
	x: {}
	t: {name: string}
	output: [ ... {t & x.value}]
	`,
		out: `
{
	[...T & {}.value]

	//cue:path: t
	let T = {name: string}
}`,
	}, {
		// Structural errors (and worse) are reported as is.
		name: "structural error",
		in: `
			#List: {
				value: _
				next: #List
			}
			a: b: #List
			`,
		path:    "a",
		options: o(cue.Final()),
		out: `
_|_ // #List.next: structural cycle
`,
	}, {
		name: "resolveReferences",
		path: "resource",
		in: `
		// User 1
		v1: #Deployment: {
			spec: {
				replicas: int
				containers: [...]
				other: option: int
			}

			incomplete: {
				// NOTE: the definition of "a" will be out of scope so this
				// reference will not be resolvable.
				// TODO: hoist the definition of "a" into a let expression.
				x: a.x
				y: 1 | 2
				z: [1, 2][a.x]
			}

			// NOTE: structural cycles are eliminated from disjunctions. This
			// means the semantics of the type is not preserved.
			// TODO: should we change this?
			recursive: #List
		}

		a: {}
		#D: {}

		#List: {
			Value: _
			Next: #List | *null
		}

		parameter: {
			image: string
			replicas: int
		}

		_mystring: string

		resource: v1.#Deployment & {
			spec: {
			   replicas: parameter.replicas
			   containers: [{
						image: parameter.image
						name: "main"
						envs: [..._mystring]
				}]
			}
		}

		parameter: image: *"myimage" | string
		parameter: replicas: *2 | >=1 & <5

		// User 2
		parameter: replicas: int

		resource: spec: replicas: parameter.replicas

		parameter: replicas: 3
		`,
		options: o(cue.Final()),
		out: `
{
	spec: {
		replicas: 3
		containers: [{
			image: "myimage"
			name:  "main"
			envs:  []
		}]
		other: option: int
	}
	incomplete: {
		x: {}.x
		y: 1 | 2
		z: [1, 2][{}.x]
	}
	recursive: {
		Value: _
		Next:  null
	}
}
		`,
	}, {
		name: "issue2339",
		in: `
s: string
if true {
	out: "\(s)": 3
}
	`,
		options: o(cue.Final()),
		out: `
{
	s: string
	out: "\(s)": 3
}
	`,
	}, {
		name: "fragments",
		in: `
		// #person is a real person
		#person: {
			children: [...#person]
			name: =~"^[A-Za-z0-9]+$"
			address: string
		}
		`,
		path:    "#person.children",
		options: o(cue.Raw()),
		out:     `[...#person]`,
	}, {
		// Hidden fields in sub-values must appear when ShowHidden is true and
		// cue.Final() is used. GetInstanceFromNode only tracks root vertices; a
		// sub-value from LookupPath has instance()=nil → ID()="". The Vertex
		// export path (taken when o.final=true) calls structComposite, which
		// checks label.PkgID(e.ctx)==e.pkgID. With e.pkgID=="" but
		// label.PkgID="_" (anonymous package marker), the comparison failed and
		// hidden fields were dropped. Fix: map e.pkgID=="" to "_".
		name:    "hidden field in sub-value",
		in:      `outer: {a: 1, _hidden: "secret"}`,
		path:    "outer",
		options: o(cue.Final(), cue.Hidden(true)),
		out: `{
	a:       1
	_hidden: "secret"
}`,
	}, {
		// Hidden-definition fields (_#name) have pkgID "_" in anonymous contexts
		// and were affected by the same bug when accessed as a sub-value with Final.
		name:    "hidden-def field in sub-value",
		in:      `outer: {_#cond: true, a: 1}`,
		path:    "outer",
		options: o(cue.Final(), cue.Hidden(true)),
		out: `{
	_#cond: true
	a:      1
}`,
	}, {
		// Each instance of a pattern constraint gets its own copy of a field
		// with an alias.
		name: "field alias in pattern constraint",
		in: `
		inp: string
		t: [string]: {
			a:   string
			y~Y: a + inp
			z:   "x" + Y
		}
		t: foo: a: "1"
		t: bar: a: "2"
		x: {p: t.foo, q: t.bar}
		`,
		path: "x",
		out: `
{
	p: {
		{
			a:     string
			y~(Y): a_9 + INP
			z:     "x" + Y
		}
		a~(a_9): "1"
	}
	q: {
		{
			a:     string
			y~(Y): a_B + INP
			z:     "x" + Y
		}
		a~(a_B): "2"
	}

	//cue:path: inp
	let INP = string
}`,
	}, {
		name: "docs and attributes of a value",
		in: `
		a: {
			// doc y
			y: 2 @foo(bar)

			@decl(a)
		}
		b: {#D: 1, y: int} & a
		`,
		path:    "b",
		options: o(cue.Final(), cue.Docs(true), cue.Attributes(true)),
		out: `
{

	@decl(a)

	// doc y
	y: 2 @foo(bar)
}`,
	}, {
		name: "docs and attributes of a schema",
		in: `
		a: {
			// doc y
			y: 2 @foo(bar)

			@decl(a)
		}
		b: {#D: 1, y: int} & a
		`,
		path:    "b",
		options: o(cue.Docs(true), cue.Attributes(true)),
		out: `
{

	@decl(a)

	// doc y
	y: 2 @foo(bar)
} & {
	#D: 1
	y:  int
}`,
	}, {
		name: "docs of a file",
		in: `
		// file comment

		// package doc
		package foo

		// doc a
		a: [
			// doc elem
			1,
		]
		`,
		options: o(cue.Raw(), cue.Docs(true)),
		out: `
// file comment

// package doc
package foo

// doc a
a: [1]`,
	}, {
		// Reference cycles and non-concrete values become errors.
		// TODO(https://cuelang.org/issue/2342): a, b, d, e, l.0, s.a, t.a,
		// r.a, and r.b should be errors.
		name: "concrete incomplete errors as values",
		in: `
		a: b + 100
		b: a - 100
		c: 1
		d: string
		e: {f: 2, if d == "" {g: 3}}
		f: *1 | int
		l: [string, 1]
		s: {a: string}
		t: s
		r: {a!: int, b!: 1, c: 2}
		`,
		options: o(cue.Concrete(true), cue.ErrorsAsValues(true)),
		out: `{
	a: b + 100
	b: a - 100
	c: 1
	d: string
	e: {
		f: 2
		if d == "" {
			g: 3
		}
	}
	f: 1
	l: [string, 1]
	s: a: string
	t: a: string
	r: {
		a!: int
		b!: 1
		c:  2
	}
}`,
	}, {
		// Without ErrorsAsValues, the error of an incomplete struct replaces it.
		// TODO(https://cuelang.org/issue/2342): a, b, d, and e should be errors.
		name: "concrete incomplete errors",
		in: `
		a: b + 100
		b: a - 100
		c: 1
		d: string
		e: {f: 2, if d == "" {g: 3}}
		`,
		options: o(cue.Concrete(true)),
		out: `{
	a: b + 100
	b: a - 100
	c: 1
	d: string
	e: {
		f: 2
		if d == "" {
			g: 3
		}
	}
}`,
	}, {
		// Errors report the path of the value being exported,
		// even when it is structure-shared.
		// TODO(https://cuelang.org/issue/2342): x.a should be an error.
		name: "concrete incomplete errors of a shared value",
		in: `
		y: {a: string, b: 1}
		x: y
		`,
		path:    "x",
		options: o(cue.Concrete(true), cue.ErrorsAsValues(true)),
		out: `{
	a: string
	b: 1
}`,
	}, {
		// The errors of each disjunct are shown along with the disjunction's.
		name: "disjunction errors as values",
		in: `
		a: {b: int} | {c: string}
		a: {b: "x", c: 1}
		`,
		options: o(cue.Concrete(true), cue.ErrorsAsValues(true)),
		out: `{
	a: _|_ // a: 2 errors in empty disjunction: (and 2 more errors)
}`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()
			f, err := parser.ParseFile("", tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			v := ctx.BuildFile(f)
			src := ast.Clone(f)
			v = v.LookupPath(cue.ParsePath(tc.path))

			syntax := v.Syntax(tc.options...)
			b, err := format.Node(syntax)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(b))

			// The result may be edited in place, so it must not share any
			// nodes with the source.
			ast.Walk(syntax, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Comment:
					x.Text = "// edited"
				case *ast.Attribute:
					x.Text = "@edited()"
				case *ast.Ident:
					x.Name = "edited"
				case *ast.BasicLit:
					x.Value = `"edited"`
				}
				return true
			}, nil)
			if _, err := format.NodeInPlace(syntax, format.Compact()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f, src) {
				got += "\n(editing the result modified the source)"
			}

			want := strings.TrimSpace(tc.out)
			if got != want {
				t.Errorf("got: %v; want %v", got, want)
			}
		})
	}
}

func TestFragment(t *testing.T) {
	in := `
	#person: {
		children: [...#person]
	}`

	ctx := cuecontext.New()
	if version, _ := (*runtime.Runtime)(ctx).Settings(); version == internal.EvalV3 {
		t.Skip("TODO: fix these tests on evalv3")
	}

	v := ctx.CompileString(in)
	v = v.LookupPath(cue.ParsePath("#person.children"))

	syntax := v.Syntax(cue.Raw()).(ast.Expr)

	// Compile the fragment from within the scope it was derived.
	v = ctx.BuildExpr(syntax, cue.Scope(v))

	// Generate the syntax, this time as self-contained.
	syntax = v.Syntax().(ast.Expr)
	b, err := format.Node(syntax)
	if err != nil {
		t.Fatal(err)
	}
	out := `{
	[...PERSON.#x]

	//cue:path: #person
	let PERSON = {
		#x: {
			children: [...#person]
		}
	}
}`
	got := strings.TrimSpace(string(b))
	want := strings.TrimSpace(out)
	if got != want {
		t.Errorf("got: %v; want %v", got, want)
	}
}

// TestSyntaxInlineImportsUnify tests that Syntax with InlineImports elides
// imports when the value was composed through [cue.Value.Unify] rather than
// built directly from an instance.
// Issue: https://cuelang.org/issue/2495
func TestSyntaxInlineImportsUnify(t *testing.T) {
	fsys := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{Data: []byte(`module: "example.com"
language: version: "v0.9.0"
`)},
		"config.cue": &fstest.MapFile{Data: []byte(`package config

import "example.com/test"

x: test.#x
`)},
		"test/test.cue": &fstest.MapFile{Data: []byte(`package test

#x: {}
`)},
	}
	insts := cueload.Instances([]string{"."}, &cueload.Config{FS: fsys})
	if err := insts[0].Err; err != nil {
		t.Fatal(err)
	}
	ctx := cuecontext.New()
	v := ctx.BuildInstance(insts[0])
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	v = v.Unify(ctx.CompileString(""))

	syntax := v.Syntax(cue.InlineImports(true))
	b, err := format.Node(syntax)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(`
{
	x: X.#x

	//cue:path: "example.com/test".#x
	let X = {#x: {}}
}`)
	got := strings.TrimSpace(string(b))
	if got != want {
		t.Errorf("got: %v; want %v", got, want)
	}
}
