package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestImports(t *testing.T) {
	registryFS, err := txtar.FS(txtar.Parse([]byte(`
-- _registry/example.com_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_v0.0.1/x/y.cue --
package x
`)))

	qt.Assert(t, qt.IsNil(err))
	reg := testRegistry{fs: registryFS}

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
		WithOptions(RootURIAsDefaultFolder(), Registry(reg), Modes(DefaultModes()&^Forwarded)).Run(t, files, func(t *testing.T, env *Env) {
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

type testRegistry struct {
	fs fs.FS
}

func (r testRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	mpath := r.modpath(m)
	info, err := fs.Stat(r.fs, mpath)
	if err != nil || !info.IsDir() {
		return module.SourceLoc{}, fmt.Errorf("module %v not found at %v", m, mpath)
	}
	return module.SourceLoc{
		FS:  r.fs,
		Dir: mpath,
	}, nil
}

func (r testRegistry) Requirements(ctx context.Context, m module.Version) ([]module.Version, error) {
	mpath := path.Join(r.modpath(m), "cue.mod/module.cue")
	data, err := fs.ReadFile(r.fs, mpath)
	if err != nil {
		return nil, err
	}
	mf, err := modfile.Parse(data, mpath)
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", m, err)
	}
	return mf.DepVersions(), nil
}

func (r testRegistry) modpath(m module.Version) string {
	mpath, _, _ := ast.SplitPackageVersion(m.Path())
	return path.Join("_registry", strings.ReplaceAll(mpath, "/", "_")+"_"+m.Version())
}
