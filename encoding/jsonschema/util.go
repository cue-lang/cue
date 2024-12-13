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

package jsonschema

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// selectorForLabel is like [cue.Label] except that it allows
// hidden fields.
func selectorForLabel(label ast.Label) (cue.Selector, error) {
	switch label := label.(type) {
	case *ast.Ident:
		switch {
		case strings.HasPrefix(label.Name, "_"):
			return cue.Hid(label.Name, "_"), nil
		case strings.HasPrefix(label.Name, "#"):
			return cue.Def(label.Name), nil
		default:
			return cue.Str(label.Name), nil
		}
	case *ast.BasicLit:
		if label.Kind != token.STRING {
			return cue.Selector{}, fmt.Errorf("cannot make selector for %v", label.Kind)
		}
		info, _, _, _ := literal.ParseQuotes(label.Value, label.Value)
		if !info.IsDouble() {
			return cue.Selector{}, fmt.Errorf("invalid string label %s", label.Value)
		}
		s, err := literal.Unquote(label.Value)
		if err != nil {
			return cue.Selector{}, fmt.Errorf("invalid string label %s: %v", label.Value, err)
		}
		return cue.Str(s), nil
	default:
		return cue.Selector{}, fmt.Errorf("invalid label type %T", label)
	}
}

// pathRefSyntax returns the syntax for an expression which
// looks up the path inside the given root expression's value.
// It returns an error if the path contains any elements with
// type [cue.OptionalConstraint], [cue.RequiredConstraint], or [cue.PatternConstraint],
// none of which are expressible as a CUE index expression.
//
// TODO implement this properly and move to a method on [cue.Path].
func pathRefSyntax(cuePath cue.Path, root ast.Expr) (ast.Expr, error) {
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
//	declAtPath(cue.ParsePath("a.b.#c"), ast.NewIdent("foo"))
//
// would result in the declaration:
//
//	a: b: #c: foo
func exprAtPath(path cue.Path, expr ast.Expr) (ast.Expr, error) {
	sels := path.Selectors()
	for i := len(sels) - 1; i >= 0; i-- {
		sel := sels[i]
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
