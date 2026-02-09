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

package eval_test

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/eval"
	"cuelang.org/go/internal/lsp/rangeset"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestEval(t *testing.T) {
	testCases{
		{
			name: "Selector_Implicit_ViaRoot",
			archive: `-- a.cue --
x: y: a.b
a: b: 5
a: b: 6
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "a"): {ln(2, 1, "a"), ln(3, 1, "a")},
				ln(1, 1, "b"): {ln(2, 1, "b"), ln(3, 1, "b")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},
				ln(2, 1, "a"): {self, ln(3, 1, "a")},
				ln(2, 1, "b"): {self, ln(3, 1, "b")},
				ln(3, 1, "a"): {self, ln(2, 1, "a")},
				ln(3, 1, "b"): {self, ln(2, 1, "b")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "x"}},
				or1(2):     {f: []string{"y"}, e: []string{"a", "x"}},
				or(3, 5):   {f: []string{"y"}},
				or(5, 8):   {e: []string{"a", "x", "y"}},
				or(8, 10):  {e: []string{"b"}},
				or(10, 12): {f: []string{"a", "x"}},
				or1(12):    {f: []string{"b"}, e: []string{"a", "x"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"a", "b", "x"}},
				or1(17):    {e: []string{"a", "b", "x"}},
				or(18, 20): {f: []string{"a", "x"}},
				or1(20):    {f: []string{"b"}, e: []string{"a", "x"}},
				or(21, 23): {f: []string{"b"}},
				or1(23):    {e: []string{"a", "b", "x"}},
				or1(25):    {e: []string{"a", "b", "x"}},
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

			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"w"}},
				or(2, 6):   {f: []string{"a", "x"}, e: []string{"w"}},
				or(6, 8):   {f: []string{"a", "x"}},
				or1(8):     {f: []string{"y"}, e: []string{"a", "w", "x"}},
				or(9, 11):  {f: []string{"y"}},
				or(11, 14): {e: []string{"a", "w", "x", "y"}},
				or(14, 16): {e: []string{"b"}},
				or1(16):    {f: []string{"a", "x"}, e: []string{"w"}},
				or(17, 19): {f: []string{"a", "x"}},
				or1(19):    {f: []string{"b"}, e: []string{"a", "w", "x"}},
				or(20, 22): {f: []string{"b"}},
				or1(22):    {e: []string{"a", "b", "w", "x"}},
				or1(24):    {e: []string{"a", "b", "w", "x"}},
				or1(25):    {f: []string{"a", "x"}, e: []string{"w"}},
				or(27, 29): {f: []string{"w"}},
				or1(29):    {f: []string{"a", "x"}, e: []string{"w"}},
				or(30, 32): {f: []string{"a", "x"}},
				or1(32):    {f: []string{"b"}, e: []string{"a", "w"}},
				or(33, 35): {f: []string{"b"}},
				or1(35):    {e: []string{"a", "b", "w"}},
				or1(37):    {e: []string{"a", "b", "w"}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 3):   {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or1(3):     {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(4, 6):   {f: []string{"f"}},
				or1(6):     {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or1(8):     {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or(9, 12):  {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or1(12):    {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(13, 15): {f: []string{"f"}},
				or1(15):    {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or1(17):    {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or(18, 20): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(20, 24): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(24, 26): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(26, 30): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(30, 32): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(32, 35): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(35, 40): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(40, 43): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(43, 48): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(48, 51): {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(51, 53): {e: []string{"f"}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 3):   {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or1(3):     {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(4, 6):   {f: []string{"f"}},
				or1(6):     {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or1(8):     {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or(9, 12):  {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or1(12):    {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(13, 15): {f: []string{"f"}},
				or1(15):    {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or1(17):    {e: []string{"f", "out1", "out2", "x1", "x2", "y", "z"}},
				or(18, 20): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(20, 24): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(24, 26): {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(26, 29): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(29, 31): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(31, 34): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(34, 39): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(39, 42): {f: []string{"f"}, e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(42, 47): {f: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(47, 50): {e: []string{"out1", "out2", "x1", "x2", "y", "z"}},
				or(50, 52): {e: []string{"f"}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "c", "d", "x", "y"}},
				or(2, 5):   {e: []string{"a", "c", "d", "x", "y"}},
				or(5, 7):   {e: []string{"b"}},
				or(7, 10):  {f: []string{"a", "c", "d", "x", "y"}},
				or1(10):    {f: []string{"b"}, e: []string{"a", "c", "d", "x", "y"}},
				or(11, 13): {f: []string{"b"}},
				or1(13):    {e: []string{"a", "b", "c", "d", "x", "y"}},
				or1(15):    {e: []string{"a", "b", "c", "d", "x", "y"}},
				or(16, 18): {f: []string{"a", "c", "d", "x", "y"}},
				or1(18):    {f: []string{"b"}, e: []string{"a", "c", "d", "x", "y"}},
				or(19, 21): {f: []string{"b"}},
				or(21, 26): {e: []string{"a", "b", "c", "d", "x", "y"}},
				or(26, 28): {f: []string{"a", "c", "d", "x", "y"}},
				or(28, 31): {f: []string{"b"}, e: []string{"a", "c", "d", "x", "y"}},
				or(31, 33): {e: []string{"a", "c", "d", "x", "y"}},
				or(33, 35): {f: []string{"b"}, e: []string{"a", "c", "d", "x", "y"}},
				or(35, 38): {f: []string{"a", "c", "d", "x", "y"}},
				or(38, 41): {e: []string{"a", "c", "d", "x", "y"}},
				or(41, 43): {e: []string{"b"}},
			},
		},

		{
			name: "Inner_Reference_Explicit",
			archive: `-- a.cue --
x: y: int
z: x & {
  y: 3
  w: y
}
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"): {ln(1, 1, "x")},
				ln(4, 1, "y"): {ln(3, 1, "y"), ln(1, 1, "y")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},

				ln(2, 1, "z"): {self},
				ln(3, 1, "y"): {self, ln(1, 1, "y")},
				ln(4, 1, "w"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "z"}},
				or1(2):     {f: []string{"y"}, e: []string{"x", "z"}},
				or(3, 5):   {f: []string{"y"}},
				or(5, 10):  {e: []string{"x", "y", "z"}},
				or(10, 12): {f: []string{"x", "z"}},
				or(12, 15): {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or(15, 17): {e: []string{"x", "z"}},
				or(17, 23): {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or1(23):    {e: []string{"w", "x", "y", "z"}},
				or1(25):    {e: []string{"w", "x", "y", "z"}},
				or(26, 30): {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or(30, 33): {e: []string{"w", "x", "y", "z"}},
				or1(33):    {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or1(34):    {e: []string{"x", "z"}},
			},
		},

		{
			name: "Inner_Reference_Implicit",
			archive: `-- a.cue --
x: y: int
z: x
z: {
  y: 3
  w: y
}
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"): {ln(1, 1, "x")},
				ln(5, 1, "y"): {ln(4, 1, "y"), ln(1, 1, "y")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},

				ln(2, 1, "z"): {self, ln(3, 1, "z")},
				ln(3, 1, "z"): {self, ln(2, 1, "z")},
				ln(4, 1, "y"): {self, ln(1, 1, "y")},
				ln(5, 1, "w"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "z"}},
				or1(2):     {f: []string{"y"}, e: []string{"x", "z"}},
				or(3, 5):   {f: []string{"y"}},
				or(5, 10):  {e: []string{"x", "y", "z"}},
				or(10, 12): {f: []string{"x", "z"}},
				or(12, 15): {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or(15, 17): {f: []string{"x", "z"}},
				or(17, 22): {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or(22, 24): {f: []string{"w", "y"}},
				or1(24):    {e: []string{"w", "x", "y", "z"}},
				or1(26):    {e: []string{"w", "x", "y", "z"}},
				or(27, 29): {f: []string{"w", "y"}, e: []string{"x", "z"}},
				or(29, 31): {f: []string{"w", "y"}},
				or(31, 34): {e: []string{"w", "x", "y", "z"}},
				or1(34):    {f: []string{"w", "y"}, e: []string{"x", "z"}},
			},
		},

		{
			name: "Inner_Reference_Implicit_Parent",
			archive: `-- a.cue --
a: b: c: int
x: a
x: b: {c: 3, d: c}
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(3, 2, "c"): {ln(3, 1, "c"), ln(1, 1, "c")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(2, 1, "x"): {self, ln(3, 1, "x")},
				ln(3, 1, "x"): {self, ln(2, 1, "x")},

				ln(3, 1, "b"): {self, ln(1, 1, "b")},
				ln(3, 1, "c"): {self, ln(1, 1, "c")},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "x"}},
				or1(2):     {f: []string{"b"}, e: []string{"a", "x"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {f: []string{"c"}, e: []string{"a", "b", "x"}},
				or(6, 8):   {f: []string{"c"}},
				or(8, 13):  {e: []string{"a", "b", "c", "x"}},
				or(13, 15): {f: []string{"a", "x"}},
				or(15, 18): {f: []string{"b"}, e: []string{"a", "x"}},
				or(18, 20): {f: []string{"a", "x"}},
				or1(20):    {f: []string{"b"}, e: []string{"a", "x"}},
				or(21, 23): {f: []string{"b"}},
				or(23, 25): {f: []string{"c", "d"}, e: []string{"a", "b", "x"}},
				or(25, 27): {f: []string{"c", "d"}},
				or1(27):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(29):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(30):    {f: []string{"c", "d"}, e: []string{"a", "b", "x"}},
				or(31, 33): {f: []string{"c", "d"}},
				or(33, 36): {e: []string{"a", "b", "c", "d", "x"}},
			},
		},

		{
			name: "Embedding",
			archive: `-- a.cue --
x: y: z: 3
o: { p: 4, x.y }
q: o.z
`,
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"o", "q", "x"}},
				or1(2):     {f: []string{"y"}, e: []string{"o", "q", "x"}},
				or(3, 5):   {f: []string{"y"}},
				or1(5):     {f: []string{"z"}, e: []string{"o", "q", "x", "y"}},
				or(6, 8):   {f: []string{"z"}},
				or1(8):     {e: []string{"o", "q", "x", "y", "z"}},
				or1(10):    {e: []string{"o", "q", "x", "y", "z"}},
				or(11, 13): {f: []string{"o", "q", "x"}},
				or(13, 16): {f: []string{"p", "z"}, e: []string{"o", "q", "x"}},
				or(16, 18): {f: []string{"p", "z"}},
				or1(18):    {e: []string{"o", "p", "q", "x"}},
				or1(20):    {e: []string{"o", "p", "q", "x"}},
				or1(21):    {f: []string{"p", "z"}, e: []string{"o", "q", "x"}},
				or(22, 24): {e: []string{"o", "p", "q", "x"}},
				or(24, 26): {e: []string{"y"}},
				or1(26):    {f: []string{"p", "z"}, e: []string{"o", "q", "x"}},
				or(28, 30): {f: []string{"o", "q", "x"}},
				or(30, 33): {e: []string{"o", "q", "x"}},
				or(33, 35): {e: []string{"p", "z"}},
			},
		},

		{
			name: "Embedding...",
			archive: `-- a.cue --
@experiment(explicitopen)
x: y: z: 3
o: { p: 4, x.y... }
q: o.z
`,
			expectDefinitions: map[position][]position{
				ln(3, 1, "x"): {ln(2, 1, "x")},
				ln(3, 1, "y"): {ln(2, 1, "y")},
				ln(4, 1, "o"): {ln(3, 1, "o")},
				ln(4, 1, "z"): {ln(2, 1, "z")},

				ln(2, 1, "x"): {self},
				ln(2, 1, "y"): {self},
				ln(2, 1, "z"): {self},
				ln(3, 1, "o"): {self},
				ln(3, 1, "p"): {self},
				ln(4, 1, "q"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(25, 28): {f: []string{"o", "q", "x"}},
				or1(28):    {f: []string{"y"}, e: []string{"o", "q", "x"}},
				or(29, 31): {f: []string{"y"}},
				or1(31):    {f: []string{"z"}, e: []string{"o", "q", "x", "y"}},
				or(32, 34): {f: []string{"z"}},
				or1(34):    {e: []string{"o", "q", "x", "y", "z"}},
				or1(36):    {e: []string{"o", "q", "x", "y", "z"}},
				or(37, 39): {f: []string{"o", "q", "x"}},
				or(39, 42): {f: []string{"p", "z"}, e: []string{"o", "q", "x"}},
				or(42, 44): {f: []string{"p", "z"}},
				or1(44):    {e: []string{"o", "p", "q", "x"}},
				or1(46):    {e: []string{"o", "p", "q", "x"}},
				or1(47):    {f: []string{"p", "z"}, e: []string{"o", "q", "x"}},
				or(48, 50): {e: []string{"o", "p", "q", "x"}},
				or(50, 52): {e: []string{"y"}},
				or(52, 55): {e: []string{"o", "p", "q", "x"}},
				or1(55):    {f: []string{"p", "z"}, e: []string{"o", "q", "x"}},
				or(57, 59): {f: []string{"o", "q", "x"}},
				or(59, 62): {e: []string{"o", "q", "x"}},
				or(62, 64): {e: []string{"p", "z"}},
			},
		},

		{
			name: "Embedded_Scopes",
			archive: `-- a.cue --
x: {
	{a: b, y: z}
	{b: _, a: b, z: _}
	z: 3
}`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "z"): {ln(3, 1, "z"), ln(4, 1, "z")},

				ln(3, 2, "b"): {ln(3, 1, "b")},

				ln(1, 1, "x"): {self},
				ln(2, 1, "a"): {self, ln(3, 1, "a")},
				ln(2, 1, "y"): {self},
				ln(3, 1, "b"): {self},
				ln(3, 1, "a"): {self, ln(2, 1, "a")},
				ln(3, 1, "z"): {self, ln(4, 1, "z")},
				ln(4, 1, "z"): {self, ln(3, 1, "z")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x"}},
				or(2, 6):   {f: []string{"a", "b", "y", "z"}, e: []string{"x"}},
				or1(6):     {f: []string{"a", "b", "y", "z"}, e: []string{"x", "z"}},
				or(7, 9):   {f: []string{"a", "b", "y", "z"}},
				or(9, 12):  {e: []string{"a", "x", "y", "z"}},
				or1(12):    {f: []string{"a", "b", "y", "z"}, e: []string{"x", "z"}},
				or(13, 15): {f: []string{"a", "b", "y", "z"}},
				or(15, 18): {e: []string{"a", "x", "y", "z"}},
				or1(19):    {f: []string{"a", "b", "y", "z"}, e: []string{"x"}},
				or1(20):    {f: []string{"a", "b", "y", "z"}, e: []string{"x", "z"}},
				or(21, 23): {f: []string{"a", "b", "y", "z"}},
				or(23, 26): {e: []string{"a", "b", "x", "z"}},
				or1(26):    {f: []string{"a", "b", "y", "z"}, e: []string{"x", "z"}},
				or(27, 29): {f: []string{"a", "b", "y", "z"}},
				or(29, 32): {e: []string{"a", "b", "x", "z"}},
				or1(32):    {f: []string{"a", "b", "y", "z"}, e: []string{"x", "z"}},
				or(33, 35): {f: []string{"a", "b", "y", "z"}},
				or(35, 38): {e: []string{"a", "b", "x", "z"}},
				or1(39):    {f: []string{"a", "b", "y", "z"}, e: []string{"x"}},
				or(40, 42): {f: []string{"a", "b", "y", "z"}},
				or1(42):    {e: []string{"x", "z"}},
				or1(44):    {e: []string{"x", "z"}},
				or1(45):    {f: []string{"a", "b", "y", "z"}, e: []string{"x"}},
			},
		},

		{
			name: "Embedded_Dependency_Simple",
			archive: `-- a.cue --
a: b: c: {
	c.d
	e
}

e: d: {x: 1}
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "c"): {ln(1, 1, "c")},
				ln(2, 1, "d"): {ln(6, 1, "d")},
				ln(3, 1, "e"): {ln(6, 1, "e")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(6, 1, "e"): {self},
				ln(6, 1, "d"): {self},
				ln(6, 1, "x"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "e"}},
				or1(2):     {f: []string{"b"}, e: []string{"a", "e"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {f: []string{"c"}, e: []string{"a", "b", "e"}},
				or(6, 8):   {f: []string{"c"}},
				or(8, 12):  {f: []string{"d", "x"}, e: []string{"a", "b", "c", "e"}},
				or(12, 14): {e: []string{"a", "b", "c", "e"}},
				or(14, 16): {e: []string{"d", "x"}},
				or(16, 20): {f: []string{"d", "x"}, e: []string{"a", "b", "c", "e"}},
				or(21, 24): {f: []string{"a", "e"}},
				or1(24):    {f: []string{"d"}, e: []string{"a", "e"}},
				or(25, 27): {f: []string{"d"}},
				or(27, 29): {f: []string{"x"}, e: []string{"a", "d", "e"}},
				or(29, 31): {f: []string{"x"}},
				or1(31):    {e: []string{"a", "d", "e", "x"}},
				or1(33):    {e: []string{"a", "d", "e", "x"}},
			},
		},

		{
			name: "Embedded_Dependency_Complex",
			archive: `-- a.cue --
a: b: c: {
	e
	c.d
	f
}

e: d: {x: 1}
f: d: {y: 1}
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "e"): {ln(7, 1, "e")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "d"): {ln(7, 1, "d"), ln(8, 1, "d")},
				ln(4, 1, "f"): {ln(8, 1, "f")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(7, 1, "e"): {self},
				ln(7, 1, "d"): {self},
				ln(7, 1, "x"): {self},

				ln(8, 1, "f"): {self},
				ln(8, 1, "d"): {self},
				ln(8, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "e", "f"}},
				or1(2):     {f: []string{"b"}, e: []string{"a", "e", "f"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {f: []string{"c"}, e: []string{"a", "b", "e", "f"}},
				or(6, 8):   {f: []string{"c"}},
				or(8, 15):  {f: []string{"d", "x", "y"}, e: []string{"a", "b", "c", "e", "f"}},
				or(15, 17): {e: []string{"a", "b", "c", "e", "f"}},
				or(17, 19): {e: []string{"d", "x", "y"}},
				or(19, 23): {f: []string{"d", "x", "y"}, e: []string{"a", "b", "c", "e", "f"}},
				or(24, 27): {f: []string{"a", "e", "f"}},
				or1(27):    {f: []string{"d"}, e: []string{"a", "e", "f"}},
				or(28, 30): {f: []string{"d"}},
				or(30, 32): {f: []string{"x"}, e: []string{"a", "d", "e", "f"}},
				or(32, 34): {f: []string{"x"}},
				or1(34):    {e: []string{"a", "d", "e", "f", "x"}},
				or1(36):    {e: []string{"a", "d", "e", "f", "x"}},
				or(38, 40): {f: []string{"a", "e", "f"}},
				or1(40):    {f: []string{"d"}, e: []string{"a", "e", "f"}},
				or(41, 43): {f: []string{"d"}},
				or(43, 45): {f: []string{"y"}, e: []string{"a", "d", "e", "f"}},
				or(45, 47): {f: []string{"y"}},
				or1(47):    {e: []string{"a", "d", "e", "f", "y"}},
				or1(49):    {e: []string{"a", "d", "e", "f", "y"}},
			},
		},

		{
			name: "Embedded_Dependency_Mutual",
			archive: `-- a.cue --
c: {x.y, e}
x: {c.d, f}
e: d: a: 1
f: y: b: 2
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "x"): {ln(2, 1, "x")},
				ln(1, 1, "y"): {ln(4, 1, "y")},
				ln(1, 1, "e"): {ln(3, 1, "e")},

				ln(2, 1, "c"): {ln(1, 1, "c")},
				ln(2, 1, "d"): {ln(3, 1, "d")},
				ln(2, 1, "f"): {ln(4, 1, "f")},

				ln(1, 1, "c"): {self},
				ln(2, 1, "x"): {self},

				ln(3, 1, "e"): {self},
				ln(3, 1, "d"): {self},
				ln(3, 1, "a"): {self},

				ln(4, 1, "f"): {self},
				ln(4, 1, "y"): {self},
				ln(4, 1, "b"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"c", "e", "f", "x"}},
				or(2, 4):   {f: []string{"b", "d"}, e: []string{"c", "e", "f", "x"}},
				or(4, 6):   {e: []string{"c", "e", "f", "x"}},
				or(6, 8):   {e: []string{"a", "y"}},
				or(8, 11):  {f: []string{"b", "d"}, e: []string{"c", "e", "f", "x"}},
				or(12, 14): {f: []string{"c", "e", "f", "x"}},
				or(14, 16): {f: []string{"a", "y"}, e: []string{"c", "e", "f", "x"}},
				or(16, 18): {e: []string{"c", "e", "f", "x"}},
				or(18, 20): {e: []string{"b", "d"}},
				or(20, 23): {f: []string{"a", "y"}, e: []string{"c", "e", "f", "x"}},
				or(24, 26): {f: []string{"c", "e", "f", "x"}},
				or1(26):    {f: []string{"d"}, e: []string{"c", "e", "f", "x"}},
				or(27, 29): {f: []string{"d"}},
				or1(29):    {f: []string{"a"}, e: []string{"c", "d", "e", "f", "x"}},
				or(30, 32): {f: []string{"a"}},
				or1(32):    {e: []string{"a", "c", "d", "e", "f", "x"}},
				or1(34):    {e: []string{"a", "c", "d", "e", "f", "x"}},
				or(35, 37): {f: []string{"c", "e", "f", "x"}},
				or1(37):    {f: []string{"y"}, e: []string{"c", "e", "f", "x"}},
				or(38, 40): {f: []string{"y"}},
				or1(40):    {f: []string{"b"}, e: []string{"c", "e", "f", "x", "y"}},
				or(41, 43): {f: []string{"b"}},
				or1(43):    {e: []string{"b", "c", "e", "f", "x", "y"}},
				or1(45):    {e: []string{"b", "c", "e", "f", "x", "y"}},
			},
		},

		{
			name: "String_Literal",
			archive: `-- a.cue --
x: y: a.b
a: b: 5
"a": b: 6
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "a"): {ln(2, 1, "a"), ln(3, 1, `"a"`)},
				ln(1, 1, "b"): {ln(2, 1, "b"), ln(3, 1, "b")},

				ln(1, 1, "x"):   {self},
				ln(1, 1, "y"):   {self},
				ln(2, 1, "a"):   {self, ln(3, 1, `"a"`)},
				ln(2, 1, "b"):   {self, ln(3, 1, "b")},
				ln(3, 1, `"a"`): {self, ln(2, 1, "a")},
				ln(3, 1, "b"):   {self, ln(2, 1, "b")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "x"}},
				or1(2):     {f: []string{"y"}, e: []string{"a", "x"}},
				or(3, 5):   {f: []string{"y"}},
				or(5, 8):   {e: []string{"a", "x", "y"}},
				or(8, 10):  {e: []string{"b"}},
				or(10, 12): {f: []string{"a", "x"}},
				or1(12):    {f: []string{"b"}, e: []string{"a", "x"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"a", "b", "x"}},
				or1(17):    {e: []string{"a", "b", "x"}},
				or1(18):    {f: []string{"a", "x"}},
				or(19, 22): {e: []string{"a", "x"}},
				or1(22):    {f: []string{"b"}, e: []string{"a", "x"}},
				or(23, 25): {f: []string{"b"}},
				or1(25):    {e: []string{"a", "b", "x"}},
				or1(27):    {e: []string{"a", "b", "x"}},
			},
		},

		{
			name: "List_Index",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}]
y: x[1].b`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"):  {ln(1, 1, "x")},
				ln(2, 1, "1]"): {ln(1, 2, "{")},
				ln(2, 1, "b"):  {ln(1, 1, "b")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or(2, 4):   {e: []string{"x", "y"}},
				or1(4):     {f: []string{"a"}, e: []string{"x", "y"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {e: []string{"a", "x", "y"}},
				or1(9):     {e: []string{"a", "x", "y"}},
				or1(11):    {e: []string{"x", "y"}},
				or1(12):    {f: []string{"b"}, e: []string{"x", "y"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"b", "x", "y"}},
				or1(17):    {e: []string{"b", "x", "y"}},
				or1(19):    {e: []string{"x", "y"}},
				or(20, 22): {f: []string{"x", "y"}},
				or(22, 25): {e: []string{"x", "y"}},
				or(28, 30): {e: []string{"b"}},
			},
		},

		{
			name: "List_Index_Ellipsis",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...{a: 4}]
y: x[17].a`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, "17]"): {ln(1, 1, "...")},
				ln(2, 1, "a"):   {ln(1, 2, "a")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "a"): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or(2, 4):   {e: []string{"x", "y"}},
				or1(4):     {f: []string{"a"}, e: []string{"x", "y"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {e: []string{"a", "x", "y"}},
				or1(9):     {e: []string{"a", "x", "y"}},
				or1(11):    {e: []string{"x", "y"}},
				or1(12):    {f: []string{"b"}, e: []string{"x", "y"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"b", "x", "y"}},
				or1(17):    {e: []string{"b", "x", "y"}},
				or1(19):    {e: []string{"x", "y"}},
				or(20, 24): {f: []string{"a"}, e: []string{"x", "y"}},
				or(24, 26): {f: []string{"a"}},
				or1(26):    {e: []string{"a", "x", "y"}},
				or1(28):    {e: []string{"a", "x", "y"}},
				or1(30):    {e: []string{"x", "y"}},
				or(31, 33): {f: []string{"x", "y"}},
				or(33, 36): {e: []string{"x", "y"}},
				or(40, 42): {e: []string{"a"}},
			},
		},

		{
			name: "List_Index_Ellipsis_Indirect",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...z]
y: x[17].a
z: a: 4`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "z"):   {ln(3, 1, "z")},
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, "17]"): {ln(1, 1, "...")},
				ln(2, 1, "a"):   {ln(3, 1, "a")},

				ln(1, 1, "x"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "y"): {self},

				ln(3, 1, "z"): {self},
				ln(3, 1, "a"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y", "z"}},
				or(2, 4):   {e: []string{"x", "y", "z"}},
				or1(4):     {f: []string{"a"}, e: []string{"x", "y", "z"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {e: []string{"a", "x", "y", "z"}},
				or1(9):     {e: []string{"a", "x", "y", "z"}},
				or1(11):    {e: []string{"x", "y", "z"}},
				or1(12):    {f: []string{"b"}, e: []string{"x", "y", "z"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"b", "x", "y", "z"}},
				or1(17):    {e: []string{"b", "x", "y", "z"}},
				or1(19):    {e: []string{"x", "y", "z"}},
				or(20, 25): {f: []string{"a"}, e: []string{"x", "y", "z"}},
				or1(25):    {e: []string{"x", "y", "z"}},
				or(26, 28): {f: []string{"x", "y", "z"}},
				or(28, 31): {e: []string{"x", "y", "z"}},
				or(35, 37): {e: []string{"a"}},
				or(37, 39): {f: []string{"x", "y", "z"}},
				or1(39):    {f: []string{"a"}, e: []string{"x", "y", "z"}},
				or(40, 42): {f: []string{"a"}},
				or1(42):    {e: []string{"a", "x", "y", "z"}},
				or1(44):    {e: []string{"a", "x", "y", "z"}},
			},
		},

		{
			name: "Ellipsis_Explicit",
			archive: `-- a.cue --
l: [...{x: int}]
d: l & [{x: 3}, {x: 4}]
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {ln(1, 1, "l")},

				ln(1, 1, "l"): {self},
				ln(1, 1, "x"): {self},

				ln(2, 1, "d"): {self},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 1, "x")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"d", "l"}},
				or(2, 4):   {e: []string{"d", "l"}},
				or(4, 8):   {f: []string{"x"}, e: []string{"d", "l"}},
				or(8, 10):  {f: []string{"x"}},
				or(10, 15): {e: []string{"d", "l", "x"}},
				or1(16):    {e: []string{"d", "l"}},
				or(17, 19): {f: []string{"d", "l"}},
				or(19, 25): {e: []string{"d", "l"}},
				or(25, 28): {f: []string{"x"}, e: []string{"d", "l"}},
				or1(28):    {e: []string{"d", "l", "x"}},
				or1(30):    {e: []string{"d", "l", "x"}},
				or(31, 33): {e: []string{"d", "l"}},
				or(33, 36): {f: []string{"x"}, e: []string{"d", "l"}},
				or1(36):    {e: []string{"d", "l", "x"}},
				or1(38):    {e: []string{"d", "l", "x"}},
				or(39, 41): {e: []string{"d", "l"}},
			},
		},

		{
			name: "Ellipsis_Implicit",
			archive: `-- a.cue --
d: [...{x: int}]
d: [{x: 3}, {x: 4}]
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "d"): {self, ln(2, 1, "d")},
				ln(1, 1, "x"): {self},

				ln(2, 1, "d"): {self, ln(1, 1, "d")},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},
				ln(2, 2, "x"): {self, ln(1, 1, "x")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"d"}},
				or(2, 4):   {e: []string{"d"}},
				or(4, 8):   {f: []string{"x"}, e: []string{"d"}},
				or(8, 10):  {f: []string{"x"}},
				or(10, 15): {e: []string{"d", "x"}},
				or1(16):    {e: []string{"d"}},
				or(17, 19): {f: []string{"d"}},
				or(19, 21): {e: []string{"d"}},
				or1(21):    {f: []string{"x"}, e: []string{"d"}},
				or(22, 24): {f: []string{"x"}},
				or1(24):    {e: []string{"d", "x"}},
				or1(26):    {e: []string{"d", "x"}},
				or1(28):    {e: []string{"d"}},
				or1(29):    {f: []string{"x"}, e: []string{"d"}},
				or(30, 32): {f: []string{"x"}},
				or1(32):    {e: []string{"d", "x"}},
				or1(34):    {e: []string{"d", "x"}},
				or1(36):    {e: []string{"d"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 1, "z"):  {ln(3, 1, "z")},
				ln(4, 1, "x"):  {ln(1, 1, "x")},
				ln(4, 1, "y"):  {ln(2, 1, "y")},
				ln(5, 1, "o"):  {ln(4, 1, "o")},
				ln(5, 1, "0]"): {ln(1, 1, "{"), ln(2, 1, "...")},
				ln(5, 1, "a"):  {ln(1, 1, "a"), ln(3, 1, "a")},
				ln(6, 1, "o"):  {ln(4, 1, "o")},
				ln(6, 1, "3]"): {ln(1, 1, "..."), ln(2, 1, "...")},
				ln(6, 1, "a"):  {ln(1, 2, "a"), ln(3, 1, "a")},

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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"o", "p", "q", "x", "y", "z"}},
				or(2, 4):   {e: []string{"o", "p", "q", "x", "y", "z"}},
				or1(4):     {f: []string{"a"}, e: []string{"o", "p", "q", "x", "y", "z"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {e: []string{"a", "o", "p", "q", "x", "y", "z"}},
				or1(9):     {e: []string{"a", "o", "p", "q", "x", "y", "z"}},
				or1(11):    {e: []string{"o", "p", "q", "x", "y", "z"}},
				or1(12):    {f: []string{"b"}, e: []string{"o", "p", "q", "x", "y", "z"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"b", "o", "p", "q", "x", "y", "z"}},
				or1(17):    {e: []string{"b", "o", "p", "q", "x", "y", "z"}},
				or1(19):    {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(20, 24): {f: []string{"a"}, e: []string{"o", "p", "q", "x", "y", "z"}},
				or(24, 26): {f: []string{"a"}},
				or1(26):    {e: []string{"a", "o", "p", "q", "x", "y", "z"}},
				or1(28):    {e: []string{"a", "o", "p", "q", "x", "y", "z"}},
				or1(30):    {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(31, 33): {f: []string{"o", "p", "q", "x", "y", "z"}},
				or(33, 35): {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(35, 40): {f: []string{"a"}, e: []string{"o", "p", "q", "x", "y", "z"}},
				or1(40):    {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(41, 43): {f: []string{"o", "p", "q", "x", "y", "z"}},
				or1(43):    {f: []string{"a"}, e: []string{"o", "p", "q", "x", "y", "z"}},
				or(44, 46): {f: []string{"a"}},
				or(46, 51): {e: []string{"a", "o", "p", "q", "x", "y", "z"}},
				or(51, 53): {f: []string{"o", "p", "q", "x", "y", "z"}},
				or(53, 60): {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(60, 62): {f: []string{"o", "p", "q", "x", "y", "z"}},
				or(62, 65): {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(68, 70): {e: []string{"a"}},
				or(70, 72): {f: []string{"o", "p", "q", "x", "y", "z"}},
				or(72, 75): {e: []string{"o", "p", "q", "x", "y", "z"}},
				or(78, 80): {e: []string{"a"}},
			},
		},

		{
			name: "StringLit_Conjunction",
			archive: `-- a.cue --
c: {a: b, "b": x: 3} & {b: x: 3, z: b.x}
b: e: 7
d: c.b.x
`,
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b", "c", "d"}},
				or(2, 4):   {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(4, 6):   {f: []string{"a", "b", "z"}},
				or(6, 9):   {f: []string{"e"}, e: []string{"a", "b", "c", "d"}},
				or1(9):     {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or1(10):    {f: []string{"a", "b", "z"}},
				or(11, 14): {e: []string{"a", "b", "z"}},
				or1(14):    {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				or(15, 17): {f: []string{"x"}},
				or1(17):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(19):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(23):    {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(24, 26): {f: []string{"a", "b", "z"}},
				or1(26):    {f: []string{"x"}, e: []string{"b", "c", "d", "z"}},
				or(27, 29): {f: []string{"x"}},
				or1(29):    {e: []string{"b", "c", "d", "x", "z"}},
				or1(31):    {e: []string{"b", "c", "d", "x", "z"}},
				or1(32):    {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(33, 35): {f: []string{"a", "b", "z"}},
				or(35, 38): {e: []string{"b", "c", "d", "z"}},
				or(38, 40): {e: []string{"x"}},
				or(41, 43): {f: []string{"b", "c", "d"}},
				or1(43):    {f: []string{"e"}, e: []string{"b", "c", "d"}},
				or(44, 46): {f: []string{"e"}},
				or1(46):    {e: []string{"b", "c", "d", "e"}},
				or1(48):    {e: []string{"b", "c", "d", "e"}},
				or(49, 51): {f: []string{"b", "c", "d"}},
				or(51, 54): {e: []string{"b", "c", "d"}},
				or(54, 56): {e: []string{"a", "b", "z"}},
				or(56, 58): {e: []string{"x"}},
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
			expectDefinitions: map[position][]position{
				ln(4, 1, "b"): {ln(1, 1, "b")},
				ln(4, 1, "a"): {ln(2, 1, "a")},

				ln(1, 1, "b"):   {self},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b"}},
				or(2, 7):   {f: []string{"a", "b"}, e: []string{"b"}},
				or(7, 9):   {f: []string{"a", "b"}},
				or1(9):     {e: []string{"a", "b"}},
				or1(11):    {e: []string{"a", "b"}},
				or(12, 14): {f: []string{"a", "b"}, e: []string{"b"}},
				or1(14):    {f: []string{"a", "b"}},
				or(15, 18): {e: []string{"a", "b"}},
				or(18, 25): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(25, 27): {f: []string{"a", "c"}},
				or(27, 30): {e: []string{"a", "b", "c"}},
				or(30, 32): {e: []string{"a", "b"}},
				or(32, 36): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(36, 38): {f: []string{"a", "c"}},
				or1(38):    {e: []string{"a", "b", "c"}},
				or1(41):    {e: []string{"a", "b", "c"}},
				or(42, 45): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or1(46):    {f: []string{"a", "b"}, e: []string{"b"}},
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
			expectDefinitions: map[position][]position{
				ln(4, 1, "b"): {ln(3, 1, `"b"`), ln(7, 1, "b")},
				ln(4, 1, "a"): {ln(5, 1, "a")},

				ln(1, 1, "b"):   {self},
				ln(2, 1, "a"):   {self},
				ln(3, 1, `"b"`): {self, ln(7, 1, "b")},
				ln(4, 1, "c"):   {self},
				ln(5, 1, "a"):   {self},
				ln(7, 1, "b"):   {self, ln(3, 1, `"b"`)},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b"}},
				or(2, 7):   {f: []string{"a", "b"}, e: []string{"b"}},
				or(7, 9):   {f: []string{"a", "b"}},
				or1(9):     {e: []string{"a", "b"}},
				or1(11):    {e: []string{"a", "b"}},
				or(12, 14): {f: []string{"a", "b"}, e: []string{"b"}},
				or1(14):    {f: []string{"a", "b"}},
				or(15, 18): {e: []string{"a", "b"}},
				or(18, 25): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(25, 27): {f: []string{"a", "c"}},
				or(27, 30): {e: []string{"a", "b", "c"}},
				or(30, 32): {e: []string{"a", "c"}},
				or(32, 36): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(36, 38): {f: []string{"a", "c"}},
				or1(38):    {e: []string{"a", "b", "c"}},
				or1(41):    {e: []string{"a", "b", "c"}},
				or(42, 45): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(46, 48): {f: []string{"a", "b"}, e: []string{"b"}},
				or(48, 50): {f: []string{"a", "b"}},
				or(50, 53): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or1(53):    {f: []string{"a", "b"}, e: []string{"b"}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b"}},
				or(2, 7):   {f: []string{"a", "b"}, e: []string{"b"}},
				or(7, 9):   {f: []string{"a", "b"}},
				or1(9):     {e: []string{"a", "b"}},
				or1(11):    {e: []string{"a", "b"}},
				or(12, 14): {f: []string{"a", "b"}, e: []string{"b"}},
				or1(14):    {f: []string{"a", "b"}},
				or(15, 18): {e: []string{"a", "b"}},
				or(18, 25): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(25, 27): {f: []string{"a", "c"}},
				or(27, 30): {e: []string{"a", "b", "c"}},
				or(30, 32): {e: []string{"a", "b"}},
				or(32, 36): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or(36, 38): {f: []string{"a", "c"}},
				or1(38):    {e: []string{"a", "b", "c"}},
				or1(41):    {e: []string{"a", "b", "c"}},
				or(42, 45): {f: []string{"a", "c"}, e: []string{"a", "b"}},
				or1(46):    {f: []string{"a", "b"}, e: []string{"b"}},
				or(48, 50): {f: []string{"b"}},
				or1(50):    {f: []string{"a", "b"}, e: []string{"b"}},
				or(51, 53): {f: []string{"a", "b"}},
				or(53, 56): {f: []string{"a", "c"}, e: []string{"b"}},
			},
		},

		{
			name: "Inline_Struct_Selector",
			archive: `-- a.cue --
a: {in: {x: 5}, out: in}.out.x`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "in"):  {ln(1, 1, "in")},
				ln(1, 2, "out"): {ln(1, 1, "out")},
				ln(1, 2, "x"):   {ln(1, 1, "x")},

				ln(1, 1, "a"):   {self},
				ln(1, 1, "in"):  {self},
				ln(1, 1, "x"):   {self},
				ln(1, 1, "out"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {e: []string{"a"}},
				or1(3):     {f: []string{"in", "out"}, e: []string{"a"}},
				or(4, 7):   {f: []string{"in", "out"}},
				or(7, 9):   {f: []string{"x"}, e: []string{"a", "in", "out"}},
				or(9, 11):  {f: []string{"x"}},
				or1(11):    {e: []string{"a", "in", "out", "x"}},
				or1(13):    {e: []string{"a", "in", "out", "x"}},
				or1(15):    {f: []string{"in", "out"}, e: []string{"a"}},
				or(16, 20): {f: []string{"in", "out"}},
				or(20, 24): {f: []string{"x"}, e: []string{"a", "in", "out"}},
				or(25, 29): {e: []string{"in", "out"}},
				or(29, 31): {e: []string{"x"}},
			},
		},

		{
			name: "Inline_Struct_Selector_Nested",
			archive: `-- a.cue --
a: b: {
    c: d: {
        g: h: m.n
        i: g
        j: i
    }.j
    e: c
    f: e
}.f
k: l: a.b.d.h
m: n: o: 33`,
			expectDefinitions: map[position][]position{
				ln(3, 1, "m"): {ln(11, 1, "m")},
				ln(3, 1, "n"): {ln(11, 1, "n")},

				ln(4, 1, "g"): {ln(3, 1, "g")},
				ln(5, 1, "i"): {ln(4, 1, "i")},
				ln(6, 1, "j"): {ln(5, 1, "j")},
				ln(7, 1, "c"): {ln(2, 1, "c")},
				ln(8, 1, "e"): {ln(7, 1, "e")},
				ln(9, 1, "f"): {ln(8, 1, "f")},

				ln(10, 1, "a"): {ln(1, 1, "a")},
				ln(10, 1, "b"): {ln(1, 1, "b")},
				ln(10, 1, "d"): {ln(2, 1, "d")},
				ln(10, 1, "h"): {ln(3, 1, "h")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
				ln(2, 1, "d"): {self},

				ln(3, 1, "g"): {self},
				ln(3, 1, "h"): {self},

				ln(4, 1, "i"): {self},
				ln(5, 1, "j"): {self},

				ln(7, 1, "e"): {self},
				ln(8, 1, "f"): {self},

				ln(10, 1, "k"): {self},
				ln(10, 1, "l"): {self},

				ln(11, 1, "m"): {self},
				ln(11, 1, "n"): {self},
				ln(11, 1, "o"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):     {f: []string{"a", "k", "m"}},
				or1(2):       {f: []string{"b"}, e: []string{"a", "k", "m"}},
				or(3, 5):     {f: []string{"b"}},
				or1(5):       {f: []string{"d"}, e: []string{"a", "b", "k", "m"}},
				or(6, 12):    {f: []string{"c", "e", "f"}, e: []string{"a", "b", "k", "m"}},
				or(12, 14):   {f: []string{"c", "e", "f"}},
				or1(14):      {f: []string{"d"}, e: []string{"a", "b", "c", "e", "f", "k", "m"}},
				or(15, 17):   {f: []string{"d"}},
				or1(17):      {f: []string{"h"}, e: []string{"a", "b", "c", "d", "e", "f", "k", "m"}},
				or(18, 28):   {f: []string{"g", "i", "j"}, e: []string{"a", "b", "c", "d", "e", "f", "k", "m"}},
				or(28, 30):   {f: []string{"g", "i", "j"}},
				or1(30):      {f: []string{"h"}, e: []string{"a", "b", "c", "d", "e", "f", "g", "i", "j", "k", "m"}},
				or(31, 33):   {f: []string{"h"}},
				or1(33):      {f: []string{"o"}, e: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "m"}},
				or(34, 36):   {e: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "m"}},
				or(36, 38):   {e: []string{"n"}},
				or(38, 46):   {f: []string{"g", "i", "j"}, e: []string{"a", "b", "c", "d", "e", "f", "k", "m"}},
				or(46, 48):   {f: []string{"g", "i", "j"}},
				or(48, 51):   {f: []string{"h"}, e: []string{"a", "b", "c", "d", "e", "f", "g", "i", "j", "k", "m"}},
				or(51, 59):   {f: []string{"g", "i", "j"}, e: []string{"a", "b", "c", "d", "e", "f", "k", "m"}},
				or(59, 61):   {f: []string{"g", "i", "j"}},
				or(61, 64):   {f: []string{"h"}, e: []string{"a", "b", "c", "d", "e", "f", "g", "i", "j", "k", "m"}},
				or(64, 69):   {f: []string{"g", "i", "j"}, e: []string{"a", "b", "c", "d", "e", "f", "k", "m"}},
				or(70, 72):   {e: []string{"g", "i", "j"}},
				or(72, 76):   {f: []string{"c", "e", "f"}, e: []string{"a", "b", "k", "m"}},
				or(76, 78):   {f: []string{"c", "e", "f"}},
				or(78, 81):   {f: []string{"d"}, e: []string{"a", "b", "c", "e", "f", "k", "m"}},
				or(81, 85):   {f: []string{"c", "e", "f"}, e: []string{"a", "b", "k", "m"}},
				or(85, 87):   {f: []string{"c", "e", "f"}},
				or(87, 90):   {f: []string{"d"}, e: []string{"a", "b", "c", "e", "f", "k", "m"}},
				or1(90):      {f: []string{"c", "e", "f"}, e: []string{"a", "b", "k", "m"}},
				or(92, 94):   {e: []string{"c", "e", "f"}},
				or(94, 96):   {f: []string{"a", "k", "m"}},
				or1(96):      {f: []string{"l"}, e: []string{"a", "k", "m"}},
				or(97, 99):   {f: []string{"l"}},
				or1(99):      {f: []string{"o"}, e: []string{"a", "k", "l", "m"}},
				or(100, 102): {e: []string{"a", "k", "l", "m"}},
				or(102, 104): {e: []string{"b"}},
				or(104, 106): {e: []string{"d"}},
				or(106, 108): {e: []string{"h"}},
				or(108, 110): {f: []string{"a", "k", "m"}},
				or1(110):     {f: []string{"n"}, e: []string{"a", "k", "m"}},
				or(111, 113): {f: []string{"n"}},
				or1(113):     {f: []string{"o"}, e: []string{"a", "k", "m", "n"}},
				or(114, 116): {f: []string{"o"}},
				or1(116):     {e: []string{"a", "k", "m", "n", "o"}},
				or1(119):     {e: []string{"a", "k", "m", "n", "o"}},
			},
		},

		{
			name: "Inline_List_Index_LiteralConst",
			archive: `-- a.cue --
a: [7, {b: 3}, true][1].b`,
			// If the index is a literal const we do resolve it.
			expectDefinitions: map[position][]position{
				ln(1, 1, "1]"): {ln(1, 1, "{")},
				ln(1, 2, "b"):  {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or(2, 4):   {e: []string{"a"}},
				or(5, 7):   {e: []string{"a"}},
				or1(7):     {f: []string{"b"}, e: []string{"a"}},
				or(8, 10):  {f: []string{"b"}},
				or1(10):    {e: []string{"a", "b"}},
				or1(12):    {e: []string{"a", "b"}},
				or1(14):    {e: []string{"a"}},
				or(19, 21): {e: []string{"a"}},
				or(24, 26): {e: []string{"b"}},
			},
		},

		{
			name: "Inline_List_Index_Dynamic",
			archive: `-- a.cue --
a: [7, {b: 3}, true][n].b
n: 1
`,
			// Even the slightest indirection defeats indexing
			expectDefinitions: map[position][]position{
				ln(1, 1, "["): {},
				ln(1, 1, "n"): {ln(2, 1, "n")},
				ln(1, 1, "]"): {},
				ln(1, 2, "b"): {},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "n"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "n"}},
				or(2, 4):   {e: []string{"a", "n"}},
				or(5, 7):   {e: []string{"a", "n"}},
				or1(7):     {f: []string{"b"}, e: []string{"a", "n"}},
				or(8, 10):  {f: []string{"b"}},
				or1(10):    {e: []string{"a", "b", "n"}},
				or1(12):    {e: []string{"a", "b", "n"}},
				or1(14):    {e: []string{"a", "n"}},
				or(19, 26): {e: []string{"a", "n"}},
				or(26, 28): {f: []string{"a", "n"}},
				or1(28):    {e: []string{"a", "n"}},
				or1(30):    {e: []string{"a", "n"}},
			},
		},

		{
			name: "StringLit_Struct_Index_LiteralConst",
			archive: `-- a.cue --
x: "a b": z: 5
y: x["a b"].z`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"):      {ln(1, 1, "x")},
				ln(2, 1, `"a b"]`): {ln(1, 1, `"a b"`)},
				ln(2, 1, "z"):      {ln(1, 1, "z")},

				ln(1, 1, "x"):     {self},
				ln(1, 1, `"a b"`): {self},
				ln(1, 1, "z"):     {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or1(2):     {f: []string{"a b"}, e: []string{"x", "y"}},
				or1(3):     {f: []string{"a b"}},
				or(4, 9):   {e: []string{"a b"}},
				or1(9):     {f: []string{"z"}, e: []string{"a b", "x", "y"}},
				or(10, 12): {f: []string{"z"}},
				or1(12):    {e: []string{"a b", "x", "y", "z"}},
				or1(14):    {e: []string{"a b", "x", "y", "z"}},
				or(15, 17): {f: []string{"x", "y"}},
				or(17, 20): {e: []string{"x", "y"}},
				or(20, 27): {e: []string{"a b"}},
				or(27, 29): {e: []string{"z"}},
			},
		},

		{
			name: "Inline_Disjunction_Internal",
			archive: `-- a.cue --
a: ({b: c, c: 3} | {c: 4}).c`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "c"): {ln(1, 2, "c")},
				ln(1, 4, "c"): {ln(1, 2, "c"), ln(1, 3, "c")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "c"): {self},
				ln(1, 3, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {e: []string{"a"}},
				or(3, 5):   {f: []string{"b", "c"}, e: []string{"a"}},
				or(5, 7):   {f: []string{"b", "c"}},
				or(7, 10):  {e: []string{"a", "b", "c"}},
				or1(10):    {f: []string{"b", "c"}, e: []string{"a"}},
				or(11, 13): {f: []string{"b", "c"}},
				or1(13):    {e: []string{"a", "b", "c"}},
				or1(15):    {e: []string{"a", "b", "c"}},
				or(17, 19): {e: []string{"a"}},
				or1(19):    {f: []string{"c"}, e: []string{"a"}},
				or(20, 22): {f: []string{"c"}},
				or1(22):    {e: []string{"a", "c"}},
				or1(24):    {e: []string{"a", "c"}},
				or1(26):    {e: []string{"a"}},
				or(27, 29): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Cycle_Simple2",
			archive: `-- a.cue --
a: b
b: a`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):  {f: []string{"a", "b"}},
				or(2, 5):  {e: []string{"a", "b"}},
				or(5, 7):  {f: []string{"a", "b"}},
				or(7, 10): {e: []string{"a", "b"}},
			},
		},

		{
			name: "Cycle_Simple3",
			archive: `-- a.cue --
a: b
b: c
c: a`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "c"): {ln(3, 1, "c")},
				ln(3, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
				ln(3, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "b", "c"}},
				or(2, 5):   {e: []string{"a", "b", "c"}},
				or(5, 7):   {f: []string{"a", "b", "c"}},
				or(7, 10):  {e: []string{"a", "b", "c"}},
				or(10, 12): {f: []string{"a", "b", "c"}},
				or(12, 15): {e: []string{"a", "b", "c"}},
			},
		},

		// These "structural" cycles are errors in the evaluator. But
		// there's no reason we can't resolve them.
		{
			name: "Cycle_Structural_Simple",
			archive: `-- a.cue --
a: b: c: a`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):  {f: []string{"a"}},
				or1(2):    {f: []string{"b"}, e: []string{"a"}},
				or(3, 5):  {f: []string{"b"}},
				or1(5):    {f: []string{"c"}, e: []string{"a", "b"}},
				or(6, 8):  {f: []string{"c"}},
				or(8, 11): {f: []string{"b"}, e: []string{"a", "b", "c"}},
			},
		},

		{
			name: "Structural_Simple_Selector",
			archive: `-- a.cue --
a: b: c: a.b`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {f: []string{"b"}, e: []string{"a"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {f: []string{"c"}, e: []string{"a", "b"}},
				or(6, 8):   {f: []string{"c"}},
				or1(8):     {f: []string{"c"}, e: []string{"a", "b", "c"}},
				or(9, 11):  {e: []string{"a", "b", "c"}},
				or(11, 13): {e: []string{"b"}},
			},
		},

		{
			name: "Cycle_Structural_Complex",
			archive: `-- a.cue --
y: [string]: b: y
x: y
x: c: x
`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "y"): {ln(1, 1, "y")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
				ln(3, 2, "x"): {ln(2, 1, "x"), ln(3, 1, "x")},

				ln(1, 1, "y"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "x"): {self, ln(3, 1, "x")},

				ln(3, 1, "x"): {self, ln(2, 1, "x")},
				ln(3, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or1(2):     {e: []string{"x", "y"}},
				or(4, 11):  {e: []string{"x", "y"}},
				or1(12):    {f: []string{"b"}, e: []string{"x", "y"}},
				or(13, 15): {f: []string{"b"}},
				or(15, 18): {e: []string{"b", "x", "y"}},
				or(18, 20): {f: []string{"x", "y"}},
				or(20, 23): {f: []string{"c"}, e: []string{"x", "y"}},
				or(23, 25): {f: []string{"x", "y"}},
				or1(25):    {f: []string{"c"}, e: []string{"x", "y"}},
				or(26, 28): {f: []string{"c"}},
				or(28, 31): {f: []string{"c"}, e: []string{"c", "x", "y"}},
			},
		},

		{
			name: "Alias_Plain_Label_Internal",
			archive: `-- a.cue --
l=a: {b: 3, c: l.b}`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "l"): {ln(1, 1, "a")},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(0):     {f: []string{"a"}},
				or1(1):     {e: []string{"a", "l"}},
				or(2, 4):   {f: []string{"a"}},
				or(4, 6):   {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(6, 8):   {f: []string{"b", "c"}},
				or1(8):     {e: []string{"a", "b", "c", "l"}},
				or1(10):    {e: []string{"a", "b", "c", "l"}},
				or1(11):    {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(12, 14): {f: []string{"b", "c"}},
				or(14, 17): {e: []string{"a", "b", "c", "l"}},
				or(17, 19): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Plain_Label_Internal_Implicit",
			archive: `-- a.cue --
l=a: b: 3
a: c: l.b`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self, ln(1, 1, "a")},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(0):     {f: []string{"a"}},
				or1(1):     {e: []string{"a", "l"}},
				or(2, 4):   {f: []string{"a"}},
				or1(4):     {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(5, 7):   {f: []string{"b", "c"}},
				or1(7):     {e: []string{"a", "b", "l"}},
				or1(9):     {e: []string{"a", "b", "l"}},
				or(10, 12): {f: []string{"a"}},
				or1(12):    {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(13, 15): {f: []string{"b", "c"}},
				or(15, 18): {e: []string{"a", "c", "l"}},
				or(18, 20): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Plain_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
a: b: 3
l=a: c: l.b`,
			expectDefinitions: map[position][]position{
				ln(2, 2, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(1, 1, "b"): {self},

				ln(2, 1, "l"): {ln(2, 1, "a"), ln(1, 1, "a")},
				ln(2, 1, "a"): {self, ln(1, 1, "a")},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(3, 5):   {f: []string{"b", "c"}},
				or1(5):     {e: []string{"a", "b", "l"}},
				or1(7):     {e: []string{"a", "b", "l"}},
				or1(8):     {f: []string{"a"}},
				or1(9):     {e: []string{"a", "l"}},
				or(10, 12): {f: []string{"a"}},
				or1(12):    {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(13, 15): {f: []string{"b", "c"}},
				or(15, 18): {e: []string{"a", "c", "l"}},
				or(18, 20): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Plain_Label_External",
			archive: `-- a.cue --
l=a: b: 3
c: l.b`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "l"): {ln(1, 1, "a")},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(0):     {f: []string{"a", "c"}},
				or1(1):     {e: []string{"a", "c", "l"}},
				or(2, 4):   {f: []string{"a", "c"}},
				or1(4):     {f: []string{"b"}, e: []string{"a", "c", "l"}},
				or(5, 7):   {f: []string{"b"}},
				or1(7):     {e: []string{"a", "b", "c", "l"}},
				or1(9):     {e: []string{"a", "b", "c", "l"}},
				or(10, 12): {f: []string{"a", "c"}},
				or(12, 15): {e: []string{"a", "c", "l"}},
				or(15, 17): {e: []string{"b"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 2, "l"): {ln(2, 1, "b")},
				ln(2, 1, "d"): {ln(2, 2, "d")},
				ln(3, 1, "l"): {ln(2, 1, "b")},
				ln(3, 1, "d"): {ln(2, 2, "d")},
				ln(5, 1, "l"): {},
				ln(5, 1, "d"): {},
				ln(6, 1, "a"): {ln(1, 1, "a"), ln(5, 1, "a")},
				ln(6, 1, "l"): {},
				ln(1, 1, "a"): {self, ln(5, 1, "a")},

				ln(2, 1, "l"): {ln(2, 1, "b")},
				ln(2, 1, "b"): {self},
				ln(2, 1, "c"): {self},
				ln(2, 2, "d"): {self},

				ln(3, 1, "e"): {self},

				ln(5, 1, "a"): {self, ln(1, 1, "a")},
				ln(5, 1, "f"): {self},

				ln(6, 1, "h"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "h"}},
				or(2, 6):   {f: []string{"b", "e", "f"}, e: []string{"a", "h"}},
				or1(6):     {f: []string{"b", "e", "f"}},
				or1(7):     {e: []string{"b", "e", "f"}},
				or(8, 10):  {f: []string{"b", "e", "f"}},
				or(10, 12): {f: []string{"c", "d"}, e: []string{"a", "b", "e", "h", "l"}},
				or(12, 14): {f: []string{"c", "d"}},
				or(14, 17): {e: []string{"a", "b", "c", "d", "e", "h", "l"}},
				or(17, 19): {e: []string{"c", "d"}},
				or1(19):    {f: []string{"c", "d"}, e: []string{"a", "b", "e", "h", "l"}},
				or(20, 22): {f: []string{"c", "d"}},
				or1(22):    {e: []string{"a", "b", "c", "d", "e", "h", "l"}},
				or1(24):    {e: []string{"a", "b", "c", "d", "e", "h", "l"}},
				or1(26):    {f: []string{"b", "e", "f"}, e: []string{"a", "h"}},
				or(27, 29): {f: []string{"b", "e", "f"}},
				or(29, 32): {e: []string{"a", "b", "e", "h", "l"}},
				or(32, 34): {e: []string{"c", "d"}},
				or1(34):    {f: []string{"b", "e", "f"}, e: []string{"a", "h"}},
				or(36, 38): {f: []string{"a", "h"}},
				or1(38):    {f: []string{"b", "e", "f"}, e: []string{"a", "h"}},
				or(39, 41): {f: []string{"b", "e", "f"}},
				or(41, 44): {e: []string{"a", "f", "h"}},
				or(46, 48): {f: []string{"a", "h"}},
				or(48, 51): {e: []string{"a", "h"}},
				or(51, 53): {e: []string{"b", "e", "f"}},
			},
		},

		{
			name: "Alias_Dynamic_Label_Internal",
			archive: `-- a.cue --
l=(a): {b: 3, c: l.b}`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(1):     {e: []string{"l"}},
				or(3, 5):   {e: []string{"l"}},
				or(6, 8):   {f: []string{"b", "c"}, e: []string{"l"}},
				or(8, 10):  {f: []string{"b", "c"}},
				or1(10):    {e: []string{"b", "c", "l"}},
				or1(12):    {e: []string{"b", "c", "l"}},
				or1(13):    {f: []string{"b", "c"}, e: []string{"l"}},
				or(14, 16): {f: []string{"b", "c"}},
				or(16, 19): {e: []string{"b", "c", "l"}},
				or(19, 21): {e: []string{"b", "c"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {ln(1, 1, "l")},
				ln(2, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(1):     {e: []string{"l"}},
				or(3, 5):   {e: []string{"l"}},
				or1(6):     {f: []string{"b"}, e: []string{"l"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"b", "l"}},
				or1(11):    {e: []string{"b", "l"}},
				or(13, 15): {e: []string{"l"}},
				or1(16):    {f: []string{"c"}, e: []string{"l"}},
				or(17, 19): {f: []string{"c"}},
				or(19, 22): {e: []string{"c", "l"}},
				or(22, 24): {e: []string{"b"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 2, "l"): {ln(2, 1, "l")},
				ln(2, 1, "b"): {},

				ln(1, 1, "b"): {self},
				ln(2, 1, "l"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(1, 3):   {e: []string{"l"}},
				or1(4):     {f: []string{"b"}, e: []string{"l"}},
				or(5, 7):   {f: []string{"b"}},
				or1(7):     {e: []string{"b", "l"}},
				or1(9):     {e: []string{"b", "l"}},
				or1(11):    {e: []string{"l"}},
				or(13, 15): {e: []string{"l"}},
				or1(16):    {f: []string{"c"}, e: []string{"l"}},
				or(17, 19): {f: []string{"c"}},
				or(19, 22): {e: []string{"c", "l"}},
				or(22, 24): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Dynamic_Label_External",
			archive: `-- a.cue --
l=(a): b: 3
c: l.b`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {ln(1, 1, ("l"))},
				ln(2, 1, "b"): {ln(1, 1, ("b"))},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(0):     {f: []string{"c"}},
				or1(1):     {e: []string{"c", "l"}},
				or(3, 5):   {e: []string{"c", "l"}},
				or1(6):     {f: []string{"b"}, e: []string{"c", "l"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"b", "c", "l"}},
				or1(11):    {e: []string{"b", "c", "l"}},
				or(12, 14): {f: []string{"c"}},
				or(14, 17): {e: []string{"c", "l"}},
				or(17, 19): {e: []string{"b"}},
			},
		},

		{
			name: "Alias_Pattern_Label_Internal",
			archive: `-- a.cue --
l=[a]: {b: 3, c: l.b}`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(1):     {e: []string{"l"}},
				or(3, 5):   {e: []string{"l"}},
				or(6, 8):   {f: []string{"b", "c"}, e: []string{"l"}},
				or(8, 10):  {f: []string{"b", "c"}},
				or1(10):    {e: []string{"b", "c", "l"}},
				or1(12):    {e: []string{"b", "c", "l"}},
				or1(13):    {f: []string{"b", "c"}, e: []string{"l"}},
				or(14, 16): {f: []string{"b", "c"}},
				or(16, 19): {e: []string{"b", "c", "l"}},
				or(19, 21): {e: []string{"b", "c"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(1):     {e: []string{"l"}},
				or(3, 5):   {e: []string{"l"}},
				or1(6):     {f: []string{"b"}, e: []string{"l"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"b", "l"}},
				or1(11):    {e: []string{"b", "l"}},
				or(16, 19): {f: []string{"c"}},
				or(19, 22): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Label_Internal_Implicit_Reversed",
			archive: `-- a.cue --
[a]: b: 3
l=[a]: c: l.b`,
			// Again, the two [a] patterns are not merged. The l of l.b
			// can be resolved, but not the b.
			expectDefinitions: map[position][]position{
				ln(2, 2, "l"): {ln(2, 1, "l")},
				ln(2, 1, "b"): {},

				ln(2, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(4, 7):   {f: []string{"b"}},
				or1(7):     {e: []string{"b"}},
				or1(9):     {e: []string{"b"}},
				or1(11):    {e: []string{"l"}},
				or(13, 15): {e: []string{"l"}},
				or1(16):    {f: []string{"c"}, e: []string{"l"}},
				or(17, 19): {f: []string{"c"}},
				or(19, 22): {e: []string{"c", "l"}},
				or(22, 24): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Label_External",
			archive: `-- a.cue --
l=[a]: b: 3
c: l.b`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or1(0):     {f: []string{"c"}},
				or1(1):     {e: []string{"c", "l"}},
				or(3, 5):   {e: []string{"c", "l"}},
				or1(6):     {f: []string{"b"}, e: []string{"c", "l"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"b", "c", "l"}},
				or1(11):    {e: []string{"b", "c", "l"}},
				or(12, 14): {f: []string{"c"}},
				or(14, 17): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Expr_Internal",
			archive: `-- a.cue --
[l=a]: {b: 3, c: l, d: l.b}`,
			// This type of alias binds l to the key. So c: l will work,
			// but for the b in d: l.b there is no resolution.
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 3, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
				ln(1, 1, "d"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(3, 5):   {e: []string{"l"}},
				or(6, 8):   {f: []string{"b", "c", "d"}, e: []string{"l"}},
				or(8, 10):  {f: []string{"b", "c", "d"}},
				or1(10):    {e: []string{"b", "c", "d", "l"}},
				or1(12):    {e: []string{"b", "c", "d", "l"}},
				or1(13):    {f: []string{"b", "c", "d"}, e: []string{"l"}},
				or(14, 16): {f: []string{"b", "c", "d"}},
				or(16, 19): {e: []string{"b", "c", "d", "l"}},
				or1(19):    {f: []string{"b", "c", "d"}, e: []string{"l"}},
				or(20, 22): {f: []string{"b", "c", "d"}},
				or(22, 25): {e: []string{"b", "c", "d", "l"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(3, 5):   {e: []string{"l"}},
				or1(6):     {f: []string{"b"}, e: []string{"l"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"b", "l"}},
				or1(11):    {e: []string{"b", "l"}},
				or(16, 19): {f: []string{"c"}},
				or(19, 22): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Pattern_Expr_External",
			archive: `-- a.cue --
[l=a]: b: 3
c: l`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {},

				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 3):   {f: []string{"c"}},
				or(3, 5):   {e: []string{"c", "l"}},
				or1(6):     {f: []string{"b"}, e: []string{"c", "l"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"b", "c", "l"}},
				or1(11):    {e: []string{"b", "c", "l"}},
				or(12, 14): {f: []string{"c"}},
				or(14, 17): {e: []string{"c"}},
			},
		},

		{
			name: "Alias_Expr_Internal",
			archive: `-- a.cue --
a: l={b: 3, c: l.b}`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {f: []string{"b", "c"}, e: []string{"a"}},
				or(3, 5):   {f: []string{"b", "c"}},
				or1(5):     {e: []string{"a"}},
				or(6, 8):   {f: []string{"b", "c"}},
				or1(8):     {e: []string{"a", "b", "c", "l"}},
				or1(10):    {e: []string{"a", "b", "c", "l"}},
				or1(11):    {e: []string{"a"}},
				or(12, 14): {f: []string{"b", "c"}},
				or(14, 17): {e: []string{"a", "b", "c", "l"}},
				or(17, 19): {e: []string{"b", "c"}},
				or1(19):    {e: []string{"a"}},
			},
		},

		{
			name: "Alias_Expr_Internal_Explicit",
			archive: `-- a.cue --
a: l={b: 3} & {c: l.b}`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {f: []string{"b", "c"}, e: []string{"a"}},
				or(3, 5):   {f: []string{"b", "c"}},
				or1(5):     {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(6, 8):   {f: []string{"b", "c"}},
				or1(8):     {e: []string{"a", "b", "l"}},
				or1(10):    {e: []string{"a", "b", "l"}},
				or1(14):    {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(15, 17): {f: []string{"b", "c"}},
				or(17, 20): {e: []string{"a", "c", "l"}},
				or(20, 22): {e: []string{"b", "c"}},
			},
		},

		{
			name: "Alias_Expr_Internal_Explicit_Paren",
			// The previous test case works because it's parsed like
			// this:
			archive: `-- a.cue --
a: l=({b: 3} & {c: l.b})`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {f: []string{"b", "c"}, e: []string{"a"}},
				or(3, 5):   {f: []string{"b", "c"}},
				or1(5):     {e: []string{"a"}},
				or1(6):     {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(7, 9):   {f: []string{"b", "c"}},
				or1(9):     {e: []string{"a", "b", "l"}},
				or1(11):    {e: []string{"a", "b", "l"}},
				or1(15):    {f: []string{"b", "c"}, e: []string{"a", "l"}},
				or(16, 18): {f: []string{"b", "c"}},
				or(18, 21): {e: []string{"a", "c", "l"}},
				or(21, 23): {e: []string{"b", "c"}},
				or1(24):    {e: []string{"a"}},
			},
		},

		{
			name: "Alias_Expr_External",
			archive: `-- a.cue --
a: l={b: 3}
c: l.b
d: a.b`,
			// This type of alias is only visible within the value.
			expectDefinitions: map[position][]position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
				ln(3, 1, "a"): {ln(1, 1, "a")},
				ln(3, 1, "b"): {ln(1, 1, "b")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "l"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "c"): {self},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "c", "d"}},
				or1(2):     {f: []string{"b"}, e: []string{"a", "c", "d"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {e: []string{"a", "c", "d"}},
				or(6, 8):   {f: []string{"b"}},
				or1(8):     {e: []string{"a", "b", "c", "d", "l"}},
				or1(10):    {e: []string{"a", "b", "c", "d", "l"}},
				or1(11):    {e: []string{"a", "c", "d"}},
				or(12, 14): {f: []string{"a", "c", "d"}},
				or(14, 17): {e: []string{"a", "c", "d"}},
				or(19, 21): {f: []string{"a", "c", "d"}},
				or(21, 24): {e: []string{"a", "c", "d"}},
				or(24, 26): {e: []string{"b"}},
			},
		},

		{
			name: "Alias_Expr_Call",
			archive: `-- a.cue --
a: n=(2 * (div(n, 2))) | error("\(n) is not even")
`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "n"): {ln(1, 1, "n")},
				ln(1, 3, "n"): {ln(1, 1, "n")},

				ln(1, 1, "n"): {self},
				ln(1, 1, "a"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {e: []string{"a"}},
				or1(5):     {e: []string{"a", "n"}},
				or(7, 18):  {e: []string{"a", "n"}},
				or(19, 23): {e: []string{"a", "n"}},
				or(23, 25): {e: []string{"a"}},
				or(25, 31): {e: []string{"a", "n"}},
				or(34, 36): {e: []string{"a", "n"}},
				or(49, 51): {e: []string{"a", "n"}},
			},
		},

		{
			name: "Call_Arg_Expr",
			archive: `-- a.cue --
c: (f({a: b, b: 3})).g
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"c"}},
				or(2, 6):   {e: []string{"c"}},
				or1(6):     {f: []string{"a", "b"}, e: []string{"c"}},
				or(7, 9):   {f: []string{"a", "b"}},
				or(9, 12):  {e: []string{"a", "b", "c"}},
				or1(12):    {f: []string{"a", "b"}, e: []string{"c"}},
				or(13, 15): {f: []string{"a", "b"}},
				or1(15):    {e: []string{"a", "b", "c"}},
				or1(17):    {e: []string{"a", "b", "c"}},
				or(19, 21): {e: []string{"c"}},
			},
		},

		{
			name: "Disjunction_Simple",
			archive: `-- a.cue --
d: {a: b: 3} | {a: b: 4, c: 5}
o: d.a.b
p: d.c
`,
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"d", "o", "p"}},
				or1(2):     {f: []string{"a", "c"}, e: []string{"d", "o", "p"}},
				or1(3):     {f: []string{"a"}, e: []string{"d", "o", "p"}},
				or(4, 6):   {f: []string{"a"}},
				or1(6):     {f: []string{"b"}, e: []string{"a", "d", "o", "p"}},
				or(7, 9):   {f: []string{"b"}},
				or1(9):     {e: []string{"a", "b", "d", "o", "p"}},
				or1(11):    {e: []string{"a", "b", "d", "o", "p"}},
				or(13, 15): {e: []string{"d", "o", "p"}},
				or1(15):    {f: []string{"a", "c"}, e: []string{"d", "o", "p"}},
				or(16, 18): {f: []string{"a", "c"}},
				or1(18):    {f: []string{"b"}, e: []string{"a", "c", "d", "o", "p"}},
				or(19, 21): {f: []string{"b"}},
				or1(21):    {e: []string{"a", "b", "c", "d", "o", "p"}},
				or1(23):    {e: []string{"a", "b", "c", "d", "o", "p"}},
				or1(24):    {f: []string{"a", "c"}, e: []string{"d", "o", "p"}},
				or(25, 27): {f: []string{"a", "c"}},
				or1(27):    {e: []string{"a", "c", "d", "o", "p"}},
				or1(29):    {e: []string{"a", "c", "d", "o", "p"}},
				or(31, 33): {f: []string{"d", "o", "p"}},
				or(33, 36): {e: []string{"d", "o", "p"}},
				or(36, 38): {e: []string{"a", "c"}},
				or(38, 40): {e: []string{"b"}},
				or(40, 42): {f: []string{"d", "o", "p"}},
				or(42, 45): {e: []string{"d", "o", "p"}},
				or(45, 47): {e: []string{"a", "c"}},
			},
		},

		{
			name: "Disjunction_Inline",
			archive: `-- a.cue --
d: ({a: b: 3} | {a: b: 4}) & {c: 5}
o: d.a.b
p: d.c
`,
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"d", "o", "p"}},
				or(2, 4):   {f: []string{"a", "c"}, e: []string{"d", "o", "p"}},
				or1(4):     {f: []string{"a"}, e: []string{"d", "o", "p"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {f: []string{"b"}, e: []string{"a", "d", "o", "p"}},
				or(8, 10):  {f: []string{"b"}},
				or1(10):    {e: []string{"a", "b", "d", "o", "p"}},
				or1(12):    {e: []string{"a", "b", "d", "o", "p"}},
				or(14, 16): {e: []string{"d", "o", "p"}},
				or1(16):    {f: []string{"a"}, e: []string{"d", "o", "p"}},
				or(17, 19): {f: []string{"a"}},
				or1(19):    {f: []string{"b"}, e: []string{"a", "d", "o", "p"}},
				or(20, 22): {f: []string{"b"}},
				or1(22):    {e: []string{"a", "b", "d", "o", "p"}},
				or1(24):    {e: []string{"a", "b", "d", "o", "p"}},
				or(26, 29): {e: []string{"d", "o", "p"}},
				or(29, 32): {f: []string{"a", "c"}, e: []string{"d", "o", "p"}},
				or1(32):    {e: []string{"c", "d", "o", "p"}},
				or1(34):    {e: []string{"c", "d", "o", "p"}},
				or1(35):    {e: []string{"d", "o", "p"}},
				or(36, 38): {f: []string{"d", "o", "p"}},
				or(38, 41): {e: []string{"d", "o", "p"}},
				or(41, 43): {e: []string{"a", "c"}},
				or(43, 45): {e: []string{"b"}},
				or(45, 47): {f: []string{"d", "o", "p"}},
				or(47, 50): {e: []string{"d", "o", "p"}},
				or(50, 52): {e: []string{"a", "c"}},
			},
		},

		{
			name: "Disjunction_Chained",
			archive: `-- a.cue --
d1: {a: 1} | {a: 2}
d2: {a: 3} | {a: 4}
o: (d1 & d2).a
`,
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 3):   {f: []string{"d1", "d2", "o"}},
				or(3, 5):   {f: []string{"a"}, e: []string{"d1", "d2", "o"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {e: []string{"a", "d1", "d2", "o"}},
				or1(9):     {e: []string{"a", "d1", "d2", "o"}},
				or(11, 13): {e: []string{"d1", "d2", "o"}},
				or1(13):    {f: []string{"a"}, e: []string{"d1", "d2", "o"}},
				or(14, 16): {f: []string{"a"}},
				or1(16):    {e: []string{"a", "d1", "d2", "o"}},
				or1(18):    {e: []string{"a", "d1", "d2", "o"}},
				or(20, 23): {f: []string{"d1", "d2", "o"}},
				or(23, 25): {f: []string{"a"}, e: []string{"d1", "d2", "o"}},
				or(25, 27): {f: []string{"a"}},
				or1(27):    {e: []string{"a", "d1", "d2", "o"}},
				or1(29):    {e: []string{"a", "d1", "d2", "o"}},
				or(31, 33): {e: []string{"d1", "d2", "o"}},
				or1(33):    {f: []string{"a"}, e: []string{"d1", "d2", "o"}},
				or(34, 36): {f: []string{"a"}},
				or1(36):    {e: []string{"a", "d1", "d2", "o"}},
				or1(38):    {e: []string{"a", "d1", "d2", "o"}},
				or(40, 42): {f: []string{"d1", "d2", "o"}},
				or1(42):    {e: []string{"d1", "d2", "o"}},
				or(43, 47): {f: []string{"a"}, e: []string{"d1", "d2", "o"}},
				or(47, 49): {e: []string{"d1", "d2", "o"}},
				or(49, 52): {f: []string{"a"}, e: []string{"d1", "d2", "o"}},
				or1(52):    {e: []string{"d1", "d2", "o"}},
				or(53, 55): {e: []string{"a"}},
			},
		},

		{
			name: "Disjunction_Selected",
			archive: `-- a.cue --
d: {x: 17} | string
r: d & {x: int}
out: r.x
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "r"): {ln(2, 1, "r")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(2, 1, "x")},

				ln(1, 1, "d"): {self},
				ln(1, 1, "x"): {self}, // note non-symmetric!

				ln(2, 1, "r"): {self},
				ln(2, 1, "x"): {self, ln(1, 1, "x")},

				ln(3, 1, "out"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"d", "out", "r"}},
				or(2, 4):   {f: []string{"x"}, e: []string{"d", "out", "r"}},
				or(4, 6):   {f: []string{"x"}},
				or1(6):     {e: []string{"d", "out", "r", "x"}},
				or1(9):     {e: []string{"d", "out", "r", "x"}},
				or(11, 20): {e: []string{"d", "out", "r"}},
				or(20, 22): {f: []string{"d", "out", "r"}},
				or(22, 25): {f: []string{"x"}, e: []string{"d", "out", "r"}},
				or(25, 27): {e: []string{"d", "out", "r"}},
				or(27, 30): {f: []string{"x"}, e: []string{"d", "out", "r"}},
				or(30, 35): {e: []string{"d", "out", "r", "x"}},
				or1(35):    {e: []string{"d", "out", "r"}},
				or(36, 40): {f: []string{"d", "out", "r"}},
				or(40, 43): {e: []string{"d", "out", "r"}},
				or(43, 45): {e: []string{"x"}},
			},
		},

		{
			name: "Disjunction_Scopes",
			archive: `-- a.cue --
c: {a: b} | {b: 3}
b: 7
d: c.b
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b", "c", "d"}},
				or1(2):     {f: []string{"a", "b"}, e: []string{"b", "c", "d"}},
				or1(3):     {f: []string{"a"}, e: []string{"b", "c", "d"}},
				or(4, 6):   {f: []string{"a"}},
				or(6, 9):   {e: []string{"a", "b", "c", "d"}},
				or(10, 12): {e: []string{"b", "c", "d"}},
				or1(12):    {f: []string{"b"}, e: []string{"b", "c", "d"}},
				or(13, 15): {f: []string{"b"}},
				or1(15):    {e: []string{"b", "c", "d"}},
				or1(17):    {e: []string{"b", "c", "d"}},
				or(19, 21): {f: []string{"b", "c", "d"}},
				or1(21):    {e: []string{"b", "c", "d"}},
				or1(23):    {e: []string{"b", "c", "d"}},
				or(24, 26): {f: []string{"b", "c", "d"}},
				or(26, 29): {e: []string{"b", "c", "d"}},
				or(29, 31): {e: []string{"a", "b"}},
			},
		},

		{
			name: "Disjunction_Looping",
			archive: `-- a.cue --
a: ({b: c.d, d: 3} & {}) | {x: _} | {y: _} | {z: _} | {d: 4}
c: a
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "c"): {ln(2, 1, "c")},
				ln(1, 1, "d"): {ln(1, 2, "d"), ln(1, 3, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 2, "d"): {self},
				ln(1, 1, "x"): {self},
				ln(1, 1, "y"): {self},
				ln(1, 1, "z"): {self},
				ln(1, 3, "d"): {self},

				ln(2, 1, "c"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "c"}},
				or1(2):     {f: []string{"b", "d", "x", "y", "z"}, e: []string{"a", "c"}},
				or(3, 5):   {f: []string{"b", "d"}, e: []string{"a", "c"}},
				or(5, 7):   {f: []string{"b", "d"}},
				or(7, 10):  {e: []string{"a", "b", "c", "d"}},
				or(10, 12): {e: []string{"b", "d", "x", "y", "z"}},
				or1(12):    {f: []string{"b", "d"}, e: []string{"a", "c"}},
				or(13, 15): {f: []string{"b", "d"}},
				or1(15):    {e: []string{"a", "b", "c", "d"}},
				or1(17):    {e: []string{"a", "b", "c", "d"}},
				or(21, 23): {f: []string{"b", "d"}, e: []string{"a", "c"}},
				or(24, 27): {e: []string{"a", "c"}},
				or1(27):    {f: []string{"x"}, e: []string{"a", "c"}},
				or(28, 30): {f: []string{"x"}},
				or(30, 33): {e: []string{"a", "c", "x"}},
				or(34, 36): {e: []string{"a", "c"}},
				or1(36):    {f: []string{"y"}, e: []string{"a", "c"}},
				or(37, 39): {f: []string{"y"}},
				or(39, 42): {e: []string{"a", "c", "y"}},
				or(43, 45): {e: []string{"a", "c"}},
				or1(45):    {f: []string{"z"}, e: []string{"a", "c"}},
				or(46, 48): {f: []string{"z"}},
				or(48, 51): {e: []string{"a", "c", "z"}},
				or(52, 54): {e: []string{"a", "c"}},
				or1(54):    {f: []string{"d"}, e: []string{"a", "c"}},
				or(55, 57): {f: []string{"d"}},
				or1(57):    {e: []string{"a", "c", "d"}},
				or1(59):    {e: []string{"a", "c", "d"}},
				or(61, 63): {f: []string{"a", "c"}},
				or(63, 66): {f: []string{"b", "d", "x", "y", "z"}, e: []string{"a", "c"}},
			},
		},

		{
			name: "Conjunction_Scopes",
			archive: `-- a.cue --
c: {a: b} & {b: 3}
b: 7
d: c.b
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 2, "b"): {self},

				ln(2, 1, "b"): {self},
				ln(3, 1, "d"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b", "c", "d"}},
				or(2, 4):   {f: []string{"a", "b"}, e: []string{"b", "c", "d"}},
				or(4, 6):   {f: []string{"a", "b"}},
				or(6, 9):   {e: []string{"a", "b", "c", "d"}},
				or1(12):    {f: []string{"a", "b"}, e: []string{"b", "c", "d"}},
				or(13, 15): {f: []string{"a", "b"}},
				or1(15):    {e: []string{"b", "c", "d"}},
				or1(17):    {e: []string{"b", "c", "d"}},
				or(19, 21): {f: []string{"b", "c", "d"}},
				or1(21):    {e: []string{"b", "c", "d"}},
				or1(23):    {e: []string{"b", "c", "d"}},
				or(24, 26): {f: []string{"b", "c", "d"}},
				or(26, 29): {e: []string{"b", "c", "d"}},
				or(29, 31): {e: []string{"a", "b"}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or(2, 6):   {f: []string{"a", "b"}, e: []string{"x", "y"}},
				or1(6):     {f: []string{"a", "b"}, e: []string{"a", "x", "y"}},
				or(7, 9):   {f: []string{"a", "b"}},
				or(9, 14):  {e: []string{"a", "b", "x", "y"}},
				or1(14):    {f: []string{"a", "b"}, e: []string{"a", "x", "y"}},
				or(15, 17): {f: []string{"a", "b"}},
				or(17, 20): {e: []string{"a", "b", "x", "y"}},
				or1(21):    {f: []string{"a", "b"}, e: []string{"x", "y"}},
				or(22, 24): {f: []string{"a", "b"}},
				or1(24):    {e: []string{"a", "x", "y"}},
				or1(26):    {e: []string{"a", "x", "y"}},
				or1(27):    {f: []string{"a", "b"}, e: []string{"x", "y"}},
				or(31, 34): {f: []string{"a", "b"}, e: []string{"x", "y"}},
				or(34, 36): {f: []string{"a", "b"}},
				or(36, 38): {e: []string{"a", "x", "y"}},
				or1(39):    {e: []string{"a", "x", "y"}},
				or1(40):    {f: []string{"a", "b"}, e: []string{"x", "y"}},
				or(42, 44): {f: []string{"x", "y"}},
				or(44, 47): {e: []string{"x", "y"}},
				or(47, 49): {e: []string{"a", "b"}},
			},
		},

		{
			name: "Conjunction_EvenMoreScopes",
			archive: `-- a.cue --
c: {a: b, b: x: 3} & {b: x: 3, z: b.x}
b: 7
d: c.b.x`,
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b", "c", "d"}},
				or(2, 4):   {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(4, 6):   {f: []string{"a", "b", "z"}},
				or(6, 9):   {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				or1(9):     {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(10, 12): {f: []string{"a", "b", "z"}},
				or1(12):    {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				or(13, 15): {f: []string{"x"}},
				or1(15):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(17):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(21):    {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(22, 24): {f: []string{"a", "b", "z"}},
				or1(24):    {f: []string{"x"}, e: []string{"b", "c", "d", "z"}},
				or(25, 27): {f: []string{"x"}},
				or1(27):    {e: []string{"b", "c", "d", "x", "z"}},
				or1(29):    {e: []string{"b", "c", "d", "x", "z"}},
				or1(30):    {f: []string{"a", "b", "z"}, e: []string{"b", "c", "d"}},
				or(31, 33): {f: []string{"a", "b", "z"}},
				or(33, 36): {e: []string{"b", "c", "d", "z"}},
				or(36, 38): {e: []string{"x"}},
				or(39, 41): {f: []string{"b", "c", "d"}},
				or1(41):    {e: []string{"b", "c", "d"}},
				or1(43):    {e: []string{"b", "c", "d"}},
				or(44, 46): {f: []string{"b", "c", "d"}},
				or(46, 49): {e: []string{"b", "c", "d"}},
				or(49, 51): {e: []string{"a", "b", "z"}},
				or(51, 53): {e: []string{"x"}},
			},
		},

		{
			name: "Conjunction_Selector",
			archive: `-- a.cue --
b: ({a: 6} & {a: int}).a
`,
			expectDefinitions: map[position][]position{
				ln(1, 3, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},

				ln(1, 1, "b"): {self},
				ln(1, 1, "a"): {self, ln(1, 2, "a")},
				ln(1, 2, "a"): {self, ln(1, 1, "a")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"b"}},
				or1(2):     {e: []string{"b"}},
				or(3, 5):   {f: []string{"a"}, e: []string{"b"}},
				or(5, 7):   {f: []string{"a"}},
				or1(7):     {e: []string{"a", "b"}},
				or1(9):     {e: []string{"a", "b"}},
				or1(13):    {f: []string{"a"}, e: []string{"b"}},
				or(14, 16): {f: []string{"a"}},
				or(16, 21): {e: []string{"a", "b"}},
				or1(22):    {e: []string{"b"}},
				or(23, 25): {e: []string{"a"}},
			},
		},

		{
			name: "Binary_Expr",
			archive: `-- a.cue --
c: ({a: 6, d: a} + {b: a}).g
a: 12
`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 3, "a"): {ln(2, 1, "a")},

				ln(1, 1, "c"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "d"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "a"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "c"}},
				or(2, 4):   {e: []string{"a", "c"}},
				or1(4):     {f: []string{"a", "d"}, e: []string{"a", "c"}},
				or(5, 7):   {f: []string{"a", "d"}},
				or1(7):     {e: []string{"a", "c", "d"}},
				or1(9):     {e: []string{"a", "c", "d"}},
				or1(10):    {f: []string{"a", "d"}, e: []string{"a", "c"}},
				or(11, 13): {f: []string{"a", "d"}},
				or(13, 16): {e: []string{"a", "c", "d"}},
				or(17, 19): {e: []string{"a", "c"}},
				or1(19):    {f: []string{"b"}, e: []string{"a", "c"}},
				or(20, 22): {f: []string{"b"}},
				or(22, 25): {e: []string{"a", "b", "c"}},
				or1(26):    {e: []string{"a", "c"}},
				or(29, 31): {f: []string{"a", "c"}},
				or1(31):    {e: []string{"a", "c"}},
				or1(34):    {e: []string{"a", "c"}},
			},
		},

		{
			name: "Import_Builtin_Call",
			archive: `-- a.cue --
import "magic"

x: magic.Merlin(y)
y: "wand"
`,
			expectDefinitions: map[position][]position{
				ln(3, 1, "magic"):  {ln(1, 1, `"magic"`)},
				ln(3, 1, "Merlin"): {},
				ln(3, 1, "y"):      {ln(4, 1, "y")},

				ln(3, 1, "x"): {self},
				ln(4, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 7):   {f: []string{"x", "y"}},
				or(7, 15):  {e: []string{"magic", "x", "y"}},
				or(15, 18): {f: []string{"x", "y"}},
				or(18, 25): {e: []string{"magic", "x", "y"}},
				or(32, 35): {e: []string{"magic", "x", "y"}},
				or(35, 37): {f: []string{"x", "y"}},
				or1(37):    {e: []string{"magic", "x", "y"}},
				or1(44):    {e: []string{"magic", "x", "y"}},
			},
			expectUsagesExtra: map[position]map[bool][]position{
				ln(1, 1, `"magic"`): {true: []position{self}},
			},
		},

		{
			name: "Import_alias",
			archive: `-- a.cue --
import wand "magic"

x: wand.foo
`,
			expectDefinitions: map[position][]position{
				ln(3, 1, "wand"): {ln(1, 1, "wand")},
				ln(3, 1, "foo"):  {},

				ln(3, 1, "x"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 7):   {f: []string{"x"}},
				or(7, 20):  {e: []string{"wand", "x"}},
				or(20, 23): {f: []string{"x"}},
				or(23, 29): {e: []string{"wand", "x"}},
			},
			expectUsagesExtra: map[position]map[bool][]position{
				ln(1, 1, "wand"): {true: []position{self}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "b", "c", "d"}},
				or1(2):     {e: []string{"a", "b", "c", "d"}},
				or1(4):     {e: []string{"a", "b", "c", "d"}},
				or(5, 7):   {f: []string{"a", "b", "c", "d"}},
				or(7, 10):  {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				or(10, 12): {f: []string{"a", "b", "c", "d"}},
				or1(12):    {f: []string{"x"}, e: []string{"a", "b", "c", "d"}},
				or(13, 15): {f: []string{"x"}},
				or1(15):    {e: []string{"a", "b", "c", "d", "x"}},
				or1(20):    {e: []string{"a", "b", "c", "d", "x"}},
				or(21, 23): {f: []string{"a", "b", "c", "d"}},
				or1(23):    {e: []string{"a", "b", "c", "d"}},
				or(29, 31): {e: []string{"a", "b", "c", "d"}},
				or(38, 40): {e: []string{"a", "b", "c", "d"}},
				or(40, 42): {e: []string{"x"}},
				or1(43):    {e: []string{"a", "b", "c", "d"}},
			},
		},

		{
			name: "Interpolation_Field",
			archive: `-- a.cue --
a: 5
"five\(a)": hello
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "a"): {ln(1, 1, "a")},

				ln(1, 1, "a"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {e: []string{"a"}},
				or1(4):     {e: []string{"a"}},
				or1(5):     {f: []string{"a"}},
				or(12, 14): {e: []string{"a"}},
				or(16, 23): {e: []string{"a"}},
			},
		},

		{
			name: "Interpolation_Expr",
			archive: `-- a.cue --
y: "\({a: 3, b: a}.b) \(a)"
a: 12
`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
				ln(1, 3, "a"): {ln(2, 1, "a")},

				ln(1, 1, "y"): {self},
				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(2, 1, "a"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "y"}},
				or1(2):     {e: []string{"a", "y"}},
				or1(6):     {f: []string{"a", "b"}, e: []string{"a", "y"}},
				or(7, 9):   {f: []string{"a", "b"}},
				or1(9):     {e: []string{"a", "b", "y"}},
				or1(11):    {e: []string{"a", "b", "y"}},
				or1(12):    {f: []string{"a", "b"}, e: []string{"a", "y"}},
				or(13, 15): {f: []string{"a", "b"}},
				or(15, 18): {e: []string{"a", "b", "y"}},
				or(19, 21): {e: []string{"a", "b"}},
				or(24, 26): {e: []string{"a", "y"}},
				or1(27):    {e: []string{"a", "y"}},
				or(28, 30): {f: []string{"a", "y"}},
				or1(30):    {e: []string{"a", "y"}},
				or1(33):    {e: []string{"a", "y"}},
			},
		},

		{
			name: "MultiByte_Expression",
			archive: `-- a.cue --
x: "💩" + y
y: "sticks"
`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "y"): {ln(2, 1, "y")},

				ln(1, 1, "x"): {self},
				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or1(2):     {e: []string{"x", "y"}},
				or(9, 14):  {e: []string{"x", "y"}},
				or(14, 16): {f: []string{"x", "y"}},
				or1(16):    {e: []string{"x", "y"}},
				or1(25):    {e: []string{"x", "y"}},
			},
		},

		{
			name: "MultiByte_Index",
			archive: `-- a.cue --
x: {"💩": "sticks"}
y: x["💩"]
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"):    {ln(1, 1, "x")},
				ln(2, 1, `"💩"]`): {ln(1, 1, `"💩"`)},

				ln(1, 1, "x"):   {self},
				ln(1, 1, `"💩"`): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or(2, 4):   {f: []string{"💩"}, e: []string{"x", "y"}},
				or1(4):     {f: []string{"💩"}},
				or(5, 11):  {e: []string{"💩"}},
				or1(11):    {e: []string{"x", "y", "💩"}},
				or1(20):    {e: []string{"x", "y", "💩"}},
				or(22, 24): {f: []string{"x", "y"}},
				or(24, 27): {e: []string{"x", "y"}},
				or(27, 35): {e: []string{"💩"}},
			},
		},

		{
			name: "MultiByte_Selector",
			archive: `-- a.cue --
x: {"💩": "sticks"}
y: x."💩"
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "x"):   {ln(1, 1, "x")},
				ln(2, 1, `"💩"`): {ln(1, 1, `"💩"`)},

				ln(1, 1, "x"):   {self},
				ln(1, 1, `"💩"`): {self},

				ln(2, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y"}},
				or(2, 4):   {f: []string{"💩"}, e: []string{"x", "y"}},
				or1(4):     {f: []string{"💩"}},
				or(5, 11):  {e: []string{"💩"}},
				or1(11):    {e: []string{"x", "y", "💩"}},
				or1(20):    {e: []string{"x", "y", "💩"}},
				or(22, 24): {f: []string{"x", "y"}},
				or(24, 27): {e: []string{"x", "y"}},
				or(27, 34): {e: []string{"💩"}},
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
			expectDefinitions: map[position][]position{
				ln(4, 1, "a"): {ln(1, 1, "a")},
				ln(5, 1, "b"): {ln(2, 1, "b")},
				ln(7, 1, "l"): {ln(3, 1, "x")},
				ln(7, 1, "c"): {ln(5, 1, "c")},
				ln(9, 1, "x"): {ln(3, 1, "x")},
				ln(9, 1, "c"): {ln(5, 1, "c")},

				ln(1, 1, "a"): {self},
				ln(2, 1, "b"): {self},
				ln(3, 1, "l"): {ln(3, 1, "x")},
				ln(3, 1, "x"): {self},
				ln(5, 1, "c"): {self},
				ln(7, 1, "z"): {self},
				ln(9, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "b", "x", "y"}},
				or1(2):     {e: []string{"a", "b", "l", "x", "y"}},
				or1(5):     {e: []string{"a", "b", "l", "x", "y"}},
				or(6, 8):   {f: []string{"a", "b", "x", "y"}},
				or1(8):     {e: []string{"a", "b", "l", "x", "y"}},
				or1(10):    {e: []string{"a", "b", "l", "x", "y"}},
				or1(11):    {f: []string{"a", "b", "x", "y"}},
				or1(12):    {e: []string{"a", "b", "l", "x", "y"}},
				or(13, 15): {f: []string{"a", "b", "x", "y"}},
				or(15, 22): {f: []string{"c", "z"}, e: []string{"a", "b", "l", "x", "y"}},
				or(22, 26): {e: []string{"a", "b", "l", "x", "y", "z"}},
				or1(28):    {e: []string{"a", "b", "l", "x", "y", "z"}},
				or(29, 33): {f: []string{"c", "z"}, e: []string{"a", "b", "l", "x", "y", "z"}},
				or(33, 35): {f: []string{"c", "z"}},
				or(35, 38): {e: []string{"a", "b", "c", "l", "x", "y", "z"}},
				or(38, 40): {f: []string{"c", "z"}, e: []string{"a", "b", "l", "x", "y", "z"}},
				or1(41):    {f: []string{"c", "z"}, e: []string{"a", "b", "l", "x", "y"}},
				or(42, 44): {f: []string{"c", "z"}},
				or(44, 47): {e: []string{"a", "b", "l", "x", "y", "z"}},
				or(47, 49): {e: []string{"c", "z"}},
				or1(49):    {f: []string{"c", "z"}, e: []string{"a", "b", "l", "x", "y"}},
				or(51, 53): {f: []string{"a", "b", "x", "y"}},
				or(53, 56): {e: []string{"a", "b", "l", "x", "y"}},
				or(56, 58): {e: []string{"c", "z"}},
			},
		},

		{
			name: "Comprehension_Let",
			archive: `-- a.cue --
a: b: c: 17
let x=a.b
y: x.c
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "a"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
				ln(3, 1, "x"): {ln(2, 1, "x")},
				ln(3, 1, "c"): {ln(1, 1, "c")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},
				ln(1, 1, "c"): {self},

				ln(2, 1, "x"): {self},
				ln(3, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "y"}},
				or1(2):     {f: []string{"b"}, e: []string{"a", "x", "y"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {f: []string{"c"}, e: []string{"a", "b", "x", "y"}},
				or(6, 8):   {f: []string{"c"}},
				or1(8):     {e: []string{"a", "b", "c", "x", "y"}},
				or1(11):    {e: []string{"a", "b", "c", "x", "y"}},
				or(12, 18): {f: []string{"a", "y"}},
				or(18, 20): {e: []string{"a", "x", "y"}},
				or(20, 22): {e: []string{"b"}},
				or(22, 24): {f: []string{"a", "y"}},
				or(24, 27): {e: []string{"a", "x", "y"}},
				or(27, 29): {e: []string{"c"}},
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
			expectDefinitions: map[position][]position{
				ln(3, 1, "b"): {ln(2, 1, "b")},
				ln(5, 1, "b"): {},
				ln(6, 1, "a"): {ln(1, 1, "a"), ln(5, 1, "a")},
				ln(6, 1, "b"): {},

				ln(1, 1, "a"): {self, ln(5, 1, "a")},
				ln(2, 1, "b"): {self},
				ln(3, 1, "c"): {self},

				ln(5, 1, "a"): {self, ln(1, 1, "a")},
				ln(5, 1, "d"): {self},

				ln(6, 1, "o"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a", "o"}},
				or(2, 10):  {f: []string{"c", "d"}, e: []string{"a", "o"}},
				or(10, 12): {f: []string{"c", "d"}},
				or1(14):    {e: []string{"a", "b", "c", "o"}},
				or1(15):    {f: []string{"c", "d"}, e: []string{"a", "o"}},
				or(16, 18): {f: []string{"c", "d"}},
				or(18, 21): {e: []string{"a", "b", "c", "o"}},
				or1(21):    {f: []string{"c", "d"}, e: []string{"a", "o"}},
				or(23, 25): {f: []string{"a", "o"}},
				or1(25):    {f: []string{"c", "d"}, e: []string{"a", "o"}},
				or(26, 28): {f: []string{"c", "d"}},
				or(28, 31): {e: []string{"a", "d", "o"}},
				or(31, 33): {f: []string{"a", "o"}},
				or(33, 36): {e: []string{"a", "o"}},
				or(36, 38): {e: []string{"c", "d"}},
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
			expectDefinitions: map[position][]position{
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

				ln(3, 1, "o"): {self},

				ln(4, 1, "k"): {self},
				ln(4, 1, "v"): {self},

				ln(6, 1, "p"):  {self},
				ln(9, 1, "q"):  {self},
				ln(10, 1, "r"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):     {f: []string{"a", "b", "o", "q", "r"}},
				or(2, 5):     {f: []string{"x", "y", "z"}, e: []string{"a", "b", "o", "q", "r"}},
				or(5, 7):     {f: []string{"x", "y", "z"}},
				or1(7):       {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(9):       {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(10):      {f: []string{"x", "y", "z"}, e: []string{"a", "b", "o", "q", "r"}},
				or(11, 13):   {f: []string{"x", "y", "z"}},
				or1(13):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(15):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(16):      {f: []string{"x", "y", "z"}, e: []string{"a", "b", "o", "q", "r"}},
				or(17, 19):   {f: []string{"x", "y", "z"}},
				or1(19):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(21):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or(23, 25):   {f: []string{"a", "b", "o", "q", "r"}},
				or(25, 28):   {f: []string{"x", "y", "z"}, e: []string{"a", "b", "o", "q", "r"}},
				or(28, 30):   {f: []string{"x", "y", "z"}},
				or1(30):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(32):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(33):      {f: []string{"x", "y", "z"}, e: []string{"a", "b", "o", "q", "r"}},
				or(34, 36):   {f: []string{"x", "y", "z"}},
				or1(36):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(38):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(39):      {f: []string{"x", "y", "z"}, e: []string{"a", "b", "o", "q", "r"}},
				or(40, 42):   {f: []string{"x", "y", "z"}},
				or1(42):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or1(44):      {e: []string{"a", "b", "o", "q", "r", "x", "y", "z"}},
				or(46, 48):   {f: []string{"a", "b", "o", "q", "r"}},
				or(48, 56):   {f: []string{"p"}, e: []string{"a", "b", "o", "q", "r"}},
				or1(58):      {e: []string{"a", "b", "o", "q", "r"}},
				or(61, 64):   {e: []string{"a", "b", "k", "o", "q", "r"}},
				or(64, 66):   {f: []string{"x", "y", "z"}, e: []string{"a", "b", "k", "o", "q", "r"}},
				or(66, 70):   {f: []string{"p"}, e: []string{"a", "b", "k", "o", "q", "r", "v"}},
				or1(70):      {f: []string{"p"}},
				or(71, 73):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				or(74, 84):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				or(84, 86):   {f: []string{"p"}, e: []string{"a", "b", "k", "o", "q", "r", "v"}},
				or(86, 88):   {f: []string{"p"}},
				or(88, 91):   {e: []string{"a", "b", "k", "o", "p", "q", "r", "v"}},
				or(91, 93):   {f: []string{"p"}, e: []string{"a", "b", "k", "o", "q", "r", "v"}},
				or1(94):      {f: []string{"p"}, e: []string{"a", "b", "o", "q", "r"}},
				or(96, 98):   {f: []string{"a", "b", "o", "q", "r"}},
				or(98, 101):  {e: []string{"a", "b", "o", "q", "r"}},
				or(101, 103): {e: []string{"p"}},
				or(103, 105): {f: []string{"a", "b", "o", "q", "r"}},
				or(105, 108): {e: []string{"a", "b", "o", "q", "r"}},
				or(108, 110): {e: []string{"p"}},
			},
		},

		{
			name: "Comprehension_For_ForwardsReference",
			archive: `-- a.cue --
for a, b in foo.bar {}
foo: bar: "baz"`,
			expectDefinitions: map[position][]position{
				ln(1, 1, "foo"): {ln(2, 1, "foo")},
				ln(1, 1, "bar"): {ln(2, 1, "bar")},

				ln(1, 1, "a"): {self},
				ln(1, 1, "b"): {self},

				ln(2, 1, "foo"): {self},
				ln(2, 1, "bar"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 4):   {f: []string{"foo"}},
				or1(6):     {e: []string{"foo"}},
				or(9, 16):  {e: []string{"a", "foo"}},
				or(16, 20): {e: []string{"a", "bar", "foo"}},
				or(20, 22): {f: []string{"foo"}, e: []string{"a", "b", "foo"}},
				or(23, 27): {f: []string{"foo"}},
				or1(27):    {f: []string{"bar"}, e: []string{"foo"}},
				or(28, 32): {f: []string{"bar"}},
				or1(32):    {e: []string{"bar", "foo"}},
				or1(38):    {e: []string{"bar", "foo"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 2, "k"): {ln(4, 1, "k")},
				ln(2, 3, "k"): {ln(2, 1, "k")},

				ln(1, 1, "x"): {self},

				ln(2, 1, "k"): {self},
				ln(2, 1, "v"): {self},
				ln(2, 2, "v"): {self},

				ln(4, 1, "k"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"k", "x"}},
				or(2, 10):  {f: []string{"v"}, e: []string{"k", "x"}},
				or1(12):    {e: []string{"k", "x"}},
				or(15, 20): {e: []string{"k", "x"}},
				or1(20):    {f: []string{"v"}, e: []string{"k", "v", "x"}},
				or(21, 23): {f: []string{"v"}},
				or(23, 26): {e: []string{"k", "v", "x"}},
				or1(27):    {f: []string{"v"}, e: []string{"k", "x"}},
				or(29, 31): {f: []string{"k", "x"}},
				or(31, 34): {e: []string{"k", "x"}},
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
			expectDefinitions: map[position][]position{
				ln(2, 2, "x"): {ln(1, 1, "x")},
				ln(3, 2, "x"): {ln(2, 1, "x")},
				ln(4, 1, "x"): {ln(3, 1, "x")},
				ln(6, 1, "g"): {ln(1, 1, "g")},

				ln(1, 1, "g"): {self},
				ln(1, 1, "x"): {self},

				ln(2, 1, "x"): {self},
				ln(3, 1, "x"): {self},
				ln(4, 1, "h"): {self},

				ln(6, 1, "i"):  {self},
				ln(6, 1, "0]"): {ln(1, 1, "for")},
				ln(6, 1, "h"):  {ln(4, 1, "h")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"g", "i"}},
				or(2, 4):   {e: []string{"g", "i"}},
				or1(4):     {f: []string{"h"}, e: []string{"g", "i"}},
				or(5, 8):   {e: []string{"g", "i"}},
				or(10, 14): {e: []string{"g", "i"}},
				or(15, 17): {e: []string{"g", "i"}},
				or(18, 20): {e: []string{"g", "i"}},
				or(20, 24): {e: []string{"g", "i", "x"}},
				or(26, 30): {e: []string{"g", "i", "x"}},
				or(31, 36): {e: []string{"g", "i", "x"}},
				or(38, 42): {e: []string{"g", "i", "x"}},
				or1(43):    {e: []string{"g", "i", "x"}},
				or(44, 51): {f: []string{"h"}, e: []string{"g", "i", "x"}},
				or(51, 53): {f: []string{"h"}},
				or(53, 56): {e: []string{"g", "h", "i", "x"}},
				or1(57):    {f: []string{"h"}, e: []string{"g", "i", "x"}},
				or1(59):    {e: []string{"g", "i"}},
				or(60, 62): {f: []string{"g", "i"}},
				or(62, 65): {e: []string{"g", "i"}},
				or(68, 70): {e: []string{"h"}},
			},
		},

		{
			name: "Definitions",
			archive: `-- a.cue --
#x: y: #z: 3
o: #x & #x.y.z
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "#x"): {ln(1, 1, "#x")},
				ln(2, 2, "#x"): {ln(1, 1, "#x")},
				ln(2, 1, "y"):  {ln(1, 1, "y")},

				ln(1, 1, "#x"): {self},
				ln(1, 1, "y"):  {self},
				ln(1, 1, "#z"): {self},

				ln(2, 1, "o"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 3):   {f: []string{"#x", "o"}},
				or1(3):     {f: []string{"y"}, e: []string{"#x", "o"}},
				or(4, 6):   {f: []string{"y"}},
				or1(6):     {f: []string{"#z"}, e: []string{"#x", "o", "y"}},
				or(7, 10):  {f: []string{"#z"}},
				or1(10):    {e: []string{"#x", "#z", "o", "y"}},
				or1(12):    {e: []string{"#x", "#z", "o", "y"}},
				or(13, 15): {f: []string{"#x", "o"}},
				or(15, 19): {f: []string{"y"}, e: []string{"#x", "o"}},
				or(19, 24): {e: []string{"#x", "o"}},
				or(24, 26): {e: []string{"#x", "o", "y"}},
				or(26, 28): {e: []string{"#x", "#z", "o"}},
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
			expectDefinitions: map[position][]position{
				fln("b.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo")},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "foo"): {self},
				fln("b.cue", 3, 1, "bar"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 15): {f: []string{"bar", "foo"}},
				orf("a.cue", 15, 16): {e: []string{"foo"}},
				orf("a.cue", 20, 21): {e: []string{"foo"}},

				orf("b.cue", 10, 15): {f: []string{"bar", "foo"}},
				orf("b.cue", 15, 20): {e: []string{"bar"}},
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
			expectDefinitions: map[position][]position{
				fln("c.cue", 3, 1, "foo"): {fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},
				fln("c.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("b.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "foo"): {self, fln("b.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},
				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("c.cue", 4, 1, "foo")},

				fln("c.cue", 3, 1, "bar"): {self},
				fln("c.cue", 4, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 15): {f: []string{"bar", "foo"}},
				orf("a.cue", 15, 16): {e: []string{"foo"}},
				orf("a.cue", 20, 21): {e: []string{"foo"}},

				orf("b.cue", 10, 15): {f: []string{"bar", "foo"}},
				orf("b.cue", 15, 16): {e: []string{"foo"}},
				orf("b.cue", 21, 22): {e: []string{"foo"}},

				orf("c.cue", 10, 15): {f: []string{"bar", "foo"}},
				orf("c.cue", 15, 20): {e: []string{"bar", "foo"}},
				orf("c.cue", 20, 24): {f: []string{"bar", "foo"}},
				orf("c.cue", 24, 27): {e: []string{"bar", "foo"}},
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
			expectDefinitions: map[position][]position{
				fln("a.cue", 3, 2, "bar"): {fln("a.cue", 3, 1, "bar")},
				fln("b.cue", 3, 1, "bar"): {},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "foo"): {self, fln("b.cue", 3, 1, "foo")},
				fln("a.cue", 3, 1, "bar"): {self},
				fln("a.cue", 3, 1, "baz"): {self},

				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo")},
				fln("b.cue", 3, 1, "qux"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 15): {f: []string{"foo"}},
				orf("a.cue", 15, 17): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("a.cue", 17, 21): {f: []string{"bar", "baz", "qux"}},
				orf("a.cue", 21, 22): {e: []string{"bar", "baz", "foo"}},
				orf("a.cue", 26, 27): {e: []string{"bar", "baz", "foo"}},
				orf("a.cue", 27, 28): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("a.cue", 28, 32): {f: []string{"bar", "baz", "qux"}},
				orf("a.cue", 32, 37): {e: []string{"bar", "baz", "foo"}},

				orf("b.cue", 10, 15): {f: []string{"foo"}},
				orf("b.cue", 15, 17): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("b.cue", 17, 21): {f: []string{"bar", "baz", "qux"}},
				orf("b.cue", 21, 26): {e: []string{"foo", "qux"}},
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
			expectDefinitions: map[position][]position{
				fln("a.cue", 4, 1, "bar"): {},
				fln("b.cue", 3, 1, "bar"): {},
				fln("c.cue", 3, 2, "foo"): {fln("a.cue", 3, 1, "foo"), fln("a.cue", 4, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("c.cue", 3, 1, "bar"): {fln("a.cue", 3, 1, "bar")},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},
				fln("c.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("b.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "foo"): {self, fln("a.cue", 4, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("a.cue", 3, 1, "bar"): {self},
				fln("a.cue", 4, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("b.cue", 3, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("a.cue", 4, 1, "baz"): {self},

				fln("b.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("a.cue", 4, 1, "foo"), fln("c.cue", 3, 1, "foo")},
				fln("b.cue", 3, 1, "qux"): {self, fln("c.cue", 3, 1, "qux")},

				fln("c.cue", 3, 1, "foo"): {self, fln("a.cue", 3, 1, "foo"), fln("a.cue", 4, 1, "foo"), fln("b.cue", 3, 1, "foo")},
				fln("c.cue", 3, 1, "qux"): {self, fln("b.cue", 3, 1, "qux")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 15): {f: []string{"foo"}},
				orf("a.cue", 15, 16): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("a.cue", 16, 20): {f: []string{"bar", "baz", "qux"}},
				orf("a.cue", 20, 21): {e: []string{"bar", "foo"}},
				orf("a.cue", 25, 26): {e: []string{"bar", "foo"}},
				orf("a.cue", 26, 30): {f: []string{"foo"}},
				orf("a.cue", 30, 31): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("a.cue", 31, 35): {f: []string{"bar", "baz", "qux"}},
				orf("a.cue", 35, 40): {e: []string{"baz", "foo"}},

				orf("b.cue", 10, 15): {f: []string{"foo"}},
				orf("b.cue", 15, 16): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("b.cue", 16, 20): {f: []string{"bar", "baz", "qux"}},
				orf("b.cue", 20, 25): {e: []string{"foo", "qux"}},

				orf("c.cue", 10, 15): {f: []string{"foo"}},
				orf("c.cue", 15, 16): {f: []string{"bar", "baz", "qux"}, e: []string{"foo"}},
				orf("c.cue", 16, 20): {f: []string{"bar", "baz", "qux"}},
				orf("c.cue", 20, 25): {e: []string{"foo", "qux"}},
				orf("c.cue", 25, 29): {e: []string{"bar", "baz", "qux"}},
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
			expectDefinitions: map[position][]position{
				fln("a.cue", 4, 1, "a"): {fln("a.cue", 3, 1, "a")},
				fln("b.cue", 3, 1, "a"): {},
				fln("c.cue", 4, 1, "a"): {fln("c.cue", 3, 1, "a")},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},
				fln("c.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("b.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "a"): {self},
				fln("a.cue", 4, 1, "q"): {self},
				fln("b.cue", 3, 1, "r"): {self},
				fln("c.cue", 3, 1, "a"): {self},
				fln("c.cue", 4, 1, "s"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 19): {f: []string{"q", "r", "s"}},
				orf("a.cue", 20, 21): {e: []string{"a", "q"}},
				orf("a.cue", 21, 23): {f: []string{"q", "r", "s"}},
				orf("a.cue", 23, 26): {e: []string{"a", "q"}},

				orf("b.cue", 10, 13): {f: []string{"q", "r", "s"}},
				orf("b.cue", 13, 16): {e: []string{"r"}},

				orf("c.cue", 10, 19): {f: []string{"q", "r", "s"}},
				orf("c.cue", 23, 24): {e: []string{"a", "s"}},
				orf("c.cue", 24, 26): {f: []string{"q", "r", "s"}},
				orf("c.cue", 26, 29): {e: []string{"a", "s"}},
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
			expectDefinitions: map[position][]position{
				fln("a.cue", 4, 1, "a"): {fln("a.cue", 5, 1, "a"), fln("b.cue", 3, 1, "a")},
				fln("a.cue", 4, 1, "b"): {fln("a.cue", 5, 1, "b"), fln("b.cue", 3, 1, "b")},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "w"): {self, fln("b.cue", 3, 1, "w")},
				fln("a.cue", 4, 1, "x"): {self},
				fln("a.cue", 4, 1, "y"): {self},
				fln("a.cue", 5, 1, "a"): {self, fln("b.cue", 3, 1, "a")},
				fln("a.cue", 5, 1, "b"): {self, fln("b.cue", 3, 1, "b")},

				fln("b.cue", 3, 1, "w"): {self, fln("a.cue", 3, 1, "w")},
				fln("b.cue", 3, 1, "a"): {self, fln("a.cue", 5, 1, "a")},
				fln("b.cue", 3, 1, "b"): {self, fln("a.cue", 5, 1, "b")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 13): {f: []string{"w"}},
				orf("a.cue", 13, 17): {f: []string{"a", "x"}, e: []string{"w"}},
				orf("a.cue", 17, 19): {f: []string{"a", "x"}},
				orf("a.cue", 19, 20): {f: []string{"y"}, e: []string{"a", "w", "x"}},
				orf("a.cue", 20, 22): {f: []string{"y"}},
				orf("a.cue", 22, 25): {e: []string{"a", "w", "x", "y"}},
				orf("a.cue", 25, 27): {e: []string{"b"}},
				orf("a.cue", 27, 28): {f: []string{"a", "x"}, e: []string{"w"}},
				orf("a.cue", 28, 30): {f: []string{"a", "x"}},
				orf("a.cue", 30, 31): {f: []string{"b"}, e: []string{"a", "w", "x"}},
				orf("a.cue", 31, 33): {f: []string{"b"}},
				orf("a.cue", 33, 34): {e: []string{"a", "b", "w", "x"}},
				orf("a.cue", 35, 36): {e: []string{"a", "b", "w", "x"}},
				orf("a.cue", 36, 37): {f: []string{"a", "x"}, e: []string{"w"}},

				orf("b.cue", 10, 13): {f: []string{"w"}},
				orf("b.cue", 13, 14): {f: []string{"a", "x"}, e: []string{"w"}},
				orf("b.cue", 14, 16): {f: []string{"a", "x"}},
				orf("b.cue", 16, 17): {f: []string{"b"}, e: []string{"a", "w"}},
				orf("b.cue", 17, 19): {f: []string{"b"}},
				orf("b.cue", 19, 20): {e: []string{"a", "b", "w"}},
				orf("b.cue", 21, 22): {e: []string{"a", "b", "w"}},
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
			expectDefinitions: map[position][]position{
				fln("a.cue", 5, 1, "X"): {fln("a.cue", 4, 1, "b"), fln("b.cue", 4, 1, "b")},
				fln("a.cue", 5, 1, "c"): {fln("a.cue", 4, 1, "c"), fln("b.cue", 4, 1, "c")},
				fln("c.cue", 4, 1, "X"): {},
				fln("c.cue", 4, 1, "c"): {},
				fln("d.cue", 5, 1, "X"): {fln("d.cue", 4, 1, "f")},

				fln("a.cue", 1, 1, "x"): {self, fln("b.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x"), fln("d.cue", 1, 1, "x")},
				fln("b.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x"), fln("d.cue", 1, 1, "x")},
				fln("c.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("b.cue", 1, 1, "x"), fln("d.cue", 1, 1, "x")},
				fln("d.cue", 1, 1, "x"): {self, fln("a.cue", 1, 1, "x"), fln("b.cue", 1, 1, "x"), fln("c.cue", 1, 1, "x")},

				fln("a.cue", 3, 1, "a"): {self, fln("b.cue", 3, 1, "a"), fln("c.cue", 3, 1, "a"), fln("d.cue", 3, 1, "a")},
				fln("a.cue", 4, 1, "X"): {fln("a.cue", 4, 1, "b"), fln("b.cue", 4, 1, "b")},
				fln("a.cue", 4, 1, "b"): {self, fln("b.cue", 4, 1, "b")},
				fln("a.cue", 4, 1, "c"): {self, fln("b.cue", 4, 1, "c")},
				fln("a.cue", 5, 1, "d"): {self},

				fln("b.cue", 3, 1, "a"): {self, fln("a.cue", 3, 1, "a"), fln("c.cue", 3, 1, "a"), fln("d.cue", 3, 1, "a")},
				fln("b.cue", 4, 1, "b"): {self, fln("a.cue", 4, 1, "b")},
				fln("b.cue", 4, 1, "c"): {self, fln("a.cue", 4, 1, "c")},

				fln("c.cue", 3, 1, "a"): {self, fln("a.cue", 3, 1, "a"), fln("b.cue", 3, 1, "a"), fln("d.cue", 3, 1, "a")},
				fln("c.cue", 4, 1, "e"): {self},

				fln("d.cue", 3, 1, "a"): {self, fln("a.cue", 3, 1, "a"), fln("b.cue", 3, 1, "a"), fln("c.cue", 3, 1, "a")},
				fln("d.cue", 4, 1, "X"): {fln("d.cue", 4, 1, "f")},
				fln("d.cue", 4, 1, "f"): {self},
				fln("d.cue", 4, 1, "c"): {self},
				fln("d.cue", 5, 1, "g"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 13): {f: []string{"a"}},
				orf("a.cue", 13, 17): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
				orf("a.cue", 17, 18): {f: []string{"b", "d", "e", "f", "g"}},
				orf("a.cue", 18, 19): {e: []string{"b", "d", "e", "f", "g"}},
				orf("a.cue", 19, 21): {f: []string{"b", "d", "e", "f", "g"}},
				orf("a.cue", 21, 23): {f: []string{"c"}, e: []string{"X", "a", "b", "d"}},
				orf("a.cue", 23, 25): {f: []string{"c"}},
				orf("a.cue", 25, 26): {e: []string{"X", "a", "b", "c", "d"}},
				orf("a.cue", 30, 31): {e: []string{"X", "a", "b", "c", "d"}},
				orf("a.cue", 32, 33): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
				orf("a.cue", 33, 35): {f: []string{"b", "d", "e", "f", "g"}},
				orf("a.cue", 35, 38): {e: []string{"X", "a", "b", "d"}},
				orf("a.cue", 38, 40): {e: []string{"c"}},
				orf("a.cue", 40, 41): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},

				orf("b.cue", 10, 13): {f: []string{"a"}},
				orf("b.cue", 13, 17): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
				orf("b.cue", 17, 19): {f: []string{"b", "d", "e", "f", "g"}},
				orf("b.cue", 19, 21): {f: []string{"c"}, e: []string{"a", "b"}},
				orf("b.cue", 21, 23): {f: []string{"c"}},
				orf("b.cue", 23, 24): {e: []string{"a", "b", "c"}},
				orf("b.cue", 29, 30): {e: []string{"a", "b", "c"}},
				orf("b.cue", 31, 32): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},

				orf("c.cue", 10, 13): {f: []string{"a"}},
				orf("c.cue", 13, 17): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
				orf("c.cue", 17, 19): {f: []string{"b", "d", "e", "f", "g"}},
				orf("c.cue", 19, 22): {e: []string{"a", "e"}},
				orf("c.cue", 24, 25): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},

				orf("d.cue", 10, 13): {f: []string{"a"}},
				orf("d.cue", 13, 17): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
				orf("d.cue", 17, 18): {f: []string{"b", "d", "e", "f", "g"}},
				orf("d.cue", 18, 19): {e: []string{"b", "d", "e", "f", "g"}},
				orf("d.cue", 19, 21): {f: []string{"b", "d", "e", "f", "g"}},
				orf("d.cue", 21, 23): {f: []string{"c"}, e: []string{"X", "a", "f", "g"}},
				orf("d.cue", 23, 25): {f: []string{"c"}},
				orf("d.cue", 25, 26): {e: []string{"X", "a", "c", "f", "g"}},
				orf("d.cue", 27, 28): {e: []string{"X", "a", "c", "f", "g"}},
				orf("d.cue", 29, 30): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
				orf("d.cue", 30, 32): {f: []string{"b", "d", "e", "f", "g"}},
				orf("d.cue", 32, 35): {f: []string{"c"}, e: []string{"X", "a", "f", "g"}},
				orf("d.cue", 35, 36): {f: []string{"b", "d", "e", "f", "g"}, e: []string{"a"}},
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
			expectDefinitions: map[position][]position{
				fln("b.cue", 3, 1, `"a"`): {fln("a.cue", 1, 3, "a"), fln("c.cue", 1, 3, "a")},
				fln("b.cue", 5, 1, "a"):   {fln("b.cue", 3, 1, `"a"`)},
				fln("b.cue", 6, 1, "y"):   {fln("b.cue", 5, 1, "y")},
				fln("b.cue", 6, 1, "x"):   {fln("a.cue", 3, 1, "x")},

				fln("a.cue", 1, 3, "a"): {self, fln("c.cue", 1, 3, "a")},
				fln("b.cue", 1, 1, "b"): {self},
				fln("c.cue", 1, 3, "a"): {self, fln("a.cue", 1, 3, "a")},

				fln("a.cue", 3, 1, "x"): {self},

				fln("b.cue", 5, 1, "y"): {self},
				fln("b.cue", 6, 1, "z"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 13): {f: []string{"x"}},
				orf("a.cue", 13, 14): {e: []string{"x"}},
				orf("a.cue", 16, 17): {e: []string{"x"}},

				orf("b.cue", 10, 18): {f: []string{"y", "z"}},
				orf("b.cue", 18, 22): {e: []string{"a", "y", "z"}},
				orf("b.cue", 22, 25): {f: []string{"y", "z"}},
				orf("b.cue", 25, 28): {f: []string{"x"}, e: []string{"a", "y", "z"}},
				orf("b.cue", 28, 30): {f: []string{"y", "z"}},
				orf("b.cue", 30, 33): {e: []string{"a", "y", "z"}},
				orf("b.cue", 33, 35): {e: []string{"x"}},
			},
			expectUsagesExtra: map[position]map[bool][]position{
				fln("b.cue", 3, 1, `"a"`): {true: []position{self}},
			},
			importedBy: map[string][]string{
				"a": {"b"},
			},
		},

		{
			name: "Resolve_Import_Embed",
			archive: `-- a.cue --
package a
-- b.cue --
package b

import "a"

y: z(a)
-- c.cue --
package a
-- d.cue --
package d

import x "a"
import "a"

y: a / true
`,
			expectDefinitions: map[position][]position{
				fln("b.cue", 3, 1, `"a"`): {fln("a.cue", 1, 3, "a"), fln("c.cue", 1, 3, "a")},
				fln("b.cue", 5, 1, "a"):   {fln("b.cue", 3, 1, `"a"`)},

				fln("d.cue", 3, 1, "x"):   {fln("a.cue", 1, 3, "a"), fln("c.cue", 1, 3, "a")},
				fln("d.cue", 4, 1, `"a"`): {fln("a.cue", 1, 3, "a"), fln("c.cue", 1, 3, "a")},
				fln("d.cue", 6, 1, "a"):   {fln("d.cue", 3, 1, "x"), fln("d.cue", 4, 1, `"a"`)},

				fln("a.cue", 1, 3, "a"): {self, fln("c.cue", 1, 3, "a")},
				fln("b.cue", 1, 1, "b"): {self},
				fln("c.cue", 1, 3, "a"): {self, fln("a.cue", 1, 3, "a")},
				fln("d.cue", 1, 1, "d"): {self},

				fln("b.cue", 5, 1, "y"): {self},
				fln("d.cue", 6, 1, "y"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("b.cue", 10, 18): {f: []string{"y"}},
				orf("b.cue", 18, 22): {e: []string{"a", "y"}},
				orf("b.cue", 22, 25): {f: []string{"y"}},
				orf("b.cue", 25, 31): {e: []string{"a", "y"}},

				orf("d.cue", 10, 18): {f: []string{"y"}},
				orf("d.cue", 18, 24): {e: []string{"a", "x", "y"}},
				orf("d.cue", 24, 31): {f: []string{"y"}},
				orf("d.cue", 31, 35): {e: []string{"a", "x", "y"}},
				orf("d.cue", 35, 38): {f: []string{"y"}},
				orf("d.cue", 38, 43): {e: []string{"a", "x", "y"}},
				orf("d.cue", 47, 48): {e: []string{"a", "x", "y"}},
			},

			expectUsagesExtra: map[position]map[bool][]position{
				fln("b.cue", 3, 1, `"a"`): {true: []position{self}},
				fln("d.cue", 3, 1, "x"):   {true: []position{self, fln("d.cue", 4, 1, `"a"`)}},
				fln("d.cue", 4, 1, `"a"`): {true: []position{self, fln("d.cue", 3, 1, "x")}},
			},
			importedBy: map[string][]string{
				"a": {"b", "d"},
			},
		},

		{
			name: "Resolve_Import_Repeated",
			archive: `-- a.cue --
package a

x: 12
-- b.cue --
package b

import "a"

y: a
-- c.cue --
package b

import "a"

z: a.x & y.x
`,
			expectDefinitions: map[position][]position{
				fln("b.cue", 3, 1, `"a"`): {fln("a.cue", 1, 3, "a")},
				fln("b.cue", 5, 1, "a"):   {fln("b.cue", 3, 1, `"a"`)},

				fln("c.cue", 3, 1, `"a"`): {fln("a.cue", 1, 3, "a")},

				fln("c.cue", 5, 1, "a"): {fln("c.cue", 3, 1, `"a"`)},
				fln("c.cue", 5, 1, "x"): {fln("a.cue", 3, 1, "x")},
				fln("c.cue", 5, 1, "y"): {fln("b.cue", 5, 1, "y")},
				fln("c.cue", 5, 2, "x"): {fln("a.cue", 3, 1, "x")},

				fln("a.cue", 1, 3, "a"): {self},
				fln("b.cue", 1, 1, "b"): {self, fln("c.cue", 1, 1, "b")},
				fln("c.cue", 1, 1, "b"): {self, fln("b.cue", 1, 1, "b")},

				fln("a.cue", 3, 1, "x"): {self},

				fln("b.cue", 5, 1, "y"): {self},
				fln("c.cue", 5, 1, "z"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 13): {f: []string{"x"}},
				orf("a.cue", 13, 14): {e: []string{"x"}},
				orf("a.cue", 16, 17): {e: []string{"x"}},

				orf("b.cue", 10, 18): {f: []string{"y", "z"}},
				orf("b.cue", 18, 22): {e: []string{"a", "y"}},
				orf("b.cue", 22, 25): {f: []string{"y", "z"}},
				orf("b.cue", 25, 28): {f: []string{"x"}, e: []string{"a", "y"}},

				orf("c.cue", 10, 18): {f: []string{"y", "z"}},
				orf("c.cue", 18, 22): {e: []string{"a", "z"}},
				orf("c.cue", 22, 25): {f: []string{"y", "z"}},
				orf("c.cue", 25, 28): {e: []string{"a", "z"}},
				orf("c.cue", 28, 30): {e: []string{"a", "x", "z"}},
				orf("c.cue", 30, 34): {e: []string{"a", "z"}},
				orf("c.cue", 34, 36): {e: []string{"a", "x", "z"}},
			},
			expectUsagesExtra: map[position]map[bool][]position{
				fln("b.cue", 3, 1, `"a"`): {true: []position{self}},
				fln("c.cue", 3, 1, `"a"`): {true: []position{self}},
			},
			importedBy: map[string][]string{
				"a": {"b"},
			},
		},

		{
			name: "Resolve_Import_Chain",
			archive: `-- a.cue --
package a

o: x: y: z: 12
-- b.cue --
package b

import "a"

o: a.o.x
-- c.cue --
package c

import "b"

o: b.o.y
-- d.cue --
package d

import "c"

o: c.o.z
`,
			expectDefinitions: map[position][]position{
				fln("b.cue", 3, 1, `"a"`): {fln("a.cue", 1, 3, "a")},
				fln("b.cue", 5, 1, "a"):   {fln("b.cue", 3, 1, `"a"`)},
				fln("b.cue", 5, 2, "o"):   {fln("a.cue", 3, 1, "o")},
				fln("b.cue", 5, 1, "x"):   {fln("a.cue", 3, 1, "x")},

				fln("c.cue", 3, 1, `"b"`): {fln("b.cue", 1, 1, "b")},
				fln("c.cue", 5, 1, "b"):   {fln("c.cue", 3, 1, `"b"`)},
				fln("c.cue", 5, 2, "o"):   {fln("b.cue", 5, 1, "o")},
				fln("c.cue", 5, 1, "y"):   {fln("a.cue", 3, 1, "y")},

				fln("d.cue", 3, 1, `"c"`): {fln("c.cue", 1, 2, "c")},
				fln("d.cue", 5, 1, "c"):   {fln("d.cue", 3, 1, `"c"`)},
				fln("d.cue", 5, 2, "o"):   {fln("c.cue", 5, 1, "o")},
				fln("d.cue", 5, 1, "z"):   {fln("a.cue", 3, 1, "z")},

				fln("a.cue", 1, 3, "a"): {self},
				fln("a.cue", 3, 1, "o"): {self},
				fln("a.cue", 3, 1, "x"): {self},
				fln("a.cue", 3, 1, "y"): {self},
				fln("a.cue", 3, 1, "z"): {self},

				fln("b.cue", 1, 1, "b"): {self},
				fln("b.cue", 5, 1, "o"): {self},

				fln("c.cue", 1, 2, "c"): {self},
				fln("c.cue", 5, 1, "o"): {self},

				fln("d.cue", 1, 1, "d"): {self},
				fln("d.cue", 5, 1, "o"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				orf("a.cue", 10, 13): {f: []string{"o"}},
				orf("a.cue", 13, 14): {f: []string{"x"}, e: []string{"o"}},
				orf("a.cue", 14, 16): {f: []string{"x"}},
				orf("a.cue", 16, 17): {f: []string{"y"}, e: []string{"o", "x"}},
				orf("a.cue", 17, 19): {f: []string{"y"}},
				orf("a.cue", 19, 20): {f: []string{"z"}, e: []string{"o", "x", "y"}},
				orf("a.cue", 20, 22): {f: []string{"z"}},
				orf("a.cue", 22, 23): {e: []string{"o", "x", "y", "z"}},
				orf("a.cue", 25, 26): {e: []string{"o", "x", "y", "z"}},

				orf("b.cue", 10, 18): {f: []string{"o"}},
				orf("b.cue", 18, 22): {e: []string{"a", "o"}},
				orf("b.cue", 22, 25): {f: []string{"o"}},
				orf("b.cue", 25, 26): {f: []string{"y"}, e: []string{"a", "o"}},
				orf("b.cue", 26, 28): {e: []string{"a", "o"}},
				orf("b.cue", 28, 30): {e: []string{"o"}},
				orf("b.cue", 30, 32): {e: []string{"x"}},

				orf("c.cue", 10, 18): {f: []string{"o"}},
				orf("c.cue", 18, 22): {e: []string{"b", "o"}},
				orf("c.cue", 22, 25): {f: []string{"o"}},
				orf("c.cue", 25, 26): {f: []string{"z"}, e: []string{"b", "o"}},
				orf("c.cue", 26, 28): {e: []string{"b", "o"}},
				orf("c.cue", 28, 30): {e: []string{"o"}},
				orf("c.cue", 30, 32): {e: []string{"y"}},

				orf("d.cue", 10, 18): {f: []string{"o"}},
				orf("d.cue", 18, 22): {e: []string{"c", "o"}},
				orf("d.cue", 22, 25): {f: []string{"o"}},
				orf("d.cue", 25, 28): {e: []string{"c", "o"}},
				orf("d.cue", 28, 30): {e: []string{"o"}},
				orf("d.cue", 30, 32): {e: []string{"z"}},
			},
			expectUsagesExtra: map[position]map[bool][]position{
				fln("b.cue", 3, 1, `"a"`): {true: []position{self}},
				fln("c.cue", 3, 1, `"b"`): {true: []position{self}},
				fln("d.cue", 3, 1, `"c"`): {true: []position{self}},
			},
			importedBy: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {"d"},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"x", "y", "z"}},
				or1(2):     {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(3, 5):   {f: []string{"x"}},
				or1(5):     {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(6, 8):   {f: []string{"x"}},
				or1(8):     {e: []string{"x", "y", "z"}},
				or1(10):    {e: []string{"x", "y", "z"}},
				or(11, 13): {f: []string{"x", "y", "z"}},
				or1(13):    {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(14, 16): {f: []string{"x"}},
				or1(16):    {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(17, 19): {f: []string{"x"}},
				or1(19):    {e: []string{"x", "y", "z"}},
				or1(21):    {e: []string{"x", "y", "z"}},
				or(22, 24): {f: []string{"x", "y", "z"}},
				or(24, 27): {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(27, 29): {f: []string{"x", "y", "z"}},
				or1(29):    {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(30, 32): {f: []string{"x"}},
				or(32, 35): {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(35, 37): {f: []string{"x", "y", "z"}},
				or1(37):    {f: []string{"x"}, e: []string{"x", "y", "z"}},
				or(38, 40): {f: []string{"x"}},
				or1(40):    {e: []string{"x", "y", "z"}},
				or1(42):    {e: []string{"x", "y", "z"}},
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
			expectDefinitions: map[position][]position{
				ln(4, 1, "#Schema"): {ln(1, 1, "#Schema")},

				ln(1, 1, "#Schema"): {self},
				ln(2, 1, "foo"):     {self},
				ln(4, 1, "x"):       {self},
				ln(7, 1, "furble"):  {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 8):   {f: []string{"#Schema", "furble", "x"}},
				or(8, 12):  {f: []string{"foo"}, e: []string{"#Schema", "furble", "x"}},
				or(12, 16): {f: []string{"foo"}},
				or(17, 22): {e: []string{"#Schema", "foo", "furble", "x"}},
				or1(22):    {f: []string{"foo"}, e: []string{"#Schema", "furble", "x"}},
				or(24, 26): {f: []string{"#Schema", "furble", "x"}},
				or(26, 35): {f: []string{"foo"}, e: []string{"#Schema", "furble", "x"}},
				or(35, 37): {e: []string{"#Schema", "furble", "x"}},
				or(37, 43): {f: []string{"foo"}, e: []string{"#Schema", "furble", "x"}},
				or1(43):    {e: []string{"#Schema", "furble", "x"}},
				or(44, 51): {f: []string{"#Schema", "furble", "x"}},
				or1(51):    {e: []string{"#Schema", "furble", "x"}},
				or1(53):    {e: []string{"#Schema", "furble", "x"}},
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
			expectDefinitions: map[position][]position{
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
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 8):   {f: []string{"#Foo", "#Schema", "something"}},
				or(8, 12):  {f: []string{"foo"}, e: []string{"#Foo", "#Schema", "something"}},
				or(12, 16): {f: []string{"foo"}},
				or(17, 23): {f: []string{"bar"}, e: []string{"#Foo", "#Schema", "foo", "something"}},
				or1(23):    {f: []string{"foo"}, e: []string{"#Foo", "#Schema", "something"}},
				or(25, 31): {f: []string{"#Foo", "#Schema", "something"}},
				or(31, 35): {f: []string{"bar"}, e: []string{"#Foo", "#Schema", "something"}},
				or(35, 39): {f: []string{"bar"}},
				or(40, 45): {e: []string{"#Foo", "#Schema", "bar", "something"}},
				or1(45):    {f: []string{"bar"}, e: []string{"#Foo", "#Schema", "something"}},
				or(47, 58): {f: []string{"#Foo", "#Schema", "something"}},
				or(58, 67): {f: []string{"foo"}, e: []string{"#Foo", "#Schema", "something"}},
				or(67, 77): {f: []string{"#Foo", "#Schema", "something"}},
				or(77, 81): {f: []string{"foo"}, e: []string{"#Foo", "#Schema", "something"}},
				or(81, 85): {f: []string{"foo"}},
				or(85, 94): {f: []string{"bar"}, e: []string{"#Foo", "#Schema", "foo", "something"}},
				or1(95):    {f: []string{"foo"}, e: []string{"#Foo", "#Schema", "something"}},
			},
		},

		{
			name: "Self_Simple",
			archive: `-- a.cue --
@experiment(aliasv2)
x: y: 3
x: z: self.y

a: {
    b: {
        c: self.d
        d: 1
    }
    d: self
}
e: self
`,
			expectDefinitions: map[position][]position{
				ln(3, 1, "self"): {ln(2, 1, "x"), ln(3, 1, "x")},
				ln(3, 1, "y"):    {ln(2, 1, "y")},

				ln(7, 1, "self"): {ln(6, 1, "b")},
				ln(7, 1, "d"):    {ln(8, 1, "d")},

				ln(10, 1, "self"): {ln(5, 1, "a")},
				ln(12, 1, "self"): {},

				ln(2, 1, "x"):  {self, ln(3, 1, "x")},
				ln(2, 1, "y"):  {self},
				ln(3, 1, "x"):  {self, ln(2, 1, "x")},
				ln(3, 1, "z"):  {self},
				ln(5, 1, "a"):  {self},
				ln(6, 1, "b"):  {self},
				ln(7, 1, "c"):  {self},
				ln(8, 1, "d"):  {self},
				ln(10, 1, "d"): {self},
				ln(12, 1, "e"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(20, 23):   {f: []string{"a", "e", "x"}},
				or1(23):      {f: []string{"y", "z"}, e: []string{"a", "e", "x"}},
				or(24, 26):   {f: []string{"y", "z"}},
				or1(26):      {e: []string{"a", "e", "x", "y"}},
				or1(28):      {e: []string{"a", "e", "x", "y"}},
				or(29, 31):   {f: []string{"a", "e", "x"}},
				or1(31):      {f: []string{"y", "z"}, e: []string{"a", "e", "x"}},
				or(32, 34):   {f: []string{"y", "z"}},
				or(34, 40):   {e: []string{"a", "e", "x", "z"}},
				or(40, 42):   {e: []string{"y", "z"}},
				or(42, 45):   {f: []string{"a", "e", "x"}},
				or(45, 52):   {f: []string{"b", "d"}, e: []string{"a", "e", "x"}},
				or(52, 54):   {f: []string{"b", "d"}},
				or(54, 65):   {f: []string{"c", "d"}, e: []string{"a", "b", "d", "e", "x"}},
				or(65, 67):   {f: []string{"c", "d"}},
				or(67, 73):   {e: []string{"a", "b", "c", "d", "e", "x"}},
				or(73, 75):   {e: []string{"c", "d"}},
				or(75, 83):   {f: []string{"c", "d"}, e: []string{"a", "b", "d", "e", "x"}},
				or(83, 85):   {f: []string{"c", "d"}},
				or1(85):      {e: []string{"a", "b", "c", "d", "e", "x"}},
				or1(87):      {e: []string{"a", "b", "c", "d", "e", "x"}},
				or(88, 93):   {f: []string{"c", "d"}, e: []string{"a", "b", "d", "e", "x"}},
				or(94, 98):   {f: []string{"b", "d"}, e: []string{"a", "e", "x"}},
				or(98, 100):  {f: []string{"b", "d"}},
				or(100, 106): {f: []string{"b", "d"}, e: []string{"a", "b", "d", "e", "x"}},
				or1(106):     {f: []string{"b", "d"}, e: []string{"a", "e", "x"}},
				or(108, 110): {f: []string{"a", "e", "x"}},
				or(110, 116): {f: []string{"a", "e", "x"}, e: []string{"a", "e", "x"}},
			},
		},

		{
			name: "Self_List",
			archive: `-- a.cue --
@experiment(aliasv2)
f: [ 1, 2, self[0] ]
let X = self
g: h: X.f[0]
`,
			expectDefinitions: map[position][]position{
				ln(2, 1, "self"): {ln(2, 1, "f")},
				ln(2, 1, "0]"):   {ln(2, 1, "1")},
				ln(4, 1, "X"):    {ln(3, 1, "X")},
				ln(4, 1, "f"):    {ln(2, 1, "f")},
				ln(4, 1, "0]"):   {ln(2, 1, "1")},

				ln(2, 1, "f"): {self},
				ln(3, 1, "X"): {self},
				ln(4, 1, "g"): {self},
				ln(4, 1, "h"): {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(20, 23): {f: []string{"f", "g"}},
				or(23, 26): {e: []string{"X", "f", "g"}},
				or(27, 29): {e: []string{"X", "f", "g"}},
				or(30, 37): {e: []string{"X", "f", "g"}},
				or(40, 42): {e: []string{"X", "f", "g"}},
				or(42, 50): {f: []string{"f", "g"}},
				or(50, 55): {f: []string{"f", "g"}, e: []string{"X", "f", "g"}},
				or(55, 57): {f: []string{"f", "g"}},
				or1(57):    {f: []string{"h"}, e: []string{"X", "f", "g"}},
				or(58, 60): {f: []string{"h"}},
				or(60, 63): {e: []string{"X", "f", "g", "h"}},
				or(63, 65): {e: []string{"f", "g"}},
			},
		},

		{
			name: "Self_Self",
			archive: `-- a.cue --
@experiment(aliasv2)
i: self: x: y: z: self
`,
			expectDefinitions: map[position][]position{
				ln(2, 2, "self"): {ln(2, 1, "self")},

				ln(2, 1, "i"):    {self},
				ln(2, 1, "self"): {self},
				ln(2, 1, "x"):    {self},
				ln(2, 1, "y"):    {self},
				ln(2, 1, "z"):    {self},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(20, 23): {f: []string{"i"}},
				or1(23):    {f: []string{"self"}, e: []string{"i"}},
				or(24, 29): {f: []string{"self"}},
				or1(29):    {f: []string{"x"}, e: []string{"i", "self"}},
				or(30, 32): {f: []string{"x"}},
				or1(32):    {f: []string{"y"}, e: []string{"i", "self", "x"}},
				or(33, 35): {f: []string{"y"}},
				or1(35):    {f: []string{"z"}, e: []string{"i", "self", "x", "y"}},
				or(36, 38): {f: []string{"z"}},
				or(38, 44): {f: []string{"x"}, e: []string{"i", "self", "x", "y", "z"}},
			},
		},

		{
			name: "Deep",
			archive: `-- a.cue --
a: b: c: d: e: f: f
a: b: c: d: e: f: f
`,
			expectDefinitions: map[position][]position{
				ln(1, 2, "f"): {ln(1, 1, "f"), ln(2, 1, "f")},
				ln(2, 2, "f"): {ln(2, 1, "f"), ln(1, 1, "f")},

				ln(1, 1, "a"): {self, ln(2, 1, "a")},
				ln(2, 1, "a"): {self, ln(1, 1, "a")},

				ln(1, 1, "b"): {self, ln(2, 1, "b")},
				ln(2, 1, "b"): {self, ln(1, 1, "b")},

				ln(1, 1, "c"): {self, ln(2, 1, "c")},
				ln(2, 1, "c"): {self, ln(1, 1, "c")},

				ln(1, 1, "d"): {self, ln(2, 1, "d")},
				ln(2, 1, "d"): {self, ln(1, 1, "d")},

				ln(1, 1, "e"): {self, ln(2, 1, "e")},
				ln(2, 1, "e"): {self, ln(1, 1, "e")},

				ln(1, 1, "f"): {self, ln(2, 1, "f")},
				ln(2, 1, "f"): {self, ln(1, 1, "f")},
			},
			expectCompletions: map[offsetRange]fieldEmbedCompletions{
				or(0, 2):   {f: []string{"a"}},
				or1(2):     {f: []string{"b"}, e: []string{"a"}},
				or(3, 5):   {f: []string{"b"}},
				or1(5):     {f: []string{"c"}, e: []string{"a", "b"}},
				or(6, 8):   {f: []string{"c"}},
				or1(8):     {f: []string{"d"}, e: []string{"a", "b", "c"}},
				or(9, 11):  {f: []string{"d"}},
				or1(11):    {f: []string{"e"}, e: []string{"a", "b", "c", "d"}},
				or(12, 14): {f: []string{"e"}},
				or1(14):    {f: []string{"f"}, e: []string{"a", "b", "c", "d", "e"}},
				or(15, 17): {f: []string{"f"}},
				or(17, 20): {e: []string{"a", "b", "c", "d", "e", "f"}},
				or(20, 22): {f: []string{"a"}},
				or1(22):    {f: []string{"b"}, e: []string{"a"}},
				or(23, 25): {f: []string{"b"}},
				or1(25):    {f: []string{"c"}, e: []string{"a", "b"}},
				or(26, 28): {f: []string{"c"}},
				or1(28):    {f: []string{"d"}, e: []string{"a", "b", "c"}},
				or(29, 31): {f: []string{"d"}},
				or1(31):    {f: []string{"e"}, e: []string{"a", "b", "c", "d"}},
				or(32, 34): {f: []string{"e"}},
				or1(34):    {f: []string{"f"}, e: []string{"a", "b", "c", "d", "e"}},
				or(35, 37): {f: []string{"f"}},
				or(37, 40): {e: []string{"a", "b", "c", "d", "e", "f"}},
			},
		},
	}.run(t)
}

type testCase struct {
	name              string
	archive           string
	expectDefinitions map[position][]position
	expectCompletions map[offsetRange]fieldEmbedCompletions
	expectUsagesExtra map[position]map[bool][]position
	importedBy        map[string][]string
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

			// Determining offsets. For all of these, we mutate the map
			// keys, and they are not pointers. This means we need to
			// delete the entry from the map, then do the mutation, then
			// re-add to the map.
			expectDefinitions := tc.expectDefinitions
			for from, tos := range expectDefinitions {
				delete(expectDefinitions, from)
				from.determineOffset(filesByName)
				for i := range tos {
					to := &tos[i]
					to.determineOffset(filesByName)
				}
				expectDefinitions[from] = tos
			}
			expectCompletions := tc.expectCompletions
			for from, completions := range expectCompletions {
				delete(expectCompletions, from)
				from.setFilename(filesByName)
				expectCompletions[from] = completions
			}

			expectUsagesExtra := tc.expectUsagesExtra
			for from, usagesM := range expectUsagesExtra {
				delete(expectUsagesExtra, from)
				from.determineOffset(filesByName)
				for _, usages := range usagesM {
					for i := range usages {
						use := &usages[i]
						use.determineOffset(filesByName)
					}
				}
				expectUsagesExtra[from] = usagesM
			}

			analyse := func() testCaseAnalysis {
				evalByFilename := make(map[string]*eval.Evaluator)
				evalByPkgName := make(map[string]*eval.Evaluator)
				forPackage := func(importPath ast.ImportPath) *eval.Evaluator {
					return evalByPkgName[importPath.String()]
				}
				importCanonicalisation := make(map[string]ast.ImportPath)
				analysis := testCaseAnalysis{
					evalByFilename: evalByFilename,
				}

				for pkgName, files := range filesByPkg {
					ip := ast.ImportPath{Path: pkgName}.Canonical()
					importCanonicalisation[pkgName] = ip
					pkgImporters := func() []*eval.Evaluator {
						pkgNames := tc.importedBy[pkgName]
						eval := make([]*eval.Evaluator, len(pkgNames))
						for i, pkgName := range pkgNames {
							eval[i] = evalByPkgName[pkgName]
						}
						return eval
					}
					config := eval.Config{
						IP:                     ip,
						ImportCanonicalisation: importCanonicalisation,
						ForPackage:             forPackage,
						PkgImporters:           pkgImporters,
					}
					eval := eval.New(config, files...)
					evalByPkgName[pkgName] = eval
					for _, fileAst := range files {
						evalByFilename[fileAst.Filename] = eval
					}
				}
				return analysis
			}

			// The subtests need fresh [*eval.FileEvaluator]
			// because each subtest causes mutations.
			tc.testDefinitions(t, files, analyse())
			tc.testCompletions(t, files, analyse())
			tc.testUsages(t, files, analyse())
		})
	}
}

type testCaseAnalysis struct {
	evalByFilename map[string]*eval.Evaluator
}

func (tc *testCase) testDefinitions(t *testing.T, files []*ast.File, analysis testCaseAnalysis) {
	evalByFilename := analysis.evalByFilename
	t.Run("definitions", func(t *testing.T) {
		ranges := rangeset.NewFilenameRangeSet()

		for posFrom, positionsWant := range tc.expectDefinitions {
			filename := posFrom.filename
			fileEval := evalByFilename[filename].ForFile(filename)
			qt.Check(t, qt.IsNotNil(fileEval))

			offset := posFrom.offset
			ranges.Add(filename, offset, offset+len(posFrom.str)+1)

			for i := range len(posFrom.str) + 1 {
				// Test every offset within the "from" token
				offset := offset + i
				nodesGot := fileEval.DefinitionsForOffset(offset)
				fileOffsetsGot := make([]fileOffset, len(nodesGot))
				for j, node := range nodesGot {
					fileOffsetsGot[j] = fileOffsetForTokenPos(node.Pos().Position())
				}
				fileOffsetsWant := make([]fileOffset, len(positionsWant))
				for j, posWant := range positionsWant {
					if posWant == self {
						fileOffsetsWant[j] = posFrom.fileOffset()
					} else {
						fileOffsetsWant[j] = posWant.fileOffset()
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
			fileEval := evalByFilename[filename].ForFile(filename)
			for i := range fileAst.Pos().File().Content() {
				if ranges.Contains(filename, i) {
					continue
				}
				nodesGot := fileEval.DefinitionsForOffset(i)
				fileOffsetsGot := make([]fileOffset, len(nodesGot))
				for j, node := range nodesGot {
					fileOffsetsGot[j] = fileOffsetForTokenPos(node.Pos().Position())
				}
				qt.Check(t, qt.DeepEquals(fileOffsetsGot, []fileOffset{}), qt.Commentf("file: %q, offset: %d", filename, i))
			}
		}
	})
}

func connectedComponents(edges map[position][]position) [][]position {
	// Build undirected adjacency
	adj := maps.Clone(edges)
	for posUse, posDfns := range edges {
		for _, posDfn := range posDfns {
			adj[posDfn] = append(adj[posDfn], posUse)
		}
	}

	visited := make(map[position]struct{})
	var components [][]position

	var dfs func(position, *[]position)
	dfs = func(p position, component *[]position) {
		visited[p] = struct{}{}
		*component = append(*component, p)
		for _, neighbour := range adj[p] {
			if _, found := visited[neighbour]; !found {
				dfs(neighbour, component)
			}
		}
	}

	for pos := range adj {
		if _, found := visited[pos]; !found {
			component := []position{}
			dfs(pos, &component)
			components = append(components, component)
		}
	}

	return components
}

func (tc *testCase) testUsages(t *testing.T, files []*ast.File, analysis testCaseAnalysis) {
	t.Run("usages", func(t *testing.T) {
		// UsagesForOffset has two modes: whether or not to include
		// field declarations in the results. We wish to test both
		// modes.
		//
		// When *excluding* declarations, we can form the expected
		// usages by simply inverting the expected definitions. There
		// are two wrinkles:
		//
		// 1. When examining the expected definitions, we must exclude
		// those where the key is a field declation itself. These can be
		// detected by the fact the value will contain self - i.e. the
		// field declaration resolves to itself.
		//
		// 2. We need to detect dynamic indexing specially. See comments
		// below.
		expectUsagesExcluding := make(map[position][]position)
		for posUse, posDfns := range tc.expectDefinitions {
			if slices.Contains(posDfns, self) {
				continue
			} else if strings.HasSuffix(posUse.str, `"]`) {
				// If posUse ends with "] then we assume it's a const
				// string dynamic index into a struct. These can be
				// inverted. E.g.
				//
				//	{"g": 13}["g"]
				//
				// works correctly in both directions.
			} else if strings.HasSuffix(posUse.str, `]`) {
				// Otherwise, it's either a const number dynamic index, or
				// some reference. These can't be inverted so we have to
				// skip. The const number can't be inverted because in the
				// list, there's no key element to query for usages
				// (i.e. we don't support [0: a, 1: b, 2: c][1]); and
				// references in dynamic indexes are not evaluated by our
				// evaluator.
				continue
			}
			for _, use := range posDfns {
				expectUsagesExcluding[use] = append(expectUsagesExcluding[use], posUse)
			}
		}

		// When *including* declarations, things get a bit more
		// complex. Consider:
		//
		//	d: x: 17         // let's call this x1
		//	r: d & {x: int}  // let's call this x2
		//
		// Here, the x on line 1 resolves only to itself, whilst the x
		// on line 2 resolves to both. I.e. we would have in
		// expectDefinitions:
		//
		//	use       -> definitions
		//	ln(1,1,x) -> {self}              (i.e. x1 -> {x1})
		//	ln(2,1,x) -> {self, ln(1,1,"x"}} (i.e. x2 -> {x1, x2})
		//
		// If we simply invert this we get
		//
		//	def -> uses
		//	x1  -> {x1, x2} // this is fine
		//	x2  -> {x2}     // this is wrong
		//
		// This entry for x2 is wrong. UsagesForOffset starts by
		// resolving the token at the offset and then establishing uses
		// of whatever it has resolved to. So x2 would be resolved to
		// {x1,x2} and then we'd search for uses of both of these.
		//
		// This problem only occurs for keys of expectDefinitions which
		// are themselves field declaration. In the above example, d.x
		// is distinct from r.x and so it is correct that x1 does *not*
		// resolve to x2. Again, we detect these scenarios because an
		// entry in expectDefinitions for a field declaration will
		// always contain self in its value (i.e. the field declaration
		// resolves to itself).
		//
		// So we find these field declarations only within
		// expectDefinitions. We treat these definitions as an
		// *undirected* graph and establish the connected components. In
		// the above example, that'll group x1 and x2 together into a
		// component. Then we invert the expectDefinitions, but, we add
		// in the full component for each definition. So, in the above
		// example, rather than simply inverting
		//
		//	use -> def
		//	x1 -> {x1}
		//
		// we would find the rhs element belongs to the component
		// {x1,x2}, and so we would actually be inverting
		//
		//	use -> def
		//	x1 -> {x1, x2}
		//
		// This then ensures the expectUsagesIncluding contains
		//
		//	def -> uses
		//	x1  -> {x1, x2}
		//	x2  -> {x1, x2}

		declarations := make(map[position][]position)
		for posUse, posDfns := range tc.expectDefinitions {
			posDfns := slices.Clone(posDfns)
			for i, posDfn := range posDfns {
				if posDfn == self { // this must be a field declaration
					posDfns[i] = posUse
					declarations[posUse] = posDfns
				}
			}
		}

		// componentsByMembers maps from any component member to its
		// full component.
		componentsByMembers := make(map[position][]position)
		for _, component := range connectedComponents(declarations) {
			for _, pos := range component {
				componentsByMembers[pos] = component
			}
		}

		expectUsagesIncluding := make(map[position][]position)
		for posUse, posDfns := range tc.expectDefinitions {
			if strings.HasSuffix(posUse.str, `"]`) {
				// Same logic as when calculating expectUsagesExcluding.
			} else if strings.HasSuffix(posUse.str, `]`) {
				// Same logic as when calculating expectUsagesExcluding.
				continue
			}

			// Expand each definition via its component.
			expandedDfns := make(map[position]struct{})
			worklist := posDfns
			for len(worklist) > 0 {
				posDfn := worklist[0]
				worklist = worklist[1:]
				if posDfn == self {
					// Unlike expectUsagesExcluding, we allow self here.
					posDfn = posUse
				}
				if _, seen := expandedDfns[posDfn]; seen {
					continue
				}
				expandedDfns[posDfn] = struct{}{}
				worklist = append(worklist, componentsByMembers[posDfn]...)
			}

			for posDfn := range expandedDfns {
				expectUsagesIncluding[posDfn] = append(expectUsagesIncluding[posDfn], posUse)
			}
		}

		for _, includeDefinitions := range []bool{false, true} {
			var expectUsages map[position][]position
			if includeDefinitions {
				expectUsages = expectUsagesIncluding
			} else {
				expectUsages = expectUsagesExcluding
			}

			for posUse, posDfnsM := range tc.expectUsagesExtra {
				posDfns := expectUsages[posUse]
				for _, posDfn := range posDfnsM[includeDefinitions] {
					if posDfn == self {
						posDfn = posUse
					}
					posDfns = append(posDfns, posDfn)
				}
				expectUsages[posUse] = posDfns
			}

			for posUse, positionsWant := range expectUsages {
				filename := posUse.filename
				fe := analysis.evalByFilename[filename].ForFile(filename)
				qt.Assert(t, qt.IsNotNil(fe))

				fileOffsetsWant := make([]fileOffset, len(positionsWant))
				for j, p := range positionsWant {
					fileOffsetsWant[j] = p.fileOffset()
				}

				offset := posUse.offset
				for i := range len(posUse.str) {
					// Test every offset within the "use" token
					offset := offset + i
					nodesGot := fe.UsagesForOffset(offset, includeDefinitions)
					fileOffsetsGot := make([]fileOffset, len(nodesGot))
					for j, node := range nodesGot {
						fileOffsetsGot[j] = fileOffsetForTokenPos(node.Pos().Position())
					}
					slices.SortFunc(fileOffsetsGot, cmpFileOffsets)
					slices.SortFunc(fileOffsetsWant, cmpFileOffsets)
					qt.Check(t, qt.DeepEquals(fileOffsetsGot, fileOffsetsWant), qt.Commentf("from %#v(+%d) includeDefinitions? %v", posUse, i, includeDefinitions))
				}
			}
		}
	})
}

func (tc *testCase) testCompletions(t *testing.T, files []*ast.File, analysis testCaseAnalysis) {
	evalByFilename := analysis.evalByFilename
	t.Run("completions", func(t *testing.T) {
		defer func() {
			if t.Failed() {
				tc.dumpCompletions(t, files, evalByFilename)
			}
		}()

		ranges := rangeset.NewFilenameRangeSet()

		for curRange, completionWant := range tc.expectCompletions {
			fieldCompletionWant := completionWant.f
			embedCompletionWant := completionWant.e
			slices.Sort(fieldCompletionWant)
			slices.Sort(embedCompletionWant)
			filename := curRange.filename
			fe := evalByFilename[filename].ForFile(filename)
			qt.Check(t, qt.IsNotNil(fe))

			ranges.Add(filename, curRange.from, curRange.to)

			for i := curRange.from; i < curRange.to; i++ {
				fieldCompletionGot := make(map[string]struct{})
				embedCompletionGot := make(map[string]struct{})
				for completions, names := range fe.CompletionsForOffset(i) {
					if completions.Kind == protocol.FieldCompletion {
						maps.Copy(fieldCompletionGot, names)
					}
					if completions.Kind == protocol.VariableCompletion {
						maps.Copy(embedCompletionGot, names)
					}
				}
				fieldCompletionGotSlice := slices.Collect(maps.Keys(fieldCompletionGot))
				slices.Sort(fieldCompletionGotSlice)
				embedCompletionGotSlice := slices.Collect(maps.Keys(embedCompletionGot))
				slices.Sort(embedCompletionGotSlice)
				qt.Check(t, qt.DeepEquals(fieldCompletionGotSlice, fieldCompletionWant), qt.Commentf("from %#v[%d]", curRange, i))
				qt.Check(t, qt.DeepEquals(embedCompletionGotSlice, embedCompletionWant), qt.Commentf("from %#v[%d]", curRange, i))
			}
		}

		// Test that all offsets not explicitly mentioned in
		// expectations, complete to nothing.
		for _, fileAst := range files {
			filename := fileAst.Filename
			fe := evalByFilename[filename].ForFile(filename)

			for i := range fileAst.Pos().File().Content() {
				if ranges.Contains(filename, i) {
					continue
				}
				completions := fe.CompletionsForOffset(i)
				qt.Check(t, qt.DeepEquals(completions, nil), qt.Commentf("file: %q, offset: %d, got %d completions", filename, i, len(completions)))
			}
		}
	})
}

func (tc *testCase) dumpCompletions(t *testing.T, files []*ast.File, evalByFilename map[string]*eval.Evaluator) {
	for _, fileAst := range files {
		filename := fileAst.Filename
		fe := evalByFilename[filename].ForFile(filename)
		content := fileAst.Pos().File().Content()

		var strs []string
		var curRange *offsetRange
		var prevFields, prevEmbeds []string
		for i := range content {
			fieldCompletionGot := make(map[string]struct{})
			embedCompletionGot := make(map[string]struct{})
			for completions, names := range fe.CompletionsForOffset(i) {
				if completions.Kind == protocol.FieldCompletion {
					maps.Copy(fieldCompletionGot, names)
				}
				if completions.Kind == protocol.VariableCompletion {
					maps.Copy(embedCompletionGot, names)
				}
			}
			fieldCompletionGotSlice := slices.Collect(maps.Keys(fieldCompletionGot))
			slices.Sort(fieldCompletionGotSlice)
			embedCompletionGotSlice := slices.Collect(maps.Keys(embedCompletionGot))
			slices.Sort(embedCompletionGotSlice)
			if curRange != nil {
				if slices.Equal(prevFields, fieldCompletionGotSlice) && slices.Equal(prevEmbeds, embedCompletionGotSlice) {
					curRange.to = i + 1
					continue
				}
				strs = completionString(files, curRange, prevFields, prevEmbeds, strs)
			}
			curRange = &offsetRange{
				filename: filename,
				from:     i,
				to:       i + 1,
			}
			prevFields = fieldCompletionGotSlice
			prevEmbeds = embedCompletionGotSlice
		}
		strs = completionString(files, curRange, prevFields, prevEmbeds, strs)

		if len(strs) > 0 {
			t.Log("Suggested expectCompletions: map[offsetRange]fieldEmbedCompletions{\n" + strings.Join(strs, "\n") + "\n},\n")
		}
	}
}

func completionString(files []*ast.File, curRange *offsetRange, fieldCompletionsGot, embedCompletionsGot, acc []string) []string {
	if curRange == nil || len(fieldCompletionsGot) == 0 && len(embedCompletionsGot) == 0 {
		return acc
	}
	msg := ""
	if len(files) == 1 {
		if curRange.from+1 == curRange.to {
			msg = fmt.Sprintf("\tor1(%d): ", curRange.from)
		} else {
			msg = fmt.Sprintf("\tor(%d, %d): ", curRange.from, curRange.to)
		}
	} else {
		msg = fmt.Sprintf("\torf(%q, %d, %d): ", curRange.filename, curRange.from, curRange.to)
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
	return append(acc, msg)
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
	content  string
}

func (p *position) String() string {
	return fmt.Sprintf(`fln(%q, %d, %d, %q)`, p.filename, p.line, p.n, p.str)
}

func (p position) fileOffset() fileOffset {
	return fileOffset{p.filename, p.offset}
}

// Convenience constructor to make a new [position] with the given
// line number (1-based), for the n-th (1-based) occurrence of str.
func ln(i, n int, str string) position {
	return position{
		line: i,
		n:    n,
		str:  str,
	}
}

// Convenience constructor to make a new [position] with the given
// line number (1-based), for the n-th (1-based) occurrence of str
// within the given file.
func fln(filename string, i, n int, str string) position {
	return position{
		filename: filename,
		line:     i,
		n:        n,
		str:      str,
	}
}

func (p *position) determineOffset(filesByName map[string]*ast.File) {
	if *p == self || p.offset != 0 {
		return
	}
	if p.filename == "" {
		if len(filesByName) == 1 {
			for name := range filesByName {
				p.filename = name
			}
		} else {
			panic("no filename set and more than one file available")
		}
	}

	file := filesByName[p.filename].Pos().File()

	// lines is the (cumulative) byte-offset of the start of each line
	lines := file.Lines()
	startOffset := lines[p.line-1]
	endOffset := file.Size()
	if len(lines) > p.line {
		endOffset = lines[p.line]
	}
	content := string(file.Content())
	p.content = content
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

type offsetRange struct {
	filename string
	// from is the start of the range; it is inclusive.
	from int
	// to is the end of the range; it is exclusive.
	to int
}

// Convenience constructor to make a new [offsetRange] with the
// 0-based byte offset range.
func or(from, to int) offsetRange {
	return offsetRange{
		from: from,
		to:   to,
	}
}

// Convenience constructor to make a new [offsetRange] starting at
// from, and finishing 1 byte later.
func or1(from int) offsetRange {
	return offsetRange{
		from: from,
		to:   from + 1,
	}
}

// Convenience constructor to make a new [offsetRange] with the
// 0-based byte offset range within the given file.
func orf(filename string, from, to int) offsetRange {
	return offsetRange{
		filename: filename,
		from:     from,
		to:       to,
	}
}

func (or *offsetRange) setFilename(filesByName map[string]*ast.File) {
	if or.filename == "" {
		if len(filesByName) == 1 {
			for name := range filesByName {
				or.filename = name
			}
		} else {
			panic("no filename set and more than one file available")
		}
	}
}

// self is a convenience singleton which can be freely used in
// expectation values to refer to that expectation's key. Typically
// used to indicate a field's key should resolve to itself.
var self = position{offset: math.MinInt}
