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

// This file implements value formatting for @test(eq, ...) bodies and
// @test(debug, ...) / @test(debugCheck, ...) output capture.
// Changes here are low-risk: their effect is immediately visible in test
// output and golden files.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/value"
)

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

// eqCompactThreshold is the maximum byte length of a compact struct expression
// before eqWriteValue switches to multi-line (one field per line) form.
const eqCompactThreshold = 40

// formatValue returns a human-readable CUE string for a value.
// Short values are returned compact (single line). Struct values whose compact
// form exceeds eqCompactThreshold are returned in multi-line form with
// recursive indentation; eqFillAttrStr handles re-indentation relative to the
// source attribute line.
func (r *inlineRunner) formatValue(v cue.Value) string {
	var b strings.Builder
	eqWriteValue(value.OpContext(v), &b, v, "\t")
	return b.String()
}

// eqWriteValue writes a CUE value to b in @test(eq, ...) body notation.
//
// nestedIndent controls multi-line formatting for struct values: when non-empty,
// a struct whose compact form exceeds eqCompactThreshold is written in
// multi-line form — each field prefixed by "\n"+nestedIndent, the closing "}"
// by "\n"+nestedIndent[:-1]. Nested struct values receive nestedIndent+"\t" so
// every level of nesting gains one extra tab. When nestedIndent is empty the
// value is always written compact (used internally for disjuncts, conjuncts,
// and nested field values that already fit in the threshold).
//
// Compared with v.Syntax() + cue.Final() + format.Node:
//   - Hidden fields use _foo$pkg notation (matching astcmp.go conventions).
//   - adt.Disjunction values are emitted as *d1 | d2 (with * for defaults)
//     instead of being collapsed to the default by cue.Final().
//   - adt.Conjunction values are emitted as c1 & c2.
func eqWriteValue(opCtx *adt.OpContext, b *strings.Builder, v cue.Value, nestedIndent string) {
	tv := v.Core()
	vx := tv.V.DerefValue()

	switch bv := vx.BaseValue.(type) {
	case *adt.Disjunction:
		eqWriteDisjunction(opCtx, b, bv)
		return
	case *adt.Conjunction:
		eqWriteConjunction(opCtx, b, bv)
		return
	}

	// Use struct emission if the kind is struct OR if there are arcs — the
	// latter handles error vertices that still carry child fields (e.g. a
	// struct with one bad field: the parent vertex is _|_ but its arcs hold
	// the successfully-evaluated sibling fields).
	// Lists also have arcs (integer-indexed elements) but must fall through
	// to the standard syntax formatter so they render as [v1, v2] not {0: v1}.
	k := v.IncompleteKind()
	if (k == cue.StructKind || len(vx.Arcs) > 0) && k != cue.ListKind {
		if nestedIndent != "" {
			// Multi-line mode: use compact if it fits, else recurse one level deeper.
			var compact strings.Builder
			eqWriteStruct(opCtx, &compact, vx, "")
			if compact.Len() <= eqCompactThreshold {
				b.WriteString(compact.String())
				return
			}
		}
		eqWriteStruct(opCtx, b, vx, nestedIndent)
		return
	}

	// Error/incomplete values (e.g. int + 3 where int is abstract) — emit _|_.
	// This avoids the confusing let-containing struct that v.Syntax(Final())
	// generates when it tries to make the expression self-contained.
	// astCmp requires _|_ to match only error values.
	// eqWriteStruct adds the @test(err, ...) annotation after _|_ for field arcs.
	if v.Err() != nil {
		b.WriteString("_|_")
		return
	}

	// Scalar values and lists: fall back to the standard syntax formatter.
	// cue.Final() resolves defaults and avoids _#def wrapping.
	syn := v.Syntax(cue.Docs(false), cue.Final(), cue.Optional(true), cue.Raw())
	stripComments(syn)
	bs, err := format.Node(syn, format.Simplify())
	if err != nil {
		fmt.Fprintf(b, "%#v", v)
	} else {
		b.Write(bs)
	}
}

// eqWriteDisjunction emits disjuncts as *d1 | d2 | d3 (defaults first with *).
func eqWriteDisjunction(opCtx *adt.OpContext, b *strings.Builder, dj *adt.Disjunction) {
	for i, v := range dj.Values {
		if i > 0 {
			b.WriteString(" | ")
		}
		if i < dj.NumDefaults {
			b.WriteByte('*')
		}
		eqWriteValue(opCtx, b, value.Make(opCtx, v), "")
	}
}

// eqWriteConjunction emits conjuncts as c1 & c2 & c3.
func eqWriteConjunction(opCtx *adt.OpContext, b *strings.Builder, conj *adt.Conjunction) {
	for i, v := range conj.Values {
		if i > 0 {
			b.WriteString(" & ")
		}
		eqWriteValue(opCtx, b, value.Make(opCtx, v), "")
	}
}

// isLeafError reports whether eqWriteValue would render v as bare _|_
// (i.e. an error that is neither a struct with child arcs nor a list).
// Used by eqWriteStruct to decide whether to append a @test(err, ...) annotation.
func isLeafError(v cue.Value) bool {
	if v.Err() == nil {
		return false
	}
	tv := v.Core()
	vx := tv.V.DerefValue()
	k := v.IncompleteKind()
	return !((k == cue.StructKind || len(vx.Arcs) > 0) && k != cue.ListKind)
}

// eqWriteErrAnnotation appends a @test(err, code=..., contains="...", pos=[])
// annotation for a leaf error arc value.  code= and contains= are filled with
// the actual error details; pos=[] is a placeholder that CUE_UPDATE=1 fills in.
func eqWriteErrAnnotation(b *strings.Builder, v cue.Value) {
	tv := v.Core()
	if tv.V == nil {
		return
	}
	bot := tv.V.DerefValue().Bottom()
	if bot == nil {
		return
	}
	b.WriteString(" @test(err, code=")
	b.WriteString(bot.Code.String())
	if bot.Err != nil {
		fmt.Fprintf(b, ", contains=%q", bot.Err.Error())
	}
	b.WriteString(", pos=[])")
}

// eqWriteStruct emits a struct using _foo$pkg notation for hidden-field labels.
//
// nestedIndent controls layout:
//   - ""  (compact): fields are separated by ", ".
//   - non-empty: each field is preceded by "\n"+nestedIndent; the closing "}"
//     is preceded by "\n"+nestedIndent[:-1] (one tab less). Field values
//     receive nestedIndent+"\t" so they can recurse one level deeper.
func eqWriteStruct(opCtx *adt.OpContext, b *strings.Builder, vx *adt.Vertex, nestedIndent string) {
	b.WriteByte('{')
	first := true
	for _, arc := range vx.Arcs {
		if arc.ArcType == adt.ArcNotPresent || arc.Label.IsLet() {
			continue
		}
		// Skip hidden fields from external packages: their PkgID is a full
		// module path (e.g. "mod.test/pkg") which cannot be encoded as a
		// valid CUE identifier in the $pkg suffix notation.
		if arc.Label.IsHidden() {
			pkg := arc.Label.PkgID(opCtx)
			if pkg != "_" && !strings.HasPrefix(pkg, ":") {
				continue
			}
		}
		if nestedIndent != "" {
			b.WriteString("\n" + nestedIndent)
		} else if !first {
			b.WriteString(", ")
		}
		first = false
		eqWriteLabel(opCtx, b, arc.Label, arc.ArcType)
		b.WriteString(": ")
		arcVal := value.Make(opCtx, arc)
		eqWriteValue(opCtx, b, arcVal, nestedIndent+"\t")
		// For leaf error fields, append a @test(err, ...) annotation with
		// actual error details and a pos=[] placeholder for CUE_UPDATE=1.
		if isLeafError(arcVal) {
			eqWriteErrAnnotation(b, arcVal)
		}
	}
	if nestedIndent != "" && !first {
		b.WriteString("\n" + nestedIndent[:len(nestedIndent)-1])
	}
	b.WriteByte('}')
}

// eqWriteLabel writes a field label.
// For hidden labels the $pkg qualifier is included when the field is
// package-scoped (pkg != "_"). Callers must ensure the label's PkgID is
// either "_" or colon-prefixed (inline package) before calling — see
// the skip guard in eqWriteStruct.
func eqWriteLabel(opCtx *adt.OpContext, b *strings.Builder, f adt.Feature, arcType adt.ArcType) {
	if f.IsHidden() {
		name := f.IdentString(opCtx)
		pkg := f.PkgID(opCtx)
		if pkg != "_" {
			// PkgID returns ":pkgname" for inline sources; convert to "$pkgname".
			b.WriteString(name + "$" + strings.TrimPrefix(pkg, ":"))
		} else {
			b.WriteString(name)
		}
	} else {
		b.WriteString(f.SelectorString(opCtx))
	}
	b.WriteString(arcType.Suffix())
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
