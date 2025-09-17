package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

func TestDelete(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- a.cue --
package a
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		err := env.Sandbox.Workdir.RemoveFile(env.Ctx, "a.cue")
		qt.Assert(t, qt.IsNil(err))

		// Closing a.cue will now cause a re-read of the file from disk
		// (as the overlay has gone), which tests that the LSP can cope
		// with the re-read finding the file has really been deleted.
		env.CloseBuffer("a.cue")
		env.Await(
			env.DoneWithClose(),
			// There is a race between the close message getting to the
			// server, and a watched-file-changed notification getting to
			// the server. There's a possibility that each message causes
			// the server to attempt to reload the a.cue file, thus we
			// could have 1 or 2 of the following message in the logs,
			// hence the atLeast=true parameter.
			LogExactf(protocol.Debug, 1, true, "Module dir=%v module=mod.example/x@v0 Cannot read file %v/a.cue: ", rootURI, rootURI),
		)
	})
}
