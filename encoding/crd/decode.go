package crd

import (
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal"
)

// Extract converts CustomResourceDefinitions to an equivalent CUE representation.
//
// It currently converts entries in spec.versions[*].openApiV3.
func Extract(data cue.InstanceOrValue, c *Config) (*ast.File, error) {
	return openapi.Extract(data, c.OpenAPICfg)
}

const oapiSchemas = "#/components/schemas/"

// rootDefs is the fallback for schemas that are not valid identifiers.
// TODO: find something more principled.
const rootDefs = "#SchemaMap"

func openAPIMapping(pos token.Pos, a []string) ([]ast.Label, error) {
	if len(a) != 3 || a[0] != "components" || a[1] != "schemas" {
		return nil, errors.Newf(pos,
			`openapi: reference must be of the form %q; found "#/%s"`,
			oapiSchemas, strings.Join(a, "/"))
	}
	name := a[2]
	if ast.IsValidIdent(name) &&
		name != rootDefs[1:] &&
		!internal.IsDefOrHidden(name) {
		return []ast.Label{ast.NewIdent("#" + name)}, nil
	}
	return []ast.Label{ast.NewIdent(rootDefs), ast.NewString(name)}, nil
}
