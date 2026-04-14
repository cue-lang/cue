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
//     fails and that docGroup must render broken. [docLineBreakBare]
//     is like [docLineBreakHard] but emits a newline without
//     indentation, for use inside multi-line string bodies where
//     content must be preserved verbatim.
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
// We decide the mode per-[docGroup] at render time with a flat-fit
// test: the renderer measures what the child's flat shape would look
// like (resolving [docLineBreakSoft] and [docSwitchMode] the way
// flat-mode would) and picks flat-mode iff the result fits the line
// budget and contains no hard breaks. Thus a flat shape that contains
// [docLineBreakHard] / [docLineBreakBare] anywhere forces
// broken-mode.
//
// "Single-line output" is therefore an emergent property of
// flat-mode renderings, not part of the definition: a [docGroup] that
// picks flat-mode won't emit hard breaks, because the flat-fit test
// would otherwise have rejected it. This leaves room for a "force
// broken" idiom - a [docSwitchMode] whose flat branch contains a hard
// break poisons the flat-fit test, so the enclosing [docGroup] must
// pick broken; once broken, the [docSwitchMode] emits its broken
// branch (often nil), contributing nothing to the output.
//
// # finite vs infinite width
//
// Width-driven Wadler-Lindig is the default. We also support an
// "infinite-width" rendering scope in which the line-width budget is
// unlimited - [docGroup]s always pick flat unless their subtree
// contains a hard break. [docInfiniteWidth] enters the scope;
// [docFiniteWidth] cancels it for its child, restoring the configured
// budget. The two compose: a [docFiniteWidth] pocket nested inside a
// [docInfiniteWidth] scope renders under the real budget, leaving its
// surrounding scope unaffected.
//
// # Tables
//
// We extend Wadler-Lindig with [docTable] for column-aligned layouts.
// A [docTable] holds [row]s; each row is either a Raw Doc (rendered
// as-is) or a list of cells. In flat-mode we join rows by their
// separator and cells by spaces. In broken-mode we partition rows
// into maximal segments that share column alignment without forced
// newlines, compute per-segment column widths, and pad cells to align
// across rows within each segment. A multi-line or overflowing row
// "flushes" the alignment, forming its own segment, so it does not
// stretch the columns of the simpler rows around it. See render.go
// for the segmentation algorithm.
//
// # AST translation
//
// The translation from AST to [doc] lives in ast.go; see that file
// for AST-layer terminology (RelPos, authored vs programmatic mode,
// wrap sites, simple vs complex fields, chains).
package pretty

import (
	"slices"
	"unicode/utf8"
)

// doc represents a node in the Wadler-Lindig document algebra. A nil
// doc is the empty document (it produces no output).
type doc interface {
	// asInfiniteWidthConverts reports whether the subtree rooted at
	// this Doc would be modified when passed to [asInfiniteWidth]. We
	// compute it at construction so that asInfiniteWidth can
	// short-circuit whole subtrees without traversing them.
	asInfiniteWidthConverts() bool

	// canBreak reports whether the subtree rooted at this Doc could
	// emit a line break when rendered in modeBreak. It is true for the
	// break primitives themselves ([docLineBreakSoft],
	// [docLineBreakHard], [docLineBreakBare]) and the break-capable
	// composites ([docSwitchMode]'s broken branch, [docTable],
	// [docBodyShape]), propagated up through the container
	// constructors. We compute it at construction so it answers "is
	// this Doc pure text?" statically, without walking the subtree.
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

// docLineBreakBare is a bare newline without indentation, for the
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
// We use it for multi-line string bodies: the whitespace before the
// closing quote is preserved verbatim in prefix.
type docAtIndent struct {
	docBase
	prefix string
	child  doc
}

// atIndent returns a Doc that renders d with newlines emitting prefix
// verbatim before the nest-driven indentation. Inside d the nest
// level resets to 0, so an internal nest one level deep emits prefix
// + indent on its newlines, two levels deep emits prefix + indent +
// indent, and so on.
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

// docGroup picks flat-mode for its child when the flat-fit test
// succeeds (the child's flat shape fits the line budget and contains
// no hard breaks), and broken-mode otherwise. See "flat-mode vs
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
// renderer's left-first descent into child. When we enter it in
// modeBreak, it makes that next docGroup skip its flat-fit retry and
// leave the frame's mode untouched (so we stay in modeBreak), and the
// docGroup's only effect is to push its child through unchanged. In
// modeFlat the primitive is transparent: a docGroup is already a
// no-op there (no flat-fit retry fires), so no flag is needed.
//
// This keeps every cell of a list/call broken when the surrounding
// [docBodyShape] commits to the hug shape, rather than letting each
// cell independently flat-fit its own content.
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
// a list/call body. We make the decision anew at every level of
// nesting, so an outer list can pick one shape and an inner list a
// different shape in the same render.
//
// Take a list with two short bracketed cells as a running example
// (the body would be `{a: _}, {b: _}`). The three shapes are:
//
//  1. flat: brackets and body all on one line.
//
//     [{a: _}, {b: _}]
//
//     We pick this when the enclosing list's [docGroup] picks modeFlat
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
//     We pick this when the list's group goes broken but body's flat
//     width still fits in `width - (indent+1) * indentWidth`.
//     docBodyShape emits HardLine (at indent+1), body in modeFlat
//     (cells flat, joined by their inline-space separator),
//     trailingComma (if non-nil), and HardLine (at the outer indent).
//     The closing bracket emitted by the enclosing Cat then lands at
//     the outer indent. This handles a list sitting deep into a long
//     line, where flat would overflow yet body's flat width still fits
//     on a fresh indented line.
//
//  3. hug: brackets adjacent to body; cells are forced multi-line.
//
//     [{
//     \ta: _
//     }, {
//     \tb: _
//     }]
//
//     We pick this when neither the whole list nor an indented body
//     fits. docBodyShape renders body directly at the outer column in
//     modeBreak with no surrounding breaks. The cells must already be
//     wrapped in [docNextGroupNoop]: the modeBreak propagates into
//     each cell, the wrapper neutralises the cell's own outer
//     docGroup, and the cell's body renders broken. The inter-cell
//     separator stays a literal space, so the cells remain
//     horizontally chained `}, {`.
//
// We emit trailingComma in the indented shape only, between body and
// the closing HardLine. It keeps the indented shape idempotent: a
// re-parsed `[\n body \n]` marks the closing bracket with `Newline`,
// which makes the standard layout add a trailing comma on re-render,
// so matching that comma in the first render closes the loop. The hug
// shape suppresses the comma (re-parsed hug marks the closer
// `NoSpace`, so no comma is added on re-render either), and the flat
// shape has no place for one.
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
// render time the [docGroup]s inside child see an unlimited budget
// for their flat-fit test, so they pick flat (unless their subtrees
// contain [docLineBreakHard] / [docLineBreakBare]). We maintain the
// infinite-width state through child's subtree; only [docFiniteWidth]
// nodes restore the configured width budget for their own children.
type docInfiniteWidth struct {
	docBase
	child doc
}

// infiniteWidth returns a Doc that renders d under an infinite
// line-width budget: nested [docGroup]s see unlimited width for their
// flat-fit test, so no width-driven breaks occur and only
// [docLineBreakHard] / [docLineBreakBare] emit newlines.
//
// We wrap d in `[docInfiniteWidth]{[group](asInfiniteWidth(d))}`:
//
//   - The [docInfiniteWidth] wrapping tells the renderer about the
//     unlimited budget, so it will not inject additional newlines.
//   - The [group] wrapping resets the rendering mode to flat (the
//     unlimited-budget flat-fit test then succeeds unless a hard
//     break exists), so inner [table]s render inline rather than
//     emitting bogus column-aligned padding on a single line.
//   - [asInfiniteWidth] stops the renderer injecting newlines based on
//     the line-width budget: [docLineBreakSoft] becomes its flat text,
//     which leaves the existing hard breaks ([docLineBreakHard] /
//     [docLineBreakBare]) as the only source of newlines. We keep the
//     [docSwitchMode] nodes (with both branches normalised) so the
//     runtime mode-choice still selects the right branch; see
//     [asInfiniteWidth].
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

// docFiniteWidth marks child as a finite-width subtree. When
// docFiniteWidth is a descendant of a [docInfiniteWidth] node, the
// renderer cancels the infiniteWidth flag and resets the rendering
// mode to broken for child. docFiniteWidth is opaque to
// [asInfiniteWidth]: the [docLineBreakSoft] and [docSwitchMode]
// alternates inside child are left unaltered.
type docFiniteWidth struct {
	docBase
	child doc
}

// finiteWidth returns a Doc that renders d under the configured
// (finite) line-width budget.
//
// When nested inside an [infiniteWidth] scope, finiteWidth ensures
// that at render time we cancel the infiniteWidth flag and rendering
// mode for d's subtree.
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
//   - A [docLineBreakSoft] tells the renderer "You can insert a
//     newline here, depending on width"; width-driven decisions don't
//     occur under infinite-width. But a [docLineBreakSoft] also emits
//     a newline when an enclosing [docGroup] renders in broken mode,
//     which can happen whenever the group's subtree contains a hard
//     break, even under infinite-width. So a [docLineBreakSoft]
//     sitting alongside a [docLineBreakHard] in the same [docGroup]
//     would still emit a newline at render time. To ensure the only
//     newlines rendered under infinite-width are the hard line breaks
//     within d, we collapse each [docLineBreakSoft] to its flat
//     alternative.
//   - A [docSwitchMode] has different [doc]s to emit in broken-mode
//     and flat-mode. The right branch depends on the enclosing
//     [docGroup]'s flat-vs-broken decision at render time: a Group
//     that fits flat under infinite-width should still emit the
//     SwitchMode's flat branch (e.g. suppress a trailing comma when a
//     small list collapses inline alongside other multi-line
//     content). We walk both branches recursively so their contents
//     are themselves normalised for the infinite-width context, but
//     keep the SwitchMode itself so the runtime mode-choice still
//     selects the right branch.
//
// We do not descend into [docGroup] or [docFiniteWidth] nodes: both
// gate a render-time mode decision ([docGroup]'s flat-vs-broken
// fit-test; [docFiniteWidth]'s reset of the infiniteWidth and mode
// flags), and any [docLineBreakSoft] or [docSwitchMode] nodes inside
// those subtrees are meant to resolve under those runtime modes
// rather than be baked statically here. We also do not descend into a
// nested [docInfiniteWidth] - its child has already been processed by
// asInfiniteWidth at construction (see [infiniteWidth]), so
// re-descending would only repeat work. We return d unchanged when
// the subtree contains nothing asInfiniteWidth would convert.
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

// docTable renders its rows with aligned columns. In flat-mode we
// render the rows inline, separated by their Sep, and join the cells
// within each row by a single space. In broken-mode we partition the
// rows into maximal aligned segments and pad each segment's columns
// to that segment's maximum widths; see
// [renderer.renderTableInMode] for the segmentation rules.
type docTable struct {
	docBase
	rows []row
}

// table returns a Doc that renders its rows with aligned columns. In
// flat-mode we render the rows inline, separated by their [row.Sep],
// and join the cells within each row by a single space. In
// broken-mode we partition the rows into maximal aligned segments and
// pad each segment's columns to that segment's maximum widths.
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
	// In broken mode a docTable renders its rows on separate lines via
	// the row Seps, so it always introduces a break.
	return &docTable{
		docBase: docBase{converts: converts, breaks: true},
		rows:    rows,
	}
}

// row represents one row of a table. A row is either:
//
//   - Raw: [row.Raw] is non-nil. We render the Doc as-is, without
//     column alignment - typically because the row's content does not
//     fit a column-aligned shape (e.g. a multi-line struct or list
//     value sitting among aligned key/value rows), or because the
//     caller wants this row to form its own segment.
//   - Aligned: [row.Cells] holds one Doc per column. We lay the
//     columns out left-to-right, computing the max width for each
//     column within an aligned segment, and pad each cell to that
//     column's width so the cells line up across rows. When a row's
//     cumulative width exceeds the target line width, we exclude that
//     row from contributing to subsequent column widths (so a
//     too-wide cell doesn't over-pad the shorter rows).
type row struct {
	sep doc // separator to emit before this row (flat and broken-mode)
	raw doc // non-nil for non-aligned rows

	// cells holds the column contents for aligned rows.
	cells []doc

	// docComment holds a doc comment that appears on its own line(s)
	// before the first cell of this Row. It does not participate in
	// column width measurement, and its presence forces an enclosing
	// group to break.
	docComment doc

	// hasComment is true when the row contains any // comment in Cells
	// or embedded at positions 1-2. When true, we force an enclosing
	// group to break, because a // comment runs to end of line and
	// would otherwise swallow subsequent tokens in flat-mode.
	hasComment bool

	// allowRowBreak permits the renderer to break the row before its
	// second cell when the row's flat width would exceed the available
	// budget. Cells beyond the second one stay on the second cell's
	// line. Field rows set it so that a wide `key: value` pair can fall
	// back to `key:\n\tvalue` rather than overflow the line; list/call
	// rows leave it false, because there the first cell is the value
	// itself.
	allowRowBreak bool

	// mergedFirstCell is an alternative rendering for cell 0 that
	// already includes cell 1 (and only that - additional cells
	// continue to render normally), with cell 1 folded into cell 0's
	// deepest [docGroup]/[docNest] structure. It applies when cell 0
	// contains nested split points (multiple [docGroup]s) and cell 1
	// is atomic: folding cell 1 into the innermost [docNest] lets each
	// split point's flat-fit check include cell 1, so Wadler-Lindig
	// breaks the outermost [docGroup] first and descends only as deep
	// as it must to fit, leaving cell 1 on cell 0's last line. E.g. for
	// cell 0 = "a: b: c:" (a [docGroup] per colon) and cell 1 = "val":
	//
	//	a:
	//	    b: c: val
	//
	// We use mergedFirstCell for a segment that does not fit flat at
	// its aligned width, and the plain cells otherwise.
	mergedFirstCell doc
}
