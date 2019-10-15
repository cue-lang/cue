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

package astutil_test

import (
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply(t *testing.T) {
	testCases := []struct {
		name   string
		in     string
		out    string
		before func(astutil.Cursor) bool
		after  func(astutil.Cursor) bool
	}{{
		// This should pass
	}, {
		name: "insert before",
		in: `
		// foo is a
		foo: {
			a: 3
		}
		`,
		out: `
iam: new
// foo is a
foo: {
	iam: new
	a:   3
}
`,
		before: func(c astutil.Cursor) bool {
			switch c.Node().(type) {
			case *ast.Field:
				c.InsertBefore(&ast.Field{
					Label: ast.NewIdent("iam"),
					Value: ast.NewIdent("new"),
				})
			}
			return true
		},
	}, {
		name: "insert after",
		in: `
			foo: {
				a: 3 @test()
			}
			`,
		out: `
foo: {
	a:   3 @test()
	iam: new
}
iam: new
`,
		before: func(c astutil.Cursor) bool {
			switch c.Node().(type) {
			case *ast.Field:
				c.InsertAfter(&ast.Field{
					Label: ast.NewIdent("iam"),
					Value: ast.NewIdent("new"),
				})
			}
			return true
		},
	}, {
		name: "insert after recursive",
		in: `
			foo: {
				a: 3 @test()
			}
			`,
		out: `
foo: {
	a: 3 @test()
	iam: {
		here:  new
		there: new
	}
	everywhere: new
}
iam: {
	here:  new
	there: new
}
everywhere: new
	`,
		before: func(c astutil.Cursor) bool {
			switch x := c.Node().(type) {
			case *ast.Field:
				switch x.Label.(*ast.Ident).Name {
				default:
					c.InsertAfter(astutil.ApplyRecursively(&ast.Field{
						Label: ast.NewIdent("iam"),
						Value: &ast.StructLit{Elts: []ast.Decl{
							&ast.Field{
								Label: ast.NewIdent("here"),
								Value: ast.NewIdent("new"),
							},
						}},
					}))
				case "iam":
					c.InsertAfter(&ast.Field{
						Label: ast.NewIdent("everywhere"),
						Value: ast.NewIdent("new"),
					})
				case "here":
					c.InsertAfter(&ast.Field{
						Label: ast.NewIdent("there"),
						Value: ast.NewIdent("new"),
					})
				case "everywhere":
				}
			}
			return true
		}}, {
		name: "templates",
		in: `
				foo: {
					a <b> c: 3
				}
				`,
		out: `
foo: {
	a <b>: {
		c:   3
		iam: new
	}
}
	`,
		before: func(c astutil.Cursor) bool {
			switch x := c.Node().(type) {
			case *ast.Field:
				if _, ok := x.Value.(*ast.StructLit); !ok {
					c.InsertAfter(&ast.Field{
						Label: ast.NewIdent("iam"),
						Value: ast.NewIdent("new"),
					})
				}
			}
			return true
		},
	}, {
		name: "replace",
		in: `
		// keep comment
		a: "string" // and this one
		b: 3
		c: [ 1, 2, 8, 4 ]
		d: "\(foo) is \(0)"
		`,
		out: `
// keep comment
a: s // and this one
b: 4
c: [4, 4, 4, 4]
d: "\(foo) is \(4)"
`,
		before: func(c astutil.Cursor) bool {
			switch x := c.Node().(type) {
			case *ast.BasicLit:
				switch x.Kind {
				case token.STRING:
					if c.Index() < 0 {
						c.Replace(ast.NewIdent("s"))
					}
				case token.INT:
					c.Replace(&ast.BasicLit{Kind: token.INT, Value: "4"})
				}
			}
			return true
		},
	}, {
		name: "delete",
		in: `
		z: 0
		a: "foo"
		b: 3
		b: "bar"
		c: 2
		`,
		out: `
a: "foo"
b: "bar"
	`,
		before: func(c astutil.Cursor) bool {
			f, ok := c.Node().(*ast.Field)
			if !ok {
				return true
			}
			switch x := f.Value.(type) {
			case *ast.BasicLit:
				switch x.Kind {
				case token.INT:
					c.Delete()
				}
			}
			return true
		},
	}, {
		name: "comments",
		in: `
		// test
		a: "string"
		`,
		out: `
// 1, 2, 3
a: "string"
	`,
		before: func(c astutil.Cursor) bool {
			switch c.Node().(type) {
			case *ast.Comment:
				c.Replace(&ast.Comment{Text: "// 1, 2, 3"})
			}
			return true
		},
	}, {
		name: "comments after",
		in: `
	// test
	a: "string"
			`,
		out: `
// 1, 2, 3
a: "string"
		`,
		after: func(c astutil.Cursor) bool {
			switch c.Node().(type) {
			case *ast.Comment:
				c.Replace(&ast.Comment{Text: "// 1, 2, 3"})
			}
			return true
		},
	}, {
		name: "imports add",
		in: `
a: "string"
			`,
		out: `
import list6c6973 "list"

a: list6c6973
		`,
		after: func(c astutil.Cursor) bool {
			switch c.Node().(type) {
			case *ast.BasicLit:
				c.Replace(c.Import("list"))
			}
			return true
		},
	}, {
		name: "imports add to",
		in: `package foo

import "math"

a: 3
				`,
		out: `package foo

import (
	"math"
	list6c6973 "list"
)

a: list6c6973
			`,
		after: func(c astutil.Cursor) bool {
			switch x := c.Node().(type) {
			case *ast.BasicLit:
				if x.Kind == token.INT {
					c.Replace(c.Import("list"))
				}
			}
			return true
		},
	}, {
		name: "imports duplicate",
		in: `package foo

import "list"

a: 3
				`,
		out: `package foo

import (
	"list"
	list6c6973 "list"
)

a: list6c6973
					`,
		after: func(c astutil.Cursor) bool {
			switch x := c.Node().(type) {
			case *ast.BasicLit:
				if x.Kind == token.INT {
					c.Replace(c.Import("list"))
				}
			}
			return true
		},
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile(tc.name, tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			n := astutil.Apply(f, tc.before, tc.after)

			b, err := format.Node(n)
			require.NoError(t, err)
			got := strings.TrimSpace(string(b))
			want := strings.TrimSpace(tc.out)
			assert.Equal(t, want, got)
		})
	}
}
