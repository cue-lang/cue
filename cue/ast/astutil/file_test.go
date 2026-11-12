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

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	_ "cuelang.org/go/pkg"
)

func TestToFile(t *testing.T) {
	mat := ast.NewIdent("math")
	mat.Node = ast.NewImport(nil, "math")
	pi := ast.NewSel(mat, "Pi")

	var longList []ast.Expr
	for c := 'a'; c <= 'k'; c++ {
		longList = append(longList, ast.NewString(strings.Repeat(string(c), 8)))
	}

	testCases := []struct {
		desc string
		expr ast.Expr
		file string // ToFile's result, formatted
		want string // the evaluated result, formatted
	}{{
		desc: "add import",
		expr: ast.NewBinExpr(token.ADD, ast.NewLit(token.INT, "1"), pi),
		file: `import "math"

1 + math.Pi
`,
		want: "4.141592653589793238462643383279503",
	}, {
		desc: "long list",
		expr: ast.NewList(longList...),
		file: `[
	"aaaaaaaa",
	"bbbbbbbb",
	"cccccccc",
	"dddddddd",
	"eeeeeeee",
	"ffffffff",
	"gggggggg",
	"hhhhhhhh",
	"iiiiiiii",
	"jjjjjjjj",
	"kkkkkkkk",
]
`,
		want: `[
	"aaaaaaaa",
	"bbbbbbbb",
	"cccccccc",
	"dddddddd",
	"eeeeeeee",
	"ffffffff",
	"gggggggg",
	"hhhhhhhh",
	"iiiiiiii",
	"jjjjjjjj",
	"kkkkkkkk",
]`,
	}, {
		desc: "resolve unresolved within struct",
		expr: ast.NewStruct(
			ast.NewIdent("a"), ast.NewString("foo"),
			ast.NewIdent("b"), ast.NewIdent("a"),
		),
		file: `a: "foo"
b: a
`,
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
		file: `a: "bar"
c: {
	a: "foo"
	b: a_1
}

let a_1 = a
`,
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
			b, err := format.Node(f)
			if err != nil {
				t.Fatal(err)
			}
			qt.Assert(t, qt.Equals(string(b), tc.file))

			v := cuecontext.New().BuildFile(f)
			if err := v.Err(); err != nil {
				t.Fatal(err)
			}

			b, err = format.Node(v.Syntax(cue.Concrete(true)))
			if err != nil {
				t.Fatal(err)
			}

			got := string(b)
			want := strings.TrimLeft(tc.want, "\n")
			qt.Assert(t, qt.Equals(got, want))
		})
	}
}
