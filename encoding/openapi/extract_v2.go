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
	"fmt"
	"iter"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
)

// ExtractConfig defines the configuration for [ExtractV2].
type ExtractConfig struct {
	// PkgName, if non-empty, adds a package clause to the extracted file.
	PkgName string

	// Version is the OpenAPI version of the document. When [VersionUnknown]
	// (the default), it is auto-detected from the document's "openapi" field.
	Version Version

	// StrictFeatures reports an error for JSON Schema features that are known
	// to be unsupported when extracting schemas.
	StrictFeatures bool

	// StrictKeywords reports an error when unknown JSON Schema keywords are
	// encountered when extracting schemas. For OpenAPI 3.0 this is implicitly
	// always true, as that specification prohibits unknown keywords other than
	// "x-" prefixed ones.
	StrictKeywords bool
}

// ExtractV2 converts a whole OpenAPI document to an equivalent CUE
// representation. It is the inverse of [GenerateV2]: the result is shaped so
// that passing it back through [GenerateV2] reproduces an equivalent document.
//
// Each entry under components.schemas becomes a top-level # definition (or,
// when its name is not a valid identifier, a quoted field of the anonymous #
// definition). At each position where an OpenAPI Schema Object is expected,
// the schema is extracted into CUE and placed under a # field; a Reference
// Object ({$ref: "#/components/schemas/Foo"}) becomes a reference to the
// corresponding definition. All other document content is preserved verbatim.
//
// The result can be converted to a [cue.Value] via [cue.Context.BuildFile].
//
// Note: this functionality is currently experimental. The form of the extracted
// representation may, and probably will, change from release to release.
func ExtractV2(data cue.Value, cfg *ExtractConfig) (*ast.File, error) {
	if cfg == nil {
		cfg = &ExtractConfig{}
	}
	if err := data.Err(); err != nil {
		return nil, err
	}
	version := cfg.Version
	if version == VersionUnknown {
		var err error
		version, err = detectVersion(data)
		if err != nil {
			return nil, err
		}
	}
	jsVersion, err := version.jsonschemaVersion()
	if err != nil {
		return nil, err
	}
	meta, err := metaSchema(version)
	if err != nil {
		return nil, err
	}

	// Unify with the meta-schema so that Schema Object positions are marked.
	doc := data.Unify(meta)
	if err := doc.Err(); err != nil {
		return nil, fmt.Errorf("value does not conform to the OpenAPI document structure: %v", err)
	}

	// Collect every schema position (components.schemas entries and every
	// inline/reference position within the document) and extract them all in a
	// single call so that references between them resolve to shared definitions
	// rather than being duplicated.
	var roots []string
	for p := range schemaPaths(doc) {
		ptr, err := json.PointerFromCUEPath(p)
		if err != nil {
			return nil, fmt.Errorf("cannot compute JSON Pointer for %v: %v", p, err)
		}
		roots = append(roots, "#"+string(ptr))
	}

	// The extraction is given the whole document as its base, with a
	// placeholder at each schema position; it places each extracted schema at
	// the # field of its original position (see mapRefV2), with references
	// between schemas resolving within the same file.
	base, err := buildBase(doc)
	if err != nil {
		return nil, err
	}
	f, err := jsonschema.Extract(data, &jsonschema.Config{
		PkgName:              cfg.PkgName,
		Roots:                roots,
		MapRef:               mapRefV2,
		Base:                 base,
		DefaultVersion:       jsVersion,
		StrictFeatures:       cfg.StrictFeatures,
		StrictKeywords:       jsVersion == jsonschema.VersionOpenAPI || cfg.StrictKeywords,
		AllowNonExistentRoot: true,
	})
	if err != nil {
		return nil, err
	}
	arrangeDecls(f)
	return f, nil
}

// arrangeDecls reorders the top-level declarations of the extracted file into
// the conventional document order: the openapi and info fields first, then
// the definitions, then the remaining document fields.
func arrangeDecls(f *ast.File) {
	preamble := f.Preamble()
	var head, defs, tail []ast.Decl
	for _, d := range f.Decls[len(preamble):] {
		fld, ok := d.(*ast.Field)
		if !ok {
			defs = append(defs, d)
			continue
		}
		switch name := labelName(baseLabel(fld.Label)); {
		case name == "openapi", name == "info":
			head = append(head, d)
		case name == "" || strings.HasPrefix(name, "#"):
			defs = append(defs, d)
		default:
			tail = append(tail, d)
		}
	}
	for i, d := range head {
		if i > 0 {
			ast.SetRelPos(d, token.Newline)
		} else {
			ast.SetRelPos(d, token.NewSection)
		}
	}
	// The definitions keep the new-section positioning they were built with,
	// putting a blank line between each.
	for i, d := range tail {
		if i > 0 {
			ast.SetRelPos(d, token.Newline)
		} else {
			ast.SetRelPos(d, token.NewSection)
		}
	}
	decls := f.Decls[:len(preamble):len(preamble)]
	decls = append(decls, head...)
	decls = append(decls, defs...)
	decls = append(decls, tail...)
	f.Decls = decls
}

// baseLabel returns the label underneath any label alias.
func baseLabel(l ast.Label) ast.Label {
	if a, ok := l.(*ast.Alias); ok {
		if lbl, ok := a.Expr.(ast.Label); ok {
			return lbl
		}
	}
	return l
}

// schemaPaths produces the path of every Schema Object within v.
func schemaPaths(v cue.Value) iter.Seq[cue.Path] {
	return func(yield func(cue.Path) bool) {
		walkSchemaPaths(v, yield)
	}
}

func walkSchemaPaths(v cue.Value, yield func(cue.Path) bool) bool {
	if v.LookupPath(schemaMarker).Exists() {
		return yield(v.Path())
	}
	switch v.IncompleteKind() {
	case cue.StructKind:
		it, err := v.Fields()
		if err != nil {
			return true
		}
		for it.Next() {
			if !walkSchemaPaths(it.Value(), yield) {
				return false
			}
		}
	case cue.ListKind:
		it, err := v.List()
		if err != nil {
			return true
		}
		for it.Next() {
			if !walkSchemaPaths(it.Value(), yield) {
				return false
			}
		}
	}
	return true
}

// mapRefV2 maps the location of a schema within an OpenAPI document to its
// CUE location. Entries under components.schemas become top-level definitions
// (#Name, or #."some-name" under the anonymous # definition when the name is
// not a valid identifier). Any other schema position maps to the # field at
// its own position within the document, which [ExtractV2] passes to the
// extraction as its base.
func mapRefV2(loc jsonschema.SchemaLoc) (string, cue.Path, error) {
	if !loc.IsLocal {
		return "", cue.Path{}, fmt.Errorf("external schema reference %v is not supported", loc.ID)
	}
	sels := loc.Path.Selectors()
	if len(sels) == 3 && sels[0] == cue.Str("components") && sels[1] == cue.Str("schemas") {
		name := sels[2].Unquoted()
		if !ast.StringLabelNeedsQuoting(name) {
			return "", cue.MakePath(cue.Def(name)), nil
		}
		return "", cue.MakePath(cue.Def("#"), cue.Str(name)), nil
	}
	sels = append(sels[:len(sels):len(sels)], cue.Def("#"))
	return "", cue.MakePath(sels...), nil
}

// buildBase renders the document as the base for the schema extraction: all
// non-schema content is emitted verbatim, each Schema Object position is
// marked with a _ placeholder for the extraction to fill, and the
// components.schemas subtree is omitted, as its entries become top-level
// definitions instead.
func buildBase(doc cue.Value) (*ast.File, error) {
	expr, _, err := buildBaseExpr(doc)
	if err != nil {
		return nil, err
	}
	st, ok := expr.(*ast.StructLit)
	if !ok {
		return nil, fmt.Errorf("OpenAPI document must be a struct, found %T", expr)
	}
	return &ast.File{Decls: st.Elts}, nil
}

// buildBaseExpr builds the base data for the value at one document position;
// see [buildBase]. It reports whether the value should be skipped by its
// caller.
func buildBaseExpr(v cue.Value) (expr ast.Expr, skip bool, err error) {
	if isUnderComponentsSchemas(v) {
		return nil, true, nil
	}
	if v.LookupPath(schemaMarker).Exists() {
		return ast.NewIdent("_"), false, nil
	}
	switch v.IncompleteKind() {
	case cue.StructKind:
		it, err := v.Fields()
		if err != nil {
			return nil, false, err
		}
		var elts []ast.Decl
		hadFields := false
		for it.Next() {
			hadFields = true
			fv, skip, err := buildBaseExpr(it.Value())
			if err != nil {
				return nil, false, err
			}
			if skip {
				continue
			}
			elts = append(elts, &ast.Field{
				Label: ast.NewStringLabel(it.Selector().Unquoted()),
				Value: fv,
			})
		}
		// Skip a struct whose every field was skipped (e.g. a components
		// section that held only schemas), but keep one that was empty to
		// begin with.
		if hadFields && len(elts) == 0 {
			return nil, true, nil
		}
		return &ast.StructLit{Elts: elts}, false, nil
	case cue.ListKind:
		it, err := v.List()
		if err != nil {
			return nil, false, err
		}
		var elts []ast.Expr
		for it.Next() {
			ev, _, err := buildBaseExpr(it.Value())
			if err != nil {
				return nil, false, err
			}
			elts = append(elts, ev)
		}
		return &ast.ListLit{Elts: elts}, false, nil
	default:
		s := v.Syntax(cue.Final())
		e, ok := s.(ast.Expr)
		if !ok {
			return nil, false, fmt.Errorf("cannot represent value at %v as an expression", v.Path())
		}
		return e, false, nil
	}
}

// isUnderComponentsSchemas reports whether v lives at or under the
// components.schemas subtree of the document.
func isUnderComponentsSchemas(v cue.Value) bool {
	sels := v.Path().Selectors()
	if len(sels) < 2 {
		return false
	}
	return sels[0] == cue.Str("components") && sels[1] == cue.Str("schemas")
}
