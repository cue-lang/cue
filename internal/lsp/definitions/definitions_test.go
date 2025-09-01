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
	"cuelang.org/go/internal/lsp/rangeset"
	"github.com/go-quicktest/qt"
	"github.com/rogpeppe/go-internal/txtar"
)

func TestDefinitions(t *testing.T) {
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

				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},
				ln(2, 1, "a"): {self, ln(3, 1, "a")},
				ln(2, 1, "b"): {self, ln(3, 1, "b")},
				ln(3, 1, "a"): {self, ln(2, 1, "a")},
				ln(3, 1, "b"): {self, ln(2, 1, "b")},
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

				ln(1, 1, "w"): {self, ln(5, 1, "w")},
				ln(2, 1, "x"): {self},
				ln(2, 1, "y"): {self},
				ln(3, 1, "a"): {self, ln(5, 1, "a")},
				ln(3, 1, "b"): {self, ln(5, 1, "b")},
				ln(5, 1, "a"): {self, ln(3, 1, "a")},
				ln(5, 1, "b"): {self, ln(3, 1, "b")},
				ln(5, 1, "w"): {self, ln(1, 1, "w")},
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

				ln(1, 1, "x1"):   {self},
				ln(1, 1, "f"):    {self},
				ln(2, 1, "x2"):   {self},
				ln(2, 1, "f"):    {self},
				ln(3, 1, "y"):    {self, ln(4, 1, "y")},
				ln(4, 1, "y"):    {self, ln(3, 1, "y")},
				ln(5, 1, "z"):    {self},
				ln(6, 1, "out1"): {self},
				ln(7, 1, "out2"): {self},
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

				ln(1, 1, "x1"):   {self},
				ln(1, 1, "f"):    {self},
				ln(2, 1, "x2"):   {self},
				ln(2, 1, "f"):    {self},
				ln(3, 1, "y"):    {self},
				ln(4, 1, "z"):    {self},
				ln(5, 1, "out1"): {self},
				ln(6, 1, "out2"): {self},
			},
		},

		{
			name: "Pointer_Chasing_Forwards_and_Back",
			archive: `-- a.cue --
x: a.b

c: b: 5
d: b: int
a: c & d

y: a.b
`,
			expectations: map[*position][]*position{
				ln(1, 1, "a"): {ln(5, 1, "a")},
				ln(1, 1, "b"): {ln(3, 1, "b"), ln(4, 1, "b")},

				ln(5, 1, "c"): {ln(3, 1, "c")},
				ln(5, 1, "d"): {ln(4, 1, "d")},

				ln(7, 1, "a"): {ln(5, 1, "a")},
				ln(7, 1, "b"): {ln(3, 1, "b"), ln(4, 1, "b")},

				ln(1, 1, "x"): {self},

				ln(3, 1, "c"): {self},
				ln(3, 1, "b"): {self},

				ln(4, 1, "d"): {self},
				ln(4, 1, "b"): {self},

				ln(5, 1, "a"): {self},
				ln(7, 1, "y"): {self},
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

				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},
				ln(1, 1, "z"): {self},
				ln(2, 1, "o"): {self},
				ln(2, 1, "p"): {self},
				ln(3, 1, "q"): {self},
			},
		},

		{
			name: "String_Literal",
			archive: `-- a.cue --
x: y: a.b
a: b: 5
"a": b: 6
`,
			expectations: map[*position][]*position{
				ln(1, 1, "a"): {ln(2, 1, "a"), ln(3, 1, `"a"`)},
				ln(1, 1, "b"): {ln(2, 1, "b"), ln(3, 1, "b")},

				ln(1, 1, "x"):   {self},
				ln(1, 1, "y"):   {self},
				ln(2, 1, "a"):   {self, ln(3, 1, `"a"`)},
				ln(2, 1, "b"):   {self, ln(3, 1, "b")},
				ln(3, 1, `"a"`): {self, ln(2, 1, "a")},
				ln(3, 1, "b"):   {self, ln(2, 1, "b")},
			},
		},

		{
			name: "List_Index",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}]
y: x[1].b`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, "[1]"): {ln(1, 2, "{")},
				ln(2, 1, "b"):   {ln(1, 1, "b")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "y"): {self},
			},
		},

		{
			name: "List_Index_Ellipsis",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...{a: 4}]
y: x[17].a`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"):    {ln(1, 1, "x")},
				ln(2, 1, "[17]"): {ln(1, 1, "...")},
				ln(2, 1, "a"):    {ln(1, 2, "a")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "a"): {self},

				ln(2, 1, "y"): {self},
			},
		},

		{
			name: "List_Index_Ellipsis_Indirect",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...z]
y: x[17].a
z: a: 4`,
			expectations: map[*position][]*position{
				ln(1, 1, "z"):    {ln(3, 1, "z")},
				ln(2, 1, "x"):    {ln(1, 1, "x")},
				ln(2, 1, "[17]"): {ln(1, 1, "...")},
				ln(2, 1, "a"):    {ln(3, 1, "a")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "y"): {self},

				ln(3, 1, "z"): {self},
				ln(3, 1, "a"): {self},
			},
		},

		{
			name: "Ellipsis_Explicit",
			archive: `-- a.cue --
l: [...{x: int}]
d: l & [{x: 3}, {x: 4}]
`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "l")},

				ln(1, 1, "l"): {self},
				ln(1, 1, "x"): {self},

				ln(2, 1, "d"): {self},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 1, "x")},
			},
		},

		{
			name: "Ellipsis_Implicit",
			archive: `-- a.cue --
d: [...{x: int}]
d: [{x: 3}, {x: 4}]
`,
			expectations: map[*position][]*position{
				ln(1, 1, "d"): {self, ln(2, 1, "d")},
				ln(1, 1, "x"): {self},

				ln(2, 1, "d"): {self, ln(1, 1, "d")},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 1, "x")},
			},
		},

		{
			name: "List_Index_Ellipsis_Mixed",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...{a: 4}]
y: [...z]
z: a: int
o: x & y
p: o[0].a
q: o[3].a`,
			expectations: map[*position][]*position{
				ln(2, 1, "z"):   {ln(3, 1, "z")},
				ln(4, 1, "x"):   {ln(1, 1, "x")},
				ln(4, 1, "y"):   {ln(2, 1, "y")},
				ln(5, 1, "o"):   {ln(4, 1, "o")},
				ln(5, 1, "[0]"): {ln(1, 1, "{"), ln(2, 1, "...")},
				ln(5, 1, "a"):   {ln(1, 1, "a"), ln(3, 1, "a")},
				ln(6, 1, "o"):   {ln(4, 1, "o")},
				ln(6, 1, "[3]"): {ln(1, 1, "..."), ln(2, 1, "...")},
				ln(6, 1, "a"):   {ln(1, 2, "a"), ln(3, 1, "a")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "a"): {self},

				ln(2, 1, "y"): {self},

				ln(3, 1, "z"): {self},
				ln(3, 1, "a"): {self},

				ln(4, 1, "o"): {self},
				ln(5, 1, "p"): {self},
				ln(6, 1, "q"): {self},
			},
		},

		{
			name: "StringLit_Conjunction",
			archive: `-- a.cue --
c: {a: b, "b": x: 3} & {b: x: 3, z: b.x}
b: 7
d: c.b.x`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(1, 4, "b"): {ln(1, 1, `"b"`), ln(1, 3, "b")},
				ln(1, 3, "x"): {ln(1, 1, "x"), ln(1, 2, "x")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 1, `"b"`), ln(1, 3, "b")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(1, 2, "x")},

				ln(1, 1, "a"):   {self},
				ln(1, 1, "c"):   {self},
				ln(1, 1, "x"):   {self, ln(1, 2, "x")},
				ln(1, 1, "z"):   {self},
				ln(1, 1, `"b"`): {self, ln(1, 3, "b")},
				ln(1, 2, "x"):   {self, ln(1, 1, "x")},
				ln(1, 3, "b"):   {self, ln(1, 1, `"b"`)},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
		},
		{
			name: "StringLit_Nested",
			archive: `-- a.cue --
b: {
  a: 7
  "b": {
    c: b.a
    a: 12
  }
}`,
			expectations: map[*position][]*position{
				ln(4, 1, "b"): {ln(1, 1, "b")},
				ln(4, 1, "a"): {ln(2, 1, "a")},

				ln(1, 1, "b"):   {self},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},
			},
		},
		{
			name: "StringLit_Nested_Promotion",
			archive: `-- a.cue --
b: {
  a: 7
  "b": {
    c: b.a
    a: 12
  }
  b: _
}`,
			expectations: map[*position][]*position{
				ln(4, 1, "b"): {ln(3, 1, `"b"`), ln(7, 1, "b")},
				ln(4, 1, "a"): {ln(5, 1, "a")},

				ln(1, 1, "b"):   {self},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self, ln(7, 1, "b")},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},
				ln(7, 1, "b"):   {self, ln(3, 1, `"b"`)},
			},
		},
		{
			name: "StringLit_Nested_RedHerring",
			archive: `-- a.cue --
b: {
  a: 7
  "b": {
    c: b.a
    a: 12
  }
}
b: b: _`,
			expectations: map[*position][]*position{
				ln(4, 1, "b"): {ln(1, 1, "b"), ln(8, 1, "b")},
				ln(4, 1, "a"): {ln(2, 1, "a")},

				ln(1, 1, "b"):   {self, ln(8, 1, "b")},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self, ln(8, 2, "b")},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},

				ln(8, 1, "b"): {self, ln(1, 1, "b")},
				ln(8, 2, "b"): {self, ln(3, 1, `"b"`)},
			},
		},

		{
			name: "Inline_Struct_Selector",
			archive: `-- a.cue --
a: {in: {x: 5}, out: in}.out.x`,
			expectations: map[*position][]*position{
				ln(1, 2, "in"):  {ln(1, 1, "in")},
				ln(1, 2, "out"): {ln(1, 1, "out")},
				ln(1, 2, "x"):   {ln(1, 1, "x")},

				ln(1, 1, "a"):   {self},
				ln(1, 1, "in"):  {self},
				ln(1, 1, "x"):   {self},
				ln(1, 1, "out"): {self},
			},
		},
		{
			name: "Inline_List_Index_LiteralConst",
			archive: `-- a.cue --
a: [7, {b: 3}, true][1].b`,
			// If the index is a literal const we do resolve it.
			expectations: map[*position][]*position{
				ln(1, 1, "[1]"): {ln(1, 1, "{")},
				ln(1, 2, "b"):   {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
			},
		},
		{
			name: "Inline_List_Index_Dynamic",
			archive: `-- a.cue --
a: [7, {b: 3}, true][n].b
n: 1
`,
			// Even the slightest indirection defeats indexing
			expectations: map[*position][]*position{
				ln(1, 1, "["): {},
				ln(1, 1, "n"): {ln(2, 1, "n")},
				ln(1, 1, "]"): {},
				ln(1, 2, "b"): {},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "n"): {self},
			},
		},
		{
			name: "StringLit_Struct_Index_LiteralConst",
			archive: `-- a.cue --
x: "a b": z: 5
y: x["a b"].z`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"):       {ln(1, 1, "x")},
				ln(2, 1, `["a b"]`): {ln(1, 1, `"a b"`)},
				ln(2, 1, "z"):       {ln(1, 1, "z")},

				ln(1, 1, "x"):     {self},
				ln(1, 1, `"a b"`): {self},
				ln(1, 1, "z"):     {self},

				ln(2, 1, "y"): {self},
			},
		},
		{
			name: "Inline_Disjunction_Internal",
			archive: `-- a.cue --
a: ({b: c, c: 3} | {c: 4}).c`,
			expectations: map[*position][]*position{
				ln(1, 1, "c"): {ln(1, 2, "c")},
				ln(1, 4, "c"): {ln(1, 2, "c"), ln(1, 3, "c")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "c"): {self},
				ln(1, 3, "c"): {self},
			},
		},

		{
			name: "Cycle_Simple2",
			archive: `-- a.cue --
a: b
b: a`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
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

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
				ln(3, 1, "c"): {self},
			},
		},
		// These "structural" cycles are errors in the evaluator. But
		// there's no reason we can't resolve them.
		{
			name: "Cycle_Structural_Simple",
			archive: `-- a.cue --
a: b: c: a`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Structural_Simple_Selector",
			archive: `-- a.cue --
a: b: c: a.b`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Cycle_Structural_Complex",
			archive: `-- a.cue --
y: [string]: b: y
x: y
x: c: x
`,
			expectations: map[*position][]*position{
				ln(1, 2, "y"): {ln(1, 1, "y")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
				ln(3, 2, "x"): {ln(2, 1, "x"), ln(3, 1, "x")},

				ln(1, 1, "y"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "x"): {self, ln(3, 1, "x")},

				ln(3, 1, "x"): {self, ln(2, 1, "x")},
				ln(3, 1, "c"): {self},
			},
		},

		{
			name: "Alias_Plain_Label_Internal",
			archive: `-- a.cue --
l=a: {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Plain_Label_Internal_Implicit",
			archive: `-- a.cue --
l=a: b: 3
a: c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self, ln(1, 1, "a")},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Plain_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
a: b: 3
l=a: c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self, ln(1, 1, "a")},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Plain_Label_External",
			archive: `-- a.cue --
l=a: b: 3
c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
			},
		},

		{
			name: "Alias_Plain_Label_Scoping",
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
				ln(1, 1, "a"): {self, ln(5, 1, "a")},

				ln(2, 1, "b"): {self},
				ln(2, 1, "c"): {self},
				ln(2, 2, "d"): {self},

				ln(3, 1, "e"): {self},

				ln(5, 1, "a"): {self, ln(1, 1, "a")},
				ln(5, 1, "f"): {self},

				ln(6, 1, "h"): {self},
			},
		},

		{
			name: "Alias_Dynamic_Label_Internal",
			archive: `-- a.cue --
l=(a): {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "(")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Dynamic_Label_Internal_Implicit",
			archive: `-- a.cue --
l=(a): b: 3
(a): c: l.b`,
			// We do not attempt to compute equivalence of
			// expressions. Therefore we don't consider the two `(a)`
			// keys to be the same.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "(")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Dynamic_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
(a): b: 3
l=(a): c: l.b`,
			// Because we don't compute equivalence of expressions, we do
			// not link the two `(a)` keys, and so we cannot resolve the
			// b in l.b.
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "(")},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Dynamic_Label_External",
			archive: `-- a.cue --
l=(a): b: 3
c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, ("("))},
				ln(2, 1, "b"): {ln(1, 1, ("b"))},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},

		{
			name: "Alias_Pattern_Label_Internal",
			archive: `-- a.cue --
l=[a]: {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "[")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Pattern_Label_Internal_Implicit",
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

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Pattern_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
[a]: b: 3
l=[a]: c: l.b`,
			// Again, the two [a] patterns are not merged. The l of l.b
			// can be resolved, but not the b.
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "[")},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Pattern_Label_External",
			archive: `-- a.cue --
l=[a]: b: 3
c: l.b`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},

		{
			name: "Alias_Pattern_Expr_Internal",
			archive: `-- a.cue --
[l=a]: {b: 3, c: l, d: l.b}`,
			// This type of alias binds l to the key. So c: l will work,
			// but for the b in d: l.b there is no resolution.
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 3, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {},

				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
				ln(1, 1, "d"): {self},
			},
		},
		{
			name: "Alias_Pattern_Expr_Internal_Implicit",
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

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Pattern_Expr_External",
			archive: `-- a.cue --
[l=a]: b: 3
c: l`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
		},

		{
			name: "Alias_Expr_Internal",
			archive: `-- a.cue --
a: l={b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Expr_Internal_Explicit",
			archive: `-- a.cue --
a: l={b: 3} & {c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Expr_Internal_Explicit_Paren",
			// The previous test case works because it's parsed like
			// this:
			archive: `-- a.cue --
a: l=({b: 3} & {c: l.b})`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
		},
		{
			name: "Alias_Expr_External",
			archive: `-- a.cue --
a: l={b: 3}
c: l.b`,
			// This type of alias is only visible within the value.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
			},
		},

		{
			name: "Alias_Expr_Call",
			archive: `-- a.cue --
a: n=(2 * (div(n, 2))) | error("\(n) is not even")
`,
			expectations: map[*position][]*position{
				ln(1, 2, "n"): {ln(1, 1, "n")},
				ln(1, 3, "n"): {ln(1, 1, "n")},

				ln(1, 1, "a"): {self},
			},
		},

		{
			name: "Call_Arg_Expr",
			archive: `-- a.cue --
c: (f({a: b, b: 3})).g
`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},
			},
		},

		{
			name: "Disjunction_Simple",
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

				ln(1, 1, "d"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "a"): {self},
				ln(1, 2, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(2, 1, "o"): {self},
				ln(3, 1, "p"): {self},
			},
		},
		{
			name: "Disjunction_Inline",
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

				ln(1, 1, "d"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "a"): {self},
				ln(1, 2, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(2, 1, "o"): {self},
				ln(3, 1, "p"): {self},
			},
		},
		{
			name: "Disjunction_Chained",
			archive: `-- a.cue --
d1: {a: 1} | {a: 2}
d2: {a: 3} | {a: 4}
o: (d1 & d2).a
`,
			expectations: map[*position][]*position{
				ln(3, 1, "d1"): {ln(1, 1, "d1")},
				ln(3, 1, "d2"): {ln(2, 1, "d2")},
				ln(3, 1, "a"):  {ln(1, 1, "a"), ln(1, 2, "a"), ln(2, 1, "a"), ln(2, 2, "a")},

				ln(1, 1, "d1"): {self},
				ln(1, 1, "a"):  {self},
				ln(1, 2, "a"):  {self},

				ln(2, 1, "d2"): {self},
				ln(2, 1, "a"):  {self},
				ln(2, 2, "a"):  {self},

				ln(3, 1, "o"): {self},
			},
		},
		{
			name: "Disjunction_Selected",
			archive: `-- a.cue --
d: {x: 17} | string
r: d & {x: int}
out: r.x
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "r"): {ln(2, 1, "r")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(2, 1, "x")},

				ln(1, 1, "d"): {self},
				ln(1, 1, "x"): {self}, // note non-symmetric!

				ln(2, 1, "r"): {self},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},

				ln(3, 1, "out"): {self},
			},
		},
		{
			name: "Disjunction_Scopes",
			archive: `-- a.cue --
c: {a: b} | {b: 3}
b: 7
d: c.b
`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
		},
		{
			name: "Disjunction_Looping",
			archive: `-- a.cue --
a: {b: c.d, d: 3} | {d: 4}
c: a
`,
			expectations: map[*position][]*position{
				ln(1, 1, "c"): {ln(2, 1, "c")},
				ln(1, 1, "d"): {ln(1, 2, "d"), ln(1, 3, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "d"): {self},
				ln(1, 3, "d"): {self},

				ln(2, 1, "c"): {self},
			},
		},

		{
			name: "Conjunction_Scopes",
			archive: `-- a.cue --
c: {a: b} & {b: 3}
b: 7
d: c.b
`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
		},
		{
			name: "Conjunction_MoreScopes",
			archive: `-- a.cue --
x: {
	{a: int, b: a}
	a: 2
} & {
	a: >1
}
y: x.a`,
			expectations: map[*position][]*position{
				ln(2, 2, "a"): {ln(2, 1, "a"), ln(3, 1, "a"), ln(5, 1, "a")},
				ln(7, 1, "x"): {ln(1, 1, "x")},
				ln(7, 1, "a"): {ln(2, 1, "a"), ln(3, 1, "a"), ln(5, 1, "a")},

				ln(1, 1, "x"): {self},

				ln(2, 1, "a"): {self, ln(3, 1, "a"), ln(5, 1, "a")},
				ln(2, 1, "b"): {self},

				ln(3, 1, "a"): {self, ln(2, 1, "a"), ln(5, 1, "a")},
				ln(5, 1, "a"): {self, ln(2, 1, "a"), ln(3, 1, "a")},
				ln(7, 1, "y"): {self},
			},
		},
		{
			name: "Conjunction_EvenMoreScopes",
			archive: `-- a.cue --
c: {a: b, b: x: 3} & {b: x: 3, z: b.x}
b: 7
d: c.b.x`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(1, 2, "b"), ln(1, 3, "b")},
				ln(1, 4, "b"): {ln(1, 2, "b"), ln(1, 3, "b")},
				ln(1, 3, "x"): {ln(1, 1, "x"), ln(1, 2, "x")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b"), ln(1, 3, "b")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(1, 2, "x")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self, ln(1, 3, "b")},
				ln(1, 1, "x"): {self, ln(1, 2, "x")},
				ln(1, 3, "b"): {self, ln(1, 2, "b")},
				ln(1, 2, "x"): {self, ln(1, 1, "x")},
				ln(1, 1, "z"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
		},

		{
			name: "Conjunction_Selector",
			archive: `-- a.cue --
b: ({a: 6} & {a: int}).a
`,
			expectations: map[*position][]*position{
				ln(1, 3, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "a"): {self, ln(1, 2, "a")},
				ln(1, 2, "a"): {self, ln(1, 1, "a")},
			},
		},

		{
			name: "Binary_Expr",
			archive: `-- a.cue --
c: ({a: 6, d: a} + {b: a}).g
a: 12
`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 3, "a"): {ln(2, 1, "a")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "d"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self},
			},
		},

		{
			name: "Import_Builtin_Call",
			archive: `-- a.cue --
import "magic"

x: magic.Merlin(y)
y: "wand"
`,
			expectations: map[*position][]*position{
				ln(3, 1, "magic"):  {ln(1, 1, `"magic"`)},
				ln(3, 1, "Merlin"): {},
				ln(3, 1, "y"):      {ln(4, 1, "y")},

				ln(3, 1, "x"): {self},
				ln(4, 1, "y"): {self},
			},
		},
		{
			name: "Import_alias",
			archive: `-- a.cue --
import wand "magic"

x: wand.foo
`,
			expectations: map[*position][]*position{
				ln(3, 1, "wand"): {ln(1, 1, "wand")},
				ln(3, 1, "foo"):  {},
				ln(3, 1, "x"):    {self},
			},
		},

		{
			name: "Interpolation_Simple",
			archive: `-- a.cue --
a: 5
b: c
c: x: true
d: "4+\(a) > 0?\(b.x)"
`,
			expectations: map[*position][]*position{
				ln(2, 1, "c"): {ln(3, 1, "c")},
				ln(4, 1, "a"): {ln(1, 1, "a")},
				ln(4, 1, "b"): {ln(2, 1, "b")},
				ln(4, 1, "x"): {ln(3, 1, "x")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
				ln(3, 1, "c"): {self},
				ln(3, 1, "x"): {self},
				ln(4, 1, "d"): {self},
			},
		},
		{
			name: "Interpolation_Field",
			archive: `-- a.cue --
a: 5
"five\(a)": hello
`,
			expectations: map[*position][]*position{
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
			},
		},
		{
			name: "Interpolation_Expr",
			archive: `-- a.cue --
y: "\({a: 3, b: a}.b) \(a)"
a: 12
`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
				ln(1, 3, "a"): {ln(2, 1, "a")},

				ln(1, 1, "y"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "a"): {self},
			},
		},

		{
			name: "MultiByte_Expression",
			archive: `-- a.cue --
x: "💩" + y
y: "sticks"
`,
			expectations: map[*position][]*position{
				ln(1, 1, "y"): {ln(2, 1, "y")},

				ln(1, 1, "x"): {self},
				ln(2, 1, "y"): {self},
			},
		},
		{
			name: "MultiByte_Index",
			archive: `-- a.cue --
x: {"💩": "sticks"}
y: x["💩"]
`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"):     {ln(1, 1, "x")},
				ln(2, 1, `["💩"]`): {ln(1, 1, `"💩"`)},

				ln(1, 1, "x"):   {self},
				ln(1, 1, `"💩"`): {self},

				ln(2, 1, "y"): {self},
			},
		},
		{
			name: "MultiByte_Selector",
			archive: `-- a.cue --
x: {"💩": "sticks"}
y: x."💩"
`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, `"💩"`): {ln(1, 1, `"💩"`)},

				ln(1, 1, "x"):   {self},
				ln(1, 1, `"💩"`): {self},

				ln(2, 1, "y"): {self},
			},
		},

		{
			name: "Comprehension_If",
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

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
				ln(3, 1, "x"): {self},
				ln(5, 1, "c"): {self},
				ln(7, 1, "z"): {self},
				ln(9, 1, "y"): {self},
			},
		},
		{
			name: "Comprehension_Let",
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

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(3, 1, "y"): {self},
			},
		},
		{
			name: "Comprehension_Let_Scoped",
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

				ln(1, 1, "a"): {self, ln(5, 1, "a")},
				ln(3, 1, "c"): {self},

				ln(5, 1, "a"): {self, ln(1, 1, "a")},
				ln(5, 1, "d"): {self},

				ln(6, 1, "o"): {self},
			},
		},
		{
			name: "Comprehension_For",
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
r: o.k
`,
			expectations: map[*position][]*position{
				ln(4, 1, "k"):  {},
				ln(4, 1, "v"):  {},
				ln(4, 1, "a"):  {ln(1, 1, "a")},
				ln(5, 1, "k"):  {ln(4, 1, "k")},
				ln(5, 1, "v"):  {ln(4, 1, "v")},
				ln(5, 1, "b"):  {ln(2, 1, "b")},
				ln(5, 2, "k"):  {ln(4, 1, "k")},
				ln(6, 1, "v"):  {ln(4, 1, "v")},
				ln(9, 1, "o"):  {ln(3, 1, "o")},
				ln(9, 1, "p"):  {ln(6, 1, "p")},
				ln(10, 1, "o"): {ln(3, 1, "o")},
				ln(10, 1, "k"): {},

				ln(1, 1, "a"): {self},
				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},
				ln(1, 1, "z"): {self},

				ln(2, 1, "b"): {self},
				ln(2, 1, "x"): {self},
				ln(2, 1, "y"): {self},
				ln(2, 1, "z"): {self},

				ln(3, 1, "o"):  {self},
				ln(6, 1, "p"):  {self},
				ln(9, 1, "q"):  {self},
				ln(10, 1, "r"): {self},
			},
		},
		{
			name: "Comprehension_For_ForwardsReference",
			archive: `-- a.cue --
for a, b in foo.bar {}
foo: bar: "baz"`,
			expectations: map[*position][]*position{
				ln(1, 1, "foo"): {ln(2, 1, "foo")},
				ln(1, 1, "bar"): {ln(2, 1, "bar")},

				ln(2, 1, "foo"): {self},
				ln(2, 1, "bar"): {self},
			},
		},
		{
			name: "Comprehension_For_Scopes",
			archive: `-- a.cue --
x: {
	for k, v in k {v: k}
}
k: {}
`,
			expectations: map[*position][]*position{
				ln(2, 2, "k"): {ln(4, 1, "k")},
				ln(2, 3, "k"): {ln(2, 1, "k")},

				ln(1, 1, "x"): {self},
				ln(2, 2, "v"): {self},
				ln(4, 1, "k"): {self},
			},
		},
		{
			name: "Comprehension_Mixed_Scopes",
			archive: `-- a.cue --
g: [for x in [1, 2]
let x = x+1
let x = x+1 {
    {h: x}
}]
i: g[0].h`,
			expectations: map[*position][]*position{
				ln(2, 2, "x"): {ln(1, 1, "x")},
				ln(3, 2, "x"): {ln(2, 1, "x")},
				ln(4, 1, "x"): {ln(3, 1, "x")},
				ln(6, 1, "g"): {ln(1, 1, "g")},

				ln(1, 1, "g"):   {self},
				ln(4, 1, "h"):   {self},
				ln(6, 1, "i"):   {self},
				ln(6, 1, "[0]"): {ln(1, 1, "for")},
				ln(6, 1, "h"):   {ln(4, 1, "h")},
			},
		},

		{
			name: "Definitions",
			archive: `-- a.cue --
#x: y: #z: 3
o: #x & #x.y.z
`,
			expectations: map[*position][]*position{
				ln(2, 1, "#x"): {ln(1, 1, "#x")},
				ln(2, 2, "#x"): {ln(1, 1, "#x")},
				ln(2, 1, "y"):  {ln(1, 1, "y")},

				ln(1, 1, "#x"): {self},
				ln(1, 1, "y"):  {self},
				ln(1, 1, "#z"): {self},

				ln(2, 1, "o"): {self},
			},
		},

		{
			name: "MultiFile_Package_Top_Single",
			archive: `-- a.cue --
package x

foo: true
-- b.cue --
package x

bar: foo
`,
			expectations: map[*position][]*position{
				fln("b.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo")},

				fln("a.cue", 3, 1, "foo"): {self},
				fln("b.cue", 3, 1, "bar"): {self},
			},
		},
		{
			name: "MultiFile_Package_Top_Multiple",
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

				fln("a.cue", 3, 1, "foo"): {self, fln("b.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},
				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},

				fln("c.cue", 3, 1, "bar"): {self},
				fln("c.cue", 4, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo")},
			},
		},
		{
			name: "MultiFile_NonTop",
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

				fln("a.cue", 3, 1, "foo"): {self, fln("b.cue", 3, 1, "foo")},
				fln("a.cue", 3, 1, "bar"): {self},
				fln("a.cue", 3, 1, "baz"): {self},

				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo")},
				fln("b.cue", 3, 1, "qux"): {self},
			},
		},
		{
			name: "MultiFile_NonTop_Implicit",
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

				fln("a.cue", 3, 1, "foo"): {self, fln("a.cue", 4, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("a.cue", 3, 1, "bar"): {self},
				fln("a.cue", 4, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("a.cue", 4, 1, "baz"): {self},

				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("a.cue", 4, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("b.cue", 3, 1, "qux"): {self, fln("c.cue", 3, 1, "qux")},

				fln("c.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("a.cue", 4, 1, "foo"), fln("b.cue", 3, 1, "foo")},
				fln("c.cue", 3, 1, "qux"): {self, fln("b.cue", 3, 1, "qux")},
			},
		},
		{
			name: "MultiFile_Package_Top_Let",
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

				fln("a.cue", 4, 1, "q"): {self},
				fln("b.cue", 3, 1, "r"): {self},
				fln("c.cue", 4, 1, "s"): {self},
			},
		},
		{
			name: "MultiFile_Selector",
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

				fln("a.cue", 3, 1, "w"): {self, fln("b.cue", 3, 1, "w")},
				fln("a.cue", 4, 1, "x"): {self},
				fln("a.cue", 4, 1, "y"): {self},
				fln("a.cue", 5, 1, "a"): {self, fln("b.cue", 3, 1, "a")},
				fln("a.cue", 5, 1, "b"): {self, fln("b.cue", 3, 1, "b")},

				fln("b.cue", 3, 1, "w"): {self, fln("a.cue", 3, 1, "w")},
				fln("b.cue", 3, 1, "a"): {self, fln("a.cue", 5, 1, "a")},
				fln("b.cue", 3, 1, "b"): {self, fln("a.cue", 5, 1, "b")},
			},
		},
		{
			name: "MultiFile_Aliases",
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

				fln("a.cue", 3, 1, "a"): {self, fln("b.cue", 3, 1, "a"), fln("c.cue", 3, 1, "a"), fln("d.cue", 3, 1, "a")},
				fln("a.cue", 4, 1, "b"): {self, fln("b.cue", 4, 1, "b")},
				fln("a.cue", 4, 1, "c"): {self, fln("b.cue", 4, 1, "c")},
				fln("a.cue", 5, 1, "d"): {self},

				fln("b.cue", 3, 1, "a"): {self, fln("a.cue", 3, 1, "a"), fln("c.cue", 3, 1, "a"), fln("d.cue", 3, 1, "a")},
				fln("b.cue", 4, 1, "b"): {self, fln("a.cue", 4, 1, "b")},
				fln("b.cue", 4, 1, "c"): {self, fln("a.cue", 4, 1, "c")},

				fln("c.cue", 3, 1, "a"): {self, fln("a.cue", 3, 1, "a"), fln("b.cue", 3, 1, "a"), fln("d.cue", 3, 1, "a")},
				fln("c.cue", 4, 1, "e"): {self},

				fln("d.cue", 3, 1, "a"): {self, fln("a.cue", 3, 1, "a"), fln("b.cue", 3, 1, "a"), fln("c.cue", 3, 1, "a")},
				fln("d.cue", 4, 1, "f"): {self},
				fln("d.cue", 4, 1, "c"): {self},
				fln("d.cue", 5, 1, "g"): {self},
			},
		},

		{
			name: "Resolve_Import",
			archive: `-- a.cue --
package a

x: 12
-- b.cue --
package b

import "a"

y: a
z: y.x
-- c.cue --
package a
`,
			expectations: map[*position][]*position{
				fln("b.cue", 3, 1, `"a"`): {fln("a.cue", 1, 1, "package a"), fln("c.cue", 1, 1, "package a")},
				fln("b.cue", 5, 1, "a"):   {fln("b.cue", 3, 1, `"a"`)},
				fln("b.cue", 6, 1, "y"):   {fln("b.cue", 5, 1, "y")},
				fln("b.cue", 6, 1, "x"):   {fln("a.cue", 3, 1, "x")},

				fln("a.cue", 3, 1, "x"): {self},

				fln("b.cue", 5, 1, "y"): {self},
				fln("b.cue", 6, 1, "z"): {self},
			},
		},

		{
			name: "Fields",
			archive: `-- a.cue --
x: x: x: 3
x: x: x: 4
x: y
y: x: z
z: x: 5
`,
			expectations: map[*position][]*position{
				ln(3, 1, "y"): {ln(4, 1, "y")},
				ln(4, 1, "z"): {ln(5, 1, "z")},

				ln(1, 1, "x"): {self, ln(2, 1, "x"), ln(3, 1, "x")},
				ln(2, 1, "x"): {self, ln(1, 1, "x"), ln(3, 1, "x")},
				ln(3, 1, "x"): {self, ln(1, 1, "x"), ln(2, 1, "x")},

				ln(1, 2, "x"): {self, ln(2, 2, "x"), ln(4, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 2, "x"), ln(4, 1, "x")},
				ln(4, 1, "x"): {self}, // note non-symmetric!

				ln(1, 3, "x"): {self, ln(2, 3, "x"), ln(5, 1, "x")},
				ln(2, 3, "x"): {self, ln(1, 3, "x"), ln(5, 1, "x")},
				ln(5, 1, "x"): {self}, // note non-symmetric!

				ln(4, 1, "y"): {self},
				ln(5, 1, "z"): {self},
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
			filesByPkg := make(map[string][]*ast.File)

			ar := txtar.Parse([]byte(tc.archive))
			qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

			for _, fh := range ar.Files {
				fileAst, err := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
				fileAst.Pos().File().SetContent(fh.Data)
				qt.Assert(t, qt.IsNil(err))
				files = append(files, fileAst)
				filesByName[fh.Name] = fileAst
				pkgName := fileAst.PackageName()
				filesByPkg[pkgName] = append(filesByPkg[pkgName], fileAst)
			}

			var allPositions []*position
			for from, tos := range tc.expectations {
				allPositions = append(allPositions, from)
				allPositions = append(allPositions, tos...)
			}

			for _, pos := range allPositions {
				if pos == self {
					continue
				}
				if pos.filename == "" && len(files) == 1 {
					pos.filename = files[0].Filename
				}
				pos.determineOffset(filesByName[pos.filename].Pos().File())
			}

			dfnsByFilename := make(map[string]*definitions.FileDefinitions)
			dfnsByPkgName := make(map[string]*definitions.Definitions)
			forPackage := func(importPath string) *definitions.Definitions {
				return dfnsByPkgName[importPath]
			}

			for pkgName, files := range filesByPkg {
				dfns := definitions.Analyse(forPackage, files...)
				dfnsByPkgName[pkgName] = dfns
				for _, fileAst := range files {
					dfnsByFilename[fileAst.Filename] = dfns.ForFile(fileAst.Filename)
				}
			}

			ranges := rangeset.NewFilenameRangeSet()

			for posFrom, positionsWant := range tc.expectations {
				filename := posFrom.filename
				fdfns := dfnsByFilename[filename]
				qt.Assert(t, qt.IsNotNil(fdfns))

				offset := posFrom.offset
				ranges.Add(filename, offset, offset+len(posFrom.str))

				for i := range len(posFrom.str) {
					// Test every offset within the "from" token
					offset := offset + i
					nodesGot := fdfns.ForOffset(offset)
					fileOffsetsGot := make([]fileOffset, len(nodesGot))
					for j, node := range nodesGot {
						fileOffsetsGot[j] = fileOffsetForTokenPos(node.Pos().Position())
					}
					fileOffsetsWant := make([]fileOffset, len(positionsWant))
					for j, p := range positionsWant {
						if p == self {
							fileOffsetsWant[j] = posFrom.fileOffset()
						} else {
							fileOffsetsWant[j] = p.fileOffset()
						}
					}
					slices.SortFunc(fileOffsetsGot, cmpFileOffsets)
					slices.SortFunc(fileOffsetsWant, cmpFileOffsets)
					qt.Assert(t, qt.DeepEquals(fileOffsetsGot, fileOffsetsWant), qt.Commentf("from %#v(+%d)", posFrom, i))
				}
			}

			// Test that all offsets not explicitly mentioned in
			// expectations, resolve to nothing.
			for _, fileAst := range files {
				filename := fileAst.Filename
				fdfns := dfnsByFilename[filename]
				for i := range fileAst.Pos().File().Content() {
					if ranges.Contains(filename, i) {
						continue
					}
					nodesGot := fdfns.ForOffset(i)
					fileOffsetsGot := make([]fileOffset, len(nodesGot))
					for j, node := range nodesGot {
						fileOffsetsGot[j] = fileOffsetForTokenPos(node.Pos().Position())
					}
					qt.Assert(t, qt.DeepEquals(fileOffsetsGot, []fileOffset{}), qt.Commentf("file: %q, offset: %d", filename, i))
				}
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

// self is a convenience singleton which can be freely used in
// expectation values to refer to that expectation's key. Typically
// used to indicate a field's key should resolve to itself.
var self = &position{}
