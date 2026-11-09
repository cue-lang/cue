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
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/internal/core/runtime"
	internalvalue "cuelang.org/go/internal/value"
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
	case Version3_0:
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

// bareSchemas checks whether v holds bare schemas, as [Generate] expects:
// regular fields other than info, $version, and "-" are not allowed.
// Otherwise other is the first such field and otherLabel its [openapiLabel].
func bareSchemas(v cue.Value) (info, other cue.Value, otherLabel string, err error) {
	it, err := v.Fields()
	if err != nil {
		return cue.Value{}, cue.Value{}, "", err
	}
	for it.Next() {
		switch label := openapiLabel(it.Selector(), it.Value()); label {
		case "info":
			info = it.Value()
		case "$version", "-":
		default:
			return cue.Value{}, it.Value(), label, nil
		}
	}
	if info.Exists() && info.IncompleteKind() != cue.StructKind {
		return cue.Value{}, cue.Value{}, "", errors.Newf(info.Pos(), "info must be a struct")
	}
	return info, cue.Value{}, "", nil
}

// bareSchemasDocument returns the document for the bare schemas v,
// with empty components.schemas.
func (g *genV2) bareSchemasDocument(v, info cue.Value, version Version) (*ast.StructLit, error) {
	var infoLit *ast.StructLit
	if info.Exists() {
		e, err := g.build(info)
		if err != nil {
			return nil, err
		}
		infoLit = e.(*ast.StructLit) // bareSchemas checked that info is a struct
	}
	infoLit = setInfoTitleVersion(infoLit, v, info)
	openapi, err := version.openapiField()
	if err != nil {
		return nil, err
	}
	return documentStruct(openapi, infoLit, ast.NewStruct()), nil
}

// openapiPath is the path of the top-level field declaring a document's
// OpenAPI version.
var openapiPath = cue.MakePath(cue.Str("openapi"))

// openapiVersions maps each supported version to the prefix of its openapi
// field value; the specification says to disregard the patch version.
var openapiVersions = []struct {
	version Version
	prefix  string
}{
	{Version3_0, "3.0."},
	{Version3_1, "3.1."},
}

// openapiField returns the openapi field value for version v.
func (v Version) openapiField() (string, error) {
	for _, ov := range openapiVersions {
		if ov.version == v {
			return ov.prefix + "0", nil
		}
	}
	return "", fmt.Errorf("no openapi field value for version %d", int(v))
}

// detectVersion parses a document's openapi field.
func detectVersion(openapi cue.Value) (Version, error) {
	s, err := openapi.String()
	if err != nil {
		return VersionUnknown, fmt.Errorf("invalid openapi field (must be string): %v", err)
	}
	for _, ov := range openapiVersions {
		if strings.HasPrefix(s, ov.prefix) {
			return ov.version, nil
		}
	}
	return VersionUnknown, fmt.Errorf("unknown OpenAPI version %q", s)
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
	case Version3_0:
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
	// NamesFunc determines how to name the shared schemas generated within
	// components.schemas. It is passed all the distinct references made by the
	// schemas in the document and, with AllSchemas or bare schemas, a
	// reference to each top-level schema; it must set a distinct
	// [jsonschema.CUERef.Name] for each. If nil, a default naming scheme is
	// used.
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

	// Version is the OpenAPI version to use. When [VersionUnknown] (the
	// default), it is auto-detected from the document's "openapi" field,
	// or 3.0 for bare schemas.
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
// A value whose only regular fields are info and $version, besides ignored
// "-" fields, holds bare schema definitions as [Generate] expects, with its
// @openapi(name) attributes. The result is then a document like [Generate]'s,
// with every definition under components.schemas as if cfg.AllSchemas were
// set. Bare schemas are not recognized with [VersionKubernetesCRD].
//
// THIS IS EXPERIMENTAL. API MIGHT CHANGE.
func GenerateV2(v cue.Value, cfg *GenerateConfig) (ast.Expr, error) {
	if cfg == nil {
		cfg = &GenerateConfig{}
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if k := v.IncompleteKind(); k != cue.StructKind {
		return nil, errors.Newf(v.Pos(), "an OpenAPI document must be a struct, found %v", k)
	}
	version := cfg.Version
	var bare bool
	var info, other cue.Value
	var otherLabel string
	if version != VersionKubernetesCRD {
		var err error
		if info, other, otherLabel, err = bareSchemas(v); err != nil {
			return nil, err
		}
		bare = !other.Exists()
	}
	openapi := v.LookupPath(openapiPath)
	switch {
	case version != VersionUnknown:
	case bare:
		version = Version3_0
	case !openapi.Exists():
		return nil, errors.Newf(other.Pos(), "top-level field %q requires an openapi field; without one, only info and $version can be declared beside schema definitions", otherLabel)
	default:
		var err error
		version, err = detectVersion(openapi)
		if err != nil {
			return nil, err
		}
	}
	jsVersion, err := version.jsonschemaVersion()
	if err != nil {
		return nil, err
	}
	g := &genV2{cfg: cfg}

	// Bare schemas are not a document, so they are not unified with the
	// meta-schema; only their definitions and info field are read.
	doc := v
	if !bare {
		meta, err := metaSchema(version)
		if err != nil {
			return nil, err
		}

		// Unify with the meta-schema so that Schema Object positions are
		// marked (and any meta-schema defaults are applied).
		doc = v.Unify(meta)
		if err := doc.Err(); err != nil {
			return nil, fmt.Errorf("value does not conform to the OpenAPI document structure: %v", err)
		}

		// Pass 1: collect every # value to convert.
		if err := g.collect(doc); err != nil {
			return nil, err
		}
	}
	// With AllSchemas or bare schemas, also convert every top-level definition
	// so it appears in components.schemas even when unreferenced.
	nPositions := len(g.schemaValues)
	var defPaths []cue.Path
	if cfg.AllSchemas || bare {
		paths, vals := topLevelSchemas(doc)
		defPaths = paths
		g.schemaValues = append(g.schemaValues, vals...)
	}

	// With AllSchemas or bare schemas, wrap the naming function so that the
	// top-level schemas are named in the same NamesFunc call as the references
	// the generator collected: a referenced one arrives as such a reference,
	// recognized by its root and path, and a synthetic reference is added for
	// each unreferenced one. referencedNames and unreferencedNames record the
	// names assigned, keyed by path from root.
	namesFunc := cfg.NamesFunc
	referencedNames := make(map[string]string)
	unreferencedNames := make(map[string]string)
	namesFuncCalled := false
	if len(defPaths) > 0 {
		inner := namesFunc
		if inner == nil {
			inner = jsonschema.DefaultNamesFunc
		}
		root := rootValue(doc)
		rootVertex := internalvalue.Vertex(root)
		namesFunc = func(refs []*jsonschema.CUERef) {
			namesFuncCalled = true
			isRef := make(map[string]bool, len(refs))
			for _, r := range refs {
				if internalvalue.Vertex(r.Inst) == rootVertex {
					isRef[r.Path.String()] = true
				}
			}
			var synthetic []*jsonschema.CUERef
			for _, p := range defPaths {
				if !isRef[p.String()] {
					synthetic = append(synthetic, &jsonschema.CUERef{Inst: root, Path: p})
				}
			}
			inner(append(refs, synthetic...))
			for _, r := range refs {
				if internalvalue.Vertex(r.Inst) == rootVertex {
					referencedNames[r.Path.String()] = r.Name
				}
			}
			for _, r := range synthetic {
				unreferencedNames[r.Path.String()] = r.Name
			}
		}
	}

	// Convert all schemas in a single call so shared definitions deduplicate
	// into components.schemas.
	exprs, shared, err := jsonschema.GenerateMany(g.schemaValues, sharedSchemaRoot, &jsonschema.GenerateConfig{
		Version:         jsVersion,
		NamesFunc:       namesFunc,
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
		namesFunc(nil)
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
	var st *ast.StructLit
	if bare {
		if st, err = g.bareSchemasDocument(v, info, version); err != nil {
			return nil, err
		}
	} else {
		out, err := g.build(doc)
		if err != nil {
			return nil, err
		}
		var ok bool
		if st, ok = out.(*ast.StructLit); !ok {
			return nil, fmt.Errorf("OpenAPI document must be a struct, found %T", out)
		}
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
	// first the values found at document positions, then (for AllSchemas or
	// bare schemas) the top-level definitions.
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
			// A "#" schema and sibling data fields at the same Schema Object
			// position are ambiguous: the "#" would be converted while the data
			// is silently dropped. Reject the combination.
			it, err := v.Fields()
			if err != nil {
				return err
			}
			if it.Next() {
				return fmt.Errorf("schema position %v has both a # definition and a data field %q; a # schema cannot be combined with data", v.Path(), it.Selector().Unquoted())
			}
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
// to be generated, along with the path of each from its root value.
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
		paths = append(paths, it.Value().Path())
		values = append(values, it.Value())
	}
	return paths, values
}

// rootValue returns the root of the value v is in, which is not v when it is
// nested within another value, as [cue.Value.ReferencePath] gives it.
func rootValue(v cue.Value) cue.Value {
	x := internalvalue.Vertex(v)
	for x.Parent != nil {
		x = x.Parent
	}
	return internalvalue.Context(v).Encode(x)
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
