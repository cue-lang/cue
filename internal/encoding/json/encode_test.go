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

package json

import (
	"encoding/json"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"github.com/google/go-cmp/cmp"
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
		out: `{
    "seq": [
        1, 2, 3, {
            "a": 1,
            "b": 2
        }
    ],
    "a": {"b": {"c": 3}},
    "b": {
        "x": 0,
        "y": 1,
        "z": 2
    }
}`,
	}, {
		name: "oneLineFields",
		in: `
		seq: [1, 2, 3]
		esq: []
		emp: {}
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
		out: `{
    "seq": [1, 2, 3],
    "esq": [],
    "emp": {},
    "map": {"a": 3},
    "str": "str",
    "int": 1000,
    "bin": 3,
    "hex": 17,
    "dec": 0.3,
    "dat": "\ufffd",
    "nil": null,
    "yes": true,
    "non": false
}`,
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
		out: `{
    "f1": 1,
    "f2": 2,
    "f3": {
        "a": 1
    },

    "f4": {}
}`,
	}, {
		// TODO: support this at some point
		name: "embed",
		in: `
	// hex
	0xabc // line
	// trail
	`,
		out: `2748`,
	}, {
		name: "anchors",
		in: `
		a: b
		b: 3
		`,
		out: "json: unsupported node b (*ast.Ident)",
	}, {
		name: "errors",
		in: `
			m: {
				a: 1
				b: 3
			}
			c: [1, [ for x in m { x } ]]
			`,
		out: "json: unsupported node for x in m {x} (*ast.Comprehension)",
	}, {
		name: "disallowMultipleEmbeddings",
		in: `
		1
		1
		`,
		out: "json: multiple embedded values",
	}, {
		name: "disallowDefinitions",
		in:   `a :: 2 `,
		out:  "json: definition not allowed",
	}, {
		name: "disallowOptionals",
		in:   `a?: 2`,
		out:  "json: optional fields not allowed",
	}, {
		name: "disallowBulkOptionals",
		in:   `[string]: 2`,
		out:  "json: only literal labels allowed",
	}, {
		name: "noImports",
		in: `
		import "foo"

		a: 1
		`,
		out: `json: unsupported node import "foo" (*ast.ImportDecl)`,
	}, {
		name: "disallowMultipleEmbeddings",
		in: `
		1
		a: 2
		`,
		out: "json: embedding mixed with fields",
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
		out: `{
    "receivers": [{
        "name": "pager",
        "slack_configs": [{
            "text": "{{ range .Alerts }}{{ .Annotations.description }}\n{{ end }}",
            "channel": "#cloudmon",
            "send_resolved": true
        }]
    }],
    "route": {
        "receiver": "pager",
        "group_by": ["alertname", "cluster"]
    }
}`,
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
				if !json.Valid(b) {
					t.Fatal("invalid JSON")
				}
				got = strings.TrimSpace(string(b))
			}
			want := strings.TrimSpace(tc.out)
			if got != want {
				t.Log("\n" + got)
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
		out: `{"field":"value","field2":"value"}`,
	}, {
		in: &ast.StructLit{Elts: []ast.Decl{
			comment("bar"),
			&ast.EmbedDecl{Expr: ast.NewBool(true)},
		}},
		out: `true`,
	}, {
		in: &ast.UnaryExpr{
			Op: token.SUB,
			X:  &ast.BasicLit{Kind: token.INT, Value: "-2"},
		},
		out: `double minus not allowed`,
	}, {
		in:  &ast.BasicLit{Kind: token.INT, Value: "-2.0.0"},
		out: `invalid number "-2.0.0"`,
	}, {
		in: &ast.StructLit{Elts: []ast.Decl{
			&ast.EmbedDecl{Expr: ast.NewBool(true)},
			&ast.Package{},
		}},
		out: `invalid package clause`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := Encode(tc.in)
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
