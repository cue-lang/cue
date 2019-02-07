// Copyright 2018 The CUE Authors
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

// Package scanner implements a scanner for CUE source text. It takes a []byte
// as source which can then be tokenized through repeated calls to the Scan
// method.
package scanner // import "cuelang.org/go/cue/scanner"

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// A Scanner holds the Scanner's internal state while processing
// a given text. It can be allocated as part of another data
// structure but must be initialized via Init before use.
type Scanner struct {
	// immutable state
	file *token.File    // source file handle
	dir  string         // directory portion of file.Name()
	src  []byte         // source
	err  errors.Handler // error reporting; or nil
	mode Mode           // scanning mode

	// scanning state
	ch              rune // current character
	offset          int  // character offset
	rdOffset        int  // reading offset (position after current character)
	lineOffset      int  // current line offset
	linesSinceLast  int
	spacesSinceLast int
	insertEOL       bool // insert a comma before next newline

	// public state - ok to modify
	ErrorCount int // number of errors encountered
}

const bom = 0xFEFF // byte order mark, only permitted as very first character

// Read the next Unicode char into s.ch.
// s.ch < 0 means end-of-file.
func (s *Scanner) next() {
	if s.rdOffset < len(s.src) {
		s.offset = s.rdOffset
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.file.AddLine(s.offset)
		}
		r, w := rune(s.src[s.rdOffset]), 1
		switch {
		case r == 0:
			s.error(s.offset, "illegal character NUL")
		case r >= utf8.RuneSelf:
			// not ASCII
			r, w = utf8.DecodeRune(s.src[s.rdOffset:])
			if r == utf8.RuneError && w == 1 {
				s.error(s.offset, "illegal UTF-8 encoding")
			} else if r == bom && s.offset > 0 {
				s.error(s.offset, "illegal byte order mark")
			}
		}
		s.rdOffset += w
		s.ch = r
	} else {
		s.offset = len(s.src)
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.file.AddLine(s.offset)
		}
		s.ch = -1 // eof
	}
}

// A Mode value is a set of flags (or 0).
// They control scanner behavior.
type Mode uint

// These constants are options to the Init function.
const (
	ScanComments     Mode = 1 << iota // return comments as COMMENT tokens
	dontInsertCommas                  // do not automatically insert commas - for testing only
)

// Init prepares the scanner s to tokenize the text src by setting the
// scanner at the beginning of src. The scanner uses the file set file
// for position information and it adds line information for each line.
// It is ok to re-use the same file when re-scanning the same file as
// line information which is already present is ignored. Init causes a
// panic if the file size does not match the src size.
//
// Calls to Scan will invoke the error handler err if they encounter a
// syntax error and err is not nil. Also, for each error encountered,
// the Scanner field ErrorCount is incremented by one. The mode parameter
// determines how comments are handled.
//
// Note that Init may call err if there is an error in the first character
// of the file.
func (s *Scanner) Init(file *token.File, src []byte, err errors.Handler, mode Mode) {
	// Explicitly initialize all fields since a scanner may be reused.
	if file.Size() != len(src) {
		panic(fmt.Sprintf("file size (%d) does not match src len (%d)", file.Size(), len(src)))
	}
	s.file = file
	s.dir, _ = filepath.Split(file.Name())
	s.src = src
	s.err = err
	s.mode = mode

	s.ch = ' '
	s.offset = 0
	s.rdOffset = 0
	s.lineOffset = 0
	s.insertEOL = false
	s.ErrorCount = 0

	s.next()
	if s.ch == bom {
		s.next() // ignore BOM at file beginning
	}
}

func (s *Scanner) error(offs int, msg string) {
	if s.err != nil {
		s.err(s.file.Position(s.file.Pos(offs, 0)), msg)
	}
	s.ErrorCount++
}

var prefix = []byte("//line ")

func (s *Scanner) interpretLineComment(text []byte) {
	if bytes.HasPrefix(text, prefix) {
		// get filename and line number, if any
		if i := bytes.LastIndex(text, []byte{':'}); i > 0 {
			if line, err := strconv.Atoi(string(text[i+1:])); err == nil && line > 0 {
				// valid //line filename:line comment
				filename := string(bytes.TrimSpace(text[len(prefix):i]))
				if filename != "" {
					filename = filepath.Clean(filename)
					if !filepath.IsAbs(filename) {
						// make filename relative to current directory
						filename = filepath.Join(s.dir, filename)
					}
				}
				// update scanner position
				s.file.AddLineInfo(s.lineOffset+len(text)+1, filename, line) // +len(text)+1 since comment applies to next line
			}
		}
	}
}

func (s *Scanner) scanComment() string {
	// initial '/' already consumed; s.ch == '/' || s.ch == '*'
	offs := s.offset - 1 // position of initial '/'
	hasCR := false

	if s.ch == '/' {
		//-style comment
		s.next()
		for s.ch != '\n' && s.ch >= 0 {
			if s.ch == '\r' {
				hasCR = true
			}
			s.next()
		}
		if offs == s.lineOffset {
			// comment starts at the beginning of the current line
			s.interpretLineComment(s.src[offs:s.offset])
		}
		goto exit
	}

	/*-style comment */
	s.next()
	for s.ch >= 0 {
		ch := s.ch
		if ch == '\r' {
			hasCR = true
		}
		s.next()
		if ch == '*' && s.ch == '/' {
			s.next()
			goto exit
		}
	}

	s.error(offs, "comment not terminated")

exit:
	lit := s.src[offs:s.offset]
	if hasCR {
		// TODO: preserve /r/n
		lit = stripCR(lit)
	}

	return string(lit)
}

func (s *Scanner) findLineEnd() bool {
	// initial '/' already consumed

	defer func(offs int) {
		// reset scanner state to where it was upon calling findLineEnd
		s.ch = '/'
		s.offset = offs
		s.rdOffset = offs + 1
		s.next() // consume initial '/' again
	}(s.offset - 1)

	// read ahead until a newline, EOF, or non-comment token is found
	for s.ch == '/' || s.ch == '*' {
		if s.ch == '/' {
			//-style comment always contains a newline
			return true
		}
		/*-style comment: look for newline */
		s.next()
		for s.ch >= 0 {
			ch := s.ch
			if ch == '\n' {
				return true
			}
			s.next()
			if ch == '*' && s.ch == '/' {
				s.next()
				break
			}
		}
		s.skipWhitespace(0) // s.insertSemi is set
		if s.ch < 0 || s.ch == '\n' {
			return true
		}
		if s.ch != '/' {
			// non-comment token
			return false
		}
		s.next() // consume '/'
	}

	return false
}

func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch >= utf8.RuneSelf && unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	// TODO(mpvl): Is this correct?
	return '0' <= ch && ch <= '9' || ch >= utf8.RuneSelf && unicode.IsDigit(ch)
}

func (s *Scanner) scanIdentifier() string {
	offs := s.offset
	for isLetter(s.ch) || isDigit(s.ch) || s.ch == '_' {
		s.next()
	}
	return string(s.src[offs:s.offset])
}

func digitVal(ch rune) int {
	switch {
	case '0' <= ch && ch <= '9':
		return int(ch - '0')
	case ch == '_':
		return 0
	case 'a' <= ch && ch <= 'f':
		return int(ch - 'a' + 10)
	case 'A' <= ch && ch <= 'F':
		return int(ch - 'A' + 10)
	}
	return 16 // larger than any legal digit val
}

func (s *Scanner) scanMantissa(base int) {
	var last rune
	for digitVal(s.ch) < base {
		last = s.ch
		s.next()
	}
	if last == '_' {
		s.error(s.offset-1, "illegal '_' in number")
	}
}

func (s *Scanner) scanNumber(seenDecimalPoint bool) (token.Token, string) {
	// digitVal(s.ch) < 10
	offs := s.offset
	tok := token.INT

	if seenDecimalPoint {
		offs--
		tok = token.FLOAT
		s.scanMantissa(10)
		goto exponent
	}

	if s.ch == '0' {
		// int or float
		offs := s.offset
		s.next()
		if s.ch == 'x' || s.ch == 'X' {
			// hexadecimal int
			s.next()
			s.scanMantissa(16)
			if s.offset-offs <= 2 {
				// only scanned "0x" or "0X"
				s.error(offs, "illegal hexadecimal number")
			}
		} else if s.ch == 'b' {
			// binary int
			s.next()
			s.scanMantissa(2)
			if s.offset-offs <= 2 {
				// only scanned "0b"
				s.error(offs, "illegal binary number")
			}
		} else if s.ch == 'o' {
			// octal int
			s.next()
			s.scanMantissa(8)
			if s.offset-offs <= 2 {
				// only scanned "0o"
				s.error(offs, "illegal octal number")
			}
		} else {
			// 0 or float
			seenDigits := false
			if s.ch >= '0' && s.ch <= '9' {
				seenDigits = true
				s.scanMantissa(10)
			}
			if s.ch == '.' || s.ch == 'e' {
				goto fraction
			}
			if seenDigits {
				// integer other than 0 may not start with 0
				s.error(offs, "illegal integer number")
			}
		}
		goto exit
	}

	// decimal int or float
	s.scanMantissa(10)

	// TODO: allow 3h4s, etc.
	// switch s.ch {
	// case 'h', 'm', 's', "µ"[0], 'u', 'n':
	// }

fraction:
	if s.ch == '.' {
		if p := s.offset + 1; p < len(s.src) && s.src[p] == '.' {
			// interpret dot as part of a range.
			goto exit
		}
		tok = token.FLOAT
		s.next()
		s.scanMantissa(10)
	}

exponent:
	switch s.ch {
	case 'K', 'M', 'G', 'T', 'P', 'E', 'Z', 'Y':
		tok = token.INT // TODO: Or should we allow this to be a float?
		s.next()
		if s.ch == 'i' {
			s.next()
		}
		goto exit
	}

	// TODO: allow 'E' for exponent? Could be used for Exa
	if s.ch == 'e' { // || s.ch == 'E' {
		tok = token.FLOAT
		s.next()
		if s.ch == '-' || s.ch == '+' {
			s.next()
		}
		s.scanMantissa(10)
	}

exit:
	return tok, string(s.src[offs:s.offset])
}

// scanEscape parses an escape sequence where rune is the accepted
// escaped quote. In case of a syntax error, it stops at the offending
// character (without consuming it) and returns false. Otherwise
// it returns true.
func (s *Scanner) scanEscape(quote rune) (ok, template bool) {
	offs := s.offset

	var n int
	var base, max uint32
	switch s.ch {
	// TODO: remove
	case '(':
		return true, true
	case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', quote:
		s.next()
		return true, false
	case '0', '1', '2', '3', '4', '5', '6', '7':
		n, base, max = 3, 8, 255
	case 'x':
		s.next()
		n, base, max = 2, 16, 255
	case 'u':
		s.next()
		n, base, max = 4, 16, unicode.MaxRune
	case 'U':
		s.next()
		n, base, max = 8, 16, unicode.MaxRune
	default:
		msg := "unknown escape sequence"
		if s.ch < 0 {
			msg = "escape sequence not terminated"
		}
		s.error(offs, msg)
		return false, false
	}

	var x uint32
	for n > 0 {
		d := uint32(digitVal(s.ch))
		if d >= base {
			msg := fmt.Sprintf("illegal character %#U in escape sequence", s.ch)
			if s.ch < 0 {
				msg = "escape sequence not terminated"
			}
			s.error(s.offset, msg)
			return false, false
		}
		x = x*base + d
		s.next()
		n--
	}

	if x > max || 0xD800 <= x && x < 0xE000 {
		s.error(offs, "escape sequence is invalid Unicode code point")
		return false, false
	}

	return true, false
}

func (s *Scanner) scanString(quote rune, offset, numQuotes int) (token.Token, string) {
	// ", """, ', or ''' opening already consumed
	offs := s.offset - offset

	tok := token.STRING

	hasCR := false
	extra := 0
	for {
		ch, n := s.consumeQuotes(quote, numQuotes)
		if n == numQuotes {
			break
		}
		if (numQuotes != 3 && ch == '\n') || ch < 0 {
			s.error(offs, "string literal not terminated")
			lit := s.src[offs:s.offset]
			if hasCR {
				lit = stripCR(lit)
			}
			return tok, string(lit)
		}
		if ch == '\r' && numQuotes == 3 {
			hasCR = true
		}
		s.next()
		if ch == '\\' {
			if s.ch == '(' {
				tok = token.INTERPOLATION
				extra = 1
				break
			}
			s.scanEscape(quote)
		}
	}
	lit := s.src[offs : s.offset+extra]
	if hasCR {
		lit = stripCR(lit)
	}
	return tok, string(lit)
}

func (s *Scanner) consumeQuotes(quote rune, max int) (next rune, n int) {
	for ; n < max; n++ {
		if s.ch != quote {
			return s.ch, n
		}
		s.next()
	}
	return s.ch, n
}

func stripCR(b []byte) []byte {
	c := make([]byte, len(b))
	i := 0
	for _, ch := range b {
		if ch != '\r' {
			c[i] = ch
			i++
		}
	}
	return c[:i]
}

func (s *Scanner) scanRawString() string {
	// '`' opening already consumed
	offs := s.offset - 1

	hasCR := false
	for {
		ch := s.ch
		if ch < 0 {
			s.error(offs, "raw string literal not terminated")
			break
		}
		s.next()
		if ch == '`' {
			break
		}
		if ch == '\r' {
			hasCR = true
		}
	}

	lit := s.src[offs:s.offset]
	if hasCR {
		lit = stripCR(lit)
	}

	return string(lit)
}

func (s *Scanner) skipWhitespace(inc int) {
	for {
		switch s.ch {
		case ' ', '\t':
			s.spacesSinceLast += inc
		case '\n':
			s.linesSinceLast += inc
			if s.insertEOL {
				return
			}
		case '\r':
		default:
			return
		}
		s.next()
	}
}

// Helper functions for scanning multi-byte tokens such as >> += >>= .
// Different routines recognize different length tok_i based on matches
// of ch_i. If a token ends in '=', the result is tok1 or tok3
// respectively. Otherwise, the result is tok0 if there was no other
// matching character, or tok2 if the matching character was ch2.

func (s *Scanner) switch2(tok0, tok1 token.Token) token.Token {
	if s.ch == '=' {
		s.next()
		return tok1
	}
	return tok0
}

// ResumeInterpolation resumes scanning of a string interpolation.
func (s *Scanner) ResumeInterpolation(quote rune, numQuotes int) string {
	_, str := s.scanString(quote, 1, numQuotes)
	return str
}

// Scan scans the next token and returns the token position, the token,
// and its literal string if applicable. The source end is indicated by
// EOF.
//
// If the returned token is a literal (IDENT, INT, FLOAT,
// IMAG, CHAR, STRING) or COMMENT, the literal string
// has the corresponding value.
//
// If the returned token is a keyword, the literal string is the keyword.
//
// If the returned token is Comma, the corresponding
// literal string is "," if the comma was present in the source,
// and "\n" if the semicolon was inserted because of a newline or
// at EOF.
//
// If the returned token is ILLEGAL, the literal string is the
// offending character.
//
// In all other cases, Scan returns an empty literal string.
//
// For more tolerant parsing, Scan will return a valid token if
// possible even if a syntax error was encountered. Thus, even
// if the resulting token sequence contains no illegal tokens,
// a client may not assume that no error occurred. Instead it
// must check the scanner's ErrorCount or the number of calls
// of the error handler, if there was one installed.
//
// Scan adds line information to the file added to the file
// set with Init. Token positions are relative to that file
// and thus relative to the file set.
func (s *Scanner) Scan() (pos token.Pos, tok token.Token, lit string) {
scanAgain:
	s.skipWhitespace(1)

	var rel token.RelPos
	switch {
	case s.linesSinceLast > 1:
		rel = token.NewSection
	case s.linesSinceLast == 1:
		rel = token.Newline
	case s.spacesSinceLast > 0:
		rel = token.Blank
	default:
		rel = token.NoSpace
	}
	// current token start
	offset := s.offset
	pos = s.file.Pos(offset, rel)

	// determine token value
	insertEOL := false
	switch ch := s.ch; {
	// case ch == '$':
	// 	lit = string(rune(ch))
	// 	s.next()
	// 	fallthrough
	case isLetter(ch):
		lit = s.scanIdentifier()
		if len(lit) > 1 {
			// keywords are longer than one letter - avoid lookup otherwise
			tok = token.Lookup(lit)
			switch tok {
			case token.IDENT, token.TRUE, token.FALSE, token.NULL, token.BOTTOM:
				insertEOL = true
			}
		} else {
			insertEOL = true
			tok = token.IDENT
		}
	case '0' <= ch && ch <= '9':
		insertEOL = true
		tok, lit = s.scanNumber(false)
	default:
		s.next() // always make progress
		switch ch {
		case -1:
			if s.insertEOL {
				s.insertEOL = false // EOF consumed
				return s.file.Pos(offset, token.Elided), token.COMMA, "\n"
			}
			tok = token.EOF
		case '_':
			if s.ch == '|' {
				// Unconditionally require this to be followed by another
				// underscore to avoid needing an extra lookahead.
				// Note that `_|x` is always equal to x.
				s.next()
				if s.ch != '_' {
					s.error(s.file.Offset(pos), "illegal token '_|'; expected '_'")
					insertEOL = s.insertEOL // preserve insertComma info
					tok = token.ILLEGAL
					lit = "_|"
					break
				}
				s.next()
				tok = token.BOTTOM
				lit = "_|_"
			} else {
				tok = token.IDENT
				lit = "_" + s.scanIdentifier()
			}
			insertEOL = true
		case '\n':
			// we only reach here if s.insertSemi was
			// set in the first place and exited early
			// from s.skipWhitespace()
			s.insertEOL = false // newline consumed
			return s.file.Pos(offset, token.Elided), token.COMMA, "\n"
		case '"', '\'':
			insertEOL = true
			switch _, n := s.consumeQuotes(ch, 2); n {
			case 1:
				if ch == '"' {
					tok, lit = token.STRING, `""`
				} else {
					tok, lit = token.STRING, `''`
				}
			default:
				tok, lit = s.scanString(ch, n+1, n+1)
			}
		case '`':
			insertEOL = true
			tok = token.STRING
			lit = s.scanRawString()
		case ':':
			tok = token.COLON
		case ';':
			tok = token.SEMICOLON
			insertEOL = true
		case '.':
			if '0' <= s.ch && s.ch <= '9' {
				insertEOL = true
				tok, lit = s.scanNumber(true)
			} else if s.ch == '.' {
				s.next()
				if s.ch == '.' {
					s.next()
					tok = token.ELLIPSIS
				} else {
					tok = token.RANGE
				}
			} else {
				tok = token.PERIOD
			}
		case ',':
			tok = token.COMMA
			lit = ","
		case '(':
			tok = token.LPAREN
		case ')':
			insertEOL = true
			tok = token.RPAREN
		case '[':
			tok = token.LBRACK
		case ']':
			insertEOL = true
			tok = token.RBRACK
		case '{':
			tok = token.LBRACE
		case '}':
			insertEOL = true
			tok = token.RBRACE
		case '+':
			tok = token.ADD // Consider ++ for list concatenate.
		case '-':
			tok = token.SUB
		case '*':
			tok = token.MUL
		case '/':
			if s.ch == '/' || s.ch == '*' {
				// comment
				if s.insertEOL && s.findLineEnd() {
					// reset position to the beginning of the comment
					s.ch = '/'
					s.offset = s.file.Offset(pos)
					s.rdOffset = s.offset + 1
					s.insertEOL = false // newline consumed
					return s.file.Pos(offset, token.Elided), token.COMMA, "\n"
				}
				comment := s.scanComment()
				if s.mode&ScanComments == 0 {
					// skip comment
					s.insertEOL = false // newline consumed
					goto scanAgain
				}
				tok = token.COMMENT
				lit = comment
			} else {
				tok = token.QUO
			}
		case '%':
			tok = token.REM
		case '<':
			if s.ch == '-' {
				s.next()
				tok = token.ARROW
			} else {
				tok = s.switch2(token.LSS, token.LEQ)
			}
		case '>':
			tok = s.switch2(token.GTR, token.GEQ)
		case '=':
			tok = s.switch2(token.BIND, token.EQL)
		case '!':
			tok = s.switch2(token.NOT, token.NEQ)
		case '&':
			switch s.ch {
			case '&':
				s.next()
				tok = token.LAND
			default:
				tok = token.UNIFY
			}
		case '|':
			if s.ch == '|' {
				s.next()
				tok = token.LOR
			} else {
				tok = token.DISJUNCTION
			}
		default:
			// next reports unexpected BOMs - don't repeat
			if ch != bom {
				s.error(s.file.Offset(pos), fmt.Sprintf("illegal character %#U", ch))
			}
			insertEOL = s.insertEOL // preserve insertSemi info
			tok = token.ILLEGAL
			lit = string(ch)
		}
	}
	if s.mode&dontInsertCommas == 0 {
		s.insertEOL = insertEOL
	}

	s.linesSinceLast = 0
	s.spacesSinceLast = 0
	return
}
