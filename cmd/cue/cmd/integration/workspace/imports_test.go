package workspace

import (
	"io/fs"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modregistrytest"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestImports(t *testing.T) {
	registryFS, err := txtar.FS(txtar.Parse([]byte(`
-- _registry/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_v0.0.1/x/y.cue --
package x
`)))

	qt.Assert(t, qt.IsNil(err))
	reg := newRegistry(t, registryFS)

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
`

	t.Run("open", func(t *testing.T) {
		WithOptions(
			RootURIAsDefaultFolder(), Registry(reg), Modes(DefaultModes()&^Forwarded),
		).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/a.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Reloaded", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 For file %v/a/a.cue found [Package dir=%v/a importPath=example.com/bar/a@v0]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loading packages [example.com/bar/a@v0]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dir=%v/a importPath=example.com/bar/a@v0", rootURI, rootURI),
			)
		})
	})
}

func newRegistry(t *testing.T, fsys fs.FS) cache.Registry {
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
	return reg
}
