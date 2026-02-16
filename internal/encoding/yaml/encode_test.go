// Copyright 2020 CUE Authors
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

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
)

func TestEncodeFile(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		out  string
	}{{
		name: "foo",
		in: `
		package test

		seq: [
			1, 2, 3, {
				a: 1
				b: 2
			}
		]
		a: b: c: 3
		b: {
			x: 0
			y: 1
			z: 2
		}
		`,
		out: `
seq:
  - 1
  - 2
  - 3
  - a: 1
    b: 2
a:
  b:
    c: 3
b:
  x: 0
  "y": 1
  z: 2
		`,
	}, {
		name: "oneLineFields",
		in: `
		seq: [1, 2, 3]
		map: {a: 3}
		str: "str"
		int: 1K
		bin: 0b11
		hex: 0x11
		dec: .3
		dat: '\x80'
		nil: null
		yes: true
		non: false
		`,
		out: `
seq: [1, 2, 3]
map: {a: 3}
str: str
int: 1000
bin: 0b11
hex: 0x11
dec: .3
dat: !!binary gA==
nil: null
"yes": true
non: false
`,
	}, {
		name: "comments",
		in: `
// Document

// head 1
f1: 1
// foot 1

// head 2
f2: 2 // line 2

// intermezzo f2
//
// with multiline

// head 3
f3:
	// struct doc
	{
		a: 1
	}

f4: {
} // line 4

// Trailing
`,
		out: `
# Document

# head 1
f1: 1
# foot 1

# head 2
f2: 2 # line 2

# intermezzo f2
#
# with multiline

# head 3
f3:
  # struct doc
  a: 1
f4: {} # line 4

# Trailing
`,
	}, {
		// TODO: support this at some point
		name: "embed",
		in: `
	// hex
	0xabc // line
	// trail
	`,
		out: `
# hex
0xabc # line
# trail
`,
	}, {
		// TODO: support this at some point
		name: "anchors",
		in: `
		a: b
		b: 3
		`,
		out: "yaml: unsupported node b (*ast.Ident)",
	}, {
		name: "errors",
		in: `
			m: {
				a: 1
				b: 3
			}
			c: [1, [for x in m {x}]]
			`,
		out: "yaml: unsupported node for x in m {x} (*ast.Comprehension)",
	}, {
		name: "disallowMultipleEmbeddings",
		in: `
		1
		1
		`,
		out: "yaml: multiple embedded values",
	}, {
		name: "disallowDefinitions",
		in:   `#a: 2 `,
		out:  "yaml: definition or hidden fields not allowed",
	}, {
		name: "disallowHidden",
		in:   `_a: 2 `,
		out:  "yaml: definition or hidden fields not allowed",
	}, {
		name: "disallowOptionals",
		in:   `a?: 2`,
		out:  "yaml: optional fields not allowed",
	}, {
		name: "disallowBulkOptionals",
		in:   `[string]: 2`,
		out:  "yaml: only literal labels allowed",
	}, {
		name: "noImports",
		in: `
		import "foo"

		a: 1
		`,
		out: `yaml: unsupported node import "foo" (*ast.ImportDecl)`,
	}, {
		name: "disallowMultipleEmbeddings",
		in: `
		1
		a: 2
		`,
		out: "yaml: embedding mixed with fields",
	}, {
		name: "prometheus",
		in: `
		{
			receivers: [{
				name: "pager"
				slack_configs: [{
					text: """
						{{ range .Alerts }}{{ .Annotations.description }}
						{{ end }}
						"""
					channel:       "#cloudmon"
					send_resolved: true
				}]
			}]
			route: {
				receiver: "pager"
				group_by: ["alertname", "cluster"]
			}
		}`,
		out: `
receivers:
  - name: pager
    slack_configs:
      - text: |-
          {{ range .Alerts }}{{ .Annotations.description }}
          {{ end }}
        channel: '#cloudmon'
        send_resolved: true
route:
  receiver: pager
  group_by: [alertname, cluster]
		`,
	}, {
		name: "yaml_tag_scalar",
		in: `
		key: "value" @yaml(,tag="!Custom")
		env: "VAR_NAME" @yaml(,tag="!Env")
		`,
		out: `
key: !Custom value
env: !Env VAR_NAME
		`,
	}, {
		name: "yaml_tag_sequence",
		in: `
		lookup: ["table", ["key", "value"]] @yaml(,tag="!Find")
		items: [1, 2, 3] @yaml(,tag="!Seq")
		`,
		out: `
lookup: !Find [table, [key, value]]
items: !Seq [1, 2, 3]
		`,
	}, {
		name: "yaml_tag_mapping",
		in: `
		config: {
			key: "value"
			count: 42
		} @yaml(,tag="!Map")
		`,
		out: `
config: !Map
  key: value
  count: 42
		`,
	}, {
		name: "yaml_tag_mixed",
		in: `
		plain: "no-tag"
		tagged: "has-tag" @yaml(,tag="!Custom")
		nested: {
			field: "value" @yaml(,tag="!Nested")
		}
		`,
		out: `
plain: no-tag
tagged: !Custom has-tag
nested:
  field: !Nested value
		`,
	}, {
		name: "yaml_tag_verbatim",
		in: `
		custom: "value" @yaml(,tag="!<tag:example.com,2000:app/foo>")
		`,
		out: `
custom: !%3Ctag:example.com,2000:app/foo%3E value
		`,
	}, {
		name: "yaml_tag_url",
		in: `
		item: "value" @yaml(,tag="!<https://example.com/schema/v1>")
		`,
		out: `
item: !%3Chttps://example.com/schema/v1%3E value
		`,
	}, {
		name: "yaml_attribute_without_tag",
		in: `
		field: "value" @yaml(,other="ignored")
		`,
		out: `
field: value
		`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile(tc.name, tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Encode(f)
			var got string
			if err != nil {
				got = err.Error()
			} else {
				got = strings.TrimSpace(string(b))
			}
			want := strings.TrimSpace(tc.out)
			if got != want {
				t.Error(cmp.Diff(got, want))
			}
		})
	}
}

func TestEncodeAST(t *testing.T) {
	comment := func(s string) *ast.CommentGroup {
		return &ast.CommentGroup{List: []*ast.Comment{
			{Text: "// " + s},
		}}
	}
	testCases := []struct {
		name string
		in   ast.Expr
		out  string
	}{{
		in: ast.NewStruct(
			comment("foo"),
			comment("bar"),
			"field", ast.NewString("value"),
			"field2", ast.NewString("value"),
			comment("trail1"),
			comment("trail2"),
		),
		out: `
# foo

# bar

field: value
field2: value

# trail1

# trail2
		`,
	}, {
		in: &ast.StructLit{Elts: []ast.Decl{
			comment("bar"),
			&ast.EmbedDecl{Expr: ast.NewBool(true)},
		}},
		out: `
# bar

true
		`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := encode(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			b, err := yaml.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(b))
			want := strings.TrimSpace(tc.out)
			if got != want {
				t.Error(cmp.Diff(got, want))
			}
		})
	}
}
