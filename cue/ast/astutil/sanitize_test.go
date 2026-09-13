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
	list_9 "list"
)

{list: list_9.Min()}
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
		want: `import list_9 "list"

a: {list: list_9.Min()}
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
	bar_9 "foo/bar"
	bar_B "foo"
)

a: {
	b: bar.A()
	c: bar_9.A()
	d: bar_B.A()
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
	bar_9 "bar"
	bar_B "foo/bar"
)

a: {
	b: bar_9.A()
	c: bar_B.A()
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
	b: c_9
	c: d
}

let c_9 = c
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
		want: `a~(a_9): "b"
c: {
	a: {}
	b: a_9
	c: d
}
`,
	}, {
		// The field already binds a name, so that name is reused through a
		// let clause rather than a second alias being introduced.
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
		want: `let X_9 = X
a~(X): "b"
c: {
	a: {}
	b: X_9
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
		want: `let X_9 = X
a~(X): "b"
c: {
	let X = "shadow"
	b: X_9
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
		want: `a~(K,a_9): "b"
c: {
	a: {}
	b: a_9
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
		want: `a~(a_9): "b"
c: {
	a: {}
	b: a_9
	c: d
	e: a_9
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
let X_9 = X
X=a: "b"
Y=q: "b"
c: {
	a: {}
	b: X
	c: d
	e: X_9
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
	a~(a_9): "b"
	b: {
		a: "bar"
		b: a_9
		e: a_9
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
	let X_9 = X
	X=a: "b"
	b: {
		a: "bar"
		b: X_9
		e: X_9
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
			qt.Assert(t, qt.Equals(got, tc.want))
		})
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
		want:    "a_9=a",
	}, {
		desc:    "postfix form where the attribute enables the experiment",
		attr:    "@experiment(aliasv2)\n",
		parseAt: "v0.17.0",
		outAt:   "v0.17.0",
		want:    "a~(a_9)",
	}, {
		desc: "postfix form at the current version",
		want: "a~(a_9)",
	}, {
		// A file that was built rather than parsed carries whatever version
		// it was told it is written for.
		desc:       "a built file carries the version it is given",
		built:      true,
		setVersion: "v0.17.0",
		outAt:      "v0.17.0",
		want:       "a_9=a",
	}, {
		desc:  "a built file with no version targets the current one",
		built: true,
		want:  "a~(a_9)",
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
