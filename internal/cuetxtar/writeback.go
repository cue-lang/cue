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

// This file implements CUE_UPDATE write-backs for the inline test runner.
//
// The inline fill write-back rewrites @test attributes in-place using byte
// offsets, handling fill, force-overwrite, regression-guard, and stale-skip
// cleanup operations.

import (
	"fmt"
	"os"
	"slices"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
)

// ── inline fill write-back ─────────────────────────────────────────────────────

// inlineFillWrite records a byte-level replacement of a @test attribute in a
// .cue archive file.  Used for three operations:
//   - fill: @test() → @test(eq, <value>) or @test(err, ...)
//   - force: @test(eq, old) → @test(eq, <actual>)
//   - force-skip: @test(eq, old) → @test(eq, old, skip:<ver>)  [CUE_UPDATE=force only]
//   - stale-skip: @test(eq, old, skip:<ver>) → @test(eq, old)  [CUE_UPDATE=1]
type inlineFillWrite struct {
	fileName    string // archive .cue file name, e.g. "in.cue"
	attrOffset  int    // byte offset of the @ in the original file data
	attrLen     int    // byte length of the original @test(...) text
	newAttrText string // replacement text
}

// applyInlineFillWritebacks applies all pending byte-level @test attribute
// rewrites to the archive, including both inline-fill writes (eq/cover/debug)
// and pos= writes.  All writes are combined and applied in a single
// descending-offset pass per file so that no write shifts the byte positions
// used by another write in the same pass.
func (r *inlineRunner) applyInlineFillWritebacks() {
	// Flush nestedPosFills into pendingPosWrites: each accumulated entry
	// represents an outer @test(eq, {...}) attribute whose text was updated
	// in-place to fill nested pos=[] placeholders.
	for _, entry := range r.nestedPosFills {
		r.pendingPosWrites = append(r.pendingPosWrites, posWrite{
			fileName:    entry.fileName,
			attrOffset:  entry.attrOffset,
			attrLen:     entry.attrLen,
			newAttrText: entry.currentText,
		})
	}
	r.nestedPosFills = nil

	if len(r.pendingInlineFillWrites) == 0 && len(r.pendingPosWrites) == 0 {
		return
	}
	// Group writes by file name, combining both sets.
	byFile := make(map[string][]inlineFillWrite, 2)
	for _, ifw := range r.pendingInlineFillWrites {
		byFile[ifw.fileName] = append(byFile[ifw.fileName], ifw)
	}
	for _, pw := range r.pendingPosWrites {
		byFile[pw.fileName] = append(byFile[pw.fileName], pw)
	}

	changed := false
	for i, f := range r.archive.Files {
		writes, ok := byFile[f.Name]
		if !ok {
			continue
		}
		// Sort descending by offset so earlier positions remain valid.
		slices.SortFunc(writes, func(a, b inlineFillWrite) int {
			return b.attrOffset - a.attrOffset
		})
		data := append([]byte(nil), f.Data...)
		for _, w := range writes {
			end := w.attrOffset + w.attrLen
			if end > len(data) {
				continue
			}
			data = append(data[:w.attrOffset:w.attrOffset],
				append([]byte(w.newAttrText), data[end:]...)...)
		}
		r.archive.Files[i].Data = data
		changed = true
	}
	if changed && r.filePath != "" {
		out := txtar.Format(r.archive)
		if err := os.WriteFile(r.filePath, out, 0o644); err != nil {
			r.t.Errorf("inline: fill write-back to %s: %v", r.filePath, err)
		}
	}
}

// formatCoverAttr returns the @test attribute text to insert for a field whose
// evaluated value is v.  For non-error values this is @test(eq, <value>).
// For error values it falls back to a bare @test(err) placeholder.
func (r *inlineRunner) formatCoverAttr(v cue.Value, srcFileName string) string {
	if r.isError(v) {
		return "@test(err)"
	}
	return fmt.Sprintf("@test(eq, %s)", r.formatValue(v, srcFileName))
}
