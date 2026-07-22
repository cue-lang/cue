package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

// TestFileNameNeedingEscaping tests LSP functionality within a file
// whose name contains a character which URIs escape (a space). URIs
// received from the client are percent-encoded, so URIs built
// internally must be canonicalized the same way.
func TestFileNameNeedingEscaping(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file="my data.json")

out: field: {
	// does the field contain cows?
	cows: bool
}
-- my data.json --
{
  "field": {
    "cows": true
  }
}
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		env.OpenFile("my data.json")
		env.Await(env.DoneWithOpen())

		mappers := makeMappers(env, files)

		docComment := "does the field contain cows?"

		p := fln("my data.json", 3, 1, `cows`)
		p.determinePos(mappers)
		got, _ := env.Hover(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		qt.Assert(t, qt.IsNotNil(got))
		qt.Assert(t, qt.StringContains(got.Value, docComment))
	})
}
