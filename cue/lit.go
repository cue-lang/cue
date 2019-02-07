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

// Unquote interprets s as a single-quoted, double-quoted, or backquoted CUE
// string literal, returning the string value that s quotes.
func Unquote(s string) (string, error) {
	quote, err := stringType(s)
	if err != nil {
		return "", err
	}
	prefix, err := wsPrefix(s, quote)
	if err != nil {
		return "", err
	}
	s = s[len(quote) : len(s)-len(quote)]
	return unquote(quote[0], len(quote) == 3, true, prefix, s)
}

// unquote interprets s as a CUE string, where quote identifies the string type:
//    s: Unicode string (normal double quoted strings)
//    b: Binary strings: allows escape sequences that may result in invalid
//       Unicode.
//    r: raw strings.
//
// quote indicates the quote used. This is relevant for raw strings, as they
// may not contain the quoting character itself.
func unquote(quote byte, multiline, first bool, wsPrefix, s string) (string, error) {
	if quote == '`' {
		if contains(s, quote) {
			return "", errSyntax
		}
		if contains(s, '\r') {
			// -1 because we know there is at least one \r to remove.
			buf := make([]byte, 0, len(s)-1)
			for i := 0; i < len(s); i++ {
				if s[i] != '\r' {
					buf = append(buf, s[i])
				}
			}
			return string(buf), nil
		}
		return s, nil
	}
	if !multiline {
		if contains(s, '\n') {
			return "", errSyntax
		}
		// Is it trivial? Avoid allocation.
		if !contains(s, '\\') && !contains(s, quote) {
			return s, nil
		}
	}

	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2) // Try to avoid more allocations.
	for len(s) > 0 {
		switch s[0] {
		case '\r':
			s = s[1:]
			continue
		case '\n':
			switch {
			case !multiline:
				fallthrough
			default:
				return "", errSyntax
			case strings.HasPrefix(s[1:], wsPrefix):
				s = s[1+len(wsPrefix):]
			case strings.HasPrefix(s[1:], "\n"):
				s = s[1:]
			}
			if !first && len(s) > 0 {
				buf = append(buf, '\n')
			}
			first = false
			continue
		}
		c, multibyte, ss, err := unquoteChar(s, quote)
		if err != nil {
			return "", err
		}
		// TODO: handle surrogates: if we have a left-surrogate, expect the
		// next value to be a right surrogate. Otherwise this is an error.
		s = ss
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
	}
	return string(buf), nil
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
//	1) value, the decoded Unicode code point or byte value;
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
func unquoteChar(s string, quote byte) (value rune, multibyte bool, tail string, err error) {
	// easy cases
	switch c := s[0]; {
	case c == quote && quote != 0:
		err = errSyntax
		return
	case c >= utf8.RuneSelf:
		r, size := utf8.DecodeRuneInString(s)
		return r, true, s[size:], nil
	case c != '\\':
		return rune(s[0]), false, s[1:], nil
	}

	// hard case: c is backslash
	if len(s) <= 1 {
		err = errSyntax
		return
	}
	c := s[1]
	s = s[2:]

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
			if quote == '"' {
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
		if quote == '"' {
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
		if c != quote {
			err = errSyntax
			return
		}
		value = rune(c)
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
	case '"', '\'', '`':
		quote, err := stringType(l.Value)
		if err != nil {
			return p.error(l, err.Error())
		}
		ws, err := wsPrefix(l.Value, quote)
		if err != nil {
			return p.error(l, err.Error())
		}
		return p.parseString(quote, quote, ws, len(quote) == 3, quote[0])
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
	errStringTooShort = errors.New("invalid string: too short")
	errMissingNewline = errors.New(
		"invalid string: opening quote of multiline string must be followed by newline")
	errUnmatchedQuote = errors.New("invalid string: unmatched quote")
)

// stringType reports the type of quoting used, being ther a ", ', """, or ''',
// or `.
func stringType(s string) (quote string, err error) {
	if len(s) < 2 {
		return "", errStringTooShort
	}
	switch s[0] {
	case '"', '\'':
		if len(s) > 3 && s[1] == s[0] && s[2] == s[0] {
			if s[3] != '\n' {
				return "", errMissingNewline
			}
			return s[:3], nil
		}
	case '`':
	default:
		return "", errSyntax
	}
	return s[:1], nil
}

func wsPrefix(s, quote string) (ws string, err error) {
	for i := 0; i < len(quote); i++ {
		if j := len(s) - i - 1; j < 0 || quote[i] != s[j] {
			return "", errUnmatchedQuote
		}
	}
	i := len(s) - len(quote)
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if r == '\n' || !unicode.IsSpace(r) {
			break
		}
		i -= size
	}
	return s[i : len(s)-len(quote)], nil
}

func (p *litParser) parseString(prefix, suffix, ws string, multi bool, quote byte) (n value) {
	if len(p.src) < len(prefix)+len(suffix) {
		return p.error(p.node, "invalid string: too short")
	}
	for _, r := range prefix {
		if byte(r) != p.ch {
			return p.error(p.node, "invalid interpolation: expected %q", prefix)
		}
		p.next()
	}
	if !strings.HasSuffix(p.src, suffix) {
		return p.error(p.node, "invalid interpolation: unmatched ')'", suffix)
	}
	start, end := len(prefix), len(p.src)-len(suffix)
	str, err := unquote(quote, multi, len(prefix) == 3, ws, p.src[start:end])
	if err != nil {
		return p.error(p.node, err, "invalid string: %v", err)
	}
	if quote == '"' {
		return &stringLit{newExpr(p.node), str}
	}
	return &bytesLit{newExpr(p.node), []byte(str)}
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
