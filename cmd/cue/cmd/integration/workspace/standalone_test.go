package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

func TestStandalone(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		// no package decl, no module
		WithOptions(RootURIAsDefaultFolder()).Run(t, "", func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.CreateBuffer("a/a.cue", `
x: 4
y: x
`[1:])
			env.Await(
				env.DoneWithOpen(),
				//				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
				//				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Reloaded", rootURI),
			)
			t.FailNow()

			// Check we can do jump to definition
			locs := env.Definition(protocol.Location{
				URI: rootURI + "/a/a.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 1, Character: 3}, // the use of x on the 2nd line
				},
			})
			qt.Assert(t, qt.ContentEquals(locs, []protocol.Location{
				{
					URI: rootURI + "/a/a.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0}, // the dfn of x on the 1st line
						End:   protocol.Position{Line: 0, Character: 1},
					},
				},
			}))
			env.CloseBuffer("a/a.cue")
			env.Await(
				env.DoneWithClose(),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
				// Once the buffer is closed, there's an attempt to read
				// it from disk, which will error:
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Error when reloading: ", rootURI),
				// And given it doesn't exist on disk, it'll be deleted
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Deleted", rootURI),
			)
		})
	})

	t.Run("open with package", func(t *testing.T) {
		// package decl, but no module
		WithOptions(RootURIAsDefaultFolder()).Run(t, "", func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.CreateBuffer("a/a.cue", `
package wibble

x: 4
y: x
`[1:])
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Reloaded", rootURI),
			)
		})
	})

	t.Run("open with module", func(t *testing.T) {
		// module, but no package decl
		WithOptions(RootURIAsDefaultFolder()).Run(t, "", func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.CreateBuffer("cue.mod/module.cue", `
module: "cue.example.net"
language: version: "v0.13.0"
`[1:])
			env.Await(env.DoneWithOpen())
			env.CreateBuffer("a/a.cue", `
x: 4
y: x
`[1:])
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Reloaded", rootURI),
			)
		})
	})

	t.Run("transition to module", func(t *testing.T) {
		// starts with a package and without a module, then we add the module
		WithOptions(RootURIAsDefaultFolder()).Run(t, "", func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.CreateBuffer("a/a.cue", `
package wibble

x: 4
y: x
`[1:])
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Reloaded", rootURI),
				NoLogExactf(protocol.Debug, "Package dirs=[%v/a] importPath=cue.example.net/a@v0:wibble Reloaded", rootURI),
			)
			env.CreateBuffer("cue.mod/module.cue", `
module: "cue.example.net"
language: version: "v0.13.0"
`[1:])
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Deleted", rootURI),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=cue.example.net/a@v0:wibble Reloaded", rootURI),
			)
		})
	})

	t.Run("transition to standalone", func(t *testing.T) {
		// starts with package and module, but then we delete the module
		WithOptions(RootURIAsDefaultFolder()).Run(t, "", func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.CreateBuffer("cue.mod/module.cue", `
module: "cue.example.net"
language: version: "v0.13.0"
`[1:])
			env.Await(env.DoneWithOpen())
			env.CreateBuffer("a/a.cue", `
package wibble

x: 4
y: x
`[1:])
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=cue.example.net/a@v0:wibble Reloaded", rootURI),
				NoLogExactf(protocol.Debug, "StandaloneFile %v/a/a.cue Created", rootURI),
			)
			env.CloseBuffer("cue.mod/module.cue")
			env.Await(
				env.DoneWithClose(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=cue.example.net/a@v0:wibble Deleted", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=cue.example.net@v0 Deleted", rootURI),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Reloaded", rootURI),
			)
		})
	})
}
