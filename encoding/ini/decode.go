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
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

var (
	intPattern   = regexp.MustCompile(`^-?(?:0|[1-9]\d*)$`)
	floatPattern = regexp.MustCompile(`^-?(?:0|[1-9]\d*)\.\d+$`)
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
	decoded  bool
}

// Decode parses the input stream as INI and converts it to a CUE [ast.Expr].
// Because INI files only contain a single top-level expression,
// subsequent calls to this method may return [io.EOF].
func (d *Decoder) Decode() (ast.Expr, error) {
	if d.decoded {
		return nil, io.EOF
	}
	d.decoded = true

	data, err := io.ReadAll(d.r)
	if err != nil {
		return nil, err
	}

	tokenFile := token.NewFile(d.filename, 0, len(data))
	tokenFile.SetLinesForContent(data)

	topLevel := &ast.StructLit{}

	// sectionStruct is the struct we're currently inserting fields into.
	// It starts as topLevel for global properties (before any [section] header).
	sectionStruct := topLevel

	// seenKeys tracks duplicate key detection per section.
	// The outer key is the section path (empty string for global), inner key is the property key.
	seenKeys := make(map[string]map[string]bool)
	seenKeys[""] = make(map[string]bool)

	// currentSectionPath is the dot-separated path of the current section,
	// used for duplicate key detection.
	currentSectionPath := ""

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip blank lines and comments.
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}

		// Section header.
		if trimmed[0] == '[' {
			closeIdx := strings.IndexByte(trimmed, ']')
			if closeIdx < 0 {
				pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
				return nil, fmt.Errorf("%s: missing closing bracket for section header", pos)
			}
			sectionName := strings.ToLower(strings.TrimSpace(trimmed[1:closeIdx]))
			if sectionName == "" {
				pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
				return nil, fmt.Errorf("%s: empty section name", pos)
			}
			currentSectionPath = sectionName
			if seenKeys[currentSectionPath] != nil {
				pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
				return nil, fmt.Errorf("%s: duplicate section: %s", pos, sectionName)
			}
			seenKeys[currentSectionPath] = make(map[string]bool)

			// Build nested structs for dot-separated section names.
			parts := strings.Split(sectionName, ".")
			sectionStruct = buildNestedSection(topLevel, parts, tokenFile, data, lineNum)
			continue
		}

		// Key-value pair.
		key, value, ok := parseKeyValue(trimmed)
		if !ok {
			pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
			return nil, fmt.Errorf("%s: invalid line: %s", pos, trimmed)
		}

		key = strings.ToLower(key)
		if seenKeys[currentSectionPath][key] {
			pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
			return nil, fmt.Errorf("%s: duplicate key: %s", pos, key)
		}
		seenKeys[currentSectionPath][key] = true

		pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
		field := makeField(key, value, pos)
		sectionStruct.Elts = append(sectionStruct.Elts, field)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
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
func buildNestedSection(root *ast.StructLit, parts []string, tokenFile *token.File, data []byte, lineNum int) *ast.StructLit {
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
			pos := tokenFile.Pos(offsetForLine(data, lineNum), token.NoRelPos)
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
	case intPattern.MatchString(s):
		lit := &ast.BasicLit{Kind: token.INT, Value: s}
		ast.SetPos(lit, pos)
		return lit
	case floatPattern.MatchString(s):
		lit := &ast.BasicLit{Kind: token.FLOAT, Value: s}
		ast.SetPos(lit, pos)
		return lit
	default:
		return newStringLit(s, pos)
	}
}

// makeLabel creates an appropriate CUE label for the given key.
// If the key is a valid CUE identifier, it uses an Ident; otherwise a string label.
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

// offsetForLine returns the byte offset at the start of the given 1-based line number.
func offsetForLine(data []byte, lineNum int) int {
	line := 1
	for i, b := range data {
		if line == lineNum {
			return i
		}
		if b == '\n' {
			line++
		}
	}
	return len(data)
}
