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
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/rogpeppe/go-internal/txtar"
)

func TestFiles(t *testing.T) {
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
			{["aFoo"]: 3}
			aFoo:     _
		}

		["aFoo"]: 3
		aFoo:     _
		`,
		out: `a: ["aFoo"]: 3
a: aFoo:     _

a: {
	{["aFoo"]: 3}
	aFoo: _
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
		{[_]: x: "hello"}
		a: x: "hello"
		`,
		out: `
{[_]: x: "hello"}
a: {}
`,
	}, {
		name: "issue303",
		in: `
		foo: c: true
		foo: #M
		#M: c?: bool
		`,
		out: `foo: c: true
foo: #M
#M: c?: bool
`,
	}, {
		name: "remove due to simplification",
		in: `
foo: [string]: {
	t: [string]: {
		x: >=0 & <=5
	}
}

foo: multipath: {
	t: [string]: {
		// Combined with the other constraints, we know the value must be 5 and
		// thus the entry below can be eliminated.
		x: >=5 & <=8 & int
	}

	t: u: { x: 5 }
}

group: {
	for k, v in foo {
		comp: "\(k)": v
	}
}

		`,
		out: `foo: [string]: {
	t: [string]: {
		x: >=0 & <=5
	}
}

foo: multipath: {
	t: [string]: {

		x: >=5 & <=8 & int
	}

	t: u: {}
}

group: {
	for k, v in foo {
		comp: "\(k)": v
	}
}
`,
	}, {
		name: "list removal",
		in: `
		service: [string]: {
			ports: [{a: 1}, {a: 1}, ...{ extra: 3 }]
		}
		service: a: {
			ports: [{a: 1}, {a: 1, extra: 3}, {}, { extra: 3 }]
		}
`,
		out: `service: [string]: {
	ports: [{a: 1}, {a: 1}, ...{extra: 3}]
}
service: a: {
	ports: [{}, {extra: 3}, {}, {}]
}
`,
	}, {
		name: "list removal",
		in: `
		service: [string]: {
			ports: [{a: 1}, {a: 1}, ...{ extra: 3 }]
		}
		service: a: {
			ports: [{a: 1}, {a: 1,}]
		}
		`,
		out: `service: [string]: {
	ports: [{a: 1}, {a: 1}, ...{extra: 3}]
}
service: a: {
}
`,
	}, {
		name: "do not overmark comprehension",
		in: `
foo: multipath: {
	t: [string]: { x: 5 }

	// Don't remove u!
	t: u: { x: 5 }
}

group: {
	for k, v in foo {
		comp: "\(k)": v
	}
}

	`,
		out: `foo: multipath: {
	t: [string]: {x: 5}

	t: u: {}
}

group: {
	for k, v in foo {
		comp: "\(k)": v
	}
}
`,
	}, {
		name: "remove implied interpolations",
		in: `
				foo: [string]: {
					a: string
					b: "--\(a)--"
				}
				foo: entry: {
					a: "insert"
					b: "--insert--"
				}
				`,
		out: `foo: [string]: {
	a: string
	b: "--\(a)--"
}
foo: entry: {
	a: "insert"
}
`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("test", tc.in)
			if err != nil {
				t.Fatal(err)
			}
			r := cuecontext.New()
			v := r.BuildFile(f)
			if err := v.Err(); err != nil {
				t.Fatal(err)
			}
			err = Files([]*ast.File{f}, v, &Config{Trace: false})
			if err != nil {
				t.Fatal(err)
			}

			out := formatNode(t, f)
			if got := string(out); got != tc.out {
				t.Errorf("\ngot:\n%s\nwant:\n%s", got, tc.out)
			}
		})
	}
}

const trace = false

func TestData(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "trim",
		Update: cuetest.UpdateGoldenFiles,
	}

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		inst := cue.Build(a[:1])[0]
		if inst.Err != nil {
			t.Fatal(inst.Err)
		}

		files := a[0].Files

		err := Files(files, inst, &Config{Trace: trace})
		if err != nil {
			t.WriteErrors(errors.Promote(err, ""))
		}

		for _, f := range files {
			t.WriteFile(f)
		}
	})
}

func formatNode(t *testing.T, n ast.Node) []byte {
	t.Helper()

	b, err := format.Node(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// For debugging, do not remove.
func TestX(t *testing.T) {
	in := `
-- in.cue --
`

	t.Skip()

	a := txtar.Parse([]byte(in))
	instances := cuetxtar.Load(a, "/tmp/test")

	inst := cue.Build(instances)[0]
	if inst.Err != nil {
		t.Fatal(inst.Err)
	}

	files := instances[0].Files

	Debug = true

	err := Files(files, inst, &Config{
		Trace: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		b := formatNode(t, f)
		t.Error(string(b))
	}
}
