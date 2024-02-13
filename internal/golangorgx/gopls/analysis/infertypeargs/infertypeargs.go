// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package infertypeargs

import (
	"go/ast"
	"go/token"
	"go/types"

	"cuelang.org/go/internal/golangorgx/tools/typeparams"
	"cuelang.org/go/internal/golangorgx/tools/versions"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const Doc = `check for unnecessary type arguments in call expressions

Explicit type arguments may be omitted from call expressions if they can be
inferred from function arguments, or from other type arguments:

	func f[T any](T) {}
	
	func _() {
		f[string]("foo") // string could be inferred
	}
`

var Analyzer = &analysis.Analyzer{
	Name:     "infertypeargs",
	Doc:      Doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
	URL:      "https://pkg.go.dev/cuelang.org/go/internal/golangorgx/gopls/analysis/infertypeargs",
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for _, diag := range diagnose(pass.Fset, inspect, token.NoPos, token.NoPos, pass.Pkg, pass.TypesInfo) {
		pass.Report(diag)
	}
	return nil, nil
}

// Diagnose reports diagnostics describing simplifications to type
// arguments overlapping with the provided start and end position.
//
// If start or end is token.NoPos, the corresponding bound is not checked
// (i.e. if both start and end are NoPos, all call expressions are considered).
func diagnose(fset *token.FileSet, inspect *inspector.Inspector, start, end token.Pos, pkg *types.Package, info *types.Info) []analysis.Diagnostic {
	var diags []analysis.Diagnostic

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	inspect.Preorder(nodeFilter, func(node ast.Node) {
		call := node.(*ast.CallExpr)
		x, lbrack, indices, rbrack := typeparams.UnpackIndexExpr(call.Fun)
		ident := calledIdent(x)
		if ident == nil || len(indices) == 0 {
			return // no explicit args, nothing to do
		}

		if (start.IsValid() && call.End() < start) || (end.IsValid() && call.Pos() > end) {
			return // non-overlapping
		}

		// Confirm that instantiation actually occurred at this ident.
		idata, ok := info.Instances[ident]
		if !ok {
			return // something went wrong, but fail open
		}
		instance := idata.Type

		// Start removing argument expressions from the right, and check if we can
		// still infer the call expression.
		required := len(indices) // number of type expressions that are required
		for i := len(indices) - 1; i >= 0; i-- {
			var fun ast.Expr
			if i == 0 {
				// No longer an index expression: just use the parameterized operand.
				fun = x
			} else {
				fun = typeparams.PackIndexExpr(x, lbrack, indices[:i], indices[i-1].End())
			}
			newCall := &ast.CallExpr{
				Fun:      fun,
				Lparen:   call.Lparen,
				Args:     call.Args,
				Ellipsis: call.Ellipsis,
				Rparen:   call.Rparen,
			}
			info := &types.Info{
				Instances: make(map[*ast.Ident]types.Instance),
			}
			versions.InitFileVersions(info)
			if err := types.CheckExpr(fset, pkg, call.Pos(), newCall, info); err != nil {
				// Most likely inference failed.
				break
			}
			newIData := info.Instances[ident]
			newInstance := newIData.Type
			if !types.Identical(instance, newInstance) {
				// The inferred result type does not match the original result type, so
				// this simplification is not valid.
				break
			}
			required = i
		}
		if required < len(indices) {
			var s, e token.Pos
			var edit analysis.TextEdit
			if required == 0 {
				s, e = lbrack, rbrack+1 // erase the entire index
				edit = analysis.TextEdit{Pos: s, End: e}
			} else {
				s = indices[required].Pos()
				e = rbrack
				//  erase from end of last arg to include last comma & white-spaces
				edit = analysis.TextEdit{Pos: indices[required-1].End(), End: e}
			}
			// Recheck that our (narrower) fixes overlap with the requested range.
			if (start.IsValid() && e < start) || (end.IsValid() && s > end) {
				return // non-overlapping
			}
			diags = append(diags, analysis.Diagnostic{
				Pos:     s,
				End:     e,
				Message: "unnecessary type arguments",
				SuggestedFixes: []analysis.SuggestedFix{{
					Message:   "Simplify type arguments",
					TextEdits: []analysis.TextEdit{edit},
				}},
			})
		}
	})

	return diags
}

func calledIdent(x ast.Expr) *ast.Ident {
	switch x := x.(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}
	return nil
}
