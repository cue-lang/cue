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
//   - Sections are declared with [name] headers; a repeated header
//     reopens the section it names.
//   - Properties use "key = value" syntax.
//   - Lines whose first non-blank character is ; or # are comments;
//     comments do not start within a value.
//   - Values are the source text after the delimiter with leading and
//     trailing whitespace trimmed and nothing else changed, so quotes,
//     backslashes, ";" and "#" are all literal.
//   - Blank lines are ignored, and a leading byte order mark is removed.
//   - Duplicate keys within the same section are an error, as are an
//     empty section name, text after "]", a line with no delimiter, and
//     an empty key.
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
	"bytes"
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
	QuotesStripped                  // a value enclosed in matching quotes loses them
	QuotesEscaped                   // double quotes around any part of a value are removed, and backslash escapes are interpreted
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
	InlineComments bool

	// Quotes controls how quotation marks around a value are treated.
	// By default they are ordinary characters ([QuotesLiteral]).
	Quotes QuoteMode

	// Case controls the case of keys and section names.
	// By default the source case is preserved ([CasePreserve]).
	Case CaseMode

	// DottedSections reports whether dots in a section name separate
	// nested sections. By default they are ordinary characters.
	DottedSections bool

	// QuotedSubsections reports whether a section name of the form
	// `a "b"` nests b one level below a, a backslash within the quotes
	// escaping the character after it. By default it does not.
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
// It outlives any one [name] header, since a repeated header reopens the
// section rather than starting a new one.
type section struct {
	// fields holds this section's fields.
	fields *ast.StructLit
	// keys records the kind of each name in fields for collision checks.
	keys map[string]fieldKind
	// children holds the sections nested directly in this one, so that a
	// section path is followed one segment at a time.
	children map[string]*section
}

// newSection starts an empty section holding the given struct.
func newSection(fields *ast.StructLit) *section {
	return &section{
		fields:   fields,
		keys:     make(map[string]fieldKind),
		children: make(map[string]*section),
	}
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

	// A byte order mark is not part of the first key.
	data = bytes.TrimPrefix(data, []byte("\uFEFF"))

	tokenFile := token.NewFile(d.filename, 0, len(data))
	tokenFile.SetLinesForContent(data)
	d.tokenFile = tokenFile

	topSection := newSection(&ast.StructLit{})
	cur := topSection

	offset := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		lineOffset := offset
		offset += len(line) + 1
		pos := tokenFile.Pos(lineOffset, token.NoRelPos)
		trimmed := strings.TrimSpace(line)

		// Skip blank lines and comments.
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}
		// indent is how far trimmed sits into line, so that an index into
		// trimmed can be turned into a position.
		indent := strings.Index(line, trimmed)

		// Section header.
		if trimmed[0] == '[' {
			cur, err = d.openSection(topSection, trimmed, pos)
			if err != nil {
				return nil, err
			}
			continue
		}

		// Key-value pair.
		key, value, valueIdx, ok := d.parseKeyValue(trimmed)
		if !ok {
			return nil, errors.Newf(pos, "invalid line: %s", trimmed)
		}
		valuePos := tokenFile.Pos(lineOffset+indent+valueIdx, token.NoRelPos)
		value, quoted, err := d.applyQuotes(value, valuePos)
		if err != nil {
			return nil, err
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

		field, err := makeField(key, value, quoted, pos, valuePos, d.cfg.Values == ValuesTyped)
		if err != nil {
			return nil, errors.Newf(pos, "%v", err)
		}
		cur.fields.Elts = append(cur.fields.Elts, field)
	}
	return topSection.fields, nil
}

// parseKeyValue splits a trimmed line into key and value using "=" as
// delimiter. It returns the trimmed key, the trimmed value, the index of the
// first character of the value text within line, and whether the split
// succeeded.
func (d *Decoder) parseKeyValue(line string) (key, value string, valueIdx int, ok bool) {
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", 0, false
	}
	valueIdx = len(key) + 1
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", 0, false
	}
	unindented := strings.TrimLeft(value, " \t")
	valueIdx += len(value) - len(unindented)
	value = strings.TrimSpace(unindented)

	if d.cfg.InlineComments {
		value = d.stripInlineComment(value)
	}
	return key, value, valueIdx, true
}

// stripInlineComment trims value before the first ";" or "#" that is
// preceded by a space or tab and sits outside a quoted span, so that a
// comment character within a quoted part of the value is kept. Since value
// is already trimmed, a value starting with ";" or "#" is never a comment.
//
// Under [QuotesEscaped] every " that no backslash escapes opens or closes a
// span, as git reads one. Otherwise either quote opens a span when its match
// follows within the value; an unmatched quote is an ordinary character, such
// as the apostrophe of "don't", unless it opens the value, in which case the
// whole remainder is quoted.
func (d *Decoder) stripInlineComment(value string) string {
	escaped := d.cfg.Quotes == QuotesEscaped
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '\\' && escaped:
			// The escaped byte neither quotes nor starts a comment.
			i++
		case c == '"' || (c == '\'' && !escaped):
			end := d.closingQuote(value, i+1, c)
			if end < 0 {
				if escaped || i == 0 {
					return value
				}
				continue
			}
			i = end
		case (c == ';' || c == '#') && i > 0 && (value[i-1] == ' ' || value[i-1] == '\t'):
			return strings.TrimRight(value[:i], " \t")
		}
	}
	return value
}

// closingQuote returns the index of the first quote byte in text at or after
// from, or -1 when there is none. Under [QuotesEscaped] a backslash escapes
// the byte after it, so an escaped quote closes nothing.
func (d *Decoder) closingQuote(text string, from int, quote byte) int {
	for i := from; i < len(text); i++ {
		if text[i] == '\\' && d.cfg.Quotes == QuotesEscaped {
			i++
			continue
		}
		if text[i] == quote {
			return i
		}
	}
	return -1
}

// sectionClose returns the index of the "]" closing a section header, or -1.
// A quoted subsection name may hold a "]", so the scan skips quoted spans,
// and the escapes within them, when [Config.QuotedSubsections] is set.
func (d *Decoder) sectionClose(trimmed string) int {
	if !d.cfg.QuotedSubsections {
		return strings.IndexByte(trimmed, ']')
	}
	quoted := false
	for i := 1; i < len(trimmed); i++ {
		switch trimmed[i] {
		case '\\':
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ']':
			if !quoted {
				return i
			}
		}
	}
	return -1
}

// openSection returns the section that the header on a trimmed line opens,
// creating it and the sections above it if they do not exist yet.
func (d *Decoder) openSection(top *section, trimmed string, pos token.Pos) (*section, error) {
	closeIdx := d.sectionClose(trimmed)
	if closeIdx < 0 {
		return nil, errors.Newf(pos, "missing closing bracket for section header")
	}
	if rest := strings.TrimSpace(trimmed[closeIdx+1:]); rest != "" {
		if !d.cfg.InlineComments || (rest[0] != ';' && rest[0] != '#') {
			return nil, errors.Newf(pos, "unexpected text after section header: %s", rest)
		}
	}
	name := strings.TrimSpace(trimmed[1:closeIdx])
	if name == "" {
		return nil, errors.Newf(pos, "empty section name")
	}
	parts, err := d.sectionPath(name, pos)
	if err != nil {
		return nil, err
	}
	return d.buildNestedSection(top, parts, pos)
}

// sectionPath splits a section name into the path of struct fields it names.
// Dots separate nested sections only under [Config.DottedSections], and a
// quoted subsection name contributes exactly one segment, never case-folded,
// only under [Config.QuotedSubsections].
func (d *Decoder) sectionPath(name string, pos token.Pos) ([]string, error) {
	base, sub := name, ""
	quoted := false
	if d.cfg.QuotedSubsections {
		if i := strings.IndexByte(name, '"'); i >= 0 {
			var err error
			if sub, err = subsection(name[i:]); err != nil {
				return nil, errors.Newf(pos, "%v: %s", err, name)
			}
			base, quoted = strings.TrimSpace(name[:i]), true
		}
	}
	parts := []string{base}
	if d.cfg.DottedSections {
		parts = strings.Split(base, ".")
	}
	for i, part := range parts {
		if part == "" {
			return nil, errors.Newf(pos, "empty section name")
		}
		if d.cfg.Case == CaseLower {
			parts[i] = strings.ToLower(part)
		}
	}
	if quoted {
		parts = append(parts, sub)
	}
	return parts, nil
}

// subsection reads the quoted subsection name s, which starts with its
// opening quote and must end with its closing one. A backslash escapes the
// character after it.
func subsection(s string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i++; i < len(s) {
				b.WriteByte(s[i])
			}
		case '"':
			if i != len(s)-1 {
				return "", fmt.Errorf("text after subsection name")
			}
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
	}
	return "", fmt.Errorf("missing closing quote in subsection name")
}

// buildNestedSection walks the section path down from top, creating the
// sections along the way that do not exist yet, and returns the innermost
// one. An existing section is reused, so a repeated header reopens it; an
// error is returned if any segment collides with a property in its parent.
func (d *Decoder) buildNestedSection(top *section, parts []string, pos token.Pos) (*section, error) {
	cur := top
	for _, part := range parts {
		if child := cur.children[part]; child != nil {
			cur = child
			continue
		}
		if cur.keys[part] == kindProperty {
			return nil, errors.Newf(pos, "section %s conflicts with property of the same name", part)
		}
		inner := &ast.StructLit{}
		cur.fields.Elts = append(cur.fields.Elts, &ast.Field{
			Label:    makeLabel(part, pos),
			Value:    inner,
			TokenPos: pos,
		})
		cur.keys[part] = kindSection
		child := newSection(inner)
		cur.children[part] = child
		cur = child
	}
	return cur, nil
}

// applyQuotes removes the quotation marks [Config.Quotes] gives meaning to
// and, under [QuotesEscaped], interprets the value's escape sequences. It
// reports whether it removed a quote, which keeps the value a string under
// [ValuesTyped].
func (d *Decoder) applyQuotes(value string, pos token.Pos) (_ string, quoted bool, _ error) {
	switch d.cfg.Quotes {
	case QuotesStripped:
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			return value[1 : len(value)-1], true, nil
		}
	case QuotesEscaped:
		value, quoted, err := unquote(value)
		if err != nil {
			return "", false, errors.Newf(pos, "%v", err)
		}
		return value, quoted, nil
	}
	return value, false, nil
}

// unquote reads value as git-config does: each " that no backslash escapes
// opens or closes a quoted part and is removed, and the escape sequences
// \\ \" \n \t and \b are interpreted wherever they occur. Any other backslash
// sequence is an error: leaving it alone would give one value two readings,
// and dropping the backslash would change values such as a Windows path. It
// reports whether value held a quote.
func unquote(value string) (_ string, quoted bool, _ error) {
	if !strings.ContainsAny(value, `"\`) {
		return value, false, nil
	}
	var b strings.Builder
	b.Grow(len(value))
	open := false
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '"':
			open, quoted = !open, true
		case '\\':
			i++
			if i == len(value) {
				return "", false, fmt.Errorf("value ends with a backslash: %s", value)
			}
			switch e := value[i]; e {
			case '\\', '"':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			default:
				return "", false, fmt.Errorf("unknown escape sequence: \\%c", e)
			}
		default:
			b.WriteByte(c)
		}
	}
	if open {
		return "", false, fmt.Errorf("unterminated quoted value: %s", value)
	}
	return b.String(), quoted, nil
}

// makeField creates a CUE field with an appropriate value literal.
// When typedValues is true, values are parsed as booleans or numbers when
// possible. The label carries keyPos and the value carries valuePos, so that
// an evaluator conflict points into the value rather than at the line.
func makeField(key, value string, quoted bool, keyPos, valuePos token.Pos, typedValues bool) (*ast.Field, error) {
	var v ast.Expr
	if typedValues {
		var err error
		v, err = makeValueLit(value, quoted, valuePos)
		if err != nil {
			return nil, err
		}
	} else {
		v = newStringLit(value, valuePos)
	}
	return &ast.Field{
		Label:    makeLabel(key, keyPos),
		Value:    v,
		TokenPos: keyPos,
	}, nil
}

// makeValueLit returns a bool, number, or string literal depending on the
// value. A value that was quoted is always a string; any other is parsed as a
// bool, number, or string.
//
// For example:
//   - port=443 -> port is parsed as an int
//   - portString="443" -> portString stays as a string "443"
func makeValueLit(s string, quoted bool, pos token.Pos) (ast.Expr, error) {
	if quoted {
		return newStringLit(s, pos), nil
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
