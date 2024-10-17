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
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
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
		options: o(cue.ResolveReferences(true)),
		out: `
{
	x: {}
	t: {
		name: string
	}
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
	let T = {
		name: string
	}
}`}, {
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
		options: o(cue.ResolveReferences(true)),
		out: `
{
	b: _|_ // #List.next: structural cycle (and 1 more errors)
}`,
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
		options: o(cue.ResolveReferences(true)),
		out: `
{
	spec: {
		replicas: 3
		containers: [{
			image: *"myimage" | string
			name:  "main"
			envs: [...string]
		}]
		other: {
			option: int
		}
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
		options: o(cue.ResolveReferences(true)),
		out: `
{
	s: string
	out: {
		"\(s)": 3
	}
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
		options: o(cue.Schema(), cue.Raw()),
		out:     `[...#person]`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()

			v := ctx.CompileString(tc.in, cue.Filename(tc.name))
			v = v.LookupPath(cue.ParsePath(tc.path))

			syntax := v.Syntax(tc.options...)
			b, err := format.Node(syntax)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(b))
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

	v := ctx.CompileString(in)
	v = v.LookupPath(cue.ParsePath("#person.children"))

	syntax := v.Syntax(cue.Schema(), cue.Raw()).(ast.Expr)

	// Compile the fragment from within the scope it was derived.
	v = ctx.BuildExpr(syntax, cue.Scope(v))

	// Generate the syntax, this time as self-contained.
	syntax = v.Syntax(cue.Schema()).(ast.Expr)
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
