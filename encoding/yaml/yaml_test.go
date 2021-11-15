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

package yaml

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
)

func TestYAML(t *testing.T) {
	testCases := []struct {
		name     string
		yaml     string
		yamlOut  string
		want     string
		isStream bool
	}{{
		name:    "empty",
		yaml:    "",
		yamlOut: "null",
		want:    "null",
	}, {
		name:     "empty stream",
		want:     "null",
		isStream: true,
	}, {
		name: "string literal",
		yaml: `foo`,
		want: `"foo"`,
	}, {
		name: "struct",
		yaml: `a: foo
b: bar`,
		want: `a: "foo"
b: "bar"`,
	}, {
		name: "stream",
		yaml: `a: foo
---
b: bar
c: baz
`,
		want: `[{
	a: "foo"
}, {
	b: "bar"
	c: "baz"
}]`,
		isStream: true,
	}, {
		name: "stream with null",
		yaml: `
---
a: foo
---
---
b: bar
c: baz
---
`,
		// Not sure if a leading document separator should be gobbled, but the
		// YAML parser seems to think so. This could have something to do with
		// the fact that the document separator is really an "end of directives"
		// marker, while ... means "end of document". YAML is hard!
		yamlOut: `a: foo
---
null
---
b: bar
c: baz
---
null
`,
		// TODO(bug): seems like bug in yaml parser. Try moving to yaml.v3,
		// or validate that this is indeed a correct interpretation.
		want: `[{
	a: "foo"
}, null, {
	b: "bar"
	c: "baz"
}, null]`,
		isStream: true,
	}}
	r := &cue.Runtime{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Extract(tc.name, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := format.Node(f)
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("Extract:\ngot  %q\nwant %q", got, tc.want)
			}

			inst, err := Decode(r, tc.name, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			n := inst.Value().Syntax()
			if s, ok := n.(*ast.StructLit); ok {
				n = &ast.File{Decls: s.Elts}
			}
			b, _ = format.Node(n)
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("Decode:\ngot  %q\nwant %q", got, tc.want)
			}

			yamlOut := tc.yaml
			if tc.yamlOut != "" {
				yamlOut = tc.yamlOut
			}

			inst, _ = r.Compile(tc.name, tc.want)
			if !tc.isStream {
				b, err = Encode(inst.Value())
				if err != nil {
					t.Error(err)
				}
				if got := strings.TrimSpace(string(b)); got != yamlOut {
					t.Errorf("Encode:\ngot  %q\nwant %q", got, yamlOut)
				}
			} else {
				iter, _ := inst.Value().List()
				b, err := EncodeStream(iter)
				if err != nil {
					t.Error(err)
				}
				if got := string(b); got != yamlOut {
					t.Errorf("EncodeStream:\ngot  %q\nwant %q", got, yamlOut)
				}
			}
		})
	}
}

func TestYAMLValues(t *testing.T) {
	testCases := []struct {
		cue  string
		yaml string
	}{
		// strings
		{`"""
	single
	"""`, "single"}, // TODO: CUE simplifies this.

		{`"""
	aaaa
	bbbb
	"""`, `|-
  aaaa
  bbbb`},

		// keep as is
		{`"non"`, `non`},

		// Non-strings in v1.2. These are single-quoted by the go-yaml.v3 package.
		{`"#cloudmon"`, `'#cloudmon'`},

		// Strings that mimic numeric values are double quoted by the go-yaml.v3
		// package.
		{`".inf"`, `".inf"`},
		{`".Inf"`, `".Inf"`},
		{`".INF"`, `".INF"`},
		{`".NaN"`, `".NaN"`},
		{`"+.Inf"`, `"+.Inf"`},
		{`"-.Inf"`, `"-.Inf"`},
		{`"2002"`, `"2002"`},
		{`"685_230.15"`, `"685_230.15"`},

		// Legacy values.format.
		{`"no"`, `"no"`},
		{`"on"`, `"on"`},
		{`".Nan"`, `".Nan"`},

		// binary
		{`'no'`, `!!binary bm8=`},

		// floats
		{`.2`, "0.2"},
		{`2.`, "2."},
		{`".inf"`, `".inf"`},
		{`685_230.15`, `685230.15`},

		// Date and time-like
		{`"2001-12-15T02:59:43.1Z"`, `"2001-12-15T02:59:43.1Z"`},
		{`"2001-12-14t21:59:43.10-05:00"`, `"2001-12-14t21:59:43.10-05:00"`},
		{`"2001-12-14 21:59:43.10 -5"`, `"2001-12-14 21:59:43.10 -5"`},
		{`"2001-12-15 2:59:43.10"`, `"2001-12-15 2:59:43.10"`},
		{`"2002-12-14"`, `"2002-12-14"`},
		{`"12-12-12"`, `"12-12-12"`},

		// legacy base60 floats
		{`"2222:22"`, `"2222:22"`},

		// hostport
		{`"hostname:22"`, `hostname:22`},

		// maps
		{
			cue: `
			true:   1
			True:   2
			".Nan": 3
			".Inf": 4
			y:      5
			`,
			yaml: `"true": 1
"True": 2
".Nan": 3
".Inf": 4
"y": 5`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.cue, func(t *testing.T) {
			c := cuecontext.New()
			v := c.CompileString(tc.cue)

			b, err := Encode(v)
			if err != nil {
				t.Error(err)
			}
			if got := strings.TrimSpace(string(b)); got != tc.yaml {
				t.Errorf("Encode:\ngot  %q\nwant %q", got, tc.yaml)
			}

		})
	}
}
