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

// Package pretty implements a Wadler-Lindig pretty-printer for CUE values.
//
// The core document algebra follows "A Prettier Printer" (Wadler, 2003) with
// the efficient layout algorithm from Lindig (2000). A CUE value is converted
// to an intermediate Doc representation, which is then rendered to a string
// given a target line width.
package pretty

// Doc is a pretty-printing document in the Wadler-Lindig algebra.
//
// The zero value is the empty document (Nil).
type Doc struct {
	kind      docKind
	text      string     // for Text
	indent    int        // for Nest
	left      *Doc       // for Concat, Union
	right     *Doc       // for Concat, Union
	inner     *Doc       // for Nest, Group
	tableRows []TableRow // for Table
}

// TableRow is a single row in a Table.
type TableRow struct {
	Cells   []*Doc
	NoAlign bool // when true, skip label-column padding for this row
}

type docKind int

const (
	docNil      docKind = iota // empty document
	docText                    // literal text
	docLine                    // newline or space (when flattened)
	docSoftLine                // newline or nothing (when flattened)
	docNest                    // increase indentation
	docConcat                  // horizontal composition
	docGroup                   // choose flat or broken layout
	docUnion                   // internal: flat | broken alternative
	docTable                   // tabular layout with column alignment
)

// Shared singletons for common documents.
var (
	docNilSingleton      = &Doc{kind: docNil}
	docLineSingleton     = &Doc{kind: docLine}
	docSoftLineSingleton = &Doc{kind: docSoftLine}
	docSpace             = &Doc{kind: docText, text: " "}
	docComma             = &Doc{kind: docText, text: ","}
	docEmpty             = &Doc{kind: docText, text: ""}
)

// Nil returns the empty document.
func Nil() *Doc { return docNilSingleton }

// Text returns a document containing literal text (must not contain newlines).
func Text(s string) *Doc {
	switch s {
	case "":
		return docEmpty
	case " ":
		return docSpace
	case ",":
		return docComma
	}
	return &Doc{kind: docText, text: s}
}

// Line returns a document that renders as a newline, or as a single space
// when flattened inside a group that fits on one line.
func Line() *Doc {
	return docLineSingleton
}

// Nest returns a document whose lines are indented by i spaces relative to
// the current indentation.
func Nest(i int, d *Doc) *Doc {
	return &Doc{kind: docNest, indent: i, inner: d}
}

// Concat joins two documents horizontally.
func Concat(a, b *Doc) *Doc {
	return &Doc{kind: docConcat, left: a, right: b}
}

// Group introduces a choice: if the flattened form fits within the page
// width, use it; otherwise break at Line boundaries.
func Group(d *Doc) *Doc {
	return &Doc{kind: docGroup, inner: d}
}

// --- Convenience combinators ---

// Spread joins a slice of documents with a single space between each.
func Spread(docs ...*Doc) *Doc {
	return join(Text(" "), docs)
}

// Stack joins a slice of documents with Line between each.
func Stack(docs ...*Doc) *Doc {
	return join(Line(), docs)
}

// SoftStack joins documents with Line (which collapses to a space in flat mode).
// Wrap in Group to get the flat-or-break choice.
func SoftStack(docs ...*Doc) *Doc {
	return join(Line(), docs)
}

func join(sep *Doc, docs []*Doc) *Doc {
	if len(docs) == 0 {
		return Nil()
	}
	result := docs[0]
	for _, d := range docs[1:] {
		result = Concat(Concat(result, sep), d)
	}
	return result
}

// Bracket wraps an inner document with open/close delimiters,
// nesting the body and inserting line breaks.
//
//	Group( open <line> Nest(indent, body) <line> close )
func Bracket(open string, indent int, body *Doc, close string) *Doc {
	return Group(
		Concat(Text(open),
			Concat(Nest(indent, Concat(Line(), body)),
				Concat(Line(), Text(close)))))
}

// TightBracket is like Bracket but uses SoftLine (empty when flat, newline
// when broken) so that flat rendering has no space after the opening
// delimiter: f(a, b) instead of f( a, b ).
func TightBracket(open string, indent int, body *Doc, close string) *Doc {
	return Group(
		Concat(Text(open),
			Concat(Nest(indent, Concat(SoftLine(), body)),
				Concat(SoftLine(), Text(close)))))
}

// SoftLine renders as nothing when flattened, or as a newline when broken.
func SoftLine() *Doc {
	return docSoftLineSingleton
}

// Table creates a tabular document from a set of rows.
// In break mode, cells are padded so that columns align across rows
// (unless a row has NoAlign set, in which case it skips padding).
// In flat mode (when the whole table fits on one line), rows are joined
// with ", " and cells within each row are concatenated without extra padding.
//
// The table behaves like a Group: it tries flat first, falls back to aligned.
func Table(rows []TableRow) *Doc {
	return &Doc{kind: docTable, tableRows: rows}
}

// Row is a convenience for building a table row.
func Row(cells ...*Doc) TableRow {
	return TableRow{Cells: cells}
}

// NoAlignRow builds a table row that skips label-column padding.
// Use this for rows whose value starts with a bracket (struct or list)
// where alignment padding before the opening delimiter is unwanted.
func NoAlignRow(cells ...*Doc) TableRow {
	return TableRow{Cells: cells, NoAlign: true}
}
