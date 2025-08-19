package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

// TestImportsAmbiguous demonstrates that handling distinct packages
// with the same import path is safe, and similarly, multiple copies
// of the same module are fine.
func TestImportsAmbiguous(t *testing.T) {
	registryFS, err := txtar.FS(txtar.Parse([]byte(`
-- _registry/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_v0.0.1/x/y.cue --
package x

y: true
-- _registry/example.com_foo_x_v0.0.1/cue.mod/module.cue --
module: "example.com/foo/x@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_x_v0.0.1/y.cue --
package x

y: false
`)))

	qt.Assert(t, qt.IsNil(err))
	reg, cacheDir := newRegistry(t, registryFS)
	t.Log(cacheDir)

	const files = `
-- r1/cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
deps: {
	"example.com/foo@v0": {
		v: "v0.0.1"
	}
}
-- r1/a/a.cue --
package a

import "example.com/foo/x"

out: x.y
-- r2/cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
deps: {
	"example.com/foo/x@v0": {
		v: "v0.0.1"
	}
}
-- r2/a/a.cue --
package a

import "example.com/foo/x"

out: x.y
`

	WithOptions(
		WorkspaceFolders("r1", "r2"), Registry(reg), Modes(DefaultModes()&^Forwarded),
	).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		r1RootURI := rootURI + "/r1"
		r2RootURI := rootURI + "/r2"
		cacheURI := protocol.URIFromPath(cacheDir) + "/mod/extract"
		env.Await(
			LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", r1RootURI),
			LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", r2RootURI),
		)
		env.OpenFile("r1/a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Reloaded", r1RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 For file %v/a/a.cue found [Package dir=%v/a importPath=example.com/bar/a@v0]", r1RootURI, r1RootURI, r1RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loading packages [example.com/bar/a@v0]", r1RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dir=%v/a importPath=example.com/bar/a@v0", r1RootURI, r1RootURI),
			// A module is created for the imported module.
			LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo@v0.0.1 module=example.com/foo@v0 Reloaded", cacheURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo@v0.0.1 module=example.com/foo@v0 Loaded Package dir=%v/example.com/foo@v0.0.1/x importPath=example.com/foo/x@v0", cacheURI, cacheURI),
		)
		// Now open the other a.cue which is in a module of the same
		// name, and imports a package with the same import path, but
		// which resolves to a different module:
		env.OpenFile("r2/a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Reloaded", r2RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 For file %v/a/a.cue found [Package dir=%v/a importPath=example.com/bar/a@v0]", r2RootURI, r2RootURI, r2RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loading packages [example.com/bar/a@v0]", r2RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dir=%v/a importPath=example.com/bar/a@v0", r2RootURI, r2RootURI),
			// A module is created for the imported module.
			LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo/x@v0.0.1 module=example.com/foo/x@v0 Reloaded", cacheURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo/x@v0.0.1 module=example.com/foo/x@v0 Loaded Package dir=%v/example.com/foo/x@v0.0.1/. importPath=example.com/foo/x@v0", cacheURI, cacheURI),
			// Repeat key assertions from the first OpenFile call to
			// prove that the r1 package (and imports) has not been
			// reloaded:
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dir=%v/a importPath=example.com/bar/a@v0", r1RootURI, r1RootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo@v0.0.1 module=example.com/foo@v0 Loaded Package dir=%v/example.com/foo@v0.0.1/x importPath=example.com/foo/x@v0", cacheURI, cacheURI))
		// Now perform the same jump-to-dfn from each of the open files:
		// from the "y" in "out: x.y", which should take us to different
		// files in the different (yet identically named) imported
		// packages:
		fromTo := map[protocol.Location][]protocol.Location{
			{
				URI:   r1RootURI + "/a/a.cue",
				Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 7}},
			}: {
				{
					URI: cacheURI + "/example.com/foo@v0.0.1/x/y.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 2, Character: 0},
						End:   protocol.Position{Line: 2, Character: 1},
					},
				},
			},

			{
				URI:   r2RootURI + "/a/a.cue",
				Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 7}},
			}: {
				{
					URI: cacheURI + "/example.com/foo/x@v0.0.1/y.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 2, Character: 0},
						End:   protocol.Position{Line: 2, Character: 1},
					},
				},
			},
		}
		for from, wantTo := range fromTo {
			gotTo := env.Definition(from)
			qt.Assert(t, qt.ContentEquals(gotTo, wantTo), qt.Commentf("from: %#v", from))
		}
	})
}
