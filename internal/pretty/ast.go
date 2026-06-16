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
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// File overview
//
// Here we translate CUE AST nodes into the [doc] algebra defined in
// doc.go. The translation entry point is [converter.node]; we recurse
// through the AST and produce a [doc] that render.go renders to bytes.
//
// # Two layout strategies
//
// CUE ASTs may or may not carry layout hints. The parser sets
// [token.Pos] RelPos values (NoSpace / Blank / Newline / NewSection /
// Elided) to encode the user's authored layout. Programmatic ASTs
// (built by code, or parser output with RelPos stripped) may have no
// RelPos values. We pick between two modes per-subtree:
//
//   - authored-mode: we respect the authored layout. We wrap the
//     converted [doc] in a [docInfiniteWidth] scope (see "finite vs
//     infinite width" in the package doc), so width-driven [docGroup]
//     breaks are suppressed and only hard line breaks produce
//     newlines.
//
//   - programmatic-mode: width-driven Wadler-Lindig. We wrap the
//     converted [doc] in a [docFiniteWidth] subtree so that, when the
//     subtree is itself nested inside a [docInfiniteWidth] scope, its
//     inner [docGroup]s still pick flat-vs-broken against the real
//     width budget rather than inheriting the outer unlimited budget.
//
// The mode only changes at "wrap-site" AST nodes: node types where we
// call [converter.maybeGroup]. See [wrapEligibility] for the list.
// [converter.analyse] sets the [isAuthored] flag on the smallest
// wrap-site AST node that covers each RelPos cluster, so a RelPos hint
// in an otherwise-programmatic AST does *not* tip the whole file into
// authored-mode. A programmatic pocket nested inside a
// [docInfiniteWidth] scope (a wrap-eligible subtree with no RelPos
// descendants) gets width-driven layout via the [docFiniteWidth]
// boundary.
//
// These modes are orthogonal to flat-mode vs broken-mode.
//
// # Tables
//
// We use [docTable] (defined in doc.go) for column-aligned layouts:
// struct fields aligned by ":", chain arms aligned by trailing
// comments, list elements by their value column, and so on. See the
// package doc for [docTable] semantics and render.go for the
// segmentation algorithm.
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
//   - wrap-site: an AST node type where we call [converter.maybeGroup].
//     Each wrap-site is a candidate for the [docInfiniteWidth] wrap;
//     isAuthored selects which actually fire.
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
// In scope: rendering ASTs that the CUE parser can produce. We hold
// the invariant "if t is an AST and b = format(t), then b ==
// format(parse(b))". Programmatic ASTs that match this shape (no
// RelPos hints, but otherwise parser-shaped) are also fully
// supported.
//
// Out of scope: rendering AST shapes the parser cannot produce.
// Consider an [ast.Field] with a Position=1 comment (between label and
// colon): it has no source layout, since `//` runs to end of line and
// the parser doesn't allow a newline between label and colon, so this
// construction can only be built programmatically. We render such ASTs
// with a best-effort placement that may not round-trip; preserving
// such positional intent is not a goal.

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
	// isAuthored marks a node which is itself eligible for wrapping,
	// and carries any RelPos, or has a descendent that carries any
	// RelPos. Further, there is no such node on the path between this
	// node and the RelPos node that is eligible for wrapping. I.e. we
	// set isAuthored on the eligible node nearest a RelPos-containing
	// subtree, and not any higher - in contrast to relPosInChildren,
	// which propagates up the tree to the very root once set.
	isAuthored
	// endsWithOwnLineComment marks a node whose rendered output ends
	// with a `//` comment on its own line (Slash RelPos != Blank and
	// cg.Line == false). We set it when the node has such a trailing
	// comment attached directly, and propagate it up only through the
	// rightmost-rendered child of non-bracketed nodes (Field,
	// BinaryExpr, UnaryExpr, SelectorExpr, LetClause, EmbedDecl,
	// PostfixExpr). Bracketed nodes (StructLit, ListLit, CallExpr,
	// IndexExpr, SliceExpr, ParenExpr, ImportDecl) end with their
	// closing bracket, so descendants don't propagate through them.
	endsWithOwnLineComment
	// endsWithLineComment marks a node whose rendered output ends with a
	// `//` comment, whether or not that comment sits on the node's own
	// line (cg.Line). CUE has only `//` line comments, which run to
	// end-of-line, so such a tail swallows any same-line token emitted
	// after the node. This is broader than endsWithOwnLineComment (which
	// we restrict to own-line comments for the declaration-separator
	// promotion in declSep); we use it to force a break before a
	// comprehension body whose preceding clause ends with a trailing
	// comment. Propagated up only through the rightmost-rendered child
	// of non-bracketed nodes, mirroring endsWithOwnLineComment.
	endsWithLineComment
)

// converter translates a CUE AST into the Doc algebra.
type converter struct {
	nodeFlags map[ast.Node]nodeFlag

	// errs accumulates formatting errors encountered while converting
	// the AST (e.g. an invalid identifier in expression position).
	// [Config.Node] returns them to the caller. It is nil when no error
	// occurred.
	errs errors.Error

	// subscript is the syntactic nesting depth inside index/slice
	// subscripts. [converter.binaryExpr] reads it to compact operator
	// spacing: a binary expression at the top of a value renders with
	// normal spacing (`1 + 2`), but the same expression inside `[...]`
	// renders compact (`1+2`). We bump it in indexExpr/sliceExpr around
	// their contents and reset it to 0 at a struct/list (block)
	// boundary, whose fields/elements start fresh at the top level. This
	// is a layout property independent of RelPos, so it lives here
	// rather than in the style pre-pass.
	subscript int
}

// errf records a formatting error at pos.
func (c *converter) errf(pos token.Pos, format string, args ...any) {
	c.errs = errors.Append(c.errs, errors.Newf(pos, format, args...))
}

// node converts an AST node to a Doc.
func (c *converter) node(n ast.Node) doc {
	c.analyse(n)
	switch n := n.(type) {
	case *ast.File:
		return cat(c.maybeGroup(n, c.file(n)), lineBreakHard)
	case ast.Expr:
		return c.expr(n)
	case ast.Decl:
		return c.decl(n)
	default:
		return nil
	}
}

// analyse walks root to populate c.nodeFlags. We OR the
// strict-descendants flags (newlineInChildren, relPosInChildren) onto
// every ancestor up the current stack, short-circuiting at the first
// ancestor that already carries every bit we're propagating, so total
// work stays amortised O(N).
//
// We set isAuthored on the smallest wrap-eligible covering subtree for
// each connected RelPos cluster: a wrap-eligible node that sees any
// uncovered RelPos sets isAuthored on itself and suppresses
// propagation to its parent.
//
// A wrap-eligible node only "covers" RelPos hints when its own
// structural tokens are authored ([wrapEligibility] reports authored
// == true). A wrap-eligible node whose structure was synthesised
// programmatically (e.g. a StructLit with Lbrace.IsValid() == false,
// or a File whose decls all lack RelPos hints) acts as a pass-through:
// RelPos hints in its subtree bubble past it to the nearest
// wrap-eligible ancestor with authored structure, and the synthesised
// wrap stays under [finiteWidth].
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
		lastChildEndsLineComment     bool
	}
	var stack stack[frame]

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
					// Propagate the flags up the stack.
					f := &stack[i]
					if missing := ancestorsFlags &^ f.flags; missing == 0 {
						// Once a frame already has the flags we want, we
						// know all its ancestors do too, so there is
						// nothing further to do.
						break
					}
					f.flags |= ancestorsFlags
				}
			}

			stack.push(frame{hasRelPos: hasRelPos})
			return true
		},
		func(n ast.Node) {
			f := stack.pop()
			var parent *frame
			if len(stack) > 0 {
				parent = &stack[len(stack)-1]
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

			endsLineComment := (f.lastChildEndsLineComment && !isBracketed(n)) || nodeEndsWithLineComment(n)
			if endsLineComment {
				flags |= endsWithLineComment
			}
			if parent != nil {
				parent.lastChildEndsLineComment = endsLineComment
			}

			if flags != 0 {
				// only write if flags differs from the zero-value
				nodeFlags[n] = flags
			}
		})
}

// file renders a [ast.File].
func (c *converter) file(f *ast.File) doc {
	// We partition f.Decls into a header (longest prefix of Package /
	// ImportDecl / CommentGroup decls) and a body (everything after).
	// The CUE parser guarantees Package occupies decl 0 (if present)
	// and every ImportDecl precedes every non-import non-comment decl.
	// Header decls are not field-value pairs and never align in a
	// table, so isolating them keeps declSlice's invariant simple: it
	// only ever sees the body's mix of fields and field-adjacent decls.

	preamble := f.Preamble()
	header := c.fileHeader(preamble)
	body := c.declSlice(f.Decls[len(preamble):])

	var leading, trailing []*ast.CommentGroup
	for _, cg := range ast.Comments(f) {
		if cg.Position == PosDoc {
			leading = append(leading, cg)
		} else {
			trailing = append(trailing, cg)
		}
	}

	// Separator between the last leading comment and the body. We use
	// LeadingRelPos so a NewSection on the first decl's own doc comment
	// (the first visible token after the file-level leading comments)
	// is honoured - otherwise a blank line between a file-level comment
	// and the next decl's doc comment is silently dropped.
	var firstDeclSep doc = lineBreakHard
	if len(f.Decls) > 0 {
		firstDeclSep = relBreakOr(LeadingRelPos(f.Decls[0]), lineBreakHard)
	}

	parts := make([]doc, 0, len(leading)*2+3+len(trailing)*2)
	lastIdx := len(leading) - 1
	for i, cg := range leading {
		parts = append(parts, c.commentGroup(cg))
		if i == lastIdx {
			parts = append(parts, firstDeclSep)
		} else {
			parts = append(parts, relBreakOr(leading[i+1].Pos().RelPos(), lineBreakHard))
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
			parts = append(parts, c.declSep(f.Decls[len(preamble)], f.Decls[len(preamble)-1]))
		}
	}
	if body != nil {
		parts = append(parts, body)
	}
	for _, cg := range trailing {
		parts = append(parts, c.commentSep(cg, c.commentGroup(cg)), switchMode(nil, lineBreakHard))
	}
	return cats(parts...)
}

// fileHeader renders the file's header decls (Package, ImportDecl,
// CommentGroup, Attribute). We emit each decl as a standalone Doc with
// relpos-driven separators between them; alignment never applies here
// because header decls are not field-value pairs. Returns nil for
// empty input.
func (c *converter) fileHeader(decls []ast.Decl) doc {
	if len(decls) == 0 {
		return nil
	}
	docs := make([]doc, 0, 2*len(decls))
	var prev ast.Decl
	for _, decl := range decls {
		var sep doc
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
	return cats(docs...)
}

// declSlice joins a slice of Decls with RelPos-driven separators.
func (c *converter) declSlice(decls []ast.Decl) doc {
	if len(decls) == 0 {
		return nil
	}

	docs := make([]doc, 0, len(decls))
	tableRows := make([]row, 0, len(decls))
	hasAligned := false // true if tableRows contains at least one aligned row
	curChainLen := 0    // chain length of the most recent aligned row (0 if none)

	// Mixed-layout uniformity: if any inter-decl separator is a hard
	// break (Newline / NewSection RelPos), we lay every decl out on its
	// own line. We promote comma-separated runs to HardLine so the
	// struct doesn't render with some fields on their own line and
	// others packed onto a shared line - that mix produces awkward
	// table alignment (column-padded keys followed by `, key:` on the
	// same row).
	multiLine := multilineDecls(decls)

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}

		if hasAligned {
			docs = append(docs, tableRows[0].sep, table(tableRows))
		} else {
			// No aligned rows - just emit raw rows directly.
			for _, row := range tableRows {
				docs = append(docs, row.sep, row.raw)
			}
		}
		tableRows = tableRows[len(tableRows):]
		hasAligned = false
		curChainLen = 0
	}

	var prev ast.Decl
	for _, decl := range decls {
		var sep doc
		if prev != nil {
			sep = c.declSep(decl, prev)
			if multiLine && sep == lineBreakOrComma {
				sep = lineBreakHard
			}
		}
		prev = decl

		if _, ok := decl.(*ast.CommentGroup); ok {
			flushTable()
			docs = append(docs, sep, c.decl(decl))
			continue
		}

		// A blank line separator breaks the table - we don't align
		// across visual section boundaries.
		if sep == blankLine {
			flushTable()
		}

		f, isField := decl.(*ast.Field)
		var chain []*ast.Field
		if isField {
			chain = c.simpleFieldChain(f)
		}
		if chain != nil {
			chainLen := len(chain)
			rows, postComments := c.fieldRow(chain)
			row0 := &rows[0]
			row0.sep = sep
			// A doc comment visually separates fields, so we flush the
			// table to prevent alignment across the comment.
			if row0.docComment != nil && len(tableRows) > 0 {
				flushTable()
			}
			// A change in the composite-key chain length (e.g. from
			// `a: b: 1` to `c: d: e: 2`) gives the first column a
			// different shape, so values would no longer line up
			// meaningfully. We flush the table to start a fresh
			// alignment group.
			if curChainLen != 0 && curChainLen != chainLen {
				flushTable()
			}
			tableRows = append(tableRows, rows...)
			hasAligned = true
			curChainLen = chainLen
			// Post-field block comments were attached to the field by
			// the parser but are visually separate from its row. We emit
			// them as sibling blocks after flushing the table so they
			// keep their original position instead of being folded into
			// the row's value cell.
			if len(postComments) > 0 {
				flushTable()
				for _, cg := range postComments {
					docs = append(docs,
						relBreakOr(cg.Pos().RelPos(), lineBreakHard),
						c.commentGroup(cg))
				}
			}
			continue
		}

		// Anything that isn't a simple field-value row breaks
		// alignment: complex fields (multi-line value, doc comment on
		// value, value on its own line, non-collapsible chain) and
		// every non-field, non-CommentGroup decl (LetClause, EmbedDecl,
		// Comprehension, Alias, Attribute, Ellipsis, BadDecl). We flush
		// the table and emit the decl standalone. Centralising this rule
		// here keeps table-flush concerns in one place rather than split
		// between construction-time decisions and render-time
		// segmentation.
		//
		// Package and ImportDecl never reach this loop: file()
		// partitions them off into the header before declSlice runs,
		// and a struct literal cannot contain either of them
		// syntactically.
		flushTable()
		doc := c.decl(decl)
		// field() (via decl()) handles all of the field's comments
		// internally, so we don't double-wrap with withComments.
		if !isField {
			doc = c.withComments(decl, doc)
		}
		docs = append(docs, sep, doc)
	}
	flushTable()

	return cats(docs...)
}

// simpleFieldChain returns the field chain (x: y: z: val -> [x,y,z])
// when f is eligible for table alignment, or nil otherwise. A chain
// is eligible when it is a braceless single-element StructLit chain
// whose leaf value:
//
//   - exists and has no doc comment,
//   - has no Newline/NewSection RelPos, and
//   - has no Position=2 comment.
//
// A StructLit or ListLit value still qualifies: whether it renders
// without newlines is decided at render time by the table's row
// partitioning.
func (c *converter) simpleFieldChain(f *ast.Field) []*ast.Field {
	chain, collapsible := unchainField(f)
	if len(chain) > 1 && !collapsible {
		// A non-collapsible chain renders as `f: {braced value}` - the
		// inner field stays inside the braces rather than chaining onto
		// f's line. So for alignment purposes it is a single field f
		// whose value is that struct; we use the single-element chain
		// instead of returning nil, so a single-line struct value still
		// aligns in the table (a multi-line value breaks the segment via
		// the leaf checks / table machinery below).
		chain = chain[:1]
	}
	leaf := chain[len(chain)-1]
	if leaf.Value == nil ||
		leaf.Value.Pos().IsNewline() ||
		HasDocComment(leaf.Value) ||
		hasCommentAt(leaf, PosSuffix) {
		return nil
	}
	return chain
}

// unchainField walks a braceless field chain (x: y: z: val is a
// chain of three Fields) and returns the sequence of Fields from the
// head to the leaf. collapsible reports whether the chain can be
// safely rendered as a single composite key + leaf value, which holds
// only when:
//   - every intermediate StructLit and every Field after the head is
//     comment-free, and
//   - no Field after the head carries Newline/NewSection RelPos, and
//   - the head has no Position=1/2 comments, and
//   - no non-leaf Field carries attributes.
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
	if hasCommentAt(f, PosPrefix) || hasCommentAt(f, PosSuffix) {
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
// Two upgrades fire on top of the authored RelPos.
//
// First, if prev's rendered output ends with an own-line `//` comment,
// we promote the separator to NewSection when prev is a Definition
// (#Foo) or any non-field, non-comment decl. Inside an [infiniteWidth]
// wrap a lineBreakComma would otherwise be rewritten to stringLit(", ")
// and the `//` would absorb the next decl. Promoting also keeps the
// parse/format cycle idempotent when the comment migrates from prev's
// trailing slot into the next decl's doc on reparse.
//
// Second, if prev's rendered output ends with any `//` comment - own
// line or same line - we force the separator to at least Newline,
// since a CUE line comment runs to end-of-line and would otherwise
// swallow an inline (`, `) separator and the following decl.
func (c *converter) declSep(d ast.Decl, prev ast.Decl) doc {
	rel := LeadingRelPos(d)

	if prev == nil {
		return relBreakOr(rel, lineBreakOrComma)
	}

	const ownLineMask = relPosInChildren | endsWithOwnLineComment
	prevOwnLineComment := c.nodeFlags[prev]&ownLineMask == ownLineMask

	if rel < token.NewSection && prevOwnLineComment {
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

	if rel < token.Newline && c.nodeFlags[prev]&endsWithLineComment != 0 {
		rel = token.Newline
	}

	return relBreakOr(rel, lineBreakOrComma)
}

// authored reports whether n's subtree carries any RelPos - that is,
// whether the subtree came from the parser (or had RelPos info added
// programmatically) rather than being built RelPos-free. The
// [infiniteWidth] wrap is driven by the [isAuthored] flag, not this
// function - see [converter.maybeGroup].
func (c *converter) authored(n ast.Node) bool {
	return c.nodeFlags[n]&relPosInChildren != 0
}

// shouldHug reports whether a bracketed construct with a single
// child should wrap its open/close tokens directly around the
// child's Doc, bypassing the usual [docGroup]+[docNest] layout. The hug
// defeats the cascade that would otherwise double-indent a child
// that is already rendering with forced breaks: produces
// "{a: {...}}" rather than "{\n    a: {...}\n}". It applies when:
//   - the child has no explicit Newline on its own Pos;
//   - some Newline/NewSection RelPos exists in the child's subtree,
//     so the child will render with forced breaks anyway;
//   - the child has no comments attached to itself: a doc comment
//     would land right after the parent's opener (no space), and a
//     trailing line comment would swallow the parent's closer.
//     Comments deep in descendants are safe - they live inside their
//     own brackets and never reach the outer parent's boundary.
func (c *converter) shouldHug(child ast.Node) bool {
	return child != nil &&
		!child.Pos().IsNewline() &&
		len(ast.Comments(child)) == 0 &&
		c.hasNewlineInSubtree(child)
}

// hasNewlineInSubtree reports whether any node in the subtree rooted
// at n carries a Newline or NewSection RelPos. Returns false for nil n.
func (c *converter) hasNewlineInSubtree(n ast.Node) bool {
	if n == nil {
		return false
	}
	// NB: because this is the only use of newlineInChildren, it's
	// tempting to change [converter.analyse] so that it sets this flag
	// on the current node rather than just ancestors (we could then drop
	// the || here). But that costs us: it grows the nodeFlags map and
	// forces a map lookup every time. Testing n.Pos().IsNewline() here
	// is measurably faster.
	return n.Pos().IsNewline() || c.nodeFlags[n]&newlineInChildren != 0
}

// renderCommentChain renders cgs joined by per-rel breaks: HardLine
// by default, upgraded to BlankLine when the next group's RelPos is
// NewSection, so we preserve blank lines the user wrote between comment
// groups. We append no trailing separator. Returns nil for an empty
// input.
func (c *converter) renderCommentChain(cgs []*ast.CommentGroup) doc {
	if len(cgs) == 0 {
		return nil
	}
	parts := make([]doc, 0, 2*len(cgs)-1)
	for i, cg := range cgs {
		if i > 0 {
			parts = append(parts, relBreakOr(cg.Pos().RelPos(), lineBreakHard))
		}
		parts = append(parts, c.commentGroup(cg))
	}
	return cats(parts...)
}

// docCommentBlock renders a sequence of Position=0 comment groups
// followed by a trailing separator chosen from trailingRel: HardLine
// by default; BlankLine when trailingRel is NewSection. The host
// node's RelPos drives trailingRel so a Position=0 leading comment
// separated from its host by a blank line keeps that blank line on the
// right side.
//
// A true doc comment ([ast.CommentGroup.Doc] is true on the last group
// in cgs) is tight to its host, so we clamp trailingRel at Newline: a
// NewSection there would wrongly insert a blank line between the doc
// comment and the host it documents.
//
// Returns nil for an empty input.
func (c *converter) docCommentBlock(cgs []*ast.CommentGroup, trailingRel token.RelPos) doc {
	chain := c.renderCommentChain(cgs)
	if chain == nil {
		return nil
	}
	if len(cgs) > 0 && cgs[len(cgs)-1].Doc && trailingRel > token.Newline {
		trailingRel = token.Newline
	}
	return cat(chain, relBreakOr(trailingRel, lineBreakHard))
}

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
// The constants (and the comment helpers [FirstCommentAt],
// [HasDocComment], and [LeadingRelPos]) are exported for
// [cuelang.org/go/internal/pretty/style], whose AST rewrites must
// classify comments and leading positions exactly as this converter
// does.
const (
	PosDoc         int8 = 0 // before the node's first token
	PosPrefix      int8 = 1 // inside, just after the opener
	PosSuffix      int8 = 2 // inside, just before the closer
	PosTrailingMin int8 = 3 // first position counted as trailing
)

type commentSlots struct {
	doc      []*ast.CommentGroup // Position==PosDoc
	prefix   []*ast.CommentGroup // Position==PosPrefix
	suffix   []*ast.CommentGroup // Position==PosSuffix
	trailing []*ast.CommentGroup // Position>=PosTrailingMin
}

// any reports whether s has at least one comment in any slot.
func (s commentSlots) any() bool {
	return len(s.doc)+len(s.prefix)+len(s.suffix)+len(s.trailing) > 0
}

// hasInterior reports whether s carries any prefix (Position=1) or
// suffix (Position=2) comments - those that live inside a bracketed
// node's `{...}` / `[...]` / `(...)` area.
func (s commentSlots) hasInterior() bool {
	return len(s.prefix)+len(s.suffix) > 0
}

// all returns the comments from every slot in slot order (doc,
// prefix, suffix, trailing).
func (s commentSlots) all() []*ast.CommentGroup {
	out := make([]*ast.CommentGroup, 0, len(s.doc)+len(s.prefix)+len(s.suffix)+len(s.trailing))
	out = append(out, s.doc...)
	out = append(out, s.prefix...)
	out = append(out, s.suffix...)
	out = append(out, s.trailing...)
	return out
}

// wrapInteriorComments places prefix comments before inner, and
// suffix comments after inner, each on its own line. We separate
// consecutive comments within a block by HardLine (upgraded to
// BlankLine when the next group's RelPos is NewSection, preserving
// authored blank lines), and bridge between the comment block(s) and
// the inner body with a HardLine. When inner is nil we emit only the
// comments, joined by a HardLine between the prefix and suffix blocks
// if both are present.
func (c *converter) wrapInteriorComments(inner doc, prefix, suffix []*ast.CommentGroup) doc {
	if len(prefix) == 0 && len(suffix) == 0 {
		return inner
	}
	parts := make([]doc, 0, 5)
	if len(prefix) > 0 {
		parts = append(parts, c.renderCommentChain(prefix))
	}
	switch {
	case inner != nil:
		if len(prefix) > 0 {
			parts = append(parts, lineBreakHard)
		}
		parts = append(parts, inner)
		if len(suffix) > 0 {
			parts = append(parts, lineBreakHard)
		}
	case len(prefix) > 0 && len(suffix) > 0:
		// inner is nil but both blocks exist - join them.
		parts = append(parts, lineBreakHard)
	}
	if len(suffix) > 0 {
		parts = append(parts, c.renderCommentChain(suffix))
	}
	return cats(parts...)
}

// withComments wraps a Doc with its node's attached comments. The
// separator between the last doc comment and the body honours the
// node's RelPos (NewSection -> blank line).
func (c *converter) withComments(n ast.Node, body doc) doc {
	return c.withCommentsSlots(n, body, classifyComments(n))
}

// withCommentsSlots is like [converter.withComments] but lets the
// caller supply pre-computed (and possibly filtered) slots.
func (c *converter) withCommentsSlots(n ast.Node, body doc, slots commentSlots) doc {
	if !slots.any() {
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

	var after []doc
	if len(trailing) > 0 {
		after = make([]doc, 0, len(trailing)+1)
		// Trailing // comment: place it, then force the enclosing group
		// to break so the comment doesn't swallow closing
		// brackets/braces in flat-mode.
		for _, cg := range trailing {
			cgDoc := c.commentGroup(cg)
			sep := c.commentSep(cg, cgDoc)
			after = append(after, sep)
		}
		// switchMode(nil, lineBreakHard) is invisible in broken-mode and
		// prevents flat rendering.
		after = append(after, switchMode(nil, lineBreakHard))
	}

	parts := make([]doc, 0, 2+len(after))
	if doc := c.docCommentBlock(slots.doc, n.Pos().RelPos()); doc != nil {
		parts = append(parts, doc)
	}
	parts = append(parts, body)
	parts = append(parts, after...)
	return cats(parts...)
}

// commentGroup converts a CommentGroup to a Doc.
func (c *converter) commentGroup(cg *ast.CommentGroup) doc {
	if len(cg.List) == 0 {
		return nil
	}
	docs := make([]doc, 0, len(cg.List)*2-1)
	for i, comment := range cg.List {
		if i > 0 {
			docs = append(docs, lineBreakHard)
		}
		docs = append(docs, stringLit(comment.Text))
	}
	return cats(docs...)
}

// commentSep returns a Doc that wraps comment cd with the appropriate
// separation based on its leading RelPos.
//
// cg.Line=true means the comment sits on the same line as the
// preceding token; the Slash RelPos there is only meaningful as
// intra-line spacing. The Line=true case is normalised to Blank so
// these comments flow through the same "same-line trailing" path
// uniformly.
//
// Mapping (for cg.Line=false):
//   - rel == Blank: same-line trailing - emit " // ...".
//   - rel == Newline: own line, single break.
//   - rel == NewSection: own line, blank line before.
//   - any other rel (NoRelPos, NoSpace, Elided): fall back to a blank
//     line. The comment must not be inlined - CUE's `//` runs to
//     end-of-line, so squashing onto a shared line would absorb
//     subsequent tokens.
func (c *converter) commentSep(cg *ast.CommentGroup, cd doc) doc {
	rel := cg.Pos().RelPos()
	if cg.Line {
		rel = token.Blank
	}
	var sep doc
	switch rel {
	case token.Blank:
		sep = spaceLit
	case token.Newline:
		sep = lineBreakHard
	default:
		// NoRelPos, NoSpace, Elided, NewSection: blank line.
		sep = blankLine
	}
	return cat(sep, cd)
}

// maybeGroup wraps body in the layout primitive selected for n's
// subtree: [infiniteWidth] when [isAuthored] is set on n (so only hard
// line breaks survive and width-driven newlines are suppressed),
// otherwise [finiteWidth] for width-driven Wadler-Lindig layout. See
// those functions for the underlying mechanisms.
//
// Panics if n is not one of the wrap-eligible types reported by
// [wrapEligibility].
func (c *converter) maybeGroup(n ast.Node, body doc) doc {
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

// expr converts an expression node to a Doc, including any comments.
func (c *converter) expr(x ast.Expr) doc {
	if x == nil {
		return nil
	}
	return c.withComments(x, c.exprCore(x))
}

// exprCore converts an expression without handling comments on it.
func (c *converter) exprCore(x ast.Expr) doc {
	if x == nil {
		return nil
	}
	switch x := x.(type) {
	case *ast.Ident:
		// An identifier in expression position must be a valid
		// identifier; unlike a label (see [converter.label]) it cannot
		// be quoted into a string. Record an error and emit the name as
		// a best-effort rendering.
		if !ast.IsValidIdent(x.Name) {
			c.errf(x.Pos(), "invalid identifier %q", x.Name)
		}
		return stringLit(x.Name)

	case *ast.BasicLit:
		return c.basicLit(x)

	case *ast.BottomLit:
		return bottomLit

	case *ast.BadExpr:
		// A bad node has no valid syntax; render it as bottom (`_|_`),
		// which is valid CUE.
		return bottomLit

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
		// The receiver is a primary expression: `.sel` binds tighter
		// than any binary operator, so a binary receiver needs parens
		// to keep its grouping (e.g. `(a & b).c`, not `a & b.c`).
		return cats(wrapForPrecedence(c.expr(x.X), x.X, token.HighestPrec), periodLit, c.label(x.Sel))

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
		return cats(stringLit(x.Ident.Name), equalsLit, c.expr(x.Expr))

	default:
		return stringLit("/* unknown expr */")
	}
}

// label converts a Label node to a Doc.
func (c *converter) label(l ast.Label) doc {
	switch x := l.(type) {
	case *ast.Ident:
		// Escape an identifier label with invalid characters (e.g. an
		// empty name) as a quoted string label. This only arises in
		// programmatic ASTs; the parser never produces an invalid ident
		// label.
		if !ast.IsValidIdent(x.Name) {
			return stringLit(literal.Label.Quote(x.Name))
		}
		return stringLit(x.Name)
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
				return stringLit(literal.Label.Quote(u))
			}
		}
		return c.basicLit(x)
	case *ast.Interpolation:
		return c.interpolation(x)
	case *ast.ListLit:
		if len(x.Elts) == 1 {
			return cats(lBracketLit, c.expr(x.Elts[0]), rBracketLit)
		}
		return c.listLit(x)
	case *ast.ParenExpr:
		return cats(lParenLit, c.expr(x.X), rParenLit)
	case *ast.Alias:
		return cats(stringLit(x.Ident.Name), equalsLit, c.expr(x.Expr))
	default:
		return c.expr(l.(ast.Expr))
	}
}

// basicLit converts a BasicLit to a Doc.
func (c *converter) basicLit(x *ast.BasicLit) doc {
	value := normaliseNumericLit(x.Kind, x.Value)

	// A multi-line string/bytes literal carries its body indentation
	// in the literal text itself (the whitespace before the closing
	// quote, repeated on each body line). That embedded indent
	// reflects whatever the source or producer happened to use -
	// parsed source preserves the author's columns, while a producer
	// such as the YAML decoder or [internal/core/export] embeds a
	// fixed guess from its own nesting that need not match the
	// literal's eventual render position. Rather than trust it, we
	// always re-indent the body to the actual render depth. This is
	// idempotent for correctly-indented source: re-indenting to the
	// level it already sits at is a no-op.
	if x.Kind == token.STRING && strings.IndexByte(value, '\n') >= 0 {
		if d := reindentMultilineString(value); d != nil {
			return d
		}
	}

	lines := strings.Split(value, "\n")
	parts := make([]doc, 0, len(lines)*2-1)
	// We have to intersperse lineBreakBare directly here. Using
	// sep(lineBreakBare, parts) wouldn't work because some parts could
	// be nil (stringLit("") gives nil) and sep skips over nil parts.
	for i, line := range lines {
		if i > 0 {
			parts = append(parts, lineBreakBare)
		}
		parts = append(parts, stringLit(line))
	}
	return cats(parts...)
}

// normaliseNumericLit rewrites Go-style numeric literals that the
// CUE parser would reject (e.g. `0755` for octal, a trailing dot like
// `5.`, or uppercase `E` for the exponent that may collide with
// future use of `E` as a SI exa multiplier) into valid CUE.
func normaliseNumericLit(kind token.Token, data string) string {
	switch kind {
	case token.INT:
		if len(data) > 1 && data[0] == '0' && data[1] >= '0' && data[1] <= '9' {
			data = "0o" + data[1:]
			break
		}
		fallthrough
	case token.FLOAT:
		switch p := strings.IndexByte(data, '.'); {
		case p < 0:
		case p == 0:
			data = "0" + data
		case p == len(data)-1:
			data += "0"
		case data[p+1] < '0' || data[p+1] > '9':
			// `.` immediately followed by a non-digit, i.e. an exponent
			// (3.e100): insert a `0` so the fractional part is explicit
			// (3.0e100).
			data = data[:p+1] + "0" + data[p+1:]
		}
	}
	// Lowercase a decimal exponent `E` to `e` for both INT and FLOAT
	// literals: CUE reserves uppercase `E` as a potential Exa SI
	// multiplier, so the formatter always writes the exponent
	// lowercase. Guarded on a non-trailing `E` so a hex literal whose
	// last digit is `E` (`0x1E`) is left alone.
	if kind == token.INT || kind == token.FLOAT {
		if p := strings.IndexByte(data, 'E'); p != -1 && p < len(data)-1 {
			data = strings.ToLower(data)
		}
	}
	return data
}

// bracketedLayout describes the shared decoration of struct / list /
// call literal bodies - the `<openPrefix><open><inner><close>` shape
// with hug, shareIndent, openBreak, closeBreak, and maybeGroup
// rules. [converter.computeBracketedPolicy] derives a
// [bracketedPolicy] from it and [converter.applyBracketed] assembles
// the final Doc.
type bracketedLayout struct {
	node       ast.Node     // for maybeGroup + relPosInChildren lookup
	openPrefix doc          // emitted before open (e.g. CallExpr's fun); nil otherwise
	open       doc          // bracket open token (lBrace / lBracket / lParen)
	close      doc          // bracket close token
	openerRel  token.RelPos // RelPos of the opening token (used for openBreak when body is empty)
	closerRel  token.RelPos // RelPos of the closing token

	firstElem ast.Node // first element (nil if body is empty); EmbedDecl-unwrapped at the call site if needed
	lastElem  ast.Node // last element (nil if body is empty)
	numElems  int

	hasInterior   bool // PosPrefix/PosSuffix interior comments on the bracket
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

	inner doc // body content; interior comments already prepended/appended for struct/list
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
	openBreak         doc
	closeBreak        doc

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
// inside the braces; doc and trailing comments on the StructLit itself
// are wrapped around the result.
func (c *converter) structLit(x *ast.StructLit) doc {
	// A struct body is a block boundary: its fields start fresh at the
	// top level, so the subscript depth resets (only relevant when
	// this struct appears inside an index/slice subscript).
	if c.subscript != 0 {
		saved := c.subscript
		c.subscript = 0
		defer func() { c.subscript = saved }()
	}
	var firstElem, lastElem ast.Node
	if len(x.Elts) > 0 {
		firstElem = x.Elts[0]
		lastElem = x.Elts[len(x.Elts)-1]
	}
	elems := x.Elts

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
	hasInterior := slots.hasInterior()

	switch {
	case len(elems) == 0 && !hasInterior:
		return stringLit("{}")
	case len(elems) == 1 && !hasInterior && !nodeType[*ast.Field](elems[0]) && c.shouldHug(elems[0]):
		// A struct hugs only when its single element renders the value
		// directly after the brace - an embed (`{ {...} }`). A *Field*
		// (`field: {...}`) would wedge the label between the braces
		// (`{field: {`), so it falls through to the broken layout
		// below.
		return cats(lBraceLit, c.decl(elems[0]), rBraceLit)
	}

	inner := c.wrapInteriorComments(c.declSlice(elems), slots.prefix, slots.suffix)

	layout := bracketedLayout{
		node:          x,
		open:          lBraceLit,
		close:         rBraceLit,
		openerRel:     x.Lbrace.RelPos(),
		closerRel:     x.Rbrace.RelPos(),
		firstElem:     firstElem,
		lastElem:      lastElem,
		numElems:      len(elems),
		hasInterior:   hasInterior,
		anyDoc:        anyHasDocComment(elems),
		anyPost:       anyHasPostComment(elems),
		sameLineOpen:  hasSameLineOpener(c, elems),
		noElemNewline: noElemHasNewline(elems),
		lineHeader:    hasLineLeadingComment(slots, firstElem),
		inner:         inner,
	}
	return c.applyBracketed(layout, c.computeBracketedPolicy(layout))
}

// listLit converts a ListLit. As with structLit, interior comments
// attached directly to the ListLit are rendered inside the brackets.
func (c *converter) listLit(x *ast.ListLit) doc {
	// A list body is a block boundary: its elements start fresh at the
	// top level, so the subscript depth resets (only relevant when
	// this list appears inside an index/slice subscript).
	if c.subscript != 0 {
		saved := c.subscript
		c.subscript = 0
		defer func() { c.subscript = saved }()
	}
	var firstElem, lastElem ast.Node
	if len(x.Elts) > 0 {
		firstElem = x.Elts[0]
		lastElem = x.Elts[len(x.Elts)-1]
	}
	elems := x.Elts

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
	hasInterior := slots.hasInterior()

	switch {
	case len(elems) == 0 && !hasInterior:
		return stringLit("[]")
	case len(elems) == 1 && !hasInterior && c.shouldHug(elems[0]):
		return cats(lBracketLit, c.expr(elems[0]), rBracketLit)
	}

	// Preserve the source's comma style. In the comma-free style no
	// trailing comma is emitted and inter-element commas appear only
	// between same-line elements.
	omitCommas := listOmitsCommas(elems, x.Lbrack, x.Rbrack)

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
		numElems:            len(elems),
		hasInterior:         hasInterior,
		anyDoc:              anyHasDocComment(elems),
		anyPost:             anyHasPostComment(elems),
		sameLineOpen:        hasSameLineOpener(c, elems),
		noElemNewline:       noElemHasNewline(elems),
		lineHeader:          hasLineLeadingComment(slots, firstElem),
		allowsTrailingComma: !omitCommas,
	}
	policy := c.computeBracketedPolicy(layout)
	useBodyShape := !hasInterior && !layout.anyDoc && !layout.anyPost && allBracketsLackRelPos(elems)
	if useBodyShape {
		// docBodyShape's hug shape renders the body in modeBreak so a
		// per-row commaWhenBroken would emit a stray `,` against the
		// closing bracket (`}]` vs `},]`). Suppress the per-row
		// trailing comma entirely when we're on the docBodyShape path;
		// the indented shape gets its trailing comma instead from
		// docBodyShape directly (see [bodyShape]'s trailingComma
		// argument), and the flat shape doesn't need one.
		policy.wantTrailingComma = false
	}
	policy.useBodyShape = useBodyShape

	var inner doc
	if rows := c.elementRows(elems, policy.wantTrailingComma, useBodyShape, omitCommas); rows != nil {
		inner = table(rows)
	}
	layout.inner = c.wrapInteriorComments(inner, slots.prefix, slots.suffix)

	return c.applyBracketed(layout, policy)
}

// computeBracketedPolicy derives the [bracketedPolicy] from b.
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
	// forceOpen breaks the opener for a single-field struct whose
	// field value breaks across lines, instead of hugging it as
	// `{field: {` (the label would sit between the braces). A Field
	// first element only occurs in structs, so list/call/embed hugs
	// are untouched. When it fires the body must keep the parent
	// [docNest], so shareIndent is suppressed too.
	forceOpen := false
	if nodeType[*ast.Field](b.firstElem) && c.hasNewlineInSubtree(b.firstElem) {
		forceOpen = true
	}
	// infiniteWidth reports whether this bracket renders under
	// infiniteWidth - i.e. it has authored structure (a real Lbrace /
	// Lbrack), the same signal maybeGroup keys off. It gates
	// shareIndent below.
	infiniteWidth := c.nodeFlags[b.node]&isAuthored != 0
	// shareIndent only drops the parent's [docNest] - closeBreak still
	// fires, so `]` / `}` / `)` keeps its own line and a post-element
	// comment can render as a separate Raw row above the closer
	// without risk. Disabling shareIndent on anyPost would just shift
	// the surviving elements' bodies by one indent level, which is
	// surprising: commenting out a sibling shouldn't move what's left.
	shareIndentPermitted := !b.hasInterior && !b.anyDoc
	// Share indent in two flavours:
	//   - sameLineOpen: an inner contiguous opener (e.g. `[{...},
	//     ...]`) provides the indent for breaks; the parent drops its
	//     [docNest] so the two layers don't compound.
	//   - noElemNewline: every element shares a line with its
	//     predecessor, so under asInfiniteWidth the parent's openBreak
	//     and row separators emit no newlines. The parent's [docNest] is
	//     then unused for its own structure, and stacking it on top of
	//     an inner break (e.g. an inner list whose body is multi-line
	//     in `{a: 1, b: [\n c\n]}`) would push that break one indent
	//     level too deep. Drop the [docNest] in this case too.
	//
	// Both flavours assume the inner opener hugs the parent bracket so
	// its own [docNest] supplies the indent. That only holds under
	// infiniteWidth: a synthesised bracket renders under finiteWidth,
	// where the soft openBreak breaks to a newline and the inner
	// opener lands on its own line, so dropping the [docNest] would
	// under-indent the body - hence the infiniteWidth gate.
	shareIndent := infiniteWidth && shareIndentPermitted && !hugFirst && !forceOpen && (b.sameLineOpen || b.noElemNewline)
	// Trailing comma policy. The if-condition excludes brackets whose
	// syntax disallows a trailing comma (allowsTrailingComma) and
	// brackets where hugging the closer to the last element (`{a:1}]`)
	// would place the comma against the closer. Inside the if-block:
	//
	//   - !authored => no RelPos in subtree: always want one. The
	//     runtime [docSwitchMode] inside commaWhenBroken resolves to
	//     "" when the bracket fits flat, so emitting unconditionally
	//     here is safe.
	//   - otherwise the comma must be decided statically - only when
	//     the closing bracket has Newline/NewSection RelPos (i.e.
	//     lands on its own line). commaWhenBroken's [docSwitchMode]
	//     cannot be trusted here: it follows the enclosing group's
	//     mode, and under [infiniteWidth] that group renders broken
	//     whenever *any* hard break exists in the subtree - including
	//     layouts where the closer still hugs the last element on a
	//     shared line (`[a,\n b]`), where a comma must not appear. The
	//     closer's placement in authored-mode is static (closerRel),
	//     so the comma keys off the same signal.
	wantTrailingComma := false
	authored := c.authored(b.node)
	if b.allowsTrailingComma && !hugLast {
		wantTrailingComma = !authored || b.closerRel >= token.Newline
	}
	// A bracketed body that opens broken must also close broken: when
	// the first element starts on its own line (or
	// interior/line-header comments force the open break), the closing
	// bracket lands on its own line too, never hugging the last
	// element. Without this an AST whose first element carries a
	// Newline RelPos but whose closing brace is Blank (as some
	// decoders emit for inline maps) renders half-broken - `{` breaks
	// but `}` hugs the last field. The converse shape - the opener
	// hugging an inner contiguous opener (`[{`-style) while the closer
	// breaks - is a legitimate shared-indent layout, so the open break
	// is left to its own RelPos and is not forced here. leadRel is the
	// first element's leading RelPos, or the opener's own when the
	// body is empty.
	leadRel := b.openerRel
	if b.firstElem != nil {
		leadRel = LeadingRelPos(b.firstElem)
	}
	openBreaks := b.lineHeader || b.hasInterior || leadRel >= token.Newline || forceOpen
	forceClose := openBreaks || b.closerRel >= token.Newline
	return bracketedPolicy{
		hugFirst:          hugFirst,
		hugLast:           hugLast,
		shareIndent:       shareIndent,
		wantTrailingComma: wantTrailingComma,
		openBreak:         openBreakDoc(b.lineHeader, b.hasInterior || forceOpen, leadRel),
		closeBreak:        closeBreakDoc(b.closerRel, forceClose),
	}
}

// applyBracketed assembles the final Doc using a precomputed policy.
// Drops the parent's [docNest] under hugFirst/shareIndent (the inner
// element's [docNest] provides the indent); drops closeBreak under
// hugLast so `}]` / `)]` / `}}` stay adjacent. When p.useBodyShape
// is set, the inner content is wrapped in [docBodyShape] which
// picks one of the three shapes (flat, indented, hug; see
// [docBodyShape]) at render time - openBreak, closeBreak, hugFirst,
// hugLast, and shareIndent are ignored in that branch.
func (c *converter) applyBracketed(b bracketedLayout, p bracketedPolicy) doc {
	if p.useBodyShape {
		// Pass commaLit as the trailing comma so [docBodyShape]
		// emits it in the indented shape. Both ListLit and CallExpr
		// accept a trailing comma before their closer in CUE;
		// emitting it keeps the indented shape idempotent under the
		// standard re-render path. allowsTrailingComma is true for
		// the brackets that reach this branch (lists and calls), so
		// we can pass commaLit unconditionally.
		var trailingComma doc
		if b.allowsTrailingComma {
			trailingComma = commaLit
		}
		return c.maybeGroup(b.node,
			cats(b.openPrefix, b.open, bodyShape(b.inner, trailingComma), b.close))
	}
	var inner doc
	switch {
	case p.hugFirst:
		// Drop openBreak and the parent's [docNest] entirely - the inner
		// element's own [docNest] provides the one indent level needed.
		inner = b.inner
	case p.shareIndent:
		// Keep openBreak so leading non-opener elements render
		// normally, but skip the parent's [docNest] so the same-line
		// opener's content shares indent.
		inner = cat(p.openBreak, b.inner)
	default:
		inner = nest(cat(p.openBreak, b.inner))
	}
	closeBreak := p.closeBreak
	if p.hugLast {
		closeBreak = nil
	}
	return c.maybeGroup(b.node,
		cats(b.openPrefix, b.open, inner, closeBreak, b.close))
}

// elementRows builds the per-element Rows for a list literal or
// call expression. Each element produces one element row (via
// listElemRow) plus zero or more Raw rows for any post-element
// comments. Inter-element rows carry an [elemBreak]-derived Sep so a
// Newline / NewSection RelPos on the element gets honoured.
// trailingComma is the parent's trailing-comma decision. Returns nil
// for empty input.
//
// Adjacent bracketed-without-RelPos pairs get a literal-space Sep
// instead of [elemBreak]'s soft break: when both elements are
// bracketed and both ends of each one carry no RelPos (see
// [bracketsLackRelPos]), the pair reads horizontally as
// `}, {` even when each cell's body breaks internally. Cells that
// carry RelPos hints on their brackets fall through to [elemBreak] so
// the authored per-line layout is preserved.
//
// wrapLinked wraps each element's value cell in [docNextGroupNoop],
// which forwards a [docBodyShape] modeBreak decision into each cell by
// neutralising the cell's own outer [docGroup], so when the hug shape
// fires every cell stays broken alongside its siblings.
func (c *converter) elementRows(elems []ast.Expr, trailingComma, wrapLinked, omitCommas bool) []row {
	if len(elems) == 0 {
		return nil
	}
	rows := make([]row, 0, len(elems))
	lastIdx := len(elems) - 1
	prevLackRelPos := false
	prevHasComment := false
	for i, e := range elems {
		row, postCgs := c.listElemRow(e, i == lastIdx, trailingComma, omitCommas)
		if wrapLinked && len(row.cells) > 0 {
			row.cells[0] = nextGroupNoop(row.cells[0])
		}
		curLackRelPos := bracketsLackRelPos(e)
		if i > 0 {
			// In the comma-free style the separator carries the
			// inter-element comma (the cell does not - see
			// [converter.listElemRow]), so a comma appears exactly when
			// the elements share a line and never otherwise. This holds
			// even under [infiniteWidth], where [asInfiniteWidth]
			// collapses the soft break and the comma it carries
			// together: a stray comma between two elements the renderer
			// puts on the same line is impossible by construction.
			switch {
			case prevLackRelPos && curLackRelPos && !prevHasComment && row.docComment == nil:
				// Two adjacent bracketed elements with no RelPos render
				// horizontally (`}, {`). That is only safe when the
				// previous element does not end with a `//` comment
				// (which would swallow the following `{`) and the current
				// element has no doc comment (which needs its own line);
				// otherwise fall back to a normal element break.
				if omitCommas {
					row.sep = commaSpaceLit
				} else {
					row.sep = spaceLit
				}
			case omitCommas:
				// lineBreakOrComma is ", " when flat (same line) and a
				// newline (no comma) when broken.
				row.sep = relBreakOr(LeadingRelPos(e), lineBreakOrComma)
			default:
				row.sep = elemBreak(e)
			}
		}
		prevLackRelPos = curLackRelPos
		prevHasComment = row.hasComment || len(postCgs) > 0
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
// trailing is true it is a TrailingComma emitted only in broken-mode,
// so an inline list does not acquire a spurious comma; when trailing
// is false no comma is emitted at all.
//
// Comments attached to e at Position 1/2 with Line=false are
// "post-element" comments: own-line comments after the element that
// cannot be folded into the element's cell (the trailing comma would
// land after the `//` and be absorbed). They are returned in the
// second result to be emitted as separate Raw rows after the
// element's row.
func (c *converter) listElemRow(e ast.Expr, last, trailing, omitCommas bool) (row, []*ast.CommentGroup) {
	var comma doc
	switch {
	case !last && omitCommas:
		// Comma-free style: a comma is emitted only when this element
		// shares a line with the next, in which case the row separator
		// carries it (see [converter.elementRows]). We deliberately
		// leave the cell comma empty so the comma and the inter-element
		// line break are one decision and cannot disagree - a
		// switchMode here would follow the enclosing group's mode,
		// which under [infiniteWidth] goes broken whenever any element
		// is multi-line, suppressing the comma even though same-line
		// siblings still need it.
	case !last:
		comma = commaLit
	case trailing:
		comma = commaWhenBroken
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
	var core doc
	// The parser attaches a trailing comment on `...T` to T (the
	// rightmost token), not to the Ellipsis. exprCore would then
	// render the comment inline after T, so the comma appended below
	// would land inside the `//` comment and be lost. Render T's core
	// instead and fold T's comments into this row's slots so the
	// trailing comment is routed to the trailing cell, after the
	// comma.
	if ell, ok := e.(*ast.Ellipsis); ok && ell.Type != nil {
		if ts := classifyComments(ell.Type); len(ts.doc)+len(ts.prefix)+len(ts.suffix)+len(ts.trailing) > 0 {
			core = cat(ellipsisLit, c.exprCore(ell.Type))
			slots.doc = append(slots.doc, ts.doc...)
			slots.prefix = append(slots.prefix, ts.prefix...)
			slots.suffix = append(slots.suffix, ts.suffix...)
			slots.trailing = append(slots.trailing, ts.trailing...)
		}
	}
	if core == nil {
		core = c.exprCore(e)
	}
	skipInterior := nodeManagesInteriorComments(e)
	var trailingComment doc
	var postComments []*ast.CommentGroup
	hasComment := false
	routeNonDoc := func(cg *ast.CommentGroup) {
		hasComment = true
		// A same-line trailing comment belongs in the element's trailing
		// cell. The parser marks these with Blank RelPos, but a
		// programmatically built AST (e.g. the textproto decoder) may set
		// only cg.Line - the authoritative "same line as the element"
		// flag - and leave the RelPos unset. Honour either.
		if cg.Line || cg.Pos().RelPos() == token.Blank {
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

	cells := []doc{cat(core, comma)}
	if trailingComment != nil {
		cells = append(cells, trailingComment)
	}

	return row{
		docComment: c.docCommentBlock(slots.doc, e.Pos().RelPos()),
		cells:      cells,
		hasComment: hasComment,
	}, postComments
}

// postCommentRows turns a list of post-element comments into Raw
// rows. Each row's Sep honours the comment's own RelPos (NewSection
// -> BlankLine, Newline/anything else -> HardLine) so blank lines the
// user wrote between the element and the comment, or between
// consecutive comments, survive into the output.
func (c *converter) postCommentRows(cgs []*ast.CommentGroup) []row {
	if len(cgs) == 0 {
		return nil
	}
	rows := make([]row, len(cgs))
	for i, cg := range cgs {
		sep := relBreakOr(cg.Pos().RelPos(), lineBreakHard)
		rows[i] = row{sep: sep, raw: c.commentGroup(cg)}
	}
	return rows
}

// ellipsis converts an Ellipsis node.
func (c *converter) ellipsis(x *ast.Ellipsis) doc {
	if x.Type != nil {
		return cat(ellipsisLit, c.expr(x.Type))
	}
	return ellipsisLit
}

// unaryExpr converts a UnaryExpr. The operand is obtained via
// exprCore so that any comments on it are placed after the
// operator+operand unit, not between them. The operand passes through
// [wrapForPrecedence] with [token.UnaryPrec] so a lower-precedence
// binary operand is parenthesised, preserving its grouping (`!a & b`
// must not re-parse as `(!a) & b`).
func (c *converter) unaryExpr(x *ast.UnaryExpr) doc {
	op := x.Op.String()
	xDoc := wrapForPrecedence(c.exprCore(x.X), x.X, token.UnaryPrec)

	// A unary operator hugs its operand (`-10`, `!a`, `>=3`). CUE has
	// no meaningful space there, so we neither emit nor preserve one,
	// regardless of the operand's RelPos - except where omitting it
	// would merge the operator with the operand's leading token into a
	// different operator (see [unaryOpMergesWithOperand]). Keying off
	// tokenisation rather than RelPos means the required space is
	// emitted by construction, even for programmatic ASTs that carry
	// no RelPos - without it `!(=~"x")` would render `!=~"x"` and fail
	// to re-parse.
	body := cat(stringLit(op), xDoc)
	if unaryOpMergesWithOperand(x.Op, x.X) {
		body = cats(stringLit(op), spaceLit, xDoc)
	}

	return c.withComments(x.X, body)
}

// postfixExpr converts a PostfixExpr. The operand is obtained via
// exprCore so that the suffix operator is placed before any trailing
// comments on the operand. The operand passes through
// [wrapForPrecedence] with [token.HighestPrec] so a lower-precedence
// binary operand is parenthesised, preserving its grouping (`a & b ...`
// must not re-parse as `a & (b ...)`).
func (c *converter) postfixExpr(x *ast.PostfixExpr) doc {
	xDoc := wrapForPrecedence(c.exprCore(x.X), x.X, token.HighestPrec)
	body := cat(xDoc, stringLit(x.Op.String()))

	return c.withComments(x.X, body)
}

// binaryExpr converts a BinaryExpr, dispatching by shape:
//
//   - a | or & chain carrying any trailing // comment goes to
//     [converter.chainTableExpr] so the trailing comments column-align;
//   - a chain whose post-first arms are all bracketed
//     (struct/list/paren/call/index) goes to [converter.binaryExprPrec],
//     keeping the operator inline as `} | {`;
//   - any other | / & chain goes to [converter.chainGroupArms], which
//     renders the arms inline when they fit and one-per-line when they
//     don't;
//   - a non-chain BinaryExpr (precedence-sensitive +, -, *, ==, ...)
//     goes to [converter.binaryExprPrec].
func (c *converter) binaryExpr(x *ast.BinaryExpr) doc {
	// The starting depth reflects how deeply the expression is nested
	// inside index/slice subscripts: at the top of a value it is 1
	// (normal spacing), inside `[...]` it is 2+ (compact spacing). The
	// binary tree threads further +1 per operand level from here.
	depth := 1 + c.subscript
	if x.Op == token.OR || x.Op == token.AND {
		arms, hasTrailing := flattenBinaryChain(x)
		if hasTrailing {
			return c.chainTableExpr(x, arms, nil)
		}
		if len(arms) > 1 && allBracketArms(arms[1:]) {
			return c.binaryExprPrec(x, binaryCutoff(x, depth), depth)
		}
		return c.chainGroupArms(x, arms)
	}
	return c.binaryExprPrec(x, binaryCutoff(x, depth), depth)
}

// chainArm holds one operand of a flattened | or & chain together
// with any comments attached to the BinaryExpr whose operator follows
// this arm (i.e. the "| // trailing" belonging to this row's op).
type chainArm struct {
	expr     ast.Expr
	trailing []*ast.CommentGroup // Position>=2, Line=true: goes in this row's comment column
	interior []*ast.CommentGroup // Position==1: interior of next arm (inject if possible)
}

// chainGroupArms formats a same-operator | or & chain as a [docGroup]
// whose arms are joined by soft separators. In flat-mode the arms
// render on one line as `a | b | c`. When the chain doesn't fit, the
// [docGroup] breaks and the soft separator becomes a hard newline,
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
func (c *converter) chainGroupArms(x *ast.BinaryExpr, arms []chainArm) doc {
	outerPrec := x.Op.Precedence()
	if len(arms) == 1 {
		return c.armDoc(arms[0], outerPrec)
	}
	opDoc := stringLit(" " + x.Op.String())

	first := c.armDoc(arms[0], outerPrec)
	first = cat(first, opDoc)

	arms = arms[1:]

	rest := make([]doc, 0, 2*len(arms)-1)
	lastIdx := len(arms) - 1
	for i, arm := range arms {
		elem := c.armDoc(arm, outerPrec)
		if i < lastIdx {
			elem = cat(elem, opDoc)
		}
		rest = append(rest, elemBreak(arm.expr), elem)
	}

	// The [docNest] indents continuation arms when the chain breaks across
	// lines (`a |\n\tb |\n\tc`). It must apply exactly when the arm
	// separators emit newlines: applying it to a chain whose arms all
	// stay on one line pushes any inner HardLine (e.g. a bracketed arm
	// whose own body breaks) one indent level too deep - `a & b &
	// {\n\t\tc: _\n\t}` instead of the wanted `a & b & {\n\tc: _\n}`.
	//
	// In an authored chain with no Newline/NewSection RelPos on any
	// arm (chainBreaks below), the separators are soft and
	// [asInfiniteWidth] bakes them to their flat text - they can never
	// break. But the enclosing [docGroup] can still render broken: an
	// arm containing a hard break (a bracketed arm whose body breaks)
	// fails the flat-fit test. A [docSwitchMode] would then pick its
	// broken branch and force the [docNest] onto a visually flat chain. So
	// this case must skip the [docNest] statically.
	//
	// In every other case the separators track the group's mode, so
	// [docSwitchMode] applies the [docNest] exactly when they break: an
	// authored chain with hard separators forces the group broken
	// ([docNest] applies), and a programmatic chain's soft separators break
	// iff the group breaks on width.
	chainBreaks := false
	for _, arm := range arms {
		if LeadingRelPos(arm.expr) >= token.Newline {
			chainBreaks = true
			break
		}
	}
	body := cats(rest...)
	var wrapped doc
	if c.authored(x) && !chainBreaks {
		wrapped = body
	} else {
		wrapped = switchMode(nest(body), body)
	}

	return c.maybeGroup(x, cat(first, wrapped))
}

// flattenBinaryChain walks a left-associative (or mixed) chain of
// BinaryExprs with operator x.Op and returns one chainArm per leaf
// operand. Comments on each intermediate BinaryExpr are attached to
// the arm whose operator they follow: trailing //-comments go on the
// left arm's row; interior (Position==1) comments belong to the
// following arm and are later injected into its body if it is a
// braced StructLit.
//
// On the outermost BinaryExpr (x), Position=PosDoc and
// Position>=PosTrailingMin comments are the chain's outer doc/trailing
// comments handled by the surrounding withComments wrap, so they are
// skipped here to avoid double-rendering; Position=PosPrefix and
// Position=PosSuffix comments are "inside the chain" with no other
// home and are collected onto arm interior/trailing.
//
// hasTrailing reports whether any intermediate node carries a trailing
// comment of any kind (anything at Position >= PosSuffix, inline or
// own-line).
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
		// Split this BinaryExpr's comments. Position=PosPrefix
		// always means interior-of-next-arm (regardless of
		// layout), matching the classification in binaryExprPrec;
		// PosSuffix is "between op and right" and also belongs
		// to the chain. Position >= PosTrailingMin is post-
		// chain trailing - on intermediate BinaryExprs it
		// belongs to the preceding arm, but on the outermost it
		// is the chain's own outer trailing and is handled by
		// the surrounding withComments wrap. Position==PosDoc on
		// the outermost is also handled by withComments and must
		// be skipped here too - a programmatic BinaryExpr can
		// carry a Position=0 doc comment, and including it as a
		// preceding-arm trailing would duplicate the comment.
		var trailing, interior []*ast.CommentGroup
		for _, cg := range ast.Comments(bin) {
			switch {
			case cg.Position == PosPrefix:
				interior = append(interior, cg)
			case outermost && (cg.Position == PosDoc || cg.Position >= PosTrailingMin):
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
// &) as a [docTable]: one row per arm, with an optional trailing-comment
// cell that column-aligns across arms. fieldTrailing, when non-nil,
// is an enclosing field's same-line trailing comment that should
// align with the chain's arm comments in the same column.
func (c *converter) chainTableExpr(x *ast.BinaryExpr, arms []chainArm, fieldTrailing doc) doc {
	opStr := " " + x.Op.String()
	outerPrec := x.Op.Precedence()

	rows := make([]row, len(arms))
	for i, arm := range arms {
		var commentDoc doc
		for _, cg := range arm.trailing {
			commentDoc = joinLines(commentDoc, c.commentGroup(cg))
		}
		cell0 := c.armDoc(arm, outerPrec)

		if i < len(arms)-1 {
			cell0 = cat(cell0, stringLit(opStr))
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
				raw = cats(raw, spaceLit, commentDoc)
			}
			rows[i] = row{
				raw:        raw,
				hasComment: hasComment,
			}
			continue
		}

		cells := []doc{cell0}
		if hasComment {
			cells = append(cells, commentDoc)
		}
		rows[i] = row{
			sep:        lineBreakHard,
			cells:      cells,
			hasComment: hasComment,
		}
	}

	return c.maybeGroup(x, nest(table(rows)))
}

// armDoc renders a single chainArm: its expression, with any interior
// comments injected. outerPrec is the precedence of the chain's own
// operator; when the arm is itself a [*ast.BinaryExpr] with a lower
// precedence, the arm is wrapped in parens so the rendered output
// re-parses to the same tree.
func (c *converter) armDoc(a chainArm, outerPrec int) doc {
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
	prefix := make([]doc, 0, 2*len(a.interior)+1)
	for _, cg := range a.interior {
		prefix = append(prefix, c.commentGroup(cg), lineBreakHard)
	}
	prefix = append(prefix, body)
	return cats(prefix...)
}

// armExpr renders e as a chain arm under a chain whose operator has
// precedence outerPrec. A nested BinaryExpr at lower precedence is
// wrapped in parens so the chain shape `a OP_outer arm` re-parses
// to the same tree on round-trip. See [converter.armDoc].
func (c *converter) armExpr(e ast.Expr, outerPrec int) doc {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op.Precedence() < outerPrec {
		return cats(lParenLit, c.expr(e), rParenLit)
	}
	return c.expr(e)
}

// injectInteriorComments renders a braced StructLit or bracketed
// ListLit with extra comments prepended to its body, so comments that
// belong inside the brackets parse back to the same attachment. Panics
// if x is neither a StructLit nor a ListLit.
//
// When the first injected comment is Line=true (e.g. `{// c` or
// `[// c`), it is kept on the opener's line with a single space,
// matching how structLit / listLit handle their own Position=1 prefix
// comments.
func (c *converter) injectInteriorComments(x ast.Expr, extra []*ast.CommentGroup) doc {
	var open, body, close doc
	var closerRel token.RelPos
	switch n := x.(type) {
	case *ast.StructLit:
		open, close = lBraceLit, rBraceLit
		body = c.declSlice(n.Elts)
		closerRel = n.Rbrace.RelPos()
	case *ast.ListLit:
		open, close = lBracketLit, rBracketLit
		omitCommas := listOmitsCommas(n.Elts, n.Lbrack, n.Rbrack)
		if rows := c.elementRows(n.Elts, false, false, omitCommas); rows != nil {
			body = table(rows)
		}
		closerRel = n.Rbrack.RelPos()
	default:
		panic(fmt.Sprintf("pretty: injectInteriorComments: unexpected %T", x))
	}
	inner := c.wrapInteriorComments(body, extra, nil)
	openBreak := doc(lineBreakHard)
	closeBreak := relBreak(closerRel)
	if len(extra) > 0 && (extra[0].Line || extra[0].Pos().RelPos() == token.Blank) {
		openBreak = spaceLit
		if closerRel < token.Newline {
			// A `//` header runs to end-of-line, so the closer must
			// start a fresh line even when the source didn't request
			// one.
			closeBreak = lineBreakHard
		}
	}
	return c.maybeGroup(x, cats(
		open,
		nest(cat(openBreak, inner)),
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
func (c *converter) binaryExprPrec(x *ast.BinaryExpr, co, depth int) doc {
	prec := x.Op.Precedence()

	left := c.binaryOperand(x.X, prec, depth+binaryDiffPrec(x.X, prec))
	right := c.binaryOperand(x.Y, prec+1, depth+1)

	op := x.Op.String()

	var maybeSpace doc
	if prec < co || operatorsWouldMerge(x.Op, x.Y) {
		// prec < co: precedence-driven spacing. operatorsWouldMerge:
		// even in compact mode, force a blank when the operator and the
		// RHS's leading sign would lex as a different token (`+ +b` ->
		// `++`).
		maybeSpace = spaceLit
	}

	// Position semantics on a BinaryExpr (only the internal slots
	// are processed here; PosDoc and trailing slots are externalised
	// to expr() -> withComments / listElemRow / fieldRow, so binary
	// handlers don't double-render them):
	//   PosPrefix : interior of the RHS - typically a comment written
	//               inside an empty `{ ... }` on the right that the
	//               parser hung off the BinaryExpr because there was
	//               no field to attach it to.
	//   PosSuffix, Line=true  : same-line `//` trailing the operator.
	//   PosSuffix, Line=false : own-line `//` comment between op and
	//                           right (forces a break before RHS).
	//
	// CUE "//" line comments extend to end-of-line, so any non-doc
	// comment forces the RHS onto the next line. Operator must stay
	// on the left's line (leading operator on a new line is not valid
	// CUE because of auto-semicolon insertion).
	slots := classifyComments(x)
	interior := slots.prefix
	var opInline []doc               // PosSuffix on the op's line (cg.Line or Blank)
	var midBlock []*ast.CommentGroup // PosSuffix on its own line
	for _, cg := range slots.suffix {
		if cg.Line || cg.Pos().RelPos() == token.Blank {
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
	brokenRHS := func(body doc) doc {
		inner := make([]doc, 0, 2*len(midBlock)+2)
		inner = append(inner, lineBreakHard)
		for _, cg := range midBlock {
			inner = append(inner, c.commentGroup(cg), lineBreakHard)
		}
		inner = append(inner, body)
		return cats(left, maybeSpace, stringLit(op), cats(opInline...), nest(cats(inner...)))
	}

	if len(interior) > 0 {
		if isBracketedInjectionTarget(x.Y) {
			injected := c.injectInteriorComments(x.Y, interior)
			if len(opInline) > 0 || len(midBlock) > 0 || LeadingRelPos(x.Y) >= token.Newline {
				// Still break before Y because of other constraints.
				return brokenRHS(injected)
			}
			return cats(left, maybeSpace, stringLit(op), maybeSpace, injected)
		}
		// No host bracket on the RHS: fall through with interior
		// comments merged into midBlock (they'll land between op and
		// right, forcing a break).
		midBlock = append(midBlock, interior...)
	}

	if LeadingRelPos(x.Y) >= token.Newline || len(opInline) > 0 || len(midBlock) > 0 {
		return brokenRHS(right)
	}
	return cats(left, maybeSpace, stringLit(op), maybeSpace, right)
}

// binaryOperand formats one operand of a binary expression, recursing
// into nested binary expressions at the same or higher precedence. A
// nested binary at *lower* precedence is wrapped in parentheses by
// [wrapForPrecedence] so the rendered output re-parses to the same
// tree shape.
func (c *converter) binaryOperand(e ast.Expr, prec, depth int) doc {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op.Precedence() >= prec {
		return c.binaryExprPrec(bin, binaryCutoff(bin, depth), depth)
	}
	return wrapForPrecedence(c.expr(e), e, prec)
}

// callExpr converts a CallExpr. Arguments are handled like list
// elements: RelPos is honoured, commas come before trailing comments,
// and a trailing comma before ')' is emitted on the same terms as
// lists.
func (c *converter) callExpr(x *ast.CallExpr) doc {
	// The callee is a primary expression: `(...)` binds tighter than
	// any binary operator, so a binary callee needs parens (`(a &
	// b)(x)`).
	fun := wrapForPrecedence(c.expr(x.Fun), x.Fun, token.HighestPrec)

	if len(x.Args) == 0 {
		return cats(fun, stringLit("()"))
	}
	if arg := x.Args[0]; len(x.Args) == 1 && c.shouldHug(arg) {
		return cats(fun, lParenLit, c.expr(arg), rParenLit)
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
		hasInterior:   false, // calls don't carry PosPrefix/PosSuffix interior comments
		anyDoc:        anyHasDocComment(x.Args),
		anyPost:       anyHasPostComment(x.Args),
		sameLineOpen:  hasSameLineOpener(c, x.Args),
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
	// Call arguments are always comma-separated; comma-free style is a
	// list-literal feature only.
	layout.inner = table(c.elementRows(x.Args, policy.wantTrailingComma, useBodyShape, false))
	return c.applyBracketed(layout, policy)
}

// indexExpr converts an IndexExpr. Honours RelPos on the index
// expression. A newline before ']' is not valid CUE (auto-comma
// insertion triggers), so the index and closing bracket stay on the
// same line.
func (c *converter) indexExpr(x *ast.IndexExpr) doc {
	openBreak := relBreak(x.Index.Pos().RelPos())
	// The receiver is a primary expression: `[i]` binds tighter than
	// any binary operator, so a binary receiver needs parens (`(a &
	// b)[i]`). It renders at the current depth; the subscript contents
	// render one level deeper, compacting binary operators (`s[1+2]`).
	recv := wrapForPrecedence(c.expr(x.X), x.X, token.HighestPrec)
	c.subscript++
	index := c.expr(x.Index)
	c.subscript--
	return c.maybeGroup(x, cats(
		recv,
		lBracketLit,
		nest(cat(openBreak, index)),
		rBracketLit,
	))
}

// sliceExpr converts a SliceExpr.
func (c *converter) sliceExpr(x *ast.SliceExpr) doc {
	// Blanks around ':' when this slice is at the top level (not
	// nested in a subscript), both bounds are present, and either
	// bound is a binary expression (e.g. `s[1+2 : 2+4]`).
	colon := colonLit
	if c.subscript == 0 && x.Low != nil && x.High != nil &&
		(nodeType[*ast.BinaryExpr](x.Low) || nodeType[*ast.BinaryExpr](x.High)) {
		colon = cats(spaceLit, colonLit, spaceLit)
	}
	// The receiver is a primary expression: the slice `[lo:hi]` binds
	// tighter than any binary operator, so a binary receiver needs
	// parens. The bounds render one level deeper (compact operators).
	recv := wrapForPrecedence(c.expr(x.X), x.X, token.HighestPrec)
	c.subscript++
	low := c.expr(x.Low)
	high := c.expr(x.High)
	c.subscript--
	return cats(recv, lBracketLit, low, colon, high, rBracketLit)
}

// operatorsWouldMerge reports whether printing binary operator op
// immediately before the rendering of rhs (with no surrounding space)
// would lex as a different token sequence: `+` before a unary `+`
// (`++`), `-` before `-` (`--`), or `/` before `*` (`/*`). It
// descends to rhs's leftmost leading token, since only a unary sign
// there can merge with the preceding operator.
func operatorsWouldMerge(op token.Token, rhs ast.Expr) bool {
	var lead token.Token
	for {
		switch x := rhs.(type) {
		case *ast.BinaryExpr:
			rhs = x.X
			continue
		case *ast.UnaryExpr:
			lead = x.Op
		}
		break
	}
	switch op {
	case token.ADD:
		return lead == token.ADD // ++
	case token.SUB:
		return lead == token.SUB // --
	case token.QUO:
		return lead == token.MUL // /*
	}
	return false
}

// parenExpr converts a ParenExpr. '(' hugs the inner expression
// unless the inner expression's RelPos explicitly requests a newline;
// otherwise breaks in the inner expression (e.g. a multi-line struct
// or a broken chain) should not force a break after '('. A newline
// before ')' is not valid CUE (auto-comma insertion triggers), so the
// expression and closing paren stay on the same line.
func (c *converter) parenExpr(x *ast.ParenExpr) doc {
	// Omit redundant parentheses around an already-parenthesized
	// expression: `((x))` renders as `(x)`. The outer paren's own
	// comments are handled by the enclosing [converter.expr].
	if x, ok := x.X.(*ast.ParenExpr); ok {
		return c.parenExpr(x)
	}
	if LeadingRelPos(x.X) >= token.Newline {
		return cats(
			lParenLit,
			nest(cat(lineBreakHard, c.expr(x.X))),
			rParenLit,
		)
	}
	return cats(lParenLit, c.expr(x.X), rParenLit)
}

// interpolation converts an Interpolation node. Its elements alternate
// between string fragments (BasicLit, which already include the \( and
// ) delimiters and are emitted verbatim) and interpolated expressions.
//
// Multi-line strings get special treatment: the body's strip prefix
// (the leading whitespace before the closing `"""`) is parsed and
// lifted into the renderer's nest level via [docAtIndent], so any
// [docNest] inside an interpolated expression lands one level deeper
// than the line that contained `\(` rather than at the field's level.
func (c *converter) interpolation(x *ast.Interpolation) doc {
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
		parts := make([]doc, len(x.Elts))
		for i, e := range x.Elts {
			if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				parts[i] = stringLit(lit.Value)
			} else {
				// An interpolation `\(...)` is a subscript-like context:
				// its expression renders one level deeper, compacting
				// binary operators (`"\(1+2)"`).
				c.subscript++
				parts[i] = c.expr(e)
				c.subscript--
			}
		}
		return cats(parts...)
	}

	return c.multiLineInterpolation(x)
}

// multiLineInterpolation handles the multi-line case of
// [converter.interpolation]. It produces an opener (everything up to
// the first newline, rendered at the caller's indent) plus a body
// wrapped in a [docNest] so it lands one level deeper than the opening
// line. The embedded strip prefix is removed from each body line and
// the renderer re-supplies the indentation at the actual render depth
// via [docLineBreakHard].
func (c *converter) multiLineInterpolation(x *ast.Interpolation) doc {
	stripPrefix := stripPrefixFromInterp(x)

	var opener, body []doc
	inBody := false

	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			for i, line := range strings.Split(lit.Value, "\n") {
				if i > 0 {
					// Line-break before this line. A line that starts with
					// the strip prefix uses [docLineBreakHard], which the
					// surrounding [docNest] re-indents to the render depth; we
					// strip the prefix from `line` so we don't double
					// up. A line without the prefix (a bare empty line, or
					// whitespace shorter than the prefix) uses
					// [docLineBreakBare], a bare `\n` that adds no
					// indentation, so it is rendered verbatim.
					inBody = true
					if rest, ok := strings.CutPrefix(line, stripPrefix); ok && stripPrefix != "" {
						body = append(body, lineBreakHard)
						line = rest
					} else {
						body = append(body, lineBreakBare)
					}
				}
				if line == "" {
					continue
				}
				if inBody {
					body = append(body, stringLit(line))
				} else {
					opener = append(opener, stringLit(line))
				}
			}
		} else {
			// Interpolation expressions render one level deeper (compact
			// operators), as in the single-line case above.
			c.subscript++
			ed := c.expr(e)
			c.subscript--
			if inBody {
				body = append(body, ed)
			} else {
				opener = append(opener, ed)
			}
		}
	}

	if !inBody {
		// Defensive: shouldn't happen since multiLine was true.
		return cats(opener...)
	}

	return cats(cats(opener...), nest(cats(body...)))
}

// funcExpr converts a Func node.
func (c *converter) funcExpr(x *ast.Func) doc {
	args := make([]doc, len(x.Args))
	for i, a := range x.Args {
		args[i] = c.expr(a)
	}
	argDoc := sep(commaSpaceLit, args...)
	return cats(stringLit("func"), lParenLit, argDoc, stringLit("): "), c.expr(x.Ret))
}

// comprehension converts a Comprehension.
func (c *converter) comprehension(x *ast.Comprehension) doc {
	parts := make([]doc, 0, len(x.Clauses)+3)

	// prevEndsLineComment tracks whether the part emitted just before
	// the current separator ends with a `//` comment. CUE line comments
	// run to end-of-line, so the following part (the next clause, the
	// body, or the fallback keyword) must start on a fresh line or the
	// comment swallows it - e.g. the body's opening brace lands inside
	// the comment and the result no longer parses.
	prevEndsLineComment := false

	// sep chooses the separator before the next part. A break is forced
	// either by the caller (forced, e.g. an authored Newline RelPos) or
	// because the preceding part ends with a `//` comment; otherwise the
	// inline separator is used.
	sep := func(forced bool, inline doc) doc {
		if forced || prevEndsLineComment {
			return lineBreakHard
		}
		return inline
	}

	for i, clause := range x.Clauses {
		clauseDoc := c.withComments(clause, c.clause(clause))
		if i > 0 {
			// LeadingRelPos gives the RelPos of the first visible token
			// (the doc comment if any, otherwise the clause itself), so
			// a doc-commented clause that begins on its own line keeps
			// its break. A clause that carries a doc comment must also
			// start on its own line regardless of RelPos, or the `//`
			// would absorb the inline separator and merge with whatever
			// follows.
			forced := HasDocComment(clause) || LeadingRelPos(clause) >= token.Newline
			clauseDoc = cat(sep(forced, spaceLit), clauseDoc)
		}
		parts = append(parts, clauseDoc)
		prevEndsLineComment = c.nodeFlags[clause]&endsWithLineComment != 0
	}

	if x.Value != nil {
		forced := LeadingRelPos(x.Value) >= token.Newline
		parts = append(parts, sep(forced, spaceLit), c.expr(x.Value))
		prevEndsLineComment = c.nodeFlags[x.Value]&endsWithLineComment != 0
	}

	if x.Fallback != nil {
		parts = append(parts, c.fallbackClause(x, sep(false, spaceLit)))
	}

	return cats(parts...)
}

// clause converts a single clause.
func (c *converter) clause(cl ast.Clause) doc {
	switch x := cl.(type) {
	case *ast.ForClause:
		return c.forClause(x)
	case *ast.IfClause:
		return cats(stringLit("if "), c.expr(x.Condition))
	case *ast.LetClause:
		return c.letClause(x)
	case *ast.TryClause:
		return c.tryClause(x)
	default:
		return nil
	}
}

// letClause converts a LetClause.
func (c *converter) letClause(x *ast.LetClause) doc {
	return cats(stringLit("let "), stringLit(x.Ident.Name), equalsSpaceLit, c.expr(x.Expr))
}

// forClause converts a ForClause.
func (c *converter) forClause(x *ast.ForClause) doc {
	parts := []doc{stringLit("for ")}
	if x.Key != nil {
		parts = append(parts, stringLit(x.Key.Name), commaSpaceLit)
	}
	parts = append(parts, stringLit(x.Value.Name), stringLit(" in "), c.expr(x.Source))
	return cats(parts...)
}

// tryClause converts a TryClause.
func (c *converter) tryClause(x *ast.TryClause) doc {
	if x.Ident != nil {
		return cats(stringLit("try "), stringLit(x.Ident.Name), equalsSpaceLit, c.expr(x.Expr))
	}
	return stringLit("try")
}

// fallbackClause converts the FallbackClause of a Comprehension. The
// keyword depends on the comprehension's clauses: "otherwise" after
// for-clauses or multiple clauses, "else" after a single if/try
// clause. lead is the separator placed before the keyword.
func (c *converter) fallbackClause(comp *ast.Comprehension, lead doc) doc {
	// The keyword choice mirrors cue/parser.parseFallbackClause: a
	// single if/try clause uses "else"; everything else (a for clause,
	// or multiple clauses) uses "otherwise".
	kw := "otherwise"
	if len(comp.Clauses) == 1 {
		switch comp.Clauses[0].(type) {
		case *ast.IfClause, *ast.TryClause:
			kw = "else"
		}
	}
	return cats(lead, stringLit(kw), spaceLit, c.expr(comp.Fallback.Body))
}

// decl converts a declaration node to a Doc, without handling comments
// on it.
func (c *converter) decl(d ast.Decl) doc {
	switch x := d.(type) {
	case *ast.Field:
		return c.field(x)

	case *ast.Alias:
		return cats(stringLit(x.Ident.Name), equalsSpaceLit, c.expr(x.Expr))

	case *ast.EmbedDecl:
		return c.expr(x.Expr)

	case *ast.LetClause:
		return c.letClause(x)

	case *ast.Ellipsis:
		return c.ellipsis(x)

	case *ast.Comprehension:
		return c.comprehension(x)

	case *ast.Package:
		return cats(stringLit("package "), stringLit(x.Name.Name))

	case *ast.ImportDecl:
		return c.importDecl(x)

	case *ast.Attribute:
		return stringLit(x.Text)

	case *ast.CommentGroup:
		return c.commentGroup(x)

	case *ast.BadDecl:
		// Render a bad declaration as bottom (`_|_`), consistently with
		// BadExpr: valid CUE (an embedded bottom).
		return bottomLit

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

// field converts a Field to a Doc (full field, not table row),
// handling all comments on the field itself.
func (c *converter) field(f *ast.Field) doc {
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
	// line by appending to key; own-line ones go inside val's [docNest]
	// at the value's indent so re-parse / re-format round-trips:
	// the parser re-attaches such a comment as a doc of the value
	// with Newline RelPos, which renders identically (also at val's
	// indent).
	//
	// preVal holds the leading-break content prepended to val inside
	// [docNest]. Seed it with lineBreakHard when val needs to start on its
	// own line (doc comment, braceless chain, or any Position=2
	// comment - even inline ones force val to a new line because
	// the suffix `//` runs to end-of-line). Subsequent HardLines
	// separate stacked own-line suffix comments, and the final
	// HardLine bridges the last comment to val.
	var preVal doc
	if HasDocComment(f.Value) || valNeedsLeadingBreak(f.Value) || len(slots.suffix) > 0 {
		preVal = lineBreakHard
	}
	for _, cg := range slots.suffix {
		cd := c.commentGroup(cg)
		if cg.Line || cg.Pos().RelPos() == token.Blank {
			key = cat(key, c.commentSep(cg, cd))
		} else {
			preVal = cats(preVal, cd, lineBreakHard)
		}
	}
	val := c.fieldValDoc(f, preVal)

	var before, after []doc
	for _, cg := range slots.doc {
		before = append(before, c.commentGroup(cg), lineBreakHard)
	}
	for _, cg := range slots.trailing {
		after = append(after, c.commentSep(cg, c.commentGroup(cg)), switchMode(nil, lineBreakHard))
	}

	var body doc
	if preVal == nil {
		body = cats(key, spaceLit, val)
	} else {
		// When val is preceded by a leading break (preVal != nil), skip
		// the " " between key and val - val already starts with HardLine.
		body = cat(key, val)
	}

	return cats(append(append(before, body), after...)...)
}

// fieldRow splits a Field into one or more table Rows for alignment.
// The first row carries the field itself; any further rows are
// continuation rows holding extra same-line trailing comments. The
// caller appends every returned row to the current table so they
// share its column layout.
//
// Comment routing:
//
//   - Doc comments (Position 0) are placed in [row.DocComment] on the
//     first row, before the key, not affecting column widths.
//   - Position 1 (prefix) comments are ignored - the parser does not
//     generate them on Fields (see [converter.field]).
//   - Position 2 (suffix, between colon and value) comments are
//     deferred and prepended to the value Doc after it is computed.
//   - Same-line trailing comments go into the trailing-cell column
//     for cross-row alignment. When a field has more than one, each
//     comment past the first becomes its own continuation row whose
//     leading cells are nil, so all the comments column-align beneath
//     the field's trailing comment.
//   - Post-field block comments (own-line, with Newline/NewSection
//     RelPos and no preceding trailing-cell comment) are returned in
//     the second result so the caller can emit them as sibling
//     comment blocks after the field, preserving their original
//     vertical position.
//
// For braceless chains (x: y: z: val) the caller provides the chain;
// the head field identifies the row and the leaf field carries the
// value.
func (c *converter) fieldRow(chain []*ast.Field) ([]row, []*ast.CommentGroup) {
	head := chain[0]
	leaf := chain[len(chain)-1]

	slots := classifyComments(head)
	// Position 1 (slots.prefix) is not generated by the parser on
	// Fields - see [converter.field] for the rationale. Ignored here
	// too.
	//
	// slots.trailing (Position >= PosTrailingMin) is classified into
	// three buckets:
	//
	//   - trailingLineDocs: comments routed to the trailing-cell
	//     column. Line=true (a same-line trailing `//`) always lands
	//     here. A Line=false RelPos=Newline comment that immediately
	//     follows a trailingLineDocs entry is treated as a
	//     continuation of that trailing (the parser produces this
	//     shape when re-parsing aligned multi-comment output, so the
	//     rule keeps alignment idempotent). When trailingLineDocs ends
	//     up with >=2 entries each becomes its own continuation row so
	//     they all column-align in the trailing-cell column.
	//   - trailingFallback: Line=false !IsNewline comments. With
	//     RelPos info this is the "Blank" case; without it
	//     (programmatic ASTs) any !IsNewline comment lands here.
	//     Joined with HardLine inside one cell - re-using the pre-
	//     existing "broken cell renders flush to row indent" path so
	//     the original layout round-trips without RelPos info.
	//   - trailingComments: cg.Pos().IsNewline() with no preceding
	//     trailingLineDocs entry, or NewSection - rendered as Raw rows
	//     below the field.
	var trailingLineDocs []doc
	var trailingFallback doc
	var trailingComments []*ast.CommentGroup
	hasComment := len(slots.suffix) > 0
	// slots.suffix (Position 2): between colon and value. valDoc isn't
	// computed yet (we need leaf.Value's type to know whether
	// attrs/trailing align in a chain table), so defer until later.
	for _, cg := range slots.trailing {
		hasComment = true
		cd := c.commentGroup(cg)
		rel := cg.Pos().RelPos()
		switch {
		case cg.Line:
			trailingLineDocs = append(trailingLineDocs, cd)
		case rel == token.Newline && len(trailingLineDocs) > 0:
			// Continuation of the preceding trailing-cell comment:
			// re-parse of an aligned multi-comment field has this shape,
			// so routing here keeps the alignment idempotent.
			trailingLineDocs = append(trailingLineDocs, cd)
		case cg.Pos().IsNewline():
			trailingComments = append(trailingComments, cg)
		default:
			trailingFallback = joinLines(trailingFallback, cd)
		}
	}
	// Lift same-line (Line=true) comments attached to a leaf-token
	// value up to the field's trailing-cell column. The parser
	// attaches source-level same-line trailing `//` to the field
	// itself, but [internal/core/export] attaches error annotations to
	// the value ([*ast.BottomLit]) with Position=2, Line=true. Both
	// are visually field-trailing comments and should column-align
	// across sibling rows; lifting normalises the two AST shapes here
	// so the rendered val cell stays pure text and doesn't trip the
	// table segmenter's break-capable boundary rule. Restricted to
	// leaf-token expression types: composite expressions (BinaryExpr,
	// UnaryExpr, CallExpr, ...) attach internal-layout comments at
	// Position=2 with Line=true that belong inside the expression's
	// rendering, not at the field's trailing cell.
	var leafValueSlots commentSlots
	leafValueSlotsFiltered := false
	if isLeafLitValue(leaf.Value) {
		leafValueSlots = classifyComments(leaf.Value)
		lifted, kept := partitionLineComments(leafValueSlots)
		if lifted.any() {
			leafValueSlots = kept
			leafValueSlotsFiltered = true
			for _, cg := range lifted.all() {
				hasComment = true
				trailingLineDocs = append(trailingLineDocs, c.commentGroup(cg))
			}
		}
	}
	// Assemble trailing-cell docs. When >=2 Line=true comments are
	// present, each becomes its own continuation row so they all align
	// in the trailing-cell column; trailingFallback (if any) merges
	// into the last entry. Otherwise everything collapses to one cell
	// (potentially with internal HardLines from joinLines, which
	// renders as a broken-segment row whose internal newlines land at
	// the row's indent - matching the original layout for
	// stripped-RelPos input).
	var trailingCommentDocs []doc
	switch {
	case len(trailingLineDocs) >= 2:
		trailingCommentDocs = append(trailingCommentDocs, trailingLineDocs...)
		if trailingFallback != nil {
			i := len(trailingCommentDocs) - 1
			trailingCommentDocs[i] = joinLines(trailingCommentDocs[i], trailingFallback)
		}
	case len(trailingLineDocs) == 1 || trailingFallback != nil:
		var combined doc
		if len(trailingLineDocs) > 0 {
			combined = trailingLineDocs[0]
		}
		if trailingFallback != nil {
			combined = joinLines(combined, trailingFallback)
		}
		trailingCommentDocs = []doc{combined}
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
	var valDoc doc
	var attrsDoc doc
	bin, isChain := leaf.Value.(*ast.BinaryExpr)
	isChain = isChain && len(trailingCommentDocs) > 0 && (bin.Op == token.OR || bin.Op == token.AND)
	var binArms []chainArm
	binHasTrailing := false
	if isChain {
		binArms, binHasTrailing = flattenBinaryChain(bin)
	}
	if isChain && binHasTrailing {
		// chainTableExpr accepts a single Doc; join the trailing
		// comments vertically (joinLines) before handing them in. The
		// chain-table path renders the trailing column itself, so the
		// standard trailing-cell column is unused here.
		var combined doc
		for _, d := range trailingCommentDocs {
			combined = joinLines(combined, d)
		}
		valDoc = appendAttrs(c.chainTableExpr(bin, binArms, combined), leaf.Attrs)
		trailingCommentDocs = nil
	} else if leafValueSlotsFiltered {
		valDoc = c.withCommentsSlots(leaf.Value, c.exprCore(leaf.Value), leafValueSlots)
		attrsDoc = attrsSpaced(leaf.Attrs)
	} else {
		valDoc = c.expr(leaf.Value)
		attrsDoc = attrsSpaced(leaf.Attrs)
	}

	// Now process Position=2 comments (between colon and value);
	// prepend them to valDoc.
	for _, cg := range slots.suffix {
		valDoc = cats(c.commentSep(cg, c.commentGroup(cg)), valDoc)
	}

	// For chain rows (len(chain) > 1) whose leaf value is atomic,
	// build a merged alternative for cell 0: a tree of nested Groups
	// with valDoc inside the deepest [docNest], woven through every label
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
	var mergedFirstCell doc
	if len(chain) > 1 && !valDoc.canBreak() {
		mergedFirstCell = valDoc
	}
	var key doc
	for i := len(chain) - 1; i >= 0; i-- {
		fk := c.fieldKey(chain[i])
		if key == nil {
			key = fk
		} else {
			// Build the chain key as nested Groups (one per split point)
			// without the leaf's value, so cell 0 has a clean chain that
			// can align across sibling rows in a multi-row segment.
			key = group(cat(fk, nest(cat(lineBreakOrSpace, key))))
		}
		if mergedFirstCell != nil {
			mergedFirstCell = group(cat(fk, nest(cat(lineBreakOrSpace, mergedFirstCell))))
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
	var firstTrailing doc
	if len(trailingCommentDocs) > 0 {
		firstTrailing = trailingCommentDocs[0]
	}
	cells := []doc{key, valDoc}
	if attrsDoc != nil || firstTrailing != nil {
		cells = append(cells, attrsDoc)
		if firstTrailing != nil {
			cells = append(cells, firstTrailing)
		}
	}

	rows := []row{{
		docComment:      docComment,
		cells:           cells,
		hasComment:      hasComment,
		allowRowBreak:   true,
		mergedFirstCell: mergedFirstCell,
	}}
	// Any extra trailing comments become continuation rows sharing the
	// segment's column layout: every cell up to the trailing column is
	// nil, so the table renderer pads to those columns' widths and the
	// comment lands at the same column as the main row's trailing
	// comment. Sep is HardLine so each continuation row appears on its
	// own line below the main row.
	if firstTrailing != nil {
		for _, d := range trailingCommentDocs[1:] {
			cont := make([]doc, len(cells))
			cont[len(cont)-1] = d
			rows = append(rows, row{
				sep:           lineBreakHard,
				cells:         cont,
				hasComment:    true,
				allowRowBreak: true,
			})
		}
	}
	return rows, trailingComments
}

// fieldKey builds the key portion of a field: label + alias +
// constraint + colon.
func (c *converter) fieldKey(f *ast.Field) doc {
	key := c.label(f.Label)
	if f.Alias != nil {
		key = cat(key, c.postfixAlias(f.Alias))
	}
	if f.Constraint == token.OPTION || f.Constraint == token.NOT {
		key = cat(key, stringLit(f.Constraint.String()))
	}
	return cat(key, colonLit)
}

// fieldValDoc builds the value portion of a field: value + attributes.
// When preVal is non-nil it is the leading-break content prepended to
// val, so val is rendered on its own line. For non-chain values the
// continuation lives inside a [docNest] so it indents relative to the
// key; for a braceless chain value (`a: b: c`) the chain elements stay
// at the outer indent and the [docNest] is omitted, since the chain is
// continuation of the same field rather than a nested body. preVal ==
// nil means no leading break: val is returned as-is and rendered on
// the key's line.
func (c *converter) fieldValDoc(f *ast.Field, preVal doc) doc {
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
		// the indenting [docNest].
		if bracelessChainCollapsible(f.Value) &&
			!HasDocComment(f.Value) &&
			!valNeedsLeadingBreak(f.Value) {
			val = cat(preVal, val)
		} else {
			val = nest(cat(preVal, val))
		}
	}
	return val
}

// fieldValExpr renders an expression that sits in the value slot of
// a Field. It is the same as [converter.expr] except that a braceless
// StructLit with a single Field child is rendered as that inner Field
// directly (no synthesised braces), preserving the parsed chain shape
// `a: b: c: 1`. The chain is only valid here, where the surrounding
// Field's key provides the binding; at every other expression position
// the StructLit emits its synthesised braces via [converter.expr].
//
// The collapse is refused when the inner Field carries side data that
// has no natural home in chain form:
//
//   - inner.Attrs: the `@attr` syntactically attaches to the leaf, so
//     collapsing would silently move it and may change semantics.
//   - inner doc / leading comments: in chain form they would be
//     squeezed between the outer and inner keys.
//   - StructLit-attached comments: no chain-form equivalent.
func (c *converter) fieldValExpr(v ast.Expr) doc {
	if !bracelessChainCollapsible(v) {
		return c.expr(v)
	}
	return c.declSlice(v.(*ast.StructLit).Elts)
}

// postfixAlias converts a PostfixAlias.
func (c *converter) postfixAlias(a *ast.PostfixAlias) doc {
	// The single-alias form is always parenthesised (`~(X)`, never
	// `~X`), consistent with the parser which accepts both but
	// normalises to the parens form.
	if a.Label == nil {
		return cats(stringLit("~("), stringLit(a.Field.Name), rParenLit)
	}
	return cats(stringLit("~("), stringLit(a.Label.Name), commaLit, stringLit(a.Field.Name), rParenLit)
}

// importDecl converts an ImportDecl. Comments on each ImportSpec are
// preserved via withComments. All separators (opener break, inter-spec,
// closer break) are RelPos-driven: a parser-produced AST carries
// Newline hints that lay the specs out one per line, while a
// programmatic AST with no RelPos hints renders flat, the enclosing
// [docGroup] picking compact `import ("a", "b")` when it fits and
// breaking across lines otherwise.
func (c *converter) importDecl(x *ast.ImportDecl) doc {
	if len(x.Specs) == 0 {
		return nil
	}
	if !x.Lparen.IsValid() && len(x.Specs) == 1 && !HasDocComment(x.Specs[0]) {
		// Single import without parens. A doc comment on the spec
		// forces the parenthesised form below: the no-parens form would
		// hoist the comment before the `import` keyword (via
		// withComments), detaching it from the spec it documents.
		s := x.Specs[0]
		body := cats(stringLit("import "), c.importSpec(s))
		return c.withComments(s, body)
	}

	specs := make([]doc, len(x.Specs))
	for i, s := range x.Specs {
		spec := c.withComments(s, c.importSpec(s))
		if i > 0 {
			// Between specs: comma+space flat, hard newline when
			// broken or when leadingRel demands it (a NewSection on a
			// doc comment becomes BlankLine).
			spec = cat(relBreakOr(LeadingRelPos(s), lineBreakOrComma), spec)
		}
		specs[i] = spec
	}

	body := cats(specs...)
	openBreak := relBreakOr(LeadingRelPos(x.Specs[0]), lineBreakOrEmpty)
	closeBreak := relBreakOr(x.Rparen.RelPos(), lineBreakOrEmpty)
	return group(cats(
		stringLit("import ("),
		nest(cat(openBreak, body)),
		closeBreak,
		rParenLit,
	))
}

// importSpec converts an ImportSpec.
func (c *converter) importSpec(s *ast.ImportSpec) doc {
	if s.Name != nil {
		return cats(stringLit(s.Name.Name), spaceLit, stringLit(s.Path.Value))
	}
	return stringLit(s.Path.Value)
}

var (
	// lineBreakHard returns a hard line break that always emits a
	// newline. Any [docGroup] containing a HardLine is forced to
	// break.
	lineBreakHard    = &docLineBreakHard{docBase: docBase{breaks: true}}
	lineBreakBare    = &docLineBreakBare{docBase: docBase{breaks: true}}
	lineBreakOrEmpty = lineBreakSoft("")
	// lineBreakOrSpace is a Line that emits a space when flat.
	lineBreakOrSpace = lineBreakSoft(" ")
	// lineBreakOrComma is a Line that emits ", " when flat.
	lineBreakOrComma = lineBreakSoft(", ")
	// blankLine emits a bare newline followed by an indented newline,
	// producing a truly blank line (no trailing whitespace) as a
	// separator.
	blankLine = cat(lineBreakBare, lineBreakHard)
	// commaWhenBroken emits a comma only in broken-mode.
	commaWhenBroken = switchMode(commaLit, nil)
	lBracketLit     = stringLit("[")
	rBracketLit     = stringLit("]")
	lBraceLit       = stringLit("{")
	rBraceLit       = stringLit("}")
	lParenLit       = stringLit("(")
	rParenLit       = stringLit(")")
	spaceLit        = stringLit(" ")
	commaLit        = stringLit(",")
	commaSpaceLit   = stringLit(", ")
	periodLit       = stringLit(".")
	colonLit        = stringLit(":")
	equalsLit       = stringLit("=")
	equalsSpaceLit  = stringLit(" = ")
	bottomLit       = stringLit("_|_")
	ellipsisLit     = stringLit("...")
)

// wrapEligibility reports whether n is one of the AST node types at
// which the converter calls [converter.maybeGroup] (eligible), and
// whether such a node has authored structural tokens of its own
// (authored). For non-eligible types, both results are false.
//
// The eligible node types are *ast.File, *ast.StructLit, *ast.ListLit,
// *ast.CallExpr, *ast.BinaryExpr, and *ast.IndexExpr; the isAuthored
// algorithm only sets the flag on these types.
//
// authored == false for a wrap-eligible node means its brackets were
// synthesised by the converter (e.g. a programmatic StructLit with
// Lbrace.IsValid() == false), so the [analyse] pass treats n as a
// pass-through for RelPos bubble-up and the synthesised wrap ends up
// under [finiteWidth] rather than [infiniteWidth], keeping its soft
// opener/closer breaks soft.
//
// Only StructLit and ListLit have a meaningful authored check; the
// other wrap-eligible types always have authored structural tokens, so
// authored is always true for them.
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
		if cg.Position >= PosTrailingMin && !cg.Line && cg.Pos().RelPos() >= token.Newline {
			return true
		}
	}
	return false
}

// nodeEndsWithLineComment reports whether the comments that
// [converter.withCommentsSlots] renders after n leave a `//` comment at
// the tail of n's output. The slot selection mirrors withCommentsSlots:
// trailing comments (Position >= PosTrailingMin) always render after the
// body, and for nodes that don't manage their own interior comments the
// prefix/suffix slots are emitted as trailing content too. CUE has only
// `//` line comments, so any such tail comment runs to end-of-line.
func nodeEndsWithLineComment(n ast.Node) bool {
	s := classifyComments(n)
	if len(s.trailing) > 0 {
		return true
	}
	if !nodeManagesInteriorComments(n) && (len(s.prefix) > 0 || len(s.suffix) > 0) {
		return true
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

// multilineDecls reports whether any inter-decl separator is a hard
// break (Newline / NewSection RelPos).
func multilineDecls(decls []ast.Decl) bool {
	for i, decl := range decls {
		if i > 0 && LeadingRelPos(decl) >= token.Newline {
			return true
		}
	}
	return false
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

// FirstCommentAt returns the first CommentGroup attached to n whose
// Position equals pos, or nil if there is none. Returns nil for nil n.
func FirstCommentAt(n ast.Node, pos int8) *ast.CommentGroup {
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

// HasDocComment reports whether a node has any doc comments.
func HasDocComment(n ast.Node) bool {
	return hasCommentAt(n, PosDoc)
}

// appendAttrs concatenates a field's attributes after val, separated
// by spaces. Returns val unchanged when attrs is empty.
func appendAttrs(val doc, attrs []*ast.Attribute) doc {
	for _, attr := range attrs {
		val = cats(val, spaceLit, stringLit(attr.Text))
	}
	return val
}

// attrsSpaced returns a Doc rendering attrs joined by spaces, or nil
// when attrs is empty.
func attrsSpaced(attrs []*ast.Attribute) doc {
	if len(attrs) == 0 {
		return nil
	}
	parts := make([]doc, 0, 2*len(attrs)-1)
	for i, attr := range attrs {
		if i > 0 {
			parts = append(parts, spaceLit)
		}
		parts = append(parts, stringLit(attr.Text))
	}
	return cats(parts...)
}

// nodeManagesInteriorComments reports whether a node's conversion
// bakes its interior (PosPrefix / PosSuffix) comments into the
// returned Doc, leaving doc and trailing comments for the caller to
// wrap. True for StructLit, ListLit, and BinaryExpr - each places its
// interior comments inside its own rendering, so withComments must skip
// the prefix and suffix slots for these nodes to avoid double-rendering.
func nodeManagesInteriorComments(n ast.Node) bool {
	switch n.(type) {
	case *ast.StructLit, *ast.ListLit, *ast.BinaryExpr:
		return true
	}
	return false
}

// joinLines appends cd below acc, separated by a HardLine. A nil acc
// is replaced by cd directly.
func joinLines(acc, cd doc) doc {
	if acc == nil {
		return cd
	}
	return cats(acc, lineBreakHard, cd)
}

// classifyComments partitions n's attached comments into slots.
// See commentSlots for the slot semantics.
func classifyComments(n ast.Node) commentSlots {
	var s commentSlots
	for _, cg := range ast.Comments(n) {
		switch cg.Position {
		case PosDoc:
			s.doc = append(s.doc, cg)
		case PosPrefix:
			s.prefix = append(s.prefix, cg)
		case PosSuffix:
			s.suffix = append(s.suffix, cg)
		default:
			s.trailing = append(s.trailing, cg)
		}
	}
	return s
}

// partitionLineComments splits in into (line, other): CommentGroups
// with Line=true land in the first return value, the rest in the
// second.
func partitionLineComments(in commentSlots) (line, other commentSlots) {
	split := func(src []*ast.CommentGroup) (l, o []*ast.CommentGroup) {
		for _, cg := range src {
			if cg.Line {
				l = append(l, cg)
			} else {
				o = append(o, cg)
			}
		}
		return
	}
	line.doc, other.doc = split(in.doc)
	line.prefix, other.prefix = split(in.prefix)
	line.suffix, other.suffix = split(in.suffix)
	line.trailing, other.trailing = split(in.trailing)
	return line, other
}

// relBreakOr returns the line-break Doc corresponding to a RelPos
// with a soft fallback for the non-newline cases. Newline always
// produces HardLine and NewSection always produces BlankLine.
func relBreakOr(rel token.RelPos, soft doc) doc {
	switch rel {
	case token.Newline:
		return lineBreakHard
	case token.NewSection:
		return blankLine
	}
	return soft
}

// relBreak returns the line-break Doc corresponding to a RelPos:
// BlankLine for NewSection, HardLine for Newline, and
// [lineBreakOrEmpty] otherwise.
func relBreak(rel token.RelPos) doc {
	return relBreakOr(rel, lineBreakOrEmpty)
}

// LeadingRelPos returns the RelPos that drives the separator placed
// before n's first visible token. If n has a Position=0 doc comment,
// the first visible token is that comment, so its RelPos wins;
// otherwise n's own RelPos applies.
func LeadingRelPos(n ast.Node) token.RelPos {
	if cg := FirstCommentAt(n, PosDoc); cg != nil {
		return cg.Pos().RelPos()
	}
	return n.Pos().RelPos()
}

// reindentMultilineString re-indents a multi-line string or bytes
// literal so its body and closing quote render one level deeper than
// the opening line (the opener stays inline after the `key: `). value
// is the full literal text, e.g. "\"\"\"\n\tbody\n\t\"\"\"".
//
// The strip prefix (the whitespace before the closing quote, returned
// by [literal.QuoteInfo.Whitespace]) is removed from each body line
// and the renderer re-emits the indentation at its actual nest level
// via [docLineBreakHard] inside a [docNest], so the body lands at
// field-indent + 1 regardless of what the producer embedded. Empty
// body lines emit a bare newline so they carry no trailing whitespace.
// Returns nil if the quotes can't be parsed or the literal isn't
// genuinely multi-line.
func reindentMultilineString(value string) doc {
	qi, _, _, err := literal.ParseQuotes(value, value)
	if err != nil || !qi.IsMulti() {
		return nil
	}
	lines := strings.Split(value, "\n")
	if len(lines) < 2 {
		return nil
	}
	strip := qi.Whitespace()
	body := make([]doc, 0, (len(lines)-1)*2)
	for _, line := range lines[1:] {
		if rest, ok := strings.CutPrefix(line, strip); ok && strip != "" {
			body = append(body, lineBreakHard, stringLit(rest))
		} else {
			// An empty line, or a line whose whitespace is shorter than
			// the strip prefix: emit it verbatim after a bare newline so
			// no indentation is added.
			body = append(body, lineBreakBare, stringLit(line))
		}
	}
	return cat(stringLit(lines[0]), nest(cats(body...)))
}

// isContiguousOpener reports whether e's rendering wraps its broken
// inner content in a [docNest] of its own. When that's true, an
// enclosing construct (struct/list/call) can drop its own [docNest] at
// the boundary where it meets e, so the two layers don't compound and
// chains like `[{...}]` or `f(g(h([...])))` stay at one indent level
// for their broken bodies.
//
// StructLit/ListLit/ParenExpr begin literally with `{`/`[`/`(`.
// CallExpr (`fun(args)`) and IndexExpr (`x[i]`) begin with their
// callee/receiver text but expose their opener on the same physical
// line, with the same [docNest] effect on indent, so they qualify too -
// "contiguous" refers to layout effect on indent, not literal bracket
// adjacency. Synthesised brackets on a programmatic StructLit/ListLit
// (Lbrace/Lbrack == NoPos) behave identically to authored ones and
// qualify the same way.
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

// hasSameLineOpener reports whether any element is a contiguous opener
// that the user wrote on the parent's opener line. It walks elements
// left-to-right and stops at the first element with a
// Newline-or-stronger leading RelPos (including a Newline carried by a
// doc comment), since that element and all later ones are on new lines.
//
// The opener must also break across lines to qualify: an opener that
// stays inline (e.g. a short embedded list `[1, 3]`) supplies no
// indent, so the same-line-opener rule that drops the parent's
// [docNest] must not fire for it.
func hasSameLineOpener[T ast.Node](c *converter, elems []T) bool {
	for _, e := range elems {
		if LeadingRelPos(e) >= token.Newline {
			return false
		}
		if nodeIsContiguousOpener(e) && c.hasNewlineInSubtree(e) {
			return true
		}
	}
	return false
}

// noElemHasNewline reports whether every element shares a line with
// its predecessor (no Newline / NewSection leading RelPos on any
// element - a doc comment's Slash is taken into account via
// [LeadingRelPos]).
func noElemHasNewline[T ast.Node](elems []T) bool {
	for _, e := range elems {
		if LeadingRelPos(e) >= token.Newline {
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
//     comment: `{ // c\n field`, `[ // c\n elem`, or `( // c\n arg`,
//     with the `// c` hung off the first child by the parser.
//
// Pass an empty [commentSlots] when the parent node carries no
// interior comments of its own (e.g. CallExpr); only the first child's
// doc comment is then inspected.
func hasLineLeadingComment(slots commentSlots, first ast.Node) bool {
	// A comment sits on the opener line when the parser gives it a Blank
	// RelPos; a programmatically built AST may signal the same with only
	// cg.Line.
	if len(slots.prefix) > 0 {
		if cg := slots.prefix[0]; cg.Line || cg.Pos().RelPos() == token.Blank {
			return true
		}
	}
	if cg := FirstCommentAt(first, PosDoc); cg != nil {
		return cg.Line || cg.Pos().RelPos() == token.Blank
	}
	return false
}

// openBreakDoc returns the separator between the opening bracket and
// the inner content. lineHeader keeps a `{ // c\n...` comment on the
// opener line; hasInterior forces a HardLine so a `//` swallowing the
// closer is impossible; otherwise leadRel (the first element's
// leading RelPos, or the opener's own when the body is empty) drives
// - a soft break for an inline body, a HardLine/BlankLine when the
// first element starts on its own line.
func openBreakDoc(lineHeader, hasInterior bool, leadRel token.RelPos) doc {
	switch {
	case lineHeader:
		return spaceLit
	case hasInterior:
		return lineBreakHard
	default:
		return relBreak(leadRel)
	}
}

// closeBreakDoc returns the separator between the inner content and
// the closing bracket. When forceClose is set (see
// [converter.computeBracketedPolicy]) the closer lands on its own
// line, promoting a soft closerRel to at least a HardLine (a stronger
// NewSection break is preserved); otherwise it follows closerRel,
// flattening away for an inline body.
func closeBreakDoc(closerRel token.RelPos, forceClose bool) doc {
	if forceClose {
		return relBreakOr(closerRel, lineBreakHard)
	}
	return relBreak(closerRel)
}

// anyHasDocComment reports whether any node in the slice carries a
// Position=0 (doc) comment.
func anyHasDocComment[T ast.Node](nodes []T) bool {
	for _, n := range nodes {
		if HasDocComment(n) {
			return true
		}
	}
	return false
}

// anyHasPostComment reports whether any node in nodes carries a
// post-element comment: a non-doc, non-Line=true comment that the
// parser hung off the node (Position 1/2 with Line=false on
// non-bracketed nodes, or Position >= PosTrailingMin with Line=false
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
		// Doc comments and same-line comments are not post-element
		// comments. The parser marks same-line with Blank RelPos;
		// programmatically built ASTs may set only cg.Line.
		if cg.Position == PosDoc || cg.Line || cg.Pos().RelPos() == token.Blank {
			continue
		}
		// Bracketed nodes (StructLit/ListLit/BinaryExpr) handle their
		// own PosPrefix/PosSuffix interior comments - those don't
		// count as "post-element" because they're inside the body.
		if nManagesInteriorComments &&
			(cg.Position == PosPrefix || cg.Position == PosSuffix) {
			continue
		}
		return true
	}
	return false
}

// elemBreak returns the line-break portion of a list element or chain
// arm separator, without any comma. Uses LeadingRelPos so a
// NewSection on the expression's doc comment becomes a blank line
// before this element - placed before the comment, not between the
// comment and the body.
func elemBreak(e ast.Expr) doc {
	return relBreakOr(LeadingRelPos(e), lineBreakOrSpace)
}

// bracketsLackRelPos reports whether e is a bracketed expression whose
// opening and closing brackets both carry no RelPos hint - the signal
// that no authored layout intent attaches to e's edges, either because
// the AST was built programmatically or because a parsed AST has had
// its RelPos stripped. Non-bracketed expressions (idents, literals,
// operators, ...) are reported as false.
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
// without any RelPos hint on its outer bracket pair (see
// [bracketsLackRelPos]). Returns false for empty input.
func allBracketsLackRelPos(elems []ast.Expr) bool {
	for _, e := range elems {
		if !bracketsLackRelPos(e) {
			return false
		}
	}
	return len(elems) > 0
}

// listOmitsCommas reports whether a list literal should render in the
// comma-free style (commas omitted between elements on separate
// lines), selecting the style from the source's existing comma usage
// rather than imposing it. It returns true when an inter-element comma
// is missing on an element that begins on its own line; for a
// single-element list the trailing comma (before ']') is checked
// instead.
//
// The Scanned bit on lbrack distinguishes scanner-produced lists from
// programmatically constructed ASTs; the latter (Scanned false) always
// keep commas, since they carry no meaningful comma hints.
//
// The comma-free list syntax is a v0.17.0+ feature in which the newline
// between two elements *replaces* the comma; the parser rejects a
// comma-less boundary that is not a newline. So a missing comma only
// signals the comma-free style when the element genuinely carries a
// Newline/NewSection leading RelPos. A comma-less element that is not on
// its own line cannot have come from parsed source - it only arises in
// programmatic ASTs (e.g. the jsonschema encoder copies Scanned
// positions onto an ast.NewList while leaving the elements with neither
// comma bits nor RelPos). Treating those as comma-free would strip the
// commas they actually need, producing invalid output.
func listOmitsCommas(elems []ast.Expr, lbrack, rbrack token.Pos) bool {
	if len(elems) == 0 || !lbrack.Scanned() {
		return false
	}
	// elems[i].Pos().HasComma() records an explicit comma preceding the
	// element (i.e. after elems[i-1]).
	for i := 1; i < len(elems); i++ {
		if !elems[i].Pos().HasComma() && LeadingRelPos(elems[i]) >= token.Newline {
			return true
		}
	}
	if len(elems) == 1 {
		return !rbrack.HasComma()
	}
	return false
}

// unaryOpMergesWithOperand reports whether rendering unary operator
// op immediately before operand would let the two re-lex as a single,
// longer operator, so a separating space must be inserted. Unary
// operators otherwise hug their operand (`-10`, `>=3`, `!a`); this is
// the only case where we force a space.
//
// The hazard arises only when the operand starts with an operator
// character, which happens only when the operand is itself a
// UnaryExpr. All other operand begins with a digit, letter, quote, or
// bracket (a binary operand is parenthesised by [wrapForPrecedence],
// so it begins with `(`) and can never merge.
//
//   - op `<`, operand first byte `-`: forms `<-` (ARROW), e.g. `<-5`.
//   - op `<`, operand first byte `=`: forms `<=` (LEQ), e.g. `<=~"x"`.
//   - op `>`, operand first byte `=`: forms `>=` (GEQ), e.g. `>=~"x"`.
//   - op `!`, operand first byte `=`: forms `!=` (NEQ), e.g. `!=~"x"`.
//
// Keying off tokenisation rather than RelPos means the space is emitted
// by construction, even for programmatic ASTs that carry no RelPos.
func unaryOpMergesWithOperand(op token.Token, operand ast.Expr) bool {
	inner, ok := operand.(*ast.UnaryExpr)
	if !ok {
		return false
	}
	lead := inner.Op.String()
	if lead == "" {
		return false
	}
	switch op {
	case token.LSS:
		return lead[0] == '-' || lead[0] == '='
	case token.GTR, token.NOT:
		return lead[0] == '='
	}
	return false
}

// allBracketArms reports whether every arm's expression is a
// contiguous opener (struct/list/paren/call/index). Returns false for
// empty input.
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

// isBracketedInjectionTarget reports whether e is a bracketed form
// onto which [converter.injectInteriorComments] can prepend interior
// comments. Only braced StructLit and bracketed ListLit qualify: the
// CUE parser routes interior comments through the surrounding
// BinaryExpr for these two shapes when the brackets are otherwise
// empty, whereas other bracketed forms attach such comments directly
// to themselves.
func isBracketedInjectionTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.StructLit:
		return x.Lbrace.IsValid()
	case *ast.ListLit:
		return x.Lbrack.IsValid()
	}
	return false
}

// isLeafLitValue reports whether v is a leaf-token expression: one
// whose entire rendered form is a single token with no internal
// structure that could anchor a comment. Composite expressions
// (BinaryExpr, UnaryExpr, CallExpr, ...) are excluded because the
// parser legitimately attaches internal-layout comments to them that
// must stay inside the expression's rendering.
func isLeafLitValue(v ast.Expr) bool {
	switch v.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.BottomLit, *ast.BadExpr:
		return true
	}
	return false
}

// wrapForPrecedence wraps doc in parens when e's outer operator binds
// less tightly than the surrounding precedence prec. The AST encodes
// grouping by tree shape, but programmatic builders don't insert
// [*ast.ParenExpr] wrappers, so each rendering site whose context binds
// tighter than some binary operator must materialise the parens itself
// or the output would re-parse to a differently-grouped tree.
//
// Only [*ast.BinaryExpr] needs the wrap: every other expression type
// either is itself bracketed, produces a single token, or binds at
// least as tightly as the postfix / unary contexts.
func wrapForPrecedence(doc doc, e ast.Expr, prec int) doc {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op.Precedence() < prec {
		return cats(lParenLit, doc, rParenLit)
	}
	return doc
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
//   - the value is a braceless StructLit that [converter.fieldValExpr]
//     will render as a chain and whose inner Field's label carries
//     Newline/NewSection RelPos (e.g. x:\n y: 1);
//   - the value itself carries Newline/NewSection RelPos (a non-struct
//     value written on a new line, e.g. x:\n 1).
//
// For a braceless StructLit, [ast.StructLit.Pos] falls through to its
// first element, so v.Pos().IsNewline() would conflate the StructLit's
// own position with its first child's. The StructLit branch is
// therefore handled separately and only fires for collapsible chains.
func valNeedsLeadingBreak(v ast.Expr) bool {
	if sl, ok := v.(*ast.StructLit); ok && !sl.Lbrace.IsValid() {
		if !bracelessChainCollapsible(v) {
			return false
		}
		return sl.Elts[0].Pos().IsNewline()
	}
	return v.Pos().IsNewline()
}

type stack[T any] []T

func (s *stack[T]) push(items ...T) {
	*s = append(*s, items...)
}

func (s *stack[T]) pop() T {
	n := len(*s)
	if n == 0 {
		panic("pop on empty stack")
	}
	x := (*s)[n-1]
	*s = (*s)[:n-1]
	return x
}

func nodeType[T ast.Node](n ast.Node) bool {
	_, ok := n.(T)
	return ok
}
