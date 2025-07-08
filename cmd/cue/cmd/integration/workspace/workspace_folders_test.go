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

// TestWorkspaceFoldersRootURI tests that the server initialization
// works, or fails, as expected, due to various combinations of
// WorkspaceFolders and the RootURI being set or unset.
func TestWorkspaceFoldersRootURI(t *testing.T) {
	const filesOneModule = `
-- cue.mod/module.cue --
module: "mod.example/b"
language: version: "v0.11.0"

-- a/a.cue --
package a
-- b/b.cue --
package c
`

	const filesTwoModules = `
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

	type tc struct {
		name          string
		opts          []RunOption
		files         string
		expectSuccess bool
	}
	tests := []tc{
		{
			// With no workspace folders and no rooturi, the server will
			// return an error during initialization.
			name: "no workspace folders, no rooturi",
			opts: []RunOption{
				WorkspaceFolders(),
				InitializeError("initialize: no WorkspaceFolders"),
			},
			files:         filesOneModule,
			expectSuccess: false,
		},
		{
			// If no workspace folders are set, but a rooturi is set, the
			// server will treat the rooturi as if it is a workspace
			// folder.
			name: "no workspace folders, rooturi set",
			opts: []RunOption{
				WorkspaceFolders(),
				RootURIAsDefaultFolder(),
			},
			files:         filesOneModule,
			expectSuccess: true,
		},
		{
			// If both workspace folders and rooturi are provided, the
			// rooturi is ignored, and only workspace folders are used.
			name: "workspace folders, rooturi set",
			opts: []RunOption{
				WorkspaceFolders("a"),
				RootURIAsDefaultFolder(),
			},
			files:         filesOneModule,
			expectSuccess: true,
		},
		{
			// By default, the test framework will set one workspace
			// folder, and will not set the rooturi.
			name:          "default workspace folders, no rooturi",
			files:         filesOneModule,
			expectSuccess: true,
		},
		{
			// cue lsp supports multiple workspace folders.
			name: "multiple folders, one module",
			opts: []RunOption{
				WorkspaceFolders("a", "b"),
			},
			files:         filesOneModule,
			expectSuccess: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hadSuccess := false
			WithOptions(tc.opts...).Run(t, tc.files, func(t *testing.T, env *Env) {
				hadSuccess = true
				if tc.expectSuccess {
					// We do a trivial edit here, which must succeed, as a
					// means of verifying basic plumbing is working
					// correctly.
					env.OpenFile("a/a.cue")
					env.EditBuffer("a/a.cue", fake.NewEdit(1, 0, 1, 0, "\nx: 5\n"))
					got := env.BufferText("a/a.cue")
					want := "package a\n\nx: 5\n"
					qt.Assert(t, qt.Equals(got, want))
					env.Await(env.DoneWithChange())
				}
			})
			if tc.expectSuccess && !hadSuccess {
				t.Fatal("Initialisation should have succeeded, but it failed")
			} else if !tc.expectSuccess && hadSuccess {
				t.Fatal("Initialisation should have failed, but it succeeded")
			}
		})
	}
}

// TODO(myitcv): add a test that verifies we get an error in the case that a
// .cue file is opened "standalone", i.e. outside of the context of a workspace
// folder. This is possible in VSCode at least. We currently implement the
// error handling in vscode-cue in that instance but perhaps it should live in
// 'cue lsp'.
