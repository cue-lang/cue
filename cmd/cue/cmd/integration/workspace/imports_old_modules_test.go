package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"

	"github.com/go-quicktest/qt"
)

// TestImportsOldModules checks that imports using the old modules
// system works: the packages get correctly loaded and analyse.
func TestImportsOldModules(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
-- a/a.cue --
package a

import "example.com/foo/x"

out: x.y
-- cue.mod/pkg/example.com/foo/x/y.cue --
package x

y: int
-- cue.mod/usr/example.com/foo/x/y.cue --
package x

y: 5
`

	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.Await(
			LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
		)
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Reloaded", rootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 For file %v/a/a.cue found [Package dirs=[%v/a] importPath=example.com/bar/a@v0]", rootURI, rootURI, rootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dirs=[%v/a] importPath=example.com/bar/a@v0", rootURI, rootURI),
			// A package is created for the imported package.
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dirs=[%v/cue.mod/gen/example.com/foo/x %v/cue.mod/pkg/example.com/foo/x %v/cue.mod/usr/example.com/foo/x] importPath=example.com/foo/x", rootURI, rootURI, rootURI, rootURI),
		)
		// Now perform a jump-to-dfn from the open a.cue file,
		// from the "y" in "out: x.y", which should take us to the two
		// files in the old module package.
		from := protocol.Location{
			URI:   rootURI + "/a/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 7}},
		}

		wantTo := []protocol.Location{
			{
				URI: rootURI + "/cue.mod/pkg/example.com/foo/x/y.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 0},
					End:   protocol.Position{Line: 2, Character: 1},
				},
			},
			{
				URI: rootURI + "/cue.mod/usr/example.com/foo/x/y.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 0},
					End:   protocol.Position{Line: 2, Character: 1},
				},
			},
		}

		gotTo := env.Definition(from)
		qt.Assert(t, qt.ContentEquals(gotTo, wantTo), qt.Commentf("from: %#v", from))
	})
}
