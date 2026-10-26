// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package diff computes differences between text files or strings.
package diff

import (
	"fmt"
	"sort"
)

// An Edit describes the replacement of a portion of a text file.
type Edit struct {
	Start, End int    // byte offsets of the region to replace
	New        string // the replacement
}

func (e Edit) String() string {
	return fmt.Sprintf("{Start:%d,End:%d,New:%q}", e.Start, e.End, e.New)
}

// Apply applies a sequence of edits to the src buffer and returns the
// result. Edits are applied in order of start offset; edits with the
// same start offset are applied in they order they were provided.
//
// Apply returns an error if any edit is out of bounds,
// or if any pair of edits is overlapping.
func Apply(src string, edits []Edit) (string, error) {
	edits, size, err := validate(src, edits)
	if err != nil {
		return "", err
	}

	// Apply edits.
	out := make([]byte, 0, size)
	lastEnd := 0
	for _, edit := range edits {
		if lastEnd < edit.Start {
			out = append(out, src[lastEnd:edit.Start]...)
		}
		out = append(out, edit.New...)
		lastEnd = edit.End
	}
	out = append(out, src[lastEnd:]...)

	if len(out) != size {
		panic("wrong size")
	}

	return string(out), nil
}

// ApplyBytes is like Apply, but it accepts a byte slice.
// The result is always a new array.
func ApplyBytes(src []byte, edits []Edit) ([]byte, error) {
	res, err := Apply(string(src), edits)
	return []byte(res), err
}

// validate checks that edits are consistent with src,
// and returns the size of the patched output.
// It may return a different slice.
func validate(src string, edits []Edit) ([]Edit, int, error) {
	if !sort.IsSorted(editsSort(edits)) {
		edits = append([]Edit(nil), edits...)
		SortEdits(edits)
	}

	// Check validity of edits and compute final size.
	size := len(src)
	lastEnd := 0
	for _, edit := range edits {
		if !(0 <= edit.Start && edit.Start <= edit.End && edit.End <= len(src)) {
			return nil, 0, fmt.Errorf("diff has out-of-bounds edits")
		}
		if edit.Start < lastEnd {
			return nil, 0, fmt.Errorf("diff has overlapping edits")
		}
		size += len(edit.New) + edit.Start - edit.End
		lastEnd = edit.End
	}

	return edits, size, nil
}

// SortEdits orders a slice of Edits by (start, end) offset.
// This ordering puts insertions (end = start) before deletions
// (end > start) at the same point, but uses a stable sort to preserve
// the order of multiple insertions at the same point.
// (Apply detects multiple deletions at the same point as an error.)
func SortEdits(edits []Edit) {
	sort.Stable(editsSort(edits))
}

type editsSort []Edit

func (a editsSort) Len() int { return len(a) }
func (a editsSort) Less(i, j int) bool {
	if cmp := a[i].Start - a[j].Start; cmp != 0 {
		return cmp < 0
	}
	return a[i].End < a[j].End
}
func (a editsSort) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
