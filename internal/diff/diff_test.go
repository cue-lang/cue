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

package diff

import (
	"bytes"
	"slices"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/cuetdtest"
)

func TestDiff(t *testing.T) {
	testCases := []struct {
		name    string
		x, y    string
		kind    Kind
		diff    string
		profile *Profile
	}{{
		name: "identity struct",
		x: `{
			a: {
				b: 1
				c: 2
			}
			l: {
				d: 1
			}
		}`,
		y: `{
			a: {
				c: 2
				b: 1
			}
			l: {
				d: 1
			}
		}`,
	}, {
		name: "identity list",
		x:    `[1, 2, 3]`,
		y:    `[1, 2, 3]`,
	}, {
		name: "identity value",
		x:    `"foo"`,
		y:    `"foo"`,
	}, {
		name: "modified value",
		x:    `"foo"`,
		y:    `"bar"`,
		kind: Modified,
		diff: `- "foo",
+ "bar",
`,
	}, {
		name: "basics",
		x: `{
			a: int
			b: 2
			s: 4
			d: 1
			e: null
		}
		`,
		y: `
		{
			a: string
			c: 3
			s: 4
			d: int
			e: null
		}
		`,
		kind: Modified,
		diff: `  {
-     a: int
+     a: string
-     b: 2
+     c: 3
      s: 4
-     d: 1
+     d: int
      e: null
  }
`,
	}, {
		name: "basics 2",
		x: `{
			ls: [2, 3, 4]
			"foo-bar": 2
			s: 4
			lm1: [2, 3, 5]
			lm2: [6]
		}
		`,
		y: `
		{
			ls: [2, 3, 4]
			"foo-bar": 3
			s: 4
			lm1: [2, 3, 4, 6]
			lm2: []
			la: [2, 3, 4]
		}
		`,
		kind: Modified,
		diff: `  {
      ls: [2, 3, 4]
-     "foo-bar": 2
+     "foo-bar": 3
      s: 4
      lm1: [
          2,
          3,
-         5,
+         4,
+         6,
      ]
      lm2: [
-         6,
      ]
+     la: [2, 3, 4]
  }
`,
	}, {
		name: "interupted run 1",
		x: `{
	a: 1
	b: 2
	c: 3
	d: 4
	e: 10
	f: 6
	g: 7
	h: 8
	i: 9
	j: 10
}
`,
		y: `
{
	a: 1
	b: 2
	c: 3
	d: 4
	e: 5
	f: 6
	g: 7
	h: 8
	i: 9
	j: 10
}
`,
		kind: Modified,
		diff: `  {
      ... // 2 identical elements
      c: 3
      d: 4
-     e: 10
+     e: 5
      f: 6
      g: 7
      ... // 3 identical elements
  }
`,
	}, {
		name: "interupted run 2",
		x: `{
			a: -1
			b: 2
			c: 3
			d: 4
			e: 5
			f: 6
			g: 7
			h: 8
			i: 9
			j: -10
		}`,
		y: `{
			a: 1
			b: 2
			c: 3
			d: 4
			e: 5
			f: 6
			g: 7
			h: 8
			i: 9
			j: 10
		}
		`,
		kind: Modified,
		diff: `  {
-     a: -1
+     a: 1
      b: 2
      c: 3
      ... // 4 identical elements
      h: 8
      i: 9
-     j: -10
+     j: 10
  }
`,
	}, {
		name: "recursion",
		x: `{
		s: {
			a: 1
			b: 3
			d: 4
		}
		l: [
			[3, 4]
		]
	}`,
		y: `{
		s: {
			a: 2
			b: 3
			c: 4
		}
		l: [
			[3, 5, 6]
		]
	}
	`,
		kind: Modified,
		diff: `  {
      s: {
-         a: 1
+         a: 2
          b: 3
-         d: 4
+         c: 4
      }
      l: [
          [
              3,
-             4,
+             5,
+             6,
          ]
      ]
  }
`,
	}, {
		name: "optional and definitions",
		x: `{
	#s: {
		#a: 1
		b: 2
	}
	o?: 3
	#od?: 1
	oc?: 5
}`,
		y: `{
	#s: {
		a: 2
		#b: 2
	}
	o?: 4
	#od: 1
	#oc?: 5
}
`,
		kind: Modified,
		diff: `  {
      #s: {
-         #a: 1
-         b: 2
+         a: 2
+         #b: 2
      }
-     o?: 3
+     o?: 4
-     #od?: 1
-     oc?: 5
+     #od: 1
+     #oc?: 5
  }
`,
	}, {
		name: "bulk optional",
		x: `{[_]: x: "hello"}

a: x: "hello"
		`,
		y: `[_]: x: "hello"

		`,
		kind: Modified,
		diff: `  {
-     a: {x: "hello"}
  }
`,
	}, {
		x: `
		#Directory: {
			{
					// Directory from another directory (e.g. subdirectory)
					from: #Directory
			} | {
					// Reference to remote directory
					ref: string
			} | {
					// Use a local directory
					local: string
			}
			path: string | *"/"
	}
		`,
		y: `
	#Directory: {
        {
                // Directory from another directory (e.g. subdirectory)
                from: #Directory
        } | {
                // Reference to remote directory
                ref: string
        } | {
                // Use a local directory
                local: string
        }
        path: string | *"/"
	}
	`,
		profile: Final,
	}, {
		x: `
		#Directory: {
			{
					// Directory from another directory (e.g. subdirectory)
					from: #Directory
			} | {
					// Reference to remote directory
					ref: string
			} | {
					// Use a local directory
					local: string
			}
			path: string | *"/"
	}
		`,
		y: `
	#Directory: {
        {
                // Directory from another directory (e.g. subdirectory)
                from: #Directory
        } | {
                // Reference to remote directory
                ref: string
        } | {
                // Use a local directory
                local: string
        }
        path: string | *"/"
	}
`,
	}, {
		name: "hidden fields",
		x:    `{a: 1, _hidden1: 1, _hidden: 1}`,
		y:    `{a: 1, _hidden2: 1, _hidden: 2}`,
		diff: `  {
      a: 1
-     _hidden1: 1
+     _hidden2: 1
-     _hidden: 1
+     _hidden: 2
  }
`,
		kind: Modified,
	}, {
		name:    "ignore hidden fields in schema",
		x:       `{a: 1, _hidden1: 1, _hidden: 1}`,
		y:       `{a: 1, _hidden2: 1, _hidden: 2}`,
		profile: &Profile{SkipHidden: true},
	}, {
		name:    "ignore hidden fields in data",
		x:       `{a: 1, _hidden1: 1, _hidden: 1}`,
		y:       `{a: 1, _hidden2: 1, _hidden: 2}`,
		profile: &Profile{SkipHidden: true, Concrete: true},
	}, {
		name: "all errors are equal",
		x:    `1 & 3`,
		y:    `1 & 4`,
	}, {
		// 'a' and 'b' reference 'g', so under the v3 evaluator they share g's
		// *adt.Vertex. The differ must return Identity without false-positive
		// Modified results caused by memoization.
		name: "shared vertices identity",
		x: `
			g: {x: 1, y: 2}
			a: g
			b: g
		`,
		y: `
			g: {x: 1, y: 2}
			a: g
			b: g
		`,

		// --- Myers list diff tests ---

	}, {
		// Pure insertion at the beginning: only the new element should be marked UniqueY.
		// The printer shows 2 context elements after the change; the 4th (last) is elided.
		name: "list insert at beginning",
		x:    `[2, 3, 4]`,
		y:    `[1, 2, 3, 4]`,
		kind: Modified,
		diff: `  [
+     1,
      2,
      3,
      ... // 1 identical elements
  ]
`,
	}, {
		// Pure insertion in the middle: only the new element should be marked UniqueY.
		name: "list insert in middle",
		x:    `[1, 3, 4]`,
		y:    `[1, 2, 3, 4]`,
		kind: Modified,
		diff: `  [
      1,
+     2,
      3,
      4,
  ]
`,
	}, {
		// Pure insertion at the end: 2 context elements before the change are shown;
		// the first element (1) is elided.
		name: "list insert at end",
		x:    `[1, 2, 3]`,
		y:    `[1, 2, 3, 4]`,
		kind: Modified,
		diff: `  [
      ... // 1 identical elements
      2,
      3,
+     4,
  ]
`,
	}, {
		// Pure deletion from the beginning.
		name: "list delete at beginning",
		x:    `[1, 2, 3, 4]`,
		y:    `[2, 3, 4]`,
		kind: Modified,
		diff: `  [
-     1,
      2,
      3,
      ... // 1 identical elements
  ]
`,
	}, {
		// Pure deletion from the middle.
		name: "list delete in middle",
		x:    `[1, 2, 3, 4]`,
		y:    `[1, 3, 4]`,
		kind: Modified,
		diff: `  [
      1,
-     2,
      3,
      4,
  ]
`,
	}, {
		// Pure deletion from the end.
		name: "list delete at end",
		x:    `[1, 2, 3, 4]`,
		y:    `[1, 2, 3]`,
		kind: Modified,
		diff: `  [
      ... // 1 identical elements
      2,
      3,
-     4,
  ]
`,
	}, {
		// Mixed: deletion and insertion at different positions.
		name: "list mixed insert and delete",
		x:    `[1, 2, 3, 4, 5]`,
		y:    `[1, 3, 99, 5]`,
		kind: Modified,
		diff: `  [
      1,
-     2,
      3,
-     4,
+     99,
      5,
  ]
`,
	}, {
		// Completely different lists: mergeAdjacentEdits pairs each UniqueX with
		// the corresponding UniqueY, producing interleaved -/+ output.
		name: "list completely different",
		x:    `[1, 2, 3]`,
		y:    `[4, 5, 6]`,
		kind: Modified,
		diff: `  [
-     1,
+     4,
-     2,
+     5,
-     3,
+     6,
  ]
`,
	}, {
		// After an insertion, the XSel/YSel of Identity edits reflect independent
		// positions in their respective lists.  The 4th identical element is elided
		// by the 2-element context window.
		name: "list index independence after insertion",
		x:    `[10, 20, 30]`,
		y:    `[5, 10, 20, 30]`,
		kind: Modified,
		diff: `  [
+     5,
      10,
      20,
      ... // 1 identical elements
  ]
`,
	}, {
		// Struct element with one changed field: expect Modified with sub-diff
		// showing all fields (identity + modified) within the context window.
		name: "list struct element single field diff",
		x:    `[{a: 1, b: 2}]`,
		y:    `[{a: 1, b: 3}]`,
		kind: Modified,
		diff: `  [
      {
          a: 1
-         b: 2
+         b: 3
      }
  ]
`,
	}, {
		// Nested list element that differs in one item.
		name: "list nested list single element diff",
		x:    `[[1, 2, 3]]`,
		y:    `[[1, 99, 3]]`,
		kind: Modified,
		diff: `  [
      [
          1,
-         2,
+         99,
          3,
      ]
  ]
`,
	}, {
		// Two UniqueX, one UniqueY: the first UniqueX pairs with the UniqueY into
		// a Modified edit; the second UniqueX remains unpaired.
		name: "list merge run length limiting",
		x:    `[1, 2, 10]`,
		y:    `[99, 10]`,
		kind: Modified,
		diff: `  [
-     1,
+     99,
-     2,
      10,
  ]
`,
	}, {
		// Adjacent UniqueX (number) and UniqueY (struct): incompatible kinds,
		// must remain as separate edits and not be merged into Modified.
		name: "list incompatible kinds not merged",
		x:    `[1]`,
		y:    `[{a: 1}]`,
		kind: Modified,
		diff: `  [
-     1,
+     {a: 1},
  ]
`,
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.name, func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()
			// it is not fatal if x or y contain errors: some test cases
			// rely on interacting with such errors.
			x := ctx.CompileString(tc.x, cue.Filename("x"))
			y := ctx.CompileString(tc.y, cue.Filename("y"))
			p := tc.profile
			if p == nil {
				p = Schema
			}
			kind, script := p.Diff(x, y)
			if kind != tc.kind {
				t.Fatalf("got %d; want %d", kind, tc.kind)
			}
			if script != nil {
				w := &bytes.Buffer{}
				err := Print(w, script)
				if err != nil {
					t.Fatal(err)
				}
				if got := w.String(); got != tc.diff {
					t.Errorf("\ngot\n%s;\nwant\n%s", got, tc.diff)
				}
			}
		})
	}
}

// TestMemoization checks that the differ's vertex-pair memoization fires when
// structure sharing is present. When 'a' and 'b' are both references to 'g',
// comparing (x.a, y.a) populates the cache so that comparing (x.b, y.b) is a
// cache hit rather than a redundant traversal of g's fields.
func TestMemoization(t *testing.T) {
	const src = `
		g: {x: 1, y: 2}
		a: g
		b: g
	`
	tests := []struct {
		name string
		x, y string
		kind Kind
		// With structure sharing, seen is the number of memoized pairs and
		// hitB is whether comparing 'b' is a cache hit, which returns no
		// sub-script. Without sharing, the cache stays empty.
		seen int
		hitB bool
	}{{
		name: "identity",
		x:    src,
		y:    src,
		kind: Identity,
		seen: 1,
	}, {
		name: "modified",
		x:    src,
		y: `
			g: {x: 99, y: 2}
			a: g
			b: g
		`,
		kind: Modified,
		seen: 1,
		hitB: true,
	}, {
		// Distinct vertex pairs must not share a cache entry: (x.g, y.g) is
		// identical, while (x.g, y.h) is not.
		name: "distinct",
		x: `
			g: {x: 1}
			h: {x: 1}
			a: g
			b: g
		`,
		y: `
			g: {x: 1}
			h: {x: 2}
			a: g
			b: h
		`,
		kind: Modified,
		seen: 2,
	}}
	for _, tc := range tests {
		cuetdtest.FullMatrix.Run(t, tc.name, func(t *testing.T, m *cuetdtest.M) {
			ctx := m.CueContext()
			x := ctx.CompileString(tc.x)
			y := ctx.CompileString(tc.y)

			d := &differ{cfg: *Schema}
			kind, script := d.diffValue(x, y)
			if kind != tc.kind {
				t.Fatalf("got %v; want %v", kind, tc.kind)
			}
			wantSeen := 0
			if m.Flags.Sharing {
				wantSeen = tc.seen
			}
			if n := len(d.seen); n != wantSeen {
				t.Errorf("expected %d memoized pairs; got %d", wantSeen, n)
			}
			if kind == Identity {
				if script != nil {
					t.Errorf("expected nil script for identical values")
				}
				return
			}
			if script == nil {
				t.Fatal("expected an edit script for modified values")
			}
			i := slices.IndexFunc(script.Edits, func(e Edit) bool { return e.XSel.String() == "b" })
			if i < 0 {
				t.Fatal("no edit for b")
			}
			b := script.Edits[i]
			if b.Kind != Modified {
				t.Errorf("b: got %v; want Modified", b.Kind)
			}
			wantHit := m.Flags.Sharing && tc.hitB
			if hit := b.Sub == nil; hit != wantHit {
				t.Errorf("cache hit for b: got %v; want %v", hit, wantHit)
			}
		})
	}
}

func TestX(t *testing.T) {
	t.Skip()

	tc := struct {
		x, y string
		kind Kind
		diff string
	}{
		x: `{
		}
		`,
		y: `
		{
		}
		`,
		kind: Modified,
		diff: ``,
	}
	ctx := cuecontext.New()
	// it is not fatal if x or y contain errors: some test cases
	// rely on interacting with such errors.
	x := ctx.CompileString(tc.x, cue.Filename("x"))
	y := ctx.CompileString(tc.y, cue.Filename("y"))

	kind, script := Diff(x, y)
	if kind != tc.kind {
		t.Fatalf("got %d; want %d", kind, tc.kind)
	}
	w := &bytes.Buffer{}
	err := Print(w, script)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.String(); got != tc.diff {
		t.Errorf("got\n%s;\nwant\n%s", got, tc.diff)
	}
}
