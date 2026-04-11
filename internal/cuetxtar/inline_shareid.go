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

package cuetxtar

// This file implements @test(shareID=...) vertex-sharing assertions.
// This is isolated here because it is high-scrutiny: it verifies evaluator
// internals (vertex identity) and any logic change could silently allow
// regressions in structure sharing.

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
)

// ─────────────────────────────────────────────────────────────────────────────
// Section 8: shareID — vertex sharing assertions
// ─────────────────────────────────────────────────────────────────────────────

// extractShareIDsFromEqExpr walks the expression of an @test(eq, EXPR) body
// and collects all @test(shareID=name) annotations.
// basePath is the CUE path of the @test(eq) attribute.
// version is the active evaluator version for version-specific share groups.
//
// Supported expression forms:
//   - *ast.StructLit: @test(shareID=name) on fields → path = basePath.fieldLabel
//   - *ast.ListLit:   @test(shareID=name) as decl attrs inside struct elements
//     → path = basePath.Index(i)
//
// Returns a map from shareID name to the absolute paths of fields in that group.
func extractShareIDsFromEqExpr(expr ast.Expr, basePath cue.Path, version string) map[string][]cue.Path {
	var result map[string][]cue.Path
	addResult := func(name string, p cue.Path) {
		if result == nil {
			result = make(map[string][]cue.Path)
		}
		result[name] = append(result[name], p)
	}
	collectShareIDAttrs := func(attrs []*ast.Attribute, path cue.Path) {
		for _, a := range attrs {
			if k, _ := a.Split(); k != "test" {
				continue
			}
			pa, err := parseTestAttr(a)
			if err != nil || pa.directive != "shareID" {
				continue
			}
			if pa.version != "" && pa.version != version {
				continue
			}
			if len(pa.raw.Fields) == 0 {
				continue
			}
			name := pa.raw.Fields[0].Value()
			if name == "" {
				continue
			}
			addResult(name, applyShareIDAt(path, pa))
		}
	}

	switch x := expr.(type) {
	case *ast.StructLit:
		// Struct body: look for @test(shareID=name) on fields.
		for _, d := range x.Elts {
			f, ok := d.(*ast.Field)
			if !ok {
				continue
			}
			collectShareIDAttrs(f.Attrs, basePath.Append(labelSelector(f.Label, "")))
		}

	case *ast.ListLit:
		// List body: look for @test(shareID=name) as decl attrs inside
		// struct elements. The path for element i is basePath.Index(i).
		for i, elt := range x.Elts {
			s, ok := elt.(*ast.StructLit)
			if !ok {
				continue
			}
			elemPath := basePath.Append(cue.Index(i))
			for _, d := range s.Elts {
				a, ok := d.(*ast.Attribute)
				if !ok {
					continue
				}
				collectShareIDAttrs([]*ast.Attribute{a}, elemPath)
			}
		}
	}
	return result
}

// collectShareIDsForRoot builds a map of shareID name → CUE paths by scanning
// all attrRecords within rootPath in two ways:
//
//  1. Direct @test(shareID=name) field attributes in the source — each record
//     with directive "shareID" contributes its rec.path to the named group.
//
//  2. @test(shareID=name) annotations on fields inside @test(eq, STRUCT) bodies
//     — the struct is parsed and fields carrying shareID annotations are mapped
//     to their absolute paths (basePath + fieldLabel).
func (r *inlineRunner) collectShareIDsForRoot(records []attrRecord, rootPath cue.Path, version string) map[string][]cue.Path {
	var shareGroups map[string][]cue.Path
	add := func(id string, p cue.Path) {
		if shareGroups == nil {
			shareGroups = make(map[string][]cue.Path)
		}
		shareGroups[id] = append(shareGroups[id], p)
	}

	// Track processed eq attrs by (fileName, offset) to avoid double-counting.
	type attrKey struct {
		file   string
		offset int
	}
	seenEq := make(map[attrKey]bool)

	for _, rec := range records {
		if !pathHasPrefix(rec.path, rootPath) {
			continue
		}
		pa := rec.parsed
		// Version filter: skip directives targeting a different version.
		if pa.version != "" && pa.version != version {
			continue
		}

		switch pa.directive {
		case "shareID":
			// Direct field attribute: @test(shareID=name) on a source field.
			// Optional at=N sub-option selects list element N within the field.
			if len(pa.raw.Fields) == 0 {
				continue
			}
			shareIDName := pa.raw.Fields[0].Value()
			if shareIDName == "" {
				continue
			}
			add(shareIDName, applyShareIDAt(rec.path, pa))

		case "eq":
			// Eq body: extract @test(shareID=name) from fields in the struct literal.
			if len(pa.raw.Fields) < 2 {
				continue
			}
			key := attrKey{file: pa.srcFileName, offset: pa.srcAttr.Pos().Offset()}
			if seenEq[key] {
				continue
			}
			seenEq[key] = true
			eqExpr, err := parser.ParseExpr("shareID", pa.raw.Fields[1].Text())
			if err != nil {
				continue
			}
			for id, paths := range extractShareIDsFromEqExpr(eqExpr, rec.path, version) {
				for _, p := range paths {
					add(id, p)
				}
			}
		}
	}
	return shareGroups
}

// collectDirectShareIDs builds a shareID group map from direct @test(shareID=name)
// field attributes and in-place @test(shareID=name) inside @test(eq, ...) bodies,
// across ALL records at any nesting depth (no root filtering).
// This is used for cross-root sharing assertions where fields from different
// roots share a vertex.
func (r *inlineRunner) collectDirectShareIDs(records []attrRecord, version string) map[string][]cue.Path {
	type attrKey struct {
		file   string
		offset int
	}
	var shareGroups map[string][]cue.Path
	seenEq := make(map[attrKey]bool)
	add := func(id string, p cue.Path) {
		if shareGroups == nil {
			shareGroups = make(map[string][]cue.Path)
		}
		shareGroups[id] = append(shareGroups[id], p)
	}
	for _, rec := range records {
		pa := rec.parsed
		if pa.version != "" && pa.version != version {
			continue
		}
		switch pa.directive {
		case "shareID":
			if len(pa.raw.Fields) == 0 {
				continue
			}
			shareIDName := pa.raw.Fields[0].Value()
			if shareIDName == "" {
				continue
			}
			add(shareIDName, applyShareIDAt(rec.path, pa))

		case "eq":
			// Extract in-place @test(shareID=name) from fields in the eq body.
			if len(pa.raw.Fields) < 2 {
				continue
			}
			key := attrKey{file: pa.srcFileName, offset: pa.srcAttr.Pos().Offset()}
			if seenEq[key] {
				continue
			}
			seenEq[key] = true
			eqExpr, err := parser.ParseExpr("shareID", pa.raw.Fields[1].Text())
			if err != nil {
				continue
			}
			for id, paths := range extractShareIDsFromEqExpr(eqExpr, rec.path, version) {
				for _, p := range paths {
					add(id, p)
				}
			}
		}
	}
	return shareGroups
}

// runShareIDChecks verifies that all paths in each shareID group dereference to
// the same canonical *adt.Vertex, confirming that the CUE evaluator shares the
// vertex rather than copying it.
func (r *inlineRunner) runShareIDChecks(t testing.TB, fileVal cue.Value, shareGroups map[string][]cue.Path) {
	t.Helper()
	for id, paths := range shareGroups {
		if len(paths) < 2 {
			continue // need at least two to assert sharing
		}
		firstVal := fileVal.LookupPath(paths[0])
		firstCore := firstVal.Core()
		if firstCore.V == nil {
			t.Errorf("@test(shareID=%s): path %s: not found in evaluated value", id, paths[0])
			continue
		}
		derefFirst := firstCore.V.DerefValue()
		for _, p := range paths[1:] {
			otherVal := fileVal.LookupPath(p)
			otherCore := otherVal.Core()
			if otherCore.V == nil {
				t.Errorf("@test(shareID=%s): path %s: not found in evaluated value", id, p)
				continue
			}
			derefOther := otherCore.V.DerefValue()
			if derefFirst != derefOther {
				t.Errorf("@test(shareID=%s): %s and %s are not shared (different vertices)",
					id, paths[0], p)
			}
		}
	}
}
