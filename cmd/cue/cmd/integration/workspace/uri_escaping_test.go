package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"golang.org/x/tools/txtar"
)

// TestFileNameNeedingEscaping shows bad behaviour: LSP functionality
// is dead within a file whose name contains a character which URIs
// escape (a space). URIs received from the client are
// percent-encoded, but URIs built internally are assembled by string
// concatenation with no encoding, so the workspace holds two
// unrelated states for such a file, and hover (among much else)
// returns nothing.
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

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(env.Sandbox.Workdir.URI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		p := fln("my data.json", 3, 1, `cows`)
		p.determinePos(mappers)
		got, _ := env.Hover(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		if got != nil {
			t.Errorf("hover in %q = %v, want nothing", "my data.json", got)
		}
	})
}
