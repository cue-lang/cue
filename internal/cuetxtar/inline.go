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
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/debug"
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

// ─────────────────────────────────────────────────────────────────────────────
// Section 1: Attribute Parsing Utilities
// ─────────────────────────────────────────────────────────────────────────────

// parsedTestAttr holds the result of parsing a single @test(...) attribute.
type parsedTestAttr struct {
	// directive is the primary directive name, e.g. "eq", "err", "kind",
	// "closed", "leq", "skip", "permute", "file", "desc".
	directive string

	// version is the optional version suffix from directive:vN, e.g. "v3".
	// Empty means unversioned.
	version string

	// raw is the parsed internal.Attr for accessing remaining arguments.
	raw internal.Attr

	// For "err" directives, parsed sub-options are stored here.
	errArgs *errArgs

	// isTodo marks directives produced by the "todo" version qualifier
	// (e.g. @test(eq:todo, X)). These run as expected-to-fail: failures
	// are logged rather than reported as errors; a pass emits a warning.
	isTodo bool

	// todoPriority is the p= value from a :todo directive, e.g. "1" for p=1.
	// 0 is the highest priority; empty means no priority specified.
	todoPriority string

	// isIncorrect marks a directive carrying the "incorrect" positional flag
	// (e.g. @test(eq, 3, incorrect) or @test(err, code=eval, incorrect)).
	// Applicable to any assertion directive. The assertion documents current
	// known-incorrect behavior: a pass is suppressed (logs a NOTE), but a
	// failure still propagates as a real test failure so that changes to the
	// incorrect value are always detected.
	isIncorrect bool

	// hint is an optional message printed when the assertion fails
	// (from hint="..."). Intended as guidance for automated tools
	// such as AI assistants reviewing test failures.
	hint string

	// srcAttr is the original AST attribute node (needed for CUE_UPDATE write-back).
	srcAttr *ast.Attribute

	// srcFileName is the archive .cue file name containing this attribute,
	// e.g. "in.cue" (needed to locate the file for CUE_UPDATE write-back).
	srcFileName string

	// baseLine is the effective 1-indexed line of the field carrying this
	// attribute in the stripped-and-formatted output.  It may differ from
	// srcAttr.Pos().Line() when earlier @test attributes on preceding fields
	// contain embedded newlines in their bodies (which are stripped before
	// formatting, collapsing those extra lines).
	baseLine int
}

// parseTestAttr parses the body of a @test(...) attribute node.
// It returns a parsedTestAttr for each logical directive in the attribute.
// A single @test(...) contains exactly one directive (the first positional
// argument or the key of the first key=value pair).
func parseTestAttr(a *ast.Attribute) (parsedTestAttr, error) {
	key, body := a.Split()
	if key != "test" {
		return parsedTestAttr{}, fmt.Errorf("not a @test attribute: @%s", key)
	}

	parsed := internal.ParseAttrBody(a.Pos(), body)
	if parsed.Err != nil {
		return parsedTestAttr{}, parsed.Err
	}

	result := parsedTestAttr{
		raw:     parsed,
		srcAttr: a,
	}

	if len(parsed.Fields) == 0 || (len(parsed.Fields) == 1 && parsed.Fields[0] == internal.KeyValue{}) {
		// @test() — empty placeholder or bare marker.
		result.directive = ""
		return result, nil
	}

	// The first field determines the directive.
	// Case 1: key=value form like desc="hello", shareID=name — directive is the key.
	// Case 2: positional form like eq, err, kind — directive (with optional :vN suffix) is the value.
	f0 := parsed.Fields[0]
	if f0.Key() != "" {
		dir := f0.Key()
		// Key-based directives may carry a version suffix: "shareID" → directive="shareID", version="v3".
		if idx := strings.LastIndex(dir, ":"); idx >= 0 {
			result.directive = dir[:idx]
			result.version = dir[idx+1:]
		} else {
			result.directive = dir
		}
	} else {
		// May have version suffix: "eq:v3" → directive="eq", version="v3".
		dir := f0.Value()
		if idx := strings.LastIndex(dir, ":"); idx >= 0 {
			result.directive = dir[:idx]
			result.version = dir[idx+1:]
		} else {
			result.directive = dir
		}
	}

	// Parse directive-specific sub-options.
	switch result.directive {
	case "err":
		ea, err := parseErrArgs(parsed)
		if err != nil {
			return result, err
		}
		result.errArgs = &ea
	}

	// Extract universal flags and reject unknown key= flags.
	// Positional args (kv.Key() == "") are accepted by directives as needed.
	// Directives with their own flag parsers (err, todo, skip, shareID) are
	// responsible for validating their own flags.
	for _, kv := range parsed.Fields[1:] {
		switch kv.Key() {
		case "hint":
			result.hint = kv.Value()
		case "p":
			// p= is a universal priority flag (e.g. p=1 on err:todo).
			result.todoPriority = kv.Value()
		case "":
			// Positional arg — check for universal flags.
			if kv.Value() == "incorrect" {
				result.isIncorrect = true
			}
		case "at":
			// at= is accepted by eq, err, and shareID; each validates it
			// in its own handler.
			switch result.directive {
			case "eq", "err", "shareID":
				// Validated in their respective handlers.
			default:
				return result, fmt.Errorf("@test(%s): unknown flag %q", result.directive, kv.Key())
			}
		default:
			switch result.directive {
			case "err", "todo", "skip", "shareID":
				// These directives parse their own flags elsewhere.
			default:
				return result, fmt.Errorf("@test(%s): unknown flag %q", result.directive, kv.Key())
			}
		}
	}

	return result, nil
}

// logHint logs hint as an additional note following a test failure.
// Call immediately after t.Errorf when pa.hint is set.
func logHint(t testing.TB, hint string) {
	if hint != "" {
		t.Helper()
		t.Log("hint:", hint)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 2: AST Extraction and Stripping
// ─────────────────────────────────────────────────────────────────────────────

// attrRecord associates a parsed @test attribute with its location in the
// evaluated CUE value.
type attrRecord struct {
	// path is the full CUE path to the field carrying this attribute.
	path cue.Path

	// parsed is the parsed directive from this attribute.
	parsed parsedTestAttr

	// parseErr is non-nil when parseTestAttr failed. The runner reports the
	// error as a test failure and skips running the directive.
	parseErr error

	// fileLevel is true when this record comes from a file-level (top-level)
	// decl attribute rather than a field attribute or struct-level decl attribute.
	// A file-level @test(eq, VALUE) checks the entire file's evaluated value.
	fileLevel bool

	// isDeclAttr is true when this record comes from a decl attribute inside
	// a struct (as opposed to a field attribute). For @test(permute), this
	// distinction matters: a decl attr means "permute all fields within this
	// struct" whereas a field attr means "this field participates in
	// permutation within its parent struct."
	isDeclAttr bool
}

// extractTestAttrs walks ast.File and:
//  1. Collects all @test(...) attributes from field attrs, struct decl attrs,
//     and file-level decl attrs.
//  2. Removes them from the AST (in-place).
//  3. Preserves all non-@test attributes.
//
// Returns the collected records.
// File-level decl attributes produce records with an empty path and
// fileLevel=true; these check the entire file's evaluated value.
func extractTestAttrs(f *ast.File, fileName string) []attrRecord {
	var records []attrRecord

	// appendErrRecord records a @test attribute whose parseTestAttr call failed.
	// The runner reports all parseErr records as test failures before running
	// any assertions.
	appendErrRecord := func(attr *ast.Attribute, path cue.Path, isDeclAttr, fileLevel bool, err error) {
		records = append(records, attrRecord{
			path:       path,
			parsed:     parsedTestAttr{srcAttr: attr, srcFileName: fileName},
			parseErr:   err,
			isDeclAttr: isDeclAttr,
			fileLevel:  fileLevel,
		})
	}

	// walkField records @test field attrs, then recurses into the field's
	// struct value (if any). Attributes are NOT stripped from the AST so that
	// the evaluated value is built from the original source; CUE ignores
	// attributes during evaluation, so positions reference original source lines.
	var walkField func(field *ast.Field, path cue.Path)

	// walkStruct records @test decl attrs and recurses into all sub-fields.
	var walkStruct func(sl *ast.StructLit, path cue.Path)

	walkField = func(field *ast.Field, path cue.Path) {
		fieldBaseLine := field.Pos().Line()

		for _, a := range field.Attrs {
			k, _ := a.Split()
			if k != "test" {
				continue
			}
			pa, err := parseTestAttr(a)
			if err != nil {
				appendErrRecord(a, path, false, false, err)
				continue
			}
			pa.baseLine = fieldBaseLine
			pa.srcFileName = fileName
			records = append(records, attrRecord{
				path:   path,
				parsed: pa,
			})
		}

		if sl, ok := field.Value.(*ast.StructLit); ok {
			walkStruct(sl, path)
		}
	}

	walkStruct = func(sl *ast.StructLit, path cue.Path) {
		// Use the opening brace line as the base for decl-level @test pos= specs.
		// File-level @test attrs (no braces) use their own line as baseLine (see below).
		structBaseLine := sl.Lbrace.Line()

		for _, elt := range sl.Elts {
			switch e := elt.(type) {
			case *ast.Attribute:
				k, _ := e.Split()
				if k != "test" {
					continue
				}
				pa, err := parseTestAttr(e)
				if err != nil {
					appendErrRecord(e, path, true, false, err)
					continue
				}
				pa.baseLine = structBaseLine
				pa.srcFileName = fileName
				records = append(records, attrRecord{
					path:       path,
					parsed:     pa,
					isDeclAttr: true,
				})

			case *ast.Field:
				subPath := appendPath(path, e.Label)
				if subPath.Err() == nil {
					walkField(e, subPath)
				} else {
					// Non-static label (e.g. string interpolation): cannot
					// register a path from the AST. Still process
					// @test(shareID=name, at=sel) where at= is a non-integer
					// field name giving the resolved key, so the shareID check
					// can look it up in the evaluated value.
					for _, a := range e.Attrs {
						k, _ := a.Split()
						if k != "test" {
							continue
						}
						pa, err := parseTestAttr(a)
						if err != nil {
							appendErrRecord(a, path, false, false, err)
							continue
						}
						if pa.directive != "shareID" || len(pa.raw.Fields) == 0 {
							continue
						}
						// Require a non-integer at= giving the resolved field name.
						// applyShareIDAt (called in collectDirectShareIDs) will
						// append it to the parent path stored here.
						if !hasShareIDAtSel(pa) {
							continue // no usable at=sel for dynamic key
						}
						pa.baseLine = a.Pos().Line()
						pa.srcFileName = fileName
						// Store the parent path; applyShareIDAt in
						// collectDirectShareIDs appends the at= selector.
						records = append(records, attrRecord{
							path:   path,
							parsed: pa,
						})
					}
				}
			}
		}
	}

	// Handle file-level decl attributes (@test as a top-level declaration).
	for _, decl := range f.Decls {
		if a, ok := decl.(*ast.Attribute); ok {
			k, _ := a.Split()
			if k == "test" {
				pa, err := parseTestAttr(a)
				if err != nil {
					appendErrRecord(a, cue.Path{}, false, true, err)
					continue
				}
				pa.baseLine = a.Pos().Line()
				pa.srcFileName = fileName
				records = append(records, attrRecord{
					path:      cue.Path{},
					parsed:    pa,
					fileLevel: true,
				})
			}
		}
	}

	for _, decl := range f.Decls {
		field, ok := decl.(*ast.Field)
		if !ok {
			continue
		}
		fieldPath := cue.MakePath(cue.Label(field.Label))
		if fieldPath.Err() == nil {
			walkField(field, fieldPath)
		}
	}

	return records
}

// identStr returns the string form of an AST label that is a simple identifier
// or string literal. Returns empty string for complex labels.
func identStr(label ast.Label) string {
	switch l := label.(type) {
	case *ast.Ident:
		return l.Name
	case *ast.BasicLit:
		// Strip quotes for string labels.
		s := l.Value
		if len(s) >= 2 && s[0] == '"' {
			s = s[1 : len(s)-1]
		}
		return s
	}
	return ""
}

// appendPath appends a selector for label to an existing path.
func appendPath(base cue.Path, label ast.Label) cue.Path {
	return base.Append(cue.Label(label))
}

// applyShareIDAt applies the at= field from a @test(shareID=...) attribute
// to base. For integer at=N it appends a list index; for a non-integer at=sel
// it parses sel as a CUE path and appends its selectors. Returns base
// unchanged if no at= is present or if the at= value cannot be parsed.
func applyShareIDAt(base cue.Path, pa parsedTestAttr) cue.Path {
	for _, kv := range pa.raw.Fields[1:] {
		if kv.Key() != "at" {
			continue
		}
		val := kv.Value()
		if n, err := strconv.Atoi(val); err == nil {
			return base.Append(cue.Index(n))
		}
		if p := cue.ParsePath(val); p.Err() == nil {
			return base.Append(p.Selectors()...)
		}
		break
	}
	return base
}

// hasShareIDAtSel reports whether pa contains at= with a non-integer value,
// i.e. a field-name selector for use with dynamic-key fields.
func hasShareIDAtSel(pa parsedTestAttr) bool {
	for _, kv := range pa.raw.Fields[1:] {
		if kv.Key() != "at" {
			continue
		}
		val := kv.Value()
		if val == "" {
			return false
		}
		_, err := strconv.Atoi(val)
		return err != nil
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 3: Mode Detection
// ─────────────────────────────────────────────────────────────────────────────

// isInlineMode parses the CUE files in the archive (AST only) and returns true
// if any @test(...) attribute is found anywhere in any CUE file — as a field
// attribute, a decl attribute inside a struct at any nesting depth, or a
// file-level decl attribute.  No compilation is required.
func isInlineMode(archive *txtar.Archive) bool {
	for _, f := range archive.Files {
		if !strings.HasSuffix(f.Name, ".cue") {
			continue
		}
		af, err := parser.ParseFile(f.Name, f.Data)
		if err != nil {
			continue
		}
		if declsHaveTestAttrs(af.Decls) {
			return true
		}
	}
	return false
}

// declsHaveTestAttrs recursively searches decls for any @test(...) attribute,
// descending into struct-valued fields and comprehension bodies at any depth.
func declsHaveTestAttrs(decls []ast.Decl) bool {
	for _, decl := range decls {
		switch d := decl.(type) {
		case *ast.Attribute:
			if k, _ := d.Split(); k == "test" {
				return true
			}
		case *ast.Field:
			for _, a := range d.Attrs {
				if k, _ := a.Split(); k == "test" {
					return true
				}
			}
			if sl, ok := d.Value.(*ast.StructLit); ok {
				if declsHaveTestAttrs(sl.Elts) {
					return true
				}
			}
		case *ast.Comprehension:
			if sl, ok := d.Value.(*ast.StructLit); ok {
				if declsHaveTestAttrs(sl.Elts) {
					return true
				}
			}
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
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
		r.t.Fatalf("inline: CUE compile error: %v", compileErr)
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
	r.applyPosWritebacks()
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

	// For each directive name, prefer the versioned form.
	byDirective := make(map[string]parsedTestAttr)
	hasVersioned := make(map[string]bool)
	for _, c := range candidates {
		dir := c.pa.directive
		if c.versioned {
			byDirective[dir] = c.pa
			hasVersioned[dir] = true
		} else if !hasVersioned[dir] {
			byDirective[dir] = c.pa
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
		atPath := cue.ParsePath(atStr)
		if err := atPath.Err(); err != nil {
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

	cmpErr := (&cmpCtx{baseLine: 0}).astCmp(cue.Path{}, expr, val)
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

// eqFillAttr builds an @test(eq, <value>[, at=<atStr>]) attribute for fill/force-update.
func (r *inlineRunner) eqFillAttr(v cue.Value, atStr string, pa parsedTestAttr) string {
	if r.isError(v) {
		return "@test(err)"
	}
	return r.eqFillAttrStr(r.formatValue(v), atStr, pa)
}

// eqFillAttrStr builds @test(eq, <exprStr>[, at=<atStr>]).
// For multi-line expressions, if the compact single-line form is < 20 chars it
// is used directly; otherwise lines after the first are re-indented using the
// leading whitespace of the source line containing the @test attribute (the
// same offset trick as formatDebugAttr, but without an extra tab because
// format.Node already carries one tab of relative indentation).
func (r *inlineRunner) eqFillAttrStr(exprStr, atStr string, pa parsedTestAttr) string {
	if strings.Contains(exprStr, "\n") {
		if compact := compactCUEExpr(exprStr); len(compact) < 20 {
			exprStr = compact
		} else {
			indent := r.attrLineIndent(pa)
			exprStr = strings.ReplaceAll(exprStr, "\n", "\n"+indent)
		}
	}
	if atStr != "" {
		return fmt.Sprintf("@test(eq, %s, at=%s)", exprStr, atStr)
	}
	return fmt.Sprintf("@test(eq, %s)", exprStr)
}

// compactCUEExpr collapses a multi-line CUE expression produced by format.Node
// into a single line. It handles struct and list literals by joining their
// tab-indented field lines with ", ". Only safe for shallow (non-nested)
// structures; deeply-nested values will exceed the 20-char threshold and use
// the indented form instead.
func compactCUEExpr(s string) string {
	lines := strings.Split(s, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, "\t")
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	open, close_ := parts[0], parts[len(parts)-1]
	middle := parts[1 : len(parts)-1]
	if (open == "{" || open == "[") && len(middle) > 0 {
		return open + strings.Join(middle, ", ") + close_
	}
	return strings.Join(parts, " ")
}

// formatValue returns a human-readable CUE string for a value.
// Routes through the Vertex export path (via cue.Final()) to avoid internal
// _#def wrapping, then re-enables optional fields (value?: T) so the
// formatted expression round-trips through astCmp.
func (r *inlineRunner) formatValue(v cue.Value) string {
	// cue.Final() routes to Vertex() export (no _#def wrapping) and sets
	// omitOptional=true.  cue.Optional(true) applied afterwards re-enables
	// optional fields, giving us the complete value without internals.
	syn := v.Syntax(cue.Docs(true), cue.Final(), cue.Optional(true), cue.Definitions(true), cue.Hidden(true))
	// Strip @test decl attributes from any struct literals in the exported
	// syntax tree.  v.Syntax() may carry over source-level attributes, which
	// must not appear in the formatted expected-value expression.
	stripTestAttrs(syn)
	// Strip all comments from the AST.  Error nodes (e.g. _|_) carry line
	// comments like "// path: error message" from v.Syntax(); if these appear
	// inside an @test(eq, ...) attribute body, the // sequence is parsed as a
	// CUE comment, which would consume the closing ) and corrupt the attribute.
	stripComments(syn)
	b, err := format.Node(syn, format.Simplify())
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(b)
}

// stripComments removes all comment groups from every node in the AST.
// Error nodes produced by v.Syntax() carry line comments like
// "// path: error message"; if left in, the // sequence inside an
// @test(eq, ...) attribute body would be parsed as a CUE comment and
// consume the closing ), corrupting the attribute syntax.
func stripComments(node ast.Node) {
	ast.Walk(node, func(n ast.Node) bool {
		ast.SetComments(n, nil)
		return true
	}, nil)
}

// stripTestAttrs removes @test(...) decl-level attributes from all struct
// literals in the AST node.  This prevents source-level test annotations from
// leaking into @test(eq, ...) expected-value expressions written by CUE_UPDATE.
func stripTestAttrs(node ast.Node) {
	ast.Walk(node, func(n ast.Node) bool {
		sl, ok := n.(*ast.StructLit)
		if !ok {
			return true
		}
		elts := sl.Elts[:0]
		for _, e := range sl.Elts {
			if a, ok := e.(*ast.Attribute); ok {
				k, _ := a.Split()
				if k == "test" {
					continue
				}
			}
			elts = append(elts, e)
		}
		sl.Elts = elts
		return true
	}, nil)
}

// eqBodySupportedDirectives lists the @test directive names that are
// intentionally processed by astCmp when they appear inside an @test(eq, ...)
// body (as field-level attributes or struct-level decl attributes).
// Any other directive has no effect there.
var eqBodySupportedDirectives = map[string]bool{
	"final":      true, // field-level and struct-level: resolve default before comparing
	"ignore":     true, // field-level: skip eq descent; field need not exist
	"err":        true, // field-level: check that value is an error
	"shareID":    true, // field-level: sharing assertion (handled by extractShareIDsFromEqExpr)
	"checkOrder": true, // struct-level decl: require fields in declaration order
}

// reportEqBodyTestAttrs walks the expected expression of an @test(eq, ...)
// body and reports any @test field attributes that have no effect there.
// Directives listed in eqBodySupportedDirectives are intentionally processed
// by astCmp and are excluded from the error.
func reportEqBodyTestAttrs(t testing.TB, path cue.Path, expr ast.Node) {
	t.Helper()
	ast.Walk(expr, func(n ast.Node) bool {
		f, ok := n.(*ast.Field)
		if !ok {
			return true
		}
		for _, a := range f.Attrs {
			k, _ := a.Split()
			if k != "test" {
				continue
			}
			pa, err := parseTestAttr(a)
			if err != nil {
				continue
			}
			if eqBodySupportedDirectives[pa.directive] {
				continue
			}
			t.Errorf("path %s: @test(%s) in @test(eq, ...) body has no effect; place it as a field attribute on the actual value", path, pa.directive)
		}
		return true
	}, nil)
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

// runDebugCheckInline checks the debug printer output of val against the
// expected string in the @test(debugCheck, "...") attribute.
// When CUE_UPDATE modes are active, enqueues a write-back.
func (r *inlineRunner) runDebugCheckInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	name := pa.raw.Fields[0].Value() // preserves any :vN version suffix
	if len(pa.raw.Fields) < 2 {
		// Empty @test(debugCheck) — fill placeholder.
		if cuetest.UpdateGoldenFiles {
			actual := r.debugPrinterOutput(val)
			r.enqueueInlineFill(pa, r.formatDebugAttr(name, actual, pa))
		}
		return
	}
	expected := pa.raw.Fields[1].Value()
	actual := r.debugPrinterOutput(val)
	match := normalizeLines(actual) == normalizeLines(expected)
	if match && !cuetest.ForceUpdateGoldenFiles {
		return
	}
	if cuetest.ForceUpdateGoldenFiles || cuetest.UpdateGoldenFiles {
		r.enqueueInlineFill(pa, r.formatDebugAttr(name, actual, pa))
		return
	}
	if !match {
		t.Errorf("path %s: @test(debugCheck) mismatch:\ngot:  %q\nwant: %q", path, actual, expected)
		logHint(t, pa.hint)
	}
}

// runDebugOutputInline captures the debug printer output of val as an
// informational annotation (@test(debug, ...)).  Unlike debugCheck, a
// mismatch does not fail the test — it only logs and auto-updates when
// CUE_UPDATE is active.
func (r *inlineRunner) runDebugOutputInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	name := pa.raw.Fields[0].Value() // preserves any :vN version suffix
	actual := r.debugPrinterOutput(val)
	if len(pa.raw.Fields) < 2 {
		// Empty @test(debug) — fill placeholder.
		if cuetest.UpdateGoldenFiles {
			r.enqueueInlineFill(pa, r.formatDebugAttr(name, actual, pa))
		}
		return
	}
	expected := pa.raw.Fields[1].Value()
	match := normalizeLines(actual) == normalizeLines(expected)
	if match && !cuetest.ForceUpdateGoldenFiles {
		return
	}
	// Always auto-update on mismatch (informational, not an assertion).
	if cuetest.ForceUpdateGoldenFiles || cuetest.UpdateGoldenFiles {
		r.enqueueInlineFill(pa, r.formatDebugAttr(name, actual, pa))
		return
	}
	if !match {
		t.Logf("path %s: @test(debug) changed:\ngot:  %q\nwant: %q", path, actual, expected)
	}
}

// formatDebugAttr returns the @test(name, ...) attribute text for a debug value.
func (r *inlineRunner) formatDebugAttr(name, actual string, pa parsedTestAttr) string {
	actual = strings.TrimRight(actual, "\n")
	n := strings.Count(r.attrLineIndent(pa), "\t")
	actual = literal.String.WithOptionalTabIndent(n + 1).Quote(actual)
	return fmt.Sprintf("@test(%s, %s)", name, actual)
}

// attrLineIndent returns the leading whitespace on the source line that
// contains pa's @test attribute. Used to compute the indentation level
// for multi-line debug attribute values.
func (r *inlineRunner) attrLineIndent(pa parsedTestAttr) string {
	offset := pa.srcAttr.Pos().Offset()
	for _, f := range r.archive.Files {
		if f.Name != pa.srcFileName {
			continue
		}
		data := f.Data
		start := offset
		for start > 0 && data[start-1] != '\n' {
			start--
		}
		end := start
		for end < offset && data[end] == '\t' {
			end++
		}
		return string(data[start:end])
	}
	return ""
}

// debugPrinterOutput returns the standard debug-printer representation of val,
// equivalent to what appears in out/eval golden sections.
// Absolute file paths from module-aware loading are normalized to relative.
func (r *inlineRunner) debugPrinterOutput(val cue.Value) string {
	c := val.Core()
	if c.V == nil {
		return ""
	}
	out := debug.NodeString(c.R, c.V, nil)
	if r.dir != "" {
		out = strings.ReplaceAll(out, filepath.ToSlash(r.dir)+"/", "")
	}
	return out
}

// normalizeLines trims trailing whitespace from each line and strips any
// trailing blank lines, for use in debug: textual comparison.
func normalizeLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// attrHasSkip reports whether the raw attribute body contains a skip:<ver> arg
// at position 2 or later.  Returns the version string (e.g. "v3") and true
// when a skip arg is found; returns "", false otherwise.
func attrHasSkip(raw internal.Attr) (ver string, ok bool) {
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
			fieldPath := applyShareIDAt(basePath.Append(cue.Label(f.Label)), pa)
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
