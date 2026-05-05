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
//   - Properties use "key = value" syntax.
//   - Comments are allowed and start with ; or # and span to the end of the line.
//   - Multi-word values do not require quoting; leading/trailing
//     whitespace is trimmed.
//   - Blank lines are ignored.
//   - Duplicate keys within the same section are an error.
//   - Key and section name case sensitivity is configured via [Config.CaseSensitivity].
//   - Section name nesting is configured via [Config.SectionNameNesting].
//   - Value type parsing is configured via [Config.ValueTypes].
//
// The following features found in some INI variants are out of scope:
//   - The ":" key-value separator.
//   - Any other non-standard features, such as multi-line values, line continuations
//     and Structured or array values.
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
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// CaseSensitivityStrategy controls how the decoder handles the case of keys and section names.
type CaseSensitivityStrategy int

const (
	caseSensitivityUnset CaseSensitivityStrategy = iota // default zero value; equal to [CaseSensitive] for now
	CaseSensitive
	CaseLower
)

// ValueTypesStrategy controls how the decoder interprets INI values.
type ValueTypesStrategy int

const (
	valueTypesUnset   ValueTypesStrategy = iota // default zero value; equal to [ValuesRawStrings] for now
	ValuesRawStrings                            // all values are represented as CUE strings
	ValuesCUELiterals                           // booleans and numbers are parsed into their corresponding CUE types,
	// quoted values are unquoted and always treated as strings
)

// SectionNameNestingStrategy controls how the decoder interprets dots in section names.
type SectionNameNestingStrategy int

const (
	sectionNamesUnset  SectionNameNestingStrategy = iota // default zero value; equal to [SectionNamesFlat] for now
	SectionNamesFlat                                     // dots are treated as regular characters in section names
	SectionNamesDotted                                   // dots are treated as nested section separators
)

// Config configures the behavior of the INI decoder.
type Config struct {
	// CaseSensitivity controls how keys and section names are cased.
	// By default the original case is preserved ([CaseSensitive]).
	// Set to [CaseLower] to lowercase all keys and section names.
	CaseSensitivity CaseSensitivityStrategy

	// SectionNameNesting controls how dots in section names are interpreted.
	// By default dots are treated as regular characters ([SectionNamesFlat]),
	// so all sections are flat.
	// Set to [SectionNamesDotted] to treat dots as nested section separators.
	SectionNameNesting SectionNameNestingStrategy

	// ValueTypes controls how INI values are interpreted.
	// By default all values are raw CUE strings ([ValuesRawStrings]).
	// Set to [ValuesCUELiterals] to parse booleans and numbers into
	// their corresponding CUE types.
	ValueTypes ValueTypesStrategy
}

// NewDecoder creates a decoder from a stream of INI input.
func NewDecoder(filename string, r io.Reader, cfg *Config) *Decoder {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Decoder{r: r, filename: filename, cfg: cfg}
}

// Decoder implements the decoding state for INI input.
//
// Note that INI files never decode multiple CUE nodes;
// subsequent calls to [Decoder.Decode] may return [io.EOF].
type Decoder struct {
	r           io.Reader
	filename    string
	cfg         *Config
	tokenFile   *token.File
	lineOffsets []int
}

type fieldKind int

const (
	kindProperty fieldKind = iota + 1 // zero means "no such field"
	kindSection
)

// section tracks per-section state needed to detect name collisions.
type section struct {
	// struct_ holds this section's fields.
	struct_ *ast.StructLit
	// keys records the kind of each name in struct_ for collision checks.
	keys map[string]fieldKind
	// explicit is true for [name] headers, and false for implicit parents
	// of a nested path like `a` in [a.b].
	explicit bool
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
	d.tokenFile = tokenFile
	d.lineOffsets = tokenFile.Lines()

	topSection := &section{
		struct_:  &ast.StructLit{},
		keys:     make(map[string]fieldKind),
		explicit: true,
	}
	cur := topSection

	// sections maps each section path to its struct; "" is the pre-header global section.
	sections := map[string]*section{"": topSection}

	lineNum := 0
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		lineNum++
		trimmed := strings.TrimSpace(string(line))

		// Skip blank lines and comments.
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}

		// Section header.
		if trimmed[0] == '[' {
			closeIdx := strings.IndexByte(trimmed, ']')
			if closeIdx < 0 {
				return nil, d.posErrf(lineNum, "missing closing bracket for section header")
			}
			sectionName := strings.TrimSpace(trimmed[1:closeIdx])
			if d.cfg.CaseSensitivity == CaseLower {
				sectionName = strings.ToLower(sectionName)
			}
			if sectionName == "" {
				return nil, d.posErrf(lineNum, "empty section name")
			}
			if existing := sections[sectionName]; existing != nil && existing.explicit {
				return nil, d.posErrf(lineNum, "duplicate section: %s", sectionName)
			}

			sec, err := d.buildNestedSection(sections, sectionName, lineNum)
			if err != nil {
				return nil, err
			}
			sec.explicit = true
			cur = sec
			continue
		}

		// Key-value pair.
		key, value, ok := parseKeyValue(trimmed)
		if !ok {
			return nil, d.posErrf(lineNum, "invalid line: %s", trimmed)
		}

		if d.cfg.CaseSensitivity == CaseLower {
			key = strings.ToLower(key)
		}
		switch cur.keys[key] {
		case kindSection:
			return nil, d.posErrf(lineNum, "property %s conflicts with section of the same name", key)
		case kindProperty:
			return nil, d.posErrf(lineNum, "duplicate key: %s", key)
		}
		cur.keys[key] = kindProperty

		pos := d.posForLine(lineNum)
		field, err := makeField(key, value, pos, d.cfg.ValueTypes == ValuesCUELiterals)
		if err != nil {
			return nil, d.posErrf(lineNum, "%v", err)
		}
		cur.struct_.Elts = append(cur.struct_.Elts, field)
	}
	return topSection.struct_, nil
}

// parseKeyValue splits a line into key and value using "=" as delimiter.
// It returns the trimmed key, trimmed value and whether the split succeeded.
func parseKeyValue(line string) (key, value string, ok bool) {
	// Find the first "=".
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)

	// Strip inline comments (only if preceded by whitespace).
	// For quoted values, only comments after the closing quote are stripped.
	start := 0
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
		if closeIdx := strings.IndexByte(value[1:], value[0]); closeIdx >= 0 {
			start = closeIdx + 2
		}
	}
	if i := strings.IndexAny(value[start:], ";#"); i >= 0 {
		i += start
		if i > 0 && (value[i-1] == ' ' || value[i-1] == '\t') {
			value = strings.TrimRight(value[:i-1], " \t")
		}
	}

	return key, value, true
}

// buildNestedSection walks the section path, creating and registering missing
// sections along the way, and returns the innermost one. Dots in the section
// name are treated as nesting separators only when [Config.SectionNameNesting]
// opts in via [SectionNamesDotted]; otherwise the whole name is a single
// segment. Existing sections are reused; an error is returned if any segment
// collides with a property in its parent.
func (d *Decoder) buildNestedSection(sections map[string]*section, sectionName string, lineNum int) (*section, error) {
	parts := []string{sectionName}
	if d.cfg.SectionNameNesting == SectionNamesDotted {
		// Explicitly opt-in to splitting section names by dots.
		parts = strings.Split(sectionName, ".")
	}
	parent := sections[""]
	var path string
	for _, part := range parts {
		if path == "" {
			path = part
		} else {
			path = path + "." + part
		}
		if existing := sections[path]; existing != nil {
			parent = existing
			continue
		}
		if parent.keys[part] == kindProperty {
			return nil, d.posErrf(lineNum, "section %s conflicts with property of the same name", part)
		}
		pos := d.posForLine(lineNum)
		inner := &ast.StructLit{}
		field := &ast.Field{
			Label:    makeLabel(part, pos),
			Value:    inner,
			TokenPos: pos,
		}
		parent.struct_.Elts = append(parent.struct_.Elts, field)
		parent.keys[part] = kindSection
		sec := &section{struct_: inner, keys: make(map[string]fieldKind)}
		sections[path] = sec
		parent = sec
	}
	return parent, nil
}

// makeField creates a CUE field with an appropriate value literal.
// When typedValues is true, values are parsed as booleans or numbers when possible.
func makeField(key, value string, pos token.Pos, typedValues bool) (*ast.Field, error) {
	var v ast.Expr
	if typedValues {
		var err error
		v, err = makeValueLit(value, pos)
		if err != nil {
			return nil, err
		}
	} else {
		v = newStringLit(value, pos)
	}
	return &ast.Field{
		Label:    makeLabel(key, pos),
		Value:    v,
		TokenPos: pos,
	}, nil
}

// makeValueLit returns a bool, number, or string literal depending on the value.
// If the value is quoted, it is unquoted and always treated as a string.
// Otherwise, it is parsed as a bool, number, or string.
//
// For example:
//   - port=443 -> port is parsed as an int
//   - portString="443" -> portString stays as a string "443"
func makeValueLit(s string, pos token.Pos) (ast.Expr, error) {
	// Quoted values are always strings; strip quotes via CUE unquoting.
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		unquoted, err := literal.Unquote(s)
		if err != nil {
			return nil, fmt.Errorf("invalid quoted value: %s", s)
		}
		return newStringLit(unquoted, pos), nil
	}
	switch s := strings.ToLower(s); s {
	case "true", "false":
		b := ast.NewBool(s == "true")
		ast.SetPos(b, pos)
		return b, nil
	}
	var num literal.NumInfo
	if err := literal.ParseNum(s, &num); err == nil {
		kind := token.FLOAT
		if num.IsInt() {
			kind = token.INT
		}
		lit := &ast.BasicLit{Kind: kind, Value: s}
		ast.SetPos(lit, pos)
		return lit, nil
	}
	return newStringLit(s, pos), nil
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

// posForLine returns a token.Pos for the start of the given 1-based line number.
func (d *Decoder) posForLine(lineNum int) token.Pos {
	if lineNum < 1 || lineNum > len(d.lineOffsets) {
		return token.NoPos
	}
	return d.tokenFile.Pos(d.lineOffsets[lineNum-1], token.NoRelPos)
}

// posErrf returns a position-aware error for the given 1-based line number.
func (d *Decoder) posErrf(lineNum int, format string, args ...any) error {
	return errors.Newf(d.posForLine(lineNum), format, args...)
}
