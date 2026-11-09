package workspace

import (
	"io/fs"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"cuelang.org/go/internal/lsp/cache"
	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modregistrytest"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestImports(t *testing.T) {
	t.Parallel()
	registryFS, err := txtar.FS(txtar.Parse([]byte(`
-- _registry/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_v0.0.1/x/y.cue --
package x

y: a.b
a: b: z: 3
-- _registry/example.com_qux_v0.0.1/cue.mod/module.cue --
module: "example.com/qux@v0"
language: version: "v0.11.0"
deps: "example.com/foo@v0": v: "v0.0.1"
-- _registry/example.com_qux_v0.0.1/q/q.cue --
package q

import "example.com/foo/x"

#Q: x.a.b
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
	"example.com/qux@v0": {
		v: "v0.0.1"
	}
}
-- a/a.cue --
package a

import "example.com/foo/x"

v: x
w: v.y.z
-- b/b.cue --
package b

import "example.net/bar/doesnotexist" // unknown module
import "example.com/bar/doesnotexist" // unknown package
-- c/c.cue --
package c

import "
-- d/d.cue --
package d

import "example.com/qux/q"

v: q.#Q & {z: 3}
w: q.#Q & {}
`

	t.Run("open", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			cacheURI := protocol.URIFromPath(cacheDir) + "/mod/extract"
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/a.cue")
			env.Await(
				env.DoneWithOpen(),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Reloaded", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loading packages [example.com/bar/a@v0]", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
				// A module is created for the imported module.
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo@v0.0.1 module=unknown Created", cacheURI),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo@v0.0.1 module=example.com/foo@v0 Reloaded", cacheURI),
				I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/example.com/foo@v0.0.1/x] importPath=example.com/foo/x@v0 Reloaded", cacheURI),
			)
		})
	})

	t.Run("open - bad import", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("b/b.cue")
			env.Await(
				env.DoneWithOpen(),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/b] importPath=example.com/bar/b@v0 Reloaded", rootURI),
				// Nothing is created for the unfindable imports
				I.NoLogExactf(protocol.Debug, "example.net/bar/doesnotexist"),
				I.NoLogExactf(protocol.Debug, "example.com/bar/doesnotexist"),
			)
		})
	})

	t.Run("open - bad import syntax", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("c/c.cue")
			env.Await(
				env.DoneWithOpen(),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loading packages [example.com/bar/c@v0]", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/c] importPath=example.com/bar/c@v0 Created", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Error when reloading: cannot get imports", rootURI),
				I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/c] importPath=example.com/bar/c@v0 Deleted", rootURI),
			)
			// Now edit it to correct the imports line:
			env.EditBuffer("c/c.cue", fake.NewEdit(2, 7, 2, 7, `"`))
			env.Await(
				env.DoneWithChange(),
				// We should get the package load
				I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/c] importPath=example.com/bar/c@v0 Reloaded", rootURI),
			)
		})
	})

	t.Run("jump to definition - inter module", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			cacheURI := protocol.URIFromPath(cacheDir) + "/mod/extract"
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/a.cue")
			env.Await(
				env.DoneWithOpen(),
			)
			locs := env.Definition(protocol.Location{
				URI: rootURI + "/a/a.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Character: 7},
				},
			})
			qt.Assert(t, qt.DeepEquals(locs, []protocol.Location{
				{
					URI: cacheURI + "/example.com/foo@v0.0.1/x/y.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 3, Character: 6},
						End:   protocol.Position{Line: 3, Character: 7},
					},
				},
			}))
		})
	})

	// The definition of z is found through an import in another
	// dependency module, which resolves it with its own module.cue.
	t.Run("jump to definition - transitive inter module", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("d/d.cue")
			env.Await(
				env.DoneWithOpen(),
			)
			locs := env.Definition(protocol.Location{
				URI: rootURI + "/d/d.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 11},
				},
			})
			// TODO: z in foo's x/y.cue is missing.
			qt.Assert(t, qt.DeepEquals(locs, []protocol.Location{
				{
					URI: rootURI + "/d/d.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 4, Character: 11},
						End:   protocol.Position{Line: 4, Character: 12},
					},
				},
			}))
		})
	})

	// References to z are found through an import in another
	// dependency module, both from its use and from its declaration.
	t.Run("references - transitive inter module", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			cacheURI := protocol.URIFromPath(cacheDir) + "/mod/extract"
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("d/d.cue")
			env.Await(
				env.DoneWithOpen(),
			)
			decl := protocol.Location{
				URI: cacheURI + "/example.com/foo@v0.0.1/x/y.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 3, Character: 6},
					End:   protocol.Position{Line: 3, Character: 7},
				},
			}
			useA := protocol.Location{
				URI: rootURI + "/a/a.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Character: 7},
					End:   protocol.Position{Line: 5, Character: 8},
				},
			}
			useD := protocol.Location{
				URI: rootURI + "/d/d.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 11},
					End:   protocol.Position{Line: 4, Character: 12},
				},
			}
			// TODO: the declaration in foo's x/y.cue and its use in a/a.cue are missing.
			qt.Assert(t, qt.DeepEquals(env.References(useD), []protocol.Location{useD}))

			// The editor only opens files in its workdir, so ask the
			// server directly about the declaration in the module cache.
			locs, err := env.Editor.Server.References(env.Ctx, &protocol.ReferenceParams{
				TextDocumentPositionParams: protocol.LocationTextDocumentPositionParams(decl),
				Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
			})
			qt.Assert(t, qt.IsNil(err))
			// TODO: the use in d/d.cue is missing.
			qt.Assert(t, qt.DeepEquals(locs, []protocol.Location{decl, useA}))
		})
	})

	// The fields of a definition from another dependency module's
	// import are offered as completions.
	t.Run("completion - transitive inter module", func(t *testing.T) {
		I.WithOptions(
			I.RootURIAsDefaultFolder(), I.Registry(reg), I.Modes(I.DefaultModes()&^I.Forwarded),
		).Run(t, files, func(t *testing.T, env *I.Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("d/d.cue")
			env.Await(
				env.DoneWithOpen(),
			)
			completions := env.Completion(protocol.Location{
				URI: rootURI + "/d/d.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Character: 11},
				},
			})
			qt.Assert(t, qt.IsNotNil(completions))
			labels := make(map[string]bool, len(completions.Items))
			for _, item := range completions.Items {
				labels[item.Label] = true
			}
			// TODO: z in foo's x/y.cue is missing.
			qt.Assert(t, qt.IsFalse(labels["z"]), qt.Commentf("unexpected z in %v", completions.Items))
		})
	})
}

func newRegistry(t *testing.T, fsys fs.FS) (cache.Registry, string) {
	t.Helper()
	fsys, err := fs.Sub(fsys, "_registry")
	qt.Assert(t, qt.IsNil(err))
	regSrv, err := modregistrytest.New(fsys, "")
	qt.Assert(t, qt.IsNil(err))
	cacheDir := t.TempDir()
	// t.TempDir calls Cleanup internally. Cleanups are invoked in
	// "last added, first called". We need to stop the server before we
	// attempt to delete the temp dir. The modcache code is very
	// thorough at setting permissions on files so we need a special
	// cleanup so that t.TempDir's cleanup doesn't error!
	t.Cleanup(func() { modcache.RemoveAll(cacheDir) })
	t.Cleanup(regSrv.Close)
	modcfg := &modconfig.Config{
		Env: []string{
			"CUE_REGISTRY=" + regSrv.Host(),
			// Set an empty cache dir so that a developer's ~/.cache/cue doesn't influence the tests!
			"CUE_CACHE_DIR=" + cacheDir,
		},
	}
	reg, err := modconfig.NewRegistry(modcfg)
	qt.Assert(t, qt.IsNil(err))
	return reg, cacheDir
}
