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

// This file tests the CUE_UPDATE=1 / CUE_UPDATE=force write-back behavior of
// the inline runner. Each testdata/inline/*.txtar file describes an update
// scenario: the in/ sections are the input, out/run/errors.txt captures errors
// from a plain run (no update), out/update/ sections are the expected state
// after two CUE_UPDATE=1 passes (for idempotency; omitted when identical to
// input), out/force/ sections are the expected state after CUE_UPDATE=force
// (omitted when identical to update), and out/status.txt records which results
// were identical (proof that the framework actually ran).
//
// CUE_UPDATE=1 adds missing out/ sections (for new tests) but validates
// existing ones without overwriting them. CUE_UPDATE=force fully regenerates
// all out/ sections, and is required when existing section content changes.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
)

// TestInlineUpdate runs txtar-driven update tests from testdata/inline/.
// Each test verifies that running the inline runner with CUE_UPDATE=1 and/or
// CUE_UPDATE=force produces the expected output, and that a second update pass
// is idempotent.
//
// Testdata format:
//
//	-- in/test.cue --
//	... CUE source with @test directives that may need updating ...
//
//	-- out/run/errors.txt --
//	... errors from a plain run (no update); absent if the run passes ...
//
//	-- out/update/test.cue --
//	... expected source after two CUE_UPDATE=1 passes (idempotent);
//	... absent when update produces no change (i.e. identical to input) ...
//
//	-- out/force/test.cue --
//	... expected source after CUE_UPDATE=force; absent when identical to update ...
//
//	-- out/status.txt --
//	... "update: identical" and/or "force: identical" lines (proof of run) ...
func TestInlineUpdate(t *testing.T) {
	matches, err := filepath.Glob("testdata/inline/*.txtar")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("testdata/inline: no *.txtar files found")
	}
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".txtar")
		t.Run(name, func(t *testing.T) {
			runUpdateTest(t, path)
		})
	}
}

func runUpdateTest(t *testing.T, filePath string) {
	t.Helper()
	outer, err := txtar.ParseFile(filePath)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	// Build input archive from in/ sections.
	inputArchive := extractTxtarSections(outer, "in/")
	if len(inputArchive.Files) == 0 {
		t.Fatalf("%s: no in/ sections", filePath)
	}

	// ctx holds the values that are constant across all applySection calls.
	ctx := &sectionCtx{
		t:           t,
		outerForce:  cuetest.ForceUpdateGoldenFiles,
		outerUpdate: cuetest.UpdateGoldenFiles,
		outer:       outer,
		filePath:    filePath,
	}
	// --- Plain run (no update mode) ---

	capR := &cuetxtar.FailCapture{TB: t}
	withUpdateMode(false, false, func() {
		runner := cuetxtar.NewInlineRunnerCapture(t, nil, cloneTxtarArchive(inputArchive), t.TempDir(), capR)
		runner.Run()
	})
	runErrors := capR.Messages()
	ctx.apply(section{
		prefix:      "out/run/errors.txt",
		content:     singleFile(runErrors),
		shouldExist: runErrors != "",
	})

	// --- Regular update (CUE_UPDATE=1) ---

	// Pass 1: run with update mode enabled; capture assertion errors.
	update1 := cloneTxtarArchive(inputArchive)
	cap1 := &cuetxtar.FailCapture{TB: t}
	withUpdateMode(true, false, func() {
		runner := cuetxtar.NewInlineRunnerCapture(t, nil, update1, t.TempDir(), cap1)
		runner.Run()
	})

	// Pass 2: run again from the pass-1 result to verify idempotency.
	update2 := cloneTxtarArchive(update1)
	cap2 := &cuetxtar.FailCapture{TB: t}
	withUpdateMode(true, false, func() {
		runner := cuetxtar.NewInlineRunnerCapture(t, nil, update2, t.TempDir(), cap2)
		runner.Run()
	})

	// Both passes must produce the same archive and errors.
	if diff := txtarFileDiff(update1, update2); diff != "" {
		t.Errorf("CUE_UPDATE=1 is not idempotent (archive):\n%s", diff)
	}
	if cap1.Messages() != cap2.Messages() {
		t.Errorf("CUE_UPDATE=1 is not idempotent (errors):\ngot (pass1):\n%swant (pass2):\n%s",
			cap1.Messages(), cap2.Messages())
	}

	// out/update/ sections: omitted when update is identical to input.
	updateIdentical := txtarFileDiff(inputArchive, update1) == ""
	ctx.apply(section{
		prefix:      "out/update/",
		content:     update1,
		shouldExist: !updateIdentical,
		staleMsg:    "out/update/ sections present but update produces same result as input (run with CUE_UPDATE=1 to remove)",
	})

	// --- Plain run on update output ---
	// When update changed the source, run the updated files as a fresh test to
	// verify the update output is itself a passing test. This replaces the need
	// for separate *_run.txtar files that duplicate the update output as input.
	updateOutputPasses := updateIdentical // trivially true when nothing changed
	if !updateIdentical {
		capUR := &cuetxtar.FailCapture{TB: t}
		withUpdateMode(false, false, func() {
			runner := cuetxtar.NewInlineRunnerCapture(t, nil, cloneTxtarArchive(update1), t.TempDir(), capUR)
			runner.Run()
		})
		if errs := capUR.Messages(); errs != "" {
			t.Errorf("plain run of update output produced errors:\n%s", errs)
		} else {
			updateOutputPasses = true
		}
	}

	// --- Force update (CUE_UPDATE=force) ---
	// out/force/ sections: present only when force produces a different result than update.

	forceArchive := cloneTxtarArchive(inputArchive)
	capF := &cuetxtar.FailCapture{TB: t}
	withUpdateMode(true, true, func() {
		runner := cuetxtar.NewInlineRunnerCapture(t, nil, forceArchive, t.TempDir(), capF)
		runner.Run()
	})
	forceDiffers := txtarFileDiff(update1, forceArchive) != "" || cap1.Messages() != capF.Messages()
	forceContent := cloneTxtarArchive(forceArchive)
	if msg := capF.Messages(); msg != "" {
		forceContent.Files = append(forceContent.Files, txtar.File{Name: "errors.txt", Data: []byte(msg)})
	}
	ctx.apply(section{
		prefix:      "out/force/",
		content:     forceContent,
		shouldExist: forceDiffers,
		staleMsg:    "out/force/ sections present but force produces same result as update (run with CUE_UPDATE=1 to remove)",
	})

	// --- Status summary ---
	// out/status.txt records which results were identical to their predecessor,
	// serving as proof that the framework actually exercised those paths.
	var statusLines []string
	if updateIdentical {
		statusLines = append(statusLines, "update: identical to input")
	} else if updateOutputPasses {
		statusLines = append(statusLines, "update: output passes run")
	}
	if !forceDiffers {
		statusLines = append(statusLines, "force:  identical to update")
	}
	var statusContent string
	if len(statusLines) > 0 {
		statusContent = strings.Join(statusLines, "\n") + "\n"
	}
	ctx.apply(section{
		prefix:      "out/status.txt",
		content:     singleFile(statusContent),
		shouldExist: statusContent != "",
	})

	// Write the outer testdata file if any sections were updated.
	if ctx.modified {
		if err := os.WriteFile(filePath, txtar.Format(outer), 0o644); err != nil {
			t.Errorf("update %s: %v", filePath, err)
		}
	}
}

// singleFile wraps content in a single-file archive with an empty file name.
// Used for sections where the prefix is the full file name (e.g. "out/status.txt").
func singleFile(content string) *txtar.Archive {
	return &txtar.Archive{Files: []txtar.File{{Data: []byte(content)}}}
}

// sectionCtx holds the values that are constant across all apply calls within
// a single runUpdateTest invocation, and accumulates whether any section was
// modified.
type sectionCtx struct {
	t           *testing.T
	outerForce  bool
	outerUpdate bool
	outer       *txtar.Archive
	filePath    string
	modified    bool
}

// section describes one output section group to manage.
type section struct {
	prefix      string
	content     *txtar.Archive
	shouldExist bool
	staleMsg    string // optional; ctx.filePath is prepended automatically
}

// apply manages the lifecycle of one output section group in ctx.outer and
// sets ctx.modified if the archive was changed.
//
//   - outerForce: write if shouldExist, remove otherwise; always sets modified.
//   - outerUpdate: write if missing and shouldExist; validate if present and
//     shouldExist; remove if present but !shouldExist.
//   - default: validate if shouldExist; log staleMsg (if non-empty) when the
//     section exists but should not.
func (ctx *sectionCtx) apply(s section) {
	ctx.t.Helper()
	exists := hasTxtarSectionPrefix(ctx.outer, s.prefix)
	switch {
	case ctx.outerForce:
		if s.shouldExist {
			replaceTxtarSections(ctx.outer, s.prefix, s.content)
		} else {
			removeTxtarSectionPrefix(ctx.outer, s.prefix)
		}
		ctx.modified = true
	case ctx.outerUpdate:
		switch {
		case s.shouldExist && !exists:
			replaceTxtarSections(ctx.outer, s.prefix, s.content)
			ctx.modified = true
		case s.shouldExist && exists:
			compareTxtarSections(ctx.t, ctx.outer, s.prefix, s.content, ctx.filePath)
		case !s.shouldExist && exists:
			removeTxtarSectionPrefix(ctx.outer, s.prefix)
			ctx.modified = true
		}
	default:
		if s.shouldExist {
			compareTxtarSections(ctx.t, ctx.outer, s.prefix, s.content, ctx.filePath)
		} else if exists && s.staleMsg != "" {
			ctx.t.Error(ctx.filePath + ": " + s.staleMsg)
		}
	}
}

// withUpdateMode temporarily sets cuetest.UpdateGoldenFiles and
// cuetest.ForceUpdateGoldenFiles for the duration of fn, then restores them.
func withUpdateMode(update, force bool, fn func()) {
	origUpdate := cuetest.UpdateGoldenFiles
	origForce := cuetest.ForceUpdateGoldenFiles
	defer func() {
		cuetest.UpdateGoldenFiles = origUpdate
		cuetest.ForceUpdateGoldenFiles = origForce
	}()
	cuetest.UpdateGoldenFiles = update
	cuetest.ForceUpdateGoldenFiles = force
	fn()
}

// extractTxtarSections returns a new archive built from files in ar whose names
// start with the given prefix; the prefix is stripped from each file name.
func extractTxtarSections(ar *txtar.Archive, prefix string) *txtar.Archive {
	var files []txtar.File
	for _, f := range ar.Files {
		if name, ok := strings.CutPrefix(f.Name, prefix); ok {
			files = append(files, txtar.File{Name: name, Data: bytes.Clone(f.Data)})
		}
	}
	return &txtar.Archive{Files: files}
}

// cloneTxtarArchive returns a deep copy of ar.
func cloneTxtarArchive(ar *txtar.Archive) *txtar.Archive {
	clone := &txtar.Archive{Comment: bytes.Clone(ar.Comment)}
	for _, f := range ar.Files {
		clone.Files = append(clone.Files, txtar.File{Name: f.Name, Data: bytes.Clone(f.Data)})
	}
	return clone
}

// hasTxtarSectionPrefix reports whether ar has any file whose name starts with prefix.
func hasTxtarSectionPrefix(ar *txtar.Archive, prefix string) bool {
	return slices.ContainsFunc(ar.Files, func(f txtar.File) bool {
		return strings.HasPrefix(f.Name, prefix)
	})
}

// removeTxtarSectionPrefix removes all files from ar whose names start with prefix.
func removeTxtarSectionPrefix(ar *txtar.Archive, prefix string) {
	ar.Files = slices.DeleteFunc(ar.Files, func(f txtar.File) bool {
		return strings.HasPrefix(f.Name, prefix)
	})
}

// replaceTxtarSections sets all files under prefix in ar to match src: existing
// files with the prefix that are not in src are removed, files that are in src
// are updated in place, and new src files are appended at the end.
func replaceTxtarSections(ar *txtar.Archive, prefix string, src *txtar.Archive) {
	// Build the set of names (with prefix) that src provides.
	srcByName := make(map[string][]byte, len(src.Files))
	for _, sf := range src.Files {
		srcByName[prefix+sf.Name] = sf.Data
	}

	var result []txtar.File
	inserted := make(map[string]bool)
	for _, f := range ar.Files {
		if strings.HasPrefix(f.Name, prefix) {
			if data, ok := srcByName[f.Name]; ok {
				result = append(result, txtar.File{Name: f.Name, Data: data})
				inserted[f.Name] = true
			}
			// else: stale file — drop it
		} else {
			result = append(result, f)
		}
	}
	// Append any src files not yet inserted (new sections with no prior entry).
	for _, sf := range src.Files {
		name := prefix + sf.Name
		if !inserted[name] {
			result = append(result, txtar.File{Name: name, Data: sf.Data})
		}
	}
	ar.Files = result
}

// compareTxtarSections checks that files in src match the sections in ar that
// have the given prefix. hint is the file path used in error messages.
func compareTxtarSections(t *testing.T, ar *txtar.Archive, prefix string, src *txtar.Archive, hint string) {
	t.Helper()
	want := make(map[string]string)
	for _, f := range ar.Files {
		if name, ok := strings.CutPrefix(f.Name, prefix); ok {
			want[name] = string(f.Data)
		}
	}
	got := make(map[string]string)
	for _, f := range src.Files {
		got[f.Name] = string(f.Data)
	}
	for name, g := range got {
		w, ok := want[name]
		if !ok {
			t.Errorf("%s: %s%s: not in testdata (run with CUE_UPDATE=1 to add)\ngot:\n%s", hint, prefix, name, g)
			continue
		}
		if g != w {
			t.Errorf("%s: %s%s: mismatch\ngot:\n%swant:\n%s", hint, prefix, name, g, w)
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("%s: %s%s: in testdata but not produced by runner", hint, prefix, name)
		}
	}
}

// txtarFileDiff returns a human-readable description of file-by-file differences
// between archive a and archive b. Returns "" if they are identical.
func txtarFileDiff(a, b *txtar.Archive) string {
	aMap := make(map[string]string)
	for _, f := range a.Files {
		aMap[f.Name] = string(f.Data)
	}
	var diffs []string
	for _, f := range b.Files {
		ac, ok := aMap[f.Name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("file %s: only in second archive", f.Name))
		} else if ac != string(f.Data) {
			diffs = append(diffs, fmt.Sprintf("file %s: differs\ngot (pass1):\n%swant (pass2):\n%s",
				f.Name, ac, string(f.Data)))
		}
		delete(aMap, f.Name)
	}
	for name := range aMap {
		diffs = append(diffs, fmt.Sprintf("file %s: only in first archive", name))
	}
	return strings.Join(diffs, "\n")
}
