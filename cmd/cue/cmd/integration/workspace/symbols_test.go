package workspace

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestDocumentSymbols(t *testing.T) {
	const files = `
-- m/cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- m/a/a.cue --
package a

x: int
y: z: {
  int
}
-- m/a/a.cue.golden --
x: range:2:0-2:6 selectionRange:2:0-2:1
y: range:3:0-5:1 selectionRange:3:0-3:1
y.z: range:3:3-5:1 selectionRange:3:3-3:4
-- standalone/a.cue --
b: {
  cat: int
}
a: int
-- standalone/a.cue.golden --
b: range:0:0-2:1 selectionRange:0:0-0:1
b.cat: range:1:2-1:10 selectionRange:1:2-1:5
a: range:3:0-3:6 selectionRange:3:0-3:1
`

	archive := make(map[string][]byte)
	for _, file := range txtar.Parse([]byte(files)).Files {
		archive[file.Name] = file.Data
	}

	for name := range archive {
		if !strings.HasSuffix(name, ".cue") {
			continue
		}

		t.Run(name, func(t *testing.T) {
			WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
				rootURI := env.Sandbox.Workdir.RootURI()

				env.OpenFile(name)
				env.Await(env.DoneWithOpen())
				symbols, err := env.Editor.Server.DocumentSymbol(env.Ctx, &protocol.DocumentSymbolParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: rootURI + "/" + protocol.DocumentURI(name)},
				})
				qt.Assert(t, qt.IsNil(err))
				text, err := symbolsToLines(symbols)
				qt.Assert(t, qt.IsNil(err))
				qt.Assert(t, qt.Equals(text, string(archive[name+".golden"])))
			})
		})
	}
}

func symbolsToLines(symbols []any) (string, error) {
	var out strings.Builder
	// Print each symbol in the response tree.
	var visit func(sym protocol.DocumentSymbol, prefix []string)
	visit = func(sym protocol.DocumentSymbol, prefix []string) {
		fmt.Fprintf(&out, "%s: range:%v selectionRange:%v\n",
			strings.Join(prefix, "."), sym.Range, sym.SelectionRange)

		for _, child := range sym.Children {
			visit(child, append(prefix, child.Name))
		}
	}

	for _, symbol := range symbols {
		symMap := symbol.(map[string]any)
		bites, err := json.Marshal(symMap)
		if err != nil {
			return "", err
		}
		var sym protocol.DocumentSymbol
		err = json.Unmarshal(bites, &sym)
		if err != nil {
			return "", err
		}
		visit(sym, []string{sym.Name})
	}

	return out.String(), nil
}
