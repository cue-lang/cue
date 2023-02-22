// Copyright 2023 CUE Authors
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

package updater

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
)

type testCase struct {
	name   string
	file   string
	update string
	want   string
}

func TestUpdater(t *testing.T) {
	testCases := []testCase{{
		name: "simple",
		file: `
// File comment.

// Field comment
a: 5
b: 6
// constraint
a: >4
		`,
		update: `
			a: 7
			"c-c": 11
		`,
		want: `
// File comment.

// Field comment
a: 7
b: 6
// constraint
a:     >4
"c-c": 11
`,
	}, {
		name: "replace reference",
		file: `
// File comment.

x: 5

// Field comment
a: x
b: x
// constraint
a: >4
		`,
		update: `
			a: 7
		`,
		want: `
// File comment.

x: 5

// Field comment
a: 7
b: x
// constraint
a: >4
`,
	}, {
		// TODO:
		name: "optional fields",
		file: `
// File comment.

["a"]: 5

// Field comment
a: 5

// Do not remove constraints.
b?: 7
b: int
a: >4
	`,
		update: `
		a: 7
		b: 8
	`,
		want: `
// File comment.

["a"]: 5

// Field comment
a: 7

// Do not remove constraints.
b?: 7
b:  int
a:  >4
b:  8
`,
	}, {
		// Note that fields are placed in existing struct if available.
		// Otherwise they are added as a single struct at a time.
		name: "fill in struct",
		file: `

foo: {}
`,
		update: `
			foo: a: 7
			foo: b: c: 8
			bar: a: 9
			bar: b: 3
			baz: 10
		`,
		want: `
foo: {
	a: 7
	b: c: 8
}
bar: a: 9
bar: b: 3
baz: 10
`,
	}, {
		name: "dont insert in template",
		file: `
foo1: [string]: bar: baz: 3
foo1: x: {}

foo2: [string]: bar: 3
foo2: x: {}

foo3: [string]: 3
foo3: x: {_}

foo4: [string]: 4
`,
		update: `
		foo1: x: bar: baz: 4
		foo2: x: bar: 4
		foo3: x: 4
		foo4: x: 4
	`,
		want: `
foo1: [string]: bar: baz: 3
foo1: x: {
	bar: baz: 4
}

foo2: [string]: bar: 3
foo2: x: {
	bar: 4
}

foo3: {
	[string]: 3
	x:        4
}
foo3: x: {_}

foo4: {
	[string]: 4
	x:        4
}
`,
	}, {
		name:   "dont modify erroneous file",
		file:   `bar: 1&2`,
		update: `bar: a: 9`,
		want:   `bar: conflicting values 2 and 1`,
	}, {
		name:   "preserve type error",
		file:   `bar: []`,
		update: `bar: a: 9`,
		want: `
bar: []
bar: a: 9
`,
	}, {
		// TODO: should this be an error? Should we preserve fields?
		name: "discard helper fields",
		file: `
			foo: x: {
				2
				#foo: 1
			}
			`,
		update: `foo: x: 4`,
		want:   `foo: x: 4`,
	}}
	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testUpdate(ctx, t, tc)
		})
	}
}

func testUpdate(ctx *cue.Context, t *testing.T, tc testCase) {
	t.Helper()

	f, err := parser.ParseFile("foo", tc.file, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	u, err := New(ctx, f)
	var got string
	if err != nil {
		got = err.Error()
	} else {
		x := ctx.CompileString(tc.update)

		VisitPaths(x, func(p cue.Path, v cue.Value) { u.Set(p, v) })

		b, err := format.Node(f)
		if err != nil {
			t.Fatal(err)
		}

		got = strings.TrimSpace(string(b))
	}

	want := strings.TrimSpace(tc.want)
	if got != want {
		t.Errorf("got:\n%s\nwant\n%s", got, want)
	}
}

// For debugging purposes, DO NOT REMOVE.
func TestX(t *testing.T) {
	t.Skip()
	ctx := cuecontext.New()
	testUpdate(ctx, t, testCase{
		file: `
		a: 1
		`,
		update: `
		a: 2
		`,
		want: `
		a: 2
		`,
	})
}
