package workspace

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"golang.org/x/tools/txtar"
)

// TestFileNameNeedingEscaping tests LSP functionality within a file
// whose name contains a character which URIs escape (a space). URIs
// received from the client are percent-encoded, so URIs built
// internally must be canonicalized the same way; historically they
// were built by string concatenation, so the workspace held two
// unrelated states for such a file, and hover (among much else)
// returned nothing.
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
