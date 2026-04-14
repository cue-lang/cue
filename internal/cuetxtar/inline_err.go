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

// This file contains all error-assertion logic for the inline test runner:
// types, parsing, matching, position checking, and write-back for
// @test(err, ...) and @test(err, suberr=(...)) directives.

package cuetxtar

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

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
	// at is a relative CUE path string (e.g. "a.b") to navigate to from the
	// annotated field before checking the error. Allows asserting errors in
	// sub-fields that cannot be directly annotated.
	at string
	// path is the expected dotted CUE path reported by the error itself
	// (cueerrors.Error.Path() joined with "."). This may differ from at=: at=
	// navigates where we check, while path= asserts what the error reports as
	// its own location. An empty string means no path check is performed.
	path string
	// pos lists expected error positions as (deltaLine:col) pairs relative to
	// the line containing the @test attribute.
	pos []posSpec
	// posSet is true when pos= was explicitly provided (including pos=[] to
	// assert no positions).
	posSet bool
	// suberrs holds expected sub-error specs for multi-error (list) values.
	// Each entry is matched order-independently against errors.Errors(val.Err()).
	suberrs []*errArgs
	// msgArgs holds expected fmt.Sprint representations of Msg() args to check
	// order-independently against the error's Msg() arguments.
	msgArgs []string
	// srcAttrText is the raw text of the @test(err,...) attribute as it
	// appears in source (e.g. "@test(err, code=eval, pos=[])").  Set only for
	// @test(err) attributes that appear inside an @test(eq, {...}) body; used
	// by cmpErr to locate and rewrite a pos=[] placeholder in the outer
	// @test(eq,...) attribute text.
	srcAttrText string
}

// matchesCode reports whether the given error code satisfies the codes
// constraint. An empty codes slice means any code is accepted.
func (ea *errArgs) matchesCode(got string) bool {
	if len(ea.codes) == 0 {
		return true
	}
	return slices.Contains(ea.codes, got)
}

// errCodeStr returns the string representation of val's error code,
// or "" if val is not an error value.
func errCodeStr(val cue.Value) string {
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

// checkIsErr returns nil when val is an error value (bottom), or a descriptive
// error when it is not. Used by both runErrAssertion and cmpErr.
func checkIsErr(val cue.Value) error {
	core := val.Core()
	if core.V == nil {
		return fmt.Errorf("@test(err): value has no vertex")
	}
	if core.V.Bottom() == nil {
		return fmt.Errorf("@test(err): expected error, got non-error value")
	}
	return nil
}

// validateErrProperties checks the error properties (code, contains, …) of val
// against ea. It assumes val is already known to be an error value (call
// checkIsErr first). Adding a new check here automatically covers all callers:
// runErrAssertion and cmpErr.
//
// Returns the first failing check's error, or nil when all pass.
func (ea *errArgs) validateErrProperties(val cue.Value) error {
	if len(ea.codes) > 0 {
		gotCode := errCodeStr(val)
		if !ea.matchesCode(gotCode) {
			return fmt.Errorf("expected error code %v, got %q", ea.codes, gotCode)
		}
	}
	if ea.contains != "" {
		msg := valErr(val).Error()
		if !ea.containsMatch(msg, valErr(val)) {
			return fmt.Errorf("expected error message to contain %q, got %q", ea.contains, msg)
		}
	}
	return nil
}

// containsMatch reports whether ea.contains appears in the rendered error
// message or, if err implements cueerrors.Error, in the raw Msg() format
// string. Checking the format string allows contains= written as the
// literal format (e.g. "conflicting values %s and %s") to match even when
// the rendered message has the args substituted.
func (ea *errArgs) containsMatch(msg string, err error) bool {
	if strings.Contains(msg, ea.contains) {
		return true
	}
	var ce cueerrors.Error
	if errors.As(err, &ce) {
		format, _ := ce.Msg()
		return strings.Contains(format, ea.contains)
	}
	return false
}

// posUpdate records a pending pos=[...] replacement within a suberr=(...) group.
type posUpdate struct {
	expIdx    int         // 0-based index of the suberr=(...) group in the attribute
	positions []token.Pos // actual positions to write
}

// posWrite is an alias for inlineFillWrite, used for pos= attribute updates.
type posWrite = inlineFillWrite

// nestedPosEntry accumulates pos=[] fill-ins for nested @test(err) attributes
// inside a single outer @test(eq, {...}) attribute. Multiple fills for the same
// outer attribute are merged sequentially so only one write is produced.
type nestedPosEntry struct {
	fileName    string
	attrOffset  int
	attrLen     int    // byte length of the original outer attribute
	currentText string // attr text with fills applied so far
}

// parseErrArgs extracts err sub-options from an already-parsed Attr.
// The attribute body is expected to start with "err" as the first positional arg.
func parseErrArgs(a *internal.Attr) (errArgs, error) {
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
		case kv.Key() == "at":
			ea.at = kv.Value()
		case kv.Key() == "path":
			ea.path = kv.Value()
		case kv.Key() == "pos":
			specs, err := parsePosSpecs(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, pos=...): %w", err)
			}
			ea.pos = specs
			ea.posSet = true
		case kv.Key() == "suberr":
			raw := strings.TrimSpace(kv.Value())
			inner, err := trimSurrounding(raw, '(', ')')
			if err != nil {
				return ea, fmt.Errorf("@test(err, suberr=...): %w", err)
			}
			// Reuse parseErrArgs by building a synthetic "err, <inner>" attr body.
			syntheticAttr := internal.ParseAttr(&ast.Attribute{
				Text: fmt.Sprintf("@test(err, %s)", inner),
			})
			subEA, err := parseErrArgs(syntheticAttr)
			if err != nil {
				return ea, fmt.Errorf("@test(err, suberr=...): %w", err)
			}
			ea.suberrs = append(ea.suberrs, &subEA)
		case kv.Key() == "args":
			args, err := parseArgsList(kv.Value())
			if err != nil {
				return ea, fmt.Errorf("@test(err, args=...): %w", err)
			}
			ea.msgArgs = args
		case kv.Key() == "hint":
			// hint= is a universal flag handled at the parsedTestAttr level; skip here.
		case kv.Key() == "p":
			// p= is a universal priority flag handled at the parsedTestAttr level; skip here.
		case kv.Key() == "":
			// Positional arg (e.g. "any"); already handled above.
			if v := strings.TrimSpace(kv.Value()); strings.HasPrefix(v, "suberr(") {
				return ea, fmt.Errorf("@test(err): %q looks like suberr=(...) with missing '='; use suberr=(...)", v)
			}
		default:
			return ea, fmt.Errorf("@test(err): unknown flag %q", kv.Key())
		}
	}
	return ea, nil
}

// trimSurrounding checks that s is wrapped in the given left and right
// delimiter bytes and returns the inner content. It returns an error if
// either delimiter is missing.
func trimSurrounding(s string, left, right byte) (string, error) {
	if len(s) < 2 || s[0] != left || s[len(s)-1] != right {
		return "", fmt.Errorf("expected %c...%c, got %q", left, right, s)
	}
	return s[1 : len(s)-1], nil
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
// The value must be enclosed in square brackets; elements are comma-separated.
// Two element forms are supported:
//
//   - deltaLine:col — relative position on the same file (one colon).
//     deltaLine is a signed offset from the @test attribute's line (0 = same line).
//   - filename:absLine:col — absolute position in another file (two colons).
//     absLine is the 1-indexed line in the named file.
func parsePosSpecs(s string) ([]posSpec, error) {
	s = strings.TrimSpace(s)
	inner, err := trimSurrounding(s, '[', ']')
	if err != nil {
		return nil, fmt.Errorf("error parsing pos= value: %w", err)
	}
	s = inner
	var specs []posSpec
	for p := range strings.SplitSeq(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
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

// parseArgsList parses a bracket-enclosed, comma-separated list of arg strings,
// e.g. `["list", "int"]` → ["list", "int"].
// Tokens wrapped in double quotes are unquoted via strconv.Unquote so that the
// stored form (produced by formatErrMsg using strconv.Quote) round-trips back to
// the original string that fmt.Sprint of the Msg() argument would produce.
func parseArgsList(s string) ([]string, error) {
	inner, err := trimSurrounding(s, '[', ']')
	if err != nil {
		return nil, fmt.Errorf("error parsing args: %w", err)
	}
	var result []string
	for tok := range strings.SplitSeq(inner, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			result = append(result, tok)
		}
	}
	return result, nil
}

// matchesErrSpec reports whether act satisfies all discriminating constraints
// in ea. It is a pure predicate — it never calls t.Errorf.
//
// Position specs are used for discrimination only when len(ea.pos) > 0.
// An empty pos=[] placeholder does not influence matching; use
// checkSubErrPositions to validate positions after a match is found.
//
// code= is not checked here because cueerrors.Error does not expose adt.Code
// directly; code checking is done at the cue.Value level in runErrAssertion.
func (r *inlineRunner) matchesErrSpec(act cueerrors.Error, ea *errArgs, baseLine int, fieldPath string) bool {
	if ea.contains != "" && !ea.containsMatch(act.Error(), act) {
		return false
	}
	if ea.path != "" {
		gotPath := strings.Join(act.Path(), ".")
		if relativePathTo(gotPath, fieldPath) != ea.path && gotPath != ea.path {
			return false
		}
	}
	// Only use pos for discrimination when it is non-empty. An empty pos=[]
	// is a placeholder; positions are validated separately by checkSubErrPositions.
	if ea.posSet && len(ea.pos) > 0 {
		positions := positionsFromSingleError(act)
		if !posSpecsMatch(positions, ea.pos, baseLine, r.relFilename) {
			return false
		}
	}
	return true
}

// runErrAssertion checks that an error is present at val, applying sub-options.
func (r *inlineRunner) runErrAssertion(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	// @test(err:todo, ...) — expected-to-fail form.
	// Failures are logged but not reported as test errors; a pass emits a warning.
	if pa.isTodo {
		suffix := ""
		if pa.todoPriority != "" {
			suffix = fmt.Sprintf(" p=%s", pa.todoPriority)
		}
		cap := &failCapture{TB: t}
		pa2 := pa
		pa2.isTodo = false
		r.runErrAssertion(cap, path, val, pa2)
		if cap.failed {
			t.Logf("TODO err:todo (still failing)%s: %s\n%s", suffix, path, cap.msgs.String())
		} else {
			t.Logf("WARNING: TODO err:todo now passes for %s — consider upgrading to @test(err, ...)", path)
		}
		return
	}
	ea := pa.errArgs
	// Bare @test(err): no arguments beyond the "err" directive keyword itself.
	// parseErrArgs always returns a non-nil errArgs, so check the field count.
	if ea == nil || (pa.raw != nil && len(pa.raw.Fields) == 1) {
		// Bare @test(err) — check that the value is an error.
		if err := checkIsErr(val); err != nil {
			t.Errorf("path %s: %v", path, err)
			logHint(t, pa.hint)
			return
		}
		if cuetest.UpdateGoldenFiles {
			r.enqueueInlineFill(pa, r.formatErrFillAttr(val, pa, path.String(), ""))
		}
		return
	}

	if ea.at != "" {
		// @test(err, at=<path>, ...) — navigate to sub-path then check error.
		subPath, err := parseAtPath(ea.at)
		if err != nil {
			t.Errorf("path %s: @test(err, at=%s): invalid path: %v", path, ea.at, err)
			return
		}
		subVal := val.LookupPath(subPath)
		if !subVal.Exists() {
			t.Errorf("path %s: @test(err, at=%s): sub-path not found", path, ea.at)
			return
		}
		subFullPath := cue.MakePath(append(path.Selectors(), subPath.Selectors()...)...)
		subPA := pa
		subPA.errArgs = &errArgs{
			codes:    ea.codes,
			contains: ea.contains,
			any:      false, // don't cascade any= to the sub-check
			path:     ea.path,
			posSet:   ea.posSet,
			pos:      ea.pos,
			suberrs:  ea.suberrs,
			msgArgs:  ea.msgArgs,
			// at is intentionally omitted: we already navigated to the sub-path.
		}
		// Bare @test(err, at=<path>): only at= present, no other constraints.
		// Fill from the sub-path error when CUE_UPDATE=1.
		if ea.isBareAt() && cuetest.UpdateGoldenFiles && pa.srcAttr != nil {
			if checkIsErr(subVal) == nil {
				r.enqueueInlineFill(pa, r.formatErrFillAttrAt(subVal, pa, ea.at))
			}
		}
		r.runErrAssertion(t, subFullPath, subVal, subPA)
		return
	}

	if ea.any {
		// @test(err, any, ...) — check that any descendant has the error.
		if ea.posSet {
			t.Errorf("path %s: @test(err, any, pos=...): pos= is not supported with any", path)
			return
		}
		found := r.findDescendantError(val, ea)
		if !found {
			t.Errorf("path %s: expected a descendant error with code=%v, none found", path, ea.codes)
			logHint(t, pa.hint)
		}
		return
	}

	if err := checkIsErr(val); err != nil {
		t.Errorf("path %s: %v", path, err)
		logHint(t, pa.hint)
		return
	}
	// Validate error properties (code, contains, …).
	if err := ea.validateErrProperties(val); err != nil {
		t.Errorf("path %s: @test(err): %v", path, err)
		logHint(t, pa.hint)
	}
	// Validate error path (cueerrors.Error.Path joined with ".", relative to the annotated field).
	var e cueerrors.Error
	if ea.path != "" && errors.As(val.Err(), &e) {
		gotPath := strings.Join(e.Path(), ".")
		gotRel := relativePathTo(gotPath, path.String())
		if gotRel != ea.path && gotPath != ea.path {
			t.Errorf("path %s: @test(err, path=...): expected error path %q, got %q", path, ea.path, gotRel)
			logHint(t, pa.hint)
		}
	}
	// Validate Msg() args (order-independent).
	if len(ea.msgArgs) > 0 && errors.As(val.Err(), &e) {
		checkMsgArgs(t, path, e, ea.msgArgs, "@test(err, args=...)", pa.hint)
	}
	// Validate error positions.
	if ea.posSet {
		r.checkErrPositions(t, path, val, pa)
	}
	// Validate sub-errors.
	if len(ea.suberrs) > 0 {
		r.checkSubErrors(t, path, val, ea, pa)
	}
}

// checkSubErrors verifies that the sub-errors of a multi-error value at val
// match the suberr=(...) specs in ea.
//
// Matching is two-pass and order-independent:
//   - Pass 1 (exact): specs with non-empty pos= are matched first — they
//     uniquely identify a sub-error even when multiple errors share a contains
//     substring.
//   - Pass 2 (contains): remaining specs (pos=[] or no pos=) are matched by
//     contains= against the still-unmatched actual errors.
//
// After matching, positions are validated for each pair. pos=[] is a
// placeholder that triggers writeback when CUE_UPDATE=1.
//
// When the error is a failed disjunction, CUE prepends a summary entry of
// the form "N errors in empty disjunction:" to the list; checkSubErrors
// detects and skips this header.
func (r *inlineRunner) checkSubErrors(t testing.TB, path cue.Path, val cue.Value, ea *errArgs, pa parsedTestAttr) {
	fieldPath := path.String()
	t.Helper()
	all := cueerrors.Errors(val.Err())
	// Skip the disjunction header entry if present.
	actual := all
	if len(all) > 1 && isDisjunctionHeader(all[0]) {
		actual = all[1:]
	}
	expected := ea.suberrs

	if len(actual) != len(expected) {
		t.Errorf("path %s: @test(err, suberr=...): got %d sub-error(s), want %d",
			path, len(actual), len(expected))
		for i, a := range actual {
			t.Logf("  actual[%d]: %s", i, a.Error())
		}
		logHint(t, pa.hint)
		return
	}

	// matchedPair records an (actual, expected) pairing together with expIdx —
	// the 0-based index of the expected spec within ea.suberrs, which is the
	// same as its ordinal position among suberr=(...) groups in the source
	// attribute text (needed for pos= writeback).
	type matchedPair struct {
		act    cueerrors.Error
		exp    *errArgs
		expIdx int
	}

	usedAct := make([]bool, len(actual))
	expMatched := make([]bool, len(expected))
	var pairs []matchedPair

	// Pass 1 — exact: specs with non-empty pos= matched first.
	// These specs uniquely identify an error even when contains substrings overlap.
	for i, exp := range expected {
		if !exp.posSet || len(exp.pos) == 0 {
			continue
		}
		for j, act := range actual {
			if !usedAct[j] && r.matchesErrSpec(act, exp, pa.baseLine, fieldPath) {
				usedAct[j] = true
				expMatched[i] = true
				pairs = append(pairs, matchedPair{act, exp, i})
				break
			}
		}
	}

	// Pass 2 — contains: specs not yet matched (pos=[] or no pos=) matched
	// against remaining actual errors by contains= only.
	// Specs with non-empty pos= are handled by pass 1 and the reporting
	// section below; skip them here to avoid duplicate error messages.
	for i, exp := range expected {
		if expMatched[i] {
			continue
		}
		if exp.posSet && len(exp.pos) > 0 {
			continue
		}
		matched := false
		for j, act := range actual {
			if !usedAct[j] && r.matchesErrSpec(act, exp, pa.baseLine, fieldPath) {
				usedAct[j] = true
				expMatched[i] = true
				matched = true
				pairs = append(pairs, matchedPair{act, exp, i})
				break
			}
		}
		if !matched {
			desc := exp.contains
			if desc == "" {
				desc = fmt.Sprintf("code=%v", exp.codes)
			}
			t.Errorf("path %s: @test(err, suberr=...): no sub-error matched %q", path, desc)
			logHint(t, pa.hint)
		}
	}
	// Report pass-1 specs that also failed to match.
	// When contains= is set, scan all actual errors for a contains-only match
	// and report a position diff; this is more informative than "no match".
	for i, exp := range expected {
		if !expMatched[i] && exp.posSet && len(exp.pos) > 0 {
			if exp.contains == "" {
				t.Errorf("path %s: @test(err, suberr=...): no sub-error matched pos=%v", path, exp.pos)
				logHint(t, pa.hint)
				continue
			}
			found := false
			for _, act := range actual {
				if !strings.Contains(act.Error(), exp.contains) {
					continue
				}
				found = true
				r.reportPosMismatch(t, path, "@test(err, suberr=...)", positionsFromSingleError(act), exp.pos, pa.baseLine, pa.hint)
				break
			}
			if !found {
				t.Errorf("path %s: @test(err, suberr=...): no sub-error matched pos=%v contains=%q",
					path, exp.pos, exp.contains)
				logHint(t, pa.hint)
			}
		}
	}

	// Validate / writeback positions for matched pairs.
	// Collect all placeholder updates and apply them atomically to avoid
	// multiple posWrite entries clobbering each other on the same attribute.
	var posUpdates []posUpdate
	needWriteback := false
	for _, p := range pairs {
		if !p.exp.posSet {
			continue
		}
		positions := positionsFromSingleError(p.act)
		isPlaceholder := len(p.exp.pos) == 0
		if posSpecsMatch(positions, p.exp.pos, pa.baseLine, r.relFilename) {
			continue
		}
		if isPlaceholder && (cuetest.UpdateGoldenFiles || cuetest.ForceUpdateGoldenFiles) {
			posUpdates = append(posUpdates, posUpdate{p.expIdx, positions})
			needWriteback = true
			continue
		}
		if !isPlaceholder && cuetest.ForceUpdateGoldenFiles {
			posUpdates = append(posUpdates, posUpdate{p.expIdx, positions})
			needWriteback = true
			continue
		}
		// Report mismatch.
		r.reportPosMismatch(t, path, "@test(err, suberr=...)", positions, p.exp.pos, pa.baseLine, pa.hint)
	}
	if needWriteback {
		r.enqueueSubErrPosWrites(pa, posUpdates)
	}
	// Validate Msg() args and path for matched pairs (order-independent).
	for _, p := range pairs {
		if p.exp.path != "" {
			gotPath := strings.Join(p.act.Path(), ".")
			gotRel := relativePathTo(gotPath, fieldPath)
			if gotRel != p.exp.path && gotPath != p.exp.path {
				t.Errorf("path %s: @test(err, suberr=...): expected error path %q, got %q", path, p.exp.path, gotRel)
				logHint(t, pa.hint)
			}
		}
		if len(p.exp.msgArgs) > 0 {
			checkMsgArgs(t, path, p.act, p.exp.msgArgs, "@test(err, suberr=...)", pa.hint)
		}
	}
}

// formatPosSpec converts a single token.Pos to a position spec string.
// Positions in the same file are written as deltaLine:col (relative to
// baseLine); positions in other files are written as filename:absLine:col.
// Pass pa.baseLine and pa.srcFileName for top-level @test(err) directives
// (relative delta form).  Pass 0 for baseLine to produce absolute line
// numbers (used for nested @test(err) inside @test(eq) bodies).
func (r *inlineRunner) formatPosSpec(p token.Pos, baseLine int, srcFileName string) string {
	if p.Filename() == "" || r.relFilename(p.Filename()) == srcFileName {
		return fmt.Sprintf("%d:%d", p.Line()-baseLine, p.Column())
	}
	return fmt.Sprintf("%s:%d:%d", r.relFilename(p.Filename()), p.Line(), p.Column())
}

// replacePosSpec replaces the content of the first pos=[...] found in text
// starting at offset with newContent. Returns (newText, true) on success, or
// (text, false) when no pos=[...] bracket pair is found.
func replacePosSpec(text string, offset int, newContent string) (string, bool) {
	prefix, rest, found := strings.Cut(text[offset:], "pos=[")
	if !found {
		return text, false
	}
	_, suffix, found := strings.Cut(rest, "]")
	if !found {
		return text, false
	}
	return text[:offset] + prefix + "pos=[" + newContent + "]" + suffix, true
}

// enqueueNestedPosWrite fills a pos=[] placeholder inside an @test(eq, {...})
// body.  innerAttrText is the raw text of the inner @test(err,...) attribute
// (e.g. "@test(err, code=eval, pos=[])"); it is located by substring search
// within the outer @test(eq,...) attribute text and the pos=[...] within it is
// replaced with the formatted positions.
//
// Multiple calls for the same outer attribute accumulate into nestedPosFills
// (keyed by outer attr byte offset) so that all fills are applied sequentially
// to the same evolving text, rather than each independently overwriting the
// original. applyInlineFillWritebacks drains nestedPosFills into pendingPosWrites.
func (r *inlineRunner) enqueueNestedPosWrite(outerPa parsedTestAttr, innerAttrText string, positions []token.Pos) {
	outerOffset := outerPa.srcAttr.Pos().Offset()

	// Look up or create the accumulator entry for this outer attribute.
	if r.nestedPosFills == nil {
		r.nestedPosFills = make(map[int]*nestedPosEntry)
	}
	entry, ok := r.nestedPosFills[outerOffset]
	if !ok {
		entry = &nestedPosEntry{
			fileName:    outerPa.srcFileName,
			attrOffset:  outerOffset,
			attrLen:     len(outerPa.srcAttr.Text),
			currentText: outerPa.srcAttr.Text,
		}
		r.nestedPosFills[outerOffset] = entry
	}

	// Search for the inner attr text within the current accumulated text so
	// that previously-filled placeholders do not confuse the index.
	innerIdx := strings.Index(entry.currentText, innerAttrText)
	if innerIdx < 0 {
		return // inner attr not found — skip
	}
	// Use baseLine=0: nested pos= specs use absolute line numbers.
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = r.formatPosSpec(p, 0, outerPa.srcFileName)
	}
	newText, replaced := replacePosSpec(entry.currentText, innerIdx, strings.Join(parts, ", "))
	if !replaced {
		return
	}
	entry.currentText = newText
}

func (r *inlineRunner) enqueueSubErrPosWrites(pa parsedTestAttr, updates []posUpdate) {
	newAttrText := pa.srcAttr.Text
	// Apply updates from highest expIdx to lowest so earlier indices stay valid.
	slices.SortFunc(updates, func(a, b posUpdate) int {
		return b.expIdx - a.expIdx
	})
	for _, u := range updates {
		parts := make([]string, len(u.positions))
		for i, p := range u.positions {
			parts[i] = r.formatPosSpec(p, pa.baseLine, pa.srcFileName)
		}
		newPosStr := strings.Join(parts, ", ")
		newAttrText = replaceSuberrPos(newAttrText, u.expIdx, newPosStr)
	}
	r.pendingPosWrites = append(r.pendingPosWrites, posWrite{
		fileName:    pa.srcFileName,
		attrOffset:  pa.srcAttr.Pos().Offset(),
		attrLen:     len(pa.srcAttr.Text),
		newAttrText: newAttrText,
	})
}

// replaceSuberrPos replaces the pos=[...] content in the n-th suberr=(...)
// group within attrText with newPosContent.
func replaceSuberrPos(attrText string, n int, newPosContent string) string {
	// Find the start of the n-th "suberr=(" occurrence.
	pos := 0
	for i := 0; i <= n; i++ {
		idx := strings.Index(attrText[pos:], "suberr=(")
		if idx < 0 {
			return attrText
		}
		if i < n {
			pos += idx + len("suberr=(")
		} else {
			pos += idx
		}
	}
	// Scan past "suberr=(" to find the content end (matching closing paren).
	innerStart := pos + len("suberr=(")
	depth := 1
	end := innerStart
	for end < len(attrText) && depth > 0 {
		switch attrText[end] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth > 0 {
			end++
		}
	}
	// attrText[innerStart:end] is the content inside suberr=(...).
	inner := attrText[innerStart:end]
	newInner, ok := replacePosSpec(inner, 0, newPosContent)
	if !ok {
		return attrText // no pos= in this suberr group
	}
	return attrText[:innerStart] + newInner + attrText[end:]
}

// isDisjunctionHeader reports whether e is the synthetic summary error that
// CUE prepends to a failed-disjunction error list, e.g. "2 errors in empty
// disjunction:". These entries are structural scaffolding and not individual
// disjunct errors.
func isDisjunctionHeader(e cueerrors.Error) bool {
	msg := e.Error()
	// The header always has the form "N errors in empty disjunction:" where N >= 2.
	// Use a simple heuristic: ends with "errors in empty disjunction:".
	return strings.Contains(msg, "errors in empty disjunction:")
}

// positionsFromSingleError extracts the token positions from a single
// cueerrors.Error (primary position first, then input positions sorted).
// Unlike cueerrors.Positions, this works on an individual error rather than
// potentially a list (where cueerrors.Positions only sees the first element).
//
// TODO: is this a bug in cueerrors.Positions()?
func positionsFromSingleError(e cueerrors.Error) []token.Pos {
	var a []token.Pos
	if p := e.Position(); p.File() != nil {
		a = append(a, p)
	}
	sortOffset := len(a)
	for _, p := range e.InputPositions() {
		if p.File() != nil && p != e.Position() {
			a = append(a, p)
		}
	}
	slices.SortFunc(a[sortOffset:], token.Pos.Compare)
	return slices.Compact(a)
}

// TODO: position matching by decomposing token.Pos into filename/line/column is
// a workaround; this should be fixed at the cueerrors API level so callers need
// not manually inspect token.Pos values.

// posSpecsMatch reports whether positions match specs in any order.
// baseLine is the line number of the @test attribute; used to resolve relative specs.
// The runner's relFilename method is needed for absolute specs — pass it as a func.
func posSpecsMatch(positions []token.Pos, specs []posSpec, baseLine int, relFilename func(string) string) bool {
	if len(positions) != len(specs) {
		return false
	}
	used := make([]bool, len(positions))
	for _, exp := range specs {
		matched := false
		for i, got := range positions {
			if used[i] {
				continue
			}
			if posMatchesSpec(got, exp, baseLine, relFilename) {
				used[i] = true
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// posMatchesSpec reports whether a single token.Pos satisfies a posSpec.
func posMatchesSpec(got token.Pos, exp posSpec, baseLine int, relFilename func(string) string) bool {
	if exp.fileName != "" {
		return relFilename(got.Filename()) == exp.fileName && got.Line() == exp.absLine && got.Column() == exp.col
	}
	return got.Line() == baseLine+exp.deltaLine && got.Column() == exp.col
}

// fmtPos formats a token.Pos for display in error messages, including the
// base filename when available: "file.cue:line:col" or "line:col".
func fmtPos(p token.Pos) string {
	if f := p.Filename(); f != "" {
		return fmt.Sprintf("%s:%d:%d", filepath.Base(f), p.Line(), p.Column())
	}
	return fmt.Sprintf("%d:%d", p.Line(), p.Column())
}

// formatPosCountMismatch returns a consistent mismatch message for @test(err,
// pos=...) assertions. When got has extra positions, it explains that this can
// be acceptable after validating relevance.
func formatPosCountMismatch(directive string, got, want int) string {
	if got > want {
		return fmt.Sprintf("%s: got %d position(s), want %d; extra positions are often acceptable, and after confirming they are relevant to this error you can add them to pos=[...]", directive, got, want)
	}
	return fmt.Sprintf("%s: got %d position(s), want %d", directive, got, want)
}

// reportPosMismatch reports a position mismatch between actual positions and
// expected specs. directive is included verbatim in each error message.
// If counts differ, only the count error is reported. Otherwise each
// unmatched expected spec is reported individually (order-independent).
func (r *inlineRunner) reportPosMismatch(t testing.TB, path cue.Path, directive string, positions []token.Pos, specs []posSpec, baseLine int, hint string) {
	t.Helper()
	if len(positions) != len(specs) {
		t.Errorf("path %s: %s", path, formatPosCountMismatch(directive, len(positions), len(specs)))
		for _, p := range positions {
			t.Logf("  actual: %s", fmtPos(p))
		}
		logHint(t, hint)
		return
	}
	used := make([]bool, len(positions))
	for _, exp := range specs {
		matched := false
		for i, got := range positions {
			if used[i] {
				continue
			}
			if posMatchesSpec(got, exp, baseLine, r.relFilename) {
				used[i] = true
				matched = true
				break
			}
		}
		if !matched {
			if exp.fileName != "" {
				t.Errorf("path %s: %s: unmatched position %s:%d:%d; actual positions:", path, directive, exp.fileName, exp.absLine, exp.col)
			} else {
				t.Errorf("path %s: %s: unmatched position %d:%d (spec %d:%d); actual positions:", path, directive, baseLine+exp.deltaLine, exp.col, exp.deltaLine, exp.col)
			}
			for _, p := range positions {
				t.Logf("  actual: %s", fmtPos(p))
			}
			logHint(t, hint)
		}
	}
}

// checkErrPositions verifies that the error positions on val match the pos=

// spec in pa.  When positions don't match:
//   - pos=[] (placeholder): update on CUE_UPDATE=1.
//   - pos=[non-empty]: update on CUE_UPDATE=force only.
func (r *inlineRunner) checkErrPositions(t testing.TB, path cue.Path, val cue.Value, pa parsedTestAttr) {
	t.Helper()
	err := valErr(val)
	if err == nil {
		t.Errorf("path %s: @test(err, pos=...): value has no error", path)
		return
	}
	positions := cueerrors.Positions(err)
	expected := pa.errArgs.pos

	if posSpecsMatch(positions, expected, pa.baseLine, r.relFilename) {
		return
	}

	// pos=[] is a fill-in placeholder: update with CUE_UPDATE=1.
	// pos=[non-empty] that is wrong: update only with CUE_UPDATE=force.
	isPlaceholder := len(expected) == 0
	if (isPlaceholder && cuetest.UpdateGoldenFiles) || cuetest.ForceUpdateGoldenFiles {
		r.enqueuePosWrite(pa, positions)
		return
	}

	r.reportPosMismatch(t, path, "@test(err, pos=...)", positions, expected, pa.baseLine, pa.hint)
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
		parts[i] = r.formatPosSpec(p, pa.baseLine, pa.srcFileName)
	}
	newAttrText, ok := replacePosSpec(pa.srcAttr.Text, 0, strings.Join(parts, ", "))
	if !ok {
		return
	}
	r.pendingPosWrites = append(r.pendingPosWrites, posWrite{
		fileName:    pa.srcFileName,
		attrOffset:  pa.srcAttr.Pos().Offset(),
		attrLen:     len(pa.srcAttr.Text),
		newAttrText: newAttrText,
	})
}

// isError reports whether val is an error value (bottom).
func (r *inlineRunner) isError(val cue.Value) bool {
	return checkIsErr(val) == nil
}

// valErr returns the error for val.  It prefers val.Err() but falls back
// to Core().V.Bottom().Err for values whose incomplete error is stored in
// the raw vertex without being surfaced by the CUE evaluator (e.g. list
// elements produced by list.FlattenN from a list containing an incomplete
// value).
//
// TODO: fix this at the cue.Value API level so that val.Err() surfaces
// incomplete errors from all vertex types, making this fallback unnecessary.
func valErr(val cue.Value) error {
	if err := val.Err(); err != nil {
		return err
	}
	if core := val.Core(); core.V != nil {
		if b := core.V.Bottom(); b != nil {
			return b.Err
		}
	}
	return nil
}

// msgArgsMatch reports whether err's Msg() args include all strings in expected
// (matched via fmt.Sprint, order-independent). Returns true when expected is empty.
func msgArgsMatch(err error, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	var e cueerrors.Error
	if !errors.As(err, &e) {
		return false
	}
	_, actualArgs := e.Msg()
	for _, exp := range expected {
		if !slices.ContainsFunc(actualArgs, func(a any) bool { return fmt.Sprint(a) == exp }) {
			return false
		}
	}
	return true
}

// checkMsgArgs checks that the Msg() args of e include all strings in expected
// (matched via fmt.Sprint, order-independent). directive is used in error messages.
func checkMsgArgs(t testing.TB, path cue.Path, e cueerrors.Error, expected []string, directive string, hint string) {
	t.Helper()
	_, actualArgs := e.Msg()
	for _, exp := range expected {
		if !slices.ContainsFunc(actualArgs, func(a any) bool { return fmt.Sprint(a) == exp }) {
			var actual []string
			for _, a := range actualArgs {
				actual = append(actual, fmt.Sprint(a))
			}
			t.Errorf("path %s: %s: args: expected %q in Msg() args, got %v", path, directive, exp, actual)
			logHint(t, hint)
		}
	}
}

// findDescendantError walks val looking for any descendant with an error
// matching ea (code=, contains=, args=). Returns true if found.
func (r *inlineRunner) findDescendantError(val cue.Value, ea *errArgs) bool {
	if checkIsErr(val) == nil &&
		ea.validateErrProperties(val) == nil &&
		msgArgsMatch(val.Err(), ea.msgArgs) {
		return true
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

// isBareAt reports whether ea has only at= set and no other constraints.
// Used to detect @test(err, at=<path>) that should be auto-filled from the
// sub-path error on CUE_UPDATE=1.
func (ea *errArgs) isBareAt() bool {
	return ea.codes == nil && ea.contains == "" && !ea.any && ea.path == "" &&
		!ea.posSet && ea.suberrs == nil && ea.msgArgs == nil
}

// formatErrFillAttrAt generates a filled @test(err, at=<atPath>, ...) string
// by delegating to formatErrFillAttr and injecting the at= flag immediately
// after the opening "err" keyword.
func (r *inlineRunner) formatErrFillAttrAt(val cue.Value, pa parsedTestAttr, atPath string) string {
	fill := r.formatErrFillAttr(val, pa, "", atPath)
	// fill is "@test(err, ...)" or "@test(err)" (no error case).
	// Insert ", at=<path>" right after "@test(err".
	const marker = "@test(err"
	if !strings.HasPrefix(fill, marker) {
		return fill
	}
	rest := fill[len(marker):]
	// rest starts with ")" or ", ...". Prepend the at= flag.
	if rest == ")" {
		return marker + ", at=" + atPath + ")"
	}
	return marker + ", at=" + atPath + rest
}

// isRedundantPath reports whether epath is redundant given atPath.
// When the at= flag already navigates to a sub-path, the error's own path
// (epath) is a postfix of that sub-path — generating path= would duplicate
// information that at= already encodes. Returns true when atPath is non-empty
// and is a path-boundary-aware suffix of epath:
//
//	isRedundantPath("a.b", "x.a.b") == true   (suffix at "." boundary)
//	isRedundantPath("a.b", "a.b")   == true   (exact match)
//	isRedundantPath("a.b", "xa.b")  == false  (no boundary before "a.b")
//	isRedundantPath("", "a.b")      == false  (no at= present)
func isRedundantPath(atPath, epath string) bool {
	if atPath == "" || epath == "" {
		return false
	}
	return epath == atPath || strings.HasSuffix(epath, "."+atPath)
}

// relativePathTo returns epath relative to fieldPath:
//   - "" if epath == fieldPath (caller should suppress path=)
//   - epath[len(fieldPath)+1:] if epath starts with fieldPath+"."
//   - epath unchanged otherwise (error outside the field's subtree)
func relativePathTo(epath, fieldPath string) string {
	if fieldPath == "" {
		return epath
	}
	if epath == fieldPath {
		return ""
	}
	if strings.HasPrefix(epath, fieldPath+".") {
		return epath[len(fieldPath)+1:]
	}
	return epath
}

// errFillIndent returns the indentation to use for suberr=(...) continuation
// lines in a multi-error fill: the @test attribute's own source-line
// indentation (tabs only) plus one additional tab.
func (r *inlineRunner) errFillIndent(pa parsedTestAttr) string {
	if pa.srcAttr == nil {
		return "\t"
	}
	return r.attrLineIndent(pa) + "\t"
}

// formatErrFillAttr formats a filled @test(err, ...) attribute string for val.
// Used by CUE_UPDATE=1 to replace bare @test(err) placeholders.
//
// fieldPath is the CUE path string (dot-joined) of the field being annotated.
// path= is suppressed when the error's own path equals fieldPath (error is at
// the current field) or when atPath is a path-boundary suffix of the error's
// path (the at= flag already encodes that location).
//
// Single errors produce:
//
//	@test(err, code=eval, contains="format string", args=["arg"], pos=[0:5])
//
// Multi-error values produce one suberr=(...) per sub-error, each on its own line:
//
//	@test(err,
//		suberr=(contains="...", pos=[0:5]),
//		suberr=(contains="...", pos=[]))
func (r *inlineRunner) formatErrFillAttr(val cue.Value, pa parsedTestAttr, fieldPath, atPath string) string {
	err := valErr(val)
	if err == nil {
		return "@test(err)"
	}

	all := cueerrors.Errors(err)
	actual := all
	if len(all) > 1 && isDisjunctionHeader(all[0]) {
		actual = all[1:]
	}

	var b strings.Builder
	b.WriteString("@test(err")

	if len(actual) <= 1 {
		// Single error: include code, path, contains, args, pos inline.
		if code := errCodeStr(val); code != "" {
			b.WriteString(", code=")
			b.WriteString(code)
		}
		var e cueerrors.Error
		if errors.As(err, &e) {
			if epath := strings.Join(e.Path(), "."); epath != "" && !isRedundantPath(atPath, epath) {
				if rel := relativePathTo(epath, fieldPath); rel != "" {
					b.WriteString(", path=")
					b.WriteString(rel)
				}
			}
			formatErrMsg(&b, e)
			positions := positionsFromSingleError(e)
			r.formatErrPos(&b, positions, pa)
		}
	} else {
		// Multiple sub-errors: include code= at the outer level, then one
		// suberr=(...) per error, each on its own line, indented one tab beyond
		// the @test attribute's own indentation level.
		// code= is taken from the outer value and reused in suberr entries
		// because cueerrors.Error does not expose the error code directly.
		code := errCodeStr(val)
		if code != "" {
			b.WriteString(", code=")
			b.WriteString(code)
		}
		indent := r.errFillIndent(pa)
		for _, e := range actual {
			b.WriteString(",\n")
			b.WriteString(indent)
			// Build the suberr body in a separate builder so we can strip the
			// leading ", " that formatErrMsg always prepends — inside suberr=(...)
			// there is no preceding content to separate from.
			var inner strings.Builder
			if code != "" {
				fmt.Fprintf(&inner, ", code=%s", code)
			}
			if epath := strings.Join(e.Path(), "."); epath != "" && !isRedundantPath(atPath, epath) {
				if rel := relativePathTo(epath, fieldPath); rel != "" {
					fmt.Fprintf(&inner, ", path=%s", rel)
				}
			}
			formatErrMsg(&inner, e)
			positions := positionsFromSingleError(e)
			r.formatErrPos(&inner, positions, pa)
			b.WriteString("suberr=(")
			b.WriteString(strings.TrimPrefix(inner.String(), ", "))
			b.WriteByte(')')
		}
	}

	b.WriteByte(')')
	return b.String()
}

// formatErrMsg appends contains= and (if present) args= for e to b.
// The format string from Msg() is used verbatim as contains=; args holds the
// raw fmt.Sprint representations of any Msg() arguments.
func formatErrMsg(b *strings.Builder, e cueerrors.Error) {
	format, args := e.Msg()
	b.WriteString(", contains=")
	b.WriteString(strconv.Quote(format))
	if len(args) > 0 {
		b.WriteString(", args=[")
		for i, a := range args {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprint(b, a)
		}
		b.WriteByte(']')
	}
}

// formatErrPos appends a pos=[...] spec for positions to b.
func (r *inlineRunner) formatErrPos(b *strings.Builder, positions []token.Pos, pa parsedTestAttr) {
	b.WriteString(", pos=[")
	for i, p := range positions {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(r.formatPosSpec(p, pa.baseLine, pa.srcFileName))
	}
	b.WriteByte(']')
}
