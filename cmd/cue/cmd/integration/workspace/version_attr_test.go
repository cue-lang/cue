package workspace

import (
	"testing"

	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestVersionAttribute tests that the language server treats a file at the
// language version that the command line treats it at: a standalone file at
// the version of its @version attribute, and a file of a module at the
// module's version, where the attribute is an error at the attribute.
func TestVersionAttribute(t *testing.T) {
	t.Parallel()

	// A prefix alias is valid before v0.18.0, where aliasv2 became stable.
	const body = "_alias: X={a: 1, b: X.a}\nout: _alias.b\n"

	t.Run("standalone", func(t *testing.T) {
		files := `
-- marked.cue --
@version("v0.17.0")

` + body + `
-- plain.cue --
` + body
		I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
			env.OpenFile("marked.cue")
			env.OpenFile("plain.cue")
			env.Await(
				env.DoneWithOpen(),
				I.NoDiagnostics(I.ForFile("marked.cue")),
				I.Diagnostics(I.ForFile("plain.cue"), env.AtRegexp("plain.cue", "_alias")),
			)
		})
	})

	t.Run("module", func(t *testing.T) {
		const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@version("v0.17.0")

package a

x: 1
`
		I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
			env.OpenFile("a.cue")
			env.Await(
				env.DoneWithOpen(),
				I.Diagnostics(I.ForFile("a.cue"), env.AtRegexp("a.cue", "@version")),
			)

			// Removing the attribute clears the diagnostic.
			env.SetBufferContent("a.cue", "package a\n\nx: 1\n")
			env.Await(
				env.DoneWithChange(),
				I.NoDiagnostics(I.ForFile("a.cue")),
			)
		})
	})

	// As for cue/load, a file within cue.mod belongs to no module, so its
	// attribute applies.
	t.Run("cuemod", func(t *testing.T) {
		const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- cue.mod/gen/example.com/g/g.cue --
@version("v0.17.0")

package g

g: 1
`
		I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
			env.OpenFile("cue.mod/gen/example.com/g/g.cue")
			env.Await(
				env.DoneWithOpen(),
				I.NoDiagnostics(I.ForFile("cue.mod/gen/example.com/g/g.cue")),
			)
		})
	})
}
