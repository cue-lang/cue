// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package unusedparams

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"cuelang.org/go/internal/golangorgx/tools/analysisinternal"
)

//go:embed doc.go
var doc string

var (
	Analyzer = &analysis.Analyzer{
		Name:     "unusedparams",
		Doc:      analysisinternal.MustExtractDoc(doc, "unusedparams"),
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run:      run,
		URL:      "https://pkg.go.dev/cuelang.org/go/internal/golangorgx/gopls/analysis/unusedparams",
	}
	inspectLits     bool
	inspectWrappers bool
)

func init() {
	Analyzer.Flags.BoolVar(&inspectLits, "lits", true, "inspect function literals")
	Analyzer.Flags.BoolVar(&inspectWrappers, "wrappers", false, "inspect functions whose body consists of a single return statement")
}

type paramData struct {
	field  *ast.Field
	ident  *ast.Ident
	typObj types.Object
}

func run(pass *analysis.Pass) (interface{}, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
	}
	if inspectLits {
		nodeFilter = append(nodeFilter, (*ast.FuncLit)(nil))
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		var fieldList *ast.FieldList
		var body *ast.BlockStmt

		// Get the fieldList and body from the function node.
		switch f := n.(type) {
		case *ast.FuncDecl:
			fieldList, body = f.Type.Params, f.Body
			// TODO(golang/go#36602): add better handling for methods, if we enable methods
			// we will get false positives if a struct is potentially implementing
			// an interface.
			if f.Recv != nil {
				return
			}

			// Ignore functions in _test.go files to reduce false positives.
			if file := pass.Fset.File(n.Pos()); file != nil && strings.HasSuffix(file.Name(), "_test.go") {
				return
			}
		case *ast.FuncLit:
			fieldList, body = f.Type.Params, f.Body
		}
		// If there are no arguments or the function is empty, then return.
		if fieldList.NumFields() == 0 || body == nil || len(body.List) == 0 {
			return
		}

		switch expr := body.List[0].(type) {
		case *ast.ReturnStmt:
			if !inspectWrappers {
				// Ignore functions that only contain a return statement to reduce false positives.
				return
			}
		case *ast.ExprStmt:
			callExpr, ok := expr.X.(*ast.CallExpr)
			if !ok || len(body.List) > 1 {
				break
			}
			// Ignore functions that only contain a panic statement to reduce false positives.
			if fun, ok := callExpr.Fun.(*ast.Ident); ok && fun.Name == "panic" {
				return
			}
		}

		// Get the useful data from each field.
		params := make(map[string]*paramData)
		unused := make(map[*paramData]bool)
		for _, f := range fieldList.List {
			for _, i := range f.Names {
				if i.Name == "_" {
					continue
				}
				params[i.Name] = &paramData{
					field:  f,
					ident:  i,
					typObj: pass.TypesInfo.ObjectOf(i),
				}
				unused[params[i.Name]] = true
			}
		}

		// Traverse through the body of the function and
		// check to see which parameters are unused.
		ast.Inspect(body, func(node ast.Node) bool {
			n, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			param, ok := params[n.Name]
			if !ok {
				return false
			}
			if nObj := pass.TypesInfo.ObjectOf(n); nObj != param.typObj {
				return false
			}
			delete(unused, param)
			return false
		})

		// Create the reports for the unused parameters.
		for u := range unused {
			start, end := u.field.Pos(), u.field.End()
			if len(u.field.Names) > 1 {
				start, end = u.ident.Pos(), u.ident.End()
			}
			// TODO(golang/go#36602): Add suggested fixes to automatically
			// remove the unused parameter from every use of this
			// function.
			pass.Report(analysis.Diagnostic{
				Pos:     start,
				End:     end,
				Message: fmt.Sprintf("potentially unused parameter: '%s'", u.ident.Name),
				SuggestedFixes: []analysis.SuggestedFix{{
					Message: `Replace with "_"`,
					TextEdits: []analysis.TextEdit{{
						Pos:     u.ident.Pos(),
						End:     u.ident.End(),
						NewText: []byte("_"),
					}},
				}},
			})
		}
	})
	return nil, nil
}
