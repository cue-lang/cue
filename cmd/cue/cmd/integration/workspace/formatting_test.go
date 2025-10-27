package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestFormatting(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- a/a.cue --
package a

  import "strings"

  v1: int

-- formatted/a/a.cue --
package a

import "strings"

v1: int
-- a/b.cue --
package a

  import "strings"

 complete
  and utter
   rubbish
-- a/c.cue --
package a

out: "A" | // first letter
	"B" | // second letter
	"C" | // third letter
										"D" // fourth letter
`

	archiveFiles := make(map[string]string)
	for _, file := range txtar.Parse([]byte(files)).Files {
		archiveFiles[file.Name] = string(file.Data)
	}

	t.Run("format syntactically valid", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/a.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=mod.example/x/a@v0 Reloaded", rootURI),
			)
			env.FormatBuffer("a/a.cue")
			content, open := env.Editor.BufferText("a/a.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["formatted/a/a.cue"]))
		})
	})

	t.Run("format syntactically invalid", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/b.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=mod.example/x/a@v0 Reloaded", rootURI),
			)
			env.FormatBuffer("a/b.cue")
			content, open := env.Editor.BufferText("a/b.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["a/b.cue"]))
		})
	})

	t.Run("format with broken formatter", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/c.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=mod.example/x/a@v0 Reloaded", rootURI),
			)
			env.FormatBuffer("a/c.cue")
			content, open := env.Editor.BufferText("a/c.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["a/c.cue"]))
		})
	})
}
