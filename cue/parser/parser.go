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

package parser

import (
	"fmt"
	"strings"
	"unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/scanner"
	"cuelang.org/go/cue/token"
)

// The parser structure holds the parser's internal state.
type parser struct {
	file    *token.File
	errors  errors.List
	scanner scanner.Scanner

	// Tracing/debugging
	mode      mode // parsing mode
	trace     bool // == (mode & Trace != 0)
	panicking bool // set if we are bailing out due to too many errors.
	indent    int  // indentation used for tracing output

	// Comments
	leadComment *ast.CommentGroup
	comments    *commentState

	// Next token
	pos token.Pos   // token position
	tok token.Token // one token look-ahead
	lit string      // token literal

	// Error recovery
	// (used to limit the number of calls to syncXXX functions
	// w/o making scanning progress - avoids potential endless
	// loops across multiple parser functions during error recovery)
	syncPos token.Pos // last synchronization position
	syncCnt int       // number of calls to syncXXX without progress

	// Non-syntactic parser control
	exprLev int // < 0: in control clause, >= 0: in expression

	imports []*ast.ImportSpec // list of imports

}

func (p *parser) init(filename string, src []byte, mode []Option) {
	p.file = token.NewFile(filename, -1, len(src))
	for _, f := range mode {
		f(p)
	}
	var m scanner.Mode
	if p.mode&parseCommentsMode != 0 {
		m = scanner.ScanComments
	}
	eh := func(pos token.Pos, msg string, args []interface{}) {
		p.errors.AddNewf(pos, msg, args...)
	}
	p.scanner.Init(p.file, src, eh, m)

	p.trace = p.mode&traceMode != 0 // for convenience (p.trace is used frequently)

	p.comments = &commentState{pos: -1}

	p.next()
}

type commentState struct {
	parent *commentState
	pos    int8
	groups []*ast.CommentGroup

	// lists are not attached to nodes themselves. Enclosed expressions may
	// miss a comment due to commas and line termination. closeLists ensures
	// that comments will be passed to someone.
	isList    int
	lastChild ast.Node
	lastPos   int8
}

// openComments reserves the next doc comment for the caller and flushes
func (p *parser) openComments() *commentState {
	if c := p.comments; c != nil && c.isList > 0 {
		if c.lastChild != nil {
			for _, cg := range c.groups {
				cg.Position = c.lastPos
				c.lastChild.AddComment(cg)
			}
			c.groups = nil
		}
		c.lastChild = nil
	}
	c := &commentState{
		parent: p.comments,
		groups: []*ast.CommentGroup{p.leadComment},
	}
	p.comments = c
	p.leadComment = nil
	return c
}

// openList is used to treat a list of comments as a single comment
// position in a production.
func (p *parser) openList() {
	if p.comments.isList > 0 {
		p.comments.isList++
		return
	}
	c := &commentState{
		parent: p.comments,
		isList: 1,
	}
	p.comments = c
}

func (c *commentState) add(g *ast.CommentGroup) {
	g.Position = c.pos
	c.groups = append(c.groups, g)
}

func (p *parser) closeList() {
	c := p.comments
	if c.lastChild != nil {
		for _, cg := range c.groups {
			cg.Position = c.lastPos
			c.lastChild.AddComment(cg)
		}
		c.groups = nil
	}
	switch c.isList--; {
	case c.isList < 0:
		if !p.panicking {
			panic("unmatched close list")
		}
	case c.isList == 0:
		parent := c.parent
		parent.groups = append(parent.groups, c.groups...)
		parent.pos++
		p.comments = parent
	}
}

func (c *commentState) closeNode(p *parser, n ast.Node) ast.Node {
	if p.comments != c {
		if !p.panicking {
			panic("unmatched comments")
		}
		return n
	}
	p.comments = c.parent
	if c.parent != nil {
		c.parent.lastChild = n
		c.parent.lastPos = c.pos
		c.parent.pos++
	}
	for _, cg := range c.groups {
		if n != nil {
			n.AddComment(cg)
		}
	}
	c.groups = nil
	return n
}

func (c *commentState) closeExpr(p *parser, n ast.Expr) ast.Expr {
	c.closeNode(p, n)
	return n
}

func (c *commentState) closeClause(p *parser, n ast.Clause) ast.Clause {
	c.closeNode(p, n)
	return n
}

// ----------------------------------------------------------------------------
// Parsing support

func (p *parser) printTrace(a ...interface{}) {
	const dots = ". . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . "
	const n = len(dots)
	pos := p.file.Position(p.pos)
	fmt.Printf("%5d:%3d: ", pos.Line, pos.Column)
	i := 2 * p.indent
	for i > n {
		fmt.Print(dots)
		i -= n
	}
	// i <= n
	fmt.Print(dots[0:i])
	fmt.Println(a...)
}

func trace(p *parser, msg string) *parser {
	p.printTrace(msg, "(")
	p.indent++
	return p
}

// Usage pattern: defer un(trace(p, "..."))
func un(p *parser) {
	p.indent--
	p.printTrace(")")
}

// Advance to the next
func (p *parser) next0() {
	// Because of one-token look-ahead, print the previous token
	// when tracing as it provides a more readable output. The
	// very first token (!p.pos.IsValid()) is not initialized
	// (it is ILLEGAL), so don't print it .
	if p.trace && p.pos.IsValid() {
		s := p.tok.String()
		switch {
		case p.tok.IsLiteral():
			p.printTrace(s, p.lit)
		case p.tok.IsOperator(), p.tok.IsKeyword():
			p.printTrace("\"" + s + "\"")
		default:
			p.printTrace(s)
		}
	}

	p.pos, p.tok, p.lit = p.scanner.Scan()
}

// Consume a comment and return it and the line on which it ends.
func (p *parser) consumeComment() (comment *ast.Comment, endline int) {
	// /*-style comments may end on a different line than where they start.
	// Scan the comment for '\n' chars and adjust endline accordingly.
	endline = p.file.Line(p.pos)
	if p.lit[1] == '*' {
		// don't use range here - no need to decode Unicode code points
		for i := 0; i < len(p.lit); i++ {
			if p.lit[i] == '\n' {
				endline++
			}
		}
	}

	comment = &ast.Comment{Slash: p.pos, Text: p.lit}
	p.next0()

	return
}

// Consume a group of adjacent comments, add it to the parser's
// comments list, and return it together with the line at which
// the last comment in the group ends. A non-comment token or n
// empty lines terminate a comment group.
func (p *parser) consumeCommentGroup(n int) (comments *ast.CommentGroup, endline int) {
	var list []*ast.Comment
	endline = p.file.Line(p.pos)
	for p.tok == token.COMMENT && p.file.Line(p.pos) <= endline+n {
		var comment *ast.Comment
		comment, endline = p.consumeComment()
		list = append(list, comment)
	}

	cg := &ast.CommentGroup{List: list}
	comments = cg
	return
}

// Advance to the next non-comment  In the process, collect
// any comment groups encountered, and refield the last lead and
// and line comments.
//
// A lead comment is a comment group that starts and ends in a
// line without any other tokens and that is followed by a non-comment
// token on the line immediately after the comment group.
//
// A line comment is a comment group that follows a non-comment
// token on the same line, and that has no tokens after it on the line
// where it ends.
//
// Lead and line comments may be considered documentation that is
// stored in the AST.
func (p *parser) next() {
	// A leadComment may not be consumed if it leads an inner token of a node.
	if p.leadComment != nil {
		p.comments.add(p.leadComment)
	}
	p.leadComment = nil
	prev := p.pos
	p.next0()
	p.comments.pos++

	if p.tok == token.COMMENT {
		var comment *ast.CommentGroup
		var endline int

		if p.file.Line(p.pos) == p.file.Line(prev) {
			// The comment is on same line as the previous token; it
			// cannot be a lead comment but may be a line comment.
			comment, endline = p.consumeCommentGroup(0)
			if p.file.Line(p.pos) != endline {
				// The next token is on a different line, thus
				// the last comment group is a line comment.
				comment.Line = true
			}
		}

		// consume successor comments, if any
		endline = -1
		for p.tok == token.COMMENT {
			if comment != nil {
				p.comments.add(comment)
			}
			comment, endline = p.consumeCommentGroup(1)
		}

		if endline+1 == p.file.Line(p.pos) && p.tok != token.EOF {
			// The next token is following on the line immediately after the
			// comment group, thus the last comment group is a lead comment.
			comment.Doc = true
			p.leadComment = comment
		} else {
			p.comments.add(comment)
		}
	}
}

func (p *parser) errf(pos token.Pos, msg string, args ...interface{}) {
	// ePos := p.file.Position(pos)
	ePos := pos

	// If AllErrors is not set, discard errors reported on the same line
	// as the last recorded error and stop parsing if there are more than
	// 10 errors.
	if p.mode&allErrorsMode == 0 {
		n := len(p.errors)
		if n > 0 && p.errors[n-1].Position().Line() == ePos.Line() {
			return // discard - likely a spurious error
		}
		if n > 10 {
			p.panicking = true
			panic("too many errors")
		}
	}

	p.errors.AddNewf(ePos, msg, args...)
}

func (p *parser) errorExpected(pos token.Pos, obj string) {
	if pos != p.pos {
		p.errf(pos, "expected %s", obj)
		return
	}
	// the error happened at the current position;
	// make the error message more specific
	if p.tok == token.COMMA && p.lit == "\n" {
		p.errf(pos, "expected %s, found newline", obj)
		return
	}

	if p.tok.IsLiteral() {
		p.errf(pos, "expected %s, found '%s'", obj, p.tok)
	} else {
		p.errf(pos, "expected %s, found '%s' %s", obj, p.tok, p.lit)
	}
}

func (p *parser) expect(tok token.Token) token.Pos {
	pos := p.pos
	if p.tok != tok {
		p.errorExpected(pos, "'"+tok.String()+"'")
	}
	p.next() // make progress
	return pos
}

// expectClosing is like expect but provides a better error message
// for the common case of a missing comma before a newline.
func (p *parser) expectClosing(tok token.Token, context string) token.Pos {
	if p.tok != tok && p.tok == token.COMMA && p.lit == "\n" {
		p.errf(p.pos, "missing ',' before newline in %s", context)
		p.next()
	}
	return p.expect(tok)
}

func (p *parser) expectComma() {
	// semicolon is optional before a closing ')', ']', '}', or newline
	if p.tok != token.RPAREN && p.tok != token.RBRACE && p.tok != token.EOF {
		switch p.tok {
		case token.COMMA:
			p.next()
		default:
			p.errorExpected(p.pos, "','")
			syncExpr(p)
		}
	}
}

func (p *parser) atComma(context string, follow ...token.Token) bool {
	if p.tok == token.COMMA {
		return true
	}
	for _, t := range follow {
		if p.tok == t {
			return false
		}
	}
	// TODO: find a way to detect crossing lines now we don't have a semi.
	if p.lit == "\n" {
		p.errf(p.pos, "missing ',' before newline")
	} else {
		p.errf(p.pos, "missing ',' in %s", context)
	}
	return true // "insert" comma and continue
}

// syncExpr advances to the next field in a field list.
// Used for synchronization after an error.
func syncExpr(p *parser) {
	for {
		switch p.tok {
		case token.COMMA:
			// Return only if parser made some progress since last
			// sync or if it has not reached 10 sync calls without
			// progress. Otherwise consume at least one token to
			// avoid an endless parser loop (it is possible that
			// both parseOperand and parseStmt call syncStmt and
			// correctly do not advance, thus the need for the
			// invocation limit p.syncCnt).
			if p.pos == p.syncPos && p.syncCnt < 10 {
				p.syncCnt++
				return
			}
			if p.syncPos.Before(p.pos) {
				p.syncPos = p.pos
				p.syncCnt = 0
				return
			}
			// Reaching here indicates a parser bug, likely an
			// incorrect token list in this function, but it only
			// leads to skipping of possibly correct code if a
			// previous error is present, and thus is preferred
			// over a non-terminating parse.
		case token.EOF:
			return
		}
		p.next()
	}
}

// safePos returns a valid file position for a given position: If pos
// is valid to begin with, safePos returns pos. If pos is out-of-range,
// safePos returns the EOF position.
//
// This is hack to work around "artificial" end positions in the AST which
// are computed by adding 1 to (presumably valid) token positions. If the
// token positions are invalid due to parse errors, the resulting end position
// may be past the file's EOF position, which would lead to panics if used
// later on.
func (p *parser) safePos(pos token.Pos) (res token.Pos) {
	defer func() {
		if recover() != nil {
			res = p.file.Pos(p.file.Base()+p.file.Size(), pos.RelPos()) // EOF position
		}
	}()
	_ = p.file.Offset(pos) // trigger a panic if position is out-of-range
	return pos
}

// ----------------------------------------------------------------------------
// Identifiers

func (p *parser) parseIdent() *ast.Ident {
	c := p.openComments()
	pos := p.pos
	name := "_"
	if p.tok == token.IDENT {
		name = p.lit
		p.next()
	} else {
		p.expect(token.IDENT) // use expect() error handling
	}
	ident := &ast.Ident{NamePos: pos, Name: name}
	c.closeNode(p, ident)
	return ident
}

// ----------------------------------------------------------------------------
// Expressions

// parseOperand returns an expression.
// Callers must verify the result.
func (p *parser) parseOperand() (expr ast.Expr) {
	if p.trace {
		defer un(trace(p, "Operand"))
	}

	switch p.tok {
	case token.IDENT:
		return p.parseIdent()

	case token.LBRACE:
		return p.parseStruct()

	case token.LBRACK:
		return p.parseList()

	case token.BOTTOM:
		c := p.openComments()
		x := &ast.BottomLit{Bottom: p.pos}
		p.next()
		return c.closeExpr(p, x)

	case token.NULL, token.TRUE, token.FALSE, token.INT, token.FLOAT, token.STRING:
		c := p.openComments()
		x := &ast.BasicLit{ValuePos: p.pos, Kind: p.tok, Value: p.lit}
		p.next()
		return c.closeExpr(p, x)

	case token.INTERPOLATION:
		return p.parseInterpolation()

	case token.LPAREN:
		c := p.openComments()
		defer func() { c.closeNode(p, expr) }()
		lparen := p.pos
		p.next()
		p.exprLev++
		p.openList()
		x := p.parseRHS() // types may be parenthesized: (some type)
		p.closeList()
		p.exprLev--
		rparen := p.expect(token.RPAREN)
		return &ast.ParenExpr{
			Lparen: lparen,
			X:      x,
			Rparen: rparen}
	}

	// we have an error
	c := p.openComments()
	pos := p.pos
	p.errorExpected(pos, "operand")
	syncExpr(p)
	return c.closeExpr(p, &ast.BadExpr{From: pos, To: p.pos})
}

func (p *parser) parseParams(ident *ast.Ident, follow token.Token) (params []*ast.Field) {
	for {
		c := p.openComments()
		if ident == nil {
			ident = p.parseIdent()
		}
		m := &ast.Field{Label: ident}
		if p.tok == token.COLON {
			m.Colon = p.expect(token.COLON)
			m.Value = p.parseRHS()
		}
		hasComma := p.tok == token.COMMA
		if hasComma {
			p.expect(token.COMMA)
		}
		c.closeNode(p, m)
		params = append(params, m)
		if !hasComma || p.tok == follow || p.tok == token.EOF {
			break
		}
		ident = nil
	}
	return params
}

func (p *parser) parseIndexOrSlice(x ast.Expr) (expr ast.Expr) {
	if p.trace {
		defer un(trace(p, "IndexOrSlice"))
	}

	c := p.openComments()
	defer func() { c.closeNode(p, expr) }()
	c.pos = 1

	const N = 2
	lbrack := p.expect(token.LBRACK)

	p.exprLev++
	var index [N]ast.Expr
	var colons [N - 1]token.Pos
	if p.tok != token.COLON {
		index[0] = p.parseRHS()
	}
	nColons := 0
	for p.tok == token.COLON && nColons < len(colons) {
		colons[nColons] = p.pos
		nColons++
		p.next()
		if p.tok != token.COLON && p.tok != token.RBRACK && p.tok != token.EOF {
			index[nColons] = p.parseRHS()
		}
	}
	p.exprLev--
	rbrack := p.expect(token.RBRACK)

	if nColons > 0 {
		return &ast.SliceExpr{
			X:      x,
			Lbrack: lbrack,
			Low:    index[0],
			High:   index[1],
			Rbrack: rbrack}
	}

	return &ast.IndexExpr{
		X:      x,
		Lbrack: lbrack,
		Index:  index[0],
		Rbrack: rbrack}
}

func (p *parser) parseCallOrConversion(fun ast.Expr) (expr *ast.CallExpr) {
	if p.trace {
		defer un(trace(p, "CallOrConversion"))
	}
	c := p.openComments()
	defer func() { c.closeNode(p, expr) }()

	lparen := p.expect(token.LPAREN)
	p.exprLev++
	var list []ast.Expr
	for p.tok != token.RPAREN && p.tok != token.EOF {
		list = append(list, p.parseRHS()) // builtins may expect a type: make(some type, ...)
		if !p.atComma("argument list", token.RPAREN) {
			break
		}
		p.next()
	}
	p.exprLev--
	rparen := p.expectClosing(token.RPAREN, "argument list")

	return &ast.CallExpr{
		Fun:    fun,
		Lparen: lparen,
		Args:   list,
		Rparen: rparen}
}

func (p *parser) parseFieldList(allowEmit bool) (list []ast.Decl) {
	if p.trace {
		defer un(trace(p, "FieldList"))
	}
	origEmit := allowEmit
	p.openList()
	defer p.closeList()

	for p.tok != token.RBRACE && p.tok != token.EOF {
		d := p.parseField(allowEmit)
		if e, ok := d.(*ast.EmitDecl); ok {
			if origEmit && !allowEmit {
				p.errf(p.pos, "only one emit allowed at top level")
			}
			if !origEmit || !allowEmit {
				d = &ast.BadDecl{From: e.Pos(), To: e.End()}
				for _, cg := range e.Comments() {
					d.AddComment(cg)
				}
			}
			// uncomment to only allow one emit per top-level
			// allowEmit = false
		}
		list = append(list, d)
	}
	return
}
func (p *parser) parseField(allowEmit bool) (decl ast.Decl) {
	if p.trace {
		defer un(trace(p, "Field"))
	}

	c := p.openComments()
	defer func() { c.closeNode(p, decl) }()

	pos := p.pos

	this := &ast.Field{Label: nil}
	m := this

	allowComprehension := true

	for i := 0; ; i++ {
		tok := p.tok

		expr, ok := p.parseLabel(m)

		if !ok {
			if !allowEmit {
				p.errf(pos, "expected label, found %s", tok)
			}
			if expr == nil {
				expr = p.parseExpr()
			}
			e := &ast.EmitDecl{Expr: expr}
			if p.atComma("file", token.RBRACE) {
				p.next()
			}
			return e
		}

		if i == 0 && tok == token.IDENT {
			ident := expr.(*ast.Ident)
			switch p.tok {
			case token.BIND:
				pos := p.pos
				p.expect(token.BIND)
				ref := p.parseRHS()
				if p.atComma("struct literal", token.RBRACE) { // TODO: may be EOF
					p.next()
				}
				return &ast.Alias{Ident: ident, Equal: pos, Expr: ref}
			}
		}

		if tok != token.LSS && p.tok == token.OPTION {
			m.Optional = p.pos
			p.next()
		}

		if p.tok == token.COLON {
			break
		}

		// TODO: consider disallowing comprehensions with more than one label.
		// This can be a bit awkward in some cases, but it would naturally
		// enforce the proper style that a comprehension be defined in the
		// smallest possible scope.
		// allowComprehension = false

		switch p.tok {
		default:
			if !allowEmit || p.tok != token.COMMA {
				p.errorExpected(p.pos, "label or ':'")
			}
			switch tok {
			case token.IDENT, token.LBRACK, token.STRING, token.INTERPOLATION, token.NULL, token.TRUE, token.FALSE:
				if p.tok == token.COMMA {
					p.expectComma()
					return &ast.EmitDecl{Expr: expr}
				}
			}
			return &ast.BadDecl{From: pos, To: p.pos}

		case token.IDENT, token.STRING, token.LSS, token.INTERPOLATION, token.LBRACK:
			field := &ast.Field{}
			m.Value = &ast.StructLit{Elts: []ast.Decl{field}}
			m = field
		}

		allowEmit = false
	}

	this.Colon = p.pos
	p.expect(token.COLON)
	m.Value = p.parseRHS()

	p.openList()
	for p.tok == token.ATTRIBUTE {
		allowComprehension = false
		c := p.openComments()
		a := &ast.Attribute{At: p.pos, Text: p.lit}
		p.next()
		c.closeNode(p, a)
		this.Attrs = append(this.Attrs, a)
	}
	p.closeList()

	decl = this
	var arrow token.Pos
	switch p.tok {
	case token.ARROW:
		arrow = p.expect(token.ARROW)
		fallthrough

	case token.FOR, token.IF:
		if !allowComprehension {
			p.errf(p.pos, "comprehension not alowed for this field")
		}
		clauses := p.parseComprehensionClauses()
		return &ast.ComprehensionDecl{
			Field:   this,
			Select:  arrow,
			Clauses: clauses,
		}
	}

	if p.atComma("struct literal", token.RBRACE) { // TODO: may be EOF
		p.next()
	}

	return decl
}

func (p *parser) parseLabel(f *ast.Field) (expr ast.Expr, ok bool) {
	switch p.tok {
	case token.IDENT:
		ident := p.parseIdent()
		f.Label = ident
		expr = ident

	case token.STRING:
		// JSON compatibility.

		expr = p.parseOperand()
		f.Label = expr.(ast.Label)

	case token.INTERPOLATION:
		expr = p.parseInterpolation()
		f.Label = expr.(ast.Label)

	case token.NULL, token.TRUE, token.FALSE:
		// Keywords that represent operands.

		// Allowing keywords to be used as a labels should not interfere with
		// generating good errors: any keyword can only appear on the RHS of a
		// field (after a ':'), whereas labels always appear on the LHS.
		ident := &ast.BasicLit{
			Kind:     p.tok,
			ValuePos: p.pos,
			Value:    p.lit,
		}
		p.next()
		f.Label = ident
		expr = ident

	case token.IF, token.FOR, token.IN, token.LET:
		// Keywords representing clauses.
		f.Label = &ast.Ident{
			NamePos: p.pos,
			Name:    p.lit,
		}
		p.next()

	case token.LSS: // element templates
		pos := p.pos
		c := p.openComments()
		p.next()
		ident := p.parseIdent()
		gtr := p.pos
		if p.tok != token.GTR {
			p.expect(token.GTR)
		}
		p.next()
		label := &ast.TemplateLabel{Langle: pos, Ident: ident, Rangle: gtr}
		c.closeNode(p, label)
		f.Label = label

	default:
		return expr, false
	}
	return expr, true
}

func (p *parser) parseStruct() (expr ast.Expr) {
	c := p.openComments()
	defer func() { c.closeNode(p, expr) }()

	lbrace := p.expect(token.LBRACE)

	if p.trace {
		defer un(trace(p, "StructLit"))
	}

	elts := p.parseStructBody()
	rbrace := p.expectClosing(token.RBRACE, "struct literal")
	return &ast.StructLit{
		Lbrace: lbrace,
		Elts:   elts,
		Rbrace: rbrace,
	}
}

func (p *parser) parseStructBody() []ast.Decl {
	if p.trace {
		defer un(trace(p, "StructBody"))
	}

	p.exprLev++
	var elts []ast.Decl
	if p.tok != token.RBRACE {
		elts = p.parseFieldList(false)
	}
	p.exprLev--

	return elts
}

func (p *parser) parseComprehensionClauses() (clauses []ast.Clause) {
	// TODO: reuse Template spec, which is possible if it doesn't check the
	// first is an identifier.
	for {
		if p.tok == token.COMMA {
			p.next()
		}
		switch p.tok {
		case token.FOR:
			c := p.openComments()
			forPos := p.expect(token.FOR)
			var key, value *ast.Ident
			var colon token.Pos
			value = p.parseIdent()
			if p.tok == token.COMMA {
				colon = p.expect(token.COMMA)
				key = value
				value = p.parseIdent()
			}
			c.pos = 4
			// params := p.parseParams(nil, ARROW)
			clauses = append(clauses, c.closeClause(p, &ast.ForClause{
				For:    forPos,
				Key:    key,
				Colon:  colon,
				Value:  value,
				In:     p.expect(token.IN),
				Source: p.parseExpr(),
			}))

		case token.IF:
			c := p.openComments()
			clauses = append(clauses, c.closeClause(p, &ast.IfClause{
				If:        p.expect(token.IF),
				Condition: p.parseExpr(),
			}))

		// TODO: case LET:
		default:
			return clauses
		}
	}
}

func (p *parser) parseList() (expr ast.Expr) {
	c := p.openComments()
	defer func() { c.closeNode(p, expr) }()

	lbrack := p.expect(token.LBRACK)

	if p.trace {
		defer un(trace(p, "ListLiteral"))
	}

	elts := p.parseListElements()

	if clauses := p.parseComprehensionClauses(); clauses != nil {
		var expr ast.Expr
		if len(elts) != 1 {
			p.errf(lbrack.Add(1), "list comprehension must have exactly one element")
		}
		if len(elts) > 0 {
			expr = elts[0]
		}
		rbrack := p.expectClosing(token.RBRACK, "list comprehension")

		return &ast.ListComprehension{
			Lbrack:  lbrack,
			Expr:    expr,
			Clauses: clauses,
			Rbrack:  rbrack,
		}
	}

	ellipsis := token.NoPos
	typ := ast.Expr(nil)
	if p.tok == token.ELLIPSIS {
		ellipsis = p.pos
		p.next()
		if p.tok != token.COMMA && p.tok != token.RBRACK {
			typ = p.parseRHS()
		}
		if p.atComma("list literal", token.RBRACK) {
			p.next()
		}
	}

	rbrack := p.expectClosing(token.RBRACK, "list literal")
	return &ast.ListLit{
		Lbrack:   lbrack,
		Elts:     elts,
		Ellipsis: ellipsis,
		Type:     typ,
		Rbrack:   rbrack}
}

func (p *parser) parseListElements() (list []ast.Expr) {
	if p.trace {
		defer un(trace(p, "ListElements"))
	}
	p.openList()
	defer p.closeList()

	for p.tok != token.RBRACK && p.tok != token.ELLIPSIS && p.tok != token.EOF {
		list = append(list, p.parseListElement())
		// Enforce there is an explicit comma. We could also allow the
		// omission of commas in lists, but this gives rise to some ambiguities
		// with list comprehensions.
		if p.tok == token.COMMA && p.lit != "," {
			p.next()
			// Allow missing comma for last element, though, to be compliant
			// with JSON.
			if p.tok == token.RBRACK || p.tok == token.FOR || p.tok == token.IF {
				break
			}
			p.errf(p.pos, "missing ',' before newline in list literal")
		} else if !p.atComma("list literal", token.RBRACK, token.FOR, token.IF) {
			break
		}
		p.next()
	}

	return
}

func (p *parser) parseListElement() (expr ast.Expr) {
	if p.trace {
		defer un(trace(p, "ListElement"))
	}
	c := p.openComments()
	defer func() { c.closeNode(p, expr) }()

	return p.parseRHS()
}

// checkExpr checks that x is an expression (and not a type).
func (p *parser) checkExpr(x ast.Expr) ast.Expr {
	switch unparen(x).(type) {
	case *ast.BadExpr:
	case *ast.BottomLit:
	case *ast.Ident:
	case *ast.BasicLit:
	case *ast.Interpolation:
	case *ast.StructLit:
	case *ast.ListLit:
	case *ast.ListComprehension:
	case *ast.ParenExpr:
		panic("unreachable")
	case *ast.SelectorExpr:
	case *ast.IndexExpr:
	case *ast.SliceExpr:
	case *ast.CallExpr:
	case *ast.UnaryExpr:
	case *ast.BinaryExpr:
	default:
		// all other nodes are not proper expressions
		p.errorExpected(x.Pos(), "expression")
		x = &ast.BadExpr{
			From: x.Pos(), To: p.safePos(x.End()),
		}
	}
	return x
}

// If x is of the form (T), unparen returns unparen(T), otherwise it returns x.
func unparen(x ast.Expr) ast.Expr {
	if p, isParen := x.(*ast.ParenExpr); isParen {
		x = unparen(p.X)
	}
	return x
}

// If lhs is set and the result is an identifier, it is not resolved.
func (p *parser) parsePrimaryExpr() ast.Expr {
	if p.trace {
		defer un(trace(p, "PrimaryExpr"))
	}

	x := p.parseOperand()

L:
	for {
		switch p.tok {
		case token.PERIOD:
			c := p.openComments()
			c.pos = 1
			p.next()
			switch p.tok {
			case token.IDENT:
				x = &ast.SelectorExpr{
					X:   p.checkExpr(x),
					Sel: p.parseIdent(),
				}
			default:
				pos := p.pos
				p.errorExpected(pos, "selector")
				p.next() // make progress
				x = &ast.SelectorExpr{X: x, Sel: &ast.Ident{NamePos: pos, Name: "_"}}
			}
			c.closeNode(p, x)
		case token.LBRACK:
			x = p.parseIndexOrSlice(p.checkExpr(x))
		case token.LPAREN:
			x = p.parseCallOrConversion(p.checkExpr(x))
		default:
			break L
		}
	}

	return x
}

// If lhs is set and the result is an identifier, it is not resolved.
func (p *parser) parseUnaryExpr() ast.Expr {
	if p.trace {
		defer un(trace(p, "UnaryExpr"))
	}

	switch p.tok {
	case token.ADD, token.SUB, token.NOT, token.MUL,
		token.LSS, token.LEQ, token.GEQ, token.GTR,
		token.NEQ, token.MAT, token.NMAT:
		pos, op := p.pos, p.tok
		c := p.openComments()
		p.next()
		return c.closeExpr(p, &ast.UnaryExpr{
			OpPos: pos,
			Op:    op,
			X:     p.checkExpr(p.parseUnaryExpr()),
		})
	}

	return p.parsePrimaryExpr()
}

func (p *parser) tokPrec() (token.Token, int) {
	tok := p.tok
	if tok == token.IDENT {
		switch p.lit {
		case "quo":
			return token.IQUO, 7
		case "rem":
			return token.IREM, 7
		case "div":
			return token.IDIV, 7
		case "mod":
			return token.IMOD, 7
		default:
			return tok, 0
		}
	}
	return tok, tok.Precedence()
}

// If lhs is set and the result is an identifier, it is not resolved.
func (p *parser) parseBinaryExpr(prec1 int) ast.Expr {
	if p.trace {
		defer un(trace(p, "BinaryExpr"))
	}
	p.openList()
	defer p.closeList()

	x := p.parseUnaryExpr()

	for {
		op, prec := p.tokPrec()
		if prec < prec1 {
			return x
		}
		c := p.openComments()
		c.pos = 1
		pos := p.expect(p.tok)
		x = c.closeExpr(p, &ast.BinaryExpr{
			X:     p.checkExpr(x),
			OpPos: pos,
			Op:    op,
			Y:     p.checkExpr(p.parseBinaryExpr(prec + 1))})
	}
}

func (p *parser) parseInterpolation() (expr ast.Expr) {
	c := p.openComments()
	defer func() { c.closeNode(p, expr) }()

	p.openList()
	defer p.closeList()

	cc := p.openComments()

	lit := p.lit
	pos := p.pos
	p.next()
	last := &ast.BasicLit{ValuePos: pos, Kind: token.STRING, Value: lit}
	exprs := []ast.Expr{last}

	for p.tok == token.LPAREN {
		c.pos = 1
		p.expect(token.LPAREN)
		cc.closeExpr(p, last)

		exprs = append(exprs, p.parseExpr())

		cc = p.openComments()
		if p.tok != token.RPAREN {
			p.errf(p.pos, "expected ')' for string interpolation")
		}
		lit = p.scanner.ResumeInterpolation()
		pos = p.pos
		p.next()
		last = &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    lit,
		}
		exprs = append(exprs, last)
	}
	cc.closeExpr(p, last)
	return &ast.Interpolation{Elts: exprs}
}

// Callers must check the result (using checkExpr), depending on context.
func (p *parser) parseExpr() ast.Expr {
	if p.trace {
		defer un(trace(p, "Expression"))
	}

	return p.parseBinaryExpr(token.LowestPrec + 1)
}

func (p *parser) parseRHS() ast.Expr {
	x := p.checkExpr(p.parseExpr())
	return x
}

// ----------------------------------------------------------------------------
// Declarations

func isValidImport(lit string) bool {
	const illegalChars = `!"#$%&'()*,:;<=>?[\]^{|}` + "`\uFFFD"
	s, _ := literal.Unquote(lit) // go/scanner returns a legal string literal
	for _, r := range s {
		if !unicode.IsGraphic(r) || unicode.IsSpace(r) || strings.ContainsRune(illegalChars, r) {
			return false
		}
	}
	return s != ""
}

func (p *parser) parseImportSpec(_ int) *ast.ImportSpec {
	if p.trace {
		defer un(trace(p, "ImportSpec"))
	}

	c := p.openComments()

	var ident *ast.Ident
	switch p.tok {
	case token.PERIOD:
		ident = &ast.Ident{NamePos: p.pos, Name: "."}
		p.next()
	case token.IDENT:
		ident = p.parseIdent()
	}

	pos := p.pos
	var path string
	if p.tok == token.STRING {
		path = p.lit
		if !isValidImport(path) {
			p.errf(pos, "invalid import path: %s", path)
		}
		p.next()
		p.expectComma() // call before accessing p.linecomment
	} else {
		p.expect(token.STRING) // use expect() error handling
		if p.tok == token.COMMA {
			p.expectComma() // call before accessing p.linecomment
		}
	}
	// collect imports
	spec := &ast.ImportSpec{
		Name: ident,
		Path: &ast.BasicLit{ValuePos: pos, Kind: token.STRING, Value: path},
	}
	c.closeNode(p, spec)
	p.imports = append(p.imports, spec)

	return spec
}

func (p *parser) parseImports() *ast.ImportDecl {
	if p.trace {
		defer un(trace(p, "Imports"))
	}
	c := p.openComments()

	ident := p.parseIdent()
	var lparen, rparen token.Pos
	var list []*ast.ImportSpec
	if p.tok == token.LPAREN {
		lparen = p.pos
		p.next()
		p.openList()
		for iota := 0; p.tok != token.RPAREN && p.tok != token.EOF; iota++ {
			list = append(list, p.parseImportSpec(iota))
		}
		p.closeList()
		rparen = p.expect(token.RPAREN)
		p.expectComma()
	} else {
		list = append(list, p.parseImportSpec(0))
	}

	d := &ast.ImportDecl{
		Import: ident.Pos(),
		Lparen: lparen,
		Specs:  list,
		Rparen: rparen,
	}
	c.closeNode(p, d)
	return d
}

// ----------------------------------------------------------------------------
// Source files

func (p *parser) parseFile() *ast.File {
	if p.trace {
		defer un(trace(p, "File"))
	}

	c := p.comments

	// Don't bother parsing the rest if we had errors scanning the first
	// Likely not a Go source file at all.
	if p.errors.Len() != 0 {
		return nil
	}

	// The package clause is not a declaration: it does not appear in any
	// scope.
	pos := p.pos
	var name *ast.Ident
	if p.tok == token.IDENT && p.lit == "package" {
		p.expect(token.IDENT)
		name = p.parseIdent()
		if name.Name == "_" && p.mode&declarationErrorsMode != 0 {
			p.errf(p.pos, "invalid package name _")
		}
		p.expectComma()
	} else {
		pos = token.NoPos
	}
	c.pos = 3

	p.openList()
	var decls []ast.Decl
	if p.mode&packageClauseOnlyMode == 0 {
		// import decls
		for p.tok == token.IDENT && p.lit == "import" {
			decls = append(decls, p.parseImports())
		}

		if p.mode&importsOnlyMode == 0 {
			// rest of package decls
			// TODO: loop and allow multiple expressions.
			decls = append(decls, p.parseFieldList(true)...)
			p.expect(token.EOF)
		}
	}
	p.closeList()

	f := &ast.File{
		Package: pos,
		Name:    name,
		Imports: p.imports,
		Decls:   decls,
	}
	c.closeNode(p, f)
	return f
}
