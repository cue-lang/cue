// Copyright 2018 The CUE Authors
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

package parser

import (
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

func init() {
	internal.DebugStr = debugStr
}

func debugStr(x interface{}) (out string) {
	if n, ok := x.(ast.Node); ok {
		comments := ""
		for _, g := range n.Comments() {
			comments += debugStr(g)
		}
		if comments != "" {
			defer func() { out = "<" + comments + out + ">" }()
		}
	}
	switch v := x.(type) {
	case *ast.File:
		out := ""
		if v.Name != nil {
			out += "package "
			out += debugStr(v.Name)
			out += ", "
		}
		out += debugStr(v.Decls)
		return out

	case *ast.Alias:
		out := debugStr(v.Ident)
		out += " = "
		out += debugStr(v.Expr)
		return out

	case *ast.BottomLit:
		return "_|_"

	case *ast.BasicLit:
		return v.Value

	case *ast.Interpolation:
		for _, e := range v.Elts {
			out += debugStr(e)
		}
		return out

	case *ast.EmitDecl:
		// out := "<"
		out += debugStr(v.Expr)
		// out += ">"
		return out

	case *ast.ImportDecl:
		out := "import "
		if v.Lparen != token.NoPos {
			out += "( "
			out += debugStr(v.Specs)
			out += " )"
		} else {
			out += debugStr(v.Specs)
		}
		return out

	case *ast.ComprehensionDecl:
		out := debugStr(v.Field)
		out += " "
		out += debugStr(v.Clauses)
		return out

	case *ast.StructLit:
		out := "{"
		out += debugStr(v.Elts)
		out += "}"
		return out

	case *ast.ListLit:
		out := "["
		out += debugStr(v.Elts)
		if v.Ellipsis != token.NoPos || v.Type != nil {
			if out != "[" {
				out += ", "
			}
			out += "..."
			if v.Type != nil {
				out += debugStr(v.Type)
			}
		}
		out += "]"
		return out

	case *ast.ListComprehension:
		out := "["
		out += debugStr(v.Expr)
		out += " "
		out += debugStr(v.Clauses)
		out += "]"
		return out

	case *ast.ForClause:
		out := "for "
		if v.Key != nil {
			out += debugStr(v.Key)
			out += ": "
		}
		out += debugStr(v.Value)
		out += " in "
		out += debugStr(v.Source)
		return out

	case *ast.IfClause:
		out := "if "
		out += debugStr(v.Condition)
		return out

	case *ast.Field:
		out := debugStr(v.Label)
		if v.Value != nil {
			out += ": "
			out += debugStr(v.Value)
			for _, a := range v.Attrs {
				out += " "
				out += debugStr(a)
			}
		}
		return out

	case *ast.Attribute:
		return v.Text

	case *ast.Ident:
		return v.Name

	case *ast.TemplateLabel:
		out := "<"
		out += debugStr(v.Ident)
		out += ">"
		return out

	case *ast.SelectorExpr:
		return debugStr(v.X) + "." + debugStr(v.Sel)

	case *ast.CallExpr:
		out := debugStr(v.Fun)
		out += "("
		out += debugStr(v.Args)
		out += ")"
		return out

	case *ast.ParenExpr:
		out := "("
		out += debugStr(v.X)
		out += ")"
		return out

	case *ast.UnaryExpr:
		return v.Op.String() + debugStr(v.X)

	case *ast.BinaryExpr:
		out := debugStr(v.X)
		op := v.Op.String()
		if 'a' <= op[0] && op[0] <= 'z' {
			op = fmt.Sprintf(" %s ", op)
		}
		out += op
		out += debugStr(v.Y)
		return out

	case []*ast.CommentGroup:
		var a []string
		for _, c := range v {
			a = append(a, debugStr(c))
		}
		return strings.Join(a, "\n")

	case *ast.CommentGroup:
		str := "["
		if v.Doc {
			str += "d"
		}
		if v.Line {
			str += "l"
		}
		str += strconv.Itoa(int(v.Position))
		var a = []string{}
		for _, c := range v.List {
			a = append(a, c.Text)
		}
		return str + strings.Join(a, " ") + "] "

	case *ast.IndexExpr:
		out := debugStr(v.X)
		out += "["
		out += debugStr(v.Index)
		out += "]"
		return out

	case *ast.SliceExpr:
		out := debugStr(v.X)
		out += "["
		out += debugStr(v.Low)
		out += ":"
		out += debugStr(v.High)
		out += "]"
		return out

	case *ast.ImportSpec:
		out := ""
		if v.Name != nil {
			out += debugStr(v.Name)
			out += " "
		}
		out += debugStr(v.Path)
		return out

	case []ast.Decl:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, d := range v {
			out += debugStr(d)
			out += sep
		}
		return out[:len(out)-len(sep)]

	case []ast.Clause:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, c := range v {
			out += debugStr(c)
			out += " "
		}
		return out

	case []ast.Expr:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, d := range v {
			out += debugStr(d)
			out += sep
		}
		return out[:len(out)-len(sep)]

	case []*ast.ImportSpec:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, d := range v {
			out += debugStr(d)
			out += sep
		}
		return out[:len(out)-len(sep)]

	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("<%T>", x)
	}
}

const sep = ", "
