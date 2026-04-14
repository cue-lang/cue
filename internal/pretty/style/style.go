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

// Package style rewrites a CUE AST to reflect the project's
// conventional style. We gate each rewrite behind a flag on [Config],
// so callers pick the subset they want.
//
// All flags default to false; with the zero value, [Config.Annotate]
// leaves the AST untouched and returns false.
//
// # Flags
//
// [Config.RelPos] applies the layout heuristics catalogued below.
// These are the "blank line after Package", "one decl per line", and
// related rules: we only set RelPos on existing nodes, never altering
// structure.
//
// [Config.InlineStructs] strips the Lbrace / Rbrace of single-field
// StructLit values of Fields when collapsing the chain would be safe -
// i.e. no attrs or comments would be lost or misplaced.
//
// [Config.Labels] rewrites string labels to identifier labels where no
// reference in the same scope would bind to a different value. A label
// like `"foo"` becomes `foo` when exposing it as an identifier
// preserves the semantics.
//
// [Config.Ellipsis] defers and merges `...` / `[string]: _` / `[_]: _`
// patterns within each struct body, so multiple equivalent "open"
// markers collapse to a single trailing `...`.
//
// # RelPos heuristics
//
// The RelPos pass visits every body of declarations - the
// [*ast.File]'s Decls list and every [*ast.StructLit]'s Elts list
// found anywhere in the tree. We examine successive (prev, curr) pairs
// and "upgrade" curr's leading RelPos to the strongest applicable
// target. The "leading RelPos" is the first doc-position comment's
// Slash RelPos if curr has one, otherwise curr's own Pos RelPos - see
// [pretty.LeadingRelPos].
//
// Group A - blank lines (target: [token.NewSection]):
//   - A1: curr follows a [*ast.Package] decl.
//   - A2: curr follows an [*ast.ImportDecl].
//   - A3: curr follows a standalone [*ast.CommentGroup] decl.
//   - A4: curr has doc comments AND prev is either a Definition
//     [*ast.Field] (label starts with `#`) or any non-Field decl
//     (other than a CommentGroup, which falls under A3).
//
// Group B - hard newlines (target: [token.Newline]):
//   - B1: every curr (the default), so each decl starts on its own
//     line.
//   - B2: curr is a [*ast.LetClause], [*ast.EmbedDecl],
//     [*ast.Comprehension], or [*ast.Alias]. These decl types always
//     start on their own line. Their target coincides with B1's
//     default, so the rule is redundant today; we keep it here so a
//     future change - e.g. promoting them to NewSection - stays local.
//   - B3: for an [*ast.ImportDecl] with a valid Lparen, the closing
//     Rparen sits on its own line. When such an import group holds two
//     or more specs, we place each spec on its own line too; we leave
//     a single-spec parenthesised import alone so it may render
//     compactly as `import ("foo")`.
//
// The strongest applicable rule wins. We never weaken an existing
// RelPos: if curr's leading position already carries NewSection, a
// Newline-targeting rule leaves it alone.
//
// # Order
//
// When several flags are enabled in one [Config.Annotate] call, we run
// the rewrites in this order:
//
//  1. Labels: BasicLit labels become Idents where safe.
//  2. Ellipsis: open-marker patterns collapse to a trailing `...`.
//  3. InlineStructs: synthesised braces are stripped from single-Field
//     StructLit values.
//  4. RelPos: layout RelPos hints are set on inter-decl positions.
//
// The order matters: each later pass would otherwise see a stale view
// of the body. E.g. RelPos's pair-iteration depends on the final decl
// list after Ellipsis has merged and InlineStructs has potentially
// exposed new chain shapes.
package style

import (
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/pretty"
)

// Config selects which house-style transformations [Config.Annotate]
// applies. The zero value is a no-op. The fields are independent and
// may be enabled in any combination.
type Config struct {
	// RelPos applies the layout heuristics described in the package
	// docs - blank lines after Package and ImportDecl, one decl per
	// line, and so on. We only set RelPos on existing nodes and never
	// alter structure.
	RelPos bool

	// InlineStructs strips the Lbrace / Rbrace of a single-Field
	// StructLit value of a Field, collapsing it to the chain shape
	// `outer: inner`, when no side data would be lost or misplaced.
	//
	// We treat a struct as eligible to inline only when all of the
	// following hold:
	//
	//   - the outer node is an [*ast.Field] (the chain is only valid
	//     at a field-value position);
	//   - the StructLit has exactly one element, and that element is
	//     itself an [*ast.Field];
	//   - the outer Field has no attributes;
	//   - the inner Field has no attributes;
	//   - the inner Field has no comments;
	//   - the StructLit itself has no comments.
	//
	// We recurse: after stripping the outer's braces, we inspect the
	// inner Field's value and may strip again.
	InlineStructs bool

	// Labels rewrites string labels to identifier labels where the
	// identifier would not collide with any in-scope reference.
	Labels bool

	// Ellipsis defers and merges `...` / `[string]: _` / `[_]: _`
	// patterns within each struct body. We remove the intermediate
	// patterns and append a single fresh [*ast.Ellipsis] at the end of
	// the body, carrying the comments of the last removed marker.
	Ellipsis bool
}

// Annotate applies the transformations selected by cfg to n in place.
// It returns true if we made any change. With the zero Config,
// Annotate is a no-op and returns false.
func (cfg Config) Annotate(n ast.Node) bool {
	if n == nil || cfg == (Config{}) {
		return false
	}
	var changed bool

	if cfg.Labels {
		if simplifyLabels(n) {
			changed = true
		}
	}

	if !cfg.RelPos && !cfg.InlineStructs && !cfg.Ellipsis {
		return changed
	}

	var w walker
	w.cfg = cfg
	ast.Walk(n, w.visit, nil)
	return changed || w.changed
}

// walker shares a single ast.Walk across the InlineStructs, Ellipsis,
// and RelPos passes. We dispatch from visit to the per-node hooks for
// each enabled flag.
type walker struct {
	cfg     Config
	changed bool
}

func (w *walker) visit(n ast.Node) bool {
	if mergeDocComments(n) {
		w.changed = true
	}
	if sectionSeparateDocComments(n) {
		w.changed = true
	}
	switch n := n.(type) {
	case *ast.File:
		if w.cfg.Ellipsis {
			if mergeEllipsisDecls(&n.Decls) {
				w.changed = true
			}
		}
		if w.cfg.RelPos {
			w.annotateBody(n.Decls, false, false, true)
		}
	case *ast.Comprehension:
		if w.cfg.RelPos {
			// House style: a comprehension body always breaks after the
			// opening brace, with the closer on its own line - even a
			// single-element body, where a plain struct would stay inline.
			// The same goes for the else/otherwise fallback body.
			if body, ok := n.Value.(*ast.StructLit); ok {
				w.breakComprehensionBody(body)
			}
			if n.Fallback != nil {
				w.breakComprehensionBody(n.Fallback.Body)
			}
		}
	case *ast.StructLit:
		if w.cfg.Ellipsis {
			if mergeEllipsisDecls(&n.Elts) {
				w.changed = true
			}
		}
		if w.cfg.RelPos {
			// Only authored (braced) StructLits participate in the
			// first-elt rule. A braceless StructLit has Lbrace == NoPos,
			// which makes [ast.StructLit.Pos] fall back to Elts[0].Pos();
			// upgrading the first elt would then shift the effective
			// leading position of the enclosing field as well - never what
			// the rule intends, since it is scoped to the bracket boundary.
			if w.annotateBody(n.Elts, n.Lbrace.IsValid(), false, false) {
				// The first-elt upgrade fired, so this struct body breaks
				// vertically. We promote the closing brace's RelPos to at
				// least Newline so it lands on its own line and matches the
				// opener.
				if n.Rbrace.RelPos() < token.Newline {
					n.Rbrace = n.Rbrace.WithRel(token.Newline)
					w.changed = true
				}
			}
		}
	case *ast.Field:
		if w.cfg.InlineStructs {
			if inlineStructValue(n) {
				w.changed = true
			}
		}
	case *ast.ImportDecl:
		if w.cfg.RelPos {
			w.annotateImport(n)
		}
	case *ast.Interpolation:
		if w.cfg.RelPos {
			w.collapseSingleLineInterpolation(n)
		}
	}
	return true
}

// collapseSingleLineInterpolation removes any newline inside the
// interpolation expressions of a single-line string, so the whole
// string stays on one line (`"\(1 +\n\t2)"` -> `"\(1 + 2)"`). We clear
// Newline/NewSection leading positions rather than inspecting source
// line numbers.
//
// We leave a multi-line string untouched, detecting it by a newline in
// one of its literal fragments (the `"""..."""` body): the newlines
// that matter here live inside the `\(...)` expressions and never
// appear in the fragments.
func (w *walker) collapseSingleLineInterpolation(x *ast.Interpolation) {
	for _, e := range x.Elts {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING &&
			strings.Contains(lit.Value, "\n") {
			return // multi-line string: leave its interpolations alone
		}
	}
	// The first element is the leading string fragment; its position is
	// the string's own, so we clear only the elements after it.
	for _, e := range x.Elts[1:] {
		ast.Walk(e, func(n ast.Node) bool {
			if n != nil && n.Pos().RelPos() >= token.Newline {
				ast.SetRelPos(n, token.NoRelPos)
				w.changed = true
			}
			return true
		}, nil)
	}
}

// breakComprehensionBody applies the comprehension-body house style to
// body: we ensure the body is braced and force a break after the
// opening brace, with the closer on its own line - even a
// single-element body, where a plain struct would stay inline.
//
// When a body is emitted without brace positions, we materialise its
// absent opening brace with a Blank RelPos so `{` stays on the clause
// line; otherwise [ast.StructLit.Pos] would fall back to the first
// element and breaking that element would push the brace down (`for x
// in y\n{`). annotateBody's anyInlineHint guard leaves authored compact
// bodies untouched.
func (w *walker) breakComprehensionBody(body *ast.StructLit) {
	if body == nil {
		return
	}
	// The value's opening brace hugs the final clause - it never sits on
	// its own line. A braceless (programmatic) value gets a synthesised
	// Blank-positioned brace; a parsed brace that begins on its own line
	// (Newline / NewSection) we downgrade to Blank so the value hugs
	// (e.g. `if x > 9\n{x}` renders as `if x > 9 {x}`).
	switch {
	case !body.Lbrace.IsValid():
		body.Lbrace = token.NoPos.WithRel(token.Blank)
		w.changed = true
	case body.Lbrace.RelPos() >= token.Newline:
		body.Lbrace = body.Lbrace.WithRel(token.Blank)
		w.changed = true
	}
	if w.annotateBody(body.Elts, true, true, false) {
		if body.Rbrace.RelPos() < token.Newline {
			body.Rbrace = body.Rbrace.WithRel(token.Newline)
			w.changed = true
		}
	}
}

// annotateBody applies the RelPos inter-decl rules (Group A and Group
// B, except B3) to a slice of declarations. The first decl is the
// anchor; we upgrade every later decl to the target RelPos computed for
// it whenever that target is stronger than its existing leading RelPos.
//
// When isStructBody is true (the body is the Elts of a [*ast.StructLit],
// not the Decls of a [*ast.File]), the body has two or more decls, and
// no decl carries an explicit "stay inline" hint (Blank / NoSpace
// RelPos), we also upgrade the first decl to Newline so the body breaks
// after the opening brace. A Blank/NoSpace on any decl signals an
// authored compact body (e.g. `{a: 1, b: 2}`), so we suppress this
// openFirst upgrade.
//
// We exclude file bodies from openFirst because their first decl's
// leading RelPos describes placement relative to BOF / leading
// file-level comments, not an enclosing bracket. We exclude
// single-element struct bodies so single-field chain shapes (a: b: c)
// and single-element hug (`[{...}]`) survive - unless alwaysOpenFirst is
// set, which fires openFirst at one-or-more elements (used for
// comprehension bodies, which always break after the opening brace).
//
// We return whether the first-elt Newline upgrade fired, so the
// StructLit caller knows it must also break before the closing `}`.
func (w *walker) annotateBody(decls []ast.Decl, isStructBody, alwaysOpenFirst, isFile bool) (openFirstFired bool) {
	anyInlineHint := false
	anyComment := false
	for _, d := range decls {
		// An explicit Blank/NoSpace RelPos on a decl is the AST builder's
		// "stay inline" signal: it means the decl was authored on the same
		// line as its predecessor (e.g. a comma-separated body like `{a:
		// 1, b: 2}`). When any decl carries that hint, we take the body to
		// be authored compact and leave inter-decl RelPos alone for the
		// whole body - openFirst stays false, and we also skip the
		// B1/B2/A4 upgrades below.
		if r := pretty.LeadingRelPos(d); r == token.Blank || r == token.NoSpace {
			anyInlineHint = true
		}
		// A `//` comment on a decl makes a compact body impossible: CUE
		// line comments run to end-of-line, so a leading (doc) comment
		// would swallow the decl it precedes and a trailing one would
		// swallow the inter-decl `, ` separator and the closing brace.
		// Either way the body must break, defeating the authored-compact
		// heuristic below. Such a body only arises programmatically (the
		// parser already forces an own-line break around any comment), so
		// we reshape only programmatic bodies here.
		if endsWithLineComment(d) || hasLeadingDocComment(d) {
			anyComment = true
		}
	}
	// The "authored compact body" heuristic only makes sense for a struct
	// body, which can legitimately render inline as `{a: 1, b: 2}`. The
	// file's top-level decls are never comma-joined, so a Blank/NoSpace
	// RelPos there (e.g. an EmbedDecl whose ast.NewStruct value carries a
	// NoSpace Lbrace) is not an inline hint - honouring it would skip the
	// blank-line-after-import (A2) and one-decl-per-line (B1) rules and
	// leave the converter to emit `import "x", {...}`.
	if anyInlineHint && !anyComment && !isFile {
		return false
	}
	// A comprehension body (alwaysOpenFirst) breaks after the opening
	// brace even when it has a single element, unlike a plain struct
	// body, which keeps `{x: 1}` inline. The anyInlineHint check above
	// still applies, so we leave an authored compact body alone - this
	// only affects programmatic bodies.
	openFirst := isStructBody && (alwaysOpenFirst || len(decls) >= 2)

	var prev ast.Decl
	for _, curr := range decls {
		if prev == nil {
			// First decl. Inside a multi-element StructLit body it gets
			// Newline so the opener brace is followed by a line break;
			// otherwise we leave it alone, since its leading RelPos
			// describes spacing from BOF or leading file-level comments.
			//
			// A leading doc comment on the first element also forces the
			// open break, even when openFirst wouldn't otherwise fire (a
			// single-element or programmatic struct): a `//` comment must
			// sit on its own line, so the opener cannot share it. We scope
			// this to struct bodies (!isFile) - a file's first decl sits at
			// BOF with no opener to break after.
			if openFirst || (!isFile && hasLeadingDocComment(curr)) {
				w.upgradeLeading(curr, token.Newline)
				openFirstFired = true
			}
			prev = curr
			continue
		}
		target := bodyTarget(prev, curr)
		w.upgradeLeading(curr, target)
		prev = curr
	}
	return openFirstFired
}

// bodyTarget returns the strongest target RelPos any rule in Group A or
// Group B (except B3) prescribes for curr given its predecessor prev.
// The returned value is what the upgrade aims for; the upgrade itself is
// conditional on the existing RelPos being weaker (see
// [walker.upgradeLeading]).
func bodyTarget(prev, curr ast.Decl) token.RelPos {
	// Start from B1 (one decl per line). B2's targets all coincide with
	// B1's Newline, so they are subsumed; we keep the switch for
	// documentation and future divergence.
	target := token.Newline
	switch curr.(type) {
	case *ast.LetClause, *ast.EmbedDecl, *ast.Comprehension, *ast.Alias:
		// B2: explicit floor of Newline.
	}

	// A4: doc comments on curr promote to NewSection when prev is a
	// Definition Field or any non-Field decl that is not itself a
	// CommentGroup (the CommentGroup case falls under A3 below).
	if pretty.HasDocComment(curr) {
		switch p := prev.(type) {
		case *ast.Field:
			if internal.IsDefinition(p.Label) {
				target = max(target, token.NewSection)
			}
		case *ast.CommentGroup:
			// A3 covers this below.
		default:
			target = max(target, token.NewSection)
		}
	}

	// A1 / A2 / A3: NewSection after Package, ImportDecl, or a
	// standalone CommentGroup. These dominate B1/B2.
	switch prev.(type) {
	case *ast.Package, *ast.ImportDecl, *ast.CommentGroup:
		target = max(target, token.NewSection)
	}

	return target
}

// annotateImport applies B3 to an ImportDecl with a valid Lparen: its
// closing Rparen sits on its own line. We leave single-line, paren-less
// imports (`import "foo"`) untouched.
func (w *walker) annotateImport(d *ast.ImportDecl) {
	// A single-spec ImportDecl without Lparen renders as the compact
	// `import "X"` form and never grows parens, so neither rule below
	// applies. Multi-spec ImportDecls always render parenthesised (the
	// renderer synthesises the parens when Lparen is unset), so we treat
	// them as parenthesised for layout purposes whether or not the AST
	// already carries a Lparen position.
	if !d.Lparen.IsValid() && len(d.Specs) <= 1 {
		return
	}
	// Rparen on its own line (B3).
	if d.Rparen.RelPos() < token.Newline {
		d.Rparen = d.Rparen.WithRel(token.Newline)
		w.changed = true
	}
	// When the import group holds more than one spec, we place every spec
	// on its own line. We leave a single-spec parenthesised import alone
	// so it can render flat as `import ("foo")` if RelPos hints don't push
	// it onto multiple lines.
	if len(d.Specs) > 1 {
		for _, s := range d.Specs {
			w.upgradeLeading(s, token.Newline)
		}
	}
}

// upgradeLeading sets the leading RelPos of n to target if and only if
// target is stronger than the currently effective leading RelPos. The
// "effective" position here is the one [pretty.LeadingRelPos] reads:
// the doc-comment Slash when a doc comment is present, otherwise the
// node's own Pos.
func (w *walker) upgradeLeading(n ast.Node, target token.RelPos) {
	if target == 0 {
		return
	}
	if pretty.LeadingRelPos(n) >= target {
		return
	}
	if cg := pretty.FirstCommentAt(n, pretty.PosDoc); cg != nil {
		setCommentRelPos(cg, target)
	} else if s := leadingBracelessStruct(n); s != nil {
		// The leading token resolves into a braceless struct's first
		// element. We materialise the absent opening brace and put the
		// RelPos there, so the renderer reads it as "break before the
		// struct" rather than "break after the opening brace". A
		// Blank/NoSpace opener would never reach here: annotateBody's
		// anyInlineHint guard catches it first.
		s.Lbrace = token.NoPos.WithRel(target)
	} else {
		ast.SetRelPos(n, target)
	}
	w.changed = true
}

// leadingBracelessStruct returns the braceless [*ast.StructLit] that
// sits at n's leading edge - the struct whose elided opening brace is
// the token an [ast.SetRelPos] on n would otherwise target - or nil
// when n's leading token is a real token (a brace, an identifier, an
// operator) that can carry the RelPos directly.
//
// A node's leading position is transparent through wrappers that have no
// token of their own: an [*ast.EmbedDecl] reports its Expr's position
// and an [*ast.BinaryExpr] its left operand's. When that leading operand
// is a braceless struct it has no `{`, so [ast.StructLit.Pos] falls
// through to its first element and a leading RelPos written via
// [ast.SetRelPos] would land inside the struct ("break after the opening
// brace") instead of before it. Returning the struct lets the RelPos
// attach to a materialised brace instead.
func leadingBracelessStruct(n ast.Node) *ast.StructLit {
	for {
		switch x := n.(type) {
		case *ast.EmbedDecl:
			n = x.Expr
		case *ast.BinaryExpr:
			n = x.X
		case *ast.StructLit:
			if x.Lbrace.IsValid() {
				return nil
			}
			return x
		default:
			return nil
		}
	}
}

// hasLeadingDocComment reports whether d renders with a leading
// doc-position comment. The comment may sit on d itself or, for an
// EmbedDecl, on the embedded expression (the parser and AST builders
// attach an embed's doc comment to its Expr, not to the EmbedDecl),
// in which case it still renders before the embed.
func hasLeadingDocComment(d ast.Decl) bool {
	if pretty.HasDocComment(d) {
		return true
	}
	if ed, ok := d.(*ast.EmbedDecl); ok {
		return pretty.HasDocComment(ed.Expr)
	}
	return false
}

// mergeDocComments canonicalises a node that carries more than one
// doc-position (Position 0, Doc) comment group into a single group,
// joining the originals with an empty `//` line. It reports whether it
// changed anything. The merged group keeps the first group's leading
// position. [*ast.File] and [*ast.Package] are exempt and return
// false (see below).
//
// Joining with a `//` line keeps both blocks as doc comments, visually
// separated, and makes the output reparse to exactly this shape:
// rendering the groups adjacent would fold them into one group on
// reparse, and blank-separating them would detach the first to the
// enclosing scope. File and package leading comments are exempt
// because they sit at the top of the file with nothing to detach to,
// so they round-trip fine blank-separated and must keep their section
// breaks.
func mergeDocComments(n ast.Node) bool {
	switch n.(type) {
	case *ast.File, *ast.Package:
		return false
	}
	all := ast.Comments(n)
	var docs []*ast.CommentGroup
	for _, cg := range all {
		if cg.Position == pretty.PosDoc && cg.Doc {
			docs = append(docs, cg)
		}
	}
	if len(docs) < 2 {
		return false
	}

	var list []*ast.Comment
	for i, cg := range docs {
		if i > 0 {
			list = append(list, &ast.Comment{
				Slash: token.NoPos.WithRel(token.Newline),
				Text:  "//",
			})
		}
		list = append(list, cg.List...)
	}
	merged := &ast.CommentGroup{Doc: true, Position: 0, List: list}

	rebuilt := make([]*ast.CommentGroup, 0, len(all))
	inserted := false
	for _, cg := range all {
		if cg.Position == pretty.PosDoc && cg.Doc {
			if !inserted {
				rebuilt = append(rebuilt, merged)
				inserted = true
			}
			continue
		}
		rebuilt = append(rebuilt, cg)
	}
	ast.SetComments(n, rebuilt)
	return true
}

// sectionSeparateDocComments is the [*ast.File] / [*ast.Package]
// counterpart to [mergeDocComments]: rather than folding multiple
// doc-position comment groups together, it keeps them apart with blank
// lines, promoting every group after the first to NewSection so the
// section breaks between top-level blocks (license header, package
// banner, package doc) are restored. It never weakens an existing
// RelPos and reports whether it changed anything. Other node types
// return false.
func sectionSeparateDocComments(n ast.Node) bool {
	switch n.(type) {
	case *ast.File, *ast.Package:
	default:
		return false
	}
	changed := false
	seen := false
	for _, cg := range ast.Comments(n) {
		if cg.Position != pretty.PosDoc || !cg.Doc {
			continue
		}
		if seen && cg.Pos().RelPos() < token.NewSection {
			setCommentRelPos(cg, token.NewSection)
			changed = true
		}
		seen = true
	}
	return changed
}

// endsWithLineComment reports whether n's rendered output ends with a
// `//` comment. CUE has only `//` line comments, which run to
// end-of-line, so a node that ends with one cannot be followed on the
// same line by another token. The check follows the rightmost-rendered
// child of non-bracketed nodes.
//
// This is an independent approximation of the pretty converter's
// endsWithLineComment flag; the two predicates differ at the edges and
// need not agree. Either miss direction is safe: a false negative
// leaves a compact body alone (the converter's declSep still breaks
// the separator when the previous decl ends with a `//`), and a false
// positive merely annotates the body one-decl-per-line.
func endsWithLineComment(n ast.Node) bool {
	if hasTrailingComment(n) {
		return true
	}
	if c := rightmostChild(n); c != nil {
		return endsWithLineComment(c)
	}
	return false
}

// hasTrailingComment reports whether n carries a comment that renders
// after its content: a trailing-position comment (Position >=
// [pretty.PosTrailingMin]) or a same-line comment that is not a doc
// comment.
func hasTrailingComment(n ast.Node) bool {
	for _, cg := range ast.Comments(n) {
		if cg.Position >= pretty.PosTrailingMin {
			return true
		}
		if cg.Line && cg.Position != pretty.PosDoc {
			return true
		}
	}
	return false
}

// rightmostChild returns the sub-node whose rendered output forms the
// tail of n, or nil when n ends with a closing bracket or is a leaf.
// The set mirrors the nodes the pretty converter propagates a
// trailing-comment tail through; bracketed nodes (StructLit, ListLit,
// CallExpr, ...) are deliberately omitted because their closing bracket
// terminates the output.
func rightmostChild(n ast.Node) ast.Node {
	switch x := n.(type) {
	case *ast.Field:
		return x.Value
	case *ast.EmbedDecl:
		return x.Expr
	case *ast.Alias:
		return x.Expr
	case *ast.LetClause:
		return x.Expr
	case *ast.BinaryExpr:
		return x.Y
	case *ast.UnaryExpr:
		return x.X
	}
	return nil
}

// setCommentRelPos updates the Slash position of a CommentGroup's
// first comment so that its effective RelPos becomes rel. A
// CommentGroup itself has no separate position; its Pos derives from
// its first comment's Slash.
func setCommentRelPos(cg *ast.CommentGroup, rel token.RelPos) {
	if len(cg.List) == 0 {
		return
	}
	cg.List[0].Slash = cg.List[0].Slash.WithRel(rel)
}
