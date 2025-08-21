package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"

	"github.com/go-quicktest/qt"
)

// TestImportsCanonical checks that imports which are needlessly spelt
// with explicit qualifiers do not cause a problem.
func TestImportsCanonical(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
-- a/a.cue --
package a

import "example.com/bar/x:x"

out: x.y
-- x/x.cue --
package x

y: 3
`

	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.Await(
			LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
		)
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dirs=[%v/a] importPath=example.com/bar/a@v0", rootURI, rootURI),
			// A package is created for the imported package.
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dirs=[%v/x] importPath=example.com/bar/x", rootURI, rootURI),
		)
		// Now perform a jump-to-dfn from the open a.cue file, from the
		// "y" in "out: x.y", which should take us to the x/x.cue file.
		from := protocol.Location{
			URI:   rootURI + "/a/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 7}},
		}

		// It doesn't work because the spelling of the import with the explicit qualifier.
		wantTo := []protocol.Location(nil)

		gotTo := env.Definition(from)
		qt.Assert(t, qt.ContentEquals(gotTo, wantTo), qt.Commentf("from: %#v", from))
	})
}
