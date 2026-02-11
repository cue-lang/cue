package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
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
		rootFilePath := rootURI.FilePath()

		// Need to wind the mtime of a.cue back into the past so that
		// fs_cache will cache the file.
		aCueFilePath := filepath.Join(rootFilePath, "a.cue")
		then := time.Now().Add(-10 * time.Second)
		err := os.Chtimes(aCueFilePath, then, then)
		qt.Assert(t, qt.IsNil(err))

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
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
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Closing a.cue will now cause a re-read of the file from disk
		// (as the overlay has gone), which exercises the code that
		// deals with two file URIs having the same underlying file on
		// disk.
		env.CloseBuffer("a.cue")
		env.Await(
			env.DoneWithClose(),
			LogExactf(protocol.Debug, 3, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)
	})
}

func TestSymlinksAncestor(t *testing.T) {
	const files = `
-- real/cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- real/a.cue --
package a

y: x

// docs for x
x: _
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		rootFilePath := rootURI.FilePath()

		err := os.Symlink(filepath.Join(rootFilePath, "real"), filepath.Join(rootFilePath, "sym link"))
		qt.Assert(t, qt.IsNil(err))

		env.OpenFile("sym link/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/sym%%20link] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		got, _ := env.Hover(protocol.Location{
			URI:   rootURI + "/sym%20link/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 3}},
		})
		qt.Assert(t, qt.IsTrue(strings.HasPrefix(got.Value, "docs for x")))

		// add a newline (and comment so that the hover coords exist), before line 2
		env.EditBuffer("sym link/a.cue", fake.NewEdit(2, 0, 2, 0, "// hi\n"))
		env.Await(env.DoneWithChange())

		got, _ = env.Hover(protocol.Location{
			URI:   rootURI + "/sym%20link/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 3}},
		})
		qt.Assert(t, qt.IsNil(got))
	})
}
