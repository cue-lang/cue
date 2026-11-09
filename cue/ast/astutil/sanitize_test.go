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
	"fmt"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueexperiment"

	"github.com/go-quicktest/qt"
)

func TestSanitize(t *testing.T) {
	testCases := []struct {
		desc string
		file *ast.File
		want string
		// version is the language version want is written in, if not the
		// current one.
		version string
	}{{
		desc: "Take existing import and rename it",
		file: func() *ast.File {
			spec := ast.NewImport(nil, "list")
			ast.AddComment(spec, internal.NewComment(true, "will be renamed"))
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

{list: list_1.Min()}
`,
	}, {
		desc: "Take existing import and rename it in a field",
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

a: {list: list_1.Min()}
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
				&ast.CommentGroup{List: []*ast.Comment{{Text: "// File comment"}}},
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
	bar_2 "foo"
)

a: {
	b: bar.A()
	c: bar_1.A()
	d: bar_2.A()
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
	bar_2 "foo/bar"
)

a: {
	b: bar_1.A()
	c: bar_2.A()
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

a: {list: bar.Min()}
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
		want: `a~(a_1): "b"
c: {
	a: {}
	b: a_1
	c: d
}
`,
	}, {
		// The field already binds a name, so that name is reused rather than
		// a second alias being introduced.
		desc: "Reuse the postfix field alias of a shadowed field",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
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
					),
				},
			}}
		}(),
		want: `a~(X): "b"
c: {
	a: {}
	b: X
}
`,
	}, {
		// A reference to the alias name itself, shadowed where it is used.
		desc: "Unshadow a reference to a postfix field alias",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{
					Label: ast.NewIdent("c"),
					Value: &ast.StructLit{Elts: []ast.Decl{
						// A let shadows the alias name; a field of that name
						// would be rejected outright.
						&ast.LetClause{
							Ident: ast.NewIdent("X"),
							Expr:  ast.NewString("shadow"),
						},
						&ast.Field{
							Label: ast.NewIdent("b"),
							Value: &ast.Ident{Name: "X", Node: field},
						},
					}},
				},
			}}
		}(),
		want: `let X_1 = X
a~(X): "b"
c: {
	let X = "shadow"
	b: X_1
}
`,
	}, {
		// A dual alias binding only the label leaves the field half free, so
		// the new name goes there rather than into a prefix alias beside it,
		// whatever the current language version would otherwise pick.
		desc: "Fill in the free half of a dual postfix alias",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				// A half that binds nothing is spelled with the blank
				// identifier; PostfixAlias.Field is never nil.
				Alias: &ast.PostfixAlias{
					Label: ast.NewIdent("K"),
					Field: ast.NewIdent("_"),
				},
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
					),
				},
			}}
		}(),
		want: `a~(K,a_1): "b"
c: {
	a: {}
	b: a_1
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
		want: `a~(a_1): "b"
c: {
	a: {}
	b: a_1
	c: d
	e: a_1
}
`,
	}, {
		desc:    "Reuse the label alias of a shadowed field",
		version: "v0.17.0",
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
X=a: "b"
Y=q: "b"
c: {
	a: {}
	b: X
	c: d
	e: X
	f: Y
}
`[1:],
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
	a~(a_1): "b"
	b: {
		a: "bar"
		b: a_1
		e: a_1
	}
}
`,
	}, {
		desc: "Add let clause to nested shadowed field with shadowed alias",
		// Resolve both identifiers to same clause.
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewString("b"),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("b"),
					Value: ast.NewStruct(
						field,
						ast.NewIdent("b"), ast.NewStruct(
							ast.NewIdent("a"), ast.NewString("bar"),
							&ast.LetClause{
								Ident: ast.NewIdent("X"),
								Expr:  ast.NewString("baz"),
							},
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
	a~(X): "b"
	b: {
		a: "bar"
		let X = "baz"
		b: X_1
		e: X_1
	}
}
`,
	}, {
		desc:    "Add let clause to nested shadowed field with shadowed label alias",
		version: "v0.17.0",
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
							&ast.LetClause{
								Ident: ast.NewIdent("X"),
								Expr:  ast.NewString("baz"),
							},
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
		let X = "baz"
		b: X_1
		e: X_1
	}
}
`,
	}, {
		desc: "Avoid joining file doc comment to added import declaration",
		// Resolve both identifiers to same clause.
		file: func() *ast.File {
			f := &ast.File{
				Decls: []ast.Decl{
					&ast.Field{
						Label: ast.NewIdent("a"),
						Value: ast.NewSel(
							&ast.Ident{
								Name: "list",
								Node: ast.NewImport(nil, "list"),
							},
							"Min",
						),
					},
				},
			}
			// Note: it's important it's not a doc comment, otherwise
			// it gets joined anyway.
			comment := internal.NewComment(true, "file-level comment")
			comment.Doc = false
			ast.SetComments(f, []*ast.CommentGroup{comment})
			return f
		}(),
		want: `// file-level comment

import "list"

a: list.Min
`,
	}, {
		// A keyword cannot name a let or an alias, but a rewrite from the
		// old prefix aliases, which allowed it, may produce one.
		desc: "Rename lets and postfix aliases named by a keyword",
		file: func() *ast.File {
			let := &ast.LetClause{
				Ident: ast.NewIdent("in"),
				Expr:  ast.NewLit(token.INT, "1"),
			}
			field := &ast.Field{
				Label: ast.NewIdent("g"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("for")},
				Value: ast.NewLit(token.INT, "2"),
			}
			return &ast.File{Decls: []ast.Decl{
				let,
				field,
				&ast.Field{
					Label: ast.NewIdent("in_1"),
					Value: ast.NewLit(token.INT, "3"),
				},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewBinExpr(token.ADD,
						&ast.Ident{Name: "in", Node: let},
						&ast.Ident{Name: "for", Node: field},
					),
				},
			}}
		}(),
		// The names are left as they are, so the result does not parse.
		want: `let in_2 = 1
g~(for_1): 2
in_1:      3
a:         in_2 + for_1
`,
	}, {
		// A reference to a field shadowed where it is used, which reaches it
		// through its alias named by a keyword.
		desc: "Reference a shadowed field through its alias named by a keyword",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("for")},
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
					),
				},
			}}
		}(),
		want: `a~(for_1): "b"
c: {
	a: {}
	b: for_1
}
`,
	}, {
		// A reference to a let named by a keyword, shadowed where it is used
		// by another let of the same name.
		desc: "Reference a shadowed let named by a keyword",
		file: func() *ast.File {
			outer := &ast.LetClause{
				Ident: ast.NewIdent("in"),
				Expr:  ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				outer,
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: &ast.StructLit{Elts: []ast.Decl{
						&ast.LetClause{
							Ident: ast.NewIdent("in"),
							Expr:  ast.NewLit(token.INT, "2"),
						},
						&ast.Field{
							Label: ast.NewIdent("b"),
							Value: &ast.Ident{Name: "in", Node: outer},
						},
					}},
				},
			}}
		}(),
		want: `let in_1 = 1
a: {
	let in_2 = 2
	b: in_1
}
`,
	}, {
		// The name of an import added for a reference is taken, so no new
		// name may be picked as that name.
		desc: "Rename a let clause past the name of an added import",
		file: func() *ast.File {
			spec := &ast.ImportSpec{Name: ast.NewIdent("in_1"), Path: ast.NewString("strings")}
			let := &ast.LetClause{Ident: ast.NewIdent("in"), Expr: ast.NewLit(token.INT, "1")}
			return &ast.File{Decls: []ast.Decl{
				let,
				&ast.Field{Label: ast.NewIdent("y"), Value: &ast.Ident{Name: "in", Node: let}},
				&ast.Field{Label: ast.NewIdent("z"), Value: ast.NewSel(&ast.Ident{Name: "in_1", Node: spec}, "ToUpper")},
			}}
		}(),
		want: `import in_1 "strings"

let in_2 = 1
y: in_2
z: in_1.ToUpper
`,
	}, {
		desc: "Alias a shadowed field past the name of an added import",
		file: func() *ast.File {
			spec := &ast.ImportSpec{Name: ast.NewIdent("a_1"), Path: ast.NewString("strings")}
			outer := &ast.Field{Label: ast.NewIdent("a"), Value: ast.NewLit(token.INT, "1")}
			return &ast.File{Decls: []ast.Decl{
				outer,
				&ast.Field{Label: ast.NewIdent("b"), Value: ast.NewStruct(
					ast.NewIdent("a"), ast.NewLit(token.INT, "2"),
					ast.NewIdent("c"), &ast.Ident{Name: "a", Node: outer.Value},
				)},
				&ast.Field{Label: ast.NewIdent("z"), Value: ast.NewSel(&ast.Ident{Name: "a_1", Node: spec}, "ToUpper")},
			}}
		}(),
		want: `import a_1 "strings"

a~(a_2): 1
b: {
	a: 2
	c: a_2
}
z: a_1.ToUpper
`,
	}, {
		// A field may not be named like an alias in scope, so the alias is
		// renamed, with its references.
		desc: "Rename an alias named like a nested field",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("x"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{
					Label: ast.NewIdent("r"),
					Value: &ast.Ident{Name: "X", Node: field},
				},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						ast.NewIdent("X"), ast.NewStruct(
							ast.NewIdent("c"), &ast.Ident{Name: "X", Node: field},
						),
					),
				},
			}}
		}(),
		want: `x~(X_1): 1
r:       X_1
a:       {X: {c: X_1}}
`,
	}, {
		desc: "Rename a let clause named like a nested field",
		file: func() *ast.File {
			let := &ast.LetClause{
				Ident: ast.NewIdent("X"),
				Expr:  ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						ast.NewIdent("X"), ast.NewLit(token.INT, "2"),
						ast.NewIdent("c"), &ast.Ident{Name: "X", Node: let},
					),
				},
				let,
			}}
		}(),
		want: `a: {
	X: 2
	c: X_1
}
let X_1 = 1
`,
	}, {
		// The field takes the alias's place in the scope, so references to the
		// alias must be renamed by the declaration they are linked to, and one
		// to the field is unshadowed through the alias's new name.
		desc: "Rename an alias named like a later field in the same scope",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("x"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{
					Label: ast.NewIdent("X"),
					Value: ast.NewLit(token.INT, "2"),
				},
				&ast.Field{
					Label: ast.NewIdent("r"),
					Value: &ast.Ident{Name: "X", Node: field},
				},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						ast.NewIdent("x"), ast.NewStruct(
							ast.NewIdent("c"), &ast.Ident{Name: "x", Node: field.Value},
						),
					),
				},
			}}
		}(),
		want: `x~(X_1): 1
X:       2
r:       X_1
a:       {x: {c: X_1}}
`,
	}, {
		desc: "Rename an alias named like an earlier field in the same scope",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("x"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "1"),
			}
			shadowing := &ast.Field{
				Label: ast.NewIdent("X"),
				Value: ast.NewLit(token.INT, "2"),
			}
			return &ast.File{Decls: []ast.Decl{
				shadowing,
				field,
				&ast.Field{
					Label: ast.NewIdent("r"),
					Value: &ast.Ident{Name: "X", Node: field},
				},
				&ast.Field{
					Label: ast.NewIdent("s"),
					Value: &ast.Ident{Name: "X", Node: shadowing.Value},
				},
			}}
		}(),
		want: `X:       2
x~(X_1): 1
r:       X_1
s:       X
`,
	}, {
		// An alias shadowing an alias of its name is in scope of a field of
		// that name declared past the aliases all the same.
		desc: "Rename an alias shadowing an alias named like a field",
		file: func() *ast.File {
			outer := &ast.LetClause{Ident: ast.NewIdent("X"), Expr: ast.NewLit(token.INT, "2")}
			inner := &ast.LetClause{Ident: ast.NewIdent("X"), Expr: ast.NewLit(token.INT, "3")}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{Label: ast.NewIdent("X"), Value: ast.NewLit(token.INT, "1")},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						outer,
						ast.NewIdent("r1"), &ast.Ident{Name: "X", Node: outer},
						ast.NewIdent("b"), ast.NewStruct(
							inner,
							ast.NewIdent("r2"), &ast.Ident{Name: "X", Node: inner},
						),
					),
				},
			}}
		}(),
		want: `X: 1
a: {
	let X_1 = 2
	r1: X_1
	b: {
		let X_2 = 3
		r2: X_2
	}
}
`,
	}, {
		// The import is only declared as the reference to it is reached, so
		// the conflict with the let clause is found once the names of the file
		// are gathered.
		desc: "Rename a let clause named like an import added for a reference",
		file: func() *ast.File {
			spec := &ast.ImportSpec{Path: ast.NewString("strings")}
			let := &ast.LetClause{
				Ident: ast.NewIdent("strings"),
				Expr:  ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("c"),
					Value: ast.NewSel(&ast.Ident{Name: "strings", Node: spec}, "ToUpper"),
				},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: &ast.StructLit{Elts: []ast.Decl{
						let,
						&ast.Field{
							Label: ast.NewIdent("b"),
							Value: &ast.Ident{Name: "strings", Node: let},
						},
					}},
				},
			}}
		}(),
		want: `import "strings"

c: strings.ToUpper
a: {
	let strings_1 = 1
	b: strings_1
}
`,
	}, {
		// The import is in scope once the names of the file are gathered, so
		// the let clause is renamed even where it is declared before the
		// reference the import is added for.
		desc: "Rename a let clause declared before the reference an import is added for",
		file: func() *ast.File {
			spec := &ast.ImportSpec{Path: ast.NewString("strings")}
			let := &ast.LetClause{
				Ident: ast.NewIdent("strings"),
				Expr:  ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: &ast.StructLit{Elts: []ast.Decl{
						let,
						&ast.Field{
							Label: ast.NewIdent("b"),
							Value: &ast.Ident{Name: "strings", Node: let},
						},
					}},
				},
				&ast.Field{
					Label: ast.NewIdent("c"),
					Value: ast.NewSel(&ast.Ident{Name: "strings", Node: spec}, "ToUpper"),
				},
			}}
		}(),
		want: `import "strings"

a: {
	let strings_1 = 1
	b: strings_1
}
c: strings.ToUpper
`,
	}, {
		desc: "Rename an alias named like a field past the name of an added import",
		file: func() *ast.File {
			spec := &ast.ImportSpec{Name: ast.NewIdent("X_1"), Path: ast.NewString("strings")}
			field := &ast.Field{
				Label: ast.NewIdent("x"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "1"),
			}
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{Label: ast.NewIdent("X"), Value: ast.NewLit(token.INT, "2")},
				&ast.Field{Label: ast.NewIdent("y"), Value: &ast.Ident{Name: "X", Node: field}},
				&ast.Field{Label: ast.NewIdent("z"), Value: ast.NewSel(&ast.Ident{Name: "X_1", Node: spec}, "ToUpper")},
			}}
		}(),
		want: `import X_1 "strings"

x~(X_2): 1
X:       2
y:       X_2
z:       X_1.ToUpper
`,
	}, {
		// A field is in scope of every alias of its name, so an alias
		// shadowed by the one in conflict with the field is renamed too.
		desc: "Rename every alias in scope named like a nested field",
		file: func() *ast.File {
			outer := &ast.Field{
				Label: ast.NewIdent("x"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "1"),
			}
			inner := &ast.Field{
				Label: ast.NewIdent("y"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "2"),
			}
			return &ast.File{Decls: []ast.Decl{
				outer,
				&ast.Field{
					Label: ast.NewIdent("r1"),
					Value: &ast.Ident{Name: "X", Node: outer},
				},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						inner,
						ast.NewIdent("r2"), &ast.Ident{Name: "X", Node: inner},
						ast.NewIdent("X"), ast.NewLit(token.INT, "3"),
					),
				},
			}}
		}(),
		want: `x~(X_1): 1
r1:      X_1
a: {
	y~(X_2): 2
	r2:      X_2
	X:       3
}
`,
	}, {
		// A clause declares no struct to insert a let clause in, so a variable
		// shadowed where it is referred to is renamed, with every reference.
		desc: "Rename a comprehension variable shadowed by a field",
		file: func() *ast.File {
			v := ast.NewIdent("v")
			return &ast.File{Decls: []ast.Decl{
				&ast.Comprehension{
					Clauses: []ast.Clause{&ast.ForClause{
						Value:  v,
						Source: ast.NewList(ast.NewLit(token.INT, "1")),
					}},
					Value: ast.NewStruct(
						ast.NewIdent("v"), ast.NewStruct(
							ast.NewIdent("c"), &ast.Ident{Name: "v", Node: v},
						),
						ast.NewIdent("d"), &ast.Ident{Name: "v", Node: v},
					),
				},
			}}
		}(),
		want: `unexpected link type *ast.ForClause`,
	}, {
		desc: "Rename a comprehension key and let variable shadowed by a field",
		file: func() *ast.File {
			k := ast.NewIdent("k")
			v := ast.NewIdent("v")
			w := ast.NewIdent("w")
			return &ast.File{Decls: []ast.Decl{
				&ast.Comprehension{
					Clauses: []ast.Clause{
						&ast.ForClause{
							Key:    k,
							Value:  v,
							Source: ast.NewStruct(ast.NewIdent("a"), ast.NewLit(token.INT, "1")),
						},
						&ast.LetClause{
							Ident: w,
							Expr:  &ast.Ident{Name: "v", Node: v},
						},
					},
					Value: ast.NewStruct(
						ast.NewIdent("k"), &ast.Ident{Name: "k", Node: k},
						ast.NewIdent("w"), &ast.Ident{Name: "w", Node: w},
						ast.NewIdent("v"), &ast.Ident{Name: "v", Node: v},
					),
				},
			}}
		}(),
		want: `unexpected link type *ast.ForClause`,
	}, {
		// The field keeps the name in the scope, so a reference to it in
		// the value of the let clause renamed for it still refers to it.
		desc: "Refer to a field from a let clause renamed for its name",
		file: func() *ast.File {
			inner := &ast.Field{
				Label: ast.NewIdent("X"),
				Value: ast.NewLit(token.INT, "1"),
			}
			let := &ast.LetClause{
				Ident: ast.NewIdent("X"),
				Expr:  ast.NewList(&ast.Ident{Name: "X", Node: inner.Value}),
			}
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{Label: ast.NewIdent("X"), Value: ast.NewLit(token.INT, "0")},
				&ast.Field{
					Label: ast.NewIdent("a"),
					Value: ast.NewStruct(
						inner,
						let,
						ast.NewIdent("r"), &ast.Ident{Name: "X", Node: let},
					),
				},
			}}
		}(),
		want: `X: 0
a: {
	X: 1
	let X_1 = [X]
	r: X_1
}
`,
	}, {
		// A field may be named like a comprehension variable, but not like
		// an alias, so the alias is renamed in a conflict between the two.
		desc: "Rename an alias rather than a comprehension variable of its name",
		file: func() *ast.File {
			field := &ast.Field{
				Label: ast.NewIdent("a"),
				Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
				Value: ast.NewLit(token.INT, "1"),
			}
			v := ast.NewIdent("X")
			return &ast.File{Decls: []ast.Decl{
				field,
				&ast.Field{
					Label: ast.NewIdent("b"),
					Value: &ast.StructLit{Elts: []ast.Decl{&ast.Comprehension{
						Clauses: []ast.Clause{&ast.ForClause{
							Value:  v,
							Source: ast.NewList(ast.NewLit(token.INT, "1")),
						}},
						Value: ast.NewStruct(
							ast.NewIdent("X"), ast.NewLit(token.INT, "2"),
							ast.NewIdent("r"), &ast.Ident{Name: "X", Node: field},
						),
					}}},
				},
			}}
		}(),
		want: `a~(X_1): 1
b: {for X in [1] {
	X: 2
	r: X_1
}}
`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			var got string
			func() {
				defer func() {
					if r := recover(); r != nil {
						got = fmt.Sprint(r)
					}
				}()
				err := astutil.Sanitize(tc.file)
				if err != nil {
					t.Fatal(err)
				}

				b, errs := format.Node(tc.file)
				if errs != nil {
					t.Fatal(errs)
				}

				got = string(b)

				// The output must be well-formed, as Sanitize promises.
				_, err = parser.ParseFile("out.cue", b, parser.Version(tc.version))
				qt.Assert(t, qt.IsNil(err))
			}()
			qt.Assert(t, qt.Equals(got, tc.want))
		})
	}
}

// TestSanitizeAmbiguousReference checks that a reference by name alone to a
// name which an alias and a field shared in one scope is an error, rather
// than silently referring to the field once the alias is renamed.
func TestSanitizeAmbiguousReference(t *testing.T) {
	// The field is declared once or twice, with the alias anywhere among
	// its declarations: a repeated declaration of the field must keep
	// the ambiguity of the one it replaces.
	for _, fields := range []int{1, 2} {
		for pos := 0; pos <= fields; pos++ {
			var decls []ast.Decl
			for i := 0; i <= fields; i++ {
				if i == pos {
					decls = append(decls, &ast.Field{
						Label: ast.NewIdent("x"),
						Alias: &ast.PostfixAlias{Field: ast.NewIdent("X")},
						Value: ast.NewLit(token.INT, "1"),
					})
				} else {
					decls = append(decls, &ast.Field{Label: ast.NewIdent("X"), Value: ast.NewLit(token.INT, "2")})
				}
			}
			decls = append(decls, &ast.Field{Label: ast.NewIdent("r"), Value: ast.NewIdent("X")})
			err := astutil.Sanitize(&ast.File{Decls: decls})
			qt.Assert(t, qt.ErrorMatches(err, `reference "X" is ambiguous: a declaration of that name was renamed`),
				qt.Commentf("%d fields, alias at %d", fields, pos))
		}
	}
}

func TestSanitizeCrossFileShadowing(t *testing.T) {
	testCases := []struct {
		desc     string
		file     *ast.File
		pkgFiles []*ast.File
		want     string
	}{{
		desc: "Predeclared self shadowed by top-level field in another file",
		file: func() *ast.File {
			// File with a reference to predeclared "self"
			selfIdent := ast.NewPredeclared("self")
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("foo"),
					Value: ast.NewStruct(
						&ast.LetClause{
							Ident: ast.NewIdent("X"),
							Expr:  selfIdent,
						},
						ast.NewIdent("a"), ast.NewIdent("X"),
					),
				},
			}}
		}(),
		pkgFiles: []*ast.File{{
			// Another file in the package with top-level "self" field
			Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("self"),
					Value: ast.NewLit(token.INT, "42"),
				},
			},
		}},
		want: `foo: {
	let X = __self
	a: X
}
`,
	}, {
		desc: "Predeclared self not shadowed when no cross-file conflict",
		file: func() *ast.File {
			selfIdent := ast.NewPredeclared("self")
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("foo"),
					Value: ast.NewStruct(
						&ast.LetClause{
							Ident: ast.NewIdent("X"),
							Expr:  selfIdent,
						},
						ast.NewIdent("a"), ast.NewIdent("X"),
					),
				},
			}}
		}(),
		pkgFiles: []*ast.File{{
			// Another file without "self" field
			Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("other"),
					Value: ast.NewLit(token.INT, "42"),
				},
			},
		}},
		want: `foo: {
	let X = self
	a: X
}
`,
	}, {
		desc: "Predeclared self shadowed by aliased field in another file",
		file: func() *ast.File {
			selfIdent := ast.NewPredeclared("self")
			return &ast.File{Decls: []ast.Decl{
				&ast.Field{
					Label: ast.NewIdent("foo"),
					Value: ast.NewStruct(
						&ast.LetClause{
							Ident: ast.NewIdent("X"),
							Expr:  selfIdent,
						},
						ast.NewIdent("a"), ast.NewIdent("X"),
					),
				},
			}}
		}(),
		pkgFiles: []*ast.File{{
			// Another file with aliased "self" field (X=self: ...)
			Decls: []ast.Decl{
				&ast.Field{
					Label: &ast.Alias{
						Ident: ast.NewIdent("X"),
						Expr:  ast.NewIdent("self"),
					},
					Value: ast.NewLit(token.INT, "42"),
				},
			},
		}},
		want: `foo: {
	let X = __self
	a: X
}
`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			var err error
			if tc.pkgFiles != nil {
				err = astutil.SanitizeFiles(append(tc.pkgFiles, tc.file))
			} else {
				err = astutil.Sanitize(tc.file)
			}
			if err != nil {
				t.Fatal(err)
			}

			b, errs := format.Node(tc.file)
			if errs != nil {
				t.Fatal(errs)
			}

			got := string(b)
			qt.Assert(t, qt.Equals(got, tc.want))
		})
	}
}

// TestSanitizeFuncSignatureImports tests that an import that is referenced
// only from a parameter constraint or only from the return type of a function
// literal is marked as used, so that Sanitize does not strip it.
func TestSanitizeFuncSignatureImports(t *testing.T) {
	const src = `@experiment(functions)

import (
	"time"
	"math"
)

f: func(x: time.Duration) -> math.MaxFloat64: 1.0
`
	f, err := parser.ParseFile("test.cue", src, parser.ParseComments)
	qt.Assert(t, qt.IsNil(err))

	qt.Assert(t, qt.IsNil(astutil.Sanitize(f)))

	b, err := format.Node(f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(b), src))
}

// TestSanitizeFuncParamDefaultImports tests that an import referenced only
// from a parameter default is marked as used, so that Sanitize does not strip
// it. The default is resolved in the same scope as the parameter constraint.
func TestSanitizeFuncParamDefaultImports(t *testing.T) {
	const src = `@experiment(functions)

import "time"

f: func(x: int = time.Second) -> int: x
`
	f, err := parser.ParseFile("test.cue", src, parser.ParseComments)
	qt.Assert(t, qt.IsNil(err))

	qt.Assert(t, qt.IsNil(astutil.Sanitize(f)))

	b, err := format.Node(f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.StringContains(string(b), `import "time"`))
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

// TestSanitizeAliasSyntax checks that an alias Sanitize introduces is written
// in the syntax the target language version accepts, since neither the prefix
// nor the postfix form parses at every version.
func TestSanitizeAliasSyntax(t *testing.T) {
	// build parses a file at the given version and adds a reference to the
	// outer "a" from inside "c", already resolved past the inner "a" that
	// shadows it. Sanitize has to bind the outer field to a fresh name for
	// that reference to keep working.
	build := func(t *testing.T, attr, version string) *ast.File {
		t.Helper()
		f, err := parser.ParseFile("in.cue", attr+`a: "b"
c: {
	a: {}
}
`, parser.ParseComments, parser.Version(version))
		qt.Assert(t, qt.IsNil(err))
		var fields []*ast.Field
		for _, d := range f.Decls {
			if x, ok := d.(*ast.Field); ok {
				fields = append(fields, x)
			}
		}
		outer := fields[0]
		inner := fields[1].Value.(*ast.StructLit)
		inner.Elts = append(inner.Elts, &ast.Field{
			Label: ast.NewIdent("b"),
			Value: &ast.Ident{Name: "a", Node: outer.Value},
		})
		return f
	}

	testCases := []struct {
		desc string
		// attr is a file-level attribute prepended to the source.
		attr string
		// parseAt is the version the source is parsed at, and outAt the one
		// the result must parse at; they differ when opts override the file.
		parseAt string
		outAt   string
		// setVersion is recorded on the built file, standing in for the
		// version a parsed file would carry.
		setVersion string
		built      bool
		want       string
	}{{
		desc:    "prefix form below the stable version",
		parseAt: "v0.17.0",
		outAt:   "v0.17.0",
		want:    "a_1=a",
	}, {
		desc:    "postfix form where the attribute enables the experiment",
		attr:    "@experiment(aliasv2)\n",
		parseAt: "v0.17.0",
		outAt:   "v0.17.0",
		want:    "a~(a_1)",
	}, {
		desc: "postfix form at the current version",
		want: "a~(a_1)",
	}, {
		// A file that was built rather than parsed carries whatever version
		// it was told it is written for.
		desc:       "a built file carries the version it is given",
		built:      true,
		setVersion: "v0.17.0",
		outAt:      "v0.17.0",
		want:       "a_1=a",
	}, {
		desc:  "a built file with no version targets the current one",
		built: true,
		want:  "a~(a_1)",
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			f := build(t, tc.attr, tc.parseAt)
			if tc.built {
				// A file an exporter assembled: its declarations keep the
				// positions they were parsed with, and it carries the
				// experiments it is written for.
				f = &ast.File{Decls: f.Decls}
				exp, err := cueexperiment.NewFile(tc.setVersion)
				qt.Assert(t, qt.IsNil(err))
				f.SetExperiments(exp)
			}
			qt.Assert(t, qt.IsNil(astutil.Sanitize(f)))

			b, errs := format.Node(f)
			qt.Assert(t, qt.IsNil(errs))
			qt.Assert(t, qt.StringContains(string(b), tc.want))

			// Whatever it emitted must parse back at the target version.
			_, err := parser.ParseFile("out.cue", b, parser.Version(tc.outAt))
			qt.Assert(t, qt.IsNil(err))
		})
	}
}

// TestSanitizeFilesAccumulates checks that SanitizeFiles reports every file
// that fails to sanitize and still sanitizes the files that can be, rather
// than stopping at the first failure and leaving the rest untouched.
func TestSanitizeFilesAccumulates(t *testing.T) {
	// Two let clauses of the same name in one scope cannot be sanitized;
	// the parser rejects this too, so the files are built.
	conflict := func(name string) *ast.File {
		return &ast.File{Decls: []ast.Decl{
			&ast.LetClause{Ident: ast.NewIdent(name), Expr: ast.NewLit(token.INT, "1")},
			&ast.LetClause{Ident: ast.NewIdent(name), Expr: ast.NewLit(token.INT, "2")},
		}}
	}
	spec := ast.NewImport(nil, "list")
	good := &ast.File{Decls: []ast.Decl{
		&ast.Field{
			Label: ast.NewIdent("a"),
			Value: ast.NewCall(ast.NewSel(&ast.Ident{Name: "list", Node: spec}, "Min"),
				ast.NewList(ast.NewLit(token.INT, "1"))),
		},
	}}

	err := astutil.SanitizeFiles([]*ast.File{conflict("X"), conflict("Y"), good})
	qt.Assert(t, qt.IsNotNil(err))
	msgs := []string{}
	for _, e := range errors.Errors(err) {
		msgs = append(msgs, e.Error())
	}
	qt.Assert(t, qt.HasLen(msgs, 2))
	qt.Assert(t, qt.StringContains(msgs[0], `"X"`))
	qt.Assert(t, qt.StringContains(msgs[1], `"Y"`))

	// The file after the failures was sanitized: its import was inserted.
	b, ferr := format.Node(good)
	qt.Assert(t, qt.IsNil(ferr))
	qt.Assert(t, qt.StringContains(string(b), `import "list"`))
}
