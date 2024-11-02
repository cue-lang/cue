package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/hooks"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

func TestMain(m *testing.M) {
	Main(m, hooks.Options)
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
