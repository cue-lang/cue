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
	"unicode/utf8"
)

type mode uint8

const (
	modeBreak mode = iota
	modeFlat
)

// Render formats a Doc into bytes using the Wadler-Lindig best-fit
// algorithm.
func (cfg Config) Render(doc Doc) []byte {
	indent := cfg.indent()
	r := &renderer{
		width:       cfg.width(),
		indent:      indent,
		indentWidth: utf8.RuneCountInString(indent),
	}
	r.renderInMode(0, modeBreak, doc)
	return r.buf.Bytes()
}

type renderer struct {
	width       int
	indent      string
	indentWidth int // rune count of indent, precomputed
	buf         bytes.Buffer
	col         int
}

// renderInMode renders a doc using the given indent and mode.
func (r *renderer) renderInMode(indent int, m mode, doc Doc) {
	// entry is a stack element for the rendering algorithm.
	type entry struct {
		indent int  // current indent level
		mode   mode // flat or break
		doc    Doc
	}

	stack := []entry{{indent, m, doc}}
	for len(stack) > 0 {
		top := len(stack) - 1
		e := stack[top]
		stack = stack[:top]

		if e.doc == nil {
			continue
		}

		switch d := e.doc.(type) {
		case *DocText:
			r.buf.WriteString(d.Text)
			r.col += d.Width

		case *DocLine:
			if e.mode == modeFlat {
				r.buf.WriteString(d.Alt)
				r.col += d.AltWidth
			} else {
				r.newline(e.indent)
			}

		case *DocHard:
			r.newline(e.indent)

		case *DocLitLine:
			r.buf.WriteByte('\n')
			r.col = 0

		case *DocCat:
			stack = append(stack,
				entry{e.indent, e.mode, d.Right},
				entry{e.indent, e.mode, d.Left})

		case *DocNest:
			stack = append(stack, entry{e.indent + 1, e.mode, d.Child})

		case *DocGroup:
			mode := e.mode
			if mode != modeFlat {
				if _, ok := measureFlat(r.width-r.col, d.Child); ok {
					mode = modeFlat
				}
			}
			stack = append(stack, entry{e.indent, mode, d.Child})

		case *DocIfBreak:
			if e.mode == modeBreak {
				stack = append(stack, entry{e.indent, e.mode, d.Broken})
			} else {
				stack = append(stack, entry{e.indent, e.mode, d.Flat})
			}

		case *DocTable:
			r.renderTableInMode(e.indent, e.mode, d.Rows)
		}
	}
}

// renderTableInMode renders table rows. In flat-mode, cells are
// concatenated with spaces between them. In broken-mode, the table
// is rendered as one or more aligned segments. Rows are partitioned
// into maximal contiguous segments where every row renders without
// forced newlines (any cell containing HardLine/LitLine, or a
// wrapping-capable cell whose flat width exceeds its column-position
// budget), and each segment computes its column widths
// independently. A multi-line or overflowing row therefore "flushes"
// the surrounding alignment rather than stretching the columns of
// the simpler rows around it.
func (r *renderer) renderTableInMode(indent int, m mode, rows []Row) {
	if m == modeFlat {
		for i, row := range rows {
			if i > 0 {
				r.renderInMode(0, modeFlat, row.Sep)
			}
			if row.Raw != nil {
				r.renderInMode(0, modeFlat, row.Raw)
				continue
			}
			for j, cell := range row.Cells {
				if j > 0 {
					r.buf.WriteByte(' ')
					r.col++
				}
				r.renderInMode(0, modeFlat, cell)
			}
		}
		return
	}

	avail := r.width - indent*r.indentWidth
	if avail < 1 {
		avail = 1
	}

	// Render one segment at a time: find the longest prefix of rows
	// that can share column alignment, render it, and continue with
	// the tail.
	first := true
	for len(rows) > 0 {
		n := longestAlignedPrefix(rows, avail)
		r.renderTableSegment(indent, rows[:n], !first)
		first = false
		rows = rows[n:]
	}
}

// renderTableSegment renders a single sub-table with its own column
// widths. emitFirstSep controls whether the first row's Sep is
// rendered (true for segments after the first, so the break between
// sub-tables is honoured).
func (r *renderer) renderTableSegment(indent int, rows []Row, emitFirstSep bool) {
	numCols := 0
	for _, row := range rows {
		if row.Raw == nil {
			numCols = max(numCols, len(row.Cells))
		}
	}

	// Count how many rows contribute a cell to each column. A column
	// with only one row has no alignment target, so padding the
	// preceding column for that row is pointless — the one-off cell
	// should hug its predecessor.
	colCount := make([]int, numCols)
	for _, row := range rows {
		if row.Raw != nil {
			continue
		}
		for c := range row.Cells {
			colCount[c]++
		}
	}

	// Measure each cell's flat width and compute max per column. If a
	// cell is unflattenable (measureFlat returns false), the remaining
	// cells in that row are excluded from contributing to column
	// widths (but they still count towards colCount above).
	colMaxW := make([]int, numCols)
	for _, row := range rows {
		if row.Raw != nil {
			continue
		}
		for c, cell := range row.Cells {
			w, ok := measureBrokenWidth(cell)
			if !ok {
				break
			}
			if w > colMaxW[c] {
				colMaxW[c] = w
			}
		}
	}

	for i, row := range rows {
		if i > 0 || emitFirstSep {
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
			if c == lastCellIdx {
				continue
			}
			// Skip padding if the next column appears only on this
			// row: there is nothing to align to, so let the next cell
			// hug with just a single-space separator.
			if c+1 < numCols && colCount[c+1] <= 1 {
				continue
			}
			// Pad to column max width. If the cell wrapped (emitted
			// newlines), r.col - colStart gives the content width on
			// the last line. If r.col < colStart (the last line ends
			// before the start column), padding is not meaningful so
			// we skip it.
			if actualWidth := r.col - colStart; actualWidth >= 0 {
				if pad := colMaxW[c] - actualWidth; pad > 0 {
					for range pad {
						r.buf.WriteByte(' ')
					}
					r.col += pad
				}
			}
		}
	}
}

// longestAlignedPrefix returns the length of the longest prefix of
// rows that can share column alignment without forcing newlines.
// Always returns at least 1: a boundary row forms a one-row segment
// on its own.
//
// A row is a boundary when either
//   - a cell (or a Raw row's Doc) is unflattenable: it contains
//     HardLine/LitLine, e.g. from a RelPos break, a multi-line
//     string literal, or an inner StructLit/ListLit that itself
//     wants to break, or
//   - a cell whose Doc could wrap (contains a DocLine, DocIfBreak,
//     DocGroup, or DocTable) has flat width exceeding the budget at
//     its column position — Wadler-Lindig will break the inner
//     Group, emitting newlines.
//
// Cells whose Doc is pure text (no wrapping element) never emit
// newlines even if they extend past the line width — a lone
// trailing // comment is the canonical example — so they do not
// trigger a boundary on their own.
func longestAlignedPrefix(rows []Row, avail int) int {
	if len(rows) == 0 {
		return 0
	}

	// Precompute per-cell width and wrap-ability once; the inner
	// re-check loop below runs O(n²) times, so measuring and walking
	// each Doc on every iteration would be O(n² · treeSize).
	type cell struct {
		w       int
		canWrap bool
	}
	type rowInfo struct {
		broken bool
		cells  []cell // aligned rows
		rawW   int    // Raw rows
		rawCan bool   // Raw rows: canWrap
	}
	info := make([]rowInfo, len(rows))
	for i, row := range rows {
		if row.Raw != nil {
			w, ok := measureFlat(math.MaxInt, row.Raw)
			info[i] = rowInfo{broken: !ok, rawW: w, rawCan: docCanWrap(row.Raw)}
			continue
		}
		cs := make([]cell, len(row.Cells))
		broken := false
		for c, d := range row.Cells {
			w, ok := measureFlat(math.MaxInt, d)
			if !ok {
				broken = true
				break
			}
			cs[c] = cell{w: w, canWrap: docCanWrap(d)}
		}
		info[i] = rowInfo{broken: broken, cells: cs}
	}

	// rowFits reports whether row j fits given segment-wide column
	// maxes. A cell only "doesn't fit" if its flat width exceeds its
	// budget AND its Doc could wrap; pure text running past the line
	// is fine (e.g. a lone trailing // comment).
	rowFits := func(j int, colMaxW []int) bool {
		ri := &info[j]
		if rows[j].Raw != nil {
			return !ri.rawCan || ri.rawW <= avail
		}
		cellStart := 0
		for c, ci := range ri.cells {
			if c > 0 {
				cellStart += colMaxW[c-1] + 1
			}
			if ci.canWrap && ci.w > avail-cellStart {
				return false
			}
		}
		return true
	}

	if info[0].broken {
		return 1
	}

	var colMaxW []int
	for i, row := range rows {
		if info[i].broken {
			return i
		}
		// Predict new column maxes if this row is admitted.
		pred := colMaxW
		if row.Raw == nil {
			grow := false
			for c, ci := range info[i].cells {
				switch {
				case c >= len(pred):
					if !grow {
						pred = append([]int(nil), colMaxW...)
						grow = true
					}
					pred = append(pred, ci.w)
				case ci.w > pred[c]:
					if !grow {
						pred = append([]int(nil), colMaxW...)
						grow = true
					}
					pred[c] = ci.w
				}
			}
		}
		// Widening a column shifts later cells rightward, so an
		// earlier row that previously fit may now overflow. Re-check
		// every row already accepted.
		allFit := true
		for j := 0; j <= i; j++ {
			if !rowFits(j, pred) {
				allFit = false
				break
			}
		}
		if !allFit {
			if i == 0 {
				return 1
			}
			return i
		}
		colMaxW = pred
	}
	return len(rows)
}

// docCanWrap reports whether d could emit a newline under any
// rendering mode beyond the unconditional newlines (DocHard /
// DocLitLine). True iff d contains a DocLine (which becomes a
// newline in broken-mode), a DocIfBreak (whose broken branch may
// emit one), a DocGroup (which may itself decide to break), or a
// DocTable (which renders rows on separate lines). Pure-text Docs
// never wrap, no matter how wide.
func docCanWrap(d Doc) bool {
	if d == nil {
		return false
	}
	switch x := d.(type) {
	case *DocText:
		return false
	case *DocHard, *DocLitLine:
		return true
	case *DocLine, *DocIfBreak, *DocTable:
		return true
	case *DocGroup:
		return docCanWrap(x.Child)
	case *DocCat:
		return docCanWrap(x.Left) || docCanWrap(x.Right)
	case *DocNest:
		return docCanWrap(x.Child)
	}
	return false
}

// measureBrokenWidth measures the width of doc for the purpose of
// column alignment in broken-mode. It is identical to [measureFlat]
// except that DocIfBreak nodes contribute their Broken branch
// instead of their Flat branch — so a TrailingComma (`IfBreak(",",
// nil)`) correctly counts its one comma character rather than zero.
// DocLine nodes still contribute their flat-mode Alt text: cells
// that genuinely wrap (contain an unflattenable group or produce
// newlines) are handled separately by the row-partitioning logic.
func measureBrokenWidth(doc Doc) (int, bool) {
	if doc == nil {
		return 0, true
	}
	width := 0
	var stackBuf [32]Doc
	stack := stackBuf[:1]
	stack[0] = doc
	for len(stack) > 0 {
		top := len(stack) - 1
		d := stack[top]
		stack = stack[:top]
		if d == nil {
			continue
		}
		switch d := d.(type) {
		case *DocText:
			width += d.Width
		case *DocLine:
			width += d.AltWidth
		case *DocHard, *DocLitLine:
			return 0, false
		case *DocCat:
			stack = append(stack, d.Right, d.Left)
		case *DocNest:
			stack = append(stack, d.Child)
		case *DocGroup:
			stack = append(stack, d.Child)
		case *DocIfBreak:
			stack = append(stack, d.Broken)
		case *DocTable:
			for _, row := range d.Rows {
				if row.HasComment || row.DocComment != nil {
					return 0, false
				}
			}
			for i := len(d.Rows) - 1; i >= 0; i-- {
				row := d.Rows[i]
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
	return width, true
}

// measureFlat computes the flat-mode width of doc. It returns the
// width and true if the doc fits within remaining columns, or (0,
// false) if it exceeds remaining or contains a hard break
// (unflattenable).
func measureFlat(remaining int, doc Doc) (int, bool) {
	if doc == nil {
		return 0, true
	}
	width := 0
	var stackBuf [32]Doc
	stack := stackBuf[:1]
	stack[0] = doc
	for len(stack) > 0 {
		if width > remaining {
			return 0, false
		}
		top := len(stack) - 1
		d := stack[top]
		stack = stack[:top]

		if d == nil {
			continue
		}

		switch d := d.(type) {
		case *DocText:
			width += d.Width

		case *DocLine:
			// In flat-mode, Line emits its alt text.
			width += d.AltWidth

		case *DocHard:
			// A HardLine means this group cannot be flattened.
			return 0, false

		case *DocLitLine:
			// A literal newline (multi-line string) also prevents
			// flattening.
			return 0, false

		case *DocCat:
			// Left above right in the stack (i.e. processed first).
			stack = append(stack, d.Right, d.Left)

		case *DocNest:
			// In flat-mode, there is no indentation to increase.
			stack = append(stack, d.Child)

		case *DocGroup:
			stack = append(stack, d.Child)

		case *DocIfBreak:
			// In flat-mode, use the flat variant.
			stack = append(stack, d.Flat)

		case *DocTable:
			// A // comment in any row runs to end of line and would
			// swallow subsequent tokens in flat-mode. Force break.
			for _, row := range d.Rows {
				if row.HasComment || row.DocComment != nil {
					return 0, false
				}
			}
			// Measure table in flat-mode. Because this is a stack, we
			// need to work backwards so that we end up with the first
			// cell of the first row at the top of the stack.
			for i := len(d.Rows) - 1; i >= 0; i-- {
				row := d.Rows[i]
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

// newline writes a newline followed by indentation for the given
// level.
func (r *renderer) newline(indent int) {
	r.buf.WriteByte('\n')
	for range indent {
		r.buf.WriteString(r.indent)
	}
	r.col = indent * r.indentWidth
}
