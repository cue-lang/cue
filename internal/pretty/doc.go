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

// Package pretty implements a Wadler-Lindig pretty printer for CUE
// AST nodes.
//
// # The Doc algebra
//
// A [doc] is an immutable description of how to emit text. Render (in
// render.go) walks a Doc and produces bytes. The node types fall into
// a few groups:
//
//   - Atoms: [docStringLit] emits a literal string.
//   - Line breaks: [docLineBreakSoft] emits a configurable flat
//     alternative in flat-mode and a newline + indent in
//     broken-mode. It does not itself force a break - it follows the
//     mode the enclosing [docGroup] already picked.
//     [docLineBreakHard] always emits a newline + indent; it is
//     unflattenable, so any enclosing [docGroup]'s flat-fit test
//     fails and the docGroup must render broken. [docLineBreakBare]
//     is like [docLineBreakHard] but emits a newline without
//     indentation. Used inside multi-line string bodies where content
//     must be preserved verbatim.
//   - Composition: [docCat] concatenates two Docs.
//   - Indentation: [docNest] increases the indent applied on
//     subsequent newlines. [docAtIndent] anchors its child at a fixed
//     visual indent independent of the surrounding nest level (used
//     for multi-line string bodies whose closing-quote indent is
//     verbatim).
//   - Mode control: [docGroup] picks flat-mode if its child fits the
//     line-width budget, otherwise broken-mode. [docSwitchMode] emits
//     different content per mode.
//   - Width control: [docInfiniteWidth] enters an unlimited-budget
//     scope; [docFiniteWidth] cancels it for its child. See "finite
//     vs infinite width" below.
//   - Tables: [docTable] holds [row]s. See "Tables" below.
//
// # flat-mode vs broken-mode
//
// Each [docGroup] renders in one of two modes:
//
//   - flat-mode: [docLineBreakSoft] emits its flat alternative
//     and [docSwitchMode] picks its flat branch.
//   - broken-mode: [docLineBreakSoft] emits a newline + indent and
//     [docSwitchMode] picks its broken branch.
//
// The mode is decided per-[docGroup] at render time by a flat-fit
// test: the renderer measures what the child's flat shape would
// look like (resolving [docLineBreakSoft] and [docSwitchMode] the
// way flat-mode would) and picks flat-mode iff the result fits the
// line budget and contains no hard breaks. So a flat shape that
// contains [docLineBreakHard] / [docLineBreakBare] anywhere forces
// broken-mode.
//
// "Single-line output" is therefore an emergent property of flat-
// mode renderings, not part of the definition: a [docGroup] that
// picks flat-mode won't emit hard breaks because the flat-fit test
// would have rejected it otherwise. This leaves room for a "force
// broken" idiom - a [docSwitchMode] whose flat branch contains a
// hard break poisons the flat-fit test so the enclosing [docGroup]
// must pick broken; once broken, the [docSwitchMode] emits its
// broken branch (often nil), contributing nothing to the output.
//
// # finite vs infinite width
//
// Width-driven Wadler-Lindig is the default. The package also
// supports an "infinite-width" rendering scope in which the
// line-width budget is unlimited - [docGroup]s always pick flat
// unless their subtree contains a hard break. [docInfiniteWidth]
// enters the scope; [docFiniteWidth] cancels it for its child,
// restoring the configured budget. The two compose: a
// [docFiniteWidth] pocket nested inside a [docInfiniteWidth] scope
// renders under the real budget, leaving its surrounding scope
// unaffected.
//
// # Tables
//
// pretty extends Wadler-Lindig with [docTable] for column-aligned
// layouts. A [docTable] holds [row]s; each row is either a Raw Doc
// (rendered as-is) or a list of cells. In flat-mode rows are joined
// by their separator and cells by spaces. In broken-mode the renderer
// partitions rows into maximal segments that share column alignment
// without forced newlines, it computes per-segment column widths, and
// pads cells to align across rows within each segment. A multi-line
// or overflowing row "flushes" the alignment, forming its own
// segment, so it does not stretch the columns of simpler rows around
// it. See render.go for the segmentation algorithm.
//
// # AST translation
//
// The translation from AST to [doc] lives in ast.go; see that
// file for AST-layer terminology (RelPos, authored vs programmatic
// mode, wrap sites, simple vs complex fields, chains).
package pretty

import (
	"slices"
	"unicode/utf8"
)

// doc represents a node in the Wadler-Lindig document algebra. A nil
// doc is the empty document (produces no output).
type doc interface {
	// asInfiniteWidthConverts reports whether the subtree rooted at
	// this Doc would be modified when passed to [asInfiniteWidth].
	// Computed at construction so asInfiniteWidth can short-circuit
	// whole subtrees without traversing them.
	asInfiniteWidthConverts() bool

	// canBreak reports whether the subtree rooted at this Doc could
	// emit a line break when rendered in modeBreak. True for the
	// break primitives themselves ([docLineBreakSoft],
	// [docLineBreakHard], [docLineBreakBare]) and the break-capable
	// composites ([docSwitchMode]'s broken branch, [docTable],
	// [docBodyShape]), propagated up through container constructors.
	// Used by callers that need a static structural answer to "is
	// this Doc pure text?" without walking the subtree at use time.
	canBreak() bool
}

func docAsInfiniteWidthConverts(d doc) bool {
	return d != nil && d.asInfiniteWidthConverts()
}

func docCanBreak(d doc) bool {
	return d != nil && d.canBreak()
}

type docBase struct {
	converts bool
	breaks   bool
}

func (d docBase) asInfiniteWidthConverts() bool { return d.converts }
func (d docBase) canBreak() bool                { return d.breaks }

// docStringLit emits a literal string. str must not contain newlines.
type docStringLit struct {
	docBase
	str   string
	width int // rune count
}

// stringLit returns a Doc that emits the literal string s. s must not
// contain newlines.
func stringLit(s string) doc {
	if s == "" {
		return nil
	}
	return &docStringLit{str: s, width: utf8.RuneCountInString(s)}
}

// docLineBreakSoft is a soft line break. In flat-mode it emits flat;
// in broken-mode it emits a newline followed by the current
// indentation.
type docLineBreakSoft struct {
	docBase
	flat      string
	flatWidth int // rune count of flat
}

// lineBreakSoft returns a soft line break. In flat-mode it emits
// flat; in broken-mode it emits a newline followed by the current
// indentation.
func lineBreakSoft(flat string) doc {
	return &docLineBreakSoft{
		docBase:   docBase{converts: true, breaks: true},
		flat:      flat,
		flatWidth: utf8.RuneCountInString(flat),
	}
}

// docLineBreakHard is a hard line break that always emits a newline
// followed by the current indentation.
type docLineBreakHard struct {
	docBase
}

// docLineBreakBare is a bare newline without indentation. Used for
// newlines inside multi-line string literals where content must be
// preserved verbatim.
type docLineBreakBare struct {
	docBase
}

// docCat is the concatenation of left followed by right.
type docCat struct {
	docBase
	left  doc
	right doc
}

// cat returns the concatenation of a and b.
func cat(a, b doc) doc {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &docCat{
		docBase: docBase{
			converts: a.asInfiniteWidthConverts() || b.asInfiniteWidthConverts(),
			breaks:   a.canBreak() || b.canBreak(),
		},
		left:  a,
		right: b,
	}
}

// cats concatenates all non-nil docs left to right.
func cats(docs ...doc) doc {
	var result doc
	for _, d := range docs {
		result = cat(result, d)
	}
	return result
}

// sep intersperses sep between non-nil docs.
func sep(sep doc, docs ...doc) doc {
	var result doc
	for _, d := range docs {
		if d == nil {
			continue
		}
		if result == nil {
			result = d
		} else {
			result = cat(cat(result, sep), d)
		}
	}
	return result
}

// docNest increases the indent level by one for child.
type docNest struct {
	docBase
	child doc
}

// nest returns a Doc that increases the indent level by one for d.
func nest(d doc) doc {
	if d == nil {
		return nil
	}
	return &docNest{
		docBase: docBase{
			converts: d.asInfiniteWidthConverts(),
			breaks:   d.canBreak(),
		},
		child: d,
	}
}

// docAtIndent anchors child at a fixed visual indent independent of
// the surrounding nest level.
//
// Used for multi-line string bodies: the whitespace before the
// closing quote is preserved verbatim in prefix.
type docAtIndent struct {
	docBase
	prefix string
	child  doc
}

// atIndent returns a Doc that renders d with newlines emitting prefix
// verbatim before the nest-driven indentation. Inside d the nest
// level resets to 0, so an internal nest, one level deep, emits
// prefix + indent on its newlines, two levels deep emits prefix +
// indent + indent, etc.
func atIndent(prefix string, d doc) doc {
	if d == nil {
		return nil
	}
	return &docAtIndent{
		docBase: docBase{
			converts: d.asInfiniteWidthConverts(),
			breaks:   d.canBreak(),
		},
		prefix: prefix,
		child:  d,
	}
}

// docGroup picks flat-mode for its child if the flat-fit test
// succeeds (child's flat shape fits the line budget and contains no
// hard breaks), and broken-mode otherwise. See "flat-mode vs
// broken-mode" in the package doc.
type docGroup struct {
	docBase
	child doc
}

// group returns a Doc that picks flat-mode for d if the flat-fit
// test succeeds, otherwise broken-mode. See "flat-mode vs
// broken-mode" in the package doc.
//
// A [docGroup] is opaque to [asInfiniteWidth].
func group(d doc) doc {
	if d == nil {
		return nil
	}
	return &docGroup{
		docBase: docBase{breaks: d.canBreak()},
		child:   d,
	}
}

// docNextGroupNoop neutralises the next [docGroup] encountered on the
// renderer's left-first descent into child. When entered in modeBreak
// it ensures the next-docGroup-only skips its flat-fit retry, leaves
// the frame's mode untouched (remaining in modeBreak) - so its only
// effect is to push its child through unchanged, indistinguishable
// from no group at all. In modeFlat the primitive is transparent:
// docGroup is already effectively a no-op in modeFlat (no flat-fit
// retry fires), so no flag is needed.
//
// The intended use is wrapping each cell of a list/call whose
// neighbours form a "linked" bracketed-no-relpos run, so that when
// the surrounding [docBodyShape] commits to the hug shape (cells
// multi-line, brackets adjacent; see [docBodyShape]), every cell
// stays broken instead of independently flat-fitting its own
// content.
type docNextGroupNoop struct {
	docBase
	child doc
}

// nextGroupNoop wraps d in a [docNextGroupNoop]. See [docNextGroupNoop]
// for the propagation semantics.
func nextGroupNoop(d doc) doc {
	if d == nil {
		return nil
	}
	return &docNextGroupNoop{
		docBase: docBase{breaks: d.canBreak()},
		child:   d,
	}
}

// docBodyShape selects between three concrete render-time shapes for
// a list/call body. The decision is made anew at every level of
// nesting, so an outer list can pick one shape and an inner list a
// different shape at the same render.
//
// Using a list with two short bracketed cells as a running example
// (the body would be `{a: _}, {b: _}`), the three shapes are:
//
//  1. flat: brackets and body all on one line.
//
//     [{a: _}, {b: _}]
//
//     Selected when the enclosing list's [docGroup] picks modeFlat
//     (the whole list fits on the current line). docBodyShape is
//     entered in modeFlat and simply renders body in modeFlat - the
//     list's group's Cat handles the brackets adjacent to body.
//
//  2. indented: brackets on their own lines, body flat on its own
//     indented line.
//
//     [
//     \t{a: _}, {b: _},
//     ]
//
//     Selected when the list's group goes broken but body's flat width
//     still fits in `width - (indent+1) * indentWidth`. docBodyShape
//     emits HardLine (at indent+1), body in modeFlat (cells flat,
//     joined by their inline-space separator), trailingComma (if
//     non-nil), HardLine (at the outer indent). The closing bracket
//     emitted by the enclosing Cat then lands at the outer indent.
//
//     The indented shape exists for the cases where the list sits
//     deep into a long line so that flat would overflow yet body's
//     flat width still fits within a fresh indented line's
//     budget. For example, at width 35:
//
//     foo: bar.baz([{a: _}, {b: _}])   // flat: 32 chars, fits
//     foo: bar.baz.qux([{a: _}, {b: _}])  // flat: 36 chars, overflows
//
//     The latter still wants its body laid out flat - the cells are
//     tiny - but cannot afford the prefix. Moving body to its own
//     indented line gives:
//
//     foo: bar.baz.qux([
//     \t{a: _}, {b: _},
//     ])
//
//  3. hug: brackets adjacent to body; cells are forced multi-line.
//
//     [{
//     \ta: _
//     }, {
//     \tb: _
//     }]
//
//     Selected when neither the whole list nor an indented body
//     fits. docBodyShape renders body directly at the outer column in
//     modeBreak with no surrounding breaks. Cells must already be
//     wrapped in [docNextGroupNoop] (the caller arranges this via
//     elementRows' wrapLinked path): the modeBreak propagates into
//     each cell, the wrapper neutralises the cell's own outer
//     docGroup, and the cell's body renders broken. The inter-cell
//     separator stays a literal space, so the cells remain
//     horizontally chained `}, {`.
//
// trailingComma is emitted in the indented shape only - between body
// and the closing HardLine. Lists and call argument lists both accept
// a trailing comma before their closer in CUE, and emitting one here
// is what keeps the indented shape idempotent: a re-parsed `[\n body
// \n]` puts `Newline` on the closing bracket, which makes the
// standard layout add a trailing comma on re-render; matching that
// comma in the first render closes the loop. The hug shape suppresses
// the comma (a `},]` adjacency would look wrong, and re-parsed hug
// has `NoSpace` on the closer so the standard layout doesn't add one
// either). The flat shape has no place for a trailing comma to live.
type docBodyShape struct {
	docBase
	body          doc
	trailingComma doc
}

// bodyShape wraps body in a [docBodyShape]. body should be the inner
// content between the list/call's brackets (typically a [docTable] of
// cells, where each cell is itself a [docNextGroupNoop]).
// trailingComma is emitted in the indented shape only; pass nil to
// suppress.
func bodyShape(body, trailingComma doc) doc {
	if body == nil {
		return nil
	}
	return &docBodyShape{
		docBase:       docBase{breaks: true},
		body:          body,
		trailingComma: trailingComma,
	}
}

// docInfiniteWidth gives child an infinite line-width budget. At
// render time [docGroup]s inside child see an unlimited budget for
// their flat-fit test, so they pick flat (unless their subtrees
// contain [docLineBreakHard] / [docLineBreakBare]). The
// infinite-width state is maintained through child's subtree; only
// [docFiniteWidth] nodes restore the configured width budget for
// their own children.
type docInfiniteWidth struct {
	docBase
	child doc
}

// infiniteWidth returns a Doc that renders d under an infinite
// line-width budget: nested [docGroup]s see unlimited width for their
// flat-fit test, so no width-driven breaks occur and only
// [docLineBreakHard] / [docLineBreakBare] emit newlines.
//
// d is wrapped in `[docInfiniteWidth]{[group](asInfiniteWidth(d))}`:
//
//   - The [docInfiniteWidth] wrapping informs the renderer of the
//     unlimited budget so the renderer will not inject additional
//     newlines.
//   - The [group] wrapping ensures the rendering mode resets to flat
//     (the unlimited-budget flat-fit test will then succeed unless a
//     hard break exists), so inner [table]s render inline rather than
//     emitting bogus column-aligned padding on a single line.
//   - [asInfiniteWidth] stops the renderer injecting newlines based
//     on the line-width budget: [docLineBreakSoft] becomes its flat
//     text, which leaves existing hard breaks in the doc
//     [docLineBreakHard] / [docLineBreakBare] as the only source of
//     newlines. [docSwitchMode] nodes are kept (with both branches
//     normalised) so the runtime mode-choice still selects the right
//     branch; see [asInfiniteWidth].
//
// A [docFiniteWidth] subtree nested inside an infiniteWidth scope
// restores the width-driven layout for its own child.
func infiniteWidth(d doc) doc {
	if d == nil {
		return nil
	}
	child := group(asInfiniteWidth(d))
	return &docInfiniteWidth{
		docBase: docBase{breaks: child.canBreak()},
		child:   child,
	}
}

// docFiniteWidth marks child as a finite-width subtree. If
// docFiniteWidth is a descendant of a [docInfiniteWidth] node, then
// when rendered the infiniteWidth flag will be cancelled and the
// rendering mode reset to broken for child. docFiniteWidth is opaque
// to [asInfiniteWidth]: [docLineBreakSoft] and [docSwitchMode]
// alternates inside child are unaltered by [asInfiniteWidth].
type docFiniteWidth struct {
	docBase
	child doc
}

// finiteWidth returns a Doc that renders d under the configured
// (finite) line-width budget.
//
// If nested inside an [infiniteWidth] scope, finiteWidth ensures that
// at render time the infiniteWidth flag and rendering mode are
// cancelled for d's subtree.
//
// Outside an enclosing [docInfiniteWidth] scope, the reset of the
// line-width budget and rendering mode is a no-op.
func finiteWidth(d doc) doc {
	if d == nil {
		return nil
	}
	child := group(d)
	return &docFiniteWidth{
		docBase: docBase{breaks: child.canBreak()},
		child:   child,
	}
}

// asInfiniteWidth rewrites d to its form for rendering under an
// infinite line-width budget.
//
//   - A [docLineBreakSoft] says to the renderer "You can insert a
//     newline here, depending on width"; width-driven decisions don't
//     occur under infinite-width. But [docLineBreakSoft]s also emit
//     newlines when an enclosing [docGroup] is rendered in broken
//     mode, which can occur whenever the group's subtree contains a
//     hard break, even under infinite-width. So a [docLineBreakSoft]
//     sitting alongside a [docLineBreakHard] in the same [docGroup]
//     would still emit a newline at render time. To ensure that the
//     only newlines rendered under infinite-width are the hard line
//     breaks within d, [docLineBreakSoft]s are collapsed to their
//     flat alternative.
//   - A [docSwitchMode] has different [doc]s to emit in broken-mode
//     and flat-mode. The right branch depends on the enclosing
//     [docGroup]'s flat-vs-broken decision at render time: a Group
//     that fits flat under infinite-width should still emit the
//     SwitchMode's flat branch (e.g. suppress a trailing comma when
//     a small list collapses inline alongside other multi-line
//     content). Both branches are walked recursively so their
//     contents are themselves normalised for the infinite-width
//     context, but the SwitchMode itself is kept so the runtime
//     mode-choice still selects the right branch.
//
// Used by [infiniteWidth] to bake these static decisions into d
// before the renderer sees it. asInfiniteWidth does not descend into
// [docGroup] or [docFiniteWidth] nodes: both gate a render-time mode
// decision ([docGroup]'s flat-vs-broken fit-test; [docFiniteWidth]'s
// reset of the infiniteWidth and mode flags), and any
// [docLineBreakSoft] or [docSwitchMode] nodes inside those subtrees
// are meant to resolve under those runtime modes rather than be baked
// statically here. asInfiniteWidth also does not descend into a
// nested [docInfiniteWidth] - its child has already been processed by
// asInfiniteWidth at construction (see [infiniteWidth]), so
// re-descending would just repeat work. Returns d unchanged when the
// subtree contains nothing asInfiniteWidth would convert.
func asInfiniteWidth(d doc) doc {
	if d == nil || !d.asInfiniteWidthConverts() {
		return d
	}
	switch d := d.(type) {
	case *docLineBreakSoft:
		return &docStringLit{str: d.flat, width: d.flatWidth}
	case *docCat:
		return cat(asInfiniteWidth(d.left), asInfiniteWidth(d.right))
	case *docNest:
		return nest(asInfiniteWidth(d.child))
	case *docAtIndent:
		return atIndent(d.prefix, asInfiniteWidth(d.child))
	case *docSwitchMode:
		return switchMode(asInfiniteWidth(d.broken), asInfiniteWidth(d.flat))
	case *docTable:
		rows := make([]row, len(d.rows))
		for i, r := range d.rows {
			var cells []doc
			for j, cell := range r.cells {
				nc := asInfiniteWidth(cell)
				if nc != cell {
					if cells == nil {
						cells = slices.Clone(r.cells)
					}
					cells[j] = nc
				}
			}
			if cells == nil {
				cells = r.cells
			}

			rows[i] = row{
				sep:             asInfiniteWidth(r.sep),
				raw:             asInfiniteWidth(r.raw),
				docComment:      asInfiniteWidth(r.docComment),
				cells:           cells,
				hasComment:      r.hasComment,
				allowRowBreak:   r.allowRowBreak,
				mergedFirstCell: asInfiniteWidth(r.mergedFirstCell),
			}
		}
		return table(rows)
	}
	return d
}

// docSwitchMode emits broken when in broken-mode and flat when in
// flat-mode.
type docSwitchMode struct {
	docBase
	broken doc
	flat   doc
}

// switchMode returns a Doc that emits broken when in broken-mode and
// flat when in flat-mode.
func switchMode(broken, flat doc) doc {
	return &docSwitchMode{
		docBase: docBase{converts: true, breaks: docCanBreak(broken)},
		broken:  broken,
		flat:    flat,
	}
}

// docTable renders its rows with aligned columns. In flat-mode, rows
// are rendered inline separated by their Sep, and the cells within
// each row are joined by a single space. In broken-mode, the rows
// are partitioned into maximal aligned segments and each segment's
// columns are padded to that segment's maximum widths; see
// [renderer.renderTableInMode] for the segmentation rules.
type docTable struct {
	docBase
	rows []row
}

// table returns a Doc that renders its rows with aligned columns. In
// flat-mode, rows are rendered inline separated by their [row.Sep],
// and the cells within each row are joined by a single space. In
// broken-mode, the rows are partitioned into maximal aligned segments
// and each segment's columns are padded to that segment's maximum
// widths.
func table(rows []row) doc {
	if len(rows) == 0 {
		return nil
	}
	converts := false
	for _, row := range rows {
		if docAsInfiniteWidthConverts(row.sep) ||
			docAsInfiniteWidthConverts(row.raw) ||
			docAsInfiniteWidthConverts(row.docComment) ||
			slices.ContainsFunc(row.cells, docAsInfiniteWidthConverts) {
			converts = true
			break
		}
	}
	// In broken mode a docTable renders rows on separate lines via
	// the row Seps, so it always introduces a break.
	return &docTable{
		docBase: docBase{converts: converts, breaks: true},
		rows:    rows,
	}
}

// row represents one row of a table. A row is either:
//
//   - Raw: [row.Raw] is non-nil. The Doc is rendered as-is, without
//     column alignment - typically because the row's content does not
//     fit a column-aligned shape (e.g. a multi-line struct or list
//     value sitting among aligned key/value rows), or because the
//     caller wants this row to form its own segment.
//   - Aligned: [row.Cells] holds one Doc per column. The renderer
//     lays out columns left-to-right, computing the max width for
//     each column within an aligned segment, and pads each cell to
//     that column's width so the cells line up across rows. If a
//     row's cumulative width exceeds the target line width, that row
//     is excluded from contributing to subsequent column widths (so a
//     too-wide cell doesn't over-pad shorter rows).
type row struct {
	sep doc // separator to emit before this row (flat and broken-mode)
	raw doc // non-nil for non-aligned rows

	// cells holds the column contents for aligned rows.
	cells []doc

	// docComment holds a doc comment that appears on its own line(s)
	// before the first cell of this Row. It does not participate in
	// column width measurement. Its presence forces an enclosing group
	// to break.
	docComment doc

	// hasComment is true if the row contains any // comment in Cells
	// or embedded at positions 1-2. When true, an enclosing group is
	// forced to break because a // comment runs to end of line and
	// would swallow subsequent tokens in flat-mode.
	hasComment bool

	// allowRowBreak permits the renderer to break the row before its
	// second cell when the row's flat width would exceed the available
	// budget. Cells beyond the second cell stay on the second cell's
	// line. Used by field rows so that a wide `key: value` pair can
	// fall back to `key:\n\tvalue` instead of overflowing the
	// line. List/call rows leave it false because their first cell is
	// the value itself.
	allowRowBreak bool

	// mergedFirstCell is an alternative rendering for cell 0 that
	// already includes cell 1 (and only that - additional cells
	// continue to render normally), with cell 1 folded into cell 0's
	// deepest [docGroup]/[docNest] structure. Used when cell 0
	// contains nested split points (multiple [docGroup]s) and cell 1
	// is atomic: folding cell 1 into the innermost [docNest] lets each
	// split point's flat-fit check include cell 1, so Wadler-Lindig
	// breaks the outermost [docGroup] first and only descends as deep
	// as necessary to fit.
	//
	// For example, cell 0 might be the doc for "a: b: c:" with a
	// [docGroup] at each colon's split point and cell 1 = "val".
	// With mergedFirstCell, each [docGroup]'s flat-fit includes val,
	// so Wadler-Lindig descends only as deep as needed:
	//
	//	a:
	//	    b: c: val
	//
	// or, when "b: c: val" also does not fit:
	//
	//	a:
	//	    b:
	//	        c: val
	//
	// In both, val ends up on cell 0's last line.
	//
	// renderTableSegment chooses between Cells and mergedFirstCell per
	// segment. When the segment fits flat at its aligned width (so
	// cells line up across rows), Cells is used. When it does not fit,
	// mergedFirstCell is used for each row so partial breaks of cell 0
	// place cell 1 on cell 0's last line.
	mergedFirstCell doc
}
