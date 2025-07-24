package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"github.com/go-quicktest/qt"
)

func TestEditing(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- a/a.cue --
package a

import "strings"

v1: int

-- a/d/d.cue --
package e

v1: 5

-- b/b.cue --
package b

v2: string

-- b/c/c.cue --
package b

import "mod.example/x/a"

v2: "hi"
v3: a.v1
`

	t.Run("open - one package only", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("a/a.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Reloaded", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/a/a.cue found [Package dir=%v/a importPath=mod.example/x/a@v0]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/a@v0]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a importPath=mod.example/x/a@v0", rootURI, rootURI),
				// We do not load stdlib packages
				NoLogExactf(protocol.Debug, "module=mod.example/x@v0 Loaded Package dir= importPath=strings"),
			)
		})
	})

	t.Run("open - one package only - ancestor", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("b/c/c.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Reloaded", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/b/c/c.cue found [Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b", rootURI, rootURI),
				// We do not load the parent/same package
				NoLogExactf(protocol.Debug, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b@v0]", rootURI),
			)
			// Although we have not loaded the mod.example/x/b@v0
			// package, the b/b.cue file will have been loaded and will
			// be being modelled. We can prove this by changing this file
			// (without opening it) and observing the x/b/c:b package
			// gets reloaded.
			//
			// WriteWorkspaceFile issues a file-watch notification to the
			// server. This tells the server the file has changed on
			// disk, but has not been opened in the editor.
			env.WriteWorkspaceFile("b/b.cue", "package b\n\nv2: int\n")
			env.Await(
				env.DoneWithChangeWatchedFiles(),
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b]", rootURI),
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b", rootURI, rootURI),
				// We still do not load the parent/same package
				NoLogExactf(protocol.Debug, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b@v0]", rootURI),
			)
		})
	})

	t.Run("open - import chain", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("b/c/c.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=unknown Created", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Reloaded", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/b/c/c.cue found [Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b", rootURI, rootURI),
				// b/c/c.cue imports mod.example/x/a. So we should see a
				// load for x/a as a side-effect of loading pkg x/b/c:b
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a importPath=mod.example/x/a@v0", rootURI, rootURI),
			)
			// Even with a.cue not open in the editor, if we rewrite
			// a.cue, we should see a reload of x/a and x/b/c:b
			env.WriteWorkspaceFile("a/a.cue", "package a\n\nv1: string\n")
			env.Await(
				env.DoneWithChangeWatchedFiles(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/a@v0 mod.example/x/b/c@v0:b]", rootURI),
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a importPath=mod.example/x/a@v0", rootURI, rootURI),
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b", rootURI, rootURI),
			)
		})
	})

	t.Run("edit - change package", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.OpenFile("b/b.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/b/b.cue found [Package dir=%v/b importPath=mod.example/x/b@v0]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b@v0]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b importPath=mod.example/x/b@v0", rootURI, rootURI),
				// We do not load the child/same package
				NoLogExactf(protocol.Debug, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b]", rootURI),
			)
			// Now open the child/same package
			env.OpenFile("b/c/c.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/b/c/c.cue found [Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b/c importPath=mod.example/x/b/c@v0:b", rootURI, rootURI),
			)
			// Change b/b.cue (which is an ancestor import of b/c/c.cue)
			// package from "b" to "bz"
			env.EditBuffer("b/b.cue", fake.NewEdit(0, 9, 0, 9, "z"))
			env.Await(
				env.DoneWithChange(),
				// We should see a single reload of both existing packages:
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b mod.example/x/b@v0]", rootURI),
				// The load of mod.example/x/b@v0 will have failed, so we should see a new pkg search for b.cue:
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/b/b.cue found [Package dir=%v/b importPath=mod.example/x/b@v0:bz]", rootURI, rootURI, rootURI),
				// And we should now see that the mod.example/x/b@v0:bz package gets loaded successfully
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b@v0:bz]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b importPath=mod.example/x/b@v0:bz", rootURI, rootURI),
			)
			// A further edit of b/b.cue should now cause package x/b:bz to be reloaded, but x/b/c:b does not get reloaded:
			env.EditBuffer("b/b.cue", fake.NewEdit(2, 0, 2, 0, "w"))
			env.Await(
				env.DoneWithChange(),
				// Now 2 loads of x/b:bz
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b@v0:bz]", rootURI),
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/b importPath=mod.example/x/b@v0:bz", rootURI, rootURI),
				// Still exactly 1 explicit load of x/b/c:b
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/b/c@v0:b]", rootURI),
			)
		})
	})

	t.Run("edit - create ancestor imports", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.OpenFile("a/a.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/a/a.cue found [Package dir=%v/a importPath=mod.example/x/a@v0]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/a@v0]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a importPath=mod.example/x/a@v0", rootURI, rootURI),
			)
			env.OpenFile("a/d/d.cue")
			env.Await(
				env.DoneWithOpen(),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/a/d/d.cue found [Package dir=%v/a/d importPath=mod.example/x/a/d@v0:e]", rootURI, rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/a/d@v0:e]", rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a/d importPath=mod.example/x/a/d@v0:e", rootURI, rootURI),
			)
			// change a/a.cue package from "a" to "e". This makes it
			// become the same package as a/d/d.cue
			env.EditBuffer("a/a.cue", fake.NewEdit(0, 8, 0, 9, "e"))
			env.Await(
				env.DoneWithChange(),
				// We should first see a reload of x/a, which will fail.
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/a@v0]", rootURI),
				// There'll then be a new search and it should find both packages.
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 For file %v/a/a.cue found ["+
					"Package dir=%v/a importPath=mod.example/x/a@v0:e "+
					"Package dir=%v/a/d importPath=mod.example/x/a/d@v0:e]",
					rootURI, rootURI, rootURI, rootURI),
				// And both packages should get reloaded in one go:
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x/a/d@v0:e mod.example/x/a@v0:e]", rootURI),
				LogExactf(protocol.Debug, 2, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a/d importPath=mod.example/x/a/d@v0:e", rootURI, rootURI),
				LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loaded Package dir=%v/a importPath=mod.example/x/a@v0:e", rootURI, rootURI),
			)
		})
	})

	t.Run("jump to definition", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			rootURI := env.Sandbox.Workdir.RootURI()
			env.Await(
				LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
			)
			env.OpenFile("b/c/c.cue")
			env.Await(
				env.DoneWithOpen(),
			)
			locs := env.Definition(protocol.Location{
				URI: rootURI + "/b/c/c.cue",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Character: 6},
				},
			})
			qt.Assert(t, qt.DeepEquals(locs, []protocol.Location{
				{
					URI: rootURI + "/a/a.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 4, Character: 0},
						End:   protocol.Position{Line: 4, Character: 2},
					},
				},
			}))
		})
	})
}
