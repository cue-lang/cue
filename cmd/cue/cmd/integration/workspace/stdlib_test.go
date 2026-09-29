package workspace

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
)

// TestStdlibCompletion checks that the members of standard library
// packages are offered as completions, that stdlib packages are
// loaded on demand, and that a loaded package is shared by all its
// importers.
func TestStdlibCompletion(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
package a

import "strings"

out: strings.ToUpper
-- b.cue --
package a

import (
	"encoding/json"
	"strings"
)

lower: strings.ToLower
js:    json.Marshal
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		env.OpenFile("a.cue")
		env.OpenFile("b.cue")
		env.Await(env.DoneWithOpen())

		mappers := makeMappers(env, files)

		testCases := []struct {
			pos    position
			expect []string
		}{
			{fln("a.cue", 5, 1, "ToUpper"), []string{"ToUpper", "ToLower", "HasPrefix", "MinRunes"}},
			{fln("b.cue", 8, 1, "ToLower"), []string{"ToUpper", "ToLower"}},
			{fln("b.cue", 9, 1, "Marshal"), []string{"Marshal", "Unmarshal", "Validate"}},
		}
		for _, tc := range testCases {
			tc.pos.determinePos(mappers)
			completions := env.Completion(protocol.Location{
				URI:   tc.pos.mapper.URI,
				Range: protocol.Range{Start: tc.pos.pos},
			})
			qt.Assert(t, qt.IsNotNil(completions), qt.Commentf("%v", &tc.pos))
			items := make(map[string]protocol.CompletionItem, len(completions.Items))
			for _, item := range completions.Items {
				items[item.Label] = item
			}
			for _, expected := range tc.expect {
				item, found := items[expected]
				qt.Assert(t, qt.IsTrue(found),
					qt.Commentf("%v: missing %q in %v", &tc.pos, expected, completions.Items))
				// Builtin functions complete as functions, with their
				// signature as the detail.
				qt.Check(t, qt.Equals(item.Kind, protocol.FunctionCompletion),
					qt.Commentf("%v: %q", &tc.pos, expected))
				qt.Check(t, qt.StringContains(item.Detail, "func("),
					qt.Commentf("%v: %q", &tc.pos, expected))
			}
		}

		// Even though two files import strings and three requests
		// consulted it, the package is loaded exactly once and shared.
		env.Await(
			I.LogExactf(protocol.Debug, 1, false, "Stdlib package strings Loaded"),
			I.LogExactf(protocol.Debug, 1, false, "Stdlib package encoding/json Loaded"),
		)
	})
}

// TestStdlibHover checks that hovering over the member of a standard
// library package shows the member's documentation, which comes from
// the doc comment in the package's definition file.
func TestStdlibHover(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
package a

import "strings"

out: strings.ToUpper
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		env.OpenFile("a.cue")
		env.Await(env.DoneWithOpen())

		mappers := makeMappers(env, files)
		p := fln("a.cue", 5, 1, "ToUpper")
		p.determinePos(mappers)

		got, _ := env.Hover(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		qt.Assert(t, qt.IsNotNil(got))
		qt.Assert(t, qt.StringContains(got.Value,
			"```cue\nfunc(s: string) -> string\n```"))
		qt.Assert(t, qt.StringContains(got.Value,
			"ToUpper returns s with all Unicode letters mapped to their"))
		// The signature replaces the "Unified with" value section,
		// which for a builtin would only show `_`.
		qt.Assert(t, qt.IsFalse(strings.Contains(got.Value, "Unified with")))

		// Jumping to the definition of a stdlib member is not yet
		// supported: the definition files have no on-disk location for
		// the editor to open. The request must fail gracefully.
		locs := env.Definition(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		qt.Assert(t, qt.HasLen(locs, 0))
	})
}

// TestStdlibHoverNamedType checks that a signature referencing a type
// the package itself declares — net.IPv4's #IP — renders the
// reference, and that the referenced type is an ordinary member: it
// can be used from user code, and hovering it shows its declaration
// and documentation.
func TestStdlibHoverNamedType(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
package a

import "net"

ok:   net.IPv4
addr: net.#IP
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		env.OpenFile("a.cue")
		env.Await(env.DoneWithOpen())

		mappers := makeMappers(env, files)
		p := fln("a.cue", 5, 1, "IPv4")
		p.determinePos(mappers)

		got, _ := env.Hover(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		qt.Assert(t, qt.IsNotNil(got))
		qt.Assert(t, qt.StringContains(got.Value,
			"```cue\nvalidator(#IP) | (func(ip: #IP) -> bool)\n```"))
		qt.Assert(t, qt.StringContains(got.Value,
			"IPv4 reports whether ip is a valid IPv4 address"))

		p = fln("a.cue", 6, 1, "#IP")
		p.determinePos(mappers)

		got, _ = env.Hover(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		qt.Assert(t, qt.IsNotNil(got))
		qt.Assert(t, qt.StringContains(got.Value, "string | bytes | [...int]"))
		qt.Assert(t, qt.StringContains(got.Value, "An #IP is an IP address"))
	})
}

// TestStdlibStandalone checks that imports of standard library
// packages also resolve within standalone files: files that are part
// of no package or module.
func TestStdlibStandalone(t *testing.T) {
	t.Parallel()
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, "", func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		content := `
import "strings"

out: strings.ToUpper
`[1:]
		env.CreateBuffer("a/a.cue", content)
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Created", rootURI),
			I.LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a/a.cue Reloaded", rootURI),
		)

		mapper := protocol.NewMapper(rootURI+"/a/a.cue", []byte(content))
		p := fln("a/a.cue", 3, 1, "ToUpper")
		p.determinePos(map[string]*protocol.Mapper{"a/a.cue": mapper})

		completions := env.Completion(protocol.Location{
			URI:   p.mapper.URI,
			Range: protocol.Range{Start: p.pos},
		})
		qt.Assert(t, qt.IsNotNil(completions))
		labels := make(map[string]bool, len(completions.Items))
		for _, item := range completions.Items {
			labels[item.Label] = true
		}
		qt.Assert(t, qt.IsTrue(labels["ToUpper"]),
			qt.Commentf("missing ToUpper in %v", completions.Items))
	})
}

// TestStdlibReferences checks that the usages of a standard library
// member are reported across every kind of importer, packages and
// standalone files alike, and that they remain complete, without
// stale duplicates, as importers reload: a reload discards an
// importer's evaluator, and the usage records it left in the shared
// standard library evaluators must not linger.
func TestStdlibReferences(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a/a.cue --
package a

import "strings"

out: strings.ToUpper

// reva0
-- b/b.cue --
package b

import "strings"

out: strings.ToUpper
-- loose.cue --
import "strings"

out: strings.ToUpper

// revl0
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.OpenFile("a/a.cue")
		env.OpenFile("b/b.cue")
		env.OpenFile("loose.cue")
		env.Await(env.DoneWithOpen())

		mappers := makeMappers(env, files)
		use := func(filename string, line uint32) protocol.Location {
			return protocol.Location{
				URI: rootURI + protocol.DocumentURI("/"+filename),
				Range: protocol.Range{
					Start: protocol.Position{Line: line, Character: 13},
					End:   protocol.Position{Line: line, Character: 20},
				},
			}
		}
		aUse := use("a/a.cue", 4)
		bUse := use("b/b.cue", 4)
		looseUse := use("loose.cue", 2)

		refsFrom := func(filename string) []protocol.Location {
			p := fln(filename, 5, 1, "ToUpper")
			p.determinePos(mappers)
			return env.References(protocol.Location{
				URI:   p.mapper.URI,
				Range: protocol.Range{Start: p.pos},
			})
		}

		refs := refsFrom("a/a.cue")
		qt.Assert(t, qt.DeepEquals(refs, []protocol.Location{aUse, bUse, looseUse}))

		// Reloading a standalone importer must neither lose its
		// current usages nor leave stale ones behind.
		for _, rev := range []string{"revl1", "revl2"} {
			env.RegexpReplace("loose.cue", `revl\d`, rev)
			env.Await(env.DoneWithChange())
			refs := refsFrom("a/a.cue")
			qt.Assert(t, qt.DeepEquals(refs, []protocol.Location{aUse, bUse, looseUse}),
				qt.Commentf("after standalone reload %s", rev))
		}

		// Reloading one importing package resets the standard library
		// packages it shares with other importers; the usages of those
		// other importers must survive.
		env.RegexpReplace("a/a.cue", `reva\d`, "reva1")
		env.Await(env.DoneWithChange())
		refs = refsFrom("b/b.cue")
		qt.Assert(t, qt.DeepEquals(refs, []protocol.Location{bUse, aUse, looseUse}))
	})
}
