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
	cueinternal "cuelang.org/go/internal"
)

// SimplifyClosedness updates the AST to remove redundant ellipses and `close` calls.
func SimplifyClosedness(n ast.Node) ast.Node {
	sc := closednessSimplifier{}
	return sc.simplify(n)
}

type frame struct {
	inDefinition bool
	inCloseCall  bool
	inParams     bool
	inBinaryExpr bool
}

type closednessSimplifier struct {
	stack   []frame
	current frame
}

func (sc *closednessSimplifier) simplify(root ast.Node) ast.Node {
	return astutil.Apply(root, func(c astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.BinaryExpr:
			sc.pushStack()
			sc.current.inBinaryExpr = true
		case *ast.Field:
			sc.pushStack()
			sc.current.inDefinition = sc.current.inDefinition || cueinternal.IsDefinition(n.Label)
		case *ast.CallExpr:
			sc.pushStack()
			if fn, ok := n.Fun.(*ast.Ident); ok {
				if fn.Name == "close" {
					sc.current.inCloseCall = true
				} else if fn.Name == "matchN" {
					// If a function returns a value containing the struct literals it received, we can't
					// omit ellipses from its parameters. Although functions in CUE can be renamed, making it
					// hard to tell if omitting ellipses is safe, doing so for a JSONSchema encoder's
					// output is considered safe.
					// For now, we only do so for `matchN`.
					sc.current.inParams = true
					sc.current.inDefinition = false
					sc.current.inBinaryExpr = false
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
					if sc.current.inParams && !sc.current.inDefinition {
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
		case *ast.BinaryExpr:
			sc.popStack()
		case *ast.Field:
			sc.popStack()
		case *ast.CallExpr:
			cur := sc.current
			sc.popStack()
			if fn, ok := n.Fun.(*ast.Ident); ok && fn.Name == "close" {
				// Remove close when
				// 1. it's used as a field value inside a definition or inside nested close calls.
				// 2. and there is no change to used in BinaryExpr.
				parent := c.Parent()
				if parent != nil {
					switch parent.Node().(type) {
					case *ast.Field, *ast.CallExpr:
						if (cur.inDefinition || sc.current.inCloseCall) && !cur.inBinaryExpr {
							c.Replace(n.Args[0])
						}
					}
				}
			}
		case *ast.StructLit:
			sc.popStack()
		}
		return true
	})
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
