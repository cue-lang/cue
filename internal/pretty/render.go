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
	"math"
	"strings"
	"unicode/utf8"
)

type mode uint8

const (
	modeBreak mode = iota
	modeFlat
)

// Render formats a Doc into bytes using the Wadler-Lindig best-fit
// algorithm.
func (cfg Config) Render(doc *Doc) []byte {
	r := &renderer{
		width:  cfg.width(),
		indent: cfg.indent(),
	}
	r.renderInMode(0, modeBreak, doc)
	return r.buf.Bytes()
}

type renderer struct {
	width  int
	indent string
	buf    bytes.Buffer
	col    int
}

// newline writes a newline followed by indentation for the given
// level.
func (r *renderer) newline(indent int) {
	r.buf.WriteByte('\n')
	for range indent {
		r.buf.WriteString(r.indent)
	}
	r.col = indent * utf8.RuneCountInString(r.indent)
}

// fits computes the flat-mode width of doc. It returns the width and
// true if the doc fits within remaining columns, or (0, false) if it
// exceeds remaining or contains a hard break (unflattenable).
func fits(remaining int, doc *Doc) (int, bool) {
	if doc == nil {
		return 0, true
	}
	width := 0
	stack := []*Doc{doc}
	for len(stack) > 0 {
		if width > remaining {
			return 0, false
		}
		top := len(stack) - 1
		doc := stack[top]
		stack = stack[:top]

		if doc == nil {
			continue
		}

		switch doc.tag {
		case tagText:
			width += utf8.RuneCountInString(doc.text)

		case tagLine:
			// In flat mode, Line emits its alt text.
			width += utf8.RuneCountInString(doc.text)

		case tagHard:
			// A HardLine means this group cannot be flattened.
			return 0, false

		case tagLitLine:
			// A literal newline (multi-line string) also prevents flattening.
			return 0, false

		case tagCat:
			stack = append(stack, doc.right, doc.left)

		case tagNest:
			stack = append(stack, doc.right)

		case tagGroup:
			// Nested groups are flattened in fits check.
			stack = append(stack, doc.right)

		case tagIfBreak:
			// In flat mode, use the flat variant.
			stack = append(stack, doc.right)

		case tagTable:
			// A // comment (trailing or doc) in any row runs to end of
			// line and would swallow subsequent tokens in flat mode.
			// Force break.
			for _, row := range doc.rows {
				if row.HasComment || row.DocComment != nil {
					return 0, false
				}
			}
			// Measure table in flat mode: sep cells... sep cells...
			for i := len(doc.rows) - 1; i >= 0; i-- {
				row := doc.rows[i]
				if row.Raw != nil {
					stack = append(stack, row.Raw)

				} else {
					for j := len(row.Cells) - 1; j >= 0; j-- {
						stack = append(stack, row.Cells[j])
						if j > 0 {
							stack = append(stack, spaceText)
						}
					}
				}
				if i > 0 && row.Sep != nil {
					stack = append(stack, row.Sep)
				}
			}
		}
	}
	if width > remaining {
		return 0, false
	}
	return width, true
}

// renderTable renders table rows. In flat mode, cells are concatenated
// with spaces between them. In broken mode, columns are padded to align
// across rows. A row whose cumulative width exceeds the target is excluded
// from contributing to subsequent column widths.
func (r *renderer) renderTable(indent int, m mode, rows []Row) {
	if m == modeFlat {
		for i, row := range rows {
			if i > 0 {
				r.renderFlat(row.Sep)
			}
			if row.Raw != nil {
				r.renderFlat(row.Raw)
				continue
			}
			for j, cell := range row.Cells {
				if j > 0 {
					r.buf.WriteByte(' ')
					r.col++
				}
				r.renderFlat(cell)
			}
		}
		return
	}

	// Broken mode: compute column widths.
	// Find the max number of columns across all aligned rows.
	numCols := 0
	for _, row := range rows {
		if row.Raw == nil {
			numCols = max(numCols, len(row.Cells))
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
		rowCumulative := 0
		for c, cell := range row.Cells {
			if containsHardBreak(cell) {
				break
			}
			if c > 0 {
				rowCumulative++ // space between columns
			}
			w, _ := fits(math.MaxInt, cell)
			rowCumulative += w
			if rowCumulative > r.width {
				break
			}
			if w > colMaxW[c] {
				colMaxW[c] = w
			}
		}
	}

	// Render rows with column padding.
	for i, row := range rows {
		if i > 0 {
			r.renderInMode(indent, modeBreak, row.Sep)
		}
		if row.Raw != nil {
			r.renderInMode(indent, modeBreak, row.Raw)
			continue
		}
		if row.DocComment != nil {
			r.renderInMode(indent, modeBreak, row.DocComment)
			r.newline(indent)
		}
		lastCellIdx := len(row.Cells) - 1
		for c, cell := range row.Cells {
			if c > 0 {
				r.buf.WriteByte(' ')
				r.col++
			}
			colStart := r.col
			r.renderInMode(indent, modeBreak, cell)
			// Pad to column max width (but not the last cell).
			// Uses actual rendered width so multi-line cells (which may
			// end on a shorter last line) are padded correctly. If the
			// cell wrapped past a newline (actualWidth < 0), skip padding.
			if c < lastCellIdx {
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
func (r *renderer) renderInMode(indent int, m mode, doc *Doc) {
	// entry is a stack element for the rendering algorithm.
	type entry struct {
		indent int  // current indent level
		mode   mode // flat or break
		doc    *Doc
	}

	stack := []entry{{indent, m, doc}}
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
				r.newline(e.indent)
			}

		case tagHard:
			r.newline(e.indent)

		case tagLitLine:
			r.buf.WriteByte('\n')
			r.col = 0

		case tagCat:
			stack = append(stack, entry{e.indent, e.mode, e.doc.right})
			stack = append(stack, entry{e.indent, e.mode, e.doc.left})

		case tagNest:
			stack = append(stack, entry{e.indent + e.doc.n, e.mode, e.doc.right})

		case tagGroup:
			fitted := e.mode == modeFlat
			if !fitted {
				_, fitted = fits(r.width-r.col, e.doc.right)
			}
			if fitted {
				stack = append(stack, entry{e.indent, modeFlat, e.doc.right})
			} else {
				stack = append(stack, entry{e.indent, modeBreak, e.doc.right})
			}

		case tagIfBreak:
			if e.mode == modeBreak {
				stack = append(stack, entry{e.indent, e.mode, e.doc.left})
			} else {
				stack = append(stack, entry{e.indent, e.mode, e.doc.right})
			}

		case tagTable:
			r.renderTable(e.indent, e.mode, e.doc.rows)
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
		d := stack[top]
		stack = stack[:top]
		if d == nil {
			continue
		}
		switch d.tag {
		case tagHard, tagLitLine:
			return true
		case tagCat:
			stack = append(stack, d.left, d.right)
		case tagNest:
			stack = append(stack, d.right)
		case tagGroup:
			stack = append(stack, d.right)
		case tagIfBreak:
			// In break mode, the left (broken) variant is used.
			stack = append(stack, d.left)
		case tagTable:
			// A multi-row table in broken mode has newlines between rows.
			if len(d.rows) > 1 {
				return true
			}
			for _, row := range d.rows {
				if row.Raw != nil {
					stack = append(stack, row.Raw)
					continue
				}
				stack = append(stack, row.Cells...)
			}
		}
	}
	return false
}
