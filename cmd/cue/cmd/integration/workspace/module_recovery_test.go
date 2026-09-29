package workspace

import (
	"path/filepath"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

// TestModuleRecovery tests the workspace's behavior when a broken
// cue.mod/module.cue file is fixed: the module must be recreated,
// and no package may be created for the cue.mod directory itself
// (files under cue.mod can never belong to a package).
func TestModuleRecovery(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
this is not valid cue
-- a.cue --
package a

x: 5
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("cue.mod/module.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Deleted", rootURI),
		)

		// Now fix the module file.
		env.SetBufferContent("cue.mod/module.cue", `module: "mod.example/x"
language: version: "v0.16.0"
`)
		env.Await(
			env.DoneWithChange(),
			I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Reloaded", rootURI),
			// No package may be created for the cue.mod directory.
			I.NoLogMatching(protocol.Debug, `Package dirs=\[%v/cue\.mod\]`, rootURI),
			I.NoLogMatching(protocol.Debug, `produced no result`),
		)
	})
}

// TestImportedModuleRecovery tests the workspace's behavior when a
// module, whose package is imported by a package in another module,
// is deleted because its cue.mod/module.cue file becomes invalid, and
// is later recreated because the file is fixed. The importing package
// must be reloaded at both points: first so that its import no longer
// resolves to the deleted package, then so that its import resolves
// to the recreated package.
func TestImportedModuleRecovery(t *testing.T) {
	t.Parallel()
	registryFS, err := txtar.FS(txtar.Parse([]byte(`
-- _registry/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_v0.0.1/x/y.cue --
package x

y: a.b
a: b: z: 3
`)))
	qt.Assert(t, qt.IsNil(err))
	reg, cacheDir := newRegistry(t, registryFS)

	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
deps: {
	"example.com/foo@v0": {
		v: "v0.0.1"
	}
}
-- a/a.cue --
package a

import "example.com/foo/x"

v: x
w: v.y.z
`
	I.WithOptions(
		I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
	).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		fooModDir := protocol.URIFromPath(cacheDir) + "/mod/extract/example.com/foo@v0.0.1"
		fooModFile := filepath.Join(cacheDir, "mod", "extract", "example.com", "foo@v0.0.1", "cue.mod", "module.cue")

		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
			I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/foo@v0 Reloaded", fooModDir),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/x] importPath=example.com/foo/x@v0 Reloaded", fooModDir),
		)

		// Definitions on "y" within "w: v.y.z" arrive at x/y.cue in
		// the imported module.
		defLoc := protocol.Location{
			URI:   rootURI + "/a/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 5, Character: 7}},
		}
		wantDefs := []protocol.Location{{
			URI: fooModDir + "/x/y.cue",
			Range: protocol.Range{
				Start: protocol.Position{Line: 3, Character: 6},
				End:   protocol.Position{Line: 3, Character: 7},
			},
		}}
		qt.Assert(t, qt.DeepEquals(env.Definition(defLoc), wantDefs))

		// Open the imported module's module file in the editor. Its
		// content is unchanged, so the module and its package are
		// simply reloaded.
		env.OpenFile(fooModFile)
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 2, false, "Module dir=%v module=example.com/foo@v0 Reloaded", fooModDir),
			I.LogExactf(protocol.Debug, 2, false, "Package dirs=[%v/x] importPath=example.com/foo/x@v0 Reloaded", fooModDir),
		)

		// Now break the module file. The module and its package are
		// deleted, so the importing package must be reloaded: its
		// import no longer resolves.
		env.SetBufferContent(fooModFile, "this is not valid cue\n")
		env.Await(
			env.DoneWithChange(),
			I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/foo@v0 Deleted", fooModDir),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/x] importPath=example.com/foo/x@v0 Deleted", fooModDir),
			I.LogExactf(protocol.Debug, 2, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
		)
		qt.Assert(t, qt.HasLen(env.Definition(defLoc), 0))

		// Fix the module file. The module and its package are
		// recreated, so the importing package must be reloaded once
		// more: its import resolves again.
		env.SetBufferContent(fooModFile, `module: "example.com/foo@v0"
language: version: "v0.11.0"
`)
		env.Await(
			env.DoneWithChange(),
			I.LogExactf(protocol.Debug, 3, false, "Module dir=%v module=example.com/foo@v0 Reloaded", fooModDir),
			I.LogExactf(protocol.Debug, 3, false, "Package dirs=[%v/x] importPath=example.com/foo/x@v0 Reloaded", fooModDir),
			I.LogExactf(protocol.Debug, 3, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
		)
		qt.Assert(t, qt.DeepEquals(env.Definition(defLoc), wantDefs))
	})
}
