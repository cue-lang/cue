// Copyright 2026 The CUE Authors
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

// Package ini converts INI to CUE.
//
// INI files are a simple configuration format consisting of sections,
// properties (key-value pairs), and comments. Since there is no single
// standard for INI files, this package supports a common subset:
//
//   - Sections are declared with [name] headers.
//   - Nested sections use dot-separated names like [parent.child].
//   - Properties use "key = value" syntax; ":" is not supported as a key-value separator.
//   - Comments are allowed and start with ; or # and span to the end of the line.
//   - Multi-word values do not require quoting; leading/trailing
//     whitespace is trimmed.
//   - Quoted values (single or double quotes) have their quotes stripped.
//   - Boolean values (true/false, case insensitive) are represented as CUE bools.
//   - Numeric values (integers and floats) are represented as CUE numbers.
//   - All other values are represented as CUE strings.
//   - Blank lines are ignored.
//   - Keys and section names are case insensitive; they are lowercased in the output.
//   - Duplicate keys within the same section are an error.
//
// Properties defined before any section header are placed at the
// top level of the resulting CUE struct. Section names become nested
// CUE struct fields.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package ini

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// NewDecoder creates a decoder from a stream of INI input.
func NewDecoder(filename string, r io.Reader) *Decoder {
	return &Decoder{r: r, filename: filename}
}

// Decoder implements the decoding state for INI input.
//
// Note that INI files never decode multiple CUE nodes;
// subsequent calls to [Decoder.Decode] may return [io.EOF].
type Decoder struct {
	r        io.Reader
	filename string
}

// section pairs an AST struct with the set of keys already defined in it,
// so that duplicate-key detection lives alongside the data it protects.
type section struct {
	struct_ *ast.StructLit
	keys    map[string]bool
}

// Decode parses the input stream as INI and converts it to a CUE [ast.Expr].
// Because INI files only contain a single top-level expression,
// subsequent calls to this method may return [io.EOF].
func (d *Decoder) Decode() (ast.Expr, error) {
	if d.r == nil {
		return nil, io.EOF
	}
	data, err := io.ReadAll(d.r)
	d.r = nil
	if err != nil {
		return nil, err
	}

	tokenFile := token.NewFile(d.filename, 0, len(data))
	tokenFile.SetLinesForContent(data)
	lineOffsets := tokenFile.Lines()

	topLevel := &ast.StructLit{}

	// sections maps each section path to its struct and key set.
	// The empty string key is the global (pre-header) section.
	sections := map[string]*section{
		"": {struct_: topLevel, keys: make(map[string]bool)},
	}
	cur := sections[""]

	lines := bytes.Split(data, []byte("\n"))
	for lineNum, line := range lines {
		lineNum++ // convert 0-based to 1-based
		trimmed := strings.TrimSpace(string(line))

		// Skip blank lines and comments.
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}

		// Section header.
		if trimmed[0] == '[' {
			closeIdx := strings.IndexByte(trimmed, ']')
			if closeIdx < 0 {
				pos := posForLine(tokenFile, lineOffsets, lineNum)
				return nil, fmt.Errorf("%s: missing closing bracket for section header", tokenFile.Position(pos))
			}
			sectionName := strings.ToLower(strings.TrimSpace(trimmed[1:closeIdx]))
			if sectionName == "" {
				pos := posForLine(tokenFile, lineOffsets, lineNum)
				return nil, fmt.Errorf("%s: empty section name", tokenFile.Position(pos))
			}
			if sections[sectionName] != nil {
				pos := posForLine(tokenFile, lineOffsets, lineNum)
				return nil, fmt.Errorf("%s: duplicate section: %s", tokenFile.Position(pos), sectionName)
			}

			// Build nested structs for dot-separated section names.
			parts := strings.Split(sectionName, ".")
			cur = &section{
				struct_: buildNestedSection(topLevel, parts, tokenFile, lineOffsets, lineNum),
				keys:    make(map[string]bool),
			}
			sections[sectionName] = cur
			continue
		}

		// Key-value pair.
		key, value, ok := parseKeyValue(trimmed)
		if !ok {
			pos := posForLine(tokenFile, lineOffsets, lineNum)
			return nil, fmt.Errorf("%s: invalid line: %s", tokenFile.Position(pos), trimmed)
		}

		key = strings.ToLower(key)
		if cur.keys[key] {
			pos := posForLine(tokenFile, lineOffsets, lineNum)
			return nil, fmt.Errorf("%s: duplicate key: %s", tokenFile.Position(pos), key)
		}
		cur.keys[key] = true

		pos := posForLine(tokenFile, lineOffsets, lineNum)
		field := makeField(key, value, pos)
		cur.struct_.Elts = append(cur.struct_.Elts, field)
	}
	return topLevel, nil
}

// parseKeyValue splits a line into key and value using "=" as delimiter.
// It returns the trimmed key, trimmed value and whether the split succeeded.
// Quoted values have their surrounding quotes stripped.
func parseKeyValue(line string) (key, value string, ok bool) {
	// Find the first "=".
	sepIdx := strings.IndexByte(line, '=')
	if sepIdx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:sepIdx])
	if key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(line[sepIdx+1:])

	// Strip inline comments (only if preceded by whitespace).
	value = stripInlineComment(value)

	// Strip surrounding quotes if present.
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}

// stripInlineComment removes an inline comment from a value string.
// Inline comments start with ; or # only when preceded by whitespace.
func stripInlineComment(value string) string {
	// For quoted strings, find the closing quote and strip comments after it.
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
		closeIdx := strings.IndexByte(value[1:], value[0])
		if closeIdx >= 0 {
			afterClose := closeIdx + 2
			rest := value[afterClose:]
			for _, marker := range []string{" ;", " #", "\t;", "\t#"} {
				if strings.Contains(rest, marker) {
					return value[:afterClose]
				}
			}
		}
		return value
	}
	for _, marker := range []string{" ;", " #", "\t;", "\t#"} {
		if idx := strings.Index(value, marker); idx >= 0 {
			value = strings.TrimRight(value[:idx], " \t")
		}
	}
	return value
}

// buildNestedSection ensures that a chain of nested struct fields exists
// for the given section path parts, and returns the innermost struct.
// For example, for section [database.pool], then the parts will be ["database", "pool"]
// It ensures:
//
//	database: { pool: { ... } }
//
// If existing fields already exist (from earlier sections or dotted keys), they are reused.
func buildNestedSection(root *ast.StructLit, parts []string, tokenFile *token.File, lineOffsets []int, lineNum int) *ast.StructLit {
	current := root
	for _, part := range parts {
		found := false
		for _, elt := range current.Elts {
			field, ok := elt.(*ast.Field)
			if !ok {
				continue
			}
			if strings.EqualFold(fieldName(field), part) {
				if inner, ok := field.Value.(*ast.StructLit); ok {
					current = inner
					found = true
					break
				}
			}
		}
		if !found {
			pos := posForLine(tokenFile, lineOffsets, lineNum)
			inner := &ast.StructLit{}
			field := &ast.Field{
				Label:    makeLabel(part, pos),
				Value:    inner,
				TokenPos: pos,
			}
			current.Elts = append(current.Elts, field)
			current = inner
		}
	}
	return current
}

// makeField creates a CUE field with an appropriate value literal.
// Numeric values become CUE number literals; all others become strings.
func makeField(key, value string, pos token.Pos) *ast.Field {
	return &ast.Field{
		Label:    makeLabel(key, pos),
		Value:    makeValueLit(value, pos),
		TokenPos: pos,
	}
}

// makeValueLit returns a bool, number, or string literal depending on the value.
func makeValueLit(s string, pos token.Pos) ast.Expr {
	switch {
	case strings.EqualFold(s, "true"):
		b := ast.NewBool(true)
		ast.SetPos(b, pos)
		return b
	case strings.EqualFold(s, "false"):
		b := ast.NewBool(false)
		ast.SetPos(b, pos)
		return b
	default:
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			lit := &ast.BasicLit{Kind: token.INT, Value: s}
			ast.SetPos(lit, pos)
			return lit
		}
		if strings.Contains(s, ".") {
			if _, err := strconv.ParseFloat(s, 64); err == nil {
				lit := &ast.BasicLit{Kind: token.FLOAT, Value: s}
				ast.SetPos(lit, pos)
				return lit
			}
		}
		return newStringLit(s, pos)
	}
}

// makeLabel creates an appropriate CUE label for the given key.
func makeLabel(key string, pos token.Pos) ast.Label {
	label := ast.NewStringLabel(key)
	ast.SetPos(label, pos)
	return label
}

// newStringLit creates a new CUE string literal from a Go string value.
func newStringLit(s string, pos token.Pos) *ast.BasicLit {
	lit := ast.NewString(s)
	ast.SetPos(lit, pos)
	return lit
}

// fieldName extracts the name string from a CUE field label.
func fieldName(f *ast.Field) string {
	switch label := f.Label.(type) {
	case *ast.Ident:
		return label.Name
	case *ast.BasicLit:
		// String labels are quoted; strip the quotes.
		s := label.Value
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			s = s[1 : len(s)-1]
		}
		return s
	}
	return ""
}

// posForLine returns a token.Pos for the start of the given 1-based line number
// using the precomputed line offset table.
func posForLine(f *token.File, lineOffsets []int, lineNum int) token.Pos {
	if lineNum < 1 || lineNum > len(lineOffsets) {
		return token.NoPos
	}
	return f.Pos(lineOffsets[lineNum-1], token.NoRelPos)
}
