package workspace

import (
	"testing"

	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestDiagnosticsUnparseableFile tests that a file whose content
// cannot be parsed at all (no AST can be produced, e.g. fatally
// invalid YAML) still has its parse error published as a diagnostic,
// and that the diagnostic clears when the content is fixed.
// Historically, once a file's parse produced no AST, diagnostics for
// the file froze entirely: new errors were never surfaced and stale
// diagnostics never cleared.
func TestDiagnosticsUnparseableFile(t *testing.T) {
	const files = `
-- a.yaml --
x: true
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("a.yaml")
		env.Await(
			env.DoneWithOpen(),
			NoDiagnostics(ForFile("a.yaml")),
		)

		// This YAML is fatally invalid: no AST can be produced.
		env.SetBufferContent("a.yaml", "x: [\n- {y\n")
		env.Await(
			env.DoneWithChange(),
			Diagnostics(ForFile("a.yaml")),
		)

		// And on fixing the content, the diagnostic clears.
		env.SetBufferContent("a.yaml", "x: true\n")
		env.Await(
			env.DoneWithChange(),
			NoDiagnostics(ForFile("a.yaml")),
		)
	})
}
