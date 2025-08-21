package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/hooks"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
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
		name  string
		opts  []RunOption
		files string
	}
	tests := []tc{
		{
			// With no workspace folders and no rooturi, the server will
			// return an error during initialization.
			name: "no workspace folders, no rooturi",
			opts: []RunOption{
				WorkspaceFolders(),
			},
			files: filesOneModule,
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
			files: filesOneModule,
		},
		{
			// If both workspace folders and rooturi are provided, the
			// rooturi is ignored, and only workspace folders are used.
			name: "workspace folders, rooturi set",
			opts: []RunOption{
				WorkspaceFolders("a"),
				RootURIAsDefaultFolder(),
			},
			files: filesOneModule,
		},
		{
			// By default, the test framework will set one workspace
			// folder, and will not set the rooturi.
			name:  "default workspace folders, no rooturi",
			files: filesOneModule,
		},
		{
			// cue lsp supports multiple workspace folders.
			name: "multiple folders, one module",
			opts: []RunOption{
				WorkspaceFolders("a", "b"),
			},
			files: filesOneModule,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			WithOptions(tc.opts...).Run(t, tc.files, func(t *testing.T, env *Env) {
				// We do a trivial edit here, which must succeed, as a
				// means of verifying basic plumbing is working
				// correctly.
				env.OpenFile("a/a.cue")
				env.EditBuffer("a/a.cue", fake.NewEdit(1, 0, 1, 0, "\nx: 5\n"))
				got := env.BufferText("a/a.cue")
				want := "package a\n\nx: 5\n"
				qt.Assert(t, qt.Equals(got, want))
				env.Await(env.DoneWithChange())
			})
		})
	}
}

func TestWorkspaceFoldersReconfigure(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/b"
language: version: "v0.11.0"

-- a/a.cue --
package a
`
	WithOptions(WorkspaceFolders()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.Await(
			env.DoneDiagnosingChanges(),
			NoLogExactf(protocol.Debug, "Module dir=%v module=unknown Created", rootURI),
		)
		env.ChangeWorkspaceFolders(rootURI.Path())
		env.Await(
			env.DoneDiagnosingChanges(),
			NoLogExactf(protocol.Debug, "Module dir=%v module=unknown Created", rootURI),
		)
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/b@v0 Reloaded", rootURI),
		)
	})
}
