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
// standard for INI files, the zero [Config] accepts the subset every
// flavor shares:
//
//   - Sections are declared with [name] headers.
//   - Properties use "key = value" syntax.
//   - Lines whose first non-blank character is ; or # are comments.
//   - Multi-word values do not require quoting; leading and trailing
//     whitespace is trimmed.
//   - Blank lines are ignored.
//   - Duplicate keys within the same section are an error.
//
// Flavor differences are selected through the fields of [Config].
//
// Properties defined before any section header are placed at the
// top level of the resulting CUE struct. Section names become nested
// CUE struct fields.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package ini

import (
	"fmt"
	"io"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// QuoteMode controls how the decoder treats quotation marks around a value.
type QuoteMode int

const (
	quotesUnset    QuoteMode = iota // default zero value; equal to [QuotesLiteral] for now
	QuotesLiteral                   // quotation marks are ordinary characters
	QuotesStripped                  // one matching pair of surrounding quotes is removed
	QuotesEscaped                   // as [QuotesStripped], and backslash escapes are interpreted
)

// CaseMode controls how the decoder treats the case of keys and section names.
type CaseMode int

const (
	caseUnset     CaseMode = iota // default zero value; equal to [CasePreserve] for now
	CasePreserve                  // keys and section names keep their source case
	CaseLowerKeys                 // keys are lowercased, section names are not
	CaseLower                     // keys and section names are lowercased
)

// DuplicateMode controls how the decoder treats a key repeated within a section.
type DuplicateMode int

const (
	duplicatesUnset DuplicateMode = iota // default zero value; equal to [DuplicatesError] for now
	DuplicatesError                      // a repeated key is an error
	DuplicatesList                       // a repeated key becomes a list in source order
	DuplicatesFirst                      // later values are ignored
	DuplicatesLast                       // later values replace earlier ones
)

// ContinuationMode controls how the decoder joins a value spanning several lines.
type ContinuationMode int

const (
	continuationsUnset     ContinuationMode = iota // default zero value; equal to [ContinuationsNone] for now
	ContinuationsNone                              // every value ends at its line
	ContinuationsBackslash                         // a trailing backslash continues the value on the next line
	ContinuationsIndented                          // an indented line continues the value of the preceding property
)

// BareKeyMode controls how the decoder treats a line holding a key and no delimiter.
type BareKeyMode int

const (
	bareKeysUnset BareKeyMode = iota // default zero value; equal to [BareKeysError] for now
	BareKeysError                    // a line with no delimiter is an error
	BareKeysNull                     // a bare key decodes to null
	BareKeysTrue                     // a bare key decodes to true
)

// ValueMode controls whether the decoder interprets the type of a value.
type ValueMode int

const (
	valuesUnset   ValueMode = iota // default zero value; equal to [ValuesStrings] for now
	ValuesStrings                  // every value is a string
	ValuesTyped                    // unquoted booleans and numbers become CUE booleans and numbers
)

// BooleanMode controls which words [ValuesTyped] recognizes as booleans.
type BooleanMode int

const (
	booleansUnset     BooleanMode = iota // default zero value; equal to [BooleansTrueFalse] for now
	BooleansTrueFalse                    // only true and false
	BooleansExtended                     // also yes, no, on, off, 1, and 0
)

// Config describes an INI flavor. The zero value is the common subset
// described in the package documentation; each field selects one flavor
// difference, independently of all others.
//
// Config is passed and returned by value, so a preset such as [GitConfig]
// combined with an explicit option is an ordinary assignment.
type Config struct {
	// Delimiters holds the bytes accepted between a key and its value;
	// the first occurrence of any of them splits the line.
	// The empty string means "=".
	//
	// Not implemented yet; "=" is always the delimiter.
	Delimiters string

	// InlineComments reports whether a ";" or "#" preceded by a space or
	// tab, outside a quoted value, starts a comment. It is off by default,
	// so that no value is silently truncated.
	//
	// Not implemented yet; inline comments are always stripped.
	InlineComments bool

	// Quotes controls how quotation marks around a value are treated.
	// By default they are ordinary characters ([QuotesLiteral]).
	//
	// Not implemented yet.
	Quotes QuoteMode

	// Case controls the case of keys and section names.
	// By default the source case is preserved ([CasePreserve]).
	Case CaseMode

	// DottedSections reports whether dots in a section name separate
	// nested sections. By default they are ordinary characters.
	DottedSections bool

	// QuotedSubsections reports whether a section name of the form
	// `a "b"` nests b one level below a. By default it does not.
	//
	// Not implemented yet.
	QuotedSubsections bool

	// DuplicateKeys controls what happens when a key recurs within a
	// section. By default the second occurrence is an error
	// ([DuplicatesError]).
	//
	// Not implemented yet; the second occurrence is always an error.
	DuplicateKeys DuplicateMode

	// Continuations controls how a value may span several lines.
	// By default it may not ([ContinuationsNone]).
	//
	// Not implemented yet.
	Continuations ContinuationMode

	// BareKeys controls what a line holding a key and no delimiter means.
	// By default it is an error ([BareKeysError]).
	//
	// Not implemented yet.
	BareKeys BareKeyMode

	// Values controls whether the type of a value is interpreted.
	// By default every value is a string ([ValuesStrings]).
	Values ValueMode

	// Booleans selects the vocabulary [ValuesTyped] recognizes as
	// booleans. By default only true and false ([BooleansTrueFalse]).
	//
	// Not implemented yet.
	Booleans BooleanMode
}

// NewDecoder creates a decoder for the INI flavor cfg describes.
// The decoder keeps its own copy of cfg; later changes to cfg have no effect.
func NewDecoder(filename string, r io.Reader, cfg Config) *Decoder {
	return &Decoder{r: r, filename: filename, cfg: cfg}
}

// Decoder implements the decoding state for INI input.
//
// Note that INI files never decode multiple CUE nodes;
// subsequent calls to [Decoder.Decode] may return [io.EOF].
type Decoder struct {
	r         io.Reader
	filename  string
	cfg       Config
	tokenFile *token.File
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

	topSection := &section{
		struct_:  &ast.StructLit{},
		keys:     make(map[string]fieldKind),
		explicit: true,
	}
	cur := topSection

	// sections maps each section path to its struct; "" is the pre-header global section.
	sections := map[string]*section{"": topSection}

	offset := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		pos := tokenFile.Pos(offset, token.NoRelPos)
		offset += len(line) + 1
		trimmed := strings.TrimSpace(line)

		// Skip blank lines and comments.
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}

		// Section header.
		if trimmed[0] == '[' {
			closeIdx := strings.IndexByte(trimmed, ']')
			if closeIdx < 0 {
				return nil, errors.Newf(pos, "missing closing bracket for section header")
			}
			sectionName := strings.TrimSpace(trimmed[1:closeIdx])
			if d.cfg.Case == CaseLower {
				sectionName = strings.ToLower(sectionName)
			}
			if sectionName == "" {
				return nil, errors.Newf(pos, "empty section name")
			}
			if existing := sections[sectionName]; existing != nil && existing.explicit {
				return nil, errors.Newf(pos, "duplicate section: %s", sectionName)
			}

			sec, err := d.buildNestedSection(sections, sectionName, pos)
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
			return nil, errors.Newf(pos, "invalid line: %s", trimmed)
		}

		if d.cfg.Case == CaseLowerKeys || d.cfg.Case == CaseLower {
			key = strings.ToLower(key)
		}
		switch cur.keys[key] {
		case kindSection:
			return nil, errors.Newf(pos, "property %s conflicts with section of the same name", key)
		case kindProperty:
			return nil, errors.Newf(pos, "duplicate key: %s", key)
		}
		cur.keys[key] = kindProperty

		field, err := makeField(key, value, pos, d.cfg.Values == ValuesTyped)
		if err != nil {
			return nil, errors.Newf(pos, "%v", err)
		}
		cur.struct_.Elts = append(cur.struct_.Elts, field)
	}
	return topSection.struct_, nil
}

// parseKeyValue splits a line into key and value using "=" as delimiter.
// It returns the trimmed key, trimmed value and whether the split succeeded.
func parseKeyValue(line string) (key, value string, ok bool) {
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
// name are treated as nesting separators only when [Config.DottedSections]
// opts in; otherwise the whole name is a single segment. Existing sections are
// reused; an error is returned if any segment collides with a property in its
// parent.
func (d *Decoder) buildNestedSection(sections map[string]*section, sectionName string, pos token.Pos) (*section, error) {
	parts := []string{sectionName}
	if d.cfg.DottedSections {
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
			return nil, errors.Newf(pos, "section %s conflicts with property of the same name", part)
		}
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
