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

// Package ini converts between INI and CUE.
//
// INI files hold sections, properties, and comments. There is no INI
// specification, so the zero [Config] accepts only the subset that every
// flavor shares and transforms nothing beyond trimming:
//
//   - a section is introduced by a [name] header, and a repeated header
//     reopens the section it names;
//   - a property is "key = value";
//   - a line whose first non-blank character is ";" or "#" is a comment,
//     and a comment never starts within a value;
//   - a value is the text after the delimiter with leading and trailing
//     whitespace removed and nothing else changed, so quotes,
//     backslashes, ";" and "#" are all literal;
//   - blank lines are ignored, a leading byte order mark is removed, and
//     lines may end in LF or CRLF;
//   - a key repeated within a section, an empty section name, text after
//     "]", a line with no delimiter, and an empty key are each an error
//     carrying the position of the offending line.
//
// Properties before the first header become top-level fields, and each
// section becomes a struct field.
//
// # Options
//
// Every flavor difference is one field of [Config], selectable
// independently of the others. The first value listed is the default:
//
//   - [Config.Delimiters]: the bytes splitting a key from its value, "="
//     by default;
//   - [Config.Comments]: [CommentsWholeLine], [CommentsInline],
//     [CommentsAnywhere];
//   - [Config.Quotes]: [QuotesLiteral], [QuotesStripped], [QuotesEscaped];
//   - [Config.Case]: [CasePreserve], [CaseLowerKeys], [CaseLower],
//     [CaseInsensitive];
//   - [Config.DottedSections]: whether a dot in a section name nests, off
//     by default;
//   - [Config.QuotedSubsections]: whether a quoted part of a section name
//     nests, off by default;
//   - [Config.TrailingHeaderText]: whether text after a section header is
//     ignored, off by default;
//   - [Config.DuplicateKeys]: [DuplicatesError], [DuplicatesList],
//     [DuplicatesFirst], [DuplicatesLast];
//   - [Config.DuplicateSections]: [DuplicateSectionsMerge],
//     [DuplicateSectionsError], [DuplicateSectionsFirst];
//   - [Config.Continuations]: [ContinuationsNone],
//     [ContinuationsBackslash], [ContinuationsBackslashSpace],
//     [ContinuationsIndented];
//   - [Config.BareKeys]: [BareKeysError], [BareKeysNull], [BareKeysTrue];
//   - [Config.Values]: [ValuesStrings], [ValuesTyped];
//   - [Config.Booleans]: [BooleansTrueFalse], [BooleansExtended].
//
// The same options describe the flavor for writing as well as reading.
//
// # Flavors
//
// A flavor is a complete [Config]. These functions each return the flavor
// of one tool, reading a file as that tool does, so combining one with an
// explicit option is an ordinary assignment:
//
//   - [GitConfig] sets [CommentsAnywhere], [QuotesEscaped], [CaseLower],
//     [Config.DottedSections], [Config.QuotedSubsections],
//     [DuplicatesList], [ContinuationsBackslash], and [BareKeysTrue];
//   - [PythonConfig] sets [Config.Delimiters] to "=:", [CaseLowerKeys],
//     [Config.TrailingHeaderText], [DuplicateSectionsError], and
//     [ContinuationsIndented];
//   - [SystemdConfig] sets [DuplicatesList] and
//     [ContinuationsBackslashSpace];
//   - [WindowsConfig] sets [QuotesStripped], [CaseInsensitive],
//     [Config.TrailingHeaderText], [DuplicatesFirst], and
//     [DuplicateSectionsFirst].
//
// A flavor aims to be compatible with its tool rather than identical to
// it: a file the tool accepts decodes to the values the tool reads, while
// some input the tool rejects may decode too. Each flavor documents where
// it departs from its tool.
//
// On the command line the flavor tag of the ini file type names one, as
// in "cue export ini+flavor=git: .gitconfig".
//
// # Encoding
//
// An [Encoder] writes a concrete CUE struct as INI in the flavor a [Config]
// describes, and what it writes decodes back under the same [Config]. The
// struct's scalar and list fields are written first, before any header,
// and each struct field becomes a section, in source order. Every flavor
// shares one layout: "key = value" with the first of [Config.Delimiters],
// no indentation but before a continuation line, and one blank line
// between sections. Names keep their CUE spelling, since a flavor that
// folds names does so when reading them.
//
// A value is written as its text when the options give that text no other
// reading, and is otherwise escaped, quoted, or spread over indented lines
// as the options allow. A value the options cannot write is an error naming
// its path and, where one exists, the option that would allow it, and
// nothing is written.
//
// Since a key repeated once is read as a single value, a list of one
// element decodes back as that element; a schema such as
// "string | [...string]" reads both.
//
// # Unsupported
//
// These features of particular flavors are out of scope:
//
//   - value interpolation, such as configparser's %(name)s;
//   - directives including another file, such as git's include.path;
//   - configparser's [DEFAULT] fallbacks, so a [DEFAULT] header decodes
//     as an ordinary section;
//   - PHP's "key[] = value" arrays, whose key decodes literally;
//   - input that is not valid UTF-8.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package ini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// CommentMode controls where the decoder recognizes a comment.
type CommentMode int

const (
	commentsUnset     CommentMode = iota // default zero value; equal to [CommentsWholeLine] for now
	CommentsWholeLine                    // only a line whose first non-blank character is ";" or "#"
	CommentsInline                       // also a ";" or "#" outside quotes that follows a space or tab
	CommentsAnywhere                     // also any ";" or "#" outside quotes
)

// QuoteMode controls how the decoder treats quotation marks in a value.
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
	caseUnset       CaseMode = iota // default zero value; equal to [CasePreserve] for now
	CasePreserve                    // keys and section names keep their source case
	CaseLowerKeys                   // keys are lowercased, section names are not
	CaseLower                       // keys and section names are lowercased
	CaseInsensitive                 // keys and section names compare regardless of case, keeping their first spelling
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

// DuplicateSectionMode controls how the decoder treats a repeated section header.
type DuplicateSectionMode int

const (
	duplicateSectionsUnset DuplicateSectionMode = iota // default zero value; equal to [DuplicateSectionsMerge] for now
	DuplicateSectionsMerge                             // a repeated header reopens its section
	DuplicateSectionsError                             // a repeated header is an error
	DuplicateSectionsFirst                             // the properties under a repeated header are ignored
)

// ContinuationMode controls how the decoder joins a value spanning several lines.
type ContinuationMode int

const (
	continuationsUnset          ContinuationMode = iota // default zero value; equal to [ContinuationsNone] for now
	ContinuationsNone                                   // every value ends at its line
	ContinuationsBackslash                              // a trailing backslash is dropped and the next line joined on
	ContinuationsBackslashSpace                         // a trailing backslash becomes a space, and comment lines between are skipped
	ContinuationsIndented                               // an indented line continues the value of the preceding property
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

// Config describes an INI flavor. The zero value is the generic flavor,
// the common subset described in the package documentation; each field
// selects one flavor difference, independently of all others.
//
// Config is passed and returned by value, so a flavor such as [GitConfig]
// combined with an explicit option is an ordinary assignment. It describes
// the flavor for writing as well as for reading; each field below states
// its writing meaning where that is not simply the reverse of its reading
// meaning.
type Config struct {
	// Delimiters holds the bytes accepted between a key and its value;
	// the first occurrence of any of them splits the line.
	// The empty string means "=", the default.
	//
	// When writing, the first byte is the one used.
	Delimiters string

	// Comments controls where a comment may start. Under
	// [CommentsWholeLine], the default, only a whole line is a comment, so
	// that no value is silently truncated. [CommentsInline] also ends a
	// line at a ";" or "#" that follows a space or tab, and
	// [CommentsAnywhere] at any ";" or "#", as git does; under either, one
	// within a quoted part of a value starts nothing. A comment may follow
	// a section header, and one before the delimiter makes the line a bare
	// key. Since a value is trimmed first, under [CommentsInline] a value
	// whose first character is ";" or "#" is never a comment.
	Comments CommentMode

	// Quotes controls how quotation marks in a value are treated:
	// [QuotesLiteral], the default, leaves them as ordinary characters.
	// [QuotesStripped] removes the first and last characters of a value
	// that starts and ends with the same " or ', as the Windows profile API
	// does. [QuotesEscaped] reads quotes as git does: a " anywhere opens or
	// closes a quoted part of the value and is removed, and the escape
	// sequences \\ \" \n \t and \b are interpreted throughout the value,
	// any other being an error, as is a quoted part left open. A ' is then
	// an ordinary character. A value holding a quote it removed stays a
	// string under [ValuesTyped].
	Quotes QuoteMode

	// Case controls the case of keys and section names: [CasePreserve],
	// the default, keeps the source case, [CaseLowerKeys] lowercases keys
	// only, and [CaseLower] lowercases section names too.
	// [CaseInsensitive] keeps the case of each name as first written, but
	// a later name differing only in case is the same name, as the Windows
	// profile API compares them. A quoted subsection name is never folded.
	//
	// When writing, names keep their spelling under every mode, since a
	// reader folds them itself; two names in one scope that the mode would
	// make the same are an error.
	Case CaseMode

	// DottedSections reports whether dots in a section name separate
	// nested sections, to any depth. By default they are ordinary
	// characters.
	DottedSections bool

	// QuotedSubsections reports whether a section name of the form
	// `a "b"` nests b exactly one level below a, keeping its case. Within
	// the quotes a backslash escapes the character after it, so \" and \\
	// stand for a quote and a backslash, as git reads a subsection name.
	// By default it does not.
	QuotedSubsections bool

	// TrailingHeaderText reports whether text after a section header is
	// ignored, as configparser and the Windows profile API ignore it; the
	// name then ends at the last "]" on the line. By default such text is
	// an error, unless [Config.Comments] makes it a comment.
	//
	// It has no effect on writing, which never puts text after a header.
	TrailingHeaderText bool

	// DuplicateKeys controls what happens when a key recurs within a
	// section: [DuplicatesError], the default, rejects the second
	// occurrence, while [DuplicatesList], [DuplicatesFirst] and
	// [DuplicatesLast] collect, keep, or replace.
	//
	// Only [DuplicatesList] lets a list be written; under the other three
	// a list has no INI spelling, so writing one is an error.
	DuplicateKeys DuplicateMode

	// DuplicateSections controls what happens when a section header names a
	// section an earlier header named: [DuplicateSectionsMerge], the
	// default, reopens the section, [DuplicateSectionsError] rejects the
	// header, as configparser does, and [DuplicateSectionsFirst] ignores the
	// properties under it, as the Windows profile API does.
	//
	// It has no effect on writing, which writes each section once.
	DuplicateSections DuplicateSectionMode

	// Continuations controls how a value may span several lines: not at all
	// ([ContinuationsNone], the default), through a trailing backslash that
	// is dropped ([ContinuationsBackslash], as git config reads one) or
	// that becomes a space ([ContinuationsBackslashSpace], as systemd reads
	// one), or through lines indented further than the key
	// ([ContinuationsIndented], as configparser reads them). A backslash is
	// only a continuation within the value, so one inside an inline comment
	// continues nothing, and a value ends at a blank line or the end of the
	// input even after one.
	//
	// When writing, a string holding a line break is written as indented
	// lines under [ContinuationsIndented]. The backslash forms join lines
	// without a line break, so under them, as under [ContinuationsNone],
	// such a string is written through [Config.Quotes] or not at all.
	Continuations ContinuationMode

	// BareKeys controls what a line holding a key and no delimiter means:
	// an error ([BareKeysError], the default), null ([BareKeysNull]), or
	// true ([BareKeysTrue]). It is distinct from "key =", which is the
	// empty string under every mode. No flavor described here reads a
	// valueless key as null; [BareKeysNull] is for callers whose flavor
	// treats one as unset rather than as enabled.
	//
	// When writing, null is a bare key under [BareKeysNull] and an error
	// otherwise; a boolean is never written as a bare key.
	BareKeys BareKeyMode

	// Values controls whether the type of a value is interpreted:
	// [ValuesStrings], the default, makes every value a string, while
	// [ValuesTyped] recognizes booleans and numbers.
	Values ValueMode

	// Booleans selects the vocabulary [ValuesTyped] recognizes as
	// booleans: true and false only ([BooleansTrueFalse], the default),
	// or also yes, no, on, off, 1 and 0 ([BooleansExtended]). It has no
	// effect until [ValuesTyped] asks for values to be typed.
	//
	// It affects acceptance only; true and false are what gets written.
	Booleans BooleanMode
}

// The methods below hold the parts of a flavor's grammar that depend only on
// the [Config], so that the decoder and the encoder share them.

// withDefaults returns cfg with an empty [Config.Delimiters] set to "=", the
// one option whose zero value is not its default.
func (cfg Config) withDefaults() Config {
	if cfg.Delimiters == "" {
		cfg.Delimiters = "="
	}
	return cfg
}

// keyName returns a key as it decodes, lowercased under [CaseLowerKeys] and
// [CaseLower].
func (cfg Config) keyName(key string) string {
	if cfg.Case == CaseLowerKeys || cfg.Case == CaseLower {
		return strings.ToLower(key)
	}
	return key
}

// sectionName returns a section name outside quotes as it decodes,
// lowercased under [CaseLower].
func (cfg Config) sectionName(name string) string {
	if cfg.Case == CaseLower {
		return strings.ToLower(name)
	}
	return name
}

// fold returns the name under which a decoded key or section name is looked
// up, which under [CaseInsensitive] ignores its case.
func (cfg Config) fold(name string) string {
	if cfg.Case == CaseInsensitive {
		return strings.ToLower(name)
	}
	return name
}

// sectionKey returns the name under which a section path segment is looked
// up once decoded; a quoted subsection name is never folded.
func (cfg Config) sectionKey(part string, quoted bool) string {
	if quoted {
		return part
	}
	return cfg.fold(cfg.sectionName(part))
}

// backslashJoins reports whether a trailing backslash continues a value onto
// the next line.
func (cfg Config) backslashJoins() bool {
	return cfg.Continuations == ContinuationsBackslash || cfg.Continuations == ContinuationsBackslashSpace
}

// inlineComments reports whether a comment may start after other text on a
// line, as [CommentsInline] and [CommentsAnywhere] allow.
func (cfg Config) inlineComments() bool {
	return cfg.Comments == CommentsInline || cfg.Comments == CommentsAnywhere
}

// commentAfter reports whether a ";" or "#" outside quotes that follows prev
// starts a comment, prev being zero at the start of a value.
func (cfg Config) commentAfter(prev byte) bool {
	switch cfg.Comments {
	case CommentsAnywhere:
		return true
	case CommentsInline:
		return prev == ' ' || prev == '\t'
	}
	return false
}

// keyComment returns the index of the comment that starts in s, the text
// before a line's delimiter, or -1 when there is none. A key holds no quotes,
// so only [Config.Comments] decides.
func (cfg Config) keyComment(s string) int {
	for i := 1; i < len(s); i++ {
		if (s[i] == ';' || s[i] == '#') && cfg.commentAfter(s[i-1]) {
			return i
		}
	}
	return -1
}

// commentStart returns the index in text, one fragment of a value, of the ";"
// or "#" that starts a comment under [Config.Comments] outside a quoted span,
// or -1 when there is none, and the quote left open at the end of text. prev
// is the byte before text within the value, zero at the start of one, and
// open the quote an earlier fragment left open. closesLater reports whether a
// quote that text leaves unmatched is matched on a line continuing the value.
//
// Under [QuotesEscaped] every " that no backslash escapes opens or closes a
// span, as git reads one. Otherwise either quote opens a span when its match
// follows within the value, which may be on a continuation line, and a quote
// opening the value does so even without one; an unmatched quote anywhere
// else, such as the apostrophe of "don't", is an ordinary character.
func (cfg Config) commentStart(text string, prev, open byte, closesLater func(quote byte) bool) (int, byte) {
	escaped := cfg.Quotes == QuotesEscaped
	i := 0
	if open != 0 {
		end := cfg.closingQuote(text, 0, open)
		if end < 0 {
			// The whole fragment sits inside the span.
			return -1, open
		}
		i = end + 1
	}
	for ; i < len(text); i++ {
		if i > 0 {
			prev = text[i-1]
		}
		switch c := text[i]; {
		case c == '\\' && escaped:
			// The escaped byte neither quotes nor starts a comment.
			i++
		case c == '"' || (c == '\'' && !escaped):
			if end := cfg.closingQuote(text, i+1, c); end >= 0 {
				i = end
			} else if escaped || (i == 0 && prev == 0) || closesLater(c) {
				// The span reaches the end of the fragment, so it holds
				// no comment.
				return -1, c
			}
		case (c == ';' || c == '#') && cfg.commentAfter(prev):
			return i, 0
		}
	}
	return -1, 0
}

// closingQuote returns the index of the first quote byte in text at or after
// from, or -1 when there is none. Under [QuotesEscaped] a backslash escapes
// the byte after it, so an escaped quote closes nothing.
func (cfg Config) closingQuote(text string, from int, quote byte) int {
	for i := from; i < len(text); i++ {
		if text[i] == '\\' && cfg.Quotes == QuotesEscaped {
			i++
			continue
		}
		if text[i] == quote {
			return i
		}
	}
	return -1
}

// NewDecoder creates a decoder for the INI flavor cfg describes.
// The decoder keeps its own copy of cfg; later changes to cfg have no effect.
func NewDecoder(filename string, r io.Reader, cfg Config) *Decoder {
	cfg = cfg.withDefaults()
	d := &Decoder{r: r, filename: filename, cfg: cfg}
	switch cfg.Continuations {
	case ContinuationsBackslashSpace:
		d.sep = " "
	case ContinuationsIndented:
		d.sep = "\n"
	}
	return d
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
	lines     []line

	// sep joins the fragments of a value. It follows from
	// [Config.Continuations], so it is resolved once.
	sep string
}

// section tracks per-section state needed to detect name collisions.
// It outlives any one [name] header, since a repeated header may reopen the
// section rather than starting a new one.
type section struct {
	// fields holds this section's fields.
	fields *ast.StructLit
	// props holds the field of each property in fields, so that a repeated
	// key can extend or replace the value already decoded, and children
	// holds the sections nested directly in this one, so that a section
	// path is followed one segment at a time. A name in either map is a
	// name taken, which is how a property and a section of the same name
	// are found to collide. Both are keyed by [Config.fold].
	props    map[string]*ast.Field
	children map[string]*section
	// headed reports whether a header has named this section, rather than
	// only a longer path through it, so that a repeated header is told
	// apart from the first.
	headed bool
}

// newSection starts an empty section holding the given struct.
func newSection(fields *ast.StructLit) *section {
	return &section{
		fields:   fields,
		props:    make(map[string]*ast.Field),
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

	d.tokenFile = token.NewFile(d.filename, 0, len(data))
	d.tokenFile.SetLinesForContent(data)

	topSection := newSection(&ast.StructLit{})
	cur := topSection

	d.lines = splitLines(string(data))
	for i := 0; i < len(d.lines); i++ {
		line := d.lines[i]
		pos := d.pos(line.offset)
		trimmed := strings.TrimSpace(line.text)
		// indent is how far trimmed sits into line.text, so that an index
		// into trimmed can be turned into a position and an indented
		// continuation line can be told from a new property.
		indent := indentOf(line.text)

		// Skip blank lines and comments.
		if trimmed == "" || isComment(trimmed) {
			continue
		}

		// Section header.
		if trimmed[0] == '[' {
			cur, err = d.openSection(topSection, trimmed, pos)
			if err != nil {
				return nil, err
			}
			continue
		}

		// Key-value pair. The value keeps any trailing whitespace, which
		// decides whether a backslash ends the line.
		key, value, valueIdx, bare, ok := d.parseKeyValue(line.text[indent:])
		if !ok {
			return nil, errors.Newf(pos, "invalid line: %s", trimmed)
		}
		key = d.cfg.keyName(key)
		p := &property{
			sec:      cur,
			key:      key,
			keyPos:   pos,
			valuePos: d.pos(line.offset + indent + valueIdx),
			indent:   indent,
			bare:     bare,
		}
		continued := d.addFragment(p, value, i)
		if d.cfg.Continuations == ContinuationsIndented {
			i = d.joinIndented(p, i)
		} else {
			i = d.joinContinuations(p, continued, i)
		}
		if err := d.addProperty(p); err != nil {
			return nil, err
		}
	}
	return topSection.fields, nil
}

// pos turns an offset into the input into a position.
func (d *Decoder) pos(offset int) token.Pos {
	return d.tokenFile.Pos(offset, token.NoRelPos)
}

// line is one logical input line: the text to classify, and the offset at
// which it starts in the input.
type line struct {
	text   string
	offset int
}

// property is a decoded key and the fragments of its value, held until no
// further input line can extend the value.
type property struct {
	sec      *section
	key      string
	keyPos   token.Pos
	valuePos token.Pos
	// indent is how far the key sits into its line, which an indented
	// continuation line must exceed.
	indent int
	// bare reports that the line held no delimiter, so that the value
	// comes from [Config.BareKeys] rather than from the text.
	bare bool

	// parts are the fragments of the value, joined by [Decoder.sep]. Each
	// fragment is scanned once, rather than the value assembled so far, so
	// that decoding stays linear in the size of the input.
	parts []string
	// prev is the last byte of the fragments so far, and open the quote one
	// of them left unclosed, which the next fragment continues inside. Both
	// are zero at the start of a value.
	prev byte
	open byte
}

// addFragment appends text, from line i, to p's value, dropping an inline
// comment it starts and carrying the quote it leaves open. It reports whether
// text ends in a line continuation, whose backslash it drops.
func (d *Decoder) addFragment(p *property, text string, i int) (continued bool) {
	if d.cfg.inlineComments() {
		prev := p.prev
		if len(p.parts) > 0 && d.sep != "" {
			prev = d.sep[len(d.sep)-1]
		}
		text, p.open = d.stripInlineComment(p, text, i, prev)
	}
	if d.cfg.backslashJoins() && continues(text) {
		text, continued = text[:len(text)-1], true
	}
	// An empty fragment contributes no bytes, so it leaves prev alone.
	if text != "" {
		p.prev = text[len(text)-1]
	}
	p.parts = append(p.parts, text)
	return continued
}

// value joins the fragments of p's value and trims it. configparser trims
// each line of an indented value, keeping the line break an empty first line
// leaves, while git keeps the whitespace before a continuation backslash even
// at the end of the value.
func (d *Decoder) value(p *property) string {
	switch d.cfg.Continuations {
	case ContinuationsIndented:
		for i, part := range p.parts {
			p.parts[i] = strings.TrimSpace(part)
		}
		return strings.TrimRightFunc(strings.Join(p.parts, d.sep), unicode.IsSpace)
	case ContinuationsBackslash:
		last := len(p.parts) - 1
		p.parts[last] = strings.TrimRightFunc(p.parts[last], unicode.IsSpace)
		return strings.TrimLeftFunc(strings.Join(p.parts, d.sep), unicode.IsSpace)
	}
	return strings.TrimSpace(strings.Join(p.parts, d.sep))
}

// splitLines splits data into its lines, dropping a carriage return before
// each newline but counting it in the offset.
func splitLines(data string) []line {
	var lines []line
	offset := 0
	for text := range strings.SplitSeq(data, "\n") {
		lines = append(lines, line{text: strings.TrimSuffix(text, "\r"), offset: offset})
		offset += len(text) + 1
	}
	return lines
}

// joinIndented extends p's value with the lines after line i that are
// indented further than its key, returning the index of the last line it
// consumed. A blank line between two such lines is an empty line of the
// value.
func (d *Decoder) joinIndented(p *property, i int) int {
	for next := d.nextIndented(p, i); next >= 0; next = d.nextIndented(p, i) {
		for i++; i < next; i++ {
			if strings.TrimSpace(d.lines[i].text) == "" {
				p.parts = append(p.parts, "")
			}
		}
		d.addFragment(p, d.lines[i].text, i)
		p.bare = false
	}
	return i
}

// indentOf returns the length of the leading white space of text.
func indentOf(text string) int {
	return len(text) - len(strings.TrimLeftFunc(text, unicode.IsSpace))
}

// nextIndented returns the index of the line after line i that continues p's
// value under [ContinuationsIndented], or -1 when the value ends at line i.
// Neither a comment line nor a blank line ends the value, so either may sit
// in the middle of one, as configparser reads it.
func (d *Decoder) nextIndented(p *property, i int) int {
	for next := i + 1; next < len(d.lines); next++ {
		text := d.lines[next].text
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || isComment(trimmed) {
			continue
		}
		if indentOf(text) <= p.indent {
			break
		}
		return next
	}
	return -1
}

// joinContinuations extends p's value with the lines that a trailing
// backslash continues it onto, returning the index of the last line it
// consumed. continued reports whether the value so far ends in such a
// backslash; since every fragment has had its inline comment removed
// already, a backslash inside a comment continues nothing.
//
// The backslash is dropped under [ContinuationsBackslash] and becomes a
// space under [ContinuationsBackslashSpace]. The text is appended as it
// stands, indentation included, and an inline comment within it ends the
// value. A blank line or the end of the input after the backslash ends the
// value, as git and systemd both read it; an empty last fragment then stands
// for that line, so that [Decoder.value] keeps the whitespace before the
// backslash.
func (d *Decoder) joinContinuations(p *property, continued bool, i int) int {
	for continued {
		next := d.nextBackslashed(i)
		if next < 0 {
			p.parts = append(p.parts, "")
			break
		}
		i = next
		continued = d.addFragment(p, d.lines[i].text, i)
	}
	return i
}

// nextBackslashed returns the index of the line that a backslash ending line
// i continues onto, or -1 when a blank line or the end of the input follows
// it. [ContinuationsBackslashSpace] skips a block of whole-line comments
// between the two.
func (d *Decoder) nextBackslashed(i int) int {
	for next := i + 1; next < len(d.lines); next++ {
		trimmed := strings.TrimSpace(d.lines[next].text)
		if trimmed == "" {
			return -1
		}
		if d.cfg.Continuations != ContinuationsBackslashSpace || !isComment(trimmed) {
			return next
		}
	}
	return -1
}

// closesLater reports whether quote, left unmatched by the rest of text, the
// fragment of p's value from line i, is matched on a line continuing the
// value. Within the span the quote would open there is no comment, so a
// backslash ending text continues the value.
//
// A search that fails reads only lines holding no such quote, where no later
// search can start, and one that succeeds reads only lines the span then
// covers, so decoding stays linear in the size of the input.
func (d *Decoder) closesLater(p *property, text string, i int, quote byte) bool {
	switch d.cfg.Continuations {
	case ContinuationsIndented:
		for i = d.nextIndented(p, i); i >= 0; i = d.nextIndented(p, i) {
			if d.cfg.closingQuote(d.lines[i].text, 0, quote) >= 0 {
				return true
			}
		}
	case ContinuationsBackslash, ContinuationsBackslashSpace:
		for continues(text) {
			if i = d.nextBackslashed(i); i < 0 {
				return false
			}
			text = d.lines[i].text
			if d.cfg.closingQuote(text, 0, quote) >= 0 {
				return true
			}
		}
	}
	return false
}

// continues reports whether text ends in a line continuation: a backslash
// that the one before it does not escape. A run of trailing backslashes
// therefore continues the value only when its length is odd.
func continues(text string) bool {
	n := 0
	for n < len(text) && text[len(text)-1-n] == '\\' {
		n++
	}
	return n%2 == 1
}

// isComment reports whether a trimmed line is a whole-line comment.
func isComment(trimmed string) bool {
	return trimmed != "" && (trimmed[0] == ';' || trimmed[0] == '#')
}

// addProperty decodes p's value and adds it to its section.
func (d *Decoder) addProperty(p *property) error {
	v, err := d.decodeValue(p)
	if err != nil {
		return err
	}
	name := d.cfg.fold(p.key)
	if p.sec.children[name] != nil {
		return errors.Newf(p.keyPos, "property %s conflicts with section of the same name", p.key)
	}
	if field := p.sec.props[name]; field != nil {
		return d.addDuplicate(p, field, v)
	}
	field := &ast.Field{
		Label:    makeLabel(p.key, p.keyPos),
		Value:    v,
		TokenPos: p.keyPos,
	}
	p.sec.fields.Elts = append(p.sec.fields.Elts, field)
	p.sec.props[name] = field
	return nil
}

// decodeValue turns p's raw value text into a CUE expression. The value
// carries its own position, so that an evaluator conflict points into the
// value rather than at the line.
func (d *Decoder) decodeValue(p *property) (ast.Expr, error) {
	if p.bare {
		var v ast.Expr = ast.NewNull()
		if d.cfg.BareKeys == BareKeysTrue {
			v = ast.NewBool(true)
		}
		ast.SetPos(v, p.valuePos)
		return v, nil
	}
	value, quoted, err := d.cfg.applyQuotes(d.value(p))
	if err != nil {
		return nil, errors.Newf(p.valuePos, "%v", err)
	}
	if d.cfg.Values != ValuesTyped || quoted {
		return newStringLit(value, p.valuePos), nil
	}
	return d.makeValueLit(value, p.valuePos), nil
}

// parseKeyValue splits text, a line without its indentation, at the first
// of [Config.Delimiters]. It returns the trimmed key, the value with its
// leading whitespace removed, the index of the value within text, whether the
// line held no delimiter at all, and whether the line is a property. A
// comment starting before any delimiter ends the line, leaving a bare key.
func (d *Decoder) parseKeyValue(text string) (key, value string, valueIdx int, bare, ok bool) {
	i := strings.IndexAny(text, d.cfg.Delimiters)
	keyEnd := i
	if keyEnd < 0 {
		keyEnd = len(text)
	}
	if c := d.cfg.keyComment(text[:keyEnd]); c >= 0 {
		i, keyEnd = -1, c
	}
	key = strings.TrimSpace(text[:keyEnd])
	if i < 0 {
		switch d.cfg.BareKeys {
		case BareKeysNull, BareKeysTrue:
			return key, "", len(key), true, true
		}
		return "", "", 0, false, false
	}
	if key == "" {
		return "", "", 0, false, false
	}
	value = strings.TrimLeft(text[i+1:], " \t")
	return key, value, len(text) - len(value), false, true
}

// stripInlineComment trims text, the fragment of p's value from line, before
// the comment [Config.commentStart] finds in it. prev is the byte before text
// within the value, zero at the start of one. It returns the text to keep and
// the quote left open after it, which p.open holds for the next fragment.
func (d *Decoder) stripInlineComment(p *property, text string, line int, prev byte) (string, byte) {
	start, open := d.cfg.commentStart(text, prev, p.open, func(quote byte) bool {
		return d.closesLater(p, text, line, quote)
	})
	if start >= 0 {
		return strings.TrimRight(text[:start], " \t"), open
	}
	return text, open
}

// sectionClose returns the index of the "]" closing a section header, or -1.
// Under [Config.TrailingHeaderText] that is the last one on the line. A
// quoted subsection name may hold a "]", so the scan otherwise skips quoted
// spans, and the escapes within them, when [Config.QuotedSubsections] is set.
func (d *Decoder) sectionClose(trimmed string) int {
	if d.cfg.TrailingHeaderText {
		return strings.LastIndexByte(trimmed, ']')
	}
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
	if rest := strings.TrimSpace(trimmed[closeIdx+1:]); rest != "" && !d.cfg.TrailingHeaderText && !(d.cfg.inlineComments() && isComment(rest)) {
		return nil, errors.Newf(pos, "unexpected text after section header: %s", rest)
	}
	name := strings.TrimSpace(trimmed[1:closeIdx])
	if name == "" {
		return nil, errors.Newf(pos, "empty section name")
	}
	parts, quoted, err := d.sectionPath(name, pos)
	if err != nil {
		return nil, err
	}
	return d.buildNestedSection(top, parts, quoted, pos)
}

// sectionPath splits a section name into the path of struct fields it names.
// Dots separate nested sections only under [Config.DottedSections], and a
// quoted subsection name contributes exactly one segment, never case-folded,
// only under [Config.QuotedSubsections]; quoted reports whether the last
// segment is such a name.
func (d *Decoder) sectionPath(name string, pos token.Pos) (parts []string, quoted bool, _ error) {
	base, sub := name, ""
	if d.cfg.QuotedSubsections {
		if i := strings.IndexByte(name, '"'); i >= 0 {
			var err error
			if sub, err = subsection(name[i:]); err != nil {
				return nil, false, errors.Newf(pos, "%v: %s", err, name)
			}
			base, quoted = strings.TrimSpace(name[:i]), true
		}
	}
	parts = []string{base}
	if d.cfg.DottedSections {
		parts = strings.Split(base, ".")
	}
	for i, part := range parts {
		if part == "" {
			return nil, false, errors.Newf(pos, "empty section name")
		}
		parts[i] = d.cfg.sectionName(part)
	}
	if quoted {
		parts = append(parts, sub)
	}
	return parts, quoted, nil
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
// one; quoted reports whether the last segment is a quoted subsection name,
// which [Config.sectionKey] leaves alone. An error is returned if any segment
// collides with a property in its parent, or if the header repeats under
// [DuplicateSectionsError]. Under [DuplicateSectionsFirst] a repeated header
// returns a section attached to nothing, so that its properties are read and
// then dropped.
func (d *Decoder) buildNestedSection(top *section, parts []string, quoted bool, pos token.Pos) (*section, error) {
	cur := top
	for i, part := range parts {
		name := d.cfg.sectionKey(part, quoted && i == len(parts)-1)
		if child := cur.children[name]; child != nil {
			cur = child
			continue
		}
		if cur.props[name] != nil {
			return nil, errors.Newf(pos, "section %s conflicts with property of the same name", part)
		}
		inner := &ast.StructLit{}
		cur.fields.Elts = append(cur.fields.Elts, &ast.Field{
			Label:    makeLabel(part, pos),
			Value:    inner,
			TokenPos: pos,
		})
		child := newSection(inner)
		cur.children[name] = child
		cur = child
	}
	if cur.headed {
		switch d.cfg.DuplicateSections {
		case DuplicateSectionsError:
			return nil, errors.Newf(pos, "duplicate section: %s", strings.Join(parts, "."))
		case DuplicateSectionsFirst:
			return newSection(&ast.StructLit{}), nil
		}
	}
	cur.headed = true
	return cur, nil
}

// applyQuotes removes the quotation marks [Config.Quotes] gives meaning to
// and, under [QuotesEscaped], interprets the value's escape sequences. It
// reports whether it removed a quote, which keeps the value a string under
// [ValuesTyped].
func (cfg Config) applyQuotes(value string) (_ string, quoted bool, _ error) {
	switch cfg.Quotes {
	case QuotesStripped:
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			return value[1 : len(value)-1], true, nil
		}
	case QuotesEscaped:
		return unquote(value)
	}
	return value, false, nil
}

// escapes pairs each byte that [QuotesEscaped] spells as an escape sequence
// with the byte following the backslash.
var escapes = [...][2]byte{{'\\', '\\'}, {'"', '"'}, {'\n', 'n'}, {'\t', 't'}, {'\b', 'b'}}

// unescape returns the byte that a backslash followed by e stands for.
func unescape(e byte) (_ byte, ok bool) {
	for _, pair := range escapes {
		if pair[1] == e {
			return pair[0], true
		}
	}
	return 0, false
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
			c, ok := unescape(value[i])
			if !ok {
				return "", false, fmt.Errorf("unknown escape sequence: \\%c", value[i])
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	if open {
		return "", false, fmt.Errorf("unterminated quoted value: %s", value)
	}
	return b.String(), quoted, nil
}

// addDuplicate folds a repeated occurrence of p's key into the field already
// decoded for it, as [Config.DuplicateKeys] selects.
func (d *Decoder) addDuplicate(p *property, field *ast.Field, v ast.Expr) error {
	switch d.cfg.DuplicateKeys {
	case DuplicatesList:
		list, ok := field.Value.(*ast.ListLit)
		if !ok {
			list = ast.NewList(field.Value)
			ast.SetPos(list, field.Value.Pos())
			field.Value = list
		}
		list.Elts = append(list.Elts, v)
	case DuplicatesFirst:
		// The value already decoded wins.
	case DuplicatesLast:
		field.Value = v
	default:
		return errors.Newf(p.keyPos, "duplicate key: %s", p.key)
	}
	return nil
}

// makeValueLit returns a bool, number, or string literal depending on the
// value. Booleans are consulted before numbers, so under [BooleansExtended]
// the values 1 and 0 are booleans rather than integers.
//
// For example:
//   - port=443 -> port is parsed as an int
//   - portString="443" -> portString stays as a string "443"
func (d *Decoder) makeValueLit(s string, pos token.Pos) ast.Expr {
	if b, ok := d.cfg.parseBool(s); ok {
		lit := ast.NewBool(b)
		ast.SetPos(lit, pos)
		return lit
	}
	if text, isInt, ok := jsonNumber(s); ok {
		kind := token.FLOAT
		if isInt {
			kind = token.INT
		}
		lit := ast.NewLit(kind, text)
		ast.SetPos(lit, pos)
		return lit
	}
	return newStringLit(s, pos)
}

// parseBool recognizes the boolean vocabulary [Config.Booleans] selects,
// ignoring case.
func (cfg Config) parseBool(s string) (_ bool, ok bool) {
	extended := cfg.Booleans == BooleansExtended
	switch strings.ToLower(s) {
	case "true":
		return true, true
	case "false":
		return false, true
	case "yes", "on", "1":
		return true, extended
	case "no", "off", "0":
		return false, extended
	}
	return false, false
}

// jsonNumber reports whether s matches the JSON number grammar, which is the
// subset of number syntax the INI flavors share, and whether it has neither
// a fraction nor an exponent. The returned text is a valid CUE literal, so a
// leading "+", which CUE has no literal for, is dropped.
func jsonNumber(s string) (text string, isInt, ok bool) {
	unsigned := s
	if s != "" && (s[0] == '+' || s[0] == '-') {
		unsigned = s[1:]
	}
	// Starting and ending with a digit rules out every JSON value other
	// than a number, and the white space [json.Valid] accepts around one.
	if unsigned == "" || !isDigit(unsigned[0]) || !isDigit(unsigned[len(unsigned)-1]) || !json.Valid([]byte(unsigned)) {
		return "", false, false
	}
	return strings.TrimPrefix(s, "+"), !strings.ContainsAny(unsigned, ".eE"), true
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

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
