// Copyright 2019 CUE Authors
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

package ast_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
)

func TestLabelName(t *testing.T) {
	testCases := []struct {
		in      ast.Label
		out     string
		isIdent bool
		err     bool
		expr    bool
	}{{
		in:      ast.NewString("foo-bar"),
		out:     "foo-bar",
		isIdent: false,
	}, {
		in:      ast.NewString("8ball"),
		out:     "8ball",
		isIdent: false,
	}, {
		in:      ast.NewString("foo bar"),
		out:     "foo bar",
		isIdent: false,
	}, {
		in:  &ast.Ident{Name: "`foo`"},
		err: true,
	}, {
		in:      &ast.Ident{Name: ""},
		out:     "",
		isIdent: false,
		err:     true,
	}, {
		in:      &ast.Ident{Name: "#"},
		out:     "#",
		isIdent: true,
	}, {
		in:      &ast.Ident{Name: "#0"},
		out:     "",
		isIdent: false,
		err:     true,
	}, {
		in:      &ast.Ident{Name: "_#"},
		out:     "_#",
		isIdent: true,
		err:     false,
	}, {
		in:      &ast.Ident{Name: "_"},
		out:     "_",
		isIdent: true,
	}, {
		in:      &ast.Ident{Name: "_1"},
		out:     "_1",
		isIdent: true,
	}, {
		in:  &ast.Ident{Name: "_#1"},
		out: "",
		err: true,
	}, {
		in:      &ast.Ident{Name: "8ball"},
		out:     "",
		isIdent: false,
		err:     true,
	}, {
		in:      &ast.Ident{Name: "_hidden"},
		out:     "_hidden",
		isIdent: true,
	}, {
		in:      &ast.Ident{Name: "#A"},
		out:     "#A",
		isIdent: true,
	}, {
		in:      &ast.Ident{Name: "#Def"},
		out:     "#Def",
		isIdent: true,
	}, {
		in:      &ast.Ident{Name: "_#Def"},
		out:     "_#Def",
		isIdent: true,
	}, {
		in:      &ast.Ident{Name: "#_Def"},
		out:     "#_Def",
		isIdent: true,
	}, {
		in:      ast.NewBool(true),
		out:     "true",
		isIdent: true,
	}, {
		in:      &ast.BasicLit{Kind: token.STRING, Value: `"foo`},
		out:     "",
		isIdent: false,
		err:     true,
	}, {
		in:      &ast.Interpolation{Elts: []ast.Expr{ast.NewString("foo")}},
		out:     "",
		isIdent: false,
		err:     true,
		expr:    true,
	}}
	for _, tc := range testCases {
		b, _ := format.Node(tc.in)
		t.Run(string(b), func(t *testing.T) {
			if id, ok := tc.in.(*ast.Ident); ok && !strings.HasPrefix(id.Name, "`") {
				qt.Assert(t, qt.Equals(ast.IsValidIdent(id.Name), tc.isIdent))
			}

			str, isIdent, err := ast.LabelName(tc.in)
			qt.Assert(t, qt.Equals(str, tc.out), qt.Commentf("value"))
			qt.Assert(t, qt.Equals(isIdent, tc.isIdent), qt.Commentf("isIdent"))
			qt.Assert(t, qt.Equals(err != nil, tc.err), qt.Commentf("err"))
			qt.Assert(t, qt.Equals(errors.Is(err, ast.ErrIsExpression), tc.expr), qt.Commentf("expr"))
		})
	}
}
