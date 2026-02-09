package workspace

import (
	"slices"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

func TestCodeActionConvertToStruct(t *testing.T) {
	type testCase struct {
		name     string
		input    string
		position protocol.Position
		expected string
	}
	testCases := []testCase{
		{
			name:     "simple",
			input:    `foo: bar: "baz"`,
			position: protocol.Position{Line: 0, Character: 5}, // bar
			expected: `
foo: {
	bar: "baz"
}
`[1:],
		},

		{
			name: "multiline_field",
			input: `
before: _
          foo: bar: {
	"baz"
}
  after: _ // weird indent
`[1:],
			position: protocol.Position{Line: 1, Character: 15}, // bar
			expected: `
before: _
          foo: {
          	bar: {
          		"baz"
          	}
          }
  after: _ // weird indent
`[1:],
		},

		{
			name:     "nested_fields",
			input:    `a: [{x: y: z: _, w: "foo"}.w]: {}`,
			position: protocol.Position{Line: 0, Character: 8}, // y
			expected: `
a: [{x: {
	y: z: _
}
w: "foo"}.w]: {}
`[1:],
		},

		{
			name:     "comma_separated",
			input:    `  a: b: _, x: y: _`,
			position: protocol.Position{Line: 0, Character: 5}, // b
			// Note how the comma between _ and x: gets removed
			expected: `
  a: {
  	b: _
  }
  x: y: _
`[1:],
		},

		{
			name: "multiline_labels",
			input: `
a:
	b: _
`[1:],
			position: protocol.Position{Line: 1, Character: 1}, // b
			expected: `
a:
	{
		b: _
	}
`[1:],
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			WithOptions(RootURIAsDefaultFolder()).Run(t, "-- input.cue --\n"+tc.input, func(t *testing.T, env *Env) {
				env.OpenFile("input.cue")
				env.Await(env.DoneWithOpen())
				rootURI := env.Sandbox.Workdir.RootURI()

				cursor := protocol.Location{
					URI: rootURI + "/input.cue",
					Range: protocol.Range{
						Start: tc.position,
					},
				}

				actions, err := env.Editor.CodeAction(env.Ctx, cursor, nil)
				if err != nil {
					qt.Assert(t, qt.IsNil(err))
				}

				var action protocol.CodeAction
				found := slices.ContainsFunc(actions, func(a protocol.CodeAction) bool {
					if a.Title == "Wrap field in struct" {
						action = a
						return true
					}
					return false
				})
				if !found {
					t.Fatal("Failed to find ConvertToStruct code action")
				}

				env.ApplyCodeAction(action)
				after := env.BufferText("input.cue")
				qt.Check(t, qt.Equals(after, tc.expected))
			})
		})
	}
}

func TestCodeActionConvertFromStruct(t *testing.T) {
	type testCase struct {
		name     string
		input    string
		position protocol.Position
		expected string
	}
	testCases := []testCase{
		{
			name: "simple",
			input: `
foo: {
	bar: "baz"
}
`[1:],
			position: protocol.Position{Line: 1, Character: 1}, // bar
			expected: `
foo: bar: "baz"
`[1:],
		},

		{
			name: "multiline_field",
			input: `
before: _
          foo: {
          	bar: {
          		"baz"
          	}
          }
  after: _ // weird indent
`[1:],
			position: protocol.Position{Line: 2, Character: 11}, // bar
			expected: `
before: _
          foo: bar: {
          			"baz"
          		}
  after: _ // weird indent
`[1:],
		},

		{
			name: "nested_fields",
			input: `
a: [{x: {
	y: z: _
}
w: "foo"}.w]: {}
`[1:],
			position: protocol.Position{Line: 1, Character: 1}, // y
			expected: `
a: [{x: y: z: _
w: "foo"}.w]: {}
`[1:],
		},

		{
			name: "comma_separated",
			input: `
  a: {
  	b: _
  }, x: y: _
`[1:],
			position: protocol.Position{Line: 1, Character: 3}, // b
			expected: `
  a: b: _
  x: y: _
`[1:],
		},

		{
			name: "multiline_labels",
			input: `
a:
	{
		b: _
	}
`[1:],
			position: protocol.Position{Line: 2, Character: 2}, // b
			expected: `
a:
	b: _
`[1:],
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			WithOptions(RootURIAsDefaultFolder()).Run(t, "-- input.cue --\n"+tc.input, func(t *testing.T, env *Env) {
				env.OpenFile("input.cue")
				env.Await(env.DoneWithOpen())
				rootURI := env.Sandbox.Workdir.RootURI()

				cursor := protocol.Location{
					URI: rootURI + "/input.cue",
					Range: protocol.Range{
						Start: tc.position,
					},
				}

				actions, err := env.Editor.CodeAction(env.Ctx, cursor, nil)
				if err != nil {
					qt.Assert(t, qt.IsNil(err))
				}

				var action protocol.CodeAction
				found := slices.ContainsFunc(actions, func(a protocol.CodeAction) bool {
					if a.Title == "Unwrap field from struct" {
						action = a
						return true
					}
					return false
				})
				if !found {
					t.Fatal("Failed to find ConvertFromStruct code action")
				}

				env.ApplyCodeAction(action)
				after := env.BufferText("input.cue")
				qt.Check(t, qt.Equals(after, tc.expected))
			})
		})
	}
}
