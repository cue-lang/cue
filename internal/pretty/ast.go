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
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// File overview
//
// This file translates CUE AST nodes into the [Doc] algebra defined
// in doc.go. The translation entry point is [converter.node]; it
// recurses through the AST and produces a [Doc] that render.go
// renders to bytes.
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
//   - authored-mode: the formatter respects the authored layout. The
//     converted [Doc] is wrapped in a [docInfiniteWidth] scope (see
//     "finite vs infinite width" in the package doc), so width-driven
//     [docGroup] breaks are suppressed and only hard line breaks
//     produce newlines.
//
//   - programmatic-mode: width-driven Wadler-Lindig. The converted
//     [Doc] is wrapped in a [docFiniteWidth] subtree so that, when
//     the subtree is itself nested inside a [docInfiniteWidth] scope,
//     its inner [docGroup]s still pick flat-vs-broken against the
//     real width budget rather than inheriting the outer unlimited
//     budget.
//
// This mode only changes at "wrap-site" AST nodes: node types where
// the converter calls [converter.maybeGroup]. See [wrapEligibility]
// for the list. [converter.analyse] sets the [isAuthored] flag on the
// smallest wrap-site AST node that covers each RelPos cluster, so a
// RelPos hint in an otherwise-programmatic AST does not set the whole
// file into authored-mode. A programmatic pocket nested inside a
// [docInfiniteWidth] scope (a wrap-eligible subtree with no RelPos
// descendants) gets width-driven layout via the [docFiniteWidth]
// boundary.
//
// These modes are orthogonal to flat-mode vs broken-mode.
//
// # Tables
//
// The converter uses [docTable] (defined in doc.go) for column-
// aligned layouts: struct fields aligned by ":", chain arms aligned
// by trailing comments, list elements by their value column, and so
// on. See the package doc for [docTable] semantics and render.go for
// the segmentation algorithm.
//
// # Terminology
//
// See the package doc (doc.go) for algebra-level terms (flat-mode /
// broken-mode, hard / soft line breaks, finite / infinite width).
// AST-specific terms used here:
//
//   - RelPos: a token.Pos's relative-position hint. Drives
//     authored-mode layout decisions.
//   - authored-mode / programmatic-mode: see above. "Programmatic"
//     covers both code-built ASTs and parser output with RelPos
//     stripped.
//   - wrap-site: an AST node type where [converter.maybeGroup] is
//     called. Each wrap-site is a candidate for the
//     [docInfiniteWidth] wrap; isAuthored selects which actually
//     fire.
//   - chain: a brace-less sequence of Fields where each value is a
//     single-element StructLit (e.g. a: b: c: 1); or a sequence of
//     BinaryExprs with the same operator (e.g. a | b | c). A chain
//     "arm" is an operand within the chain.
//   - simple field / complex field: an [ast.Field] is "simple" if
//     [converter.simpleFieldChain] returns a non-nil chain for it -
//     it's then eligible for table alignment as `key: value`. If not,
//     it's "complex": multi-line values, doc comments on the value,
//     values on their own lines, or a non-collapsible chain.
//
// # Limits of Scope
//
// In scope: rendering ASTs that the CUE parser can produce. The
// formatter's invariant is "if t is an AST and b = format(t), then b
// == format(parse(b))". Programmatic ASTs that match this shape (no
// RelPos hints, but otherwise parser-shaped) are also fully
// supported.
//
// Out of scope:
//
//   - Rendering AST shapes the parser cannot produce. For example, a
//     [ast.Field] with a Position=1 comment (between label and colon)
//     has no source layout: `//` runs to end of line and the parser
//     doesn't allow a newline between label and colon, so this
//     construction can only be built programmatically. We render such
//     ASTs with a best-effort placement that may not round-trip;
//     preserving such positional intent is not a goal.

// nodeFlag bits record precomputed properties of AST nodes used by
// the layout decision points (see [converter.analyse]).
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
	// cg.Line == false). Set when the node has such a trailing comment
	// attached directly; propagated up only through the
	// rightmost-rendered child of non-bracketed nodes (Field,
	// BinaryExpr, UnaryExpr, SelectorExpr, LetClause, EmbedDecl,
	// PostfixExpr). Bracketed nodes (StructLit, ListLit, CallExpr,
	// IndexExpr, SliceExpr, ParenExpr, ImportDecl) end with their
	// closing bracket, so descendants don't propagate through them.
	endsWithOwnLineComment
)

// converter translates a CUE AST into the Doc algebra.
type converter struct {
	nodeFlags map[ast.Node]nodeFlag
}

// node converts an AST node to a Doc.
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

// analyse walks root to populate c.nodeFlags. For each visited
// node it ORs the relevant flag bits onto every ancestor up the
// current stack, short-circuiting at the first ancestor that already
// carries every bit being propagated - total work stays amortized
// O(N). The strict-descendants flags (newlineInChildren,
// relPosInChildren) are propagated from the parent up.
//
// isAuthored is computed by the post-order callback. Each node OR-s
// its own RelPos and its children's uncovered bits up; if a
// wrap-eligible node sees any uncovered RelPos, it sets isAuthored on
// itself and suppresses propagation to its parent (its subtree is now
// covered by its own wrap). This finds the smallest wrap-eligible
// covering subtree for each connected RelPos cluster.
//
// A wrap-eligible node only "covers" RelPos hints when its own
// structural tokens are authored ([wrapEligibility] reports authored
// == true). A wrap-eligible node whose structure was synthesised
// programmatically (e.g. a StructLit with Lbrace.IsValid() == false,
// or a File whose decls all lack RelPos hints) has no authored layout
// signal of its own, so it acts as a pass-through: RelPos hints in
// its subtree bubble past it to the nearest wrap-eligible ancestor
// with authored structure, and the synthesised wrap stays under
// [finiteWidth].
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
				if eligible, authored := wrapEligibility(n); eligible && authored {
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

// wrapEligibility reports whether n is one of the AST node types at
// which the converter calls [converter.maybeGroup] (eligible), and
// whether such a node has authored structural tokens of its own
// (authored). For non-eligible types, both results are false.
//
// The eligible node types mirror the [converter.maybeGroup] call
// sites:
//
//   - *ast.File via [converter.node]
//   - *ast.StructLit and *ast.ListLit via [converter.applyBracketed]
//     and the [converter.injectInteriorComments] path
//   - *ast.CallExpr via [converter.applyBracketed]
//   - *ast.BinaryExpr via the binary chain converters
//   - *ast.IndexExpr via [converter.indexExpr]
//
// The isAuthored algorithm only sets the flag on these node types. If
// a new converter adds a maybeGroup call on a different type, add
// that type here.
//
// authored == false for a wrap-eligible node means its brackets were
// synthesised by the converter (e.g. a programmatic StructLit with
// Lbrace.IsValid() == false), so there is no authored layout signal
// for the wrap decision to preserve and the [analyse] pass treats n
// as a pass-through for RelPos bubble-up: any RelPos hints in the
// subtree propagate past n to the nearest wrap-eligible ancestor that
// does have authored structure. The synthesised wrap then ends up
// under [finiteWidth] rather than [infiniteWidth], so the synthesised
// brackets' soft opener/closer breaks remain soft and emit newlines
// in broken mode rather than getting baked to their flat alternative
// by [asInfiniteWidth] - which would otherwise leave the closer
// smashed against the last interior decl.
//
// Only StructLit and ListLit have a meaningful authored check. The
// other wrap-eligible node types (File, CallExpr, BinaryExpr,
// IndexExpr) either always have authored structural tokens or have no
// "synthesised structure" shape worth special-casing, so authored is
// always true for them. In particular, File-level layout signals are
// supplied up front by [cuelang.org/go/internal/pretty/style.Annotate],
// which writes Newline / NewSection RelPos onto top-level decls so
// the inter-decl separators are hard breaks rather than soft - no
// pass-through is needed there.
func wrapEligibility(n ast.Node) (eligible, authored bool) {
	switch x := n.(type) {
	case *ast.StructLit:
		return true, x.Lbrace.IsValid()
	case *ast.ListLit:
		return true, x.Lbrack.IsValid()
	case *ast.File, *ast.CallExpr, *ast.BinaryExpr, *ast.IndexExpr:
		return true, true
	}
	return false, false
}

// hasTrailingCommentLine reports whether n has a trailing-position
// comment that would render on its own line (Slash RelPos >= Newline
// and !cg.Line). Same-line trailing comments don't qualify: the line
// ends naturally after them, and the parser keeps them attached to n
// on reparse.
func hasTrailingCommentLine(n ast.Node) bool {
	for _, cg := range ast.Comments(n) {
		if cg.Position >= posTrailingMin && !cg.Line && cg.Pos().RelPos() >= token.Newline {
			return true
		}
	}
	return false
}

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

// file renders a [ast.File].
func (c *converter) file(f *ast.File) Doc {
	// Partition f.Decls into a header (longest prefix of Package /
	// ImportDecl / CommentGroup decls) and a body (everything after).
	// The CUE parser guarantees Package occupies decl 0 (if present)
	// and every ImportDecl precedes every non-import non-comment
	// decl. Header decls are not field-value pairs and never align in
	// a table, so isolating them keeps declSlice's invariant simple:
	// it only ever sees the body's mix of fields and field-adjacent
	// decls.

	headerEnd := headerPrefix(f.Decls)
	header := c.fileHeader(f.Decls[:headerEnd])
	body := c.declSlice(f.Decls[headerEnd:])

	var leading, trailing []*ast.CommentGroup
	for _, cg := range ast.Comments(f) {
		if cg.Position == posDoc {
			leading = append(leading, cg)
		} else {
			trailing = append(trailing, cg)
		}
	}

	// Separator between the last leading comment and the body. Use
	// leadingRelPos so a NewSection on the first decl's own doc comment
	// (the first visible token after the file-level leading comments)
	// is honoured - otherwise a blank line between a file-level
	// comment and the next decl's doc comment is silently dropped.
	firstDeclSep := HardLine()
	if len(f.Decls) > 0 {
		firstDeclSep = relBreakOr(leadingRelPos(f.Decls[0]), HardLine())
	}

	parts := make([]Doc, 0, len(leading)*2+3+len(trailing)*2)
	lastIdx := len(leading) - 1
	for i, cg := range leading {
		parts = append(parts, c.commentGroup(cg))
		if i == lastIdx {
			parts = append(parts, firstDeclSep)
		} else {
			parts = append(parts, relBreakOr(leading[i+1].Pos().RelPos(), HardLine()))
		}
	}
	if header != nil {
		parts = append(parts, header)
		if body != nil {
			// Separator between the header and the body. declSep keys
			// off the first body decl's leading RelPos, so a blank line
			// in the source stays a BlankLine, a Newline stays a
			// HardLine, and programmatic source (NoSpace) gets a
			// lineBreakComma.
			for i := headerEnd - 1; i >= 0; i-- {
				if f.Decls[i].Pos().RelPos() != token.Elided {
					parts = append(parts, c.declSep(f.Decls[headerEnd], f.Decls[i]))
					break
				}
			}
		}
	}
	if body != nil {
		parts = append(parts, body)
	}
	for _, cg := range trailing {
		parts = append(parts, c.commentSep(cg, c.commentGroup(cg)), SwitchMode(nil, HardLine()))
	}
	return Cats(parts...)
}

// headerPrefix returns the length of the longest prefix of decls
// composed of Package / ImportDecl / CommentGroup / Attribute. The
// CUE parser rejects field decls before any ImportDecl, so this
// prefix is the run of "header" decls that visually precede the file
// body. Elided decls are skipped (they're invisible) so they don't
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
		doc := c.decl(decl)
		if _, ok := decl.(*ast.CommentGroup); !ok {
			doc = c.withComments(decl, doc)
		}
		docs = append(docs, sep, doc)
		prev = decl
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
		tableRows = tableRows[len(tableRows):]
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
			if multiLine && sep == lineBreakOrComma {
				sep = HardLine()
			}
		}
		prev = decl

		if _, ok := decl.(*ast.CommentGroup); ok {
			flushTable()
			docs = append(docs, sep, c.decl(decl))
			continue
		}

		// A blank line separator breaks the table - alignment doesn't
		// cross visual section boundaries.
		if sep == BlankLine() {
			flushTable()
		}

		f, isField := decl.(*ast.Field)
		var chain []*ast.Field
		if isField {
			chain = c.simpleFieldChain(f)
		}
		if chain != nil {
			chainLen := len(chain)
			row, postComments := c.fieldRow(chain)
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
		// rule here means table-flush concerns live in one place rather
		// than being split between construction-time decisions and
		// render-time segmentation.
		//
		// Package and ImportDecl never reach this loop: file()
		// partitions them off into the header before declSlice runs,
		// and a struct literal cannot contain either of them
		// syntactically.
		flushTable()
		doc := c.decl(decl)
		// field() (via decl()) handles all of the field's comments
		// internally; don't double-wrap with withComments.
		if !isField {
			doc = c.withComments(decl, doc)
		}
		docs = append(docs, sep, doc)
	}
	flushTable()

	return Cats(docs...)
}

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
//
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
	if leaf.Value == nil ||
		leaf.Value.Pos().IsNewline() ||
		hasDocComment(leaf.Value) ||
		hasCommentAt(leaf, posSuffix) {
		return nil
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
//     need to appear in the middle of the composite key), and
//   - no non-leaf Field carries attributes ([converter.fieldRow]
//     uses leaf.Attrs only, so attrs on the head or any intermediate
//     would be silently dropped or re-attached to the wrong target).
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
	if hasCommentAt(f, posPrefix) || hasCommentAt(f, posSuffix) {
		return chain, false
	}
	lastIdx := len(chain) - 1
	for i, cf := range chain {
		if i > 0 && (len(ast.Comments(cf)) > 0 || cf.Pos().IsNewline()) {
			return chain, false
		}
		if i < lastIdx && len(cf.Attrs) > 0 {
			return chain, false
		}
	}
	return chain, true
}

// declSep computes the separator before a declaration based on its
// leading RelPos. Newline produces a HardLine, NewSection a
// BlankLine; lower RelPos values fall back to [lineBreakOrComma] since
// declarations need at least a comma or newline between them.
//
// One upgrade fires on top of the authored RelPos: if prev's
// rendered output ends with an own-line `//` comment, the separator
// is promoted. When prev is a Definition (#Foo) or any non-field,
// non-comment decl the promotion is to NewSection (matching A4's
// shape); otherwise it is to Newline. Inside an [infiniteWidth] wrap
// a lineBreakComma would otherwise be rewritten to StringLit(", ")
// and the `//` would absorb the next decl. Promoting also keeps the
// parse/format cycle idempotent when the comment migrates from
// prev's trailing slot into the next decl's doc on reparse.
// Same-line trailing comments (`cg.Line` or `Slash.RelPos == Blank`)
// need neither: the line ends naturally after them, and the parser
// keeps them attached to prev.
//
// The companion rule "doc-commented curr after Definition / non-Field
// non-CommentGroup decl gets a blank line" (A4) is applied by
// [cuelang.org/go/internal/pretty/style.Config.Annotate], which
// raises the doc comment's Slash to NewSection before pretty runs.
// declSep reads the resulting NewSection via [leadingRelPos] and does
// not duplicate the upgrade here.
func (c *converter) declSep(d ast.Decl, prev ast.Decl) Doc {
	rel := leadingRelPos(d)

	if prev == nil {
		return relBreakOr(rel, lineBreakOrComma)
	}

	const mask = relPosInChildren | endsWithOwnLineComment
	prevTrailing := c.nodeFlags[prev]&mask == mask

	if rel < token.NewSection && prevTrailing {
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

	return relBreakOr(rel, lineBreakOrComma)
}

// hasCommentAt reports whether n has any CommentGroup whose Position
// equals pos. Returns false for nil n.
func hasCommentAt(n ast.Node, pos int8) bool {
	if n == nil {
		return false
	}
	for _, cg := range ast.Comments(n) {
		if cg.Position == pos {
			return true
		}
	}
	return false
}

// firstCommentAt returns the first CommentGroup attached to n whose
// Position equals pos, or nil if there is none. Returns nil for nil n.
func firstCommentAt(n ast.Node, pos int8) *ast.CommentGroup {
	if n == nil {
		return nil
	}
	for _, cg := range ast.Comments(n) {
		if cg.Position == pos {
			return cg
		}
	}
	return nil
}

// hasDocComment reports whether a node has any doc comments.
func hasDocComment(n ast.Node) bool {
	return hasCommentAt(n, posDoc)
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
// construction. The [infiniteWidth] wrap is driven by the
// [isAuthored] flag, not this function - see [converter.maybeGroup].
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
	// NB: because this is the only use of newlineInChildren, it's
	// tempting to change [converter.analyse] so that it includes this
	// flag on the current node rather than just ancestors (and then we
	// can drop the || here). However, doing so has performance
	// implications: it increases the size of the nodeFlags map, and it
	// means we're always doing a lookup into the map. So testing
	// n.Pos().IsNewline() here is measurably faster.
	return n.Pos().IsNewline() || c.nodeFlags[n]&newlineInChildren != 0
}

// nodeManagesInteriorComments reports whether a node's conversion
// bakes its interior (posPrefix / posSuffix) comments into the
// returned Doc, leaving doc-before and trailing- after comments for
// the caller to wrap. withComments skips prefix and suffix slots for
// these nodes so they aren't double-rendered.
//
//   - StructLit / ListLit: `{ // c }` / `[ // c ]` interior comments
//     belong between the brackets, not after them.
//   - BinaryExpr: posPrefix is "interior of RHS" (typically a comment
//     written inside an empty struct on the right that the parser
//     hung off the BinaryExpr); posSuffix non-Line is the "between op
//     and right" mid-block comment that binaryExprPrec injects. Both
//     are placed inside the chain's Doc by the binary handlers.
func nodeManagesInteriorComments(n ast.Node) bool {
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

// renderCommentChain renders cgs joined by per-rel breaks: HardLine
// by default, upgraded to BlankLine when the next group's RelPos is
// NewSection (preserving blank lines the user wrote between comment
// groups). No trailing separator is appended; callers add their own
// bridge to whatever follows. Returns nil for an empty input.
func (c *converter) renderCommentChain(cgs []*ast.CommentGroup) Doc {
	if len(cgs) == 0 {
		return nil
	}
	parts := make([]Doc, 0, 2*len(cgs)-1)
	for i, cg := range cgs {
		if i > 0 {
			parts = append(parts, relBreakOr(cg.Pos().RelPos(), HardLine()))
		}
		parts = append(parts, c.commentGroup(cg))
	}
	return Cats(parts...)
}

// docCommentBlock renders a sequence of Position=0 comment groups
// followed by a trailing separator chosen from trailingRel: HardLine
// by default; BlankLine when trailingRel is NewSection. The host
// node's RelPos drives trailingRel so a Position=0 leading comment
// separated from its host by a blank line (parsed shape) keeps that
// blank line on the right side.
//
// Exception: a true doc comment ([ast.CommentGroup.Doc] is true on
// the last group in cgs) is by definition tight to its host. The CUE
// parser only sets Doc=true when there is no blank line between the
// comment and the host, so trailingRel arriving as NewSection on such
// a node is always a synthesised AST quirk (cf. the package
// declaration coming out of [cuelang.org/go/internal/core/export],
// which carries NewSection on PackagePos so the pkg block separates
// from preceding file-level content - but the doc comment's own Slash
// already encodes that separation, so we must not repeat it between
// doc and host). Clamp at Newline in that case.
//
// Returns nil for an empty input.
func (c *converter) docCommentBlock(cgs []*ast.CommentGroup, trailingRel token.RelPos) Doc {
	chain := c.renderCommentChain(cgs)
	if chain == nil {
		return nil
	}
	if len(cgs) > 0 && cgs[len(cgs)-1].Doc && trailingRel > token.Newline {
		trailingRel = token.Newline
	}
	return Cat(chain, relBreakOr(trailingRel, HardLine()))
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

// wrapInteriorComments places prefix comments before inner, and
// suffix comments after inner, each on its own line. Consecutive
// comments within a block are separated by HardLine (upgraded to
// BlankLine when the next group's RelPos is NewSection, preserving
// authored blank lines), and a HardLine bridges between the comment
// block(s) and the inner body. When inner is nil only the comments
// are emitted, joined by a HardLine between the prefix and suffix
// blocks if both are present.
//
// Used by structLit / listLit to fold their slots.prefix and
// slots.suffix interior comments into the body before the bracketed
// rendering pipeline applies hug/shareIndent. injectInteriorComments
// reuses the prepend-only shape by passing nil for suffix.
func (c *converter) wrapInteriorComments(inner Doc, prefix, suffix []*ast.CommentGroup) Doc {
	if len(prefix) == 0 && len(suffix) == 0 {
		return inner
	}
	parts := make([]Doc, 0, 5)
	if len(prefix) > 0 {
		parts = append(parts, c.renderCommentChain(prefix))
	}
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
	if len(suffix) > 0 {
		parts = append(parts, c.renderCommentChain(suffix))
	}
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
	skipInterior := nodeManagesInteriorComments(n)
	trailing := make([]*ast.CommentGroup, 0,
		len(slots.prefix)+len(slots.suffix)+len(slots.trailing))
	if !skipInterior {
		trailing = append(trailing, slots.prefix...)
		trailing = append(trailing, slots.suffix...)
	}
	trailing = append(trailing, slots.trailing...)

	var after []Doc
	if len(trailing) > 0 {
		after = make([]Doc, 0, len(trailing)+1)
		// Trailing // comment: place it, then force the enclosing group
		// to break so the comment doesn't swallow closing
		// brackets/braces in flat-mode.
		for _, cg := range trailing {
			cgDoc := c.commentGroup(cg)
			sep := c.commentSep(cg, cgDoc)
			after = append(after, sep)
		}
		// SwitchMode(nil, HardLine()) is invisible in broken-mode and
		// prevents flat rendering.
		after = append(after, SwitchMode(nil, HardLine()))
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

// commentSep returns a Doc that wraps comment cd with the appropriate
// separation based on its leading RelPos.
//
// cg.Line=true means the comment sits on the same line as the
// preceding token; the Slash RelPos for such a comment is only
// meaningful as intra-line spacing (Blank / NoSpace). Any stronger
// RelPos there is either uninitialised (parser-produced ASTs default
// to Blank when a space precedes) or synthesised by an AST builder
// that wrote a section break onto a position where it cannot mean
// what RelPos normally means - notably
// [cuelang.org/go/internal/core/export], which sets every pkg
// comment's Slash to NewSection regardless of whether the comment is
// inline. We normalise the Line=true case to Blank so that these
// comments flow through the same "same-line trailing" path uniformly.
//
// Mapping (for cg.Line=false):
//   - rel == Blank: same-line trailing - emit " // ...".
//   - rel == Newline: own line, single break.
//   - rel == NewSection: own line, blank line before.
//   - any other rel (NoRelPos, NoSpace, Elided): fall back to a blank
//     line. The comment must not be inlined - CUE's `//` runs to
//     end-of-line, so squashing onto a shared line would absorb
//     subsequent tokens.
func (c *converter) commentSep(cg *ast.CommentGroup, cd Doc) Doc {
	rel := cg.Pos().RelPos()
	if cg.Line {
		rel = token.Blank
	}
	var sep Doc
	switch rel {
	case token.Blank:
		sep = spaceLit
	case token.Newline:
		sep = HardLine()
	default:
		// NoRelPos, NoSpace, Elided, NewSection: blank line.
		sep = BlankLine()
	}
	return Cat(sep, cd)
}

// maybeGroup wraps body in the layout primitive selected for n's
// subtree: [infiniteWidth] when n's RelPos hints must be preserved,
// otherwise [finiteWidth] for width-driven layout.
//
// When [isAuthored] is set on n, body is wrapped in [infiniteWidth] -
// the formatter cannot inject width-driven newlines into the wrapped
// subtree (only hard line breaks - [docLineBreakHard],
// [docLineBreakBare], or [BlankLine] - survive), and nested chains,
// calls, and lists stay flat unless a hard break forces them
// open. See [infiniteWidth] for the underlying mechanism.
//
// Otherwise body is wrapped in [finiteWidth], which renders it in
// width-driven Wadler-Lindig mode. See [finiteWidth] for the
// underlying mechanism, including its hole-punching behaviour when
// this subtree is nested inside an enclosing [infiniteWidth] wrap.
func (c *converter) maybeGroup(n ast.Node, body Doc) Doc {
	// Integrity check: every maybeGroup call site must operate on a
	// type listed in wrapEligibility. The isAuthored analyse pass
	// only flags eligible types, so a call on a non-eligible type
	// would silently never enter the infiniteWidth branch even when
	// the subtree carries RelPos. Panic loudly so the drift is caught
	// immediately.
	if eligible, _ := wrapEligibility(n); !eligible {
		panic(fmt.Sprintf("pretty: maybeGroup called on non-wrap-eligible type %T", n))
	}
	if c.nodeFlags[n]&isAuthored != 0 {
		return infiniteWidth(body)
	}
	return finiteWidth(body)
}

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

// relBreak returns the line-break Doc corresponding to a RelPos:
// BlankLine for NewSection, HardLine for Newline, and
// [lineBreakOrEmpty] otherwise.
func relBreak(rel token.RelPos) Doc {
	return relBreakOr(rel, lineBreakOrEmpty)
}

// leadingRelPos returns the RelPos that drives the separator placed
// before n's first visible token. If n has a Position=0 doc comment,
// the first visible token is that comment, so its RelPos wins;
// otherwise n's own RelPos applies. Distinguishing "blank line before
// the comment" from "blank line between the comment and the body"
// lets callers render each side correctly instead of collapsing both
// to a single decision.
func leadingRelPos(n ast.Node) token.RelPos {
	if cg := firstCommentAt(n, posDoc); cg != nil {
		return cg.Pos().RelPos()
	}
	return n.Pos().RelPos()
}

// expr converts an expression node to a Doc, including any comments.
func (c *converter) expr(x ast.Expr) Doc {
	if x == nil {
		return nil
	}
	return c.withComments(x, c.exprCore(x))
}

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

// label converts a Label node to a Doc.
func (c *converter) label(l ast.Label) Doc {
	switch x := l.(type) {
	case *ast.Ident:
		return StringLit(x.Name)
	case *ast.BasicLit:
		// A label rendered as a multi-line `"""..."""` or raw `#"..."#`
		// string is collapsed to its single-line escaped form. A label
		// must fit on one line (it has to share a line with at least
		// the colon, and chain labels share with one another), so the
		// multi-line representation is unusable here. Other BasicLit
		// labels - kept-single-line strings, numbers, etc. - go through
		// basicLit unchanged.
		if x.Kind == token.STRING && (strings.HasPrefix(x.Value, `"""`) || strings.HasPrefix(x.Value, "#")) {
			if u, err := literal.Unquote(x.Value); err == nil {
				return StringLit(literal.Label.Quote(u))
			}
		}
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

// basicLit converts a BasicLit to a Doc.
func (c *converter) basicLit(x *ast.BasicLit) Doc {
	value := normaliseNumericLit(x.Kind, x.Value)
	lines := strings.Split(value, "\n")
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

// normaliseNumericLit rewrites Go-style numeric literals that the
// CUE parser would reject (e.g. `0755` for octal, a trailing dot like
// `5.`, or uppercase `E` for the exponent that may collide with
// future use of `E` as a SI exa multiplier). AST builders such as
// cue get go drop Go literals into [ast.BasicLit] unchanged; this
// pass keeps the output valid CUE without forcing every builder to
// normalise on its own.
func normaliseNumericLit(kind token.Token, data string) string {
	switch kind {
	case token.INT:
		if len(data) > 1 && data[0] == '0' && data[1] >= '0' && data[1] <= '9' {
			data = "0o" + data[1:]
		}
		if p := strings.IndexByte(data, '.'); p >= 0 && data[p+1] > '9' {
			data = data[:p+1] + "0" + data[p+1:]
		}
		if p := strings.IndexByte(data, 'E'); p != -1 && p < len(data)-1 {
			data = strings.ToLower(data)
		}
	case token.FLOAT:
		switch p := strings.IndexByte(data, '.'); {
		case p < 0:
		case p == 0:
			data = "0" + data
		case p == len(data)-1:
			data += "0"
		}
	}
	return data
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
	lineHeader    bool // user wrote `{ // c\n...`

	// allowsTrailingComma reports whether CUE syntax permits a
	// trailing comma before the closing bracket. True for list
	// literals only; struct fields are separated by commas/newlines
	// but `}` itself does not accept a trailing comma, and `f(a,)`
	// is a parse error. Centralising the rule here means the policy
	// for "do we want one on the last element?" lives in
	// computeBracketedPolicy as policy.wantTrailingComma rather than
	// scattered in each callsite.
	allowsTrailingComma bool

	inner Doc // body content; interior comments already prepended/appended for struct/list
}

// bracketedPolicy holds the layout decisions derived from a
// bracketedLayout: whether the bracket pair hugs first/last, whether
// it shares indent with an inner opener, the resolved open and close
// breaks, and whether the last element should carry a trailing comma.
type bracketedPolicy struct {
	hugFirst          bool
	hugLast           bool
	shareIndent       bool
	wantTrailingComma bool
	openBreak         Doc
	closeBreak        Doc

	// useBodyShape selects the [docBodyShape]-driven layout: the body
	// gets wrapped in [docBodyShape] which independently picks one of
	// three shapes at render time. See [docBodyShape] for the
	// examples. When set, the inner content produced by
	// [applyBracketed] takes the docBodyShape branch and the policy's
	// openBreak/closeBreak/hugFirst/hugLast fields are unused.
	useBodyShape bool
}

// structLit converts a StructLit. Interior comments attached directly
// to the StructLit (Position > 0, not a trailing // line) are rendered
// inside the braces. Doc and trailing comments on the StructLit itself
// are wrapped around the result. expr() skips withComments for
// StructLit because interior comments must go inside the braces, not
// after them.
func (c *converter) structLit(x *ast.StructLit) Doc {
	var firstElem, lastElem ast.Node
	var unelided []ast.Decl
	for i, e := range x.Elts {
		if e.Pos().RelPos() == token.Elided {
			if unelided == nil {
				unelided = slices.Clip(x.Elts[:i])
			}
			continue
		}
		if firstElem == nil {
			firstElem = e
		}
		lastElem = e
		if unelided != nil {
			unelided = append(unelided, e)
		}
	}
	if unelided == nil {
		unelided = x.Elts
	}

	// A no-Lbrace StructLit always renders with synthesised braces at
	// this point: the chain form (`a: b: c: 1` written as a nested
	// braceless StructLit chain) is handled one level out by
	// [converter.fieldValDoc] and [converter.declSlice]'s
	// [converter.simpleFieldChain] - never by structLit itself.
	// Falling through to the braced rendering path is the only safe
	// choice in any other context, since a braceless multi-element
	// or non-Field body at expression position (CallExpr argument,
	// BinaryExpr operand, ListLit element, ...) is not valid CUE.
	// The braced path picks up NoRelPos from the invalid
	// Lbrace/Rbrace via openerRel/closerRel, which translates to a
	// soft break - exactly the behaviour we want for synthesised
	// braces. [analyse] has already arranged for x's isAuthored flag
	// to stay clear (see [wrapEligibility]), so [maybeGroup]
	// will pick [finiteWidth] and the soft breaks emitted inside the
	// body stay soft.

	slots := classifyComments(x)
	hasInterior := len(slots.prefix) > 0 || len(slots.suffix) > 0

	switch {
	case len(unelided) == 0 && !hasInterior:
		return StringLit("{}")
	case len(unelided) == 1 && !hasInterior && c.shouldHug(unelided[0]):
		return Cats(lBraceLit, c.decl(unelided[0]), rBraceLit)
	}

	inner := c.wrapInteriorComments(c.declSlice(unelided), slots.prefix, slots.suffix)

	layout := bracketedLayout{
		node:          x,
		open:          lBraceLit,
		close:         rBraceLit,
		openerRel:     x.Lbrace.RelPos(),
		closerRel:     x.Rbrace.RelPos(),
		firstElem:     firstElem,
		lastElem:      lastElem,
		numElems:      len(unelided),
		hasInterior:   hasInterior,
		anyDoc:        anyHasDocComment(unelided),
		anyPost:       anyHasPostComment(unelided),
		sameLineOpen:  hasSameLineOpener(unelided),
		noElemNewline: noElemHasNewline(unelided),
		lineHeader:    hasLineLeadingComment(slots, firstElem),
		inner:         inner,
	}
	return c.applyBracketed(layout, c.computeBracketedPolicy(layout))
}

// listLit converts a ListLit. As with structLit, interior comments
// attached directly to the ListLit are rendered inside the brackets
// and expr() skips withComments for ListLit.
func (c *converter) listLit(x *ast.ListLit) Doc {
	var firstElem, lastElem ast.Node
	var unelided []ast.Expr
	for i, e := range x.Elts {
		if e.Pos().RelPos() == token.Elided {
			if unelided == nil {
				unelided = slices.Clip(x.Elts[:i])
			}
			continue
		}
		if firstElem == nil {
			firstElem = e
		}
		lastElem = e
		if unelided != nil {
			unelided = append(unelided, e)
		}
	}
	if unelided == nil {
		unelided = x.Elts
	}

	// A ListLit without Lbrack only arises in programmatic ASTs: the
	// parser always populates Lbrack for parsed list literals. Unlike
	// braceless structs (which have a chain form), there is no
	// bracketless list shape in CUE syntax, so the only safe choice
	// is to synthesise brackets. We fall through to the bracketed
	// path; openerRel/closerRel pick up NoRelPos from the invalid
	// positions, which translates to a soft break - what we want for
	// synthesised brackets. [analyse] keeps x's isAuthored flag clear
	// in this case (see [wrapEligibility]), so [maybeGroup] uses
	// [finiteWidth] and the soft breaks inside the body stay soft.

	slots := classifyComments(x)
	hasInterior := len(slots.prefix) > 0 || len(slots.suffix) > 0

	switch {
	case len(unelided) == 0 && !hasInterior:
		return StringLit("[]")
	case len(unelided) == 1 && !hasInterior && c.shouldHug(unelided[0]):
		return Cats(lBracketLit, c.expr(unelided[0]), rBracketLit)
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
	useBodyShape := !hasInterior && !layout.anyDoc && !layout.anyPost && allBracketsLackRelPos(unelided)
	if useBodyShape {
		// docBodyShape's hug shape renders the body in modeBreak so a
		// per-row TrailingComma() would emit a stray `,` against the
		// closing bracket (`}]` vs `},]`). Suppress the per-row
		// trailing comma entirely when we're on the docBodyShape path;
		// the indented shape gets its trailing comma instead from
		// docBodyShape directly (see [BodyShape]'s trailingComma
		// argument), and the flat shape doesn't need one.
		policy.wantTrailingComma = false
	}
	policy.useBodyShape = useBodyShape

	var inner Doc
	if rows := c.elementRows(unelided, policy.wantTrailingComma, useBodyShape); rows != nil {
		inner = Table(rows)
	}
	layout.inner = c.wrapInteriorComments(inner, slots.prefix, slots.suffix)

	return c.applyBracketed(layout, policy)
}

// computeBracketedPolicy derives the [bracketedPolicy] from b.
// Callers that need policy values before they build inner
// (e.g. listLit needs WantTrailingComma to thread through to
// listElemRow) call this directly, then call applyBracketed once
// inner is ready.
func (c *converter) computeBracketedPolicy(b bracketedLayout) bracketedPolicy {
	// hug strips the closer's line break entirely (`{a:1}]` style), so
	// a post-element comment on any element would land on the closer's
	// line and `//` would swallow the bracket - anyPost disqualifies
	// it.
	//
	// hugFirst and hugLast fire together only for the single-element
	// `[{...}]` / `({...})` shape, where dropping both edges leaves
	// the inner opener's body at one indent level instead of two. The
	// multi-element bracketed-at-both-ends shape (`[{...}, {...}]`)
	// is handled by the [docBodyShape] / [docNextGroupNoop] machinery
	// at render time (see [bracketedPolicy.useBodyShape]), so the
	// policy here keeps the conservative single-elt-only gate.
	hugPermitted := !b.hasInterior && !b.anyDoc && !b.anyPost
	soleElem := hugPermitted && b.numElems == 1
	hugFirst, hugLast := false, false
	if soleElem && b.firstElem != nil && nodeIsContiguousOpener(b.firstElem) {
		hugFirst = !b.firstElem.Pos().IsNewline()
		hugLast = b.closerRel < token.Newline
	}
	authored := c.authored(b.node)
	// shareIndent only drops the parent's Nest - closeBreak still
	// fires, so `]` / `}` / `)` keeps its own line and a post-element
	// comment can render as a separate Raw row above the closer
	// without risk. Disabling shareIndent on anyPost would just shift
	// the surviving elements' bodies by one indent level, which is
	// surprising: commenting out a sibling shouldn't move what's left.
	shareIndentPermitted := !b.hasInterior && !b.anyDoc
	// Share indent in two flavours:
	//   - sameLineOpen: an inner contiguous opener (e.g. `[{...},
	//     ...]`) provides the indent for breaks; the parent drops its
	//     Nest so the two layers don't compound.
	//   - noElemNewline: every element shares a line with its
	//     predecessor, so under asInfiniteWidth the parent's openBreak
	//     and row separators emit no newlines. The parent's Nest is
	//     then unused for its own structure, and stacking it on top of
	//     an inner break (e.g. an inner list whose body is multi-line
	//     in `{a: 1, b: [\n c\n]}`) would push that break one indent
	//     level too deep. Drop the Nest in this case too.
	shareIndent := authored && shareIndentPermitted && !hugFirst && (b.sameLineOpen || b.noElemNewline)
	// Trailing comma policy. The if-condition excludes brackets whose
	// syntax disallows a trailing comma (allowsTrailingComma) and
	// brackets where hugging the closer to the last element (`{a:1}]`)
	// would place the comma against the closer. Inside the if-block:
	//
	//   - !authored => no RelPos in subtree: always want one. The
	//     runtime [docSwitchMode] inside TrailingComma() resolves to
	//     "" when the bracket fits flat, so emitting unconditionally
	//     here is safe.
	//   - otherwise: the body passes through asInfiniteWidth and
	//     [docSwitchMode] is resolved statically to its broken branch,
	//     so we have to decide statically too - only when the closing
	//     bracket has Newline/NewSection RelPos (i.e. lands on its own
	//     line).
	wantTrailingComma := false
	if b.allowsTrailingComma && !hugLast {
		wantTrailingComma = !authored || b.closerRel >= token.Newline
	}
	return bracketedPolicy{
		hugFirst:          hugFirst,
		hugLast:           hugLast,
		shareIndent:       shareIndent,
		wantTrailingComma: wantTrailingComma,
		openBreak:         openBreakDoc(b.lineHeader, b.hasInterior, b.firstElem, b.openerRel),
		closeBreak:        closeBreakDoc(b.closerRel, b.lineHeader),
	}
}

// applyBracketed assembles the final Doc using a precomputed policy.
// Drops the parent's Nest under hugFirst/shareIndent (the inner
// element's Nest provides the indent); drops closeBreak under
// hugLast so `}]` / `)]` / `}}` stay adjacent. When p.useBodyShape
// is set, the inner content is wrapped in [docBodyShape] which
// picks one of the three shapes (flat, indented, hug; see
// [docBodyShape]) at render time - openBreak, closeBreak, hugFirst,
// hugLast, and shareIndent are ignored in that branch.
func (c *converter) applyBracketed(b bracketedLayout, p bracketedPolicy) Doc {
	if p.useBodyShape {
		// Pass commaLit as the trailing comma so [docBodyShape]
		// emits it in the indented shape. Both ListLit and CallExpr
		// accept a trailing comma before their closer in CUE;
		// emitting it keeps the indented shape idempotent under the
		// standard re-render path. allowsTrailingComma is true for
		// the brackets that reach this branch (lists and calls), so
		// we can pass commaLit unconditionally.
		var trailingComma Doc
		if b.allowsTrailingComma {
			trailingComma = commaLit
		}
		return c.maybeGroup(b.node,
			Cats(b.openPrefix, b.open, BodyShape(b.inner, trailingComma), b.close))
	}
	var inner Doc
	switch {
	case p.hugFirst:
		// Drop openBreak and the parent's Nest entirely - the inner
		// element's own Nest provides the one indent level needed.
		inner = b.inner
	case p.shareIndent:
		// Keep openBreak so leading non-opener elements render
		// normally, but skip the parent's Nest so the same-line
		// opener's content shares indent.
		inner = Cat(p.openBreak, b.inner)
	default:
		inner = Nest(Cat(p.openBreak, b.inner))
	}
	closeBreak := p.closeBreak
	if p.hugLast {
		closeBreak = nil
	}
	return c.maybeGroup(b.node,
		Cats(b.openPrefix, b.open, inner, closeBreak, b.close))
}

// isContiguousOpener reports whether e's rendering wraps its broken
// inner content in a Nest of its own. When that's true, an enclosing
// construct (struct/list/call) can drop its own Nest at the boundary
// where it meets e: e's Nest will provide the one indent level the
// inner content needs, so the two layers don't compound. This is the
// mechanism that keeps chains like `[{...}]`, `({...})`, or
// `f(g(h([...])))` at one indent level for their broken bodies instead
// of one per opener.
//
// StructLit/ListLit/ParenExpr begin literally with `{`/`[`/`(`.
// CallExpr (`fun(args)`) and IndexExpr (`x[i]`) begin with their
// callee/receiver text and only then expose their opener, but the
// opener is still on the same physical line as the callee, and the
// Nest they introduce around their broken body has the same effect
// on indent. So they qualify too - the "contiguous" in the name
// refers to *layout effect on indent*, not literal bracket adjacency.
//
// Programmatic ASTs can present StructLits / ListLits with `Lbrace ==
// NoPos` / `Lbrack == NoPos`: the brackets don't appear in the AST,
// but the converter still synthesises `{`/`[` around the body. Those
// synthesised brackets behave identically to authored ones at render
// time: the body is wrapped in a Nest and emitted between the
// synthesised tokens. So for the contiguous-opener purpose (which is
// about layout effect on indent, not provenance of the bracket
// character) they qualify just as authored brackets do.
func isContiguousOpener(e ast.Expr) bool {
	switch e.(type) {
	case *ast.StructLit, *ast.ListLit, *ast.ParenExpr, *ast.CallExpr, *ast.IndexExpr:
		return true
	}
	return false
}

// nodeIsContiguousOpener reports whether n's rendering begins with
// an opening `{`, `[`, or `(`. For Exprs that's a StructLit, ListLit,
// or ParenExpr; for Decls only an EmbedDecl wrapping such an Expr
// qualifies (fields always begin with a label).
func nodeIsContiguousOpener(n ast.Node) bool {
	switch e := n.(type) {
	case *ast.EmbedDecl:
		return isContiguousOpener(e.Expr)
	case ast.Expr:
		return isContiguousOpener(e)
	}
	return false
}

// hasSameLineOpener walks elements left-to-right looking for the
// first opener that the user wrote on the parent's opener line.
// Walking stops at the first element with a Newline-or-stronger
// leading RelPos (including a Newline carried by a doc comment):
// that element is on a new line, and so are all later ones. Used to
// implement the same-line-opener rule: when any element opener
// shares a line with the parent's `[`/`{`/`(`, the parent suppresses
// its own Nest so the inner content sits at one shared indent level.
func hasSameLineOpener[T ast.Node](elems []T) bool {
	for _, e := range elems {
		if leadingRelPos(e) >= token.Newline {
			return false
		}
		if nodeIsContiguousOpener(e) {
			return true
		}
	}
	return false
}

// noElemHasNewline reports whether every element shares a line with
// its predecessor (no Newline / NewSection leading RelPos on any
// element - a doc comment's Slash is taken into account via
// [leadingRelPos]).
// When true, the bracketed construct emits no newlines from its own
// openBreak / row separators under asInfiniteWidth - its Nest is
// unused, and stacking it on top of an inner construct's own Nest
// pushes inner breaks one indent level too deep. The parent's Nest is
// then dropped (see [converter.computeBracketedPolicy]'s shareIndent
// path), so an inner list/struct/call that breaks across lines lands
// at the parent's caller-indent + its own Nest - not + 2.
func noElemHasNewline[T ast.Node](elems []T) bool {
	for _, e := range elems {
		if leadingRelPos(e) >= token.Newline {
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
	if cg := firstCommentAt(first, posDoc); cg != nil {
		return cg.Pos().RelPos() == token.Blank
	}
	return false
}

// openBreakDoc returns the separator between the opening bracket
// and the inner content. lineHeader keeps a `{ // c\n...` comment on
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
// Position=0 (doc) comment.
func anyHasDocComment[T ast.Node](nodes []T) bool {
	for _, n := range nodes {
		if hasDocComment(n) {
			return true
		}
	}
	return false
}

// anyHasPostComment reports whether any node in nodes carries a
// post-element comment: a non-doc, non-Line=true comment that the
// parser hung off the node (Position 1/2 with Line=false on
// non-bracketed nodes, or Position >= posTrailingMin with Line=false
// on any node).
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
	nManagesInteriorComments := nodeManagesInteriorComments(n)
	for _, cg := range ast.Comments(n) {
		if cg.Position == posDoc || cg.Pos().RelPos() == token.Blank {
			continue
		}
		// Bracketed nodes (StructLit/ListLit/BinaryExpr) handle their
		// own posPrefix/posSuffix interior comments - those don't
		// count as "post-element" because they're inside the body.
		if nManagesInteriorComments &&
			(cg.Position == posPrefix || cg.Position == posSuffix) {
			continue
		}
		return true
	}
	return false
}

// elemBreak returns the line-break portion of a list element or chain
// arm separator, without any comma. Uses leadingRelPos so a
// NewSection on the expression's doc comment becomes a blank line
// before this element - placed before the comment, not between the
// comment and the body.
func elemBreak(e ast.Expr) Doc {
	return relBreakOr(leadingRelPos(e), lineBreakOrSpace)
}

// bracketsLackRelPos reports whether e is a bracketed expression
// whose opening and closing brackets both carry no RelPos hint. This
// is the signal that no authored layout intent attaches to e's edges
// - either because the AST was built programmatically (Lbrace/Lbrack/
// Lparen == NoPos, so both ends fall through to NoRelPos) or because
// a parsed AST has had its RelPos stripped. When two adjacent
// list/call elements both satisfy this, [elementRows] replaces the
// default inter-element soft break with a literal space so the
// bracketed pair reads horizontally (e.g. `}, {`) instead of
// vertically (`},\n{`); a parsed element with `Newline` on its `{`
// falls through to [elemBreak] and keeps the line break the source
// asked for.
//
// Non-bracketed expressions (idents, literals, operators, ...) are
// reported as false: they have no brackets, so the rule doesn't
// apply, and the caller's pair-check will fall back to [elemBreak].
func bracketsLackRelPos(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.StructLit:
		return x.Lbrace.RelPos() == token.NoRelPos &&
			x.Rbrace.RelPos() == token.NoRelPos
	case *ast.ListLit:
		return x.Lbrack.RelPos() == token.NoRelPos &&
			x.Rbrack.RelPos() == token.NoRelPos
	case *ast.ParenExpr:
		return x.Lparen.RelPos() == token.NoRelPos &&
			x.Rparen.RelPos() == token.NoRelPos
	case *ast.CallExpr:
		return x.Lparen.RelPos() == token.NoRelPos &&
			x.Rparen.RelPos() == token.NoRelPos
	case *ast.IndexExpr:
		return x.Lbrack.RelPos() == token.NoRelPos &&
			x.Rbrack.RelPos() == token.NoRelPos
	}
	return false
}

// allBracketsLackRelPos reports whether every elem is bracketed
// without any RelPos hint on its outer bracket pair. Used by
// listLit / callExpr to decide whether the body is a candidate for
// the [docBodyShape] / [docNextGroupNoop] machinery: it only applies
// when every cell can participate in the per-level indented/hug
// decision, otherwise the body falls through to the standard
// bracketedLayout flow.
func allBracketsLackRelPos(elems []ast.Expr) bool {
	for _, e := range elems {
		if !bracketsLackRelPos(e) {
			return false
		}
	}
	return len(elems) > 0
}

// elementRows builds the per-element Rows for a list literal or
// call expression. Each element produces one element row (with
// listElemRow handling its own categorisation of doc / trailing /
// post-element comments) plus zero or more Raw rows for any post-
// element comments that listElemRow returned. Inter-element rows
// carry an [elemBreak]-derived Sep so a Newline / NewSection RelPos
// on the element gets honoured. trailingComma threads the parent's
// trailing-comma decision (listLit's policy.wantTrailingComma; for
// callExpr it is always false because the parser disallows
// `f(a,)`).
//
// Adjacent bracketed-without-RelPos pairs get a literal-space Sep
// instead of [elemBreak]'s soft break: when both elements are
// bracketed and both ends of each one carry no RelPos (see
// [bracketsLackRelPos]), the pair reads horizontally as
// `}, {` even when each cell's body breaks internally. Cells that
// carry RelPos hints on their brackets (parsed source with the
// authored layout intact) fall through to [elemBreak] so the user's
// per-line layout is preserved.
//
// Shared by listLit and callExpr - the loop body is identical
// except for the source slice ([]ast.Expr) and the surrounding
// container.
//
// `wrapLinked` instructs elementRows to wrap each element's value
// cell in [NextGroupNoop]. The caller sets this param when the body's
// cells will be rendered via [docBodyShape]; NextGroupNoop forwards
// the shape's modeBreak decision into each cell by neutralising the
// cell's own outer docGroup, so when the hug shape fires every cell
// stays broken alongside its siblings.
func (c *converter) elementRows(elems []ast.Expr, trailingComma, wrapLinked bool) []Row {
	if len(elems) == 0 {
		return nil
	}
	rows := make([]Row, 0, len(elems))
	lastIdx := len(elems) - 1
	prevLackRelPos := false
	for i, e := range elems {
		row, postCgs := c.listElemRow(e, i == lastIdx, trailingComma)
		if wrapLinked && len(row.Cells) > 0 {
			row.Cells[0] = NextGroupNoop(row.Cells[0])
		}
		curLackRelPos := bracketsLackRelPos(e)
		if i > 0 {
			if prevLackRelPos && curLackRelPos {
				row.Sep = spaceLit
			} else {
				row.Sep = elemBreak(e)
			}
		}
		prevLackRelPos = curLackRelPos
		rows = append(rows, row)
		rows = append(rows, c.postCommentRows(postCgs)...)
	}
	return rows
}

// listElemRow builds a Row for a list element (or call argument).
// The Row has cells [value+comma, trailing-comment] so trailing
// comments align in a column across rows when the list is rendered
// broken. Doc comments become Row.DocComment and render above the
// value without contributing to column widths.
//
// For the last element, the separator depends on trailing: when
// trailing is true (list literals) it is a TrailingComma emitted only
// in broken-mode, so an inline list does not acquire a spurious
// comma; when trailing is false (function-call arguments) no comma is
// emitted at all.
//
// Comments attached to e at Position 1/2 with Line=false are
// "post-element" comments: the user wrote them on their own line
// after the element (e.g. `[a, b,\n\n// c\n]`, the parser hangs the
// comment off `b` because there is no following element). They cannot
// be folded into the element's cell: that would put the trailing
// comma after `// c`, which would absorb the comma. So they are
// returned to the caller to be emitted as separate Raw rows after the
// element's row.
func (c *converter) listElemRow(e ast.Expr, last, trailing bool) (Row, []*ast.CommentGroup) {
	var comma Doc
	switch {
	case !last:
		comma = commaLit
	case trailing:
		comma = TrailingComma()
	}

	// For StructLit/ListLit/BinaryExpr, exprCore already placed
	// prefix/suffix (interior) comments inside the brackets so those
	// slots are skipped here. The doc slot becomes the row's
	// DocComment; the trailing slot is split by Line into trailing-
	// comment cell content (Line=true) vs post-element rows
	// (Line=false), the latter returned to the caller for emission as
	// Raw rows after this row's own Sep+comma so the trailing comma
	// stays glued to the value rather than being swallowed by a `//`
	// comment.
	slots := classifyComments(e)
	skipInterior := nodeManagesInteriorComments(e)
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

	cells := []Doc{Cat(c.exprCore(e), comma)}
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
// rows. Each row's Sep honours the comment's own RelPos (NewSection
// -> BlankLine, Newline/anything else -> HardLine) so blank lines the
// user wrote between the element and the comment, or between
// consecutive comments, survive into the output.
func (c *converter) postCommentRows(cgs []*ast.CommentGroup) []Row {
	if len(cgs) == 0 {
		return nil
	}
	rows := make([]Row, len(cgs))
	for i, cg := range cgs {
		sep := relBreakOr(cg.Pos().RelPos(), HardLine())
		rows[i] = Row{Sep: sep, Raw: c.commentGroup(cg)}
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
	if x.X.Pos().RelPos() == token.Blank {
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
// same-line trailing // comment is routed to
// [converter.chainTableExpr] so the trailing comments line up in a
// single column. Chains whose post-first arms are all bracketed
// (struct/list/paren/call/index) fall through to
// [converter.binaryExprPrec]; the operator stays inline as `} | {`
// and each bracketed arm breaks itself when needed. The remaining | /
// & chains go through [converter.chainGroupArms], which renders the
// arms inline when they fit the configured width and breaks them onto
// one-arm-per-line when they don't. Non-chain BinaryExprs
// (precedence-sensitive operators like +, -, *, ==, etc.) go through
// [converter.binaryExprPrec].
//
// Callers that need to inject an enclosing field's trailing comment
// into the chain (see [converter.fieldRow]) call chainTableExpr
// directly with a non-nil fieldTrailing.
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

// chainArm holds one operand of a flattened | or & chain together
// with any comments attached to the BinaryExpr whose operator follows
// this arm (i.e. the "| // trailing" belonging to this row's op).
type chainArm struct {
	expr     ast.Expr
	trailing []*ast.CommentGroup // Position>=2, Line=true: goes in this row's comment column
	interior []*ast.CommentGroup // Position==1: interior of next arm (inject if possible)
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
	outerPrec := x.Op.Precedence()
	if len(arms) == 1 {
		return c.armDoc(arms[0], outerPrec)
	}
	opDoc := StringLit(" " + x.Op.String())

	first := c.armDoc(arms[0], outerPrec)
	first = Cat(first, opDoc)

	arms = arms[1:]

	rest := make([]Doc, 0, 2*len(arms)-1)
	lastIdx := len(arms) - 1
	for i, arm := range arms {
		elem := c.armDoc(arm, outerPrec)
		if i < lastIdx {
			elem = Cat(elem, opDoc)
		}
		rest = append(rest, elemBreak(arm.expr), elem)
	}

	// The Nest indents continuation arms when the chain breaks across
	// lines (`a |\n\tb |\n\tc`). When the chain stays flat, applying
	// the Nest pushes any inner HardLine (e.g. a bracketed arm whose
	// own body breaks) one indent level too deep - `a & b & {\n\t\tc:
	// _\n\t}` instead of the wanted `a & b & {\n\tc: _\n}`. So:
	//
	//   - authored-mode: detect statically. The chain breaks iff any
	//     arm separator carries Newline/NewSection RelPos. The
	//     [docSwitchMode] trick doesn't work here because
	//     asInfiniteWidth (under infiniteWidth) resolves
	//     [docSwitchMode] to its broken branch, which would force
	//     Nest even on a flat chain.
	//   - programmatic-mode: chain might break on width at render
	//     time. Use [docSwitchMode] so the [docGroup]'s flat-vs-broken
	//     decision drives whether the Nest applies.
	chainBreaks := false
	for _, arm := range arms {
		if leadingRelPos(arm.expr) >= token.Newline {
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

// flattenBinaryChain walks a left-associative (or mixed) chain of
// BinaryExprs with operator x.Op and returns one chainArm per leaf
// operand. Comments on each intermediate BinaryExpr are attached to
// the arm whose operator they follow: trailing //-comments go on the
// left arm's row; interior (Position==1) comments belong to the
// following arm and are later injected into its body if it is a
// braced StructLit.
//
// Comments on the outermost BinaryExpr (x) split by
// Position. Position=posTrailingMin and above are the chain's *outer*
// trailing comments (withComments around the chain handles those, so
// flattenBinaryChain skips them to avoid
// double-rendering). Position=posPrefix (interior-of-next-arm) and
// Position=posSuffix (between op and right) still get collected onto
// arm trailing/interior; they're "inside the chain" and have no other
// home (withComments skips them because BinaryExpr self-manages
// interior comments).
//
// hasTrailing reports whether any intermediate node carries a
// trailing comment of any kind (anything at Position >= posSuffix,
// inline or own-line). The chain-render dispatch uses this to route
// to chainTableExpr (which renders trailing comments in a
// column-aligned cell) whenever there are trailing comments to
// preserve; the alternative path, chainGroupArms, doesn't render
// trailing comments and would silently drop them.
func flattenBinaryChain(x *ast.BinaryExpr) (arms []chainArm, hasTrailing bool) {
	var pending []*ast.CommentGroup // interior comments pending for next arm
	var walk func(e ast.Expr, outermost bool)
	walk = func(e ast.Expr, outermost bool) {
		bin, ok := e.(*ast.BinaryExpr)
		if !ok || bin.Op != x.Op {
			arms = append(arms, chainArm{expr: e, interior: pending})
			pending = nil
			return
		}

		walk(bin.X, false)
		// Split this BinaryExpr's comments. Position=posPrefix
		// always means interior-of-next-arm (regardless of
		// layout), matching the classification in binaryExprPrec;
		// posSuffix is "between op and right" and also belongs
		// to the chain. Position >= posTrailingMin is post-
		// chain trailing - on intermediate BinaryExprs it
		// belongs to the preceding arm, but on the outermost it
		// is the chain's own outer trailing and is handled by
		// the surrounding withComments wrap. Position==posDoc on
		// the outermost is also handled by withComments and must
		// be skipped here too - a programmatic BinaryExpr can
		// carry a Position=0 doc comment, and including it as a
		// preceding-arm trailing would duplicate the comment.
		var trailing, interior []*ast.CommentGroup
		for _, cg := range ast.Comments(bin) {
			switch {
			case cg.Position == posPrefix:
				interior = append(interior, cg)
			case outermost && (cg.Position == posDoc || cg.Position >= posTrailingMin):
				// Skip - withComments handles this.
			default:
				trailing = append(trailing, cg)
			}
		}
		hasTrailing = hasTrailing || len(trailing) > 0
		prevArm := &arms[len(arms)-1]
		prevArm.trailing = append(prevArm.trailing, trailing...)
		pending = append(pending, interior...)
		walk(bin.Y, false)
	}
	walk(x, true)
	return arms, hasTrailing
}

// chainTableExpr formats a chain of same-operator BinaryExprs (| or
// &) as a Table: one row per arm, with an optional trailing-comment
// cell that column-aligns across arms. fieldTrailing, when non-nil,
// is an enclosing field's same-line trailing comment that should
// align with the chain's arm comments in the same column. Used only
// when there is at least one trailing comment somewhere in the chain
// or a fieldTrailing is supplied; without comments, binaryExprPrec
// gives a cleaner result.
func (c *converter) chainTableExpr(x *ast.BinaryExpr, arms []chainArm, fieldTrailing Doc) Doc {
	opStr := " " + x.Op.String()
	outerPrec := x.Op.Precedence()

	rows := make([]Row, len(arms))
	for i, arm := range arms {
		var commentDoc Doc
		for _, cg := range arm.trailing {
			commentDoc = joinLines(commentDoc, c.commentGroup(cg))
		}
		cell0 := c.armDoc(arm, outerPrec)

		if i < len(arms)-1 {
			cell0 = Cat(cell0, StringLit(opStr))
		} else if fieldTrailing != nil {
			// Attach the enclosing field's trailing comment to the last
			// arm's comment cell so it column-aligns with the chain's
			// own trailing comments.
			commentDoc = joinLines(commentDoc, fieldTrailing)
		}

		hasComment := commentDoc != nil

		if i == 0 {
			// First raw as Raw so its op suffix stays glued to the arm
			// expression; its trailing comment is appended with a space
			// since there's no column to align to yet.
			raw := cell0
			if hasComment {
				raw = Cats(raw, spaceLit, commentDoc)
			}
			rows[i] = Row{
				Raw:        raw,
				HasComment: hasComment,
			}
			continue
		}

		cells := []Doc{cell0}
		if hasComment {
			cells = append(cells, commentDoc)
		}
		rows[i] = Row{
			Sep:        HardLine(),
			Cells:      cells,
			HasComment: hasComment,
		}
	}

	return c.maybeGroup(x, Nest(Table(rows)))
}

// armDoc renders a single chainArm: its expression, with any
// interior comments injected (matching chainTableExpr's behaviour
// for consistency). outerPrec is the precedence of the chain's own
// operator; when the arm is itself a [*ast.BinaryExpr] with a lower
// precedence, the arm is wrapped in parens so the rendered output
// re-parses to the same tree (the source-level grouping is encoded
// by tree shape, and the parser's compensating ParenExpr is absent
// from programmatic ASTs).
func (c *converter) armDoc(a chainArm, outerPrec int) Doc {
	body := c.armExpr(a.expr, outerPrec)
	if len(a.interior) == 0 {
		return body
	}
	if isBracketedInjectionTarget(a.expr) {
		// Bracketed targets aren't BinaryExprs, so the parens-or-not
		// decision in armExpr collapsed to a plain c.expr call. The
		// interior comments belong inside the brackets, not before
		// them, so route through injectInteriorComments.
		return c.injectInteriorComments(a.expr, a.interior)
	}
	prefix := make([]Doc, 0, 2*len(a.interior)+1)
	for _, cg := range a.interior {
		prefix = append(prefix, c.commentGroup(cg), HardLine())
	}
	prefix = append(prefix, body)
	return Cats(prefix...)
}

// armExpr renders e as a chain arm under a chain whose operator has
// precedence outerPrec. A nested BinaryExpr at lower precedence is
// wrapped in parens so the chain shape `a OP_outer arm` re-parses
// to the same tree on round-trip. See [converter.armDoc].
func (c *converter) armExpr(e ast.Expr, outerPrec int) Doc {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op.Precedence() < outerPrec {
		return Cats(lParenLit, c.expr(e), rParenLit)
	}
	return c.expr(e)
}

// isBracketedInjectionTarget reports whether e is a bracketed form
// onto which injectInteriorComments can prepend interior comments.
// Only braced StructLit and bracketed ListLit qualify: the CUE parser
// routes interior comments through the surrounding BinaryExpr for
// these two shapes when the brackets are otherwise empty. Other
// bracketed forms (CallExpr, IndexExpr, ParenExpr) attach interior
// comments directly to themselves, so they never produce interior
// comments at the BinaryExpr layer.
func isBracketedInjectionTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.StructLit:
		return x.Lbrace.IsValid()
	case *ast.ListLit:
		return x.Lbrack.IsValid()
	}
	return false
}

// injectInteriorComments renders a braced StructLit or bracketed
// ListLit with extra comments prepended to its body. Used when
// comments that the parser attached to a surrounding BinaryExpr
// logically belong inside the brackets - the parser hangs interior
// comments inside an otherwise-empty `{ ... }` or `[ ... ]` on the
// enclosing BinaryExpr because there is no field or element to host
// them. Injecting them here makes the output parse back to the same
// attachment and preserves the user's visual intent.
//
// When the first injected comment is Line=true (the user wrote it
// trailing the opener on the same line, e.g. `{// c` or `[// c`),
// keep it on the opener's line with a single space - matching how
// structLit / listLit handle their own Position=1 prefix comments.
//
// In practice x is always an empty bracket: the parser only routes
// interior comments through the BinaryExpr when the bracketed RHS
// has no field/element to absorb them. The helper still falls back
// to rendering the bracket's body for safety.
func (c *converter) injectInteriorComments(x ast.Expr, extra []*ast.CommentGroup) Doc {
	var open, body, close Doc
	var closerRel token.RelPos
	switch n := x.(type) {
	case *ast.StructLit:
		open, close = lBraceLit, rBraceLit
		body = c.declSlice(n.Elts)
		closerRel = n.Rbrace.RelPos()
	case *ast.ListLit:
		open, close = lBracketLit, rBracketLit
		if rows := c.elementRows(n.Elts, false, false); rows != nil {
			body = Table(rows)
		}
		closerRel = n.Rbrack.RelPos()
	default:
		panic(fmt.Sprintf("pretty: injectInteriorComments: unexpected %T", x))
	}
	inner := c.wrapInteriorComments(body, extra, nil)
	openBreak := Doc(HardLine())
	closeBreak := relBreak(closerRel)
	if len(extra) > 0 && extra[0].Pos().RelPos() == token.Blank {
		openBreak = spaceLit
		if closerRel < token.Newline {
			// A `//` header runs to end-of-line, so the closer must
			// start a fresh line even when the source didn't request
			// one.
			closeBreak = HardLine()
		}
	}
	return c.maybeGroup(x, Cats(
		open,
		Nest(Cat(openBreak, inner)),
		closeBreak,
		close,
	))
}

// binaryExprPrec formats a binary expression with precedence-aware
// spacing. Spaces are added around operators at precedences below the
// cutoff. Newline RelPos on Y is always honoured. Blank/NoSpace
// RelPos is ignored (the spacing is determined by precedence).
//
// (Algorithm suggestion by Russ Cox.)
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
	//               inside an empty `{ ... }` on the right that the
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
	// When the RHS is a braced StructLit or bracketed ListLit we can
	// inject them into the bracket body so the output parses back to
	// the same attachment. Without a host bracket we fall back to
	// placing them between op and right - which is not ideal but
	// better than dropping them.
	// brokenRHS builds `left op<opInline>\n<midBlock comments>\nbody`,
	// indenting body and any midBlock comments by one nest level.
	// Used in two places: the interior-comment-into-empty-bracket
	// case when there are *also* other constraints forcing a break,
	// and the general "RHS goes on its own line" path below.
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
		if isBracketedInjectionTarget(x.Y) {
			injected := c.injectInteriorComments(x.Y, interior)
			if len(opInline) > 0 || len(midBlock) > 0 || leadingRelPos(x.Y) >= token.Newline {
				// Still break before Y because of other constraints.
				return brokenRHS(injected)
			}
			return Cats(left, maybeSpace, StringLit(op), maybeSpace, injected)
		}
		// No host bracket on the RHS: fall through with interior
		// comments merged into midBlock (they'll land between op and
		// right, forcing a break).
		midBlock = append(midBlock, interior...)
	}

	if leadingRelPos(x.Y) >= token.Newline || len(opInline) > 0 || len(midBlock) > 0 {
		return brokenRHS(right)
	}
	return Cats(left, maybeSpace, StringLit(op), maybeSpace, right)
}

// binaryOperand formats one operand of a binary expression, recursing
// into nested binary expressions at the same or higher precedence.
// A nested binary at *lower* precedence is wrapped in parentheses:
// the AST encodes that grouping by tree shape (the parser inserts a
// ParenExpr for parsed source, but programmatic AST builders such
// as [cuelang.org/go/cue.Value.Syntax] do not), so we have to
// materialise the parens on the way out or the rendered output
// would re-parse to a differently-grouped tree.
func (c *converter) binaryOperand(e ast.Expr, prec, depth int) Doc {
	if bin, ok := e.(*ast.BinaryExpr); ok {
		if bin.Op.Precedence() >= prec {
			return c.binaryExprPrec(bin, binaryCutoff(bin, depth), depth)
		}
		return Cats(lParenLit, c.expr(e), rParenLit)
	}
	return c.expr(e)
}

// binaryCutoff determines the precedence cutoff for spacing
// decisions. Only operators at precedences below the cutoff get
// spaces.
//
// In normal mode (depth 1), spaces are always used unless there's a
// mix of + and * (in which case only + gets spaces, making a*b + c*d
// clear). In compact mode (depth > 1, inside a larger expression),
// spaces are minimised.
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
// lands on its own line (authored-mode), and via the [docSwitchMode]
// inside TrailingComma() when a width-driven break occurs
// (programmatic mode).
func (c *converter) callExpr(x *ast.CallExpr) Doc {
	fun := c.expr(x.Fun)

	if len(x.Args) == 0 {
		return Cats(fun, StringLit("()"))
	}
	if arg := x.Args[0]; len(x.Args) == 1 && c.shouldHug(arg) {
		return Cats(fun, lParenLit, c.expr(arg), rParenLit)
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
	useBodyShape := !layout.anyDoc && !layout.anyPost && allBracketsLackRelPos(x.Args)
	if useBodyShape {
		policy.wantTrailingComma = false
	}
	policy.useBodyShape = useBodyShape
	layout.inner = Table(c.elementRows(x.Args, policy.wantTrailingComma, useBodyShape))
	return c.applyBracketed(layout, policy)
}

// indexExpr converts an IndexExpr. Honours RelPos on the index
// expression. A newline before ']' is not valid CUE (auto-comma
// insertion triggers), so the index and closing bracket stay on the
// same line.
func (c *converter) indexExpr(x *ast.IndexExpr) Doc {
	// TODO - check recent changes to auto-comma stuff
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
	// TODO - check recent changes to auto-comma stuff
	if leadingRelPos(x.X) >= token.Newline {
		return Cats(
			lParenLit,
			Nest(Cat(HardLine(), c.expr(x.X))),
			rParenLit,
		)
	}
	return Cats(lParenLit, c.expr(x.X), rParenLit)
}

// interpolation converts an Interpolation node. The Interpolation
// elements alternate between string fragments (BasicLit) and
// interpolated expressions. The string fragments already include the
// \( and ) delimiters, so we emit them verbatim and just format the
// expressions.
//
// Multi-line strings get special treatment: the body's strip prefix
// (the leading whitespace before the closing `"""`) is parsed and
// lifted into the renderer's nest level via [AtIndent]. Without this
// the literal `\t...` body indent is just text from the renderer's
// point of view, leaving its nest level at the field's level - so a
// width-driven break inside `\(...)` would land at field-indent + 1
// rather than at body-indent + 1. With AtIndent, line breaks inside
// the body emit indentation at strip-prefix levels, so any Nest
// inside the interpolated expression lands one level deeper than the
// line that contained `\(`.
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

// multiLineInterpolation handles the multi-line case of
// [converter.interpolation]. It walks the segments, splitting
// BasicLit text on `\n`, and produces an opener (everything up to the
// first newline, rendered at the caller's indent) plus a body wrapped
// in AtIndent. Body lines have the strip prefix removed from the
// start - the renderer re-emits it verbatim via AtIndent's prefix.
func (c *converter) multiLineInterpolation(x *ast.Interpolation) Doc {
	stripPrefix := stripPrefixFromInterp(x)

	var opener, body []Doc
	inBody := false

	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			for i, line := range strings.Split(lit.Value, "\n") {
				if i > 0 {
					// Line-break before this line. A line that starts with
					// the strip prefix uses [docLineBreakHard] - AtIndent
					// re-emits the prefix on render, and we strip it from
					// `p` so we don't double up. A line without the prefix
					// (a bare empty line, or whitespace shorter than the
					// prefix) uses [docLineBreakBare], a bare `\n` that
					// bypasses AtIndent's prefix; we render `p`
					// verbatim. This preserves exactly what the user wrote
					// - we never *add* indentation to a line that didn't
					// have it.
					inBody = true
					if strings.HasPrefix(line, stripPrefix) {
						body = append(body, HardLine())
						line = line[len(stripPrefix):]
					} else {
						body = append(body, lineBreakBare)
					}
				}
				if line == "" {
					continue
				}
				if !inBody {
					opener = append(opener, StringLit(line))
				} else {
					body = append(body, StringLit(line))
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
		clauseDoc := c.withComments(clause, c.clause(clause))
		if i > 0 {
			// leadingRelPos gives the RelPos of the first visible token
			// (the doc comment if any, otherwise the clause itself), so
			// a doc-commented clause that begins on its own line keeps
			// its break. A clause that carries a doc comment must also
			// start on its own line regardless of RelPos, or the `//`
			// would absorb the inline separator and merge with whatever
			// follows.
			if hasDocComment(clause) || leadingRelPos(clause) >= token.Newline {
				clauseDoc = Cat(HardLine(), clauseDoc)
			} else {
				clauseDoc = Cat(spaceLit, clauseDoc)
			}
		}
		parts[i] = clauseDoc
	}

	if x.Value != nil {
		valSep := spaceLit
		if leadingRelPos(x.Value) >= token.Newline {
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
	return Cats(StringLit("let "), StringLit(x.Ident.Name), equalsSpaceLit, c.expr(x.Expr))
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
		return Cats(StringLit("try "), StringLit(x.Ident.Name), equalsSpaceLit, c.expr(x.Expr))
	}
	return StringLit("try")
}

// fallbackClause converts the FallbackClause of a Comprehension. The
// keyword depends on the comprehension's clauses: "otherwise" after
// for-clauses or multiple clauses, "else" after a single if/try
// clause.
func (c *converter) fallbackClause(comp *ast.Comprehension) Doc {
	// TODO check we're using the right words here
	kw := "otherwise"
	if len(comp.Clauses) == 1 {
		switch comp.Clauses[0].(type) {
		case *ast.IfClause, *ast.TryClause:
			kw = "else"
		}
	}
	return Cats(spaceLit, StringLit(kw), spaceLit, c.expr(comp.Fallback.Body))
}

// decl converts a declaration node to a Doc (without comments - those
// are handled by the caller in declSlice or expr).
func (c *converter) decl(d ast.Decl) Doc {
	switch x := d.(type) {
	case *ast.Field:
		return c.field(x)

	case *ast.Alias:
		return Cats(StringLit(x.Ident.Name), equalsSpaceLit, c.expr(x.Expr))

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

	case ast.Expr:
		// An [ast.Expr] in a Decl slot acts as an embed: the [ast.Expr]
		// interface satisfies [ast.Decl], so programmatic AST builders
		// (notably the evaluator's value-to-syntax conversion) can put a
		// bare expression directly into [ast.StructLit.Elts] without
		// wrapping it in an [*ast.EmbedDecl].
		return c.expr(x)
	}
	return nil
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
	// Apply Position=2 comments (between colon and value). Inline
	// ones (rel == Blank, e.g. `: // c\n  v`) stay on the colon's
	// line by appending to key; own-line ones go inside val's Nest
	// at the value's indent so re-parse / re-format round-trips:
	// the parser re-attaches such a comment as a doc of the value
	// with Newline RelPos, which renders identically (also at val's
	// indent).
	//
	// preVal holds the leading-break content prepended to val inside
	// Nest. Seed it with HardLine() when val needs to start on its
	// own line (doc comment, braceless chain, or any Position=2
	// comment - even inline ones force val to a new line because
	// the suffix `//` runs to end-of-line). Subsequent HardLines
	// separate stacked own-line suffix comments, and the final
	// HardLine bridges the last comment to val.
	var preVal Doc
	if hasDocComment(f.Value) || valNeedsLeadingBreak(f.Value) || len(slots.suffix) > 0 {
		preVal = HardLine()
	}
	for _, cg := range slots.suffix {
		cd := c.commentGroup(cg)
		if cg.Line || cg.Pos().RelPos() == token.Blank {
			key = Cat(key, c.commentSep(cg, cd))
		} else {
			preVal = Cats(preVal, cd, HardLine())
		}
	}
	val := c.fieldValDoc(f, preVal)

	var before, after []Doc
	for _, cg := range slots.doc {
		before = append(before, c.commentGroup(cg), HardLine())
	}
	for _, cg := range slots.trailing {
		after = append(after, c.commentSep(cg, c.commentGroup(cg)), SwitchMode(nil, HardLine()))
	}

	var body Doc
	// When val is preceded by a leading break (preVal != nil), skip
	// the " " between key and val - val already starts with HardLine.
	if preVal != nil {
		body = Cat(key, val)
	} else {
		body = Cats(key, spaceLit, val)
	}

	return Cats(append(append(before, body), after...)...)
}

// fieldRow splits a Field into a table Row for alignment. Doc
// comments are placed in [Row.DocComment] (before the key, not
// affecting column widths). Same-line trailing comments go into a
// separate cell for cross-row alignment. Position 1 comments are
// ignored - the parser does not generate them on Fields (see
// [converter.field]). Position 2 comments are deferred and applied
// to the value Doc after it is computed. Post-field block comments
// (Position >= 3, not same-line trailing, with Newline/NewSection
// RelPos) are returned separately so the caller can emit them as
// sibling comment blocks after the field, preserving their original
// vertical position.
func (c *converter) fieldRow(chain []*ast.Field) (Row, []*ast.CommentGroup) {
	// For braceless chains (x: y: z: val) the caller provides the
	// chain; the head field identifies this row, the leaf field
	// carries the value.
	head := chain[0]
	leaf := chain[len(chain)-1]

	slots := classifyComments(head)
	// Position 1 (slots.prefix) is not generated by the parser on
	// Fields - see [converter.field] for the rationale. Ignored here
	// too.
	var trailingCommentDoc Doc
	var trailingComments []*ast.CommentGroup
	hasComment := len(slots.suffix) > 0
	// slots.suffix (Position 2): between colon and value. valDoc isn't
	// computed yet (we need leaf.Value's type to know whether
	// attrs/trailing align in a chain table), so defer until later.

	// slots.trailing (Position >= posTrailingMin): a
	// Newline/NewSection RelPos means the comment is on its own line
	// below the field - emit it as a sibling Raw row. Anything else
	// (Blank, or no RelPos) is a same-line trailing `//` placed in the
	// trailingComment cell so it column-aligns across rows.
	for _, cg := range slots.trailing {
		hasComment = true
		if cg.Pos().IsNewline() {
			trailingComments = append(trailingComments, cg)
		} else {
			trailingCommentDoc = joinLines(trailingCommentDoc, c.commentGroup(cg))
		}
	}
	docComment := c.docCommentBlock(slots.doc, head.Pos().RelPos())

	// If the value is a | or & chain AND the chain carries any
	// trailing comments, hand the field's own trailing comment to
	// chainTableExpr so it column-aligns with the chain's arm
	// comments. Otherwise keep it as a separate cell in the field row
	// (so it aligns with simple fields' trailing comments).
	//
	// Attributes: for the plain (non-chain) path, attrs get their own
	// table cell (column 2) so they column-align across rows just like
	// trailing comments do. For the chain path the valDoc cell is itself
	// a multi-line table, so attrs are appended inline instead - there
	// is no well-defined column position for them in that case.
	var valDoc Doc
	var attrsDoc Doc
	bin, isChain := leaf.Value.(*ast.BinaryExpr)
	isChain = isChain && trailingCommentDoc != nil && (bin.Op == token.OR || bin.Op == token.AND)
	var binArms []chainArm
	binHasTrailing := false
	if isChain {
		binArms, binHasTrailing = flattenBinaryChain(bin)
	}
	if isChain && binHasTrailing {
		valDoc = appendAttrs(c.chainTableExpr(bin, binArms, trailingCommentDoc), leaf.Attrs)
		trailingCommentDoc = nil
	} else {
		valDoc = c.expr(leaf.Value)
		attrsDoc = attrsSpaced(leaf.Attrs)
	}

	// Now process Position=2 comments (between colon and value);
	// prepend them to valDoc.
	for _, cg := range slots.suffix {
		valDoc = Cats(c.commentSep(cg, c.commentGroup(cg)), valDoc)
	}

	// For chain rows (len(chain) > 1) whose leaf value is atomic,
	// build a merged alternative for cell 0: a tree of nested Groups
	// with valDoc inside the deepest Nest, woven through every label
	// of the chain. The renderer uses this when the segment as a
	// whole can't fit flat, so each split point's flat-fit check
	// includes the value and partial chain breaks land the value on
	// the chain's actual deepest line. When valDoc is complex
	// ([docGroup] / [docTable] / [docLineBreakBare] inside) we leave
	// mergedFirstCell nil so its own break point handles overflow.
	//
	// The loop below weaves the same fk into both the chain key
	// (without valDoc) and, when set, mergedFirstCell (with valDoc
	// innermost).
	var mergedFirstCell Doc
	if len(chain) > 1 && !valDoc.canBreak() {
		mergedFirstCell = valDoc
	}
	var key Doc
	for i := len(chain) - 1; i >= 0; i-- {
		fk := c.fieldKey(chain[i])
		if key == nil {
			key = fk
		} else {
			// Build the chain key as nested Groups (one per split point)
			// without the leaf's value, so cell 0 has a clean chain that
			// can align across sibling rows in a multi-row segment.
			key = Group(Cat(fk, Nest(Cat(lineBreakOrSpace, key))))
		}
		if mergedFirstCell != nil {
			mergedFirstCell = Group(Cat(fk, Nest(Cat(lineBreakOrSpace, mergedFirstCell))))
		}
	}

	// Column layout:
	//   [key, val]                     - neither attrs nor comment
	//   [key, val, attrs]              - attrs only
	//   [key, val, nil, trailing]      - comment only (reserves the
	//                                    attrs column so comment lands
	//                                    in the same column across
	//                                    attr-bearing rows)
	//   [key, val, attrs, trailing]    - both
	cells := []Doc{key, valDoc}
	if attrsDoc != nil || trailingCommentDoc != nil {
		cells = append(cells, attrsDoc)
		if trailingCommentDoc != nil {
			cells = append(cells, trailingCommentDoc)
		}
	}

	return Row{
		DocComment:      docComment,
		Cells:           cells,
		HasComment:      hasComment,
		AllowRowBreak:   true,
		MergedFirstCell: mergedFirstCell,
	}, trailingComments
}

// fieldKey builds the key portion of a field: label + alias +
// constraint + colon.
func (c *converter) fieldKey(f *ast.Field) Doc {
	key := c.label(f.Label)
	if f.Alias != nil {
		key = Cat(key, c.postfixAlias(f.Alias))
	}
	if f.Constraint == token.OPTION || f.Constraint == token.NOT {
		key = Cat(key, StringLit(f.Constraint.String()))
	}
	return Cat(key, colonLit)
}

// fieldValDoc builds the value portion of a field: value + attributes.
// When preVal is non-nil it is the leading-break content prepended to
// val, so val is rendered on its own line. For non-chain values the
// continuation lives inside a Nest so it indents relative to the key;
// for a braceless chain value (`a: b: c`) the chain elements stay at
// the outer indent and the Nest is omitted - the chain isn't a
// nested body, just continuation of the same field. Callers build
// preVal to start with a HardLine and to include any own-line
// Position=2 suffix comments at val's indent, each followed by a
// HardLine - the final HardLine bridges the last comment to val, and
// the placement round-trips cleanly: re-parsing such output attaches
// the comment as a doc comment of the value, and re-rendering
// produces the same layout. preVal == nil means no leading break:
// val is returned as-is and rendered on the key's line.
func (c *converter) fieldValDoc(f *ast.Field, preVal Doc) Doc {
	// If f itself carries attributes, the chain-collapse in
	// [converter.fieldValExpr] would move them past a closer that no
	// longer exists - `entries: {[string]: string} @attr` collapsed to
	// `entries: [string]: string @attr` re-binds @attr from entries
	// to the leaf, changing semantics. Take the regular c.expr path
	// instead, which goes through structLit and keeps the synthesised
	// braces so the trailing attr attaches to f.
	valExprFun := c.fieldValExpr
	if len(f.Attrs) > 0 {
		valExprFun = c.expr
	}
	val := appendAttrs(valExprFun(f.Value), f.Attrs)
	if preVal != nil {
		// A chain value continues the outer field, so its
		// continuation only indents when the chain itself was
		// authored multi-line (inner field carries Newline RelPos
		// via [valNeedsLeadingBreak]) or carries a doc comment.
		// An inline `//` comment after the colon forces preVal but
		// doesn't imply an indent: the chain body stays at the outer
		// indent. Other values (struct, list, expression) always get
		// the indenting Nest.
		if bracelessChainCollapsible(f.Value) &&
			!hasDocComment(f.Value) &&
			!valNeedsLeadingBreak(f.Value) {
			val = Cat(preVal, val)
		} else {
			val = Nest(Cat(preVal, val))
		}
	}
	return val
}

// fieldValExpr renders an expression that sits in the value slot of
// a Field. It is the same as [converter.expr] except for one
// special-case: a braceless StructLit with a single Field child is
// rendered as that inner Field directly (no synthesised braces),
// preserving the parsed chain shape `a: b: c: 1` in cases where
// [converter.declSlice]'s simpleFieldChain path could not absorb it
// (e.g. a non-collapsible chain, where intermediate fields have
// Newline RelPos or comments). The chain is only valid at this
// position - the surrounding Field's key provides the binding. At
// every other expression position (CallExpr argument, BinaryExpr
// operand, ListLit element, ...) the regular [converter.expr] path
// applies and the StructLit emits its synthesised braces.
//
// The collapse is refused when the inner Field carries side data
// that has no natural home in chain form:
//
//   - inner.Attrs: in chain `outer_key: inner_key: leaf: val
//     @attr`, the `@attr` syntactically attaches to the leaf, not to
//     inner. Collapsing would silently move the attribute and
//     potentially change semantics.
//   - inner doc / leading comments: in chain form, the inner Field
//     would render on the same line as outer_key, leaving its doc
//     comment squeezed between outer_key and inner_key. Keeping the
//     braces gives the doc its usual place above its host.
//   - StructLit-attached comments: a comment attached to the
//     synthesised StructLit itself (rare but possible from AST
//     builders) has no chain-form equivalent either.
//
// In all those cases we fall through to c.expr, which routes the
// StructLit through structLit and emits synthesised braces so the
// inner Field renders normally inside them.
func (c *converter) fieldValExpr(v ast.Expr) Doc {
	if !bracelessChainCollapsible(v) {
		return c.expr(v)
	}
	return c.declSlice(v.(*ast.StructLit).Elts)
}

// bracelessChainCollapsible reports whether v is a braceless
// StructLit that [converter.fieldValExpr] will render as a Field
// chain (no synthesised braces): exactly one inner Field, no
// attributes on the inner Field, and no comments on either the inner
// Field or the StructLit - the shapes where chain form has a
// syntactic home for everything attached.
func bracelessChainCollapsible(v ast.Expr) bool {
	sl, ok := v.(*ast.StructLit)
	if !ok || sl.Lbrace.IsValid() || len(sl.Elts) != 1 {
		return false
	}
	inner, ok := sl.Elts[0].(*ast.Field)
	if !ok {
		return false
	}
	return len(inner.Attrs) == 0 && len(ast.Comments(inner)) == 0 && len(ast.Comments(sl)) == 0
}

// valNeedsLeadingBreak reports whether a field's value must be
// rendered on its own line, indented relative to the key, rather than
// continued after "key: " on the same line. Two shapes qualify:
//
//   - the value is a braceless StructLit that [fieldValExpr] will
//     render as a chain and whose inner Field's label carries
//     Newline/NewSection RelPos (a chain whose first body decl was
//     written on its own line, e.g. x:\n y: 1). A braceless StructLit
//     that won't collapse - multi-element or carrying side data -
//     falls through to [converter.structLit] which synthesises
//     braces, and its body structure provides newlines on its own;
//   - the value itself carries Newline/NewSection RelPos (the user
//     wrote a non-struct value on a new line, e.g. x:\n 1). This
//     mirrors the row-break that fieldRow performs for over-wide
//     simple fields, so the formatter is idempotent on its own
//     output.
//
// For a braceless StructLit, [ast.StructLit.Pos] falls through to its
// first element, so v.Pos().IsNewline() would conflate the
// StructLit's own position with its first child's. The StructLit
// branch is therefore handled separately and only fires for
// collapsible chains.
func valNeedsLeadingBreak(v ast.Expr) bool {
	if sl, ok := v.(*ast.StructLit); ok && !sl.Lbrace.IsValid() {
		if !bracelessChainCollapsible(v) {
			return false
		}
		return sl.Elts[0].Pos().IsNewline()
	}
	return v.Pos().IsNewline()
}

// postfixAlias converts a PostfixAlias.
func (c *converter) postfixAlias(a *ast.PostfixAlias) Doc {
	// TODO: check this is the behaviour we really want re parens
	if a.Label == nil {
		return Cats(StringLit("~("), StringLit(a.Field.Name), rParenLit)
	}
	return Cats(StringLit("~("), StringLit(a.Label.Name), commaLit, StringLit(a.Field.Name), rParenLit)
}

// importDecl converts an ImportDecl. Comments attached to each
// ImportSpec are wrapped via withComments so that position-0 doc
// comments and position>0 trailing/after comments are preserved.
//
// All separators (opener break, inter-spec, closer break) are
// RelPos-driven. The parser supplies Newline RelPos on each spec
// line and on Rparen for the standard multi-line form, and
// [style.Config]'s B3 promotes Rparen to Newline ahead of
// rendering. A programmatic AST that carries no RelPos hints
// renders flat - the enclosing Group then picks compact
// `import ("a", "b")` when the line fits and breaks across lines
// otherwise.
func (c *converter) importDecl(x *ast.ImportDecl) Doc {
	if len(x.Specs) == 0 {
		return nil
	}
	if !x.Lparen.IsValid() && len(x.Specs) == 1 {
		// Single import without parens.
		s := x.Specs[0]
		body := Cats(StringLit("import "), c.importSpec(s))
		return c.withComments(s, body)
	}

	specs := make([]Doc, len(x.Specs))
	for i, s := range x.Specs {
		spec := c.withComments(s, c.importSpec(s))
		if i > 0 {
			// Between specs: comma+space flat, hard newline when
			// broken or when leadingRel demands it (a NewSection on a
			// doc comment becomes BlankLine).
			spec = Cat(relBreakOr(leadingRelPos(s), lineBreakOrComma), spec)
		}
		specs[i] = spec
	}

	body := Cats(specs...)
	openBreak := relBreakOr(leadingRelPos(x.Specs[0]), lineBreakOrEmpty)
	closeBreak := relBreakOr(x.Rparen.RelPos(), lineBreakOrEmpty)
	return Group(Cats(
		StringLit("import ("),
		Nest(Cat(openBreak, body)),
		closeBreak,
		rParenLit,
	))
}

// importSpec converts an ImportSpec.
func (c *converter) importSpec(s *ast.ImportSpec) Doc {
	if s.Name != nil {
		return Cats(StringLit(s.Name.Name), spaceLit, StringLit(s.Path.Value))
	}
	return StringLit(s.Path.Value)
}

var (
	lineBreakHard    = &docLineBreakHard{docBase: docBase{breaks: true}}
	lineBreakBare    = &docLineBreakBare{docBase: docBase{breaks: true}}
	lineBreakOrEmpty = LineBreakSoft("")
	lineBreakOrSpace = LineBreakSoft(" ")
	lineBreakOrComma = LineBreakSoft(", ")
	blankLine        = Cat(lineBreakBare, lineBreakHard)
	commaWhenBroken  = SwitchMode(commaLit, nil)
	lBracketLit      = StringLit("[")
	rBracketLit      = StringLit("]")
	lBraceLit        = StringLit("{")
	rBraceLit        = StringLit("}")
	lParenLit        = StringLit("(")
	rParenLit        = StringLit(")")
	spaceLit         = StringLit(" ")
	commaLit         = StringLit(",")
	commaSpaceLit    = StringLit(", ")
	periodLit        = StringLit(".")
	colonLit         = StringLit(":")
	equalsLit        = StringLit("=")
	equalsSpaceLit   = StringLit(" = ")
	bottomLit        = StringLit("_|_")
	ellipsisLit      = StringLit("...")
)

// SoftLineSpace is a Line that emits a space when flat.
func SoftLineSpace() Doc { return lineBreakOrSpace }

// SoftLineComma is a Line that emits ", " when flat.
func SoftLineComma() Doc { return lineBreakOrComma }

// HardLine returns a hard line break that always emits a newline.
// Any Group containing a HardLine is forced to break.
func HardLine() Doc { return lineBreakHard }

// BlankLine emits a bare newline followed by an indented newline,
// producing a truly blank line (no trailing whitespace) as a separator.
func BlankLine() Doc { return blankLine }

// TrailingComma emits a comma only in broken-mode.
func TrailingComma() Doc { return commaWhenBroken }
