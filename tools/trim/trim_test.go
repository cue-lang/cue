// Copyright 2019 CUE Authors
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

package trim

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
)

func TestFiles(t *testing.T) {
	const trace = false
	testCases := []struct {
		name string
		in   string
		out  string
	}{{
		name: "optional does not remove required",
		in: `
		a: ["aFoo"]: 3
		a: aFoo:     _

		a: {
			["aFoo"]: 3
			aFoo:     _
		}

		["aFoo"]: 3
		aFoo:     _
		`,
		out: `a: ["aFoo"]: 3
a: aFoo:     _

a: {
	["aFoo"]: 3
	aFoo:     _
}

["aFoo"]: 3
aFoo:     _
`,
	}, {
		// TODO: make this optional
		name: "defaults can remove non-defaults",
		in: `
		foo: [string]: a: *1 | int
		foo: b: a: 1
		`,
		out: `foo: [string]: a: *1 | int
foo: b: {}
`,
	}, {
		name: "remove top-level struct",
		in: `
		a: b: 3
		for k, v in a {
			c: "\(k)": v
		}
		c: b: 3

		z: {
			a: b: 3
			for k, v in a {
				c: "\(k)": v
			}
			c: b: 3
		}
		`,
		out: `a: b: 3
for k, v in a {
	c: "\(k)": v
}

z: {
	a: b: 3
	for k, v in a {
		c: "\(k)": v
	}
}
`,
	}, {
		name: "do not remove field",
		in: `
		[_]: x: "hello"
		a: x: "hello"
		`,
		out: `[_]: x: "hello"
a: {}
`,
		// TODO: This used to work.
		// 	name: "remove implied interpolations",
		// 	in: `
		// 	foo: [string]: {
		// 		a: string
		// 		b: "--\(a)--"
		// 	}
		// 	foo: entry: {
		// 		a: "insert"
		// 		b: "--insert--"
		// 	}
		// 	`,
		// 	out: ``,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("test", tc.in)
			if err != nil {
				t.Fatal(err)
			}
			r := cue.Runtime{}
			inst, err := r.CompileFile(f)
			if err != nil {
				t.Fatal(err)
			}
			err = Files([]*ast.File{f}, inst, &Config{Trace: trace})
			if err != nil {
				t.Fatal(err)
			}
			out, err := format.Node(f)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(out); got != tc.out {
				t.Errorf("\ngot:\n%s\nwant:\n%s", got, tc.out)
			}
		})
	}
}
