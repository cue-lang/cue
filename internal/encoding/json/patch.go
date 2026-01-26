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

package json

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// PatchExpr simplifies the AST parsed from JSON.
// TODO: some of the modifications are already done in format, but are
// a package deal of a more aggressive simplify. Other pieces of modification
// should probably be moved to format.
func PatchExpr(n ast.Node, patchPos func(n ast.Node)) {
	type info struct {
		reflow bool
	}
	stack := []info{{true}}

	afterFn := func(n ast.Node) {
		switch n.(type) {
		case *ast.ListLit, *ast.StructLit:
			stack = stack[:len(stack)-1]
		}
	}

	var beforeFn func(n ast.Node) bool

	beforeFn = func(n ast.Node) bool {
		if patchPos != nil {
			patchPos(n)
		}

		isLarge := n.End().Offset()-n.Pos().Offset() > 50
		descent := true

		switch x := n.(type) {
		case *ast.ListLit:
			reflow := true
			if !isLarge {
				for _, e := range x.Elts {
					if hasSpaces(e) {
						reflow = false
						break
					}
				}
			}
			stack = append(stack, info{reflow})
			if reflow {
				x.Lbrack = x.Lbrack.WithRel(token.NoRelPos)
				x.Rbrack = x.Rbrack.WithRel(token.NoRelPos)
			}
			return true

		case *ast.StructLit:
			reflow := true
			if !isLarge {
				for _, e := range x.Elts {
					if f, ok := e.(*ast.Field); !ok || hasSpaces(f) || hasSpaces(f.Value) {
						reflow = false
						break
					}
				}
			}
			stack = append(stack, info{reflow})
			if reflow {
				x.Lbrace = x.Lbrace.WithRel(token.NoRelPos)
				x.Rbrace = x.Rbrace.WithRel(token.NoRelPos)
			}
			return true

		case *ast.Field:
			// label is always a string for JSON.
			s, ok := x.Label.(*ast.BasicLit)
			if !ok || s.Kind != token.STRING {
				break // should not happen: implies invalid JSON
			}

			u, err := literal.Unquote(s.Value)
			if err != nil {
				break // should not happen: implies invalid JSON
			}

			// TODO(legacy): remove checking for '_' prefix once hidden
			// fields are removed.
			if ast.StringLabelNeedsQuoting(u) {
				break // keep string
			}

			x.Label = ast.NewIdent(u)
			astutil.CopyMeta(x.Label, s)
			// Having removed the quote-marks, the ident start should be
			// incremented by 1 so that the label content matches up with
			// the raw json.
			ast.SetPos(x.Label, x.Label.Pos().Add(1))

			ast.Walk(x.Value, beforeFn, afterFn)
			descent = false

		case *ast.BasicLit:
			if x.Kind == token.STRING && len(x.Value) > 10 {
				s, err := literal.Unquote(x.Value)
				if err != nil {
					break // should not happen: implies invalid JSON
				}

				x.Value = literal.String.WithOptionalTabIndent(len(stack)).Quote(s)
			}
		}

		if stack[len(stack)-1].reflow {
			ast.SetRelPos(n, token.NoRelPos)
		}
		return descent
	}

	ast.Walk(n, beforeFn, afterFn)
}

func hasSpaces(n ast.Node) bool {
	return n.Pos().RelPos() > token.NoSpace
}
