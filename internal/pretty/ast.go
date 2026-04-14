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
	"fmt"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// File overview
//
// This file translates CUE AST nodes into the Doc algebra defined in
// doc.go. The translation entry point is [converter.node]; it
// recurses through the AST and produces a Doc that render.go renders
// to bytes.
//
// # Two layout strategies
//
// CUE ASTs may or may not carry layout hints. The parser sets
// [token.Pos] RelPos values (NoSpace / Blank / Newline / NewSection /
// Elided) to encode the user's authored layout. Programmatic ASTs
// (built by code, or parser output with RelPos stripped) may have no
// RelPos values. This pretty package selects between two modes
// per-subtree:
//
//   - authored mode: the formatter respects the authored layout. The
//     converted Doc is wrapped in [infiniteWidth]; soft Lines and
//     IfBreak resolve statically, and width-driven Group breaks are
//     suppressed. Only HardLine / LitLine / BlankLine produce line
//     breaks.
//
//   - programmatic mode: width-driven Wadler-Lindig. Groups pick flat
//     or broken to fit the width budget. Soft Lines break when the
//     line overflows. Wrapped in [finiteWidth] so that, when this
//     subtree is itself nested inside an [infiniteWidth] wrap, the
//     inner Group's flat-vs-broken decision still uses the real
//     width budget instead of inheriting the outer unlimited budget.
//
// The mode is decided per-subtree at "wrap sites" - node types
// where the converter calls [converter.maybeGroup]. See
// [isWrapEligible] for the list. [converter.precompute] flags the
// smallest wrap-eligible covering subtree of each connected RelPos
// cluster (via isAuthored), so a stray RelPos hint in an
// otherwise-programmatic AST does not flip the whole file into
// authored mode. A programmatic pocket nested inside an
// [infiniteWidth] wrap (a wrap-eligible subtree with no RelPos
// descendants) gets width-driven layout via the [finiteWidth]
// boundary; the surrounding authored-mode treatment continues to
// apply to its authored siblings.
//
// # Tables
//
// pretty extends Wadler-Lindig with a Table Doc (doc.go) for column-
// aligned layouts: struct fields aligned by ":", chain arms aligned
// by trailing comments, list elements by their value column, and so
// on. The renderer partitions tables into segments - maximal
// contiguous prefixes that share column alignment without forcing
// newlines - so that a multi-line or overflowing row does not
// disrupt the alignment of simpler rows around it. See render.go
// for the segmentation algorithm.
//
// # Terminology
//
//   - RelPos: a token.Pos's relative-position hint. Drives
//     authored-mode layout decisions.
//   - authored mode / programmatic mode: see above. "Programmatic"
//     covers both code-built ASTs and parser output with RelPos
//     stripped.
//   - flat-mode / broken-mode: a Wadler-Lindig Group's two
//     rendering modes. Flat = single line; broken = with line
//     breaks.
//   - hard line / soft line / blank line: HardLine forces a newline
//     and breaks any enclosing Group; soft Line (docLine) emits its
//     alternate in flat-mode and a newline+indent in broken-mode;
//     BlankLine emits two newlines (a visually blank line).
//   - wrap site: an AST node type where [converter.maybeGroup] is
//     called. Each wrap site is a candidate for the [infiniteWidth]
//     wrap; isAuthored selects which actually fire.
//   - simple field / complex field: an [ast.Field] is simple iff
//     [converter.isSimpleField] returns true (eligible for table
//     alignment as "key: value"). Anything else is complex -
//     multi-line value, doc comment on the value, value on its own
//     line, or a non-collapsible chain.
//   - chain: a brace-less sequence of Fields where each value is a
//     single-element StructLit (e.g. a: b: c: 1); or a sequence of
//     BinaryExprs with the same operator (e.g. a | b | c).
//
// # Scope
//
// In scope: rendering ASTs that the CUE parser can produce. The
// formatter's invariant is "any AST that goes parser -> AST -> format
// round-trips cleanly". Programmatic ASTs that match this shape (no
// RelPos hints, but otherwise parser-shaped) are also fully
// supported - the per-subtree wrap routes them through programmatic
// mode while preserving any authored layout in nested authored
// pockets.
//
// Out of scope:
//
//   - Modifying the AST (pretty is read-only).
//   - Honouring real terminal tab stops; a fixed visual indent
//     width is assumed (see [Config.IndentWidth]).
//   - Byte-for-byte parity with cue/format. We follow cue/format's
//     conventions where they make sense - e.g. the blank-line
//     discipline around definitions and non-field decls - but do
//     not aim for exact equivalence.
//   - Faithfully rendering AST shapes the parser cannot produce.
//     For example, a *ast.Field with a Position=1 comment (between
//     label and colon) has no source layout: `//` runs to end of
//     line and the parser doesn't allow a newline between label
//     and colon, so this construction can only be built
//     programmatically. We render such ASTs with a best-effort
//     placement that may not round-trip; preserving such
//     positional intent is not a goal.

// nodeFlag bits record precomputed properties of AST nodes used by
// the layout decision points (see [converter.precompute]).
type nodeFlag uint8

const (
	// relPosInChildren marks a node which has at least one strict
	// descendant (i.e. excluding the node itself) that carries any
	// RelPos at all (NoSpace, Blank, Newline, NewSection, Elided).
	relPosInChildren nodeFlag = 1 << iota
	// newlineInChildren refines relPosInChildren: it is used to mark a
	// node which has at least one strict descendant (i.e. excluding
	// the node itself) that carries a Newline or NewSection RelPos.
	newlineInChildren
	// isAuthored marks a node which is itself eligable for wrapping,
	// and carries any RelPos, or has a descendent that carries any
	// RelPos. Further, there is no such node on the path between this
	// node and the RelPos node that is eligable for
	// wrapping. I.e. isAuthored is set on the eligable node nearest a
	// RelPos-containing subtree, and not any higher (in contrast to
	// relPosInChildren which propogates up the tree to the very root,
	// once set).
	isAuthored
	// endsWithOwnLineComment marks a node whose rendered output ends
	// with a `//` comment on its own line (Slash RelPos != Blank and
	// cg.Line == false). Self-set when the node has such a trailing
	// comment attached directly; propagated up only through the
	// rightmost-rendered child of non-bracketed nodes (Field, BinaryExpr,
	// UnaryExpr, SelectorExpr, LetClause, EmbedDecl, PostfixExpr).
	// Bracketed nodes (StructLit, ListLit, CallExpr, IndexExpr,
	// SliceExpr, ParenExpr, ImportDecl) end with their closing
	// bracket, so descendants don't propagate through them.
	endsWithOwnLineComment
)

// converter translates a CUE AST into the Doc algebra.
type converter struct {
	nodeFlags map[ast.Node]nodeFlag
}

// REVIEW_DONE_MS
//
// node converts an AST node ([ast.File], [ast.Expr] or [ast.Decl]
// only) to a Doc.
func (c *converter) node(n ast.Node) Doc {
	c.analyse(n)
	switch n := n.(type) {
	case *ast.File:
		return Cat(c.maybeGroup(n, c.file(n)), HardLine())
	case ast.Expr:
		return c.expr(n)
	case ast.Decl:
		return c.decl(n)
	default:
		return nil
	}
}

// REVIEW_DONE_MS
//
// isWrapEligible reports whether n is one of the AST node types at
// which the converter calls [converter.maybeGroup]. Mirrors the
// call sites:
//
//   - *ast.File via [converter.node]
//   - *ast.StructLit via [converter.applyBracketed] and the
//     structLitInjected path
//   - *ast.ListLit and *ast.CallExpr via [converter.applyBracketed]
//   - *ast.BinaryExpr via the binary chain converters
//   - *ast.IndexExpr via [converter.indexExpr]
//
// The isAuthored algorithm only sets the flag on these types
// because non-eligible nodes can't actually wrap - flagging them
// would be inert. If a new converter adds a maybeGroup call on a
// different type, add that type here. maybeGroup itself panics if
// called on a non-eligible type, catching drift between this list
// and the actual call sites.
func isWrapEligible(n ast.Node) bool {
	switch n.(type) {
	case *ast.File, *ast.StructLit, *ast.ListLit, *ast.CallExpr,
		*ast.BinaryExpr, *ast.IndexExpr:
		return true
	}
	return false
}

// REVIEW_DONE_MS
//
// analyse walks root to populate c.nodeFlags. For each visited
// node it ORs the relevant flag bits onto every ancestor up the
// current stack, short-circuiting at the first ancestor that already
// carries every bit being propagated - total work stays amortized
// O(N). The strict-descendants flags (newlineInChildren,
// relPosInChildren) are propagated from the parent up.
//
// isAuthored is computed by the post-order callback. each node OR-s
// its own RelPos and its children's uncovered bits up; if a
// wrap-eligible node sees any uncovered RelPos, it sets isAuthored on
// itself and suppresses propagation to its parent (its subtree is now
// covered by its own wrap). This finds the smallest wrap-eligible
// covering subtree for each connected RelPos cluster.
func (c *converter) analyse(root ast.Node) {
	if root == nil {
		return
	}
	nodeFlags := c.nodeFlags
	if nodeFlags == nil {
		nodeFlags = make(map[ast.Node]nodeFlag)
		c.nodeFlags = nodeFlags
	} else {
		clear(nodeFlags)
	}

	type frame struct {
		flags                        nodeFlag
		hasRelPos                    bool
		lastChildTrailingCommentLine bool
	}
	var stack []frame

	ast.Walk(root,
		func(n ast.Node) bool {
			relPos := n.Pos().RelPos()
			hasRelPos := relPos != 0

			if hasRelPos {
				ancestorsFlags := relPosInChildren
				if relPos >= token.Newline {
					ancestorsFlags |= newlineInChildren
				}

				for i := len(stack) - 1; i >= 0; i-- {
					f := &stack[i]
					if missing := ancestorsFlags &^ f.flags; missing == 0 {
						break
					}
					f.flags |= ancestorsFlags
				}
			}

			stack = append(stack, frame{hasRelPos: hasRelPos})
			return true
		},
		func(n ast.Node) {
			i := len(stack) - 1
			f := &stack[i]
			stack = stack[:i]
			var parent *frame
			if i > 0 {
				parent = &stack[i-1]
			}

			flags := f.flags
			if f.hasRelPos {
				if isWrapEligible(n) {
					flags |= isAuthored
				} else if parent != nil {
					parent.hasRelPos = true
				}
			}

			trailingCommentLine := (f.lastChildTrailingCommentLine && !isBracketed(n)) || hasTrailingCommentLine(n)
			if trailingCommentLine {
				flags |= endsWithOwnLineComment
			}
			if parent != nil {
				parent.lastChildTrailingCommentLine = trailingCommentLine
			}

			if flags != 0 {
				// only write if flags differs from the zero-value
				nodeFlags[n] = flags
			}
		})
}

// REVIEW_DONE_MS
//
// hasTrailingCommentLine reports whether n has a trailing-position
// comment that would render on its own line (Slash RelPos != Blank
// and cg.Line == false). Same-line trailing comments don't qualify:
// the line ends naturally after them, and the parser keeps them
// attached to n on reparse.
func hasTrailingCommentLine(n ast.Node) bool {
	for _, cg := range ast.Comments(n) {
		if cg.Position >= posTrailingMin && !cg.Line && cg.Pos().RelPos() >= token.Newline {
			return true
		}
	}
	return false
}

// REVIEW_DONE_MS
//
// isBracketed reports whether n's rendered output ends with a
// closing bracket - so descendant trailing comments are inside the
// brackets and don't reach n's rendered end. Only n's own attached
// trailing comments (rendered after the closer) can carry the tail
// flag for these nodes. StructLit and ImportDecl can appear without
// their delimiters (a braceless struct, e.g. a file body; a parenless
// single-spec import); in that case the rendered end is the last
// child and descendants do propagate.
func isBracketed(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.StructLit:
		return x.Lbrace.IsValid()
	case *ast.ImportDecl:
		return x.Lparen.IsValid()
	case *ast.ListLit, *ast.CallExpr, *ast.IndexExpr, *ast.SliceExpr, *ast.ParenExpr:
		return true
	default:
		return false
	}
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
	// Partition f.Decls into a header (longest prefix of Package /
	// ImportDecl / CommentGroup decls) and a body (everything after).
	// The CUE parser guarantees Package occupies decl 0 (if present)
	// and every ImportDecl precedes every non-import non-comment
	// decl, so this prefix maps cleanly onto the visual file header.
	// Header decls are not field-value pairs and never align in a
	// table, so isolating them keeps declSlice's invariant simple:
	// it only ever sees the body's mix of fields and field-adjacent
	// decls.
	headerEnd := headerPrefix(f.Decls)
	header := c.fileHeader(f.Decls[:headerEnd])
	body := c.declSlice(f.Decls[headerEnd:])

	// Separator between the header and the body. declSep keys off
	// the first body decl's leading RelPos, so a blank line in the
	// source stays a BlankLine, a Newline stays a HardLine, and
	// programmatic source (NoSpace) gets a lineBreakComma.
	var headerBodySep Doc
	if header != nil && body != nil {
		var lastHeader ast.Decl
		for i := headerEnd - 1; i >= 0; i-- {
			if f.Decls[i].Pos().RelPos() != token.Elided {
				lastHeader = f.Decls[i]
				break
			}
		}
		headerBodySep = c.declSep(f.Decls[headerEnd], lastHeader)
	}

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
	// is honoured - otherwise a blank line between a file-level
	// comment and the next decl's doc comment is silently dropped.
	firstDeclSep := HardLine()
	if len(f.Decls) > 0 {
		firstDeclSep = relBreakOr(leadingRelPos(f.Decls[0]), HardLine())
	}

	parts := make([]Doc, 0, len(leading)*2+3+len(trailing)*2)
	for i, cg := range leading {
		parts = append(parts, c.commentGroup(cg))
		var sep Doc
		if i == len(leading)-1 {
			sep = firstDeclSep
		} else {
			sep = relBreakOr(leading[i+1].Pos().RelPos(), HardLine())
		}
		parts = append(parts, sep)
	}
	if header != nil {
		parts = append(parts, header)
	}
	if body != nil {
		if header != nil {
			parts = append(parts, headerBodySep)
		}
		parts = append(parts, body)
	}
	for _, cg := range trailing {
		parts = append(parts, c.commentSep(cg, c.commentGroup(cg)), SwitchMode(nil, HardLine()))
	}
	return Cats(parts...)
}

// REVIEW_DONE_MS
//
// headerPrefix returns the length of the longest prefix of decls
// composed of Package / ImportDecl / CommentGroup / Attribute. The
// CUE parser rejects field decls before any ImportDecl, so this
// prefix is the run of "header" decls that visually precede the file
// body.  Elided decls are skipped (they're invisible) so they don't
// terminate the prefix prematurely.
func headerPrefix(decls []ast.Decl) int {
	for i, d := range decls {
		if d.Pos().RelPos() == token.Elided {
			continue
		}
		switch d.(type) {
		case *ast.Package, *ast.ImportDecl, *ast.CommentGroup, *ast.Attribute:
			// header-eligible; keep scanning
		default:
			return i
		}
	}
	return len(decls)
}

// fileHeader renders the file's header decls (Package, ImportDecl,
// CommentGroup, Attribute). Each decl is emitted as a standalone Doc
// with relpos-driven separators between them; alignment never applies
// here because header decls are not field-value pairs.
func (c *converter) fileHeader(decls []ast.Decl) Doc {
	if len(decls) == 0 {
		return nil
	}
	docs := make([]Doc, 0, 2*len(decls))
	var prev ast.Decl
	for _, decl := range decls {
		if decl.Pos().RelPos() == token.Elided {
			continue
		}
		var sep Doc
		if prev != nil {
			sep = c.declSep(decl, prev)
		}
		prev = decl
		d := c.decl(decl)
		if _, ok := decl.(*ast.CommentGroup); !ok {
			d = c.withComments(decl, d)
		}
		docs = append(docs, sep, d)
	}
	return Cats(docs...)
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

	// Mixed-layout uniformity: if any inter-decl separator is a hard
	// break (Newline / NewSection RelPos), every decl is laid out on
	// its own line. Comma-separated runs are promoted to HardLine so
	// the struct doesn't render with some fields on their own line and
	// others packed onto a shared line - that mix produces awkward
	// table alignment (column-padded keys followed by `, key:` on the
	// same row).
	multiLine := multilineDecls(decls)

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
			if multiLine && sep == lineBreakComma {
				sep = HardLine()
			}
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

		f, _ := decl.(*ast.Field)
		var chain []*ast.Field
		if f != nil {
			chain = c.simpleFieldChain(f)
		}
		if chain != nil {
			row, postComments, chainLen := c.fieldRow(chain)
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
					docs = append(docs,
						relBreakOr(cg.Pos().RelPos(), HardLine()),
						c.commentGroup(cg))
				}
			}
			continue
		}

		// Anything that isn't a simple field-value row breaks
		// alignment: complex fields (multi-line value, doc comment on
		// value, value on its own line, non-collapsible chain) and
		// every non-field, non-CommentGroup decl (LetClause, EmbedDecl,
		// Comprehension, Alias, Attribute, Ellipsis, BadDecl). Flush
		// the table and emit the decl standalone. Centralising this
		// rule here means table-flush concerns live in one place
		// rather than being split between construction-time decisions
		// and render-time segmentation.
		//
		// Package and ImportDecl never reach this loop: file()
		// partitions them off into the header before declSlice runs,
		// and a struct literal cannot contain either of them
		// syntactically.
		flushTable()
		var doc Doc
		if f != nil {
			// field() handles all of the field's comments internally;
			// don't double-wrap with withComments.
			doc = c.decl(decl)
		} else {
			doc = c.withComments(decl, c.decl(decl))
		}
		docs = append(docs, sep, doc)
	}
	flushTable()

	return Cats(docs...)
}

// REVIEW_DONE_MS
//
// multilineDecls reports whether any inter-decl separator is a hard
// break (Newline / NewSection RelPos).
func multilineDecls(decls []ast.Decl) bool {
	first := true
	for _, decl := range decls {
		if decl.Pos().RelPos() == token.Elided {
			continue
		}
		if !first && leadingRelPos(decl) >= token.Newline {
			return true
		}
		first = false
	}
	return false
}

// simpleFieldChain returns the field chain (x: y: z: val -> [x,y,z])
// when f is eligible for table alignment, or nil otherwise. A chain
// is eligible when it is a braceless single-element StructLit chain
// whose leaf value:
//   - exists and has no doc comment (doc-commented values render on
//     their own line via field()),
//   - has no Newline/NewSection RelPos (a user-written break is
//     preserved by field() under Nest+HardLine), and
//   - has no Position=2 comment (which forces val to its own line).
//
// A StructLit or ListLit value still qualifies: whether it renders
// without newlines is decided at render time by the table's row
// partitioning.
func (c *converter) simpleFieldChain(f *ast.Field) []*ast.Field {
	chain, collapsible := unchainField(f)
	if len(chain) > 1 && !collapsible {
		return nil
	}
	leaf := chain[len(chain)-1]
	if leaf.Value == nil {
		return nil
	}
	if c.exprHasDocComment(leaf.Value) {
		return nil
	}
	if leaf.Value.Pos().IsNewline() {
		return nil
	}
	for _, cg := range ast.Comments(leaf) {
		if cg.Position == posSuffix {
			return nil
		}
	}
	return chain
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

// REVIEW_DONE_MS
//
// declSep computes the separator before a declaration based on its leading
// RelPos. Newline produces a HardLine, NewSection a BlankLine; lower
// RelPos values fall back to lineBreakComma since declarations need
// at least a comma or newline between them.
//
// Two upgrades fire on top of the authored RelPos:
//
//   - A blank line is inserted before a doc-commented declaration
//     when prev is a Definition (#Foo) or any non-field, non-comment
//     decl.
//   - If prev's rendered output ends with an own-line `//` comment,
//     the separator is promoted (to NewSection where the rule above
//     also fires, otherwise to Newline). Inside an [infiniteWidth]
//     wrap a lineBreakComma would otherwise be rewritten to
//     Text(", ") and the `//` would absorb the next
//     decl. Promoting also keeps the parse/format cycle idempotent
//     when the comment migrates from prev's trailing slot into the
//     next decl's doc on reparse. Same-line trailing comments
//     (`cg.Line` or `Slash.RelPos == Blank`) need neither: the line
//     ends naturally after them, and the parser keeps them attached
//     to prev.
func (c *converter) declSep(d ast.Decl, prev ast.Decl) Doc {
	rel := leadingRelPos(d)

	if prev == nil {
		return relBreakOr(rel, lineBreakComma)
	}

	const mask = relPosInChildren | endsWithOwnLineComment
	prevTrailing := c.nodeFlags[prev]&mask == mask

	if rel < token.NewSection && (prevTrailing || hasDocComment(d)) {
		switch prev := prev.(type) {
		case *ast.Field:
			if internal.IsDefinition(prev.Label) {
				rel = token.NewSection
			}
		case *ast.CommentGroup: // noop
		default:
			rel = token.NewSection
		}
	}

	if rel < token.Newline && prevTrailing {
		rel = token.Newline
	}

	return relBreakOr(rel, lineBreakComma)
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

// appendAttrs concatenates a field's attributes after val, separated
// by spaces. Returns val unchanged when attrs is empty.
func appendAttrs(val Doc, attrs []*ast.Attribute) Doc {
	for _, attr := range attrs {
		val = Cats(val, spaceLit, StringLit(attr.Text))
	}
	return val
}

// attrsSpaced returns a Doc rendering attrs joined by spaces, or nil
// when attrs is empty.
func attrsSpaced(attrs []*ast.Attribute) Doc {
	if len(attrs) == 0 {
		return nil
	}
	parts := make([]Doc, 0, 2*len(attrs)-1)
	for i, attr := range attrs {
		if i > 0 {
			parts = append(parts, spaceLit)
		}
		parts = append(parts, StringLit(attr.Text))
	}
	return Cats(parts...)
}

// authored reports whether n's subtree carries any RelPos - that is,
// whether the subtree came from the parser (or had RelPos info added
// programmatically) rather than being built RelPos-free. Used by
// layout decisions that branch on "does this subtree carry authored
// layout": see e.g. the bracketed-layout policy and the chain-Doc
// construction. The [infiniteWidth] wrap is driven by isAuthored,
// not this function - see [maybeGroup].
func (c *converter) authored(n ast.Node) bool {
	return c.nodeFlags[n]&relPosInChildren != 0
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
//     Comments deep in descendants are safe - they live inside their
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
	return n.Pos().IsNewline() || c.nodeFlags[n]&newlineInChildren != 0
}

// selfManagesInteriorComments reports whether an expression's
// conversion bakes *only* its interior (posPrefix / posSuffix)
// comments into the returned Doc, leaving doc-before and trailing-
// after comments for the caller to wrap. withComments skips prefix
// and suffix slots for these nodes so they aren't double-rendered.
//
//   - StructLit / ListLit: `{ // c }` / `[ // c ]` interior comments
//     belong between the brackets, not after them.
//   - BinaryExpr: posPrefix is "interior of RHS" (typically a comment
//     written inside an empty struct on the right that the parser
//     hung off the BinaryExpr); posSuffix non-Line is the "between op
//     and right" mid-block comment that binaryExprPrec injects. Both
//     are placed inside the chain's Doc by the binary handlers.
func selfManagesInteriorComments(n ast.Node) bool {
	switch n.(type) {
	case *ast.StructLit, *ast.ListLit, *ast.BinaryExpr:
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

// docCommentBlock renders a sequence of doc comment groups in order,
// using HardLine between consecutive groups by default but BlankLine
// when the next group's RelPos is NewSection (preserving blank lines
// the user wrote between comment blocks). The final separator is
// chosen from trailingRel - pass the host node's RelPos to keep a
// blank line between the last comment and the body when the source
// had one.
//
// Returns nil for an empty cgs slice.
func (c *converter) docCommentBlock(cgs []*ast.CommentGroup, trailingRel token.RelPos) Doc {
	if len(cgs) == 0 {
		return nil
	}
	parts := make([]Doc, 0, 2*len(cgs))
	for i, cg := range cgs {
		parts = append(parts, c.commentGroup(cg))
		nextRel := trailingRel
		if i+1 < len(cgs) {
			nextRel = cgs[i+1].Pos().RelPos()
		}
		parts = append(parts, relBreakOr(nextRel, HardLine()))
	}
	return Cats(parts...)
}

// maybeGroup wraps body in the layout primitive selected for n's
// subtree: [infiniteWidth] when n's RelPos hints must be preserved,
// otherwise [finiteWidth] for width-driven layout.
//
// When isAuthored is set on n, body is wrapped in [infiniteWidth] -
// the formatter cannot inject width-driven newlines into the wrapped
// subtree (only HardLine / LitLine / BlankLine survive), and nested
// chains, calls, and lists stay flat unless a hard break forces them
// open. See [infiniteWidth] for the underlying mechanism.
//
// Otherwise body is wrapped in [finiteWidth], which renders it in
// width-driven Wadler-Lindig mode. See [finiteWidth] for the
// underlying mechanism, including its hole-punching behaviour when
// this subtree is nested inside an enclosing [infiniteWidth] wrap.
//
// isAuthored is set per-subtree by [converter.precompute]: see the
// "uncovered RelPos" pass there and [isWrapEligible] for the set of
// node types where the flag (and therefore this wrap) can fire.
func (c *converter) maybeGroup(n ast.Node, body Doc) Doc {
	// Integrity check: every maybeGroup call site must operate on a
	// type listed in isWrapEligible. The isAuthored precompute pass
	// only flags eligible types, so a call on a non-eligible type
	// would silently never enter the infiniteWidth branch even when
	// the subtree carries RelPos. Panic loudly so the drift is caught
	// immediately.
	if !isWrapEligible(n) {
		panic(fmt.Sprintf("pretty: maybeGroup called on non-wrap-eligible type %T", n))
	}
	if c.nodeFlags[n]&isAuthored != 0 {
		return infiniteWidth(body)
	}
	return finiteWidth(body)
}

// REVIEW_DONE_MS
//
// relBreakOr returns the line-break Doc corresponding to a RelPos
// with a soft fallback for the non-newline cases. Newline always
// produces HardLine and NewSection always produces BlankLine.
func relBreakOr(rel token.RelPos, soft Doc) Doc {
	switch rel {
	case token.Newline:
		return HardLine()
	case token.NewSection:
		return BlankLine()
	}
	return soft
}

// REVIEW_DONE_MS
//
// relBreak returns the line-break Doc corresponding to a RelPos:
// BlankLine for NewSection, HardLine for Newline, and
// [lineBreakEmpty] otherwise.
func relBreak(rel token.RelPos) Doc {
	return relBreakOr(rel, lineBreakEmpty)
}

// REVIEW_DONE_MS
//
// leadingRelPos returns the RelPos that drives the separator placed
// before n's first visible token. If n has a Position=0 doc comment,
// the first visible token is that comment, so its RelPos wins;
// otherwise n's own RelPos applies. Distinguishing "blank line before
// the comment" from "blank line between the comment and the body"
// lets callers render each side correctly instead of collapsing both
// to a single decision.
func leadingRelPos(n ast.Node) token.RelPos {
	for _, cg := range ast.Comments(n) {
		if cg.Position == posDoc {
			return cg.Pos().RelPos()
		}
	}
	return n.Pos().RelPos()
}

// REVIEW_DONE_MS
//
// expr converts an expression node to a Doc, including any comments.
func (c *converter) expr(x ast.Expr) Doc {
	if x == nil {
		return nil
	}
	return c.withComments(x, c.exprCore(x))
}

// REVIEW_DONE_MS
//
// exprCore converts an expression without handling comments on it.
// Comments are handled by the caller (expr or listElem).
func (c *converter) exprCore(x ast.Expr) Doc {
	if x == nil {
		return nil
	}
	switch x := x.(type) {
	case *ast.Ident:
		return StringLit(x.Name)

	case *ast.BasicLit:
		return c.basicLit(x)

	case *ast.BottomLit:
		return bottomLit

	case *ast.BadExpr:
		return StringLit("/* BadExpr */")

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
		return Cats(c.expr(x.X), periodLit, c.label(x.Sel))

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
		return Cats(StringLit(x.Ident.Name), equalsLit, c.expr(x.Expr))

	default:
		return StringLit("/* unknown expr */")
	}
}

// REVIEW_DONE_MS
//
// label converts a Label node to a Doc.
func (c *converter) label(l ast.Label) Doc {
	switch x := l.(type) {
	case *ast.Ident:
		return StringLit(x.Name)
	case *ast.BasicLit:
		return c.basicLit(x)
	case *ast.Interpolation:
		return c.interpolation(x)
	case *ast.ListLit:
		if len(x.Elts) == 1 {
			return Cats(lBracketLit, c.expr(x.Elts[0]), rBracketLit)
		}
		return c.listLit(x)
	case *ast.ParenExpr:
		return Cats(lParenLit, c.expr(x.X), rParenLit)
	case *ast.Alias:
		return Cats(StringLit(x.Ident.Name), equalsLit, c.expr(x.Expr))
	default:
		return c.expr(l.(ast.Expr))
	}
}

// REVIEW_DONE_MS
//
// basicLit converts a BasicLit to a Doc.
func (c *converter) basicLit(x *ast.BasicLit) Doc {
	lines := strings.Split(x.Value, "\n")
	parts := make([]Doc, 0, len(lines)*2-1)
	// We have to intersperse lineBreakBare directly here. Using
	// Sep(lineBreakBare, parts) wouldn't work because some parts could
	// be nil (StringLit("") gives nil) and Sep skips over nil parts.
	for i, line := range lines {
		if i > 0 {
			parts = append(parts, lineBreakBare)
		}
		parts = append(parts, StringLit(line))
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

	switch {
	case len(x.Elts) == 0 && !hasInterior:
		return StringLit("{}")
	case len(x.Elts) == 1 && !hasInterior && c.shouldHug(x.Elts[0]):
		return Cats(lBraceLit, c.decl(x.Elts[0]), rBraceLit)
	}

	var firstElt, lastElt ast.Node
	if len(x.Elts) > 0 {
		firstElt = x.Elts[0]
		lastElt = x.Elts[len(x.Elts)-1]
	}
	inner := c.wrapInteriorComments(c.declSlice(x.Elts), slots.prefix, slots.suffix)

	// Only the body is returned - interior comments (slots.prefix /
	// slots.suffix) are already in inner. Doc and trailing comments
	// are handled by the caller (expr() -> withComments, or
	// listElemRow / declSlice which categorise them so post-element
	// comments land in the right position relative to separators).
	layout := bracketedLayout{
		node:          x,
		open:          lBraceLit,
		close:         rBraceLit,
		openerRel:     x.Lbrace.RelPos(),
		closerRel:     x.Rbrace.RelPos(),
		firstElem:     firstElt,
		lastElem:      lastElt,
		numElems:      len(x.Elts),
		hasInterior:   hasInterior,
		anyDoc:        anyHasDocComment(x.Elts),
		anyPost:       anyHasPostComment(x.Elts),
		sameLineOpen:  hasSameLineOpener(x.Elts),
		noElemNewline: noElemHasNewline(x.Elts),
		lineHeader:    hasLineLeadingComment(slots, firstElt),
		inner:         inner,
	}
	return c.applyBracketed(layout, c.computeBracketedPolicy(layout))
}

// listLit converts a ListLit. As for structLit, interior comments
// attached directly to the ListLit are rendered inside the brackets
// and expr() skips withComments for ListLit.
func (c *converter) listLit(x *ast.ListLit) Doc {
	if !x.Lbrack.IsValid() {
		// Shouldn't normally happen for lists, but respect the AST.
		elems := make([]Doc, len(x.Elts))
		for i, e := range x.Elts {
			elems[i] = c.expr(e)
		}
		return Sep(commaSpaceLit, elems...)
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

	switch {
	case len(unelided) == 0 && !hasInterior:
		return StringLit("[]")
	case len(unelided) == 1 && !hasInterior && c.shouldHug(unelided[0]):
		return Cats(lBracketLit, c.expr(unelided[0]), rBracketLit)
	}

	var firstElem, lastElem ast.Node
	if len(unelided) > 0 {
		firstElem = unelided[0]
		lastElem = unelided[len(unelided)-1]
	}

	// Compute policy first so we can thread WantTrailingComma into
	// listElemRow before building inner.
	layout := bracketedLayout{
		node:                x,
		open:                lBracketLit,
		close:               rBracketLit,
		openerRel:           x.Lbrack.RelPos(),
		closerRel:           x.Rbrack.RelPos(),
		firstElem:           firstElem,
		lastElem:            lastElem,
		numElems:            len(unelided),
		hasInterior:         hasInterior,
		anyDoc:              anyHasDocComment(unelided),
		anyPost:             anyHasPostComment(unelided),
		sameLineOpen:        hasSameLineOpener(unelided),
		noElemNewline:       noElemHasNewline(unelided),
		lineHeader:          hasLineLeadingComment(slots, firstElem),
		allowsTrailingComma: true,
	}
	policy := c.computeBracketedPolicy(layout)

	var inner Doc
	if rows := c.elementRows(unelided, policy.WantTrailingComma); rows != nil {
		inner = Table(rows)
	}
	layout.inner = c.wrapInteriorComments(inner, slots.prefix, slots.suffix)

	// Only the body is returned - interior comments (slots.prefix /
	// slots.suffix) are already in inner. Doc and trailing comments
	// are handled by the caller (expr() -> withComments, or
	// listElemRow which categorises them so post-element comments
	// land *after* the parent's comma).
	return c.applyBracketed(layout, policy)
}

// isContiguousOpener reports whether e's rendering wraps its broken
// inner content in a Nest of its own. When that's true, an enclosing
// construct (struct/list/call) can drop its own Nest at the boundary
// where it meets e: e's Nest will provide the one indent level the
// inner content needs, so the two layers don't compound. This is the
// mechanism that keeps chains like `[{…}]`, `({…})`, or
// `f(g(h([…])))` at one indent level for their broken bodies instead
// of one per opener.
//
// StructLit/ListLit/ParenExpr begin literally with `{`/`[`/`(`.
// CallExpr (`fun(args)`) and IndexExpr (`x[i]`) begin with their
// callee/receiver text and only then expose their opener, but the
// opener is still on the same physical line as the callee, and the
// Nest they introduce around their broken body has the same effect
// on indent. So they qualify too - the "contiguous" in the name
// refers to *layout effect on indent*, not literal bracket adjacency.
func isContiguousOpener(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.StructLit:
		return x.Lbrace.IsValid()
	case *ast.ListLit:
		return x.Lbrack.IsValid()
	case *ast.ParenExpr:
		return true
	case *ast.CallExpr:
		return true
	case *ast.IndexExpr:
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

// noElemHasNewline reports whether every element shares a line with
// its predecessor (no Newline / NewSection RelPos on any element).
// When true, the bracketed construct emits no newlines from its own
// openBreak / row separators under asInfiniteWidth - its Nest is unused,
// and stacking it on top of an inner construct's own Nest pushes
// inner breaks one indent level too deep. The parent's Nest is then
// dropped (see [computeBracketedPolicy]'s shareIndent path), so an
// inner list/struct/call that breaks across lines lands at the
// parent's caller-indent + its own Nest - not + 2.
func noElemHasNewline[T ast.Node](elems []T) bool {
	for _, e := range elems {
		if e.Pos().IsNewline() {
			return false
		}
	}
	return true
}

// hasLineLeadingComment reports whether the parent's opener line ends
// with a `//` comment that the formatter must keep there. It returns
// true in two cases:
//
//   - slots.prefix's first comment is Line=true: a comment attached
//     to the bracketed node itself, written as `{ // c` immediately
//     after the opener.
//   - the first element/decl carries a Line=true Position=0 doc
//     comment: the user wrote `{ // c\n field`, `[ // c\n elem`, or
//     `( // c\n arg`, and the `// c` was hung off the first child by
//     the parser.
//
// In both shapes the renderer should emit a space - not a hard break
// - between the opener and the comment so the user's "header" comment
// keeps its position. The user's exact spacing between the opener and
// the `//` is not preserved: a single space is always emitted, since
// CUE's parser collapses both adjacency and any-amount-of-whitespace
// into the same RelPos (Blank).
//
// Pass an empty [commentSlots] when the parent node carries no
// interior comments of its own (e.g. CallExpr) - only the first
// child's doc comment is then inspected.
func hasLineLeadingComment(slots commentSlots, first ast.Node) bool {
	if len(slots.prefix) > 0 && slots.prefix[0].Pos().RelPos() == token.Blank {
		return true
	}
	if first == nil {
		return false
	}
	for _, cg := range ast.Comments(first) {
		if cg.Position == posDoc {
			return cg.Pos().RelPos() == token.Blank
		}
	}
	return false
}

// bracketedLayout describes the shared decoration of struct / list /
// call literal bodies - the `<openPrefix><open><inner><close>` shape
// with hug, shareIndent, openBreak, closeBreak, and maybeGroup
// rules. Every field is filled by the caller; finishBracketed and
// applyBracketed compute the rest.
type bracketedLayout struct {
	node       ast.Node     // for maybeGroup + relPosInChildren lookup
	openPrefix Doc          // emitted before open (e.g. CallExpr's fun); nil otherwise
	open       Doc          // bracket open token (lBrace / lBracket / lParen)
	close      Doc          // bracket close token
	openerRel  token.RelPos // RelPos of the opening token (used for openBreak when body is empty)
	closerRel  token.RelPos // RelPos of the closing token

	firstElem ast.Node // first element (nil if body is empty); EmbedDecl-unwrapped at the call site if needed
	lastElem  ast.Node // last element (nil if body is empty)
	numElems  int

	hasInterior   bool // posPrefix/posSuffix interior comments on the bracket
	anyDoc        bool // any element has a Position-0 doc comment (anyHasDocComment)
	anyPost       bool // any element has a post-element comment (anyHasPostComment)
	sameLineOpen  bool // hasSameLineOpener over the elements
	noElemNewline bool // noElemHasNewline over the elements
	lineHeader    bool // user wrote `{ // c\n…`

	// allowsTrailingComma reports whether CUE syntax permits a
	// trailing comma before the closing bracket. True for list
	// literals only; struct fields are separated by commas/newlines
	// but `}` itself does not accept a trailing comma, and `f(a,)`
	// is a parse error. Centralising the rule here means the policy
	// for "do we want one on the last element?" lives in
	// computeBracketedPolicy as policy.WantTrailingComma rather than
	// scattered in each callsite.
	allowsTrailingComma bool

	inner Doc // body content; interior comments already prepended/appended for struct/list
}

// bracketedPolicy holds the layout decisions derived from a
// bracketedLayout: whether the bracket pair hugs first/last, whether
// it shares indent with an inner opener, the resolved open and close
// breaks, and whether the last element should carry a trailing comma.
type bracketedPolicy struct {
	HugFirst          bool
	HugLast           bool
	ShareIndent       bool
	WantTrailingComma bool
	OpenBreak         Doc
	CloseBreak        Doc
}

// computeBracketedPolicy derives the [bracketedPolicy] from b.
// Callers that need policy values before they build inner - listLit
// in particular needs WantTrailingComma to thread through to
// listElemRow - call this directly, then call applyBracketed once
// inner is ready.
func (c *converter) computeBracketedPolicy(b bracketedLayout) bracketedPolicy {
	// hug strips the closer's line break entirely (`{a:1}]` style),
	// so a post-element comment on any element would land on the
	// closer's line and `//` would swallow the bracket - anyPost
	// disqualifies it.
	cleanForHug := !b.hasInterior && !b.anyDoc && !b.anyPost
	soleElem := cleanForHug && b.numElems == 1
	hugFirst := soleElem && b.firstElem != nil &&
		nodeIsContiguousOpener(b.firstElem) &&
		!b.firstElem.Pos().IsNewline()
	hugLast := soleElem && b.lastElem != nil &&
		nodeIsContiguousOpener(b.lastElem) &&
		b.closerRel < token.Newline
	authored := c.authored(b.node)
	// shareIndent only drops the parent's Nest - closeBreak still
	// fires, so `]` / `}` / `)` keeps its own line and a post-element
	// comment can render as a separate Raw row above the closer
	// without risk. Disabling shareIndent on anyPost would just shift
	// the surviving elements' bodies by one indent level, which is
	// surprising: commenting out a sibling shouldn't move what's left.
	cleanForShare := !b.hasInterior && !b.anyDoc
	// Share indent in two flavours:
	//   - sameLineOpen: an inner contiguous opener (e.g. `[{…}, …]`)
	//     provides the indent for breaks; the parent drops its Nest
	//     so the two layers don't compound.
	//   - noElemNewline: every element shares a line with its
	//     predecessor, so under asInfiniteWidth the parent's openBreak and
	//     row separators emit no newlines. The parent's Nest is then
	//     unused for its own structure - and stacking it on top of an
	//     inner break (e.g. an inner list whose body is multi-line in
	//     `{a: 1, b: [\n c\n]}`) would push that break one indent
	//     level too deep. Drop the Nest in this case too.
	shareIndent := authored && cleanForShare && !hugFirst && (b.sameLineOpen || b.noElemNewline)
	// Trailing comma policy:
	//   - The bracket's syntax must allow one (allowsTrailingComma).
	//   - In Group-mode rendering (no RelPos in subtree) the runtime
	//     IfBreak inside TrailingComma() decides at render time, so
	//     we set WantTrailingComma=true unconditionally and let the
	//     IfBreak resolve to "" when the bracket fits flat.
	//   - In authored mode the body passes through asInfiniteWidth and
	//     IfBreak is resolved statically to its Broken branch - so we
	//     have to decide statically too: only when the closing bracket
	//     has Newline/NewSection RelPos (i.e. lands on its own line).
	//   - Hugging the closer to the last element (`{a:1}]`) leaves
	//     no space for a comma - never emit one.
	wantTrailingComma := b.allowsTrailingComma
	if wantTrailingComma && authored {
		wantTrailingComma = b.closerRel >= token.Newline
	}
	if hugLast {
		wantTrailingComma = false
	}
	return bracketedPolicy{
		HugFirst:          hugFirst,
		HugLast:           hugLast,
		ShareIndent:       shareIndent,
		WantTrailingComma: wantTrailingComma,
		OpenBreak:         openBreakDoc(b.lineHeader, b.hasInterior, b.firstElem, b.openerRel),
		CloseBreak:        closeBreakDoc(b.closerRel, b.lineHeader),
	}
}

// applyBracketed assembles the final Doc using a precomputed policy.
// Drops the parent's Nest under hugFirst/shareIndent (the inner
// element's Nest provides the indent); drops closeBreak under
// hugLast so `}]` / `)]` / `}}` stay adjacent.
func (c *converter) applyBracketed(b bracketedLayout, p bracketedPolicy) Doc {
	nested := Nest(Cat(p.OpenBreak, b.inner))
	switch {
	case p.HugFirst:
		// Drop openBreak and the parent's Nest entirely - the inner
		// element's own Nest provides the one indent level needed.
		nested = b.inner
	case p.ShareIndent:
		// Keep openBreak so leading non-opener elements render
		// normally, but skip the parent's Nest so the same-line
		// opener's content shares indent.
		nested = Cat(p.OpenBreak, b.inner)
	}
	closeBreak := p.CloseBreak
	if p.HugLast {
		closeBreak = nil
	}
	return c.maybeGroup(b.node,
		Cats(b.openPrefix, b.open, nested, closeBreak, b.close))
}

// openBreakDoc returns the separator between the opening bracket
// and the inner content. lineHeader keeps a `{ // c\n…` comment on
// the opener line; hasInterior forces a HardLine so a `//` swallowing
// the closer is impossible; otherwise the first element's leading
// RelPos drives, falling back to the opener's own RelPos when the
// body is empty.
func openBreakDoc(lineHeader, hasInterior bool, firstElem ast.Node, openerRel token.RelPos) Doc {
	switch {
	case lineHeader:
		return spaceLit
	case hasInterior:
		return HardLine()
	case firstElem != nil:
		return relBreak(leadingRelPos(firstElem))
	default:
		return relBreak(openerRel)
	}
}

// closeBreakDoc returns the separator between the inner content and
// the closing bracket. A `//` header on the opener line forces a
// HardLine so the closer doesn't get swallowed by the comment.
func closeBreakDoc(closerRel token.RelPos, lineHeader bool) Doc {
	if lineHeader && closerRel < token.Newline {
		return HardLine()
	}
	return relBreak(closerRel)
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

// anyHasPostComment reports whether any node in nodes carries a
// post-element comment - a non-doc, non-Line-true comment that the
// parser hung off the node (Position 1/2 with Line=false on
// non-bracketed nodes, or Position >= posTrailingMin with Line=false
// on any node). Used to disqualify list/struct/call hugging because
// an unhugged closing bracket would land on the same line as the
// last comment, which then swallows it.
func anyHasPostComment[T ast.Node](nodes []T) bool {
	for _, n := range nodes {
		if hasPostComment(n) {
			return true
		}
	}
	return false
}

// hasPostComment reports whether n carries a post-element comment.
// (See anyHasPostComment for the classification.)
func hasPostComment(n ast.Node) bool {
	for _, cg := range ast.Comments(n) {
		if cg.Position == posDoc {
			continue
		}
		if cg.Pos().RelPos() == token.Blank {
			continue
		}
		// Bracketed nodes (StructLit/ListLit/BinaryExpr) handle their
		// own posPrefix/posSuffix interior comments - those don't
		// count as "post-element" because they're inside the body.
		if selfManagesInteriorComments(n) &&
			(cg.Position == posPrefix || cg.Position == posSuffix) {
			continue
		}
		return true
	}
	return false
}

// elemBreak returns the line-break portion of a list element or
// chain arm separator, without any comma. Uses leadingRel so a
// NewSection on the expression's doc comment becomes a blank line
// before this element - placed before the comment, not between the
// comment and the body.
func elemBreak(e ast.Expr) Doc {
	return relBreakOr(leadingRelPos(e), lineBreakSpace)
}

// elementRows builds the per-element Rows for a list literal or
// call expression. Each element produces one element row (with
// listElemRow handling its own categorisation of doc / trailing /
// post-element comments) plus zero or more Raw rows for any post-
// element comments that listElemRow returned. Inter-element rows
// carry an [elemBreak]-derived Sep so a Newline / NewSection RelPos
// on the element gets honoured. trailingComma threads the parent's
// trailing-comma decision (listLit's policy.WantTrailingComma; for
// callExpr it is always false because the parser disallows
// `f(a,)`).
//
// Shared by listLit and callExpr - the loop body is identical
// except for the source slice ([]ast.Expr) and the surrounding
// container.
func (c *converter) elementRows(elems []ast.Expr, trailingComma bool) []Row {
	if len(elems) == 0 {
		return nil
	}
	rows := make([]Row, 0, len(elems))
	lastIdx := len(elems) - 1
	for i, e := range elems {
		row, postCgs := c.listElemRow(e, i == lastIdx, trailingComma)
		if i > 0 {
			row.Sep = elemBreak(e)
		}
		rows = append(rows, row)
		rows = append(rows, c.postCommentRows(postCgs)...)
	}
	return rows
}

// listElemRow builds a Row for a list element (or call argument).
// The Row has cells [value+comma, trailing-//-comment] so trailing
// comments align in a column across rows when the list is rendered
// broken. Doc comments become Row.DocComment and render above the
// value without contributing to column widths.
//
// For the last element, the separator depends on trailing: when
// trailing is true (list literals) it is a TrailingComma emitted
// only in broken-mode, so an inline list does not acquire a spurious
// comma; when trailing is false (function-call arguments) no comma
// is emitted at all.
//
// Comments attached to e at Position 1/2 with Line=false are
// "post-element" comments: the user wrote them on their own line
// after the element (e.g. `[a, b,\n\n// c\n]` where `// c` sits before
// `]` and the parser hangs it off `b` because there is no following
// element). They cannot be folded into the element's cell - that
// would put the trailing comma after `// c`, which then absorbs the
// comma. They are returned to the caller to be emitted as separate
// Raw rows after the element's row.
func (c *converter) listElemRow(e ast.Expr, last, trailing bool) (Row, []*ast.CommentGroup) {
	eDoc := c.exprCore(e)

	var comma Doc
	switch {
	case !last:
		comma = commaLit
	case trailing:
		comma = TrailingComma()
	}

	// For StructLit/ListLit/BinaryExpr, exprCore already placed
	// prefix/suffix (interior) comments inside the brackets - those
	// slots are skipped here. The doc slot becomes the row's
	// DocComment; the trailing slot is split by Line into trailing-
	// comment cell content (Line=true) vs post-element rows
	// (Line=false), the latter returned to the caller for emission
	// as Raw rows after this row's own Sep+comma so the trailing
	// comma stays glued to the value rather than being swallowed by
	// a `//` comment.
	slots := classifyComments(e)
	skipInterior := selfManagesInteriorComments(e)
	var trailingComment Doc
	var postComments []*ast.CommentGroup
	hasComment := false
	routeNonDoc := func(cg *ast.CommentGroup) {
		hasComment = true
		if cg.Pos().RelPos() == token.Blank {
			trailingComment = joinLines(trailingComment, c.commentGroup(cg))
		} else {
			postComments = append(postComments, cg)
		}
	}
	if !skipInterior {
		for _, cg := range slots.prefix {
			routeNonDoc(cg)
		}
		for _, cg := range slots.suffix {
			routeNonDoc(cg)
		}
	}
	for _, cg := range slots.trailing {
		routeNonDoc(cg)
	}

	cells := []Doc{Cat(eDoc, comma)}
	if trailingComment != nil {
		cells = append(cells, trailingComment)
	}

	return Row{
		DocComment: c.docCommentBlock(slots.doc, e.Pos().RelPos()),
		Cells:      cells,
		HasComment: hasComment,
	}, postComments
}

// postCommentRows turns a list of post-element comments into Raw
// rows. Each row's Sep honours the comment's own RelPos
// (NewSection -> BlankLine, Newline/anything else -> HardLine) so
// blank lines the user wrote between the element and the comment, or
// between consecutive comments, survive into the output.
func (c *converter) postCommentRows(cgs []*ast.CommentGroup) []Row {
	if len(cgs) == 0 {
		return nil
	}
	rows := make([]Row, 0, len(cgs))
	for _, cg := range cgs {
		sep := relBreakOr(cg.Pos().RelPos(), HardLine())
		rows = append(rows, Row{Sep: sep, Raw: c.commentGroup(cg)})
	}
	return rows
}

// ellipsis converts an Ellipsis node.
func (c *converter) ellipsis(x *ast.Ellipsis) Doc {
	if x.Type != nil {
		return Cat(ellipsisLit, c.expr(x.Type))
	}
	return ellipsisLit
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
		body = Cats(StringLit(op), spaceLit, xDoc)
	} else {
		body = Cat(StringLit(op), xDoc)
	}

	return c.withComments(x.X, body)
}

// postfixExpr converts a PostfixExpr. The operand is obtained via
// exprCore so that the suffix operator is placed before any
// trailing comments on the operand.
func (c *converter) postfixExpr(x *ast.PostfixExpr) Doc {
	xDoc := c.exprCore(x.X)
	body := Cat(xDoc, StringLit(x.Op.String()))

	return c.withComments(x.X, body)
}

// binaryExpr converts a BinaryExpr. A | or & chain that carries any
// same-line trailing // comment is routed to chainTableExpr so the
// trailing comments line up in a single column. Chains whose
// post-first arms are all bracketed (struct/list/paren/call/index)
// fall through to binaryExprPrec - the operator stays inline as
// `} | {` and each bracketed arm breaks itself when needed. The
// remaining | / & chains go through chainGroup, which renders the
// arms inline when they fit the configured width and breaks them
// onto one-arm-per-line when they don't. Non-chain BinaryExprs
// (precedence-sensitive operators like +, -, *, ==, etc.) go through
// binaryExprPrec.
//
// Callers that need to inject an enclosing field's trailing comment
// into the chain (see fieldRow) call chainTableExpr directly with a
// non-nil fieldTrailing.
func (c *converter) binaryExpr(x *ast.BinaryExpr) Doc {
	if x.Op == token.OR || x.Op == token.AND {
		arms, hasTrailing := flattenBinaryChain(x)
		if hasTrailing {
			return c.chainTableExpr(x, arms, nil)
		}
		if len(arms) > 1 && allBracketArms(arms[1:]) {
			return c.binaryExprPrec(x, binaryCutoff(x, 1), 1)
		}
		return c.chainGroupArms(x, arms)
	}
	return c.binaryExprPrec(x, binaryCutoff(x, 1), 1)
}

// chainGroupArms formats a same-operator | or & chain as a Group
// whose arms are joined by soft separators. In flat-mode the arms
// render on one line as `a | b | c`. When the chain doesn't fit, the
// Group breaks and the soft separator becomes a hard newline,
// yielding
//
//	a |
//		b |
//		c
//
// Continuation arms are indented one level deeper than the first
// arm. Newline / NewSection RelPos on an arm is honoured: it forces
// the corresponding separator to a HardLine / BlankLine, so a
// hand-broken chain in the source preserves its layout under
// [infiniteWidth].
func (c *converter) chainGroupArms(x *ast.BinaryExpr, arms []chainArm) Doc {
	if len(arms) == 1 {
		return c.armDoc(arms[0])
	}
	opStr := " " + x.Op.String()

	first := c.armDoc(arms[0])
	first = Cat(first, StringLit(opStr))

	rest := make([]Doc, 0, 2*(len(arms)-1))
	for i := 1; i < len(arms); i++ {
		elem := c.armDoc(arms[i])
		if i < len(arms)-1 {
			elem = Cat(elem, StringLit(opStr))
		}
		rest = append(rest, elemBreak(arms[i].expr), elem)
	}

	// The Nest indents continuation arms when the chain breaks across
	// lines (`a |\n\tb |\n\tc`). When the chain stays flat, applying
	// the Nest pushes any inner HardLine (e.g. a bracketed arm whose
	// own body breaks) one indent level too deep - `a & b & {\n\t\tc:
	// _\n\t}` instead of the wanted `a & b & {\n\tc: _\n}`. So:
	//
	//   - authored mode: detect statically. The chain breaks iff any
	//     arm separator carries Newline/NewSection RelPos. The IfBreak
	//     trick doesn't work here because asInfiniteWidth (under
	//     infiniteWidth) resolves IfBreak to its Broken branch, which
	//     would force Nest even on a flat chain.
	//   - programmatic mode: chain might break on width at render
	//     time. Use IfBreak so the Group's flat-vs-broken decision
	//     drives whether the Nest applies.
	chainBreaks := false
	for i := 1; i < len(arms); i++ {
		if leadingRelPos(arms[i].expr) >= token.Newline {
			chainBreaks = true
			break
		}
	}
	body := Cats(rest...)
	var wrapped Doc
	switch {
	case chainBreaks:
		wrapped = Nest(body)
	case c.authored(x):
		wrapped = body
	default:
		wrapped = SwitchMode(Nest(body), body)
	}

	return c.maybeGroup(x, Cat(first, wrapped))
}

// allBracketArms reports whether every arm's expression is a
// contiguous opener (struct/list/paren/call/index). When true, the
// chain's continuation arms have their own brackets to provide
// visual grouping, so the chain skips its own Nest and the arms
// share the enclosing context's indent.
func allBracketArms(arms []chainArm) bool {
	if len(arms) == 0 {
		return false
	}
	for _, a := range arms {
		if !isContiguousOpener(a.expr) {
			return false
		}
	}
	return true
}

// armDoc renders a single chainArm: its expression, with any
// interior comments injected (matching chainTableExpr's behaviour
// for consistency).
func (c *converter) armDoc(a chainArm) Doc {
	elem := c.expr(a.expr)
	if len(a.interior) == 0 {
		return elem
	}
	if s, ok := a.expr.(*ast.StructLit); ok && s.Lbrace.IsValid() {
		return c.structLitInjected(s, a.interior)
	}
	prefix := make([]Doc, 0, 2*len(a.interior)+1)
	for _, cg := range a.interior {
		prefix = append(prefix, c.commentGroup(cg), HardLine())
	}
	prefix = append(prefix, elem)
	return Cats(prefix...)
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
		maybeSpace = spaceLit
	}

	// Position semantics on a BinaryExpr (only the internal slots
	// are processed here; posDoc and trailing slots are externalised
	// to expr() -> withComments / listElemRow / fieldRow, so binary
	// handlers don't double-render them):
	//   posPrefix : interior of the RHS - typically a comment written
	//               inside an empty `{ … }` on the right that the
	//               parser hung off the BinaryExpr because there was
	//               no field to attach it to.
	//   posSuffix, Line=true  : same-line `//` trailing the operator.
	//   posSuffix, Line=false : own-line `//` comment between op and
	//                           right (forces a break before RHS).
	//
	// CUE "//" line comments extend to end-of-line, so any non-doc
	// comment forces the RHS onto the next line. Operator must stay
	// on the left's line (leading operator on a new line is not valid
	// CUE because of auto-semicolon insertion).
	slots := classifyComments(x)
	interior := slots.prefix
	var opInline []Doc               // posSuffix && rel == Blank (same line as op)
	var midBlock []*ast.CommentGroup // posSuffix && rel != Blank (own line)
	for _, cg := range slots.suffix {
		if cg.Pos().RelPos() == token.Blank {
			opInline = append(opInline, spaceLit, c.commentGroup(cg))
		} else {
			midBlock = append(midBlock, cg)
		}
	}

	// Interior (Position==1) comments must be rendered inside the RHS.
	// When the RHS is a braced StructLit we can inject them into the
	// struct body so the output parses back to the same attachment.
	// Without a braced struct to host them we fall back to placing
	// them between op and right - which is not ideal but better than
	// dropping them.
	// brokenRHS builds `left op<opInline>\n<midBlock comments>\nbody`,
	// indenting body and any midBlock comments by one nest level.
	// Used in two places: the interior-comment-into-empty-struct case
	// when there are *also* other constraints forcing a break, and
	// the general "RHS goes on its own line" path below.
	brokenRHS := func(body Doc) Doc {
		inner := make([]Doc, 0, 2*len(midBlock)+2)
		inner = append(inner, HardLine())
		for _, cg := range midBlock {
			inner = append(inner, c.commentGroup(cg), HardLine())
		}
		inner = append(inner, body)
		return Cats(left, maybeSpace, StringLit(op), Cats(opInline...), Nest(Cats(inner...)))
	}

	if len(interior) > 0 {
		if s, ok := x.Y.(*ast.StructLit); ok && s.Lbrace.IsValid() {
			injected := c.structLitInjected(s, interior)
			if len(opInline) > 0 || len(midBlock) > 0 || x.Y.Pos().IsNewline() {
				// Still break before Y because of other constraints.
				return brokenRHS(injected)
			}
			return Cats(left, maybeSpace, StringLit(op), maybeSpace, injected)
		}
		// No braced struct on the RHS: fall through with interior
		// comments merged into midBlock (they'll land between op and
		// right, forcing a break).
		midBlock = append(midBlock, interior...)
	}

	if x.Y.Pos().IsNewline() || len(opInline) > 0 || len(midBlock) > 0 {
		return brokenRHS(right)
	}
	return Cats(left, maybeSpace, StringLit(op), maybeSpace, right)
}

// structLitInjected renders a braced StructLit with extra comments
// prepended to its body. Used when comments that the parser attached
// to a surrounding BinaryExpr logically belong inside the struct's
// braces (typical for comments written inside an otherwise-empty
// "{ ... }"). When the first injected comment is Line=true (the user
// wrote it trailing the opener on the same line, e.g. `{// c`), keep
// it on `{`'s line with a single space - matching how structLit
// handles its own Position=1 prefix comments.
func (c *converter) structLitInjected(x *ast.StructLit, extra []*ast.CommentGroup) Doc {
	inner := c.wrapInteriorComments(c.declSlice(x.Elts), extra, nil)
	openBreak := Doc(HardLine())
	closeBreak := relBreak(x.Rbrace.RelPos())
	if len(extra) > 0 && extra[0].Pos().RelPos() == token.Blank {
		openBreak = spaceLit
		if x.Rbrace.RelPos() < token.Newline {
			// A `//` header runs to end-of-line, so `}` must start
			// a fresh line even when the source didn't request one.
			closeBreak = HardLine()
		}
	}
	return c.maybeGroup(x, Cats(
		lBraceLit,
		Nest(Cat(openBreak, inner)),
		closeBreak,
		rBraceLit,
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
//
// Comments on the outermost BinaryExpr (the one passed in as x)
// split by Position. Position=posTrailingMin and above are the
// chain's *outer* trailing comments - withComments around the chain
// handles those, so flattenBinaryChain skips them to avoid double-
// rendering. Position=posPrefix (interior-of-next-arm) and
// Position=posSuffix (between op and right) still get collected
// onto arm trailing/interior; they're "inside the chain" and have
// no other home - withComments skips them because BinaryExpr
// self-manages interior comments.
//
// hasTrailing reports whether any intermediate node carries a
// trailing comment of any kind (anything at Position >= posSuffix,
// inline or own-line). The chain-render dispatch uses this to
// route to chainTableExpr (which renders trailing comments in a
// column-aligned cell) whenever there are trailing comments to
// preserve; the alternative path, chainGroupArms, doesn't render
// trailing comments and would silently drop them.
func flattenBinaryChain(x *ast.BinaryExpr) (arms []chainArm, hasTrailing bool) {
	var pending []*ast.CommentGroup // interior comments pending for next arm
	var walk func(e ast.Expr, outermost bool)
	walk = func(e ast.Expr, outermost bool) {
		if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == x.Op {
			walk(bin.X, false)
			// Split this BinaryExpr's comments. Position=posPrefix
			// always means interior-of-next-arm (regardless of
			// layout), matching the classification in binaryExprPrec;
			// posSuffix is "between op and right" and also belongs
			// to the chain. Position >= posTrailingMin is post-
			// chain trailing - on intermediate BinaryExprs it
			// belongs to the preceding arm, but on the outermost it
			// is the chain's own outer trailing and is handled by
			// the surrounding withComments wrap.
			var trailing, interior []*ast.CommentGroup
			for _, cg := range ast.Comments(bin) {
				switch {
				case cg.Position == posPrefix:
					interior = append(interior, cg)
				case outermost && cg.Position >= posTrailingMin:
					// Skip - withComments handles this.
				default:
					trailing = append(trailing, cg)
				}
			}
			if len(trailing) > 0 {
				hasTrailing = true
			}
			arms[len(arms)-1].trailing = append(arms[len(arms)-1].trailing, trailing...)
			pending = append(pending, interior...)
			walk(bin.Y, false)
			return
		}
		arms = append(arms, chainArm{expr: e, interior: pending})
		pending = nil
	}
	walk(x, true)
	return arms, hasTrailing
}

// chainTableExpr formats a chain of same-operator BinaryExprs (| or &)
// as a Table: one row per arm, with an optional trailing-comment cell
// that column-aligns across arms. fieldTrailing, when non-nil, is an
// enclosing field's same-line trailing comment that should align with
// the chain's arm comments in the same column. Used only when there
// is at least one trailing comment somewhere in the chain or a
// fieldTrailing is supplied; without comments, binaryExprPrec gives
// a cleaner result.
func (c *converter) chainTableExpr(x *ast.BinaryExpr, arms []chainArm, fieldTrailing Doc) Doc {
	opStr := " " + x.Op.String()

	type armInfo struct {
		elem    Doc
		comment Doc
	}

	infos := make([]armInfo, len(arms))
	for i, a := range arms {
		var commentDoc Doc
		for _, cg := range a.trailing {
			commentDoc = joinLines(commentDoc, c.commentGroup(cg))
		}
		infos[i] = armInfo{elem: c.armDoc(a), comment: commentDoc}
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
			cell0 = Cat(cell0, StringLit(opStr))
		}
		if i == 0 {
			// First row as Raw so its op suffix stays glued to the
			// arm expression; its trailing comment is appended with a
			// space since there's no column to align to yet.
			row := cell0
			if info.comment != nil {
				row = Cats(row, spaceLit, info.comment)
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

	return c.maybeGroup(x, Nest(Table(rows)))
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
// elements: RelPos is honoured, commas come before trailing comments.
// Calls allow a trailing comma before ')' on the same terms as lists:
// computeBracketedPolicy emits one statically when the closing paren
// lands on its own line (authored mode), and via the IfBreak inside
// TrailingComma() when a width-driven break occurs (programmatic
// mode).
func (c *converter) callExpr(x *ast.CallExpr) Doc {
	fun := c.expr(x.Fun)

	if len(x.Args) == 0 {
		return Cats(fun, StringLit("()"))
	}
	if len(x.Args) == 1 && c.shouldHug(x.Args[0]) {
		return Cats(fun, lParenLit, c.expr(x.Args[0]), rParenLit)
	}

	layout := bracketedLayout{
		node:          x,
		openPrefix:    fun,
		open:          lParenLit,
		close:         rParenLit,
		openerRel:     x.Lparen.RelPos(),
		closerRel:     x.Rparen.RelPos(),
		firstElem:     x.Args[0],
		lastElem:      x.Args[len(x.Args)-1],
		numElems:      len(x.Args),
		hasInterior:   false, // calls don't carry posPrefix/posSuffix interior comments
		anyDoc:        anyHasDocComment(x.Args),
		anyPost:       anyHasPostComment(x.Args),
		sameLineOpen:  hasSameLineOpener(x.Args),
		noElemNewline: noElemHasNewline(x.Args),
		// CallExpr carries no interior comments of its own - the
		// parser hangs Position 1/2 comments on the args themselves,
		// not on the call. Pass an empty commentSlots so only the
		// first arg's leading doc-comment is inspected.
		lineHeader:          hasLineLeadingComment(commentSlots{}, x.Args[0]),
		allowsTrailingComma: true,
	}
	policy := c.computeBracketedPolicy(layout)
	layout.inner = Table(c.elementRows(x.Args, policy.WantTrailingComma))
	return c.applyBracketed(layout, policy)
}

// indexExpr converts an IndexExpr. Honours RelPos on the index
// expression.  A newline before ']' is not valid CUE (auto-comma
// insertion triggers), so the index and closing bracket stay on the
// same line.
func (c *converter) indexExpr(x *ast.IndexExpr) Doc {
	openBreak := relBreak(x.Index.Pos().RelPos())
	return c.maybeGroup(x, Cats(
		c.expr(x.X),
		lBracketLit,
		Nest(Cat(openBreak, c.expr(x.Index))),
		rBracketLit,
	))
}

// sliceExpr converts a SliceExpr.
func (c *converter) sliceExpr(x *ast.SliceExpr) Doc {
	low := c.expr(x.Low)
	high := c.expr(x.High)
	return Cats(c.expr(x.X), lBracketLit, low, colonLit, high, rBracketLit)
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
			lParenLit,
			Nest(Cat(HardLine(), c.expr(x.X))),
			rParenLit,
		)
	}
	return Cats(lParenLit, c.expr(x.X), rParenLit)
}

// interpolation converts an Interpolation node. The Elts alternate
// between string fragments (BasicLit) and interpolated
// expressions. The string fragments already include the \( and )
// delimiters, so we emit them verbatim and just format the
// expressions.
//
// Multi-line strings get special treatment: the body's strip prefix
// (the leading whitespace before the closing `"""`) is parsed and
// lifted into the renderer's nest level via [AtIndent]. Without this
// the literal `\t…` body indent is just text from the renderer's
// point of view, leaving its nest level at the field's level - so a
// width-driven break inside `\(…)` would land at field-indent + 1
// rather than at body-indent + 1. With AtIndent, line breaks inside
// the body (whether from HardLine or from the call's broken `(`)
// emit indentation at strip-prefix levels, so the existing single
// Nest inside callExpr/listLit lands the args one level deeper than
// the line that contained `\(`.
func (c *converter) interpolation(x *ast.Interpolation) Doc {
	multiLine := false
	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if strings.Contains(lit.Value, "\n") {
				multiLine = true
				break
			}
		}
	}

	if !multiLine {
		parts := make([]Doc, len(x.Elts))
		for i, e := range x.Elts {
			if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				parts[i] = StringLit(lit.Value)
			} else {
				parts[i] = c.expr(e)
			}
		}
		return Cats(parts...)
	}

	return c.multiLineInterpolation(x)
}

// multiLineInterpolation handles the multi-line case of [interpolation].
// It walks the segments, splitting BasicLit text on `\n`, and produces
// an opener (everything up to the first newline, rendered at the
// caller's indent) plus a body wrapped in AtIndent. Body lines have
// the strip prefix removed from the start - the renderer re-emits it
// verbatim via AtIndent's prefix.
func (c *converter) multiLineInterpolation(x *ast.Interpolation) Doc {
	stripPrefix := stripPrefixFromInterp(x)

	var opener, body []Doc
	inBody := false

	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			parts := strings.Split(lit.Value, "\n")
			for i, p := range parts {
				if i > 0 {
					// Line break before this part. A line that
					// starts with the strip prefix uses HardLine -
					// AtIndent re-emits the prefix on render, and we
					// strip it from `p` so we don't double up. A
					// line without the prefix (a bare empty line, or
					// whitespace shorter than the prefix) uses
					// LitLine, a bare `\n` that bypasses AtIndent's
					// prefix; we render `p` verbatim. This preserves
					// exactly what the user wrote - we never *add*
					// indentation to a line that didn't have it.
					inBody = true
					if strings.HasPrefix(p, stripPrefix) {
						body = append(body, HardLine())
						p = p[len(stripPrefix):]
					} else {
						body = append(body, lineBreakBare)
					}
				}
				if p == "" {
					continue
				}
				if !inBody {
					opener = append(opener, StringLit(p))
				} else {
					body = append(body, StringLit(p))
				}
			}
		} else {
			ed := c.expr(e)
			if !inBody {
				opener = append(opener, ed)
			} else {
				body = append(body, ed)
			}
		}
	}

	if !inBody {
		// Defensive: shouldn't happen since multiLine was true.
		return Cats(opener...)
	}

	return Cats(Cats(opener...), AtIndent(stripPrefix, Cats(body...)))
}

// stripPrefixFromInterp returns the body strip prefix of a multi-line
// Interpolation: the whitespace before the closing `"""`. Returns ""
// if x is empty or its quotes can't be parsed.
func stripPrefixFromInterp(x *ast.Interpolation) string {
	if len(x.Elts) == 0 {
		return ""
	}
	first, last := x.Quotes()
	qi, _, _, err := literal.ParseQuotes(first.Value, last.Value)
	if err != nil {
		return ""
	}
	return qi.Whitespace()
}

// funcExpr converts a Func node.
func (c *converter) funcExpr(x *ast.Func) Doc {
	args := make([]Doc, len(x.Args))
	for i, a := range x.Args {
		args[i] = c.expr(a)
	}
	argDoc := Sep(commaSpaceLit, args...)
	return Cats(StringLit("func"), lParenLit, argDoc, StringLit("): "), c.expr(x.Ret))
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
			if leadingRelPos(clause) >= token.Newline || hasDocComment(clause) {
				cl = Cat(HardLine(), cl)
			} else {
				cl = Cat(spaceLit, cl)
			}
		}
		parts[i] = cl
	}

	if x.Value != nil {
		valSep := spaceLit
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
		return Cats(StringLit("if "), c.expr(x.Condition))
	case *ast.LetClause:
		return c.letClause(x)
	case *ast.TryClause:
		return c.tryClause(x)
	default:
		return nil
	}
}

// letClause converts a LetClause.
func (c *converter) letClause(x *ast.LetClause) Doc {
	return Cats(StringLit("let "), StringLit(x.Ident.Name), StringLit(" = "), c.expr(x.Expr))
}

// forClause converts a ForClause.
func (c *converter) forClause(x *ast.ForClause) Doc {
	parts := []Doc{StringLit("for ")}
	if x.Key != nil {
		parts = append(parts, StringLit(x.Key.Name), commaSpaceLit)
	}
	parts = append(parts, StringLit(x.Value.Name), StringLit(" in "), c.expr(x.Source))
	return Cats(parts...)
}

// tryClause converts a TryClause.
func (c *converter) tryClause(x *ast.TryClause) Doc {
	if x.Ident != nil {
		return Cats(StringLit("try "), StringLit(x.Ident.Name), StringLit(" = "), c.expr(x.Expr))
	}
	return StringLit("try")
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
	return Cats(StringLit(" "), StringLit(kw), StringLit(" "), c.expr(comp.Fallback.Body))
}

// decl converts a declaration node to a Doc (without comments - those
// are handled by the caller in declSlice or expr).
func (c *converter) decl(d ast.Decl) Doc {
	switch x := d.(type) {
	case *ast.Field:
		return c.field(x)

	case *ast.Alias:
		return Cats(StringLit(x.Ident.Name), StringLit(" = "), c.expr(x.Expr))

	case *ast.EmbedDecl:
		return c.expr(x.Expr)

	case *ast.LetClause:
		return c.letClause(x)

	case *ast.Ellipsis:
		return c.ellipsis(x)

	case *ast.Comprehension:
		return c.comprehension(x)

	case *ast.Package:
		return Cats(StringLit("package "), StringLit(x.Name.Name))

	case *ast.ImportDecl:
		return c.importDecl(x)

	case *ast.Attribute:
		return StringLit(x.Text)

	case *ast.CommentGroup:
		return c.commentGroup(x)

	case *ast.BadDecl:
		return StringLit("/* BadDecl */")

	default:
		return nil
	}
}

// field converts a Field to a Doc (full field, not table row).
// All comments are handled here so the caller does not need to
// wrap the result in withComments.
func (c *converter) field(f *ast.Field) Doc {
	slots := classifyComments(f)
	// Position 1 (slots.prefix, between label and colon) is not
	// generated by the CUE parser: there is no source layout for
	// `name <comment> :` - `//` runs to end-of-line, so the colon
	// would have to start a fresh line, and the parser doesn't
	// allow a newline between label and colon. Programmatic ASTs
	// that set Position=1 on a Field are out of scope; we ignore
	// slots.prefix here.
	key := c.fieldKey(f)
	// Position 2: between colon and value. A suffix comment forces
	// val onto its own line - `//` runs to end-of-line, so val
	// can't share a line with the comment regardless of whether
	// the comment was authored inline (rel == Blank, on the colon's
	// line) or on its own line.
	leadingBreak := c.exprHasDocComment(f.Value) || valNeedsLeadingBreak(f.Value) || len(slots.suffix) > 0
	// Apply Position=2 comments. Inline ones (rel == Blank, e.g.
	// `: // c\n  v`) stay on the colon's line by appending to key;
	// own-line ones go inside val's Nest at the value's indent so
	// re-parse / re-format round-trips: the parser re-attaches such
	// a comment as a doc of the value with Newline RelPos, which
	// renders identically (also at val's indent).
	var preVal Doc
	for _, cg := range slots.suffix {
		cd := c.commentGroup(cg)
		rel := cg.Pos().RelPos()
		if cg.Line {
			rel = token.Blank
		}
		if rel == token.Blank {
			key = Cat(key, c.commentSep(cg, cd))
		} else {
			if preVal != nil {
				preVal = Cats(preVal, HardLine(), cd)
			} else {
				preVal = cd
			}
		}
	}
	val := c.fieldValDoc(f, leadingBreak, preVal)

	var before, after []Doc
	for _, cg := range slots.doc {
		before = append(before, c.commentGroup(cg), HardLine())
	}
	for _, cg := range slots.trailing {
		// Position >= posTrailingMin: after the field - placed in
		// the after slot with the appropriate separator.
		after = append(after, c.commentSep(cg, c.commentGroup(cg)), SwitchMode(nil, HardLine()))
	}

	var body Doc
	// When val is preceded by a leading break (doc comment or a
	// braceless struct that starts on a new line), skip the " "
	// between key and val.
	if leadingBreak {
		body = Cat(key, val)
	} else {
		body = Cats(key, spaceLit, val)
	}

	return Cats(append(append(before, body), after...)...)
}

// fieldRow splits a Field into a table Row for alignment.
// Doc comments are placed in DocComment (before the key, not affecting
// column widths). Same-line trailing comments go into a separate cell
// for cross-row alignment. Position 1 comments are appended to the
// key. Position 2 comments are deferred and applied to val after it
// is computed. Post-field block comments (Position >= 3, not same-line
// trailing, with Newline/NewSection RelPos) are returned separately
// so the caller can emit them as sibling comment blocks after the
// field, preserving their original vertical position. The third
// return value is the braceless-chain length for the row's key
// (1 for a plain field, 2 for `x: y: val`, etc.); the caller uses it
// to flush the table when consecutive rows have differently-shaped
// composite keys.
func (c *converter) fieldRow(chain []*ast.Field) (Row, []*ast.CommentGroup, int) {
	// For braceless chains (x: y: z: val) the caller provides the
	// chain so we don't re-walk it; the head is the field this row
	// is for, the leaf carries the value.
	f := chain[0]
	leaf := chain[len(chain)-1]

	// Build per-label Docs once; both the chain key and (for atomic
	// chain rows) the merged-first-cell shape weave the same labels
	// into different nested-Group structures.
	var labelDocs []Doc
	if len(chain) > 1 {
		labelDocs = make([]Doc, len(chain))
		for i, cf := range chain {
			labelDocs[i] = c.fieldKey(cf)
		}
	}

	var key Doc
	if len(chain) == 1 {
		key = c.fieldKey(f)
	} else {
		// Build the chain key as nested Groups (one per split point)
		// without val, so cell 0 has a clean chain that can align
		// across sibling rows in a multi-row segment.
		inner := labelDocs[len(chain)-1]
		for i := len(chain) - 2; i >= 0; i-- {
			inner = Group(Cat(labelDocs[i], Nest(Cat(lineBreakSpace, inner))))
		}
		key = inner
	}

	slots := classifyComments(f)
	// Position 1 (slots.prefix) is not generated by the parser on
	// Fields - see [field] for the rationale. Ignored here too.
	var trailingComment Doc
	var postComments []*ast.CommentGroup
	hasComment := false
	// Position 2: between colon and value. The val isn't computed
	// yet (we need its type to know whether attrs/trailing align in
	// a chain table), so defer until later.
	deferred := slots.suffix
	if len(deferred) > 0 {
		hasComment = true
	}
	// Position >= posTrailingMin: a Newline/NewSection RelPos means
	// the comment is on its own line below the field - emit it as a
	// sibling Raw row. Anything else (Blank, or no RelPos) is a same-
	// line trailing `//` placed in the trailingComment cell so it
	// column-aligns across rows.
	for _, cg := range slots.trailing {
		hasComment = true
		if cg.Pos().IsNewline() {
			postComments = append(postComments, cg)
		} else {
			trailingComment = joinLines(trailingComment, c.commentGroup(cg))
		}
	}
	docComment := c.docCommentBlock(slots.doc, f.Pos().RelPos())

	// If the value is a | or & chain AND the chain carries any
	// trailing comments, hand the field's own trailing comment to
	// chainTableExpr so it column-aligns with the chain's arm
	// comments. Otherwise keep it as a separate cell in the field row
	// (so it aligns with simple fields' trailing comments).
	//
	// Attributes: for the plain (non-chain) path, attrs get their own
	// table cell (column 2) so they column-align across rows just like
	// trailing comments do. For the chain path the val cell is itself
	// a multi-line table, so attrs are appended inline instead - there
	// is no well-defined column position for them in that case.
	var val Doc
	var attrsDoc Doc
	bin, isChain := leaf.Value.(*ast.BinaryExpr)
	isChain = isChain && trailingComment != nil && (bin.Op == token.OR || bin.Op == token.AND)
	var binArms []chainArm
	var binHasTrailing bool
	if isChain {
		binArms, binHasTrailing = flattenBinaryChain(bin)
	}
	if isChain && binHasTrailing {
		val = appendAttrs(c.chainTableExpr(bin, binArms, trailingComment), leaf.Attrs)
		trailingComment = nil
	} else {
		val = c.expr(leaf.Value)
		attrsDoc = attrsSpaced(leaf.Attrs)
	}

	// Deferred comments are all Position=2 (between colon and value);
	// prepend them to val.
	for _, cg := range deferred {
		val = Cats(c.commentSep(cg, c.commentGroup(cg)), val)
	}

	// Column layout:
	//   [key, val]                     - neither attrs nor comment
	//   [key, val, attrs]              - attrs only
	//   [key, val, nil, trailing]      - comment only (reserves the
	//                                    attrs column so comment lands
	//                                    in the same column across
	//                                    attr-bearing rows)
	//   [key, val, attrs, trailing]    - both
	cells := []Doc{key, val}
	if attrsDoc != nil || trailingComment != nil {
		cells = append(cells, attrsDoc)
		if trailingComment != nil {
			cells = append(cells, trailingComment)
		}
	}

	// For chain rows whose val is atomic, also build a merged
	// alternative for cell 0: a tree of nested Groups with val
	// inside the deepest Nest. The renderer uses this when the
	// segment as a whole can't fit flat, so each split point's
	// flat-fit check includes val and partial chain breaks land
	// the value on the chain's actual deepest line. When val is
	// complex (Group/Table/LitLine inside) we leave MergedFirstCell
	// nil so its own break point handles overflow.
	var mergedFirstCell Doc
	if len(chain) >= 2 {
		_, valBroken, valCanWrap := measureCell(val)
		if !valBroken && !valCanWrap {
			inner := val
			for i := len(chain) - 1; i >= 0; i-- {
				inner = Group(Cat(labelDocs[i], Nest(Cat(lineBreakSpace, inner))))
			}
			mergedFirstCell = inner
		}
	}

	return Row{
		DocComment:      docComment,
		Cells:           cells,
		HasComment:      hasComment,
		AllowRowBreak:   true,
		MergedFirstCell: mergedFirstCell,
	}, postComments, len(chain)
}

// commentSep returns a Doc that places a comment with the
// appropriate separation based on its leading RelPos. The CUE
// parser sets cg.Line=true exactly when it sets the comment's
// Slash RelPos to token.Blank (same line as the previous token -
// see [parser.consumeCommentGroup] and [parser.next]); the two
// knobs encode the same intent. We fold Line into the RelPos here
// so downstream logic only inspects one source of truth.
//
// Mapping:
//   - rel == Blank: same-line trailing - emit " // ...".
//   - rel == Newline: own line, single break.
//   - rel == NewSection: own line, blank line before.
//   - any other rel (NoRelPos, NoSpace, Elided): fall back to a
//     blank line. The comment must not be inlined - CUE's `//`
//     runs to end-of-line, so squashing onto a shared line would
//     absorb subsequent tokens. The blank-line default is also
//     load-bearing for round-trip stability: when the parser
//     re-attaches such a comment as a doc of the next decl,
//     declSep's NewSection upgrade fires for Definition / non-
//     field prev, and the original output must already have the
//     blank line for idempotency.
func (c *converter) commentSep(cg *ast.CommentGroup, cd Doc) Doc {
	rel := cg.Pos().RelPos()
	if cg.Line {
		rel = token.Blank
	}
	soft := Doc(blankLine)
	if rel == token.Blank {
		soft = spaceLit
	}
	return Cat(relBreakOr(rel, soft), cd)
}

// fieldKey builds the key portion of a field: label + alias +
// constraint + colon.
func (c *converter) fieldKey(f *ast.Field) Doc {
	hasColon := f.Value != nil || f.TokenPos.IsValid()
	// Fast path for the overwhelmingly common shape `label:` -
	// avoids the parts slice + Cats(...) vararg allocation in the
	// general path.
	if f.Alias == nil && f.Constraint == token.ILLEGAL && hasColon {
		return Cat(c.label(f.Label), colonLit)
	}

	key := c.label(f.Label)
	if f.Alias != nil {
		key = Cat(key, c.postfixAlias(f.Alias))
	}
	if f.Constraint == token.OPTION || f.Constraint == token.NOT {
		key = Cat(key, StringLit(f.Constraint.String()))
	}
	if hasColon {
		key = Cat(key, colonLit)
	}
	return key
}

// fieldValDoc builds the value portion of a field: value + attributes.
// If leadingBreak is true, the value is wrapped in Nest(HardLine + ...)
// so it is rendered on its own line, indented relative to the key.
// preVal, when non-nil, is inserted inside the Nest before the value
// (separated from value by a HardLine) - used by [field] to place
// own-line Position=2 comments at the value's indent level so they
// round-trip cleanly: re-parsing such output attaches the comment as
// a doc comment of the value, and re-rendering produces the same
// indented placement.
func (c *converter) fieldValDoc(f *ast.Field, leadingBreak bool, preVal Doc) Doc {
	val := appendAttrs(c.expr(f.Value), f.Attrs)

	if leadingBreak {
		if preVal != nil {
			val = Nest(Cats(HardLine(), preVal, HardLine(), val))
		} else {
			val = Nest(Cat(HardLine(), val))
		}
	}

	return val
}

// valNeedsLeadingBreak reports whether a field's value must be
// rendered on its own line, indented relative to the key, rather
// than continued after "key: " on the same line. Two shapes qualify:
//
//   - the value is a braceless StructLit whose first element carries
//     Newline/NewSection RelPos (a synthetic chain whose first body
//     decl was written on its own line, e.g. x:\n y: 1);
//   - the value itself carries Newline/NewSection RelPos (the user
//     wrote a non-struct value on a new line, e.g. x:\n 1). This
//     mirrors the row-break that fieldRow performs for over-wide
//     simple fields, so the formatter is idempotent on its own
//     output even when re-parse drops a chain into the field()
//     complex path.
func valNeedsLeadingBreak(v ast.Expr) bool {
	if v.Pos().IsNewline() {
		return true
	}
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
		return Cats(StringLit("~("), StringLit(a.Label.Name), commaLit, StringLit(a.Field.Name), rParenLit)
	}
	// Simple form: ~X
	return Cat(StringLit("~"), StringLit(a.Field.Name))
}

// importDecl converts an ImportDecl. Comments attached to each
// ImportSpec are wrapped via withComments so that position-0 doc
// comments and position->0 trailing/after comments are preserved.
func (c *converter) importDecl(x *ast.ImportDecl) Doc {
	if !x.Lparen.IsValid() {
		// Single import without parens.
		if len(x.Specs) == 1 {
			s := x.Specs[0]
			body := Cats(StringLit("import "), c.importSpec(s))
			return c.withComments(s, body)
		}
	}

	specs := make([]Doc, len(x.Specs))
	for i, s := range x.Specs {
		spec := c.withComments(s, c.importSpec(s))
		if i > 0 {
			// Use leadingRel so a NewSection on the spec's doc
			// comment (the first visible token when the spec has
			// one) is honoured - otherwise a blank line written
			// before a doc-commented spec is silently dropped, since
			// the spec's own RelPos is just Newline.
			spec = Cat(relBreakOr(leadingRelPos(s), HardLine()), spec)
		}
		specs[i] = spec
	}

	body := Cats(specs...)
	return Cats(
		StringLit("import ("),
		Nest(Cat(HardLine(), body)),
		HardLine(),
		rParenLit,
	)
}

// importSpec converts an ImportSpec.
func (c *converter) importSpec(s *ast.ImportSpec) Doc {
	if s.Name != nil {
		return Cats(StringLit(s.Name.Name), spaceLit, StringLit(s.Path.Value))
	}
	return StringLit(s.Path.Value)
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
//   - Position 0   -> doc (before the node)
//   - Position 1   -> prefix (inside, just after the opener)
//   - Position 2   -> suffix (inside, just before the closer)
//   - Position >=3 -> trailing (after the node)
//
// CommentGroup.Line distinguishes layout flavour, not syntax: it is
// true when the comment ends the line that the previous token starts
// on (e.g. `{ // c` followed by a newline) and false when the comment
// stands on its own line. Line does NOT change which slot the comment
// belongs in: a `// c` written immediately after `{` is still at
// Position 1 and is interior to the braces.
//
// Non-bracketed nodes (e.g. fields) generally only use Position 0
// (doc) and trailing positions. Any Position 1/2 on such a node is
// still classified into prefix/suffix/trailing consistently, but
// specialised converters (e.g. fieldRow) handle the token-specific
// semantics themselves rather than going through this function.
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

// REVIEW_DONE_MS
//
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

// REVIEW_DONE_MS
//
// wrapInteriorComments places prefix comments before inner and suffix
// comments after inner, each on its own line - consecutive comments
// separated by HardLine, and a HardLine between the comment block(s)
// and the inner body. When inner is nil only the comments are
// emitted, joined by a HardLine between the prefix and suffix blocks
// if both are present.
//
// Used by structLit / listLit to fold their slots.prefix and
// slots.suffix interior comments into the body before the bracketed
// rendering pipeline applies hug/shareIndent. structLitInjected
// reuses the prepend-only shape by passing nil for suffix.
func (c *converter) wrapInteriorComments(inner Doc, prefix, suffix []*ast.CommentGroup) Doc {
	if len(prefix) == 0 && len(suffix) == 0 {
		return inner
	}
	parts := make([]Doc, 0, (len(prefix)+len(suffix))*2+1)
	addBlock := func(cgs []*ast.CommentGroup) {
		for i, cg := range cgs {
			if i > 0 {
				parts = append(parts, HardLine())
			}
			parts = append(parts, c.commentGroup(cg))
		}
	}
	addBlock(prefix)
	switch {
	case inner != nil:
		if len(prefix) > 0 {
			parts = append(parts, HardLine())
		}
		parts = append(parts, inner)
		if len(suffix) > 0 {
			parts = append(parts, HardLine())
		}
	case len(prefix) > 0 && len(suffix) > 0:
		// inner is nil but both blocks exist - join them.
		parts = append(parts, HardLine())
	}
	addBlock(suffix)
	return Cats(parts...)
}

// withComments wraps a Doc with its node's attached comments. The
// separator between the last doc comment and the body honours the
// node's RelPos (NewSection -> blank line).
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
	//
	// Bracketed nodes (StructLit/ListLit) - those whose exprCore
	// already places interior comments inside the body - skip the
	// prefix/suffix slots here so the same comments aren't rendered
	// both inside and outside the brackets.
	skipInterior := selfManagesInteriorComments(n)
	trailing := make([]*ast.CommentGroup, 0,
		len(slots.prefix)+len(slots.suffix)+len(slots.trailing))
	if !skipInterior {
		trailing = append(trailing, slots.prefix...)
		trailing = append(trailing, slots.suffix...)
	}
	trailing = append(trailing, slots.trailing...)

	var after []Doc
	prevOwnLine := false
	for _, cg := range trailing {
		// Trailing // comment: place it, then force the enclosing
		// group to break so the comment doesn't swallow closing
		// brackets/braces in flat-mode. IfBreak(nil, HardLine) is
		// invisible in broken-mode but prevents flat rendering.
		sep := c.commentSep(cg, c.commentGroup(cg))
		// Consecutive own-line comments with no specific RelPos
		// would each request a BlankLine separator from commentSep;
		// collapse the gap between the second and later ones to a
		// HardLine so the sequence re-parses as a single comment
		// group (matching what declSep's NewSection upgrade would
		// produce on the next decl).
		rel := cg.Pos().RelPos()
		ownLine := rel != token.Blank
		if ownLine && prevOwnLine && rel <= token.Blank {
			sep = Cat(HardLine(), c.commentGroup(cg))
		}
		prevOwnLine = ownLine
		after = append(after, sep, SwitchMode(nil, HardLine()))
	}

	parts := make([]Doc, 0, 2+len(after))
	if doc := c.docCommentBlock(slots.doc, n.Pos().RelPos()); doc != nil {
		parts = append(parts, doc)
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
		docs = append(docs, StringLit(comment.Text))
	}
	return Cats(docs...)
}

// HardLine returns a hard line break that always emits a newline.
// Any Group containing a HardLine is forced to break.
func HardLine() Doc {
	return lineBreakHard
}

var (
	lineBreakHard   = &docLineBreakHard{}
	lineBreakBare   = &docLineBreakBare{}
	lineBreakEmpty  = LineBreakSoft("")
	lineBreakSpace  = LineBreakSoft(" ")
	lineBreakComma  = LineBreakSoft(", ")
	blankLine       = Cat(lineBreakBare, lineBreakHard)
	commaWhenBroken = SwitchMode(StringLit(","), nil)
	lBracketLit     = StringLit("[")
	rBracketLit     = StringLit("]")
	lBraceLit       = StringLit("{")
	rBraceLit       = StringLit("}")
	lParenLit       = StringLit("(")
	rParenLit       = StringLit(")")
	spaceLit        = StringLit(" ")
	commaLit        = StringLit(",")
	commaSpaceLit   = StringLit(", ")
	periodLit       = StringLit(".")
	colonLit        = StringLit(":")
	equalsLit       = StringLit("=")
	bottomLit       = StringLit("_|_")
	ellipsisLit     = StringLit("...")
)

// SoftLineSpace is a Line that emits a space when flat.
func SoftLineSpace() Doc { return lineBreakSpace }

// SoftLineComma is a Line that emits ", " when flat.
func SoftLineComma() Doc { return lineBreakComma }

// BlankLine emits a bare newline followed by an indented newline,
// producing a truly blank line (no trailing whitespace) as a separator.
func BlankLine() Doc { return blankLine }

// TrailingComma emits a comma only in broken-mode.
func TrailingComma() Doc { return commaWhenBroken }
