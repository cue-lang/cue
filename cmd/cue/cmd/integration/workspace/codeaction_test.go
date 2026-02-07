package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestCodeActionConvertToStruct(t *testing.T) {
	const files = `
-- a.cue --
foo: bar: "baz"
-- want/a.cue --
foo: {
	bar: "baz"
}
-- b.cue --
before: _
          foo: bar: {
	"baz"
}
  after: _ // weird indent
-- want/b.cue --
before: _
          foo: {
          	bar: {
          	"baz"
          }
}
  after: _ // weird indent
-- c.cue --
a: [{x: y: z: _, w: "foo"}.w]: {}
-- want/c.cue --
a: [{x: {
	y: z: _
}, w: "foo"}.w]: {}
-- d.cue --
  a: b: _, x: y: _
-- want/d.cue --
  a: {
  	b: _
  }
  x: y: _
`

	type testCase struct {
		name     string
		filename string
		position protocol.Position
	}
	testCases := []testCase{
		{
			name:     "simple",
			filename: "a.cue",
			position: protocol.Position{Line: 0, Character: 5}, // bar
		},
		{
			name:     "multiline_field",
			filename: "b.cue",
			position: protocol.Position{Line: 1, Character: 15}, // bar
		},
		{
			name:     "nested_fields",
			filename: "c.cue",
			position: protocol.Position{Line: 0, Character: 8}, // y
		},
		{
			name:     "comma_separated",
			filename: "d.cue",
			position: protocol.Position{Line: 0, Character: 5}, // b
			// Note how the comma between _ and x: gets removed
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
				env.OpenFile(tc.filename)
				env.Await(env.DoneWithOpen())
				rootURI := env.Sandbox.Workdir.RootURI()

				cursor := protocol.Location{
					URI: rootURI + "/" + protocol.DocumentURI(tc.filename),
					Range: protocol.Range{
						Start: tc.position,
					},
				}

				actions, err := env.Editor.CodeAction(env.Ctx, cursor, nil)
				if err != nil {
					qt.Assert(t, qt.IsNil(err))
				}

				found := false
				var action protocol.CodeAction
				for _, action = range actions {
					if action.Title == "Wrap field in struct" {
						found = true
						break
					}
				}
				if !found {
					t.Fatal("Failed to find ConvertToStruct code action")
				}

				env.ApplyCodeAction(action)
				after := env.BufferText(tc.filename)
				for _, file := range txtar.Parse([]byte(files)).Files {
					if file.Name != "want/"+tc.name {
						continue
					}
					t.Log(after)
					qt.Check(t, qt.Equals(after, string(file.Data)))
					break
				}
			})
		})
	}
}
