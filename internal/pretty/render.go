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
	"iter"
	"math"
	"slices"
	"unicode/utf8"
)

type mode uint8

const (
	modeBreak mode = iota
	modeFlat
)

// Render converts a [doc] into bytes using the Wadler-Lindig best-fit
// algorithm.
func (cfg Config) Render(doc doc) []byte {
	r := &renderer{
		width:       cfg.width(),
		indent:      cfg.Indent,
		indentWidth: cfg.indentWidth(),
	}
	r.renderInMode(0, "", modeBreak, false, doc)
	return r.buf.Bytes()
}

type renderer struct {
	// immutable fields:

	// width is the target line width for flat-fit decisions.
	width int
	// indent is the string emitted per nest level on each newline.
	indent string
	// indentWidth is the visual column width of one indent level.
	indentWidth int
	// buf accumulates the rendered output bytes.
	buf bytes.Buffer

	// mutable fields:

	// col is the current column position on the current line, in
	// visual columns (rune count of all emitted text (including
	// indentation) since the last newline.
	col int
	// lastIndent is the indent level (in [docNest] units) of the most
	// recent newline we emitted via r.newline. The table layer reads it
	// after rendering a cell to decide where the row's break-point
	// should land - specifically, when a chain [docGroup] inside cell
	// 0 partially breaks, we drop the value one level deeper than
	// wherever the chain actually ended.
	lastIndent int

	// renderStack is the explicit evaluation stack [renderer.renderInMode]
	// uses in place of recursion. We reuse it across calls with stack
	// semantics (save the length on entry, restore on exit) so a nested
	// rendering inside the same renderer does not clobber outer frames.
	renderStack stack[renderFrame]
	// infoScratch caches per-row measurements for table
	// segmentation. We reuse it across calls with stack semantics like
	// renderStack, so a nested table rendering can carve off the rest
	// of the scratch without clobbering the outer frame.
	infoScratch []rowInfo
	// colScratch backs the per-segment column-count and column-max-
	// width arrays in [renderer.renderTableSegment]. We reuse it across
	// calls with stack semantics like renderStack.
	colScratch []int
}

// walkState is the mode/flag portion of a Doc-walker frame.
type walkState struct {
	mode mode
	// infWidth is set inside a [docInfiniteWidth] scope: there,
	// width-based break decisions see an unlimited budget, so docGroups
	// pick flat unless a hard line break forces them broken.
	infWidth bool
	// nextGroupNoop is set by [docNextGroupNoop] when it is entered in
	// modeBreak. The next [docGroup] consumes it: rather than running
	// its usual flat-fit retry, the group leaves the frame's mode
	// (modeBreak) untouched, clears this flag, and pushes its child -
	// functionally a no-op. We carry this flag down to the children of
	// every other Doc type while descending (except docTable, whose
	// rows and cells start fresh), so the "next group encountered while
	// descending" semantics work even when the wrapped subtree's root is
	// not literally a group node.
	nextGroupNoop bool
}

// enterNextGroupNoop updates s for descending into a
// [docNextGroupNoop]'s child, arming the flag so we neutralise the
// next [docGroup] on the descent.
//
// We only arm the flag in modeBreak. In modeFlat a docGroup is already
// a no-op (no flat-fit retry fires), and the flag must *not* survive:
// an intermediate [docFiniteWidth] resets the mode to modeBreak before
// the docGroup runs, and that docGroup should pick modeFlat for content
// that fits at the current column rather than be neutralised.
func (s *walkState) enterNextGroupNoop() {
	if s.mode == modeBreak {
		s.nextGroupNoop = true
	}
}

// groupRunsFitTest updates s for descending into a [docGroup]'s child
// and reports whether the group runs its flat-fit decision. An armed
// nextGroupNoop neutralises the group: we consume the flag and leave
// the mode untouched (necessarily modeBreak, since we only arm the flag
// in modeBreak), so the group returns false. A group entered in
// modeFlat stays flat with no retry and returns false. Otherwise -
// modeBreak with no armed flag - the group may flip to modeFlat, so it
// returns true.
func (s *walkState) groupRunsFitTest() bool {
	if s.nextGroupNoop {
		s.nextGroupNoop = false
		return false
	}
	return s.mode == modeBreak
}

// enterInfiniteWidth updates s for descending into a
// [docInfiniteWidth]'s child.
func (s *walkState) enterInfiniteWidth() {
	s.infWidth = true
}

// enterFiniteWidth updates s for descending into a [docFiniteWidth]'s
// child: we reset both infWidth and mode so inner docGroups do their
// own flat-fit against the real width budget, even when enclosed by a
// [docInfiniteWidth] scope (which would otherwise force modeFlat
// throughout via the unlimited budget). We leave an armed nextGroupNoop
// armed, so it can still neutralise the docGroup one level below the
// docFiniteWidth.
func (s *walkState) enterFiniteWidth() {
	s.mode = modeBreak
	s.infWidth = false
}

// switchModeBranch returns the branch of d selected under s.
func (s walkState) switchModeBranch(d *docSwitchMode) doc {
	if s.mode == modeBreak {
		return d.broken
	}
	return d.flat
}

// renderFrame is one frame on renderInMode's explicit evaluation
// stack.
type renderFrame struct {
	indent     int
	basePrefix string
	walkState
	doc doc
}

// renderInMode renders a doc using the given indent, mode, and
// infWidth flag. Under infWidth, width-based break decisions see an
// unlimited budget - docGroups pick flat unless a hard line break
// forces them broken. basePrefix is a verbatim string we prepend
// before the nest-driven indent on each newline; only [docAtIndent]
// overrides it.
func (r *renderer) renderInMode(indent int, basePrefix string, m mode, infWidth bool, doc doc) {
	if doc == nil {
		return
	}
	base := len(r.renderStack)
	r.renderStack.push(renderFrame{
		indent:     indent,
		basePrefix: basePrefix,
		walkState:  walkState{mode: m, infWidth: infWidth},
		doc:        doc,
	})
	for len(r.renderStack) > base {
		f := r.renderStack.pop()

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
			r.renderStack.push(fRight, fLeft)

		case *docNest:
			f.indent++
			f.doc = d.child
			r.renderStack.push(f)

		case *docGroup:
			// We only run the fit test in modeBreak with budget to spare
			// (r.width > r.col); in infWidth mode it sees an unlimited
			// budget, so a docGroup always picks flat unless its child
			// contains a hard line break.
			if f.groupRunsFitTest() && (f.infWidth || r.width > r.col) {
				remaining := r.width - r.col
				if f.infWidth {
					remaining = 0
				}
				if w, broken, _ := measureDoc(d.child, modeFlat, remaining); !broken && (f.infWidth || w <= remaining) {
					f.mode = modeFlat
				}
			}
			f.doc = d.child
			r.renderStack.push(f)

		case *docNextGroupNoop:
			f.enterNextGroupNoop()
			f.doc = d.child
			r.renderStack.push(f)

		case *docInfiniteWidth:
			f.enterInfiniteWidth()
			f.doc = d.child
			r.renderStack.push(f)

		case *docFiniteWidth:
			f.enterFiniteWidth()
			f.doc = d.child
			r.renderStack.push(f)

		case *docAtIndent:
			f.indent = 0
			f.basePrefix = d.prefix
			f.doc = d.child
			r.renderStack.push(f)

		case *docSwitchMode:
			f.doc = f.switchModeBranch(d)
			r.renderStack.push(f)

		case *docTable:
			r.renderTableInMode(f, d)

		case *docBodyShape:
			r.renderBodyShape(f, d)
		}
	}
}

// renderBodyShape implements the per-list-level layout decision for
// [docBodyShape], selecting one of three shapes:
//
//   - Entered in modeFlat: we render body in modeFlat, yielding the flat
//     shape `[body]`, and suppress trailingComma.
//
//   - Entered in modeBreak with body's flat width fitting at indent+1's
//     budget: we render the indented shape - HardLine at indent+1, body
//     in modeFlat, trailingComma (if non-nil), HardLine at the outer
//     indent.
//
//   - Entered in modeBreak with body's flat width not fitting: we render
//     the hug shape - body in modeBreak at the outer indent with no
//     surrounding breaks, so each [docNextGroupNoop]-wrapped cell
//     renders its own broken shape, chained by inter-cell spaces.
func (r *renderer) renderBodyShape(f renderFrame, d *docBodyShape) {
	if f.mode == modeFlat {
		// flat shape: body in modeFlat, brackets adjacent (the
		// enclosing Cat handles them).
		f.doc = d.body
		r.renderStack.push(f)
		return
	}

	outerIndent := f.indent
	newIndent := outerIndent + 1
	avail := max(r.width-newIndent*r.indentWidth, 1)

	if w, broken, _ := measureDoc(d.body, modeFlat, avail); broken || w > avail {
		// hug shape: body in modeBreak at the outer indent, no
		// surrounding breaks. docNextGroupNoop-wrapped cells inside
		// body convert this modeBreak into their cells' own broken
		// renderings.
		f.mode = modeBreak
		f.doc = d.body
		r.renderStack.push(f)
		return
	}

	// indented shape. Pushes are LIFO: we want to pop in order
	// opening HardLine, body, trailingComma (if any), closing
	// HardLine, so we push in reverse.
	closing := f
	closing.indent = outerIndent
	closing.doc = lineBreakHard
	r.renderStack.push(closing)

	if d.trailingComma != nil {
		comma := f
		comma.indent = newIndent
		comma.mode = modeFlat
		comma.doc = d.trailingComma
		r.renderStack.push(comma)
	}

	bodyFrame := f
	bodyFrame.indent = newIndent
	bodyFrame.mode = modeFlat
	bodyFrame.doc = d.body
	r.renderStack.push(bodyFrame)

	opening := f
	opening.indent = newIndent
	opening.doc = lineBreakHard
	r.renderStack.push(opening)
}

// renderTableInMode renders table rows. In flat-mode we concatenate
// cells with spaces between them. In broken-mode we render the table
// as one or more aligned segments: we partition rows into maximal
// contiguous segments where every row renders without newlines (no
// cell containing [docLineBreakHard] / [docLineBreakBare], and no
// breakable cell whose flat width exceeds its budget), and each
// segment computes its column widths independently. A multi-line or
// overflowing row therefore "flushes" the surrounding alignment rather
// than stretching the columns of the simpler rows around it.
func (r *renderer) renderTableInMode(f renderFrame, d *docTable) {
	rows := d.rows
	if f.mode == modeFlat {
		for i, row := range rows {
			if i > 0 {
				r.renderInMode(0, f.basePrefix, modeFlat, f.infWidth, row.sep)
			}
			if row.raw != nil {
				r.renderInMode(0, f.basePrefix, modeFlat, f.infWidth, row.raw)
				continue
			}
			for j, cell := range row.cells {
				if j > 0 {
					r.buf.WriteByte(' ')
					r.col++
				}
				r.renderInMode(0, f.basePrefix, modeFlat, f.infWidth, cell)
			}
		}
		return
	}

	avail := max(r.width-f.indent*r.indentWidth, 1)

	// We render one segment at a time: find the longest prefix of rows
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
		n := r.longestAlignedPrefix(rows, avail, f.infWidth, info)
		r.renderTableSegment(f.indent, f.basePrefix, f.infWidth, rows[:n], info, !first)
		first = false
		rows = rows[n:]
	}
	r.infoScratch = r.infoScratch[:base]
}

// cellInfo models a cell's broken-mode width (what it would render to
// on a single line in broken-mode) and whether its Doc could
// introduce a newline under any mode.
type cellInfo struct {
	width    int
	canBreak bool
}

// rowInfo models per-row measurements. For aligned rows, cells holds
// each cell's width/canBreak in order. On a broken row (a cell
// contains [docLineBreakHard]/[docLineBreakBare]) we truncate the slice
// right before the broken cell, since later cells can't contribute to
// column widths once the row is forced multi-line.
type rowInfo struct {
	broken      bool
	cells       []cellInfo // aligned rows
	rawWidth    int        // only set by [row]s with non-nil raw
	rawCanBreak bool       // only set by [row]s with non-nil raw
}

// rowColWidths returns an iterator over (column, width) pairs giving
// the width each measured cell of an aligned row contributes to its
// column's running max. A cell only widens its column when the row has
// content after it: a column's max width exists to align the cells that
// follow it across rows, so the row's last non-nil cell (and anything
// past it) contributes 0. E.g. a long comment-less value sitting among
// rows that do carry a trailing comment must *not* push those later
// columns out. On a broken row ri.cells is truncated right before the
// broken cell, so later cells contribute nothing here either.
//
// We iterate the same pairs both when accumulating the column max
// during rendering and when predicting it during segmentation, so the
// predicted cell positions match what we pad to, by construction.
func rowColWidths(row row, ri rowInfo) iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		lastNonNilIdx := -1
		for c, cell := range row.cells {
			if cell != nil {
				lastNonNilIdx = c
			}
		}
		for c, ci := range ri.cells {
			w := ci.width
			if c >= lastNonNilIdx {
				w = 0
			}
			if !yield(c, w) {
				return
			}
		}
	}
}

// rowFlatWidth returns an aligned row's total flat width for the
// row-break decision: the widths of the cells that count toward it
// (see [countsTowardRowWidth]) plus one inter-cell space per counted
// column past the first. We call this from both segmentation and
// rendering with the same input, so the two agree on which rows
// overflow, by construction.
func rowFlatWidth(ri rowInfo) int {
	w := 0
	for c, ci := range ri.cells {
		if !countsTowardRowWidth(c, ci) {
			continue
		}
		if c > 0 {
			w++
		}
		w += ci.width
	}
	return w
}

// renderTableSegment renders a single sub-table with its own column
// widths. info must have one entry per row with precomputed cell
// widths. emitFirstSep controls whether we render the first row's Sep
// (true for segments after the first, so we honour the break between
// sub-tables).
func (r *renderer) renderTableSegment(indent int, basePrefix string, infiniteWidth bool, rows []row, info []rowInfo, emitFirstSep bool) {
	numCols := 0
	for _, row := range rows {
		if row.raw == nil {
			numCols = max(numCols, len(row.cells))
		}
	}

	// We count how many rows contribute non-nil content to each column,
	// and derive colMaxW from the cached cell widths. A column with only
	// one row has no alignment target, so padding the preceding column
	// for that row is pointless - the one-off cell should hug its
	// predecessor. On a broken row info[i].cells is truncated right
	// before the first unflattenable cell, so later cells don't
	// contribute to column widths (but still count towards colCount).
	// Nil cells are "reserved" column slots that keep later columns
	// aligned across rows.
	//
	// We carve colCount and colMaxW off r.colScratch with stack
	// semantics: we restore the carve on exit, so a nested
	// renderTableSegment (via cell content) gets fresh slots past ours
	// without clobbering them.
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
		if row.raw != nil {
			continue
		}
		for c, cell := range row.cells {
			if cell != nil {
				colCount[c]++
			}
		}
		// Per-cell colMaxW contributions come from [rowColWidths]: the
		// row's last non-nil cell (and anything past it) contributes 0,
		// though colCount above still counts it.
		for c, w := range rowColWidths(row, info[i]) {
			colMaxW[c] = max(colMaxW[c], w)
		}
	}

	// Under infiniteWidth the table preserves the author's layout
	// regardless of overflow, so we consult neither avail nor useMerged:
	// avail stays 0, useMerged stays false, and we gate the breakRow
	// check inside the row loop on !infiniteWidth too.
	avail := 0
	useMerged := false
	if !infiniteWidth {
		// alignedWidth is the width every row would render to if every
		// cell stayed flat at this segment's column maxes (sum of
		// column maxes plus one inter-cell space per non-empty column
		// past the first). If that fits in avail we render the segment
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
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.sep)
		}
		if row.raw != nil {
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.raw)
			continue
		}
		if row.docComment != nil {
			// DocComment carries its own trailing separator (HardLine or
			// BlankLine) so the blank-line-after-doc-comment case
			// renders correctly when the node itself has NewSection.
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.docComment)
		}
		// Identify the last non-nil cell so trailing reserved slots
		// don't emit spurious trailing whitespace.
		lastNonNilIdx := -1
		for c, cell := range row.cells {
			if cell != nil {
				lastNonNilIdx = c
			}
		}
		// Reset lastIndent to the row's own indent. As cell 0 renders,
		// any newline it emits overwrites r.lastIndent with the indent at
		// that newline; the breakRow path below reads it back to place
		// cell 1 one level deeper than where cell 0 ended.
		r.lastIndent = indent
		// Merged-render path: when the segment doesn't fit aligned and
		// this row has a MergedFirstCell, we render it in place of cells 0
		// and 1. The merged Doc folds cell 1 into cell 0's deepest Nest,
		// so each split point inside cell 0's flat-fit decision includes
		// cell 1. Wadler-Lindig breaks the outermost split first and only
		// descends as deep as needed. Cells past index 1 (attrs, comment)
		// continue to render normally with inter-cell spaces.
		if useMerged && row.mergedFirstCell != nil {
			r.renderInMode(indent, basePrefix, modeBreak, infiniteWidth, row.mergedFirstCell)
			for c := 2; c < len(row.cells); c++ {
				cell := row.cells[c]
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
		// Decide whether to break the row before cell 1. The row breaks
		// when ALL of:
		//
		//   - we don't have infiniteWidth;
		//   - the row didn't already break in measurement (ri.broken);
		//   - AllowRowBreak is set (caller opt-in);
		//   - the row has no MergedFirstCell (those go through the
		//     merged-render path above);
		//   - cell 1 is atomic - no Group/Table inside (this keeps `key:
		//     {...}` hugging intact for struct/list values);
		//   - the row's total flat width exceeds avail.
		//
		// longestAlignedPrefix has already segmented around such rows,
		// so siblings don't pad to the broken row's wide key.
		//
		// When the row does break, cell 1 lands at r.lastIndent+1 - one
		// level deeper than wherever cell 0's last newline placed us (or
		// one deeper than the row's indent if cell 0 stayed flat).
		breakRow := false
		ri := info[i]
		if !infiniteWidth && !ri.broken && row.allowRowBreak && row.mergedFirstCell == nil && len(ri.cells) >= 2 && !ri.cells[1].canBreak {
			breakRow = rowFlatWidth(ri) > avail
		}
		// firstCol tracks whether we've already processed (rendered or
		// padded for) a non-skipped column in this row. The leading
		// column doesn't take an inter-cell space; later columns do.
		// Consider a row whose first few cells are nil AND whose columns
		// are empty across the segment (a continuation row holding only a
		// late-column comment, say): it hits the `continue` skip, and we
		// treat the first non-skipped column as the row's leading column,
		// emitting no leading space.
		firstCol := true
		for c, cell := range row.cells {
			// Skip columns where no row in the segment contributes
			// content at all - we emit nothing, no separator, no pad.
			// colMaxW[c] == 0 does *not* suffice on its own: a broken
			// cell (value contains a HardLine) also has width 0 in
			// ri.cells because it's truncated, so we use colCount to tell
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
				// We drop cell 1 onto a new line, one level deeper than
				// wherever cell 0 last broke (or one deeper than the
				// row's indent if cell 0 stayed flat).
				breakIndent := r.lastIndent + 1
				r.newline(breakIndent, basePrefix)
				cellIndent = breakIndent
			} else if !firstCol {
				r.buf.WriteByte(' ')
				r.col++
				// If a previous cell emitted a newline (e.g. cell 0
				// contained a key like `g:\n abcde:` that broke), this
				// cell sits inline after that newline's indented
				// position. We promote cellIndent to r.lastIndent so any
				// inner break emitted while rendering this cell (a struct
				// body break, a list explode, etc.) lands at the right
				// column instead of at the row's own indent.
				cellIndent = max(cellIndent, r.lastIndent)
			}
			firstCol = false
			colStart := r.col
			r.renderInMode(cellIndent, basePrefix, modeBreak, infiniteWidth, cell)
			if c == lastNonNilIdx {
				continue
			}
			// When this row will break before cell 1, we must *not* pad
			// cell 0 to colMaxW: the trailing spaces would land just
			// before the break newline, leaving stray whitespace at end
			// of line.
			if c == 0 && breakRow {
				continue
			}
			// Skip padding if no later column in this segment needs
			// alignment. A later dense column (colCount > 1) appears in
			// more than one row, so its content must line up across rows
			// - that requires every column before it to be padded to its
			// max width. A column that's sparse (one row) or entirely
			// empty imposes no such constraint on its own. We test
			// emptiness with colCount, not colMaxW: a column may have
			// content (colCount > 0) yet zero max width because every
			// cell in it is the last in its row and so was excluded from
			// colMaxW above - that column still needs the columns before
			// it padded so its cells align. So we look past empty columns
			// (colCount == 0 - these render nothing) AND past sparse
			// columns to reach a later dense one. Consider a continuation
			// row holding only a late-column comment: its nil leading
			// cells must still pad to their columns' widths so the
			// comment lands in the same column as the dense comment
			// column in the rows above it.
			paddingNeeded := false
			for d := c + 1; d < numCols; d++ {
				if colCount[d] == 0 {
					continue
				}
				if colCount[d] > 1 {
					paddingNeeded = true
					break
				}
			}
			if !paddingNeeded {
				continue
			}
			// Pad to column max width. If the cell broke (emitted
			// newlines), r.col - colStart gives the content width on the
			// last line. If r.col < colStart (the last line ends before
			// the start column) padding is not meaningful, so we skip
			// it.
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

// countsTowardRowWidth reports whether cell c contributes to the flat
// width we use to decide whether a row must break (drop its value onto
// a new line) or be isolated into its own segment. Cells 0 (key) and 1
// (value), and any break-capable cell, always count. Pure-text trailing
// decoration - attributes and comments, placed at column >= 2 - does
// *not* count: it runs to end of line and breaking the value onto its
// own line cannot relocate it, so if we let it drive the decision we'd
// only produce a pointless break.
func countsTowardRowWidth(c int, ci cellInfo) bool {
	return c < 2 || ci.canBreak
}

// longestAlignedPrefix returns the length of the longest prefix of
// rows that can share column alignment without forcing newlines. The
// caller provides info (same length as rows); we populate info for
// every row scanned. We always return at least 1: a "boundary" row
// forms a one-row segment on its own.
//
// A row is a "boundary" when either:
//
//   - a cell (or a row's Raw Doc) is unflattenable: it contains
//     [docLineBreakHard] / [docLineBreakBare]; or
//   - a cell whose Doc could break (contains a [docLineBreakSoft],
//     [docSwitchMode], [docGroup], or [docTable]) has optimistic flat
//     width (what [measureDoc] returns: the width assuming every inner
//     [docGroup] picks flat) exceeding the budget at its column
//     position, i.e. some inner [docGroup] cannot fit flat and
//     Wadler-Lindig will break it, emitting newlines.
//
// Cells whose Doc is pure text (no break-capable primitive) never emit
// newlines even when they extend past the line width - a lone trailing
// // comment is the canonical example - so they do not trigger a
// boundary on their own.
func (r *renderer) longestAlignedPrefix(rows []row, avail int, infiniteWidth bool, info []rowInfo) int {
	if len(rows) == 0 {
		return 0
	}

	// Precompute per-cell width and break-ability once.
	for i, row := range rows {
		// cells may already be non-nil, so we truncate to permit memory
		// reuse.
		cs := info[i].cells[:0]
		if row.raw != nil {
			width, broken, canBreak := measureDoc(row.raw, modeBreak, 0)
			info[i] = rowInfo{
				broken:      broken,
				rawWidth:    width,
				rawCanBreak: canBreak,
				cells:       cs,
			}
			continue
		}
		broken := false
		for _, d := range row.cells {
			width, rowBroken, canBreak := measureDoc(d, modeBreak, 0)
			if rowBroken {
				broken = true
				break
			}
			cs = append(cs, cellInfo{width: width, canBreak: canBreak})
		}
		info[i] = rowInfo{broken: broken, cells: cs}
	}

	// rowFits reports whether row j fits given segment-wide column
	// max-widths. A cell only "doesn't fit" if its width exceeds its
	// budget AND its Doc could break; pure text running past the line
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
		if r.raw != nil {
			return !ri.rawCanBreak || ri.rawWidth <= avail
		}
		cellStartCol := 0
		for c, ci := range ri.cells {
			if c > 0 {
				cellStartCol += colMaxW[c-1] + 1
			}
			if ci.canBreak && ci.width > avail-cellStartCol {
				return false
			}
		}
		// The final condition is the breakRow test from
		// [renderer.renderTableSegment]; [rowFlatWidth] keeps the two
		// agreeing on which rows overflow.
		return infiniteWidth || !r.allowRowBreak || r.mergedFirstCell != nil || len(ri.cells) < 2 || ri.cells[1].canBreak || rowFlatWidth(*ri) <= avail
	}

	var colMaxW []int
	for i, row := range rows {
		if info[i].broken {
			return max(i, 1) // the prefix is always at least 1 row long.
		}
		// Predict new column maxes if this row is admitted, using the
		// same per-cell contributions ([rowColWidths]) that
		// renderTableSegment accumulates, so the cell positions
		// predicted here match what it actually pads to.
		grew := false
		if row.raw == nil {
			for c, w := range rowColWidths(row, info[i]) {
				switch {
				case c >= len(colMaxW):
					grew = true
					colMaxW = append(colMaxW, w)
				case w > colMaxW[c]:
					grew = true
					colMaxW[c] = w
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

// measureDoc measures the width that doc would render to under
// startMode and reports whether the descent encountered an
// unflattenable node or a break-capable one. Returns:
//
//   - width: the cumulative width accumulated along the descent.
//     [docLineBreakSoft] and the per-row separators inside a
//     [docTable] contribute their flat-text width; nested
//     [docGroup]s contribute as if they had picked modeFlat
//     (optimistic, mirroring the renderer's flat-fit test).
//   - broken: true if doc is unflattenable - contains
//     [docLineBreakHard] / [docLineBreakBare] / a table row with a
//     `//` comment somewhere in the descent. The width returned in
//     that case is meaningless.
//   - canBreak: true if doc contains a break-capable node
//     ([docLineBreakSoft], [docSwitchMode], [docTable],
//     [docBodyShape]) reachable outside an [docInfiniteWidth]
//     scope, i.e. the cell could emit a newline at render time
//     even if it doesn't have to.
//
// startMode controls how [docSwitchMode] resolves while descending:
// modeFlat takes the flat branch (used when we are asking "would
// this fit on a single line?"); modeBreak takes the broken branch
// initially, with the flat branch becoming live once a [docGroup]
// has flipped the frame's mode to modeFlat. This mirrors the
// renderer's frame.mode.
//
// budget caps the work: when budget > 0 and width exceeds budget,
// the function returns early with width == [math.MaxInt] as a
// sentinel for "over budget" - any `w <= budget` check at the call
// site will naturally reject it without paying the full descent
// cost. Pass 0 for no budget (full traversal).
func measureDoc(d doc, startMode mode, budget int) (width int, broken, canBreak bool) {
	if d == nil {
		return 0, false, false
	}
	// The frame carries the same [walkState] as the renderer's frames,
	// and the state transitions go through the same walkState methods,
	// so mode, infWidth scoping, and nextGroupNoop arm/consume behave
	// identically in both walkers by construction.
	//
	// infWidth matters here because [docLineBreakSoft],
	// [docSwitchMode], and inner [docTable]s only mark the cell as
	// canBreak if they could break at render time: in an
	// infinite-width scope they cannot (the surrounding [docGroup]'s
	// flat-fit succeeds with the unlimited budget, so it stays flat
	// and [docLineBreakSoft]s emit their flat content).
	// [docFiniteWidth] resets the scope so its inner content's
	// canBreak is reported truthfully.
	//
	// nextGroupNoop matters with startMode == modeBreak (the
	// longestAlignedPrefix use case, where the cell's runtime starting
	// mode is also modeBreak): a docGroup reached through a
	// [docNextGroupNoop] does NOT introduce a flat scope, so an inner
	// [docSwitchMode] resolves to its broken branch, matching what
	// renderInMode will actually emit.
	type frame struct {
		doc doc
		walkState
	}
	var stackBuf [32]frame
	var stack stack[frame] = stackBuf[:1]
	stack[0] = frame{doc: d, walkState: walkState{mode: startMode}}
	for len(stack) > 0 {
		if budget > 0 && width > budget {
			// Over-budget early exit: width is now at least budget+1,
			// which is enough information for the caller's fits
			// check. Saves walking the rest of the doc.
			// Return MaxInt because we don't know the true width.
			return math.MaxInt, false, canBreak
		}
		f := stack.pop()
		if f.doc == nil {
			continue
		}
		switch d := f.doc.(type) {
		case *docStringLit:
			width += d.width
		case *docLineBreakSoft:
			width += d.flatWidth
			if !f.infWidth {
				canBreak = true
			}
		case *docLineBreakHard, *docLineBreakBare:
			return 0, true, true
		case *docCat:
			fLeft, fRight := f, f
			fLeft.doc = d.left
			fRight.doc = d.right
			stack.push(fRight, fLeft)
		case *docNest:
			f.doc = d.child
			stack.push(f)
		case *docGroup:
			// Optimistic flat-fit measurement: when the group would run
			// a fit test at render time, assume it picks modeFlat. A
			// group neutralised by an armed nextGroupNoop leaves the
			// mode unchanged, so a SwitchMode inside resolves to its
			// broken branch, matching the renderer.
			if f.groupRunsFitTest() {
				f.mode = modeFlat
			}
			f.doc = d.child
			stack.push(f)
		case *docNextGroupNoop:
			f.enterNextGroupNoop()
			f.doc = d.child
			stack.push(f)
		case *docInfiniteWidth:
			f.enterInfiniteWidth()
			f.doc = d.child
			stack.push(f)
		case *docFiniteWidth:
			f.enterFiniteWidth()
			f.doc = d.child
			stack.push(f)
		case *docAtIndent:
			// Indentation is irrelevant to width measurement (we
			// accumulate visual width along the descent, and the
			// rebase to indent 0 happens at newlines we don't emit
			// during measurement). Pass through to the child.
			f.doc = d.child
			stack.push(f)
		case *docSwitchMode:
			if !f.infWidth {
				canBreak = true
			}
			f.doc = f.switchModeBranch(d)
			stack.push(f)
		case *docTable:
			if !f.infWidth {
				canBreak = true
			}
			for i, row := range d.rows {
				if row.hasComment || row.docComment != nil {
					return 0, true, true
				}
				if i > 0 && row.sep != nil {
					f.doc = row.sep
					stack.push(f)
				}
				if row.raw == nil {
					for j := len(row.cells) - 1; j >= 0; j-- {
						f.doc = row.cells[j]
						stack.push(f)
						if j > 0 {
							f.doc = spaceLit
							stack.push(f)
						}
					}
				} else {
					f.doc = row.raw
					stack.push(f)
				}
			}
			// Rows and cells start fresh, like the renderer's
			// renderTableInMode, which issues new renderInMode calls
			// that do not carry an armed nextGroupNoop.
			f.nextGroupNoop = false
		case *docBodyShape:
			// measureDoc sees docBodyShape as a break point: its render
			// decision depends on the surrounding mode and column, so
			// the cell can break; recurse into body to capture any inner
			// hard breaks.
			if !f.infWidth {
				canBreak = true
			}
			f.doc = d.body
			stack.push(f)
		}
	}
	return width, false, canBreak
}

// newline writes a newline followed by basePrefix and the configured
// indent string repeated indent times. basePrefix is written verbatim.
func (r *renderer) newline(indent int, basePrefix string) {
	r.buf.WriteByte('\n')
	r.buf.WriteString(basePrefix)
	for range indent {
		r.buf.WriteString(r.indent)
	}
	r.col = utf8.RuneCountInString(basePrefix) + indent*r.indentWidth
	r.lastIndent = indent
}
