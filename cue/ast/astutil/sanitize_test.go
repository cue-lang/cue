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
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal"
	"github.com/stretchr/testify/assert"
)

func TestSanitize(t *testing.T) {
	testCases := []struct {
		desc string
		file *ast.File
		want string
	}{{
		desc: "Take existing import and rename it",
		file: func() *ast.File {
			spec := ast.NewImport(nil, "list")
			spec.AddComment(internal.NewComment(true, "will be renamed"))
			return &ast.File{Decls: []ast.Decl{
				&ast.ImportDecl{Specs: []*ast.ImportSpec{spec}},
				&ast.EmbedDecl{
					Expr: ast.NewStruct(
						ast.NewIdent("list"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "list", Node: spec},
								"Min")),
					)},
			}}
		}(),
		want: `import (
	// will be renamed
	list_1 "list"
)

{
	list: list_1.Min()
}
`,
	}, {
		desc: "Take existing import and rename it",
		file: func() *ast.File {
			spec := ast.NewImport(nil, "list")
			return &ast.File{Decls: []ast.Decl{
				&ast.ImportDecl{Specs: []*ast.ImportSpec{spec}},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						ast.NewIdent("list"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "list", Node: spec}, "Min")),
					),
				},
			}}
		}(),
		want: `import list_1 "list"

a: {
	list: list_1.Min()
}
`,
	}, {
		desc: "One import added, one removed",
		file: &ast.File{Decls: []ast.Decl{
			&ast.ImportDecl{Specs: []*ast.ImportSpec{
				{Path: ast.NewString("foo")},
			}},
			&ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewCall(
					ast.NewSel(&ast.Ident{
						Name: "bar",
						Node: &ast.ImportSpec{Path: ast.NewString("bar")},
					}, "Min")),
			},
		}},
		want: `import "bar"

a: bar.Min()
`,
	}, {
		desc: "Rename duplicate import",
		file: func() *ast.File {
			spec1 := ast.NewImport(nil, "bar")
			spec2 := ast.NewImport(nil, "foo/bar")
			spec3 := ast.NewImport(ast.NewIdent("bar"), "foo")
			return &ast.File{Decls: []ast.Decl{
				internal.NewComment(false, "File comment"),
				&ast.Package{Name: ast.NewIdent("pkg")},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						ast.NewIdent("b"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "bar", Node: spec1}, "A")),
						ast.NewIdent("c"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "bar", Node: spec2}, "A")),
						ast.NewIdent("d"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "bar", Node: spec3}, "A")),
					),
				},
			}}
		}(),
		want: `// File comment

package pkg

import (
	"bar"
	bar_1 "foo/bar"
	bar_5 "foo"
)

a: {
	b: bar.A()
	c: bar_1.A()
	d: bar_5.A()
}
`,
	}, {
		desc: "Rename duplicate import, reuse and drop",
		file: func() *ast.File {
			spec1 := ast.NewImport(nil, "bar")
			spec2 := ast.NewImport(nil, "foo/bar")
			spec3 := ast.NewImport(ast.NewIdent("bar"), "foo")
			return &ast.File{Decls: []ast.Decl{
				&ast.ImportDecl{Specs: []*ast.ImportSpec{
					spec3,
					ast.NewImport(nil, "foo"),
				}},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						ast.NewIdent("b"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "bar", Node: spec1}, "A")),
						ast.NewIdent("c"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "bar", Node: spec2}, "A")),
						ast.NewIdent("d"), ast.NewCall(
							ast.NewSel(&ast.Ident{Name: "bar", Node: spec3}, "A")),
					),
				},
			}}
		}(),
		want: `import (
	bar "foo"
	bar_1 "bar"
	bar_5 "foo/bar"
)

a: {
	b: bar_1.A()
	c: bar_5.A()
	d: bar.A()
}
`,
	}, {
		desc: "Reuse different import",
		file: &ast.File{Decls: []ast.Decl{
			&ast.Package{Name: ast.NewIdent("pkg")},
			&ast.ImportDecl{Specs: []*ast.ImportSpec{
				{Path: ast.NewString("bar")},
			}},
			&ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewStruct(
					ast.NewIdent("list"), ast.NewCall(
						ast.NewSel(&ast.Ident{
							Name: "bar",
							Node: &ast.ImportSpec{Path: ast.NewString("bar")},
						}, "Min")),
				),
			},
		}},
		want: `package pkg

import "bar"

a: {
	list: bar.Min()
}
`,
	}, {
		desc: "Clear reference that does not exist in scope",
		file: &ast.File{Decls: []ast.Decl{
			&ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewStruct(
					ast.NewIdent("b"), &ast.Ident{
						Name: "c",
						Node: ast.NewString("foo"),
					},
					ast.NewIdent("d"), ast.NewIdent("e"),
				),
			},
		}},
		want: `a: {
	b: c
	d: e
}
`,
	}, {
		desc: "Unshadow possible reference to other file",
		file: &ast.File{Decls: []ast.Decl{
			&ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewStruct(
					ast.NewIdent("b"), &ast.Ident{
						Name: "c",
						Node: ast.NewString("foo"),
					},
					ast.NewIdent("c"), ast.NewIdent("d"),
				),
			},
		}},
		want: `a: {
	b: c_1
	c: d
}

let c_1 = c
`,
	}, {
		desc: "Add alias to shadowed field",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{
					Label: ast.NewIdent("c"),
					Value: ast.NewStruct(
						ast.NewIdent("a"), ast.NewStruct(),
						ast.NewIdent("b"), &ast.Ident{
							Name: "a",
							Node: field.Value,
						},
						ast.NewIdent("c"), ast.NewIdent("d"),
					),
				},
			}}
		}(),
		want: `a_1=a: "b"
c: {
	a: {}
	b: a_1
	c: d
}
`,
	}, {
		desc: "Add let clause to shadowed field",
		// Resolve both identifiers to same clause.
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{
					Label: ast.NewIdent("c"),
					Value: ast.NewStruct(
						ast.NewIdent("a"), ast.NewStruct(),
						// Remove this reference.
						ast.NewIdent("b"), &ast.Ident{
							Name: "a",
							Node: field.Value,
						},
						ast.NewIdent("c"), ast.NewIdent("d"),
						ast.NewIdent("e"), &ast.Ident{
							Name: "a",
							Node: field.Value,
						},
					),
				},
			}}
		}(),
		want: `a_1=a: "b"
c: {
	a: {}
	b: a_1
	c: d
	e: a_1
}
`,
	}, {
		desc: "Add let clause to shadowed field",
		// Resolve both identifiers to same clause.
		file: func() *ast.File {
			fieldX := &ast.Field{
				Label: &ast.Alias{
					Ident: ast.NewIdent("X"),
					Expr:  ast.NewIdent("a"), // shadowed
				},
				Value: ast.NewString("b"),
			}
			fieldY := &ast.Field{
				Label: &ast.Alias{
					Ident: ast.NewIdent("Y"), // shadowed
					Expr:  ast.NewIdent("q"), // not shadowed
				},
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				fieldX,
				fieldY,
				&ast.Field{
					Label: ast.NewIdent("c"),
					Value: ast.NewStruct(
						ast.NewIdent("a"), ast.NewStruct(),
						ast.NewIdent("b"), &ast.Ident{
							Name: "X",
							Node: fieldX,
						},
						ast.NewIdent("c"), ast.NewIdent("d"),
						ast.NewIdent("e"), &ast.Ident{
							Name: "a",
							Node: fieldX.Value,
						},
						ast.NewIdent("f"), &ast.Ident{
							Name: "Y",
							Node: fieldY,
						},
					),
				},
			}}
		}(),
		want: `
let X_1 = X
X=a: "b"
Y=q: "b"
c: {
	a: {}
	b: X
	c: d
	e: X_1
	f: Y
}
`,
	}, {
		desc: "Add let clause to nested shadowed field",
		// Resolve both identifiers to same clause.
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("b"),
					Value: ast.NewStruct(
						field,
						ast.NewIdent("b"), ast.NewStruct(
							ast.NewIdent("a"), ast.NewString("bar"),
							ast.NewIdent("b"), &ast.Ident{
								Name: "a",
								Node: field.Value,
							},
							ast.NewIdent("e"), &ast.Ident{
								Name: "a",
								Node: field.Value,
							},
						),
					),
				},
			}}
		}(),
		want: `b: {
	a_1=a: "b"
	b: {
		a: "bar"
		b: a_1
		e: a_1
	}
}
`,
	}, {
		desc: "Add let clause to nested shadowed field with alias",
		// Resolve both identifiers to same clause.
		file: func() *ast.File {
			field := &ast.Field{
				Label: &ast.Alias{
					Ident: ast.NewIdent("X"),
					Expr:  ast.NewIdent("a"),
				},
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("b"),
					Value: ast.NewStruct(
						field,
						ast.NewIdent("b"), ast.NewStruct(
							ast.NewIdent("a"), ast.NewString("bar"),
							ast.NewIdent("b"), &ast.Ident{
								Name: "a",
								Node: field.Value,
							},
							ast.NewIdent("e"), &ast.Ident{
								Name: "a",
								Node: field.Value,
							},
						),
					),
				},
			}}
		}(),
		want: `b: {
	let X_1 = X
	X=a: "b"
	b: {
		a: "bar"
		b: X_1
		e: X_1
	}
}
`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			err := astutil.Sanitize(tc.file)
			if err != nil {
				t.Fatal(err)
			}

			b, errs := format.Node(tc.file)
			if errs != nil {
				t.Fatal(errs)
			}

			got := string(b)
			assert.Equal(t, got, tc.want)
		})
	}
}

// For testing purposes: do not remove.
func TestX(t *testing.T) {
	t.Skip()

	field := &ast.Field{
		Label: &ast.Alias{
			Ident: ast.NewIdent("X"),
			Expr:  ast.NewIdent("a"),
		},
		Value: ast.NewString("b"),
	}

	file := &ast.File{Decls: []ast.Decl{
		&ast.Field{
			Label: ast.NewIdent("b"),
			Value: ast.NewStruct(
				field,
				ast.NewIdent("b"), ast.NewStruct(
					ast.NewIdent("a"), ast.NewString("bar"),
					ast.NewIdent("b"), &ast.Ident{
						Name: "a",
						Node: field.Value,
					},
					ast.NewIdent("e"), &ast.Ident{
						Name: "a",
						Node: field.Value,
					},
				),
			),
		},
	}}

	err := astutil.Sanitize(file)
	if err != nil {
		t.Fatal(err)
	}

	b, errs := format.Node(file)
	if errs != nil {
		t.Fatal(errs)
	}

	t.Error(string(b))
}
