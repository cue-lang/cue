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

func TestSymlinks(t *testing.T) {
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
			NoLogExactf(protocol.Debug, "Module dir=%v module=mod.example/x@v0 For file %v/b.cue found [Package dirs=[%v] importPath=mod.example/x@v0:a]", rootURI, rootURI, rootURI),
		)

		// The order of these events is very important. We need to
		// create the symlink and notify the server whilst the
		// file-buffer is still open. That keeps the module open and the
		// symlinked file (b.cue) will be added to the package within
		// the module.

		err = os.Symlink(aCueFilePath, filepath.Join(rootFilePath, "b.cue"))
		qt.Assert(t, qt.IsNil(err))
		env.CheckForFileChanges()

		env.Await(
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/b.cue found [Package dirs=[%v] importPath=mod.example/x@v0:a]", rootURI, rootURI, rootURI),
		)

		// Closing a.cue will now cause a re-read of the file from disk
		// (as the overlay has gone), which exercises the code that
		// deals with two file URIs having the same underlying file on
		// disk.
		env.CloseBuffer("a.cue")
		env.Await(
			env.DoneWithClose(),
		)
	})
}
