// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cuetxtar

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"golang.org/x/tools/txtar"
)

// stubError implements cueerrors.Error with a fixed Msg() return.
type stubError struct {
	format string
	args   []any
}

func (s stubError) Position() token.Pos         { return token.NoPos }
func (s stubError) InputPositions() []token.Pos { return nil }
func (s stubError) Error() string               { return fmt.Sprintf(s.format, s.args...) }
func (s stubError) Path() []string              { return nil }
func (s stubError) Msg() (string, []any)        { return s.format, s.args }
func TestCheckMsgArgs(t *testing.T) {
	path := testMakePath("field")
	tests := []struct {
		name     string
		args     []any    // actual Msg() args
		expected []string // expected strings passed to checkMsgArgs
		wantFail bool
	}{
		{
			name:     "exact match passes",
			args:     []any{"list", "int"},
			expected: []string{"list", "int"},
		},
		{
			name:     "subset passes — one of two args",
			args:     []any{"list", "int"},
			expected: []string{"list"},
		},
		{
			name:     "subset passes — other arg",
			args:     []any{"list", "int"},
			expected: []string{"int"},
		},
		{
			name:     "order-independent — reversed",
			args:     []any{"list", "int"},
			expected: []string{"int", "list"},
		},
		{
			name:     "extra actual args are allowed",
			args:     []any{"list", "int", "extra"},
			expected: []string{"list", "int"},
		},
		{
			name:     "missing expected arg fails",
			args:     []any{"list"},
			expected: []string{"list", "int"},
			wantFail: true,
		},
		{
			name:     "empty expected always passes",
			args:     []any{"list", "int"},
			expected: nil,
		},
		{
			name:     "no actual args with non-empty expected fails",
			args:     nil,
			expected: []string{"list"},
			wantFail: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &failCapture{TB: t}
			checkMsgArgs(rec, path, stubError{args: tc.args}, tc.expected, "@test(err, args=...)", "")
			if rec.failed != tc.wantFail {
				if tc.wantFail {
					t.Errorf("expected failure but checkMsgArgs passed")
				} else {
					t.Errorf("unexpected failure:\n%s", rec.msgs.String())
				}
			}
		})
	}
}

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
			name:  "multiple relative comma-separated",
			input: "[0:5, 1:13, -2:3]",
			want: []posSpec{
				{deltaLine: 0, col: 5},
				{deltaLine: 1, col: 13},
				{deltaLine: -2, col: 3},
			},
		},
		{
			name:    "multiple relative whitespace-only is rejected",
			input:   "[0:5 1:13 -2:3]",
			wantErr: true,
		},
		{
			name:  "absolute form",
			input: "[fixture.cue:3:5]",
			want:  []posSpec{{fileName: "fixture.cue", absLine: 3, col: 5}},
		},
		{
			name:  "mixed relative and absolute",
			input: "[0:5, fixture.cue:1:13]",
			want: []posSpec{
				{deltaLine: 0, col: 5},
				{fileName: "fixture.cue", absLine: 1, col: 13},
			},
		},
		{
			name:    "mixed relative and absolute whitespace-only is rejected",
			input:   "[0:5 fixture.cue:1:13]",
			wantErr: true,
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
		{
			name:  "trailing comma only",
			input: "[0:5,]",
			want:  []posSpec{{deltaLine: 0, col: 5}},
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
			archive: "-- test.cue --\nx: 1 & 2 @test(err, pos=[0:4, 0:8])\n",
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
			archive: "-- test.cue --\nx: {\n\ta: 1\n\ta: 2\n} @test(err, pos=[1:5, 2:5])\n",
		},
		{
			// Decl-attribute form inside a struct.
			// Source (no stripping — @test attr is kept):
			//   line 1: x: {
			//   line 2: 	@test(err, pos=[2:5, 3:5])
			//   line 3: 	a: 1
			//   line 4: 	a: 2
			//   line 5: }
			// baseLine = sl.Lbrace.Line() = 1 (the "{" on line 1).
			// deltaLine=2 → line 3 (a: 1), deltaLine=3 → line 4 (a: 2).
			name:    "decl attr pos relative",
			archive: "-- test.cue --\nx: {\n\t@test(err, pos=[2:5, 3:5])\n\ta: 1\n\ta: 2\n}\n",
		},
		{
			// Decl-attribute at file-level with a conflict.
			// Source (no stripping — @test attr is kept):
			//   line 1: @test(err, pos=[1:4, 1:8])
			//   line 2: x: 1 & 2
			// baseLine = a.Pos().Line() = 1 (original line of @test).
			// deltaLine=1 → line 2 (x: 1 & 2).
			// Positions: 2:4 and 2:8.
			name:    "file-level decl attr pos relative",
			archive: "-- test.cue --\n@test(err, pos=[1:4, 1:8])\nx: 1 & 2\n",
		},
		{
			// Multiple fields: second field's baseLine accounts for
			// the stripped @test on the first field.
			// Original:
			//   line 1: x: 1 @test(eq, 1)
			//   line 2: y: 1 & 2 @test(err, pos=[0:4, 0:8])
			// After stripping (both on same line, no extra newlines):
			//   line 1: x: 1
			//   line 2: y: 1 & 2
			// baseLine for y = 2, deltaLine=0 → line 2.
			name:    "field attr after prior field attr",
			archive: "-- test.cue --\nx: 1 @test(eq, 1)\ny: 1 & 2 @test(err, pos=[0:4, 0:8])\n",
		},
		{
			// Absolute position form: filename:absLine:col.
			// After stripping, "test.cue" has "x: 1 & 2" on line 1.
			name:    "absolute pos form",
			archive: "-- test.cue --\nx: 1 & 2 @test(err, pos=[test.cue:1:4, test.cue:1:8])\n",
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

// TestRunShareIDChecks_Negative verifies that runShareIDChecks correctly
// reports an error when two paths do NOT share the same vertex.
// This cannot be expressed as a txtar inline test (we can't annotate "should
// fail"), so it is tested here by calling runShareIDChecks directly with a
// failCapture that captures errors without propagating to the parent test.
func TestRunShareIDChecks_Negative(t *testing.T) {
	ctx := cuecontext.New()
	r := &inlineRunner{}
	t.Run("shared vertices pass", func(t *testing.T) {
		// b: a creates vertex sharing; both paths should deref to the same node.
		v := ctx.CompileString("a: {x: 1}\nb: a")
		rec := &failCapture{TB: t}
		r.runShareIDChecks(rec, v, map[string][]cue.Path{
			"AB": {cue.MakePath(cue.Str("a")), cue.MakePath(cue.Str("b"))},
		})
		if rec.failed {
			t.Errorf("expected shared vertices to pass, got errors:\n%s", rec.msgs.String())
		}
	})
	t.Run("independent structs fail", func(t *testing.T) {
		// a and b are independently defined; they must not share a vertex.
		v := ctx.CompileString("a: {x: 1}\nb: {x: 1}")
		rec := &failCapture{TB: t}
		r.runShareIDChecks(rec, v, map[string][]cue.Path{
			"AB": {cue.MakePath(cue.Str("a")), cue.MakePath(cue.Str("b"))},
		})
		if !rec.failed {
			t.Errorf("expected independent structs to fail shareID check, but it passed")
		}
	})
	t.Run("list element via at=0 shared passes", func(t *testing.T) {
		// l: [a] makes l[0] the same vertex as a.
		v := ctx.CompileString("a: {x: 1}\nl: [a]")
		rec := &failCapture{TB: t}
		r.runShareIDChecks(rec, v, map[string][]cue.Path{
			"EL": {
				cue.MakePath(cue.Str("l"), cue.Index(0)),
				cue.MakePath(cue.Str("a")),
			},
		})
		if rec.failed {
			t.Errorf("expected l[0] and a to be shared, got errors:\n%s", rec.msgs.String())
		}
	})
	t.Run("list element literal not shared fails", func(t *testing.T) {
		// l: [{x: 1}] is a literal; no sharing with a.
		v := ctx.CompileString("a: {x: 1}\nl: [{x: 1}]")
		rec := &failCapture{TB: t}
		r.runShareIDChecks(rec, v, map[string][]cue.Path{
			"EL": {
				cue.MakePath(cue.Str("l"), cue.Index(0)),
				cue.MakePath(cue.Str("a")),
			},
		})
		if !rec.failed {
			t.Errorf("expected list literal element to fail shareID check, but it passed")
		}
	})
}

// TestShareIDPathAliasing is a regression test for a cue.Path backing-array
// aliasing bug in appendPath. When a parent path has excess backing capacity,
// cue.Path.Append reuses the same underlying array for all children, so a
// later sibling's append overwrites position len(parent) for all previously
// stored paths. The result: every @test(shareID=...) record for the same
// parent ends up with the path of the last child visited.
//
// The test verifies that collectShareIDsForRoot records the correct path for
// each annotated sibling — not the last one — by checking that the collected
// paths match the expected field names.
func TestShareIDPathAliasing(t *testing.T) {
	// The aliasing requires the parent path to have excess backing capacity.
	// Go doubles slice capacity on growth: len 2 → cap 4 at the third append.
	// So a path of length 3 (e.g. "p.q.r") has cap 4, meaning all children
	// of "p.q.r" share backing-array index 3 without the fix.
	//
	// Four siblings under "p.q.r". @test(shareID=AB) is on "a" and "b" (not
	// the last field "d"), so without the fix both records end up with path
	// "p.q.r.d" instead of "p.q.r.a" / "p.q.r.b".
	src := `p: q: r: {
	a: {x: 1}         @test(shareID=AB)
	b: a               @test(shareID=AB)
	c: {x: 2}
	d: {x: 3}
}`
	f, err := parser.ParseFile("test.cue", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	records := extractTestAttrs(f, "test.cue")

	r := &inlineRunner{}
	rootPath := cue.MakePath(cue.Str("p"), cue.Str("q"), cue.Str("r"))
	groups := r.collectShareIDsForRoot(records, rootPath, "v3")

	paths, ok := groups["AB"]
	if !ok {
		t.Fatal("shareID group 'AB' not found")
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths in group AB, got %d: %v", len(paths), paths)
	}
	want := []string{"p.q.r.a", "p.q.r.b"}
	got := []string{paths[0].String(), paths[1].String()}
	if !slices.Equal(got, want) {
		t.Errorf("path aliasing: got %v, want %v\n"+
			"(if both paths show 'root.d', appendPath is aliasing sibling paths)",
			got, want)
	}
}

// TestAtDirective verifies that @test(err, at=<path>, ...) navigates to a
// sub-path before checking the error.
func TestAtDirective(t *testing.T) {
	t.Run("at= navigates to sub-field error", func(t *testing.T) {
		ctx := cuecontext.New()
		parent := ctx.CompileString("a: {b: int & string}")
		if parent.LookupPath(cue.MakePath(cue.Str("a"), cue.Str("b"))).Err() == nil {
			t.Fatal("expected a.b to be an error")
		}
		r := &inlineRunner{}
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "err", errArgs: &errArgs{at: "a.b"}}
		r.runErrAssertion(rec, cue.MakePath(cue.Str("x")), parent, pa)
		if rec.failed {
			t.Errorf("unexpected failure: %s", rec.msgs.String())
		}
	})
	t.Run("at= missing sub-path fails", func(t *testing.T) {
		ctx := cuecontext.New()
		val := ctx.CompileString("a: 1")
		r := &inlineRunner{}
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "err", errArgs: &errArgs{at: "a.nonexistent"}}
		r.runErrAssertion(rec, cue.MakePath(cue.Str("x")), val, pa)
		if !rec.failed {
			t.Errorf("expected failure for missing sub-path")
		}
	})
	t.Run("at= sub-path not an error fails", func(t *testing.T) {
		ctx := cuecontext.New()
		val := ctx.CompileString("a: {b: 42}")
		r := &inlineRunner{}
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "err", errArgs: &errArgs{at: "a.b"}}
		r.runErrAssertion(rec, cue.MakePath(cue.Str("x")), val, pa)
		if !rec.failed {
			t.Errorf("expected failure when sub-path is not an error")
		}
	})
}

// TestRunAllowsAssertion verifies that @test(allows, sel) and @test(allows=false, sel)
// correctly report failures when the assertion is wrong.
func TestRunAllowsAssertion(t *testing.T) {
	ctx := cuecontext.New()
	r := &inlineRunner{}
	path := cue.MakePath(cue.Str("x"))

	t.Run("allows fails when field is not allowed in closed struct", func(t *testing.T) {
		val := ctx.CompileString("x: close({a: 1})")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows, b)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if !rec.failed {
			t.Errorf("expected failure: closed struct should not allow field b")
		}
	})

	t.Run("allows=false fails when field is actually allowed", func(t *testing.T) {
		val := ctx.CompileString("x: close({a: 1})")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows=false, a)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if !rec.failed {
			t.Errorf("expected failure: closed struct should allow known field a")
		}
	})

	t.Run("allows fails for int pattern in closed string-keyed struct", func(t *testing.T) {
		val := ctx.CompileString("x: close({[string]: 1})")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows, int)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if !rec.failed {
			t.Errorf("expected failure: string-keyed struct should not allow int pattern")
		}
	})

	t.Run("allows errors on scalar value", func(t *testing.T) {
		val := ctx.CompileString("x: 42")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows, foo)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if !rec.failed {
			t.Errorf("expected failure: @test(allows) should error on scalar value")
		}
	})

	t.Run("allows passes for AnyIndex on open list", func(t *testing.T) {
		val := ctx.CompileString("x: [...]")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows, int)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if rec.failed {
			t.Errorf("unexpected failure: open list should allow int indices\n%s", rec.msgs.String())
		}
	})

	t.Run("allows passes for open struct", func(t *testing.T) {
		val := ctx.CompileString("x: {a: 1}")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows, b)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if rec.failed {
			t.Errorf("unexpected failure: open struct should allow any field\n%s", rec.msgs.String())
		}
	})

	t.Run("allows=false passes for unknown field in closed struct", func(t *testing.T) {
		val := ctx.CompileString("x: close({a: 1})")
		rec := &failCapture{TB: t}
		pa := parsedTestAttr{directive: "allows", raw: internal.ParseAttr(&ast.Attribute{Text: "@test(allows=false, b)"})}
		r.runAllowsAssertion(rec, path, val.LookupPath(path), pa)
		if rec.failed {
			t.Errorf("unexpected failure: closed struct should deny unknown field b\n%s", rec.msgs.String())
		}
	})
}

// makeTestPos creates a token.Pos at the given 1-indexed line and column in
// a fresh file with the given name. Each line is allocated lineWidth bytes.
func makeTestPos(filename string, line, col int) token.Pos {
	const lineWidth = 100
	size := line*lineWidth + col
	f := token.NewFile(filename, 0, size)
	for i := 1; i < line; i++ {
		f.AddLine(i * lineWidth)
	}
	return f.Pos((line-1)*lineWidth+(col-1), token.Blank)
}

// TestPosSpecsMatch verifies that posSpecsMatch is order-independent.
func TestPosSpecsMatch(t *testing.T) {
	identity := func(s string) string { return s }
	// Two positions in "in.cue": line 5 col 3 and line 7 col 1.
	pos5_3 := makeTestPos("in.cue", 5, 3)
	pos7_1 := makeTestPos("in.cue", 7, 1)
	// Absolute specs for the same positions (baseLine is irrelevant for absolute).
	spec5_3 := posSpec{fileName: "in.cue", absLine: 5, col: 3}
	spec7_1 := posSpec{fileName: "in.cue", absLine: 7, col: 1}
	t.Run("same order matches", func(t *testing.T) {
		if !posSpecsMatch([]token.Pos{pos5_3, pos7_1}, []posSpec{spec5_3, spec7_1}, 0, identity) {
			t.Error("expected match in same order")
		}
	})
	t.Run("reversed order matches", func(t *testing.T) {
		if !posSpecsMatch([]token.Pos{pos5_3, pos7_1}, []posSpec{spec7_1, spec5_3}, 0, identity) {
			t.Error("expected match with reversed spec order")
		}
	})
	t.Run("wrong position does not match", func(t *testing.T) {
		pos9_2 := makeTestPos("in.cue", 9, 2)
		if posSpecsMatch([]token.Pos{pos5_3, pos9_2}, []posSpec{spec5_3, spec7_1}, 0, identity) {
			t.Error("expected no match for wrong position")
		}
	})
	t.Run("count mismatch does not match", func(t *testing.T) {
		if posSpecsMatch([]token.Pos{pos5_3}, []posSpec{spec5_3, spec7_1}, 0, identity) {
			t.Error("expected no match for count mismatch")
		}
	})
	t.Run("empty positions and specs match", func(t *testing.T) {
		if !posSpecsMatch(nil, nil, 0, identity) {
			t.Error("expected empty slices to match")
		}
	})
}

func TestHintFlag(t *testing.T) {
	parseAttr := func(src string) (parsedTestAttr, error) {
		f, err := parser.ParseFile("test.cue", src)
		if err != nil {
			return parsedTestAttr{}, err
		}
		field := f.Decls[0].(*ast.Field)
		return parseTestAttr(field.Attrs[0])
	}

	t.Run("hint= is parsed into pa.hint", func(t *testing.T) {
		pa, err := parseAttr(`x: 1 @test(eq, 42, hint="fix the evaluator")`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pa.hint != "fix the evaluator" {
			t.Errorf("got hint=%q, want %q", pa.hint, "fix the evaluator")
		}
	})

	t.Run("hint= works on err directive", func(t *testing.T) {
		pa, err := parseAttr(`x: 1 @test(err, code=eval, hint="check the cycle")`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pa.hint != "check the cycle" {
			t.Errorf("got hint=%q, want %q", pa.hint, "check the cycle")
		}
	})

	t.Run("unknown flag rejected for eq", func(t *testing.T) {
		_, err := parseAttr(`x: 1 @test(eq, 42, foo="bar")`)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("expected unknown flag error, got: %v", err)
		}
	})

	t.Run("unknown flag rejected for err", func(t *testing.T) {
		_, err := parseAttr(`x: 1 @test(err, unknownKey=x)`)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("expected unknown flag error, got: %v", err)
		}
	})

	t.Run("suberr without = is rejected", func(t *testing.T) {
		_, err := parseAttr(`x: 1 @test(err, suberr(code=eval, contains="foo"))`)
		if err == nil || !strings.Contains(err.Error(), "missing '='") {
			t.Errorf("expected missing '=' error, got: %v", err)
		}
	})

	t.Run("no hint= gives empty hint", func(t *testing.T) {
		pa, err := parseAttr(`x: 1 @test(eq, 42)`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pa.hint != "" {
			t.Errorf("expected empty hint, got %q", pa.hint)
		}
	})
}

// TestUnreachableTestAttr verifies that @test directives placed inside struct
// literals that are binary-expression operands (e.g. X & {@test(...)}) are
// detected and reported as errors rather than silently ignored.
func TestUnreachableTestAttr(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantErrFrag string // non-empty means a parseErr record with this substring is expected
	}{
		{
			name:        "decl attr in conjunction operand",
			src:         "f: X & {\n\t@test(eq, 1)\n}\n",
			wantErrFrag: "not reachable by the test runner",
		},
		{
			name:        "field attr in conjunction operand",
			src:         "f: X & {\n\tv: 1 @test(eq, 1)\n}\n",
			wantErrFrag: "not reachable by the test runner",
		},
		{
			name:        "decl attr in bare embedding conjunction",
			src:         "f: 1\nX & {\n\t@test(eq, 1)\n}\n",
			wantErrFrag: "not reachable by the test runner",
		},
		{
			// @test as a field attr after the full expression must NOT be flagged.
			name: "field attr after conjunction is valid",
			src:  "f: X & {} @test(eq, 1)\n",
		},
		{
			// Decl @test inside a plain struct (not a binary operand) is valid.
			name: "decl attr in plain struct is valid",
			src:  "f: {\n\t@test(eq, {v: 1})\n\tv: 1\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parser.ParseFile("test.cue", tt.src)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			records := extractTestAttrs(f, "test.cue")

			var errMsgs []string
			for _, r := range records {
				if r.parseErr != nil {
					errMsgs = append(errMsgs, r.parseErr.Error())
				}
			}

			if tt.wantErrFrag == "" {
				if len(errMsgs) > 0 {
					t.Errorf("expected no parse errors, got: %v", errMsgs)
				}
				return
			}
			if len(errMsgs) == 0 {
				t.Errorf("expected a parse error containing %q, got none", tt.wantErrFrag)
				return
			}
			for _, msg := range errMsgs {
				if strings.Contains(msg, tt.wantErrFrag) {
					return
				}
			}
			t.Errorf("no error message contains %q; got: %v", tt.wantErrFrag, errMsgs)
		})
	}
}
