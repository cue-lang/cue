package workspace

import (
	"testing"

	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
)

func TestDiagnostics(t *testing.T) {
	module := `-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"
`

	for _, withModule := range []bool{false, true} {
		prefix := ""
		name := "without module"
		if withModule {
			name = "with module"
			prefix = module
		}

		t.Run(name, func(t *testing.T) {
			t.Run("bad start", func(t *testing.T) {
				files := prefix + `
-- a/a.cue --
hello world
`
				WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
					env.OpenFile("a/a.cue")
					env.Await(
						env.DoneWithOpen(),
						Diagnostics(ForFile("a/a.cue"), env.AtRegexp("a/a.cue", "world")),
					)
				})
			})

			t.Run("good start", func(t *testing.T) {
				files := prefix + `
-- a/a.cue --
"hello world"
`
				WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
					env.OpenFile("a/a.cue")
					env.Await(
						env.DoneWithOpen(),
						NoDiagnostics(ForFile("a/a.cue")),
					)
				})
			})

			t.Run("goes bad", func(t *testing.T) {
				files := prefix + `
-- a/a.cue --
helloworld
`
				WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
					env.OpenFile("a/a.cue")
					env.Await(
						env.DoneWithOpen(),
						NoDiagnostics(ForFile("a/a.cue")),
					)

					env.EditBuffer("a/a.cue", fake.NewEdit(0, 5, 0, 5, " ")) // transform to `hello world`
					env.Await(
						Diagnostics(ForFile("a/a.cue"), env.AtRegexp("a/a.cue", "world")),
					)
				})
			})

			t.Run("goes good", func(t *testing.T) {
				files := prefix + `
-- a/a.cue --
hello world
`
				WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
					env.OpenFile("a/a.cue")
					env.Await(
						env.DoneWithOpen(),
						Diagnostics(ForFile("a/a.cue"), env.AtRegexp("a/a.cue", "world")),
					)

					env.EditBuffer("a/a.cue", fake.NewEdit(0, 5, 0, 6, "")) // transform to `helloworld`
					env.Await(
						NoDiagnostics(ForFile("a/a.cue")),
					)
				})
			})
		})
	}
}
