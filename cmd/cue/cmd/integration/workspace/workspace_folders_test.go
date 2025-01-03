//go:build !windows

package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/hooks"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"github.com/go-quicktest/qt"
)

func TestMain(m *testing.M) {
	Main(m, hooks.Options)
}

// TestWorkingSimpleModule ensures that we have a successful package load for a
// simple module rooted in the workspace folder with a single CUE file at the
// root.
func TestWorkingSimpleModule(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example"
language: {
        version: "v0.11.0"
}
-- a.cue --
package a
-- b/b.cue --
package c
`
	WithOptions().Run(t, files, func(t *testing.T, env *Env) {
		// Simulate a change and ensure we get diagnostics back
		env.OpenFile("a.cue")
		env.EditBuffer("a.cue", fake.NewEdit(1, 0, 1, 0, "\nx: 5\n"))
		got := env.BufferText("a.cue")
		want := "package a\n\nx: 5\n"
		qt.Assert(t, qt.Equals(got, want))
		env.Await(env.DoneWithChange())
	})
}

// TestMultipleWorkspaceFolders verifies the behaviour of starting 'cue lsp'
// with multiple WorkspaceFolders. This is currently not supported, and hence
// the test is a negative test that asserts 'cue lsp' will fail (during the
// Initialize phase).
func TestMultipleWorkspaceFolders(t *testing.T) {
	const files = `

-- a/cue.mod/module.cue --
module: "mod.example/b"
language: version: "v0.11.0"

-- a/a.cue --
package a

-- b/cue.mod/module.cue --
module: "mod.example/b"
language: version: "v0.11.0"

-- b/b.cue --
package a

`
	WithOptions(
		WorkspaceFolders("a", "b"),
		InitializeError("initialize: got 2 WorkspaceFolders; expected 1"),
	).Run(t, files, nil)
}
