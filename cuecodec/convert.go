// Copyright 2026 The CUE Authors
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

package cuecodec

import (
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// toFile wraps a decoded expression as an *ast.File. A struct literal
// becomes the file's top-level declarations; any other expression is
// embedded.
func toFile(expr ast.Expr) *ast.File {
	if s, ok := expr.(*ast.StructLit); ok {
		f := &ast.File{Decls: s.Elts}
		ast.SetComments(f, ast.Comments(s))
		return f
	}
	return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: expr}}}
}

// astToValue converts a data-only *ast.File to a Go value suitable for
// re-encoding by a format library that consumes ordinary Go values
// (JSON, TOML). It supports the concrete subset of CUE that data files
// decode to: structs, lists, and scalars.
//
// TODO(cleanup): this bridge exists because the encoding libraries below
// still speak Go values or v1 cue.Value; once the v1 encoding layer is
// dismantled and cue/v2 exists, encode directly from values.
func astToValue(f *ast.File) (any, error) {
	return declsToMap(f.Decls)
}

func declsToMap(decls []ast.Decl) (map[string]any, error) {
	m := make(map[string]any, len(decls))
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.Field:
			name, err := labelName(d.Label)
			if err != nil {
				return nil, err
			}
			v, err := exprToValue(d.Value)
			if err != nil {
				return nil, err
			}
			m[name] = v
		case *ast.EmbedDecl:
			return nil, fmt.Errorf("cannot encode embedded expression as a struct field")
		default:
			return nil, fmt.Errorf("cannot encode declaration of type %T", d)
		}
	}
	return m, nil
}

func exprToValue(expr ast.Expr) (any, error) {
	switch x := expr.(type) {
	case *ast.StructLit:
		return declsToMap(x.Elts)
	case *ast.ListLit:
		list := make([]any, 0, len(x.Elts))
		for _, e := range x.Elts {
			v, err := exprToValue(e)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case *ast.BasicLit:
		return scalarValue(x)
	case *ast.UnaryExpr:
		return unaryValue(x)
	default:
		return nil, fmt.Errorf("cannot encode expression of type %T", expr)
	}
}

func unaryValue(u *ast.UnaryExpr) (any, error) {
	v, err := exprToValue(u.X)
	if err != nil {
		return nil, err
	}
	switch u.Op {
	case token.SUB:
		switch n := v.(type) {
		case int64:
			return -n, nil
		case float64:
			return -n, nil
		}
		return nil, fmt.Errorf("cannot negate value of type %T", v)
	case token.MUL:
		// A default marker (*x); use the marked value.
		return v, nil
	default:
		return nil, fmt.Errorf("cannot encode unary operator %v", u.Op)
	}
}

func scalarValue(b *ast.BasicLit) (any, error) {
	switch b.Kind {
	case token.INT:
		return strconv.ParseInt(strings.ReplaceAll(b.Value, "_", ""), 10, 64)
	case token.FLOAT:
		return strconv.ParseFloat(strings.ReplaceAll(b.Value, "_", ""), 64)
	case token.STRING:
		return literal.Unquote(b.Value)
	case token.TRUE:
		return true, nil
	case token.FALSE:
		return false, nil
	case token.NULL:
		return nil, nil
	default:
		return nil, fmt.Errorf("cannot encode literal of kind %v", b.Kind)
	}
}

func labelName(l ast.Label) (string, error) {
	switch x := l.(type) {
	case *ast.Ident:
		return x.Name, nil
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return literal.Unquote(x.Value)
		}
		return x.Value, nil
	default:
		return "", fmt.Errorf("cannot encode label of type %T", l)
	}
}
