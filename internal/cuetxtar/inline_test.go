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
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
)

// testMakePath creates a CUE path from a dot-separated string for test use.
func testMakePath(s string) cue.Path {
	if s == "" {
		return cue.MakePath()
	}
	parts := strings.Split(s, ".")
	sels := make([]cue.Selector, len(parts))
	for i, p := range parts {
		sels[i] = cue.Str(p)
	}
	return cue.MakePath(sels...)
}

// parseFirstFieldAttr parses a CUE source string, retrieves the first field's
// first @test attribute, and calls parseTestAttr on it.
func parseFirstFieldAttr(t *testing.T, src string) (*ast.Attribute, parsedTestAttr) {
	t.Helper()
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Decls) == 0 {
		t.Fatal("no decls")
	}
	field, ok := f.Decls[0].(*ast.Field)
	if !ok {
		t.Fatalf("first decl is not a field: %T", f.Decls[0])
	}
	if len(field.Attrs) == 0 {
		t.Fatal("no attrs on field")
	}
	pa, err := parseTestAttr(field.Attrs[0])
	if err != nil {
		t.Fatalf("parseTestAttr: %v", err)
	}
	return field.Attrs[0], pa
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests for Section 1: Attribute Parsing Utilities
// ─────────────────────────────────────────────────────────────────────────────

func TestParseTestAttr_SimpleDirectives(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantDir string
		wantVer string
	}{
		{"eq", `x: 1 @test(eq, 42)`, "eq", ""},
		{"eq versioned", `x: 1 @test(eq:v3, 42)`, "eq", "v3"},
		{"err bare", `x: 1 @test(err)`, "err", ""},
		{"kind", `x: 1 @test(kind=int)`, "kind", ""},
		{"closed", `x: 1 @test(closed)`, "closed", ""},
		{"leq", `x: 1 @test(leq, int)`, "leq", ""},
		{"skip", `x: 1 @test(skip)`, "skip", ""},
		{"permute", `x: 1 @test(permute)`, "permute", ""},
		{"empty", `x: 1 @test()`, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, pa := parseFirstFieldAttr(t, tt.src)
			if pa.directive != tt.wantDir {
				t.Errorf("directive: got %q, want %q", pa.directive, tt.wantDir)
			}
			if pa.version != tt.wantVer {
				t.Errorf("version: got %q, want %q", pa.version, tt.wantVer)
			}
		})
	}
}

func TestParseTestAttr_ErrSubOptions(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantAny     bool
		wantCodes   []string
		wantContain string
		wantPaths   []string
	}{
		{
			name:    "bare err",
			src:     `x: 1 @test(err)`,
			wantAny: false,
		},
		{
			name:      "err with code",
			src:       `x: 1 @test(err, code=cycle)`,
			wantCodes: []string{"cycle"},
		},
		{
			name:      "err structural cycle",
			src:       `x: 1 @test(err, code="structural cycle")`,
			wantCodes: []string{"structural cycle"},
		},
		{
			name:    "err any",
			src:     `x: 1 @test(err, any)`,
			wantAny: true,
		},
		{
			name:      "err any with code",
			src:       `x: 1 @test(err, any, code=cycle)`,
			wantAny:   true,
			wantCodes: []string{"cycle"},
		},
		{
			name:      "err multi-code",
			src:       `x: 1 @test(err, code=(cycle|incomplete))`,
			wantCodes: []string{"cycle", "incomplete"},
		},
		{
			name:        "err contains",
			src:         `x: 1 @test(err, contains="cannot use")`,
			wantContain: "cannot use",
		},
		{
			name:      "err path",
			src:       `x: 1 @test(err, path=(a|b|c))`,
			wantPaths: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, pa := parseFirstFieldAttr(t, tt.src)
			if pa.directive != "err" {
				t.Fatalf("directive: got %q, want %q", pa.directive, "err")
			}
			if pa.errArgs == nil {
				t.Fatal("errArgs is nil")
			}
			ea := pa.errArgs
			if ea.any != tt.wantAny {
				t.Errorf("any: got %v, want %v", ea.any, tt.wantAny)
			}
			if !slices.Equal(ea.codes, tt.wantCodes) {
				t.Errorf("codes: got %v, want %v", ea.codes, tt.wantCodes)
			}
			if ea.contains != tt.wantContain {
				t.Errorf("contains: got %q, want %q", ea.contains, tt.wantContain)
			}
			if !slices.Equal(ea.paths, tt.wantPaths) {
				t.Errorf("paths: got %v, want %v", ea.paths, tt.wantPaths)
			}
		})
	}
}

func TestParseTestAttr_ShareId(t *testing.T) {
	src := `x: _ @test(shareId=myGroup)`
	_, pa := parseFirstFieldAttr(t, src)
	if pa.directive != "shareId" {
		t.Fatalf("directive: got %q, want %q", pa.directive, "shareId")
	}
	if len(pa.raw.Fields) == 0 {
		t.Fatal("no fields in parsed attr")
	}
	if pa.raw.Fields[0].Key() != "shareId" {
		t.Errorf("key: got %q, want %q", pa.raw.Fields[0].Key(), "shareId")
	}
	if pa.raw.Fields[0].Value() != "myGroup" {
		t.Errorf("value: got %q, want %q", pa.raw.Fields[0].Value(), "myGroup")
	}
}

func TestParsePosSpecs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []posSpec
		wantErr bool
	}{
		{
			name:  "empty brackets",
			input: "[]",
			want:  nil,
		},
		{
			name:  "single relative",
			input: "[0:5]",
			want:  []posSpec{{deltaLine: 0, col: 5}},
		},
		{
			name:  "multiple relative with whitespace",
			input: "[0:5 1:13 -2:3]",
			want: []posSpec{
				{deltaLine: 0, col: 5},
				{deltaLine: 1, col: 13},
				{deltaLine: -2, col: 3},
			},
		},
		{
			name:  "absolute form",
			input: "[fixture.cue:3:5]",
			want:  []posSpec{{fileName: "fixture.cue", absLine: 3, col: 5}},
		},
		{
			name:  "mixed relative and absolute",
			input: "[0:5 fixture.cue:1:13]",
			want: []posSpec{
				{deltaLine: 0, col: 5},
				{fileName: "fixture.cue", absLine: 1, col: 13},
			},
		},
		{
			name:    "missing brackets",
			input:   "0:5",
			wantErr: true,
		},
		{
			name:    "four parts",
			input:   "[a:b:c:d]",
			wantErr: true,
		},
		{
			name:    "non-integer deltaLine",
			input:   "[x:5]",
			wantErr: true,
		},
		{
			name:    "non-integer col",
			input:   "[0:x]",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePosSpecs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (result: %v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d specs, want %d: %v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if g != w {
					t.Errorf("spec[%d]: got %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestParseParenList(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"(a|b|c)", []string{"a", "b", "c"}},
		{"a|b|c", []string{"a", "b", "c"}},
		{"(a)", []string{"a"}},
		{"", nil},
		{"( a | b )", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseParenList(tt.input)
			if err != nil {
				t.Fatalf("parseParenList: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests for Section 2: AST Extraction and Stripping
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractTestAttrs_InlineForm(t *testing.T) {
	src := `
myField: 42 @test(eq, 42) @json("x")
other: "hello"
`
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	records := extractTestAttrs(f, "test.cue")

	// Should have one record for myField's @test attr.
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	r := records[0]
	if r.parsed.directive != "eq" {
		t.Errorf("directive: got %q, want %q", r.parsed.directive, "eq")
	}
	if r.path.String() != "myField" {
		t.Errorf("path: got %q, want %q", r.path.String(), "myField")
	}

	// Verify @json attr is preserved on myField.
	field := f.Decls[0].(*ast.Field)
	if len(field.Attrs) != 1 {
		t.Errorf("expected 1 attr after stripping, got %d", len(field.Attrs))
	} else {
		k, _ := field.Attrs[0].Split()
		if k != "json" {
			t.Errorf("remaining attr: got @%s, want @json", k)
		}
	}

	// Verify @test attr is stripped.
	for _, a := range field.Attrs {
		if k, _ := a.Split(); k == "test" {
			t.Error("@test attr was not stripped")
		}
	}
}

func TestExtractTestAttrs_DeclAttrStripping(t *testing.T) {
	src := `
myTest: {
	@test(desc="my test")
	@test(err, code=cycle)
	x: 1
	y: 2 @test(eq, 2)
}
`
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	records := extractTestAttrs(f, "test.cue")

	// Should have: desc decl attr, err decl attr, and y's eq field attr.
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}

	// The struct literal should have its decl attrs stripped.
	topField := f.Decls[0].(*ast.Field)
	structLit := topField.Value.(*ast.StructLit)
	for _, elt := range structLit.Elts {
		if a, ok := elt.(*ast.Attribute); ok {
			if k, _ := a.Split(); k == "test" {
				t.Error("@test decl attr should be stripped from struct literal")
			}
		}
	}
}

// TestExtractTestAttrs_DeclAttrOnNestedStruct verifies that a @test decl attr
// placed inside a sub-field's struct literal is extracted and stripped.
func TestExtractTestAttrs_DeclAttrOnNestedStruct(t *testing.T) {
	src := `
myTest: {
	@test()
	in: {
		@test(permute)
		a: b + 1
		b: 2
	}
	eq: {a: 3, b: 2}
}
`
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	records := extractTestAttrs(f, "test.cue")

	// Should have the container @test() and the in @test(permute).
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2; records: %v", len(records),
			func() []string {
				var s []string
				for _, r := range records {
					s = append(s, r.parsed.directive+"@"+r.path.String())
				}
				return s
			}())
	}

	// Find the permute record.
	var permuteRec *attrRecord
	for i := range records {
		if records[i].parsed.directive == "permute" {
			permuteRec = &records[i]
			break
		}
	}
	if permuteRec == nil {
		t.Fatal("no permute record found")
	}
	if permuteRec.path.String() != "myTest.in" {
		t.Errorf("permute path: got %q, want %q", permuteRec.path.String(), "myTest.in")
	}

	// Verify the @test(permute) decl attr is stripped from in's struct literal.
	topField := f.Decls[0].(*ast.Field)
	containerLit := topField.Value.(*ast.StructLit)
	var inField *ast.Field
	for _, elt := range containerLit.Elts {
		if f2, ok := elt.(*ast.Field); ok && identStr(f2.Label) == "in" {
			inField = f2
			break
		}
	}
	if inField == nil {
		t.Fatal("in field not found")
	}
	inLit := inField.Value.(*ast.StructLit)
	for _, elt := range inLit.Elts {
		if a, ok := elt.(*ast.Attribute); ok {
			if k, _ := a.Split(); k == "test" {
				t.Error("@test decl attr should be stripped from in's struct literal")
			}
		}
	}
}

func TestExtractTestAttrs_PreservesNonTestAttrs(t *testing.T) {
	src := `x: 1 @json("x") @test(eq, 1) @proto(1)`
	f, err := parser.ParseFile("test.cue", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	extractTestAttrs(f, "test.cue")

	field := f.Decls[0].(*ast.Field)
	var attrNames []string
	for _, a := range field.Attrs {
		k, _ := a.Split()
		attrNames = append(attrNames, k)
	}

	// @json and @proto should be preserved; @test should be stripped.
	if slices.Contains(attrNames, "test") {
		t.Error("@test attr should be stripped")
	}
	if !slices.Contains(attrNames, "json") {
		t.Error("@json attr should be preserved")
	}
	if !slices.Contains(attrNames, "proto") {
		t.Error("@proto attr should be preserved")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests for Section 3: Mode Detection
// ─────────────────────────────────────────────────────────────────────────────

func TestIsInlineMode(t *testing.T) {
	tests := []struct {
		name    string
		archive *txtar.Archive
		want    bool
	}{
		{
			name:    "inline field attr",
			archive: txtar.Parse([]byte("-- test.cue --\nx: 42 @test(eq, 42)\n")),
			want:    true,
		},
		{
			name:    "structural decl attr",
			archive: txtar.Parse([]byte("-- test.cue --\nmyTest: {\n\t@test(err)\n\tx: 1\n}\n")),
			want:    true,
		},
		{
			// @test(permute) inside `in: { ... }` — two levels below file root.
			name:    "permute decl attr in sub-field struct",
			archive: txtar.Parse([]byte("-- test.cue --\nmyTest: {\n\tin: {\n\t\t@test(permute)\n\t\ta: 1\n\t}\n}\n")),
			want:    true,
		},
		{
			// @test attribute three levels deep: a: { b: { c: { @test(...) } } }.
			name:    "decl attr three levels deep",
			archive: txtar.Parse([]byte("-- test.cue --\na: {\n\tb: {\n\t\tc: {\n\t\t\t@test(permute)\n\t\t\tx: 1\n\t\t}\n\t}\n}\n")),
			want:    true,
		},
		{
			name:    "no test attrs",
			archive: txtar.Parse([]byte("-- test.cue --\nx: 42\ny: \"hello\"\n")),
			want:    false,
		},
		{
			name:    "only golden file output",
			archive: txtar.Parse([]byte("-- test.cue --\nx: 42\n-- out/evalalpha --\nx: 42\n")),
			want:    false,
		},
		{
			name:    "non-test @attr",
			archive: txtar.Parse([]byte("-- test.cue --\nx: 42 @json(\"x\")\n")),
			want:    false,
		},
		{
			// @test(ignore) as field attr: still an inline-mode archive.
			name:    "ignore field attr triggers inline mode",
			archive: txtar.Parse([]byte("-- test.cue --\nx: 42 @test(ignore)\n")),
			want:    true,
		},
		{
			// @test(ignore) as decl attr: still an inline-mode archive.
			name:    "ignore decl attr triggers inline mode",
			archive: txtar.Parse([]byte("-- test.cue --\nx: {\n\t@test(ignore)\n\ta: 1\n}\n")),
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInlineMode(tt.archive)
			if got != tt.want {
				t.Errorf("isInlineMode: got %v, want %v", got, tt.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests for Section 6.7: findPermFieldsAtPath helper
// ─────────────────────────────────────────────────────────────────────────────

func TestFindPermFieldsAtPath(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		path       string
		fieldNames []string // nil means all fields
		wantCount  int
		wantNames  []string
	}{
		{
			name:      "all fields in top-level struct",
			src:       "myStruct: {\n\ta: 1\n\tb: 2\n\tc: 3\n}",
			path:      "myStruct",
			wantCount: 3,
			wantNames: []string{"a", "b", "c"},
		},
		{
			name:       "named subset of fields",
			src:        "myStruct: {\n\ta: 1\n\tb: 2\n\tc: 3\n}",
			path:       "myStruct",
			fieldNames: []string{"a", "c"},
			wantCount:  2,
			wantNames:  []string{"a", "c"},
		},
		{
			name:      "nested path",
			src:       "outer: {\n\tinner: {\n\t\tx: 1\n\t\ty: 2\n\t}\n}",
			path:      "outer.inner",
			wantCount: 2,
			wantNames: []string{"x", "y"},
		},
		{
			name:      "missing path returns nil",
			src:       "outer: {\n\tinner: {\n\t\tx: 1\n\t}\n}",
			path:      "outer.missing",
			wantCount: 0,
		},
		{
			name:      "non-struct field returns nil",
			src:       "outer: 42",
			path:      "outer",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parser.ParseFile("test.cue", tt.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			structLit, indices := findPermFieldsAtPath(f, testMakePath(tt.path), tt.fieldNames)

			if tt.wantCount == 0 {
				if len(indices) != 0 {
					t.Errorf("want 0 indices, got %d", len(indices))
				}
				return
			}
			if structLit == nil {
				t.Fatal("structLit is nil, want non-nil")
			}
			if len(indices) != tt.wantCount {
				t.Errorf("got %d indices, want %d", len(indices), tt.wantCount)
				return
			}
			// Verify the field names at the returned indices.
			var gotNames []string
			for _, idx := range indices {
				if f2, ok := structLit.Elts[idx].(*ast.Field); ok {
					gotNames = append(gotNames, identStr(f2.Label))
				}
			}
			if len(gotNames) != len(tt.wantNames) {
				t.Errorf("got field names %v, want %v", gotNames, tt.wantNames)
				return
			}
			for i, name := range tt.wantNames {
				if gotNames[i] != name {
					t.Errorf("field[%d]: got %q, want %q", i, gotNames[i], name)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests for Section 5: Version Discrimination
// ─────────────────────────────────────────────────────────────────────────────

func makeRec(path string, directive, version string) attrRecord {
	return attrRecord{
		path: testMakePath(path),
		parsed: parsedTestAttr{
			directive: directive,
			version:   version,
		},
	}
}

func TestSelectActiveDirectives(t *testing.T) {
	tests := []struct {
		name     string
		records  []attrRecord
		path     string
		version  string
		wantDirs []string
	}{
		{
			name:     "unversioned applies to all",
			records:  []attrRecord{makeRec("field1", "eq", "")},
			path:     "field1",
			version:  "v3",
			wantDirs: []string{"eq"},
		},
		{
			name: "versioned overrides unversioned",
			records: []attrRecord{
				makeRec("field1", "eq", ""),
				makeRec("field1", "eq", "v3"),
			},
			path:     "field1",
			version:  "v3",
			wantDirs: []string{"eq"}, // only one eq (versioned wins)
		},
		{
			name: "versioned for other version skipped unversioned still applies",
			records: []attrRecord{
				makeRec("field1", "eq", ""),
				makeRec("field1", "eq", "v2"),
			},
			path:     "field1",
			version:  "v3",
			wantDirs: []string{"eq"}, // v2 skipped; unversioned applies
		},
		{
			name:     "wrong path excluded",
			records:  []attrRecord{makeRec("field2", "eq", "")},
			path:     "field1",
			version:  "v3",
			wantDirs: nil,
		},
		{
			name: "multiple directives at same path",
			records: []attrRecord{
				makeRec("field1", "eq", ""),
				makeRec("field1", "err", ""),
			},
			path:     "field1",
			version:  "v3",
			wantDirs: []string{"eq", "err"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectActiveDirectives(tt.records, testMakePath(tt.path), tt.version)
			var gotDirs []string
			for _, pa := range got {
				gotDirs = append(gotDirs, pa.directive)
			}
			slices.Sort(gotDirs)
			wantDirs := append([]string(nil), tt.wantDirs...)
			slices.Sort(wantDirs)
			if !slices.Equal(gotDirs, wantDirs) {
				t.Errorf("got directives %v, want %v", gotDirs, wantDirs)
			}
		})
	}
}

// TestInlineRunner_ErrPos verifies @test(err, pos=[...]) position checking.
func TestInlineRunner_ErrPos(t *testing.T) {
	tests := []struct {
		name    string
		archive string
	}{
		{
			// Field-attribute form: pos on same line as error source.
			// Stripped output: "x: 1 & 2" on line 1.
			// Positions: 1:4 (the 1) and 1:8 (the 2).
			// baseLine=1, deltaLine=0 → expected line 1.
			name:    "field attr pos relative same line",
			archive: "-- test.cue --\nx: 1 & 2 @test(err, pos=[0:4 0:8])\n",
		},
		{
			// Field-attribute on a struct with conflict below.
			// Stripped output:
			//   line 1: x: {
			//   line 2: 	a: 1
			//   line 3: 	a: 2
			//   line 4: }
			// baseLine=1 (field x on line 1), deltas: 1→line 2, 2→line 3.
			name:    "field attr pos relative below",
			archive: "-- test.cue --\nx: {\n\ta: 1\n\ta: 2\n} @test(err, pos=[1:5 2:5])\n",
		},
		{
			// Decl-attribute form inside a struct.
			// Original source:
			//   line 1: x: {
			//   line 2: 	@test(err, pos=[1:5 2:5])
			//   line 3: 	a: 1
			//   line 4: 	a: 2
			//   line 5: }
			// After stripping the @test (line 2 removed):
			//   line 1: x: {
			//   line 2: 	a: 1
			//   line 3: 	a: 2
			//   line 4: }
			// baseLine = sl.Lbrace.Line() - 0 = 1 (the "{" on line 1).
			// deltaLine=1 → line 2 (a: 1), deltaLine=2 → line 3 (a: 2).
			name:    "decl attr pos relative",
			archive: "-- test.cue --\nx: {\n\t@test(err, pos=[1:5 2:5])\n\ta: 1\n\ta: 2\n}\n",
		},
		{
			// Decl-attribute at file-level with a conflict.
			// Original source:
			//   line 1: @test(err, pos=[0:4 0:8])
			//   line 2: x: 1 & 2
			// After stripping the @test (line 1 removed):
			//   line 1: x: 1 & 2
			// baseLine = 1 (original line of @test) - 0 = 1.
			// deltaLine=0 → line 1 (x: 1 & 2).
			// Positions: 1:4 and 1:8.
			name:    "file-level decl attr pos relative",
			archive: "-- test.cue --\n@test(err, pos=[0:4 0:8])\nx: 1 & 2\n",
		},
		{
			// Multiple fields: second field's baseLine accounts for
			// the stripped @test on the first field.
			// Original:
			//   line 1: x: 1 @test(eq, 1)
			//   line 2: y: 1 & 2 @test(err, pos=[0:4 0:8])
			// After stripping (both on same line, no extra newlines):
			//   line 1: x: 1
			//   line 2: y: 1 & 2
			// baseLine for y = 2, deltaLine=0 → line 2.
			name:    "field attr after prior field attr",
			archive: "-- test.cue --\nx: 1 @test(eq, 1)\ny: 1 & 2 @test(err, pos=[0:4 0:8])\n",
		},
		{
			// Absolute position form: filename:absLine:col.
			// After stripping, "test.cue" has "x: 1 & 2" on line 1.
			name:    "absolute pos form",
			archive: "-- test.cue --\nx: 1 & 2 @test(err, pos=[test.cue:1:4 test.cue:1:8])\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := txtar.Parse([]byte(tt.archive))
			runner := NewInlineRunner(t, nil, archive, t.TempDir())
			runner.Run()
		})
	}
}

// withUpdateGoldenFiles temporarily enables the UpdateGoldenFiles flag for
// the duration of f, then restores the original value.  Must not be called
// from parallel sub-tests.
func withUpdateGoldenFiles(force bool, f func()) {
	old := cuetest.UpdateGoldenFiles
	oldForce := cuetest.ForceUpdateGoldenFiles
	cuetest.UpdateGoldenFiles = true
	cuetest.ForceUpdateGoldenFiles = force
	defer func() {
		cuetest.UpdateGoldenFiles = old
		cuetest.ForceUpdateGoldenFiles = oldForce
	}()
	f()
}
