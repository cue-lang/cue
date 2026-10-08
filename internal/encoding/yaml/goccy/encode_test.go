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

package goccy

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	yparser "github.com/goccy/go-yaml/parser"

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
		// Scalars inside single-line flow collections need quoting for
		// characters which are special in flow context, such as commas,
		// braces, and colons. Scalars beginning with the "?" indicator,
		// merge keys spelled "<<", and keys containing newlines need
		// quoting in any context.
		name: "quoting",
		in: `
		v: {a: "x,y", b: 2}
		w: ["x,y", "z"]
		u: {a: "x}y"}
		fk: {"x,y": 1}
		c: {a: "a:b", u: "http://x/y"}
		"k\nl": 3
		q: "?"
		r: "? q"
		s: "?x"
		m1: "<<"
		m2: {"<<": 1}
		`,
		out: `
v: {a: 'x,y', b: 2}
w: ['x,y', z]
u: {a: 'x}y'}
fk: {'x,y': 1}
c: {a: 'a:b', u: 'http://x/y'}
"k\nl": 3
q: '?'
r: '? q'
s: ?x
m1: '<<'
m2: {'<<': 1}
`,
	}, {
		// Strings use a literal block only when the block decodes back
		// to the same value: no line may end in a space, and characters
		// needing escapes require double quotes instead. Blank lines
		// within a block stay truly empty. Multi-line CUE literals keep
		// their block form even with single-line content.
		name: "strings",
		in: `
blank: """
	line1

	line2
	"""
single: """
	{"tmpl": {{value}}}
	"""
deep: nested: """
	one line
	"""
list: ["""
	elem
	""", "plain"]
trailsp:   "a \nb"
spaceline: "a\n \nb"
endsp:     "x\n "
tab:       "x\ty"
cr:        "a\rb"
nel:       "x\u0085y"
tabline: """
	a\tb
	c
	"""
`,
		out: `
blank: |-
  line1

  line2
single: |-
  {"tmpl": {{value}}}
deep:
  nested: |-
    one line
list:
  - |-
    elem
  - plain
trailsp: "a \nb"
spaceline: "a\n \nb"
endsp: "x\n "
tab: "x\ty"
cr: "a\rb"
nel: "x\u0085y"
tabline: |-
  a	b
  c
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
		// An unsupported node next to an embedding is reported as such.
		name: "disallowEllipsis",
		in: `
		a: {"x", ...}
		`,
		out: "yaml: unsupported node ... (*ast.Ellipsis)",
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
custom: !<tag:example.com,2000:app/foo> value
		`,
	}, {
		name: "yaml_tag_url",
		in: `
		item: "value" @yaml(,tag="!<https://example.com/schema/v1>")
		`,
		out: `
item: !<https://example.com/schema/v1> value
		`,
	}, {
		// An untagged embedding keeps the tag of its value.
		name: "yaml_tag_embed_untagged",
		in: `
		{'\x80'}
		`,
		out: `
!!binary gA==
		`,
	}, {
		// Declaration attributes tag the enclosing value, and a field
		// attribute on that value takes precedence over them.
		name: "yaml_tag_decl",
		in: `
		a: {
			@yaml(,tag="!A")
			b: 1
		}
		c: {"x", @yaml(,tag="!C")} @yaml(,tag="!D")
		`,
		out: `
a: !A
  b: 1
c: !D x
		`,
	}, {
		// Tagged collections and block strings as sequence elements.
		name: "yaml_tag_decl_elements",
		in: `
		l: [{
			@yaml(,tag="!Ref")
			a: 1
			b: 2
		}, {
			[
				"x",
				{"z", @yaml(,tag="!Env")},
			]
			@yaml(,tag="!Join")
		}, {
			"""
			line1
			line2

			"""
			@yaml(,tag="!Text")
		}, [[{
			@yaml(,tag="!Deep")
			c: 1
			d: 2
		}]]] @yaml(,tag="!Format")
		`,
		out: `
l: !Format
  - !Ref
    a: 1
    b: 2
  - !Join
    - x
    - !Env z
  - !Text |
    line1
    line2
  - - - !Deep
        c: 1
        d: 2
		`,
	}, {
		// A declaration attribute in a file tags the document.
		name: "yaml_tag_decl_file",
		in: `
		@yaml(,tag="!Doc")
		a: 1
		`,
		out: `
!Doc
a: 1
		`,
	}, {
		name: "yaml_tag_conflict_field",
		in: `
		a: "x" @yaml(,tag="!A") @yaml(,tag="!B")
		`,
		out: `yaml: conflicting tags "!A" and "!B"`,
	}, {
		name: "yaml_tag_conflict_decl",
		in: `
		a: {"x", @yaml(,tag="!A"), @yaml(,tag="!B")}
		`,
		out: `yaml: conflicting tags "!A" and "!B"`,
	}, {
		// A tag is either local, starting with "!", or global, a URI.
		name: "yaml_tag_without_bang",
		in: `
		a: "v" @yaml(,tag="Env")
		`,
		out: `yaml: invalid tag "Env": must be a local tag starting with "!", or a URI`,
	}, {
		// A URI is written as a verbatim tag.
		name: "yaml_tag_uri",
		in: `
		b: "v" @yaml(,tag="tag:example.com,2000:app/B")
		`,
		out: `
b: !<tag:example.com,2000:app/B> v
		`,
	}, {
		// Characters a tag cannot carry are percent-escaped: "!" would
		// end a tag handle and "," a tag in a flow collection.
		name: "yaml_tag_escape",
		in: `
		a: "x" @yaml(,tag="!a!b,c")
		b: ["y"] @yaml(,tag="!!my type")
		c: "z" @yaml(,tag="!<!a b>")
		d: "w" @yaml(,tag="!a%21b")
		`,
		out: `
a: !a%21b%2Cc x
b: !!my%20type ["y"]
c: !<!a%20b> z
d: !a%21b w
		`,
	}, {
		// A tag shorthand needs a suffix.
		name: "yaml_tag_empty_suffix",
		in: `
		a: "v" @yaml(,tag="!!")
		`,
		out: `yaml: invalid tag "!!": must be a local tag starting with "!", or a URI`,
	}, {
		// The core tags are abbreviated with "!!", as given by YAML
		// directly, so that decoders which support them recognize them.
		name: "yaml_tag_core_uri",
		in: `
		a: "v" @yaml(,tag="tag:yaml.org,2002:str")
		b: "w" @yaml(,tag="!<tag:yaml.org,2002:str>")
		`,
		out: `
a: !!str v
b: !!str w
		`,
	}, {
		// TODO: comments on a tagged value, and on a @yaml declaration
		// attribute, are dropped; want "# line1", "# line2", and "# doc".
		name: "yaml_tag_comments",
		in: `
		l: [
			{"x", @yaml(,tag="!Env")}, // line1
		]
		s: "v" @yaml(,tag="!S") // line2
		m: {
			// doc
			@yaml(,tag="!M")
			k: 1
		}
		`,
		out: `
l:
  - !Env x
s: !S v
m: !M
  k: 1
		`,
	}, {
		// A scalar embedded in a single-line struct is not in a flow
		// collection, so it needs no quoting.
		name: "embed_single_line",
		in: `
		l: [
			{"http://x"},
			"http://x",
		]
		`,
		out: `
l:
  - http://x
  - http://x
		`,
	}, {
		// A tagged single-line string written as a multi-line literal
		// cannot be rendered as a block, and is quoted like any other.
		name: "literal_tagged_quoted",
		in: `
		a: """
			x\u00a0#y
			""" @yaml(,tag="!T")
		`,
		out: "a: !T 'x\u00a0#y'",
	}, {
		// Decoders such as yaml.v3 read "?" in a flow collection as an
		// explicit key indicator, even when it is followed by a
		// character other than a space.
		name: "flow_question_mark",
		in: `
		a: {"?0": "x", b: "?y", c: ["a?b"]}
		`,
		out: `
a: {'?0': x, b: '?y', c: ['a?b']}
		`,
	}, {
		// Number literals are kept only in forms which YAML's core
		// schema resolves as numbers.
		// TODO: binary literals, underscores, and signs before a base
		// prefix are kept, which the core schema resolves as strings.
		name: "number_literals",
		in: `
		a: 0b101
		b: 1_000
		c: 0x1_F
		d: -0x1F
		e: -0o17
		f: 0o17
		g: 1.5e3
		h: 1Ki
		`,
		out: `
a: 0b101
b: 1_000
c: 0x1_F
d: -0x1F
e: -0o17
"f": 0o17
g: 1.5e3
h: 1024
		`,
	}, {
		// A literal block with keep chomping takes in the blank lines
		// which follow it, so none separates it from a following comment.
		name: "keep_block_foot_comment",
		in: `
		a: {
			b: """
				x


				"""

			// c
		}
		d: """
			y


			"""

		// trailing

		e: 1
		`,
		out: `
a:
  b: |+
    x

  # c
d: |+
  y

# trailing
e: 1
		`,
	}, {
		name: "yaml_attribute_without_tag",
		in: `
		field: "value" @yaml(,other="ignored")
		`,
		out: `
field: value
		`,
	}, {
		// A CUE bytes value renders as !!binary; an explicit @yaml tag
		// replaces that tag rather than nesting a second one, which
		// would be invalid YAML.
		name: "yaml_tag_on_bytes",
		in: `
		item: '\xff\xfe' @yaml(,tag="!custom")
		`,
		out: `
item: !custom //4=
		`,
	}, {
		name: "yaml_binary_tag_on_bytes",
		in: `
		item: '\xff\xfe' @yaml(,tag="!!binary")
		`,
		out: `
item: !!binary //4=
		`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile(tc.name, tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Encode(f, EncodeOptions{})
			var got string
			if err != nil {
				got = err.Error()
			} else {
				got = strings.TrimSpace(string(b))
				// Any successfully encoded output must be valid YAML.
				if _, err := yparser.ParseBytes(b, 0); err != nil {
					t.Errorf("output does not re-parse as YAML: %v", err)
				}
			}
			want := strings.TrimSpace(tc.out)
			qt.Assert(t, qt.Equals(got, want))
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
	}, {
		// [cue.Value.Syntax] emits embedded expressions bare.
		in: ast.NewList(
			ast.NewString("first"),
			&ast.StructLit{Elts: []ast.Decl{
				ast.NewString("second"),
				&ast.Attribute{Text: `@yaml(,tag="!Env")`},
			}},
		),
		out: `
- first
- !Env second
		`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := Encode(tc.in, EncodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(b))
			want := strings.TrimSpace(tc.out)
			qt.Assert(t, qt.Equals(got, want))
		})
	}
}
