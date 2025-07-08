//go:build !windows

package feature

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

func TestFormatFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example"

language: version: "v0.10.0"
-- foo.cue --
package foo

  // this is a test
`
	Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("foo.cue")
		env.EditBuffer("foo.cue", fake.NewEdit(0, 0, 1, 0, "package bar\n"))
		env.FormatBuffer("foo.cue")
		got := env.BufferText("foo.cue")
		want := "package bar\n\n// this is a test\n"
		qt.Assert(t, qt.Equals(got, want))
	})
}
