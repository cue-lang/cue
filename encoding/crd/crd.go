package crd

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/encoding/openapi"
)

// A Config defines options for converting CUE to and from Kubernetes CustomResourceDefinitions.
type Config struct {
	OpenAPICfg *openapi.Config

	// PkgName defines the package name for a generated CUE package.
	PkgName string

	// Info specifies the info section of the OpenAPI document. To be a valid
	// OpenAPI document, it must include at least the title and version fields.
	// Info may be a *ast.StructLit or any type that marshals to JSON.
	Info interface{}

	// ReferenceFunc allows users to specify an alternative representation
	// for references. An empty string tells the generator to expand the type
	// in place and, if applicable, not generate a schema for that entity.
	//
	// If this field is non-nil and a cue.Value is passed as the InstanceOrValue,
	// there will be a panic.
	//
	// Deprecated: use NameFunc instead.
	ReferenceFunc func(inst *cue.Instance, path []string) string

	// NameFunc allows users to specify an alternative representation
	// for references. It is called with the value passed to the top level
	// method or function and the path to the entity being generated.
	// If it returns an empty string the generator will  expand the type
	// in place and, if applicable, not generate a schema for that entity.
	//
	// Note: this only returns the final element of the /-separated
	// reference.
	NameFunc func(val cue.Value, path cue.Path) string

	// DescriptionFunc allows rewriting a description associated with a certain
	// field. A typical implementation compiles the description from the
	// comments obtains from the Doc method. No description field is added if
	// the empty string is returned.
	DescriptionFunc func(v cue.Value) string

	// SelfContained causes all non-expanded external references to be included
	// in this document.
	SelfContained bool

	// OpenAPI version to use. Supported as of v3.0.0.
	Version string

	// FieldFilter defines a regular expression of all fields to omit from the
	// output. It is only allowed to filter fields that add additional
	// constraints. Fields that indicate basic types cannot be removed. It is
	// an error for such fields to be excluded by this filter.
	// Fields are qualified by their Object type. For instance, the
	// minimum field of the schema object is qualified as Schema/minimum.
	FieldFilter string

	// ExpandReferences replaces references with actual objects when generating
	// OpenAPI Schema. It is an error for an CUE value to refer to itself
	// if this option is used.
	ExpandReferences bool
}
