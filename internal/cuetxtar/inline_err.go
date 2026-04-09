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
	"slices"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue"
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
}

// matchesCode reports whether the given error code satisfies the codes
// constraint. An empty codes slice means any code is accepted.
func (ea *errArgs) matchesCode(got string) bool {
	if len(ea.codes) == 0 {
		return true
	}
	return slices.Contains(ea.codes, got)
}

// posUpdate records a pending pos=[...] replacement within a suberr=(...) group.
type posUpdate struct {
	expIdx    int         // 0-based index of the suberr=(...) group in the attribute
	positions []token.Pos // actual positions to write
}

// posWrite is an alias for inlineFillWrite, used for pos= attribute updates.
type posWrite = inlineFillWrite

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
		case kv.Key() == "at":
			ea.at = kv.Value()
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
			syntheticAttr := internal.ParseAttrBody(token.NoPos, "err, "+inner)
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
// e.g. "[list, int]" → ["list", "int"].
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
func (r *inlineRunner) matchesErrSpec(act cueerrors.Error, ea *errArgs, baseLine int) bool {
	if ea.contains != "" && !strings.Contains(act.Error(), ea.contains) {
		return false
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
	if ea == nil {
		// Bare @test(err) — just check that the value is an error.
		if !r.isError(val) {
			t.Errorf("path %s: expected error, got non-error value", path)
			logHint(t, pa.hint)
		}
		return
	}

	if ea.at != "" {
		// @test(err, at=<path>, ...) — navigate to sub-path then check error.
		subPath := cue.ParsePath(ea.at)
		if err := subPath.Err(); err != nil {
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
			posSet:   ea.posSet,
			pos:      ea.pos,
			suberrs:  ea.suberrs,
			msgArgs:  ea.msgArgs,
			// at is intentionally omitted: we already navigated to the sub-path.
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

	if !r.isError(val) {
		t.Errorf("path %s: expected error, got non-error value", path)
		logHint(t, pa.hint)
		return
	}

	// Validate error code (uses adt.Bottom.Code, only available at cue.Value level).
	if len(ea.codes) > 0 {
		gotCode := r.errorCode(val)
		if !ea.matchesCode(gotCode) {
			t.Errorf("path %s: expected error code %v, got %q", path, ea.codes, gotCode)
			logHint(t, pa.hint)
		}
	}
	// Validate error message contains.
	if ea.contains != "" {
		msg := r.errorMessage(val)
		if !strings.Contains(msg, ea.contains) {
			t.Errorf("path %s: expected error message to contain %q, got %q", path, ea.contains, msg)
			logHint(t, pa.hint)
		}
	}
	// Validate Msg() args (order-independent).
	if len(ea.msgArgs) > 0 {
		var e cueerrors.Error
		if errors.As(val.Err(), &e) {
			checkMsgArgs(t, path, e, ea.msgArgs, "@test(err, args=...)", pa.hint)
		}
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
			if !usedAct[j] && r.matchesErrSpec(act, exp, pa.baseLine) {
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
			if !usedAct[j] && r.matchesErrSpec(act, exp, pa.baseLine) {
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
	// Validate Msg() args for matched pairs (order-independent).
	for _, p := range pairs {
		if len(p.exp.msgArgs) > 0 {
			checkMsgArgs(t, path, p.act, p.exp.msgArgs, "@test(err, suberr=...)", pa.hint)
		}
	}
}

// enqueueSubErrPosWrites applies all sub-error position updates atomically to
// the source attribute, producing a single posWrite entry. Each update replaces
// pos=[...] in the expIdx-th suberr=(...) group.
// formatPosSpec converts a single token.Pos to a position spec string.
// Positions in the same file as the @test attribute are written as
// deltaLine:col (relative to pa.baseLine); positions in other files are
// written as filename:absLine:col (absolute).
func (r *inlineRunner) formatPosSpec(p token.Pos, pa parsedTestAttr) string {
	if p.Filename() == "" || r.relFilename(p.Filename()) == pa.srcFileName {
		return fmt.Sprintf("%d:%d", p.Line()-pa.baseLine, p.Column())
	}
	return fmt.Sprintf("%s:%d:%d", r.relFilename(p.Filename()), p.Line(), p.Column())
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
			parts[i] = r.formatPosSpec(p, pa)
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
	posIdx := strings.Index(inner, "pos=[")
	if posIdx < 0 {
		return attrText // no pos= in this suberr group
	}
	bracket := posIdx + len("pos=[")
	closeIdx := strings.Index(inner[bracket:], "]")
	if closeIdx < 0 {
		return attrText
	}
	closeIdx += bracket + 1 // include "]"
	newInner := inner[:posIdx] + "pos=[" + newPosContent + "]" + inner[closeIdx:]
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
			t.Logf("  actual: %d:%d", p.Line(), p.Column())
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
				t.Errorf("path %s: %s: unmatched position %d:%d; actual positions:", path, directive, exp.deltaLine, exp.col)
			}
			for _, p := range positions {
				t.Logf("  actual: %d:%d", p.Line(), p.Column())
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
	err := val.Err()
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
		parts[i] = r.formatPosSpec(p, pa)
	}
	newPosStr := strings.Join(parts, ", ")

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

// applyPosWritebacks is a no-op: pos= writes are folded into the combined
// write-back pass in applyInlineFillWritebacks to ensure all byte-level
// replacements are applied in a single descending-offset pass, avoiding
// stale-offset corruption when multiple writes share the same file.
func (r *inlineRunner) applyPosWritebacks() {}

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
	if r.isError(val) {
		if ea.matchesCode(r.errorCode(val)) &&
			(ea.contains == "" || strings.Contains(r.errorMessage(val), ea.contains)) &&
			msgArgsMatch(val.Err(), ea.msgArgs) {
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
