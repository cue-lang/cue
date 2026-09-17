package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestDiagnosticsUnparseableFile tests that a file whose content
// cannot be parsed at all (no AST can be produced, e.g. fatally
// invalid YAML) still has its parse error published as a diagnostic,
// and that the diagnostic clears when the content is fixed.
func TestDiagnosticsUnparseableFile(t *testing.T) {
	const files = `
-- a.yaml --
x: true
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		env.OpenFile("a.yaml")
		env.Await(
			env.DoneWithOpen(),
			I.NoDiagnostics(I.ForFile("a.yaml")),
		)

		// This YAML is fatally invalid: no AST can be produced.
		env.SetBufferContent("a.yaml", "x: [\n- {y\n")
		env.Await(
			env.DoneWithChange(),
			I.Diagnostics(I.ForFile("a.yaml")),
		)

		// And on fixing the content, the diagnostic clears.
		env.SetBufferContent("a.yaml", "x: true\n")
		env.Await(
			env.DoneWithChange(),
			I.NoDiagnostics(I.ForFile("a.yaml")),
		)
	})
}

// TestDiagnosticsUnparseablePackageFile tests that a package file
// whose content cannot be parsed at all still has its parse errors
// published as diagnostics, and that the package survives the reload.
// Package membership is decided by a scan of the package clause and
// imports alone, so a file whose body defeats the parser is still a
// member of its package.
func TestDiagnosticsUnparseablePackageFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
package a

x: 1
`
	// Each numeric label is a parse error on its own line: enough of
	// them trip the parser's too-many-errors bailout, after which it
	// returns no AST at all.
	bailout := `
package a

0: 1
1: 1
2: 1
3: 1
4: 1
5: 1
6: 1
7: 1
8: 1
9: 1
10: 1
11: 1
12: 1
13: 1
`[1:]
	good := `
package a

x: 1
`[1:]
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			I.NoDiagnostics(I.ForFile("a.cue")),
		)

		env.SetBufferContent("a.cue", bailout)
		env.Await(
			env.DoneWithChange(),
			I.LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			I.Diagnostics(I.ForFile("a.cue")),
		)

		// And on fixing the content, the diagnostics clear.
		env.SetBufferContent("a.cue", good)
		env.Await(
			env.DoneWithChange(),
			I.LogExactf(protocol.Debug, 3, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			I.NoDiagnostics(I.ForFile("a.cue")),
		)
	})
}
