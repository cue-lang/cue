//go:build !windows

package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
)

func TestEditing(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- a/a.cue --
package a

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

	t.Run("open", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			env.OpenFile("a/a.cue")
			env.Await(
				// There's a snapshot for a/a.cue (because it was opened):
				LogMatching(protocol.Debug, `/a/a\.cue":{"file://`, 1, false),
				// a/a.cue belongs to two packages:
				LogMatching(protocol.Debug, `/a/a\.cue":\["mod\.example/x/a","mod\.example/x/a@v0:a"\]`, 1, false),
				// a/d/d.cue belongs to one package:
				LogMatching(protocol.Debug, `/a/d/d\.cue":\["mod\.example/x/a/d@v0:e"\]`, 1, false),
				// b/b.cue is used in two packages because of ancestor imports:
				LogMatching(protocol.Debug, `/b/b\.cue":\["mod\.example/x/b/c@v0:b","mod\.example/x/b@v0:b"\]`, 1, false),
				// b/c/c.cue only exists in one package:
				LogMatching(protocol.Debug, `/b/c/c.cue":\["mod\.example/x/b/c@v0:b"\]`, 1, false),
			)
		})
	})

	t.Run("edit - split ancestor imports", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			env.OpenFile("b/b.cue")
			env.Await(
				// There's a snapshot for b/b.cue (because it was opened):
				LogMatching(protocol.Debug, `/b/b\.cue":{"file://`, 1, false),
				// b/b.cue is used in two packages because of ancestor imports:
				LogMatching(protocol.Debug, `/b/b\.cue":\["mod\.example/x/b/c@v0:b","mod\.example/x/b@v0:b"\]`, 1, false),
				// b/c/c.cue only exists in one package:
				LogMatching(protocol.Debug, `/b/c/c.cue":\["mod\.example/x/b/c@v0:b"\]`, 1, false),
			)

			// change b/b.cue (which is an ancestor import of b/c/c.cue)
			// package from "b" to "bz" (thus impacting the build files
			// of b/c/c.cue)
			env.EditBuffer("b/b.cue", fake.NewEdit(0, 9, 0, 9, "z"))

			env.Await(
				// There's still a snapshot for b/b.cue:
				LogMatching(protocol.Debug, `/b/b\.cue":{"file://`, 2, false),
				// b.cue is now only in the package bz
				LogMatching(protocol.Debug, `/b/b\.cue":\["mod\.example/x/b@v0:bz"\]`, 1, false),
				// b/c/c.cue only exists in one package:
				LogMatching(protocol.Debug, `/b/c/c.cue":\["mod\.example/x/b/c@v0:b"\]`, 2, false),
			)
		})
	})

	t.Run("edit - create ancestor imports", func(t *testing.T) {
		WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
			env.OpenFile("a/a.cue")
			env.Await(
				// There's a snapshot for a/a.cue (because it was opened):
				LogMatching(protocol.Debug, `/a/a\.cue":{"file://`, 1, false),
				// a/a.cue belongs to two packages:
				LogMatching(protocol.Debug, `/a/a\.cue":\["mod\.example/x/a","mod\.example/x/a@v0:a"\]`, 1, false),
				// a/d/d.cue belongs to one package:
				LogMatching(protocol.Debug, `/a/d/d\.cue":\["mod\.example/x/a/d@v0:e"\]`, 1, false),
			)

			// change a/a.cue package from "a" to "e". This makes it
			// become the same package as a/d/d.cue
			env.EditBuffer("a/a.cue", fake.NewEdit(0, 8, 0, 9, "e"))

			env.Await(
				// There's still a snapshot for a/a.cue:
				LogMatching(protocol.Debug, `/a/a\.cue":{"file://`, 2, false),
				// a/a.cue has moved into the e package, both in the x/a/d/ path, and the x/a/ path:
				LogMatching(protocol.Debug, `/a/a\.cue":\["mod\.example/x/a/d@v0:e","mod\.example/x/a@v0:e"\]`, 1, false),
				// a/d/d.cue still belongs to one package:
				LogMatching(protocol.Debug, `/a/d/d\.cue":\["mod\.example/x/a/d@v0:e"\]`, 2, false),
			)
		})
	})
}
