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

package style

import (
	"strconv"

	"cuelang.org/go/cue/ast"
)

// simplifyLabels rewrites string labels to identifier labels where the
// identifier would not collide with any in-scope reference. Nested
// struct bodies form child scopes that inherit candidates from their
// parents. Returns true if we rewrote any label.
func simplifyLabels(n ast.Node) bool {
	ls := &labelSimplifier{scope: map[string]bool{}}
	ls.markReferences(n)
	return ls.changed
}

// labelSimplifier tracks, per scope, a map from candidate names to
// whether they are still eligible for unquoting (true means no
// reference observed yet).
type labelSimplifier struct {
	parent  *labelSimplifier
	scope   map[string]bool
	changed bool
}

// markReferences is the [ast.Walk]-compatible reference visitor, and
// also the entry point. For File and StructLit we delegate to
// [labelSimplifier.processDecls] and return false to stop the outer
// walk over that body.
func (s *labelSimplifier) markReferences(n ast.Node) bool {
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
		// We walk outwards through enclosing scopes and invalidate the
		// candidate in the innermost scope that has this name. Outer
		// scopes that happen to also use the name stay valid (shadowing
		// semantics).
		for c := s; c != nil; c = c.parent {
			if _, ok := c.scope[x.Name]; ok {
				c.scope[x.Name] = false
				break
			}
		}
	}
	return true
}

// processDecls runs the three sub-passes we apply to one body.
func (s *labelSimplifier) processDecls(decls []ast.Decl) {
	sc := &labelSimplifier{parent: s, scope: map[string]bool{}}

	// Sub-pass 1: collect candidates from labels.
	for _, d := range decls {
		switch x := d.(type) {
		case *ast.Field:
			ast.Walk(x.Label, sc.markStrings, nil)
		}
	}

	// Sub-pass 2: collect references from values.
	for _, d := range decls {
		switch x := d.(type) {
		case *ast.Field:
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
		f.Label = ast.NewIdent(str)
		s.changed = true
	}
}

// markStrings walks a label subtree, recording every unquotable string
// and every identifier as a candidate for the current scope. ListLit
// and Interpolation labels (pattern constraints, interpolated strings)
// are not candidates, so we stop the walk there.
func (s *labelSimplifier) markStrings(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.BasicLit:
		str, err := strconv.Unquote(x.Value)
		if err != nil || ast.StringLabelNeedsQuoting(str) {
			return false
		}
		s.scope[str] = true

	case *ast.Ident:
		s.scope[x.Name] = true

	case *ast.ListLit, *ast.Interpolation:
		return false
	}
	return true
}
