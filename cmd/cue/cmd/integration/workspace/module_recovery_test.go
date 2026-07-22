package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestModuleRecovery shows bad behaviour: when a broken
// cue.mod/module.cue file is fixed, the module is correctly
// recreated, but a bogus package is also created for the cue.mod
// directory itself, and loaded, only to fail and be deleted again -
// files under cue.mod can never belong to a package.
func TestModuleRecovery(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
this is not valid cue
-- a.cue --
package a

x: 5
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("cue.mod/module.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Deleted", rootURI),
		)

		// Now fix the module file.
		env.SetBufferContent("cue.mod/module.cue", `module: "mod.example/x"
language: version: "v0.16.0"
`)
		env.Await(
			env.DoneWithChange(),
			// NB "at least once": the bogus package load churns the
			// module, so it can be reloaded several times.
			LogExactf(protocol.Debug, 1, true, "Module dir=%v module=mod.example/x@v0 Reloaded", rootURI),
			// A bogus package is created for the cue.mod directory.
			LogMatching(protocol.Debug, 1, true, `Package dirs=\[%v/cue\.mod\] importPath=mod\.example/x/cue\.mod@v0:_.+ Created`, rootURI),
		)
	})
}
