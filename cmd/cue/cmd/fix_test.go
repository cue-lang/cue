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

package cmd

import (
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
)

func TestFix(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		out  string
	}{{
		name: "referenced string fields",
		in: `package foo

"foo": 3
"foo-bar": 2
"baz": ` + "`foo-bar`" + `

a: {
	"qux": 3
	"qux-quux": qux
	"qaz": ` + "`qux-quux`" + `
}
`,
		out: `package foo

"foo":     3
` + "`foo-bar`" + `: 2
"baz":     ` + "`foo-bar`" + `

a: {
	qux:        3
	` + "`qux-quux`" + `: qux
	"qaz":      ` + "`qux-quux`" + `
}
`,
	}, {
		name: "comprehensions",
		in: `
package fix

"\(k)": v for k, v in src

"\(k)": v <-
 for k, v in src

/* foo
   bar
 */

a: 3 + /* foo */ 5
	 `,
		out: `package fix

for k, v in src {
	"\(k)": v
}

// foo
// bar
for k, v in src {
	"\(k)": v
}

a:
	// foo
	3 + 5
`,
	}, {
		name: "comments",
		in: `package foo

a: /* b */ 3 + 5
a: 3 /* c */ + 5
a: 3 + /* d */ 5
a: 3 + 5 /* e
f */
`,
		out: `package foo

// b
a: 3 + 5
a:
	// c
	3 + 5
a:
	// d
	3 + 5
// e
// f
a: 3 + 5
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
			n := fix(f)
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
