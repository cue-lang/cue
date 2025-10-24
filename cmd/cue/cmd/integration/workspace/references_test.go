package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"

	"github.com/go-quicktest/qt"
)

// TestReferences checks that querying for references will load
// packages within the current module and search them for references.
func TestReferences(t *testing.T) {
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
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
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
