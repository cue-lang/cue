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
package pretty

import (
	"slices"
	"unicode/utf8"
)

// Doc represents a node in the Wadler-Lindig document algebra. A nil
// Doc is the empty document (produces no output).
type Doc interface {
	// asInfiniteWidthConverts reports whether the subtree rooted at
	// this Doc would be modified when passed to [asInfiniteWidth].
	// Computed at construction so asInfiniteWidth can short-circuit
	// whole subtrees without traversing them.
	asInfiniteWidthConverts() bool
}

func docAsInfiniteWidthConverts(d Doc) bool {
	return d != nil && d.asInfiniteWidthConverts()
}

type docBase struct {
	converts bool
}

func (d docBase) asInfiniteWidthConverts() bool { return d.converts }

// docStringLit emits a literal string. str must not contain newlines.
type docStringLit struct {
	docBase
	str   string
	width int // rune count
}

// StringLit returns a Doc that emits the literal string s. s must not
// contain newlines.
func StringLit(s string) Doc {
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

// LineBreakSoft returns a soft line break. In flat-mode it emits
// flat; in broken-mode it emits a newline followed by the current
// indentation.
func LineBreakSoft(flat string) Doc {
	return &docLineBreakSoft{
		docBase:   docBase{converts: true},
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
	left  Doc
	right Doc
}

// Cat returns the concatenation of a and b.
func Cat(a, b Doc) Doc {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &docCat{
		docBase: docBase{converts: a.asInfiniteWidthConverts() || b.asInfiniteWidthConverts()},
		left:    a,
		right:   b,
	}
}

// Cats concatenates all non-nil docs left to right.
func Cats(docs ...Doc) Doc {
	var result Doc
	for _, d := range docs {
		result = Cat(result, d)
	}
	return result
}

// Sep intersperses sep between non-nil docs.
func Sep(sep Doc, docs ...Doc) Doc {
	var result Doc
	for _, d := range docs {
		if d == nil {
			continue
		}
		if result == nil {
			result = d
		} else {
			result = Cat(Cat(result, sep), d)
		}
	}
	return result
}

// docNest increases the indent level by one for child.
type docNest struct {
	docBase
	child Doc
}

// Nest returns a Doc that increases the indent level by one for d.
func Nest(d Doc) Doc {
	if d == nil {
		return nil
	}
	return &docNest{
		docBase: docBase{converts: d.asInfiniteWidthConverts()},
		child:   d,
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
	child  Doc
}

// AtIndent returns a Doc that renders d with newlines emitting prefix
// verbatim before the nest-driven indentation. Inside d the nest
// level resets to 0, so an internal nest, one level deep, emits
// prefix + indent on its newlines, two levels deep emits prefix +
// indent + indent, etc.
func AtIndent(prefix string, d Doc) Doc {
	if d == nil {
		return nil
	}
	return &docAtIndent{
		docBase: docBase{converts: d.asInfiniteWidthConverts()},
		prefix:  prefix,
		child:   d,
	}
}

// docGroup tries to render child on a single line (flat-mode). If it
// doesn't fit within the target width, child is rendered in broken
// mode.
type docGroup struct {
	docBase
	child Doc
}

// Group returns a Doc that tries to render d on a single line (flat
// mode). If it doesn't fit within the target width, d is rendered in
// broken-mode.
//
// A [docGroup] is opaque to [asInfiniteWidth].
func Group(d Doc) Doc {
	if d == nil {
		return nil
	}
	return &docGroup{child: d}
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
	child Doc
}

// infiniteWidth returns a Doc that renders d under an infinite
// line-width budget: nested [docGroup]s see unlimited width for their
// flat-fit test, so no width-driven breaks occur and only
// [docLineBreakHard] / [docLineBreakBare] emit newlines.
//
// d is wrapped in `[docInfiniteWidth]{[Group](asInfiniteWidth(d))}`:
//
//   - The [docInfiniteWidth] wrapping informs the renderer of the
//     unlimited budget so the renderer will not inject additional
//     newlines.
//   - The [Group] wrapping ensures the rendering mode resets to flat
//     (the unlimited-budget flat-fit test will then succeed unless a
//     hard break exists), so inner [Table]s render inline rather than
//     emitting bogus column-aligned padding on a single line.
//   - [asInfiniteWidth] stops the renderer injecting newlines based
//     on the line-width budget: [docLineBreakSoft] becomes its flat
//     text and [docSwitchMode] resolves to its broken branch, which
//     leaves existing hard breaks in the doc [docLineBreakHard] /
//     [docLineBreakBare] as the only source of newlines.
//
// A [docFiniteWidth] subtree nested inside an infiniteWidth scope
// restores the width-driven layout for its own child.
func infiniteWidth(d Doc) Doc {
	if d == nil {
		return nil
	}
	return &docInfiniteWidth{child: Group(asInfiniteWidth(d))}
}

// docFiniteWidth marks child as a finite-width subtree. If
// docFiniteWidth is a descendant of a [docInfiniteWidth] node, then
// when rendered the infiniteWidth flag will be cancelled and the
// rendering mode reset to broken for child. docFiniteWidth is opaque
// to [asInfiniteWidth]: [docLineBreakSoft] and [docSwitchMode]
// alternates inside child are unaltered by [asInfiniteWidth].
type docFiniteWidth struct {
	docBase
	child Doc
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
func finiteWidth(d Doc) Doc {
	if d == nil {
		return nil
	}
	return &docFiniteWidth{child: Group(d)}
}

// asInfiniteWidth rewrites d to its form for rendering under an
// infinite line-width budget: [docLineBreakSoft] becomes its flat
// text (no newline) and [docSwitchMode] resolves to its broken
// branch. The two rewrites point in opposite directions because they
// have different jobs to do:
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
//   - A [docSwitchMode] has different [Doc]s to emit in broken-mode
//     and flat-mode. Although we're rendering with infinite-width,
//     the convention is that the [docSwitchMode]'s broken-mode
//     content is the correct content to use when d is
//     multi-line. Additionally, in some cases, the flat content of a
//     [docSwitchMode] can contain hard line breaks which would then
//     cause the flat-fit test to fail even with infinite
//     width. Consequently, we always want the broken-mode content of
//     [docSwitchMode] nodes.
//
// Used by [infiniteWidth] to bake these static decisions into d
// before the renderer sees it. asInfiniteWidth does not descend into
// [docGroup] or [docFiniteWidth] nodes: both gate a render-time mode
// decision ([docGroup]'s flat-vs-broken fit-test; [docFiniteWidth]'s
// reset of the infiniteWidth and mode flags), and any
// [docLineBreakSoft] or [docSwitchMode] nodes inside those subtrees
// are meant to resolve under those runtime modes rather than be baked
// statically here. Returns d unchanged when the subtree contains
// nothing asInfiniteWidth would convert.
func asInfiniteWidth(d Doc) Doc {
	if d == nil || !d.asInfiniteWidthConverts() {
		return d
	}
	switch d := d.(type) {
	case *docLineBreakSoft:
		return &docStringLit{str: d.flat, width: d.flatWidth}
	case *docCat:
		return Cat(asInfiniteWidth(d.left), asInfiniteWidth(d.right))
	case *docNest:
		return Nest(asInfiniteWidth(d.child))
	case *docAtIndent:
		return AtIndent(d.prefix, asInfiniteWidth(d.child))
	case *docSwitchMode:
		return asInfiniteWidth(d.broken)
	case *docTable:
		rows := make([]Row, len(d.rows))
		for i, row := range d.rows {
			var cells []Doc
			for j, cell := range row.Cells {
				nc := asInfiniteWidth(cell)
				if nc != cell {
					if cells == nil {
						cells = slices.Clone(row.Cells)
					}
					cells[j] = nc
				}
			}
			if cells == nil {
				cells = row.Cells
			}

			rows[i] = Row{
				Sep:        asInfiniteWidth(row.Sep),
				Raw:        asInfiniteWidth(row.Raw),
				DocComment: asInfiniteWidth(row.DocComment),
				Cells:      cells,
				HasComment: row.HasComment,
			}
		}
		return Table(rows)
	}
	return d
}

// docSwitchMode emits broken when in broken-mode and flat when in
// flat-mode.
type docSwitchMode struct {
	docBase
	broken Doc
	flat   Doc
}

// SwitchMode returns a Doc that emits broken when in broken-mode and
// flat when in flat-mode.
func SwitchMode(broken, flat Doc) Doc {
	return &docSwitchMode{
		docBase: docBase{converts: true},
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
	rows []Row
}

// Table returns a Doc that renders its rows with aligned columns. In
// flat-mode, rows are rendered inline separated by their [Row.Sep],
// and the cells within each row are joined by a single space. In
// broken-mode, the rows are partitioned into maximal aligned segments
// and each segment's columns are padded to that segment's maximum
// widths.
func Table(rows []Row) Doc {
	if len(rows) == 0 {
		return nil
	}
	converts := false
	for _, row := range rows {
		if docAsInfiniteWidthConverts(row.Sep) ||
			docAsInfiniteWidthConverts(row.Raw) ||
			docAsInfiniteWidthConverts(row.DocComment) ||
			slices.ContainsFunc(row.Cells, docAsInfiniteWidthConverts) {
			converts = true
			break
		}
	}
	return &docTable{
		docBase: docBase{converts: converts},
		rows:    rows,
	}
}

// Row represents one row of a table.
//
// Aligned rows have a Cells slice - a Doc per column that is padded
// for alignment across rows. The renderer lays out columns
// left-to-right, computing the max width for each column. If a row's
// cumulative width exceeds the target line width, it is excluded from
// contributing to subsequent column widths (so a too-wide cell
// doesn't over-pad shorter rows).
//
// Raw rows ([Row.Raw] != nil) are rendered as-is without alignment -
// used for complex fields (struct/list values) interspersed among
// aligned fields, and for the leading row of a chain table (see
// below).
//
// Struct fields use one cell per column so that keys, values,
// attributes and trailing comments each align across rows.  For chain
// arms (| or &), the first arm is emitted as a Raw row (so its
// operator suffix stays glued to the expression) and subsequent arms
// use Cells of [expr+op] or [expr+op, comment].
type Row struct {
	Sep Doc // separator to emit before this row (flat and broken-mode)
	Raw Doc // non-nil for non-aligned rows

	// Cells holds the column contents for aligned rows.
	Cells []Doc

	// DocComment holds a doc comment that appears on its own line(s)
	// before the first cell of this Row. It does not participate in
	// column width measurement. Its presence forces an enclosing
	// group to break.
	DocComment Doc

	// HasComment is true if the row contains any // comment in Cells
	// or embedded at positions 1-2. When true, an enclosing group is
	// forced to break because a // comment runs to end of line and
	// would swallow subsequent tokens in flat-mode.
	HasComment bool

	// AllowRowBreak permits the renderer to break the row before its
	// second cell when the row's flat width would exceed the available
	// budget. Cells beyond the second cell stay on the second cell's
	// line. Used by field rows so that a wide `key: value` pair can
	// fall back to `key:\n\tvalue` instead of overflowing the
	// line. List/call rows leave it false because their first cell is
	// the value itself.
	AllowRowBreak bool

	// MergedFirstCell is an alternative rendering for cell 0 that
	// already includes cell 1 (and only that - additional cells
	// continue to render normally) inside a nested-Group structure.
	// Used by field rows whose key is a braceless chain and whose
	// value is atomic: the merged Doc lets each chain split point's
	// flat-fit check include val, so Wadler-Lindig breaks the
	// outermost Group first and only descends as deep as necessary.
	//
	// renderTableSegment chooses between Cells and MergedFirstCell
	// per segment: when the segment fits flat at its aligned width
	// (so all values line up across rows), Cells is used. When it
	// doesn't fit, MergedFirstCell is used for each row so partial
	// chain breaks place val on the chain's actual deepest line -
	// at the cost of losing val-column alignment, which doesn't
	// matter in the broken case anyway.
	MergedFirstCell Doc
}
