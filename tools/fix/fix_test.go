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
`,
			out: `import list6c6973 "list"

a: [7]
b: a + a
c: list6c6973.Concat([a, [8]])
d: list6c6973.Concat([[9], a])
e: list6c6973.Concat([[0], [1]])
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
`,
			out: `import list6c6973 "list"

a: [7]
b: a * 3
c: 4
d: list6c6973.Repeat([7], c)
e: list6c6973.Repeat([8], c)
f: list6c6973.Repeat([9], 5)
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
			n := File(f, opts...)

			b, err := format.Node(n)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
			_, err = parser.ParseFile("rewritten", got, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
