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

package internal

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
)

// SimplifyClosedness updates the AST to remove redundant ellipses and `close` calls.
func SimplifyClosedness(n ast.Node, asDef bool) ast.Node {
	sc := closednessSimplifier{}
	return sc.simplify(n, asDef)
}

type frame struct {
	inDefinition bool
	inCloseCall  bool
}

type closednessSimplifier struct {
	stack   []frame
	current frame
}

func (sc *closednessSimplifier) pushStack() {
	sc.stack = append(sc.stack, sc.current)
	sc.current.inCloseCall = false
}

func (sc *closednessSimplifier) popStack() {
	sc.current = sc.stack[len(sc.stack)-1]
	sc.stack = sc.stack[:len(sc.stack)-1]
}

func containsEllipsis(elts []ast.Decl) bool {
	for _, elt := range elts {
		if _, ok := elt.(*ast.Ellipsis); ok {
			return true
		}
	}
	return false
}

func (sc *closednessSimplifier) simplify(root ast.Node, asDef bool) ast.Node {
	sc.current = frame{
		inDefinition: asDef,
		inCloseCall:  false,
	}

	return astutil.Apply(root, func(c astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Field:
			sc.pushStack()
			sc.current.inDefinition = sc.current.inDefinition || IsDefinition(n.Label)
		case *ast.CallExpr:
			sc.pushStack()
			if fn, ok := n.Fun.(*ast.Ident); ok {
				if fn.Name == "close" {
					sc.current.inCloseCall = true
				} else {
					sc.current.inDefinition = false
				}
			}
		case *ast.StructLit:
			f := sc.current
			sc.pushStack()

			sc.current.inCloseCall = f.inCloseCall
		case *ast.Ellipsis:
			parent := c.Parent()
			if parent != nil {
				switch parent.Node().(type) {
				case *ast.StructLit, *ast.File:
					if sc.current.inCloseCall || !sc.current.inDefinition {
						for _, comm := range ast.Comments(c.Node()) {
							ast.AddComment(parent.Node(), comm)
						}
						c.Delete()
					}
				}
			}
		}
		return true
	}, func(c astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Field:
			sc.popStack()
		case *ast.CallExpr:
			if fn, ok := n.Fun.(*ast.Ident); ok && fn.Name == "close" {
				if sc.current.inDefinition {
					c.Replace(n.Args[0])
				}
			}
			sc.popStack()
		case *ast.StructLit:
			sc.popStack()
		}
		return true
	})
}
