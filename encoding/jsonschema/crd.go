package jsonschema

import (
	_ "embed"
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

//go:generate go run cuelang.org/go/cmd/cue exp gengotypes .

//go:embed crd.cue
var crdCUE []byte

// CRDConfig holds configuration for [ExtractCRDs].
// Although this empty currently, it allows configuration
// to be added in the future without breaking the API.
type CRDConfig struct{}

// ExtractedCRD holds an extracted Kubernetes CRD and the data it was derived from.
type ExtractedCRD struct {
	// Versions holds the CUE schemas extracted from the CRD: one per
	// version.
	Versions map[string]*ast.File

	// Data holds chosen fields extracted from the source CRD document.
	Data *CRDSpec

	// Source holds the raw CRD document from which Data is derived.
	Source cue.Value
}

// ExtractCRDs extracts Kubernetes custom resource definitions
// (CRDs) from the given data. If data holds an array, each element
// of the array might itself be a Kubernetes resource.
//
// While the data must hold Kubernetes resources, those resources
// need not all be CRDs: resources with a kind that's not "CustomResourceDefinition"
// will be ignored.
//
// If cfg is nil, it's equivalent to passing a pointer to the zero-valued [CRDConfig].
func ExtractCRDs(data cue.Value, cfg *CRDConfig) ([]*ExtractedCRD, error) {
	crdInfos, crdValues, err := decodeCRDSpecs(data)
	if err != nil {
		return nil, fmt.Errorf("cannot decode CRD: %v", err)
	}
	crds := make([]*ExtractedCRD, len(crdInfos))
	for crdIndex, crd := range crdInfos {
		versions := make(map[string]*ast.File)
		for i, version := range crd.Spec.Versions {
			f, err := Extract(crdValues[crdIndex], &Config{
				PkgName: version.Name,
				// There are several kubernetes-related keywords that aren't implemented yet
				StrictFeatures: false,
				StrictKeywords: true,
				Root:           fmt.Sprintf("#/spec/versions/%d/schema/openAPIV3Schema", i),
				SingleRoot:     true,
				DefaultVersion: VersionKubernetesCRD,
			})
			if err != nil {
				return nil, err
			}
			namespaceConstraint := token.OPTION
			if crd.Spec.Scope == "Namespaced" {
				namespaceConstraint = token.NOT
			}
			// TODO provide a way to let this refer to a shared definition
			// in another package as the canonical definition for the schema.
			f.Decls = append(f.Decls,
				&ast.Field{
					Label: ast.NewIdent("apiVersion"),
					Value: ast.NewString(crd.Spec.Group + "/" + version.Name),
				},
				&ast.Field{
					Label: ast.NewIdent("kind"),
					Value: ast.NewString(crd.Spec.Names.Kind),
				},
				&ast.Field{
					Label:      ast.NewIdent("metadata"),
					Constraint: token.NOT,
					Value: ast.NewStruct(
						"name", token.NOT, ast.NewIdent("string"),
						"namespace", namespaceConstraint, ast.NewIdent("string"),
						// TODO inline struct lit
						"labels", token.OPTION, ast.NewStruct(
							ast.NewList(ast.NewIdent("string")), ast.NewIdent("string"),
						),
						"annotations", token.OPTION, ast.NewStruct(
							ast.NewList(ast.NewIdent("string")), ast.NewIdent("string"),
						),
						// The above fields aren't exhaustive.
						// TODO it would be nicer to refer to the actual #ObjectMeta
						// definition instead (and for that to be the case for embedded
						// resources in general) but that needs a deeper fix inside
						// encoding/jsonschema.
						&ast.Ellipsis{},
					),
				},
			)
			versions[version.Name] = f
		}
		crds[crdIndex] = &ExtractedCRD{
			Versions: versions,
			Data:     crdInfos[crdIndex],
			Source:   crdValues[crdIndex],
		}
	}
	return crds, nil
}

// decodeCRDSpecs decodes the CRD(s) held in the given value.
// It returns both the (partially) decoded CRDs and the values
// they were decoded from.
func decodeCRDSpecs(v cue.Value) ([]*CRDSpec, []cue.Value, error) {
	ctx := v.Context()

	// Check against the CUE version of the schema which can
	// do more detailed checks, including checking required fields,
	// before decoding into the Go struct.

	// TODO it would be nice to avoid compiling crdCUE every time, but
	// that's not possible given the restrictions on combining cue.Values
	// derived from different contexts.
	crdSchema := ctx.CompileBytes(crdCUE)
	crdSchema = crdSchema.FillPath(cue.MakePath(cue.Str("input")), v)
	specsv := crdSchema.LookupPath(cue.MakePath(cue.Str("specs")))
	if err := specsv.Err(); err != nil {
		return nil, nil, err
	}
	var specs []*CRDSpec
	var specsValues []cue.Value
	if err := specsv.Decode(&specs); err != nil {
		return nil, nil, err
	}
	if err := specsv.Decode(&specsValues); err != nil {
		return nil, nil, err
	}
	return specs, specsValues, nil
}
