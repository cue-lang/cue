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

package yaml_test

import (
	"io"
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/yaml"
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
		want:    "*null | _",
	}, {
		name:     "empty stream",
		want:     "*null | _",
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
	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := yaml.Extract(tc.name, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := format.Node(f)
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("Extract:\ngot  %q\nwant %q", got, tc.want)
			}

			file, err := yaml.Extract(tc.name, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			b, _ = format.Node(file)
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("Decode:\ngot  %q\nwant %q", got, tc.want)
			}

			yamlOut := tc.yaml
			if tc.yamlOut != "" {
				yamlOut = tc.yamlOut
			}

			wantVal := ctx.CompileString(tc.want)
			if !tc.isStream {
				b, err = yaml.Encode(wantVal)
				if err != nil {
					t.Error(err)
				}
				if got := strings.TrimSpace(string(b)); got != yamlOut {
					t.Errorf("Encode:\ngot  %q\nwant %q", got, yamlOut)
				}
			} else {
				iter, _ := wantVal.List()
				b, err := yaml.EncodeStream(iter)
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

func TestDecoder(t *testing.T) {
	testCases := []struct {
		name    string
		yaml    string
		want    []string
		wantErr bool
	}{{
		name: "empty document",
		yaml: ``,
		want: []string{`*null | _`},
	}, {
		name: "single struct",
		yaml: `a: foo
b: bar`,
		want: []string{`{
	a: "foo"
	b: "bar"
}`},
	}, {
		name: "single struct - inline",
		yaml: `a: foo`,
		want: []string{`{
	a: "foo"
}`},
	}, {
		name: "single list",
		yaml: `[1, 2, 3]`,
		want: []string{`[1, 2, 3]`},
	}, {
		name: "single object",
		yaml: `{"key": "value"}`,
		want: []string{`{
	key: "value"
}`},
	}, {
		name: "single string",
		yaml: `simple string`,
		want: []string{`"simple string"`},
	}, {
		name: "single number",
		yaml: `42`,
		want: []string{`42`},
	}, {
		name: "single boolean",
		yaml: `true`,
		want: []string{`true`},
	}, {
		name: "single null",
		yaml: `null`,
		want: []string{`null`},
	}, {
		name: "multiple documents with separator",
		yaml: `a: foo
---
b: bar
c: baz`,
		want: []string{
			`{
	a: "foo"
}`,
			`{
	b: "bar"
	c: "baz"
}`,
		},
	}, {
		name: "three documents",
		yaml: `name: first
---
name: second
---
name: third`,
		want: []string{
			`{
	name: "first"
}`,
			`{

	name: "second"
}`,
			`{

	name: "third"
}`,
		},
	}, {
		name: "documents with lists",
		yaml: `- one
- two
---
- three
- four`,
		want: []string{
			`[
	"one",
	"two",
]`,
			`[
	"three",
	"four",
]`,
		},
	}, {
		name: "document with null",
		yaml: `---
null
---
a: value`,
		want: []string{
			`null`,
			`{

	a: "value"
}`,
		},
	}, {
		name: "mixed types",
		yaml: `string: text
number: 42
bool: true
---
list:
  - item1
  - item2`,
		want: []string{
			`{
	string: "text"
	number: 42
	bool:   true
}`,
			`{
	list: [
		"item1",
		"item2",
	]
}`,
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d := yaml.NewDecoder(tc.name, strings.NewReader(tc.yaml))

			var results []string
			for {
				expr, err := d.Extract()
				if err == io.EOF {
					break
				}
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				b, err := format.Node(expr)
				if err != nil {
					t.Fatalf("format error: %v", err)
				}
				results = append(results, strings.TrimSpace(string(b)))
			}

			if len(results) != len(tc.want) {
				t.Fatalf("got %d documents, want %d\nresults: %v\nwant: %v",
					len(results), len(tc.want), results, tc.want)
			}

			for i, got := range results {
				if got != tc.want[i] {
					t.Errorf("document %d:\ngot  %q\nwant %q", i, got, tc.want[i])
				}
			}

			// Verify that calling Extract again returns EOF
			_, err := d.Extract()
			if err != io.EOF {
				t.Errorf("expected io.EOF on subsequent Extract, got %v", err)
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
		// Note that go-yaml doesn't quote strings which look like hexadecimal numbers,
		// but we do in our fork. See: https://github.com/go-yaml/yaml/issues/847
		{`"0x123456789012345678901234567890"`, `"0x123456789012345678901234567890"`},

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

			b, err := yaml.Encode(v)
			if err != nil {
				t.Error(err)
			}
			if got := strings.TrimSpace(string(b)); got != tc.yaml {
				t.Errorf("Encode:\ngot  %q\nwant %q", got, tc.yaml)
			}

		})
	}
}
