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
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

func export(ctx *context, v value) ast.Expr {
	e := exporter{ctx}
	return e.expr(v)
}

type exporter struct {
	ctx *context
}

func (p *exporter) label(f label) *ast.Ident {
	orig := p.ctx.labelStr(f)
	str := strconv.Quote(orig)
	if len(orig)+2 == len(str) {
		str = str[1 : len(str)-1]
	}
	return &ast.Ident{Name: str}
}

func (p *exporter) ident(str string) *ast.Ident {
	return &ast.Ident{Name: str}
}

func (p *exporter) clause(v value) (n ast.Clause, next yielder) {
	switch x := v.(type) {
	case *feed:
		feed := &ast.ForClause{
			Value:  p.label(x.fn.params.arcs[1].feature),
			Source: p.expr(x.source),
		}
		key := x.fn.params.arcs[0]
		if p.ctx.labelStr(key.feature) != "_" {
			feed.Key = p.label(key.feature)
		}
		return feed, x.fn.value.(yielder)

	case *guard:
		return &ast.IfClause{Condition: p.expr(x.condition)}, x.value
	}
	panic(fmt.Sprintf("unsupported clause type %T", v))
}

func (p *exporter) expr(v value) ast.Expr {
	// TODO: also add position information.
	switch x := v.(type) {
	case *builtin:
		return &ast.Ident{Name: x.Name}
	case *nodeRef:
		return nil
	case *selectorExpr:
		n := p.expr(x.x)
		if n == nil {
			return p.label(x.feature)
		}
		return &ast.SelectorExpr{X: n, Sel: p.label(x.feature)}
	case *indexExpr:
		return &ast.IndexExpr{X: p.expr(x.x), Index: p.expr(x.index)}
	case *sliceExpr:
		return &ast.SliceExpr{
			X:    p.expr(x.x),
			Low:  p.expr(x.lo),
			High: p.expr(x.hi),
		}
	case *callExpr:
		call := &ast.CallExpr{Fun: p.expr(x.x)}
		for _, a := range x.args {
			call.Args = append(call.Args, p.expr(a))
		}
		return call
	case *unaryExpr:
		return &ast.UnaryExpr{Op: opMap[x.op], X: p.expr(x.x)}
	case *binaryExpr:
		return &ast.BinaryExpr{
			X:  p.expr(x.left),
			Op: opMap[x.op], Y: p.expr(x.right),
		}
	case *disjunction:
		if len(x.values) == 1 {
			return p.expr(x.values[0].val)
		}
		expr := func(v dValue) ast.Expr {
			e := p.expr(v.val)
			if v.marked {
				e = &ast.UnaryExpr{Op: token.MUL, X: e}
			}
			return e
		}
		bin := &ast.BinaryExpr{
			X:  expr(x.values[0]),
			Op: token.DISJUNCTION,
			Y:  expr(x.values[1]),
		}
		for _, v := range x.values[2:] {
			bin = &ast.BinaryExpr{X: bin, Op: token.DISJUNCTION, Y: expr(v)}
		}
		return bin

	case *structLit:
		obj := &ast.StructLit{}
		if x.emit != nil {
			obj.Elts = append(obj.Elts, &ast.EmitDecl{Expr: p.expr(x.emit)})
		}
		for _, a := range x.arcs {
			obj.Elts = append(obj.Elts, &ast.Field{
				Label: p.label(a.feature),
				Value: p.expr(a.v),
			})
		}
		for _, c := range x.comprehensions {
			var clauses []ast.Clause
			next := c.clauses
			for {
				if yield, ok := next.(*yield); ok {
					f := &ast.Field{
						Label: p.expr(yield.key).(ast.Label),
						Value: p.expr(yield.value),
					}
					var decl ast.Decl = f
					if len(clauses) > 0 {
						decl = &ast.ComprehensionDecl{Field: f, Clauses: clauses}
					}
					obj.Elts = append(obj.Elts, decl)
					break
				}

				var y ast.Clause
				y, next = p.clause(next)
				clauses = append(clauses, y)
			}
		}
		return obj

	case *fieldComprehension:
		panic("should be handled in structLit")

	case *listComprehension:
		var clauses []ast.Clause
		for y, next := p.clause(x.clauses); ; y, next = p.clause(next) {
			clauses = append(clauses, y)
			if yield, ok := next.(*yield); ok {
				return &ast.ListComprehension{
					Expr:    p.expr(yield.value),
					Clauses: clauses,
				}
			}
		}

	case *nullLit:
		return p.ident("null")

	case *boolLit:
		return p.ident(fmt.Sprint(x.b))

	case *stringLit:
		return &ast.BasicLit{
			Kind:  token.STRING,
			Value: quote(x.str, '"'),
		}

	case *bytesLit:
		return &ast.BasicLit{
			Kind:  token.STRING,
			Value: quote(string(x.b), '\''),
		}

	case *numLit:
		if x.k&intKind != 0 {
			return &ast.BasicLit{
				Kind:  token.INT,
				Value: x.v.Text('f'),
			}
		}
		return &ast.BasicLit{
			Kind:  token.FLOAT,
			Value: x.v.Text('g'),
		}

	case *durationLit:
		panic("unimplemented")

	case *rangeLit:
		return &ast.BinaryExpr{
			X:  p.expr(x.from),
			Op: token.RANGE,
			Y:  p.expr(x.to),
		}

	case *interpolation:
		t := &ast.Interpolation{}
		multiline := false
		// TODO: mark formatting in interpolation itself.
		for i := 0; i < len(x.parts); i += 2 {
			str := x.parts[i].(*stringLit).str
			if strings.IndexByte(str, '\n') >= 0 {
				multiline = true
				break
			}
		}
		quote := `"`
		if multiline {
			quote = `"""`
		}
		prefix := quote
		suffix := `\(`
		for i, e := range x.parts {
			if i%2 == 1 {
				t.Elts = append(t.Elts, p.expr(e))
			} else {
				buf := []byte(prefix)
				if i == len(x.parts)-1 {
					suffix = quote
				}
				str := e.(*stringLit).str
				if multiline {
					buf = appendEscapeMulti(buf, str, '"')
				} else {
					buf = appendEscaped(buf, str, '"', true)
				}
				buf = append(buf, suffix...)
				t.Elts = append(t.Elts, &ast.BasicLit{
					Kind:  token.STRING,
					Value: string(buf),
				})
			}
			prefix = ")"
		}
		return t

	case *list:
		list := &ast.ListLit{}
		var expr ast.Expr = list
		for _, e := range x.a {
			list.Elts = append(list.Elts, p.expr(e))
		}
		max := maxNumRaw(x.len)
		num, ok := max.(*numLit)
		if !ok {
			min := minNumRaw(x.len)
			num, _ = min.(*numLit)
		}
		ln := 0
		if num != nil {
			x, _ := num.v.Int64()
			ln = int(x)
		}
		if !ok || ln > len(x.a) {
			list.Type = p.expr(x.typ)
			if !isTop(max) && !isTop(x.typ) {
				expr = &ast.BinaryExpr{
					X: &ast.BinaryExpr{
						X:  p.expr(x.len),
						Op: token.MUL,
						Y: &ast.ListLit{Elts: []ast.Expr{
							p.expr(x.typ),
						}},
					},
					Op: token.UNIFY,
					Y:  list,
				}

			}
		}
		return expr

	case *bottom:
		err := &ast.BottomLit{}
		comment := &ast.Comment{Text: "// " + x.msg}
		err.AddComment(&ast.CommentGroup{
			Line:     true,
			Position: 1,
			List:     []*ast.Comment{comment},
		})
		return err

	case *top:
		return p.ident("_")

	case *basicType:
		return p.ident(x.k.String())

	case *lambdaExpr:
		return p.ident("TODO: LAMBDA")

	default:
		panic(fmt.Sprintf("unimplemented type %T", x))
	}
}

// quote quotes the given string.
func quote(str string, quote byte) string {
	if strings.IndexByte(str, '\n') < 0 {
		buf := []byte{quote}
		buf = appendEscaped(buf, str, quote, true)
		buf = append(buf, quote)
		return string(buf)
	}
	buf := []byte{quote, quote, quote}
	buf = append(buf, multiSep...)
	buf = appendEscapeMulti(buf, str, quote)
	buf = append(buf, quote, quote, quote)
	return string(buf)
}

// TODO: consider the best indent strategy.
const multiSep = "\n        "

func appendEscapeMulti(buf []byte, str string, quote byte) []byte {
	// TODO(perf)
	a := strings.Split(str, "\n")
	for _, s := range a {
		buf = appendEscaped(buf, s, quote, true)
		buf = append(buf, multiSep...)
	}
	return buf
}

const lowerhex = "0123456789abcdef"

func appendEscaped(buf []byte, s string, quote byte, graphicOnly bool) []byte {
	for width := 0; len(s) > 0; s = s[width:] {
		r := rune(s[0])
		width = 1
		if r >= utf8.RuneSelf {
			r, width = utf8.DecodeRuneInString(s)
		}
		if width == 1 && r == utf8.RuneError {
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[s[0]>>4])
			buf = append(buf, lowerhex[s[0]&0xF])
			continue
		}
		buf = appendEscapedRune(buf, r, quote, graphicOnly)
	}
	return buf
}

func appendEscapedRune(buf []byte, r rune, quote byte, graphicOnly bool) []byte {
	var runeTmp [utf8.UTFMax]byte
	if r == rune(quote) || r == '\\' { // always backslashed
		buf = append(buf, '\\')
		buf = append(buf, byte(r))
		return buf
	}
	// TODO(perf): IsGraphic calls IsPrint.
	if strconv.IsPrint(r) || graphicOnly && strconv.IsGraphic(r) {
		n := utf8.EncodeRune(runeTmp[:], r)
		buf = append(buf, runeTmp[:n]...)
		return buf
	}
	switch r {
	case '\a':
		buf = append(buf, `\a`...)
	case '\b':
		buf = append(buf, `\b`...)
	case '\f':
		buf = append(buf, `\f`...)
	case '\n':
		buf = append(buf, `\n`...)
	case '\r':
		buf = append(buf, `\r`...)
	case '\t':
		buf = append(buf, `\t`...)
	case '\v':
		buf = append(buf, `\v`...)
	default:
		switch {
		case r < ' ':
			// Invalid for strings, only bytes.
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[byte(r)>>4])
			buf = append(buf, lowerhex[byte(r)&0xF])
		case r > utf8.MaxRune:
			r = 0xFFFD
			fallthrough
		case r < 0x10000:
			buf = append(buf, `\u`...)
			for s := 12; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		default:
			buf = append(buf, `\U`...)
			for s := 28; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		}
	}
	return buf
}
