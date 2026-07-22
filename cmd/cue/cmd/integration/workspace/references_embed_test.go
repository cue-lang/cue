package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

// TestReferencesEmbedCoEmbedders shows bad behaviour: find-references
// within a file which is embedded by two different packages silently
// misses results after one of the embedding packages has been
// edited. Editing one embedder resets the embedded package's
// evaluator, but not the other embedder's, whose usage records
// within the embedded package are lost: its own memoized state
// prevents them from being re-recorded.
func TestReferencesEmbedCoEmbedders(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data.json)

ax: out.field
-- b.cue --
@extern(embed)
package b

out: _ @embed(file=data/data.json)

bx: out.field
-- data/data.json --
{"field": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.OpenFile("b.cue")
		env.OpenFile("data/data.json")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:b Reloaded", rootURI),
		)

		// "field" within `bx: out.field` in b.cue.
		fromB := protocol.Location{
			URI:   rootURI + "/b.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 5, Character: 8}},
		}
		// "field" within the data.json object.
		fromJSON := protocol.Location{
			URI:   rootURI + "/data/data.json",
			Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 3}},
		}

		wantTo := []protocol.Location{
			{
				URI: rootURI + "/a.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Character: 8},
					End:   protocol.Position{Line: 5, Character: 13},
				},
			},
			{
				URI: rootURI + "/b.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Character: 8},
					End:   protocol.Position{Line: 5, Character: 13},
				},
			},
			{
				URI: rootURI + "/data/data.json",
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 2},
					End:   protocol.Position{Line: 0, Character: 7},
				},
			},
		}

		// Force evaluation of package b (and its usage-recording
		// within the embedded package's evaluator).
		gotTo := env.References(fromB)
		qt.Assert(t, qt.ContentEquals(gotTo, wantTo))

		// Now edit a.cue: this reloads package a, resetting the
		// embedded data.json package's evaluator.
		env.RegexpReplace("a.cue", "ax:", "ay:")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// References within data.json should still find the usages in
		// both embedding packages, but b.cue's usage has been lost.
		wantToStale := []protocol.Location{wantTo[0], wantTo[2]}
		gotTo = env.References(fromJSON)
		qt.Assert(t, qt.ContentEquals(gotTo, wantToStale))
	})
}
