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
	// nextGroupNoop is set by [docNextGroupNoop] when it is entered in
	// modeBreak. The next [docGroup] consumes it: rather than running
	// its usual flat-fit retry, the group leaves the frame's mode
	// (modeBreak) untouched, clears this flag, and pushes its child:
	// functionally a no-op. This flag is copied by all other Doc types
	// to their children when rendering (except docTable), so the "next
	// group encountered while descending" semantics work even when the
	// wrapped subtree's root is not literally a group node.
	nextGroupNoop bool
	doc           Doc
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
			switch {
			case f.nextGroupNoop:
				// A [docNextGroupNoop] up the stack set this flag when it
				// was entered in modeBreak. Skip the flat-fit retry and
				// leave mode untouched (it is already modeBreak).
				f.nextGroupNoop = false
			case mode == modeBreak && (f.infWidth || r.width > r.col):
				remaining := r.width - r.col
				// In infWidth mode the fit-test sees an unlimited budget
				// so a docGroup always picks flat unless its child
				// contains a hard line break.
				if f.infWidth {
					remaining = 0
				}
				if w, broken, _ := measureDoc(d.child, modeFlat, remaining); !broken && (f.infWidth || w <= remaining) {
					mode = modeFlat
				}
			}
			f.mode = mode
			f.doc = d.child
			r.renderStack = append(r.renderStack, f)

		case *docNextGroupNoop:
			// Inherit parent's mode unchanged. When the parent went
			// broken, set nextGroupNoop so the first docGroup
			// encountered while descending into child is a noop.
			//
			// When the parent went flat, do not set the flag - even
			// though a docGroup entered in modeFlat does nothing on its
			// own, an intermediate [docFiniteWidth] on the descent path
			// sets the mode to modeBreak before docGroup runs. We want
			// that downstream docGroup to pick modeFlat for content that
			// fits at the current column; so that docGroup must not be a
			// noop.
			if f.mode == modeBreak {
				f.nextGroupNoop = true
			}
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
			r.renderTableInMode(f, d)

		case *docBodyShape:
			r.renderBodyShape(f, d)
		}
	}
}

// renderBodyShape implements the per-list-level layout decision for
// docBodyShape. See [docBodyShape] for the three shapes (flat,
// indented, hug) and example renderings.
//
// Three cases:
//
//   - Entered in modeFlat (enclosing list's group picked flat):
//     render body in modeFlat. The enclosing Cat handles brackets
//     adjacent to body, yielding the flat shape
//     `[body]`. trailingComma is suppressed: the flat shape has no
//     place for it.
//
//   - Entered in modeBreak and body's flat width fits at indent+1's
//     budget: render the indented shape. Emit HardLine at indent+1,
//     body in modeFlat (cells stay flat, joined by inline-space
//     separators), trailingComma (if non-nil), HardLine at the outer
//     indent. The closing bracket emitted by the enclosing Cat then
//     lands at the outer indent.
//
//   - Entered in modeBreak and body's flat width doesn't fit: render
//     the hug shape. body is pushed in modeBreak at the outer indent
//     with no surrounding breaks; cells (wrapped in
//     [docNextGroupNoop] by the caller) propagate the modeBreak into
//     each cell's own docGroup-rendered-as-no-op, so each cell
//     renders its own broken shape and the inter-cell separator (a
//     literal space) keeps the cells horizontally chained.
func (r *renderer) renderBodyShape(f renderFrame, d *docBodyShape) {
	if f.mode == modeFlat {
		// flat shape: body in modeFlat, brackets adjacent (handled
		// by the enclosing Cat).
		f.doc = d.body
		r.renderStack = append(r.renderStack, f)
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
		r.renderStack = append(r.renderStack, f)
		return
	}

	// indented shape. Pushes are LIFO: we want the renderer to
	// pop in order opening HardLine, body, trailingComma (if
	// any), closing HardLine. Push in reverse.
	closing := f
	closing.indent = outerIndent
	closing.doc = HardLine()
	r.renderStack = append(r.renderStack, closing)

	if d.trailingComma != nil {
		comma := f
		comma.indent = newIndent
		comma.mode = modeFlat
		comma.doc = d.trailingComma
		r.renderStack = append(r.renderStack, comma)
	}

	bodyFrame := f
	bodyFrame.indent = newIndent
	bodyFrame.mode = modeFlat
	bodyFrame.doc = d.body
	r.renderStack = append(r.renderStack, bodyFrame)

	opening := f
	opening.indent = newIndent
	opening.doc = HardLine()
	r.renderStack = append(r.renderStack, opening)
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
func (r *renderer) renderTableInMode(f renderFrame, d *docTable) {
	rows := d.rows
	if f.mode == modeFlat {
		for i, row := range rows {
			if i > 0 {
				r.renderInMode(0, f.basePrefix, modeFlat, f.infWidth, row.Sep)
			}
			if row.Raw != nil {
				r.renderInMode(0, f.basePrefix, modeFlat, f.infWidth, row.Raw)
				continue
			}
			for j, cell := range row.Cells {
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
// contains [docLineBreakHard]/[docLineBreakBare]) the slice is
// truncated right before the broken cell, since later cells can't
// contribute to column widths once the row is forced multi-line.
type rowInfo struct {
	broken      bool
	cells       []cellInfo // aligned rows
	rawWidth    int        // raw rows
	rawCanBreak bool       // raw rows
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
		if row.Raw != nil {
			continue
		}
		lastNonNil := -1
		for c, cell := range row.Cells {
			if cell != nil {
				colCount[c]++
				lastNonNil = c
			}
		}
		// A cell only widens its column when the row has content after
		// it. A column's max width exists to align the cells that follow
		// it across rows, so a trailing cell with nothing to its right -
		// e.g. a long comment-less value sitting among rows that do carry
		// a trailing comment - must not push those later columns out. We
		// therefore skip the row's last non-nil cell (and anything past
		// it) when accumulating colMaxW; colCount above still counts it.
		for c, ci := range info[i].cells {
			if c == lastNonNil {
				break
			}
			colMaxW[c] = max(colMaxW[c], ci.width)
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
		// Decide whether to break the row before cell 1. The row breaks
		// when ALL of:
		//
		//   - we don't have infiniteWidth;
		//   - the row didn't already break in measurement (ri.broken);
		//   - AllowRowBreak is set (caller opt-in);
		//   - the row has no MergedFirstCell (those go through the
		//     merged-render path above);
		//   - cell 1 is atomic - no Group/Table inside (keeps `key:
		//     {...}` hugging intact for struct/list values);
		//   - the row's total flat width exceeds avail.
		//
		// longestAlignedPrefix has already segmented around such rows
		// so siblings don't pad to the broken row's wide key.
		//
		// When the row does break, cell 1 lands at r.lastIndent+1 - one
		// level deeper than wherever cell 0's last newline placed us
		// (or one deeper than the row's indent if cell 0 stayed flat).
		breakRow := false
		ri := info[i]
		if !infiniteWidth && !ri.broken && row.AllowRowBreak && row.MergedFirstCell == nil && len(ri.cells) >= 2 && !ri.cells[1].canBreak {
			totalFlat := 0
			for c, ci := range ri.cells {
				if !countsTowardRowWidth(c, ci) {
					continue
				}
				if c > 0 {
					totalFlat++
				}
				totalFlat += ci.width
			}
			breakRow = totalFlat > avail
		}
		// firstCol tracks whether we've already processed (rendered or
		// padded for) a non-skipped column in this row. The leading
		// column doesn't take an inter-cell space; later columns do.  A
		// row whose first few cells are nil AND their columns are empty
		// across the segment (a continuation row holding only a
		// late-column comment, say) hits the `continue` skip and the
		// first non-skipped column is treated as the row's leading
		// column - emitting no leading space.
		firstCol := true
		for c, cell := range row.Cells {
			// Skip columns where no row in the segment contributes
			// content at all - emit nothing, no separator, no pad.
			// colMaxW[c] == 0 is insufficient on its own: a broken cell
			// (value contains a HardLine) also has width 0 in ri.cells
			// because it's truncated; colCount tells us whether any row
			// has a non-nil cell at this column.
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
			} else if !firstCol {
				r.buf.WriteByte(' ')
				r.col++
				// If a previous cell emitted a newline (e.g. cell 0
				// contained a key like `g:\n abcde:` that broke), this
				// cell sits inline after that newline's indented
				// position. Promote cellIndent to r.lastIndent so any
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
			// When this row will break before cell 1, cell 0 must not be
			// padded to colMaxW: the trailing spaces would land just
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
			// it padded so its cells align. We look past empty columns
			// (colCount == 0 - these render nothing) AND past sparse
			// columns to reach a later dense one: a continuation row
			// holding only a late-column comment, for example, has nil
			// leading cells that must still pad to their columns' widths
			// so the comment lands in the same column as the dense
			// comment column in the rows above it.
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
			// the start column), padding is not meaningful so we skip
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
// width used to decide whether a row must break (drop its value onto a
// new line) or be isolated into its own segment. Pure-text trailing
// decoration - attributes and comments, which the table places at
// column >= 2 - runs to end of line and cannot be relocated by breaking
// the value onto its own line: breaking moves it to a deeper indent, so
// a long trailing comment that overflows still overflows afterwards.
// Letting it drive the decision only produces a pointless break, so it
// does not count. Cells 0 (key) and 1 (value), and any break-capable
// cell, always count. Used by both [renderer.renderTableSegment]'s
// breakRow test and [renderer.longestAlignedPrefix]'s rowFits so the
// two agree on which rows overflow.
func countsTowardRowWidth(c int, ci cellInfo) bool {
	return c < 2 || ci.canBreak
}

// longestAlignedPrefix returns the length of the longest prefix of
// rows that can share column alignment without forcing newlines. The
// caller provides info (same length as rows); longestAlignedPrefix
// populates info for every row scanned. Always returns at least 1: a
// "boundary" row forms a one-row segment on its own.
//
// A row is a "boundary" when either:
//
//   - a cell (or a row's Raw Doc) is unflattenable: it contains
//     [docLineBreakHard] / [docLineBreakBare]; or
//   - a cell whose Doc could break (contains a [docLineBreakSoft],
//     [docSwitchMode], [docGroup], or [docTable]) has optimistic flat
//     width (what [measureDoc] returns: the width assuming every
//     inner [docGroup] picks flat) exceeding the budget at its column
//     position: some inner [docGroup] cannot fit flat and
//     Wadler-Lindig will break it, emitting newlines.
//
// Cells whose Doc is pure text (no break-capable primitive) never
// emit newlines even if they extend past the line width - a lone
// trailing // comment is the canonical example - so they do not
// trigger a boundary on their own.
func (r *renderer) longestAlignedPrefix(rows []Row, avail int, infiniteWidth bool, info []rowInfo) int {
	if len(rows) == 0 {
		return 0
	}

	// Precompute per-cell width and break-ability once.
	for i, row := range rows {
		// cells may already be non-nil so truncate to permit memory
		// reuse.
		cs := info[i].cells[:0]
		if row.Raw != nil {
			width, broken, canBreak := measureDoc(row.Raw, modeBreak, 0)
			info[i] = rowInfo{
				broken:      broken,
				rawWidth:    width,
				rawCanBreak: canBreak,
				cells:       cs,
			}
			continue
		}
		broken := false
		for _, d := range row.Cells {
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
		if r.Raw != nil {
			return !ri.rawCanBreak || ri.rawWidth <= avail
		}
		cellStartCol := 0
		totalFlatWidth := 0
		for c, ci := range ri.cells {
			if c > 0 {
				cellStartCol += colMaxW[c-1] + 1
			}
			if ci.canBreak && ci.width > avail-cellStartCol {
				return false
			}
			// Pure-text trailing decoration doesn't count toward whether
			// the row overflows - mirrors the totalFlat exclusion in
			// renderTableSegment's breakRow so the two agree.
			if !countsTowardRowWidth(c, ci) {
				continue
			}
			if c > 0 {
				totalFlatWidth++
			}
			totalFlatWidth += ci.width
		}
		return infiniteWidth || !r.AllowRowBreak || r.MergedFirstCell != nil || len(ri.cells) < 2 || ri.cells[1].canBreak || totalFlatWidth <= avail
	}

	var colMaxW []int
	for i, row := range rows {
		if info[i].broken {
			return max(i, 1) // the prefix is always at least 1 row long.
		}
		// Predict new column maxes if this row is admitted. This
		// mirrors the colMaxW rule in [renderer.renderTableSegment]: a
		// cell only widens its column when the row has content after
		// it, so the row's last non-nil cell (and anything past it)
		// contributes 0.  Keeping the two computations identical means
		// the cell positions predicted here match what
		// renderTableSegment actually pads to.
		grew := false
		if row.Raw == nil {
			lastNonNil := -1
			for c, cell := range row.Cells {
				if cell != nil {
					lastNonNil = c
				}
			}
			for c, ci := range info[i].cells {
				w := ci.width
				if c >= lastNonNil {
					w = 0
				}
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
func measureDoc(doc Doc, startMode mode, budget int) (width int, broken, canBreak bool) {
	if doc == nil {
		return 0, false, false
	}
	// inInfiniteWidth tracks whether we are inside a
	// [docInfiniteWidth] scope. [docLineBreakSoft], [docSwitchMode],
	// and inner [docTable]s only mark the cell as canBreak if they
	// could break at render time: in an infinite-width scope they
	// cannot (the surrounding [docGroup]'s flat-fit succeeds with the
	// unlimited budget, so it stays flat and [docLineBreakSoft]s emit
	// their flat content). [docFiniteWidth] resets the scope so its
	// inner content's canBreak is reported truthfully.
	//
	// nextGroupNoop mirrors the renderer's frame flag of the same
	// name. With startMode == modeBreak (the longestAlignedPrefix
	// use case, where the cell's runtime starting mode is also
	// modeBreak), any [docNextGroupNoop] on the descent will have
	// its flag armed and consumed by the next [docGroup],
	// neutralising that group. The flag lets us model that
	// accurately: a docGroup reached through a docNextGroupNoop
	// does NOT introduce a flat scope, so an inner [docSwitchMode]
	// resolves to its broken branch (matching what renderInMode
	// will actually emit). With startMode == modeFlat (the flat-
	// fit retry use case), mode is already modeFlat throughout and
	// the nextGroupNoop arm/consume sequence is a no-op for
	// SwitchMode resolution anyway.
	type frame struct {
		doc             Doc
		mode            mode
		inInfiniteWidth bool
		nextGroupNoop   bool
	}
	var stackBuf [32]frame
	stack := stackBuf[:1]
	stack[0] = frame{doc: doc, mode: startMode}
	for len(stack) > 0 {
		if budget > 0 && width > budget {
			// Over-budget early exit: width is now at least budget+1,
			// which is enough information for the caller's fits
			// check. Saves walking the rest of the doc.
			// Return MaxInt because we don't know the true width.
			return math.MaxInt, false, canBreak
		}
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
				canBreak = true
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
			if f.nextGroupNoop {
				// Consumed by a docNextGroupNoop up the stack: this
				// group is a no-op at render time, so leave mode
				// unchanged. SwitchMode inside will resolve to its
				// broken branch, matching the renderer.
				f.nextGroupNoop = false
			} else {
				// Optimistic flat-fit measurement: assume the group
				// would pick modeFlat at render time.
				f.mode = modeFlat
			}
			stack = append(stack, f)
		case *docNextGroupNoop:
			// Arm the flag for the next docGroup, mirroring the
			// renderer. The docGroup it leads to will be a no-op, so
			// we deliberately do not flip mode here either.
			f.doc = d.child
			f.nextGroupNoop = true
			stack = append(stack, f)
		case *docInfiniteWidth:
			f.doc = d.child
			f.inInfiniteWidth = true
			stack = append(stack, f)
		case *docFiniteWidth:
			f.doc = d.child
			f.inInfiniteWidth = false
			stack = append(stack, f)
		case *docAtIndent:
			// Indentation is irrelevant to width measurement (we
			// accumulate visual width along the descent, and the
			// rebase to indent 0 happens at newlines we don't emit
			// during measurement). Pass through to the child.
			f.doc = d.child
			stack = append(stack, f)
		case *docSwitchMode:
			if !f.inInfiniteWidth {
				canBreak = true
			}
			if f.mode == modeFlat {
				f.doc = d.flat
			} else {
				f.doc = d.broken
			}
			stack = append(stack, f)
		case *docTable:
			if !f.inInfiniteWidth {
				canBreak = true
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
		case *docBodyShape:
			// measureDoc sees docBodyShape as a break point: its render
			// decision depends on the surrounding mode and column, so
			// the cell can break; recurse into body to capture any inner
			// hard breaks.
			if !f.inInfiniteWidth {
				canBreak = true
			}
			f.doc = d.body
			stack = append(stack, f)
		}
	}
	return width, false, canBreak
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
