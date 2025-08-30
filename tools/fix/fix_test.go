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

package fix

import (
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
)

func TestFile(t *testing.T) {
	testCases := []struct {
		name     string
		in       string
		out      string
		simplify bool
		exps     []string
	}{
		{
			name: "rewrite integer division",
			in: `package foo

a: 1 div 2
b: 3 mod 5
c: 2 quo 9
d: 1.0 rem 1.0 // pass on illegal values.
`,
			out: `package foo

a: __div(1, 2)
b: __mod(3, 5)
c: __quo(2, 9)
d: __rem(1.0, 1.0) // pass on illegal values.
`,
		},

		{
			name:     "simplify literal tops",
			simplify: true,
			in: `
x1: 3 & _
x2: _ | {[string]: int}
x3: 4 & (9 | _)
x4: (_ | 9) & 4
x5: (_ & 9) & 4
x6: 4 & (_ & 9)
`,
			out: `x1: 3
x2: _
x3: 4
x4: 4
x5: 9 & 4
x6: 4 & 9
`,
		},

		{
			name: "rewrite list addition",
			in: `a: [7]
b: a + a
c: a + [8]
d: [9] + a
e: [0] + [1]
f: [0] + [1] + [2]
g: list.Concat([[0], [1, 2]]) + list.Concat([[3, 4], [5]])
h: list.Concat([[0], [1, 2]]) + [3] + [4] + list.Concat([[5, 6], [7]])
i: list.Concat(list.Concat([[0], [1, 2]]), list.Concat([[3, 4], [5]]))
`,
			out: `import "list"

a: [7]
b: a + a
c: list.Concat([a, [8]])
d: list.Concat([[9], a])
e: list.Concat([[0], [1]])
f: list.Concat([[0], [1], [2]])
g: list.Concat([[0], [1, 2], [3, 4], [5]])
h: list.Concat([[0], [1, 2], [3], [4], [5, 6], [7]])
i: list.Concat(list.Concat([[0], [1, 2]]), list.Concat([[3, 4], [5]]))
`,
		},

		{
			name: "rewrite list multiplication",
			in: `a: [7]
b: a * 3
c: 4
d: [7] * c
e: c * [8]
f: [9] * 5
g: ([9] * 5) + (6 * [10])
`,
			out: `import "list"

a: [7]
b: a * 3
c: 4
d: list.Repeat([7], c)
e: list.Repeat([8], c)
f: list.Repeat([9], 5)
g: (list.Repeat([9], 5)) + (list.Repeat([10], 6))
`,
		},

		{
			name: "add ellipsis to embeddings (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: a: int
#B: b: int

X: {
	#A // foo

	// bar
	#B
	b: string
}
`,
			out: `@experiment(explicitopen)

package foo

#A: a: int
#B: b: int

X: __closeAll({
	#A... // foo

	// bar
	#B...
	b: string
})
`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("", tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			var opts []Option
			if tc.simplify {
				opts = append(opts, Simplify())
			}
			if len(tc.exps) > 0 {
				opts = append(opts, Experiments(tc.exps...))
			}
			File(f, opts...)

			b, err := format.Node(f)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
			// Skip parsing validation for tests that use experiments that create
			// syntax that requires the same experiments to parse
			if len(tc.exps) == 0 {
				_, err = parser.ParseFile("rewritten", got, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestX is for debugging; DO NOT REMOVE.
func TestX(t *testing.T) {
	t.Skip("for debugging")

	astFile, parseErr := parser.ParseFile("", `
	#A: a: int
	X: {
		#A
	}
	`, parser.ParseComments)
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}

	file(astFile, "v0.15.0", Experiments("explicitopen"))

	out, fmtErr := format.Node(astFile)
	if fmtErr != nil {
		t.Fatalf("format: %v", fmtErr)
	}
	t.Error(string(out))
}
