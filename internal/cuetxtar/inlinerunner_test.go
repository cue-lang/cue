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

package cuetxtar_test

import (
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/cuetxtar"
)

// TestInlineRunner_Basic verifies end-to-end inline test execution using
// in-memory txtar archives.
func TestInlineRunner_Basic(t *testing.T) {
	tests := []struct {
		name    string
		archive string
	}{
		{
			name:    "eq passes for matching int",
			archive: "-- test.cue --\nx: 42 @test(eq, 42)\n",
		},
		{
			name:    "eq passes for matching string",
			archive: "-- test.cue --\nx: \"hello\" @test(eq, \"hello\")\n",
		},
		{
			name:    "err detects conflict",
			archive: "-- test.cue --\nerrField: 1 & 2 @test(err)\n",
		},
		{
			name:    "kind int passes",
			archive: "-- test.cue --\nx: int @test(kind=int)\n",
		},
		{
			name:    "closed struct passes",
			archive: "-- test.cue --\nx: close({a: 1}) @test(closed)\n",
		},
		{
			name:    "leq passes",
			archive: "-- test.cue --\nx: 42 @test(leq, number)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := txtar.Parse([]byte(tt.archive))
			runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
			runner.Run()
		})
	}
}

// TestInlineRunner_Permute verifies @test(permute) assertions.
func TestInlineRunner_Permute(t *testing.T) {
	tests := []struct {
		name        string
		archive     string
		wantFail    bool // whether the test is expected to fail
		wantSkipped bool // whether there is nothing to permute (< 2 fields)
	}{
		{
			// Field-attribute permute: only a and b are marked; c is not permuted.
			name:    "inline permute two fields passes",
			archive: "-- test.cue --\nmyStruct: {\n\ta: b + 1 @test(permute)\n\tb: 2 @test(permute)\n\tc: 99\n} @test(eq, {a: 3, b: 2, c: 99})\n",
		},
		{
			// Only one field marked with @test(permute): nothing to permute, should be no-op.
			name:        "single permute field skipped",
			archive:     "-- test.cue --\nmyStruct: {a: 1 @test(permute), b: 2} @test(eq, {a: 1, b: 2})\n",
			wantSkipped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := txtar.Parse([]byte(tt.archive))
			if tt.wantFail {
				// Use a sub-test to capture failure.
				var failed bool
				t.Run("inner", func(inner *testing.T) {
					runner := cuetxtar.NewInlineRunner(inner, nil, archive, t.TempDir())
					runner.Run()
					failed = inner.Failed()
				})
				if !failed {
					t.Errorf("expected permute assertion to fail, but it passed")
				}
				return
			}
			runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
			runner.Run()
		})
	}
}

// TestInlineRunner_SubPath verifies #subpath restricts execution.
func TestInlineRunner_SubPath(t *testing.T) {
	// When #subpath is set, only the named test runs.
	archive := txtar.Parse([]byte("#subpath: second\n-- test.cue --\nfirst: 1 @test(eq, 999)\nsecond: 2 @test(eq, 2)\n"))
	// The "first" test has a wrong eq (999 != 1) but should be skipped.
	// The "second" test should pass.
	runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
	runner.Run()
}

// TestInlineRunner_Ignore verifies that @test(ignore) placed as a field
// attribute on a top-level field suppresses the coverage check and does NOT
// create a sub-test for that field.
func TestInlineRunner_Ignore(t *testing.T) {
	tests := []struct {
		name        string
		archive     string
		wantSubtest string // if set, a sub-test with this name must run; if empty, "fixture" must NOT appear
	}{
		{
			// @test(ignore) on the fixture field itself (field attribute form).
			// fixture: {foo: 1} @test(ignore) — attribute on fixture, not on foo.
			// No "fixture" sub-test should appear; coverage is suppressed.
			name: "ignore on top-level field",
			archive: "-- test.cue --\n" +
				"fixture: {foo: 1} @test(ignore)\n" +
				"a: fixture.foo & int @test(eq, 1)\n",
		},
		{
			// Pure fixture file: no @test attrs anywhere in fixture.cue.
			// Its fields are compiled into the value but exempt from coverage.
			name: "pure fixture file no annotation needed",
			archive: "-- fixture.cue --\n" +
				"fixture: foo: 1\n" +
				"-- test.cue --\n" +
				"a: fixture.foo & int @test(eq, 1)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := txtar.Parse([]byte(tt.archive))
			runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
			runner.Run()
		})
	}
}
