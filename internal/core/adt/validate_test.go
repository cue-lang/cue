// Copyright 2020 CUE Authors
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

package adt_test

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/cuetdtest"
	"github.com/google/go-cmp/cmp"
)

func TestValidate(t *testing.T) {
	type testCase struct {
		name   string
		in     string
		out    string
		lookup string
		cfg    *adt.ValidateConfig

		todo_v3 bool
	}
	testCases := []testCase{{
		name: "no error, but not concrete, even with definition label",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		#foo: { use: string }
		`,
		lookup: "#foo",
		out:    "incomplete\n#foo.use: incomplete value string:\n    test:2:16",
	}, {
		name: "definitions not considered for completeness",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		#foo: { use: string }
		`,
	}, {
		name: "hidden fields not considered for completeness",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		_foo: { use: string }
		`,
	}, {
		name: "hidden fields not considered for completeness",
		in: `
		_foo: { use: string }
		`,
	}, {
		name: "evaluation error at top",
		in: `
		1 & 2
		`,
		out: "eval\nconflicting values 2 and 1:\n    test:2:3\n    test:2:7",
	}, {
		name: "evaluation error in field",
		in: `
		x: 1 & 2
		`,
		out: "eval\nx: conflicting values 2 and 1:\n    test:2:6\n    test:2:10",
	}, {
		name: "first error",
		in: `
		x: 1 & 2
		y: 2 & 4
		`,
		out: "eval\nx: conflicting values 2 and 1:\n    test:2:6\n    test:2:10",
	}, {
		name: "all errors",
		cfg:  &adt.ValidateConfig{AllErrors: true},
		in: `
		x: 1 & 2
		y: 2 & 4
		`,
		out: `eval
x: conflicting values 2 and 1:
    test:2:6
    test:2:10
y: conflicting values 4 and 2:
    test:3:6
    test:3:10`,
	}, {
		name: "incomplete",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		y: 2 + x
		x: string
		`,
		out: "incomplete\ny: non-concrete value string in operand to +:\n    test:2:6\n    test:3:6",
	}, {
		name: "allowed incomplete cycle",
		in: `
		y: x
		x: y
		`,
	}, {
		name: "allowed incomplete when disallowing cycles",
		cfg:  &adt.ValidateConfig{DisallowCycles: true},
		in: `
		y: string
		x: y
		`,
	}, {
		// TODO: different error position
		todo_v3: true,

		name: "disallow cycle",
		cfg:  &adt.ValidateConfig{DisallowCycles: true},
		in: `
		y: x + 1
		x: y - 1
		`,
		out: "cycle\ncycle error:\n    test:2:6",
	}, {
		// TODO: different error position
		todo_v3: true,

		name: "disallow cycle",
		cfg:  &adt.ValidateConfig{DisallowCycles: true},
		in: `
		a: b - 100
		b: a + 100
		c: [c[1], c[0]]		`,
		out: "cycle\ncycle error:\n    test:2:6",
	}, {
		name: "treat cycles as incomplete when not disallowing",
		cfg:  &adt.ValidateConfig{},
		in: `
		y: x + 1
		x: y - 1
		`,
	}, {
		// Note: this is already handled by evaluation, as terminal errors
		// are percolated up.
		name: "catch most serious error",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		y: string
		x: 1 & 2
		`,
		out: "eval\nx: conflicting values 2 and 1:\n    test:3:6\n    test:3:10",
	}, {
		name: "consider defaults for concreteness",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		x: *1 | 2
		`,
	}, {
		name: "allow non-concrete in definitions in concrete mode",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		x: 2
		#d: {
			b: int
			c: b + b
		}
		`,
	}, {
		name: "pick up non-concrete value in default",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		x: null | *{
			a: int
		}
		`,
		out: "incomplete\nx.a: incomplete value int:\n    test:3:7",
	}, {
		name: "pick up non-concrete value in default",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
			x: null | *{
				a: 1 | 2
			}
			`,
		out: "incomplete\nx.a: incomplete value 1 | 2",
	}, {
		name: "required field not present",
		cfg:  &adt.ValidateConfig{Final: true},
		in: `
			Person: {
				name!:  string
				age?:   int
				height: 1.80
			}
			`,
		out: "incomplete\nPerson.name: field is required but not present:\n    test:3:5",
	}, {
		name: "allow required fields in definitions",
		cfg:  &adt.ValidateConfig{Concrete: true},
		in: `
		#Person: {
			name!: string
			age?:  int
		}
		`,
		out: "",
	}, {
		name: "allow required fields when not concrete",
		in: `
		Person: {
			name!: string
			age?:  int
		}
		`,
		out: "",
	}, {
		name: "indirect resolved disjunction using matchN",
		cfg:  &adt.ValidateConfig{Final: true},
		in: `
			x: {}
			x: matchN(0, [bool | {x!: _}])
		`,
		out: "",
	}, {
		name: "indirect resolved disjunction",
		cfg:  &adt.ValidateConfig{Final: true},
		in: `
				x: {bar: 2}
				x: string | {foo!: string}
			`,
		out: "incomplete\nx.foo: field is required but not present:\n    test:3:18",
	}, {
		name: "disallow incomplete error with final",
		cfg:  &adt.ValidateConfig{Final: true},
		in: `
			x: y + 1
			y: int
				`,
		out: "incomplete\nx: non-concrete value int in operand to +:\n    test:2:7\n    test:3:7",
	}, {
		name: "allow incomplete error with final while in definition",
		cfg:  &adt.ValidateConfig{Final: true},
		in: `
			#D: x: #D.y + 1
			#D: y: int
				`,
	}, {
		name: "allow incomplete error with final while in definition",
		cfg:  &adt.ValidateConfig{Final: true},
		in: `
			#D: x: #D.y + 1
			#D: y: int
				`,
	}, {
		name: "report non-concrete value of structure shared node in correct position",
		cfg: &adt.ValidateConfig{
			Concrete: true,
			Final:    true,
		},
		in: `
			#Def: a: x!: int
			b: #Def
			`,
		out: "incomplete\nb.a.x: field is required but not present:\n    test:2:13\n    test:3:7",

		todo_v3: true, // missing position
	}, {
		// Issue #3864: issue resulting from structure sharing.
		name: "attribute incomplete values in definitions to concrete path",
		cfg: &adt.ValidateConfig{
			Concrete: true,
			Final:    true,
		},
		in: `
			#A: y: string
			#B: x: #A
			#C: {
				x: #A
				v: #B & { x: x } // note: 'x' resolves to self.
			}
			config: #C & {
				x: y: "dev"
			}
		`,
		out: "incomplete\nconfig.v.x.y: incomplete value string:\n    test:2:11",
	}}

	cuetdtest.Run(t, testCases, func(t *cuetdtest.T, tc *testCase) {
		if tc.todo_v3 {
			t.M.TODO_V3(t) // P1: cycle error missing? Other error.
		}
		r := t.M.Runtime()
		ctx := eval.NewContext(r, nil)

		f, err := parser.ParseFile("test", tc.in)
		if err != nil {
			t.Fatal(err)
		}
		v, err := compile.Files(nil, r, "", f)
		if err != nil {
			t.Fatal(err)
		}
		v.Finalize(ctx)
		if tc.lookup != "" {
			v = v.Lookup(adt.MakeIdentLabel(r, tc.lookup, "main"))
		}

		b := adt.Validate(ctx, v, tc.cfg)

		w := &strings.Builder{}
		if b != nil {
			fmt.Fprintln(w, b.Code)
			errors.Print(w, b.Err, nil)
		}

		got := strings.TrimSpace(w.String())
		if tc.out != got {
			t.Error(cmp.Diff(tc.out, got))
		}
	})
}
