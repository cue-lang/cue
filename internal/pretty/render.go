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
// concatenated with spaces between them. In broken-mode, columns are
// padded to align across rows. A row whose cumulative width exceeds
// the target is excluded from contributing to subsequent column
// widths.
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

	// broken-mode: compute column widths.
	numCols := 0
	for _, row := range rows {
		if row.Raw == nil {
			numCols = max(numCols, len(row.Cells))
		}
	}

	// Measure each cell's flat width and compute max per column. If a
	// cell is unflattenable (measureFlat returns false), the remaining
	// cells in that row are excluded from contributing to column
	// widths.
	colMaxW := make([]int, numCols)
	for _, row := range rows {
		if row.Raw != nil {
			continue
		}
		for c, cell := range row.Cells {
			w, ok := measureFlat(math.MaxInt, cell)
			if !ok {
				break
			}
			if w > colMaxW[c] {
				colMaxW[c] = w
			}
		}
	}

	// Render rows with column padding.
	for i, row := range rows {
		if i > 0 {
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
			// Pad to column max width (but not the last cell). If the
			// cell wrapped (emitted newlines), r.col - colStart gives
			// the content width on the last line. If r.col < colStart
			// (the last line ends before the start column), padding is
			// not meaningful so we skip it.
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
