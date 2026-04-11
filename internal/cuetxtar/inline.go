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

// Package cuetxtar provides utilities for running CUE tests from txtar archives.
// This file implements inline-assertion mode where @test(...) attributes on CUE
// fields replace golden-file comparison.
//
// # Placement
//
// A @test attribute may appear in two positions:
//
//  1. As a field attribute:  field: expr @test(eq, VALUE)
//     Checks the evaluated field value against VALUE.
//
//  2. As a file-level decl attribute (top-level declaration):
//     a: 1
//     b: a + 1
//     @test(eq, {a: 1, b: 2})
//     Checks the entire file's evaluated value against VALUE.
//     All fields are implicitly covered — no per-field @test is required.
package cuetxtar

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
)

// RunInlineTests iterates over txtar archives in dir, detects inline-assertion
// mode (presence of any @test(...) attribute), and runs inline assertions.
// Archives without @test attributes are left for the existing TxTarTest.Run.
func RunInlineTests(t *testing.T, matrix cuetdtest.Matrix, dir string) {
	t.Helper()
	if matrix == nil {
		runInlineTestsForMatrix(t, nil, dir)
		return
	}
	matrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		runInlineTestsForMatrix(t, m, dir)
	})
}

func runInlineTestsForMatrix(t *testing.T, m *cuetdtest.M, dir string) {
	t.Helper()

	// Determine the base for test names (strip everything up to and including /testdata/).
	baseDir := dir

	err := filepath.WalkDir(dir, func(fullpath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(fullpath) != ".txtar" {
			return nil
		}

		archive, err := txtar.ParseFile(fullpath)
		if err != nil || !isInlineMode(archive) {
			return nil
		}

		// Derive test name from path relative to dir.
		rel, err := filepath.Rel(baseDir, fullpath)
		if err != nil {
			rel = filepath.Base(fullpath)
		}
		// Use forward slashes and strip .txtar extension.
		testName := filepath.ToSlash(rel)
		testName = testName[:len(testName)-len(".txtar")]

		t.Run(testName, func(t *testing.T) {
			runner := &inlineRunner{
				t:        t,
				m:        m,
				archive:  archive,
				dir:      filepath.Dir(fullpath),
				filePath: fullpath,
			}
			runner.runArchive()
		})

		return nil
	})
	if err != nil {
		t.Fatalf("inline: walk %s: %v", dir, err)
	}
}

// InlineRunner is the exported handle for running inline-assertion tests
// against a single txtar archive. Primarily for use in tests.
type InlineRunner struct {
	r *inlineRunner
}

// NewInlineRunner creates an InlineRunner for the given archive.
// m may be nil for unmatrix'd tests. dir is the base directory for loading.
func NewInlineRunner(t *testing.T, m *cuetdtest.M, archive *txtar.Archive, dir string) *InlineRunner {
	return &InlineRunner{r: &inlineRunner{t: t, m: m, archive: archive, dir: dir}}
}

// Run executes all inline test cases in the archive.
func (ir *InlineRunner) Run() {
	ir.r.runArchive()
}

// Section 4: Core Test Runner (inline mode)
// ─────────────────────────────────────────────────────────────────────────────

// inlineRunner handles execution of a single txtar archive in inline mode.
type inlineRunner struct {
	t        *testing.T
	m        *cuetdtest.M
	archive  *txtar.Archive
	dir      string
	filePath string           // path to the archive file on disk; empty for in-memory tests
	cueFiles []*cueFileResult // set by runArchive; used by runPermuteAssertion

	// pendingPosWrites accumulates pos= attribute updates to write back to the
	// archive file after all subtests have run (CUE_UPDATE mode only).
	pendingPosWrites []posWrite

	// nestedPosFills accumulates pos=[] fill-ins for nested @test(err) attrs
	// inside @test(eq, {...}) bodies. Keyed by outer attribute byte offset.
	// Multiple fills for the same outer attribute are merged here so that only
	// one write is produced per outer attribute, avoiding duplicate-bracket
	// artifacts when two pos=[] placeholders share the same outer @test(eq).
	nestedPosFills map[int]*nestedPosEntry

	// pendingInlineFillWrites accumulates inline @test attribute rewrites
	// (fill, force overwrite, regression guard, stale-skip cleanup) to apply
	// after all subtests have run (CUE_UPDATE mode only).
	pendingInlineFillWrites []inlineFillWrite
}

// failCapture wraps *testing.T and captures failures without propagating them.
// It is used for @test(todo) XFAIL mode: all directives run, but failures are
// logged rather than reported as test errors.
//
// failCapture embeds *testing.T to satisfy the testing.TB interface (via the
// promoted unexported private() method). Only Errorf and Error are overridden;
// all other methods delegate to the embedded T.
type failCapture struct {
	testing.TB
	failed bool
	msgs   strings.Builder
}

func (c *failCapture) Error(args ...any) {
	c.failed = true
	fmt.Fprintln(&c.msgs, args...)
}

func (c *failCapture) Errorf(format string, args ...any) {
	c.failed = true
	fmt.Fprintf(&c.msgs, format+"\n", args...)
}

// runDirectivesForPath runs all directives for a single path, handling
// @test(skip) and @test(todo) according to their semantics:
//   - skip directives fire first so t.Skip() is called before other assertions
//   - todo wraps all other directives in a todoCapture to suppress failures
//
// path is used both as the argument to runDirective and in log messages;
// an empty path is displayed as "(file level)".
func (r *inlineRunner) runDirectivesForPath(t *testing.T, path cue.Path, val cue.Value, directives []parsedTestAttr) {
	for _, pa := range directives {
		if pa.directive == "skip" {
			r.runDirective(t, path, val, pa)
		}
	}
	hasTodo := false
	var todoWhy, todoPriority string
	for _, pa := range directives {
		if pa.directive == "todo" {
			hasTodo = true
			for _, kv := range pa.raw.Fields[1:] {
				switch kv.Key() {
				case "why":
					todoWhy = kv.Value()
				case "p":
					todoPriority = kv.Value()
				}
			}
			break
		}
	}
	label := path.String()
	if label == "" {
		label = "(file level)"
	}
	suffix := ""
	if todoPriority != "" {
		suffix += fmt.Sprintf(" p=%s", todoPriority)
	}
	if todoWhy != "" {
		suffix += fmt.Sprintf(" why=%q", todoWhy)
	}
	if hasTodo {
		cap := &failCapture{TB: t}
		for _, pa := range directives {
			if pa.directive == "skip" || pa.directive == "todo" {
				continue
			}
			r.runDirective(cap, path, val, pa)
		}
		if cap.failed {
			t.Logf("TODO (still failing): %s%s\n%s", label, suffix, cap.msgs.String())
		} else {
			t.Logf("WARNING: TODO now passes for %s — consider removing @test(todo)", label)
		}
	} else {
		for _, pa := range directives {
			if pa.directive == "skip" {
				continue
			}
			r.runDirective(t, path, val, pa)
		}
	}
}

// runArchive runs all test cases in the archive.
func (r *inlineRunner) runArchive() {
	r.t.Helper()

	// Check for #subpath restriction.
	subpath := r.subpath()

	// Build and evaluate the stripped CUE.
	// Note: val.Err() may be non-nil if sub-fields are erroneous; this is
	// intentional for tests that assert errors. We only fatal on compile errors.
	ctx := r.cueContext()
	val, allRecords, compileErr := r.buildValue(ctx, nil)
	if compileErr != nil {
		r.t.Fatalf("inline: CUE compile error:\n%s", cueerrors.Details(compileErr, nil))
		return
	}

	// Report any parse errors collected during attribute extraction.
	// An invalid @test attribute is always a test failure.
	for _, rec := range allRecords {
		if rec.parseErr != nil {
			r.t.Errorf("@test parse error at %s: %v", rec.parsed.srcAttr.Pos(), rec.parseErr)
		}
	}

	// Collect file-level records (decl @test attrs at file scope).
	var fileLevelRecords []attrRecord
	for _, rec := range allRecords {
		if rec.fileLevel && rec.parseErr == nil {
			fileLevelRecords = append(fileLevelRecords, rec)
		}
	}

	// Determine which top-level fields are test-case roots.
	// A field is a root if it has any @test attribute (other than file-level).
	// Fields with no @test attributes are silently skipped (fixture fields).
	rootNames := make(map[cue.Selector]bool)
	for _, rec := range allRecords {
		if rec.fileLevel || rec.parseErr != nil {
			continue // file-level and error records are handled separately
		}
		sels := rec.path.Selectors()
		if len(sels) == 0 {
			continue
		}
		rootNames[sels[0]] = true
	}

	// Run file-level @test assertions against the entire file value.
	if len(fileLevelRecords) > 0 {
		version := r.versionName()
		seenFilePaths := make(map[string]bool)
		for _, rec := range fileLevelRecords {
			if seenFilePaths[rec.path.String()] {
				continue
			}
			seenFilePaths[rec.path.String()] = true
			directives := selectActiveDirectives(allRecords, rec.path, version)
			r.runDirectivesForPath(r.t, cue.Path{}, val, directives)
		}
	}

	// Build ordered roots preserving declaration order.
	var roots []testCaseRoot
	for _, f := range r.cueFiles {
		for _, decl := range f.strippedAST.Decls {
			field, ok := decl.(*ast.Field)
			if !ok {
				continue
			}
			sel := cue.Label(field.Label)
			if !rootNames[sel] {
				continue
			}
			if subpath != "" && sel.String() != subpath {
				continue
			}
			roots = append(roots, testCaseRoot{
				sel: sel,
			})
			delete(rootNames, sel) // avoid duplicates across files
		}
	}

	for _, root := range roots {
		root := root
		name := r.subTestName(root, allRecords)
		r.t.Run(name, func(t *testing.T) {
			r.runInline(t, root, val, allRecords)
		})
	}

	// Run file-level shareID checks: @test(shareID=...) annotations at any
	// nesting depth may form groups spanning different roots. These cross-root
	// sharing assertions cannot be detected per-root, so we collect them once
	// over all records and check after all subtests run.
	version := r.versionName()
	if fileShareGroups := r.collectDirectShareIDs(allRecords, version); len(fileShareGroups) > 0 {
		r.runShareIDChecks(r.t, val, fileShareGroups)
	}

	// After all subtests complete, write back any pending updates.
	// Byte-level write-backs (pos, inline-fill) run first so subsequent
	// AST-based write-backs re-parse the updated bytes.
	r.applyInlineFillWritebacks()

	// Update the optional out/errors.txt documentary section.
	r.handleErrorsTxtSection(val)
}

// handleErrorsTxtSection manages the out/errors.txt documentary section.
// The section is only processed if it already exists in the archive:
//   - CUE_UPDATE=1:    updates the section with current error output
//   - otherwise:       silently skips any difference
func (r *inlineRunner) handleErrorsTxtSection(val cue.Value) {
	const sectionName = "out/errors.txt"

	// Find the section in the archive.
	sectionIdx := -1
	for i, f := range r.archive.Files {
		if f.Name == sectionName {
			sectionIdx = i
			break
		}
	}
	// Never auto-create the section.
	if sectionIdx < 0 {
		return
	}

	// Collect all errors (including incomplete) from the evaluated value.
	// Do not pass Cwd: cueerrors.Print prepends "./" to relative paths for
	// IDE compatibility, which we don't want in the golden section. Strip the
	// directory prefix manually instead, consistent with how the rest of the
	// inline runner normalizes paths.
	var buf strings.Builder
	core := val.Core()
	if core.V != nil {
		PrintErrors(&buf, core.V, &cueerrors.Config{
			Cwd:     r.dir,
			ToSlash: true,
		})
	}
	result := buf.String()
	if result != "" && result[len(result)-1] != '\n' {
		result += "\n"
	}
	resultBytes := []byte(result)

	existing := r.archive.Files[sectionIdx].Data
	if bytes.Equal(existing, resultBytes) {
		return
	}

	if cuetest.UpdateGoldenFiles {
		r.archive.Files[sectionIdx].Data = resultBytes
		if r.filePath != "" {
			out := txtar.Format(r.archive)
			if err := os.WriteFile(r.filePath, out, 0o644); err != nil {
				r.t.Errorf("inline: errors.txt write-back to %s: %v", r.filePath, err)
			}
		}
		return
	}

	if cuetest.DiffGoldenFiles {
		r.t.Errorf("result for %s differs: (-want +got)\n%s",
			sectionName,
			cmp.Diff(string(existing), result),
		)
	}
}

// subTestName returns the sub-test name for a root.
// Always uses the field name. @test(desc="...") is purely a human-readable
// description annotation and does not affect the sub-test name.
// TODO: use a name directive to allow an explicit name separate from desc, and support
func (r *inlineRunner) subTestName(root testCaseRoot, _ []attrRecord) string {
	return root.sel.String()
}

// testCaseRoot represents a top-level test case.
type testCaseRoot struct {
	sel cue.Selector
}

// cueFileResult holds the parsed AST for one CUE file.
// @test attributes are recorded but not stripped; CUE ignores them during evaluation.
type cueFileResult struct {
	name        string
	strippedAST *ast.File // original parsed AST (attrs retained)
	// hasTestAttrs is true when the file contained at least one @test attribute.
	// Files where this is false are treated as fixture files: they are still
	// compiled into the evaluated value (so references from other files work)
	// but their top-level fields are NOT required to carry @test directives.
	hasTestAttrs bool
}

// buildValue compiles and evaluates CUE files.
//
// When cueFiles is nil, loads fresh from r.archive using loadWithConfig (which
// handles external package imports and cross-file references). @test attributes
// are recorded from the parsed AST files (but not stripped — CUE ignores them),
// and r.cueFiles is populated.
//
// When cueFiles is non-nil (permutation rebuild), the caller has already
// modified the AST elements in place (field reordering). buildValue reformats
// those ASTs, creates a fresh archive, and reloads to produce a new cue.Value
// that reflects the permuted field ordering.
func (r *inlineRunner) buildValue(ctx *cue.Context, cueFiles []*cueFileResult) (cue.Value, []attrRecord, error) {
	if cueFiles != nil {
		// Permutation rebuild: format modified ASTs and reload.
		return r.buildFromFilesViaLoad(ctx, cueFiles)
	}
	// Initial load.
	return r.buildFromArchive(ctx)
}

// relFilename converts an absolute filename to a relative one by stripping the
// runner's directory prefix. Falls back to the basename if stripping fails.
func (r *inlineRunner) relFilename(absPath string) string {
	if rel, err := filepath.Rel(r.dir, absPath); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(absPath)
}

// buildFromArchive loads the archive via loadWithConfig, extracts @test attrs
// from the parsed AST files, then builds the value directly from the original
// loaded instance (with @test attrs still present). CUE ignores attributes
// during evaluation, so the result is identical to stripping them, but error
// positions reference the original source lines — what the user sees in their
// editor — rather than a reformatted stripped copy.
func (r *inlineRunner) buildFromArchive(ctx *cue.Context) (cue.Value, []attrRecord, error) {
	insts := loadWithConfig(r.archive, r.dir, load.Config{Env: []string{}})
	if len(insts) == 0 {
		return cue.Value{}, nil, fmt.Errorf("no instances found")
	}
	inst := insts[0]
	if inst.Err != nil {
		return cue.Value{}, nil, inst.Err
	}

	var allRecords []attrRecord
	for _, f := range inst.Files {
		relName := r.relFilename(f.Filename)
		records := extractTestAttrs(f, relName)
		allRecords = append(allRecords, records...)
		r.cueFiles = append(r.cueFiles, &cueFileResult{
			name:         relName,
			strippedAST:  f,
			hasTestAttrs: len(records) > 0,
		})
	}

	// Build from the original instance so that error positions match the
	// original source, not a reformatted stripped copy.
	val := ctx.BuildInstance(inst)
	if val.BuildInstance() == nil && val.Err() != nil {
		return cue.Value{}, nil, val.Err()
	}
	return val, allRecords, nil
}

// buildFromFilesViaLoad formats ASTs, creates a stripped archive, and reloads
// via loadWithConfig. Used for permutation rebuilds in module-aware archives.
func (r *inlineRunner) buildFromFilesViaLoad(ctx *cue.Context, cueFiles []*cueFileResult) (cue.Value, []attrRecord, error) {
	strippedByName := make(map[string][]byte, len(cueFiles))
	for _, cf := range cueFiles {
		b, err := format.Node(cf.strippedAST)
		if err != nil {
			return cue.Value{}, nil, fmt.Errorf("format %s: %w", cf.name, err)
		}
		strippedByName[cf.name] = b
	}

	stripped := *r.archive
	stripped.Files = slices.Clone(r.archive.Files)
	for i, f := range stripped.Files {
		if b, ok := strippedByName[f.Name]; ok {
			stripped.Files[i].Data = b
		}
	}

	insts := loadWithConfig(&stripped, r.dir, load.Config{Env: []string{}})
	if len(insts) == 0 {
		return cue.Value{}, nil, fmt.Errorf("no instances found")
	}
	inst := insts[0]
	if inst.Err != nil {
		return cue.Value{}, nil, inst.Err
	}
	val := ctx.BuildInstance(inst)
	if val.BuildInstance() == nil && val.Err() != nil {
		return cue.Value{}, nil, val.Err()
	}
	return val, nil, nil
}

// cueContext returns the appropriate cue.Context for the current matrix entry.
func (r *inlineRunner) cueContext() *cue.Context {
	if r.m != nil {
		return r.m.CueContext()
	}
	return cuecontext.New()
}

// subpath returns the #subpath value from the archive comment, or empty string.
func (r *inlineRunner) subpath() string {
	prefix := []byte("#subpath:")
	for line := range strings.SplitSeq(string(r.archive.Comment), "\n") {
		b := []byte(strings.TrimSpace(line))
		if strings.HasPrefix(string(b), string(prefix)) {
			return strings.TrimSpace(string(b[len(prefix):]))
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 5: Version Discrimination
// ─────────────────────────────────────────────────────────────────────────────

// versionName returns the current evaluator version token (e.g. "v3").
func (r *inlineRunner) versionName() string {
	if r.m != nil {
		return r.m.Name()
	}
	return ""
}

// selectActiveDirectives filters records for a given CUE path and returns the
// effective set of directives to evaluate. Versioned directives take precedence
// over unversioned for the same directive name.
func selectActiveDirectives(records []attrRecord, path cue.Path, version string) []parsedTestAttr {
	// Collect all records matching this path.
	type entry struct {
		pa        parsedTestAttr
		versioned bool
	}
	var candidates []entry
	// todoDirectives are @test(directive:todo, ...) forms — always active and
	// additive (they do not replace unversioned directives of the same name).
	var todoDirectives []parsedTestAttr
	for _, rec := range records {
		if rec.parseErr != nil || rec.path.String() != path.String() {
			continue
		}
		pa := rec.parsed
		if pa.version == "todo" {
			// "todo" is not a real version; treat as expected-to-fail annotation.
			pa.isTodo = true
			todoDirectives = append(todoDirectives, pa)
			continue
		}
		versioned := pa.version != ""
		if versioned && pa.version != version {
			continue // wrong version
		}
		candidates = append(candidates, entry{pa: pa, versioned: versioned})
	}

	// For each (directive, at) pair, prefer the versioned form.
	// Two directives with the same name but different at= values are independent
	// assertions and must both survive deduplication.
	byDirective := make(map[string]parsedTestAttr)
	hasVersioned := make(map[string]bool)
	for _, c := range candidates {
		key := directiveKey(c.pa)
		if c.versioned {
			byDirective[key] = c.pa
			hasVersioned[key] = true
		} else if !hasVersioned[key] {
			byDirective[key] = c.pa
		}
	}

	result := slices.Collect(maps.Values(byDirective))
	// Append todo-form directives last; they are additive and not deduplicated.
	result = append(result, todoDirectives...)
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 6: Inline Form — Directive Assertions
// ─────────────────────────────────────────────────────────────────────────────

// runInline runs assertions for an inline-form test-case root.
func (r *inlineRunner) runInline(t *testing.T, root testCaseRoot, fileVal cue.Value, records []attrRecord) {
	t.Helper()
	version := r.versionName()

	// Gather all records for this root and its descendants.
	// A field may have multiple attrRecords (one per @test attribute), so
	// deduplicate by path to avoid running all directives once per attribute.
	rootPath := cue.MakePath(root.sel)
	seenPaths := make(map[string]bool)
	for _, rec := range records {
		if !pathHasPrefix(rec.path, rootPath) {
			continue
		}
		if seenPaths[rec.path.String()] {
			continue
		}
		seenPaths[rec.path.String()] = true
		directives := selectActiveDirectives(records, rec.path, version)
		fieldVal := fileVal.LookupPath(rec.path)
		r.runDirectivesForPath(t, rec.path, fieldVal, directives)
	}

	// Check @test(shareID=...) vertex-sharing assertions collected from eq bodies.
	shareGroups := r.collectShareIDsForRoot(records, rootPath, version)
	if len(shareGroups) > 0 {
		r.runShareIDChecks(t, fileVal, shareGroups)
	}

	// Handle @test(permute) field attributes.
	r.runInlinePermutes(t, rootPath, records, version)
}

// pathHasPrefix reports whether path starts with prefix, treating each
// selector as an atomic component (not a substring of a selector).
func pathHasPrefix(path, prefix cue.Path) bool {
	ps := path.Selectors()
	prefs := prefix.Selectors()
	if len(ps) < len(prefs) {
		return false
	}
	for i, s := range prefs {
		if ps[i].String() != s.String() {
			return false
		}
	}
	return true
}

// runDirective dispatches a single parsed directive against a cue.Value.
func (r *inlineRunner) runDirective(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	// @test(..., incorrect) — run the directive normally (failures are still
	// reported), but log a NOTE when it passes so readers know this is
	// documenting known-incorrect behavior and that a change here might be
	// intentional (a fix) rather than a regression.
	if pa.isIncorrect {
		cap := &failCapture{TB: t}
		pa2 := pa
		pa2.isIncorrect = false
		r.runDirective(cap, path, val, pa2)
		if !cap.failed {
			t.Logf("NOTE: path %s: %s: matches (documented as known incorrect behavior)", path, pa.directive)
		} else {
			// Propagate as a real test failure — the incorrect behavior changed
			// and needs attention (may be a fix or a new regression).
			t.Errorf("%s", strings.TrimRight(cap.msgs.String(), "\n"))
		}
		return
	}
	switch pa.directive {
	case "eq":
		r.runEqInline(t, path, val, pa)
	case "err":
		r.runErrAssertion(t, path, val, pa)
	case "leq":
		r.runLeqInline(t, path, val, pa)
	case "kind":
		r.runKindAssertion(t, path, val, pa)
	case "closed":
		r.runClosedAssertion(t, path, val, pa)
	case "allows":
		r.runAllowsAssertion(t, path, val, pa)
	case "skip":
		// @test(skip) or @test(skip, why="reason") — skip this test.
		reason := "skipped"
		if len(pa.raw.Fields) > 1 {
			for _, kv := range pa.raw.Fields[1:] {
				if kv.Key() == "why" {
					reason = kv.Value()
				}
			}
		}
		t.Skip(reason) // t.Skip calls runtime.Goexit, stopping the goroutine.
	case "debugCheck":
		r.runDebugCheckInline(t, path, val, pa)
	case "debug":
		r.runDebugOutputInline(t, path, val, pa)
	case "todo":
		// @test(todo) is handled at the loop level in runInline; no-op here.
	case "permute":
		// Handled by permute-group collection in runInline; no-op here.
	case "permuteCount":
		// Handled by checkPermuteCount after permutations run; no-op here.
	case "shareID":
		// @test(shareID=name) annotations appear on fields within @test(eq, {...})
		// bodies; sharing is verified by runShareIDChecks in runInline — no-op here.
	case "desc":
		// @test(desc="...") is a human-readable description annotation — no assertions.
	case "":
		// Empty placeholder @test() — fill with actual value when CUE_UPDATE=1.
		if cuetest.UpdateGoldenFiles {
			r.enqueueInlineFill(pa, r.formatCoverAttr(val))
		}
	default:
		t.Errorf("path %s: unknown @test directive %q", path, pa.directive)
	}
}

// runEqInline checks that val equals the CUE expression in the first arg of pa.
// When CUE_UPDATE modes are active it enqueues the appropriate write-back
// instead of (or in addition to) running the comparison:
//   - empty placeholder @test(eq): fill with actual value (UpdateGoldenFiles)
//   - passing assertion with stale skip: remove the skip (UpdateGoldenFiles)
//   - failing assertion: overwrite with actual value (ForceUpdateGoldenFiles)
func (r *inlineRunner) runEqInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()

	// Extract at= and the expected-value expression from extra fields.
	// Both may appear in any order; at= is named, expression is unnamed.
	var atStr, exprStr string
	for _, kv := range pa.raw.Fields[1:] {
		switch {
		case kv.Key() == "at" && atStr == "":
			atStr = kv.Value()
		case kv.Key() == "" && exprStr == "":
			exprStr = kv.Text()
		}
	}

	// Navigate val to the at= sub-path if specified.
	if atStr != "" {
		atPath, err := parseAtPath(atStr)
		if err != nil {
			t.Errorf("path %s: @test(eq, at=%s): invalid path: %v", path, atStr, err)
			return
		}
		sub := val.LookupPath(atPath)
		if !sub.Exists() {
			t.Errorf("path %s: @test(eq, at=%s): sub-path not found", path, atStr)
			return
		}
		path = cue.MakePath(append(path.Selectors(), atPath.Selectors()...)...)
		val = sub
	}

	// @test(eq:todo, X) — expected-to-fail form.
	// Failures are logged but not reported as test errors; a match emits a warning.
	if pa.isTodo {
		if exprStr == "" {
			return
		}
		expr, err := parser.ParseExpr("@test(eq:todo)", exprStr)
		if err != nil {
			t.Logf("path %s: @test(eq:todo, ...): cannot parse expected expression: %v", path, err)
			return
		}
		cmpErr := (&cmpCtx{baseLine: pa.baseLine}).astCmp(cue.Path{}, expr, val)
		if cmpErr == nil {
			t.Logf("WARNING: path %s: TODO eq:todo now passes — consider upgrading to @test(eq, %s)", path, exprStr)
		} else {
			t.Logf("path %s: TODO eq:todo still failing: %v", path, cmpErr)
		}
		return
	}
	if exprStr == "" {
		// Empty @test(eq) or @test(eq, at=N) — fill placeholder.
		if cuetest.UpdateGoldenFiles {
			r.enqueueInlineFill(pa, r.eqFillAttr(val, atStr, pa))
		}
		return
	}
	expr, err := parser.ParseExpr("@test(eq)", exprStr)
	if err != nil {
		t.Errorf("path %s: @test(eq, ...): cannot parse expected expression: %v", path, err)
		return
	}

	// Detect any @test(...) field attributes inside the eq body — they have no
	// effect there and are almost certainly misplaced.
	reportEqBodyTestAttrs(t, path, expr)

	// Detect stale-skip: an existing skip:<ver> positional arg on this attr
	// marks a known discrepancy recorded by a prior manual annotation.
	_, hasSkip := attrHasSkip(pa.raw)

	ctx := &cmpCtx{
		baseLine: 0, // nested pos= specs use absolute line numbers (deltaLine == absLine)
		posWriteback: func(innerAttrText string, positions []token.Pos) {
			r.enqueueNestedPosWrite(pa, innerAttrText, positions)
		},
	}
	cmpErr := ctx.astCmp(cue.Path{}, expr, val)
	if cmpErr == nil {
		// Assertion passes via AST comparison.
		if hasSkip && cuetest.UpdateGoldenFiles {
			// Stale-skip cleanup: the assertion now passes; strip the skip,
			// restoring @test(eq, <expr>[, at=<sel>]).
			r.enqueueInlineFill(pa, r.eqFillAttrStr(exprStr, atStr, pa))
		}
		return
	}

	// Comparison failed — genuine mismatch.
	if cuetest.ForceUpdateGoldenFiles {
		// CUE_UPDATE=force: overwrite the assertion with the actual value.
		r.enqueueInlineFill(pa, r.eqFillAttr(val, atStr, pa))
		return
	}
	// Report the failure (unless already annotated with a skip).
	if !hasSkip {
		t.Errorf("path %s: %v", path, cmpErr)
		logHint(t, pa.hint)
	}
}

// runLeqInline checks that val is subsumed by the constraint in pa.
func (r *inlineRunner) runLeqInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	if len(pa.raw.Fields) < 2 {
		t.Errorf("path %s: @test(leq) requires a constraint argument", path)
		return
	}
	exprStr := pa.raw.Fields[1].Text()
	ctx := r.cueContext()
	constraint := ctx.CompileString(exprStr)
	if constraint.Err() != nil {
		t.Errorf("path %s: @test(leq, ...): cannot compile constraint: %v", path, constraint.Err())
		return
	}
	r.runLeqAssertion(t, path, val, constraint, pa.hint)
}

// runLeqAssertion asserts that val is subsumed by constraint (constraint ⊑ val, i.e. val is at least as specific).
func (r *inlineRunner) runLeqAssertion(t testing.TB, path cue.Path, val, constraint cue.Value, hint string) {
	t.Helper()
	if err := constraint.Subsume(val); err != nil {
		t.Errorf("path %s: @test(leq): value %v is not subsumed by constraint %v: %v", path, val, constraint, err)
		logHint(t, hint)
	}
}

// runKindAssertion checks val.IncompleteKind() against the expected kind list.
// Syntax: @test(kind=int) or @test(kind=int|string).
func (r *inlineRunner) runKindAssertion(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	// The directive is the key ("kind") and the expected kind(s) are the value.
	if len(pa.raw.Fields) == 0 || pa.raw.Fields[0].Value() == "" {
		t.Errorf("path %s: @test(kind=...) requires a kind value", path)
		return
	}
	expectedStr := pa.raw.Fields[0].Value()
	gotKind := val.IncompleteKind()

	// Parse expected kind(s) — may be pipe-separated like "int|string".
	var expectedKind cue.Kind
	for ks := range strings.SplitSeq(expectedStr, "|") {
		k := parseKindStr(strings.TrimSpace(ks))
		if k == cue.BottomKind {
			t.Errorf("path %s: @test(kind=%q): unknown kind %q", path, expectedStr, ks)
			return
		}
		expectedKind |= k
	}
	if pa.isTodo {
		if gotKind == expectedKind {
			t.Logf("WARNING: path %s: TODO kind:todo now passes — consider upgrading to @test(kind=%s)", path, expectedStr)
		} else {
			t.Logf("path %s: TODO kind:todo still failing: got kind %v, want %v", path, gotKind, expectedKind)
		}
		return
	}
	if gotKind != expectedKind {
		t.Errorf("path %s: @test(kind=%s): got kind %v, want %v", path, expectedStr, gotKind, expectedKind)
		logHint(t, pa.hint)
	}
}

// parseKindStr converts a kind name string to a cue.Kind.
func parseKindStr(s string) cue.Kind {
	switch s {
	case "bool":
		return cue.BoolKind
	case "int":
		return cue.IntKind
	case "float":
		return cue.FloatKind
	case "string":
		return cue.StringKind
	case "bytes":
		return cue.BytesKind
	case "struct":
		return cue.StructKind
	case "list":
		return cue.ListKind
	case "null":
		return cue.NullKind
	case "top", "_":
		return cue.TopKind
	case "bottom", "_|_":
		return cue.BottomKind
	}
	return cue.BottomKind // sentinel for unknown
}

// runClosedAssertion checks val.IsClosed() matches expected.
// Syntax: @test(closed) for closed=true, @test(closed=false) for closed=false.
func (r *inlineRunner) runClosedAssertion(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	expected := true // bare @test(closed) means closed=true
	if len(pa.raw.Fields) >= 1 && pa.raw.Fields[0].Key() == "closed" {
		if pa.raw.Fields[0].Value() == "false" {
			expected = false
		}
	}
	got := val.IsClosed()
	if got != expected {
		t.Errorf("path %s: @test(closed): got closed=%v, want %v", path, got, expected)
		logHint(t, pa.hint)
	}
}

// runAllowsAssertion checks val.Allows(sel) against the expected result.
// Syntax: @test(allows, sel) for expected=true, @test(allows=false, sel) for false.
// sel is a CUE selector expression, e.g. foo, "foo", #Def, [string], or [int].
// The special forms [string] and [int] map to cue.AnyString and cue.AnyIndex.
func (r *inlineRunner) runAllowsAssertion(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	expected := true
	if len(pa.raw.Fields) >= 1 && pa.raw.Fields[0].Key() == "allows" {
		if pa.raw.Fields[0].Value() == "false" {
			expected = false
		}
	}
	var selStr string
	for _, kv := range pa.raw.Fields[1:] {
		if kv.Key() == "" {
			selStr = kv.Value()
			break
		}
	}
	if selStr == "" {
		t.Errorf("path %s: @test(allows): missing selector argument", path)
		return
	}
	k := val.IncompleteKind()
	if k != cue.StructKind && k != cue.ListKind {
		t.Errorf("path %s: @test(allows): directive only valid on struct or list values, got %v", path, k)
		return
	}
	var sel cue.Selector
	switch selStr {
	case "[string]":
		sel = cue.AnyString
	case "[int]":
		sel = cue.AnyIndex
	default:
		p := cue.ParsePath(selStr)
		if err := p.Err(); err != nil || len(p.Selectors()) != 1 {
			t.Errorf("path %s: @test(allows): invalid selector %s: %v", path, selStr, err)
			return
		}
		sel = p.Selectors()[0]
	}
	got := val.Allows(sel)
	if got != expected {
		t.Errorf("path %s: @test(allows, %s): got Allows=%v, want %v", path, selStr, got, expected)
		logHint(t, pa.hint)
	}
}

// attrHasSkip reports whether the raw attribute body contains a skip:<ver> arg
// at position 2 or later.  Returns the version string (e.g. "v3") and true
// when a skip arg is found; returns "", false otherwise.
func attrHasSkip(raw *internal.Attr) (ver string, ok bool) {
	for i := 2; i < len(raw.Fields); i++ {
		text := raw.Fields[i].Text()
		if text == "skip" {
			return "", true
		}
		if strings.HasPrefix(text, "skip:") {
			return text[5:], true
		}
	}
	return "", false
}

// enqueueInlineFill appends a byte-level replacement for pa's @test attribute.
// newAttrText is the full replacement text including the leading @.
func (r *inlineRunner) enqueueInlineFill(pa parsedTestAttr, newAttrText string) {
	r.pendingInlineFillWrites = append(r.pendingInlineFillWrites, inlineFillWrite{
		fileName:    pa.srcFileName,
		attrOffset:  pa.srcAttr.Pos().Offset(),
		attrLen:     len(pa.srcAttr.Text),
		newAttrText: newAttrText,
	})
}
