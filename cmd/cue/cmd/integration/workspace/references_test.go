package workspace

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

const referencesFiles = `
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
-- want/a/a.cue --
package a

result: 3
a1: a2: result
-- want/b/b1.cue --
package b

import "example.com/bar/a"

b1: a.result
-- want/b/b2.cue --
package b

import "example.com/bar/a:a"

b2: a.result
-- want/b/b3.cue --
package b

import mya "example.com/bar/a"

b3: mya.result
-- want/c/c.cue --
package c

import p1 "example.com/bar/a"
import p2 "example.com/bar/a"
import p3 "example.com/bar/a"

c1: p1.result
c2: p2.result
c3: p3.result
`

// TestReferences checks that querying for references will load
// packages within the current module and search them for references.
func TestReferences(t *testing.T) {
	WithOptions(RootURIAsDefaultFolder()).Run(t, referencesFiles, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
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
		qt.Assert(t, qt.ContentEquals(gotTo, wantTo), qt.Commentf("from: %#v", from))
	})
}

func TestRename(t *testing.T) {
	WithOptions(RootURIAsDefaultFolder()).Run(t, referencesFiles, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.OpenFile("c/c.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
		)

		result, err := env.Editor.Server.PrepareRename(env.Ctx, &protocol.PrepareRenameParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: rootURI + "/c/c.cue",
				},
				Position: protocol.Position{Line: 6, Character: 8}, // the u of out
			},
		})
		qt.Assert(t, qt.IsNil(err))
		// PrepareRename should return the range which is the whole of out
		qt.Assert(t, qt.DeepEquals(result.Range, protocol.Range{
			Start: protocol.Position{Line: 6, Character: 7},
			End:   protocol.Position{Line: 6, Character: 10},
		}))

		// Now do the rename for real
		env.Rename(protocol.Location{
			URI:   rootURI + "/c/c.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 6, Character: 8}}, // the u of out
		}, "result")

		for _, file := range txtar.Parse([]byte(referencesFiles)).Files {
			if filename, ok := strings.CutPrefix(file.Name, "want/"); ok {
				content, _ := env.Editor.BufferText(filename)
				qt.Check(t, qt.Equals(content, string(file.Data)), qt.Commentf("%s", filename))
			}
		}
	})
}
