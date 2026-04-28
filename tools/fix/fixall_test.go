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

package fix

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/mod/modfile"
	"golang.org/x/tools/txtar"
)

func TestInstances(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "fixmod",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.Instances("./...")
		opts := []Option{}
		if exp, ok := t.Value("exp"); ok {
			opts = append(opts, Experiments(strings.Split(exp, ",")...))
		}
		if str, ok := t.Value("upgrade"); ok {
			opts = append(opts, UpgradeVersion(str))
		}
		if t.HasTag("removelistcommas") {
			opts = append(opts, RemoveListCommas())
		}
		err := Instances(a, opts...)
		t.WriteErrors(err)

		// Collect formatted fixed files for golden output and inline testing.
		fixedFiles := make(map[string][]byte)
		for _, b := range a {
			// Output module file if it exists and was potentially modified
			if b.ModuleFile != nil {
				if data, err := modfile.Format(b.ModuleFile); err == nil {
					fmt.Fprintln(t, "---", "cue.mod/module.cue")
					fmt.Fprint(t, string(data))
					fixedFiles["cue.mod/module.cue"] = data
				}
			}
			for _, f := range b.Files {
				b, _ := format.Node(f)
				fmt.Fprintln(t, "---", t.Rel(f.Filename))
				fmt.Fprint(t, string(b))
				fixedFiles[t.Rel(f.Filename)] = b
			}
		}

		// If any input CUE file has @test annotations, verify that the
		// assertions pass on both the original and the fixed output.
		// This ensures the fix is semantics-preserving.
		runInlineTests(t.T, t.Archive, t.Dir, fixedFiles)
	})
}

// runInlineTests runs @test assertions on both the original archive and
// on a post-fix archive built from fixedFiles. Skipped if the archive
// has no @test annotations (making this a no-op for existing tests).
func runInlineTests(t *testing.T, archive *txtar.Archive, dir string, fixedFiles map[string][]byte) {
	t.Helper()

	if !archiveHasTestAttrs(archive) {
		return
	}

	// Run @test assertions on the original (pre-fix) archive.
	t.Run("pre-fix", func(t *testing.T) {
		cap := &cuetxtar.FailCapture{TB: t}
		runner := cuetxtar.NewInlineRunnerCapture(t, nil, archive, dir, cap)
		runner.Run()
		if cap.Failed() {
			t.Errorf("@test assertions failed on original (pre-fix) input:\n%s", cap.Messages())
		}
	})

	// Build a post-fix archive with the fixed CUE files.
	postFixArchive := buildPostFixArchive(archive, fixedFiles)

	// Run @test assertions on the fixed output.
	t.Run("post-fix", func(t *testing.T) {
		cap := &cuetxtar.FailCapture{TB: t}
		runner := cuetxtar.NewInlineRunnerCapture(t, nil, postFixArchive, dir, cap)
		runner.Run()
		if cap.Failed() {
			t.Errorf("@test assertions failed on fixed output:\n%s", cap.Messages())
		}
	})
}

// TestFixSemanticsDetectsBreak verifies that runInlineTests catches a
// deliberately broken fix. It provides a pre-fix archive with @test
// annotations and a fixedFiles map where the __reclose wrapper is
// missing, then asserts that the post-fix @test assertions fail.
func TestFixSemanticsDetectsBreak(t *testing.T) {
	// Pre-fix archive: old semantics (no explicitopen), embedding
	// propagates closedness from #A.
	archive := txtar.Parse([]byte(`
#no-coverage

-- cue.mod/module.cue --
module: "test.example"
language: version: "v0.15.0"

-- in.cue --
package foo

#A: a: int

X: {
	#A
	b: int
}

tests: {
	t1: err: X & {c: 1} @test(err, code=eval, contains="field not allowed", pos=[0:16])
}
`))

	dir := t.TempDir()

	// Correct fix: __reclose preserves closedness in new semantics.
	t.Run("correct-fix", func(t *testing.T) {
		correctFixed := map[string][]byte{
			"in.cue": []byte(`@experiment(explicitopen)

package foo

#A: a: int

X: __reclose({
	#A...
	b: int
})

tests: {
	t1: err: X & {c: 1} @test(err, code=eval, contains="field not allowed", pos=[0:16])
}
`),
		}
		runInlineTests(t, archive, dir, correctFixed)
	})

	// Broken fix: __reclose is missing, X becomes open, t1 should fail.
	t.Run("broken-fix", func(t *testing.T) {
		brokenFixed := map[string][]byte{
			"in.cue": []byte(`@experiment(explicitopen)

package foo

#A: a: int

X: {
	#A...
	b: int
}

tests: {
	t1: err: X & {c: 1} @test(err, code=eval, contains="field not allowed", pos=[0:16])
}
`),
		}

		// We expect the post-fix sub-test to fail. Wrap in a helper
		// that captures and verifies the failure.
		var postFixFailed bool
		t.Run("post-fix", func(t *testing.T) {
			postFixArchive := buildPostFixArchive(archive, brokenFixed)
			cap := &cuetxtar.FailCapture{TB: t}
			runner := cuetxtar.NewInlineRunnerCapture(t, nil, postFixArchive, dir, cap)
			runner.Run()
			if cap.Failed() {
				postFixFailed = true
				t.Logf("correctly detected broken fix:\n%s", cap.Messages())
			}
		})
		if !postFixFailed {
			t.Fatal("expected broken fix to fail @test assertions, but it passed")
		}
	})
}

// archiveHasTestAttrs reports whether any CUE file in the archive contains
// an @test( attribute.
func archiveHasTestAttrs(a *txtar.Archive) bool {
	for _, f := range a.Files {
		if strings.HasSuffix(f.Name, ".cue") && bytes.Contains(f.Data, []byte("@test(")) {
			return true
		}
	}
	return false
}

// buildPostFixArchive constructs a new txtar archive with the fixed CUE file
// contents, preserving all other files (module.cue, non-CUE files) and the
// archive comment. Output sections (out/*) are stripped since the inline
// runner doesn't need them.
func buildPostFixArchive(orig *txtar.Archive, fixedFiles map[string][]byte) *txtar.Archive {
	result := &txtar.Archive{
		Comment: orig.Comment,
		Files:   slices.Clone(orig.Files),
	}
	// Replace CUE files and module.cue with fixed versions.
	for i, f := range result.Files {
		if data, ok := fixedFiles[f.Name]; ok {
			result.Files[i] = txtar.File{Name: f.Name, Data: data}
		}
	}
	// Strip out/* sections — they're for golden-file comparison, not evaluation.
	result.Files = slices.DeleteFunc(result.Files, func(f txtar.File) bool {
		return strings.HasPrefix(f.Name, "out/")
	})
	return result
}
