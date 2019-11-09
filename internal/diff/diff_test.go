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
	"testing"

	"cuelang.org/go/cue"
)

func TestDiff(t *testing.T) {
	testCases := []struct {
		name string
		x, y string
		kind Kind
		diff string
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
	}, {
		name: "basics",
		x: `{
			a: int
			b: 2
			s: 4
			d: 1
		}
		`,
		y: `
		{
			a: string
			c: 3
			s: 4
			d: int
		}
		`,
		kind: Modified,
		diff: `  {
-     a: int
+     a: string
-     b: 2
      s: 4
-     d: 1
+     d: int
+     c: 3
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
	s :: {
		a :: 1
		b: 2
	}
	o?: 3
	od? :: 1
	oc?: 5
}`,
		y: `{
	s :: {
		a: 2
		b :: 2
	}
	o?: 4
	od :: 1
	oc? :: 5
}
`,
		kind: Modified,
		diff: `  {
      s :: {
-         a :: 1
+         a: 2
-         b: 2
+         b :: 2
      }
-     o?: 3
+     o?: 4
-     od? :: 1
+     od :: 1
-     oc?: 5
+     oc? :: 5
  }
`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var r cue.Runtime
			x, err := r.Compile("x", tc.x)
			if err != nil {
				t.Fatal(err)
			}
			y, err := r.Compile("y", tc.y)
			if err != nil {
				t.Fatal(err)
			}
			kind, script := Diff(x.Value(), y.Value())
			if kind != tc.kind {
				t.Fatalf("got %d; want %d", kind, tc.kind)
			}
			if script != nil {
				w := &bytes.Buffer{}
				err = Print(w, script)
				if err != nil {
					t.Fatal(err)
				}
				if got := w.String(); got != tc.diff {
					t.Errorf("got\n%s;\nwant\n%s", got, tc.diff)
				}
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
	var r cue.Runtime
	x, err := r.Compile("x", tc.x)
	if err != nil {
		t.Fatal(err)
	}
	y, err := r.Compile("y", tc.y)
	if err != nil {
		t.Fatal(err)
	}
	kind, script := Diff(x.Value(), y.Value())
	if kind != tc.kind {
		t.Fatalf("got %d; want %d", kind, tc.kind)
	}
	w := &bytes.Buffer{}
	err = Print(w, script)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.String(); got != tc.diff {
		t.Errorf("got\n%s;\nwant\n%s", got, tc.diff)
	}
}
