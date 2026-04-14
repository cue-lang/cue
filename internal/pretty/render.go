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

package pretty

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

type mode uint8

const (
	modeBreak mode = iota
	modeFlat
)

// entry is a stack element for the rendering algorithm.
type entry struct {
	ind  int  // current indent level
	mode mode // flat or break
	doc  *Doc
}

// Render formats a Doc into bytes using the Wadler-Lindig best-fit algorithm.
func Render(width int, indent string, doc *Doc) []byte {
	r := &renderer{
		width:  width,
		indent: indent,
	}
	r.render(doc)
	return r.buf.Bytes()
}

type renderer struct {
	width  int
	indent string
	buf    bytes.Buffer
	col    int
}

func (r *renderer) render(doc *Doc) {
	r.renderInMode(0, modeBreak, doc)
}

// newline writes a newline followed by indentation for the given level.
func (r *renderer) newline(ind int) {
	r.buf.WriteByte('\n')
	for range ind {
		r.buf.WriteString(r.indent)
	}
	r.col = ind * utf8.RuneCountInString(r.indent)
}

// fits checks whether doc can be rendered in flat mode within remaining columns.
// It short-circuits on HardLine or when remaining goes negative.
func (r *renderer) fits(remaining int, ind int, doc *Doc) bool {
	// Use an explicit stack to avoid recursion.
	stack := []entry{{ind, modeFlat, doc}}
	for len(stack) > 0 {
		if remaining < 0 {
			return false
		}
		top := len(stack) - 1
		e := stack[top]
		stack = stack[:top]

		if e.doc == nil {
			continue
		}

		switch e.doc.tag {
		case tagText:
			remaining -= utf8.RuneCountInString(e.doc.text)

		case tagLine:
			// In flat mode, Line emits its alt text.
			remaining -= utf8.RuneCountInString(e.doc.text)

		case tagHard:
			// A HardLine means this group cannot be flattened.
			return false

		case tagLitLine:
			// A literal newline (multi-line string) also prevents flattening.
			return false

		case tagCat:
			stack = append(stack, entry{e.ind, e.mode, e.doc.right})
			stack = append(stack, entry{e.ind, e.mode, e.doc.left})

		case tagNest:
			stack = append(stack, entry{e.ind + e.doc.n, e.mode, e.doc.right})

		case tagGroup:
			// Nested groups are flattened in fits check.
			stack = append(stack, entry{e.ind, modeFlat, e.doc.right})

		case tagIfBreak:
			// In flat mode, use the flat variant.
			stack = append(stack, entry{e.ind, e.mode, e.doc.right})

		case tagTable:
			// A // comment (trailing or doc) in any row runs to end of
			// line and would swallow subsequent tokens in flat mode.
			// Force break.
			for _, row := range e.doc.rows {
				if row.HasComment || row.DocComment != nil {
					return false
				}
			}
			// Measure table in flat mode: sep cells... sep cells...
			for i := len(e.doc.rows) - 1; i >= 0; i-- {
				row := e.doc.rows[i]
				if row.Raw != nil {
					stack = append(stack, entry{e.ind, modeFlat, row.Raw})
				} else {
					for j := len(row.Cells) - 1; j >= 0; j-- {
						if j > 0 {
							stack = append(stack, entry{e.ind, modeFlat, spaceText})
						}
						stack = append(stack, entry{e.ind, modeFlat, row.Cells[j]})
					}
				}
				if i > 0 && row.Sep != nil {
					stack = append(stack, entry{e.ind, modeFlat, row.Sep})
				}
			}
		}
	}
	return remaining >= 0
}

// measure returns the flat-mode width of a doc (for table column measurement).
func (r *renderer) measure(doc *Doc) int {
	if doc == nil {
		return 0
	}
	width := 0
	stack := []*Doc{doc}
	for len(stack) > 0 {
		top := len(stack) - 1
		d := stack[top]
		stack = stack[:top]

		if d == nil {
			continue
		}

		switch d.tag {
		case tagText:
			width += utf8.RuneCountInString(d.text)
		case tagLine:
			width += utf8.RuneCountInString(d.text)
		case tagHard:
			// Shouldn't appear in table keys, but count as 0.
		case tagLitLine:
			// Shouldn't appear in table keys, but count as 0.
		case tagCat:
			stack = append(stack, d.right)
			stack = append(stack, d.left)
		case tagNest:
			stack = append(stack, d.right)
		case tagGroup:
			stack = append(stack, d.right)
		case tagIfBreak:
			// Measure in flat mode.
			stack = append(stack, d.right)
		case tagTable:
			for i, row := range d.rows {
				if i > 0 {
					if row.Sep != nil {
						stack = append(stack, row.Sep)
					} else {
						width += 2 // ", "
					}
				}
				if row.Raw != nil {
					stack = append(stack, row.Raw)
				} else {
					for j := len(row.Cells) - 1; j >= 0; j-- {
						if j > 0 {
							stack = append(stack, spaceText)
						}
						stack = append(stack, row.Cells[j])
					}
				}
			}
		}
	}
	return width
}

// renderTable renders table rows. In flat mode, cells are concatenated
// with spaces between them. In broken mode, columns are padded to align
// across rows. A row whose cumulative width exceeds the target is excluded
// from contributing to subsequent column widths.
func (r *renderer) renderTable(ind int, m mode, rows []Row) {
	if m == modeFlat {
		for i, row := range rows {
			if i > 0 {
				if row.Sep != nil {
					r.renderFlat(row.Sep)
				} else {
					r.buf.WriteString(", ")
					r.col += 2
				}
			}
			if row.Raw != nil {
				r.renderFlat(row.Raw)
			} else {
				for j, cell := range row.Cells {
					if j > 0 && cell != nil {
						r.buf.WriteByte(' ')
						r.col++
					}
					r.renderFlat(cell)
				}
			}
		}
		return
	}

	// Broken mode: compute column widths.
	// Find the max number of columns across all aligned rows.
	numCols := 0
	for _, row := range rows {
		if row.Raw == nil && len(row.Cells) > numCols {
			numCols = len(row.Cells)
		}
	}

	// Measure each cell's flat width and compute max per column.
	// A row is "overflowed" if its cumulative width exceeds the target;
	// overflowed rows are excluded from contributing to subsequent columns.
	// A row whose cell contains a hard break (multi-line when rendered) is
	// also excluded, because its flat-mode width doesn't reflect the actual
	// last-line width after rendering.
	colMaxW := make([]int, numCols)
	for _, row := range rows {
		if row.Raw != nil {
			continue
		}
		cumulative := 0
		overflowed := false
		for c := range row.Cells {
			w := r.measure(row.Cells[c])
			cumulative += w
			if c > 0 {
				cumulative++ // space between columns
			}
			if cumulative > r.width || containsHardBreak(row.Cells[c]) {
				overflowed = true
			}
			if !overflowed && w > colMaxW[c] {
				colMaxW[c] = w
			}
		}
	}

	// Render rows with column padding.
	for i, row := range rows {
		if i > 0 {
			if row.Sep != nil {
				r.renderInMode(ind, modeBreak, row.Sep)
			} else {
				r.newline(ind)
			}
		}
		if row.Raw != nil {
			r.renderInMode(ind, modeBreak, row.Raw)
			continue
		}
		if row.DocComment != nil {
			r.renderInMode(ind, modeBreak, row.DocComment)
			r.newline(ind)
		}
		// Find last non-nil cell index for padding decisions.
		lastNonNil := -1
		for j := len(row.Cells) - 1; j >= 0; j-- {
			if row.Cells[j] != nil {
				lastNonNil = j
				break
			}
		}
		for c, cell := range row.Cells {
			if cell == nil {
				continue
			}
			if c > 0 {
				r.buf.WriteByte(' ')
				r.col++
			}
			colStart := r.col
			r.renderInMode(ind, modeBreak, cell)
			// Pad to column max width (but not the last non-nil cell).
			// Uses actual rendered width so multi-line cells (which may
			// end on a shorter last line) are padded correctly. If the
			// cell wrapped past a newline (actualWidth < 0), skip padding.
			if c < lastNonNil && c < numCols {
				if actualWidth := r.col - colStart; actualWidth >= 0 {
					if pad := colMaxW[c] - actualWidth; pad > 0 {
						r.buf.WriteString(strings.Repeat(" ", pad))
						r.col += pad
					}
				}
			}
		}
	}
}

// renderFlat renders a doc in flat mode (no line breaks).
func (r *renderer) renderFlat(doc *Doc) {
	r.renderInMode(0, modeFlat, doc)
}

// renderInMode renders a doc using the given indent and mode.
func (r *renderer) renderInMode(ind int, m mode, doc *Doc) {
	stack := []entry{{ind, m, doc}}
	for len(stack) > 0 {
		top := len(stack) - 1
		e := stack[top]
		stack = stack[:top]

		if e.doc == nil {
			continue
		}

		switch e.doc.tag {
		case tagText:
			r.buf.WriteString(e.doc.text)
			r.col += utf8.RuneCountInString(e.doc.text)

		case tagLine:
			if e.mode == modeFlat {
				r.buf.WriteString(e.doc.text)
				r.col += utf8.RuneCountInString(e.doc.text)
			} else {
				r.newline(e.ind)
			}

		case tagHard:
			r.newline(e.ind)

		case tagLitLine:
			r.buf.WriteByte('\n')
			r.col = 0

		case tagCat:
			stack = append(stack, entry{e.ind, e.mode, e.doc.right})
			stack = append(stack, entry{e.ind, e.mode, e.doc.left})

		case tagNest:
			stack = append(stack, entry{e.ind + e.doc.n, e.mode, e.doc.right})

		case tagGroup:
			if e.mode == modeFlat || r.fits(r.width-r.col, e.ind, e.doc.right) {
				stack = append(stack, entry{e.ind, modeFlat, e.doc.right})
			} else {
				stack = append(stack, entry{e.ind, modeBreak, e.doc.right})
			}

		case tagIfBreak:
			if e.mode == modeBreak {
				stack = append(stack, entry{e.ind, e.mode, e.doc.left})
			} else {
				stack = append(stack, entry{e.ind, e.mode, e.doc.right})
			}

		case tagTable:
			r.renderTable(e.ind, e.mode, e.doc.rows)
		}
	}
}

// containsHardBreak reports whether a Doc tree contains any hard break
// (HardLine or LitLine) that would fire in break mode. This is used to
// detect table cells that will render as multi-line, so their flat-mode
// width measurement shouldn't inflate column widths.
func containsHardBreak(d *Doc) bool {
	if d == nil {
		return false
	}
	stack := []*Doc{d}
	for len(stack) > 0 {
		top := len(stack) - 1
		n := stack[top]
		stack = stack[:top]
		if n == nil {
			continue
		}
		switch n.tag {
		case tagHard, tagLitLine:
			return true
		case tagCat:
			stack = append(stack, n.left, n.right)
		case tagNest:
			stack = append(stack, n.right)
		case tagGroup:
			stack = append(stack, n.right)
		case tagIfBreak:
			// In break mode, the left (broken) variant is used.
			stack = append(stack, n.left)
		case tagTable:
			// A multi-row table in broken mode has newlines between rows.
			if len(n.rows) > 1 {
				return true
			}
			for _, row := range n.rows {
				if row.Raw != nil {
					stack = append(stack, row.Raw)
				} else {
					for _, cell := range row.Cells {
						stack = append(stack, cell)
					}
				}
			}
		}
	}
	return false
}
