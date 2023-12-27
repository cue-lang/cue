//go:build !windows

package feature

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/hooks"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

func TestMain(m *testing.M) {
	Main(m, hooks.Options)
}

func TestOpenFile(t *testing.T) {
	const files = `
-- go.mod --
module mod.com

go 1.12
-- foo.go --
	package foo
`
	Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("foo.go")
		env.FormatBuffer("foo.go")
		got := env.BufferText("foo.go")
		want := "package foo\n"
		qt.Assert(t, qt.Equals(got, want))
	})
}
