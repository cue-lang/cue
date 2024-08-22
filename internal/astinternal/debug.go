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

package astinternal

import (
	"fmt"
	gotoken "go/token"
	"io"
	"reflect"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// DebugPrint writes a multi-line Go-like representation of a syntax tree node,
// including node position information and any relevant Go types.
//
// Note that since this is an internal debugging API, [io.Writer] errors are ignored,
// as it is assumed that the caller is using a [bytes.Buffer] or directly
// writing to standard output.
func DebugPrint(w io.Writer, node ast.Node) {
	d := &debugPrinter{w: w}
	d.value(reflect.ValueOf(node), nil)
	d.newline()
}

type debugPrinter struct {
	w     io.Writer
	level int
}

func (d *debugPrinter) printf(format string, args ...any) {
	fmt.Fprintf(d.w, format, args...)
}

func (d *debugPrinter) newline() {
	fmt.Fprintf(d.w, "\n%s", strings.Repeat("\t", d.level))
}

var (
	typeTokenPos   = reflect.TypeFor[token.Pos]()
	typeTokenToken = reflect.TypeFor[token.Token]()
)

func (d *debugPrinter) value(v reflect.Value, impliedType reflect.Type) {
	// Skip over interface types.
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	// Indirecting a nil interface gives a zero value.
	if !v.IsValid() {
		d.printf("nil")
		return
	}

	// We print the original pointer type if there was one.
	origType := v.Type()

	v = reflect.Indirect(v)
	// Indirecting a nil pointer gives a zero value.
	if !v.IsValid() {
		d.printf("nil")
		return
	}

	t := v.Type()
	switch t {
	// Simple types which can stringify themselves.
	case typeTokenPos, typeTokenToken:
		d.printf("%s(%q)", t, v)
		return
	}

	switch t.Kind() {
	default:
		// We assume all other kinds are basic in practice, like string or bool.
		if t.PkgPath() != "" {
			// Mention defined and non-predeclared types, for clarity.
			d.printf("%s(%#v)", t, v)
		} else {
			d.printf("%#v", v)
		}

	case reflect.Slice:
		if origType != impliedType {
			d.printf("%s", origType)
		}
		d.printf("{")
		if v.Len() > 0 {
			d.level++
			for i := 0; i < v.Len(); i++ {
				d.newline()
				ev := v.Index(i)
				// Note: a slice literal implies the type of its elements
				// so we can avoid mentioning the type
				// of each element if it matches.
				d.value(ev, t.Elem())
			}
			d.level--
			d.newline()
		}
		d.printf("}")

	case reflect.Struct:
		if origType != impliedType {
			d.printf("%s", origType)
		}
		d.printf("{")
		printed := false
		d.level++
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if !gotoken.IsExported(f.Name) {
				continue
			}
			switch f.Name {
			// These fields are cyclic, and they don't represent the syntax anyway.
			case "Scope", "Node", "Unresolved":
				continue
			}
			printed = true
			d.newline()
			d.printf("%s: ", f.Name)
			d.value(v.Field(i), nil)
		}
		val := v.Addr().Interface()
		if val, ok := val.(ast.Node); ok {
			// Comments attached to a node aren't a regular field, but are still useful.
			// The majority of nodes won't have comments, so skip them when empty.
			if comments := ast.Comments(val); len(comments) > 0 {
				printed = true
				d.newline()
				d.printf("Comments: ")
				d.value(reflect.ValueOf(comments), nil)
			}
		}
		d.level--
		if printed {
			d.newline()
		}
		d.printf("}")
	}
}

func DebugStr(x interface{}) (out string) {
	if n, ok := x.(ast.Node); ok {
		comments := ""
		for _, g := range ast.Comments(n) {
			comments += DebugStr(g)
		}
		if comments != "" {
			defer func() { out = "<" + comments + out + ">" }()
		}
	}
	switch v := x.(type) {
	case *ast.File:
		out := ""
		out += DebugStr(v.Decls)
		return out

	case *ast.Package:
		out := "package "
		out += DebugStr(v.Name)
		return out

	case *ast.LetClause:
		out := "let "
		out += DebugStr(v.Ident)
		out += "="
		out += DebugStr(v.Expr)
		return out

	case *ast.Alias:
		out := DebugStr(v.Ident)
		out += "="
		out += DebugStr(v.Expr)
		return out

	case *ast.BottomLit:
		return "_|_"

	case *ast.BasicLit:
		return v.Value

	case *ast.Interpolation:
		for _, e := range v.Elts {
			out += DebugStr(e)
		}
		return out

	case *ast.EmbedDecl:
		out += DebugStr(v.Expr)
		return out

	case *ast.ImportDecl:
		out := "import "
		if v.Lparen != token.NoPos {
			out += "( "
			out += DebugStr(v.Specs)
			out += " )"
		} else {
			out += DebugStr(v.Specs)
		}
		return out

	case *ast.Comprehension:
		out := DebugStr(v.Clauses)
		out += DebugStr(v.Value)
		return out

	case *ast.StructLit:
		out := "{"
		out += DebugStr(v.Elts)
		out += "}"
		return out

	case *ast.ListLit:
		out := "["
		out += DebugStr(v.Elts)
		out += "]"
		return out

	case *ast.Ellipsis:
		out := "..."
		if v.Type != nil {
			out += DebugStr(v.Type)
		}
		return out

	case *ast.ForClause:
		out := "for "
		if v.Key != nil {
			out += DebugStr(v.Key)
			out += ": "
		}
		out += DebugStr(v.Value)
		out += " in "
		out += DebugStr(v.Source)
		return out

	case *ast.IfClause:
		out := "if "
		out += DebugStr(v.Condition)
		return out

	case *ast.Field:
		out := DebugStr(v.Label)
		if t, ok := internal.ConstraintToken(v); ok {
			out += t.String()
		}
		if v.Value != nil {
			switch v.Token {
			case token.ILLEGAL, token.COLON:
				out += ": "
			default:
				out += fmt.Sprintf(" %s ", v.Token)
			}
			out += DebugStr(v.Value)
			for _, a := range v.Attrs {
				out += " "
				out += DebugStr(a)
			}
		}
		return out

	case *ast.Attribute:
		return v.Text

	case *ast.Ident:
		return v.Name

	case *ast.SelectorExpr:
		return DebugStr(v.X) + "." + DebugStr(v.Sel)

	case *ast.CallExpr:
		out := DebugStr(v.Fun)
		out += "("
		out += DebugStr(v.Args)
		out += ")"
		return out

	case *ast.ParenExpr:
		out := "("
		out += DebugStr(v.X)
		out += ")"
		return out

	case *ast.UnaryExpr:
		return v.Op.String() + DebugStr(v.X)

	case *ast.BinaryExpr:
		out := DebugStr(v.X)
		op := v.Op.String()
		if 'a' <= op[0] && op[0] <= 'z' {
			op = fmt.Sprintf(" %s ", op)
		}
		out += op
		out += DebugStr(v.Y)
		return out

	case []*ast.CommentGroup:
		var a []string
		for _, c := range v {
			a = append(a, DebugStr(c))
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
		out := DebugStr(v.X)
		out += "["
		out += DebugStr(v.Index)
		out += "]"
		return out

	case *ast.SliceExpr:
		out := DebugStr(v.X)
		out += "["
		out += DebugStr(v.Low)
		out += ":"
		out += DebugStr(v.High)
		out += "]"
		return out

	case *ast.ImportSpec:
		out := ""
		if v.Name != nil {
			out += DebugStr(v.Name)
			out += " "
		}
		out += DebugStr(v.Path)
		return out

	case *ast.Func:
		return fmt.Sprintf("func(%v): %v", DebugStr(v.Args), DebugStr(v.Ret))

	case []ast.Decl:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, d := range v {
			out += DebugStr(d)
			out += sep
		}
		return out[:len(out)-len(sep)]

	case []ast.Clause:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, c := range v {
			out += DebugStr(c)
			out += " "
		}
		return out

	case []ast.Expr:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, d := range v {
			out += DebugStr(d)
			out += sep
		}
		return out[:len(out)-len(sep)]

	case []*ast.ImportSpec:
		if len(v) == 0 {
			return ""
		}
		out := ""
		for _, d := range v {
			out += DebugStr(d)
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
