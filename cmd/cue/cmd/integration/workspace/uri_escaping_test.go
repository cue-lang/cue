package workspace

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
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
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
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
		if got == nil || !strings.Contains(got.Value, docComment) {
			t.Errorf("hover in %q = %v, want doc comment", "my data.json", got)
		}
	})
}
