//go:build !windows

package feature

import (
	"fmt"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"

	"cuelang.org/go/internal/golangorgx/gopls/hooks"
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

func TestEdits(t *testing.T) {
	const files = `
-- foo.cue --
package foo

  // this is a test
-- other/file1.cue --
-- other/file2.cue --
`
	Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("foo.cue")
		env.OpenFile("other/file1.cue")
		env.OpenFile("other/file2.cue")

		// Leave in for now for debugging of server events, caches etc
		env.EditBuffer("foo.cue", fake.NewEdit(0, 0, 1, 0, "package bar\n"))

		env.FormatBuffer("foo.cue")
		got := env.BufferText("foo.cue")
		want := "package bar\n\n// this is a test\n"
		qt.Assert(t, qt.Equals(got, want))
	})
}

func xTestDefinition(t *testing.T) {
	const files = `
-- foo1.cue --
package foo

x: 5
-- foo2.cue --
package foo

y: x
-- another.cue --
package another

something: "else"
`
	Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("foo2.cue")
		got := env.GoToDefinition(env.LineCol8Location("foo2.cue:3:4"))
		want := []protocol.Location{
			env.LineCol8Location("foo1.cue:3:1-3:2"),
		}
		fmt.Printf("%v\n", want[0])
		qt.Assert(t, qt.DeepEquals(got, want))
	})
}
