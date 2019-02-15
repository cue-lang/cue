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

package cue

import (
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"github.com/cockroachdb/apd"
)

// errRange indicates that a value is out of range for the target type.
var errRange = errors.New("value out of range")

// errSyntax indicates that a value does not have the right syntax for the
// target type.
var errSyntax = errors.New("invalid syntax")

var errInvalidString = errors.New("invalid string")

// Unquote interprets s as a single- or double-quoted, single- or multi-line
// string, possibly with custom escape delimiters, returning the string value
// that s quotes.
func Unquote(s string) (string, error) {
	info, nStart, _, err := ParseQuotes(s, s)
	if err != nil {
		return "", err
	}
	s = s[nStart:]
	return info.Unquote(s)
}

// Unquote unquotes the given string. It must be terminated with a quote or an
// interpolation start.
func (q QuoteInfo) Unquote(s string) (string, error) {
	if len(s) > 0 && !q.multiline {
		if contains(s, '\n') || contains(s, '\r') {
			return "", errSyntax
		}
		// Is it trivial? Avoid allocation.
		if s[len(s)-1] == q.char &&
			q.numHash == 0 &&
			!contains(s, '\\') &&
			!contains(s[:len(s)-1], q.char) {
			return s[:len(s)-1], nil
		}
	}

	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2) // Try to avoid more allocations.
	stripNL := false
	for len(s) > 0 {
		switch s[0] {
		case '\r':
			s = s[1:]
			continue
		case '\n':
			switch {
			case !q.multiline:
				fallthrough
			default:
				return "", errInvalidWhitespace
			case strings.HasPrefix(s[1:], q.whitespace):
				s = s[1+len(q.whitespace):]
			case strings.HasPrefix(s[1:], "\n"):
				s = s[1:]
			}
			stripNL = true
			buf = append(buf, '\n')
			continue
		}
		c, multibyte, ss, err := unquoteChar(s, q)
		if err != nil {
			return "", err
		}
		// TODO: handle surrogates: if we have a left-surrogate, expect the
		// next value to be a right surrogate. Otherwise this is an error.
		s = ss
		if c < 0 {
			if c == -2 {
				stripNL = false
			}
			if stripNL {
				// Strip the last newline, but only if it came from a closing
				// quote.
				buf = buf[:len(buf)-1]
			}
			return string(buf), nil
		}
		stripNL = false
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
	}
	// allow unmatched quotes if already checked.
	return "", errUnmatchedQuote
}

// contains reports whether the string contains the byte c.
func contains(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

// unquoteChar decodes the first character or byte in the escaped string.
// It returns four values:
//
//	1) value, the decoded Unicode code point or byte value; the special value
//     of -1 indicates terminated by quotes and -2 means terminated by \(.
//	2) multibyte, a boolean indicating whether the decoded character requires a multibyte UTF-8 representation;
//	3) tail, the remainder of the string after the character; and
//	4) an error that will be nil if the character is syntactically valid.
//
// The second argument, kind, specifies the type of literal being parsed
// and therefore which kind of escape sequences are permitted.
// For kind 's' only JSON escapes and \u{ are permitted.
// For kind 'b' also hexadecimal and octal escape sequences are permitted.
//
// The third argument, quote, specifies that an ASCII quoting character that
// is not permitted in the output.
func unquoteChar(s string, info QuoteInfo) (value rune, multibyte bool, tail string, err error) {
	// easy cases
	switch c := s[0]; {
	case c == info.char && info.char != 0:
		for i := 1; byte(i) < info.numChar; i++ {
			if i >= len(s) || s[i] != info.char {
				return rune(info.char), false, s[1:], nil
			}
		}
		for i := 0; i < info.numHash; i++ {
			if i+int(info.numChar) >= len(s) || s[i+int(info.numChar)] != '#' {
				return rune(info.char), false, s[1:], nil
			}
		}
		if ln := int(info.numChar) + info.numHash; len(s) != ln {
			// TODO: terminating quote in middle of string
			return 0, false, s[ln:], errSyntax
		}
		return -1, false, "", nil
	case c >= utf8.RuneSelf:
		r, size := utf8.DecodeRuneInString(s)
		return r, true, s[size:], nil
	case c != '\\':
		return rune(s[0]), false, s[1:], nil
	}

	if len(s) <= 1+info.numHash {
		return '\\', false, s[1:], nil
	}
	for i := 1; i <= info.numHash && i < len(s); i++ {
		if s[i] != '#' {
			return '\\', false, s[1:], nil
		}
	}

	c := s[1+info.numHash]
	s = s[2+info.numHash:]

	switch c {
	case 'a':
		value = '\a'
	case 'b':
		value = '\b'
	case 'f':
		value = '\f'
	case 'n':
		value = '\n'
	case 'r':
		value = '\r'
	case 't':
		value = '\t'
	case 'v':
		value = '\v'
	case '/':
		value = '/'
	case 'x', 'u', 'U':
		n := 0
		switch c {
		case 'x':
			n = 2
		case 'u':
			n = 4
		case 'U':
			n = 8
		}
		var v rune
		if len(s) < n {
			err = errSyntax
			return
		}
		for j := 0; j < n; j++ {
			x, ok := unhex(s[j])
			if !ok {
				err = errSyntax
				return
			}
			v = v<<4 | x
		}
		s = s[n:]
		if c == 'x' {
			if info.char == '"' {
				err = errSyntax
				return
			}
			// single-byte string, possibly not UTF-8
			value = v
			break
		}
		if v > utf8.MaxRune {
			err = errSyntax
			return
		}
		value = v
		multibyte = true
	case '0', '1', '2', '3', '4', '5', '6', '7':
		if info.char == '"' {
			err = errSyntax
			return
		}
		v := rune(c) - '0'
		if len(s) < 2 {
			err = errSyntax
			return
		}
		for j := 0; j < 2; j++ { // one digit already; two more
			x := rune(s[j]) - '0'
			if x < 0 || x > 7 {
				err = errSyntax
				return
			}
			v = (v << 3) | x
		}
		s = s[2:]
		if v > 255 {
			err = errSyntax
			return
		}
		value = v
	case '\\':
		value = '\\'
	case '\'', '"':
		// TODO: should we allow escaping of quotes regardless?
		if c != info.char {
			err = errSyntax
			return
		}
		value = rune(c)
	case '(':
		if s != "" {
			// TODO: terminating quote in middle of string
			return 0, false, s, errSyntax
		}
		value = -2
	default:
		err = errSyntax
		return
	}
	tail = s
	return
}

func unhex(b byte) (v rune, ok bool) {
	c := rune(b)
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return
}

type numInfo struct {
	rep multiplier
	k   kind
}

func newNumInfo(k kind, m multiplier, base int, sep bool) numInfo {
	switch base {
	case 10:
		m |= base10
	case 2:
		m |= base2
		k = intKind
	case 8:
		m |= base8
		k = intKind
	case 16:
		m |= base16
		k = intKind
	}
	if sep {
		m |= hasSeparators
	}
	return numInfo{m, k}
}

func unifyNuminfo(a, b numInfo) numInfo {
	k := unifyType(a.k, b.k)
	return numInfo{a.rep | b.rep, k}
}

func (n numInfo) isValid() bool          { return n.k != bottomKind }
func (n numInfo) multiplier() multiplier { return n.rep & (hasSeparators - 1) }

type multiplier uint16

const (
	mul1 multiplier = 1 << iota
	mul2
	mul3
	mul4
	mul5
	mul6
	mul7
	mul8

	mulBin
	mulDec

	// _ 3 for dec, 4 for hex. Maybe support first and rest, like CLDR.
	hasSeparators

	base2
	base8
	base10
	base16

	mulK = mulDec | mul1
	mulM = mulDec | mul2
	mulG = mulDec | mul3
	mulT = mulDec | mul4
	mulP = mulDec | mul5
	mulE = mulDec | mul6
	mulZ = mulDec | mul7
	mulY = mulDec | mul8

	mulKi = mulBin | mul1
	mulMi = mulBin | mul2
	mulGi = mulBin | mul3
	mulTi = mulBin | mul4
	mulPi = mulBin | mul5
	mulEi = mulBin | mul6
	mulZi = mulBin | mul7
	mulYi = mulBin | mul8
)

type litParser struct {
	ctx  *context
	node *ast.BasicLit
	src  string
	p    int
	// pDot   int // first position after the dot, if any
	ch     byte
	useSep bool
	buf    []byte
	err    value
}

func (p *litParser) error(l ast.Node, args ...interface{}) value {
	return p.ctx.mkErr(newNode(l), args...)
}

func (p *litParser) next() bool {
	if p.p >= len(p.src) {
		p.ch = 0
		return false
	}
	p.ch = p.src[p.p]
	p.p++
	if p.ch == '.' {
		p.buf = append(p.buf, '.')

	}
	return true
}

func (p *litParser) init(l *ast.BasicLit) (err value) {
	s := l.Value
	b := p.buf
	*p = litParser{ctx: p.ctx, node: l, src: s}
	p.buf = b[:0]
	if !p.next() {
		return p.error(l, "invalid literal %q", s)
	}
	return nil
}

func (p *litParser) parse(l *ast.BasicLit) (n value) {
	s := l.Value
	switch s {
	case "null":
		return &nullLit{newExpr(l)}
	case "true":
		return &boolLit{newExpr(l), true}
	case "false":
		return &boolLit{newExpr(l), false}
	}
	if err := p.init(l); err != nil {
		return err
	}
	switch p.ch {
	case '"', '\'', '`', '#':
		info, nStart, _, err := ParseQuotes(s, s)
		if err != nil {
			return p.error(l, err.Error())
		}
		s := p.src[nStart:]
		return parseString(p.ctx, p.node, info, s)
	case '.':
		p.next()
		n = p.scanNumber(true)
	default:
		n = p.scanNumber(false)
	}
	if p.err != nil {
		return p.err
	}
	if p.p < len(p.src) {
		return p.error(l, "invalid number")
	}
	return n
}

var (
	errStringTooShort    = errors.New("invalid string: too short")
	errInvalidWhitespace = errors.New("invalid string: invalid whitespace")
	errMissingNewline    = errors.New(
		"invalid string: opening quote of multiline string must be followed by newline")
	errUnmatchedQuote = errors.New("invalid string: unmatched quote")
)

// QuoteInfo describes the type of quotes used for a string.
type QuoteInfo struct {
	quote      string
	whitespace string
	numHash    int
	multiline  bool
	char       byte
	numChar    byte
}

// IsDouble reports whether the literal uses double quotes.
func (q QuoteInfo) IsDouble() bool {
	return q.char == '"'
}

// ParseQuotes checks if the opening quotes in start matches the ending quotes
// in end and reports its type as q or an error if they do not matching or are
// invalid. nStart indicates the number of bytes used for the opening quote.
func ParseQuotes(start, end string) (q QuoteInfo, nStart, nEnd int, err error) {
	for i, c := range start {
		if c != '#' {
			break
		}
		q.numHash = i + 1
	}
	if len(start) < 2+2*q.numHash {
		return q, 0, 0, errStringTooShort
	}
	s := start[q.numHash:]
	switch s[0] {
	case '"', '\'':
		q.char = s[0]
		if len(s) > 3 && s[1] == s[0] && s[2] == s[0] {
			switch s[3] {
			case '\n':
				q.quote = start[:3+q.numHash]
			case '\r':
				if len(s) > 4 && s[4] == '\n' {
					q.quote = start[:4+q.numHash]
					break
				}
				fallthrough
			default:
				return q, 0, 0, errMissingNewline
			}
			q.multiline = true
			q.numChar = 3
			nStart = len(q.quote) + 1 // add whitespace later
		} else {
			q.quote = start[:1+q.numHash]
			q.numChar = 1
			nStart = len(q.quote)
		}
	default:
		return q, 0, 0, errSyntax
	}
	quote := start[:int(q.numChar)+q.numHash]
	for i := 0; i < len(quote); i++ {
		if j := len(end) - i - 1; j < 0 || quote[i] != end[j] {
			return q, 0, 0, errUnmatchedQuote
		}
	}
	if q.multiline {
		i := len(end) - len(quote)
		for i > 0 {
			r, size := utf8.DecodeLastRuneInString(end[:i])
			if r == '\n' || !unicode.IsSpace(r) {
				break
			}
			i -= size
		}
		q.whitespace = end[i : len(end)-len(quote)]

		if len(start) > nStart && start[nStart] != '\n' {
			if !strings.HasPrefix(start[nStart:], q.whitespace) {
				return q, 0, 0, errInvalidWhitespace
			}
			nStart += len(q.whitespace)
		}
	}

	return q, nStart, int(q.numChar) + q.numHash, nil
}

// parseString decodes a string without the starting and ending quotes.
func parseString(ctx *context, node ast.Expr, q QuoteInfo, s string) (n value) {
	src := newExpr(node)
	str, err := q.Unquote(s)
	if err != nil {
		return ctx.mkErr(src, err, "invalid string: %v", err)
	}
	if q.IsDouble() {
		return &stringLit{src, str}
	}
	return &bytesLit{src, []byte(str)}
}

func (p *litParser) digitVal(ch byte) (d int) {
	switch {
	case '0' <= ch && ch <= '9':
		d = int(ch - '0')
	case ch == '_':
		p.useSep = true
		return 0
	case 'a' <= ch && ch <= 'f':
		d = int(ch - 'a' + 10)
	case 'A' <= ch && ch <= 'F':
		d = int(ch - 'A' + 10)
	default:
		return 16 // larger than any legal digit val
	}
	return d
}

func (p *litParser) scanMantissa(base int) {
	var last byte
	for p.digitVal(p.ch) < base {
		if p.ch != '_' {
			p.buf = append(p.buf, p.ch)
		}
		last = p.ch
		p.next()
	}
	if last == '_' {
		p.err = p.error(p.node, "illegal '_' in number")
	}
}

func (p *litParser) scanNumber(seenDecimalPoint bool) value {
	// digitVal(s.ch) < 10
	isFloat := false
	base := 10

	if seenDecimalPoint {
		isFloat = true
		p.scanMantissa(10)
		goto exponent
	}

	if p.ch == '0' {
		// int or float
		p.next()
		if p.ch == 'x' || p.ch == 'X' {
			base = 16
			// hexadecimal int
			p.next()
			p.scanMantissa(16)
			if p.p <= 2 {
				// only scanned "0x" or "0X"
				return p.error(p.node, "illegal hexadecimal number %q", p.src)
			}
		} else if p.ch == 'b' {
			base = 2
			// binary int
			p.next()
			p.scanMantissa(2)
			if p.p <= 2 {
				// only scanned "0b"
				return p.error(p.node, "illegal binary number %q", p.src)
			}
		} else if p.ch == 'o' {
			base = 8
			// octal int
			p.next()
			p.scanMantissa(8)
			if p.p <= 2 {
				// only scanned "0o"
				return p.error(p.node, "illegal octal number %q", p.src)
			}
		} else {
			// int or float
			p.scanMantissa(10)
			if p.ch == '.' || p.ch == 'e' {
				goto fraction
			}
		}
		goto exit
	}

	// decimal int or float
	p.scanMantissa(10)

	// TODO: allow 3h4s, etc.
	// switch p.ch {
	// case 'h', 'm', 's', "µ"[0], 'u', 'n':
	// }

fraction:
	if p.ch == '.' {
		isFloat = true
		p.next()
		p.scanMantissa(10)
	}

exponent:
	switch p.ch {
	case 'K', 'M', 'G', 'T', 'P', 'E', 'Z', 'Y':
		mul := charToMul[p.ch]
		p.next()
		if p.ch == 'i' {
			mul |= mulBin
			p.next()
		} else {
			mul |= mulDec
		}
		n := &numLit{
			numBase: newNumBase(p.node, newNumInfo(numKind, mul, 10, p.useSep)),
		}
		n.v.UnmarshalText(p.buf)
		p.ctx.Mul(&n.v, &n.v, mulToRat[mul])
		cond, _ := p.ctx.RoundToIntegralExact(&n.v, &n.v)
		if cond.Inexact() {
			return p.error(p.node, "number cannot be represented as int")
		}
		return n

	case 'e':
		isFloat = true
		p.next()
		p.buf = append(p.buf, 'e')
		if p.ch == '-' || p.ch == '+' {
			p.buf = append(p.buf, p.ch)
			p.next()
		}
		p.scanMantissa(10)
	}

exit:
	if isFloat {
		f := &numLit{
			numBase: newNumBase(p.node, newNumInfo(floatKind, 0, 10, p.useSep)),
		}
		f.v.UnmarshalText(p.buf)
		return f
	}
	i := &numLit{numBase: newNumBase(p.node, newNumInfo(numKind, 0, base, p.useSep))}
	i.v.Coeff.SetString(string(p.buf), base)
	return i
}

type mulInfo struct {
	fact *big.Rat
	mul  multiplier
}

var charToMul = map[byte]multiplier{
	'K': mul1,
	'M': mul2,
	'G': mul3,
	'T': mul4,
	'P': mul5,
	'E': mul6,
	'Z': mul7,
	'Y': mul8,
}

var mulToRat = map[multiplier]*apd.Decimal{}

func init() {
	d := apd.New(1, 0)
	b := apd.New(1, 0)
	dm := apd.New(1000, 0)
	bm := apd.New(1024, 0)

	c := apd.BaseContext
	for i := uint(0); int(i) < len(charToMul); i++ {
		// TODO: may we write to one of the sources?
		var bn, dn apd.Decimal
		c.Mul(&dn, d, dm)
		d = &dn
		c.Mul(&bn, b, bm)
		b = &bn
		mulToRat[mulDec|1<<i] = d
		mulToRat[mulBin|1<<i] = b
	}
}
