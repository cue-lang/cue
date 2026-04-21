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

// This file implements field coverage checking for inline-assertion test
// archives. Every field in any struct that contains @test attributes must be
// either directly tested (has a @test) or reachable by some reference from a
// tested sibling field (transitively). The check applies recursively: any
// struct whose sub-fields include at least one @test is subject to the check.
//
// Coverage propagation within a struct follows three mechanisms:
//
//  1. Identifier reference: if field F references field G by its name (e.g.
//     `F: G & {...}`), coverage propagates from F to G.
//
//  2. Postfix alias: if field G declares a postfix alias (`G~X: ...`), then
//     a reference to `X` in a covered field covers G.
//
//  3. Let binding: if a let `let X = G` binds X to field G in the same scope,
//     then a reference to X in a covered field covers G.
//
//  4. Comprehension: if a comprehension (for/if clause) at the same scope
//     references two fields F and G, then covering F also covers G.
//     This is needed because a comprehension like `for k, v in items { results:
//     ... }` ties items and results together: testing results implicitly
//     exercises items. See coverage_comprehension.txtar for an example.
//
// CUE files without any @test attribute (fixture files) are still included in
// the coverage check: their fields must be reachable from tested fields via
// identifier references. A fixture file whose top-level fields are all
// referenced (transitively) by tested fields is fine; one with unreachable
// fields is flagged, since those fields are never exercised.
// Archives with a file-level @test that covers the whole file are exempt too.
// Suppressing the check for a specific archive is possible via:
//
//	#no-coverage
//
// in the archive's comment header.

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
)

// fieldEntry describes one named field within a scope being coverage-checked.
type fieldEntry struct {
	name     string
	fileName string
	line     int
	refs     map[string]bool // identifiers referenced in the field's value
	valueAST ast.Expr        // for recursion into nested struct literals
}

// checkFieldCoverage reports any field (at any nesting depth) in a
// @test-bearing CUE file that is neither directly tested nor reachable,
// transitively, from a directly tested sibling field within the same struct.
//
// fileLevelRecords are file-scope @test attributes (no field path); when any
// are present the entire file is under test and all fields are implicitly
// covered.
//
// rootNames is the map of top-level selectors that have at least one @test
// attribute somewhere in their subtree. It must not be modified after this
// call returns.
//
// allRecords is the complete set of @test records for the archive, used to
// drive the recursive coverage checks into nested struct literals.
func (r *inlineRunner) checkFieldCoverage(t testing.TB, fileLevelRecords []attrRecord, rootNames map[cue.Selector]bool, allRecords []attrRecord) {
	// Check for opt-out tag in the archive comment.
	for line := range strings.SplitSeq(string(r.archive.Comment), "\n") {
		if strings.TrimSpace(line) == "#no-coverage" {
			return
		}
	}

	// A file-level @test covers the whole evaluated value; all fields are
	// implicitly covered.
	if len(fileLevelRecords) > 0 {
		return
	}

	// Convert all @test record paths to string slices for the recursive checker.
	// File-level (empty path) and parse-error records are already handled above.
	var testedPaths [][]string
	for _, rec := range allRecords {
		if rec.fileLevel || rec.parseErr != nil {
			continue
		}
		sels := rec.path.Selectors()
		if len(sels) == 0 {
			continue
		}
		path := make([]string, len(sels))
		for i, s := range sels {
			path[i] = s.String()
		}
		testedPaths = append(testedPaths, path)
	}
	if len(testedPaths) == 0 {
		return
	}

	var (
		entries      []fieldEntry
		allNames     = make(map[string]bool)
		aliasToField = make(map[string]string)
		letRefs      = make(map[string]map[string]bool)
		compRefs     []map[string]bool
	)
	for _, cf := range r.cueFiles {
		parseDecls(cf.strippedAST.Decls, cf.name, &entries, allNames, aliasToField, letRefs, &compRefs)
	}
	if len(entries) == 0 {
		return
	}

	covered := make(map[string]bool, len(rootNames))
	for sel := range rootNames {
		covered[sel.String()] = true
	}
	runCoverage(t, entries, allNames, aliasToField, letRefs, compRefs, covered, "", testedPaths)
}

// checkStructCoverage checks field coverage for a struct literal at pathStr.
// subPaths are relative paths within this struct: each path's first element
// is a child field name; an empty path means the struct itself is tested.
func checkStructCoverage(
	t testing.TB,
	decls []ast.Decl,
	fileName string,
	pathStr string,
	subPaths [][]string,
) {
	// If any sub-path is empty, the struct itself is tested → all sub-fields
	// are implicitly covered.
	for _, sp := range subPaths {
		if len(sp) == 0 {
			return
		}
	}

	covered := make(map[string]bool)
	for _, sp := range subPaths {
		if len(sp) > 0 {
			covered[sp[0]] = true
		}
	}

	var (
		entries      []fieldEntry
		allNames     = make(map[string]bool)
		aliasToField = make(map[string]string)
		letRefs      = make(map[string]map[string]bool)
		compRefs     []map[string]bool
	)
	parseDecls(decls, fileName, &entries, allNames, aliasToField, letRefs, &compRefs)
	if len(entries) == 0 {
		return
	}
	runCoverage(t, entries, allNames, aliasToField, letRefs, compRefs, covered, pathStr, subPaths)
}

// parseDecls extracts field entries and coverage-propagation data from a slice
// of AST declarations.
func parseDecls(
	decls []ast.Decl,
	fileName string,
	entries *[]fieldEntry,
	allNames map[string]bool,
	aliasToField map[string]string,
	letRefs map[string]map[string]bool,
	compRefs *[]map[string]bool,
) {
	for _, decl := range decls {
		switch d := decl.(type) {
		case *ast.Comprehension:
			// Comprehension references form mutual-coverage groups: if any
			// field referenced by a comprehension is covered, all others in
			// that comprehension are covered too (see comment at top of file).
			*compRefs = append(*compRefs, collectRefs(d))
		case *ast.Field:
			name := identStr(d.Label)
			if name == "" {
				continue
			}
			if pa := d.Alias; pa != nil {
				if pa.Field != nil && pa.Field.Name != "_" {
					aliasToField[pa.Field.Name] = name
				}
				if pa.Label != nil && pa.Label.Name != "_" {
					aliasToField[pa.Label.Name] = name
				}
			}
			*entries = append(*entries, fieldEntry{
				name:     name,
				fileName: fileName,
				line:     d.Pos().Line(),
				refs:     collectRefs(d.Value),
				valueAST: d.Value,
			})
			allNames[name] = true
		case *ast.LetClause:
			if d.Ident != nil {
				letRefs[d.Ident.Name] = collectRefs(d.Expr)
			}
		}
	}
}

// runCoverage propagates coverage and reports any uncovered fields. It also
// recurses into struct-literal values of covered fields.
//
// covered is pre-seeded with the directly-tested field names for this scope.
// pathStr is the dotted path prefix used in error messages (empty at top level).
// testedPaths drives recursive coverage into nested structs.
func runCoverage(
	t testing.TB,
	entries []fieldEntry,
	allNames map[string]bool,
	aliasToField map[string]string,
	letRefs map[string]map[string]bool,
	compRefs []map[string]bool,
	covered map[string]bool,
	pathStr string,
	testedPaths [][]string,
) {
	// Build comprehension groups: restrict each comprehension's raw identifier
	// set to known field names so that covering any one member covers all others.
	var compGroups []map[string]bool
	for _, refs := range compRefs {
		group := make(map[string]bool)
		for id := range refs {
			if allNames[id] {
				group[id] = true
			}
		}
		if len(group) > 1 {
			compGroups = append(compGroups, group)
		}
	}

	nodes := make([]coverageNode, len(entries))
	for i, e := range entries {
		nodes[i] = coverageNode{name: e.name, refs: e.refs}
	}
	propagateCoverage(nodes, allNames, aliasToField, letRefs, compGroups, covered)

	for _, e := range entries {
		fullName := e.name
		if pathStr != "" {
			fullName = pathStr + "." + e.name
		}
		if !covered[e.name] {
			t.Errorf("%s:%d: field %s is not covered: add a @test directive or reference it from a tested field",
				e.fileName, e.line, fullName)
			continue
		}
		sl, ok := e.valueAST.(*ast.StructLit)
		if !ok {
			continue
		}
		var subPaths [][]string
		for _, p := range testedPaths {
			if len(p) > 0 && p[0] == e.name {
				subPaths = append(subPaths, p[1:])
			}
		}
		if len(subPaths) > 0 {
			checkStructCoverage(t, sl.Elts, e.fileName, fullName, subPaths)
		}
	}
}

// coverageNode carries the name and identifier set needed by propagateCoverage.
type coverageNode struct {
	name string
	refs map[string]bool
}

// propagateCoverage runs a fixed-point BFS, spreading coverage from
// already-covered fields to the fields they reference.
// covered is modified in place.
func propagateCoverage(
	nodes []coverageNode,
	allNames map[string]bool,
	aliasToField map[string]string,
	letRefs map[string]map[string]bool,
	compGroups []map[string]bool,
	covered map[string]bool,
) {
	for changed := true; changed; {
		changed = false
		for _, n := range nodes {
			if !covered[n.name] {
				continue
			}
			for ident := range n.refs {
				// Direct identifier reference.
				if allNames[ident] && !covered[ident] {
					covered[ident] = true
					changed = true
				}
				// Postfix alias: identifier resolves to a field via ~.
				if fieldName, ok := aliasToField[ident]; ok && !covered[fieldName] {
					covered[fieldName] = true
					changed = true
				}
				// Let binding: identifier is a let variable referencing other fields.
				for ref := range letRefs[ident] {
					if allNames[ref] && !covered[ref] {
						covered[ref] = true
						changed = true
					}
				}
			}
		}
		// Comprehension group propagation: if any member of a group is covered,
		// all members become covered.
		for _, group := range compGroups {
			anyCovered := false
			for name := range group {
				if covered[name] {
					anyCovered = true
					break
				}
			}
			if anyCovered {
				for name := range group {
					if !covered[name] {
						covered[name] = true
						changed = true
					}
				}
			}
		}
	}
}

// collectRefs returns the set of all identifier names referenced
// anywhere within node.
func collectRefs(node ast.Node) map[string]bool {
	if node == nil {
		return nil
	}
	result := make(map[string]bool)
	ast.Walk(node, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			result[id.Name] = true
		}
		return true
	}, nil)
	return result
}
