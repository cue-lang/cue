package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
		rootFilePath := rootURI.Path()

		// Need to wind the mtime of a.cue back into the past so that
		// fs_cache will cache the file.
		aCueFilePath := filepath.Join(rootFilePath, "a.cue")
		then := time.Now().Add(-10 * time.Second)
		err := os.Chtimes(aCueFilePath, then, then)
		qt.Assert(t, qt.IsNil(err))

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/a.cue found [Package dirs=[%v] importPath=mod.example/x@v0:a]", rootURI, rootURI, rootURI),
		)

		err = env.Sandbox.Workdir.RemoveFile(env.Ctx, "a.cue")
		qt.Assert(t, qt.IsNil(err))

		// Closing a.cue will now cause a re-read of the file from disk
		// (as the overlay has gone), which tests that the LSP can cope
		// with the re-read finding the file has really been deleted.
		env.CloseBuffer("a.cue")
		env.Await(
			env.DoneWithClose(),
		)
	})
}
