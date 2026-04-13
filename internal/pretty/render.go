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

import "strings"

// Indent controls how each indentation level is rendered.
type Indent struct {
	// UseTab, when true, renders each indent level as a single tab character.
	// When false, each level is rendered as Spaces space characters.
	UseTab bool

	// Spaces is the number of spaces per indent level (ignored when UseTab is true).
	// Defaults to 4 if zero.
	Spaces int
}

func (ind Indent) render(b *strings.Builder, level int) {
	if ind.UseTab {
		for range level {
			b.WriteByte('\t')
		}
		return
	}
	spaces := ind.Spaces
	if spaces <= 0 {
		spaces = 4
	}
	for range level * spaces {
		b.WriteByte(' ')
	}
}

// indentWidth returns the column width of one indent level for line-fitting calculations.
func (ind Indent) width() int {
	if ind.UseTab {
		return 4 // conventional tab width for fitting purposes
	}
	if ind.Spaces <= 0 {
		return 4
	}
	return ind.Spaces
}

// Render lays out a document to fit within width columns, using the default
// indent of 4 spaces per level.
func Render(width int, doc *Doc) string {
	return RenderIndent(width, Indent{}, doc)
}

// RenderIndent lays out a document to fit within width columns, using the
// given indentation style.
func RenderIndent(width int, ind Indent, doc *Doc) string {
	var b strings.Builder
	indWidth := ind.width()

	type entry struct {
		level int // logical indent level
		mode  layoutMode
		doc   *Doc
	}

	fits := func(remaining int, stack []entry) bool {
		for len(stack) > 0 && remaining >= 0 {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch top.doc.kind {
			case docNil:
			case docText:
				remaining -= len(top.doc.text)
			case docLine:
				if top.mode == modeFlat {
					remaining--
				} else {
					// New line: remaining is width minus the indent columns.
					remaining = width - top.level*indWidth
				}
			case docSoftLine:
				if top.mode != modeFlat {
					remaining = width - top.level*indWidth
				}
			case docNest:
				stack = append(stack, entry{top.level + top.doc.indent, top.mode, top.doc.inner})
			case docConcat:
				stack = append(stack, entry{top.level, top.mode, top.doc.right})
				stack = append(stack, entry{top.level, top.mode, top.doc.left})
			case docUnion:
				stack = append(stack, entry{top.level, top.mode, top.doc.left})
			case docGroup:
				stack = append(stack, entry{top.level, modeFlat, top.doc.inner})
			case docTable:
				flat := flattenTable(top.doc)
				stack = append(stack, entry{top.level, modeFlat, flat})
			}
		}
		return remaining >= 0
	}

	var stack []entry
	stack = append(stack, entry{0, modeBreak, doc})

	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch top.doc.kind {
		case docNil:

		case docText:
			b.WriteString(top.doc.text)

		case docLine:
			if top.mode == modeFlat {
				b.WriteByte(' ')
			} else {
				b.WriteByte('\n')
				ind.render(&b, top.level)
			}

		case docSoftLine:
			if top.mode != modeFlat {
				b.WriteByte('\n')
				ind.render(&b, top.level)
			}

		case docNest:
			stack = append(stack, entry{top.level + top.doc.indent, top.mode, top.doc.inner})

		case docConcat:
			stack = append(stack, entry{top.level, top.mode, top.doc.right})
			stack = append(stack, entry{top.level, top.mode, top.doc.left})

		case docUnion:
			flatStack := []entry{{top.level, modeFlat, top.doc.left}}
			remaining := width - b.Len()
			s := b.String()
			if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
				remaining = width - (len(s) - idx - 1)
			}
			if fits(remaining, flatStack) {
				stack = append(stack, entry{top.level, modeFlat, top.doc.left})
			} else {
				stack = append(stack, entry{top.level, modeBreak, top.doc.right})
			}

		case docTable:
			// Table acts like a Group: try flat, fall back to aligned.
			flatDoc := flattenTable(top.doc)
			remaining := width - currentCol(&b)
			if fits(remaining, []entry{{top.level, modeFlat, flatDoc}}) {
				stack = append(stack, entry{top.level, modeFlat, flatDoc})
			} else {
				aligned := expandTable(top.doc)
				stack = append(stack, entry{top.level, modeBreak, aligned})
			}

		case docGroup:
			flat := flatten(top.doc.inner)
			u := &Doc{kind: docUnion, left: flat, right: top.doc.inner}
			stack = append(stack, entry{top.level, top.mode, u})
		}
	}

	return b.String()
}

type layoutMode int

const (
	modeFlat layoutMode = iota
	modeBreak
)

// flatten replaces all Line nodes with single spaces.
func flatten(d *Doc) *Doc {
	switch d.kind {
	case docNil, docText:
		return d
	case docLine:
		return Text(" ")
	case docSoftLine:
		return Nil()
	case docNest:
		return &Doc{kind: docNest, indent: d.indent, inner: flatten(d.inner)}
	case docConcat:
		return &Doc{kind: docConcat, left: flatten(d.left), right: flatten(d.right)}
	case docUnion:
		return flatten(d.left)
	case docGroup:
		return flatten(d.inner)
	case docTable:
		return flattenTable(d)
	}
	return d
}

// flattenTable converts a Table to a flat comma-separated document:
// "cell0 cell1, cell0 cell1, ..."
func flattenTable(d *Doc) *Doc {
	var rowDocs []*Doc
	for _, row := range d.tableRows {
		var r *Doc
		for i, cell := range row.Cells {
			flat := flatten(cell)
			if i == 0 {
				r = flat
			} else {
				r = Concat(r, Concat(Text(" "), flat))
			}
		}
		if r != nil {
			rowDocs = append(rowDocs, r)
		}
	}
	return joinCommaFlat(rowDocs)
}

// joinCommaFlat joins documents with ", ".
func joinCommaFlat(docs []*Doc) *Doc {
	if len(docs) == 0 {
		return Nil()
	}
	result := docs[0]
	for _, d := range docs[1:] {
		result = Concat(result, Concat(Text(", "), d))
	}
	return result
}

// flatWidth computes the width of a document when rendered flat (all Lines
// become spaces, all SoftLines vanish). Returns -1 if the doc contains
// a table in break mode (which we can't cheaply measure).
func flatWidth(d *Doc) int {
	switch d.kind {
	case docNil:
		return 0
	case docText:
		return len(d.text)
	case docLine:
		return 1 // space
	case docSoftLine:
		return 0
	case docNest:
		return flatWidth(d.inner)
	case docConcat:
		l := flatWidth(d.left)
		r := flatWidth(d.right)
		if l < 0 || r < 0 {
			return -1
		}
		return l + r
	case docUnion:
		return flatWidth(d.left)
	case docGroup:
		return flatWidth(d.inner)
	case docTable:
		return flatWidth(flattenTable(d))
	}
	return 0
}

// expandTable converts a table into a regular Doc with padding so columns
// align. The returned Doc uses standard Concat/Line/Text nodes, so the
// main renderer handles indentation naturally.
func expandTable(d *Doc) *Doc {
	rows := d.tableRows
	if len(rows) == 0 {
		return Nil()
	}

	// Determine the number of columns (max across rows).
	numCols := 0
	for _, row := range rows {
		if len(row.Cells) > numCols {
			numCols = len(row.Cells)
		}
	}

	// Measure the flat width of each cell, and find the max per column.
	// Only count rows that participate in alignment (NoAlign rows are excluded).
	// We don't pad the last column.
	colWidths := make([]int, numCols)
	for _, row := range rows {
		if row.NoAlign {
			continue
		}
		for c := 0; c < len(row.Cells) && c < numCols-1; c++ {
			w := flatWidth(row.Cells[c])
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	// Build a Doc for each row with padding.
	var rowDocs []*Doc
	for _, row := range rows {
		var r *Doc
		for c, cell := range row.Cells {
			var cellDoc *Doc
			if c < numCols-1 {
				// Label columns: render flat, pad to column width
				// (unless this row opts out of alignment).
				flat := flatten(cell)
				if row.NoAlign {
					cellDoc = flat
				} else {
					w := flatWidth(cell)
					pad := ""
					if colWidths[c]-w > 0 {
						pad = strings.Repeat(" ", colWidths[c]-w)
					}
					cellDoc = Concat(flat, Text(pad))
				}
			} else {
				// Value column: keep as-is so it can break.
				cellDoc = cell
			}
			if c == 0 {
				r = cellDoc
			} else {
				r = Concat(r, Concat(Text(" "), cellDoc))
			}
		}
		if r != nil {
			rowDocs = append(rowDocs, r)
		}
	}

	return joinComma(rowDocs)
}

// currentCol returns the current column position (distance from last newline).
func currentCol(b *strings.Builder) int {
	s := b.String()
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		return len(s) - idx - 1
	}
	return b.Len()
}
