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
// conventional style. Each rewrite is gated by a flag on [Config], so
// callers can pick the subset they want.
//
// All flags default to false; [Config.Annotate] with the zero value
// returns false and leaves the AST untouched.
//
// # Flags
//
// [Config.RelPos] applies the layout heuristics catalogued below.
// These are the "blank line after Package", "one decl per line", and
// related rules: they only set RelPos on existing nodes; never alter
// structure.
//
// [Config.InlineStructs] strips the Lbrace / Rbrace of single-field
// StructLit values of Fields when chain-collapse would be safe (no
// attrs or comments would be lost or misplaced).
//
// [Config.Labels] rewrites string labels to identifier labels where
// no reference in the same scope would bind to a different value. A
// label like `"foo"` becomes `foo` if exposing it as an identifier
// preserves the semantics.
//
// [Config.Ellipsis] defers and merges `...` / `[string]: _` / `[_]:
// _` patterns within each struct body so multiple equivalent "open"
// markers collapse to a single trailing `...`.
//
// # RelPos heuristics
//
// The RelPos pass applies its rules to every body of declarations -
// the [*ast.File]'s Decls list and every [*ast.StructLit]'s Elts list
// found anywhere in the tree. It examines successive (prev, curr)
// pairs and "upgrades" curr's leading RelPos to the strongest
// applicable target. "Leading RelPos" is the first doc-position
// comment's Slash RelPos if curr has one, otherwise curr's own Pos
// RelPos - see [leadingRelPos].
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
//   - B1: every curr (default), so each decl starts on its own line.
//   - B2: curr is a [*ast.LetClause], [*ast.EmbedDecl],
//     [*ast.Comprehension], or [*ast.Alias]. These decl types
//     always start on their own line. (In practice their target
//     coincides with B1's default, but the rule is kept here so
//     future changes - e.g. promoting them to NewSection - are
//     local.)
//   - B3: for an [*ast.ImportDecl] with a valid Lparen, the closing
//     Rparen sits on its own line. When such an import group holds
//     two or more specs, each spec is placed on its own line too;
//     a single-spec parenthesised import is left alone so it may
//     render compactly as `import ("foo")`.
//
// The strongest applicable rule wins. The RelPos pass never weakens
// an existing RelPos: if curr's leading position already carries
// NewSection, a Newline-targeting rule leaves it alone.
//
// # Order
//
// When multiple flags are enabled in one [Config.Annotate] call,
// the rewrites run in this order:
//
//  1. Labels: BasicLit labels become Idents where safe.
//  2. Ellipsis: open-marker patterns collapse to a trailing `...`.
//  3. InlineStructs: synthesised braces are stripped from
//     single-Field StructLit values.
//  4. RelPos: layout RelPos hints are set on inter-decl positions.
//
// The order matters because each later pass would otherwise see a
// stale view of the body: e.g. RelPos's pair-iteration depends on the
// final decl list after Ellipsis has merged and InlineStructs has
// potentially exposed new chain shapes.
package style

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// Config selects which house-style transformations [Config.Annotate]
// applies. The zero value is a no-op. The fields are independent and
// can be enabled in any combination.
type Config struct {
	// RelPos applies the layout heuristics described in the package
	// docs - blank lines after Package and ImportDecl, one decl per
	// line, and so on. It only sets RelPos on existing nodes and never
	// alters structure.
	RelPos bool

	// InlineStructs strips the Lbrace / Rbrace of a single-Field
	// StructLit value of a Field when the chain shape `outer: inner`
	// would not lose any side data.
	//
	// A struct is eligible to be inlined only when all of the
	// following hold:
	//
	//   - the outer node is an [*ast.Field] (the chain is only valid
	//     at a field-value position; CallExpr arguments, ListLit
	//     elements, and so on need their braces);
	//   - the StructLit has exactly one element, and that element is
	//     itself an [*ast.Field];
	//   - the outer Field has no attributes (chain-form would attach
	//     them to the leaf instead of the outer);
	//   - the inner Field has no attributes (same risk in the other
	//     direction: fieldRow uses leaf.Attrs only);
	//   - the inner Field has no comments (its doc comment would
	//     render between outer_key: and inner_key: rather than above
	//     the host);
	//   - the StructLit itself has no comments (no chain-form
	//     equivalent for them).
	//
	// The pass recurses: after stripping the outer's braces, it looks
	// at the inner Field's value and may strip again.
	InlineStructs bool

	// Labels rewrites string labels to identifier labels where the
	// identifier would not collide with any in-scope reference.
	Labels bool

	// Ellipsis defers and merges `...` / `[string]: _` / `[_]: _`
	// patterns within each struct body. The intermediate patterns are
	// removed and a single fresh [*ast.Ellipsis] is appended at the
	// end of the body, carrying the comments of the last removed
	// marker.
	Ellipsis bool
}

// Annotate applies the transformations selected by cfg to n in
// place. Returns true if any change was made. With the zero Config,
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
// and RelPos passes. visit dispatches to the per-node hooks for each
// enabled flag.
type walker struct {
	cfg     Config
	changed bool
}

func (w *walker) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.File:
		if w.cfg.Ellipsis {
			if mergeEllipsisDecls(&n.Decls) {
				w.changed = true
			}
		}
		if w.cfg.RelPos {
			w.annotateBody(n.Decls, false)
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
			// which makes [ast.StructLit.Pos] fall back to
			// Elts[0].Pos(); upgrading the first elt would then change
			// the effective leading position of the enclosing field as
			// well - never the intent of the rule, which is scoped to
			// the bracket boundary.
			if w.annotateBody(n.Elts, n.Lbrace.IsValid()) {
				// The first-elt upgrade fired, so this struct body breaks
				// vertically. Promote the closing brace's RelPos to at
				// least Newline so it lands on its own line and matches
				// the opener.
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
	}
	return true
}

// annotateBody applies the RelPos inter-decl rules (Group A and Group
// B, except B3) to a slice of declarations. The first non-elided decl
// is the anchor; for every later non-elided decl we compute the
// target RelPos and upgrade if it is stronger than the existing
// leading RelPos.
//
// When isStructBody is true (the body is the Elts of a
// [*ast.StructLit], not the Decls of a [*ast.File]) and the body has
// two or more non-elided decls and no decl carries an explicit
// "stay inline" hint (Blank / NoSpace RelPos), the first non-elided
// decl also gets a Newline target so the body breaks across lines
// after the opening brace. A Blank/NoSpace on any decl signals that
// the builder placed the decl on the same line as its predecessor
// (e.g. a comma-separated body like `{a: 1, b: 2}`), so the
// openFirst upgrade is suppressed and the compact shape is
// preserved. File bodies are excluded because their first decl's
// leading RelPos describes placement relative to BOF / leading
// file-level comments, not relative to an enclosing bracket.
// Single-element struct bodies are also excluded so single-field
// chain shapes (a: b: c) and single-element hug (`[{...}]`) still
// fire. Returns whether the first-elt Newline upgrade fired, so the
// StructLit caller knows it also needs to break before the closing
// `}`.
func (w *walker) annotateBody(decls []ast.Decl, isStructBody bool) (openFirstFired bool) {
	visibleCount := 0
	anyInlineHint := false
	for _, d := range decls {
		if d.Pos().RelPos() == token.Elided {
			continue
		}
		visibleCount++
		// An explicit Blank/NoSpace RelPos on a decl is the AST
		// builder's "stay inline" signal: it means the decl was
		// authored on the same line as its predecessor (e.g. a
		// comma-separated body like `{a: 1, b: 2}`). When any decl
		// carries that hint, the body is taken to be authored
		// compact and we leave inter-decl RelPos alone for the
		// whole body - openFirst stays false, and the B1/B2/A4
		// upgrades below are also skipped.
		if r := leadingRelPos(d); r == token.Blank || r == token.NoSpace {
			anyInlineHint = true
		}
	}
	if anyInlineHint {
		return false
	}
	openFirst := isStructBody && visibleCount >= 2

	var prev ast.Decl
	for _, curr := range decls {
		if curr.Pos().RelPos() == token.Elided {
			continue
		}
		if prev == nil {
			// First non-elided decl. Inside a multi-element StructLit
			// body it gets Newline so the opener brace is followed by a
			// line break; otherwise it is left alone (its leading RelPos
			// describes spacing from BOF or leading file-level
			// comments).
			if openFirst {
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

// bodyTarget returns the strongest target RelPos any rule in Group A
// or Group B (except B3) prescribes for curr given its predecessor
// prev. The returned value is the value the upgrade aims for; the
// upgrade itself is conditional on the existing RelPos being weaker
// (see [walker.upgradeLeading]).
func bodyTarget(prev, curr ast.Decl) token.RelPos {
	// Start from B1 (one decl per line). B2's targets all coincide
	// with B1's Newline, so they are subsumed; the switch is kept
	// for documentation and future divergence.
	target := token.Newline
	switch curr.(type) {
	case *ast.LetClause, *ast.EmbedDecl, *ast.Comprehension, *ast.Alias:
		// B2: explicit floor of Newline.
	}

	// A4: doc comments on curr promote to NewSection when prev is a
	// Definition Field or any non-Field decl that is not itself a
	// CommentGroup (the CommentGroup case is covered by A3 below).
	if hasDocComment(curr) {
		switch p := prev.(type) {
		case *ast.Field:
			if internal.IsDefinition(p.Label) {
				target = max(target, token.NewSection)
			}
		case *ast.CommentGroup:
			// Covered by A3 below.
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

// annotateImport applies B3 to an ImportDecl with a valid Lparen:
// its closing Rparen sits on its own line. Single-line, paren-less
// imports (`import "foo"`) are untouched.
func (w *walker) annotateImport(d *ast.ImportDecl) {
	// A single-spec ImportDecl without Lparen renders as the compact
	// `import "X"` form and never grows parens, so neither rule below
	// applies. Multi-spec ImportDecls always render parenthesised
	// (the renderer synthesises the parens when Lparen is unset),
	// so we treat them as parenthesised for layout purposes whether
	// or not the AST already carries a Lparen position.
	if !d.Lparen.IsValid() && len(d.Specs) <= 1 {
		return
	}
	// Rparen on its own line (B3).
	if d.Rparen.RelPos() < token.Newline {
		d.Rparen = d.Rparen.WithRel(token.Newline)
		w.changed = true
	}
	// When the import group holds more than one spec, every spec is
	// placed on its own line. A single-spec parenthesised import is
	// left alone so it can render flat `import ("foo")` if RelPos
	// hints don't push it onto multiple lines.
	if len(d.Specs) > 1 {
		for _, s := range d.Specs {
			w.upgradeLeading(s, token.Newline)
		}
	}
}

// upgradeLeading sets the leading RelPos of n to target if and only
// if target is stronger than the currently effective leading RelPos.
// "Effective" here means the position [leadingRelPos] reads:
// doc-comment Slash when a doc comment is present, otherwise the
// node's own Pos.
func (w *walker) upgradeLeading(n ast.Node, target token.RelPos) {
	if target == 0 {
		return
	}
	if leadingRelPos(n) >= target {
		return
	}
	if cg := firstDocComment(n); cg != nil {
		setCommentRelPos(cg, target)
	} else {
		ast.SetRelPos(n, target)
	}
	w.changed = true
}

// leadingRelPos returns the effective leading RelPos of n: the first
// doc comment's Slash RelPos when present, otherwise
// n.Pos().RelPos(). A leading doc comment renders before the host
// node, so its Slash position governs the inter-decl spacing rather
// than the host's own Pos.
func leadingRelPos(n ast.Node) token.RelPos {
	if cg := firstDocComment(n); cg != nil {
		return cg.Pos().RelPos()
	}
	return n.Pos().RelPos()
}

// firstDocComment returns the first doc-position (Position==0)
// comment attached to n, or nil.
func firstDocComment(n ast.Node) *ast.CommentGroup {
	for _, cg := range ast.Comments(n) {
		if cg.Position == 0 {
			return cg
		}
	}
	return nil
}

// hasDocComment reports whether n carries any doc-position comment.
func hasDocComment(n ast.Node) bool {
	return firstDocComment(n) != nil
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
