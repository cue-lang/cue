package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

// TestImportCreatedLater tests that a package with an import which
// does not (yet) resolve, recovers once the imported package comes
// into existence: the importing package must be reloaded so that the
// import resolves, without requiring any edit to the importing
// package itself.
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
			// The importing package must be reloaded so that its
			// import now resolves.
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Definitions on "y" within "ax: sub.y" must arrive at
		// sub/sub.cue.
		gotDefs := env.Definition(protocol.Location{
			URI:   rootURI + "/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 8}},
		})
		wantDefs := []protocol.Location{{
			URI: rootURI + "/sub/sub.cue",
			Range: protocol.Range{
				Start: protocol.Position{Line: 2, Character: 0},
				End:   protocol.Position{Line: 2, Character: 1},
			},
		}}
		qt.Assert(t, qt.DeepEquals(gotDefs, wantDefs))
	})
}

// TestImportCreatedLaterOnDisk is like TestImportCreatedLater,
// except the imported package is created on disk (not in the
// editor), in a directory which contains no other active files. The
// file's creation must still be noticed, because a loaded package
// has an unresolved import which it helps satisfy.
func TestImportCreatedLaterOnDisk(t *testing.T) {
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

		// Now create the imported package on disk, without opening
		// any of its files in the editor.
		env.WriteWorkspaceFile("sub/sub.cue", "package sub\n\ny: 4\n")
		env.Await(
			env.DoneWithChangeWatchedFiles(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/sub] importPath=mod.example/x/sub@v0 Reloaded", rootURI),
			// The importing package must be reloaded so that its
			// import now resolves.
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		gotDefs := env.Definition(protocol.Location{
			URI:   rootURI + "/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 8}},
		})
		wantDefs := []protocol.Location{{
			URI: rootURI + "/sub/sub.cue",
			Range: protocol.Range{
				Start: protocol.Position{Line: 2, Character: 0},
				End:   protocol.Position{Line: 2, Character: 1},
			},
		}}
		qt.Assert(t, qt.DeepEquals(gotDefs, wantDefs))
	})
}
