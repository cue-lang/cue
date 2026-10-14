package workspace

import (
	"testing"

	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"github.com/go-quicktest/qt"
)

func TestMain(m *testing.M) {
	// The tests here start an LSP server, which can resolve modules via a
	// registry. They are meant to be hermetic; catch any that are not.
	cuetest.DenyRegistryAccess()
	I.Main(m)
}

// TestWorkspaceFoldersRootURI tests that the server initialization
// works, or fails, as expected, due to various combinations of
// WorkspaceFolders and the RootURI being set or unset.
func TestWorkspaceFoldersRootURI(t *testing.T) {
	t.Parallel()
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
		opts  []I.RunOption
		files string
		// skippedFolders is the number of virtual folders, with non-file
		// URIs, which the server is expected to skip.
		skippedFolders int
	}
	tests := []tc{
		{
			// With no workspace folders and no rooturi, the server will
			// return an error during initialization.
			name: "no workspace folders, no rooturi",
			opts: []I.RunOption{
				I.WorkspaceFolders(),
			},
			files: filesOneModule,
		},
		{
			// If no workspace folders are set, but a rooturi is set, the
			// server will treat the rooturi as if it is a workspace
			// folder.
			name: "no workspace folders, rooturi set",
			opts: []I.RunOption{
				I.WorkspaceFolders(),
				I.RootURIAsDefaultFolder(),
			},
			files: filesOneModule,
		},
		{
			// If both workspace folders and rooturi are provided, the
			// rooturi is ignored, and only workspace folders are used.
			name: "workspace folders, rooturi set",
			opts: []I.RunOption{
				I.WorkspaceFolders("a"),
				I.RootURIAsDefaultFolder(),
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
			opts: []I.RunOption{
				I.WorkspaceFolders("a", "b"),
			},
			files: filesOneModule,
		},
		{
			// Virtual folders, such as those registered by VS Code
			// extensions like GitLens, are skipped.
			name: "virtual folder",
			opts: []I.RunOption{
				I.WorkspaceFolders("virtual:///virtualpath"),
			},
			files:          filesOneModule,
			skippedFolders: 1,
		},
		{
			name: "real and virtual folders",
			opts: []I.RunOption{
				I.WorkspaceFolders("a", "decompileFs:///virtualpath", "b"),
			},
			files:          filesOneModule,
			skippedFolders: 1,
		},
		{
			// The rooturi is the first workspace folder in VS Code,
			// so it may be a virtual folder as well.
			name: "virtual first folder, rooturi set",
			opts: []I.RunOption{
				I.WorkspaceFolders("virtual:///virtualpath", "a"),
				I.RootURIAsDefaultFolder(),
			},
			files:          filesOneModule,
			skippedFolders: 1,
		},
		{
			// A malformed file URI is not a virtual folder, so it is an error.
			name: "malformed file folder",
			opts: []I.RunOption{
				I.WorkspaceFolders("file:///bad%zz"),
				I.InitializeError(`.*invalid WorkspaceFolder.URI.*`),
			},
			files: filesOneModule,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			I.WithOptions(tc.opts...).Run(t, tc.files, func(t *testing.T, env *I.Env) {
				// We do a trivial edit here, which must succeed, as a
				// means of verifying basic plumbing is working
				// correctly.
				env.OpenFile("a/a.cue")
				env.EditBuffer("a/a.cue", fake.NewEdit(1, 0, 1, 0, "\nx: 5\n"))
				got := env.BufferText("a/a.cue")
				want := "package a\n\nx: 5\n"
				qt.Assert(t, qt.Equals(got, want))
				env.Await(
					env.DoneWithChange(),
					I.LogExactf(protocol.Warning, tc.skippedFolders, false, "skip adding virtual folder"),
				)
			})
		})
	}
}

func TestWorkspaceFoldersReconfigure(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/b"
language: version: "v0.11.0"

-- a/a.cue --
package a
`
	I.WithOptions(I.WorkspaceFolders()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.Await(
			env.DoneDiagnosingChanges(),
			I.NoLogExactf(protocol.Debug, "Module dir=%v module=unknown Created", rootURI),
		)
		// The virtual folder is skipped, and the real one is added.
		env.ChangeWorkspaceFolders(rootURI.FilePath(), "virtual:///virtualpath")
		env.Await(
			env.DoneDiagnosingChanges(),
			I.LogExactf(protocol.Warning, 1, false, "skip adding virtual folder"),
			I.LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			I.NoLogExactf(protocol.Debug, "Module dir=%v module=unknown Created", rootURI),
		)
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
			I.LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/b@v0 Reloaded", rootURI),
		)
	})
}
