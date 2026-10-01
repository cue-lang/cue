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

package format

import (
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// simplifyLabels rewrites string labels to identifier labels where the
// identifier would not collide with any in-scope reference. Nested
// struct bodies form child scopes that inherit candidates from their
// parents. Single-string pattern constraints are rewritten as optional
// fields first; see [labelSimplifier.simplifyPatternLabel].
//
// This backs both the [ASTStyle] Labels flag and the [Simplify] option
// of the pre-v2 formatter, which drives it from [printNode] through a
// walker that allows every change.
func (w *walker) simplifyLabels(n ast.Node) {
	ls := &labelSimplifier{scope: map[string]bool{}, walker: w}
	ls.markReferences(n)
}

// labelSimplifier tracks, per scope, a map from the names of quoted
// labels to whether they are still eligible for unquoting (true means no
// reference observed yet).
type labelSimplifier struct {
	parent *labelSimplifier
	scope  map[string]bool

	// bound holds the names which identifier labels bind in this scope.
	// Quoted labels do not bind references.
	bound map[string]bool

	// walker is shared by every scope of one simplification run; it
	// holds the permission to rewrite a label.
	walker *walker
}

// newLabelSimplifier returns the root scope of a simplification run
// that may make every rewrite it finds. The pre-v2 formatter drives the
// simplifier this way, outside of any [ASTStyle] pass.
func newLabelSimplifier() *labelSimplifier {
	return &labelSimplifier{scope: map[string]bool{}, walker: &walker{allowChanges: true}}
}

// markReferences is the [ast.Walk]-compatible reference visitor, and
// also the entry point. For File and StructLit we delegate to
// [labelSimplifier.processDecls] and return false to stop the outer
// walk over that body.
func (s *labelSimplifier) markReferences(n ast.Node) bool {
	if s.walker.stopped {
		return false
	}
	switch x := n.(type) {
	case *ast.File:
		s.processDecls(x.Decls)
		return false

	case *ast.StructLit:
		s.processDecls(x.Elts)
		return false

	case *ast.SelectorExpr:
		// We treat only the receiver (X) as a reference; the selector
		// (Sel) is just a member name and doesn't bind to any
		// enclosing scope.
		ast.Walk(x.X, s.markReferences, nil)
		return false

	case *ast.Ident:
		s.markReference(x)
	}
	return true
}

// markLabelReferences is the reference visitor for label expressions
// such as `(x)`, `[x]`, or `"\(x)"`, which resolve references in the
// scope of the field they label. Unlike [labelSimplifier.markReferences],
// it does not process any struct within the label as a scope of its own,
// so it leaves the struct's labels alone and treats each identifier in it
// as a reference.
func (s *labelSimplifier) markLabelReferences(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		ast.Walk(x.X, s.markLabelReferences, nil)
		return false

	case *ast.Ident:
		s.markReference(x)
	}
	return true
}

// markReference invalidates the candidates for a referenced name. We
// walk outwards through enclosing scopes and invalidate the candidate in
// each scope up to the innermost one binding this name, as the reference
// resolves past any quoted labels. Outer scopes that happen to also use
// the name stay valid (shadowing semantics).
func (s *labelSimplifier) markReference(x *ast.Ident) {
	for c := s; c != nil; c = c.parent {
		if _, ok := c.scope[x.Name]; ok {
			c.scope[x.Name] = false
		}
		if c.bound[x.Name] {
			break
		}
	}
}

// processDecls runs the three sub-passes we apply to one body.
func (s *labelSimplifier) processDecls(decls []ast.Decl) {
	sc := &labelSimplifier{parent: s, scope: map[string]bool{}, bound: map[string]bool{}, walker: s.walker}

	// Sub-pass 1: collect candidates and bound names from labels, after
	// rewriting single-string pattern constraints as optional fields.
	for _, d := range decls {
		switch x := d.(type) {
		case *ast.Field:
			if !s.simplifyPatternLabel(x) {
				return
			}
			ast.Walk(x.Label, sc.markStrings, nil)
		}
	}

	// Sub-pass 2: collect references from values and label expressions.
	for _, d := range decls {
		switch x := d.(type) {
		case *ast.Field:
			var label ast.Node = x.Label
			if a, ok := label.(*ast.Alias); ok {
				label = a.Expr
			}
			// An identifier label is not a reference to itself.
			if _, ok := label.(*ast.Ident); !ok {
				ast.Walk(label, sc.markLabelReferences, nil)
			}
			ast.Walk(x.Value, sc.markReferences, nil)
		default:
			ast.Walk(x, sc.markReferences, nil)
		}
	}

	// Sub-pass 3: rewrite labels whose candidate flag survived.
	for _, d := range decls {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		bl, ok := f.Label.(*ast.BasicLit)
		if !ok {
			continue
		}
		str, err := strconv.Unquote(bl.Value)
		if err != nil {
			continue
		}
		if !sc.scope[str] {
			continue
		}
		if !s.walker.tryMutate() {
			return
		}
		f.Label = &ast.Ident{NamePos: bl.ValuePos, Name: str}
	}
}

// simplifyPatternLabel rewrites a pattern constraint whose pattern is a
// single string literal, such as `["k"]: v`, as the equivalent optional
// field `"k"?: v`. A pattern like this is often a mistake for a regular
// expression match such as `[=~"k"]`, which the rewrite makes apparent.
//
// We leave aliased patterns alone, as well as those carrying comments
// which the rewrite would have nowhere to put. We report false if the
// rewrite was refused, stopping the passes.
func (s *labelSimplifier) simplifyPatternLabel(f *ast.Field) bool {
	l, ok := f.Label.(*ast.ListLit)
	if !ok || len(l.Elts) != 1 || f.Alias != nil || f.Constraint != token.ILLEGAL {
		return true
	}
	lit, ok := l.Elts[0].(*ast.BasicLit)
	if !ok || len(ast.Comments(l)) > 0 || len(ast.Comments(lit)) > 0 {
		return true
	}
	// Only allow double-quoted, single-line strings. strconv.Unquote alone
	// would also accept a single-character bytes literal like 'k'.
	if !strings.HasPrefix(lit.Value, `"`) {
		return true
	}
	if _, err := strconv.Unquote(lit.Value); err != nil {
		return true
	}
	if !s.walker.tryMutate() {
		return false
	}
	lit.ValuePos = l.Lbrack
	f.Label = lit
	f.Constraint = token.OPTION
	return true
}

// markStrings walks a label subtree, recording every unquotable string
// as a candidate for the current scope, and every identifier as a name
// bound by it. ListLit, Interpolation, and ParenExpr labels (pattern
// constraints, interpolated strings, dynamic labels) are not candidates,
// and the identifiers in them are references, so we stop the walk there.
func (s *labelSimplifier) markStrings(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.BasicLit:
		str, err := strconv.Unquote(x.Value)
		if err != nil || ast.StringLabelNeedsQuoting(str) {
			return false
		}
		s.scope[str] = true

	case *ast.Ident:
		s.bound[x.Name] = true

	case *ast.ListLit, *ast.Interpolation, *ast.ParenExpr:
		return false
	}
	return true
}
