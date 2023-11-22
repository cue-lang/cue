/*
Copyright 2023 Stefan Prodan

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package crd

import (
	"fmt"
	"path"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/crd/k8s/apiextensions"
	"cuelang.org/go/encoding/openapi"
)

// Extractor generates CUE definitions from Kubernetes CRDs using the OpenAPI v3 spec.
type Extractor struct {
	ctx    *cue.Context
	header string
}

// NewExtractor creates an Importer for the given CUE context.
func NewExtractor(ctx *cue.Context, header string) *Extractor {
	return &Extractor{
		ctx:    ctx,
		header: header,
	}
}

// Instances takes a multi-doc YAML containing Kubernetes CRDs and returns the CUE definitions
// generated from the OpenAPI spec. The resulting key value pairs, contain a unique identifier
// in the format `<group>/<kind>/<version>` and the contents of the CUE definition.
func (b *Extractor) Instances(crdData []byte) (map[string][]byte, error) {
	result := make(map[string][]byte)

	crds, err := b.fromYAML(crdData)
	if err != nil {
		return result, err
	}

	for _, crd := range crds {
		for _, crdVersion := range crd.Schemas {
			def, err := format.Node(crdVersion.Schema.Syntax(cue.All(), cue.Docs(true)))
			if err != nil {
				return result, err
			}
			name := path.Join(crd.Props.Spec.Group, crd.Props.Spec.Names.Singular, crdVersion.Version)
			result[name] = []byte(fmt.Sprintf("%s\n\npackage %s\n\n%s", b.header, crdVersion.Version, string(def)))
		}
	}

	return result, nil
}

// fromYAML converts a byte slice containing one or more YAML-encoded
// CustomResourceDefinitions into a slice of [IntermediateCRD].
//
// This function preserves the ordering of schemas declared in the input YAML in
// the resulting [IntermediateCRD.Schemas] field.
func (b *Extractor) fromYAML(data []byte) ([]*IntermediateCRD, error) {
	// The filename provided here is only used in error messages
	all, err := splitFile(b.ctx, "crd.yaml", data)
	if err != nil {
		return nil, err
	}

	ret := make([]*IntermediateCRD, 0, len(all))
	for _, crd := range all {
		cc, err := convertCRD(crd)
		if err != nil {
			return nil, err
		}
		ret = append(ret, cc)
	}

	return ret, nil
}

// IntermediateCRD is an intermediate representation of CRD YAML. It contains the original CRD YAML input,
// a subset of useful naming-related fields, and an extracted list of the version schemas in the CRD,
// having been converted from OpenAPI to CUE.
type IntermediateCRD struct {
	// The original unmodified CRD YAML, after conversion to a cue.Value.
	Original cue.Value

	// Object form of CRD, decoded by k8s decoder
	Props *apiextensions.CustomResourceDefinition

	// All the schemas in the original CRD, converted to CUE representation.
	Schemas []VersionedSchema
}

// VersionedSchema is an intermediate form of a single versioned schema from a CRD
// (an element in `spec.versions`), converted to CUE.
type VersionedSchema struct {
	// The contents of the `spec.versions[].name`
	Version string
	// The contents of `spec.versions[].schema.openAPIV3Schema`, after conversion of the OpenAPI
	// schema to native CUE constraints.
	Schema cue.Value
}

func convertCRD(crd cue.Value) (*IntermediateCRD, error) {
	cc := &IntermediateCRD{
		Schemas: make([]VersionedSchema, 0),
	}

	var err error
	cc.Props, err = parseCRD(crd)
	if err != nil {
		return nil, fmt.Errorf("error decoding crd props into Go struct: %w", err)
	}

	// shorthand
	kname := cc.Props.Spec.Names.Kind

	vlist := crd.LookupPath(cue.ParsePath("spec.versions"))
	if !vlist.Exists() {
		return nil, fmt.Errorf("crd versions list is absent")
	}

	ctx := crd.Context()
	shell := ctx.CompileString(fmt.Sprintf(`
		openapi: "3.0.0",
		info: {
			title: "dummy",
			version: "1.0.0",
		}
		components: schemas: %s: _
	`, kname))
	schpath := cue.ParsePath("components.schemas." + kname)
	defpath := cue.MakePath(cue.Def(kname))

	// The CUE stdlib openapi encoder expects a whole openapi document, and then
	// operates on schema elements defined within #/components/schema. Each
	// versions[].schema.openAPIV3Schema within a CRD is ~equivalent to a single
	// element under #/components/schema, as k8s does not allow CRD schemas to
	// contain any kind of external references.
	//
	// So, for each schema.openAPIV3Schema, we wrap it in an openapi document
	// structure, convert it to CUE, then appends it into the [IntermediateCRD.Schemas] slice.
	for i, crdVersion := range cc.Props.Spec.Versions {
		ver := crdVersion.Name
		if crdVersion.Schema == nil || crdVersion.Schema.OpenAPIV3Schema == nil {
			continue
		}

		rootosch := *crdVersion.Schema.OpenAPIV3Schema

		doc := shell.FillPath(schpath, crd.LookupPath(cue.ParsePath(fmt.Sprintf("spec.versions[%d].schema.openAPIV3Schema", i))))
		of, err := openapi.Extract(doc, &openapi.Config{})
		if err != nil {
			return nil, fmt.Errorf("could not convert schema for version %s to CUE: %w", ver, err)
		}

		// first, extract and get the schema handle itself
		extracted := ctx.BuildFile(of)

		// then unify with our desired base constraints
		nsConstraint := "!"
		if cc.Props.Spec.Scope != "Namespaced" {
			nsConstraint = "?"
		}
		sch := extracted.FillPath(defpath, ctx.CompileString(fmt.Sprintf(`
					import "strings"

					apiVersion: "%s/%s"
					kind: "%s"
		
					metadata!: {
						name!:        string & strings.MaxRunes(253) & strings.MinRunes(1)
						namespace%s:  string & strings.MaxRunes(63) & strings.MinRunes(1)
						labels?:      [string]: string
						annotations?: [string]: string
					}
				`, cc.Props.Spec.Group, ver, kname, nsConstraint)))

		// Add attributes for k8s oapi extensions
		// construct a map of all paths using x-kubernetes-* OpenAPI extensions
		sch = mapAttributes(sch, rootosch)

		// now, go back to an AST because it's easier to manipulate references there
		var schast *ast.File
		switch x := sch.Syntax(cue.All(), cue.Docs(true)).(type) {
		case *ast.File:
			schast = x
		case *ast.StructLit:
			schast, _ = astutil.ToFile(x)
		default:
			panic("unreachable")
		}

		// construct a map of all the paths that have x-kubernetes-embedded-resource: true defined
		preserve := make(map[string]bool)
		var walkfn func(path []cue.Selector, sch apiextensions.JSONSchemaProps) error
		walkfn = func(path []cue.Selector, sch apiextensions.JSONSchemaProps) error {
			if sch.XPreserveUnknownFields != nil {
				preserve[cue.MakePath(path...).String()] = *sch.XPreserveUnknownFields
			}
			for name, nextProp := range sch.Properties {
				if err := walkfn(append(path, cue.Str(name)), nextProp); err != nil {
					return err
				}
			}

			return nil
		}

		// Have to prepend with the defpath where the CUE CRD representation
		// lives because the astutil walker to remove ellipses operates over the
		// whole file, and therefore will be looking for full paths, extending
		// all the way to the file root
		err = walkfn(defpath.Selectors(), rootosch)
		if err != nil {
			return nil, err
		}

		// First pass of astutil.Apply to remove ellipses for fields not marked with x-kubernetes-embedded-resource: true
		// Note that this implementation is only correct for CUE inputs that do not contain references.
		// It is safe to use in this context because CRDs already have that invariant.
		var stack []ast.Node
		var pathstack []cue.Selector

		astutil.Apply(schast, func(c astutil.Cursor) bool {
			// Skip the root
			if c.Parent() == nil {
				return true
			}

			switch x := c.Node().(type) {
			case *ast.StructLit:
				psel, pc := parentPath(c)
				// Finding the parent-of-parent in this way is questionable.
				// pathTo will hop up the tree a potentially large number of
				// levels until it finds an *ast.Field or *ast.ListLit...but
				// who knows what's between here and there?
				_, ppc := parentPath(pc)
				var i int
				if ppc != nil {
					for i = len(stack); i > 0 && stack[i-1] != ppc.Node(); i-- {
					}
				}
				stack = append(stack[:i], pc.Node())
				pathstack = append(pathstack[:i], psel)
				pathKey := cue.MakePath(pathstack...).String()

				if !preserve[pathKey] {
					newlist := make([]ast.Decl, 0, len(x.Elts))
					for _, elt := range x.Elts {
						if _, is := elt.(*ast.Ellipsis); !is {
							newlist = append(newlist, elt)
						}
					}
					x.Elts = newlist
				}
			}
			return true
		}, nil)

		// walk over the AST and replace the spec and status fields with references to standalone defs
		var specf, statusf *ast.Field
		astutil.Apply(schast, func(cursor astutil.Cursor) bool {
			switch x := cursor.Node().(type) {
			case *ast.Field:
				if str, _, err := ast.LabelName(x.Label); err == nil {
					switch str {
					// Grab pointers to the spec and status fields, and replace with ref
					case "spec":
						specf = new(ast.Field)
						*specf = *x
						specref := &ast.Field{
							Label: ast.NewIdent("spec"),
							Value: ast.NewIdent("#" + kname + "Spec"),
						}
						specref.Constraint = token.NOT
						astutil.CopyComments(specref, x)
						cursor.Replace(specref)
						return false
					case "status":
						//TODO: decide if status should be included
						//statusf = new(ast.Field)
						//*statusf = *x
						cursor.Delete()
						return false
					case "metadata":
						// Avoid walking other known subtrees
						return false
					case "info":
						cursor.Delete()
					}
				}
			}
			return true
		}, nil)

		if specf != nil {
			specd := &ast.Field{
				Label: ast.NewIdent("#" + kname + "Spec"),
				Value: specf.Value,
			}
			astutil.CopyComments(specd, specf)
			schast.Decls = append(schast.Decls, specd)
		}

		if statusf != nil {
			statusd := &ast.Field{
				Label: ast.NewIdent("#" + kname + "Status"),
				Value: statusf.Value,
			}
			astutil.CopyComments(statusd, statusf)
			schast.Decls = append(schast.Decls, statusd)
		}

		// Then build back to a cue.Value again for the return
		cc.Schemas = append(cc.Schemas, VersionedSchema{
			Version: ver,
			Schema:  ctx.BuildFile(schast),
		})
	}

	return cc, nil
}

// parentPath walks up the AST via Cursor.Parent() to find the parent AST node
// that is expected to be the anchor of a path element.
//
// Returns the cue.Selector that should navigate to the provided cursor's
// corresponding cue.Value, and the cursor of that parent element.
//
// Returns nil, nil if no such parent node can be found.
//
// Node types considered candidates for path anchors:
//   - *ast.ListLit (index is the path)
//   - *ast.Field (label is the path)
//
// If the there exceptions for the above two items, or the list should properly
// have more items, this func will be buggy
func parentPath(c astutil.Cursor) (cue.Selector, astutil.Cursor) {
	p, prior := c.Parent(), c
	for p != nil {
		switch x := p.Node().(type) {
		case *ast.Field:
			lab, _, _ := ast.LabelName(x.Label)
			if strings.HasPrefix(lab, "#") {
				return cue.Def(lab), p
			}
			return cue.Str(lab), p
		case *ast.ListLit:
			for i, v := range x.Elts {
				if prior.Node() == v {
					return cue.Index(i), p
				}
			}
		}
		prior = p
		p = p.Parent()
	}

	return cue.Selector{}, nil
}

type XExtensionAttr string

const (
	XPreserveUnknownFields XExtensionAttr = "preserveUnknownFields"
	XEmbeddedResource      XExtensionAttr = "embeddedResource"
	XIntOrString           XExtensionAttr = "intOrString"
	XListMapKeys           XExtensionAttr = "listMapKeys"
	XListType              XExtensionAttr = "listType"
	XMapType               XExtensionAttr = "mapType"
	XValidations           XExtensionAttr = "validations"
)

// Preserves Kubernetes OpenAPI extensions in an attribute for each field utilizing them
func mapAttributes(val cue.Value, prop apiextensions.JSONSchemaProps) cue.Value {
	attr := xk8sattr(*val.Context(), prop)
	if attr != nil {
		_, p := val.ReferencePath()
		fmt.Println(p.String() + ": " + attr.Text)
		node := val.Source()
		switch x := node.(type) {
		case *ast.StructLit:
			x.Elts = append(x.Elts, attr)
		case *ast.Field:
			x.Attrs = append(x.Attrs, attr)
		case *ast.File:
			x.Decls = append(x.Decls, attr)
		default:
			// fmt.Println(attrPath)
			// fmt.Printf("extendedVal: %v\n\n", extendedVal)
			// fmt.Println(reflect.TypeOf(node))
			// fmt.Printf("node: %v\n", node)
		}
	}

	for name := range prop.Properties {
		// Recursively add subextensions for each property
		nextPath := cue.MakePath(cue.Str(name))
		nextVal := mapAttributes(val.LookupPath(nextPath), prop.Properties[name])
		val = val.FillPath(nextPath, nextVal)
	}

	// TODO: array does not work right, see https://github.com/istio/istio/blob/0d5f530188dfe571bf0d8f515618ba99a0dc3e6c/manifests/charts/base/crds/crd-all.gen.yaml#L188
	if prop.Type == "array" {
		if len(prop.Items.JSONSchemas) > 0 {
			iter, err := val.List()
			if err != nil {
				fmt.Println(err)
			}
			// Iterate each schema in list and add attribute for that list index
			for i := range prop.Items.JSONSchemas {
				// Add attribute to the item at index i
				if !iter.Next() {
					break
				}
				nextVal := iter.Value()
				val = val.FillPath(nextVal.Path(), mapAttributes(nextVal, prop.Items.JSONSchemas[i]))
			}
		} else {
			// if val.Allows(cue.AnyIndex) {
			anyIndex := cue.MakePath(cue.AnyIndex)
			val.LookupPath(cue.MakePath(cue.AnyIndex))
			nextVal := val.LookupPath(anyIndex)
			fmt.Println(nextVal)
			nextVal = mapAttributes(nextVal, *prop.Items.Schema)
			val = val.FillPath(anyIndex, nextVal)
			// } else {
			// 	fmt.Println("here")
			// }

			// Add attribute to the pattern constraint
			// // Recursively add subextensions for each property
			// subExts := xKubernetesAttributes(append(path, cue.AnyIndex), *prop.Items.Schema)
			// extensions = append(extensions, subExts...)
		}
	}

	return val
}

// TODO: use internal.Attr if it can support writing attributes
type attr struct {
	name   string
	fields []keyval
}

func (a attr) String() string {
	fields := []string{}
	for _, f := range a.fields {
		fields = append(fields, f.String())
	}
	return fmt.Sprintf("@%s(%s)", a.name, strings.Join(fields, ", "))
}

type keyval struct {
	key string
	val string
}

func (kv keyval) String() string {
	if kv.val != "" {
		return kv.key + "=" + kv.val
	}
	return kv.key
}

func xk8sattr(ctx cue.Context, prop apiextensions.JSONSchemaProps) *ast.Attribute {
	a := attr{
		name:   "crd",
		fields: []keyval{},
	}

	attrBody := make([]string, 0)
	appendField := func(key XExtensionAttr, val string) {
		a.fields = append(a.fields, keyval{key: string(key), val: val})
		attrBody = append(attrBody, fmt.Sprintf("%s=%s", key, val))
	}

	if prop.XPreserveUnknownFields != nil {
		appendField(XPreserveUnknownFields, fmt.Sprintf("%t", *prop.XPreserveUnknownFields))
	}

	if prop.XEmbeddedResource {
		appendField(XEmbeddedResource, fmt.Sprintf("%t", prop.XEmbeddedResource))
	}

	if prop.XIntOrString {
		appendField(XIntOrString, fmt.Sprintf("%t", prop.XIntOrString))
	}

	if len(prop.XListMapKeys) > 0 {
		appendField(XListMapKeys, fmt.Sprint(ctx.Encode(prop.XListMapKeys)))
	}

	if prop.XListType != nil {
		appendField(XListType, fmt.Sprintf("%q", *prop.XListType))
	}

	if prop.XMapType != nil {
		appendField(XMapType, fmt.Sprintf("%q", *prop.XMapType))
	}

	if len(prop.XValidations) > 0 {
		appendField(XValidations, fmt.Sprint(ctx.Encode(prop.XValidations)))
	}

	if len(a.fields) > 0 {
		return &ast.Attribute{Text: a.String()}
	}

	return nil
}
