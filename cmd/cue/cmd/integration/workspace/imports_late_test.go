package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

// TestImportCreatedLater shows bad behaviour: a package with an
// import which does not (yet) resolve never recovers when the
// imported package comes into existence. Nothing links the new
// package back to its failed importers, so the import stays dead -
// no definitions, hover or completion through it - until the
// importing package happens to be edited for some other reason.
func TestImportCreatedLater(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
package a

import "mod.example/x/sub"

ax: sub.y
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Now create the imported package.
		env.CreateBuffer("sub/sub.cue", "package sub\n\ny: 4\n")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/sub] importPath=mod.example/x/sub@v0 Reloaded", rootURI),
			// The importing package is not reloaded: its import
			// stays unresolved.
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Definitions on "y" within "ax: sub.y" find nothing.
		gotDefs := env.Definition(protocol.Location{
			URI:   rootURI + "/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 8}},
		})
		qt.Assert(t, qt.HasLen(gotDefs, 0))
	})
}
