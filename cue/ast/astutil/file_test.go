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

package astutil_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	_ "cuelang.org/go/pkg"
)

func TestToFile(t *testing.T) {
	mat := ast.NewIdent("math")
	mat.Node = ast.NewImport(nil, "math")
	pi := ast.NewSel(mat, "Pi")

	testCases := []struct {
		desc string
		expr ast.Expr
		want string
	}{{
		desc: "add import",
		expr: ast.NewBinExpr(token.ADD, ast.NewLit(token.INT, "1"), pi),
		want: "4.14159265358979323846264",
	}, {
		desc: "resolve unresolved within struct",
		expr: ast.NewStruct(
			ast.NewIdent("a"), ast.NewString("foo"),
			ast.NewIdent("b"), ast.NewIdent("a"),
		),
		want: `{
	a: "foo"
	b: "foo"
}`,
	}, {
		desc: "unshadow",
		expr: func() ast.Expr {
			ident := ast.NewIdent("a")
			ref := ast.NewIdent("a")
			ref.Node = ident

			return ast.NewStruct(
				ident, ast.NewString("bar"),
				ast.NewIdent("c"), ast.NewStruct(
					ast.NewIdent("a"), ast.NewString("foo"),
					ast.NewIdent("b"), ref, // refers to outer `a`.
				))
		}(),
		want: `{
	a: "bar"
	c: {
		a: "foo"
		b: "bar"
	}
}`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			f, err := astutil.ToFile(tc.expr)
			if err != nil {
				t.Fatal(err)
			}

			var r cue.Runtime

			inst, err := r.CompileFile(f)
			if err != nil {
				t.Fatal(err)
			}

			b, err := format.Node(inst.Value().Syntax(cue.Concrete(true)))
			if err != nil {
				t.Fatal(err)
			}

			got := string(b)
			want := strings.TrimLeft(tc.want, "\n")
			if got != want {
				t.Error(cmp.Diff(want, got))
			}
		})
	}
}
