package workspace

import (
	"testing"

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
