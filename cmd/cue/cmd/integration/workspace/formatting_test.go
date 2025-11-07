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
-- m/cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- m/a/a.cue --
package a

  import "strings"

  v1: int

-- formatted/m/a/a.cue --
package a

import "strings"

v1: int
-- m/a/b.cue --
package a

  import "strings"

 complete
  and utter
   rubbish
-- m/a/c.cue --
package a

out: "A" | // first letter
	"B" | // second letter
	"C" | // third letter
										"D" // fourth letter
-- m/a/nopkg.cue --
x: 3
    y: x
-- formatted/m/a/nopkg.cue --
x: 3
y: x
-- standalone/a.cue --
 y: x
   x: 4
-- formatted/standalone/a.cue --
y: x
x: 4
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
			env.OpenFile("m/a/a.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/m/a] importPath=mod.example/x/a@v0 Reloaded", rootURI),
			)
			env.FormatBuffer("m/a/a.cue")
			content, open := env.Editor.BufferText("m/a/a.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["formatted/m/a/a.cue"]))
		})
	})

	t.Run("format syntactically invalid", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("m/a/b.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/m/a] importPath=mod.example/x/a@v0 Reloaded", rootURI),
			)
			env.FormatBuffer("m/a/b.cue")
			content, open := env.Editor.BufferText("m/a/b.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["m/a/b.cue"]))
		})
	})

	t.Run("format with broken formatter", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("m/a/c.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/m/a] importPath=mod.example/x/a@v0 Reloaded", rootURI),
			)
			env.FormatBuffer("m/a/c.cue")
			content, open := env.Editor.BufferText("m/a/c.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["m/a/c.cue"]))
		})
	})

	t.Run("format no-package file", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			env.OpenFile("m/a/nopkg.cue")
			env.Await(env.DoneWithOpen())
			env.FormatBuffer("m/a/nopkg.cue")
			content, open := env.Editor.BufferText("m/a/nopkg.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["formatted/m/a/nopkg.cue"]))
		})
	})

	t.Run("format standalone file", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			env.OpenFile("standalone/a.cue")
			env.Await(env.DoneWithOpen())
			env.FormatBuffer("standalone/a.cue")
			content, open := env.Editor.BufferText("standalone/a.cue")
			qt.Assert(t, qt.Equals(open, true))
			qt.Assert(t, qt.Equals(content, archiveFiles["formatted/standalone/a.cue"]))
		})
	})
}
