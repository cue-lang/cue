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

// extractShareIDsFromEqExpr walks the struct literal of an @test(eq, STRUCT)
// body and collects all @test(shareID=name) annotations on fields.
// basePath is the CUE path of the @test(eq) attribute; field paths in the
// struct are appended to it.  version is the active evaluator version name
// used for version-specific share groups (@test(shareID=name)).
// Returns a map from shareID name to the absolute paths of fields in that group.
func extractShareIDsFromEqExpr(expr ast.Expr, basePath cue.Path, version string) map[string][]cue.Path {
	s, ok := expr.(*ast.StructLit)
	if !ok {
		return nil
	}
	var result map[string][]cue.Path
	for _, d := range s.Elts {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		for _, a := range f.Attrs {
			if k, _ := a.Split(); k != "test" {
				continue
			}
			pa, err := parseTestAttr(a)
			if err != nil || pa.directive != "shareID" {
				continue
			}
			// Version filter: skip if a non-matching version is specified.
			if pa.version != "" && pa.version != version {
				continue
			}
			if len(pa.raw.Fields) == 0 {
				continue
			}
			shareIDName := pa.raw.Fields[0].Value()
			if shareIDName == "" {
				continue
			}
			fieldPath := applyShareIDAt(basePath.Append(labelSelector(f.Label, "")), pa)
			if result == nil {
				result = make(map[string][]cue.Path)
			}
			result[shareIDName] = append(result[shareIDName], fieldPath)
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
// field attributes across ALL records at any nesting depth (no root filtering).
// This is used for cross-root sharing assertions where fields from different
// roots share a vertex. Eq-body sharing is handled per-root by
// collectShareIDsForRoot.
func (r *inlineRunner) collectDirectShareIDs(records []attrRecord, version string) map[string][]cue.Path {
	var shareGroups map[string][]cue.Path
	for _, rec := range records {
		if rec.fileLevel {
			continue
		}

		pa := rec.parsed
		if pa.version != "" && pa.version != version {
			continue
		}
		if pa.directive != "shareID" {
			continue
		}
		if len(pa.raw.Fields) == 0 {
			continue
		}
		shareIDName := pa.raw.Fields[0].Value()
		if shareIDName == "" {
			continue
		}
		if shareGroups == nil {
			shareGroups = make(map[string][]cue.Path)
		}
		shareGroups[shareIDName] = append(shareGroups[shareIDName], applyShareIDAt(rec.path, pa))
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
