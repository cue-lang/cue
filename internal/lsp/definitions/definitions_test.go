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
	"fmt"
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
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "a"): {ln(2, 1, "a"), ln(3, 1, "a")},
				ln(1, 1, "b"): {ln(2, 1, "b"), ln(3, 1, "b")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},
				ln(2, 1, "a"): {self, ln(3, 1, "a")},
				ln(2, 1, "b"): {self, ln(3, 1, "b")},
				ln(3, 1, "a"): {self, ln(2, 1, "a")},
				ln(3, 1, "b"): {self, ln(2, 1, "b")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):  {f: []string{"a", "x"}},
				ln(1, 1, "y"):  {f: []string{"y"}},
				ln(1, 1, "a"):  {e: []string{"a", "x", "y"}},
				ln(1, 1, ".b"): {e: []string{"b"}},
				ln(2, 1, "a"):  {f: []string{"a", "x"}},
				ln(2, 1, "b"):  {f: []string{"b"}},
				ln(3, 1, "a"):  {f: []string{"a", "x"}},
				ln(3, 1, "b"):  {f: []string{"b"}},
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

			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "w"):  {f: []string{"w"}},
				ln(2, 1, "x"):  {f: []string{"a", "x"}},
				ln(2, 1, "y"):  {f: []string{"y"}},
				ln(2, 1, "a"):  {e: []string{"a", "w", "x", "y"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
				ln(3, 1, "a"):  {f: []string{"a", "x"}},
				ln(3, 1, "b"):  {f: []string{"b"}},
				ln(5, 1, "w"):  {f: []string{"w"}},
				ln(5, 1, "a"):  {f: []string{"a", "x"}},
				ln(5, 1, "b"):  {f: []string{"b"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x1"):   {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(1, 1, "f"):    {f: []string{"f"}},
				ln(2, 1, "x2"):   {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(2, 1, "f"):    {f: []string{"f"}},
				ln(3, 1, "y"):    {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(3, 1, "x1"):   {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(4, 1, "y"):    {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(4, 1, "x2"):   {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(5, 1, "z"):    {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(5, 1, "y"):    {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(6, 1, "out1"): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(6, 1, "z"):    {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(7, 1, "out2"): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(7, 1, "z"):    {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(7, 1, ".f"):   {e: []string{"f"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x1"):   {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(1, 1, "f"):    {f: []string{"f"}},
				ln(2, 1, "x2"):   {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(2, 1, "f"):    {f: []string{"f"}},
				ln(3, 1, "y"):    {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(3, 1, "x1"):   {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(3, 1, "x2"):   {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(4, 1, "z"):    {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(4, 1, "y"):    {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(5, 1, "out1"): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(5, 1, "z"):    {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(6, 1, "out2"): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(6, 1, "z"):    {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				ln(6, 1, ".f"):   {e: []string{"f"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):   {f: []string{"a", "c", "d", "x", "y"}},
				ln(1, 1, "a"):   {e: []string{"a", "c", "d", "x", "y"}},
				ln(1, 1, ".b"):  {e: []string{"b"}},
				ln(3, 1, "c"):   {f: []string{"a", "c", "d", "x", "y"}},
				ln(3, 1, "b"):   {f: []string{"b"}},
				ln(4, 1, "d"):   {f: []string{"a", "c", "d", "x", "y"}},
				ln(4, 1, "b"):   {f: []string{"b"}},
				ln(4, 1, "int"): {e: []string{"a", "b", "c", "d", "x", "y"}},
				ln(5, 1, "a"):   {f: []string{"a", "c", "d", "x", "y"}},
				ln(5, 1, "c"):   {e: []string{"a", "c", "d", "x", "y"}},
				ln(5, 1, "d"):   {e: []string{"a", "c", "d", "x", "y"}},
				ln(7, 1, "y"):   {f: []string{"a", "c", "d", "x", "y"}},
				ln(7, 1, "a"):   {e: []string{"a", "c", "d", "x", "y"}},
				ln(7, 1, ".b"):  {e: []string{"b"}},
			},
		},

		{
			name: "Embedding",
			archive: `-- a.cue --
x: y: z: 3
o: { p: 4, x.y }
q: o.z
`,
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):  {f: []string{"o", "q", "x"}},
				ln(1, 1, "y"):  {f: []string{"y"}},
				ln(1, 1, "z"):  {f: []string{"z"}},
				ln(2, 1, "o"):  {f: []string{"o", "q", "x"}},
				ln(2, 1, "p"):  {f: []string{"p", "z"}},
				ln(2, 1, "x"):  {e: []string{"o", "p", "q", "x"}},
				ln(2, 1, ".y"): {e: []string{"y"}},
				ln(3, 1, "q"):  {f: []string{"o", "q", "x"}},
				ln(3, 1, "o"):  {e: []string{"o", "q", "x"}},
				ln(3, 1, ".z"): {e: []string{"p", "z"}},
			},
		},

		{
			name: "String_Literal",
			archive: `-- a.cue --
x: y: a.b
a: b: 5
"a": b: 6
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "a"): {ln(2, 1, "a"), ln(3, 1, `"a"`)},
				ln(1, 1, "b"): {ln(2, 1, "b"), ln(3, 1, "b")},

				ln(1, 1, "x"):   {self},
				ln(1, 1, "y"):   {self},
				ln(2, 1, "a"):   {self, ln(3, 1, `"a"`)},
				ln(2, 1, "b"):   {self, ln(3, 1, "b")},
				ln(3, 1, `"a"`): {self, ln(2, 1, "a")},
				ln(3, 1, "b"):   {self, ln(2, 1, "b")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):   {f: []string{"a", "x"}},
				ln(1, 1, "y"):   {f: []string{"y"}},
				ln(1, 1, "a"):   {e: []string{"a", "x", "y"}},
				ln(1, 1, ".b"):  {e: []string{"b"}},
				ln(2, 1, "a"):   {f: []string{"a", "x"}},
				ln(2, 1, "b"):   {f: []string{"b"}},
				ln(3, 1, `"a"`): {f: []string{"a", "x"}},
				ln(3, 1, "b"):   {f: []string{"b"}},
			},
		},

		{
			name: "List_Index",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}]
y: x[1].b`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, "[1]"): {ln(1, 2, "{")},
				ln(2, 1, "b"):   {ln(1, 1, "b")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):  {f: []string{"x", "y"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(2, 1, "y"):  {f: []string{"x", "y"}},
				ln(2, 1, "x"):  {e: []string{"x", "y"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
			},
		},

		{
			name: "List_Index_Ellipsis",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...{a: 4}]
y: x[17].a`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "x"):    {ln(1, 1, "x")},
				ln(2, 1, "[17]"): {ln(1, 1, "...")},
				ln(2, 1, "a"):    {ln(1, 2, "a")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "a"): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):  {f: []string{"x", "y"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 2, "a"):  {f: []string{"a"}},
				ln(2, 1, "y"):  {f: []string{"x", "y"}},
				ln(2, 1, "x"):  {e: []string{"x", "y"}},
				ln(2, 1, ".a"): {e: []string{"a"}},
			},
		},

		{
			name: "List_Index_Ellipsis_Indirect",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...z]
y: x[17].a
z: a: 4`,
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):  {f: []string{"x", "y", "z"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 1, "z"):  {e: []string{"x", "y", "z"}},
				ln(2, 1, "y"):  {f: []string{"x", "y", "z"}},
				ln(2, 1, "x"):  {e: []string{"x", "y", "z"}},
				ln(2, 1, ".a"): {e: []string{"a"}},
				ln(3, 1, "z"):  {f: []string{"x", "y", "z"}},
				ln(3, 1, "a"):  {f: []string{"a"}},
			},
		},

		{
			name: "Ellipsis_Explicit",
			archive: `-- a.cue --
l: [...{x: int}]
d: l & [{x: 3}, {x: 4}]
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "l")},

				ln(1, 1, "l"): {self},
				ln(1, 1, "x"): {self},

				ln(2, 1, "d"): {self},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 1, "x")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "l"):   {f: []string{"d", "l"}},
				ln(1, 1, "x"):   {f: []string{"x"}},
				ln(1, 1, "int"): {e: []string{"d", "l", "x"}},
				ln(2, 1, "d"):   {f: []string{"d", "l"}},
				ln(2, 1, "l"):   {e: []string{"d", "l"}},
				ln(2, 1, "x"):   {f: []string{"x"}},
				ln(2, 2, "x"):   {f: []string{"x"}},
			},
		},

		{
			name: "Ellipsis_Implicit",
			archive: `-- a.cue --
d: [...{x: int}]
d: [{x: 3}, {x: 4}]
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "d"): {self, ln(2, 1, "d")},
				ln(1, 1, "x"): {self},

				ln(2, 1, "d"): {self, ln(1, 1, "d")},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 1, "x")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "d"):   {f: []string{"d"}},
				ln(1, 1, "x"):   {f: []string{"x"}},
				ln(1, 1, "int"): {e: []string{"d", "x"}},
				ln(2, 1, "d"):   {f: []string{"d"}},
				ln(2, 1, "x"):   {f: []string{"x"}},
				ln(2, 2, "x"):   {f: []string{"x"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				ln(1, 1, "a"):   {f: []string{"a"}},
				ln(1, 1, "b"):   {f: []string{"b"}},
				ln(1, 2, "a"):   {f: []string{"a"}},
				ln(2, 1, "y"):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				ln(2, 1, "z"):   {e: []string{"o", "p", "q", "x", "y", "z"}},
				ln(3, 1, "z"):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				ln(3, 1, "a"):   {f: []string{"a"}},
				ln(3, 1, "int"): {e: []string{"a", "o", "p", "q", "x", "y", "z"}},
				ln(4, 1, "o"):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				ln(4, 1, "x"):   {e: []string{"o", "p", "q", "x", "y", "z"}},
				ln(4, 1, "y"):   {e: []string{"o", "p", "q", "x", "y", "z"}},
				ln(5, 1, "p"):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				ln(5, 1, "o"):   {e: []string{"o", "p", "q", "x", "y", "z"}},
				ln(5, 1, ".a"):  {e: []string{"a"}},
				ln(6, 1, "q"):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				ln(6, 1, "o"):   {e: []string{"o", "p", "q", "x", "y", "z"}},
				ln(6, 1, ".a"):  {e: []string{"a"}},
			},
		},

		{
			name: "StringLit_Conjunction",
			archive: `-- a.cue --
c: {a: b, "b": x: 3} & {b: x: 3, z: b.x}
b: e: 7
d: c.b.x
`,
			expectDefinitions: map[*position][]*position{
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
				ln(2, 1, "e"): {self},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "c"):   {f: []string{"b", "c", "d"}},
				ln(1, 1, "a"):   {f: []string{"a", "b", "z"}},
				ln(1, 1, "b"):   {f: []string{"e"}, e: []string{"a", "b", "c", "d"}},
				ln(1, 1, `"b"`): {f: []string{"a", "b", "z"}},
				ln(1, 1, "x"):   {f: []string{"x"}},
				ln(1, 3, "b"):   {f: []string{"a", "b", "z"}},
				ln(1, 2, "x"):   {f: []string{"x"}},
				ln(1, 1, "z"):   {f: []string{"a", "b", "z"}},
				ln(1, 4, "b"):   {e: []string{"b", "c", "d", "z"}},
				ln(1, 1, ".x"):  {e: []string{"x"}},
				ln(2, 1, "b"):   {f: []string{"b", "c", "d"}},
				ln(2, 1, "e"):   {f: []string{"e"}},
				ln(3, 1, "d"):   {f: []string{"b", "c", "d"}},
				ln(3, 1, "c"):   {e: []string{"b", "c", "d"}},
				ln(3, 1, ".b"):  {e: []string{"a", "b", "z"}},
				ln(3, 1, ".x"):  {e: []string{"x"}},
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
			expectDefinitions: map[*position][]*position{
				ln(4, 1, "b"): {ln(1, 1, "b")},
				ln(4, 1, "a"): {ln(2, 1, "a")},

				ln(1, 1, "b"):   {self},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"):   {f: []string{"b"}},
				ln(2, 1, "a"):   {f: []string{"a", "b"}},
				ln(3, 1, `"b"`): {f: []string{"a", "b"}},
				ln(4, 1, "c"):   {f: []string{"a", "c"}},
				ln(4, 1, "b"):   {e: []string{"a", "b", "c"}},
				ln(4, 1, ".a"):  {e: []string{"a", "b"}},
				ln(5, 1, "a"):   {f: []string{"a", "c"}},
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
			expectDefinitions: map[*position][]*position{
				ln(4, 1, "b"): {ln(3, 1, `"b"`), ln(7, 1, "b")},
				ln(4, 1, "a"): {ln(5, 1, "a")},

				ln(1, 1, "b"):   {self},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self, ln(7, 1, "b")},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},
				ln(7, 1, "b"):   {self, ln(3, 1, `"b"`)},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"):   {f: []string{"b"}},
				ln(2, 1, "a"):   {f: []string{"a", "b"}},
				ln(3, 1, `"b"`): {f: []string{"a", "b"}},
				ln(4, 1, "c"):   {f: []string{"a", "c"}},
				ln(4, 1, "b"):   {e: []string{"a", "b", "c"}},
				ln(4, 1, ".a"):  {e: []string{"a", "c"}},
				ln(5, 1, "a"):   {f: []string{"a", "c"}},
				ln(7, 1, "b"):   {f: []string{"a", "b"}},
				ln(7, 1, "_"):   {f: []string{"a", "c"}, e: []string{"a", "b"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"):   {f: []string{"b"}},
				ln(2, 1, "a"):   {f: []string{"a", "b"}},
				ln(3, 1, `"b"`): {f: []string{"a", "b"}},
				ln(4, 1, "c"):   {f: []string{"a", "c"}},
				ln(4, 1, "b"):   {e: []string{"a", "b", "c"}},
				ln(4, 1, ".a"):  {e: []string{"a", "b"}},
				ln(5, 1, "a"):   {f: []string{"a", "c"}},
				ln(8, 1, "b"):   {f: []string{"b"}},
				ln(8, 2, "b"):   {f: []string{"a", "b"}},
				ln(8, 1, "_"):   {f: []string{"a", "c"}, e: []string{"b"}},
			},
		},

		{
			name: "Inline_Struct_Selector",
			archive: `-- a.cue --
a: {in: {x: 5}, out: in}.out.x`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "in"):  {ln(1, 1, "in")},
				ln(1, 2, "out"): {ln(1, 1, "out")},
				ln(1, 2, "x"):   {ln(1, 1, "x")},

				ln(1, 1, "a"):   {self},
				ln(1, 1, "in"):  {self},
				ln(1, 1, "x"):   {self},
				ln(1, 1, "out"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):    {f: []string{"a"}},
				ln(1, 1, "in"):   {f: []string{"in", "out"}},
				ln(1, 1, "x"):    {f: []string{"x"}},
				ln(1, 1, "out"):  {f: []string{"in", "out"}},
				ln(1, 2, "in"):   {f: []string{"x"}, e: []string{"a", "in", "out"}},
				ln(1, 1, ".out"): {e: []string{"in", "out"}},
				ln(1, 1, ".x"):   {e: []string{"x"}},
			},
		},

		{
			name: "Inline_List_Index_LiteralConst",
			archive: `-- a.cue --
a: [7, {b: 3}, true][1].b`,
			// If the index is a literal const we do resolve it.
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "[1]"): {ln(1, 1, "{")},
				ln(1, 2, "b"):   {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 1, ".b"): {e: []string{"b"}},
			},
		},

		{
			name: "Inline_List_Index_Dynamic",
			archive: `-- a.cue --
a: [7, {b: 3}, true][n].b
n: 1
`,
			// Even the slightest indirection defeats indexing
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "["): {},
				ln(1, 1, "n"): {ln(2, 1, "n")},
				ln(1, 1, "]"): {},
				ln(1, 2, "b"): {},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "n"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {f: []string{"a", "n"}},
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(1, 1, "n"): {e: []string{"a", "n"}},
				ln(2, 1, "n"): {f: []string{"a", "n"}},
			},
		},

		{
			name: "StringLit_Struct_Index_LiteralConst",
			archive: `-- a.cue --
x: "a b": z: 5
y: x["a b"].z`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "x"):       {ln(1, 1, "x")},
				ln(2, 1, `["a b"]`): {ln(1, 1, `"a b"`)},
				ln(2, 1, "z"):       {ln(1, 1, "z")},

				ln(1, 1, "x"):     {self},
				ln(1, 1, `"a b"`): {self},
				ln(1, 1, "z"):     {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):     {f: []string{"x", "y"}},
				ln(1, 1, `"a b"`): {f: []string{"a b"}},
				ln(1, 1, "z"):     {f: []string{"z"}},
				ln(2, 1, "y"):     {f: []string{"x", "y"}},
				ln(2, 1, "x"):     {e: []string{"x", "y"}},
				ln(2, 1, ".z"):    {e: []string{"z"}},
			},
		},

		{
			name: "Inline_Disjunction_Internal",
			archive: `-- a.cue --
a: ({b: c, c: 3} | {c: 4}).c`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "c"): {ln(1, 2, "c")},
				ln(1, 4, "c"): {ln(1, 2, "c"), ln(1, 3, "c")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "c"): {self},
				ln(1, 3, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {e: []string{"a", "b", "c"}},
				ln(1, 2, "c"):  {f: []string{"b", "c"}},
				ln(1, 3, "c"):  {f: []string{"c"}},
				ln(1, 1, ".c"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Cycle_Simple2",
			archive: `-- a.cue --
a: b
b: a`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {f: []string{"a", "b"}},
				ln(1, 1, "b"): {e: []string{"a", "b"}},
				ln(2, 1, "b"): {f: []string{"a", "b"}},
				ln(2, 1, "a"): {e: []string{"a", "b"}},
			},
		},

		{
			name: "Cycle_Simple3",
			archive: `-- a.cue --
a: b
b: c
c: a`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "c"): {ln(3, 1, "c")},
				ln(3, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
				ln(3, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {f: []string{"a", "b", "c"}},
				ln(1, 1, "b"): {e: []string{"a", "b", "c"}},
				ln(2, 1, "b"): {f: []string{"a", "b", "c"}},
				ln(2, 1, "c"): {e: []string{"a", "b", "c"}},
				ln(3, 1, "c"): {f: []string{"a", "b", "c"}},
				ln(3, 1, "a"): {e: []string{"a", "b", "c"}},
			},
		},

		// These "structural" cycles are errors in the evaluator. But
		// there's no reason we can't resolve them.
		{
			name: "Cycle_Structural_Simple",
			archive: `-- a.cue --
a: b: c: a`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {f: []string{"a"}},
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(1, 1, "c"): {f: []string{"c"}},
				ln(1, 2, "a"): {f: []string{"b"}, e: []string{"a", "b", "c"}},
			},
		},

		{
			name: "Structural_Simple_Selector",
			archive: `-- a.cue --
a: b: c: a.b`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 1, "c"):  {f: []string{"c"}},
				ln(1, 2, "a"):  {e: []string{"a", "b", "c"}},
				ln(1, 1, ".b"): {e: []string{"b"}},
			},
		},

		{
			name: "Cycle_Structural_Complex",
			archive: `-- a.cue --
y: [string]: b: y
x: y
x: c: x
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "y"): {ln(1, 1, "y")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
				ln(3, 2, "x"): {ln(2, 1, "x"), ln(3, 1, "x")},

				ln(1, 1, "y"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "x"): {self, ln(3, 1, "x")},

				ln(3, 1, "x"): {self, ln(2, 1, "x")},
				ln(3, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "y"):      {f: []string{"x", "y"}},
				ln(1, 1, "string"): {e: []string{"x", "y"}},
				ln(1, 1, "b"):      {f: []string{"b"}},
				ln(1, 2, "y"):      {e: []string{"b", "x", "y"}},
				ln(2, 1, "x"):      {f: []string{"x", "y"}},
				ln(2, 1, "y"):      {f: []string{"c"}, e: []string{"x", "y"}},
				ln(3, 1, "x"):      {f: []string{"x", "y"}},
				ln(3, 1, "c"):      {f: []string{"c"}},
				ln(3, 2, "x"):      {f: []string{"c"}, e: []string{"c", "x", "y"}},
			},
		},

		{
			name: "Alias_Plain_Label_Internal",
			archive: `-- a.cue --
l=a: {b: 3, c: l.b}`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {f: []string{"b", "c"}},
				ln(1, 2, "l"):  {e: []string{"a", "b", "c", "l"}},
				ln(1, 1, ".b"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Plain_Label_Internal_Implicit",
			archive: `-- a.cue --
l=a: b: 3
a: c: l.b`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self, ln(1, 1, "a")},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(2, 1, "a"):  {f: []string{"a"}},
				ln(2, 1, "c"):  {f: []string{"b", "c"}},
				ln(2, 1, "l"):  {e: []string{"a", "c", "l"}},
				ln(2, 1, ".b"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Plain_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
a: b: 3
l=a: c: l.b`,
			expectDefinitions: map[*position][]*position{
				ln(2, 2, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self, ln(1, 1, "a")},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(2, 1, "a"):  {f: []string{"a"}},
				ln(2, 1, "c"):  {f: []string{"b", "c"}},
				ln(2, 2, "l"):  {e: []string{"a", "c", "l"}},
				ln(2, 1, ".b"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Plain_Label_External",
			archive: `-- a.cue --
l=a: b: 3
c: l.b`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "c"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(2, 1, "c"):  {f: []string{"a", "c"}},
				ln(2, 1, "l"):  {e: []string{"a", "c", "l"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "h"}},
				ln(2, 1, "b"):  {f: []string{"b", "e", "f"}},
				ln(2, 1, "c"):  {f: []string{"c", "d"}},
				ln(2, 2, "l"):  {e: []string{"a", "b", "c", "d", "e", "h", "l"}},
				ln(2, 1, ".d"): {e: []string{"c", "d"}},
				ln(2, 2, "d"):  {f: []string{"c", "d"}},
				ln(3, 1, "e"):  {f: []string{"b", "e", "f"}},
				ln(3, 1, "l"):  {e: []string{"a", "b", "e", "h", "l"}},
				ln(3, 1, ".d"): {e: []string{"c", "d"}},
				ln(5, 1, "a"):  {f: []string{"a", "h"}},
				ln(5, 1, "f"):  {f: []string{"b", "e", "f"}},
				ln(5, 1, "l"):  {e: []string{"a", "f", "h"}},
				ln(6, 1, "h"):  {f: []string{"a", "h"}},
				ln(6, 1, "a"):  {e: []string{"a", "h"}},
				ln(6, 1, ".l"): {e: []string{"b", "e", "f"}},
			},
		},

		{
			name: "Alias_Dynamic_Label_Internal",
			archive: `-- a.cue --
l=(a): {b: 3, c: l.b}`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "(")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {e: []string{"l"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {f: []string{"b", "c"}},
				ln(1, 2, "l"):  {e: []string{"b", "c", "l"}},
				ln(1, 1, ".b"): {e: []string{"b", "c"}},
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
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "(")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {e: []string{"l"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(2, 1, "a"):  {e: []string{"l"}},
				ln(2, 1, "c"):  {f: []string{"c"}},
				ln(2, 1, "l"):  {e: []string{"c", "l"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
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
			expectDefinitions: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "(")},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {e: []string{"l"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(2, 1, "a"):  {e: []string{"l"}},
				ln(2, 1, "c"):  {f: []string{"c"}},
				ln(2, 2, "l"):  {e: []string{"c", "l"}},
				ln(2, 1, ".b"): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Dynamic_Label_External",
			archive: `-- a.cue --
l=(a): b: 3
c: l.b`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, ("("))},
				ln(2, 1, "b"): {ln(1, 1, ("b"))},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {e: []string{"c", "l"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(2, 1, "c"):  {f: []string{"c"}},
				ln(2, 1, "l"):  {e: []string{"c", "l"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
			},
		},

		{
			name: "Alias_Pattern_Label_Internal",
			archive: `-- a.cue --
l=[a]: {b: 3, c: l.b}`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "[")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {f: []string{"b", "c"}},
				ln(1, 2, "l"):  {e: []string{"b", "c", "l"}},
				ln(1, 1, ".b"): {e: []string{"b", "c"}},
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
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(2, 1, "c"): {f: []string{"c"}},
				ln(2, 1, "l"): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
[a]: b: 3
l=[a]: c: l.b`,
			// Again, the two [a] patterns are not merged. The l of l.b
			// can be resolved, but not the b.
			expectDefinitions: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "[")},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(2, 1, "c"):  {f: []string{"c"}},
				ln(2, 2, "l"):  {e: []string{"c", "l"}},
				ln(2, 1, ".b"): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Label_External",
			archive: `-- a.cue --
l=[a]: b: 3
c: l.b`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {e: []string{"c"}},
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(2, 1, "c"): {f: []string{"c"}},
				ln(2, 1, "l"): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Expr_Internal",
			archive: `-- a.cue --
[l=a]: {b: 3, c: l, d: l.b}`,
			// This type of alias binds l to the key. So c: l will work,
			// but for the b in d: l.b there is no resolution.
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 3, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {},

				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
				ln(1, 1, "d"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {e: []string{"l"}},
				ln(1, 1, "b"): {f: []string{"b", "c", "d"}},
				ln(1, 1, "c"): {f: []string{"b", "c", "d"}},
				ln(1, 2, "l"): {e: []string{"b", "c", "d", "l"}},
				ln(1, 1, "d"): {f: []string{"b", "c", "d"}},
				ln(1, 3, "l"): {e: []string{"b", "c", "d", "l"}},
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
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(2, 1, "c"): {f: []string{"c"}},
				ln(2, 1, "l"): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Expr_External",
			archive: `-- a.cue --
[l=a]: b: 3
c: l`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(2, 1, "c"): {f: []string{"c"}},
				ln(2, 1, "l"): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Expr_Internal",
			archive: `-- a.cue --
a: l={b: 3, c: l.b}`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {f: []string{"b", "c"}},
				ln(1, 2, "l"):  {e: []string{"a", "b", "c", "l"}},
				ln(1, 1, ".b"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Expr_Internal_Explicit",
			archive: `-- a.cue --
a: l={b: 3} & {c: l.b}`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {f: []string{"b", "c"}},
				ln(1, 2, "l"):  {e: []string{"a", "c", "l"}},
				ln(1, 1, ".b"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Expr_Internal_Explicit_Paren",
			// The previous test case works because it's parsed like
			// this:
			archive: `-- a.cue --
a: l=({b: 3} & {c: l.b})`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b", "c"}},
				ln(1, 1, "c"):  {f: []string{"b", "c"}},
				ln(1, 2, "l"):  {e: []string{"a", "c", "l"}},
				ln(1, 1, ".b"): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Expr_External",
			archive: `-- a.cue --
a: l={b: 3}
c: l.b`,
			// This type of alias is only visible within the value.
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"): {f: []string{"a", "c"}},
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(2, 1, "c"): {f: []string{"a", "c"}},
				ln(2, 1, "l"): {e: []string{"a", "c"}},
			},
		},

		{
			name: "Alias_Expr_Call",
			archive: `-- a.cue --
a: n=(2 * (div(n, 2))) | error("\(n) is not even")
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "n"): {ln(1, 1, "n")},
				ln(1, 3, "n"): {ln(1, 1, "n")},

				ln(1, 1, "a"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):     {f: []string{"a"}},
				ln(1, 1, "div"):   {e: []string{"a", "n"}},
				ln(1, 2, "n"):     {e: []string{"a", "n"}},
				ln(1, 1, "error"): {e: []string{"a", "n"}},
				ln(1, 3, "n"):     {e: []string{"a", "n"}},
			},
		},

		{
			name: "Call_Arg_Expr",
			archive: `-- a.cue --
c: (f({a: b, b: 3})).g
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "c"): {f: []string{"c"}},
				ln(1, 1, "f"): {e: []string{"c"}},
				ln(1, 1, "a"): {f: []string{"a", "b"}},
				ln(1, 1, "b"): {e: []string{"a", "b", "c"}},
				ln(1, 2, "b"): {f: []string{"a", "b"}},
			},
		},

		{
			name: "Disjunction_Simple",
			archive: `-- a.cue --
d: {a: b: 3} | {a: b: 4, c: 5}
o: d.a.b
p: d.c
`,
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "d"):  {f: []string{"d", "o", "p"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 2, "a"):  {f: []string{"a", "c"}},
				ln(1, 2, "b"):  {f: []string{"b"}},
				ln(1, 1, "c"):  {f: []string{"a", "c"}},
				ln(2, 1, "o"):  {f: []string{"d", "o", "p"}},
				ln(2, 1, "d"):  {e: []string{"d", "o", "p"}},
				ln(2, 1, ".a"): {e: []string{"a", "c"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
				ln(3, 1, "p"):  {f: []string{"d", "o", "p"}},
				ln(3, 1, "d"):  {e: []string{"d", "o", "p"}},
				ln(3, 1, ".c"): {e: []string{"a", "c"}},
			},
		},

		{
			name: "Disjunction_Inline",
			archive: `-- a.cue --
d: ({a: b: 3} | {a: b: 4}) & {c: 5}
o: d.a.b
p: d.c
`,
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "d"):  {f: []string{"d", "o", "p"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 2, "a"):  {f: []string{"a"}},
				ln(1, 2, "b"):  {f: []string{"b"}},
				ln(1, 1, "c"):  {f: []string{"a", "c"}},
				ln(2, 1, "o"):  {f: []string{"d", "o", "p"}},
				ln(2, 1, "d"):  {e: []string{"d", "o", "p"}},
				ln(2, 1, ".a"): {e: []string{"a", "c"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
				ln(3, 1, "p"):  {f: []string{"d", "o", "p"}},
				ln(3, 1, "d"):  {e: []string{"d", "o", "p"}},
				ln(3, 1, ".c"): {e: []string{"a", "c"}},
			},
		},

		{
			name: "Disjunction_Chained",
			archive: `-- a.cue --
d1: {a: 1} | {a: 2}
d2: {a: 3} | {a: 4}
o: (d1 & d2).a
`,
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "d1"): {f: []string{"d1", "d2", "o"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 2, "a"):  {f: []string{"a"}},
				ln(2, 1, "d2"): {f: []string{"d1", "d2", "o"}},
				ln(2, 1, "a"):  {f: []string{"a"}},
				ln(2, 2, "a"):  {f: []string{"a"}},
				ln(3, 1, "o"):  {f: []string{"d1", "d2", "o"}},
				ln(3, 1, "d1"): {e: []string{"d1", "d2", "o"}},
				ln(3, 1, "d2"): {e: []string{"d1", "d2", "o"}},
				ln(3, 1, ".a"): {e: []string{"a"}},
			},
		},

		{
			name: "Disjunction_Selected",
			archive: `-- a.cue --
d: {x: 17} | string
r: d & {x: int}
out: r.x
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "r"): {ln(2, 1, "r")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(2, 1, "x")},

				ln(1, 1, "d"): {self},
				ln(1, 1, "x"): {self}, // note non-symmetric!

				ln(2, 1, "r"): {self},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},

				ln(3, 1, "out"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "d"):      {f: []string{"d", "out", "r"}},
				ln(1, 1, "x"):      {f: []string{"x"}},
				ln(1, 1, "string"): {e: []string{"d", "out", "r"}},
				ln(2, 1, "r"):      {f: []string{"d", "out", "r"}},
				ln(2, 1, "d"):      {e: []string{"d", "out", "r"}},
				ln(2, 1, "x"):      {f: []string{"x"}},
				ln(2, 1, "int"):    {e: []string{"d", "out", "r", "x"}},
				ln(3, 1, "out"):    {f: []string{"d", "out", "r"}},
				ln(3, 1, "r"):      {e: []string{"d", "out", "r"}},
				ln(3, 1, ".x"):     {e: []string{"x"}},
			},
		},

		{
			name: "Disjunction_Scopes",
			archive: `-- a.cue --
c: {a: b} | {b: 3}
b: 7
d: c.b
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "c"):  {f: []string{"b", "c", "d"}},
				ln(1, 1, "a"):  {f: []string{"a"}},
				ln(1, 1, "b"):  {e: []string{"a", "b", "c", "d"}},
				ln(1, 2, "b"):  {f: []string{"b"}},
				ln(2, 1, "b"):  {f: []string{"b", "c", "d"}},
				ln(3, 1, "d"):  {f: []string{"b", "c", "d"}},
				ln(3, 1, "c"):  {e: []string{"b", "c", "d"}},
				ln(3, 1, ".b"): {e: []string{"a", "b"}},
			},
		},

		{
			name: "Disjunction_Looping",
			archive: `-- a.cue --
a: {b: c.d, d: 3} | {d: 4}
c: a
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "c"): {ln(2, 1, "c")},
				ln(1, 1, "d"): {ln(1, 2, "d"), ln(1, 3, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "d"): {self},
				ln(1, 3, "d"): {self},

				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "c"}},
				ln(1, 1, "b"):  {f: []string{"b", "d"}},
				ln(1, 1, "c"):  {e: []string{"a", "b", "c", "d"}},
				ln(1, 1, ".d"): {e: []string{"b", "d"}},
				ln(1, 2, "d"):  {f: []string{"b", "d"}},
				ln(1, 3, "d"):  {f: []string{"d"}},
				ln(2, 1, "c"):  {f: []string{"a", "c"}},
				ln(2, 1, "a"):  {f: []string{"b", "d"}, e: []string{"a", "c"}},
			},
		},

		{
			name: "Conjunction_Scopes",
			archive: `-- a.cue --
c: {a: b} & {b: 3}
b: 7
d: c.b
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "c"):  {f: []string{"b", "c", "d"}},
				ln(1, 1, "a"):  {f: []string{"a", "b"}},
				ln(1, 1, "b"):  {e: []string{"a", "b", "c", "d"}},
				ln(1, 2, "b"):  {f: []string{"a", "b"}},
				ln(2, 1, "b"):  {f: []string{"b", "c", "d"}},
				ln(3, 1, "d"):  {f: []string{"b", "c", "d"}},
				ln(3, 1, "c"):  {e: []string{"b", "c", "d"}},
				ln(3, 1, ".b"): {e: []string{"a", "b"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):   {f: []string{"x", "y"}},
				ln(2, 1, "a"):   {f: []string{"a", "b"}},
				ln(2, 1, "int"): {e: []string{"a", "b", "x", "y"}},
				ln(2, 1, "b"):   {f: []string{"a", "b"}},
				ln(2, 2, "a"):   {e: []string{"a", "b", "x", "y"}},
				ln(3, 1, "a"):   {f: []string{"a", "b"}},
				ln(5, 1, "a"):   {f: []string{"a", "b"}},
				ln(7, 1, "y"):   {f: []string{"x", "y"}},
				ln(7, 1, "x"):   {e: []string{"x", "y"}},
				ln(7, 1, ".a"):  {e: []string{"a", "b"}},
			},
		},

		{
			name: "Conjunction_EvenMoreScopes",
			archive: `-- a.cue --
c: {a: b, b: x: 3} & {b: x: 3, z: b.x}
b: 7
d: c.b.x`,
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "c"):  {f: []string{"b", "c", "d"}},
				ln(1, 1, "a"):  {f: []string{"a", "b", "z"}},
				ln(1, 1, "b"):  {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				ln(1, 2, "b"):  {f: []string{"a", "b", "z"}},
				ln(1, 1, "x"):  {f: []string{"x"}},
				ln(1, 3, "b"):  {f: []string{"a", "b", "z"}},
				ln(1, 2, "x"):  {f: []string{"x"}},
				ln(1, 1, "z"):  {f: []string{"a", "b", "z"}},
				ln(1, 4, "b"):  {e: []string{"b", "c", "d", "z"}},
				ln(1, 1, ".x"): {e: []string{"x"}},
				ln(2, 1, "b"):  {f: []string{"b", "c", "d"}},
				ln(3, 1, "d"):  {f: []string{"b", "c", "d"}},
				ln(3, 1, "c"):  {e: []string{"b", "c", "d"}},
				ln(3, 1, ".b"): {e: []string{"a", "b", "z"}},
				ln(3, 1, ".x"): {e: []string{"x"}},
			},
		},

		{
			name: "Conjunction_Selector",
			archive: `-- a.cue --
b: ({a: 6} & {a: int}).a
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 3, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "a"): {self, ln(1, 2, "a")},
				ln(1, 2, "a"): {self, ln(1, 1, "a")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "b"):   {f: []string{"b"}},
				ln(1, 1, "a"):   {f: []string{"a"}},
				ln(1, 2, "a"):   {f: []string{"a"}},
				ln(1, 1, "int"): {e: []string{"a", "b"}},
				ln(1, 1, ".a"):  {e: []string{"a"}},
			},
		},

		{
			name: "Binary_Expr",
			archive: `-- a.cue --
c: ({a: 6, d: a} + {b: a}).g
a: 12
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 3, "a"): {ln(2, 1, "a")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "d"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "c"): {f: []string{"a", "c"}},
				ln(1, 1, "a"): {f: []string{"a", "d"}},
				ln(1, 1, "d"): {f: []string{"a", "d"}},
				ln(1, 2, "a"): {e: []string{"a", "c", "d"}},
				ln(1, 1, "b"): {f: []string{"b"}},
				ln(1, 3, "a"): {e: []string{"a", "b", "c"}},
				ln(2, 1, "a"): {f: []string{"a", "c"}},
			},
		},

		{
			name: "Import_Builtin_Call",
			archive: `-- a.cue --
import "magic"

x: magic.Merlin(y)
y: "wand"
`,
			expectDefinitions: map[*position][]*position{
				ln(3, 1, "magic"):  {ln(1, 1, `"magic"`)},
				ln(3, 1, "Merlin"): {},
				ln(3, 1, "y"):      {ln(4, 1, "y")},

				ln(3, 1, "x"): {self},
				ln(4, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(3, 1, "x"):     {f: []string{"x", "y"}},
				ln(3, 1, "magic"): {e: []string{"magic", "x", "y"}},
				ln(3, 1, "y"):     {e: []string{"magic", "x", "y"}},
				ln(4, 1, "y"):     {f: []string{"x", "y"}},
			},
		},

		{
			name: "Import_alias",
			archive: `-- a.cue --
import wand "magic"

x: wand.foo
`,
			expectDefinitions: map[*position][]*position{
				ln(3, 1, "wand"): {ln(1, 1, "wand")},
				ln(3, 1, "foo"):  {},
				ln(3, 1, "x"):    {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(3, 1, "x"):    {f: []string{"x"}},
				ln(3, 1, "wand"): {e: []string{"wand", "x"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "b", "c", "d"}},
				ln(2, 1, "b"):  {f: []string{"a", "b", "c", "d"}},
				ln(2, 1, "c"):  {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				ln(3, 1, "c"):  {f: []string{"a", "b", "c", "d"}},
				ln(3, 1, "x"):  {f: []string{"x"}},
				ln(4, 1, "d"):  {f: []string{"a", "b", "c", "d"}},
				ln(4, 1, "a"):  {e: []string{"a", "b", "c", "d"}},
				ln(4, 1, "b"):  {e: []string{"a", "b", "c", "d"}},
				ln(4, 1, ".x"): {e: []string{"x"}},
			},
		},

		{
			name: "Interpolation_Field",
			archive: `-- a.cue --
a: 5
"five\(a)": hello
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):     {f: []string{"a"}},
				ln(2, 1, "a"):     {e: []string{"a"}},
				ln(2, 1, "hello"): {e: []string{"a"}},
			},
		},

		{
			name: "Interpolation_Expr",
			archive: `-- a.cue --
y: "\({a: 3, b: a}.b) \(a)"
a: 12
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
				ln(1, 3, "a"): {ln(2, 1, "a")},

				ln(1, 1, "y"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "a"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "y"):  {f: []string{"a", "y"}},
				ln(1, 1, "a"):  {f: []string{"a", "b"}},
				ln(1, 1, "b"):  {f: []string{"a", "b"}},
				ln(1, 2, "a"):  {e: []string{"a", "b", "y"}},
				ln(1, 1, ".b"): {e: []string{"a", "b"}},
				ln(1, 3, "a"):  {e: []string{"a", "y"}},
				ln(2, 1, "a"):  {f: []string{"a", "y"}},
			},
		},

		{
			name: "MultiByte_Expression",
			archive: `-- a.cue --
x: "💩" + y
y: "sticks"
`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "y"): {ln(2, 1, "y")},

				ln(1, 1, "x"): {self},
				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"): {f: []string{"x", "y"}},
				ln(1, 1, "y"): {e: []string{"x", "y"}},
				ln(2, 1, "y"): {f: []string{"x", "y"}},
			},
		},

		{
			name: "MultiByte_Index",
			archive: `-- a.cue --
x: {"💩": "sticks"}
y: x["💩"]
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "x"):     {ln(1, 1, "x")},
				ln(2, 1, `["💩"]`): {ln(1, 1, `"💩"`)},

				ln(1, 1, "x"):   {self},
				ln(1, 1, `"💩"`): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):   {f: []string{"x", "y"}},
				ln(1, 1, `"💩"`): {f: []string{"💩"}},
				ln(2, 1, "y"):   {f: []string{"x", "y"}},
				ln(2, 1, "x"):   {e: []string{"x", "y"}},
			},
		},

		{
			name: "MultiByte_Selector",
			archive: `-- a.cue --
x: {"💩": "sticks"}
y: x."💩"
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, `"💩"`): {ln(1, 1, `"💩"`)},

				ln(1, 1, "x"):   {self},
				ln(1, 1, `"💩"`): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"):   {f: []string{"x", "y"}},
				ln(1, 1, `"💩"`): {f: []string{"💩"}},
				ln(2, 1, "y"):   {f: []string{"x", "y"}},
				ln(2, 1, "x"):   {e: []string{"x", "y"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "b", "x", "y"}},
				ln(2, 1, "b"):  {f: []string{"a", "b", "x", "y"}},
				ln(3, 1, "x"):  {f: []string{"a", "b", "x", "y"}},
				ln(4, 1, "a"):  {e: []string{"a", "b", "l", "x", "y", "z"}},
				ln(5, 1, "c"):  {f: []string{"c", "z"}},
				ln(5, 1, "b"):  {e: []string{"a", "b", "c", "l", "x", "y", "z"}},
				ln(7, 1, "z"):  {f: []string{"c", "z"}},
				ln(7, 1, "l"):  {e: []string{"a", "b", "l", "x", "y", "z"}},
				ln(7, 1, ".c"): {e: []string{"c", "z"}},
				ln(9, 1, "y"):  {f: []string{"a", "b", "x", "y"}},
				ln(9, 1, "x"):  {e: []string{"a", "b", "l", "x", "y"}},
				ln(9, 1, ".c"): {e: []string{"c", "z"}},
			},
		},

		{
			name: "Comprehension_Let",
			archive: `-- a.cue --
a: b: c: 17
let x=a.b
y: x.c
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "a"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
				ln(3, 1, "x"): {ln(2, 1, "x")},
				ln(3, 1, "c"): {ln(1, 1, "c")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(3, 1, "y"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "y"}},
				ln(1, 1, "b"):  {f: []string{"b"}},
				ln(1, 1, "c"):  {f: []string{"c"}},
				ln(2, 1, "a"):  {e: []string{"a", "x", "y"}},
				ln(2, 1, ".b"): {e: []string{"b"}},
				ln(3, 1, "y"):  {f: []string{"a", "y"}},
				ln(3, 1, "x"):  {e: []string{"a", "x", "y"}},
				ln(3, 1, ".c"): {e: []string{"c"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):  {f: []string{"a", "o"}},
				ln(3, 1, "c"):  {f: []string{"c", "d"}},
				ln(3, 1, "b"):  {e: []string{"a", "b", "c", "o"}},
				ln(5, 1, "a"):  {f: []string{"a", "o"}},
				ln(5, 1, "d"):  {f: []string{"c", "d"}},
				ln(5, 1, "b"):  {e: []string{"a", "d", "o"}},
				ln(6, 1, "o"):  {f: []string{"a", "o"}},
				ln(6, 1, "a"):  {e: []string{"a", "o"}},
				ln(6, 1, ".b"): {e: []string{"c", "d"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "a"):   {f: []string{"a", "b", "o", "q", "r"}},
				ln(1, 1, "x"):   {f: []string{"x", "y", "z"}},
				ln(1, 1, "y"):   {f: []string{"x", "y", "z"}},
				ln(1, 1, "z"):   {f: []string{"x", "y", "z"}},
				ln(2, 1, "b"):   {f: []string{"a", "b", "o", "q", "r"}},
				ln(2, 1, "x"):   {f: []string{"x", "y", "z"}},
				ln(2, 1, "y"):   {f: []string{"x", "y", "z"}},
				ln(2, 1, "z"):   {f: []string{"x", "y", "z"}},
				ln(3, 1, "o"):   {f: []string{"a", "b", "o", "q", "r"}},
				ln(4, 1, "a"):   {e: []string{"a", "b", "o", "q", "r"}},
				ln(5, 1, "k"):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				ln(5, 1, "v"):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				ln(5, 1, "b"):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				ln(5, 2, "k"):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				ln(6, 1, "p"):   {f: []string{"p"}},
				ln(6, 1, "v"):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				ln(9, 1, "q"):   {f: []string{"a", "b", "o", "q", "r"}},
				ln(9, 1, "o"):   {e: []string{"a", "b", "o", "q", "r"}},
				ln(9, 1, ".p"):  {e: []string{"p"}},
				ln(10, 1, "r"):  {f: []string{"a", "b", "o", "q", "r"}},
				ln(10, 1, "o"):  {e: []string{"a", "b", "o", "q", "r"}},
				ln(10, 1, ".k"): {e: []string{"p"}},
			},
		},

		{
			name: "Comprehension_For_ForwardsReference",
			archive: `-- a.cue --
for a, b in foo.bar {}
foo: bar: "baz"`,
			expectDefinitions: map[*position][]*position{
				ln(1, 1, "foo"): {ln(2, 1, "foo")},
				ln(1, 1, "bar"): {ln(2, 1, "bar")},

				ln(2, 1, "foo"): {self},
				ln(2, 1, "bar"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "foo"):  {e: []string{"foo"}},
				ln(1, 1, ".bar"): {e: []string{"bar"}},
				ln(2, 1, "foo"):  {f: []string{"foo"}},
				ln(2, 1, "bar"):  {f: []string{"bar"}},
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
			expectDefinitions: map[*position][]*position{
				ln(2, 2, "k"): {ln(4, 1, "k")},
				ln(2, 3, "k"): {ln(2, 1, "k")},

				ln(1, 1, "x"): {self},
				ln(2, 2, "v"): {self},
				ln(4, 1, "k"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"): {f: []string{"k", "x"}},
				ln(2, 2, "k"): {e: []string{"k", "x"}},
				ln(2, 2, "v"): {f: []string{"v"}},
				ln(2, 3, "k"): {e: []string{"k", "v", "x"}},
				ln(4, 1, "k"): {f: []string{"k", "x"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "g"):  {f: []string{"g", "i"}},
				ln(2, 2, "x"):  {e: []string{"g", "i", "x"}},
				ln(3, 2, "x"):  {e: []string{"g", "i", "x"}},
				ln(4, 1, "h"):  {f: []string{"h"}},
				ln(4, 1, "x"):  {e: []string{"g", "h", "i", "x"}},
				ln(6, 1, "i"):  {f: []string{"g", "i"}},
				ln(6, 1, "g"):  {e: []string{"g", "i"}},
				ln(6, 1, ".h"): {e: []string{"h"}},
			},
		},

		{
			name: "Definitions",
			archive: `-- a.cue --
#x: y: #z: 3
o: #x & #x.y.z
`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "#x"): {ln(1, 1, "#x")},
				ln(2, 2, "#x"): {ln(1, 1, "#x")},
				ln(2, 1, "y"):  {ln(1, 1, "y")},

				ln(1, 1, "#x"): {self},
				ln(1, 1, "y"):  {self},
				ln(1, 1, "#z"): {self},

				ln(2, 1, "o"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "#x"): {f: []string{"#x", "o"}},
				ln(1, 1, "y"):  {f: []string{"y"}},
				ln(1, 1, "#z"): {f: []string{"#z"}},
				ln(2, 1, "o"):  {f: []string{"#x", "o"}},
				ln(2, 1, "#x"): {e: []string{"#x", "o"}},
				ln(2, 2, "#x"): {e: []string{"#x", "o"}},
				ln(2, 1, ".y"): {e: []string{"y"}},
				ln(2, 1, ".z"): {e: []string{"#z"}},
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
			expectDefinitions: map[*position][]*position{
				fln("b.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo")},

				fln("a.cue", 3, 1, "foo"): {self},
				fln("b.cue", 3, 1, "bar"): {self},
			},

			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "foo"): {f: []string{"bar", "foo"}},

				fln("b.cue", 3, 1, "bar"): {f: []string{"bar", "foo"}},
				fln("b.cue", 3, 1, "foo"): {e: []string{"bar", "foo"}},
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
			expectDefinitions: map[*position][]*position{
				fln("c.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},

				fln("a.cue", 3, 1, "foo"): {self, fln("b.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},
				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},

				fln("c.cue", 3, 1, "bar"): {self},
				fln("c.cue", 4, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "foo"): {f: []string{"bar", "foo"}},

				fln("b.cue", 3, 1, "foo"): {f: []string{"bar", "foo"}},

				fln("c.cue", 3, 1, "bar"): {f: []string{"bar", "foo"}},
				fln("c.cue", 3, 1, "foo"): {e: []string{"bar", "foo"}},
				fln("c.cue", 4, 1, "foo"): {f: []string{"bar", "foo"}},
				fln("c.cue", 4, 1, "_"):   {e: []string{"bar", "foo"}},
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
			expectDefinitions: map[*position][]*position{
				fln("a.cue", 3, 2, "bar"): {fln("a.cue", 3, 1, "bar")},
				fln("b.cue", 3, 1, "bar"): {},

				fln("a.cue", 3, 1, "foo"): {self, fln("b.cue", 3, 1, "foo")},
				fln("a.cue", 3, 1, "bar"): {self},
				fln("a.cue", 3, 1, "baz"): {self},

				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo")},
				fln("b.cue", 3, 1, "qux"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "foo"): {f: []string{"foo"}},
				fln("a.cue", 3, 1, "bar"): {f: []string{"bar", "baz", "qux"}},
				fln("a.cue", 3, 1, "baz"): {f: []string{"bar", "baz", "qux"}},
				fln("a.cue", 3, 2, "bar"): {e: []string{"bar", "baz", "foo"}},

				fln("b.cue", 3, 1, "foo"): {f: []string{"foo"}},
				fln("b.cue", 3, 1, "qux"): {f: []string{"bar", "baz", "qux"}},
				fln("b.cue", 3, 1, "bar"): {e: []string{"foo", "qux"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "foo"): {f: []string{"foo"}},
				fln("a.cue", 3, 1, "bar"): {f: []string{"bar", "baz", "qux"}},
				fln("a.cue", 4, 1, "foo"): {f: []string{"foo"}},
				fln("a.cue", 4, 1, "baz"): {f: []string{"bar", "baz", "qux"}},
				fln("a.cue", 4, 1, "bar"): {e: []string{"baz", "foo"}},

				fln("b.cue", 3, 1, "foo"): {f: []string{"foo"}},
				fln("b.cue", 3, 1, "qux"): {f: []string{"bar", "baz", "qux"}},
				fln("b.cue", 3, 1, "bar"): {e: []string{"foo", "qux"}},

				fln("c.cue", 3, 1, "foo"):  {f: []string{"foo"}},
				fln("c.cue", 3, 1, "qux"):  {f: []string{"bar", "baz", "qux"}},
				fln("c.cue", 3, 2, "foo"):  {e: []string{"foo", "qux"}},
				fln("c.cue", 3, 1, ".bar"): {e: []string{"bar", "baz", "qux"}},
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
			expectDefinitions: map[*position][]*position{
				fln("a.cue", 4, 1, "a"): {fln("a.cue", 3, 1, "a")},
				fln("b.cue", 3, 1, "a"): {},
				fln("c.cue", 4, 1, "a"): {fln("c.cue", 3, 1, "a")},

				fln("a.cue", 4, 1, "q"): {self},
				fln("b.cue", 3, 1, "r"): {self},
				fln("c.cue", 4, 1, "s"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 4, 1, "q"): {f: []string{"q", "r", "s"}},
				fln("a.cue", 4, 1, "a"): {e: []string{"a", "q", "r", "s"}},

				fln("b.cue", 3, 1, "r"): {f: []string{"q", "r", "s"}},
				fln("b.cue", 3, 1, "a"): {e: []string{"q", "r", "s"}},

				fln("c.cue", 4, 1, "s"): {f: []string{"q", "r", "s"}},
				fln("c.cue", 4, 1, "a"): {e: []string{"a", "q", "r", "s"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "w"):  {f: []string{"w"}},
				fln("a.cue", 4, 1, "x"):  {f: []string{"a", "x"}},
				fln("a.cue", 4, 1, "y"):  {f: []string{"y"}},
				fln("a.cue", 4, 1, "a"):  {e: []string{"a", "w", "x", "y"}},
				fln("a.cue", 4, 1, ".b"): {e: []string{"b"}},
				fln("a.cue", 5, 1, "a"):  {f: []string{"a", "x"}},
				fln("a.cue", 5, 1, "b"):  {f: []string{"b"}},

				fln("b.cue", 3, 1, "w"): {f: []string{"w"}},
				fln("b.cue", 3, 1, "a"): {f: []string{"a", "x"}},
				fln("b.cue", 3, 1, "b"): {f: []string{"b"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "a"):  {f: []string{"a"}},
				fln("a.cue", 4, 1, "b"):  {f: []string{"b", "d", "e", "f", "g"}},
				fln("a.cue", 4, 1, "c"):  {f: []string{"c"}},
				fln("a.cue", 5, 1, "d"):  {f: []string{"b", "d", "e", "f", "g"}},
				fln("a.cue", 5, 1, "X"):  {e: []string{"X", "a", "b", "d"}},
				fln("a.cue", 5, 1, ".c"): {e: []string{"c"}},

				fln("b.cue", 3, 1, "a"): {f: []string{"a"}},
				fln("b.cue", 4, 1, "b"): {f: []string{"b", "d", "e", "f", "g"}},
				fln("b.cue", 4, 1, "c"): {f: []string{"c"}},

				fln("c.cue", 3, 1, "a"): {f: []string{"a"}},
				fln("c.cue", 4, 1, "e"): {f: []string{"b", "d", "e", "f", "g"}},
				fln("c.cue", 4, 1, "X"): {e: []string{"a", "e"}},

				fln("d.cue", 3, 1, "a"): {f: []string{"a"}},
				fln("d.cue", 4, 1, "f"): {f: []string{"b", "d", "e", "f", "g"}},
				fln("d.cue", 4, 1, "c"): {f: []string{"c"}},
				fln("d.cue", 5, 1, "g"): {f: []string{"b", "d", "e", "f", "g"}},
				fln("d.cue", 5, 1, "X"): {f: []string{"c"}, e: []string{"X", "a", "f", "g"}},
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
			expectDefinitions: map[*position][]*position{
				fln("b.cue", 3, 1, `"a"`): {fln("a.cue", 1, 1, "package a"), fln("c.cue", 1, 1, "package a")},
				fln("b.cue", 5, 1, "a"):   {fln("b.cue", 3, 1, `"a"`)},
				fln("b.cue", 6, 1, "y"):   {fln("b.cue", 5, 1, "y")},
				fln("b.cue", 6, 1, "x"):   {fln("a.cue", 3, 1, "x")},

				fln("a.cue", 3, 1, "x"): {self},

				fln("b.cue", 5, 1, "y"): {self},
				fln("b.cue", 6, 1, "z"): {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				fln("a.cue", 3, 1, "x"): {f: []string{"x"}},

				fln("b.cue", 5, 1, "y"):  {f: []string{"y", "z"}},
				fln("b.cue", 5, 1, "a"):  {f: []string{"x"}, e: []string{"a", "y", "z"}},
				fln("b.cue", 6, 1, "z"):  {f: []string{"y", "z"}},
				fln("b.cue", 6, 1, "y"):  {e: []string{"a", "y", "z"}},
				fln("b.cue", 6, 1, ".x"): {e: []string{"x"}},
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
			expectDefinitions: map[*position][]*position{
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
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "x"): {f: []string{"x", "y", "z"}},
				ln(1, 2, "x"): {f: []string{"x"}},
				ln(1, 3, "x"): {f: []string{"x"}},
				ln(2, 1, "x"): {f: []string{"x", "y", "z"}},
				ln(2, 2, "x"): {f: []string{"x"}},
				ln(2, 3, "x"): {f: []string{"x"}},
				ln(3, 1, "x"): {f: []string{"x", "y", "z"}},
				ln(3, 1, "y"): {f: []string{"x"}, e: []string{"x", "y", "z"}},
				ln(4, 1, "y"): {f: []string{"x", "y", "z"}},
				ln(4, 1, "x"): {f: []string{"x"}},
				ln(4, 1, "z"): {f: []string{"x"}, e: []string{"x", "y", "z"}},
				ln(5, 1, "z"): {f: []string{"x", "y", "z"}},
				ln(5, 1, "x"): {f: []string{"x"}},
			},
		},

		{
			name: "Completions_Schema",
			archive: `-- a.cue --
#Schema: {
	foo!: int
}
x: #Schema & {
	f
}
furble: 4
`,
			expectDefinitions: map[*position][]*position{
				ln(4, 1, "#Schema"): {ln(1, 1, "#Schema")},

				ln(1, 1, "#Schema"): {self},
				ln(2, 1, "foo"):     {self},
				ln(4, 1, "x"):       {self},
				ln(7, 1, "furble"):  {self},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "#Schema"): {f: []string{"#Schema", "furble", "x"}},
				ln(2, 1, "foo"):     {f: []string{"foo"}},
				ln(2, 1, "int"):     {e: []string{"#Schema", "foo", "furble", "x"}},
				ln(4, 1, "x"):       {f: []string{"#Schema", "furble", "x"}},
				ln(4, 1, "#Schema"): {e: []string{"#Schema", "furble", "x"}},
				ln(5, 1, "f"):       {f: []string{"foo"}, e: []string{"#Schema", "furble", "x"}},
				ln(7, 1, "furble"):  {f: []string{"#Schema", "furble", "x"}},
			},
		},

		{
			name: "Completions_SchemaInner",
			archive: `-- a.cue --
#Schema: {
	foo?: #Foo
}

#Foo: {
	bar?: int
}

something: #Schema
something: {
	foo: {
		b
	}
}`,
			expectDefinitions: map[*position][]*position{
				ln(2, 1, "#Foo"):    {ln(5, 1, "#Foo")},
				ln(9, 1, "#Schema"): {ln(1, 1, "#Schema")},

				ln(1, 1, "#Schema"):    {self},
				ln(2, 1, "foo"):        {self},
				ln(5, 1, "#Foo"):       {self},
				ln(6, 1, "bar"):        {self},
				ln(9, 1, "something"):  {self, ln(10, 1, "something")},
				ln(10, 1, "something"): {self, ln(9, 1, "something")},
				ln(11, 1, "foo"):       {self, ln(2, 1, "foo")},
			},
			expectCompletions: map[*position]fieldEmbedCompletions{
				ln(1, 1, "#Schema"):    {f: []string{"#Foo", "#Schema", "something"}},
				ln(2, 1, "foo"):        {f: []string{"foo"}},
				ln(2, 1, "#Foo"):       {f: []string{"bar"}, e: []string{"#Foo", "#Schema", "foo", "something"}},
				ln(5, 1, "#Foo"):       {f: []string{"#Foo", "#Schema", "something"}},
				ln(6, 1, "bar"):        {f: []string{"bar"}},
				ln(6, 1, "int"):        {e: []string{"#Foo", "#Schema", "bar", "something"}},
				ln(9, 1, "something"):  {f: []string{"#Foo", "#Schema", "something"}},
				ln(9, 1, "#Schema"):    {f: []string{"foo"}, e: []string{"#Foo", "#Schema", "something"}},
				ln(10, 1, "something"): {f: []string{"#Foo", "#Schema", "something"}},
				ln(11, 1, "foo"):       {f: []string{"foo"}},
				ln(12, 1, "b"):         {f: []string{"bar"}, e: []string{"#Foo", "#Schema", "foo", "something"}},
			},
		},
	}.run(t)
}

type testCase struct {
	name              string
	archive           string
	expectDefinitions map[*position][]*position
	expectCompletions map[*position]fieldEmbedCompletions
}

type fieldEmbedCompletions struct {
	// field completions
	f []string
	// embed completions
	e []string
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
				fileAst, _ := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
				fileAst.Pos().File().SetContent(fh.Data)
				qt.Assert(t, qt.IsNotNil(fileAst))
				files = append(files, fileAst)
				filesByName[fh.Name] = fileAst
				pkgName := fileAst.PackageName()
				filesByPkg[pkgName] = append(filesByPkg[pkgName], fileAst)
			}

			var allPositions []*position
			for from, tos := range tc.expectDefinitions {
				allPositions = append(allPositions, from)
				allPositions = append(allPositions, tos...)
			}
			for from := range tc.expectCompletions {
				allPositions = append(allPositions, from)
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

			tc.testDefinitions(t, files, dfnsByFilename)
			tc.testCompletions(t, files, dfnsByFilename)
		})
	}
}

func (tc *testCase) testDefinitions(t *testing.T, files []*ast.File, dfnsByFilename map[string]*definitions.FileDefinitions) {
	t.Run("definitions", func(t *testing.T) {
		ranges := rangeset.NewFilenameRangeSet()

		for posFrom, positionsWant := range tc.expectDefinitions {
			filename := posFrom.filename
			fdfns := dfnsByFilename[filename]
			qt.Check(t, qt.IsNotNil(fdfns))

			offset := posFrom.offset
			ranges.Add(filename, offset, offset+len(posFrom.str))

			for i := range len(posFrom.str) {
				// Test every offset within the "from" token
				offset := offset + i
				nodesGot := fdfns.DefinitionsForOffset(offset)
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
				qt.Check(t, qt.DeepEquals(fileOffsetsGot, fileOffsetsWant), qt.Commentf("from %#v(+%d)", posFrom, i))
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
				nodesGot := fdfns.DefinitionsForOffset(i)
				fileOffsetsGot := make([]fileOffset, len(nodesGot))
				for j, node := range nodesGot {
					fileOffsetsGot[j] = fileOffsetForTokenPos(node.Pos().Position())
				}
				qt.Check(t, qt.DeepEquals(fileOffsetsGot, []fileOffset{}), qt.Commentf("file: %q, offset: %d", filename, i))
			}
		}
	})
}

func (tc *testCase) testCompletions(t *testing.T, files []*ast.File, dfnsByFilename map[string]*definitions.FileDefinitions) {
	t.Run("completions", func(t *testing.T) {
		defer func() {
			if t.Failed() {
				tc.dumpCompletions(t, files, dfnsByFilename)
			}
		}()

		ranges := rangeset.NewFilenameRangeSet()

		for posFrom, completionWant := range tc.expectCompletions {
			fieldCompletionWant := completionWant.f
			embedCompletionWant := completionWant.e
			slices.Sort(fieldCompletionWant)
			slices.Sort(embedCompletionWant)
			filename := posFrom.filename
			fdfns := dfnsByFilename[filename]
			qt.Check(t, qt.IsNotNil(fdfns))

			offset := posFrom.offset
			ranges.Add(filename, offset, offset+len(posFrom.str))

			for i := range len(posFrom.str) {
				// Test every offset within the "from" token
				offset := offset + i
				fieldCompletionGot, embedCompletionGot, _, _, _ := fdfns.CompletionsForOffset(offset)
				qt.Check(t, qt.DeepEquals(fieldCompletionGot, fieldCompletionWant), qt.Commentf("from %#v(+%d)", posFrom, i))
				qt.Check(t, qt.DeepEquals(embedCompletionGot, embedCompletionWant), qt.Commentf("from %#v(+%d)", posFrom, i))
			}
		}

		// Test that all offsets not explicitly mentioned in
		// expectations, complete to nothing.
		for _, fileAst := range files {
			filename := fileAst.Filename
			fdfns := dfnsByFilename[filename]

			for i := range fileAst.Pos().File().Content() {
				if ranges.Contains(filename, i) {
					continue
				}
				fieldCompletionGot, embedCompletionGot, _, _, _ := fdfns.CompletionsForOffset(i)
				qt.Check(t, qt.DeepEquals(fieldCompletionGot, nil), qt.Commentf("file: %q, offset: %d, got %d field completions", filename, i, len(fieldCompletionGot)))
				qt.Check(t, qt.DeepEquals(embedCompletionGot, nil), qt.Commentf("file: %q, offset: %d, got %d embed completions", filename, i, len(embedCompletionGot)))
			}
		}
	})
}

func (tc *testCase) dumpCompletions(t *testing.T, files []*ast.File, dfnsByFilename map[string]*definitions.FileDefinitions) {
	for _, fileAst := range files {
		filename := fileAst.Filename
		fdfns := dfnsByFilename[filename]
		content := fileAst.Pos().File().Content()

		fields := strings.FieldsFunc(string(content), func(r rune) bool {
			switch r {
			case ' ', '\t', '\n', ':', '.', '{', '}', '[', ']', '(', ')', ',', '=', '+', '-', '!', '?':
				return true
			default:
				return false
			}
		})

		offsetMap := make(map[int]*position, len(fields))
		fieldPerLine := make(map[string]int)
		lineNum := 1
		lineStartOffset := 0

		for line := range strings.Lines(string(content)) {
			lineLen := len(line)
			clear(fieldPerLine)
			column := 0
			for len(fields) > 0 {
				field := fields[0]
				columnRel := strings.Index(line, field)
				if columnRel == -1 {
					break
				}

				fields = fields[1:]
				line = line[columnRel+len(field):]

				column += columnRel
				offset := lineStartOffset + column
				column += len(field)
				// If it's a path element (not the root) then include
				// the dot. E.g. .y in x.y. But if it's an ellipsis
				// (e.g. ...z) then leave the z alone.
				if offset > 0 && content[offset-1] == '.' && (offset == 1 || content[offset-2] != '.') {
					field = "." + field
					offset--
				}

				// Increase the counts for field and every substring of
				// field for this line.
				for start := range field {
					for end := start + 1; end <= len(field); end++ {
						field := field[start:end]
						fieldPerLine[field]++
					}
				}
				n := fieldPerLine[field]

				pos := fln(filename, lineNum, n, field)
				pos.offset = offset
				offsetMap[offset] = pos
			}

			lineNum++
			lineStartOffset += lineLen
		}

		var strs []string
		for i := range content {
			fieldCompletionGot, embedCompletionGot, _, _, _ := fdfns.CompletionsForOffset(i)
			if len(fieldCompletionGot) > 0 || len(embedCompletionGot) > 0 {
				if pos, found := offsetMap[i]; found {
					strs = append(strs, completionString(files, pos, fieldCompletionGot, embedCompletionGot))
				}
			}
		}

		if len(strs) > 0 {
			t.Log("Suggested expectCompletions: map[*position]fieldEmbedCompletions{\n" + strings.Join(strs, "\n") + "\n},\n")
		}
	}
}

func completionString(files []*ast.File, posFrom *position, fieldCompletionsGot, embedCompletionsGot []string) string {
	if len(fieldCompletionsGot) == 0 && len(embedCompletionsGot) == 0 {
		return ""
	}
	msg := ""
	if len(files) == 1 {
		msg = fmt.Sprintf("\tln(%d, %d, %q): ", posFrom.line, posFrom.n, posFrom.str)
	} else {
		msg = fmt.Sprintf("\tfln(%q, %d, %d, %q): ", posFrom.filename, posFrom.line, posFrom.n, posFrom.str)
	}

	var strs []string
	fields := strings.Join(fieldCompletionsGot, `", "`)
	if fields != "" {
		fields = `f: []string{"` + fields + `"}`
		strs = append(strs, fields)
	}
	embeds := strings.Join(embedCompletionsGot, `", "`)
	if embeds != "" {
		embeds = `e: []string{"` + embeds + `"}`
		strs = append(strs, embeds)
	}

	msg += "{" + strings.Join(strs, `, `) + "},"
	return msg
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

func (p *position) String() string {
	return fmt.Sprintf(`fln(%q, %d, %d, %q)`, p.filename, p.line, p.n, p.str)
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
