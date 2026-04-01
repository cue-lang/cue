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
		{
			name:    "eq passes for builtin validator call",
			archive: "-- test.cue --\nimport \"struct\"\nx: struct.MaxFields(2) & {} @test(eq, struct.MaxFields(2) & {})\n",
		},
		{
			name:    "eq passes for selector (math.Pi)",
			archive: "-- test.cue --\nimport \"math\"\nx: math.Pi @test(eq, math.Pi)\n",
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

// TestInlineRunner_Todo verifies @test(todo) and @test(eq:todo) semantics.
func TestInlineRunner_Todo(t *testing.T) {
	// runExpectPass runs the archive and asserts it does NOT fail.
	runExpectPass := func(t *testing.T, archiveStr string) {
		t.Helper()
		archive := txtar.Parse([]byte(archiveStr))
		var failed bool
		t.Run("inner", func(inner *testing.T) {
			runner := cuetxtar.NewInlineRunner(inner, nil, archive, t.TempDir())
			runner.Run()
			failed = inner.Failed()
		})
		if failed {
			t.Errorf("expected test to pass, but it failed")
		}
	}

	t.Run("todo suppresses failing eq", func(t *testing.T) {
		// @test(todo) wraps @test(eq): the eq would normally fail (42 != 99)
		// but the failure is suppressed.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 99) @test(todo)\n")
	})

	t.Run("todo does not fail when eq passes", func(t *testing.T) {
		// @test(todo) with a passing eq: the test passes (with a logged warning).
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 42) @test(todo)\n")
	})

	t.Run("todo with priority and why", func(t *testing.T) {
		runExpectPass(t, `-- test.cue --
x: 42 @test(eq, 99) @test(todo, p=1, why="known issue")
`)
	})

	t.Run("todo suppresses failing err", func(t *testing.T) {
		// @test(todo) wraps @test(err): x=42 is not an error, so @test(err) would fail.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(err) @test(todo)\n")
	})

	t.Run("eq:todo still failing does not fail test", func(t *testing.T) {
		// @test(eq:todo, X) where value doesn't match X: no test failure.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 42) @test(eq:todo, 99)\n")
	})

	t.Run("eq:todo passing does not fail test", func(t *testing.T) {
		// @test(eq:todo, X) where value matches X: logs a warning but no failure.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 42) @test(eq:todo, 42)\n")
	})

	t.Run("eq and eq:todo coexist independently", func(t *testing.T) {
		// @test(eq, 42) passes; @test(eq:todo, 99) fails silently.
		// Both run; test passes overall.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 42) @test(eq:todo, 99)\n")
	})
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

// TestInlineRunner_FixtureField verifies that a field with no @test attribute
// is silently skipped (not run as a sub-test) but is still accessible to other
// test fields in the same archive.
func TestInlineRunner_FixtureField(t *testing.T) {
	tests := []struct {
		name    string
		archive string
	}{
		{
			// Plain field with no @test: not a sub-test but still accessible.
			name: "plain fixture field in same file",
			archive: "-- test.cue --\n" +
				"fixture: {foo: 1}\n" +
				"a: fixture.foo & int @test(eq, 1)\n",
		},
		{
			// Pure fixture file: no @test attrs anywhere in fixture.cue.
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
