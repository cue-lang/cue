package workspace

import (
	"testing"

	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestDiagnosticsClearedAfterClose tests that diagnostics published
// for a file are cleared if the underlying problem is resolved after
// the file has been closed in the editor. The client retains
// published diagnostics until they are explicitly cleared, so the
// server must keep updating files whose diagnostics it has published,
// even once they are closed.
func TestDiagnosticsClearedAfterClose(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- b.cue --
package a

y: 6
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			// data1.json does not exist, so a.cue has a diagnostic.
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)

		env.CloseBuffer("a.cue")
		env.Await(env.DoneWithClose())

		// Resolve the problem on disk while the file is closed.
		env.WriteWorkspaceFile("data/data1.json", `{"field1": true}`)
		env.Await(
			env.DoneWithChangeWatchedFiles(),
			NoDiagnostics(ForFile("a.cue")),
		)
	})
}

// TestDiagnosticsClearedAfterDelete tests that diagnostics published
// for a file are cleared if the file is deleted from disk after
// being closed in the editor.
func TestDiagnosticsClearedAfterDelete(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- b.cue --
package a

y: 6
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)

		env.CloseBuffer("a.cue")
		env.Await(env.DoneWithClose())

		// Delete the offending file entirely: its diagnostics must
		// not outlive it.
		env.RemoveWorkspaceFile("a.cue")
		env.Await(
			env.DoneWithChangeWatchedFiles(),
			NoDiagnostics(ForFile("a.cue")),
		)
	})
}
