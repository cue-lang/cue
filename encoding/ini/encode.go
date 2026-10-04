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

package ini

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// NewEncoder creates an encoder writing the INI flavor cfg describes to w.
// The encoder keeps its own copy of cfg; later changes to cfg have no effect.
func NewEncoder(w io.Writer, cfg Config) *Encoder {
	return &Encoder{w: w, cfg: cfg.withDefaults()}
}

// Encoder implements the encoding state for INI output.
//
// An INI file holds a single document, so an Encoder writes one value.
type Encoder struct {
	w    io.Writer
	cfg  Config
	done bool
}

// Encode writes v, which must be a concrete struct, as an INI document.
// Nothing is written unless the whole document can be: every error leaves
// the writer untouched. A second call returns an error.
func (e *Encoder) Encode(v cue.Value) error {
	if e.done {
		return errors.Newf(token.NoPos, "cannot write a second value: an INI file holds one document")
	}
	e.done = true
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return err
	}
	if kind := v.Kind(); kind != cue.StructKind {
		return errorf(v, nil, kind.String(), "an INI document is a struct", "")
	}

	// The global properties come before the first header, wherever they
	// sit among the sections in the CUE value.
	root := &encSection{v: v, keys: []string{}}
	var buf bytes.Buffer
	children, err := e.fields(&buf, root)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := e.section(&buf, child); err != nil {
			return err
		}
	}
	_, err = e.w.Write(buf.Bytes())
	return err
}

// encSection is a struct written as a section, or the root struct.
type encSection struct {
	v      cue.Value
	path   []cue.Selector
	parent *encSection
	// keys are the names the decoder files the section under, one per path
	// element, once a header has named the section: its own, or that of a
	// section it holds. They are nil until then.
	keys []string
	// taken holds the path of each field filed in the section, by the name
	// it is filed under.
	taken map[string][]cue.Selector
}

// claim records that the field v at path is filed in s under key, which is
// an error if another field already is.
func (s *encSection) claim(key string, path []cue.Selector, v cue.Value) error {
	if other, ok := s.taken[key]; ok {
		return errorf(v, path, "name", cue.MakePath(other...).String()+" has the same name under Config.Case", "Config.Case to CasePreserve")
	}
	if s.taken == nil {
		s.taken = make(map[string][]cue.Selector)
	}
	s.taken[key] = path
	return nil
}

// fields writes the scalar and list fields of s as properties to buf, and
// returns its struct fields as the sections it holds. A section is filed in
// s once its keys are known, by [Encoder.header].
func (e *Encoder) fields(buf *bytes.Buffer, s *encSection) ([]*encSection, error) {
	var children []*encSection
	iter, err := s.v.Fields()
	if err != nil {
		return nil, err
	}
	for iter.Next() {
		fv := iter.Value()
		path := append(slices.Clip(s.path), iter.Selector())
		if fv.Kind() == cue.StructKind {
			children = append(children, &encSection{v: fv, path: path, parent: s})
			continue
		}
		key := e.cfg.fold(e.cfg.keyName(iter.Selector().Unquoted()))
		if err := s.claim(key, path, fv); err != nil {
			return nil, err
		}
		if err := e.property(buf, path, fv); err != nil {
			return nil, err
		}
	}
	return children, nil
}

// section writes s as a section block to buf, a blank line apart from any
// block before it, followed by the blocks of the sections it holds. A
// header ends the properties above it, so the section's own properties come
// first. A section holding only sections gets no header of its own, as
// their headers imply it.
func (e *Encoder) section(buf *bytes.Buffer, s *encSection) error {
	// The header comes first, as a section that cannot be named makes the
	// spelling of its properties moot.
	headed, err := ownsFields(s.v)
	if err != nil {
		return err
	}
	if headed {
		header, err := e.header(s)
		if err != nil {
			return err
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(header)
		buf.WriteByte('\n')
	}
	// Without a header the section has no properties to write.
	children, err := e.fields(buf, s)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := e.section(buf, child); err != nil {
			return err
		}
	}
	return nil
}

// ownsFields reports whether the struct v has a field other than a struct, or
// no fields at all, so that its section needs a header of its own.
func ownsFields(v cue.Value) (bool, error) {
	iter, err := v.Fields()
	if err != nil {
		return false, err
	}
	empty := true
	for iter.Next() {
		if iter.Value().Kind() != cue.StructKind {
			return true, nil
		}
		empty = false
	}
	return empty, nil
}

// quotedAt reports whether a section at the given depth is written as a
// quoted subsection name, which the decoder never folds.
func (e *Encoder) quotedAt(depth int) bool {
	return depth == 2 && e.cfg.QuotedSubsections
}

// subsectionEscaper escapes a quoted subsection name, the inverse of
// [subsection].
var subsectionEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// header returns the header line of s, in the form the nesting options give
// its depth, and files s and its implied ancestors under the keys the
// decoder files them under.
func (e *Encoder) header(s *encSection) (string, error) {
	names := make([]string, len(s.path))
	for i, sel := range s.path {
		names[i] = sel.Unquoted()
	}
	last := len(names) - 1
	quoted := e.quotedAt(len(names))

	var text string
	switch {
	case last == 0:
		text = names[0]
	case quoted:
		text = names[0] + ` "` + subsectionEscaper.Replace(names[1]) + `"`
	case e.cfg.DottedSections:
		text = strings.Join(names, ".")
	case e.cfg.QuotedSubsections:
		return "", errorf(s.v, s.path, "section", "sections nested more than two deep need dotted headers", "Config.DottedSections to true")
	default:
		return "", errorf(s.v, s.path, "section", "nested sections need a nesting option", "Config.QuotedSubsections or Config.DottedSections to true")
	}

	keys := make([]string, len(names))
	for i, name := range names {
		subsection := quoted && i == last
		// Within the quotes, the escapes protect every byte but a line
		// break.
		if subsection && strings.ContainsAny(name, "\r\n") || !subsection && !e.plainSegment(name) {
			return "", errorf(s.v, s.path, "section", "its header would not decode back to it", "")
		}
		keys[i] = e.cfg.sectionKey(name, subsection)
	}
	// A dotted header also names the sections above, so where the case
	// option folds names it must file them where an earlier header did.
	if p := s.parent; p.keys != nil && !slices.Equal(keys[:last], p.keys) {
		return "", errorf(s.v, s.path, "section", "its header would not decode back to it", "")
	}
	s.keys = keys
	if err := s.parent.claim(keys[last], s.path, s.v); err != nil {
		return "", err
	}
	for p := s.parent; p.keys == nil; p = p.parent {
		p.keys = keys[:len(p.path)]
		if err := p.parent.claim(p.keys[len(p.keys)-1], p.path, p.v); err != nil {
			return "", err
		}
	}
	return "[" + text + "]", nil
}

// property writes the field v at path as property lines to buf: one, or one
// per element of a list.
func (e *Encoder) property(buf *bytes.Buffer, path []cue.Selector, v cue.Value) error {
	key := path[len(path)-1].Unquoted()
	if !e.plainKey(key) {
		return errorf(v, path, "key", "no spelling decodes back to it", "")
	}
	if v.Kind() != cue.ListKind {
		return e.line(buf, path, key, v)
	}
	if e.cfg.DuplicateKeys != DuplicatesList {
		return errorf(v, path, "list", "lists are repeated keys", "Config.DuplicateKeys to DuplicatesList")
	}
	iter, err := v.List()
	if err != nil {
		return err
	}
	i := 0
	for ; iter.Next(); i++ {
		elem := iter.Value()
		if kind := elem.Kind(); kind == cue.StructKind || kind == cue.ListKind {
			return errorf(v, path, "list", fmt.Sprintf("element %d is a %s", i, kind), "")
		}
		if err := e.line(buf, append(slices.Clip(path), cue.Index(i)), key, elem); err != nil {
			return err
		}
	}
	if i == 0 {
		return errorf(v, path, "empty list", "no lines would decode back to it", "")
	}
	return nil
}

// line writes the scalar v at path as one property line for key to buf.
func (e *Encoder) line(buf *bytes.Buffer, path []cue.Selector, key string, v cue.Value) error {
	var value string
	switch kind := v.Kind(); kind {
	case cue.StringKind:
		s, err := v.String()
		if err != nil {
			return err
		}
		var ok bool
		if value, ok = e.stringValue(s); !ok {
			hint := ""
			if e.cfg.Quotes != QuotesEscaped {
				hint = "Config.Quotes to QuotesEscaped"
			}
			return errorf(v, path, "string", "no spelling decodes back to it", hint)
		}
	case cue.BoolKind:
		b, err := v.Bool()
		if err != nil {
			return err
		}
		value = fmt.Sprint(b)
	case cue.IntKind, cue.FloatKind:
		text, err := v.MarshalJSON()
		if err != nil {
			return err
		}
		value = string(text)
		if _, ok := e.cfg.parseBool(value); ok && e.cfg.Values == ValuesTyped {
			return errorf(v, path, "number", "it would decode as a boolean", "Config.Booleans to BooleansTrueFalse")
		}
	case cue.NullKind:
		if e.cfg.BareKeys != BareKeysNull {
			return errorf(v, path, "null", "null is a bare key", "Config.BareKeys to BareKeysNull")
		}
		buf.WriteString(key)
		buf.WriteByte('\n')
		return nil
	default:
		return errorf(v, path, kind.String(), "INI has no "+kind.String()+" values", "")
	}
	buf.WriteString(key)
	buf.WriteByte(' ')
	buf.WriteByte(e.cfg.Delimiters[0])
	// An empty value, or one whose first line is empty, ends the line at
	// the delimiter.
	if value != "" && value[0] != '\n' {
		buf.WriteByte(' ')
	}
	buf.WriteString(value)
	buf.WriteByte('\n')
	return nil
}

// plainKey reports whether key reads back as itself as the key of a
// property line, mirroring [Decoder.parseKeyValue] and the classification
// of a line in [Decoder.Decode].
func (e *Encoder) plainKey(key string) bool {
	switch {
	case key == "", strings.TrimSpace(key) != key, strings.ContainsAny(key, "\r\n"):
		// The key is trimmed, and a line ends at a line break.
		return false
	case key[0] == '[' || isComment(key):
		// The line would be a header or a comment.
		return false
	case strings.ContainsAny(key, e.cfg.Delimiters):
		// The first delimiter splits the line.
		return false
	case e.cfg.keyComment(key) >= 0:
		// A comment would end the line within the key.
		return false
	}
	return true
}

// plainSegment reports whether name reads back as itself as a section
// name outside quotes, mirroring [Decoder.sectionClose],
// [Decoder.openSection], and [Decoder.sectionPath].
func (e *Encoder) plainSegment(name string) bool {
	switch {
	case name == "", strings.TrimSpace(name) != name, strings.ContainsAny(name, "\r\n"):
		// The name is trimmed, and a line ends at a line break.
		return false
	case strings.Contains(name, "]") && !e.cfg.TrailingHeaderText:
		// The header ends at the first "]".
		return false
	case strings.Contains(name, `"`) && e.cfg.QuotedSubsections:
		// A quote starts a subsection name.
		return false
	case strings.Contains(name, ".") && e.cfg.DottedSections:
		// A dot separates nested sections.
		return false
	}
	return true
}

// stringValue returns the spelling of s as a property value, and false if
// the options give s no spelling that reads back as s.
func (e *Encoder) stringValue(s string) (string, bool) {
	if e.plainValue(s) {
		return s, true
	}
	if e.cfg.Continuations == ContinuationsIndented && strings.Contains(s, "\n") {
		if value, ok := e.indentedValue(s); ok {
			return value, true
		}
	}
	switch e.cfg.Quotes {
	case QuotesStripped:
		if strings.Contains(s, "\n") {
			return "", false
		}
		// Stripping removes only the outer pair of quotes, but under
		// inline comments a quote within s may end the quoted span
		// early; the other kind of quote may still cover all of s.
		for _, quote := range []string{`"`, `'`} {
			if start, _ := e.cfg.commentStart(quote+s+quote, 0, 0, never); start < 0 {
				return quote + s + quote, true
			}
		}
		return "", false
	case QuotesEscaped:
		// As git writes a value: escaped, and in quotes only when the
		// escaped text would not read back on its own.
		escaped := escape(s)
		if strings.TrimSpace(escaped) == escaped && !e.commentIn(escaped) && !e.typedReading(s) {
			return escaped, true
		}
		return `"` + escaped + `"`, true
	}
	return "", false
}

// indentedValue returns the spelling of s, which holds a line break, as a
// first line followed by indented continuation lines, and false if s does not
// read back from them, mirroring [Decoder.joinIndented], [Decoder.nextIndented],
// and [Decoder.value].
func (e *Encoder) indentedValue(s string) (string, bool) {
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		// Trailing white space is trimmed from the value.
		return "", false
	}
	for i, line := range lines {
		if strings.TrimSpace(line) != line || (i > 0 && isComment(line)) {
			// Each line is trimmed, and a comment line is skipped.
			return "", false
		}
	}
	if !e.quotesInert(s) {
		return "", false
	}
	if e.cfg.inlineComments() {
		// Scan the lines as [Decoder.joinIndented] hands them to the
		// comment scan, a continuation line after its indentation, and
		// with a quote left open carried over. An empty line never
		// reaches the scan.
		var open byte
		for i, line := range lines {
			prev := byte(0)
			if i > 0 {
				if line == "" {
					continue
				}
				prev = '\t'
			}
			closesLater := func(quote byte) bool {
				for _, later := range lines[i+1:] {
					if e.cfg.closingQuote(later, 0, quote) >= 0 {
						return true
					}
				}
				return false
			}
			var start int
			if start, open = e.cfg.commentStart(line, prev, open, closesLater); start >= 0 {
				return "", false
			}
		}
	}
	var b strings.Builder
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteByte('\n')
		if line != "" {
			// An empty line is part of the value as long as an indented
			// line follows it, which the last line always is.
			b.WriteByte('\t')
			b.WriteString(line)
		}
	}
	return b.String(), true
}

// plainValue reports whether s reads back as itself as the text of a
// property value, mirroring [Decoder.addFragment], [Decoder.value], and
// [Decoder.decodeValue].
func (e *Encoder) plainValue(s string) bool {
	switch {
	case strings.Contains(s, "\n"), strings.TrimSpace(s) != s:
		// A line ends at a line break, and a value is trimmed.
		return false
	case e.commentIn(s), !e.quotesInert(s):
		return false
	case e.cfg.backslashJoins() && continues(s):
		// A trailing backslash would continue the value onto the next line.
		return false
	case e.typedReading(s):
		return false
	}
	return true
}

// commentIn reports whether a comment would start within s, the text of a
// value on one line.
func (e *Encoder) commentIn(s string) bool {
	if !e.cfg.inlineComments() {
		return false
	}
	start, _ := e.cfg.commentStart(s, 0, 0, never)
	return start >= 0
}

// typedReading reports whether s, written without quotes, would decode as a
// boolean or a number rather than as a string.
func (e *Encoder) typedReading(s string) bool {
	if e.cfg.Values != ValuesTyped {
		return false
	}
	if _, ok := e.cfg.parseBool(s); ok {
		return true
	}
	_, _, ok := jsonNumber(s)
	return ok
}

// quotesInert reports whether [Config.Quotes] gives no quote or backslash in
// s a meaning, so that it reads back as s.
func (e *Encoder) quotesInert(s string) bool {
	value, quoted, err := e.cfg.applyQuotes(s)
	return err == nil && !quoted && value == s
}

// never reports that a quote is never closed on a later line, for text
// written on one line.
func never(quote byte) bool { return false }

// escape spells s with the escape sequences [QuotesEscaped] reads, the
// inverse of [unquote] on text that holds no quotes of its own.
func escape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		for _, pair := range escapes {
			if pair[0] == c {
				b.WriteByte('\\')
				c = pair[1]
				break
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// errorf returns the error for the value v at path, a kind of thing named by
// what that the options cannot write for the given reason. hint, when not
// empty, names the option settings that would allow it.
func errorf(v cue.Value, path []cue.Selector, what, reason, hint string) error {
	at := "the root"
	if len(path) > 0 {
		at = cue.MakePath(path...).String()
	}
	msg := fmt.Sprintf("cannot write %s at %s: %s", what, at, reason)
	if hint != "" {
		msg += "; set " + hint + " to allow it"
	}
	return errors.Newf(v.Pos(), "%s", msg)
}
