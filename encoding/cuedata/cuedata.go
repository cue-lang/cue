// Copyright 2021 CUE Authors
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

package cuedata

import (
	"fmt"
	"log"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
)

// CUEdata coverts all nodes, except for BasicLit, StructLit and ListList into a syntax strings and assigns
// the syntax to a special field.
//
// Struct and list in binary expressions remain as data, everything else is converted to a syntax string.

const syntaxLabel = "$$cue"

type Encoder struct {
}

func NewEncoder() *Encoder {
	return &Encoder{}
}

func (e *Encoder) RewriteFile(file *ast.File) error {
	var enc encoder
	file.Decls = enc.rewriteDecls(file.Decls)
	return enc.errs
}

type encoder struct {
	errs errors.Error
}

func (e *encoder) addErr(err errors.Error) {
	e.errs = errors.Append(e.errs, err)
}

func (e *encoder) addErrf(p token.Pos, format string, args ...interface{}) {
	//format = "%s: " + format
	//args = append([]interface{}{schema.Path()}, args...)
	e.addErr(errors.Newf(p, format, args...))
}

func (e *encoder) debug(format string, v ...interface{}) {
	if false {
		log.Printf(format, v...)
	}
}

func (e *encoder) rewriteDecls(decls []ast.Decl) (d []ast.Decl) {
	e.debug("rewriteDecls()")
	newDecls := []ast.Decl{}
	syntax := ""
	for _, dec := range decls {
		switch d := dec.(type) {
		case *ast.Package, *ast.ImportDecl, *ast.LetClause, *ast.Attribute, *ast.Comprehension, *ast.EmbedDecl:
			b, _ := format.Node(d)
			e.debug("%T %s\n", d, string(b))
			if syntax != "" {
				syntax += "\n"
			}
			// let clause syntax always has a proceeding \n
			syntax += strings.TrimLeft(string(b), "\n")
		case *ast.Field:
			sel := cue.Label(d.Label)
			if !sel.IsString() {
				b, _ := format.Node(d)
				if syntax != "" {
					syntax += "\n"
				}
				syntax += string(b)
			} else {
				e.debug("Field `%s` ", sel)
				expr, s := e.rewrite(d.Value)
				if s != "" {
					if syntax != "" {
						syntax += "\n"
					}
					optional := ""
					if d.Optional != token.NoPos {
						optional = "?"
					}
					syntax += fmt.Sprintf("%s%s: %s", sel.String(), optional, strings.TrimLeft(s, "\n"))
				}
				if expr != nil {
					d.Value = expr
					newDecls = append(newDecls, d)
				}
			}
		default:
			e.addErrf(d.Pos(), "rewriteDecls() does not handle %T", d)
		}
	}
	if syntax != "" {
		newField := &ast.Field{Label: ast.NewString(syntaxLabel), Value: ast.NewString(syntax)}
		newDecls = append(newDecls, newField)
	}
	return newDecls
}

func (e *encoder) rewriteBinaryExpr(expr *ast.BinaryExpr) (newExpr ast.Expr, syntax string) {
	if expr.Op == token.OR {
		b, _ := format.Node(expr)
		return nil, string(b)
	}
	e.debug("X: ")
	exprX, xSyntax := e.rewrite(expr.X)
	if xSyntax != "" {
		if syntax != "" {
			syntax += " & "
		}
		syntax += xSyntax
	}
	e.debug("Y: ")
	exprY, ySyntax := e.rewrite(expr.Y)
	if ySyntax != "" {
		if syntax != "" {
			syntax += " & "
		}
		syntax += ySyntax
	}

	if exprX == nil && exprY == nil {
		return nil, syntax
	}
	e.debug("BinaryExpr() X %T Y %T\n", exprX, exprY)

	switch x := exprX.(type) {
	case *ast.StructLit, *ast.ListLit:
		return x, syntax
	}
	switch y := exprY.(type) {
	case *ast.StructLit, *ast.ListLit:
		return y, syntax
	}

	e.addErrf(expr.Pos(), "rewriteBinaryExpression() does not handle %T %T", exprX, exprY)
	return nil, syntax
}

func (e *encoder) rewrite(expr ast.Expr) (x ast.Expr, syntax string) {
	b, _ := format.Node(expr)
	switch x := expr.(type) {
	case *ast.BinaryExpr:
		e.debug("rewrite() expr %T\n", expr)
		return e.rewriteBinaryExpr(x)
	case *ast.Ident:
		e.debug("rewrite() expr %T, value %s\n", expr, x.Name)
		return nil, x.Name
	case *ast.ListLit:
		e.debug("rewrite() expr %T\n", expr)
		for i, elem := range x.Elts {
			switch elem.(type) {
			case *ast.Comprehension:
				return nil, string(b)
			case *ast.Ellipsis:
				return nil, string(b)
			}
			newExpr, s := e.rewrite(elem)
			if s != "" {
				if syntax != "" {
					syntax += ","
				}
				syntax += s
			}
			x.Elts[i] = newExpr
		}
		return x, syntax
	case *ast.StructLit:
		e.debug("rewrite() expr %T\n", expr)
		x.Elts = e.rewriteDecls(x.Elts)
		return x, ""
	case *ast.BasicLit:
		e.debug("rewrite() expr %T, value %s\n", expr, x.Value)
		return x, ""
	case *ast.CallExpr, *ast.BottomLit, *ast.Comprehension, *ast.Ellipsis, *ast.UnaryExpr, *ast.Interpolation:
		e.debug("rewrite() expr %T, value %s\n", expr, string(b))
		return nil, string(b)
	default:
		e.addErrf(expr.Pos(), "rewrite() does not handle %T", x)
		return nil, ""
	}
}
