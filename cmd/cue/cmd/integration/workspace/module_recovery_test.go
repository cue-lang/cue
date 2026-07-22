package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestModuleRecovery tests the workspace's behavior when a broken
// cue.mod/module.cue file is fixed: the module must be recreated,
// and no package may be created for the cue.mod directory itself
// (files under cue.mod can never belong to a package).
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
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Reloaded", rootURI),
			// No package may be created for the cue.mod directory.
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/cue\.mod\]`, rootURI),
			NoLogMatching(protocol.Debug, `produced no result`),
		)
	})
}
