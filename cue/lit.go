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

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"github.com/cockroachdb/apd/v2"
)

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
		if len(p.buf) == 0 {
			p.buf = append(p.buf, '0')
		}
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
		info, nStart, _, err := literal.ParseQuotes(s, s)
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

// parseString decodes a string without the starting and ending quotes.
func parseString(ctx *context, node ast.Expr, q literal.QuoteInfo, s string) (n value) {
	src := newExpr(node)
	str, err := q.Unquote(s)
	if err != nil {
		return ctx.mkErr(src, err, "invalid string: %v", err)
	}
	if q.IsDouble() {
		return &stringLit{src, str, nil}
	}
	return &bytesLit{src, []byte(str), nil}
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
		switch p.ch {
		case 'x', 'X':
			base = 16
			// hexadecimal int
			p.next()
			p.scanMantissa(16)
			if p.p <= 2 {
				// only scanned "0x" or "0X"
				return p.error(p.node, "illegal hexadecimal number %q", p.src)
			}
		case 'b':
			base = 2
			// binary int
			p.next()
			p.scanMantissa(2)
			if p.p <= 2 {
				// only scanned "0b"
				return p.error(p.node, "illegal binary number %q", p.src)
			}
		case 'o':
			base = 8
			// octal int
			p.next()
			p.scanMantissa(8)
			if p.p <= 2 {
				// only scanned "0o"
				return p.error(p.node, "illegal octal number %q", p.src)
			}
		default:
			// int (base 8 or 10) or float
			p.scanMantissa(8)
			if p.ch == '.' || p.ch == 'e' || p.ch == '8' || p.ch == '9' {
				p.scanMantissa(10)
				if p.ch != '.' && p.ch != 'e' {
					return p.error(p.node, "illegal octal number %q", p.src)
				}
				goto fraction
			}
			if len(p.buf) > 0 {
				base = 8
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
			numBase: newNumBase(p.node, newNumInfo(intKind, mul, 10, p.useSep)),
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
	i := &numLit{numBase: newNumBase(p.node, newNumInfo(intKind, 0, base, p.useSep))}
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
