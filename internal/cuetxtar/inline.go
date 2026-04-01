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
// @test(eq, VALUE) may appear in two positions:
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
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
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

// posSpec is an expected error position. Two forms are supported:
//
// Relative form (fileName == ""): the position is expressed as a signed line
// delta from the @test attribute's line (0 = same line) and a 1-indexed
// column. Written as deltaLine:col (e.g. "0:5", "-2:13"). Using a delta keeps
// the assertion stable when lines are added or removed above the test.
//
// Absolute form (fileName != ""): the position is in a different file, given
// as a filename, absolute 1-indexed line number, and 1-indexed column. Written
// as filename:absLine:col (e.g. "fixture.cue:3:5"). This form is used for
// errors whose source location is in a shared fixture file.
type posSpec struct {
	// fileName is non-empty for cross-file (absolute) positions.
	fileName string
	// deltaLine is the signed offset from the @test line; used when fileName == "".
	deltaLine int
	// absLine is the absolute 1-indexed line number; used when fileName != "".
	absLine int
	// col is the 1-indexed column number (used in both forms).
	col int
}

// errArgs holds parsed sub-options from an @test(err, ...) directive.
type errArgs struct {
	// codes holds the acceptable error codes, e.g. ["cycle"] or ["cycle", "incomplete"].
	// A single code= value is stored as a one-element slice; code=(a|b) as two.
	// An empty slice means any error code is accepted.
	codes []string
	// contains is a substring the error message must contain.
	contains string
	// any requires any descendant of the annotated field to have the error.
	any bool
	// paths lists specific paths (relative to test-case root) where the error
	// must occur. Populated when path=(...) is present.
	paths []string
	// pos lists expected error positions as (deltaLine:col) pairs relative to
	// the line containing the @test attribute.
	pos []posSpec
	// posSet is true when pos= was
	// explicitly provided (including pos=[] to assert no positions).
	posSet bool
}

// matchesCode reports whether the given error code satisfies the codes
// constraint. An empty codes slice means any code is accepted.
func (ea *errArgs) matchesCode(got string) bool {
	if len(ea.codes) == 0 {
		return true
	}
	return slices.Contains(ea.codes, got)
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
		// Key-based directives may carry a version suffix: "shareID:v3" → directive="shareID", version="v3".
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

	return result, nil
}

// parseErrArgs extracts err sub-options from an already-parsed Attr.
// The attribute body is expected to start with "err" as the first positional arg.
func parseErrArgs(a internal.Attr) (errArgs, error) {
	var ea errArgs
	// Start from index 1 (index 0 is "err").
	for _, kv := range a.Fields[1:] {
		switch {
		case kv.Key() == "code":
			codes, err := parseParenList(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, code=...): %w", err)
			}
			ea.codes = codes
		case kv.Key() == "contains":
			ea.contains = kv.Value()
		case kv.Key() == "" && kv.Value() == "any":
			ea.any = true
		case kv.Key() == "path":
			paths, err := parseParenList(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, path=...): %w", err)
			}
			ea.paths = paths
		case kv.Key() == "pos":
			specs, err := parsePosSpecs(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, pos=...): %w", err)
			}
			ea.pos = specs
			ea.posSet = true
		}
	}
	return ea, nil
}

// parseParenList parses a balanced parenthesized pipe-separated list like
// (path1|path2|path3), returning ["path1","path2","path3"].
// The input may or may not include the outer parentheses.
func parseParenList(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts, nil
}

// parsePosSpecs parses a pos= value into a slice of posSpec.
// The value must be enclosed in square brackets; elements are whitespace-separated.
// Two element forms are supported:
//
//   - deltaLine:col — relative position on the same file (one colon).
//     deltaLine is a signed offset from the @test attribute's line (0 = same line).
//   - filename:absLine:col — absolute position in another file (two colons).
//     absLine is the 1-indexed line in the named file.
func parsePosSpecs(s string) ([]posSpec, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("pos= value must be enclosed in square brackets, got %q", s)
	}
	s = s[1 : len(s)-1]
	var specs []posSpec
	for _, p := range strings.Fields(s) {
		parts := strings.SplitN(p, ":", 3)
		switch len(parts) {
		case 2:
			// Relative form: deltaLine:col
			deltaLine, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			colNum, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			specs = append(specs, posSpec{deltaLine: deltaLine, col: colNum})
		case 3:
			// Absolute form: filename:absLine:col
			fileName := parts[0]
			absLine, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			colNum, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, fmt.Errorf("invalid pos spec %q: %w", p, err)
			}
			specs = append(specs, posSpec{fileName: fileName, absLine: absLine, col: colNum})
		default:
			return nil, fmt.Errorf("invalid pos spec %q: expected deltaLine:col or filename:line:col", p)
		}
	}
	return specs, nil
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

	// extraNewlines tracks the cumulative count of newlines embedded inside
	// @test attribute bodies that have been stripped from preceding fields.
	// When the CUE scanner reads a multiline attribute body (e.g. pos=[0:5\n1:5])
	// it advances its line counter, shifting subsequent AST node positions upward.
	// After stripping and reformatting, those embedded newlines are gone, so we
	// subtract the running total to recover the effective line in the formatted
	// output.
	extraNewlines := 0

	// walkField strips @test field attrs from field, records them, then recurses
	// into the field's struct value (if any).
	var walkField func(field *ast.Field, path cue.Path)

	// walkStruct strips @test decl attrs from sl, records them, and recurses
	// into all sub-fields.
	var walkStruct func(sl *ast.StructLit, path cue.Path)

	walkField = func(field *ast.Field, path cue.Path) {
		// Effective line of this field in the stripped-and-formatted output.
		fieldBaseLine := field.Pos().Line() - extraNewlines

		// Strip @test field attrs and record them.
		var keep []*ast.Attribute
		for _, a := range field.Attrs {
			k, body := a.Split()
			if k != "test" {
				keep = append(keep, a)
				continue
			}
			pa, err := parseTestAttr(a)
			if err != nil {
				// Keep on parse error; runner will report it.
				keep = append(keep, a)
				continue
			}
			pa.baseLine = fieldBaseLine
			pa.srcFileName = fileName
			records = append(records, attrRecord{
				path:   path,
				parsed: pa,
			})
			// Newlines inside a stripped attr body shift subsequent line numbers.
			extraNewlines += strings.Count(body, "\n")
		}
		field.Attrs = keep

		if sl, ok := field.Value.(*ast.StructLit); ok {
			walkStruct(sl, path)
		}
	}

	walkStruct = func(sl *ast.StructLit, path cue.Path) {
		// Capture the struct's {-relative base line for decl @test pos= specs.
		// Captured before processing elements so it stays stable even if earlier
		// elements in this struct strip lines. File-level @test attrs (no braces)
		// use their own line as baseLine instead (see below).
		structBaseLine := sl.Lbrace.Line() - extraNewlines

		// Strip decl attrs, recurse into sub-fields.
		var newElts []ast.Decl
		for _, elt := range sl.Elts {
			switch e := elt.(type) {
			case *ast.Attribute:
				k, body := e.Split()
				if k != "test" {
					newElts = append(newElts, elt)
					continue
				}
				pa, err := parseTestAttr(e)
				if err != nil {
					newElts = append(newElts, elt)
					continue
				}
				pa.baseLine = structBaseLine
				pa.srcFileName = fileName
				records = append(records, attrRecord{
					path:       path,
					parsed:     pa,
					isDeclAttr: true,
				})
				// The entire decl attr line is stripped (+1), plus any
				// embedded newlines in the body shift subsequent lines.
				extraNewlines += 1 + strings.Count(body, "\n")
				// Stripped — not added to newElts.

			case *ast.Field:
				subPath := appendPath(path, e.Label)
				walkField(e, subPath)
				newElts = append(newElts, elt)

			default:
				newElts = append(newElts, elt)
			}
		}
		sl.Elts = newElts
	}

	// Handle file-level decl attributes (@test as a top-level declaration).
	var newDecls []ast.Decl
	for _, decl := range f.Decls {
		if a, ok := decl.(*ast.Attribute); ok {
			k, body := a.Split()
			if k == "test" {
				pa, err := parseTestAttr(a)
				if err != nil {
					newDecls = append(newDecls, decl)
					continue
				}
				pa.baseLine = a.Pos().Line() - extraNewlines
				pa.srcFileName = fileName
				records = append(records, attrRecord{
					path:      cue.Path{},
					parsed:    pa,
					fileLevel: true,
				})
				extraNewlines += 1 + strings.Count(body, "\n")
				// Stripped — not added to newDecls.
				continue
			}
		}
		newDecls = append(newDecls, decl)
	}
	f.Decls = newDecls

	for _, decl := range f.Decls {
		field, ok := decl.(*ast.Field)
		if !ok {
			continue
		}
		fieldPath := cue.MakePath(cue.Label(field.Label))
		walkField(field, fieldPath)
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

// todoCapture wraps *testing.T and captures failures without propagating them.
// It is used for @test(todo) XFAIL mode: all directives run, but failures are
// logged rather than reported as test errors.
//
// todoCapture embeds *testing.T to satisfy the testing.TB interface (via the
// promoted unexported private() method). Only Errorf and Error are overridden;
// all other methods delegate to the embedded T.
type todoCapture struct {
	*testing.T
	failed bool
	msgs   strings.Builder
}

func (c *todoCapture) Error(args ...any) {
	c.failed = true
	fmt.Fprintln(&c.msgs, args...)
}

func (c *todoCapture) Errorf(format string, args ...any) {
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
		cap := &todoCapture{T: t}
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

	// Parse and extract @test attrs from all CUE files.
	cueFiles, allRecords, err := r.parseAndExtract()
	if err != nil {
		r.t.Fatalf("inline: failed to parse CUE files: %v", err)
		return
	}
	r.cueFiles = cueFiles

	// Check for #subpath restriction.
	subpath := r.subpath()

	// Build and evaluate the stripped CUE.
	// Note: val.Err() may be non-nil if sub-fields are erroneous; this is
	// intentional for tests that assert errors. We only fatal on compile errors.
	ctx := r.cueContext()
	val, compileErr := r.buildValue(ctx, cueFiles)
	if compileErr != nil {
		r.t.Fatalf("inline: CUE compile error: %v", compileErr)
		return
	}

	// Collect file-level records (decl @test attrs at file scope).
	var fileLevelRecords []attrRecord
	for _, rec := range allRecords {
		if rec.fileLevel {
			fileLevelRecords = append(fileLevelRecords, rec)
		}
	}

	// Determine which top-level fields are test-case roots.
	// A field is a root if it has any @test attribute (other than file-level).
	// Fields with no @test attributes are silently skipped (fixture fields).
	rootNames := make(map[cue.Selector]bool)
	for _, rec := range allRecords {
		if rec.fileLevel {
			continue // file-level records are handled separately
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
	for _, f := range cueFiles {
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
//   - CUE_UPDATE=diff: fails if the section is stale, showing a diff
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

// cueFileResult holds the parsed and stripped AST for one CUE file.
type cueFileResult struct {
	name        string
	strippedAST *ast.File
	// hasTestAttrs is true when the file contained at least one @test attribute.
	// Files where this is false are treated as fixture files: they are still
	// compiled into the evaluated value (so references from other files work)
	// but their top-level fields are NOT required to carry @test directives.
	hasTestAttrs bool
}

// parseAndExtract parses all .cue files, extracts @test attrs, and returns:
// - the stripped AST files
// - all attrRecords
func (r *inlineRunner) parseAndExtract() ([]*cueFileResult, []attrRecord, error) {
	var results []*cueFileResult
	var allRecords []attrRecord

	for _, f := range r.archive.Files {
		if !strings.HasSuffix(f.Name, ".cue") {
			continue
		}
		af, err := parser.ParseFile(f.Name, f.Data, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", f.Name, err)
		}
		records := extractTestAttrs(af, f.Name)
		allRecords = append(allRecords, records...)

		results = append(results, &cueFileResult{
			name:         f.Name,
			strippedAST:  af,
			hasTestAttrs: len(records) > 0,
		})
	}
	return results, allRecords, nil
}

// buildValue compiles and evaluates the stripped CUE files using the test context.
// Returns a compile-level error (parse/compile failure), not evaluation errors.
// Evaluation errors in sub-fields are returned as bottom values within the
// returned cue.Value and are intentional for tests asserting errors.
//
// Fixture files (hasTestAttrs==false) are compiled first and collected into a
// scope value; test files are compiled with that scope so that references from
// test files to fixture-file fields resolve correctly.
func (r *inlineRunner) buildValue(ctx *cue.Context, cueFiles []*cueFileResult) (cue.Value, error) {
	combined := ctx.CompileString("{}")

	// First pass: compile fixture files (no @test attrs).
	// Their fields form the scope for test files.
	fixtureScope := ctx.CompileString("{}")
	for _, cf := range cueFiles {
		if cf.hasTestAttrs {
			continue
		}
		fmtBytes, err := format.Node(cf.strippedAST)
		if err != nil {
			return cue.Value{}, fmt.Errorf("format %s: %w", cf.name, err)
		}
		v := ctx.CompileBytes(fmtBytes, cue.Filename(cf.name))
		if v.BuildInstance() == nil && v.Err() != nil {
			return cue.Value{}, v.Err()
		}
		fixtureScope = fixtureScope.Unify(v)
		combined = combined.Unify(v)
	}

	// Second pass: compile test files with fixture scope so that
	// cross-file references to fixture fields resolve correctly.
	for _, cf := range cueFiles {
		if !cf.hasTestAttrs {
			continue // already compiled above
		}
		fmtBytes, err := format.Node(cf.strippedAST)
		if err != nil {
			return cue.Value{}, fmt.Errorf("format %s: %w", cf.name, err)
		}
		v := ctx.CompileBytes(fmtBytes, cue.Filename(cf.name), cue.Scope(fixtureScope))
		// Distinguish syntax/compile errors from evaluation errors.
		// BuildInstance() returns nil only when there is a syntax error;
		// evaluation errors produce a non-nil instance with a bottom value.
		if v.BuildInstance() == nil && v.Err() != nil {
			return cue.Value{}, v.Err()
		}
		combined = combined.Unify(v)
	}
	return combined, nil
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
	for _, line := range strings.Split(string(r.archive.Comment), "\n") {
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
		if rec.path.String() != path.String() {
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
	case "debugOutput":
		r.runDebugOutputInline(t, path, val, pa)
	case "todo":
		// @test(todo) is handled at the loop level in runInline; no-op here.
	case "permute":
		// Handled by permute-group collection in runInline; no-op here.
	case "permuteCount":
		// Handled by checkPermuteCount after permutations run; no-op here.
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
//   - passing assertion with skip+diff: remove stale skip (UpdateGoldenFiles)
//   - failing assertion: force-overwrite (ForceUpdateGoldenFiles) or
//     regression-guard with skip:<ver>+diff annotation (UpdateGoldenFiles)
func (r *inlineRunner) runEqInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	// @test(eq:todo, X) — expected-to-fail form.
	// Failures are logged but not reported as test errors; a match emits a warning.
	if pa.isTodo {
		if len(pa.raw.Fields) < 2 {
			return
		}
		exprStr := pa.raw.Fields[1].Text()
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
	if len(pa.raw.Fields) < 2 {
		// Empty @test(eq) — fill placeholder.
		if cuetest.UpdateGoldenFiles {
			r.enqueueInlineFill(pa, "@test(eq, "+r.formatValue(val)+")")
		}
		return
	}
	exprStr := pa.raw.Fields[1].Text()
	expr, err := parser.ParseExpr("@test(eq)", exprStr)
	if err != nil {
		t.Errorf("path %s: @test(eq, ...): cannot parse expected expression: %v", path, err)
		return
	}

	// Detect stale-skip: a prior regression guard annotated this attr with
	// skip:<ver> and diff="...".
	_, hasSkip := attrHasSkip(pa.raw)

	cmpErr := (&cmpCtx{baseLine: 0}).astCmp(cue.Path{}, expr, val)
	if cmpErr == nil {
		// Assertion passes via AST comparison.
		if hasSkip && cuetest.UpdateGoldenFiles {
			// Stale-skip cleanup (task 11.7): the assertion now passes; strip
			// the skip and diff args, restoring the plain @test(eq, <expr>).
			r.enqueueInlineFill(pa, "@test(eq, "+exprStr+")")
		}
		return
	}

	// Comparison failed — genuine mismatch.
	if cuetest.ForceUpdateGoldenFiles {
		// Force overwrite (task 11.4): replace expected with actual.
		r.enqueueInlineFill(pa, "@test(eq, "+r.formatValue(val)+")")
		return
	}
	if cuetest.UpdateGoldenFiles && !hasSkip {
		// Regression guard (tasks 11.2-11.3): annotate with skip:<ver>+diff
		// so the test keeps passing under CUE_UPDATE without silent overwrite.
		got := r.formatValue(val)
		diffStr := fmt.Sprintf("got %s; want %s", got, exprStr)
		escaped := strings.ReplaceAll(diffStr, `"`, `\"`)
		ver := r.versionName()
		if ver == "" {
			ver = "unknown"
		}
		r.enqueueInlineFill(pa, fmt.Sprintf(`@test(eq, %s, skip:%s, diff="%s")`, exprStr, ver, escaped))
		return
	}
	// Report the failure (unless already annotated with a skip).
	if !hasSkip {
		t.Errorf("path %s: %v", path, cmpErr)
	}
}

// formatValue returns a human-readable CUE string for a value.
func (r *inlineRunner) formatValue(v cue.Value) string {
	syn := v.Syntax(cue.All(), cue.Docs(true), cue.Final())
	if syn == nil {
		return fmt.Sprintf("%v", v)
	}
	b, err := format.Node(syn)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// runErrAssertion checks that an error is present at val, applying sub-options.
func (r *inlineRunner) runErrAssertion(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	ea := pa.errArgs
	if ea == nil {
		// Bare @test(err) — just check that the value is an error.
		if !r.isError(val) {
			t.Errorf("path %s: expected error, got non-error value", path)
		}
		return
	}

	if ea.any {
		// @test(err, any, ...) — check that any descendant has the error.
		found := r.findDescendantError(val, ea)
		if !found {
			t.Errorf("path %s: expected a descendant error with code=%v, none found", path, ea.codes)
		}
		return
	}

	if !r.isError(val) {
		t.Errorf("path %s: expected error, got non-error value", path)
		return
	}

	// Validate error code.
	if len(ea.codes) > 0 {
		gotCode := r.errorCode(val)
		if !ea.matchesCode(gotCode) {
			t.Errorf("path %s: expected error code %v, got %q", path, ea.codes, gotCode)
		}
	}
	// Validate error message contains.
	if ea.contains != "" {
		msg := r.errorMessage(val)
		if !strings.Contains(msg, ea.contains) {
			t.Errorf("path %s: expected error message to contain %q, got %q", path, ea.contains, msg)
		}
	}
	// Validate error positions.
	if ea.posSet {
		r.checkErrPositions(t, path, val, pa)
	}
}

// checkErrPositions verifies that the error positions on val match the pos=
// spec in pa.  When positions don't match:
//   - pos=[] (placeholder): update on CUE_UPDATE=1.
//   - pos=[non-empty]: update on CUE_UPDATE=force only.
func (r *inlineRunner) checkErrPositions(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	err := val.Err()
	if err == nil {
		t.Errorf("path %s: @test(err, pos=...): value has no error", path)
		return
	}
	positions := cueerrors.Positions(err)
	expected := pa.errArgs.pos

	match := len(positions) == len(expected)
	if match {
		for i, exp := range expected {
			got := positions[i]
			if exp.fileName != "" {
				// Absolute form: match filename + absolute line + column.
				if got.Filename() != exp.fileName || got.Line() != exp.absLine || got.Column() != exp.col {
					match = false
					break
				}
			} else {
				// Relative form: match line delta from @test + column.
				if got.Line() != pa.baseLine+exp.deltaLine || got.Column() != exp.col {
					match = false
					break
				}
			}
		}
	}
	if match {
		return
	}

	// pos=[] is a fill-in placeholder: update with CUE_UPDATE=1.
	// pos=[non-empty] that is wrong: update only with CUE_UPDATE=force.
	isPlaceholder := len(expected) == 0
	if (isPlaceholder && cuetest.UpdateGoldenFiles) || cuetest.ForceUpdateGoldenFiles {
		r.enqueuePosWrite(pa, positions)
		return
	}

	if len(positions) != len(expected) {
		t.Errorf("path %s: @test(err, pos=...): got %d position(s), want %d", path, len(positions), len(expected))
		for i, p := range positions {
			t.Logf("  actual[%d]: %d:%d", i, p.Line(), p.Column())
		}
		return
	}
	for i, exp := range expected {
		got := positions[i]
		if exp.fileName != "" {
			if got.Filename() != exp.fileName || got.Line() != exp.absLine || got.Column() != exp.col {
				t.Errorf("path %s: @test(err, pos=...): position[%d]: got %s:%d:%d, want %s:%d:%d",
					path, i, got.Filename(), got.Line(), got.Column(), exp.fileName, exp.absLine, exp.col)
			}
		} else {
			wantLine := pa.baseLine + exp.deltaLine
			if got.Line() != wantLine || got.Column() != exp.col {
				t.Errorf("path %s: @test(err, pos=...): position[%d]: got %d:%d, want %d:%d",
					path, i, got.Line(), got.Column(), wantLine, exp.col)
			}
		}
	}
}

// posWrite records a pending pos= attribute update for CUE_UPDATE write-back.
type posWrite struct {
	fileName    string // archive .cue file name, e.g. "in.cue"
	attrOffset  int    // byte offset of the @test attr in the original file data
	attrLen     int    // byte length of the original @test attr text
	newAttrText string // replacement attribute text with updated pos=[...]
}

// enqueuePosWrite formats positions as pos specs and enqueues a write-back
// that replaces the pos=[...] value in the source attribute.
//
// Positions in the same file as the @test attribute are written as deltaLine:col
// (relative to pa.baseLine).  Positions in a different file are written as
// filename:absLine:col (absolute).
func (r *inlineRunner) enqueuePosWrite(pa parsedTestAttr, positions []token.Pos) {
	parts := make([]string, len(positions))
	for i, p := range positions {
		if p.Filename() == "" || p.Filename() == pa.srcFileName {
			parts[i] = fmt.Sprintf("%d:%d", p.Line()-pa.baseLine, p.Column())
		} else {
			parts[i] = fmt.Sprintf("%s:%d:%d", p.Filename(), p.Line(), p.Column())
		}
	}
	newPosStr := strings.Join(parts, " ")

	old := pa.srcAttr.Text
	start := strings.Index(old, "pos=[")
	if start < 0 {
		return
	}
	bracket := start + len("pos=[")
	end := strings.Index(old[bracket:], "]")
	if end < 0 {
		return
	}
	end += bracket + 1 // include the "]"
	newAttrText := old[:start] + "pos=[" + newPosStr + "]" + old[end:]

	r.pendingPosWrites = append(r.pendingPosWrites, posWrite{
		fileName:    pa.srcFileName,
		attrOffset:  pa.srcAttr.Pos().Offset(),
		attrLen:     len(pa.srcAttr.Text),
		newAttrText: newAttrText,
	})
}

// applyPosWritebacks writes pending pos= attribute updates to the archive file.
// Replacements are applied by byte offset from end to start so that earlier
// offsets remain valid after each substitution.
func (r *inlineRunner) applyPosWritebacks() {
	if len(r.pendingPosWrites) == 0 || r.filePath == "" {
		return
	}
	changed := false
	for i, f := range r.archive.Files {
		var writes []posWrite
		for _, pw := range r.pendingPosWrites {
			if pw.fileName == f.Name {
				writes = append(writes, pw)
			}
		}
		if len(writes) == 0 {
			continue
		}
		// Sort descending by offset so earlier offsets stay valid.
		slices.SortFunc(writes, func(a, b posWrite) int {
			return b.attrOffset - a.attrOffset
		})
		data := append([]byte(nil), f.Data...)
		for _, pw := range writes {
			end := pw.attrOffset + pw.attrLen
			if end > len(data) {
				continue
			}
			data = append(data[:pw.attrOffset:pw.attrOffset],
				append([]byte(pw.newAttrText), data[end:]...)...)
			changed = true
		}
		r.archive.Files[i].Data = data
	}
	if changed {
		out := txtar.Format(r.archive)
		if err := os.WriteFile(r.filePath, out, 0o644); err != nil {
			r.t.Errorf("inline: pos write-back to %s: %v", r.filePath, err)
		}
	}
}

// isError reports whether val is an error value (bottom).
func (r *inlineRunner) isError(val cue.Value) bool {
	core := val.Core()
	if core.V == nil {
		return false
	}
	return core.V.Bottom() != nil
}

// errorCode returns the string code of the error at val, or "" if not an error.
func (r *inlineRunner) errorCode(val cue.Value) string {
	core := val.Core()
	if core.V == nil {
		return ""
	}
	b := core.V.Bottom()
	if b == nil {
		return ""
	}
	return b.Code.String()
}

// errorMessage returns the human-readable error message for val.
func (r *inlineRunner) errorMessage(val cue.Value) string {
	if err := val.Err(); err != nil {
		return err.Error()
	}
	return ""
}

// findDescendantError walks val looking for any descendant with an error
// matching ea. Returns true if found.
func (r *inlineRunner) findDescendantError(val cue.Value, ea *errArgs) bool {
	if r.isError(val) {
		if ea.matchesCode(r.errorCode(val)) {
			return true
		}
	}
	// Walk fields.
	iter, err := val.Fields()
	if err != nil {
		return false
	}
	for iter.Next() {
		if r.findDescendantError(iter.Value(), ea) {
			return true
		}
	}
	return false
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
	r.runLeqAssertion(t, path, val, constraint)
}

// runLeqAssertion asserts that val is subsumed by constraint (constraint ⊑ val, i.e. val is at least as specific).
func (r *inlineRunner) runLeqAssertion(t testing.TB, path cue.Path, val, constraint cue.Value) {
	t.Helper()
	if err := constraint.Subsume(val); err != nil {
		t.Errorf("path %s: @test(leq): value %v is not subsumed by constraint %v: %v", path, val, constraint, err)
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
	for _, ks := range strings.Split(expectedStr, "|") {
		k := parseKindStr(strings.TrimSpace(ks))
		if k == cue.BottomKind {
			t.Errorf("path %s: @test(kind=%q): unknown kind %q", path, expectedStr, ks)
			return
		}
		expectedKind |= k
	}
	if gotKind != expectedKind {
		t.Errorf("path %s: @test(kind=%s): got kind %v, want %v", path, expectedStr, gotKind, expectedKind)
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
	}
}

// runDebugCheckInline checks the debug printer output of val against the
// expected string in the @test(debugCheck, "...") attribute.
// When CUE_UPDATE modes are active, enqueues a write-back.
func (r *inlineRunner) runDebugCheckInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	if len(pa.raw.Fields) < 2 {
		// Empty @test(debugCheck) — fill placeholder.
		if cuetest.UpdateGoldenFiles {
			actual := r.debugPrinterOutput(val)
			escaped := strings.ReplaceAll(actual, `"`, `\"`)
			r.enqueueInlineFill(pa, fmt.Sprintf(`@test(debugCheck, """%s""")`, escaped))
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
		escaped := strings.ReplaceAll(actual, `"`, `\"`)
		r.enqueueInlineFill(pa, fmt.Sprintf(`@test(debugCheck, """%s""")`, escaped))
		return
	}
	if !match {
		t.Errorf("path %s: @test(debugCheck) mismatch:\ngot:  %q\nwant: %q", path, actual, expected)
	}
}

// runDebugOutputInline captures the debug printer output of val as an
// informational annotation.  Unlike debugCheck, a mismatch does not fail the
// test — it only logs and auto-updates when CUE_UPDATE is active.
func (r *inlineRunner) runDebugOutputInline(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	actual := r.debugPrinterOutput(val)
	if len(pa.raw.Fields) < 2 {
		// Empty @test(debugOutput) — fill placeholder.
		if cuetest.UpdateGoldenFiles {
			escaped := strings.ReplaceAll(actual, `"`, `\"`)
			r.enqueueInlineFill(pa, fmt.Sprintf(`@test(debugOutput, """%s""")`, escaped))
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
		escaped := strings.ReplaceAll(actual, `"`, `\"`)
		r.enqueueInlineFill(pa, fmt.Sprintf(`@test(debugOutput, """%s""")`, escaped))
		return
	}
	if !match {
		t.Logf("path %s: @test(debugOutput) changed:\ngot:  %q\nwant: %q", path, actual, expected)
	}
}

// debugPrinterOutput returns the standard debug-printer representation of val,
// equivalent to what appears in out/eval golden sections.
func (r *inlineRunner) debugPrinterOutput(val cue.Value) string {
	c := val.Core()
	if c.V == nil {
		return ""
	}
	return debug.NodeString(c.R, c.V, nil)
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
