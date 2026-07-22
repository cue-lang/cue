package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestDiagnosticsUnparseableFile shows bad behaviour: a file whose
// content cannot be parsed at all (no AST can be produced, e.g.
// fatally invalid YAML) never has its parse error published as a
// diagnostic. The standalone file is silently deleted, taking the
// error record with it, and the user is told nothing.
func TestDiagnosticsUnparseableFile(t *testing.T) {
	const files = `
-- a.yaml --
x: true
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.yaml")
		env.Await(
			env.DoneWithOpen(),
			NoDiagnostics(ForFile("a.yaml")),
		)

		// This YAML is fatally invalid: no AST can be produced. The
		// standalone file is deleted, and no diagnostic appears.
		env.SetBufferContent("a.yaml", "x: [\n- {y\n")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 1, false, "StandaloneFile %v/a.yaml Deleted", rootURI),
			NoDiagnostics(ForFile("a.yaml")),
		)
	})
}
