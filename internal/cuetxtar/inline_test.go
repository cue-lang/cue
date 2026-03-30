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
