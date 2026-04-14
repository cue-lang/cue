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

// This file implements @test attribute parsing, AST extraction, and inline-mode
// detection. These components are self-contained and medium-risk: they parse
// CUE source and attribute syntax but do not interact with the evaluator.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
)

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
	raw *internal.Attr

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
func parseTestAttr(astAttr *ast.Attribute) (parsedTestAttr, error) {
	if name := astAttr.Name(); name != "test" {
		return parsedTestAttr{}, fmt.Errorf("not a @test attribute: @%s", name)
	}

	attr := internal.ParseAttr(astAttr)
	if attr.Err != nil {
		return parsedTestAttr{}, attr.Err
	}

	result := parsedTestAttr{
		raw:     attr,
		srcAttr: astAttr,
	}

	if len(attr.Fields) == 0 || (len(attr.Fields) == 1 && attr.Fields[0] == internal.KeyValue{}) {
		// @test() — empty placeholder or bare marker.
		result.directive = ""
		return result, nil
	}

	// The first field determines the directive.
	// Case 1: key=value form like desc="hello", shareID=name — directive is the key.
	// Case 2: positional form like eq, err, kind — directive (with optional :vN suffix) is the value.
	f0 := attr.Fields[0]
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
		ea, err := parseErrArgs(attr)
		if err != nil {
			return result, err
		}
		result.errArgs = &ea
	}

	// Extract universal flags and reject unknown key= flags.
	// Positional args (kv.Key() == "") are accepted by directives as needed.
	// Directives with their own flag parsers (err, todo, skip, shareID) are
	// responsible for validating their own flags.
	for _, kv := range attr.Fields[1:] {
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

// attrExtractor holds state for the extractTestAttrs walk.
type attrExtractor struct {
	records  []attrRecord
	hidPkg   string
	fileName string
	// visited tracks all @test attribute nodes that have been seen by the
	// walker, whether or not they were added to records. Any @test attr in
	// the file that is not in visited after the walk is unreachable and is
	// reported as an error.
	visited map[*ast.Attribute]bool
}

// processAttr handles a single @test attribute: marks it visited, parses it,
// and appends an attrRecord (or an error record on parse failure).
func (ex *attrExtractor) processAttr(a *ast.Attribute, path cue.Path, isDeclAttr, fileLevel bool, baseLine int) {
	ex.visited[a] = true
	pa, err := parseTestAttr(a)
	if err != nil {
		ex.records = append(ex.records, attrRecord{
			path:       path,
			parsed:     parsedTestAttr{srcAttr: a, srcFileName: ex.fileName},
			parseErr:   err,
			isDeclAttr: isDeclAttr,
			fileLevel:  fileLevel,
		})
		return
	}
	pa.baseLine = baseLine
	pa.srcFileName = ex.fileName
	ex.records = append(ex.records, attrRecord{
		path:       path,
		parsed:     pa,
		isDeclAttr: isDeclAttr,
		fileLevel:  fileLevel,
	})
}

// walkField records @test field attrs, then recurses into the field's
// struct value (if any). Attributes are NOT stripped from the AST so that
// the evaluated value is built from the original source; CUE ignores
// attributes during evaluation, so positions reference original source lines.
func (ex *attrExtractor) walkField(field *ast.Field, path cue.Path) {
	fieldBaseLine := field.Pos().Line()

	for _, a := range field.Attrs {
		k, _ := a.Split()
		if k != "test" {
			continue
		}
		ex.processAttr(a, path, false, false, fieldBaseLine)
	}

	expr := field.Value
	if alias, ok := expr.(*ast.Alias); ok {
		expr = alias.Expr
	}
	if sl, ok := expr.(*ast.StructLit); ok {
		ex.walkStruct(sl, path)
	}
	// Non-struct field values (e.g. binary exprs) are not recursed into.
	// Any @test attrs inside them will be caught by the unreachable-attr check.
}

// walkStruct records @test decl attrs and recurses into all sub-fields.
func (ex *attrExtractor) walkStruct(sl *ast.StructLit, path cue.Path) {
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
			ex.processAttr(e, path, true, false, structBaseLine)

		case *ast.Field:
			subPath := appendPath(path, e.Label, ex.hidPkg)
			if subPath.Err() == nil {
				ex.walkField(e, subPath)
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
					// Mark as seen regardless of whether it is added to records,
					// so the unreachable-attr check does not flag it as an error.
					ex.visited[a] = true
					pa, err := parseTestAttr(a)
					if err != nil {
						ex.records = append(ex.records, attrRecord{
							path:     path,
							parsed:   parsedTestAttr{srcAttr: a, srcFileName: ex.fileName},
							parseErr: err,
						})
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
					pa.srcFileName = ex.fileName
					// Store the parent path; applyShareIDAt in
					// collectDirectShareIDs appends the at= selector.
					ex.records = append(ex.records, attrRecord{
						path:   path,
						parsed: pa,
					})
				}
			}

			// EmbedDecl and other element types are not recursed into.
			// Any @test attrs inside them will be caught by the unreachable-attr check.
		}
	}
}

// extractTestAttrs walks ast.File and:
//  1. Collects all @test(...) attributes from field attrs, struct decl attrs,
//     and file-level decl attrs.
//  2. Reports any @test attrs not reached by the walk as errors. This catches
//     attributes in unreachable positions (e.g. inside binary-expression operands
//     or bare embedding expressions).
//  3. Preserves all non-@test attributes.
//
// Returns the collected records.
// File-level decl attributes produce records with an empty path and
// fileLevel=true; these check the entire file's evaluated value.
func extractTestAttrs(f *ast.File, fileName string) []attrRecord {
	// hidPkg is the package scope for hidden fields in this file.
	// Inline-compiled sources use ":" + pkgname; anonymous files use "_".
	hidPkg := ":" + f.PackageName()
	if hidPkg == ":" {
		hidPkg = "_"
	}

	ex := &attrExtractor{
		hidPkg:   hidPkg,
		fileName: fileName,
		visited:  make(map[*ast.Attribute]bool),
	}

	// Handle file-level decl attributes (@test as a top-level declaration).
	for _, decl := range f.Decls {
		if a, ok := decl.(*ast.Attribute); ok {
			k, _ := a.Split()
			if k == "test" {
				ex.processAttr(a, cue.Path{}, false, true, a.Pos().Line())
			}
		}
	}

	// Walk top-level fields.
	for _, decl := range f.Decls {
		if d, ok := decl.(*ast.Field); ok {
			fieldPath := appendPath(cue.Path{}, d.Label, ex.hidPkg)
			if fieldPath.Err() == nil {
				ex.walkField(d, fieldPath)
			}
		}
	}

	// Report any @test attrs in the file not visited by the walk above.
	// These are unreachable and would be silently ignored without this check.
	ast.Walk(f, func(n ast.Node) bool {
		a, ok := n.(*ast.Attribute)
		if !ok {
			return true
		}
		k, _ := a.Split()
		if k != "test" || ex.visited[a] {
			return true
		}
		ex.records = append(ex.records, attrRecord{
			path:     cue.Path{},
			parsed:   parsedTestAttr{srcAttr: a, srcFileName: fileName},
			parseErr: fmt.Errorf("@test attribute is not reachable by the test runner; @test is only valid on top-level fields, fields inside struct literals, or as decl attributes in a struct"),
		})
		return true
	}, nil)

	return ex.records
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

// labelSelector returns the cue.Selector for an AST label, correctly handling
// hidden fields (_foo) in two contexts:
//
//   - Source-file context: pass hidPkg = ":" + f.PackageName() (or "_" for
//     anonymous packages). The package scope is applied to all hidden idents.
//   - Eq-body context: pass hidPkg = "". The caller signals that the name may
//     carry a $pkg suffix (e.g. "_foo$mypkg"), which is stripped and converted
//     to ":" + pkgname. Without a suffix, "_" is used (anonymous hidden field).
func labelSelector(label ast.Label, hidPkg string) cue.Selector {
	// Unwrap field aliases (e.g. A="foo=bar" or B=bb): the alias binds a name
	// to the field value; the actual label is the aliased expression.
	if alias, ok := label.(*ast.Alias); ok {
		if l, ok := alias.Expr.(ast.Label); ok {
			label = l
		}
	}
	if ident, ok := label.(*ast.Ident); ok && internal.IsHidden(ident.Name) {
		name := ident.Name
		pkg := hidPkg
		if pkg == "" {
			if i := strings.IndexByte(name, '$'); i >= 0 {
				pkg = ":" + name[i+1:]
				name = name[:i]
			} else {
				pkg = "_"
			}
		}
		return cue.Hid(name, pkg)
	}
	return cue.Label(label)
}

// appendPath appends a selector for label to path.
// hidPkg is forwarded to labelSelector; see its documentation.
//
// NOTE: a fresh slice is allocated intentionally so that multiple calls with
// the same base do not share the same backing array. cue.Path.Append reuses
// excess capacity, so callers that store the result and then append again from
// the same base would silently overwrite each other's stored paths.
func appendPath(base cue.Path, label ast.Label, hidPkg string) cue.Path {
	sels := base.Selectors()
	fresh := make([]cue.Selector, len(sels)+1)
	copy(fresh, sels)
	fresh[len(sels)] = labelSelector(label, hidPkg)
	return cue.MakePath(fresh...)
}

// parseAtPath parses an at= selector string into a cue.Path.
// Unlike cue.ParsePath, it handles:
//   - Hidden field names with a $pkg qualifier, e.g. "_foo$pkg" →
//     cue.Hid("_foo", ":pkg"), matching the syntax used inside @test(eq, ...)
//     bodies.
//   - Integer segments as list-index selectors, e.g. "items.0" →
//     [items, Index(0)].
//
// directiveKey returns the deduplication key for a directive. Two directives
// with the same name but different at= values are independent assertions and
// must both survive deduplication in selectActiveDirectives.
func directiveKey(pa parsedTestAttr) string {
	for i := 1; i < len(pa.raw.Fields); i++ {
		if kv := pa.raw.Fields[i]; kv.Key() == "at" {
			return pa.directive + "\x00" + kv.Value()
		}
	}
	return pa.directive
}

// Dotted paths are split on "." and each segment is processed independently,
// so "a._foo$pkg.0" works correctly.
func parseAtPath(at string) (cue.Path, error) {
	// Fast path: no hidden fields and no integers — delegate directly.
	if !strings.Contains(at, "_") && !strings.ContainsAny(at, "0123456789") {
		p := cue.ParsePath(at)
		return p, p.Err()
	}
	var sels []cue.Selector
	for _, seg := range strings.Split(at, ".") {
		if internal.IsHidden(seg) {
			name := seg
			pkg := "_"
			if i := strings.IndexByte(name, '$'); i >= 0 {
				pkg = ":" + name[i+1:]
				name = name[:i]
			}
			sels = append(sels, cue.Hid(name, pkg))
		} else if n, err := strconv.Atoi(seg); err == nil {
			sels = append(sels, cue.Index(n))
		} else {
			p := cue.ParsePath(seg)
			if err := p.Err(); err != nil {
				return cue.Path{}, err
			}
			sels = append(sels, p.Selectors()...)
		}
	}
	return cue.MakePath(sels...), nil
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
		if p, err := parseAtPath(val); err == nil {
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
