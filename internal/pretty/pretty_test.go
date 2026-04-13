// Copyright 2026 CUE Authors
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

package pretty_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/pretty"
)

func parseExpr(t *testing.T, src string) *pretty.Doc {
	t.Helper()
	expr, err := parser.ParseExpr("test", src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return pretty.Node(expr)
}

// checkExprIdempotent verifies that for the given source, formatting the output
// of format(parse(src)) re-parses without error and produces identical output.
// It checks at the given widths (defaulting to 80 if none provided).
func checkExprIdempotent(t *testing.T, src string, widths ...int) {
	t.Helper()
	if len(widths) == 0 {
		widths = []int{80}
	}
	for _, w := range widths {
		f1 := pretty.Render(w, parseExpr(t, src))

		// f1 must re-parse.
		expr2, err := parser.ParseExpr("reparse", f1)
		if err != nil {
			t.Errorf("width=%d: re-parse failed: %v\nformatted output:\n%s", w, err, f1)
			continue
		}

		// format(parse(f1)) must equal f1.
		f2 := pretty.Render(w, pretty.Node(expr2))
		if f1 != f2 {
			t.Errorf("width=%d: not idempotent:\nf1: %s\nf2: %s", w, f1, f2)
		}
	}
}

// checkExprIdempotentIndent is like checkExprIdempotent but with a specific Indent.
func checkExprIdempotentIndent(t *testing.T, src string, ind pretty.Indent, widths ...int) {
	t.Helper()
	if len(widths) == 0 {
		widths = []int{80}
	}
	for _, w := range widths {
		doc := parseExpr(t, src)
		f1 := pretty.RenderIndent(w, ind, doc)

		expr2, err := parser.ParseExpr("reparse", f1)
		if err != nil {
			t.Errorf("width=%d: re-parse failed: %v\nformatted output:\n%s", w, err, f1)
			continue
		}

		f2 := pretty.RenderIndent(w, ind, pretty.Node(expr2))
		if f1 != f2 {
			t.Errorf("width=%d: not idempotent:\nf1: %s\nf2: %s", w, f1, f2)
		}
	}
}

// checkFileIdempotent is like checkExprIdempotent but for full CUE files.
func checkFileIdempotent(t *testing.T, src string, widths ...int) {
	t.Helper()
	if len(widths) == 0 {
		widths = []int{80}
	}
	for _, w := range widths {
		f, err := parser.ParseFile("test.cue", src)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		f1 := pretty.Render(w, pretty.Node(f))

		f2parsed, err := parser.ParseFile("reparse.cue", f1)
		if err != nil {
			t.Errorf("width=%d: re-parse failed: %v\nformatted output:\n%s", w, err, f1)
			continue
		}

		f2 := pretty.Render(w, pretty.Node(f2parsed))
		if f1 != f2 {
			t.Errorf("width=%d: not idempotent:\nf1:\n%s\nf2:\n%s", w, f1, f2)
		}
	}
}

func TestBasicScalars(t *testing.T) {
	tests := []struct {
		cue  string
		want string
	}{
		{`null`, `null`},
		{`true`, `true`},
		{`false`, `false`},
		{`42`, `42`},
		{`3.14`, `3.14`},
		{`"hello"`, `"hello"`},
		{`_|_`, `_|_`},
		{`_`, `_`},
	}
	for _, tt := range tests {
		doc := parseExpr(t, tt.cue)
		got := pretty.Render(80, doc)
		if got != tt.want {
			t.Errorf("Node(%s): got %q, want %q", tt.cue, got, tt.want)
		}
		checkExprIdempotent(t, tt.cue)
	}
}

func TestSmallStruct(t *testing.T) {
	src := `{a: 1, b: "hello"}`
	doc := parseExpr(t, src)

	got80 := pretty.Render(80, doc)
	t.Logf("width=80:\n%s", got80)

	got15 := pretty.Render(15, doc)
	t.Logf("width=15:\n%s", got15)

	if got80 == got15 {
		t.Error("expected different layouts for width=80 vs width=15")
	}

	checkExprIdempotent(t, src, 80, 15)
}

func TestNestedStruct(t *testing.T) {
	src := `{
		name: "server"
		config: {
			port: 8080
			host: "localhost"
			tls: {
				enabled: true
				cert: "/etc/ssl/cert.pem"
			}
		}
		tags: ["production", "us-east-1", "critical"]
	}`
	doc := parseExpr(t, src)

	wide := pretty.Render(120, doc)
	narrow := pretty.Render(40, doc)

	fmt.Printf("=== width=120 ===\n%s\n", wide)
	fmt.Printf("=== width=40 ===\n%s\n", narrow)

	checkExprIdempotent(t, src, 120, 40)
}

func TestList(t *testing.T) {
	src := `[1, 2, 3, "four", "five"]`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	t.Logf("list: %s", got)

	checkExprIdempotent(t, src, 80, 20)
}

func TestConstraints(t *testing.T) {
	src := `{
		name: string
		port: int & >0 & <=65535
		debug: bool | *false
	}`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	fmt.Printf("=== constraints ===\n%s\n", got)

	checkExprIdempotent(t, src, 80, 30)
}

func TestBinaryExpr(t *testing.T) {
	src := `a & b | c`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	t.Logf("binary: %s", got)
	if got != "a & b | c" {
		t.Errorf("got %q", got)
	}

	checkExprIdempotent(t, src)
}

func TestUnaryExpr(t *testing.T) {
	src := `!true`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	if got != "!true" {
		t.Errorf("got %q, want %q", got, "!true")
	}

	checkExprIdempotent(t, src)
}

func TestSelector(t *testing.T) {
	src := `foo.bar.baz`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	if got != "foo.bar.baz" {
		t.Errorf("got %q", got)
	}

	checkExprIdempotent(t, src)
}

func TestCallExpr(t *testing.T) {
	src := `strings.Join(list, ", ")`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	t.Logf("call: %s", got)

	checkExprIdempotent(t, src, 80, 15)
}

func TestComprehension(t *testing.T) {
	src := `{for x in items { (x.name): x.value }}`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	fmt.Printf("=== comprehension ===\n%s\n", got)

	checkExprIdempotent(t, src, 80, 30)
}

func TestFile(t *testing.T) {
	src := `package config

import "strings"

name: "myapp"
port: 8080
env: {
	HOME: string
	PATH: string
}
tags: ["a", "b", "c"]
`
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatal(err)
	}
	doc := pretty.Node(f)

	wide := pretty.Render(80, doc)
	narrow := pretty.Render(30, doc)

	fmt.Printf("=== file width=80 ===\n%s\n", wide)
	fmt.Printf("=== file width=30 ===\n%s\n", narrow)

	checkFileIdempotent(t, src, 80, 30)
}

func TestDefinition(t *testing.T) {
	src := `{
		#Schema: {
			name: string
			port: int
		}
		config: #Schema & {
			name: "myapp"
			port: 8080
		}
	}`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	fmt.Printf("=== definition ===\n%s\n", got)

	checkExprIdempotent(t, src, 80, 30)
}

func TestInterpolation(t *testing.T) {
	src := `"hello \(name), you are \(age) years old"`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	t.Logf("interpolation: %s", got)

	checkExprIdempotent(t, src)
}

func TestOptionalFields(t *testing.T) {
	src := `{
		name: string
		port?: int
		debug?: bool
	}`
	doc := parseExpr(t, src)
	got := pretty.Render(80, doc)
	fmt.Printf("=== optional ===\n%s\n", got)

	checkExprIdempotent(t, src, 80, 20)
}

func TestIndentSpaces(t *testing.T) {
	src := `{a: 1, b: "hello"}`
	doc := parseExpr(t, src)

	got2 := pretty.RenderIndent(15, pretty.Indent{Spaces: 2}, doc)
	got8 := pretty.RenderIndent(15, pretty.Indent{Spaces: 8}, doc)

	want2 := "{\n  a: 1,\n  b: \"hello\"\n}"
	want8 := "{\n        a: 1,\n        b: \"hello\"\n}"

	if got2 != want2 {
		t.Errorf("2-space:\ngot  %q\nwant %q", got2, want2)
	}
	if got8 != want8 {
		t.Errorf("8-space:\ngot  %q\nwant %q", got8, want8)
	}

	checkExprIdempotentIndent(t, src, pretty.Indent{Spaces: 2}, 80, 15)
	checkExprIdempotentIndent(t, src, pretty.Indent{Spaces: 8}, 80, 15)
}

func TestIndentTab(t *testing.T) {
	src := `{a: 1, b: "hello"}`
	doc := parseExpr(t, src)

	got := pretty.RenderIndent(15, pretty.Indent{UseTab: true}, doc)
	want := "{\n\ta: 1,\n\tb: \"hello\"\n}"

	if got != want {
		t.Errorf("tab:\ngot  %q\nwant %q", got, want)
	}

	checkExprIdempotentIndent(t, src, pretty.Indent{UseTab: true}, 80, 15)
}

func TestPostfixAlias(t *testing.T) {
	// Postfix aliases require @experiment(aliasv2) and file-level parsing.
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "simple",
			src:  "@experiment(aliasv2)\na~X: 1",
			want: "@experiment(aliasv2)\na~X: 1",
		},
		{
			name: "dual",
			src:  "@experiment(aliasv2)\na~(K,V): 1",
			want: "@experiment(aliasv2)\na~(K,V): 1",
		},
		{
			name: "with optional",
			src:  "@experiment(aliasv2)\na~X?: 1",
			want: "@experiment(aliasv2)\na~X?: 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parser.ParseFile("test.cue", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			doc := pretty.Node(f)
			got := pretty.Render(80, doc)
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}

			// Idempotency check.
			checkFileIdempotent(t, tt.src, 80)
		})
	}
}

func TestTryElse(t *testing.T) {
	// try/else requires @experiment(try) and file-level parsing.
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "if else",
			src: `@experiment(try)
a: {
	if true { x: 1 } else { y: 2 }
}`,
			want: `@experiment(try)
a: { if true { x: 1 } else { y: 2 } }`,
		},
		{
			name: "for otherwise",
			src: `@experiment(try)
a: {
	for x in [] { "\(x)": x } otherwise { empty: true }
}`,
			want: `@experiment(try)
a: { for x in [] { "\(x)": x } otherwise { empty: true } }`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parser.ParseFile("test.cue", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			doc := pretty.Node(f)
			got := pretty.Render(80, doc)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			checkFileIdempotent(t, tt.src, 80)
		})
	}
}

func TestAlignment(t *testing.T) {
	src := `{
		x:         1
		name:      "hello"
		longLabel: true
	}`
	doc := parseExpr(t, src)

	// At width=80, fits on one line (no alignment needed).
	got80 := pretty.Render(80, doc)
	want80 := `{ x: 1, name: "hello", longLabel: true }`
	if got80 != want80 {
		t.Errorf("width=80:\ngot:  %s\nwant: %s", got80, want80)
	}

	// At width=30, must break — labels should be padded so values align.
	got30 := pretty.Render(30, doc)
	want30 := "{\n    x:         1,\n    name:      \"hello\",\n    longLabel: true\n}"
	if got30 != want30 {
		t.Errorf("width=30:\ngot:\n%s\nwant:\n%s", got30, want30)
	}

	checkExprIdempotent(t, src, 80, 30)
}

func TestAlignmentNoBlockPad(t *testing.T) {
	src := `{
		x:      1
		name:   "hello"
		config: { a: 1 }
		tags:   [1, 2, 3]
		y:      true
	}`
	doc := parseExpr(t, src)
	got := pretty.Render(30, doc)
	// x, name, y should align with each other.
	// config (struct value) and tags (list value) should NOT be padded.
	// config's single-field struct is flattened to chained-label syntax.
	want := "{\n" +
		"    x:    1,\n" +
		"    name: \"hello\",\n" +
		"    config: a: 1,\n" +
		"    tags: [ 1, 2, 3 ],\n" +
		"    y:    true\n" +
		"}"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	checkExprIdempotent(t, src, 30)
}

func TestSingleFieldFlattening(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "one level",
			src:  `{ a: { b: 4 } }`,
			want: `{ a: b: 4 }`,
		},
		{
			name: "two levels",
			src:  `{ a: { b: { c: 4 } } }`,
			want: `{ a: b: c: 4 }`,
		},
		{
			name: "no flatten multi-field",
			src:  `{ a: { b: 1, c: 2 } }`,
			want: `{ a: { b: 1, c: 2 } }`,
		},
		{
			name: "mixed",
			src:  `{ a: { b: { c: 1 } }, d: 2 }`,
			want: `{ a: b: c: 1, d: 2 }`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseExpr(t, tt.src)
			got := pretty.Render(80, doc)
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
			checkExprIdempotent(t, tt.src, 80, 20)
		})
	}
}

func TestEpic(t *testing.T) {
	src := `d: a: a: a: b: 4
out: {
	a: {
		a: {
			a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
			b: 4
		} | {
			a: {
				a: a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
				b: 4
			} | {
				a: a: {
					a: {b: 4} | {a: a: a: b: 4}
					b: 4
				} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
					b: 4
				} | {a: a: {a: b: 4
					b: 4
				} | {a: {a: a: b: 4
					b: 4
				}
				}}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
		b: 4
	} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
		b: 4
	} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	}}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}
		b: 4
	} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	} | {a: a: {a: b: 4
		b: 4
	}}}
		b: 4
	} | {a: {a: {a: b: 4
		b: 4
	} | {a: {a: a: b: 4
		b: 4
	}}
		b: 4
	} | {a: {a: {a: b: 4
		b: 4
	}
		b: 4
	} | {a: {a: {a: b: 4
		b: 4
	}
		b: 4
	}}}}}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: b: 4
	b: 4
} | {a: {a: a: b: 4
	b: 4
}}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: {b: 4} | {a: a: a: b: 4}}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: a: {a: b: 4
	b: 4
} | {a: {a: a: b: 4
	b: 4
}}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}
	b: 4
} | {a: {a: {a: {b: 4} | {a: a: a: b: 4}
	b: 4
} | {a: {a: a: b: 4
	b: 4
} | {a: a: {a: b: 4
	b: 4
}}}
	b: 4
} | {a: {a: {a: b: 4
	b: 4
} | {a: {a: a: b: 4
	b: 4
}}
	b: 4
} | {a: {a: {a: b: 4
	b: 4
}
	b: 4
} | {a: {a: {a: b: 4
	b: 4
}
	b: 4
}}}}}}}}}}
`
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatal(err)
	}
	doc := pretty.Node(f)
	got := pretty.RenderIndent(80, pretty.Indent{Spaces: 2}, doc)
	fmt.Printf("=== epic ===\n%s\n", got)

	checkFileIdempotent(t, src, 80, 8)
}
