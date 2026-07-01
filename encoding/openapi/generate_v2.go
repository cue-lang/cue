// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package openapi

import (
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/internal/core/runtime"
)

// Version defines the version of an OpenAPI document.
type Version int

const (
	VersionUnknown Version = iota
	Version3_0
	Version3_1
	VersionKubernetesCRD
	// TODO VersionKubernetesAPI ?
)

// jsonschemaVersion returns the JSON Schema dialect used for schemas within a
// document of this version.
func (v Version) jsonschemaVersion() (jsonschema.Version, error) {
	switch v {
	case VersionUnknown, Version3_0:
		return jsonschema.VersionOpenAPI, nil
	case Version3_1:
		return jsonschema.VersionDraft2020_12, nil
	case VersionKubernetesCRD:
		// TODO the JSON Schema generator has no dedicated structural-schema
		// (CRD) dialect yet, so fall back to the OpenAPI 3.0 dialect. This is
		// close to structural schema but does not emit CRD-specific keywords
		// such as x-kubernetes-*. See the structural schema notes in crd.go.
		return jsonschema.VersionOpenAPI, nil
	}
	return jsonschema.VersionUnknown, fmt.Errorf("unsupported OpenAPI version %d", int(v))
}

// sharedSchemaRoot is the JSON Pointer of the location within an OpenAPI
// document that holds the shared, referenceable schemas.
const sharedSchemaRoot = "#/components/schemas"

//go:embed openapimeta.cue
var openapiMetaCUE []byte

// openapiMetaFile compiles the embedded OpenAPI meta-schema once on first use.
// Values from different contexts can be combined, so the cached value can be
// unified with a caller's document regardless of the caller's context.
var openapiMetaFile = sync.OnceValue(func() cue.Value {
	ctx := (*cue.Context)(runtime.New())
	v := ctx.CompileBytes(openapiMetaCUE)
	if err := v.Err(); err != nil {
		panic(fmt.Errorf("cannot compile OpenAPI meta-schema: %v", err))
	}
	return v
})

// metaSchema returns the OpenAPI document meta-schema for the given version.
// The document structure differs between versions, so each supported version
// has its own #Document_* definition in openapimeta.cue.
func metaSchema(v Version) (cue.Value, error) {
	var def string
	switch v {
	case VersionUnknown, Version3_0:
		def = "#Document_3_0"
	case Version3_1:
		def = "#Document_3_1"
	case VersionKubernetesCRD:
		// A CRD is not itself an OpenAPI document; until full CRD document
		// generation is modeled, treat it structurally as 3.0.
		def = "#Document_3_0"
	default:
		return cue.Value{}, fmt.Errorf("no OpenAPI meta-schema for version %d", int(v))
	}
	m := openapiMetaFile().LookupPath(cue.MakePath(cue.Def(def)))
	if !m.Exists() {
		return cue.Value{}, fmt.Errorf("internal error: OpenAPI meta-schema %s not found", def)
	}
	return m, nil
}

// schemaMarker is the path of the hidden field with which the meta-schema marks
// every Schema Object position. schemaDef is the path of the # definition
// that a caller places at such a position to have a CUE schema converted.
var (
	schemaMarker = cue.MakePath(cue.Hid("_openapiSchema", "_"))
	schemaDef    = cue.MakePath(cue.Def("#"))
)

// GenerateConfig defines the configuration for [GenerateV2].
type GenerateConfig struct {
	// PkgName defines the package name for a generated CUE package.
	PkgName string

	// NamesFunc determines how to name the shared schemas generated within
	// components.schemas. It is passed all the distinct references made by the
	// schemas in the document and, when AllSchemas is set, a reference to each
	// top-level schema; it must set a distinct [jsonschema.CUERef.Name] for
	// each. If nil, a default naming scheme is used.
	NamesFunc func(refs []*jsonschema.CUERef)

	// DescriptionFunc returns the description for the given field. A typical
	// implementation compiles the description from the comments obtained from
	// the Doc method. No description field is added if the empty string is
	// returned. If this is nil, descriptions are derived from doc comments.
	DescriptionFunc func(v cue.Value) string

	// AllSchemas causes [GenerateV2] to generate an entry in components.schemas
	// for every top-level schema (definition) found in the value. When this is
	// false, schemas are only generated when referred to from within a #
	// field in a schema position in the OpenAPI document.
	AllSchemas bool

	// Version is the OpenAPI version to use. By default [Version3_0] is used.
	Version Version
}

// GenerateV2 generates an OpenAPI document from the given value.
//
// The value should conform to an OpenAPI document structure, as documented at
// https://spec.openapis.org/oas/, except that in positions where a schema is
// expected the generation code looks for a # definition and generates the
// JSON Schema from that. If there is no # definition, it uses any JSON
// Schema value it finds there verbatim.
//
// Schemas referenced from more than one position are emitted once, under
// components.schemas, and referenced with a $ref.
//
// THIS IS EXPERIMENTAL. API MIGHT CHANGE.
func GenerateV2(v cue.Value, cfg *GenerateConfig) (ast.Expr, error) {
	if cfg == nil {
		cfg = &GenerateConfig{}
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	jsVersion, err := cfg.Version.jsonschemaVersion()
	if err != nil {
		return nil, err
	}
	meta, err := metaSchema(cfg.Version)
	if err != nil {
		return nil, err
	}

	// Unify with the meta-schema so that Schema Object positions are marked
	// (and any meta-schema defaults are applied).
	doc := v.Unify(meta)
	if err := doc.Err(); err != nil {
		return nil, fmt.Errorf("value does not conform to the OpenAPI document structure: %v", err)
	}

	g := &genV2{cfg: cfg}

	// Pass 1: collect every # value to convert.
	if err := g.collect(doc); err != nil {
		return nil, err
	}
	// With AllSchemas, also convert every top-level definition so it appears in
	// components.schemas even when unreferenced.
	nPositions := len(g.schemaValues)
	var defPaths []cue.Path
	if cfg.AllSchemas {
		paths, vals := topLevelSchemas(doc)
		defPaths = paths
		g.schemaValues = append(g.schemaValues, vals...)
	}

	// Wrap the naming function so that, with AllSchemas, the top-level
	// schemas are named in the same NamesFunc invocation as the references
	// collected by the generator. A referenced top-level schema arrives as
	// one of those references, recognized by its root and path; a synthetic
	// reference is added for each unreferenced one. This makes the names
	// consistent whatever the NamesFunc, and guarantees they cannot clash.
	namesFunc := cfg.NamesFunc
	if namesFunc == nil {
		namesFunc = jsonschema.DefaultNamesFunc
	}
	// referencedNames and unreferencedNames record the name assigned to each
	// top-level schema, keyed by its path within doc, according to whether
	// the schema is referenced from the document (and hence already present
	// in the shared definitions) or not.
	referencedNames := make(map[string]string)
	unreferencedNames := make(map[string]string)
	namesFuncCalled := false
	wrappedNamesFunc := func(refs []*jsonschema.CUERef) {
		namesFuncCalled = true
		isRef := make(map[string]bool, len(refs))
		for _, r := range refs {
			if r.Inst == doc {
				isRef[r.Path.String()] = true
			}
		}
		var synthetic []*jsonschema.CUERef
		for _, p := range defPaths {
			if !isRef[p.String()] {
				synthetic = append(synthetic, &jsonschema.CUERef{Inst: doc, Path: p})
			}
		}
		namesFunc(append(refs, synthetic...))
		for _, r := range refs {
			if r.Inst == doc {
				referencedNames[r.Path.String()] = r.Name
			}
		}
		for _, r := range synthetic {
			unreferencedNames[r.Path.String()] = r.Name
		}
	}

	// Convert all schemas in a single call so shared definitions deduplicate
	// into components.schemas.
	exprs, shared, err := jsonschema.GenerateMany(g.schemaValues, sharedSchemaRoot, &jsonschema.GenerateConfig{
		Version:         jsVersion,
		NamesFunc:       wrappedNamesFunc,
		DescriptionFunc: cfg.DescriptionFunc,
	})
	if err != nil {
		return nil, err
	}
	g.generated = make(map[string]ast.Expr, nPositions)
	for i := range g.schemaPaths {
		g.generated[g.schemaPaths[i]] = exprs[i]
	}
	// GenerateMany invokes the naming function only when the schemas contain
	// references; make sure the top-level schemas are always named.
	if !namesFuncCalled && len(defPaths) > 0 {
		wrappedNamesFunc(nil)
	}
	// Fold the unreferenced AllSchemas top-level schemas into the shared set;
	// the referenced ones are already there under their assigned names.
	for i, p := range defPaths {
		ps := p.String()
		if _, ok := referencedNames[ps]; ok {
			continue
		}
		name := unreferencedNames[ps]
		if name == "" {
			return nil, fmt.Errorf("NamesFunc did not set a name for %v", p)
		}
		if _, ok := shared[name]; ok {
			return nil, fmt.Errorf("NamesFunc returned non-unique name %q", name)
		}
		shared[name] = exprs[nPositions+i]
	}

	// Pass 2: rebuild the document, substituting the generated schemas.
	out, err := g.build(doc)
	if err != nil {
		return nil, err
	}
	st, ok := out.(*ast.StructLit)
	if !ok {
		return nil, fmt.Errorf("OpenAPI document must be a struct, found %T", out)
	}
	if err := mergeComponentsSchemas(st, shared); err != nil {
		return nil, err
	}
	return st, nil
}

// genV2 holds the state accumulated while generating one OpenAPI document.
type genV2 struct {
	cfg *GenerateConfig

	// schemaValues holds the schema values to convert, in collection order:
	// first the values found at document positions, then (for AllSchemas) the
	// top-level definitions.
	schemaValues []cue.Value

	// schemaPaths[i] is the document path of the position holding
	// schemaValues[i], for i < number of positions.
	schemaPaths []string

	// generated maps a position's path to its generated schema, filled after
	// conversion.
	generated map[string]ast.Expr
}

// collect walks the document collecting the # value at each Schema Object
// position. Positions that hold a raw JSON Schema value (no #) need no
// collection: they are emitted structurally by build.
func (g *genV2) collect(v cue.Value) error {
	if v.LookupPath(schemaMarker).Exists() {
		if sch := v.LookupPath(schemaDef); sch.Exists() {
			g.schemaValues = append(g.schemaValues, sch)
			g.schemaPaths = append(g.schemaPaths, v.Path().String())
		}
		// Do not descend into a schema.
		return nil
	}
	switch v.IncompleteKind() {
	case cue.StructKind:
		it, err := v.Fields()
		if err != nil {
			return err
		}
		for it.Next() {
			if err := g.collect(it.Value()); err != nil {
				return err
			}
		}
	case cue.ListKind:
		it, err := v.List()
		if err != nil {
			return err
		}
		for it.Next() {
			if err := g.collect(it.Value()); err != nil {
				return err
			}
		}
	}
	return nil
}

// build rebuilds the document as an AST, substituting the generated schema at
// each # position and emitting everything else verbatim. A Schema Object
// position without a # (a raw JSON Schema value) is emitted structurally,
// which drops the hidden meta-schema marker.
func (g *genV2) build(v cue.Value) (ast.Expr, error) {
	if e, ok := g.generated[v.Path().String()]; ok {
		return e, nil
	}
	switch v.IncompleteKind() {
	case cue.StructKind:
		it, err := v.Fields()
		if err != nil {
			return nil, err
		}
		var elts []ast.Decl
		for it.Next() {
			fv, err := g.build(it.Value())
			if err != nil {
				return nil, err
			}
			elts = append(elts, &ast.Field{
				Label: ast.NewStringLabel(it.Selector().Unquoted()),
				Value: fv,
			})
		}
		return &ast.StructLit{Elts: elts}, nil
	case cue.ListKind:
		it, err := v.List()
		if err != nil {
			return nil, err
		}
		var elts []ast.Expr
		for it.Next() {
			ev, err := g.build(it.Value())
			if err != nil {
				return nil, err
			}
			elts = append(elts, ev)
		}
		return &ast.ListLit{Elts: elts}, nil
	default:
		s := v.Syntax(cue.Final())
		e, ok := s.(ast.Expr)
		if !ok {
			return nil, fmt.Errorf("cannot represent value at %v as an expression", v.Path())
		}
		return e, nil
	}
}

// topLevelSchemas returns the top-level definitions of the document as schemas
// to be generated, along with the path of each within the document.
func topLevelSchemas(doc cue.Value) (paths []cue.Path, values []cue.Value) {
	it, err := doc.Fields(cue.Definitions(true))
	if err != nil {
		return nil, nil
	}
	for it.Next() {
		sel := it.Selector()
		if sel.LabelType() != cue.DefinitionLabel {
			continue
		}
		paths = append(paths, cue.MakePath(sel))
		values = append(values, it.Value())
	}
	return paths, values
}

// mergeComponentsSchemas adds the shared schemas to the document's
// components.schemas struct, creating the components and schemas fields if
// necessary. It reports an error on a name collision with an existing entry.
func mergeComponentsSchemas(doc *ast.StructLit, shared map[string]ast.Expr) error {
	if len(shared) == 0 {
		return nil
	}
	components := findOrAddStruct(doc, "components")
	schemas := findOrAddStruct(components, "schemas")
	for _, name := range slices.Sorted(maps.Keys(shared)) {
		if hasField(schemas, name) {
			return fmt.Errorf("generated schema %q conflicts with an existing components.schemas entry", name)
		}
		schemas.Elts = append(schemas.Elts, &ast.Field{
			Label: ast.NewStringLabel(name),
			Value: shared[name],
		})
	}
	return nil
}

// findOrAddStruct returns the struct literal value of the named field of st,
// adding an empty one if the field is absent.
func findOrAddStruct(st *ast.StructLit, name string) *ast.StructLit {
	for _, d := range st.Elts {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		if labelName(f.Label) != name {
			continue
		}
		if inner, ok := f.Value.(*ast.StructLit); ok {
			return inner
		}
	}
	inner := &ast.StructLit{}
	st.Elts = append(st.Elts, &ast.Field{
		Label: ast.NewStringLabel(name),
		Value: inner,
	})
	return inner
}

// hasField reports whether st has a field with the given name.
func hasField(st *ast.StructLit, name string) bool {
	for _, d := range st.Elts {
		if f, ok := d.(*ast.Field); ok && labelName(f.Label) == name {
			return true
		}
	}
	return false
}

// labelName returns the field name for a label that is an identifier or basic
// string literal, or "" otherwise.
func labelName(l ast.Label) string {
	switch x := l.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.BasicLit:
		s, err := literal.Unquote(x.Value)
		if err != nil {
			return ""
		}
		return s
	}
	return ""
}
