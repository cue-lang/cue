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

// This file implements @test(permute) assertions for the inline test runner.
// It contains the Heap's-algorithm permutation engine and the AST field-finder
// that locates struct literals by CUE path.
package cuetxtar

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/diff"
)

// runInlinePermutes processes @test(permute) attributes within an inline-form
// test root. It supports two forms:
//
//  1. Field attribute: @test(permute) on individual fields collects those fields
//     into a group under their parent struct. Only the marked fields are permuted.
//
//  2. Decl attribute: @test(permute) as a declaration inside a struct means
//     "permute all fields within this struct."
func (r *inlineRunner) runInlinePermutes(t testing.TB, rootPath cue.Path, records []attrRecord, version string) {
	type permuteGroup struct {
		parentPath cue.Path
		fields     []string // nil means permute all fields
	}
	var permuteGroups []permuteGroup
	parentSeen := map[string]int{} // parentPath.String() → index in permuteGroups

	for _, rec := range records {
		if !pathHasPrefix(rec.path, rootPath) {
			continue
		}
		for _, pa := range selectActiveDirectives(records, rec.path, version) {
			if pa.directive != "permute" {
				continue
			}
			if rec.isDeclAttr {
				// Decl form: @test(permute) inside a struct → permute all
				// fields in that struct. The path points to the struct itself.
				key := rec.path.String()
				if _, ok := parentSeen[key]; !ok {
					parentSeen[key] = len(permuteGroups)
					permuteGroups = append(permuteGroups, permuteGroup{rec.path, nil})
				}
				continue
			}

			// Field form: @test(permute) on a field → collect into parent group.
			sels := rec.path.Selectors()
			if len(sels) < 2 {
				continue // top-level field permute not supported in inline form
			}
			parentPath := cue.MakePath(sels[:len(sels)-1]...)
			fieldName := sels[len(sels)-1].String()
			key := parentPath.String()
			if idx, ok := parentSeen[key]; ok {
				permuteGroups[idx].fields = append(permuteGroups[idx].fields, fieldName)
			} else {
				parentSeen[key] = len(permuteGroups)
				permuteGroups = append(permuteGroups, permuteGroup{parentPath, []string{fieldName}})
			}
		}
	}

	totalPerms := 0
	for _, group := range permuteGroups {
		groupPerms := r.runPermuteAssertion(t, group.parentPath, group.fields)
		// @test(permuteCount, N) placed alongside @test(permute) in the same
		// struct is checked here with the per-group count.
		if groupPerms > 0 {
			r.checkPermuteCount(t, group.parentPath, records, version, groupPerms)
		}
		totalPerms += groupPerms
	}
	// Also check @test(permuteCount, N) at the root with the total across all
	// groups — but only when the root path is not itself one of the group paths
	// (to avoid double-checking single-group cases where the permuted struct IS
	// the root).
	if totalPerms > 0 {
		rootStr := rootPath.String()
		rootIsGroup := false
		for _, group := range permuteGroups {
			if group.parentPath.String() == rootStr {
				rootIsGroup = true
				break
			}
		}
		if !rootIsGroup {
			r.checkPermuteCount(t, rootPath, records, version, totalPerms)
		}
	}
}

// checkPermuteCount verifies or auto-updates a @test(permuteCount, N) directive
// at path after all permutations for a group have run. When CUE_UPDATE=1, the
// count is filled or replaced with the actual value.
func (r *inlineRunner) checkPermuteCount(t testing.TB, path cue.Path, records []attrRecord, version string, actualCount int) {
	t.Helper()
	directives := selectActiveDirectives(records, path, version)
	for _, pa := range directives {
		if pa.directive != "permuteCount" {
			continue
		}
		if len(pa.raw.Fields) < 2 {
			// Bare @test(permuteCount) — fill with actual count.
			if cuetest.UpdateGoldenFiles {
				r.enqueueInlineFill(pa, fmt.Sprintf("@test(permuteCount, %d)", actualCount))
			}
			return
		}
		expectedStr := pa.raw.Fields[1].Value()
		expected, err := strconv.Atoi(expectedStr)
		if err != nil {
			t.Errorf("path %s: @test(permuteCount, %q): cannot parse as integer", path, expectedStr)
			return
		}
		if expected == actualCount {
			return // matches
		}
		if cuetest.UpdateGoldenFiles || cuetest.ForceUpdateGoldenFiles {
			r.enqueueInlineFill(pa, fmt.Sprintf("@test(permuteCount, %d)", actualCount))
			return
		}
		t.Errorf("path %s: @test(permuteCount): got %d permutations, want %d", path, actualCount, expected)
		return
	}
}

// runPermuteAssertion evaluates all N! field-order permutations of the struct
// at structPath and asserts that every ordering produces an identical result.
// fieldNames lists the fields to permute; nil means permute all fields.
// Uses Heap's algorithm to enumerate permutations without allocating N! slices.
// Returns the total number of permutations evaluated (N!), or 0 if skipped
// (fewer than 2 permutable fields).
func (r *inlineRunner) runPermuteAssertion(t testing.TB, structPath cue.Path, fieldNames []string) int {
	t.Helper()
	if r.cueFiles == nil {
		return 0
	}

	// Locate the struct literal and the indices of permutable fields in the AST.
	var targetLit *ast.StructLit
	var permIndices []int
	for _, cf := range r.cueFiles {
		targetLit, permIndices = findPermFieldsAtPath(cf.strippedAST, structPath, fieldNames)
		if targetLit != nil && len(permIndices) >= 2 {
			break
		}
	}
	if targetLit == nil || len(permIndices) < 2 {
		return 0 // fewer than two permutable fields — nothing to test
	}

	n := len(permIndices)

	// Save the original AST elements at the permuted positions.
	origElts := make([]ast.Decl, n)
	for i, idx := range permIndices {
		origElts[i] = targetLit.Elts[idx]
	}

	// Evaluate the baseline (identity permutation / original source order).
	ctx := r.cueContext()
	baselineAll, _, err := r.buildValue(ctx, r.cueFiles)
	if err != nil {
		t.Errorf("path %s: @test(permute): baseline evaluation error: %v", structPath, err)
		return 0
	}
	baseline := baselineAll.LookupPath(structPath)

	// perm[i] = which origElt goes to permuted position i.
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	c := make([]int, n)

	permNum := 0
	reported := false // report only the first differing permutation

	// Heap's algorithm: generates all N! permutations via in-place swaps.
	var generate func(k int)
	generate = func(k int) {
		if k == 1 {
			permNum++
			if permNum == 1 {
				return // skip identity — already evaluated as baseline
			}
			// Apply permutation: position permIndices[i] gets origElts[perm[i]].
			for i, p := range perm {
				targetLit.Elts[permIndices[i]] = origElts[p]
			}
			// Re-evaluate the modified archive.
			permAll, _, evalErr := r.buildValue(ctx, r.cueFiles)
			// Restore immediately so subsequent permutations start from original.
			for i, idx := range permIndices {
				targetLit.Elts[idx] = origElts[i]
			}
			if evalErr != nil || reported {
				return
			}
			permVal := permAll.LookupPath(structPath)
			kind, _ := diff.Diff(baseline, permVal)
			if kind != diff.Identity {
				reported = true
				permNames := make([]string, n)
				for i, p := range perm {
					if f, ok := origElts[p].(*ast.Field); ok {
						permNames[i] = identStr(f.Label)
					} else {
						permNames[i] = fmt.Sprintf("[%d]", p)
					}
				}
				t.Errorf("path %s: @test(permute): ordering [%s] produces a different result\ngot:  %s\nwant: %s",
					structPath, strings.Join(permNames, ", "),
					r.formatValue(permVal, ""), r.formatValue(baseline, ""))
			}
			return
		}
		for i := 0; i < k; i++ {
			generate(k - 1)
			if k%2 == 0 {
				perm[i], perm[k-1] = perm[k-1], perm[i]
			} else {
				perm[0], perm[k-1] = perm[k-1], perm[0]
			}
			c[i]++
		}
	}
	generate(n)
	t.Logf("path %s: @test(permute): evaluated %d permutations of %d fields", structPath, permNum, n)
	return permNum
}

// findPermFieldsAtPath walks file following structPath and returns the
// *ast.StructLit at that location together with the indices of permutable
// *ast.Field entries inside it.  If fieldNames is nil or empty every
// *ast.Field in the struct is included; otherwise only the named ones.
func findPermFieldsAtPath(file *ast.File, structPath cue.Path, fieldNames []string) (*ast.StructLit, []int) {
	sels := structPath.Selectors()
	if len(sels) == 0 {
		return nil, nil
	}

	// Navigate the AST following each selector.
	decls := file.Decls
	var targetLit *ast.StructLit
	for i, sel := range sels {
		name := sel.String()
		var found *ast.Field
		for _, d := range decls {
			f, ok := d.(*ast.Field)
			if !ok {
				continue
			}
			if identStr(f.Label) == name {
				found = f
				break
			}
		}
		if found == nil {
			return nil, nil
		}
		sl, ok := found.Value.(*ast.StructLit)
		if !ok {
			return nil, nil
		}
		if i == len(sels)-1 {
			targetLit = sl
		} else {
			decls = sl.Elts
		}
	}
	if targetLit == nil {
		return nil, nil
	}

	// Collect the indices of fields to permute.
	permSet := make(map[string]bool, len(fieldNames))
	for _, name := range fieldNames {
		permSet[name] = true
	}
	allFields := len(fieldNames) == 0

	var indices []int
	for i, elt := range targetLit.Elts {
		f, ok := elt.(*ast.Field)
		if !ok {
			continue
		}
		name := identStr(f.Label)
		if allFields || permSet[name] {
			indices = append(indices, i)
		}
	}
	return targetLit, indices
}
