package workspace

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestRename(t *testing.T) {
	const files = `
NB want/a/a.cue shows the current bad behaviour: the renaming is
applying to the alias too.

-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.14.0"
-- a/a.cue --
package a

_x: {
  outer="out": 3
  a1: a2: _x.out
  a3: outer
}
_x
-- b/b1.cue --
package b

import "example.com/bar/a"

b1: a.out
-- b/b2.cue --
package b

import "example.com/bar/a:a"

b2: a.out
-- b/b3.cue --
package b

import mya "example.com/bar/a"

b3: mya.out
-- c/c.cue --
package c

import p1 "example.com/bar/a"
import p2 "example.com/bar/a"
import p3 "example.com/bar/a"

c1: p1.out
c2: p2.out
c3: p3.out
-- want/a/a.cue --
package a

_x: {
  result="result": 3
  a1: a2: _x.result
  a3: result
}
_x
-- want/b/b1.cue --
package b

import "example.com/bar/a"

b1: a.result
-- want/b/b2.cue --
package b

import "example.com/bar/a:a"

b2: a.result
-- want/b/b3.cue --
package b

import mya "example.com/bar/a"

b3: mya.result
-- want/c/c.cue --
package c

import p1 "example.com/bar/a"
import p2 "example.com/bar/a"
import p3 "example.com/bar/a"

c1: p1.result
c2: p2.result
c3: p3.result
`

	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.OpenFile("c/c.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
		)

		result, err := env.Editor.Server.PrepareRename(env.Ctx, &protocol.PrepareRenameParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: rootURI + "/c/c.cue",
				},
				Position: protocol.Position{Line: 6, Character: 8}, // the u of out
			},
		})
		qt.Assert(t, qt.IsNil(err))
		// PrepareRename should return the range which is the whole of out
		qt.Assert(t, qt.DeepEquals(result.Range, protocol.Range{
			Start: protocol.Position{Line: 6, Character: 7},
			End:   protocol.Position{Line: 6, Character: 10},
		}))
		qt.Assert(t, qt.Equals(result.Placeholder, "out"))

		// Now do the rename for real
		env.Rename(protocol.Location{
			URI:   rootURI + "/c/c.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 6, Character: 8}}, // the u of out
		}, "result")

		for _, file := range txtar.Parse([]byte(files)).Files {
			if filename, ok := strings.CutPrefix(file.Name, "want/"); ok {
				content, _ := env.Editor.BufferText(filename)
				qt.Check(t, qt.Equals(content, string(file.Data)), qt.Commentf("%s", filename))
			}
		}
	})
}
