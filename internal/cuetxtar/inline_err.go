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

// This file contains all error-assertion logic for the inline test runner:
// types, parsing, matching, position checking, and write-back for
// @test(err, ...) directives.

package cuetxtar

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

// posSpec is an expected error position. Two forms are supported:
//
// Relative form (fileName == ""): the position is expressed as a signed line
// delta from the @test attribute's line (0 = same line) and a 1-indexed
// column. Written as deltaLine:col (e.g. "0:5", "-2:13"). Using a delta keeps
// the assertion stable when lines are added or removed above the test.
//
// Absolute form (fileName != ""): the position is in a different file, given
// as a filename, absolute 1-indexed line number, and 1-indexed column. Written
// as filename:absLine:col (e.g. "fixture.cue:3:5"). This form is used for
// errors whose source location is in a shared fixture file.
type posSpec struct {
	// fileName is non-empty for cross-file (absolute) positions.
	fileName string
	// deltaLine is the signed offset from the @test line; used when fileName == "".
	deltaLine int
	// absLine is the absolute 1-indexed line number; used when fileName != "".
	absLine int
	// col is the 1-indexed column number (used in both forms).
	col int
}

// errArgs holds parsed sub-options from an @test(err, ...) directive.
type errArgs struct {
	// codes holds the acceptable error codes, e.g. ["cycle"] or ["cycle", "incomplete"].
	// A single code= value is stored as a one-element slice; code=(a|b) as two.
	// An empty slice means any error code is accepted.
	codes []string
	// contains is a substring the error message must contain.
	contains string
	// any requires any descendant of the annotated field to have the error.
	any bool
	// paths lists specific paths (relative to test-case root) where the error
	// must occur. Populated when path=(...) is present.
	paths []string
	// pos lists expected error positions as (deltaLine:col) pairs relative to
	// the line containing the @test attribute.
	pos []posSpec
	// posSet is true when pos= was
	// explicitly provided (including pos=[] to assert no positions).
	posSet bool
}

// matchesCode reports whether the given error code satisfies the codes
// constraint. An empty codes slice means any code is accepted.
func (ea *errArgs) matchesCode(got string) bool {
	if len(ea.codes) == 0 {
		return true
	}
	return slices.Contains(ea.codes, got)
}

// parseErrArgs extracts err sub-options from an already-parsed Attr.
// The attribute body is expected to start with "err" as the first positional arg.
func parseErrArgs(a internal.Attr) (errArgs, error) {
	var ea errArgs
	// Start from index 1 (index 0 is "err").
	for _, kv := range a.Fields[1:] {
		switch {
		case kv.Key() == "code":
			codes, err := parseParenList(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, code=...): %w", err)
			}
			ea.codes = codes
		case kv.Key() == "contains":
			ea.contains = kv.Value()
		case kv.Key() == "" && kv.Value() == "any":
			ea.any = true
		case kv.Key() == "path":
			paths, err := parseParenList(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, path=...): %w", err)
			}
			ea.paths = paths
		case kv.Key() == "pos":
			specs, err := parsePosSpecs(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, pos=...): %w", err)
			}
			ea.pos = specs
			ea.posSet = true
		}
	}
	return ea, nil
}

// parseParenList parses a balanced parenthesized pipe-separated list like
// (path1|path2|path3), returning ["path1","path2","path3"].
// The input may or may not include the outer parentheses.
func parseParenList(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts, nil
}

// parsePosSpecs parses a pos= value into a slice of posSpec.
// The value must be enclosed in square brackets; elements are whitespace-separated.
// Two element forms are supported:
//
//   - deltaLine:col — relative position on the same file (one colon).
//     deltaLine is a signed offset from the @test attribute's line (0 = same line).
//   - filename:absLine:col — absolute position in another file (two colons).
//     absLine is the 1-indexed line in the named file.
func parsePosSpecs(s string) ([]posSpec, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("pos= value must be enclosed in square brackets, got %q", s)
	}
	s = s[1 : len(s)-1]
	var specs []posSpec
	for _, p := range strings.Fields(s) {
		parts := strings.SplitN(p, ":", 3)
		switch len(parts) {
		case 2:
			// Relative form: deltaLine:col
			deltaLine, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			colNum, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			specs = append(specs, posSpec{deltaLine: deltaLine, col: colNum})
		case 3:
			// Absolute form: filename:absLine:col
			fileName := parts[0]
			absLine, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			colNum, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			specs = append(specs, posSpec{fileName: fileName, absLine: absLine, col: colNum})
		default:
			return nil, fmt.Errorf("invalid pos spec %q: expected deltaLine:col or filename:line:col", p)
		}
	}
	return specs, nil
}

// runErrAssertion checks that an error is present at val, applying sub-options.
func (r *inlineRunner) runErrAssertion(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	ea := pa.errArgs
	if ea == nil {
		// Bare @test(err) — just check that the value is an error.
		if !r.isError(val) {
			t.Errorf("path %s: expected error, got non-error value", path)
		}
		return
	}

	if ea.any {
		// @test(err, any, ...) — check that any descendant has the error.
		found := r.findDescendantError(val, ea)
		if !found {
			t.Errorf("path %s: expected a descendant error with code=%v, none found", path, ea.codes)
		}
		return
	}

	if !r.isError(val) {
		t.Errorf("path %s: expected error, got non-error value", path)
		return
	}

	// Validate error code.
	if len(ea.codes) > 0 {
		gotCode := r.errorCode(val)
		if !ea.matchesCode(gotCode) {
			t.Errorf("path %s: expected error code %v, got %q", path, ea.codes, gotCode)
		}
	}
	// Validate error message contains.
	if ea.contains != "" {
		msg := r.errorMessage(val)
		if !strings.Contains(msg, ea.contains) {
			t.Errorf("path %s: expected error message to contain %q, got %q", path, ea.contains, msg)
		}
	}
	// Validate error positions.
	if ea.posSet {
		r.checkErrPositions(t, path, val, pa)
	}
}

// checkErrPositions verifies that the error positions on val match the pos=
// spec in pa.  When positions don't match:
//   - pos=[] (placeholder): update on CUE_UPDATE=1.
//   - pos=[non-empty]: update on CUE_UPDATE=force only.
func (r *inlineRunner) checkErrPositions(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	err := val.Err()
	if err == nil {
		t.Errorf("path %s: @test(err, pos=...): value has no error", path)
		return
	}
	positions := cueerrors.Positions(err)
	expected := pa.errArgs.pos

	match := len(positions) == len(expected)
	if match {
		for i, exp := range expected {
			got := positions[i]
			if exp.fileName != "" {
				// Absolute form: match filename + absolute line + column.
				// Normalize the position filename for archives loaded via
				// loadWithConfig, which stores absolute paths in positions.
				if r.relFilename(got.Filename()) != exp.fileName || got.Line() != exp.absLine || got.Column() != exp.col {
					match = false
					break
				}
			} else {
				// Relative form: match line delta from @test + column.
				if got.Line() != pa.baseLine+exp.deltaLine || got.Column() != exp.col {
					match = false
					break
				}
			}
		}
	}
	if match {
		return
	}

	// pos=[] is a fill-in placeholder: update with CUE_UPDATE=1.
	// pos=[non-empty] that is wrong: update only with CUE_UPDATE=force.
	isPlaceholder := len(expected) == 0
	if (isPlaceholder && cuetest.UpdateGoldenFiles) || cuetest.ForceUpdateGoldenFiles {
		r.enqueuePosWrite(pa, positions)
		return
	}

	if len(positions) != len(expected) {
		t.Errorf("path %s: @test(err, pos=...): got %d position(s), want %d", path, len(positions), len(expected))
		for i, p := range positions {
			t.Logf("  actual[%d]: %d:%d", i, p.Line(), p.Column())
		}
		return
	}
	for i, exp := range expected {
		got := positions[i]
		if exp.fileName != "" {
			gotFile := r.relFilename(got.Filename())
			if gotFile != exp.fileName || got.Line() != exp.absLine || got.Column() != exp.col {
				t.Errorf("path %s: @test(err, pos=...): position[%d]: got %s:%d:%d, want %s:%d:%d",
					path, i, gotFile, got.Line(), got.Column(), exp.fileName, exp.absLine, exp.col)
			}
		} else {
			wantLine := pa.baseLine + exp.deltaLine
			if got.Line() != wantLine || got.Column() != exp.col {
				t.Errorf("path %s: @test(err, pos=...): position[%d]: got %d:%d, want %d:%d",
					path, i, got.Line(), got.Column(), wantLine, exp.col)
			}
		}
	}
}

// posWrite records a pending pos= attribute update for CUE_UPDATE write-back.
type posWrite struct {
	fileName    string // archive .cue file name, e.g. "in.cue"
	attrOffset  int    // byte offset of the @test attr in the original file data
	attrLen     int    // byte length of the original @test attr text
	newAttrText string // replacement attribute text with updated pos=[...]
}

// enqueuePosWrite formats positions as pos specs and enqueues a write-back
// that replaces the pos=[...] value in the source attribute.
//
// Positions in the same file as the @test attribute are written as deltaLine:col
// (relative to pa.baseLine).  Positions in a different file are written as
// filename:absLine:col (absolute).
func (r *inlineRunner) enqueuePosWrite(pa parsedTestAttr, positions []token.Pos) {
	parts := make([]string, len(positions))
	for i, p := range positions {
		if p.Filename() == "" || p.Filename() == pa.srcFileName {
			parts[i] = fmt.Sprintf("%d:%d", p.Line()-pa.baseLine, p.Column())
		} else {
			parts[i] = fmt.Sprintf("%s:%d:%d", p.Filename(), p.Line(), p.Column())
		}
	}
	newPosStr := strings.Join(parts, " ")

	old := pa.srcAttr.Text
	start := strings.Index(old, "pos=[")
	if start < 0 {
		return
	}
	bracket := start + len("pos=[")
	end := strings.Index(old[bracket:], "]")
	if end < 0 {
		return
	}
	end += bracket + 1 // include the "]"
	newAttrText := old[:start] + "pos=[" + newPosStr + "]" + old[end:]

	r.pendingPosWrites = append(r.pendingPosWrites, posWrite{
		fileName:    pa.srcFileName,
		attrOffset:  pa.srcAttr.Pos().Offset(),
		attrLen:     len(pa.srcAttr.Text),
		newAttrText: newAttrText,
	})
}

// applyPosWritebacks writes pending pos= attribute updates to the archive file.
// Replacements are applied by byte offset from end to start so that earlier
// offsets remain valid after each substitution.
func (r *inlineRunner) applyPosWritebacks() {
	if len(r.pendingPosWrites) == 0 || r.filePath == "" {
		return
	}
	changed := false
	for i, f := range r.archive.Files {
		var writes []posWrite
		for _, pw := range r.pendingPosWrites {
			if pw.fileName == f.Name {
				writes = append(writes, pw)
			}
		}
		if len(writes) == 0 {
			continue
		}
		// Sort descending by offset so earlier offsets stay valid.
		slices.SortFunc(writes, func(a, b posWrite) int {
			return b.attrOffset - a.attrOffset
		})
		data := append([]byte(nil), f.Data...)
		for _, pw := range writes {
			end := pw.attrOffset + pw.attrLen
			if end > len(data) {
				continue
			}
			data = append(data[:pw.attrOffset:pw.attrOffset],
				append([]byte(pw.newAttrText), data[end:]...)...)
			changed = true
		}
		r.archive.Files[i].Data = data
	}
	if changed {
		out := txtar.Format(r.archive)
		if err := os.WriteFile(r.filePath, out, 0o644); err != nil {
			r.t.Errorf("inline: pos write-back to %s: %v", r.filePath, err)
		}
	}
}

// isError reports whether val is an error value (bottom).
func (r *inlineRunner) isError(val cue.Value) bool {
	core := val.Core()
	if core.V == nil {
		return false
	}
	return core.V.Bottom() != nil
}

// errorCode returns the string code of the error at val, or "" if not an error.
func (r *inlineRunner) errorCode(val cue.Value) string {
	core := val.Core()
	if core.V == nil {
		return ""
	}
	b := core.V.Bottom()
	if b == nil {
		return ""
	}
	return b.Code.String()
}

// errorMessage returns the human-readable error message for val.
func (r *inlineRunner) errorMessage(val cue.Value) string {
	if err := val.Err(); err != nil {
		return err.Error()
	}
	return ""
}

// findDescendantError walks val looking for any descendant with an error
// matching ea. Returns true if found.
func (r *inlineRunner) findDescendantError(val cue.Value, ea *errArgs) bool {
	if r.isError(val) {
		if ea.matchesCode(r.errorCode(val)) {
			return true
		}
	}
	// Walk fields.
	iter, err := val.Fields()
	if err != nil {
		return false
	}
	for iter.Next() {
		if r.findDescendantError(iter.Value(), ea) {
			return true
		}
	}
	return false
}
