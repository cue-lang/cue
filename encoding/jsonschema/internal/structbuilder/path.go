// Copyright 2024 CUE Authors
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

package structbuilder

import (
	"fmt"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// SelectorForLabel is like [cue.Label] except that it allows
// hidden fields, which aren't allowed there because technically
// we can't work out what package to associate with the resulting
// selector. In our case we always imply the local package so
// we don't mind about that.
func SelectorForLabel(label ast.Label) cue.Selector {
	if label, _ := label.(*ast.Ident); label != nil && strings.HasPrefix(label.Name, "_") {
		return cue.Hid(label.Name, "_")
	}
	return cue.Label(label)
}

// PathRefSyntax returns the syntax for an expression which
// looks up the path inside the given root expression's value.
// It returns an error if the path contains any elements with
// type [cue.OptionalConstraint], [cue.RequiredConstraint], or [cue.PatternConstraint],
// none of which are expressible as a CUE index expression.
//
// TODO implement this properly and move to a method on [cue.Path].
func PathRefSyntax(cuePath cue.Path, root ast.Expr) (ast.Expr, error) {
	expr := root
	for _, sel := range cuePath.Selectors() {
		if sel.LabelType() == cue.IndexLabel {
			expr = &ast.IndexExpr{
				X: expr,
				Index: &ast.BasicLit{
					Kind:  token.INT,
					Value: sel.String(),
				},
			}
		} else {
			lab, err := labelForSelector(sel)
			if err != nil {
				return nil, err
			}
			expr = &ast.SelectorExpr{
				X:   expr,
				Sel: lab,
			}
		}
	}
	return expr, nil
}

// exprAtPath returns an expression that places the given
// expression at the given path.
// For example:
//
//	exprAtPath(cue.ParsePath("a.b.#c"), ast.NewIdent("foo"))
//
// would result in the declaration:
//
//	a: b: #c: foo
//
// TODO this is potentially generally useful. It could
// be exposed as a method on [cue.Path], say
// `SyntaxForDefinition` or something.
func exprAtPath(path cue.Path, expr ast.Expr) (ast.Expr, error) {
	for i, sel := range slices.Backward(path.Selectors()) {
		label, err := labelForSelector(sel)
		if err != nil {
			return nil, err
		}
		// A StructLit is inlined if both:
		// - the Lbrace position is invalid
		// - the Label position is valid.
		rel := token.Blank
		if i == 0 {
			rel = token.Newline
		}
		ast.SetPos(label, token.NoPos.WithRel(rel))
		expr = &ast.StructLit{
			Elts: []ast.Decl{
				&ast.Field{
					Label: label,
					Value: expr,
				},
			},
		}
	}
	return expr, nil
}

// TODO define this as a Label method on cue.Selector?
func labelForSelector(sel cue.Selector) (ast.Label, error) {
	switch sel.LabelType() {
	case cue.StringLabel, cue.DefinitionLabel, cue.HiddenLabel, cue.HiddenDefinitionLabel:
		str := sel.String()
		switch {
		case strings.HasPrefix(str, `"`):
			// It's quoted for a reason, so maintain the quotes.
			return &ast.BasicLit{
				Kind:  token.STRING,
				Value: str,
			}, nil
		case ast.IsValidIdent(str):
			return ast.NewIdent(str), nil
		}
		// Should never happen.
		return nil, fmt.Errorf("cannot form expression for selector %q", sel)
	default:
		return nil, fmt.Errorf("cannot form label for selector %q with type %v", sel, sel.LabelType())
	}
}
