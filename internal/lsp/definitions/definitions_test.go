// Copyright 2025 CUE Authors
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

package definitions_test

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/definitions"
	"github.com/go-quicktest/qt"
	"github.com/rogpeppe/go-internal/txtar"
)

func TestSimple(t *testing.T) {
	testCases{
		{
			name: "Selector_Implicit_ViaRoot",
			archive: `-- a.cue --
x: y: a.b
a: b: 5
a: b: 6
`,
			expectations: map[*position][]*position{
				ln(1, 1, "a"): {ln(2, 1, "a"), ln(3, 1, "a")},
				ln(1, 1, "b"): {ln(2, 1, "b"), ln(3, 1, "b")},
			},
		},
		{
			name: "Selector_Implicit_ViaNonRoot",
			archive: `-- a.cue --
w: {
	x: y: a.b
	a: b: 5
}
w: a: b: 6
`,
			expectations: map[*position][]*position{
				ln(2, 1, "a"): {ln(3, 1, "a"), ln(5, 1, "a")},
				ln(2, 1, "b"): {ln(3, 1, "b"), ln(5, 1, "b")},
			},
		},
		{
			name: "Pointer_Chasing_Implicit",
			archive: `-- a.cue --
x1: f: 3
x2: f: 4
y: x1
y: x2
z: y
out1: z
out2: z.f
`,
			expectations: map[*position][]*position{
				ln(3, 1, "x1"): {ln(1, 1, "x1")},
				ln(4, 1, "x2"): {ln(2, 1, "x2")},
				ln(5, 1, "y"):  {ln(3, 1, "y"), ln(4, 1, "y")},
				ln(6, 1, "z"):  {ln(5, 1, "z")},
				ln(7, 1, "z"):  {ln(5, 1, "z")},
				ln(7, 1, "f"):  {ln(1, 1, "f"), ln(2, 1, "f")},
			},
		},

		{
			name: "Pointer_Chasing_Explicit",
			archive: `-- a.cue --
x1: f: 3
x2: f: 4
y: x1 & x2
z: y
out1: z
out2: z.f
`,
			expectations: map[*position][]*position{
				ln(3, 1, "x1"): {ln(1, 1, "x1")},
				ln(3, 1, "x2"): {ln(2, 1, "x2")},
				ln(4, 1, "y"):  {ln(3, 1, "y")},
				ln(5, 1, "z"):  {ln(4, 1, "z")},
				ln(6, 1, "z"):  {ln(4, 1, "z")},
				ln(6, 1, "f"):  {ln(1, 1, "f"), ln(2, 1, "f")},
			},
		},

		{
			name: "Embedding",
			archive: `-- a.cue --
x: y: z: 3
o: { p: 4, x.y }
q: o.z
`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"): {ln(1, 1, "x")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
				ln(3, 1, "o"): {ln(2, 1, "o")},
				ln(3, 1, "z"): {ln(1, 1, "z")},
			},
		},
	}.run(t)
}

func TestInline(t *testing.T) {
	testCases{
		{
			name: "Struct_Selector",
			archive: `-- a.cue --
a: {in: {x: 5}, out: in}.out.x`,
			expectations: map[*position][]*position{
				ln(1, 2, "in"):  {ln(1, 1, "in")},
				ln(1, 2, "out"): {ln(1, 1, "out")},
				ln(1, 2, "x"):   {ln(1, 1, "x")},
			},
		},
		{
			name: "List_Index",
			archive: `-- a.cue --
a: [7, {b: 3}, true][1].b`,
			// We do not attempt any sort of resolution via dynamic
			// indices.
			expectations: map[*position][]*position{
				ln(1, 1, "1"): {},
				ln(1, 2, "b"): {},
			},
		},
		{
			name: "Disjunction_Internal",
			archive: `-- a.cue --
a: ({b: c, c: 3} | {c: 4}).c`,
			expectations: map[*position][]*position{
				ln(1, 1, "c"): {ln(1, 2, "c")},
				ln(1, 4, "c"): {ln(1, 2, "c"), ln(1, 3, "c")},
			},
		},
	}.run(t)
}

func TestCycles(t *testing.T) {
	testCases{
		{
			name: "Cycle_Simple2",
			archive: `-- a.cue --
a: b
b: a`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "a"): {ln(1, 1, "a")},
			},
		},
		{
			name: "Cycle_Simple3",
			archive: `-- a.cue --
a: b
b: c
c: a`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "c"): {ln(3, 1, "c")},
				ln(3, 1, "a"): {ln(1, 1, "a")},
			},
		},
		// These "structural" cycles are errors in the evaluator. But
		// there's no reason we can't resolve them.
		{
			name: "Structural_Simple",
			archive: `-- a.cue --
a: b: c: a`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
			},
		},
		{
			name: "Structural_Simple_Selector",
			archive: `-- a.cue --
a: b: c: a.b`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Structural_Complex",
			archive: `-- a.cue --
y: [string]: b: y
x: y
x: c: x
`,
			expectations: map[*position][]*position{
				ln(1, 2, "y"): {ln(1, 1, "y")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
				ln(3, 2, "x"): {ln(2, 1, "x"), ln(3, 1, "x")},
			},
		},
	}.run(t)
}

func TestAliases(t *testing.T) {
	testCases{
		{
			name: "Plain_Label_Internal",
			archive: `-- a.cue --
l=a: {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Plain_Label_Internal_Implicit",
			archive: `-- a.cue --
l=a: b: 3
a: c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Plain_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
a: b: 3
l=a: c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Plain_Label_External",
			archive: `-- a.cue --
l=a: b: 3
c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},

		{
			name: "Plain_Label_Scoping",
			archive: `-- a.cue --
a: {
	l=b: {c: l.d, d: 3}
	e: l.d
}
a: f: l.d
h: a.l
`,
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "b")},
				ln(2, 1, "d"): {ln(2, 2, "d")},
				ln(3, 1, "l"): {ln(2, 1, "b")},
				ln(3, 1, "d"): {ln(2, 2, "d")},
				ln(5, 1, "l"): {},
				ln(5, 1, "d"): {},
				ln(6, 1, "a"): {ln(1, 1, "a"), ln(5, 1, "a")},
				ln(6, 1, "l"): {},
			},
		},

		{
			name: "Dynamic_Label_Internal",
			archive: `-- a.cue --
l=(a): {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "(")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Dynamic_Label_Internal_Implicit",
			archive: `-- a.cue --
l=(a): b: 3
(a): c: l.b`,
			// We do not attempt to compute equivalence of
			// expressions. Therefore we don't consider the two `(a)`
			// keys to be the same.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "(")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Dynamic_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
(a): b: 3
l=(a): c: l.b`,
			// Because we don't compute equivalence of expressions, we do
			// not link the two `(a)` keys, and so we cannot resolve the
			// b in l.b.
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "(")},
				ln(2, 1, "b"): {},
			},
		},
		{
			name: "Dynamic_Label_External",
			archive: `-- a.cue --
l=(a): b: 3
c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, ("("))},
				ln(2, 1, "b"): {ln(1, 1, ("b"))},
			},
		},

		{
			name: "Pattern_Label_Internal",
			archive: `-- a.cue --
l=[a]: {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "[")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Pattern_Label_Internal_Implicit",
			archive: `-- a.cue --
l=[a]: b: 3
[a]: c: l.b`,
			// We do not attempt to compute equivalence of
			// patterns. Therefore we don't consider the two `[a]`
			// patterns to be the same. Because this style of alias is
			// only visible within the key's value, no part of l.b can be
			// resolved.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
			},
		},
		{
			name: "Pattern_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
[a]: b: 3
l=[a]: c: l.b`,
			// Again, the two [a] patterns are not merged. The l of l.b
			// can be resolved, but not the b.
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "[")},
				ln(2, 1, "b"): {},
			},
		},
		{
			name: "Pattern_Label_External",
			archive: `-- a.cue --
l=[a]: b: 3
c: l.b`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
			},
		},

		{
			name: "Pattern_Expr_Internal",
			archive: `-- a.cue --
[l=a]: {b: 3, c: l, d: l.b}`,
			// This type of alias binds l to the key. So c: l will work,
			// but for the b in d: l.b there is no resolution.
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 3, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {},
			},
		},
		{
			name: "Pattern_Expr_Internal_Implicit",
			archive: `-- a.cue --
[l=a]: b: 3
[a]: c: l`,
			// We do not attempt to compute equivalence of
			// patterns. Therefore we don't consider the two `[a]`
			// patterns to be the same. Because this style of alias is
			// only visible within the key's value, no part of l.b can be
			// resolved.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
			},
		},
		{
			name: "Pattern_Expr_External",
			archive: `-- a.cue --
[l=a]: b: 3
c: l`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
			},
		},

		{
			name: "Expr_Internal",
			archive: `-- a.cue --
a: l={b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Expr_Internal_Explicit",
			archive: `-- a.cue --
a: l={b: 3} & {c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Expr_Internal_Explicit_Paren",
			// The previous test case works because it's parsed like
			// this:
			archive: `-- a.cue --
a: l=({b: 3} & {c: l.b})`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "Expr_External",
			archive: `-- a.cue --
a: l={b: 3}
c: l.b`,
			// This type of alias is only visible within the value.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
			},
		},
	}.run(t)
}

func TestDisjunctions(t *testing.T) {
	testCases{
		{
			name: "Simple",
			archive: `-- a.cue --
d: {a: b: 3} | {a: b: 4, c: 5}
o: d.a.b
p: d.c
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b"), ln(1, 2, "b")},
				ln(3, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
			},
		},
		{
			name: "Inline",
			archive: `-- a.cue --
d: ({a: b: 3} | {a: b: 4}) & {c: 5}
o: d.a.b
p: d.c
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b"), ln(1, 2, "b")},
				ln(3, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
			},
		},
		{
			name: "Chained",
			archive: `-- a.cue --
d1: {a: 1} | {a: 2}
d2: {a: 3} | {a: 4}
o: (d1 & d2).a
`,
			expectations: map[*position][]*position{
				ln(3, 1, "a"): {ln(1, 1, "a"), ln(1, 2, "a"), ln(2, 1, "a"), ln(2, 2, "a")},
			},
		},
		{
			name: "Selected",
			archive: `-- a.cue --
d: {x: 17} | string
r: d & {x: int}
out: r.x
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "r"): {ln(2, 1, "r")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(2, 1, "x")},
			},
		},
		{
			name: "Scopes",
			archive: `-- a.cue --
c: {a: b} | {b: 3}
b: 7
d: c.b
`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},
			},
		},
		{
			name: "Looping",
			archive: `-- a.cue --
a: {b: c.d, d: 3} | {d: 4}
c: a
`,
			expectations: map[*position][]*position{
				ln(1, 1, "c"): {ln(2, 1, "c")},
				ln(1, 1, "d"): {ln(1, 2, "d"), ln(1, 3, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a")},
			},
		},
	}.run(t)
}

func TestConjunctions(t *testing.T) {
	testCases{
		{
			name: "Scopes",
			archive: `-- a.cue --
c: {a: b} & {b: 3}
b: 7
d: c.b
`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},
			},
		},
		{
			name: "MoreScopes",
			archive: `-- a.cue --
x: {
	{a: int, b: a}
	a: 2
} & {
	a: >1
}
y: x.a`,
			expectations: map[*position][]*position{
				ln(2, 2, "a"): {ln(2, 1, "a"), ln(3, 1, "a")},
				ln(7, 1, "x"): {ln(1, 1, "x")},
				ln(7, 1, "a"): {ln(2, 1, "a"), ln(3, 1, "a"), ln(5, 1, "a")},
			},
		},
	}.run(t)
}

func TestComprehensions(t *testing.T) {
	testCases{
		{
			name: "If",
			archive: `-- a.cue --
a: 17
b: 3
l=x: {
	if a < 10 {
		c: b
	}
	z: l.c
}
y: x.c`,
			expectations: map[*position][]*position{
				ln(4, 1, "a"): {ln(1, 1, "a")},
				ln(5, 1, "b"): {ln(2, 1, "b")},
				ln(7, 1, "l"): {ln(3, 1, "x")},
				ln(7, 1, "c"): {ln(5, 1, "c")},
				ln(9, 1, "x"): {ln(3, 1, "x")},
				ln(9, 1, "c"): {ln(5, 1, "c")},
			},
		},
		{
			name: "Let",
			archive: `-- a.cue --
a: b: c: 17
let x=a.b
y: x.c
`,
			expectations: map[*position][]*position{
				ln(2, 1, "a"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
				ln(3, 1, "x"): {ln(2, 1, "x")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
			},
		},
		{
			name: "Let_Scoped",
			archive: `-- a.cue --
a: {
	let b=17
	c: b
}
a: d: b
o: a.b
`,
			expectations: map[*position][]*position{
				ln(3, 1, "b"): {ln(2, 1, "b")},
				ln(5, 1, "b"): {},
				ln(6, 1, "a"): {ln(1, 1, "a"), ln(5, 1, "a")},
				ln(6, 1, "b"): {},
			},
		},
		{
			name: "For",
			archive: `-- a.cue --
a: { x: 1, y: 2, z: 3}
b: { x: 4, y: 5, z: 6}
o: {
	for k, v in a {
		(k): v * b[k]
		p: v
	}
}
q: o.p
`,
			expectations: map[*position][]*position{
				ln(4, 1, "k"): {},
				ln(4, 1, "v"): {},
				ln(4, 1, "a"): {ln(1, 1, "a")},
				ln(5, 1, "k"): {ln(4, 1, "k")},
				ln(5, 1, "v"): {ln(4, 1, "v")},
				ln(5, 1, "b"): {ln(2, 1, "b")},
				ln(5, 2, "k"): {ln(4, 1, "k")},
				ln(9, 1, "o"): {ln(3, 1, "o")},
				ln(9, 1, "p"): {ln(6, 1, "p")},
			},
		},
	}.run(t)
}

func TestFileScopes(t *testing.T) {
	testCases{
		{
			name: "Package_Top_Single",
			archive: `-- a.cue --
package x

foo: true
-- b.cue --
package x

bar: foo
`,
			expectations: map[*position][]*position{
				fln("b.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo")},
			},
		},
		{
			name: "Package_Top_Multiple",
			archive: `-- a.cue --
package x

foo: true
-- b.cue --
package x

foo: false
-- c.cue --
package x

bar: foo
foo: _
`,
			expectations: map[*position][]*position{
				fln("c.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},
			},
		},
		{
			name: "NonTop",
			archive: `-- a.cue --
package x

foo: {bar: true, baz: bar}
-- b.cue --
package x

foo: {qux: bar}
`,
			expectations: map[*position][]*position{
				fln("a.cue", 3, 2, "bar"): {fln("a.cue", 3, 1, "bar")},
				fln("b.cue", 3, 1, "bar"): {},
			},
		},
		{
			name: "NonTop_Implicit",
			archive: `-- a.cue --
package x

foo: bar: true
foo: baz: bar
-- b.cue --
package x

foo: qux: bar
-- c.cue --
package x

foo: qux: foo.bar
`,
			expectations: map[*position][]*position{
				fln("a.cue", 4, 1, "bar"): {},
				fln("b.cue", 3, 1, "bar"): {},
				fln("c.cue", 3, 2, "foo"): {fln("a.cue", 3, 1, "foo"), fln("a.cue", 4, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("c.cue", 3, 1, "bar"): {fln("a.cue", 3, 1, "bar")},
			},
		},
		{
			name: "Package_Top_Let",
			archive: `-- a.cue --
package x

let a = 5
q: a
-- b.cue --
package x

r: a
-- c.cue --
package x

let a = true
s: a
`,
			expectations: map[*position][]*position{
				fln("a.cue", 4, 1, "a"): {fln("a.cue", 3, 1, "a")},
				fln("b.cue", 3, 1, "a"): {},
				fln("c.cue", 4, 1, "a"): {fln("c.cue", 3, 1, "a")},
			},
		},
		{
			name: "Selector",
			archive: `-- a.cue --
package x

w: {
	x: y: a.b
	a: b: 5
}
-- b.cue --
package x

w: a: b: 6
`,
			expectations: map[*position][]*position{
				fln("a.cue", 4, 1, "a"): {fln("a.cue", 5, 1, "a"), fln("b.cue", 3, 1, "a")},
				fln("a.cue", 4, 1, "b"): {fln("a.cue", 5, 1, "b"), fln("b.cue", 3, 1, "b")},
			},
		},
		{
			name: "Aliases",
			archive: `-- a.cue --
package x

a: {
	X=b: {c: true}
	d: X.c
}
-- b.cue --
package x

a: {
	b: {c: false}
}
-- c.cue --
package x

a: {
	e: X.c
}
-- d.cue --
package x

a: {
	X=f: {c: 5}
	g: X
}
`,
			expectations: map[*position][]*position{
				fln("a.cue", 5, 1, "X"): {fln("a.cue", 4, 1, "b"), fln("b.cue", 4, 1, "b")},
				fln("a.cue", 5, 1, "c"): {fln("a.cue", 4, 1, "c"), fln("b.cue", 4, 1, "c")},
				fln("c.cue", 4, 1, "X"): {},
				fln("c.cue", 4, 1, "c"): {},
				fln("d.cue", 5, 1, "X"): {fln("d.cue", 4, 1, "f")},
			},
		},
	}.run(t)
}

type testCase struct {
	name         string
	archive      string
	expectations map[*position][]*position
}

type testCases []testCase

func (tcs testCases) run(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var files []*ast.File
			filesByName := make(map[string]*ast.File)

			ar := txtar.Parse([]byte(tc.archive))
			qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

			for _, fh := range ar.Files {
				ast, err := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
				ast.Pos().File().SetContent(fh.Data)
				qt.Assert(t, qt.IsNil(err))
				files = append(files, ast)
				filesByName[fh.Name] = ast
			}

			allPositions := make(map[*position]struct{})
			for from, tos := range tc.expectations {
				allPositions[from] = struct{}{}
				for _, to := range tos {
					allPositions[to] = struct{}{}
				}
			}

			for pos := range allPositions {
				if pos.filename == "" && len(files) == 1 {
					pos.filename = files[0].Filename
				}
				pos.determineOffset(filesByName[pos.filename].Pos().File())
			}

			// It's deliberate that we allow the expectations to be a
			// subset of what is resolved. However, for each expectation,
			// the resolutions must match exactly (reordering permitted).

			dfns := definitions.Analyse(files...)
			for posFrom, positionsWant := range tc.expectations {
				offset := posFrom.offset

				fdfns := dfns.ForFile(posFrom.filename)
				qt.Assert(t, qt.IsNotNil(fdfns))

				nodesGot := fdfns.ForOffset(offset)
				fileOffsetsGot := make([]fileOffset, len(nodesGot))
				for i, node := range nodesGot {
					fileOffsetsGot[i] = fileOffsetForTokenPos(node.Pos().Position())
				}
				fileOffsetsWant := make([]fileOffset, len(positionsWant))
				for i, p := range positionsWant {
					fileOffsetsWant[i] = p.fileOffset()
				}
				slices.SortFunc(fileOffsetsGot, cmpFileOffsets)
				slices.SortFunc(fileOffsetsWant, cmpFileOffsets)
				qt.Assert(t, qt.DeepEquals(fileOffsetsGot, fileOffsetsWant))
			}
		})
	}
}

type fileOffset struct {
	Filename string
	Offset   int
}

func cmpFileOffsets(a, b fileOffset) int {
	return cmp.Or(
		cmp.Compare(a.Filename, b.Filename),
		cmp.Compare(a.Offset, b.Offset),
	)
}

func fileOffsetForTokenPos(p token.Position) fileOffset {
	return fileOffset{p.Filename, p.Offset}
}

type position struct {
	filename string
	line     int
	n        int
	str      string
	offset   int
}

func (p position) fileOffset() fileOffset {
	return fileOffset{p.filename, p.offset}
}

// Convenience constructor to make a new [position] with the given
// line number (1-based), for the n-th (1-based) occurrence of str.
func ln(i, n int, str string) *position {
	return &position{
		line: i,
		n:    n,
		str:  str,
	}
}

// Convenience constructor to make a new [position] with the given
// line number (1-based), for the n-th (1-based) occurrence of str
// within the given file.
func fln(filename string, i, n int, str string) *position {
	return &position{
		filename: filename,
		line:     i,
		n:        n,
		str:      str,
	}
}

func (p *position) determineOffset(file *token.File) {
	// lines is the (cumulative) byte-offset of the start of each line
	lines := file.Lines()
	startOffset := lines[p.line-1]
	endOffset := file.Size()
	if len(lines) > p.line {
		endOffset = lines[p.line]
	}
	content := string(file.Content())
	line := content[startOffset:endOffset]
	n := p.n
	for i := range line {
		if strings.HasPrefix(line[i:], p.str) {
			n--
			if n == 0 {
				p.offset = startOffset + i
				return
			}
		}
	}
	panic("Failed to determine offset")
}
