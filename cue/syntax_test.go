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
		`,
		options: o(cue.Docs(true)),
		out: `
{
	// Aloha
	hello: "world"
}`,
	}, {
		name: "partially resolvable",
		in: `
		x: {}
		t: {name: string}
		output: [ ... {t & x.value}]
		`,
		options: o(cue.ResolveReferences(true)),

		// TODO: note that this does not resolve t, even though it potentially
		// could. The current implementation makes this rather hard. As the
		// output would not be correct anyway, the question is whether this
		// makes sense.
		// One way to implement this would be for the evaluator to keep track
		// of good and bad conjuncts, and then package them nicely in a Vertex
		// so they remain accessible.
		out: `
{
	x: {}
	t: {
		name: string
	}
	output: [...t & x.value]
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
		options: o(cue.ResolveReferences(true)),
		out: `
{
	b: _|_ // #List.next: structural cycle
}`,
	}, {
		name: "resolveReferences",
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
		path:    "resource",
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
		x: a.x
		y: 1 | 2
		z: [1, 2][a.x]
	}
	recursive: {
		Value: _
		Next:  null
	}
}
		`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()

			v := ctx.CompileString(tc.in)
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
