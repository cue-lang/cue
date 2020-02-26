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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

const base10 literal.Multiplier = 100

type litParser struct {
	ctx *context
	num literal.NumInfo
}

func (p *litParser) parse(l *ast.BasicLit) (n value) {
	ctx := p.ctx
	s := l.Value
	if s == "" {
		return p.ctx.mkErr(newNode(l), "invalid literal %q", s)
	}
	switch l.Kind {
	case token.STRING:
		info, nStart, _, err := literal.ParseQuotes(s, s)
		if err != nil {
			return ctx.mkErr(newNode(l), err.Error())
		}
		s := s[nStart:]
		return parseString(ctx, l, info, s)

	case token.FLOAT, token.INT:
		err := literal.ParseNum(s, &p.num)
		if err != nil {
			return ctx.mkErr(newNode(l), err)
		}
		kind := floatKind
		if p.num.IsInt() {
			kind = intKind
		}
		n := newNum(newExpr(l), kind, 0)
		if err = p.num.Decimal(&n.v); err != nil {
			return ctx.mkErr(newNode(l), err)
		}
		return n

	case token.TRUE:
		return &boolLit{newExpr(l), true}
	case token.FALSE:
		return &boolLit{newExpr(l), false}
	case token.NULL:
		return &nullLit{newExpr(l)}
	default:
		return ctx.mkErr(newExpr(l), "unknown literal type")
	}
}

// parseString decodes a string without the starting and ending quotes.
func parseString(ctx *context, node ast.Expr, q literal.QuoteInfo, s string) (n value) {
	src := newExpr(node)
	str, err := q.Unquote(s)
	if err != nil {
		return ctx.mkErr(src, "invalid string: %v", err)
	}
	if q.IsDouble() {
		return &stringLit{src, str, nil}
	}
	return &bytesLit{src, []byte(str), nil}
}
