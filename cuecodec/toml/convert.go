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

package toml

import (
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// toFile wraps a decoded expression as an *ast.File. A TOML document
// always decodes to a struct.
func toFile(expr ast.Expr) *ast.File {
	if s, ok := expr.(*ast.StructLit); ok {
		f := &ast.File{Decls: s.Elts}
		ast.SetComments(f, ast.Comments(s))
		return f
	}
	return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: expr}}}
}

// astToValue converts a data-only *ast.File to a Go value for encoding
// by the underlying TOML library.
//
// TODO(cleanup): this bridge duplicates the equivalent helper in
// cuecodec and exists because the encoding library still speaks Go
// values; once the v1 encoding layer is dismantled and cue/v2 exists,
// encode directly from values.
func astToValue(f *ast.File) (any, error) {
	return declsToMap(f.Decls)
}

func declsToMap(decls []ast.Decl) (map[string]any, error) {
	m := make(map[string]any, len(decls))
	for _, d := range decls {
		field, ok := d.(*ast.Field)
		if !ok {
			return nil, fmt.Errorf("cannot encode declaration of type %T as TOML", d)
		}
		name, err := labelName(field.Label)
		if err != nil {
			return nil, err
		}
		v, err := exprToValue(field.Value)
		if err != nil {
			return nil, err
		}
		m[name] = v
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
		v, err := exprToValue(x.X)
		if err != nil {
			return nil, err
		}
		if x.Op != token.SUB {
			return nil, fmt.Errorf("cannot encode unary operator %v as TOML", x.Op)
		}
		switch n := v.(type) {
		case int64:
			return -n, nil
		case float64:
			return -n, nil
		}
		return nil, fmt.Errorf("cannot negate value of type %T", v)
	default:
		return nil, fmt.Errorf("cannot encode expression of type %T as TOML", expr)
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
		return nil, fmt.Errorf("cannot encode null as TOML")
	default:
		return nil, fmt.Errorf("cannot encode literal of kind %v as TOML", b.Kind)
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
