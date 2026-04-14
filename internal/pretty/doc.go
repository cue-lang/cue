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

import "unicode/utf8"

// Doc represents a node in the Wadler-Lindig document algebra. A nil
// Doc is the empty document (produces no output).
type Doc interface {
	docNode()
}

type docBase struct{}

func (docBase) docNode() {}

// DocText emits a literal string. Text must not contain newlines.
type DocText struct {
	docBase
	Text  string
	Width int // rune count, precomputed
}

// DocLine is a soft line break. In flat-mode it emits Alt; in broken
// mode it emits a newline followed by the current indentation.
type DocLine struct {
	docBase
	Alt      string
	AltWidth int // rune count of Alt, precomputed
}

// DocHard is a hard line break that always emits a newline followed
// by the current indentation. Any Group containing a DocHard is
// forced to break.
type DocHard struct {
	docBase
}

// DocLitLine is a bare newline without indentation. Used for newlines
// inside multi-line string literals where content must be preserved
// verbatim.
type DocLitLine struct {
	docBase
}

// DocCat is the concatenation of Left followed by Right.
type DocCat struct {
	docBase
	Left  Doc
	Right Doc
}

// DocNest increases the indent level by one for Child.
type DocNest struct {
	docBase
	Child Doc
}

// DocGroup tries to render Child on a single line (flat-mode). If it
// doesn't fit within the target width, Child is rendered in
// broken-mode.
type DocGroup struct {
	docBase
	Child Doc
}

// DocIfBreak emits Broken when in broken-mode and Flat when in
// flat-mode.
type DocIfBreak struct {
	docBase
	Broken Doc
	Flat   Doc
}

// DocTable renders its rows with aligned columns. In flat-mode, rows
// are rendered inline separated by their Sep.  In broken-mode,
// columns are padded to their maximum widths.
type DocTable struct {
	docBase
	Rows []Row
}

// Row represents one row of a table.
//
// Aligned rows have Cells set - a slice of column Docs that are
// padded to align across rows. The renderer lays out columns
// left-to-right, computing the max width for each column. If a row's
// cumulative width exceeds the target line width, it is excluded from
// contributing to subsequent column widths (so a wide cell doesn't
// over-pad shorter rows).
//
// Raw rows (Raw != nil) are rendered as-is without alignment - used
// for complex fields (struct/list values) interspersed among aligned
// fields.
//
// For struct fields, Cells is [key, val] or [key, val, comment]. For
// chain arms (| or &), Cells is [expr+op] or [expr+op, comment].
type Row struct {
	Sep Doc // separator to emit before this row (flat and broken-mode)
	Raw Doc // non-nil for non-aligned rows

	// Cells holds the column contents for aligned rows.
	Cells []Doc

	// DocComment holds a doc comment that appears on its own line(s)
	// before the first cell. It does not participate in column width
	// measurement. Its presence forces the enclosing group to break.
	DocComment Doc

	// HasComment is true if the row contains any // comment in Cells
	// or embedded at positions 1-2. When true, the enclosing group is
	// forced to break because a // comment runs to end of line and
	// would swallow subsequent tokens in flat-mode.
	HasComment bool
}

// Text returns a Doc that emits the literal string s. s must not
// contain newlines.
func Text(s string) Doc {
	if s == "" {
		return nil
	}
	return &DocText{Text: s, Width: utf8.RuneCountInString(s)}
}

// Line returns a soft line break. In flat-mode it emits alt; in
// broken-mode it emits a newline followed by the current indentation.
func Line(alt string) Doc {
	return &DocLine{Alt: alt, AltWidth: utf8.RuneCountInString(alt)}
}

// Cat returns the concatenation of a and b.
func Cat(a, b Doc) Doc {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &DocCat{Left: a, Right: b}
}

// Nest returns a Doc that increases the indent level by one for d.
func Nest(d Doc) Doc {
	if d == nil {
		return nil
	}
	return &DocNest{Child: d}
}

// Group returns a Doc that tries to render d on a single line
// (flat-mode). If it doesn't fit within the target width, d is
// rendered in broken-mode.
func Group(d Doc) Doc {
	if d == nil {
		return nil
	}
	return &DocGroup{Child: d}
}

// IfBreak returns a Doc that emits broken when in broken-mode and
// flat when in flat-mode.
func IfBreak(broken, flat Doc) Doc {
	return &DocIfBreak{Broken: broken, Flat: flat}
}

// Table returns a Doc that renders its rows with aligned columns. In
// flat-mode, rows are rendered inline separated by their Sep. In
// broken-mode, columns are padded to their maximum widths.
func Table(rows []Row) Doc {
	if len(rows) == 0 {
		return nil
	}
	return &DocTable{Rows: rows}
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
