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

	t.Run("err:todo still failing does not fail test", func(t *testing.T) {
		// @test(err:todo, ...) where value is not an error: no test failure.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(err:todo, code=eval)\n")
	})

	t.Run("err:todo passing does not fail test", func(t *testing.T) {
		// @test(err:todo) where value IS an error: logs a warning but no failure.
		runExpectPass(t, "-- test.cue --\nx: 1/0 @test(err:todo, code=eval)\n")
	})

	t.Run("err:todo with priority does not fail test", func(t *testing.T) {
		// p= is included in the log but does not affect pass/fail.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(err:todo, p=1, code=eval)\n")
	})

	t.Run("kind:todo still failing does not fail test", func(t *testing.T) {
		// @test(kind:todo=int) where kind is float: no test failure.
		runExpectPass(t, "-- test.cue --\nx: 1.0 @test(kind:todo=int)\n")
	})

	t.Run("kind:todo passing does not fail test", func(t *testing.T) {
		// @test(kind:todo=int) where kind matches: logs a warning but no failure.
		runExpectPass(t, "-- test.cue --\nx: int @test(kind:todo=int)\n")
	})

	t.Run("eq incorrect passing logs note", func(t *testing.T) {
		// @test(eq, X, incorrect) where value matches X: suppresses "this is wrong"
		// feeling by logging a NOTE, but does not fail.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 42, incorrect)\n")
	})

	// Note: a mismatch on an incorrect-flagged assertion fails normally (same as
	// without the flag). The incorrect flag only suppresses the pass case.

	t.Run("eq incorrect and err:todo coexist", func(t *testing.T) {
		// Both document different aspects of the same incorrect-behavior field.
		runExpectPass(t, "-- test.cue --\nx: 42 @test(eq, 42, incorrect) @test(err:todo, p=1, code=eval)\n")
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
			// Package declarations required for cross-file identifier resolution.
			name: "pure fixture file no annotation needed",
			archive: "-- fixture.cue --\n" +
				"package p\n" +
				"fixture: foo: 1\n" +
				"-- test.cue --\n" +
				"package p\n" +
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

// TestInlineRunner_ShareID verifies @test(shareID=name) vertex-sharing
// assertions for the passing (positive) cases. Negative cases (asserting that
// the check correctly rejects non-shared vertices) are in inline_test.go as
// TestRunShareIDChecks_Negative, which calls runShareIDChecks directly with a
// non-propagating failRecorder.
func TestInlineRunner_ShareID(t *testing.T) {
	run := func(t *testing.T, archiveStr string) {
		t.Helper()
		archive := txtar.Parse([]byte(archiveStr))
		runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
		runner.Run()
	}

	t.Run("sharing passes when p and q reference g", func(t *testing.T) {
		run(t, `-- test.cue --
g: {x: 1, y: 2}
p: g @test(shareID=V)
q: g @test(shareID=V)
`)
	})

	t.Run("sharing passes inside eq body", func(t *testing.T) {
		run(t, `-- test.cue --
root: {
	a: {x: 1}
	b: a
	@test(eq, {
		a: {x: 1} @test(shareID=AB)
		b: a       @test(shareID=AB)
	})
}
`)
	})

	t.Run("single annotated path is silently skipped", func(t *testing.T) {
		// A group with a single path needs no second vertex to compare against.
		run(t, `-- test.cue --
a: {x: 1} @test(shareID=SOLO)
`)
	})

	t.Run("sharing passes with at=0 for list element", func(t *testing.T) {
		// l[0] is the same vertex as a because l: [a].
		run(t, `-- test.cue --
a: {x: 1}
l: [a] @test(shareID=EL, at=0)
m: a   @test(shareID=EL)
`)
	})

	t.Run("sharing passes inside eq body with at=0", func(t *testing.T) {
		run(t, `-- test.cue --
root: {
	a: {x: 1}
	l: [a]
	@test(eq, {
		a: {x: 1} @test(shareID=AB)
		l: [a]    @test(shareID=AB, at=0)
	})
}
`)
	})

	t.Run("sharing passes for nested fields across roots", func(t *testing.T) {
		// @test(shareID=...) at depth > 1 participates in cross-root groups.
		// outer1.inner and outer2.inner both reference g, so they share a vertex.
		run(t, `-- test.cue --
g: {x: 1}
outer1: {inner: g @test(shareID=V)}
outer2: {inner: g @test(shareID=V)}
`)
	})

	t.Run("sharing passes with at=selector for dynamic key field", func(t *testing.T) {
		// "dyn_\(k)": def uses an interpolated key so appendPath fails.
		// at=dyn_a gives the resolved field name so the shareID check can
		// look up X.dyn_a in the evaluated value.
		run(t, `-- test.cue --
k: "a"
X: {
	def: {x: 1} @test(shareID=D)
	"dyn_\(k)": def @test(shareID=D, at=dyn_a)
}
`)
	})

	t.Run("sharing passes across different depths", func(t *testing.T) {
		// A top-level field and a nested field may share the same vertex and
		// participate in the same shareID group despite being at different depths.
		run(t, `-- test.cue --
g: {x: 1} @test(shareID=V)
p: g            @test(shareID=V)
outer: {q: g    @test(shareID=V)}
`)
	})
}

// TestInlineRunner_EqAt verifies @test(eq, at=<sel>) sub-path navigation and
// disjunction comparison through shared/forwarding vertices.
func TestInlineRunner_EqAt(t *testing.T) {
	run := func(t *testing.T, archiveStr string) {
		t.Helper()
		archive := txtar.Parse([]byte(archiveStr))
		runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
		runner.Run()
	}

	t.Run("eq with at= navigates to sub-field", func(t *testing.T) {
		// @test(eq, at=b) on a struct checks the b sub-field's value.
		run(t, `-- test.cue --
s: {a: 1, b: 2} @test(eq, 2, at=b)
`)
	})

	t.Run("eq on shared disjunction value", func(t *testing.T) {
		// test_1 is assigned #Dis, which is shared. The value at test_1 is
		// a forwarding vertex pointing to #Dis (a disjunction). DerefValue()
		// is required to reach the *adt.Disjunction base value.
		run(t, `-- test.cue --
#Dis: {x: 1} | {y: 2}
test_1: #Dis @test(shareID=D) @test(eq, {x: 1} | {y: 2})
#Dis: _       @test(shareID=D)
`)
	})
}

// TestInlineRunner_SubErrors verifies @test(err, suberr=(...)) sub-error matching.
func TestInlineRunner_SubErrors(t *testing.T) {
	run := func(t *testing.T, archiveStr string) {
		t.Helper()
		archive := txtar.Parse([]byte(archiveStr))
		runner := cuetxtar.NewInlineRunner(t, nil, archive, t.TempDir())
		runner.Run()
	}

	t.Run("two sub-errors both matched passes", func(t *testing.T) {
		// null | {n: 3} unified with #empty (closed {}) & {n: 3} produces two sub-errors.
		run(t, `-- test.cue --
#empty: {}
x: null | {n: 3}
x: #empty & {n: 3} @test(err, code=eval,
	suberr=(contains="conflicting values"),
	suberr=(contains="not allowed"))
`)
	})

	t.Run("order-independent matching passes", func(t *testing.T) {
		// Specs in reversed order relative to actual sub-errors — should still pass.
		run(t, `-- test.cue --
#empty: {}
x: null | {n: 3}
x: #empty & {n: 3} @test(err, code=eval,
	suberr=(contains="not allowed"),
	suberr=(contains="conflicting values"))
`)
	})
}
