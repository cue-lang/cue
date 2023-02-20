// Copyright 2023 CUE Authors
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

// Package extern provides a parser for @extern attributes.
package extern

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type tokClass int

//go:generate stringer -linecomment -trimprefix=tok -type=tokClass
const (
	tokErr    tokClass = iota
	tokIdent           // identifier
	tokFunc            // keyword "func"
	tokLParen          // '('
	tokRParen          // ')'
	tokComma           // ','
	tokColon           // ':'
)

type token struct {
	tokClass
	value string
}

func (t token) String() string {
	return fmt.Sprintf("token(%v, %q)", t.tokClass, t.value)
}

func skipSpace(s string) string {
	for i, v := range s {
		if unicode.IsSpace(v) {
			continue
		}
		return s[i:]
	}
	return ""
}

// cutPunctuation takes a string and returns its leading character if
// it belongs to the set "(),:", followed by the rest of the string
// and a boolean indicating if the leading character has been found.
func cutPunctuation(s string) (sym string, rem string, ok bool) {
	if len(s) == 0 {
		return "", s, false
	}

	c, other := string(s[0]), s[1:]
	if ok := strings.ContainsAny(c, "(),:"); ok {
		return c, other, true
	}
	return "", s, false
}

// cutAlpha takes a string and returns its leading alphanumeric prefix,
// if it exists, followed by the rest of the string.
func cutAlpha(s string) (prefix string, after string) {
	for i, v := range s {
		if unicode.IsLetter(v) || unicode.IsDigit(v) {
			continue
		}
		return s[:i], s[i:]
	}
	return s, ""
}

// punctuationTok returns the token corresponding to the symbol c,
// which must belong to the set "(),:".
func punctuationTok(c string) token {
	switch c {
	case "(":
		return token{tokLParen, c}
	case ")":
		return token{tokRParen, c}
	case ",":
		return token{tokComma, c}
	case ":":
		return token{tokColon, c}
	}
	panic("unreachable")
}

// alphaTok returns the token corresponding to the alphanumeric string,
// which could be either a keyword or an identifier.
func alphaTok(s string) token {
	if s == "func" {
		return token{tokFunc, s}
	}
	return token{tokIdent, s}
}

// lexer is a tokenizer for simple function declarations.
type lexer struct {
	whole string // entire input.
	rem   string // input still awaiting processing.
	tok   token  // the current processed token.
	err   error  // the error enountered while tokenizing tok.
}

// newLexer returns a new lexer that tokenizes s.
func newLexer(s string) *lexer {
	return &lexer{
		whole: s,
		rem:   s,
		err:   errors.New("uninitialized lexer"),
	}
}

// Next advances the lexer to the next token, returning it along with
// a boolean indicating success.
func (l *lexer) Next() (token, bool) {
	l.tok, l.rem, l.err = scan(skipSpace(l.rem))
	if l.err != nil {
		return l.tok, false
	}
	return l.tok, true
}

// Peek returns the next token if it exists, or an error otherwise.
// It does not advance the lexer.
func (l *lexer) Peek() (token, error) {
	t, _, err := scan(skipSpace(l.rem))
	return t, err
}

// Err returns the error enountered by Next.
func (l *lexer) Err() error {
	return l.err
}

// scan scans the string for the next token, which it returns if found,
// along with the remainder of the string following the token. The
// error indicates if the string couldn't be tokenized.
func scan(s string) (token, string, error) {
	s = skipSpace(s)
	if len(s) == 0 {
		return token{}, s, io.EOF
	}

	if c, s, ok := cutPunctuation(s); ok {
		return punctuationTok(c), s, nil
	}
	if pre, after := cutAlpha(s); pre != "" {
		return alphaTok(pre), after, nil
	}
	return token{}, s, errors.New("unexpected token")
}

// FuncSig represents a parsed function declaration.
type FuncSig struct {
	Args []string
	Ret  string
}

func sPrintArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return args[0]
	}

	var str strings.Builder
	fmt.Fprintf(&str, args[0])
	for _, v := range args[1:] {
		fmt.Fprintf(&str, ", %v", v)
	}
	return str.String()
}

func (f FuncSig) String() string {
	return fmt.Sprintf("func(%v): %v", sPrintArgs(f.Args), f.Ret)
}

// parser is a parser for simple function declarations.
type parser struct {
	l   *lexer
	err error
}

func newParser(l *lexer) *parser {
	return &parser{l: l}
}

func (p *parser) Err() error {
	return p.err
}

// expects advances the parser in the search for a token of the specified
// class. If the awaited token cannot be found, it records the error
// in the parser.
func (p *parser) expect(c tokClass) token {
	if p.err != nil {
		return token{}
	}
	t, ok := p.l.Next()
	if !ok {
		p.err = fmt.Errorf("expected %v, got %w", c, p.l.Err())
		return t
	}
	if t.tokClass != c {
		p.err = fmt.Errorf("expected %v, got %v", c, t.tokClass)
	}
	return t
}

// isNext peeks into the next token, returning true if it is of the
// specified class.
func (p *parser) isNext(c tokClass) bool {
	if p.err != nil {
		return false
	}
	next, _ := p.l.Peek()
	if next.tokClass == c {
		return true
	}
	return false
}

func (p *parser) peek() (token, bool) {
	if p.err != nil {
		return token{}, false
	}
	t, err := p.l.Peek()
	if err != nil {
		return t, false
	}
	return t, true
}

// alpha := letter | digit .
// ident := alpha { alpha } .
// list  := ident [ { "," ident } ]
func parseIdentList(p *parser) []string {
	t := p.expect(tokIdent)
	list := []string{t.value}

	for p.isNext(tokComma) {
		p.expect(tokComma)
		t := p.expect(tokIdent)
		list = append(list, t.value)
	}
	return list
}

// func	:= "func" "(" [ list ] ")" ":" ident
func parseFn(p *parser) *FuncSig {
	var fn FuncSig

	p.expect(tokFunc)
	p.expect(tokLParen)

	if t, ok := p.peek(); ok && t.tokClass != tokRParen {
		fn.Args = parseIdentList(p)
	}

	p.expect(tokRParen)
	p.expect(tokColon)
	t := p.expect(tokIdent)
	fn.Ret = t.value

	if p.Err() != nil {
		return nil
	}
	return &fn
}

func (p *parser) expectEOF() bool {
	if p.err != nil {
		return false
	}
	t, ok := p.l.Next()
	if ok {
		p.err = fmt.Errorf("expected EOF, got %q", t.value)
		return false
	}

	if p.l.Err() == io.EOF {
		return true
	}
	p.err = p.l.Err()
	return false
}

// ParseOneFuncSig returns the parsed representation of a function
// signature expressed in a @extern() annotation. The string s must
// contain exactly one function signature and nothing else. If the
// string cannot be parsed, the function returns the reason as an
// error.
//
// The function accepts the following grammar:
//
//	letter	:= /* Unicode "Letter" */ .
//	digit	:= /* Unicode "Number, decimal digit" */ .
//	alpha	:= letter | digit .
//	ident	:= alpha { alpha } .
//	list	:= ident [ { "," ident } ]
//	func	:= "func" "(" [ list ] ")" ":" ident
func ParseOneFuncSig(s string) (*FuncSig, error) {
	p := newParser(newLexer(s))
	fn := parseFn(p)
	if err := p.Err(); err != nil {
		return nil, err
	}
	if ok := p.expectEOF(); !ok {
		return nil, p.Err()
	}
	return fn, nil
}
