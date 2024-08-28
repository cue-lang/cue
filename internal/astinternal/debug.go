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

// AppendDebug writes a multi-line Go-like representation of a syntax tree node,
// including node position information and any relevant Go types.
func AppendDebug(dst []byte, node ast.Node, config DebugConfig) []byte {
	d := &debugPrinter{cfg: config}
	dst = d.value(dst, reflect.ValueOf(node), nil)
	dst = d.newline(dst)
	return dst
}

// DebugConfig configures the behavior of [AppendDebug].
type DebugConfig struct {
	// Filter is called before each value in a syntax tree.
	// Values for which the function returns false are omitted.
	Filter func(reflect.Value) bool

	// OmitEmpty causes empty strings, empty structs, empty lists,
	// nil pointers, invalid positions, and missing tokens to be omitted.
	OmitEmpty bool
}

type debugPrinter struct {
	w     io.Writer
	cfg   DebugConfig
	level int
}

func (d *debugPrinter) printf(dst []byte, format string, args ...any) []byte {
	return fmt.Appendf(dst, format, args...)
}

func (d *debugPrinter) newline(dst []byte) []byte {
	return fmt.Appendf(dst, "\n%s", strings.Repeat("\t", d.level))
}

var (
	typeTokenPos   = reflect.TypeFor[token.Pos]()
	typeTokenToken = reflect.TypeFor[token.Token]()
)

func (d *debugPrinter) value(dst []byte, v reflect.Value, impliedType reflect.Type) []byte {
	if d.cfg.Filter != nil && !d.cfg.Filter(v) {
		return dst
	}
	// Skip over interface types.
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	// Indirecting a nil interface gives a zero value.
	if !v.IsValid() {
		if !d.cfg.OmitEmpty {
			dst = d.printf(dst, "nil")
		}
		return dst
	}

	// We print the original pointer type if there was one.
	origType := v.Type()

	v = reflect.Indirect(v)
	// Indirecting a nil pointer gives a zero value.
	if !v.IsValid() {
		if !d.cfg.OmitEmpty {
			dst = d.printf(dst, "nil")
		}
		return dst
	}

	if d.cfg.OmitEmpty && v.IsZero() {
		return dst
	}

	t := v.Type()
	switch t {
	// Simple types which can stringify themselves.
	case typeTokenPos, typeTokenToken:
		dst = d.printf(dst, "%s(%q)", t, v)
		return dst
	}

	undoValue := len(dst)
	switch t.Kind() {
	default:
		// We assume all other kinds are basic in practice, like string or bool.
		if t.PkgPath() != "" {
			// Mention defined and non-predeclared types, for clarity.
			dst = d.printf(dst, "%s(%#v)", t, v)
		} else {
			dst = d.printf(dst, "%#v", v)
		}

	case reflect.Slice:
		if origType != impliedType {
			dst = d.printf(dst, "%s", origType)
		}
		dst = d.printf(dst, "{")
		d.level++
		anyElems := false
		for i := 0; i < v.Len(); i++ {
			ev := v.Index(i)
			undoElem := len(dst)
			dst = d.newline(dst)
			// Note: a slice literal implies the type of its elements
			// so we can avoid mentioning the type
			// of each element if it matches.
			if dst2 := d.value(dst, ev, t.Elem()); len(dst2) == len(dst) {
				dst = dst[:undoElem]
			} else {
				dst = dst2
				anyElems = true
			}
		}
		d.level--
		if !anyElems && d.cfg.OmitEmpty {
			dst = dst[:undoValue]
		} else {
			if anyElems {
				dst = d.newline(dst)
			}
			dst = d.printf(dst, "}")
		}

	case reflect.Struct:
		if origType != impliedType {
			dst = d.printf(dst, "%s", origType)
		}
		dst = d.printf(dst, "{")
		anyElems := false
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
			undoElem := len(dst)
			dst = d.newline(dst)
			dst = d.printf(dst, "%s: ", f.Name)
			if dst2 := d.value(dst, v.Field(i), nil); len(dst2) == len(dst) {
				dst = dst[:undoElem]
			} else {
				dst = dst2
				anyElems = true
			}
		}
		val := v.Addr().Interface()
		if val, ok := val.(ast.Node); ok {
			// Comments attached to a node aren't a regular field, but are still useful.
			// The majority of nodes won't have comments, so skip them when empty.
			if comments := ast.Comments(val); len(comments) > 0 {
				anyElems = true
				dst = d.newline(dst)
				dst = d.printf(dst, "Comments: ")
				dst = d.value(dst, reflect.ValueOf(comments), nil)
			}
		}
		d.level--
		if !anyElems && d.cfg.OmitEmpty {
			dst = dst[:undoValue]
		} else {
			if anyElems {
				dst = d.newline(dst)
			}
			dst = d.printf(dst, "}")
		}
	}
	return dst
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
