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
	"strings"
)

type mode uint8

const (
	modeBreak mode = iota
	modeFlat
)

// entry is a stack element for the rendering algorithm.
type entry struct {
	ind  int  // current indent level
	mode mode // flat or break
	doc  *Doc
}

// Render formats a Doc into bytes using the Wadler-Lindig best-fit algorithm.
func Render(width int, indent string, doc *Doc) []byte {
	r := &renderer{
		width:  width,
		indent: indent,
	}
	r.render(doc)
	return r.buf.Bytes()
}

type renderer struct {
	width  int
	indent string
	buf    bytes.Buffer
	col    int
}

func (r *renderer) render(doc *Doc) {
	r.renderInMode(0, modeBreak, doc)
}

// newline writes a newline followed by indentation for the given level.
func (r *renderer) newline(ind int) {
	r.buf.WriteByte('\n')
	for range ind {
		r.buf.WriteString(r.indent)
	}
	r.col = ind * len(r.indent)
}

// fits checks whether doc can be rendered in flat mode within remaining columns.
// It short-circuits on HardLine or when remaining goes negative.
func (r *renderer) fits(remaining int, ind int, doc *Doc) bool {
	// Use an explicit stack to avoid recursion.
	stack := []entry{{ind, modeFlat, doc}}
	for len(stack) > 0 {
		if remaining < 0 {
			return false
		}
		top := len(stack) - 1
		e := stack[top]
		stack = stack[:top]

		if e.doc == nil {
			continue
		}

		switch e.doc.tag {
		case tagText:
			remaining -= len(e.doc.text)

		case tagLine:
			// In flat mode, Line emits its alt text.
			remaining -= len(e.doc.text)

		case tagHard:
			// A HardLine means this group cannot be flattened.
			return false

		case tagLitLine:
			// A literal newline (multi-line string) also prevents flattening.
			return false

		case tagCat:
			stack = append(stack, entry{e.ind, e.mode, e.doc.right})
			stack = append(stack, entry{e.ind, e.mode, e.doc.left})

		case tagNest:
			stack = append(stack, entry{e.ind + e.doc.n, e.mode, e.doc.right})

		case tagGroup:
			// Nested groups are flattened in fits check.
			stack = append(stack, entry{e.ind, modeFlat, e.doc.right})

		case tagIfBreak:
			// In flat mode, use the flat variant.
			stack = append(stack, entry{e.ind, e.mode, e.doc.right})

		case tagTable:
			// A // comment (trailing or doc) in any row runs to end of
			// line and would swallow subsequent tokens in flat mode.
			// Force break.
			for _, row := range e.doc.rows {
				if row.HasComment || row.DocComment != nil {
					return false
				}
			}
			// Measure table in flat mode: sep key val sep key val ...
			for i := len(e.doc.rows) - 1; i >= 0; i-- {
				row := e.doc.rows[i]
				if row.Raw != nil {
					stack = append(stack, entry{e.ind, modeFlat, row.Raw})
				} else {
					stack = append(stack, entry{e.ind, modeFlat, row.Val})
					stack = append(stack, entry{e.ind, modeFlat, spaceText})
					stack = append(stack, entry{e.ind, modeFlat, row.Key})
				}
				if i > 0 && row.Sep != nil {
					stack = append(stack, entry{e.ind, modeFlat, row.Sep})
				}
			}
		}
	}
	return remaining >= 0
}

// measure returns the flat-mode width of a doc (for table column measurement).
func (r *renderer) measure(doc *Doc) int {
	if doc == nil {
		return 0
	}
	width := 0
	stack := []*Doc{doc}
	for len(stack) > 0 {
		top := len(stack) - 1
		d := stack[top]
		stack = stack[:top]

		if d == nil {
			continue
		}

		switch d.tag {
		case tagText:
			width += len(d.text)
		case tagLine:
			width += len(d.text)
		case tagHard:
			// Shouldn't appear in table keys, but count as 0.
		case tagLitLine:
			// Shouldn't appear in table keys, but count as 0.
		case tagCat:
			stack = append(stack, d.right)
			stack = append(stack, d.left)
		case tagNest:
			stack = append(stack, d.right)
		case tagGroup:
			stack = append(stack, d.right)
		case tagIfBreak:
			// Measure in flat mode.
			stack = append(stack, d.right)
		case tagTable:
			for i, row := range d.rows {
				if i > 0 {
					if row.Sep != nil {
						stack = append(stack, row.Sep)
					} else {
						width += 2 // ", "
					}
				}
				if row.Raw != nil {
					stack = append(stack, row.Raw)
				} else {
					stack = append(stack, row.Val)
					stack = append(stack, spaceText)
					stack = append(stack, row.Key)
				}
			}
		}
	}
	return width
}

// renderTable renders table rows. In flat mode, rows are joined by ", ".
// In broken mode, aligned rows (Key/Val) have their keys padded to align
// values; raw rows are rendered as-is.
func (r *renderer) renderTable(ind int, m mode, rows []Row) {
	if m == modeFlat {
		for i, row := range rows {
			if i > 0 {
				if row.Sep != nil {
					r.renderFlat(row.Sep)
				} else {
					r.buf.WriteString(", ")
					r.col += 2
				}
			}
			if row.Raw != nil {
				r.renderFlat(row.Raw)
			} else {
				r.renderFlat(row.Key)
				r.buf.WriteByte(' ')
				r.col++
				r.renderFlat(row.Val)
				if row.TrailingComment != nil {
					r.buf.WriteByte(' ')
					r.col++
					r.renderFlat(row.TrailingComment)
				}
			}
		}
		return
	}

	// Broken mode: measure aligned keys and compute max width.
	maxKeyW := 0
	keyWidths := make([]int, len(rows))
	for i, row := range rows {
		if row.Raw != nil {
			continue // raw rows don't participate in alignment
		}
		keyWidths[i] = r.measure(row.Key)
		if keyWidths[i] > maxKeyW {
			maxKeyW = keyWidths[i]
		}
	}

	// Measure the full key+padding+space+value width for each aligned row
	// that has a trailing comment, to align comments across rows.
	maxFullW := 0
	hasAnyComment := false
	fullWidths := make([]int, len(rows))
	for i, row := range rows {
		if row.Raw != nil || row.TrailingComment == nil {
			continue
		}
		// Full width = maxKeyW + 1 (space) + value width.
		fullWidths[i] = maxKeyW + 1 + r.measure(row.Val)
		if fullWidths[i] > maxFullW {
			maxFullW = fullWidths[i]
		}
		hasAnyComment = true
	}

	for i, row := range rows {
		if i > 0 {
			if row.Sep != nil {
				r.renderInMode(ind, modeBreak, row.Sep)
			} else {
				r.newline(ind)
			}
		}
		if row.Raw != nil {
			r.renderInMode(ind, modeBreak, row.Raw)
			continue
		}
		if row.DocComment != nil {
			r.renderInMode(ind, modeBreak, row.DocComment)
			r.newline(ind)
		}
		r.renderFlat(row.Key)
		pad := maxKeyW - keyWidths[i]
		if pad > 0 {
			r.buf.WriteString(strings.Repeat(" ", pad))
			r.col += pad
		}
		r.buf.WriteByte(' ')
		r.col++
		// Render value in broken mode so nested groups can break.
		r.renderInMode(ind, modeBreak, row.Val)

		// Trailing comment: pad to align with other rows' comments.
		if row.TrailingComment != nil {
			if hasAnyComment && maxFullW > fullWidths[i] {
				cpad := maxFullW - fullWidths[i]
				r.buf.WriteString(strings.Repeat(" ", cpad))
				r.col += cpad
			}
			r.buf.WriteByte(' ')
			r.col++
			r.renderFlat(row.TrailingComment)
		}
	}
}

// renderFlat renders a doc in flat mode (no line breaks).
func (r *renderer) renderFlat(doc *Doc) {
	r.renderInMode(0, modeFlat, doc)
}

// renderInMode renders a doc using the given indent and mode.
func (r *renderer) renderInMode(ind int, m mode, doc *Doc) {
	stack := []entry{{ind, m, doc}}
	for len(stack) > 0 {
		top := len(stack) - 1
		e := stack[top]
		stack = stack[:top]

		if e.doc == nil {
			continue
		}

		switch e.doc.tag {
		case tagText:
			r.buf.WriteString(e.doc.text)
			r.col += len(e.doc.text)

		case tagLine:
			if e.mode == modeFlat {
				r.buf.WriteString(e.doc.text)
				r.col += len(e.doc.text)
			} else {
				r.newline(e.ind)
			}

		case tagHard:
			r.newline(e.ind)

		case tagLitLine:
			r.buf.WriteByte('\n')
			r.col = 0

		case tagCat:
			stack = append(stack, entry{e.ind, e.mode, e.doc.right})
			stack = append(stack, entry{e.ind, e.mode, e.doc.left})

		case tagNest:
			stack = append(stack, entry{e.ind + e.doc.n, e.mode, e.doc.right})

		case tagGroup:
			if e.mode == modeFlat || r.fits(r.width-r.col, e.ind, e.doc.right) {
				stack = append(stack, entry{e.ind, modeFlat, e.doc.right})
			} else {
				stack = append(stack, entry{e.ind, modeBreak, e.doc.right})
			}

		case tagIfBreak:
			if e.mode == modeBreak {
				stack = append(stack, entry{e.ind, e.mode, e.doc.left})
			} else {
				stack = append(stack, entry{e.ind, e.mode, e.doc.right})
			}

		case tagTable:
			r.renderTable(e.ind, e.mode, e.doc.rows)
		}
	}
}
