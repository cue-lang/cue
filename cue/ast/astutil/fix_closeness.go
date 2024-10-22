// Copyright 2020 CUE Authors
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

package astutil

import (
	"cuelang.org/go/cue/ast"
)

// TagCloseStruct is a sentinel attribute indicating that a related struct should be closed.
const TagCloseStruct = "@cueinternal(close)"

// FixCloseness updates the AST to close the structs and
// remove redundant ellipses according to the [TagCloseStruct].
func FixCloseness(f *ast.File, isClosed func(ast.Label) bool) *ast.File {
	sc := closenessFixer{}
	return sc.fix(f, isClosed)
}

type fixerFrame struct {
	inOpenStruct bool
	shouldClose  bool
}

type closenessFixer struct {
	stack   []fixerFrame
	current fixerFrame
}

func (sc *closenessFixer) pushStack() {
	sc.stack = append(sc.stack, sc.current)
	sc.current.shouldClose = false
}

func (sc *closenessFixer) popStack() {
	sc.current = sc.stack[len(sc.stack)-1]
	sc.stack = sc.stack[:len(sc.stack)-1]
}

func containsTagCloseStruct(elts []ast.Decl) bool {
	for _, elt := range elts {
		if t, ok := elt.(*ast.Attribute); ok && t.Text == TagCloseStruct {
			return true
		}
	}
	return false
}

func (sc *closenessFixer) fix(f *ast.File, isClosed func(ast.Label) bool) *ast.File {
	sc.current = fixerFrame{
		inOpenStruct: true,
		shouldClose:  false,
	}

	sc.pushStack()
	if containsTagCloseStruct(f.Decls) {
		sc.current.shouldClose = true
		sc.current.inOpenStruct = false
	}

	f = Apply(f, func(c Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Field:
			sc.pushStack()
			sc.current.inOpenStruct = sc.current.inOpenStruct && !isClosed(n.Label)
		case *ast.CallExpr:
			sc.pushStack()
			sc.current.inOpenStruct = true
		case *ast.StructLit:
			sc.pushStack()
			if containsTagCloseStruct(n.Elts) {
				sc.current.shouldClose = true
				sc.current.inOpenStruct = false
			}
		case *ast.Ellipsis:
			parent := c.Parent()
			if parent != nil {
				switch parent.Node().(type) {
				case *ast.StructLit, *ast.File:
					if sc.current.shouldClose || sc.current.inOpenStruct {
						c.Delete()
					}
				}
			}
		}
		return true
	}, func(c Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Attribute:
			if n.Text == TagCloseStruct {
				c.Delete()
			}
		case *ast.Field, *ast.CallExpr:
			sc.popStack()
		case *ast.StructLit:
			shouldClose := sc.current.shouldClose
			sc.popStack()

			if sc.current.inOpenStruct && shouldClose {
				c.Replace(ast.NewCall(ast.NewIdent("close"), n))
			}
		}
		return true
	}).(*ast.File)

	shouldClose := sc.current.shouldClose
	sc.popStack()

	if sc.current.inOpenStruct && shouldClose {
		imports := []ast.Decl{}
		rest := make([]ast.Decl, 0, len(f.Decls))
		for _, decl := range f.Decls {
			if _, ok := decl.(*ast.ImportDecl); ok {
				imports = append(imports, decl)
			} else {
				rest = append(rest, decl)
			}
		}

		f.Decls = append(imports, ast.NewCall(ast.NewIdent("close"), &ast.StructLit{Elts: rest}))
	}
	return f
}
