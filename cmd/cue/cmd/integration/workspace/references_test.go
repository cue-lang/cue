package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"

	"github.com/go-quicktest/qt"
)

// TestReferences checks that querying for references will load
// packages within the current module and search them for references.
func TestReferences(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.14.0"
-- a/a.cue --
package a

out: 3
a1: a2: out
-- b/b1.cue --
package b

import "example.com/bar/a"

b1: a.out
-- b/b2.cue --
package b

import "example.com/bar/a:a"

b2: a.out
-- b/b3.cue --
package b

import mya "example.com/bar/a"

b3: mya.out
-- c/c.cue --
package c

import p1 "example.com/bar/a"
import p2 "example.com/bar/a"
import p3 "example.com/bar/a"

c1: p1.out
c2: p2.out
c3: p3.out
-- nested/cue.mod/module.cue --
module: "example.org/nested"
language: version: "v0.14.0"
-- nested/dontload.cue --
package dontload

// NB these nested files appear to do nothing. They have been added to
// test that when FindReferences triggers loading of all packages
// within the current module, we do not accidentally walk down into
// nested modules, which could previously provoke infinite loops when
// packages are loaded.

greeting: "hello"
`

	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
		)

		// Now perform a find-references from the open a/a.cue file,
		// from the "out" of "a1: a2: out".
		from := protocol.Location{
			URI:   rootURI + "/a/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 3, Character: 8}},
		}

		wantTo := []protocol.Location{
			{
				URI: rootURI + "/a/a.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 0},
					End:   protocol.Position{Line: 2, Character: 3},
				},
			},
			{
				URI: rootURI + "/a/a.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 3, Character: 8},
					End:   protocol.Position{Line: 3, Character: 11},
				},
			},
			{
				URI: rootURI + "/b/b1.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 6},
					End:   protocol.Position{Line: 4, Character: 9},
				},
			},
			{
				URI: rootURI + "/b/b2.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 6},
					End:   protocol.Position{Line: 4, Character: 9},
				},
			},
			{
				URI: rootURI + "/b/b3.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 8},
					End:   protocol.Position{Line: 4, Character: 11},
				},
			},
			{
				URI: rootURI + "/c/c.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 6, Character: 7},
					End:   protocol.Position{Line: 6, Character: 10},
				},
			},
			{
				URI: rootURI + "/c/c.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 7, Character: 7},
					End:   protocol.Position{Line: 7, Character: 10},
				},
			},
			{
				URI: rootURI + "/c/c.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 8, Character: 7},
					End:   protocol.Position{Line: 8, Character: 10},
				},
			},
		}

		gotTo := env.References(from)
		qt.Assert(t, qt.DeepEquals(gotTo, wantTo), qt.Commentf("from: %#v", from))

		env.Await(
			// Make sure we did not create a package dontload: that pkg
			// only belongs to the nested module.
			I.NoLogExactf(protocol.Debug, "Package dirs=[%v/nested] importPath=example.com/bar/nested@v0:dontload", rootURI),
		)
	})
}

// TestReferencesAncestorPackage checks that definitions, references
// and renames work in a file which belongs to two packages: the
// package in its own directory, and the package of the same name in a
// child directory, which includes the files of its ancestors. Each
// package can be reloaded without the other.
func TestReferencesAncestorPackage(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.14.0"
-- a/x.cue --
package a

foo: 1
bar: foo
-- a/b/y.cue --
package a

baz: 2
`

	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		// fooLocations holds the location of the declaration of foo,
		// of its usage in "bar: foo", and of the start of that usage,
		// to query from.
		type fooLocations struct {
			decl, usage, from protocol.Location
		}
		// locations returns the fooLocations when foo is declared on
		// the given line of a/x.cue.
		locations := func(line uint32) fooLocations {
			decl := protocol.Location{
				URI: rootURI + "/a/x.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: line, Character: 0},
					End:   protocol.Position{Line: line, Character: 3},
				},
			}
			usage := protocol.Location{
				URI: rootURI + "/a/x.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: line + 1, Character: 5},
					End:   protocol.Position{Line: line + 1, Character: 8},
				},
			}
			from := protocol.Location{URI: usage.URI, Range: protocol.Range{Start: usage.Range.Start}}
			return fooLocations{decl, usage, from}
		}
		loc := locations(2)

		// Finding references loads the package in a/b too.
		// Wrong: the references are the declaration and the usage.
		env.OpenFile("a/x.cue")
		env.Await(env.DoneWithOpen())
		qt.Check(t, qt.DeepEquals(env.Definition(loc.from), []protocol.Location{loc.decl}))
		qt.Check(t, qt.DeepEquals(env.References(loc.from), []protocol.Location(nil)))

		// Opening or editing a/b/y.cue reloads only the package in a/b.
		// Wrong: the definition and the references are as above.
		env.OpenFile("a/b/y.cue")
		env.Await(env.DoneWithOpen())
		qt.Check(t, qt.DeepEquals(env.Definition(loc.from), []protocol.Location(nil)))
		qt.Check(t, qt.DeepEquals(env.References(loc.from), []protocol.Location(nil)))

		env.EditBuffer("a/b/y.cue", fake.NewEdit(0, 0, 0, 0, "\n"))
		env.Await(env.DoneWithChange())
		qt.Check(t, qt.DeepEquals(env.Definition(loc.from), []protocol.Location(nil)))
		qt.Check(t, qt.DeepEquals(env.References(loc.from), []protocol.Location(nil)))

		// Editing a/x.cue reloads both packages.
		env.EditBuffer("a/x.cue", fake.NewEdit(0, 0, 0, 0, "\n"))
		env.Await(env.DoneWithChange())
		loc = locations(3)
		qt.Check(t, qt.DeepEquals(env.Definition(loc.from), []protocol.Location{loc.decl}))
		qt.Check(t, qt.DeepEquals(env.References(loc.from), []protocol.Location{loc.decl, loc.usage}))

		// Wrong: foo can be renamed, which edits a/x.cue.
		env.EditBuffer("a/b/y.cue", fake.NewEdit(0, 0, 0, 0, "\n"))
		env.Await(env.DoneWithChange())
		pos := protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: loc.decl.URI},
			Position:     loc.decl.Range.Start,
		}
		placeholder, err := env.Editor.Server.PrepareRename(env.Ctx, &protocol.PrepareRenameParams{TextDocumentPositionParams: pos})
		qt.Assert(t, qt.IsNil(err))
		qt.Check(t, qt.IsNil(placeholder))
		edit, err := env.Editor.Server.Rename(env.Ctx, &protocol.RenameParams{
			TextDocument: pos.TextDocument,
			Position:     pos.Position,
			NewName:      "qux",
		})
		qt.Assert(t, qt.IsNil(err))
		qt.Check(t, qt.IsNil(edit))
	})
}
