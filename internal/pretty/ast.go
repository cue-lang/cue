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
	"cuelang.org/go/internal"
)

// nodeFlag bits record precomputed properties of AST nodes used by
// the layout decision points (see [converter.precompute]).
type nodeFlag uint8

const (
	// flagNewlineInChildren marks a node whose strict descendants
	// carry a Newline or NewSection RelPos.
	flagNewlineInChildren nodeFlag = 1 << iota
	// flagCommentInSubtree marks a node whose subtree (including
	// itself) has an attached comment group.
	flagCommentInSubtree
)

// converter translates a CUE AST into the Doc algebra.
type converter struct {
	// nodeFlags stores per-node layout bits. Combining both flags
	// into one map halves hash-table overhead compared with two
	// parallel maps.
	nodeFlags map[ast.Node]nodeFlag
}

// node converts an AST node ([ast.File], [ast.Expr] or [ast.Decl]
// only) to a Doc.
func (c *converter) node(n ast.Node) Doc {
	c.precompute(n)
	switch n := n.(type) {
	case *ast.File:
		return Cat(c.maybeGroup(n, false, c.file(n)), HardLine())
	case ast.Expr:
		return c.expr(n)
	case ast.Decl:
		return c.decl(n)
	default:
		return nil
	}
}

// precompute populates c.newlineInChildren and c.commentInSubtree in
// a single ast.Walk over root's subtree. Each pair of flags is set by
// walking up the current ancestor stack and short-circuiting at the
// first already-marked ancestor, keeping total work amortized O(N).
func (c *converter) precompute(root ast.Node) {
	c.nodeFlags = make(map[ast.Node]nodeFlag)
	if root == nil {
		return
	}
	var stack []ast.Node
	ast.Walk(root,
		func(n ast.Node) bool {
			if n == nil {
				return false
			}
			stack = append(stack, n)
			if n.Pos().IsNewline() {
				for i := len(stack) - 2; i >= 0; i-- {
					f := c.nodeFlags[stack[i]]
					if f&flagNewlineInChildren != 0 {
						break
					}
					c.nodeFlags[stack[i]] = f | flagNewlineInChildren
				}
			}
			if len(ast.Comments(n)) > 0 {
				for i := len(stack) - 1; i >= 0; i-- {
					f := c.nodeFlags[stack[i]]
					if f&flagCommentInSubtree != 0 {
						break
					}
					c.nodeFlags[stack[i]] = f | flagCommentInSubtree
				}
			}
			return true
		},
		func(n ast.Node) {
			stack = stack[:len(stack)-1]
		})
}

// file renders a *ast.File. The separator between File-level leading
// comments and the first decl honours the first decl's RelPos, so a
// blank line between a header comment and e.g. the package
// declaration is preserved. Separators *between* leading comments
// follow the same rule applied to each comment's own RelPos: a
// NewSection between two file-level comments produces a BlankLine
// (collapsing any number of source blank lines to one), so a chain
// of `// c1\n\n// c2\n\n// c3` is not silently squashed into three
// adjacent lines.
func (c *converter) file(f *ast.File) Doc {
	body := c.declSlice(f.Decls)

	var leading, trailing []*ast.CommentGroup
	for _, cg := range ast.Comments(f) {
		if cg.Position == posDoc {
			leading = append(leading, cg)
		} else {
			trailing = append(trailing, cg)
		}
	}

	// Separator between the last leading comment and the body. Use
	// leadingRel so a NewSection on the first decl's own doc comment
	// (the first visible token after the file-level leading comments)
	// is honoured — otherwise a blank line between a file-level
	// comment and the next decl's doc comment is silently dropped.
	firstDeclSep := HardLine()
	if len(f.Decls) > 0 && leadingRel(f.Decls[0]) == token.NewSection {
		firstDeclSep = BlankLine()
	}

	parts := make([]Doc, 0, len(leading)*2+1+len(trailing)*2)
	for i, cg := range leading {
		parts = append(parts, c.commentGroup(cg))
		switch {
		case i == len(leading)-1:
			parts = append(parts, firstDeclSep)
		case leading[i+1].Pos().RelPos() == token.NewSection:
			parts = append(parts, BlankLine())
		default:
			parts = append(parts, HardLine())
		}
	}
	parts = append(parts, body)
	for _, cg := range trailing {
		parts = append(parts, c.commentSep(cg, c.commentGroup(cg)), IfBreak(nil, HardLine()))
	}
	return Cats(parts...)
}

// declSlice joins a slice of Decls with RelPos-driven separators.
func (c *converter) declSlice(decls []ast.Decl) Doc {
	if len(decls) == 0 {
		return nil
	}

	docs := make([]Doc, 0, len(decls))
	tableRows := make([]Row, 0, len(decls))
	hasAligned := false // true if tableRows contains at least one aligned row
	curChainLen := 0    // chain length of the most recent aligned row (0 if none)

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}

		if !hasAligned {
			// No aligned rows - just emit raw rows directly.
			for _, row := range tableRows {
				docs = append(docs, row.Sep, row.Raw)
			}
		} else {
			docs = append(docs, tableRows[0].Sep, Table(tableRows))
		}
		tableRows = nil
		hasAligned = false
		curChainLen = 0
	}

	var prev ast.Decl
	for _, decl := range decls {
		// Elided declarations are skipped entirely.
		if decl.Pos().RelPos() == token.Elided {
			continue
		}

		var sep Doc
		if prev != nil {
			sep = c.declSep(decl, prev)
		}
		prev = decl

		// Skip pure comment groups as declarations; they're handled via
		// withComments on the nodes they're attached to.
		if _, ok := decl.(*ast.CommentGroup); ok {
			flushTable()
			docs = append(docs, sep, c.decl(decl))
			continue
		}

		// A blank line separator breaks the table - alignment doesn't
		// cross visual section boundaries.
		if sep == blankLine {
			flushTable()
		}

		if f, ok := decl.(*ast.Field); ok && c.isSimpleField(f) {
			row, postComments, chainLen := c.fieldRow(f)
			row.Sep = sep
			// A doc comment visually separates fields, so flush
			// the table to prevent alignment across the comment.
			if row.DocComment != nil && len(tableRows) > 0 {
				flushTable()
			}
			// A change in the composite-key chain length (e.g. from
			// `a: b: 1` to `c: d: e: 2`) gives the first column a
			// different shape, so values would no longer line up
			// meaningfully. Flush the table to start a fresh
			// alignment group.
			if curChainLen != 0 && curChainLen != chainLen {
				flushTable()
			}
			tableRows = append(tableRows, row)
			hasAligned = true
			curChainLen = chainLen
			// Post-field block comments were attached to the field by
			// the parser but are visually separate from its row. Emit
			// them as sibling blocks after flushing the table so they
			// keep their original position instead of being folded into
			// the row's value cell.
			if len(postComments) > 0 {
				flushTable()
				for _, cg := range postComments {
					cgSep := HardLine()
					if cg.Pos().RelPos() == token.NewSection {
						cgSep = BlankLine()
					}
					docs = append(docs, cgSep, c.commentGroup(cg))
				}
			}
			continue
		}

		// Complex field or non-field decl: add as a raw row in the
		// table so alignment spans across it. Fields handle their
		// own comments; other decls are wrapped in withComments.
		var doc Doc
		if _, ok := decl.(*ast.Field); ok {
			doc = c.decl(decl)
		} else {
			doc = c.withComments(decl, c.decl(decl))
		}
		tableRows = append(tableRows, Row{Sep: sep, Raw: doc})
	}
	flushTable()

	return Cats(docs...)
}

// isSimpleField reports whether a field is eligible for table
// alignment. It follows any braceless chain (x: y: z: val) to the
// leaf and checks the leaf's value. A StructLit or ListLit value
// still qualifies: whether it renders without newlines is decided
// at render time by the table's row partitioning, which isolates
// rows whose values do wrap so they do not stretch the surrounding
// alignment.
func (c *converter) isSimpleField(f *ast.Field) bool {
	chain, collapsible := unchainField(f)
	if len(chain) > 1 && !collapsible {
		return false
	}
	leaf := chain[len(chain)-1]
	if leaf.Value == nil {
		return false
	}
	// Fields whose value has doc comments need special formatting
	// (indented on their own line) and can't participate in table
	// rows.
	if c.exprHasDocComment(leaf.Value) {
		return false
	}
	return true
}

// unchainField walks a braceless field chain (x: y: z: val is a
// chain of three Fields) and returns the sequence of Fields from the
// head to the leaf. collapsible reports whether the chain can be
// safely rendered as a single composite key + leaf value. A chain
// is collapsible only when:
//   - every intermediate StructLit and every Field after the head is
//     comment-free (any comment would either be lost or land in the
//     wrong place), and
//   - no Field after the head carries Newline/NewSection RelPos (the
//     user wrote it on its own line and that break must be kept), and
//   - the head has no Position=1/2 comments (which would otherwise
//     need to appear in the middle of the composite key).
func unchainField(f *ast.Field) (chain []*ast.Field, collapsible bool) {
	chain = []*ast.Field{f}
	cur := f
	for {
		sl, ok := cur.Value.(*ast.StructLit)
		if !ok || sl.Lbrace.IsValid() || len(sl.Elts) != 1 || len(ast.Comments(sl)) > 0 {
			break
		}
		inner, ok := sl.Elts[0].(*ast.Field)
		if !ok {
			break
		}
		chain = append(chain, inner)
		cur = inner
	}
	if len(chain) < 2 {
		return chain, false
	}
	for _, cf := range chain[1:] {
		if len(ast.Comments(cf)) > 0 {
			return chain, false
		}
		if cf.Pos().IsNewline() {
			return chain, false
		}
	}
	for _, cg := range ast.Comments(f) {
		if cg.Position == posPrefix || cg.Position == posSuffix {
			return chain, false
		}
	}
	return chain, true
}

// declSep computes the separator before a declaration based on its
// RelPos. RelPos values are honoured only if they produce valid
// syntax. Newline always produces a hard line break, NewSection
// always produces a blank line. NoSpace and Blank are overridden to
// SoftComma because declarations require at least a comma or newline
// between them.
//
// Additionally, a blank line is inserted before a doc-commented
// declaration when the previous declaration is a definition (#Foo) or
// a non-field declaration. This matches cue/format's visual grouping
// heuristic.
func (c *converter) declSep(d ast.Decl, prev ast.Decl) Doc {
	rel := leadingRel(d)

	// If the current declaration has doc comments and the previous
	// declaration is a definition or a non-field declaration, upgrade
	// to a blank line (unless already a NewSection or higher).
	if rel < token.NewSection && prev != nil && hasDocComment(d) {
		if field, ok := prev.(*ast.Field); ok {
			if internal.IsDefinition(field.Label) {
				rel = token.NewSection
			}
		} else {
			// Non-field declarations (let, embed, comprehension, etc.)
			// always get a blank line before a doc-commented sibling.
			if _, ok := prev.(*ast.CommentGroup); !ok {
				rel = token.NewSection
			}
		}
	}

	switch rel {
	case token.Newline:
		// Newline is always honoured as a hard line break.
		return HardLine()
	case token.NewSection:
		// NewSection is always honoured as a blank line.
		return BlankLine()
	default:
		return SoftLineComma()
	}
}

// hasDocComment reports whether a node has any doc comments.
func hasDocComment(n ast.Node) bool {
	for _, cg := range ast.Comments(n) {
		if cg.Position == posDoc {
			return true
		}
	}
	return false
}

// hasAnyCommentInSubtree reports whether any node in the subtree
// rooted at n has at least one attached comment. Used by the
// single-element hug paths (structLit, listLit, callExpr) to disable
// the hug when the content carries //-comments anywhere — those
// comments run to end-of-line and would make the inline
// "{decl}" / "[elem]" / "fn(arg)" layout misread.
func (c *converter) hasAnyCommentInSubtree(n ast.Node) bool {
	return n != nil && c.nodeFlags[n]&flagCommentInSubtree != 0
}

// shouldHug reports whether a bracketed construct with a single
// child should wrap its open/close tokens directly around the
// child's Doc, bypassing the usual Group+Nest layout. The hug
// defeats the cascade that would otherwise double-indent a child
// that is already rendering with forced breaks: produces
// "{a: {...}}" rather than "{\n    a: {...}\n}". It applies when:
//   - the child has no explicit Newline on its own Pos (otherwise
//     the user wrote it on a new line and that break must be kept);
//   - some Newline/NewSection RelPos exists in the child's subtree,
//     so the child WILL render with forced breaks anyway;
//   - the child has no comments attached to itself: a doc comment
//     would land right after the parent's opener (no space), and a
//     trailing line comment would swallow the parent's closer.
//     Comments deep in descendants are safe — they live inside their
//     own brackets and never reach the outer parent's boundary.
//
// When the child can fit flat, the standard Group-based path handles
// width-based breaking correctly.
func (c *converter) shouldHug(child ast.Node) bool {
	return child != nil &&
		!child.Pos().IsNewline() &&
		len(ast.Comments(child)) == 0 &&
		c.hasNewlineInSubtree(child)
}

// hasNewlineInSubtree reports whether any node in the subtree rooted
// at n carries a Newline or NewSection RelPos. Used by the
// single-element hug paths to decide whether the inner content will
// render with forced breaks. When it will, the outer brackets should
// hug the content (so its HardLines don't cascade the outer group
// into a doubly-broken layout). When it won't, the standard Group
// path handles width-based breaking correctly.
func (c *converter) hasNewlineInSubtree(n ast.Node) bool {
	if n == nil {
		return false
	}
	return n.Pos().IsNewline() || c.nodeFlags[n]&flagNewlineInChildren != 0
}

// selfManagesComments reports whether an expression's conversion
// already bakes its attached comments into the returned Doc. For
// these nodes, callers must skip withComments to avoid rendering
// comments twice.
//   - BinaryExpr: routed through binaryExpr which splits chains with
//     trailing // comments to chainTableExpr and others to
//     binaryExprPrec — both place comments themselves.
//   - StructLit / ListLit: interior comments ("{ // c }") belong
//     inside the braces/brackets, not after them.
func selfManagesComments(x ast.Expr) bool {
	switch x.(type) {
	case *ast.BinaryExpr, *ast.StructLit, *ast.ListLit:
		return true
	}
	return false
}

// joinLines appends cd below acc, separated by a HardLine. A nil acc
// is replaced by cd directly.
func joinLines(acc, cd Doc) Doc {
	if acc == nil {
		return cd
	}
	return Cats(acc, HardLine(), cd)
}

// maybeGroup wraps body in a docGroup only when neither user-written
// newlines (n's subtree carries Newline/NewSection RelPos) nor the
// same-line-opener rule (shareIndent) require otherwise. In both
// forced cases the body is pre-flattened instead: soft Line nodes
// are replaced by their Alt text (so Wadler-Lindig will not inject
// additional width-driven newlines at this level) while HardLine /
// LitLine / BlankLine nodes are preserved. Nested docGroups are
// left untouched so RelPos-free pockets retain their own width-based
// flat/break decisions.
//
// The shareIndent path keeps openers that share a line together: the
// parent must not introduce its own width-driven break between them,
// but the inner Group is still free to break on its own width budget.
func (c *converter) maybeGroup(n ast.Node, shareIndent bool, body Doc) Doc {
	if shareIndent || c.nodeFlags[n]&flagNewlineInChildren != 0 {
		return preFlatten(body)
	}
	return Group(body)
}

// preFlatten rewrites a Doc so that soft line breaks (docLine)
// become their Alt text and docIfBreak resolves to its Broken
// branch, while docHard / docLitLine are kept intact. Nested
// docGroups are preserved verbatim: they remain independent
// fit-test units so a RelPos-free subtree inside a RelPos-carrying
// parent can still break on width. Returns d unchanged when the
// subtree contains nothing to rewrite (checked in O(1) via the
// precomputed needsPreFlatten bit on each Doc).
func preFlatten(d Doc) Doc {
	if d == nil || !d.needsPreFlatten() {
		return d
	}
	switch x := d.(type) {
	case *docLine:
		return Text(x.Alt)
	case *docIfBreak:
		return preFlatten(x.Broken)
	case *docCat:
		return Cat(preFlatten(x.Left), preFlatten(x.Right))
	case *docNest:
		return Nest(preFlatten(x.Child))
	case *docTable:
		rows := make([]Row, len(x.Rows))
		for i, row := range x.Rows {
			nr := Row{
				Sep:        preFlatten(row.Sep),
				Raw:        preFlatten(row.Raw),
				DocComment: preFlatten(row.DocComment),
				Cells:      row.Cells,
				HasComment: row.HasComment,
			}
			for j, cell := range row.Cells {
				nc := preFlatten(cell)
				if nc != cell {
					if &nr.Cells[0] == &row.Cells[0] {
						nr.Cells = append([]Doc(nil), row.Cells...)
					}
					nr.Cells[j] = nc
				}
			}
			rows[i] = nr
		}
		return Table(rows)
	}
	return d
}

// relBreak returns the break Doc corresponding to a RelPos:
// BlankLine for NewSection, HardLine for Newline, and a soft Line
// (empty when flat, newline when broken) otherwise. measureFlat
// treats nested Groups as independent fit units, so a Group that
// decides to break does not cascade its soft breaks into the outer
// Group.
func relBreak(rel token.RelPos) Doc {
	switch rel {
	case token.NewSection:
		return BlankLine()
	case token.Newline:
		return HardLine()
	}
	return noSepLine
}

// leadingRel returns the RelPos that drives the separator placed
// before n's first visible token. If n has a Position=0 doc comment,
// the first visible token is that comment, so its RelPos wins;
// otherwise n's own RelPos applies. Distinguishing "blank line before
// the comment" from "blank line between the comment and the body"
// lets callers render each side correctly instead of collapsing both
// to a single decision.
func leadingRel(n ast.Node) token.RelPos {
	for _, cg := range ast.Comments(n) {
		if cg.Position == posDoc {
			return cg.Pos().RelPos()
		}
	}
	return n.Pos().RelPos()
}

// expr converts an expression node to a Doc, including any comments.
func (c *converter) expr(x ast.Expr) Doc {
	if x == nil {
		return nil
	}
	doc := c.exprCore(x)
	if selfManagesComments(x) {
		return doc
	}
	return c.withComments(x, doc)
}

// exprCore converts an expression without handling comments on it.
// Comments are handled by the caller (expr or listElem).
func (c *converter) exprCore(x ast.Expr) Doc {
	if x == nil {
		return nil
	}
	switch x := x.(type) {
	case *ast.Ident:
		return Text(x.Name)

	case *ast.BasicLit:
		return c.basicLit(x)

	case *ast.BottomLit:
		return bottomText

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
		return c.binaryExpr(x)

	case *ast.PostfixExpr:
		return c.postfixExpr(x)

	case *ast.SelectorExpr:
		return Cats(c.expr(x.X), periodText, c.label(x.Sel))

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
		// In expression position (including inside pattern labels
		// like [x=string]) aliases use tight "=" without surrounding
		// spaces. Decl-position aliases are rendered by decl() with
		// " = " instead.
		return Cats(Text(x.Ident.Name), equalsText, c.expr(x.Expr))

	default:
		return Text("/* unknown expr */")
	}
}

// label converts a Label node to a Doc.
func (c *converter) label(l ast.Label) Doc {
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
		return Cats(Text(x.Ident.Name), equalsText, c.expr(x.Expr))
	default:
		return c.expr(l.(ast.Expr))
	}
}

// basicLit converts a BasicLit. Multi-line strings contain literal
// newlines in their Value; we split on those and join with LitLine
// (bare newlines without indentation) so the string content is
// reproduced verbatim.
func (c *converter) basicLit(x *ast.BasicLit) Doc {
	lines := strings.Split(x.Value, "\n")
	parts := make([]Doc, 0, len(lines)*2-1)
	// We have to intersperse LitLine directly here. Using
	// Sep(litLine, parts) wouldn't work because some parts could be
	// nil (Text("") gives nil) and Sep skips over nil parts.
	for i, line := range lines {
		if i > 0 {
			parts = append(parts, litLine)
		}
		parts = append(parts, Text(line))
	}
	return Cats(parts...)
}

// structLit converts a StructLit. Interior comments attached directly
// to the StructLit (Position > 0, not a trailing // line) are rendered
// inside the braces. Doc and trailing comments on the StructLit itself
// are wrapped around the result. expr() skips withComments for
// StructLit because interior comments must go inside the braces, not
// after them.
func (c *converter) structLit(x *ast.StructLit) Doc {
	if !x.Lbrace.IsValid() {
		return c.declSlice(x.Elts)
	}

	slots := classifyComments(x)
	hasInterior := len(slots.prefix) > 0 || len(slots.suffix) > 0

	var body Doc
	switch {
	case len(x.Elts) == 0 && !hasInterior:
		body = Text("{}")

	case len(x.Elts) == 1 && !hasInterior && c.shouldHug(x.Elts[0]):
		body = Cats(lBrace, c.decl(x.Elts[0]), rBrace)

	default:
		var firstElt ast.Node
		if len(x.Elts) > 0 {
			firstElt = x.Elts[0]
		}
		lineHeader := hasLineLeadingComment(slots, firstElt)
		var openBreak Doc
		switch {
		case lineHeader:
			// User wrote `{ // c` (a Line=true comment trailing the
			// opener). Keep the space-then-comment-then-break shape
			// instead of breaking right after `{`.
			openBreak = spaceText
		case hasInterior:
			// Interior // comments would swallow '}' if rendered
			// flat; force the enclosing group to break.
			openBreak = HardLine()
		case len(x.Elts) > 0:
			openBreak = relBreak(leadingRel(x.Elts[0]))
		default:
			openBreak = relBreak(x.Lbrace.RelPos())
		}
		closeBreak := relBreak(x.Rbrace.RelPos())
		if lineHeader && x.Rbrace.RelPos() < token.Newline {
			// A `//` header runs to end-of-line, so `}` must start a
			// fresh line — even when the user's original RelPos didn't
			// request one. Without this the comment would swallow `}`.
			closeBreak = HardLine()
		}

		inner := c.declSlice(x.Elts)
		inner = c.prependInteriorComments(slots.prefix, inner)
		inner = c.appendInteriorComments(inner, slots.suffix)

		// Same-line-opener rule: see listLit for the full explanation.
		// For a struct, an embed decl whose expression is itself a
		// brace-opener starts contiguously with `{`; fields never do
		// (they start with a label).
		cleanStruct := !hasInterior && !anyHasDocComment(x.Elts)
		hugFirst := cleanStruct &&
			len(x.Elts) > 0 &&
			nodeIsContiguousOpener(x.Elts[0]) &&
			!x.Elts[0].Pos().IsNewline()
		hugLast := cleanStruct &&
			len(x.Elts) > 0 &&
			nodeIsContiguousOpener(x.Elts[len(x.Elts)-1]) &&
			x.Rbrace.RelPos() < token.Newline
		shareIndent := cleanStruct && !hugFirst && hasSameLineOpener(x.Elts)

		nested := Nest(Cat(openBreak, inner))
		switch {
		case hugFirst:
			nested = inner
		case shareIndent:
			nested = Cat(openBreak, inner)
		}
		if hugLast {
			closeBreak = nil
		}

		body = c.maybeGroup(x, shareIndent, Cats(
			lBrace,
			nested,
			closeBreak,
			rBrace,
		))
	}

	return c.applyBeforeAfter(slots.doc, x.Pos().RelPos(), body, slots.trailing)
}

// listLit converts a ListLit. As for structLit, interior comments
// attached directly to the ListLit are rendered inside the brackets
// and expr() skips withComments for ListLit.
func (c *converter) listLit(x *ast.ListLit) Doc {
	hasBrackets := x.Lbrack.IsValid()

	if !hasBrackets {
		// Shouldn't normally happen for lists, but respect the AST.
		elems := make([]Doc, 0, len(x.Elts))
		for _, e := range x.Elts {
			elems = append(elems, c.expr(e))
		}
		return Sep(commaSpaceText, elems...)
	}

	unelided := make([]ast.Expr, 0, len(x.Elts))
	for _, e := range x.Elts {
		if e.Pos().RelPos() == token.Elided {
			continue
		}
		unelided = append(unelided, e)
	}

	slots := classifyComments(x)
	hasInterior := len(slots.prefix) > 0 || len(slots.suffix) > 0

	var body Doc
	switch {
	case len(unelided) == 0 && !hasInterior:
		body = Text("[]")

	case len(unelided) == 1 && !hasInterior && c.shouldHug(unelided[0]):
		body = Cats(lBracket, c.expr(unelided[0]), rBracket)

	default:
		var firstElem ast.Node
		if len(unelided) > 0 {
			firstElem = unelided[0]
		}
		lineHeader := hasLineLeadingComment(slots, firstElem)
		var openBreak Doc
		switch {
		case lineHeader:
			// User wrote `[ // c` — keep the comment on `[`'s line
			// with a space rather than breaking immediately.
			openBreak = spaceText
		case hasInterior:
			openBreak = HardLine()
		case len(unelided) > 0:
			openBreak = relBreak(leadingRel(unelided[0]))
		default:
			openBreak = relBreak(x.Lbrack.RelPos())
		}
		closeBreak := relBreak(x.Rbrack.RelPos())
		if lineHeader && x.Rbrack.RelPos() < token.Newline {
			// `// c` runs to end-of-line, so `]` must start a fresh
			// line even when the source didn't request a Newline.
			closeBreak = HardLine()
		}

		// Same-line-opener rule: whenever any element opener shares a
		// line with `[` (or `]`), the two openers share one level of
		// indent — the inner content sits at +1 from the list's parent,
		// not +2. The first-element opener case lets `[{` stay adjacent
		// even in broken mode: openBreak and the list's Nest are both
		// dropped so the inner element's own Group decides if/where to
		// break. The symmetric last-element case keeps `}]` adjacent and
		// suppresses the trailing comma (there is no line between them).
		// For openers in middle positions (e.g. `[1, {...}]`) only the
		// list's Nest is dropped — openBreak/closeBreak still fire on
		// the surrounding non-opener elements.
		//
		// Only "clean" lists can hug their openers/closers. Any doc
		// comment on an element, or any interior comment attached to the
		// list, forces a line break at the list level (independent of
		// where the brace-opener is), so the list must render as a true
		// multi-line bracket pair with `]` on its own line and a
		// trailing comma.
		cleanList := !hasInterior && !anyHasDocComment(unelided)
		hugFirst := cleanList &&
			len(unelided) > 0 &&
			isContiguousOpener(unelided[0]) &&
			!unelided[0].Pos().IsNewline()
		hugLast := cleanList &&
			len(unelided) > 0 &&
			isContiguousOpener(unelided[len(unelided)-1]) &&
			x.Rbrack.RelPos() < token.Newline
		// shareIndent fires when an opener appears later in the list
		// (after non-opener elements) but still on `[`'s line. Only the
		// Nest is dropped in this case; the list's openBreak still runs
		// before the first element.
		shareIndent := cleanList && !hugFirst && hasSameLineOpener(unelided)

		// A trailing comma on the last element is only wanted when the
		// list's own `]` will close on a new line. In the Group path
		// (no user newlines in the subtree) that's handled by the
		// IfBreak inside TrailingComma(). In the preFlatten path,
		// IfBreak always resolves to its Broken branch, so we have to
		// decide statically: a trailing comma is correct only when
		// Rbrack has Newline/NewSection RelPos. An element with a
		// Newline RelPos only breaks that element's position, not the
		// closing bracket's — `[1, 2,\n3]` still hugs `]` to `3`.
		wantTrailingComma := true
		// shareIndent forces preFlatten at the list level (so the
		// openers stay together on `[`'s line), which resolves the
		// trailing comma's IfBreak statically. Use the same Rbrack-
		// based decision the flagNewlineInChildren branch already uses
		// so a flat list does not acquire a spurious trailing comma.
		if shareIndent || c.nodeFlags[x]&flagNewlineInChildren != 0 {
			wantTrailingComma = x.Rbrack.RelPos() >= token.Newline
		}
		if hugLast {
			wantTrailingComma = false
		}

		var inner Doc
		if len(unelided) > 0 {
			rows := make([]Row, 0, len(unelided))
			lastElemIdx := len(unelided) - 1
			for i, e := range unelided {
				row, postCgs := c.listElemRow(e, i == lastElemIdx, wantTrailingComma)
				if i > 0 {
					row.Sep = c.elemBreak(e)
				}
				rows = append(rows, row)
				rows = append(rows, c.postCommentRows(postCgs)...)
			}
			inner = Table(rows)
		}
		inner = c.prependInteriorComments(slots.prefix, inner)
		inner = c.appendInteriorComments(inner, slots.suffix)

		nested := Nest(Cat(openBreak, inner))
		switch {
		case hugFirst:
			// Drop openBreak entirely and skip the list's Nest: the
			// inner element's own Nest already provides one indent.
			nested = inner
		case shareIndent:
			// A non-first opener sits on `[`'s line. Keep openBreak so
			// the leading non-opener element renders normally, but skip
			// the list's Nest so the opener's content shares indent.
			nested = Cat(openBreak, inner)
		}
		if hugLast {
			// Drop closeBreak so `}]` stays adjacent regardless of mode.
			closeBreak = nil
		}

		body = c.maybeGroup(x, shareIndent, Cats(
			lBracket,
			nested,
			closeBreak,
			rBracket,
		))
	}

	return c.applyBeforeAfter(slots.doc, x.Pos().RelPos(), body, slots.trailing)
}

// isContiguousOpener reports whether e begins with an opening
// `{`, `[`, or `(` — i.e. it is the kind of expression whose own
// rendering introduces a Nest for its inner content. Used to detect
// contiguous opener chains (`[{`, `[[`, `({`, ...) so callers can
// suppress their own Nest and avoid doubling up the indent.
func isContiguousOpener(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.StructLit:
		return x.Lbrace.IsValid()
	case *ast.ListLit:
		return x.Lbrack.IsValid()
	case *ast.ParenExpr:
		return true
	}
	return false
}

// nodeIsContiguousOpener reports whether n's rendering begins with
// an opening `{`, `[`, or `(`. For Exprs that's a StructLit, ListLit,
// or ParenExpr; for Decls only an EmbedDecl wrapping such an Expr
// qualifies (fields always begin with a label).
func nodeIsContiguousOpener(n ast.Node) bool {
	if e, ok := n.(*ast.EmbedDecl); ok {
		return isContiguousOpener(e.Expr)
	}
	if e, ok := n.(ast.Expr); ok {
		return isContiguousOpener(e)
	}
	return false
}

// hasSameLineOpener walks elements left-to-right looking for the
// first opener that the user wrote on the parent's opener line.
// Walking stops at the first element with a Newline RelPos: that
// element is on a new line, and so are all later ones. Used to
// implement the same-line-opener rule: when any element opener
// shares a line with the parent's `[`/`{`/`(`, the parent suppresses
// its own Nest so the inner content sits at one shared indent level.
func hasSameLineOpener[T ast.Node](elems []T) bool {
	for _, e := range elems {
		if e.Pos().IsNewline() {
			return false
		}
		if nodeIsContiguousOpener(e) {
			return true
		}
	}
	return false
}

// firstChildHasLineDoc reports whether n's leading doc comment
// (Position==0) is Line=true — the parser attached the comment as a
// doc on n but the comment itself trails the previous token (the
// parent's opener) on the same line. Concretely the user wrote
// `{ // c\n field`, `[ // c\n elem`, or `( // c\n arg`, and the
// `// c` was hung off the first child.
func firstChildHasLineDoc(n ast.Node) bool {
	if n == nil {
		return false
	}
	for _, cg := range ast.Comments(n) {
		if cg.Position == posDoc {
			return cg.Line
		}
	}
	return false
}

// hasLineLeadingComment reports whether the parent's opener line ends
// with a `//` comment that the formatter must keep there. It returns
// true in two cases:
//
//   - slots.prefix's first comment is Line=true: a comment attached
//     to the bracketed node itself, written as `{ // c` immediately
//     after the opener.
//   - the first element/decl carries a Line=true Position=0 doc
//     comment — see [firstChildHasLineDoc].
//
// In both shapes the renderer should emit a space — not a hard break
// — between the opener and the comment so the user's "header" comment
// keeps its position. The user's exact spacing between the opener and
// the `//` is not preserved: a single space is always emitted, since
// CUE's parser collapses both adjacency and any-amount-of-whitespace
// into the same RelPos (Blank).
func hasLineLeadingComment(slots commentSlots, first ast.Node) bool {
	if len(slots.prefix) > 0 && slots.prefix[0].Line {
		return true
	}
	return firstChildHasLineDoc(first)
}

// anyHasDocComment reports whether any node in the slice carries a
// Position=0 (doc) comment. Used by the same-line-opener rule to
// disqualify a list/struct/call from "hugging" its brackets: a doc
// comment forces a multi-line layout independent of the inner
// element's own break decisions.
func anyHasDocComment[T ast.Node](nodes []T) bool {
	for _, n := range nodes {
		if hasDocComment(n) {
			return true
		}
	}
	return false
}

// elemBreak returns just the line-break portion of a list element
// separator, without the comma (the comma is handled by listElem).
// Uses leadingRel so a NewSection on the element's doc comment
// (meaning the user wrote a blank line before the comment) becomes a
// blank line between this element and the previous one — placed
// before the comment, not between the comment and the body.
func (c *converter) elemBreak(e ast.Expr) Doc {
	switch leadingRel(e) {
	case token.Newline:
		return HardLine()
	case token.NewSection:
		return BlankLine()
	}
	return softLineSpace
}

// listElemRow builds a Row for a list element (or call argument).
// The Row has cells [value+comma, trailing-//-comment] so trailing
// comments align in a column across rows when the list is rendered
// broken. Doc comments become Row.DocComment and render above the
// value without contributing to column widths.
//
// For the last element, the separator depends on trailing: when
// trailing is true (list literals) it is a TrailingComma emitted
// only in broken mode, so an inline list does not acquire a spurious
// comma; when trailing is false (function-call arguments) no comma
// is emitted at all.
//
// Comments attached to e at Position 1/2 with Line=false are
// "post-element" comments: the user wrote them on their own line
// after the element (e.g. `[a, b,\n\n// c\n]` where `// c` sits before
// `]` and the parser hangs it off `b` because there is no following
// element). They cannot be folded into the element's cell — that
// would put the trailing comma after `// c`, which then absorbs the
// comma. They are returned to the caller to be emitted as separate
// Raw rows after the element's row.
func (c *converter) listElemRow(e ast.Expr, last, trailing bool) (Row, []*ast.CommentGroup) {
	doc := c.exprCore(e)

	var comma Doc
	switch {
	case !last:
		comma = commaText
	case trailing:
		comma = TrailingComma()
	}

	// Self-managed nodes (BinaryExpr/StructLit/ListLit) already bake
	// their attached comments into doc; don't try to split comments
	// here. Emit as a Raw row so the value renders as-is.
	if selfManagesComments(e) {
		return Row{Raw: Cat(doc, comma)}, nil
	}

	var docComment, trailingComment Doc
	var postComments []*ast.CommentGroup
	hasComment := false
	for _, cg := range ast.Comments(e) {
		switch {
		case cg.Position == posDoc:
			docComment = joinLines(docComment, c.commentGroup(cg))
		case cg.Line:
			hasComment = true
			trailingComment = joinLines(trailingComment, c.commentGroup(cg))
		default:
			// Position 1/2 with Line=false: an own-line comment hung
			// off this element by the parser. Render it as its own row
			// after the element so the trailing comma stays glued to
			// the value and is not swallowed by `//`.
			postComments = append(postComments, cg)
		}
	}

	cells := []Doc{Cat(doc, comma)}
	if trailingComment != nil {
		cells = append(cells, trailingComment)
	}

	// The doc comment's trailing separator is driven by the element's
	// own RelPos: NewSection means the user wrote a blank line between
	// the doc comment and the element, which must be preserved.
	if docComment != nil {
		sep := HardLine()
		if e.Pos().RelPos() == token.NewSection {
			sep = BlankLine()
		}
		docComment = Cat(docComment, sep)
	}

	return Row{
		DocComment: docComment,
		Cells:      cells,
		HasComment: hasComment,
	}, postComments
}

// postCommentRows turns a list of post-element comments into Raw
// rows. Each row's Sep honours the comment's own RelPos
// (NewSection → BlankLine, Newline/anything else → HardLine) so
// blank lines the user wrote between the element and the comment, or
// between consecutive comments, survive into the output.
func (c *converter) postCommentRows(cgs []*ast.CommentGroup) []Row {
	if len(cgs) == 0 {
		return nil
	}
	rows := make([]Row, 0, len(cgs))
	for _, cg := range cgs {
		sep := HardLine()
		if cg.Pos().RelPos() == token.NewSection {
			sep = BlankLine()
		}
		rows = append(rows, Row{Sep: sep, Raw: c.commentGroup(cg)})
	}
	return rows
}

// ellipsis converts an Ellipsis node.
func (c *converter) ellipsis(x *ast.Ellipsis) Doc {
	if x.Type != nil {
		return Cat(ellipsisText, c.expr(x.Type))
	}
	return ellipsisText
}

// unaryExpr converts a UnaryExpr. The operand is obtained via
// exprCore so that any comments on it are placed after the
// operator+operand unit, not between them.
func (c *converter) unaryExpr(x *ast.UnaryExpr) Doc {
	op := x.Op.String()
	xDoc := c.exprCore(x.X)

	// Check RelPos between operator and operand.
	var body Doc
	if x.X != nil && x.X.Pos().RelPos() == token.Blank {
		// E.g. ! =~"pattern" - must have the space between ! and =~
		body = Cats(Text(op), spaceText, xDoc)
	} else {
		body = Cat(Text(op), xDoc)
	}

	if selfManagesComments(x.X) {
		return body
	}
	return c.withComments(x.X, body)
}

// postfixExpr converts a PostfixExpr. The operand is obtained via
// exprCore so that the suffix operator is placed before any
// trailing comments on the operand.
func (c *converter) postfixExpr(x *ast.PostfixExpr) Doc {
	xDoc := c.exprCore(x.X)
	body := Cat(xDoc, Text(x.Op.String()))

	if selfManagesComments(x.X) {
		return body
	}
	return c.withComments(x.X, body)
}

// binaryExpr converts a BinaryExpr. A | or & chain that carries any
// same-line trailing // comment is routed to chainTableExpr so the
// trailing comments line up in a single column; everything else,
// including comment-free chains, goes through the per-node
// binaryExprPrec formatter. Callers that need to inject an enclosing
// field's trailing comment into the chain (see fieldRow) call
// chainTableExpr directly with a non-nil fieldTrailing.
func (c *converter) binaryExpr(x *ast.BinaryExpr) Doc {
	if x.Op == token.OR || x.Op == token.AND {
		if chainHasTrailingComment(x) {
			return c.chainTableExpr(x, nil)
		}
	}
	return c.binaryExprPrec(x, binaryCutoff(x, 1), 1)
}

// chainHasTrailingComment reports whether any BinaryExpr in the
// chain (same operator) carries a same-line // trailing comment
// attached after its operator (Position >= 2, Line). Interior
// (Position == 1) comments are excluded: binaryExprPrec's struct-
// injection path handles those more cleanly.
func chainHasTrailingComment(x *ast.BinaryExpr) bool {
	var walk func(e ast.Expr) bool
	walk = func(e ast.Expr) bool {
		bin, ok := e.(*ast.BinaryExpr)
		if !ok || bin.Op != x.Op {
			return false
		}
		for _, cg := range ast.Comments(bin) {
			if cg.Line && cg.Position >= posSuffix {
				return true
			}
		}
		return walk(bin.X) || walk(bin.Y)
	}
	return walk(x)
}

// binaryExprPrec formats a binary expression with precedence-aware
// spacing. Spaces are added around operators at precedences below the
// cutoff. Newline RelPos on Y is always honoured. Blank/NoSpace
// RelPos is ignored (the spacing is determined by precedence).
//
// This matches the algorithm from cue/format (suggested by Russ Cox):
//
//	7             *  /  % quo rem div mod
//	6             +  -
//	5             ==  !=  <  <=  >  >=
//	4             &&
//	3             ||
//	2             &
//	1             |
//
// Spaces are always used at precedence 5 and below. At levels 6-7,
// spaces are used when there's a mix of precedences (to distinguish
// them visually).
func (c *converter) binaryExprPrec(x *ast.BinaryExpr, co, depth int) Doc {
	prec := x.Op.Precedence()

	left := c.binaryOperand(x.X, prec, depth+binaryDiffPrec(x.X, prec))
	right := c.binaryOperand(x.Y, prec+1, depth+1)

	op := x.Op.String()

	var maybeSpace Doc
	if prec < co {
		maybeSpace = spaceText
	}

	cgs := ast.Comments(x)

	// Classify comments. Position semantics on a BinaryExpr:
	//   Position 0           : doc comment before the whole expression
	//   Position 1           : interior of the RHS (typically comments
	//                          written inside an empty "{ ... }" — the
	//                          parser can't attach them to the empty
	//                          StructLit so they hang off the BinaryExpr)
	//   Position >= 2, Line  : same-line // trailing the operator
	//   Position >= 2, !Line : own-line // comment between op and right
	//
	// CUE "//" line comments extend to end-of-line, so any non-doc
	// comment forces the RHS onto the next line. Operator must stay
	// on the left's line (leading operator on a new line is not valid
	// CUE because of auto-semicolon insertion).
	var docBefore []Doc              // Position==0
	var interior []*ast.CommentGroup // Position==1: interior of RHS
	var opInline []Doc               // Position>=2 && Line
	var midBlock []*ast.CommentGroup // Position>=2 && !Line
	for _, cg := range cgs {
		cd := c.commentGroup(cg)
		switch {
		case cg.Position == posDoc:
			docBefore = append(docBefore, cd, HardLine())
		case cg.Position == posPrefix:
			interior = append(interior, cg)
		case cg.Line:
			opInline = append(opInline, spaceText, cd)
		default:
			midBlock = append(midBlock, cg)
		}
	}

	// Interior (Position==1) comments must be rendered inside the RHS.
	// When the RHS is a braced StructLit we can inject them into the
	// struct body so the output parses back to the same attachment.
	// Without a braced struct to host them we fall back to placing
	// them between op and right — which is not ideal but better than
	// dropping them.
	if len(interior) > 0 {
		if s, ok := x.Y.(*ast.StructLit); ok && s.Lbrace.IsValid() {
			injected := c.structLitInjected(s, interior)
			if len(opInline) > 0 || len(midBlock) > 0 || x.Y.Pos().IsNewline() {
				// Still break before Y because of other constraints.
				inner := []Doc{HardLine()}
				for _, cg := range midBlock {
					inner = append(inner, c.commentGroup(cg), HardLine())
				}
				inner = append(inner, injected)
				body := Cats(left, maybeSpace, Text(op), Cats(opInline...),
					Nest(Cats(inner...)))
				return Cats(append(docBefore, body)...)
			}
			body := Cats(left, maybeSpace, Text(op), maybeSpace, injected)
			return Cats(append(docBefore, body)...)
		}
		// No braced struct on the RHS: fall through with interior
		// comments merged into midBlock (they'll land between op and
		// right, forcing a break).
		midBlock = append(midBlock, interior...)
	}

	// breakRHS: put the RHS on a new line, indented.
	breakRHS := x.Y.Pos().IsNewline() || len(opInline) > 0 || len(midBlock) > 0

	var body Doc
	if breakRHS {
		inner := []Doc{HardLine()}
		for _, cg := range midBlock {
			inner = append(inner, c.commentGroup(cg), HardLine())
		}
		inner = append(inner, right)
		body = Cats(left, maybeSpace, Text(op), Cats(opInline...), Nest(Cats(inner...)))
	} else {
		body = Cats(left, maybeSpace, Text(op), maybeSpace, right)
	}
	return Cats(append(docBefore, body)...)
}

// structLitInjected renders a braced StructLit with extra comments
// prepended to its body. Used when comments that the parser attached
// to a surrounding BinaryExpr logically belong inside the struct's
// braces (typical for comments written inside an otherwise-empty
// "{ ... }").
func (c *converter) structLitInjected(x *ast.StructLit, extra []*ast.CommentGroup) Doc {
	body := c.declSlice(x.Elts)
	parts := []Doc{}
	for i, cg := range extra {
		if i > 0 {
			parts = append(parts, HardLine())
		}
		parts = append(parts, c.commentGroup(cg))
	}
	if body != nil {
		parts = append(parts, HardLine(), body)
	}
	closeBreak := relBreak(x.Rbrace.RelPos())
	return c.maybeGroup(x, false, Cats(
		lBrace,
		Nest(Cat(HardLine(), Cats(parts...))),
		closeBreak,
		rBrace,
	))
}

// chainArm holds one operand of a flattened | or & chain together
// with any comments attached to the BinaryExpr whose operator follows
// this arm (i.e. the "| // trailing" belonging to this row's op).
type chainArm struct {
	expr     ast.Expr
	trailing []*ast.CommentGroup // Position>=2, Line=true: goes in this row's comment column
	interior []*ast.CommentGroup // Position==1: interior of next arm (inject if possible)
}

// flattenBinaryChain walks a left-associative (or mixed) chain of
// BinaryExprs with operator x.Op and returns one chainArm per leaf
// operand. Comments on each intermediate BinaryExpr are attached to
// the arm whose operator they follow: trailing //-comments go on the
// left arm's row; interior (Position==1) comments belong to the
// following arm and are later injected into its body if it is a
// braced StructLit.
func flattenBinaryChain(x *ast.BinaryExpr) []chainArm {
	var arms []chainArm
	var pending []*ast.CommentGroup // interior comments pending for next arm
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == x.Op {
			walk(bin.X)
			// Split this intermediate BinaryExpr's comments.
			// Position=1 always means interior-of-next-arm (regardless
			// of Line=true/false), matching the classification in
			// binaryExprPrec; all other comments become the preceding
			// arm's trailing column content.
			var trailing, interior []*ast.CommentGroup
			for _, cg := range ast.Comments(bin) {
				if cg.Position == posPrefix {
					interior = append(interior, cg)
				} else {
					trailing = append(trailing, cg)
				}
			}
			arms[len(arms)-1].trailing = append(arms[len(arms)-1].trailing, trailing...)
			pending = append(pending, interior...)
			walk(bin.Y)
			return
		}
		arms = append(arms, chainArm{expr: e, interior: pending})
		pending = nil
	}
	walk(x)
	return arms
}

// chainTableExpr formats a chain of same-operator BinaryExprs (| or &)
// as a Table: one row per arm, with an optional trailing-comment cell
// that column-aligns across arms. fieldTrailing, when non-nil, is an
// enclosing field's same-line trailing comment that should align with
// the chain's arm comments in the same column. Used only when there
// is at least one trailing comment somewhere in the chain or a
// fieldTrailing is supplied; without comments, binaryExprPrec gives
// a cleaner result.
func (c *converter) chainTableExpr(x *ast.BinaryExpr, fieldTrailing Doc) Doc {
	arms := flattenBinaryChain(x)
	opStr := " " + x.Op.String()

	type armInfo struct {
		elem    Doc
		comment Doc
	}

	infos := make([]armInfo, len(arms))
	for i, a := range arms {
		// Interior comments (from the BinaryExpr after this arm's op
		// but before the next arm) belong *inside* the next arm when
		// it is a braced struct. They're attached here to arm i's
		// expr (not i+1) because the original representation had them
		// on the BinaryExpr; we consume them via structLitInjected if
		// possible.
		elem := c.expr(a.expr)
		if len(a.interior) > 0 {
			if s, ok := a.expr.(*ast.StructLit); ok && s.Lbrace.IsValid() {
				elem = c.structLitInjected(s, a.interior)
			} else {
				// No braced struct to host them; emit before the arm
				// on their own lines.
				prefix := []Doc{}
				for _, cg := range a.interior {
					prefix = append(prefix, c.commentGroup(cg), HardLine())
				}
				elem = Cats(append(prefix, elem)...)
			}
		}

		var commentDoc Doc
		for _, cg := range a.trailing {
			commentDoc = joinLines(commentDoc, c.commentGroup(cg))
		}
		infos[i] = armInfo{elem: elem, comment: commentDoc}
	}

	// Attach the enclosing field's trailing comment to the last arm's
	// comment cell so it column-aligns with the chain's own trailing
	// comments.
	if fieldTrailing != nil {
		last := &infos[len(infos)-1]
		last.comment = joinLines(last.comment, fieldTrailing)
	}

	rows := make([]Row, len(infos))
	for i, info := range infos {
		cell0 := info.elem
		if i < len(infos)-1 {
			cell0 = Cat(cell0, Text(opStr))
		}
		if i == 0 {
			// First row as Raw so its op suffix stays glued to the
			// arm expression; its trailing comment is appended with a
			// space since there's no column to align to yet.
			row := cell0
			if info.comment != nil {
				row = Cats(row, spaceText, info.comment)
			}
			rows[i] = Row{
				Raw:        row,
				HasComment: info.comment != nil,
			}
			continue
		}
		cells := []Doc{cell0}
		if info.comment != nil {
			cells = append(cells, info.comment)
		}
		rows[i] = Row{
			Sep:        HardLine(),
			Cells:      cells,
			HasComment: info.comment != nil,
		}
	}

	return c.maybeGroup(x, false, Nest(Table(rows)))
}

// binaryOperand formats one operand of a binary expression, recursing
// into nested binary expressions at the same or higher precedence.
func (c *converter) binaryOperand(e ast.Expr, prec, depth int) Doc {
	if bin, ok := e.(*ast.BinaryExpr); ok {
		// If the nested binary has lower precedence, the parser would
		// have inserted parens. If same or higher, recurse with
		// precedence-aware formatting.
		if bin.Op.Precedence() >= prec {
			return c.binaryExprPrec(bin, binaryCutoff(bin, depth), depth)
		}
	}
	return c.expr(e)
}

// binaryCutoff determines the precedence cutoff for spacing
// decisions. Only operators at precedences below the cutoff get
// spaces.
//
// In normal mode (depth 1), spaces are always used unless there's a mix
// of + and * (in which case only + gets spaces, making a*b + c*d clear).
// In compact mode (depth > 1, inside a larger expression), spaces are
// minimised.
func binaryCutoff(e *ast.BinaryExpr, depth int) int {
	has6, has7 := binaryWalk(e)
	if has6 && has7 {
		if depth == 1 {
			return 7 // spaces around +/- but not */
		}
		return 6 // no spaces at all in compact mode
	}
	if depth == 1 {
		return 8 // spaces around everything in normal mode
	}
	return 6 // no spaces in compact mode
}

// binaryWalk scans a binary expression tree to determine whether
// precedence levels 6 (+, -) and 7 (*, /) are both present.
func binaryWalk(e *ast.BinaryExpr) (has6, has7 bool) {
	switch e.Op.Precedence() {
	case 6:
		has6 = true
	case 7:
		has7 = true
	}

	if l, ok := e.X.(*ast.BinaryExpr); ok && l.Op.Precedence() >= e.Op.Precedence() {
		h6, h7 := binaryWalk(l)
		has6 = has6 || h6
		has7 = has7 || h7
	}

	if r, ok := e.Y.(*ast.BinaryExpr); ok && r.Op.Precedence() > e.Op.Precedence() {
		h6, h7 := binaryWalk(r)
		has6 = has6 || h6
		has7 = has7 || h7
	}

	return
}

// binaryDiffPrec returns 0 if expr is a BinaryExpr at the same
// precedence as prec (used to avoid increasing depth for
// same-precedence chains), and 1 otherwise.
func binaryDiffPrec(expr ast.Expr, prec int) int {
	if x, ok := expr.(*ast.BinaryExpr); ok && x.Op.Precedence() == prec {
		return 0
	}
	return 1
}

// callExpr converts a CallExpr. Arguments are handled like list
// elements: RelPos is honoured, commas come before trailing comments,
// and trailing commas are allowed before ')'.
func (c *converter) callExpr(x *ast.CallExpr) Doc {
	fun := c.expr(x.Fun)

	if len(x.Args) == 0 {
		return Cats(fun, Text("()"))
	}

	if len(x.Args) == 1 && c.shouldHug(x.Args[0]) {
		return Cats(fun, lParen, c.expr(x.Args[0]), rParen)
	}

	rows := make([]Row, 0, len(x.Args))
	lastArgIdx := len(x.Args) - 1
	for i, a := range x.Args {
		row, postCgs := c.listElemRow(a, i == lastArgIdx, false)
		if i > 0 {
			row.Sep = c.elemBreak(a)
		}
		rows = append(rows, row)
		rows = append(rows, c.postCommentRows(postCgs)...)
	}

	lineHeader := firstChildHasLineDoc(x.Args[0])
	var openBreak Doc
	if lineHeader {
		openBreak = spaceText
	} else {
		openBreak = relBreak(leadingRel(x.Args[0]))
	}
	closeBreak := relBreak(x.Rparen.RelPos())
	if lineHeader && x.Rparen.RelPos() < token.Newline {
		closeBreak = HardLine()
	}

	// Same-line-opener rule: see listLit for the full explanation.
	// A call whose first arg is a brace-opener hugs `(` to it, and
	// symmetrically at the close. No trailing comma is involved
	// because callExpr never emits one.
	cleanArgs := !anyHasDocComment(x.Args)
	hugFirst := cleanArgs &&
		isContiguousOpener(x.Args[0]) &&
		!x.Args[0].Pos().IsNewline()
	hugLast := cleanArgs &&
		isContiguousOpener(x.Args[lastArgIdx]) &&
		x.Rparen.RelPos() < token.Newline
	shareIndent := cleanArgs && !hugFirst && hasSameLineOpener(x.Args)

	nested := Nest(Cat(openBreak, Table(rows)))
	switch {
	case hugFirst:
		nested = Table(rows)
	case shareIndent:
		nested = Cat(openBreak, Table(rows))
	}
	if hugLast {
		closeBreak = nil
	}

	return c.maybeGroup(x, shareIndent, Cats(
		fun,
		lParen,
		nested,
		closeBreak,
		rParen,
	))
}

// indexExpr converts an IndexExpr. Honours RelPos on the index
// expression.  A newline before ']' is not valid CUE (auto-comma
// insertion triggers), so the index and closing bracket stay on the
// same line.
func (c *converter) indexExpr(x *ast.IndexExpr) Doc {
	openBreak := relBreak(x.Index.Pos().RelPos())
	return c.maybeGroup(x, false, Cats(
		c.expr(x.X),
		lBracket,
		Nest(Cat(openBreak, c.expr(x.Index))),
		rBracket,
	))
}

// sliceExpr converts a SliceExpr.
func (c *converter) sliceExpr(x *ast.SliceExpr) Doc {
	low := c.expr(x.Low)
	high := c.expr(x.High)
	return Cats(c.expr(x.X), lBracket, low, colonText, high, rBracket)
}

// parenExpr converts a ParenExpr. '(' hugs the inner expression
// unless the inner expression's RelPos explicitly requests a newline;
// otherwise breaks in the inner expression (e.g. a multi-line struct
// or a broken chain) should not force a break after '('. A newline
// before ')' is not valid CUE (auto-comma insertion triggers), so the
// expression and closing paren stay on the same line.
func (c *converter) parenExpr(x *ast.ParenExpr) Doc {
	if x.X.Pos().IsNewline() {
		return Cats(
			lParen,
			Nest(Cat(HardLine(), c.expr(x.X))),
			rParen,
		)
	}
	return Cats(lParen, c.expr(x.X), rParen)
}

// interpolation converts an Interpolation node. The Elts alternate
// between string fragments (BasicLit) and interpolated
// expressions. The string fragments already include the \( and )
// delimiters, so we emit them verbatim and just format the
// expressions.
func (c *converter) interpolation(x *ast.Interpolation) Doc {
	parts := make([]Doc, len(x.Elts))
	for i, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			parts[i] = Text(lit.Value)
		} else {
			parts[i] = c.expr(e)
		}
	}
	return Cats(parts...)
}

// funcExpr converts a Func node.
func (c *converter) funcExpr(x *ast.Func) Doc {
	args := make([]Doc, len(x.Args))
	for i, a := range x.Args {
		args[i] = c.expr(a)
	}
	argDoc := Sep(commaSpaceText, args...)
	return Cats(Text("func"), lParen, argDoc, Text("): "), c.expr(x.Ret))
}

// comprehension converts a Comprehension.
func (c *converter) comprehension(x *ast.Comprehension) Doc {
	parts := make([]Doc, len(x.Clauses), len(x.Clauses)+3)
	for i, clause := range x.Clauses {
		cl := c.withComments(clause, c.clause(clause))
		if i > 0 {
			// leadingRel gives the RelPos of the first visible token
			// (the doc comment if any, otherwise the clause itself), so
			// a doc-commented clause that begins on its own line keeps
			// the break. A clause that carries a doc comment must also
			// start on its own line regardless of RelPos, or the `//`
			// would absorb the inline separator and merge with whatever
			// follows.
			if leadingRel(clause) >= token.Newline || hasDocComment(clause) {
				cl = Cat(HardLine(), cl)
			} else {
				cl = Cat(spaceText, cl)
			}
		}
		parts[i] = cl
	}

	if x.Value != nil {
		valSep := spaceText
		if x.Value.Pos().IsNewline() {
			valSep = HardLine()
		}
		parts = append(parts, valSep, c.expr(x.Value))
	}

	if x.Fallback != nil {
		parts = append(parts, c.fallbackClause(x))
	}

	return Cats(parts...)
}

// clause converts a single clause.
func (c *converter) clause(cl ast.Clause) Doc {
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
func (c *converter) forClause(x *ast.ForClause) Doc {
	parts := []Doc{Text("for ")}
	if x.Key != nil {
		parts = append(parts, Text(x.Key.Name), commaSpaceText)
	}
	parts = append(parts, Text(x.Value.Name), Text(" in "), c.expr(x.Source))
	return Cats(parts...)
}

// tryClause converts a TryClause.
func (c *converter) tryClause(x *ast.TryClause) Doc {
	if x.Ident != nil {
		return Cats(Text("try "), Text(x.Ident.Name), Text(" = "), c.expr(x.Expr))
	}
	return Text("try")
}

// fallbackClause converts the FallbackClause of a Comprehension.  The
// keyword depends on the comprehension's clauses: "otherwise" after
// for-clauses or multiple clauses, "else" after a single if/try
// clause.
func (c *converter) fallbackClause(comp *ast.Comprehension) Doc {
	kw := "otherwise"
	if len(comp.Clauses) == 1 {
		switch comp.Clauses[0].(type) {
		case *ast.IfClause, *ast.TryClause:
			kw = "else"
		}
	}
	return Cats(Text(" "), Text(kw), Text(" "), c.expr(comp.Fallback.Body))
}

// decl converts a declaration node to a Doc (without comments - those
// are handled by the caller in declSlice or expr).
func (c *converter) decl(d ast.Decl) Doc {
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
// All comments are handled here so the caller does not need to
// wrap the result in withComments.
func (c *converter) field(f *ast.Field) Doc {
	key := c.fieldKey(f)
	leadingBreak := c.exprHasDocComment(f.Value) || valNeedsLeadingBreak(f.Value)
	val := c.fieldValDoc(f, leadingBreak)

	var before []Doc
	var after []Doc
	for _, cg := range ast.Comments(f) {
		switch cg.Position {
		case 0:
			before = append(before, c.commentGroup(cg), HardLine())
		case 1:
			key = Cat(key, c.commentSep(cg, c.commentGroup(cg)))
		case 2:
			cd := c.commentGroup(cg)
			val = Cats(HardLine(), cd, HardLine(), val)
		default:
			cd := c.commentGroup(cg)
			after = append(after, c.commentSep(cg, cd), IfBreak(nil, HardLine()))
		}
	}

	var body Doc
	// When val is preceded by a leading break (doc comment or a
	// braceless struct that starts on a new line), skip the " "
	// between key and val.
	if leadingBreak {
		body = Cat(key, val)
	} else {
		body = Cats(key, spaceText, val)
	}

	return Cats(append(append(before, body), after...)...)
}

// fieldRow splits a Field into a table Row for alignment.
// Doc comments are placed in DocComment (before the key, not affecting
// column widths). Same-line trailing comments go into a separate cell
// for cross-row alignment. Position 1 comments are appended to the
// key. Position 2 comments are deferred and applied to val after it
// is computed. Post-field block comments (Position ≥ 3, not same-line
// trailing, with Newline/NewSection RelPos) are returned separately
// so the caller can emit them as sibling comment blocks after the
// field, preserving their original vertical position. The third
// return value is the braceless-chain length for the row's key
// (1 for a plain field, 2 for `x: y: val`, etc.); the caller uses it
// to flush the table when consecutive rows have differently-shaped
// composite keys.
func (c *converter) fieldRow(f *ast.Field) (Row, []*ast.CommentGroup, int) {
	// For braceless chains (x: y: z: val) collapse the chain into a
	// single composite key so the leaf values align across sibling
	// rows, regardless of how many chain levels each row has.
	chain, _ := unchainField(f)
	leaf := chain[len(chain)-1]

	var key Doc
	if len(chain) == 1 {
		key = c.fieldKey(f)
	} else {
		var parts []Doc
		for i, cf := range chain {
			if i > 0 {
				parts = append(parts, spaceText)
			}
			parts = append(parts, c.fieldKey(cf))
		}
		key = Cats(parts...)
	}

	var docComment Doc
	var trailingComment Doc
	var postComments []*ast.CommentGroup
	hasComment := false

	// Comments that need val (not yet computed) are deferred.
	var deferred []*ast.CommentGroup
	for _, cg := range ast.Comments(f) {
		switch {
		case cg.Position == posDoc:
			docComment = joinLines(docComment, c.commentGroup(cg))
		case cg.Position == posPrefix:
			key = Cat(key, c.commentSep(cg, c.commentGroup(cg)))
			hasComment = true
		case cg.Position == posSuffix:
			hasComment = true
			deferred = append(deferred, cg)
		case cg.Line:
			hasComment = true
			trailingComment = joinLines(trailingComment, c.commentGroup(cg))
		case cg.Position >= posTrailingMin:
			// Non-same-line post-field comment. If it has a
			// Newline/NewSection RelPos it is visually separate from
			// the field's row (on its own line(s), possibly after a
			// blank line); return it to the caller to emit as a
			// sibling block. Otherwise fall back to treating it as a
			// trailing comment.
			hasComment = true
			if cg.Pos().IsNewline() {
				postComments = append(postComments, cg)
			} else {
				trailingComment = joinLines(trailingComment, c.commentGroup(cg))
			}
		}
	}

	// If the value is a | or & chain AND the chain carries any
	// trailing comments, hand the field's own trailing comment to
	// chainTableExpr so it column-aligns with the chain's arm
	// comments. Otherwise keep it as a separate cell in the field row
	// (so it aligns with simple fields' trailing comments).
	//
	// Attributes: for the plain (non-chain) path, attrs get their own
	// table cell (column 2) so they column-align across rows just like
	// trailing comments do. For the chain path the val cell is itself
	// a multi-line table, so attrs are appended inline instead — there
	// is no well-defined column position for them in that case.
	var val Doc
	var attrsDoc Doc
	if bin, ok := leaf.Value.(*ast.BinaryExpr); ok && trailingComment != nil &&
		(bin.Op == token.OR || bin.Op == token.AND) &&
		chainHasTrailingComment(bin) {
		val = c.chainTableExpr(bin, trailingComment)
		for _, attr := range leaf.Attrs {
			val = Cats(val, spaceText, Text(attr.Text))
		}
		trailingComment = nil
	} else {
		val = c.expr(leaf.Value)
		if len(leaf.Attrs) > 0 {
			parts := make([]Doc, 0, 2*len(leaf.Attrs)-1)
			for i, attr := range leaf.Attrs {
				if i > 0 {
					parts = append(parts, spaceText)
				}
				parts = append(parts, Text(attr.Text))
			}
			attrsDoc = Cats(parts...)
		}
	}

	// Deferred comments are all Position=2 (between colon and value);
	// prepend them to val.
	for _, cg := range deferred {
		val = Cats(c.commentSep(cg, c.commentGroup(cg)), val)
	}

	// Column layout:
	//   [key, val]                     — neither attrs nor comment
	//   [key, val, attrs]              — attrs only
	//   [key, val, nil, trailing]      — comment only (reserves the
	//                                    attrs column so comment lands
	//                                    in the same column across
	//                                    attr-bearing rows)
	//   [key, val, attrs, trailing]    — both
	cells := []Doc{key, val}
	if attrsDoc != nil || trailingComment != nil {
		cells = append(cells, attrsDoc)
		if trailingComment != nil {
			cells = append(cells, trailingComment)
		}
	}

	// The doc comment's trailing separator is driven by the field's
	// own RelPos: NewSection means the user wrote a blank line between
	// the doc comment and the field body, which must be preserved.
	if docComment != nil {
		sep := HardLine()
		if f.Pos().RelPos() == token.NewSection {
			sep = BlankLine()
		}
		docComment = Cat(docComment, sep)
	}

	return Row{
		DocComment: docComment,
		Cells:      cells,
		HasComment: hasComment,
	}, postComments, len(chain)
}

// commentSep returns a Doc that places a comment with the appropriate
// separation based on its RelPos. Same-line comments get " // ...",
// while comments with Newline/NewSection get their own line(s).
func (c *converter) commentSep(cg *ast.CommentGroup, cd Doc) Doc {
	switch cg.Pos().RelPos() {
	case token.NewSection:
		return Cats(BlankLine(), cd)
	case token.Newline:
		return Cat(HardLine(), cd)
	}
	// Line=false means the comment was written on its own line in the
	// source rather than trailing the previous token. CUE only has `//`
	// line comments, so squashing it onto a shared line would silently
	// absorb everything up to the next newline. Fall back to a blank
	// line — this matches how the parser re-attaches such comments as
	// doc of the following decl, which triggers the declSep NewSection
	// upgrade, and so preserves idempotency.
	if !cg.Line {
		return Cats(BlankLine(), cd)
	}
	return Cat(spaceText, cd)
}

// fieldKey builds the key portion of a field: label + alias + constraint + colon.
func (c *converter) fieldKey(f *ast.Field) Doc {
	// Fast path for the overwhelmingly common shape `label:` — avoids
	// the parts slice + Cats(...) vararg allocation in the general path.
	hasColon := f.Value != nil || f.TokenPos.IsValid()
	if f.Alias == nil && f.Constraint == token.ILLEGAL && hasColon {
		return Cat(c.label(f.Label), colonText)
	}

	key := c.label(f.Label)
	if f.Alias != nil {
		key = Cat(key, c.postfixAlias(f.Alias))
	}
	switch f.Constraint {
	case token.OPTION:
		key = Cat(key, Text("?"))
	case token.NOT:
		key = Cat(key, Text("!"))
	}
	if hasColon {
		key = Cat(key, colonText)
	}
	return key
}

// fieldValDoc builds the value portion of a field: value + attributes.
// If leadingBreak is true, the value is wrapped in Nest(HardLine + ...)
// so it is rendered on its own line, indented relative to the key.
func (c *converter) fieldValDoc(f *ast.Field, leadingBreak bool) Doc {
	val := c.expr(f.Value)

	for _, attr := range f.Attrs {
		val = Cats(val, spaceText, Text(attr.Text))
	}

	if leadingBreak {
		val = Nest(Cat(HardLine(), val))
	}

	return val
}

// valNeedsLeadingBreak reports whether a field's value is a braceless
// StructLit whose first element carries Newline or NewSection RelPos.
// Such a value was written on its own line by the user (e.g. x:\n
// y: 1) and must be rendered on its own line, indented from the key,
// rather than continued after "key: " on the same line.
func valNeedsLeadingBreak(v ast.Expr) bool {
	sl, ok := v.(*ast.StructLit)
	if !ok || sl.Lbrace.IsValid() || len(sl.Elts) == 0 {
		return false
	}
	return sl.Elts[0].Pos().IsNewline()
}

// exprHasDocComment reports whether an expression has a doc comment
// attached directly to it. Doc comments on descendant expressions do
// not count: if the descendant lives inside a bracketed construct
// (struct/list/paren/call), its doc comment renders inside those
// brackets and does not need the field's value to be placed on its
// own line; if the descendant is part of a binary expression, the
// binary formatter handles its comments internally.
func (c *converter) exprHasDocComment(e ast.Expr) bool {
	if e == nil {
		return false
	}
	for _, cg := range ast.Comments(e) {
		if cg.Position == posDoc {
			return true
		}
	}
	return false
}

// postfixAlias converts a PostfixAlias.
func (c *converter) postfixAlias(a *ast.PostfixAlias) Doc {
	if a.Lparen.IsValid() {
		// Dual form: ~(K,V)
		return Cats(Text("~("), Text(a.Label.Name), commaText, Text(a.Field.Name), rParen)
	}
	// Simple form: ~X
	return Cat(Text("~"), Text(a.Field.Name))
}

// importDecl converts an ImportDecl. Comments attached to each
// ImportSpec are wrapped via withComments so that position-0 doc
// comments and position->0 trailing/after comments are preserved.
func (c *converter) importDecl(x *ast.ImportDecl) Doc {
	if !x.Lparen.IsValid() {
		// Single import without parens.
		if len(x.Specs) == 1 {
			s := x.Specs[0]
			body := Cats(Text("import "), c.importSpec(s))
			return c.withComments(s, body)
		}
	}

	specs := make([]Doc, len(x.Specs))
	for i, s := range x.Specs {
		spec := c.withComments(s, c.importSpec(s))
		if i > 0 {
			var sep Doc
			if s.Pos().RelPos() == token.NewSection {
				sep = BlankLine()
			} else {
				sep = HardLine()
			}
			spec = Cat(sep, spec)
		}
		specs[i] = spec
	}

	body := Cats(specs...)
	return Cats(
		Text("import ("),
		Nest(Cat(HardLine(), body)),
		HardLine(),
		rParen,
	)
}

// importSpec converts an ImportSpec.
func (c *converter) importSpec(s *ast.ImportSpec) Doc {
	if s.Name != nil {
		return Cats(Text(s.Name.Name), spaceText, Text(s.Path.Value))
	}
	return Text(s.Path.Value)
}

// commentSlots holds a node's attached comments partitioned into
// visual slots. It is the single source of truth for comment
// classification: every converter that needs to render comments on
// a node routes through classifyComments so the mapping from AST
// Position/Line fields to rendering roles is defined in exactly
// one place.
//
// The CUE parser numbers comment positions by token index. For a
// bracketed node `<0> open <1> body <2> close <3>`:
//   - Position 0  → doc (before the node)
//   - Position 1  → prefix (inside, just after the opener)
//   - Position 2  → suffix (inside, just before the closer)
//   - Position ≥3 → trailing (after the node)
//
// CommentGroup.Line distinguishes layout flavour, not syntax: it is
// true when the comment ends the line that the previous token starts
// on (e.g. `{ // c` followed by a newline) and false when the comment
// stands on its own line. Both are `//` comments — CUE has no block
// comment syntax. Crucially, Line does NOT change which slot the
// comment belongs in: a `// c` written immediately after `{` is still
// at Position 1 and is interior to the braces; promoting it to
// trailing would move it outside the node entirely.
//
// Non-bracketed nodes (Field, etc.) generally only use Position 0
// (doc) and trailing positions. Any Position 1/2 on such a node is
// still classified into prefix/suffix/trailing consistently, but
// specialised converters (e.g. fieldRow) handle the token-specific
// semantics themselves rather than going through this function.
// Comment position constants. The CUE parser numbers comment
// positions by token index; for a bracketed node these correspond to
// "before the opener", "just inside the opener", "just inside the
// closer", and "after the closer".
const (
	posDoc         int8 = 0 // before the node's first token
	posPrefix      int8 = 1 // inside, just after the opener
	posSuffix      int8 = 2 // inside, just before the closer
	posTrailingMin int8 = 3 // first position counted as trailing
)

type commentSlots struct {
	doc      []*ast.CommentGroup // Position==posDoc
	prefix   []*ast.CommentGroup // Position==posPrefix
	suffix   []*ast.CommentGroup // Position==posSuffix
	trailing []*ast.CommentGroup // Position>=posTrailingMin
}

// classifyComments partitions n's attached comments into slots.
// See commentSlots for the slot semantics.
func classifyComments(n ast.Node) commentSlots {
	var s commentSlots
	for _, cg := range ast.Comments(n) {
		switch cg.Position {
		case posDoc:
			s.doc = append(s.doc, cg)
		case posPrefix:
			s.prefix = append(s.prefix, cg)
		case posSuffix:
			s.suffix = append(s.suffix, cg)
		default:
			s.trailing = append(s.trailing, cg)
		}
	}
	return s
}

// prependInteriorComments builds a body with interior comments placed
// before the existing inner doc, each on its own line.
func (c *converter) prependInteriorComments(interior []*ast.CommentGroup, inner Doc) Doc {
	if len(interior) == 0 {
		return inner
	}
	parts := make([]Doc, 0, len(interior)*2+1)
	for i, cg := range interior {
		if i > 0 {
			parts = append(parts, HardLine())
		}
		parts = append(parts, c.commentGroup(cg))
	}
	if inner != nil {
		parts = append(parts, HardLine(), inner)
	}
	return Cats(parts...)
}

// appendInteriorComments builds a body with interior comments placed
// after the existing inner doc, each on its own line.
func (c *converter) appendInteriorComments(inner Doc, interior []*ast.CommentGroup) Doc {
	if len(interior) == 0 {
		return inner
	}
	parts := make([]Doc, 0, len(interior)*2+1)
	if inner != nil {
		parts = append(parts, inner, HardLine())
	}
	for i, cg := range interior {
		if i > 0 {
			parts = append(parts, HardLine())
		}
		parts = append(parts, c.commentGroup(cg))
	}
	return Cats(parts...)
}

// applyBeforeAfter wraps a body with doc comments before and trailing
// comments after. bodyRel is the body node's RelPos and determines
// the separator between the last doc comment and the body: a
// NewSection produces a blank line (matching the user's source).
func (c *converter) applyBeforeAfter(before []*ast.CommentGroup, bodyRel token.RelPos, body Doc, after []*ast.CommentGroup) Doc {
	if len(before) == 0 && len(after) == 0 {
		return body
	}
	parts := make([]Doc, 0, len(before)*2+1+len(after)*2)
	for i, cg := range before {
		sep := HardLine()
		if i == len(before)-1 && bodyRel == token.NewSection {
			sep = BlankLine()
		}
		parts = append(parts, c.commentGroup(cg), sep)
	}
	parts = append(parts, body)
	for _, cg := range after {
		parts = append(parts, c.commentSep(cg, c.commentGroup(cg)), IfBreak(nil, HardLine()))
	}
	return Cats(parts...)
}

// withComments wraps a Doc with its node's attached comments. The
// separator between the last doc comment and the body honours the
// node's RelPos (NewSection → blank line).
func (c *converter) withComments(n ast.Node, body Doc) Doc {
	slots := classifyComments(n)
	if len(slots.doc) == 0 && len(slots.prefix) == 0 &&
		len(slots.suffix) == 0 && len(slots.trailing) == 0 {
		return body
	}
	// Non-bracketed nodes don't have a natural "inside the brackets"
	// area, so prefix/suffix slots are treated as trailing content.
	// Ordering: prefix first (nearest the opening), then suffix
	// (nearest the closing), then trailing (after the whole node).
	trailing := make([]*ast.CommentGroup, 0,
		len(slots.prefix)+len(slots.suffix)+len(slots.trailing))
	trailing = append(trailing, slots.prefix...)
	trailing = append(trailing, slots.suffix...)
	trailing = append(trailing, slots.trailing...)

	var after []Doc
	prevLineFalse := false
	for _, cg := range trailing {
		// Trailing // comment: place it, then force the enclosing
		// group to break so the comment doesn't swallow closing
		// brackets/braces in flat mode. IfBreak(nil, HardLine) is
		// invisible in broken mode but prevents flat rendering.
		sep := c.commentSep(cg, c.commentGroup(cg))
		// Consecutive Line=false comments with no RelPos would each
		// request a BlankLine separator; collapse the gap between the
		// second and later ones to a HardLine so the sequence re-parses
		// as a single comment group (matching what declSep's NewSection
		// upgrade would produce on the next decl).
		if !cg.Line && prevLineFalse && cg.Pos().RelPos() <= token.Blank {
			sep = Cat(HardLine(), c.commentGroup(cg))
		}
		prevLineFalse = !cg.Line
		after = append(after, sep, IfBreak(nil, HardLine()))
	}

	parts := make([]Doc, 0, len(slots.doc)*2+1+len(after))
	for i, cg := range slots.doc {
		sep := HardLine()
		if i == len(slots.doc)-1 && n.Pos().RelPos() == token.NewSection {
			sep = BlankLine()
		}
		parts = append(parts, c.commentGroup(cg), sep)
	}
	parts = append(parts, body)
	parts = append(parts, after...)
	return Cats(parts...)
}

// commentGroup converts a CommentGroup to a Doc.
func (c *converter) commentGroup(cg *ast.CommentGroup) Doc {
	if len(cg.List) == 0 {
		return nil
	}
	docs := make([]Doc, 0, len(cg.List)*2-1)
	for i, comment := range cg.List {
		if i > 0 {
			docs = append(docs, HardLine())
		}
		docs = append(docs, Text(comment.Text))
	}
	return Cats(docs...)
}

// HardLine returns a hard line break that always emits a newline.
// Any Group containing a HardLine is forced to break.
func HardLine() Doc {
	return hardLine
}

var (
	hardLine       = &docHard{}
	litLine        = &docLitLine{}
	noSepLine      = Line("")
	softLineSpace  = Line(" ")
	softLineComma  = Line(", ")
	blankLine      = Cat(litLine, hardLine)
	trailingComma  = IfBreak(Text(","), nil)
	lBracket       = Text("[")
	rBracket       = Text("]")
	lBrace         = Text("{")
	rBrace         = Text("}")
	lParen         = Text("(")
	rParen         = Text(")")
	spaceText      = Text(" ")
	commaText      = Text(",")
	commaSpaceText = Text(", ")
	periodText     = Text(".")
	colonText      = Text(":")
	equalsText     = Text("=")
	bottomText     = Text("_|_")
	ellipsisText   = Text("...")
)

// SoftLineSpace is a Line that emits a space when flat.
func SoftLineSpace() Doc { return softLineSpace }

// SoftLineComma is a Line that emits ", " when flat.
func SoftLineComma() Doc { return softLineComma }

// BlankLine emits a bare newline followed by an indented newline,
// producing a truly blank line (no trailing whitespace) as a separator.
func BlankLine() Doc { return blankLine }

// TrailingComma emits a comma only in broken mode.
func TrailingComma() Doc { return trailingComma }
