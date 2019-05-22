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

package format

import (
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

func printNode(node interface{}, f *printer) error {
	s := newFormatter(f)

	// format node
	f.allowed = nooverride // gobble initial whitespace.
	switch x := node.(type) {
	case *ast.File:
		s.file(x)
	case ast.Expr:
		s.expr(x)
	case ast.Decl:
		s.decl(x)
	// case ast.Node: // TODO: do we need this?
	// 	s.walk(x)
	case []ast.Decl:
		s.walkDeclList(x)
	default:
		goto unsupported
	}

	return nil

unsupported:
	return fmt.Errorf("cue/format: unsupported node type %T", node)
}

// Helper functions for common node lists. They may be empty.

func (f *formatter) walkDeclList(list []ast.Decl) {
	f.before(nil)
	for i, x := range list {
		if i > 0 {
			f.print(declcomma)
		}
		f.decl(x)
		f.print(f.current.parentSep)
	}
	f.after(nil)
}

func (f *formatter) walkSpecList(list []*ast.ImportSpec) {
	f.before(nil)
	for _, x := range list {
		f.importSpec(x)
	}
	f.after(nil)
}

func (f *formatter) walkClauseList(list []ast.Clause) {
	f.before(nil)
	for _, x := range list {
		f.clause(x)
	}
	f.after(nil)
}

func (f *formatter) walkExprList(list []ast.Expr, depth int) {
	f.before(nil)
	for _, x := range list {
		f.before(x)
		f.exprRaw(x, token.LowestPrec, depth)
		f.print(comma, blank)
		f.after(x)
	}
	f.after(nil)
}

func (f *formatter) file(file *ast.File) {
	f.before(file)
	if file.Name != nil {
		f.print(file.Package, "package")
		f.print(blank, file.Name, newsection, nooverride)
	}
	f.current.pos = 3
	f.visitComments(3)
	f.walkDeclList(file.Decls)
	f.after(file)
	f.print(token.EOF)
}
func (f *formatter) decl(decl ast.Decl) {
	if decl == nil {
		return
	}
	if !f.before(decl) {
		goto after
	}
	switch n := decl.(type) {
	case *ast.Field:
		// shortcut single-element structs.
		lastSize := len(f.labelBuf)
		f.labelBuf = f.labelBuf[:0]
		first, opt := n.Label, n.Optional != token.NoPos
		for {
			obj, ok := n.Value.(*ast.StructLit)
			if !ok || len(obj.Elts) != 1 || (obj.Lbrace.IsValid() && !f.printer.cfg.simplify) {
				break
			}

			// Verify that struct doesn't have inside comments and that
			// element doesn't have doc comments.
			hasComments := len(obj.Elts[0].Comments()) > 0
			for _, c := range obj.Comments() {
				if c.Position == 1 || c.Position == 2 {
					hasComments = true
				}
			}
			if hasComments {
				break
			}

			mem, ok := obj.Elts[0].(*ast.Field)
			if !ok {
				break
			}
			entry := labelEntry{mem.Label, mem.Optional != token.NoPos}
			f.labelBuf = append(f.labelBuf, entry)
			n = mem
		}

		if lastSize != len(f.labelBuf) {
			f.print(formfeed)
		}

		f.before(nil)
		f.label(first, opt)
		for _, x := range f.labelBuf {
			f.print(blank, nooverride)
			f.label(x.label, x.optional)
		}
		f.after(nil)

		nextFF := f.nextNeedsFormfeed(n.Value)
		tab := vtab
		if nextFF {
			tab = blank
		}

		f.print(n.Colon, token.COLON, tab)
		if n.Value != nil {
			switch n.Value.(type) {
			case *ast.ListComprehension, *ast.ListLit, *ast.StructLit:
				f.expr(n.Value)
			default:
				f.print(indent)
				f.expr(n.Value)
				f.markUnindentLine()
			}
		} else {
			f.current.pos++
			f.visitComments(f.current.pos)
		}

		space := tab
		for _, a := range n.Attrs {
			if f.before(a) {
				f.print(space, a.At, a)
			}
			f.after(a)
			space = blank
		}

		if nextFF {
			f.print(formfeed)
		}

	case *ast.ComprehensionDecl:
		f.decl(n.Field)
		f.print(blank)
		if n.Select != token.NoPos {
			f.print(n.Select, token.ARROW, blank)
		}
		f.print(indent)
		f.walkClauseList(n.Clauses)
		f.print(unindent)
		f.print("") // force whitespace to be written

	case *ast.BadDecl:
		f.print(n.From, "*bad decl*", declcomma)

	case *ast.ImportDecl:
		f.print(n.Import, "import")
		if len(n.Specs) == 0 {
			f.print(blank, n.Lparen, token.LPAREN, n.Rparen, token.RPAREN, newline)
			break
		}
		switch {
		case len(n.Specs) == 1:
			if !n.Lparen.IsValid() {
				f.print(blank)
				f.walkSpecList(n.Specs)
				break
			}
			fallthrough
		default:
			f.print(blank, n.Lparen, token.LPAREN, newline, indent)
			f.walkSpecList(n.Specs)
			f.print(unindent, newline, n.Rparen, token.RPAREN, newline)
		}
		f.print(newsection, nooverride)

	case *ast.EmitDecl:
		f.expr(n.Expr)
		f.print(newline, newsection, nooverride) // force newline

	case *ast.Alias:
		f.expr(n.Ident)
		f.print(blank, n.Equal, token.BIND, blank)
		f.expr(n.Expr)
		f.print(declcomma, newline) // implied

	case *ast.CommentGroup:
		f.print(newsection)
		f.printComment(n)
		f.print(newsection)
	}
after:
	f.after(decl)
}

func (f *formatter) nextNeedsFormfeed(n ast.Expr) bool {
	switch x := n.(type) {
	case *ast.StructLit:
		return true
	case *ast.BasicLit:
		return strings.IndexByte(x.Value, '\n') >= 0
	case *ast.ListLit:
		return true
	}
	return false
}

func (f *formatter) importSpec(x *ast.ImportSpec) {
	if x.Name != nil {
		f.label(x.Name, false)
		f.print(blank)
	} else {
		f.current.pos++
		f.visitComments(f.current.pos)
	}
	f.expr(x.Path)
	f.print(newline)
}

func (f *formatter) label(l ast.Label, optional bool) {
	switch n := l.(type) {
	case *ast.Ident:
		f.print(n.NamePos, n)

	case *ast.BasicLit:
		if f.cfg.simplify && n.Kind == token.STRING && len(n.Value) > 2 {
			s := n.Value
			unquoted, err := strconv.Unquote(s)
			if err == nil {
				e, _ := parser.ParseExpr("check", unquoted)
				if _, ok := e.(*ast.Ident); ok {
					f.print(n.ValuePos, unquoted)
					break
				}
			}
		}
		f.print(n.ValuePos, n.Value)

	case *ast.TemplateLabel:
		f.print(n.Langle, token.LSS, indent)
		f.label(n.Ident, false)
		f.print(unindent, n.Rangle, token.GTR)

	case *ast.Interpolation:
		f.expr(n)

	default:
		panic(fmt.Sprintf("unknown label type %T", n))
	}
	if optional {
		f.print(token.OPTION)
	}
}

func (f *formatter) expr(x ast.Expr) {
	const depth = 1
	f.expr1(x, token.LowestPrec, depth)
}

func (f *formatter) expr0(x ast.Expr, depth int) {
	f.expr1(x, token.LowestPrec, depth)
}

func (f *formatter) expr1(expr ast.Expr, prec1, depth int) {
	if f.before(expr) {
		f.exprRaw(expr, prec1, depth)
	}
	f.after(expr)
}

func (f *formatter) exprRaw(expr ast.Expr, prec1, depth int) {

	switch x := expr.(type) {
	case *ast.BadExpr:
		f.print(x.From, "BadExpr")

	case *ast.BottomLit:
		f.print(x.Bottom, token.BOTTOM)

	case *ast.Ident:
		f.print(x.NamePos, x)

	case *ast.BinaryExpr:
		if depth < 1 {
			f.internalError("depth < 1:", depth)
			depth = 1
		}
		f.binaryExpr(x, prec1, cutoff(x, depth), depth)

	case *ast.UnaryExpr:
		const prec = token.UnaryPrec
		if prec < prec1 {
			// parenthesis needed
			f.print(token.LPAREN, nooverride)
			f.expr(x)
			f.print(token.RPAREN)
		} else {
			// no parenthesis needed
			f.print(x.OpPos, x.Op, nooverride)
			f.expr1(x.X, prec, depth)
		}

	case *ast.BasicLit:
		f.print(x.ValuePos, x)

	case *ast.Interpolation:
		f.before(nil)
		for _, x := range x.Elts {
			f.expr0(x, depth+1)
		}
		f.after(nil)

	case *ast.ParenExpr:
		if _, hasParens := x.X.(*ast.ParenExpr); hasParens {
			// don't print parentheses around an already parenthesized expression
			// TODO: consider making this more general and incorporate precedence levels
			f.expr0(x.X, depth)
		} else {
			f.print(x.Lparen, token.LPAREN)
			f.expr0(x.X, reduceDepth(depth)) // parentheses undo one level of depth
			f.print(x.Rparen, token.RPAREN)
		}

	case *ast.SelectorExpr:
		f.selectorExpr(x, depth)

	case *ast.IndexExpr:
		f.expr1(x.X, token.HighestPrec, 1)
		f.print(x.Lbrack, token.LBRACK)
		f.expr0(x.Index, depth+1)
		f.print(x.Rbrack, token.RBRACK)

	case *ast.SliceExpr:
		f.expr1(x.X, token.HighestPrec, 1)
		f.print(x.Lbrack, token.LBRACK)
		indices := []ast.Expr{x.Low, x.High}
		for i, y := range indices {
			if i > 0 {
				// blanks around ":" if both sides exist and either side is a binary expression
				x := indices[i-1]
				if depth <= 1 && x != nil && y != nil && (isBinary(x) || isBinary(y)) {
					f.print(blank, token.COLON, blank)
				} else {
					f.print(token.COLON)
				}
			}
			if y != nil {
				f.expr0(y, depth+1)
			}
		}
		f.print(x.Rbrack, token.RBRACK)

	case *ast.CallExpr:
		if len(x.Args) > 1 {
			depth++
		}
		wasIndented := f.possibleSelectorExpr(x.Fun, token.HighestPrec, depth)
		f.print(x.Lparen, token.LPAREN)
		f.walkExprList(x.Args, depth)
		f.print(trailcomma, noblank, x.Rparen, token.RPAREN)
		if wasIndented {
			f.print(unindent)
		}

	case *ast.StructLit:
		f.print(x.Lbrace, token.LBRACE, noblank, f.formfeed(), indent)
		f.walkDeclList(x.Elts)
		f.matchUnindent()
		f.print(noblank, x.Rbrace, token.RBRACE)

	case *ast.ListLit:
		f.print(x.Lbrack, token.LBRACK, indent)
		f.walkExprList(x.Elts, 1)
		if x.Ellipsis != token.NoPos || x.Type != nil {
			f.print(x.Ellipsis, token.ELLIPSIS)
			if x.Type != nil && !isTop(x.Type) {
				f.expr(x.Type)
			}
		} else {
			f.print(trailcomma, noblank)
			f.current.pos += 2
			f.visitComments(f.current.pos)
		}
		f.matchUnindent()
		f.print(noblank, x.Rbrack, token.RBRACK)

	case *ast.ListComprehension:
		f.print(x.Lbrack, token.LBRACK, blank, indent)
		f.expr(x.Expr)
		f.print(blank)
		f.walkClauseList(x.Clauses)
		f.print(unindent, f.wsOverride(blank), x.Rbrack, token.RBRACK)

	default:
		panic(fmt.Sprintf("unimplemented type %T", x))
	}
	return
}

func (f *formatter) clause(clause ast.Clause) {
	switch n := clause.(type) {
	case *ast.ForClause:
		f.print(blank, n.For, "for", blank)
		if n.Key != nil {
			f.label(n.Key, false)
			f.print(n.Colon, token.COMMA, blank)
		} else {
			f.current.pos++
			f.visitComments(f.current.pos)
		}
		f.label(n.Value, false)
		f.print(blank, n.In, "in", blank)
		f.expr(n.Source)

	case *ast.IfClause:
		f.print(blank, n.If, "if", blank)
		f.expr(n.Condition)

	default:
		panic("unknown clause type")
	}
}

func walkBinary(e *ast.BinaryExpr) (has6, has7, has8 bool, maxProblem int) {
	switch e.Op.Precedence() {
	case 6:
		has6 = true
	case 7:
		has7 = true
	case 8:
		has8 = true
	}

	switch l := e.X.(type) {
	case *ast.BinaryExpr:
		if l.Op.Precedence() < e.Op.Precedence() {
			// parens will be inserted.
			// pretend this is an *syntax.ParenExpr and do nothing.
			break
		}
		h6, h7, h8, mp := walkBinary(l)
		has6 = has6 || h6
		has7 = has7 || h7
		has8 = has8 || h8
		if maxProblem < mp {
			maxProblem = mp
		}
	}

	switch r := e.Y.(type) {
	case *ast.BinaryExpr:
		if r.Op.Precedence() <= e.Op.Precedence() {
			// parens will be inserted.
			// pretend this is an *syntax.ParenExpr and do nothing.
			break
		}
		h6, h7, h8, mp := walkBinary(r)
		has6 = has6 || h6
		has7 = has7 || h7
		has8 = has8 || h8
		if maxProblem < mp {
			maxProblem = mp
		}

	case *ast.UnaryExpr:
		switch e.Op.String() + r.Op.String() {
		case "/*":
			maxProblem = 8
		case "++", "--":
			if maxProblem < 6 {
				maxProblem = 6
			}
		}
	}
	return
}

func cutoff(e *ast.BinaryExpr, depth int) int {
	has6, has7, has8, maxProblem := walkBinary(e)
	if maxProblem > 0 {
		return maxProblem + 1
	}
	if (has6 || has7) && has8 {
		if depth == 1 {
			return 8
		}
		if has7 {
			return 7
		}
		return 6
	}
	if has6 && has7 {
		if depth == 1 {
			return 7
		}
		return 6
	}
	if depth == 1 {
		return 8
	}
	return 6
}

func diffPrec(expr ast.Expr, prec int) int {
	x, ok := expr.(*ast.BinaryExpr)
	if !ok || prec != x.Op.Precedence() {
		return 1
	}
	return 0
}

func reduceDepth(depth int) int {
	depth--
	if depth < 1 {
		depth = 1
	}
	return depth
}

// Format the binary expression: decide the cutoff and then format.
// Let's call depth == 1 Normal mode, and depth > 1 Compact mode.
// (Algorithm suggestion by Russ Cox.)
//
// The precedences are:
//	7             *  /  % quo rem div mod
//	6             +  -
//	5             ==  !=  <  <=  >  >=
//	4             &&
//	3             ||
//	2             &
//	1             |
//
// The only decision is whether there will be spaces around levels 6 and 7.
// There are never spaces at level 8 (unary), and always spaces at levels 5 and below.
//
// To choose the cutoff, look at the whole expression but excluding primary
// expressions (function calls, parenthesized exprs), and apply these rules:
//
//	1) If there is a binary operator with a right side unary operand
//	   that would clash without a space, the cutoff must be (in order):
//
//		/*	8
//		++	7 // not necessary, but to avoid confusion
//		--	7
//
//         (Comparison operators always have spaces around them.)
//
//	2) If there is a mix of level 7 and level 6 operators, then the cutoff
//	   is 7 (use spaces to distinguish precedence) in Normal mode
//	   and 6 (never use spaces) in Compact mode.
//
//	3) If there are no level 6 operators or no level 7 operators, then the
//	   cutoff is 8 (always use spaces) in Normal mode
//	   and 6 (never use spaces) in Compact mode.
//
func (f *formatter) binaryExpr(x *ast.BinaryExpr, prec1, cutoff, depth int) {
	f.nestExpr++
	defer func() { f.nestExpr-- }()

	prec := x.Op.Precedence()
	if prec < prec1 {
		// parenthesis needed
		// Note: The parser inserts an syntax.ParenExpr node; thus this case
		//       can only occur if the AST is created in a different way.
		// defer p.pushComment(nil).pop()
		f.print(token.LPAREN, nooverride)
		f.expr0(x, reduceDepth(depth)) // parentheses undo one level of depth
		f.print(token.RPAREN)
		return
	}

	printBlank := prec < cutoff

	f.expr1(x.X, prec, depth+diffPrec(x.X, prec))
	f.print(nooverride)
	if printBlank {
		f.print(blank)
	}
	f.print(x.OpPos, x.Op)
	if x.Y.Pos().IsNewline() {
		// at least one line break, but respect an extra empty line
		// in the source
		f.print(formfeed)
		printBlank = false // no blank after line break
	} else {
		f.print(nooverride)
	}
	if printBlank {
		f.print(blank)
	}
	f.expr1(x.Y, prec+1, depth+1)
}

func isBinary(expr ast.Expr) bool {
	_, ok := expr.(*ast.BinaryExpr)
	return ok
}

func (f *formatter) possibleSelectorExpr(expr ast.Expr, prec1, depth int) bool {
	if x, ok := expr.(*ast.SelectorExpr); ok {
		return f.selectorExpr(x, depth)
	}
	f.expr1(expr, prec1, depth)
	return false
}

// selectorExpr handles an *syntax.SelectorExpr node and returns whether x spans
// multiple lines.
func (f *formatter) selectorExpr(x *ast.SelectorExpr, depth int) bool {
	f.expr1(x.X, token.HighestPrec, depth)
	f.print(token.PERIOD)
	if x.Sel.Pos().IsNewline() {
		f.print(indent, formfeed, x.Sel.Pos(), x.Sel)
		f.print(unindent)
		return true
	}
	f.print(x.Sel.Pos(), x.Sel)
	return false
}

func isTop(e ast.Expr) bool {
	ident, ok := e.(*ast.Ident)
	return ok && ident.Name == "_"
}
