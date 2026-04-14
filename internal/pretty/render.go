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

	// Scratch buffers reused across calls to avoid per-call heap
	// allocation (the renderer is the hot path; a 52 MB input shows
	// ~40 % of runtime in GC without reuse). renderStack is shared
	// across recursive renderInMode invocations by save/restore of
	// its length; infoScratch similarly flows through
	// longestAlignedPrefix.
	renderStack []renderEntry
	infoScratch []rowInfo
}

// renderEntry is one frame on renderInMode's explicit evaluation
// stack.
type renderEntry struct {
	indent int
	mode   mode
	doc    Doc
}

// renderInMode renders a doc using the given indent and mode.
func (r *renderer) renderInMode(indent int, m mode, doc Doc) {
	if doc == nil {
		return
	}
	base := len(r.renderStack)
	r.renderStack = append(r.renderStack, renderEntry{indent, m, doc})
	for len(r.renderStack) > base {
		top := len(r.renderStack) - 1
		e := r.renderStack[top]
		r.renderStack = r.renderStack[:top]

		if e.doc == nil {
			continue
		}

		switch d := e.doc.(type) {
		case *docText:
			r.buf.WriteString(d.Text)
			r.col += d.Width

		case *docLine:
			if e.mode == modeFlat {
				r.buf.WriteString(d.Alt)
				r.col += d.AltWidth
			} else {
				r.newline(e.indent)
			}

		case *docHard:
			r.newline(e.indent)

		case *docLitLine:
			r.buf.WriteByte('\n')
			r.col = 0

		case *docCat:
			r.renderStack = append(r.renderStack,
				renderEntry{e.indent, e.mode, d.Right},
				renderEntry{e.indent, e.mode, d.Left})

		case *docNest:
			r.renderStack = append(r.renderStack, renderEntry{e.indent + 1, e.mode, d.Child})

		case *docGroup:
			mode := e.mode
			if mode != modeFlat {
				if _, ok := measureFlat(r.width-r.col, d.Child); ok {
					mode = modeFlat
				}
			}
			r.renderStack = append(r.renderStack, renderEntry{e.indent, mode, d.Child})

		case *docIfBreak:
			if e.mode == modeBreak {
				r.renderStack = append(r.renderStack, renderEntry{e.indent, e.mode, d.Broken})
			} else {
				r.renderStack = append(r.renderStack, renderEntry{e.indent, e.mode, d.Flat})
			}

		case *docTable:
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
		n, info := r.longestAlignedPrefix(rows, avail)
		r.renderTableSegment(indent, rows[:n], info, !first)
		first = false
		rows = rows[n:]
	}
}

// cellInfo caches a cell's broken-mode width (what it would render
// to on a single line in broken-mode) and whether its Doc could
// introduce a newline under any mode. Computed once per cell by
// [longestAlignedPrefix] and reused by [renderTableSegment] so we
// don't walk the Doc tree twice.
type cellInfo struct {
	w       int
	canWrap bool
}

// rowInfo caches per-row measurements. For aligned rows, cells holds
// each cell's width/canWrap in order — on a broken row (a cell
// contains HardLine/LitLine) the slice is truncated at the broken
// cell, mirroring the old measureFlat-break-on-unflattenable
// semantics. For Raw rows, rawW/rawCanWrap carry the equivalent.
type rowInfo struct {
	broken     bool
	cells      []cellInfo // aligned rows
	rawW       int        // Raw rows
	rawCanWrap bool       // Raw rows
}

// renderTableSegment renders a single sub-table with its own column
// widths. info must have one entry per row with precomputed cell
// widths. emitFirstSep controls whether the first row's Sep is
// rendered (true for segments after the first, so the break between
// sub-tables is honoured).
func (r *renderer) renderTableSegment(indent int, rows []Row, info []rowInfo, emitFirstSep bool) {
	numCols := 0
	for _, row := range rows {
		if row.Raw == nil {
			numCols = max(numCols, len(row.Cells))
		}
	}

	// Count how many rows contribute non-nil content to each column,
	// and derive colMaxW from the cached cell widths. A column with
	// only one row has no alignment target, so padding the preceding
	// column for that row is pointless — the one-off cell should hug
	// its predecessor. On a broken row, info[i].cells is truncated at
	// the first unflattenable cell, so later cells don't contribute to
	// column widths (but they still count towards colCount). Nil cells
	// are "reserved" column slots (used to keep following columns
	// aligned across rows); they don't count as contributions.
	colCount := make([]int, numCols)
	colMaxW := make([]int, numCols)
	for i, row := range rows {
		if row.Raw != nil {
			continue
		}
		for c, cell := range row.Cells {
			if cell != nil {
				colCount[c]++
			}
		}
		for c, ci := range info[i].cells {
			if ci.w > colMaxW[c] {
				colMaxW[c] = ci.w
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
			// DocComment carries its own trailing separator (HardLine
			// or BlankLine) so the blank-line-after-doc-comment case
			// renders correctly when the node itself has NewSection.
			r.renderInMode(indent, modeBreak, row.DocComment)
		}
		// Identify the last non-nil cell so trailing reserved slots
		// don't emit spurious trailing whitespace.
		lastNonNilIdx := -1
		for c, cell := range row.Cells {
			if cell != nil {
				lastNonNilIdx = c
			}
		}
		for c, cell := range row.Cells {
			// Skip columns where no row in the segment contributes
			// content at all — emit nothing, no separator, no pad.
			// colMaxW[c] == 0 is insufficient on its own: a broken
			// cell (value contains a HardLine) also has width 0 in
			// info[i].cells because it's truncated; colCount tells us
			// whether any row has a non-nil cell at this column.
			if colMaxW[c] == 0 && colCount[c] == 0 {
				continue
			}
			// Skip everything past the row's last non-nil cell: no
			// trailing separator + reserved-slot padding.
			if c > lastNonNilIdx {
				break
			}
			if c > 0 {
				r.buf.WriteByte(' ')
				r.col++
			}
			colStart := r.col
			r.renderInMode(indent, modeBreak, cell)
			if c == lastNonNilIdx {
				continue
			}
			// Skip padding if no later column in this segment needs
			// alignment — a later dense column (colCount > 1) needs
			// this column padded so its content lines up; a column
			// that's sparse or entirely empty does not. We look past
			// empty columns (colMaxW == 0 — these render nothing) so
			// that comments in column 3 can still align when column 2
			// happens to be empty for every row in the segment.
			paddingNeeded := false
			for d := c + 1; d < numCols; d++ {
				if colMaxW[d] == 0 {
					continue
				}
				if colCount[d] > 1 {
					paddingNeeded = true
				}
				break
			}
			if !paddingNeeded {
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
// rows that can share column alignment without forcing newlines,
// together with cached [rowInfo] for every row scanned (info[:n] is
// the prefix). Always returns at least 1: a boundary row forms a
// one-row segment on its own. The cached widths are reused by
// [renderTableSegment] so we don't walk each cell's Doc tree twice.
//
// A row is a boundary when either
//   - a cell (or a Raw row's Doc) is unflattenable: it contains
//     HardLine/LitLine, e.g. from a RelPos break, a multi-line
//     string literal, or an inner StructLit/ListLit that itself
//     wants to break, or
//   - a cell whose Doc could wrap (contains a docLine, docIfBreak,
//     docGroup, or docTable) has broken-mode width exceeding the
//     budget at its column position — Wadler-Lindig will break the
//     inner Group, emitting newlines.
//
// Cells whose Doc is pure text (no wrapping element) never emit
// newlines even if they extend past the line width — a lone
// trailing // comment is the canonical example — so they do not
// trigger a boundary on their own.
func (r *renderer) longestAlignedPrefix(rows []Row, avail int) (int, []rowInfo) {
	if len(rows) == 0 {
		return 0, nil
	}

	// Precompute per-cell width and wrap-ability once; the inner
	// re-check loop below runs O(n²) times, so measuring and walking
	// each Doc on every iteration would be O(n² · treeSize). We use
	// [measureBrokenWidth] here — cells are rendered in broken-mode,
	// so a TrailingComma (IfBreak(",", nil)) contributes its one
	// character, matching what the renderer will actually emit.
	//
	// Grow r.infoScratch to cover len(rows); reuse existing capacity
	// when possible. We also reuse each entry's cells slice so we
	// don't allocate a fresh one per row on every call.
	if cap(r.infoScratch) < len(rows) {
		r.infoScratch = make([]rowInfo, len(rows))
	} else {
		r.infoScratch = r.infoScratch[:len(rows)]
	}
	info := r.infoScratch
	for i, row := range rows {
		// Reuse any previously-allocated cells slice for this slot.
		cs := info[i].cells[:0]
		info[i] = rowInfo{}
		if row.Raw != nil {
			w, broken, canWrap := measureCell(row.Raw)
			info[i] = rowInfo{broken: broken, rawW: w, rawCanWrap: canWrap, cells: cs}
			continue
		}
		broken := false
		for _, d := range row.Cells {
			w, rowBroken, canWrap := measureCell(d)
			if rowBroken {
				broken = true
				break
			}
			cs = append(cs, cellInfo{w: w, canWrap: canWrap})
		}
		info[i] = rowInfo{broken: broken, cells: cs}
	}

	// rowFits reports whether row j fits given segment-wide column
	// maxes. A cell only "doesn't fit" if its width exceeds its
	// budget AND its Doc could wrap; pure text running past the line
	// is fine (e.g. a lone trailing // comment).
	rowFits := func(j int, colMaxW []int) bool {
		ri := &info[j]
		if rows[j].Raw != nil {
			return !ri.rawCanWrap || ri.rawW <= avail
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
		return 1, info
	}

	var colMaxW []int
	for i, row := range rows {
		if info[i].broken {
			return i, info
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
				return 1, info
			}
			return i, info
		}
		colMaxW = pred
	}
	return len(rows), info
}

// measureCell measures a cell's broken-mode width and whether its
// Doc could emit a newline. Combines what would otherwise be two
// passes (measureBrokenWidth + docCanWrap): both walk the same tree
// and are always called together from [longestAlignedPrefix]. Returns
// (width, broken, canWrap) where broken means the Doc is
// unflattenable (HardLine/LitLine somewhere — it must render on
// multiple lines), and canWrap means the Doc contains a
// wrapping-capable node (DocLine / DocIfBreak / DocTable) so it
// could emit a newline under some mode even if it isn't strictly
// broken.
//
// Like [measureFlat] it picks the Broken branch of DocIfBreak — so
// a TrailingComma (`IfBreak(",", nil)`) contributes its one comma
// character to the cell width, matching what the renderer actually
// emits in broken-mode.
func measureCell(doc Doc) (width int, broken, canWrap bool) {
	if doc == nil {
		return 0, false, false
	}
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
		case *docText:
			width += d.Width
		case *docLine:
			width += d.AltWidth
			canWrap = true
		case *docHard, *docLitLine:
			return 0, true, true
		case *docCat:
			stack = append(stack, d.Right, d.Left)
		case *docNest:
			stack = append(stack, d.Child)
		case *docGroup:
			stack = append(stack, d.Child)
		case *docIfBreak:
			canWrap = true
			stack = append(stack, d.Broken)
		case *docTable:
			canWrap = true
			for _, row := range d.Rows {
				if row.HasComment || row.DocComment != nil {
					return 0, true, true
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
	return width, false, canWrap
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
		case *docText:
			width += d.Width

		case *docLine:
			// In flat-mode, Line emits its alt text.
			width += d.AltWidth

		case *docHard:
			// A HardLine means this group cannot be flattened.
			return 0, false

		case *docLitLine:
			// A literal newline (multi-line string) also prevents
			// flattening.
			return 0, false

		case *docCat:
			// Left above right in the stack (i.e. processed first).
			stack = append(stack, d.Right, d.Left)

		case *docNest:
			// In flat-mode, there is no indentation to increase.
			stack = append(stack, d.Child)

		case *docGroup:
			stack = append(stack, d.Child)

		case *docIfBreak:
			// In flat-mode, use the flat variant.
			stack = append(stack, d.Flat)

		case *docTable:
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
