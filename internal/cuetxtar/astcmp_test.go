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

package cuetxtar

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
)

// parseExpr is a test helper that parses a CUE expression string.
func parseExpr(t *testing.T, s string) ast.Expr {
	t.Helper()
	expr, err := parser.ParseExpr("test", s)
	if err != nil {
		t.Fatalf("parseExpr(%q): %v", s, err)
	}
	return expr
}

// compileVal is a test helper that compiles a CUE expression and returns the Value.
func compileVal(t *testing.T, s string) cue.Value {
	t.Helper()
	ctx := cuecontext.New()
	v := ctx.CompileString(s)
	if err := v.Err(); err != nil {
		t.Fatalf("compileVal(%q): %v", s, err)
	}
	return v
}

func TestAstCompare_Scalars(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string // empty means expect match
	}{
		{name: "int negative", expr: "-1", val: "-1"},

		// Integers
		{name: "int match", expr: "42", val: "42"},
		{name: "int zero", expr: "0", val: "0"},
		{name: "int negative", expr: "-1", val: "-1"},
		{name: "int mismatch", expr: "42", val: "43", wantErr: "expected 42, got 43"},
		{name: "int big", expr: "100000000000000000000", val: "100000000000000000000"},
		{name: "int vs string", expr: "42", val: `"42"`, wantErr: `expected 42, got "42"`},

		// Floats
		{name: "float match", expr: "3.14", val: "3.14"},
		{name: "float mismatch", expr: "3.14", val: "2.71", wantErr: "expected 3.14, got 2.71"},

		// Strings
		{name: "string match", expr: `"hello"`, val: `"hello"`},
		{name: "string empty", expr: `""`, val: `""`},
		{name: "string mismatch", expr: `"hello"`, val: `"world"`, wantErr: `expected "hello", got "world"`},
		{name: "string with escapes", expr: `"a\nb"`, val: `"a\nb"`},

		// Booleans
		{name: "true match", expr: "true", val: "true"},
		{name: "false match", expr: "false", val: "false"},
		{name: "bool mismatch", expr: "true", val: "false", wantErr: "expected true"},

		// Null
		{name: "null match", expr: "null", val: "null"},
		{name: "null mismatch", expr: "null", val: "42", wantErr: "expected null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Types(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		{name: "int type", expr: "int", val: "int"},
		{name: "string type", expr: "string", val: "string"},
		{name: "bool type", expr: "bool", val: "bool"},
		{name: "float type", expr: "float", val: "float"},
		{name: "number type", expr: "number", val: "number"},
		{name: "bytes type", expr: "bytes", val: "bytes"},

		// Type mismatch.
		{
			name:    "int vs string",
			expr:    "int",
			val:     "string",
			wantErr: "expected int, got string",
		},

		// Concrete values versus type constraint.
		{
			name:    "concrete int matches int",
			expr:    "int",
			val:     "42",
			wantErr: "expected int, got 42",
		},
		{
			name:    "concrete string matches string",
			expr:    "string",
			val:     `"hello"`,
			wantErr: "expected string, got \"hello\"",
		},

		// Top and bottom.
		{name: "top matches top", expr: "_", val: "_"},
		{
			name:    "top matches struct",
			expr:    "_",
			val:     "{a: 1}",
			wantErr: "expected _, got",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Structs(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		{name: "simple struct", expr: `{b: "x", a: 1}`, val: `{a: 1, b: "x"}`},
		{name: "nested struct", expr: `{a: {b: 1}}`, val: `{a: {b: 1}}`},
		{name: "empty struct", expr: `{}`, val: `{}`},

		// Field value mismatch.
		{name: "field mismatch", expr: `{a: 1}`, val: `{a: 2}`, wantErr: "a: expected 1, got 2"},

		// Missing field.
		{name: "missing field", expr: `{a: 1, b: 2}`, val: `{a: 1}`, wantErr: `field "b" not found`},

		// Extra field.
		{name: "extra field", expr: `{a: 1}`, val: `{a: 1, b: 2}`, wantErr: `unexpected field "b"`},

		// Definitions.
		{name: "definition match", expr: `{#D: 1}`, val: `{#D: 1}`},
		{name: "definition mismatch", expr: `{#D: 1}`, val: `{#D: 2}`, wantErr: "#D: expected 1, got 2"},

		// Mixed regular and definition.
		{name: "mixed fields and defs", expr: `{#D: "x", a: 1}`, val: `{a: 1, #D: "x"}`},

		// Final attribute on fields.
		{
			name:    "without final rejects plain vs disjunction",
			expr:    `{a: 1}`,
			val:     `{a: *1 | 2}`,
			wantErr: "disjunction",
		},

		// Optional fields.
		{name: "optional match", expr: `{foo?: string}`, val: `{foo?: string}`},
		{name: "optional concrete", expr: `{foo?: "bar"}`, val: `{foo?: "bar"}`},
		{
			name:    "optional vs regular mismatch",
			expr:    `{foo?: string}`,
			val:     `{foo: string}`,
			wantErr: `unexpected field "foo"`,
		},
		{
			name:    "regular vs optional mismatch",
			expr:    `{foo: string}`,
			val:     `{foo?: string}`,
			wantErr: `field "foo" not found`,
		},
		{
			name:    "optional value mismatch",
			expr:    `{foo?: string}`,
			val:     `{foo?: int}`,
			wantErr: "foo?: expected string, got int",
		},
		{
			name: "mixed optional and regular",
			expr: `{a: 1, b?: string}`,
			val:  `{a: 1, b?: string}`,
		},

		// Required fields.
		{name: "required match", expr: `{foo!: string}`, val: `{foo!: string}`},
		{
			name:    "required vs regular mismatch",
			expr:    `{foo!: string}`,
			val:     `{foo: string}`,
			wantErr: `unexpected field "foo"`,
		},
		{
			name:    "required vs optional mismatch",
			expr:    `{foo!: string}`,
			val:     `{foo?: string}`,
			wantErr: `unexpected field "foo"`,
		},

		// Nested @test(err) — tested separately below in TestAstCompare_ErrDirective.

		// Hidden fields.
		{name: "hidden match", expr: `{_foo: 1}`, val: `{_foo: 1}`},
		{
			name:    "hidden mismatch",
			expr:    `{_foo: 1}`,
			val:     `{_foo: 2}`,
			wantErr: "_foo: expected 1, got 2",
		},
		{
			name:    "hidden missing from value",
			expr:    `{_foo: 1}`,
			val:     `{}`,
			wantErr: `field "_foo" not found`,
		},
		{
			// Hidden field present in value but absent from expected must be reported.
			// This ensures hidden fields are not silently ignored by the comparison.
			name:    "hidden unexpected in value",
			expr:    `{a: 1}`,
			val:     `{_foo: 42, a: 1}`,
			wantErr: `unexpected field "_foo" in value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_ErrDirective(t *testing.T) {
	ctx := cuecontext.New()

	// Helper: compile a struct and return the whole value (errors allowed).
	compile := func(s string) cue.Value { return ctx.CompileString(s) }

	// Use a struct where field b has an error but the struct itself is valid.
	errStruct := compile(`{b: 1, a: null & string}`)

	t.Run("bare err on error field", func(t *testing.T) {
		expr := parseExpr(t, "{\nb: 1\na: _|_ @test(err)\n}")
		checkErr(t, astCompare(expr, errStruct), "")
	})
	t.Run("err with code", func(t *testing.T) {
		expr := parseExpr(t, "{\nb: 1\na: _|_ @test(err, code=eval)\n}")
		checkErr(t, astCompare(expr, errStruct), "")
	})
	t.Run("err on non-error field", func(t *testing.T) {
		expr := parseExpr(t, "{\nb: 1 @test(err)\na: _|_ @test(err)\n}")
		checkErr(t, astCompare(expr, errStruct), "@test(err): expected error")
	})
	t.Run("err wrong code", func(t *testing.T) {
		expr := parseExpr(t, "{\nb: 1\na: _|_ @test(err, code=incomplete)\n}")
		checkErr(t, astCompare(expr, errStruct), "@test(err): expected error code")
	})
}

func TestAstCompare_Lists(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		{name: "simple list", expr: `[1, 2, 3]`, val: `[1, 2, 3]`},
		{name: "string list", expr: `["a", "b"]`, val: `["a", "b"]`},
		{name: "nested list", expr: `[[1, 2], [3, 4]]`, val: `[[1, 2], [3, 4]]`},
		{name: "empty list", expr: `[]`, val: `[]`},

		// Length mismatch.
		{name: "too few elements", expr: `[1, 2, 3]`, val: `[1, 2]`, wantErr: "expected 3 elements, value has 2"},
		{name: "too many elements", expr: `[1, 2]`, val: `[1, 2, 3]`, wantErr: "expected 2 elements, value has 3"},

		// Element mismatch.
		{name: "element mismatch", expr: `[1, 2, 3]`, val: `[1, 99, 3]`, wantErr: "[1]: expected 2, got 99"},

		// Mixed types.
		{name: "mixed types", expr: `[1, "two", true]`, val: `[1, "two", true]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Disjunctions(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		// Simple disjunction.
		{name: "two values", expr: `1 | 2`, val: `1 | 2`},
		{name: "three values", expr: `1 | 2 | 3`, val: `1 | 2 | 3`},
		{name: "string disj", expr: `"a" | "b"`, val: `"a" | "b"`},

		// Order independence.
		{name: "order independent", expr: `2 | 1`, val: `1 | 2`},
		{name: "three reversed", expr: `3 | 2 | 1`, val: `1 | 2 | 3`},

		// Default markers.
		{
			name: "default match",
			expr: `*1 | 2`,
			val:  `*1 | 2`,
		},
		{
			name:    "plain vs disjunction rejected",
			expr:    `1`,
			val:     `*1 | 2`,
			wantErr: "value is a disjunction but expected expression is not",
		},
		{
			name: "plain vs disjunction accepted with field final",
			expr: "{\na: 1 @test(final)\n}",
			val:  `{a: *1 | 2}`,
		},
		{
			name: "plain vs disjunction accepted with decl final",
			expr: "{\n@test(final)\na: 1\n}",
			val:  `{a: *1 | 2}`,
		},
		{
			name:    "final with wrong default",
			expr:    "{\na: 2 @test(final)\n}",
			val:     `{a: *1 | 2}`,
			wantErr: "a: expected 2, got 1",
		},

		// Disjunction count mismatch.
		{
			name:    "count mismatch",
			expr:    `1 | 2 | 3`,
			val:     `1 | 2`,
			wantErr: "expected 3 disjunct(s), got 2",
		},
		{
			name:    "count mismatch default",
			expr:    `*1 | 2 | 3`,
			val:     `*1 | 2`,
			wantErr: "expected 3 disjunct(s), got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Bounds(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		// Unary bounds.
		{name: "geq match", expr: `>=0`, val: `>=0`},
		{name: "leq match", expr: `<=100`, val: `<=100`},
		{name: "lt match", expr: `<10`, val: `<10`},
		{name: "gt match", expr: `>0`, val: `>0`},
		{name: "neq match", expr: `{a: !=null}`, val: `{a: !=null}`},

		// Conjunctions of bounds.
		{name: "range", expr: `>=0 & <=100`, val: `>=0 & <=100`},
		{name: "type and bound", expr: `int & >=0`, val: `int & >=0`},
		{name: "simplifies", expr: `=~"c"`, val: `!="b" & =~"c"`},
		{
			name: "simplifies to conjunction",
			expr: `=~"c" & =~"d"`,
			val:  `!="b" & =~"c" & =~"d"`,
		},
		// Note: int & >=0 & <=100 simplifies to >=0 & <=100 after evaluation,
		// so we cannot test a triple conjunction with redundant int.
		{name: "conjunction order independent", expr: `<=100 & >=0`, val: `>=0 & <=100`},

		// Conjunction structural consistency.
		{
			name:    "plain vs conjunction rejected",
			expr:    `int`,
			val:     `int & >=0`,
			wantErr: "value is a conjunction",
		},
		{
			name: "field final with conjunction",
			expr: "{\na: int & >=0 @test(final)\n}",
			val:  `a: int & >=0`,
		},
		{
			name: "decl final with conjunction",
			expr: `{int & >=0, @test(final)}`,
			val:  `int & >=0`,
		},
		{
			name:    "conjunction count mismatch",
			expr:    `>=0 & <=100`,
			val:     `int & >=0`,
			wantErr: "conjunct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_PatternConstraints(t *testing.T) {
	ctx := cuecontext.New()

	t.Run("simple pattern", func(t *testing.T) {
		// {[string]: int} with a concrete field.
		val := ctx.CompileString(`{[string]: int, a: 1}`)
		if err := val.Err(); err != nil {
			t.Fatal(err)
		}
		expr := parseExpr(t, `{[string]: int, a: 1}`)
		err := astCompare(expr, val)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestAstCompare_Complex(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		{
			name: "struct with list",
			expr: `{a: [1, 2], b: "x"}`,
			val:  `{a: [1, 2], b: "x"}`,
		},
		{
			name: "deeply nested",
			expr: `{a: {b: {c: 42}}}`,
			val:  `{a: {b: {c: 42}}}`,
		},
		{
			name: "struct with disjunction",
			expr: `{x: 1 | 2}`,
			val:  `{x: 1 | 2}`,
		},
		{
			name:    "deep mismatch",
			expr:    `{a: {b: {c: 42}}}`,
			val:     `{a: {b: {c: 99}}}`,
			wantErr: "a.b.c: expected 42, got 99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_CheckOrder(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		{
			name: "order matches",
			expr: "{\n@test(checkOrder)\na: 1\nb: 2\n}",
			val:  `{a: 1, b: 2}`,
		},
		{
			name:    "order mismatch",
			expr:    "{\n@test(checkOrder)\na: 1\nb: 2\n}",
			val:     `{b: 2, a: 1}`,
			wantErr: `checkOrder: field 0: expected "a", got "b"`,
		},
		{
			name: "without checkOrder order is ignored",
			expr: `{b: 2, a: 1}`,
			val:  `{a: 1, b: 2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Attributes(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		// Attribute match.
		{
			name: "single attribute match",
			expr: `{a: 1 @foo(bar)}`,
			val:  `{a: 1 @foo(bar)}`,
		},
		{
			name: "multiple attributes match",
			expr: "{\na: 1 @foo(x) @bar(y)\n}",
			val:  "{\na: 1 @foo(x) @bar(y)\n}",
		},
		// Attribute order is irrelevant: AST has @bar then @foo,
		// value has @foo then @bar.
		{
			name: "attribute order irrelevant",
			expr: "{\na: 1 @bar(y) @foo(x)\n}",
			val:  "{\na: 1 @foo(x) @bar(y)\n}",
		},
		// Missing attribute.
		{
			name:    "attribute missing from value",
			expr:    `{a: 1 @foo(bar)}`,
			val:     `{a: 1}`,
			wantErr: "@foo",
		},
		// Content mismatch.
		{
			name:    "attribute content mismatch",
			expr:    `{a: 1 @foo(bar)}`,
			val:     `{a: 1 @foo(baz)}`,
			wantErr: "expected attribute @foo(bar), not found",
		},
		// Empty vs non-empty contents.
		{
			name:    "attribute empty vs non-empty",
			expr:    `{a: 1 @foo()}`,
			val:     `{a: 1 @foo(bar)}`,
			wantErr: "expected attribute @foo(), not found",
		},
		// Unexpected attribute in value.
		{
			name:    "unexpected attribute in value",
			expr:    `{a: 1}`,
			val:     `{a: 1 @foo(bar)}`,
			wantErr: "unexpected attribute @foo",
		},
		// Multiple attributes with same key.
		{
			name: "multiple same-key attrs match",
			expr: "{\na: 1 @foo() @foo(other)\n}",
			val:  "{\na: 1 @foo() @foo(other)\n}",
		},
		{
			name: "multiple same-key attrs order independent",
			expr: "{\na: 1 @foo(other) @foo()\n}",
			val:  "{\na: 1 @foo() @foo(other)\n}",
		},
		{
			name:    "multiple same-key attrs missing one",
			expr:    "{\na: 1 @foo() @foo(other)\n}",
			val:     "{\na: 1 @foo()\n}",
			wantErr: "expected attribute @foo(other), not found",
		},
		// @test attributes are excluded from comparison.
		{
			name: "test attribute ignored",
			expr: "{\na: 1 @test(final)\n}",
			val:  `{a: *1 | 2}`,
		},
		// Non-@test attribute alongside @test attribute.
		{
			name: "non-test attr with test attr",
			expr: "{\na: 1 @foo(x) @test(final)\n}",
			val:  `{a: *1 | 2 @foo(x)}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Parens(t *testing.T) {
	expr := parseExpr(t, `(42)`)
	val := compileVal(t, `42`)
	if err := astCompare(expr, val); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAstCompare_LetBindings(t *testing.T) {
	// Note: CUE requires let bindings to be referenced, so test values use a
	// concrete field that references each let (e.g. "a: b" where "let b = 3").
	// Expected structs use the evaluated concrete value (e.g. "a: 3"), not the
	// let identifier, since expected expressions are compared structurally.
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		// Let binding present and value matches.
		{
			name: "let match",
			expr: "{\na: 3\nlet b = 3\n}",
			val:  "{\na: b\nlet b = 3\n}", // b referenced by a
		},
		// Let binding present but value mismatches.
		{
			name:    "let value mismatch",
			expr:    "{\na: 3\nlet b = 4\n}", // expected: let b = 4 (wrong)
			val:     "{\na: b\nlet b = 3\n}", // actual: let b = 3
			wantErr: `"let b": expected 4, got 3`,
		},
		// Expected has a let but value has no let with that name.
		{
			name:    "let missing from value",
			expr:    "{\na: 1\nlet x = 2\n}",
			val:     "{\na: 1\n}",
			wantErr: `let binding "x" not found`,
		},
		// Let with top (_) matches a top value.
		{
			name: "let top matches top",
			expr: "{\na: _\nlet b = _\n}",
			val:  "{\na: b\nlet b = _\n}", // b=top, a=b=top
		},
		// Value has an extra let not listed in expected — not an error.
		{
			name: "extra let in value is allowed",
			expr: "{\na: 3\n}",
			val:  "{\na: b\nlet b = 3\n}",
		},
		// Multiple lets.
		{
			name: "multiple lets match",
			expr: "{\na: 1\nb: 2\nlet x = 1\nlet y = 2\n}",
			val:  "{\na: x\nb: y\nlet x = 1\nlet y = 2\n}",
		},
		{
			name:    "multiple lets one mismatch",
			expr:    "{\na: 1\nb: 2\nlet x = 1\nlet y = 99\n}",
			val:     "{\na: x\nb: y\nlet x = 1\nlet y = 2\n}",
			wantErr: `"let y": expected 99, got 2`,
		},
		// Hidden let (underscore prefix).
		{
			name: "hidden let match",
			expr: "{\na: 3\nlet _b = 3\n}",
			val:  "{\na: _b\nlet _b = 3\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestAstCompare_Ignore(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		val     string
		wantErr string
	}{
		// @test(ignore) on a field skips eq check; field need not be present.
		{
			name: "ignore skips missing field",
			expr: "{\na: 1\nb: _ @test(ignore)\n}",
			val:  "{\na: 1\n}",
		},
		// @test(ignore) on a field skips eq check; field can be present with any value.
		{
			name: "ignore skips field with any value",
			expr: "{\na: 1\nb: _ @test(ignore)\n}",
			val:  "{\na: 1\nb: 99\n}",
		},
		// @test(ignore) does not suppress @test(err) — the err check still runs
		// and fails when the field has no error.
		{
			name:    "ignore does not suppress err check - fails non-error field",
			expr:    "{\nb: _ @test(ignore) @test(err)\n}",
			val:     "{\nb: 3\n}",
			wantErr: "@test(err): expected error",
		},
		// @test(ignore) on a field that would otherwise fail value comparison.
		{
			name: "ignore skips value mismatch",
			expr: "{\na: 1 @test(ignore)\n}",
			val:  "{\na: 999\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseExpr(t, tt.expr)
			val := compileVal(t, tt.val)
			err := astCompare(expr, val)
			checkErr(t, err, tt.wantErr)
		})
	}

	// @test(ignore) does not suppress @test(err) — the err check still passes
	// when the field IS an error. Use a value with an error field (skip top-level error check).
	t.Run("ignore does not suppress err check - passes error field", func(t *testing.T) {
		ctx := cuecontext.New()
		errVal := ctx.CompileString("{\nb: 1 & 2\n}") // b has a conflict error
		expr := parseExpr(t, "{\nb: _|_ @test(ignore) @test(err)\n}")
		checkErr(t, astCompare(expr, errVal), "")
	})
}

// TestCatchInvalidConjuncts verifies @test(eq, ...) that conjunctions are
// valid.
//
// The astCompare implementation mostly assumes that literal values are
// provided that can be checked verbatim. One exception is conjunctions, as
// it is needed for testing validators. We can check each conjunct separately,
// as long as their unification is valid. We do so to avoid having improper
// CUE in the AST, which will be workable, but confusing.
func TestCatchInvalidConjuncts(t *testing.T) {
	// CUE source that produces (string){"s", #a: "s"}: a struct with an
	// embedded #a field whose value is also the struct's scalar.
	const srcField = `x: {
	#a: _
	_
} & {
	#a: "s"
	#a
}`
	t.Run("conjunction form is invalid", func(t *testing.T) {
		// "s" & {#a: "s"} is invalid CUE: {#a: "s"} has no embedded _ so a
		// string literal cannot unify with it. astCompare must return an error
		// for such a conjunction. This cannot be tested via a txtar test file
		// because a failing @test would also fail the outer test run.
		ctx := cuecontext.New()
		val := ctx.CompileString(`{
			#a: _
			_
		} & {
			#a: "s"
			#a
		}`)
		expr, err := parser.ParseExpr("test.cue", `"s" & {#a: "s"}`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := astCompare(expr, val); err == nil {
			t.Error("expected error for invalid conjunction expression, got nil")
		}
	})
}

// checkErr is a test helper that verifies error expectations.
func checkErr(t *testing.T, err error, wantErr string) {
	t.Helper()
	if wantErr == "" {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Errorf("expected error containing %q, got nil", wantErr)
		return
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("error %q does not contain %q", err.Error(), wantErr)
	}
}

// TestShareIDInEqBody verifies the first-occurrence eq-check rule for
// @test(shareID=name) fields in an eq struct body.
func TestShareIDInEqBody(t *testing.T) {
	t.Run("first occurrence runs eq check, mismatch fails", func(t *testing.T) {
		// The first field with shareID=A has value {x: 99} but the actual
		// value of "a" is {x: 1}.  The eq check must run and fail.
		expr := parseExpr(t, `{
			a: {x: 99} @test(shareID=A)
			b: {x: 1}  @test(shareID=A)
		}`)
		val := compileVal(t, `{a: {x: 1}, b: {x: 1}}`)
		err := astCompare(expr, val.LookupPath(cue.MakePath()))
		if err == nil {
			t.Error("expected eq check to fail for first shareID occurrence, but it passed")
		}
	})

	t.Run("second occurrence skips eq check, mismatch ok", func(t *testing.T) {
		// First field matches; second has wrong value but is skipped.
		expr := parseExpr(t, `{
			a: {x: 1}  @test(shareID=A)
			b: {x: 99} @test(shareID=A)
		}`)
		val := compileVal(t, `{a: {x: 1}, b: {x: 1}}`)
		err := astCompare(expr, val.LookupPath(cue.MakePath()))
		if err != nil {
			t.Errorf("expected second shareID occurrence to skip eq check, but got: %v", err)
		}
	})

	t.Run("identifier value in second occurrence is skipped", func(t *testing.T) {
		// Second occurrence uses 'a' as a documentation reference; it is skipped.
		expr := parseExpr(t, `{
			a: {x: 1} @test(shareID=A)
			b: a       @test(shareID=A)
		}`)
		val := compileVal(t, `{a: {x: 1}, b: {x: 1}}`)
		err := astCompare(expr, val.LookupPath(cue.MakePath()))
		if err != nil {
			t.Errorf("identifier as second shareID value should be skipped, but got: %v", err)
		}
	})
}
