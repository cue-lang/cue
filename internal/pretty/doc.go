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

// Package pretty implements a Wadler-Lindig pretty printer for CUE AST nodes.
package pretty

// tag discriminates the variants of a Doc node.
type tag uint8

const (
	tagText    tag = iota + 1 // literal string (must not contain newlines)
	tagLine                   // flat: emit .text; broken: newline + indent
	tagHard                   // always newline + indent; forces enclosing group to break
	tagLitLine                // always bare newline (no indent); for multi-line string content
	tagCat                    // concatenation of .left and .right
	tagNest                   // increase indent by .n for .right
	tagGroup                  // try to fit .right on one line
	tagIfBreak                // broken: .left; flat: .right
	tagTable                  // aligned rows
)

// Doc represents a node in the Wadler-Lindig document algebra.
// A nil *Doc is the empty document (produces no output).
type Doc struct {
	tag   tag
	text  string // tagText: content; tagLine: flat-mode alternative
	n     int    // tagNest: indent increment
	left  *Doc   // tagCat: left child; tagIfBreak: broken variant
	right *Doc   // tagCat: right child; tagNest/tagGroup: child; tagIfBreak: flat variant
	rows  []Row  // tagTable: aligned rows
}

// Row represents one row of a table. Aligned rows have Key and Val set;
// the key column is padded to the maximum width across all aligned rows.
// Raw rows (Raw != nil) are rendered as-is without alignment — used for
// complex fields (struct/list values) interspersed among aligned fields.
type Row struct {
	Sep        *Doc // separator to emit before this row in broken mode
	DocComment *Doc // doc comment to emit before the key (does not affect key width)
	Key        *Doc // nil for raw rows
	Val        *Doc // nil for raw rows
	Raw        *Doc // non-nil for non-aligned rows
	Comment    bool // true if Val contains a // comment (forces group to break)
}

// Text returns a Doc that emits the literal string s.
// s must not contain newlines.
func Text(s string) *Doc {
	if s == "" {
		return nil
	}
	return &Doc{tag: tagText, text: s}
}

// Line returns a soft line break. In flat mode it emits alt;
// in broken mode it emits a newline followed by the current indentation.
func Line(alt string) *Doc {
	return &Doc{tag: tagLine, text: alt}
}

// HardLine returns a hard line break that always emits a newline.
// Any Group containing a HardLine is forced to break.
func HardLine() *Doc {
	return hardLineSingleton
}

// LitLine returns a bare newline without indentation. This is used
// for newlines inside multi-line string literals where the content
// must be preserved verbatim.
func LitLine() *Doc {
	return litLineSingleton
}

// Cat returns the concatenation of a and b.
func Cat(a, b *Doc) *Doc {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &Doc{tag: tagCat, left: a, right: b}
}

// Nest returns a Doc that increases the indent level by n for d.
func Nest(n int, d *Doc) *Doc {
	if d == nil {
		return nil
	}
	return &Doc{tag: tagNest, n: n, right: d}
}

// Group returns a Doc that tries to render d on a single line (flat mode).
// If it doesn't fit within the target width, d is rendered in broken mode.
func Group(d *Doc) *Doc {
	if d == nil {
		return nil
	}
	return &Doc{tag: tagGroup, right: d}
}

// IfBreak returns a Doc that emits broken when in broken mode
// and flat when in flat mode.
func IfBreak(broken, flat *Doc) *Doc {
	return &Doc{tag: tagIfBreak, left: broken, right: flat}
}

// Table returns a Doc that renders its rows with aligned columns.
// In flat mode, rows are rendered inline separated by commas.
// In broken mode, the key column is padded to the maximum key width.
func Table(rows []Row) *Doc {
	if len(rows) == 0 {
		return nil
	}
	return &Doc{tag: tagTable, rows: rows}
}

// --- Derived combinators ---

// Cats concatenates all non-nil docs left to right.
func Cats(docs ...*Doc) *Doc {
	var result *Doc
	for _, d := range docs {
		result = Cat(result, d)
	}
	return result
}

// Sep intersperses sep between non-nil docs.
func Sep(sep *Doc, docs ...*Doc) *Doc {
	var result *Doc
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

var (
	hardLineSingleton      = &Doc{tag: tagHard}
	litLineSingleton       = &Doc{tag: tagLitLine}
	softLineSingleton      = Line(" ")
	softCommaSingleton     = Line(", ")
	blankLineSingleton     = Cat(HardLine(), HardLine())
	trailingCommaSingleton = IfBreak(Text(","), nil)
	// noSep is a zero-width Doc used as an explicit "no separator" marker,
	// distinguishable from nil (which means "use default separator").
	noSep = &Doc{tag: tagText, text: ""}
)

// NoSep returns an explicit zero-width separator indicating
// that no whitespace should be emitted between table rows.
func NoSep() *Doc { return noSep }

// SoftLine is a Line that emits a space when flat.
func SoftLine() *Doc { return softLineSingleton }

// SoftComma is a Line that emits ", " when flat.
func SoftComma() *Doc { return softCommaSingleton }

// BlankLine emits two consecutive hard newlines (a blank line separator).
func BlankLine() *Doc { return blankLineSingleton }

// TrailingComma emits a comma only in broken mode.
func TrailingComma() *Doc { return trailingCommaSingleton }
