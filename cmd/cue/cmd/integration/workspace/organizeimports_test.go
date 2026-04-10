package workspace

import (
	"slices"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

func TestCodeActionOrganizeImports(t *testing.T) {
	type testCase struct {
		name     string
		input    string
		expected string
	}
	testCases := []testCase{
		{
			name:     "empty",
			input:    "package p1\n",
			expected: "package p1\n",
		},

		{
			name: "used_single",
			input: `
package p1

import "mod.com/p2"

x: p2
`[1:],
			expected: `
package p1

import "mod.com/p2"

x: p2
`[1:],
		},

		{
			name: "used_multiple_separate",
			input: `
package p1

import "mod.com/p3"
import "mod.com/p2"

x: p2 & p3
`[1:],
			expected: `
package p1

import "mod.com/p3"
import "mod.com/p2"

x: p2 & p3
`[1:],
		},

		{
			name: "used_multiple_joined",
			input: `
package p1

import (
  "mod.com/p3"
  	  	 "mod.com/p2"
              )

x: p2 & p3
`[1:],
			expected: `
package p1

import (
	"mod.com/p2"
	"mod.com/p3"
)

x: p2 & p3
`[1:],
		},

		{
			name: "mixed_separate",
			input: `
package p1

import "mod.com/p3"
import "mod.com/p2"

x: p3
`[1:],
			expected: `
package p1

import "mod.com/p3"


x: p3
`[1:],
		},

		{
			name: "mixed_joined_one_survives",
			input: `
package p1

import (
"mod.com/p3"
      "mod.com/p2")

x: p3
`[1:],
			expected: `
package p1

import "mod.com/p3"

x: p3
`[1:],
		},

		{
			name: "mixed_joined_several_survive",
			input: `
package p1

import (
"mod.com/p3"
"mod.com/p4"
      "mod.com/p2")

x: p4 & p2
`[1:],
			expected: `
package p1

import (
	"mod.com/p2"
	"mod.com/p4"
)

x: p4 & p2
`[1:],
		},

		{
			name: "mixed_mixed",
			input: `
package p1

import (
"mod.com/p3"
"mod.com/p2"
)

import "mod.com/p1"

import (
"mod.com/p4"
"mod.com/p7"
      "mod.com/p6")

import "mod.com/p5"

x: p1 & p4 & p7
`[1:],
			expected: `
package p1



import "mod.com/p1"

import (
	"mod.com/p4"
	"mod.com/p7"
)



x: p1 & p4 & p7
`[1:],
		},
	}

	for _, tc := range testCases {
		fun := func(t *testing.T, env *Env) {
			resolveSupport, err := env.Editor.EditResolveSupport()
			if err != nil {
				t.Fatal(err)
			}

			env.OpenFile("input.cue")
			env.Await(env.DoneWithOpen())
			rootURI := env.Sandbox.Workdir.RootURI()

			cursor := protocol.Location{URI: rootURI + "/input.cue"}

			actions, err := env.Editor.CodeAction(env.Ctx, cursor, nil)
			if err != nil {
				qt.Assert(t, qt.IsNil(err))
			}

			var action protocol.CodeAction
			found := slices.ContainsFunc(actions, func(a protocol.CodeAction) bool {
				if a.Title == "Organize Imports" {
					action = a
					return true
				}
				return false
			})
			if !found {
				t.Fatal("Failed to find Organize Imports code action")
			}
			// If we advertised to the LSP that we support lazy
			// resolution for codeactions, we should have been sent back
			// a nil-Edit property.
			qt.Assert(t, qt.Equals(action.Edit == nil, resolveSupport))
			// Calling ApplyCodeAction will make the additional call to
			// resolve the Edit property if necessary.
			env.ApplyCodeAction(action)
			after := env.BufferText("input.cue")
			qt.Check(t, qt.Equals(after, tc.expected))
		}

		t.Run(tc.name+"/eager", func(t *testing.T) {
			WithOptions(RootURIAsDefaultFolder()).Run(t, "-- input.cue --\n"+tc.input, fun)
		})

		t.Run(tc.name+"/lazy", func(t *testing.T) {
			WithOptions(
				RootURIAsDefaultFolder(),
				CapabilitiesJSON([]byte(`{
  "textDocument": {"codeAction": {
    "dataSupport": true,
    "resolveSupport": {"properties": ["edit"]}
  }}
}`)),
			).Run(t, "-- input.cue --\n"+tc.input, fun)
		})
	}
}
