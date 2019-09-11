// Copyright 2019 CUE Authors
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

package cmd

import (
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

func fix(f *ast.File) *ast.File {
	// Rewrite block comments to regular comments.
	ast.Walk(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CommentGroup:
			comments := []*ast.Comment{}
			for _, c := range x.List {
				s := c.Text
				if !strings.HasPrefix(s, "/*") || !strings.HasSuffix(s, "*/") {
					comments = append(comments, c)
					continue
				}
				if x.Position > 0 {
					// Moving to the end doesn't work, as it still
					// may inject at a false line break position.
					x.Position = 0
					x.Doc = true
				}
				s = strings.TrimSpace(s[2 : len(s)-2])
				for _, s := range strings.Split(s, "\n") {
					for i := 0; i < 3; i++ {
						if strings.HasPrefix(s, " ") || strings.HasPrefix(s, "*") {
							s = s[1:]
						}
					}
					comments = append(comments, &ast.Comment{Text: "// " + s})
				}
			}
			x.List = comments
			return false
		}
		return true
	}, nil)

	// Rewrite strings fields that are referenced.
	referred := map[ast.Node]string{}
	ast.Walk(f, func(n ast.Node) bool {
		if i, ok := n.(*ast.Ident); ok {
			str, err := ast.ParseIdent(i)
			if err != nil {
				return false
			}
			referred[i.Node] = str
		}
		return true
	}, nil)

	f = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch x := n.(type) {
		case *ast.Field:
			m, ok := referred[x.Value]
			if !ok {
				break
			}
			b, ok := x.Label.(*ast.BasicLit)
			if !ok || b.Kind != token.STRING {
				break
			}
			str, err := strconv.Unquote(b.Value)
			if err != nil || str != m {
				break
			}
			str, err = ast.QuoteIdent(str)
			if err != nil {
				return false
			}
			x.Label = astutil.CopyMeta(ast.NewIdent(str), x.Label).(ast.Label)
		}
		return true
	}, nil).(*ast.File)

	// Rewrite slice expression.
	f = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		getVal := func(n ast.Expr) ast.Expr {
			if n == nil {
				return nil
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == "_" {
				return nil
			}
			return n
		}
		switch x := n.(type) {
		case *ast.SliceExpr:
			ast.SetRelPos(x.X, token.NoRelPos)

			lo := getVal(x.Low)
			hi := getVal(x.High)
			if lo == nil { // a[:j]
				lo = mustParseExpr("0")
				astutil.CopyMeta(lo, x.Low)
			}
			if hi == nil { // a[i:]
				hi = ast.NewCall(ast.NewIdent("len"), x.X)
				astutil.CopyMeta(lo, x.High)
			}
			if pkg := c.Import("list"); pkg != nil {
				c.Replace(ast.NewCall(ast.NewSel(pkg, "Slice"), x.X, lo, hi))
			}
		}
		return true
	}, nil).(*ast.File)

	return f
}

func mustParseExpr(expr string) ast.Expr {
	ex, err := parser.ParseExpr("fix", expr)
	if err != nil {
		panic(err)
	}
	return ex
}
