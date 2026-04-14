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
	"slices"
	"unicode/utf8"
)

type mode uint8

const (
	modeBreak mode = iota
	modeFlat
)

// Render converts a [Doc] into bytes using the Wadler-Lindig best-fit
// algorithm.
func (cfg Config) Render(doc Doc) []byte {
	r := &renderer{
		width:       cfg.width(),
		indent:      cfg.Indent,
		indentWidth: cfg.indentWidth(),
	}
	r.renderInMode(0, "", modeBreak, false, doc)
	return r.buf.Bytes()
}

type renderer struct {
	// width is the target line width for flat-fit decisions.
	width int
	// indent is the string emitted per nest level on each newline.
	indent string
	// indentWidth is the visual column width of one indent level.
	indentWidth int
	// buf accumulates the rendered output bytes.
	buf bytes.Buffer
	// col is the current column position on the current line, in
	// visual columns (rune count of all emitted text (including
	// indentation) since the last newline.
	col int
	// lastIndent is the indent level (in [docNest] units) of the most
	// recent newline emitted via r.newline. The table layer reads it
	// after rendering a cell to decide where the row's break-point
	// should land - specifically, when a chain [docGroup] inside cell
	// 0 partially breaks, the value goes one level deeper than
	// wherever the chain actually ended.
	lastIndent int

	// renderStack is the explicit evaluation stack used by
	// [renderer.renderInMode] in place of recursion. Reused across
	// calls with stack semantics (save the length on entry, restore on
	// exit) so a nested rendering inside the same renderer does not
	// clobber outer frames.
	renderStack []renderFrame
	// infoScratch caches per-row measurements for table
	// segmentation. Reused across calls with stack semantics like
	// renderStack so a nested table rendering can carve off the rest
	// of the scratch without clobbering the outer frame.
	infoScratch []rowInfo
	// colScratch backs the per-segment column-count and column-max-
	// width arrays in [renderer.renderTableSegment]. Reused across
	// calls with stack semantics like renderStack.
	colScratch []int
}

// renderFrame is one frame on renderInMode's explicit evaluation
// stack.
type renderFrame struct {
	indent     int
	basePrefix string
	mode       mode
	infWidth   bool
	doc        Doc
}

// renderInMode renders a doc using the given indent, mode, and
// infWidth flag. Under infWidth, width-based break decisions see an
// unlimited budget - docGroups pick flat unless a hard line break
// forces them broken. basePrefix is a verbatim string prepended
// before the nest- driven indent on each newline; only [docAtIndent]
// overrides it.
func (r *renderer) renderInMode(indent int, basePrefix string, m mode, infWidth bool, doc Doc) {
	if doc == nil {
		return
	}
	base := len(r.renderStack)
	r.renderStack = append(r.renderStack, renderFrame{
		indent:     indent,
		basePrefix: basePrefix,
		mode:       m,
		infWidth:   infWidth,
		doc:        doc,
	})
	for len(r.renderStack) > base {
		i := len(r.renderStack) - 1
		f := r.renderStack[i]
		r.renderStack = r.renderStack[:i]

		if f.doc == nil {
			continue
		}

		switch d := f.doc.(type) {
		case *docStringLit:
			r.buf.WriteString(d.str)
			r.col += d.width

		case *docLineBreakSoft:
			if f.mode == modeFlat {
				r.buf.WriteString(d.flat)
				r.col += d.flatWidth
			} else {
				r.newline(f.indent, f.basePrefix)
			}

		case *docLineBreakHard:
			r.newline(f.indent, f.basePrefix)

		case *docLineBreakBare:
			r.buf.WriteByte('\n')
			r.col = 0

		case *docCat:
			fLeft, fRight := f, f
			fLeft.doc = d.left
			fRight.doc = d.right
			r.renderStack = append(r.renderStack, fRight, fLeft)

		case *docNest:
			f.indent++
			f.doc = d.child
			r.renderStack = append(r.renderStack, f)

		case *docGroup:
			mode := f.mode
			if mode == modeBreak && (f.infWidth || r.width > r.col) {
				remaining := r.width - r.col
				// In infWidth mode the fit-test sees an unlimited budget
				// so a docGroup always picks flat unless its child
				// contains a hard line break.
				if f.infWidth {
					remaining = 0
				}
				if _, ok := measureFlat(remaining, d.child); ok {
					mode = modeFlat
				}
			}
			f.mode = mode
			f.doc = d.child
			r.renderStack = append(r.renderStack, f)

		case *docInfiniteWidth:
			f.infWidth = true
			f.doc = d.child
			r.renderStack = append(r.renderStack, f)

		case *docFiniteWidth:
			// Reset both infWidth and mode for child so its inner
			// docGroups do their own flat-fit against the real width
			// budget, even when enclosed by a [docInfiniteWidth] scope
			// (which would otherwise force modeFlat throughout via the
			// unlimited budget).
			f.mode = modeBreak
			f.infWidth = false
			f.doc = d.child
			r.renderStack = append(r.renderStack, f)

		case *docAtIndent:
			f.indent = 0
			f.basePrefix = d.prefix
			f.doc = d.child
			r.renderStack = append(r.renderStack, f)

		case *docSwitchMode:
			if f.mode == modeBreak {
				f.doc = d.broken
			} else {
				f.doc = d.flat
			}
			r.renderStack = append(r.renderStack, f)

		case *docTable:
			r.renderTableInMode(f.indent, f.basePrefix, f.mode, f.infWidth, d.rows)
		}
	}
}

// renderTableInMode renders table rows. In flat-mode, cells are
// concatenated with spaces between them. In broken-mode, the table is
// rendered as one or more aligned segments. Rows are partitioned into
// maximal contiguous segments where every row renders without
// newlines (any cell containing [docLineBreakHard] /
// [docLineBreakBare]; or a breakable cell whose flat width exceeds
// its budget), and each segment computes its column widths
// independently. A multi-line or overflowing row therefore "flushes"
// the surrounding alignment rather than stretching the columns of the
// simpler rows around it.
func (r *renderer) renderTableInMode(indent int, basePrefix string, m mode, infWidth bool, rows []Row) {
	if m == modeFlat {
		for i, row := range rows {
			if i > 0 {
				r.renderInMode(0, basePrefix, modeFlat, infWidth, row.Sep)
			}
			if row.Raw != nil {
				r.renderInMode(0, basePrefix, modeFlat, infWidth, row.Raw)
				continue
			}
			for j, cell := range row.Cells {
				if j > 0 {
					r.buf.WriteByte(' ')
					r.col++
				}
				r.renderInMode(0, basePrefix, modeFlat, infWidth, cell)
			}
		}
		return
	}

	avail := max(r.width-indent*r.indentWidth, 1)

	// Render one segment at a time: find the longest prefix of rows
	// that can share column alignment, render it, then continue with
	// the tail. We carve a slice off r.infoScratch with stack
	// semantics and pass it to longestAlignedPrefix to fill in - a
	// nested table rendered inside a cell will carve from the rest of
	// the scratch, so its writes don't clobber ours.
	first := true
	base := len(r.infoScratch)
	for len(rows) > 0 {
		required := base + len(rows)
		if required > cap(r.infoScratch) {
			r.infoScratch = slices.Grow(r.infoScratch, len(rows))
		}
		r.infoScratch = r.infoScratch[:required]
		info := r.infoScratch[base:required]
		n := r.longestAlignedPrefix(rows, avail, infWidth, info)
		r.renderTableSegment(indent, basePrefix, infWidth, rows[:n], info, !first)
		first = false
		rows = rows[n:]
	}
	r.infoScratch = r.infoScratch[:base]
}

// cellInfo models a cell's broken-mode width (what it would render to
// on a single line in broken-mode) and whether its Doc could
// introduce a newline under any mode.
type cellInfo struct {
	width   int
	canWrap bool
}

// rowInfo models per-row measurements. For aligned rows, cells holds
// each cell's width/canWrap in order. On a broken row (a cell
// contains [docLineBreakHard]/[docLineBreakBare]) the slice is
// truncated right before the broken cell, since later cells can't
// contribute to column widths once the row is forced multi-line.
type rowInfo struct {
	broken     bool
	cells      []cellInfo // aligned rows
	rawWidth   int        // raw rows
	rawCanWrap bool       // raw rows
}

// renderTableSegment renders a single sub-table with its own column
// widths. info must have one entry per row with precomputed cell
// widths. emitFirstSep controls whether the first row's Sep is
// rendered (true for segments after the first, so the break between
// sub-tables is honoured).
func (r *renderer) renderTableSegment(indent int, basePrefix string, infiniteWidth bool, rows []Row, info []rowInfo, emitFirstSep bool) {
	numCols := 0
	for _, row := range rows {
		if row.Raw == nil {
			numCols = max(numCols, len(row.Cells))
		}
	}

	// Count how many rows contribute non-nil content to each column,
	// and derive colMaxW from the cached cell widths. A column with
	// only one row has no alignment target, so padding the preceding
	// column for that row is pointless - the one-off cell should hug
	// its predecessor. On a broken row, info[i].cells is truncated
	// right before the first unflattenable cell, so later cells don't
	// contribute to column widths (but still count towards
	// colCount). Nil cells are "reserved" column slots used to keep
	// later columns aligned across rows.
	//
	// colCount and colMaxW are carved off r.colScratch with stack
	// semantics: the carve is restored on exit so a nested
	// renderTableSegment (via cell content) gets fresh slots past
	// ours without clobbering them.
	base := len(r.colScratch)
	need := 2 * numCols
	required := base + need
	if required > cap(r.colScratch) {
		r.colScratch = slices.Grow(r.colScratch, need)
	}
	r.colScratch = r.colScratch[:required]
	clear(r.colScratch[base:required])
	// colCount[i] gives the number of rows that contribute non-nil
	// cells to column i.
	colCount := r.colScratch[base : base+numCols]
	// colMaxW[i] gives the maximum width found of a cell in column i.
	colMaxW := r.colScratch[base+numCols : required]

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
			if ci.width > colMaxW[c] {
				colMaxW[c] = ci.width
			}
		}
	}

	// Under infiniteWidth the table preserves the author's layout
	// regardless of overflow, so neither avail nor useMerged is
	// consulted: avail stays 0, useMerged stays false, and the
	// breakRow check inside the row loop is gated on !infiniteWidth
	// too.
	avail := 0
	useMerged := false
	if !infiniteWidth {
		// alignedWidth is the width every row would render to if every
		// cell stayed flat at this segment's column maxes (sum of
		// column maxes plus one inter-cell space per non-empty column
		// past the first). If that fits in avail the segment renders
		// aligned; if not, rows fall back to their MergedFirstCell.
		alignedWidth := 0
		for c := range numCols {
			if c > 0 && (colMaxW[c] > 0 || colCount[c] > 0) {
				alignedWidth++
			}
			alignedWidth += colMaxW[c]
		}

		avail = max(r.width-indent*r.indentWidth, 1)
		useMerged = alignedWidth > avail
	}

	for i, row := range rows {
		if i > 0 || emitFirstSep {
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.Sep)
		}
		if row.Raw != nil {
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.Raw)
			continue
		}
		if row.DocComment != nil {
			// DocComment carries its own trailing separator (HardLine or
			// BlankLine) so the blank-line-after-doc-comment case
			// renders correctly when the node itself has NewSection.
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.DocComment)
		}
		// Identify the last non-nil cell so trailing reserved slots
		// don't emit spurious trailing whitespace.
		lastNonNilIdx := -1
		for c, cell := range row.Cells {
			if cell != nil {
				lastNonNilIdx = c
			}
		}
		// Reset lastIndent to the row's own indent. As cell 0 renders,
		// any newline it emits will overwrite r.lastIndent with the
		// indent at that newline; the breakRow path below reads it back
		// to place cell 1 one level deeper than where cell 0 ended.
		r.lastIndent = indent
		// Merged-render path: when the segment doesn't fit aligned and
		// this row has a MergedFirstCell, render it in place of cells 0
		// and 1. The merged Doc folds cell 1 into cell 0's deepest Nest
		// so each split point inside cell 0's flat-fit decision
		// includes cell 1. Wadler-Lindig breaks the outermost split
		// first and only descends as deep as needed. Cells past index 1
		// (attrs, comment) continue to render normally with inter-cell
		// spaces.
		if useMerged && row.MergedFirstCell != nil {
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.MergedFirstCell)
			for c := 2; c < len(row.Cells); c++ {
				cell := row.Cells[c]
				if cell == nil || (colMaxW[c] == 0 && colCount[c] == 0) {
					continue
				}
				if c > lastNonNilIdx {
					break
				}
				r.buf.WriteByte(' ')
				r.col++
				r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, cell)
			}
			continue
		}
		// Decide whether to break the row before cell 1. The row
		// breaks when ALL of:
		//   - we don't have infiniteWidth;
		//   - the row didn't already break in measurement (ri.broken);
		//   - AllowRowBreak is set (caller opt-in);
		//   - the row has no MergedFirstCell (those go through the
		//     merged-render path above);
		//   - cell 1 is atomic - no Group/Table inside (keeps
		//     `key: {…}` hugging intact for struct/list values);
		//   - the row's total flat width exceeds avail.
		// longestAlignedPrefix has already segmented around such
		// rows so siblings don't pad to the broken row's wide key.
		//
		// When the row does break, cell 1 lands at r.lastIndent+1
		// - one level deeper than wherever cell 0's last newline
		// placed us (or one deeper than the row's indent if cell 0
		// stayed flat).
		breakRow := false
		ri := info[i]
		if !infiniteWidth && !ri.broken && row.AllowRowBreak && row.MergedFirstCell == nil && len(ri.cells) >= 2 && !ri.cells[1].canWrap {
			totalFlat := 0
			for c, ci := range ri.cells {
				if c > 0 {
					totalFlat++
				}
				totalFlat += ci.width
			}
			breakRow = totalFlat > avail
		}
		for c, cell := range row.Cells {
			// Skip columns where no row in the segment contributes
			// content at all - emit nothing, no separator, no pad.
			// colMaxW[c] == 0 is insufficient on its own: a broken
			// cell (value contains a HardLine) also has width 0 in
			// ri.cells because it's truncated; colCount tells us
			// whether any row has a non-nil cell at this column.
			if colMaxW[c] == 0 && colCount[c] == 0 {
				continue
			}
			// Skip everything past the row's last non-nil cell: no
			// trailing separator + reserved-slot padding.
			if c > lastNonNilIdx {
				break
			}
			cellIndent := indent
			if breakRow && c == 1 {
				// Drop cell 1 onto a new line, one level deeper than
				// wherever cell 0 last broke (or one deeper than the
				// row's indent if cell 0 stayed flat).
				breakIndent := r.lastIndent + 1
				r.newline(breakIndent, basePrefix)
				cellIndent = breakIndent
			} else if c >= 1 {
				r.buf.WriteByte(' ')
				r.col++
				// If a previous cell emitted a newline (e.g. cell 0
				// contained a key like `g:\n abcde:` that broke),
				// this cell sits inline after that newline's
				// indented position. Promote cellIndent to
				// r.lastIndent so any inner break emitted while
				// rendering this cell (a struct body break, a list
				// explode, etc.) lands at the right column instead
				// of at the row's own indent.
				cellIndent = max(cellIndent, r.lastIndent)
			}
			colStart := r.col
			r.renderInMode(cellIndent, basePrefix, modeBreak, infiniteWidth, cell)
			if c == lastNonNilIdx {
				continue
			}
			// When this row will break before cell 1, cell 0 must not be
			// padded to colMaxW: the trailing spaces would land just
			// before the break newline, leaving stray whitespace at end
			// of line.
			if c == 0 && breakRow {
				continue
			}
			// Skip padding if no later column in this segment needs
			// alignment - a later dense column (colCount > 1) needs this
			// column padded so its content lines up; a column that's
			// sparse or entirely empty does not. We look past empty
			// columns (colMaxW == 0 - these render nothing) so that
			// comments in column 3 can still align when column 2 happens
			// to be empty for every row in the segment.
			paddingNeeded := false
			for d := c + 1; d < numCols; d++ {
				if colMaxW[d] == 0 {
					continue
				}
				paddingNeeded = colCount[d] > 1
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
	r.colScratch = r.colScratch[:base]
}

// longestAlignedPrefix returns the length of the longest prefix of
// rows that can share column alignment without forcing newlines. The
// caller provides info (same length as rows); longestAlignedPrefix
// populates info for every row scanned. Always returns at least 1: a
// "boundary" row forms a one-row segment on its own.
//
// A row is a "boundary" when either:
//   - a cell (or a row's Raw Doc) is unflattenable: it contains
//     [docLineBreakHard] / [docLineBreakBare]; or
//   - a cell whose Doc could wrap (contains a [docLineBreakSoft],
//     [docSwitchMode], [docGroup], or [docTable]) has optimistic flat
//     width (what [measureCell] returns: the width assuming every
//     inner [docGroup] picks flat) exceeding the budget at its column
//     position: some inner [docGroup] cannot fit flat and
//     Wadler-Lindig will break it, emitting newlines.
//
// Cells whose Doc is pure text (no wrapping element) never emit
// newlines even if they extend past the line width - a lone
// trailing // comment is the canonical example - so they do not
// trigger a boundary on their own.
func (r *renderer) longestAlignedPrefix(rows []Row, avail int, infiniteWidth bool, info []rowInfo) int {
	if len(rows) == 0 {
		return 0
	}

	// Precompute per-cell width and wrap-ability once.
	for i, row := range rows {
		// cells may already be non-nil so truncate to permit memory
		// reuse.
		cs := info[i].cells[:0]
		if row.Raw != nil {
			width, broken, canWrap := measureCell(row.Raw)
			info[i] = rowInfo{
				broken:     broken,
				rawWidth:   width,
				rawCanWrap: canWrap,
				cells:      cs,
			}
			continue
		}
		broken := false
		for _, d := range row.Cells {
			width, rowBroken, canWrap := measureCell(d)
			if rowBroken {
				broken = true
				break
			}
			cs = append(cs, cellInfo{width: width, canWrap: canWrap})
		}
		info[i] = rowInfo{broken: broken, cells: cs}
	}

	// rowFits reports whether row j fits given segment-wide column
	// max-widths. A cell only "doesn't fit" if its width exceeds its
	// budget AND its Doc could wrap; pure text running past the line
	// is fine (e.g. a lone trailing // comment).
	//
	// An AllowRowBreak row whose own flat width exceeds avail is also
	// reported as not fitting, even when its cells are pure text. Such
	// rows render via renderTableSegment's breakRow path and must form
	// their own segment so siblings don't pad to the broken row's
	// (wide) cell-0 width.
	rowFits := func(j int, colMaxW []int) bool {
		ri := &info[j]
		r := rows[j]
		if r.Raw != nil {
			return !ri.rawCanWrap || ri.rawWidth <= avail
		}
		cellStartCol := 0
		totalFlatWidth := 0
		for c, ci := range ri.cells {
			if c > 0 {
				cellStartCol += colMaxW[c-1] + 1
				totalFlatWidth++
			}
			if ci.canWrap && ci.width > avail-cellStartCol {
				return false
			}
			totalFlatWidth += ci.width
		}
		return infiniteWidth || !r.AllowRowBreak || r.MergedFirstCell != nil || len(ri.cells) < 2 || ri.cells[1].canWrap || totalFlatWidth <= avail
	}

	var colMaxW []int
	for i, row := range rows {
		if info[i].broken {
			return max(i, 1) // the prefix is always at least 1 row long.
		}
		// Predict new column maxes if this row is admitted.
		grew := false
		if row.Raw == nil {
			for c, ci := range info[i].cells {
				switch {
				case c >= len(colMaxW):
					grew = true
					colMaxW = append(colMaxW, ci.width)
				case ci.width > colMaxW[c]:
					grew = true
					colMaxW[c] = ci.width
				}
			}
		}
		// Row i must fit under colMaxW. It might not do if it's Raw.
		if !rowFits(i, colMaxW) {
			return max(i, 1)
		}
		// If colMaxW grew, re-check every previously-admitted row too:
		// a widened column shifts later cells rightward and may newly
		// push an earlier row past avail.
		if grew {
			for j := range i {
				if !rowFits(j, colMaxW) {
					return i
				}
			}
		}
	}
	return len(rows)
}

// measureCell measures the smallest width the cell's Doc could render
// to (the width assuming every inner [docGroup] picks flat-mode), and
// whether its Doc could emit a newline. Returns (width, broken,
// canWrap) where broken means the Doc is unflattenable (doc contains
// [docLineBreakHard] / [docLineBreakBare] somewhere - it must render
// on multiple lines), and canWrap means the Doc contains a
// wrapping-capable node ([docLineBreakSoft] / [docSwitchMode] /
// [docTable]) so it could emit a newline.
//
// [docSwitchMode] resolves the same way the renderer would: when it
// sits inside a [docGroup], that group can choose flat-mode and the flat
// branch is taken; outside any group, the cell is rendered in modeBreak
// and the broken branch is taken.
func measureCell(doc Doc) (width int, broken, canWrap bool) {
	if doc == nil {
		return 0, false, false
	}
	// inInfiniteWidth tracks whether we are inside a
	// [docInfiniteWidth] scope. [docLineBreakSoft], [docSwitchMode],
	// and inner [docTable]s only mark the cell as canWrap if they
	// could break at render time: in an infinite-width scope they
	// cannot (the surrounding [docGroup]'s flat-fit succeeds with the
	// unlimited budget, so it stays flat and [docLineBreakSoft]s emit
	// their flat content). [docFiniteWidth] resets the scope so its
	// inner content's canWrap is reported truthfully.
	type frame struct {
		doc             Doc
		inGroup         bool
		inInfiniteWidth bool
	}
	var stackBuf [32]frame
	stack := stackBuf[:1]
	stack[0] = frame{doc: doc}
	for len(stack) > 0 {
		top := len(stack) - 1
		f := stack[top]
		stack = stack[:top]
		if f.doc == nil {
			continue
		}
		switch d := f.doc.(type) {
		case *docStringLit:
			width += d.width
		case *docLineBreakSoft:
			width += d.flatWidth
			if !f.inInfiniteWidth {
				canWrap = true
			}
		case *docLineBreakHard, *docLineBreakBare:
			return 0, true, true
		case *docCat:
			fLeft, fRight := f, f
			fLeft.doc = d.left
			fRight.doc = d.right
			stack = append(stack, fRight, fLeft)
		case *docNest:
			f.doc = d.child
			stack = append(stack, f)
		case *docGroup:
			f.doc = d.child
			f.inGroup = true
			stack = append(stack, f)
		case *docInfiniteWidth:
			f.doc = d.child
			f.inInfiniteWidth = true
			stack = append(stack, f)
		case *docFiniteWidth:
			f.doc = d.child
			f.inInfiniteWidth = false
			stack = append(stack, f)
		case *docSwitchMode:
			if !f.inInfiniteWidth {
				canWrap = true
			}
			if f.inGroup {
				f.doc = d.flat
			} else {
				f.doc = d.broken
			}
			stack = append(stack, f)
		case *docTable:
			if !f.inInfiniteWidth {
				canWrap = true
			}
			for _, row := range d.rows {
				if row.HasComment || row.DocComment != nil {
					return 0, true, true
				}
			}
			for i := len(d.rows) - 1; i >= 0; i-- {
				row := d.rows[i]
				if row.Raw != nil {
					f.doc = row.Raw
					stack = append(stack, f)
				} else {
					for j := len(row.Cells) - 1; j >= 0; j-- {
						f.doc = row.Cells[j]
						stack = append(stack, f)
						if j > 0 {
							f.doc = spaceLit
							stack = append(stack, f)
						}
					}
				}
				if i > 0 && row.Sep != nil {
					f.doc = row.Sep
					stack = append(stack, f)
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
		if remaining > 0 && width > remaining {
			return 0, false
		}
		top := len(stack) - 1
		d := stack[top]
		stack = stack[:top]

		if d == nil {
			continue
		}

		switch d := d.(type) {
		case *docStringLit:
			width += d.width

		case *docLineBreakSoft:
			width += d.flatWidth // We're in flat-mode.

		case *docLineBreakHard, *docLineBreakBare:
			// A line break means this group cannot be flattened.
			return 0, false

		case *docCat:
			// Left after right in the stack (i.e. processed first).
			stack = append(stack, d.right, d.left)

		case *docNest:
			// In flat-mode, there is no indentation to increase.
			stack = append(stack, d.child)

		case *docGroup:
			stack = append(stack, d.child)

		case *docInfiniteWidth:
			stack = append(stack, d.child)

		case *docFiniteWidth:
			stack = append(stack, d.child)

		case *docAtIndent:
			// In flat-mode, no changes to indentation.
			stack = append(stack, d.child)

		case *docSwitchMode:
			stack = append(stack, d.flat) // We're in flat-mode.

		case *docTable:
			// A // comment in any row runs to end of line and would
			// swallow subsequent tokens in flat-mode. Force break.
			for _, row := range d.rows {
				if row.HasComment || row.DocComment != nil {
					return 0, false
				}
			}
			// Measure table in flat-mode. Because this is a stack, we
			// want to work backwards so that we end up with the first
			// cell of the first row at the top of the stack.
			for i := len(d.rows) - 1; i >= 0; i-- {
				row := d.rows[i]
				if row.Raw != nil {
					stack = append(stack, row.Raw)

				} else {
					for j := len(row.Cells) - 1; j >= 0; j-- {
						stack = append(stack, row.Cells[j])
						if j > 0 {
							stack = append(stack, spaceLit)
						}
					}
				}
				if i > 0 && row.Sep != nil {
					stack = append(stack, row.Sep)
				}
			}
		}
	}
	if remaining > 0 && width > remaining {
		return 0, false
	}
	return width, true
}

// newline writes a newline followed by basePrefix and the configured
// indent string repeated indent times. basePrefix is written verbatim
// - used for multi-line string body indentation.
func (r *renderer) newline(indent int, basePrefix string) {
	r.buf.WriteByte('\n')
	r.buf.WriteString(basePrefix)
	for range indent {
		r.buf.WriteString(r.indent)
	}
	r.col = utf8.RuneCountInString(basePrefix) + indent*r.indentWidth
	r.lastIndent = indent
}
