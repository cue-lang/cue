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
			docs = append(docs, sep)
			docs = append(docs, doc)
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

// isSimpleField reports whether a field's value is NOT a struct or list,
// making it eligible for table alignment.
func (c *converter) isSimpleField(f *ast.Field) bool {
	if f.Value == nil {
		return false
	}
	switch f.Value.(type) {
	case *ast.StructLit, *ast.ListLit:
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
	return Line("")
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
			return Cats(Text(","), HardLine())
		case token.NewSection:
			return Cats(Text(","), BlankLine())
		}
		// Elided, NoSpace, Blank: fall through to default (comma required).
	}
	return Cats(Text(","), Line(" "))
}

// expr converts an expression node to a Doc.
func (c *converter) expr(x ast.Expr) *Doc {
	if x == nil {
		return nil
	}
	var d *Doc
	switch x := x.(type) {
	case *ast.Ident:
		d = Text(x.Name)

	case *ast.BasicLit:
		d = c.basicLit(x)

	case *ast.BottomLit:
		d = Text("_|_")

	case *ast.BadExpr:
		d = Text("/* BadExpr */")

	case *ast.StructLit:
		d = c.structLit(x)

	case *ast.ListLit:
		d = c.listLit(x)

	case *ast.Ellipsis:
		d = c.ellipsis(x)

	case *ast.Comprehension:
		d = c.comprehension(x)

	case *ast.UnaryExpr:
		d = c.unaryExpr(x)

	case *ast.BinaryExpr:
		d = c.binaryExpr(x)

	case *ast.PostfixExpr:
		d = Cat(c.expr(x.X), Text(x.Op.String()))

	case *ast.SelectorExpr:
		d = Cats(c.expr(x.X), Text("."), c.label(x.Sel))

	case *ast.IndexExpr:
		d = Cats(c.expr(x.X), Text("["), c.expr(x.Index), Text("]"))

	case *ast.SliceExpr:
		low := c.expr(x.Low)
		high := c.expr(x.High)
		d = Cats(c.expr(x.X), Text("["), low, Text(":"), high, Text("]"))

	case *ast.CallExpr:
		d = c.callExpr(x)

	case *ast.ParenExpr:
		d = Cats(Text("("), c.expr(x.X), Text(")"))

	case *ast.Interpolation:
		d = c.interpolation(x)

	case *ast.Func:
		d = c.funcExpr(x)

	case *ast.Alias:
		d = Cats(Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))

	default:
		d = Text("/* unknown expr */")
	}

	return d
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
			return Cats(Text("["), c.expr(x.Elts[0]), Text("]"))
		}
		return c.listLit(x)
	case *ast.ParenExpr:
		return Cats(Text("("), c.expr(x.X), Text(")"))
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
		Text("{"),
		Nest(1, Cat(openBreak, body)),
		closeBreak,
		Text("}"),
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

	var elems []*Doc
	for i, e := range x.Elts {
		// Skip elided elements.
		if e.Pos().HasRelPos() && e.Pos().RelPos() == token.Elided {
			continue
		}
		elem := c.expr(e)
		if i > 0 {
			// Honour RelPos between list elements.
			sep := c.elemSep(e)
			elem = Cat(sep, elem)
		}
		elems = append(elems, elem)
	}

	body := Cats(elems...)

	// Honour RelPos on the first element and closing bracket.
	openBreak := c.bracketBreak(x.Elts[0].Pos())
	closeBreak := c.bracketBreak(x.Rbrack)

	return Group(Cats(
		Text("["),
		Nest(1, Cat(openBreak, body)),
		TrailingComma(),
		closeBreak,
		Text("]"),
	))
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
		return Cats(Text(op), Text(" "), inner)
	}
	return Cat(Text(op), inner)
}

// binaryExpr converts a BinaryExpr.
func (c *converter) binaryExpr(x *ast.BinaryExpr) *Doc {
	// Disjunctions are flattened and formatted like a list:
	// either all on one line or all broken with "| " separators.
	if x.Op == token.OR {
		return c.disjunction(x)
	}

	left := c.expr(x.X)
	op := x.Op.String()
	right := c.expr(x.Y)

	// If RHS has Newline RelPos, honour it with a hard line break.
	// No Nest: the RHS stays at the current indent level to ensure
	// idempotency (nesting would accumulate on each re-format).
	if x.Y.Pos().HasRelPos() && x.Y.Pos().RelPos() >= token.Newline {
		return Group(Cats(left, Text(" "), Text(op), HardLine(), right))
	}

	return Group(Cats(left, Text(" "), Text(op), Line(" "), right))
}

// disjunct holds one arm of a flattened disjunction chain.
type disjunct struct {
	expr     ast.Expr
	opPos    token.Pos           // position of the "|" before this disjunct (invalid for first)
	exprPos  token.Pos           // position of the expression (for RelPos)
	comments []*ast.CommentGroup // comments from intermediate BinaryExpr nodes
}

// flattenDisjunction collects all disjuncts from a chain of "|" BinaryExprs,
// preserving comments from intermediate nodes.
// Both (a | b) | c and a | (b | c) are flattened to [a, b, c].
//
// Comments on a BinaryExpr node (e.g., "// first letter" on "A" | "B")
// are line comments that appear after the "|" on the same line as the LHS.
// They are collected into the comments field of the disjunct whose "|"
// separator they follow — i.e., the RHS disjunct. In the output they
// appear as: LHS_value | // comment \n RHS_value
func flattenDisjunction(x *ast.BinaryExpr) []disjunct {
	var result []disjunct

	// walkLeft walks the left spine of a left-associative chain.
	// Each BinaryExpr's comments belong to its direct RHS (the first
	// leaf of the Y subtree).
	var walkLeft func(e ast.Expr)
	walkLeft = func(e ast.Expr) {
		bin, ok := e.(*ast.BinaryExpr)
		if !ok || bin.Op != token.OR {
			result = append(result, disjunct{expr: e, exprPos: e.Pos()})
			return
		}
		walkLeft(bin.X)
		// Comments on this BinaryExpr (e.g., "// first letter" on "A"|"B")
		// belong to the first leaf of bin.Y — that's the disjunct whose
		// "|" separator they follow.
		cgs := ast.Comments(bin)
		addLeaf(bin.Y, bin.OpPos, cgs, &result)
	}

	// The outermost node's comments belong to its direct RHS.
	cgs := ast.Comments(x)
	walkLeft(x.X)
	addLeaf(x.Y, x.OpPos, cgs, &result)
	return result
}

// addLeaf adds a disjunct leaf, or recurses for right-associative chains.
// comments are attached to the first leaf reached.
func addLeaf(e ast.Expr, opPos token.Pos, comments []*ast.CommentGroup, result *[]disjunct) {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.OR {
		// Right-associative: a | (b | c).
		// The passed-in comments belong to the first leaf (b).
		// bin's own comments belong to the first leaf of bin.Y (c).
		addLeaf(bin.X, opPos, comments, result)
		addLeaf(bin.Y, bin.OpPos, ast.Comments(bin), result)
		return
	}
	*result = append(*result, disjunct{
		expr:     e,
		opPos:    opPos,
		exprPos:  e.Pos(),
		comments: comments,
	})
}

// disjunction formats a flattened disjunction chain.
// It behaves like a list: either all disjuncts on one line separated by " | ",
// or all broken to separate lines. When broken, each disjunct after the first
// is preceded by a line break followed by "| ", keeping "| {" together.
func (c *converter) disjunction(x *ast.BinaryExpr) *Doc {
	disjs := flattenDisjunction(x)

	// Build the first disjunct (no separator).
	first := c.expr(disjs[0].expr)

	// Build the rest with their separators. These are wrapped in
	// Nest(1, ...) so that when the group breaks, continuation lines
	// are indented one level deeper than the first disjunct.
	var rest []*Doc

	for _, d := range disjs[1:] {
		elem := c.expr(d.expr)

		// Determine separator based on RelPos of the disjunct's expression
		// or the "|" operator position.
		rel := token.NoRelPos
		if d.exprPos.HasRelPos() {
			rel = d.exprPos.RelPos()
		} else if d.opPos.HasRelPos() {
			rel = d.opPos.RelPos()
		}

		// Build comment doc from any comments on intermediate BinaryExpr
		// nodes. These are line comments (// ...) that appear after "|"
		// on the previous line.
		var commentDoc *Doc
		for _, cg := range d.comments {
			if cg.Line || cg.Position >= 1 {
				cd := c.commentGroup(cg)
				if commentDoc == nil {
					commentDoc = cd
				} else {
					commentDoc = Cats(commentDoc, Text(" "), cd)
				}
			}
		}

		// If there are comments, they appear after "|" and force a hard
		// break (// comments run to end of line).
		// Flat (no comments): " | disjunct"
		// Broken (no comments): " |" + newline + disjunct
		// With comments: " | // comment" + hardline + disjunct
		var sep *Doc
		if commentDoc != nil {
			sep = Cats(Text(" |"), Text(" "), commentDoc, HardLine())
		} else {
			switch {
			case rel >= token.Newline:
				sep = Cats(Text(" |"), HardLine())
			default:
				sep = Cats(Text(" |"), Line(" "))
			}
		}

		rest = append(rest, sep, elem)
	}

	return Group(Cat(first, Nest(1, Cats(rest...))))
}

// callExpr converts a CallExpr.
func (c *converter) callExpr(x *ast.CallExpr) *Doc {
	fun := c.expr(x.Fun)

	if len(x.Args) == 0 {
		return Cats(fun, Text("()"))
	}

	var args []*Doc
	for _, a := range x.Args {
		args = append(args, c.expr(a))
	}

	body := Sep(Cats(Text(","), Line(" ")), args...)

	return Group(Cats(
		fun,
		Text("("),
		Nest(1, Cat(Line(""), body)),
		TrailingComma(),
		Line(""),
		Text(")"),
	))
}

// interpolation converts an Interpolation node.
func (c *converter) interpolation(x *ast.Interpolation) *Doc {
	var parts []*Doc
	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			parts = append(parts, Text(lit.Value))
		} else {
			parts = append(parts, Cats(Text("\\("), c.expr(e), Text(")")))
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
	return Cats(Text("func"), Text("("), argDoc, Text("): "), c.expr(x.Ret))
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
				cl = Cat(Text(" "), cl)
			}
		}
		parts = append(parts, cl)
	}

	// Separator before the value (struct body).
	valSep := Text(" ")
	if x.Value != nil {
		pos := x.Value.Pos()
		if pos.HasRelPos() && pos.RelPos() >= token.Newline {
			valSep = HardLine()
		}
	}
	parts = append(parts, valSep)
	parts = append(parts, c.expr(x.Value))

	if x.Fallback != nil {
		parts = append(parts, c.fallbackClause(x.Fallback))
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

// fallbackClause converts a FallbackClause.
func (c *converter) fallbackClause(x *ast.FallbackClause) *Doc {
	return Cats(Text(" fallback "), c.expr(x.Body))
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
	return Cats(key, Text(" "), val)
}

// fieldRow splits a Field into a table Row for alignment.
// Doc comments are prepended before the key (separated by HardLine so
// they don't affect key-width measurement). Trailing comments (line or
// position-based) are appended to the value.
func (c *converter) fieldRow(f *ast.Field) Row {
	key := c.fieldKey(f)
	val := c.fieldVal(f)
	val = c.appendTrailingComments(f, val)

	// Prepend doc comments. These go before the key but must not
	// participate in key-width measurement for table alignment.
	var docComment *Doc
	for _, cg := range ast.Comments(f) {
		if cg.Doc || cg.Position == 0 {
			if docComment == nil {
				docComment = c.commentGroup(cg)
			} else {
				docComment = Cats(docComment, HardLine(), c.commentGroup(cg))
			}
		}
	}

	// Check if the field has a line comment (// ...). Such comments
	// run to end of line and force the enclosing struct to break.
	hasComment := false
	for _, cg := range ast.Comments(f) {
		if cg.Line || (!cg.Doc && cg.Position >= 3) {
			hasComment = true
			break
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
	return Cat(Text(" "), cd)
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
		parts = append(parts, Text(":"))
	}

	return Cats(parts...)
}

// fieldVal builds the value portion of a field: value + attributes.
func (c *converter) fieldVal(f *ast.Field) *Doc {
	val := c.expr(f.Value)

	for _, attr := range f.Attrs {
		val = Cats(val, Text(" "), Text(attr.Text))
	}

	return val
}

// postfixAlias converts a PostfixAlias.
func (c *converter) postfixAlias(a *ast.PostfixAlias) *Doc {
	if a.Lparen.IsValid() {
		// Dual form: ~(K,V)
		return Cats(Text("~("), Text(a.Label.Name), Text(","), Text(a.Field.Name), Text(")"))
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
		Text(")"),
	)
}

// importSpec converts an ImportSpec.
func (c *converter) importSpec(s *ast.ImportSpec) *Doc {
	if s.Name != nil {
		return Cats(Text(s.Name.Name), Text(" "), Text(s.Path.Value))
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
		} else if cg.Line || cg.Position >= 3 {
			// Trailing comment: honour its RelPos for placement.
			after = append(after, c.commentSep(cg, cdoc))
		}
		// Other position-based comments (1, 2) are internal to the node
		// and are handled within the specific node conversion.
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
