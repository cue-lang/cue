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
	r := &renderer{
		width:       cfg.width(),
		indent:      cfg.Indent,
		indentWidth: cfg.indentWidth(),
	}
	r.renderInMode(0, "", modeBreak, false, doc)
	return r.buf.Bytes()
}

type renderer struct {
	width       int
	indent      string
	indentWidth int
	buf         bytes.Buffer
	col         int
	// lastIndent is the indent level (in Nest units) of the most
	// recent newline emitted via r.newline. The table layer reads it
	// after rendering a cell to decide where the row's break-point
	// should land - specifically, when a chain Group inside cell 0
	// partially breaks, the value goes one level deeper than wherever
	// the chain actually ended.
	lastIndent int

	// Scratch buffers reused across calls to avoid per-call heap
	// allocation. renderStack and colScratch use stack semantics
	// (save the length on entry, restore on exit) so nested
	// rendering doesn't clobber outer frames.
	renderStack []renderEntry
	infoScratch []rowInfo
	colScratch  []int
}

// renderEntry is one frame on renderInMode's explicit evaluation
// stack.
type renderEntry struct {
	indent        int
	basePrefix    string // verbatim prefix prepended before nest-driven indent on newlines (set by docAtIndent)
	mode          mode
	infiniteWidth bool // child of a docInfiniteWidth, infinite-width budget
	doc           Doc
}

// renderInMode renders a doc using the given indent, mode, and
// infiniteWidth flag. Under infiniteWidth, width-based break decisions see an
// unlimited budget - Groups pick flat unless a HardLine forces them
// open. basePrefix is a verbatim string prepended before the nest-
// driven indent on each newline; only [docAtIndent] overrides it.
func (r *renderer) renderInMode(indent int, basePrefix string, m mode, infiniteWidth bool, doc Doc) {
	if doc == nil {
		return
	}
	base := len(r.renderStack)
	r.renderStack = append(r.renderStack, renderEntry{indent: indent, basePrefix: basePrefix, mode: m, infiniteWidth: infiniteWidth, doc: doc})
	for len(r.renderStack) > base {
		idx := len(r.renderStack) - 1
		e := r.renderStack[idx]
		r.renderStack = r.renderStack[:idx]

		if e.doc == nil {
			continue
		}

		switch d := e.doc.(type) {
		case *docStringLit:
			r.buf.WriteString(d.str)
			r.col += d.width

		case *docLineBreakSoft:
			if e.mode == modeFlat {
				r.buf.WriteString(d.flat)
				r.col += d.flatWidth
			} else {
				r.newline(e.indent, e.basePrefix)
			}

		case *docLineBreakHard:
			r.newline(e.indent, e.basePrefix)

		case *docLineBreakBare:
			r.buf.WriteByte('\n')
			r.col = 0

		case *docCat:
			r.renderStack = append(r.renderStack,
				renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: e.mode, infiniteWidth: e.infiniteWidth, doc: d.right},
				renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: e.mode, infiniteWidth: e.infiniteWidth, doc: d.left})

		case *docNest:
			r.renderStack = append(r.renderStack, renderEntry{indent: e.indent + 1, basePrefix: e.basePrefix, mode: e.mode, infiniteWidth: e.infiniteWidth, doc: d.child})

		case *docGroup:
			mode := e.mode
			if mode == modeBreak && (e.infiniteWidth || r.width > r.col) {
				remaining := r.width - r.col
				// In infiniteWidth mode the fit-test sees an unlimited budget
				// so the Group always picks flat unless its child
				// contains a HardLine. The rule: any subtree under an AST
				// node whose descendants carry a Newline/NewSection
				// RelPos is not subject to the line-width limit.
				if e.infiniteWidth {
					remaining = 0
				}
				if _, ok := measureFlat(remaining, d.child); ok {
					mode = modeFlat
				}
			}
			r.renderStack = append(r.renderStack, renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: mode, infiniteWidth: e.infiniteWidth, doc: d.child})

		case *docInfiniteWidth:
			r.renderStack = append(r.renderStack, renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: e.mode, infiniteWidth: true, doc: d.child})

		case *docFiniteWidth:
			// Reset both infiniteWidth and mode for Child so its inner
			// Groups do their own flat-fit against the real width
			// budget, even when enclosed by a [docInfiniteWidth]
			// scope (which would otherwise force modeFlat throughout
			// via the unlimited budget). asInfiniteWidth doesn't enter
			// docFiniteWidth, so soft Lines and IfBreak alternates
			// inside Child remain available for those decisions.
			r.renderStack = append(r.renderStack, renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: modeBreak, infiniteWidth: false, doc: d.child})

		case *docAtIndent:
			r.renderStack = append(r.renderStack, renderEntry{indent: 0, basePrefix: d.prefix, mode: e.mode, infiniteWidth: e.infiniteWidth, doc: d.child})

		case *docSwitchMode:
			if e.mode == modeBreak {
				r.renderStack = append(r.renderStack, renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: e.mode, infiniteWidth: e.infiniteWidth, doc: d.broken})
			} else {
				r.renderStack = append(r.renderStack, renderEntry{indent: e.indent, basePrefix: e.basePrefix, mode: e.mode, infiniteWidth: e.infiniteWidth, doc: d.flat})
			}

		case *docTable:
			r.renderTableInMode(e.indent, e.basePrefix, e.mode, e.infiniteWidth, d.rows)
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
func (r *renderer) renderTableInMode(indent int, basePrefix string, m mode, infiniteWidth bool, rows []Row) {
	if m == modeFlat {
		for i, row := range rows {
			if i > 0 {
				r.renderInMode(0, basePrefix, modeFlat, infiniteWidth, row.Sep)
			}
			if row.Raw != nil {
				r.renderInMode(0, basePrefix, modeFlat, infiniteWidth, row.Raw)
				continue
			}
			for j, cell := range row.Cells {
				if j > 0 {
					r.buf.WriteByte(' ')
					r.col++
				}
				r.renderInMode(0, basePrefix, modeFlat, infiniteWidth, cell)
			}
		}
		return
	}

	avail := r.width - indent*r.indentWidth
	if avail < 1 {
		avail = 1
	}
	// avail stays at the real width budget. The decision of
	// whether a row's overflow forces segmentation is made per-
	// cell via measureCell's canWrap, which is only true when the
	// cell's wrap-capable content (soft Lines, IfBreaks, inner
	// Tables) effectively can break at render time. Cells under
	// an enclosing [docInfiniteWidth] scope without a
	// [docFiniteWidth] boundary to escape it report canWrap=false -
	// their inner Groups go flat regardless of width, so segmenting
	// around them would just visibly break the user's column layout
	// for no gain. Cells that escape the [docInfiniteWidth] scope
	// via a [docFiniteWidth] (option B: finite-width pockets nested
	// in an infinite-width scope) report canWrap=true and properly
	// segment around overflow.

	// Render one segment at a time: find the longest prefix of rows
	// that can share column alignment, render it, and continue with
	// the tail. longestAlignedPrefix carves its info slice off the
	// shared r.infoScratch using stack semantics; we save and restore
	// the length around each call so a nested table rendering in a
	// cell can use the rest of the scratch without clobbering ours.
	first := true
	for len(rows) > 0 {
		base := len(r.infoScratch)
		n, info := r.longestAlignedPrefix(rows, avail, infiniteWidth)
		r.renderTableSegment(indent, basePrefix, infiniteWidth, rows[:n], info, !first)
		r.infoScratch = r.infoScratch[:base]
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
// each cell's width/canWrap in order - on a broken row (a cell
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
func (r *renderer) renderTableSegment(indentWidth int, basePrefix string, infiniteWidth bool, rows []Row, info []rowInfo, emitFirstSep bool) {
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
	// its predecessor. On a broken row, info[i].cells is truncated at
	// the first unflattenable cell, so later cells don't contribute
	// to column widths (but still count towards colCount). Nil cells
	// are "reserved" column slots used to keep later columns aligned
	// across rows.
	//
	// colCount and colMaxW are stack-allocated on r.colScratch so a
	// nested renderTableSegment (via cell content) gets its own slot.
	base := len(r.colScratch)
	need := 2 * numCols
	if cap(r.colScratch) < base+need {
		r.colScratch = append(r.colScratch, make([]int, need)...)
	} else {
		r.colScratch = r.colScratch[:base+need]
		clear(r.colScratch[base : base+need])
	}
	colCount := r.colScratch[base : base+numCols]
	colMaxW := r.colScratch[base+numCols : base+need]

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

	// In infiniteWidth mode, the breakRow check below must not fire:
	// the rows preserve the doc author's layout regardless of
	// overflow. Cells that escape the infinite-width scope via a
	// [docFiniteWidth] carry canWrap=true and the breakRow guard
	// `!cells[1].canWrap` already excludes them. So skipping the
	// budget below only suppresses the row-break for genuine
	// infinite-width rows, not for finite-width pockets nested
	// inside.
	avail := 0
	useMerged := false
	if !infiniteWidth {
		// Compute the segment's aligned width - the column-padded width
		// every row would render to if all cells stay flat at this
		// segment's column maxes. If that fits in the available budget
		// the segment renders aligned (the existing per-cell padding
		// keeps values aligned across rows). If not we want each chain
		// row to fall back to its MergedFirstCell so left-to-right
		// partial chain breaks include val in their flat-fit decisions.
		avail = max(r.width-indentWidth*r.indentWidth, 1)

		alignedWidth := 0
		for c := range numCols {
			if c > 0 && (colMaxW[c] != 0 || colCount[c] != 0) {
				alignedWidth++
			}
			alignedWidth += colMaxW[c]
		}
		useMerged = alignedWidth > avail
	}

	// Decide whether to break the row before its second cell. Fires
	// for rows that opt in via AllowRowBreak, have no merged
	// alternative (chain rows with atomic val go through the merged-
	// render path instead), and have cell 1 atomic (no Group/Table
	// inside - keeps `key: {…}` hugging intact for struct/list
	// values), and whose total flat width would exceed the budget.
	// longestAlignedPrefix segments around such rows so sibling rows
	// don't pad to the broken row's wide key.
	//
	// The break indent comes from r.lastIndent read after cell 0
	// renders, so a partial chain break in cell 0 places val one
	// level deeper than the chain's actual deepest line.

	for i, row := range rows {
		if i > 0 || emitFirstSep {
			r.renderInMode(indentWidth, basePrefix, modeBreak, infiniteWidth, row.Sep)
		}
		if row.Raw != nil {
			r.renderInMode(indentWidth, basePrefix, modeBreak, infiniteWidth, row.Raw)
			continue
		}
		if row.DocComment != nil {
			// DocComment carries its own trailing separator (HardLine
			// or BlankLine) so the blank-line-after-doc-comment case
			// renders correctly when the node itself has NewSection.
			r.renderInMode(indentWidth, basePrefix, modeBreak, infiniteWidth, row.DocComment)
		}
		// Identify the last non-nil cell so trailing reserved slots
		// don't emit spurious trailing whitespace.
		lastNonNilIdx := -1
		for c, cell := range row.Cells {
			if cell != nil {
				lastNonNilIdx = c
			}
		}
		// Reset lastIndent to the row's own indent so any newline
		// emitted while rendering cell 0 (e.g. a chain Group that
		// partially breaks) is observable as the chain's deepest
		// break level when we place the value cell.
		r.lastIndent = indentWidth
		// Merged-render path: when the segment doesn't fit at its
		// aligned width and this row has a MergedFirstCell, render
		// it in place of cells[0] + cells[1]. The merged cell has
		// val folded into the chain Group's deepest Nest so each
		// split point's flat-fit check includes val and the chain
		// breaks left-to-right just enough to land val on the
		// chain's deepest line. Subsequent cells (attrs, comment)
		// continue to render with literal inter-cell spaces.
		if useMerged && row.MergedFirstCell != nil {
			r.renderInMode(indentWidth, basePrefix, modeBreak, infiniteWidth, row.MergedFirstCell)
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
				r.renderInMode(indentWidth, basePrefix, modeBreak, infiniteWidth, cell)
			}
			continue
		}
		breakRow := false
		if !info[i].broken && row.AllowRowBreak && row.MergedFirstCell == nil &&
			len(info[i].cells) >= 2 && !info[i].cells[1].canWrap {
			totalFlat := 0
			for c, ci := range info[i].cells {
				if c > 0 {
					totalFlat++
				}
				totalFlat += ci.w
			}
			breakRow = !infiniteWidth && totalFlat > avail
		}
		for c, cell := range row.Cells {
			// Skip columns where no row in the segment contributes
			// content at all - emit nothing, no separator, no pad.
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
			cellIndent := indentWidth
			if c > 0 {
				if breakRow && c == 1 {
					// Place the value one level deeper than the
					// chain's last broken level (or one deeper than
					// the row's own indent if the chain stayed flat).
					breakIndent := r.lastIndent + 1
					r.newline(breakIndent, basePrefix)
					cellIndent = breakIndent
				} else {
					r.buf.WriteByte(' ')
					r.col++
					// When a previous cell emitted a HardLine (e.g. a
					// braceless chain key like `g:\n abcde:` broke), this
					// cell sits inline after the chain's deepest line.
					// Its caller-indent must match that line's level so
					// any HardLine emitted inside this cell (a struct
					// body break, a list/call explode, etc.) lands at
					// the right column rather than at the row's own
					// indent. r.lastIndent tracks the most recent
					// newline's level - promote cellIndent to it when
					// it's deeper than the row's own indent.
					if r.lastIndent > cellIndent {
						cellIndent = r.lastIndent
					}
				}
			}
			colStart := r.col
			r.renderInMode(cellIndent, basePrefix, modeBreak, infiniteWidth, cell)
			if c == lastNonNilIdx {
				continue
			}
			// When this row will break before cell 1, cell 0 must not
			// be padded to colMaxW: the trailing spaces would land
			// just before the break newline, leaving stray whitespace
			// at end of line.
			if c == 0 && breakRow {
				continue
			}
			// Skip padding if no later column in this segment needs
			// alignment - a later dense column (colCount > 1) needs
			// this column padded so its content lines up; a column
			// that's sparse or entirely empty does not. We look past
			// empty columns (colMaxW == 0 - these render nothing) so
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
	r.colScratch = r.colScratch[:base]
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
//     budget at its column position - Wadler-Lindig will break the
//     inner Group, emitting newlines.
//
// Cells whose Doc is pure text (no wrapping element) never emit
// newlines even if they extend past the line width - a lone
// trailing // comment is the canonical example - so they do not
// trigger a boundary on their own.
func (r *renderer) longestAlignedPrefix(rows []Row, avail int, infiniteWidth bool) (int, []rowInfo) {
	if len(rows) == 0 {
		return 0, nil
	}

	// Precompute per-cell width and wrap-ability once; the inner
	// re-check loop below can run up to O(n²) times when admitting
	// each row widens a column, so measuring and walking each Doc on
	// every iteration would be O(n² · treeSize). We use
	// [measureBrokenWidth] here - cells are rendered in broken-mode,
	// so a TrailingComma (IfBreak(",", nil)) contributes its one
	// character, matching what the renderer will actually emit.
	//
	// Grow r.infoScratch to cover len(rows); reuse existing capacity
	// when possible. We also reuse each entry's cells slice so we
	// don't allocate a fresh one per row on every call.
	// Carve a slice off r.infoScratch using stack semantics - a cell
	// rendered below us may recurse into another table and call
	// longestAlignedPrefix again, which would otherwise overwrite the
	// outer caller's rowInfo entries (cells inside rowInfo are
	// :0-reset and re-grown, so the aliasing isn't just stale data
	// but actively rewritten). Save the base length and let the outer
	// frame restore it once it's done with info.
	base := len(r.infoScratch)
	if cap(r.infoScratch) < base+len(rows) {
		r.infoScratch = append(r.infoScratch, make([]rowInfo, len(rows))...)
	} else {
		r.infoScratch = r.infoScratch[:base+len(rows)]
	}
	info := r.infoScratch[base : base+len(rows)]
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
	//
	// An AllowRowBreak row whose own flat width exceeds avail is also
	// reported as not fitting, even when its cells are pure text.
	// Such rows render via renderTableSegment's breakRow path and
	// must form their own segment so siblings don't pad to the
	// broken row's (wide) cell-0 width - which would also break
	// idempotency, since on reparse the broken-out row carries a
	// Newline RelPos and segments naturally.
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
		if !infiniteWidth && rows[j].AllowRowBreak && rows[j].MergedFirstCell == nil &&
			len(ri.cells) >= 2 && !ri.cells[1].canWrap {
			totalFlat := 0
			for c, ci := range ri.cells {
				if c > 0 {
					totalFlat++
				}
				totalFlat += ci.w
			}
			if totalFlat > avail {
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
		grow := false
		if row.Raw == nil {
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
		// Row i must fit under pred. If pred grew, re-check every
		// previously-admitted row too: a widened column shifts later
		// cells rightward and may newly push an earlier row past avail.
		// If pred is unchanged, the earlier rows still fit by induction.
		if !rowFits(i, pred) {
			if i == 0 {
				return 1, info
			}
			return i, info
		}
		if grow {
			for j := 0; j < i; j++ {
				if !rowFits(j, pred) {
					return i, info
				}
			}
		}
		colMaxW = pred
	}
	return len(rows), info
}

// measureCell measures a cell's optimistic-render width and whether
// its Doc could emit a newline. Combines what would otherwise be two
// passes (width + docCanWrap): both walk the same tree and are always
// called together from [longestAlignedPrefix]. Returns
// (width, broken, canWrap) where broken means the Doc is
// unflattenable (HardLine/LitLine somewhere - it must render on
// multiple lines), and canWrap means the Doc contains a wrapping-
// capable node (DocLine / DocIfBreak / DocTable) so it could emit a
// newline under some mode even if it isn't strictly broken.
//
// IfBreak resolves the same way the renderer would: when the IfBreak
// sits inside a Group, that Group can choose flat-mode and the Flat
// branch is taken; outside any Group, the cell is rendered in modeBreak
// and the Broken branch is taken. Concretely:
//
//   - A list's `[...]` Group wraps a TrailingComma so that comma
//     contributes 0 to the list's width - the Group can render flat.
//   - A list-element cell `Cat(value, TrailingComma())` is not itself
//     wrapped in a Group; the trailing comma contributes 1 because
//     the renderer will pick its broken branch.
func measureCell(doc Doc) (width int, broken, canWrap bool) {
	if doc == nil {
		return 0, false, false
	}
	// inInfiniteWidth tracks whether we are inside a
	// [docInfiniteWidth] scope that has not been escaped by a
	// [docFiniteWidth] boundary. Soft Lines, IfBreaks, and inner
	// Tables only mark the cell as canWrap if they could ACTUALLY
	// break at render time - under an effective infinite-width scope
	// they cannot (the surrounding Group's flat-fit succeeds with
	// the unlimited budget, so it stays flat and soft Lines emit
	// their alts). [docFiniteWidth] resets the scope so its inner
	// content's canWrap is reported truthfully.
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
		e := stack[top]
		stack = stack[:top]
		if e.doc == nil {
			continue
		}
		switch d := e.doc.(type) {
		case *docStringLit:
			width += d.width
		case *docLineBreakSoft:
			width += d.flatWidth
			if !e.inInfiniteWidth {
				canWrap = true
			}
		case *docLineBreakHard, *docLineBreakBare:
			return 0, true, true
		case *docCat:
			stack = append(stack, frame{doc: d.right, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
			stack = append(stack, frame{doc: d.left, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
		case *docNest:
			stack = append(stack, frame{doc: d.child, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
		case *docGroup:
			stack = append(stack, frame{doc: d.child, inGroup: true, inInfiniteWidth: e.inInfiniteWidth})
		case *docInfiniteWidth:
			stack = append(stack, frame{doc: d.child, inGroup: e.inGroup, inInfiniteWidth: true})
		case *docFiniteWidth:
			stack = append(stack, frame{doc: d.child, inGroup: e.inGroup, inInfiniteWidth: false})
		case *docSwitchMode:
			if !e.inInfiniteWidth {
				canWrap = true
			}
			if e.inGroup {
				stack = append(stack, frame{doc: d.flat, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
			} else {
				stack = append(stack, frame{doc: d.broken, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
			}
		case *docTable:
			if !e.inInfiniteWidth {
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
					stack = append(stack, frame{doc: row.Raw, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
				} else {
					for j := len(row.Cells) - 1; j >= 0; j-- {
						stack = append(stack, frame{doc: row.Cells[j], inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
						if j > 0 {
							stack = append(stack, frame{doc: spaceLit, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
						}
					}
				}
				if i > 0 && row.Sep != nil {
					stack = append(stack, frame{doc: row.Sep, inGroup: e.inGroup, inInfiniteWidth: e.inInfiniteWidth})
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
			// In flat-mode, Line emits its alt text.
			width += d.flatWidth

		case *docLineBreakHard:
			// A HardLine means this group cannot be flattened.
			return 0, false

		case *docLineBreakBare:
			// A literal newline (multi-line string) also prevents
			// flattening.
			return 0, false

		case *docCat:
			// Left above right in the stack (i.e. processed first).
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
			stack = append(stack, d.child)

		case *docSwitchMode:
			// In flat-mode, use the flat variant.
			stack = append(stack, d.flat)

		case *docTable:
			// A // comment in any row runs to end of line and would
			// swallow subsequent tokens in flat-mode. Force break.
			for _, row := range d.rows {
				if row.HasComment || row.DocComment != nil {
					return 0, false
				}
			}
			// Measure table in flat-mode. Because this is a stack, we
			// need to work backwards so that we end up with the first
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
// indent string repeated indent times. basePrefix is verbatim - used
// for multi-line string body indentation, where the strip prefix is
// whatever whitespace mixture the user wrote and must round-trip
// byte-for-byte.
func (r *renderer) newline(indent int, basePrefix string) {
	r.buf.WriteByte('\n')
	r.buf.WriteString(basePrefix)
	for range indent {
		r.buf.WriteString(r.indent)
	}
	r.col = utf8.RuneCountInString(basePrefix) + indent*r.indentWidth
	r.lastIndent = indent
}
