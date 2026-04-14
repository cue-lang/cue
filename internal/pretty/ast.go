// Copyright 2026 CUE Authors
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

package pretty

import (
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// converter translates a CUE AST into the Doc algebra.
type converter struct {
	cfg *Config
}

// node converts any AST node to a Doc.
func (c *converter) node(n ast.Node) *Doc {
	switch x := n.(type) {
	case *ast.File:
		return c.file(x)
	case ast.Expr:
		return c.expr(x)
	case ast.Decl:
		return c.decl(x)
	default:
		return nil
	}
}

// file converts a File node.
func (c *converter) file(f *ast.File) *Doc {
	return c.withComments(f, c.declSlice(f.Decls))
}

// declSlice joins a slice of Decls with RelPos-driven separators.
func (c *converter) declSlice(decls []ast.Decl) *Doc {
	if len(decls) == 0 {
		return nil
	}

	var docs []*Doc
	var tableRows []Row
	hasAligned := false // true if tableRows contains at least one aligned row

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		// The first row's separator goes before the table itself.
		docs = append(docs, tableRows[0].Sep)
		if !hasAligned {
			// No aligned rows — just emit raw rows directly.
			for i, row := range tableRows {
				if i > 0 {
					docs = append(docs, row.Sep)
				}
				docs = append(docs, row.Raw)
			}
		} else {
			// Clear the first row's sep since it's emitted above.
			tableRows[0].Sep = nil
			docs = append(docs, Table(tableRows))
		}
		tableRows = nil
		hasAligned = false
	}

	for i, d := range decls {
		// Elided declarations are skipped entirely.
		if d.Pos().HasRelPos() && d.Pos().RelPos() == token.Elided {
			continue
		}

		var sep *Doc
		if i > 0 {
			sep = c.declSep(d)
		}

		// Skip pure comment groups as declarations; they're handled
		// via withComments on the nodes they're attached to.
		if _, ok := d.(*ast.CommentGroup); ok {
			doc := c.decl(d)
			flushTable()
			docs = append(docs, sep, doc)
			continue
		}

		if f, ok := d.(*ast.Field); ok && c.isSimpleField(f) {
			row := c.fieldRow(f)
			row.Sep = sep
			tableRows = append(tableRows, row)
			hasAligned = true
			continue
		}

		// Complex field or non-field decl: add as a raw row in the table
		// so alignment spans across it.
		doc := c.withComments(d, c.decl(d))
		tableRows = append(tableRows, Row{Sep: sep, Raw: doc})
	}
	flushTable()

	return Cats(docs...)
}

// isSimpleField reports whether a field's value is NOT a struct or list
// and has no doc comments on its value expression, making it eligible
// for table alignment.
func (c *converter) isSimpleField(f *ast.Field) bool {
	if f.Value == nil {
		return false
	}
	switch f.Value.(type) {
	case *ast.StructLit, *ast.ListLit:
		return false
	}
	// Fields whose value has doc comments need special formatting
	// (indented on their own line) and can't participate in table rows.
	if c.exprHasDocComment(f.Value) {
		return false
	}
	return true
}

// declSep computes the separator Doc before a declaration based on its RelPos.
// RelPos values are honoured when doing so produces valid syntax. Newline
// always produces a hard line break, NewSection always produces a blank line.
// Elided, NoSpace, and Blank are overridden to SoftComma because declarations
// require at least a comma or newline between them. SoftComma produces ", "
// when flat (inside a struct group) and a newline when broken (top-level or
// broken struct).
func (c *converter) declSep(d ast.Decl) *Doc {
	// The effective RelPos is the maximum of the declaration's own RelPos
	// and the RelPos of any doc comment attached to it. This ensures that
	// a blank line before a doc comment block is preserved even if the
	// field itself only has Newline RelPos.
	rel := token.NoRelPos
	pos := d.Pos()
	if pos.HasRelPos() {
		rel = pos.RelPos()
	}
	for _, cg := range ast.Comments(d) {
		if (cg.Doc || cg.Position == 0) && cg.Pos().HasRelPos() {
			if cgRel := cg.Pos().RelPos(); cgRel > rel {
				rel = cgRel
			}
		}
	}

	switch rel {
	case token.Elided:
		// Elided declarations are skipped entirely in declSlice.
		// If we get here, fall back to default.
		return NoSep()
	case token.NoSpace, token.Blank:
		// Between declarations, bare concatenation or a bare space is not
		// valid CUE — a comma or newline is required.
		return SoftComma()
	case token.Newline:
		// Newline is always honoured as a hard line break.
		return HardLine()
	case token.NewSection:
		// NewSection is always honoured as a blank line.
		return BlankLine()
	}
	return SoftComma()
}

// bracketBreak returns the break doc to use between an opening bracket/brace
// and the first element, or between the last element and a closing bracket/brace.
// If the position has Newline or NewSection RelPos, a HardLine is returned to
// force the group to break. Otherwise a soft Line("") is returned.
func (c *converter) bracketBreak(pos token.Pos) *Doc {
	if pos.HasRelPos() {
		switch pos.RelPos() {
		case token.Newline, token.NewSection:
			return HardLine()
		}
	}
	return NoSepLineSingleton()
}

// elemSep returns the separator before a list element based on its RelPos.
// Newline and NewSection are always honoured as hard breaks. NoSpace and
// Blank are overridden because list elements require a comma separator.
// Elided elements are skipped in listLit.
func (c *converter) elemSep(e ast.Expr) *Doc {
	pos := e.Pos()
	if pos.HasRelPos() {
		switch pos.RelPos() {
		case token.Newline:
			return Cats(commaText, HardLine())
		case token.NewSection:
			return Cats(commaText, BlankLine())
		}
		// Elided, NoSpace, Blank: fall through to default (comma required).
	}
	return Cats(commaText, SoftLine())
}

// expr converts an expression node to a Doc, including any comments.
func (c *converter) expr(x ast.Expr) *Doc {
	if x == nil {
		return nil
	}
	d := c.exprCore(x)
	// Handle comments attached directly to this expression node.
	// Chained binary expressions (| and &) handle their own comments
	// via flattenChain and are returned directly from exprCore.
	if bin, ok := x.(*ast.BinaryExpr); ok && (bin.Op == token.OR || bin.Op == token.AND) {
		return d // comments already handled
	}
	return c.exprComments(x, d)
}

// exprCore converts an expression without handling comments on it.
// Comments are handled by the caller (expr or listElem).
func (c *converter) exprCore(x ast.Expr) *Doc {
	if x == nil {
		return nil
	}
	switch x := x.(type) {
	case *ast.Ident:
		return Text(x.Name)

	case *ast.BasicLit:
		return c.basicLit(x)

	case *ast.BottomLit:
		return Text("_|_")

	case *ast.BadExpr:
		return Text("/* BadExpr */")

	case *ast.StructLit:
		return c.structLit(x)

	case *ast.ListLit:
		return c.listLit(x)

	case *ast.Ellipsis:
		return c.ellipsis(x)

	case *ast.Comprehension:
		return c.comprehension(x)

	case *ast.UnaryExpr:
		return c.unaryExpr(x)

	case *ast.BinaryExpr:
		if x.Op == token.OR || x.Op == token.AND {
			return c.chainedBinaryExpr(x)
		}
		return c.binaryExpr(x)

	case *ast.PostfixExpr:
		return Cat(c.expr(x.X), Text(x.Op.String()))

	case *ast.SelectorExpr:
		return Cats(c.expr(x.X), Text("."), c.label(x.Sel))

	case *ast.IndexExpr:
		return c.indexExpr(x)

	case *ast.SliceExpr:
		return c.sliceExpr(x)

	case *ast.CallExpr:
		return c.callExpr(x)

	case *ast.ParenExpr:
		return c.parenExpr(x)

	case *ast.Interpolation:
		return c.interpolation(x)

	case *ast.Func:
		return c.funcExpr(x)

	case *ast.Alias:
		return Cats(Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))

	default:
		return Text("/* unknown expr */")
	}
}

// exprComments wraps an expression Doc with its attached comments.
// Doc comments are placed before the expression with a HardLine after
// (so the comment is on its own line). The calling context (fieldVal,
// list element separator, etc.) is responsible for ensuring a line break
// before the comment so it doesn't share a line with preceding tokens.
func (c *converter) exprComments(n ast.Node, body *Doc) *Doc {
	cgs := ast.Comments(n)
	if len(cgs) == 0 {
		return body
	}

	var before []*Doc
	var after []*Doc

	for _, cg := range cgs {
		cdoc := c.commentGroup(cg)
		if cg.Doc || cg.Position == 0 {
			before = append(before, cdoc, HardLine())
		} else {
			// Trailing // comment: place it, then force the enclosing
			// group to break so the comment doesn't swallow closing
			// brackets/braces in flat mode. IfBreak(nil, HardLine)
			// is invisible in broken mode but prevents flat rendering.
			after = append(after, c.commentSep(cg, cdoc), IfBreak(nil, HardLine()))
		}
	}

	result := body
	if len(before) > 0 {
		result = Cats(append(before, result)...)
	}
	if len(after) > 0 {
		result = Cats(append([]*Doc{result}, after...)...)
	}
	return result
}

// label converts a Label node to a Doc.
func (c *converter) label(l ast.Label) *Doc {
	switch x := l.(type) {
	case *ast.Ident:
		return Text(x.Name)
	case *ast.BasicLit:
		return c.basicLit(x)
	case *ast.Interpolation:
		return c.interpolation(x)
	case *ast.ListLit:
		// Computed label: [expr]
		if len(x.Elts) == 1 {
			return Cats(lBracket, c.expr(x.Elts[0]), rBracket)
		}
		return c.listLit(x)
	case *ast.ParenExpr:
		return Cats(lParen, c.expr(x.X), rParen)
	case *ast.Alias:
		return Cats(Text(x.Ident.Name), Text("="), c.expr(x.Expr))
	default:
		return c.expr(l.(ast.Expr))
	}
}

// basicLit converts a BasicLit. Multi-line strings contain literal newlines
// in their Value; we split on those and join with LitLine (bare newlines
// without indentation) so the renderer's column tracking stays accurate
// and the string content is reproduced verbatim.
func (c *converter) basicLit(x *ast.BasicLit) *Doc {
	val := x.Value
	// Fast path: no embedded newlines.
	if !strings.Contains(val, "\n") {
		return Text(val)
	}
	lines := strings.Split(val, "\n")
	var parts []*Doc
	for i, line := range lines {
		if i > 0 {
			parts = append(parts, LitLine())
		}
		parts = append(parts, Text(line))
	}
	return Cats(parts...)
}

// structLit converts a StructLit.
func (c *converter) structLit(x *ast.StructLit) *Doc {
	hasBraces := x.Lbrace.IsValid()

	if !hasBraces {
		return c.declSlice(x.Elts)
	}

	if len(x.Elts) == 0 {
		return Text("{}")
	}

	body := c.declSlice(x.Elts)

	// Honour RelPos on the first element and closing brace.
	// If either requires a newline, use HardLine to force the group to break.
	openBreak := c.bracketBreak(x.Elts[0].Pos())
	closeBreak := c.bracketBreak(x.Rbrace)

	return Group(Cats(
		lBrace,
		Nest(1, Cat(openBreak, body)),
		closeBreak,
		rBrace,
	))
}

// listLit converts a ListLit.
func (c *converter) listLit(x *ast.ListLit) *Doc {
	hasBrackets := x.Lbrack.IsValid()

	if !hasBrackets {
		// Shouldn't normally happen for lists, but respect the AST.
		var elems []*Doc
		for _, e := range x.Elts {
			elems = append(elems, c.expr(e))
		}
		return Sep(Text(", "), elems...)
	}

	if len(x.Elts) == 0 {
		return Text("[]")
	}

	// Build elements. For each non-last element we emit:
	//   line_break + value + "," + trailing_comments
	// This ensures "1, // comment" not "1 // comment," — the comma
	// must come before any // comment to avoid being swallowed.
	// The last element gets a TrailingComma (only in broken mode).
	var active []ast.Expr
	for _, e := range x.Elts {
		if e.Pos().HasRelPos() && e.Pos().RelPos() == token.Elided {
			continue
		}
		active = append(active, e)
	}

	var elems []*Doc
	for i, e := range active {
		if i > 0 {
			elems = append(elems, c.elemBreak(e))
		}
		last := i == len(active)-1
		elems = append(elems, c.listElem(e, last))
	}

	body := Cats(elems...)

	// Honour RelPos on the first element and closing bracket.
	openBreak := c.bracketBreak(x.Elts[0].Pos())
	closeBreak := c.bracketBreak(x.Rbrack)

	return Group(Cats(
		lBracket,
		Nest(1, Cat(openBreak, body)),
		closeBreak,
		rBracket,
	))
}

// elemBreak returns just the line-break portion of a list element separator,
// without the comma (the comma is handled by listElem).
func (c *converter) elemBreak(e ast.Expr) *Doc {
	pos := e.Pos()
	if pos.HasRelPos() {
		switch pos.RelPos() {
		case token.Newline:
			return HardLine()
		case token.NewSection:
			return BlankLine()
		}
	}
	return Line(" ")
}

// listElem formats a list element with comma and comments in the right order:
//
//	value + "," + trailing_comments
//
// For the last element, the comma is a TrailingComma (only in broken mode).
// This ensures "1, // comment" not "1 // comment,".
func (c *converter) listElem(e ast.Expr, last bool) *Doc {
	d := c.exprCore(e)

	// Chained binary expressions handle their own comments.
	if bin, ok := e.(*ast.BinaryExpr); ok && (bin.Op == token.OR || bin.Op == token.AND) {
		if last {
			return Cat(d, TrailingComma())
		}
		return Cat(d, commaText)
	}

	// Collect comments on this expression.
	cgs := ast.Comments(e)

	// Determine the comma: always for non-last, TrailingComma for last.
	comma := commaText
	if last {
		comma = TrailingComma()
	}

	if len(cgs) == 0 {
		return Cat(d, comma)
	}

	var before []*Doc
	var after []*Doc
	for _, cg := range cgs {
		cdoc := c.commentGroup(cg)
		if cg.Doc || cg.Position == 0 {
			before = append(before, cdoc, HardLine())
		} else {
			after = append(after, c.commentSep(cg, cdoc), IfBreak(nil, HardLine()))
		}
	}

	// Assemble: doc_comments + value + comma + trailing_comments
	result := d
	if len(before) > 0 {
		result = Cats(append(before, result)...)
	}
	result = Cat(result, comma)
	if len(after) > 0 {
		result = Cats(append([]*Doc{result}, after...)...)
	}
	return result
}

// ellipsis converts an Ellipsis node.
func (c *converter) ellipsis(x *ast.Ellipsis) *Doc {
	if x.Type != nil {
		return Cat(Text("..."), c.expr(x.Type))
	}
	return Text("...")
}

// unaryExpr converts a UnaryExpr.
func (c *converter) unaryExpr(x *ast.UnaryExpr) *Doc {
	op := x.Op.String()
	inner := c.expr(x.X)

	// Check RelPos between operator and operand.
	if x.X != nil && x.X.Pos().HasRelPos() && x.X.Pos().RelPos() == token.Blank {
		return Cats(Text(op), spaceText, inner)
	}
	return Cat(Text(op), inner)
}

// binaryExpr converts a BinaryExpr.
func (c *converter) binaryExpr(x *ast.BinaryExpr) *Doc {
	// Disjunctions and conjunctions are flattened and formatted like a
	// list: either all on one line or all broken with the operator as
	// separator.
	if x.Op == token.OR || x.Op == token.AND {
		return c.chainedBinaryExpr(x)
	}

	left := c.expr(x.X)
	op := x.Op.String()
	right := c.expr(x.Y)

	// If RHS has Newline RelPos, honour it with a hard line break.
	// No Nest: the RHS stays at the current indent level to ensure
	// idempotency (nesting would accumulate on each re-format).
	if x.Y.Pos().HasRelPos() && x.Y.Pos().RelPos() >= token.Newline {
		return Group(Cats(left, spaceText, Text(op), HardLine(), right))
	}

	return Group(Cats(left, spaceText, Text(op), SoftLine(), right))
}

// chainArm holds one arm of a flattened binary chain (| or &).
type chainArm struct {
	expr     ast.Expr
	opPos    token.Pos           // position of the operator before this arm (invalid for first)
	exprPos  token.Pos           // position of the expression (for RelPos)
	comments []*ast.CommentGroup // comments from intermediate BinaryExpr nodes
}

// flattenChain collects all operands from a chain of same-operator BinaryExprs,
// preserving comments from intermediate nodes.
// Both (a op b) op c and a op (b op c) are flattened to [a, b, c].
func flattenChain(x *ast.BinaryExpr) []chainArm {
	op := x.Op
	var result []chainArm

	var walkLeft func(e ast.Expr)
	walkLeft = func(e ast.Expr) {
		bin, ok := e.(*ast.BinaryExpr)
		if !ok || bin.Op != op {
			result = append(result, chainArm{expr: e, exprPos: e.Pos()})
			return
		}
		walkLeft(bin.X)
		cgs := ast.Comments(bin)
		addChainLeaf(bin.Y, bin.OpPos, op, cgs, &result)
	}

	cgs := ast.Comments(x)
	walkLeft(x.X)
	addChainLeaf(x.Y, x.OpPos, op, cgs, &result)
	return result
}

func addChainLeaf(e ast.Expr, opPos token.Pos, op token.Token, comments []*ast.CommentGroup, result *[]chainArm) {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == op {
		addChainLeaf(bin.X, opPos, op, comments, result)
		addChainLeaf(bin.Y, bin.OpPos, op, ast.Comments(bin), result)
		return
	}
	*result = append(*result, chainArm{
		expr:     e,
		opPos:    opPos,
		exprPos:  e.Pos(),
		comments: comments,
	})
}

// chainedBinaryExpr formats a flattened chain of same-operator binary
// expressions (| or &). It behaves like a list: either all on one line
// or all broken with the operator as separator, continuation arms indented.
func (c *converter) chainedBinaryExpr(x *ast.BinaryExpr) *Doc {
	arms := flattenChain(x)
	opStr := " " + x.Op.String()

	first := c.expr(arms[0].expr)

	var rest []*Doc
	for _, a := range arms[1:] {
		elem := c.expr(a.expr)

		rel := token.NoRelPos
		if a.exprPos.HasRelPos() {
			rel = a.exprPos.RelPos()
		} else if a.opPos.HasRelPos() {
			rel = a.opPos.RelPos()
		}

		// Collect comments from intermediate BinaryExpr nodes.
		var commentDoc *Doc
		for _, cg := range a.comments {
			if cg.Line || cg.Position >= 1 {
				cd := c.commentGroup(cg)
				if commentDoc == nil {
					commentDoc = cd
				} else {
					commentDoc = Cats(commentDoc, spaceText, cd)
				}
			}
		}

		// Comments appear after the operator and force a hard break.
		var sep *Doc
		if commentDoc != nil {
			sep = Cats(Text(opStr), spaceText, commentDoc, HardLine())
		} else {
			switch {
			case rel >= token.Newline:
				sep = Cats(Text(opStr), HardLine())
			default:
				sep = Cats(Text(opStr), SoftLine())
			}
		}

		rest = append(rest, sep, elem)
	}

	return Group(Cat(first, Nest(1, Cats(rest...))))
}

// callExpr converts a CallExpr. Arguments are handled like list elements:
// RelPos is honoured, commas come before trailing comments, and trailing
// commas are allowed before ')'.
func (c *converter) callExpr(x *ast.CallExpr) *Doc {
	fun := c.expr(x.Fun)

	if len(x.Args) == 0 {
		return Cats(fun, Text("()"))
	}

	var elems []*Doc
	for i, a := range x.Args {
		if i > 0 {
			elems = append(elems, c.elemBreak(a))
		}
		last := i == len(x.Args)-1
		elems = append(elems, c.listElem(a, last))
	}

	body := Cats(elems...)
	openBreak := c.bracketBreak(x.Args[0].Pos())
	closeBreak := c.bracketBreak(x.Rparen)

	return Group(Cats(
		fun,
		lParen,
		Nest(1, Cat(openBreak, body)),
		closeBreak,
		rParen,
	))
}

// indexExpr converts an IndexExpr. Honours RelPos on the index expression.
// A newline before ']' is not valid CUE (auto-comma insertion triggers),
// so the index and closing bracket stay on the same line.
func (c *converter) indexExpr(x *ast.IndexExpr) *Doc {
	openBreak := c.bracketBreak(x.Index.Pos())
	return Group(Cats(
		c.expr(x.X),
		lBracket,
		Nest(1, Cat(openBreak, c.expr(x.Index))),
		rBracket,
	))
}

// sliceExpr converts a SliceExpr. Similar to indexExpr.
func (c *converter) sliceExpr(x *ast.SliceExpr) *Doc {
	low := c.expr(x.Low)
	high := c.expr(x.High)
	return Cats(c.expr(x.X), lBracket, low, colonText, high, rBracket)
}

// parenExpr converts a ParenExpr. Honours RelPos on the inner expression.
// A newline before ')' is not valid CUE (auto-comma insertion triggers),
// so the expression and closing paren stay on the same line.
func (c *converter) parenExpr(x *ast.ParenExpr) *Doc {
	openBreak := c.bracketBreak(x.X.Pos())
	return Group(Cats(
		lParen,
		Nest(1, Cat(openBreak, c.expr(x.X))),
		rParen,
	))
}

// interpolation converts an Interpolation node.
// The Elts alternate between string fragments (BasicLit) and interpolated
// expressions. The string fragments already include the \( and ) delimiters,
// so we emit them verbatim and just format the expressions.
func (c *converter) interpolation(x *ast.Interpolation) *Doc {
	var parts []*Doc
	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			parts = append(parts, Text(lit.Value))
		} else {
			parts = append(parts, c.expr(e))
		}
	}
	return Cats(parts...)
}

// funcExpr converts a Func node.
func (c *converter) funcExpr(x *ast.Func) *Doc {
	var args []*Doc
	for _, a := range x.Args {
		args = append(args, c.expr(a))
	}
	argDoc := Sep(Text(", "), args...)
	return Cats(Text("func"), lParen, argDoc, Text("): "), c.expr(x.Ret))
}

// comprehension converts a Comprehension.
func (c *converter) comprehension(x *ast.Comprehension) *Doc {
	var parts []*Doc
	for i, clause := range x.Clauses {
		cl := c.clause(clause)
		if i > 0 {
			// Honour RelPos between clauses.
			pos := clause.(ast.Node).Pos()
			if pos.HasRelPos() && pos.RelPos() >= token.Newline {
				cl = Cat(HardLine(), cl)
			} else {
				cl = Cat(spaceText, cl)
			}
		}
		parts = append(parts, cl)
	}

	// Separator before the value (struct body).
	valSep := spaceText
	if x.Value != nil {
		pos := x.Value.Pos()
		if pos.HasRelPos() && pos.RelPos() >= token.Newline {
			valSep = HardLine()
		}
	}
	parts = append(parts, valSep)
	parts = append(parts, c.expr(x.Value))

	if x.Fallback != nil {
		parts = append(parts, c.fallbackClause(x))
	}

	return Cats(parts...)
}

// clause converts a single clause.
func (c *converter) clause(cl ast.Clause) *Doc {
	switch x := cl.(type) {
	case *ast.ForClause:
		return c.forClause(x)
	case *ast.IfClause:
		return Cats(Text("if "), c.expr(x.Condition))
	case *ast.LetClause:
		return Cats(Text("let "), Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))
	case *ast.TryClause:
		return c.tryClause(x)
	default:
		return nil
	}
}

// forClause converts a ForClause.
func (c *converter) forClause(x *ast.ForClause) *Doc {
	parts := []*Doc{Text("for ")}
	if x.Key != nil {
		parts = append(parts, Text(x.Key.Name), Text(", "))
	}
	parts = append(parts, Text(x.Value.Name), Text(" in "), c.expr(x.Source))
	return Cats(parts...)
}

// tryClause converts a TryClause.
func (c *converter) tryClause(x *ast.TryClause) *Doc {
	if x.Ident != nil {
		return Cats(Text("try "), Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))
	}
	return Text("try")
}

// fallbackClause converts the FallbackClause of a Comprehension.
// The keyword depends on the comprehension's clauses: "otherwise" after
// for-clauses or multiple clauses, "else" after a single if/try clause.
func (c *converter) fallbackClause(comp *ast.Comprehension) *Doc {
	kw := "otherwise"
	if len(comp.Clauses) == 1 {
		if _, ok := comp.Clauses[0].(*ast.ForClause); !ok {
			kw = "else"
		}
	}
	return Cats(Text(" "), Text(kw), Text(" "), c.expr(comp.Fallback.Body))
}

// decl converts a declaration node to a Doc (without comments—those are
// handled by the caller in declSlice or expr).
func (c *converter) decl(d ast.Decl) *Doc {
	switch x := d.(type) {
	case *ast.Field:
		return c.field(x)

	case *ast.Alias:
		return Cats(Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))

	case *ast.EmbedDecl:
		return c.expr(x.Expr)

	case *ast.LetClause:
		return Cats(Text("let "), Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))

	case *ast.Ellipsis:
		return c.ellipsis(x)

	case *ast.Comprehension:
		return c.comprehension(x)

	case *ast.Package:
		return Cats(Text("package "), Text(x.Name.Name))

	case *ast.ImportDecl:
		return c.importDecl(x)

	case *ast.Attribute:
		return Text(x.Text)

	case *ast.CommentGroup:
		return c.commentGroup(x)

	case *ast.BadDecl:
		return Text("/* BadDecl */")

	default:
		return nil
	}
}

// field converts a Field to a Doc (full field, not table row).
func (c *converter) field(f *ast.Field) *Doc {
	key := c.fieldKey(f)
	val := c.fieldVal(f)

	// Handle position 1 and 2 comments on the field.
	for _, cg := range ast.Comments(f) {
		if cg.Position == 1 {
			key = Cat(key, c.commentSep(cg, c.commentGroup(cg)))
		} else if cg.Position == 2 {
			cd := c.commentGroup(cg)
			val = Cats(HardLine(), cd, HardLine(), val)
		}
	}

	// If the value has doc comments (and is therefore Nest-wrapped with
	// a leading HardLine), don't emit a space between key and value.
	if c.exprHasDocComment(f.Value) {
		return Cat(key, val)
	}
	return Cats(key, spaceText, val)
}

// fieldRow splits a Field into a table Row for alignment.
// Doc comments are prepended before the key (separated by HardLine so
// they don't affect key-width measurement). Trailing comments (line or
// position-based) are appended to the value. Position 1/2 comments are
// handled inline.
func (c *converter) fieldRow(f *ast.Field) Row {
	key := c.fieldKey(f)
	val := c.fieldVal(f)
	val = c.appendTrailingComments(f, val)

	var docComment *Doc
	hasComment := false

	for _, cg := range ast.Comments(f) {
		switch {
		case cg.Doc || cg.Position == 0:
			// Doc comment: goes before the key, doesn't affect key-width.
			if docComment == nil {
				docComment = c.commentGroup(cg)
			} else {
				docComment = Cats(docComment, HardLine(), c.commentGroup(cg))
			}
		case cg.Position == 1:
			// Between label and ":": append to key.
			key = Cat(key, c.commentSep(cg, c.commentGroup(cg)))
			hasComment = true
		case cg.Position == 2:
			// Between ":" and value: prepend to value.
			val = Cats(c.commentSep(cg, c.commentGroup(cg)), val)
			hasComment = true
		case cg.Line || cg.Position >= 3:
			// Trailing comment: already handled by appendTrailingComments.
			// Just check if it forces a break.
			hasComment = true
		}
	}

	return Row{
		DocComment: docComment,
		Key:        key,
		Val:        val,
		Comment:    hasComment,
	}
}

// appendTrailingComments appends any trailing comments (line comments or
// position-based comments after the value) to the doc.
// Comments with Newline/NewSection RelPos are placed on their own line(s)
// rather than on the same line.
func (c *converter) appendTrailingComments(n ast.Node, d *Doc) *Doc {
	for _, cg := range ast.Comments(n) {
		if cg.Doc || cg.Position == 0 {
			continue // handled elsewhere (withComments / fieldRow DocComment)
		}
		if !cg.Line && cg.Position < 3 {
			continue // internal position, handled by node-specific code
		}
		cd := c.commentGroup(cg)
		d = Cat(d, c.commentSep(cg, cd))
	}
	return d
}

// commentSep returns a Doc that places a comment with the appropriate
// separation based on its RelPos. Same-line comments get " // ...",
// while comments with Newline/NewSection get their own line(s).
func (c *converter) commentSep(cg *ast.CommentGroup, cd *Doc) *Doc {
	if cg.Pos().HasRelPos() {
		switch cg.Pos().RelPos() {
		case token.NewSection:
			return Cats(BlankLine(), cd)
		case token.Newline:
			return Cat(HardLine(), cd)
		}
	}
	return Cat(spaceText, cd)
}

// fieldKey builds the key portion of a field: label + alias + constraint + colon.
func (c *converter) fieldKey(f *ast.Field) *Doc {
	parts := []*Doc{c.label(f.Label)}

	if f.Alias != nil {
		parts = append(parts, c.postfixAlias(f.Alias))
	}

	switch f.Constraint {
	case token.OPTION:
		parts = append(parts, Text("?"))
	case token.NOT:
		parts = append(parts, Text("!"))
	}

	if f.Value != nil || f.TokenPos.IsValid() {
		parts = append(parts, colonText)
	}

	return Cats(parts...)
}

// fieldVal builds the value portion of a field: value + attributes.
// If the value expression has doc comments, the value is wrapped in
// Nest(1, HardLine + ...) so the comment and value are on their own
// lines, indented relative to the key.
func (c *converter) fieldVal(f *ast.Field) *Doc {
	val := c.expr(f.Value)

	for _, attr := range f.Attrs {
		val = Cats(val, spaceText, Text(attr.Text))
	}

	if c.exprHasDocComment(f.Value) {
		// The value has doc comments (from exprComments). Wrap in
		// Nest(1, HardLine + val) so the output is:
		//   key:
		//       // comment
		//       value
		val = Nest(1, Cat(HardLine(), val))
	}

	return val
}

// exprHasDocComment reports whether an expression or any of its
// descendant expressions has a doc comment attached.
func (c *converter) exprHasDocComment(e ast.Expr) bool {
	if e == nil {
		return false
	}
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if found {
			return false
		}
		for _, cg := range ast.Comments(n) {
			if cg.Doc || cg.Position == 0 {
				found = true
				return false
			}
		}
		return true
	}, nil)
	return found
}

// postfixAlias converts a PostfixAlias.
func (c *converter) postfixAlias(a *ast.PostfixAlias) *Doc {
	if a.Lparen.IsValid() {
		// Dual form: ~(K,V)
		return Cats(Text("~("), Text(a.Label.Name), commaText, Text(a.Field.Name), rParen)
	}
	// Simple form: ~X
	return Cat(Text("~"), Text(a.Field.Name))
}

// importDecl converts an ImportDecl.
func (c *converter) importDecl(x *ast.ImportDecl) *Doc {
	if !x.Lparen.IsValid() {
		// Single import without parens.
		if len(x.Specs) == 1 {
			return Cats(Text("import "), c.importSpec(x.Specs[0]))
		}
	}

	var specs []*Doc
	for i, s := range x.Specs {
		spec := c.importSpec(s)
		if i > 0 {
			var sep *Doc
			if s.Pos().HasRelPos() && s.Pos().RelPos() >= token.NewSection {
				sep = BlankLine()
			} else {
				sep = HardLine()
			}
			spec = Cat(sep, spec)
		}
		specs = append(specs, spec)
	}

	body := Cats(specs...)
	return Cats(
		Text("import ("),
		Nest(1, Cat(HardLine(), body)),
		HardLine(),
		rParen,
	)
}

// importSpec converts an ImportSpec.
func (c *converter) importSpec(s *ast.ImportSpec) *Doc {
	if s.Name != nil {
		return Cats(Text(s.Name.Name), spaceText, Text(s.Path.Value))
	}
	return Text(s.Path.Value)
}

// withComments wraps a Doc with its node's attached comments.
func (c *converter) withComments(n ast.Node, body *Doc) *Doc {
	cgs := ast.Comments(n)
	if len(cgs) == 0 {
		return body
	}

	var before []*Doc
	var after []*Doc

	for _, cg := range cgs {
		cdoc := c.commentGroup(cg)
		if cg.Doc || cg.Position == 0 {
			// Doc comment or position-0 comment: prepend before the body.
			before = append(before, cdoc, HardLine())
		} else {
			// All other comments (line, trailing, position 1/2/3+):
			// place after the body, honouring RelPos for placement.
			after = append(after, c.commentSep(cg, cdoc))
		}
	}

	result := body
	if len(before) > 0 {
		result = Cats(append(before, result)...)
	}
	if len(after) > 0 {
		result = Cats(append([]*Doc{result}, after...)...)
	}
	return result
}

// commentGroup converts a CommentGroup to a Doc.
func (c *converter) commentGroup(cg *ast.CommentGroup) *Doc {
	if len(cg.List) == 0 {
		return nil
	}
	var docs []*Doc
	for i, comment := range cg.List {
		if i > 0 {
			docs = append(docs, HardLine())
		}
		docs = append(docs, Text(comment.Text))
	}
	return Cats(docs...)
}
