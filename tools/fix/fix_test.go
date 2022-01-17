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
	}{{
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
	}, {
		in: `
		y = foo
		`,
		out: `
let y = foo
`,
	}, {
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

		// 	}, {
		// 		name: "slice",
		// 		in: `package foo

		// // keep comment
		// l[3:4] // and this one

		// a: len(l[3:4])
		// b: len(l[a:_])
		// c: len(l[_:x])
		// d: len(l[_:_])
		// `,
		// 		out: `package foo

		// import list6c6973 "list"

		// // keep comment
		// list6c6973.Slice(l, 3, 4)// and this one

		// a: len(list6c6973.Slice(l, 3, 4))
		// b: len(list6c6973.Slice(l, a, len(l)))
		// c: len(list6c6973.Slice(l, 0, x))
		// d: len(list6c6973.Slice(l, 0, len(l)))
		// `,
		// 	}, {
		// 		name: "slice2",
		// 		in: `package foo

		// import "list"

		// a: list.Contains("foo")
		// b: len(l[_:_])
		// `,
		// 		out: `package foo

		// import (
		// 	"list"
		// 	list6c6973 "list"
		// )

		// a: list.Contains("foo")
		// b: len(list6c6973.Slice(l, 0, len(l)))
		// `,
	}}
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
