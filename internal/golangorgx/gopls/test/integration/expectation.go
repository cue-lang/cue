// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/server"
)

var (
	// InitialWorkspaceLoad is an expectation that the workspace initial load has
	// completed. It is verified via workdone reporting.
	InitialWorkspaceLoad = CompletedWork(server.DiagnosticWorkTitle(server.FromInitialWorkspaceLoad), 1, false)
)

// A Verdict is the result of checking an expectation against the current
// editor state.
type Verdict int

// Order matters for the following constants: verdicts are sorted in order of
// decisiveness.
const (
	// Met indicates that an expectation is satisfied by the current state.
	Met Verdict = iota
	// Unmet indicates that an expectation is not currently met, but could be met
	// in the future.
	Unmet
	// Unmeetable indicates that an expectation cannot be satisfied in the
	// future.
	Unmeetable
)

func (v Verdict) String() string {
	switch v {
	case Met:
		return "Met"
	case Unmet:
		return "Unmet"
	case Unmeetable:
		return "Unmeetable"
	}
	return fmt.Sprintf("unrecognized verdict %d", v)
}

// An Expectation is an expected property of the state of the LSP client.
// The Check function reports whether the property is met.
//
// Expectations are combinators. By composing them, tests may express
// complex expectations in terms of simpler ones.
//
// TODO(rfindley): as expectations are combined, it becomes harder to identify
// why they failed. A better signature for Check would be
//
//	func(State) (Verdict, string)
//
// returning a reason for the verdict that can be composed similarly to
// descriptions.
type Expectation struct {
	Check func(State) Verdict

	// Description holds a noun-phrase identifying what the expectation checks.
	//
	// TODO(rfindley): revisit existing descriptions to ensure they compose nicely.
	Description string
}

func describeExpectations(expectations ...Expectation) string {
	var descriptions []string
	for _, e := range expectations {
		descriptions = append(descriptions, e.Description)
	}
	return strings.Join(descriptions, "\n")
}

// AllOf expects that all given expectations are met.
//
// TODO(rfindley): the problem with combinators such as AllOf is that we lose
// the information of *why* they failed: the Awaiter is not smart enough to
// look inside.
//
// Refactor the API such that the Check function is responsible for explaining
// why an expectation failed. This should allow us to significantly improve
// test output: we won't need to summarize state at all, as the verdict
// explanation itself should describe clearly why the expectation not met.
func AllOf(allOf ...Expectation) Expectation {
	check := func(s State) Verdict {
		verdict := Met
		for _, e := range allOf {
			if v := e.Check(s); v > verdict {
				verdict = v
			}
		}
		return verdict
	}
	description := describeExpectations(allOf...)
	return Expectation{
		Check:       check,
		Description: fmt.Sprintf("All of:\n%s", description),
	}
}

// NoShownMessage asserts that the editor has not received a ShowMessage.
func NoShownMessage(subString string) Expectation {
	check := func(s State) Verdict {
		for _, m := range s.showMessage {
			if strings.Contains(m.Message, subString) {
				return Unmeetable
			}
		}
		return Met
	}
	return Expectation{
		Check:       check,
		Description: fmt.Sprintf("no ShowMessage received containing %q", subString),
	}
}

// ShownMessage asserts that the editor has received a ShowMessageRequest
// containing the given substring.
func ShownMessage(containing string) Expectation {
	check := func(s State) Verdict {
		for _, m := range s.showMessage {
			if strings.Contains(m.Message, containing) {
				return Met
			}
		}
		return Unmet
	}
	return Expectation{
		Check:       check,
		Description: fmt.Sprintf("received window/showMessage containing %q", containing),
	}
}

// DoneDiagnosingChanges expects that diagnostics are complete from common
// change notifications: didOpen, didChange, didSave, didChangeWatchedFiles,
// and didClose.
//
// This can be used when multiple notifications may have been sent, such as
// when a didChange is immediately followed by a didSave. It is insufficient to
// simply await the absence of outstanding work, because the LSP client has no
// control over when the server starts processing a notification. Therefore, we
// must keep track of
func (e *Env) DoneDiagnosingChanges() Expectation {
	stats := e.Editor.Stats()
	statsBySource := map[server.ModificationSource]uint64{
		server.FromDidOpen:                stats.DidOpen,
		server.FromDidChange:              stats.DidChange,
		server.FromDidSave:                stats.DidSave,
		server.FromDidChangeWatchedFiles:  stats.DidChangeWatchedFiles,
		server.FromDidClose:               stats.DidClose,
		server.FromDidChangeConfiguration: stats.DidChangeConfiguration,
	}

	var expected []server.ModificationSource
	for k, v := range statsBySource {
		if v > 0 {
			expected = append(expected, k)
		}
	}

	// Sort for stability.
	slices.Sort(expected)

	var all []Expectation
	for _, source := range expected {
		all = append(all, CompletedWork(server.DiagnosticWorkTitle(source), statsBySource[source], true))
	}

	return AllOf(all...)
}

// DoneWithOpen expects all didOpen notifications currently sent by the editor
// to be completely processed.
func (e *Env) DoneWithOpen() Expectation {
	opens := e.Editor.Stats().DidOpen
	return CompletedWork(server.DiagnosticWorkTitle(server.FromDidOpen), opens, true)
}

// DoneWithChange expects all didChange notifications currently sent by the
// editor to be completely processed.
func (e *Env) DoneWithChange() Expectation {
	changes := e.Editor.Stats().DidChange
	return CompletedWork(server.DiagnosticWorkTitle(server.FromDidChange), changes, true)
}

// DoneWithSave expects all didSave notifications currently sent by the editor
// to be completely processed.
func (e *Env) DoneWithSave() Expectation {
	saves := e.Editor.Stats().DidSave
	return CompletedWork(server.DiagnosticWorkTitle(server.FromDidSave), saves, true)
}

// DoneWithChangeWatchedFiles expects all didChangeWatchedFiles notifications
// currently sent by the editor to be completely processed.
func (e *Env) DoneWithChangeWatchedFiles() Expectation {
	changes := e.Editor.Stats().DidChangeWatchedFiles
	return CompletedWork(server.DiagnosticWorkTitle(server.FromDidChangeWatchedFiles), changes, true)
}

// DoneWithClose expects all didClose notifications currently sent by the
// editor to be completely processed.
func (e *Env) DoneWithClose() Expectation {
	changes := e.Editor.Stats().DidClose
	return CompletedWork(server.DiagnosticWorkTitle(server.FromDidClose), changes, true)
}

// CompletedWork expects a work item to have been completed >= atLeast times.
//
// Since the Progress API doesn't include any hidden metadata, we must use the
// progress notification title to identify the work we expect to be completed.
func CompletedWork(title string, count uint64, atLeast bool) Expectation {
	check := func(s State) Verdict {
		completed := s.completedWork()
		if completed[title] == count || atLeast && completed[title] > count {
			return Met
		}
		return Unmet
	}
	desc := fmt.Sprintf("completed work %q %v times", title, count)
	if atLeast {
		desc = fmt.Sprintf("completed work %q at least %d time(s)", title, count)
	}
	return Expectation{
		Check:       check,
		Description: desc,
	}
}

// LogMatching asserts that the client has received a log message
// of type typ matching the regexp re a certain number of times.
//
// The count argument specifies the expected number of matching logs. If
// atLeast is set, this is a lower bound, otherwise there must be exactly count
// matching logs.
//
// Logs are asynchronous to other LSP messages, so this expectation should not
// be used to assert on ordering with respect to other operations.
func LogMatching(typ protocol.MessageType, count int, atLeast bool, formatRe string, args ...any) Expectation {
	re := fmt.Sprintf(formatRe, args...)
	rec, err := regexp.Compile(re)
	if err != nil {
		panic(err)
	}
	check := func(state State) Verdict {
		var found int
		for _, msg := range state.logs {
			if msg.Type == typ && rec.Match([]byte(msg.Message)) {
				found++
			}
		}
		// Check for an exact or "at least" match.
		if found == count || (found >= count && atLeast) {
			return Met
		}
		// If we require an exact count, and have received more than expected, the
		// expectation can never be met.
		if found > count && !atLeast {
			return Unmeetable
		}
		return Unmet
	}
	desc := fmt.Sprintf("log message matching %q expected %v times", re, count)
	if atLeast {
		desc = fmt.Sprintf("log message matching %q expected at least %v times", re, count)
	}
	return Expectation{
		Check:       check,
		Description: desc,
	}
}

// LogExactf asserts that the client has received a log message of
// type typ which contains exactly the string created by
// fmt.Sprintf(format, args...) a certain number of times.
//
// It is a convenience wrapper around [LogMatching], using
// [regexp.QuoteMeta] on the format argument only. Note the
// string/regexp is not anchored to the beginning or end of any log
// message, so this behaves like [strings.Contains] rather than ==.
func LogExactf(typ protocol.MessageType, count int, atLeast bool, format string, args ...any) Expectation {
	return LogMatching(typ, count, atLeast, regexp.QuoteMeta(format), args...)
}

// NoLogMatching asserts that the client has not received a log message
// of type typ matching the regexp re. If re is an empty string, any log
// message is considered a match.
func NoLogMatching(typ protocol.MessageType, formatRe string, args ...any) Expectation {
	var r *regexp.Regexp
	re := fmt.Sprintf(formatRe, args...)
	if re != "" {
		var err error
		r, err = regexp.Compile(re)
		if err != nil {
			panic(err)
		}
	}
	check := func(state State) Verdict {
		for _, msg := range state.logs {
			if msg.Type != typ {
				continue
			}
			if r == nil || r.Match([]byte(msg.Message)) {
				return Unmeetable
			}
		}
		return Met
	}
	return Expectation{
		Check:       check,
		Description: fmt.Sprintf("no log message matching %q", re),
	}
}

// NoLogExactf is a convenience wrapper around [NoLogMatching], in the
// same way that [LogExactf] is a wrapper around [LogMatching].
func NoLogExactf(typ protocol.MessageType, format string, args ...any) Expectation {
	return NoLogMatching(typ, regexp.QuoteMeta(format), args...)
}

// Diagnostics asserts that there is at least one diagnostic matching the given
// filters.
func Diagnostics(filters ...DiagnosticFilter) Expectation {
	check := func(s State) Verdict {
		diags := flattenDiagnostics(s)
		for _, filter := range filters {
			var filtered []flatDiagnostic
			for _, d := range diags {
				if filter.check(d.name, d.diag) {
					filtered = append(filtered, d)
				}
			}
			if len(filtered) == 0 {
				// TODO(rfindley): if/when expectations describe their own failure, we
				// can provide more useful information here as to which filter caused
				// the failure.
				return Unmet
			}
			diags = filtered
		}
		return Met
	}
	var descs []string
	for _, filter := range filters {
		descs = append(descs, filter.desc)
	}
	return Expectation{
		Check:       check,
		Description: "any diagnostics " + strings.Join(descs, ", "),
	}
}

// NoDiagnostics asserts that there are no diagnostics matching the given
// filters. Notably, if no filters are supplied this assertion checks that
// there are no diagnostics at all, for any file.
func NoDiagnostics(filters ...DiagnosticFilter) Expectation {
	check := func(s State) Verdict {
		diags := flattenDiagnostics(s)
		for _, filter := range filters {
			var filtered []flatDiagnostic
			for _, d := range diags {
				if filter.check(d.name, d.diag) {
					filtered = append(filtered, d)
				}
			}
			diags = filtered
		}
		if len(diags) > 0 {
			return Unmet
		}
		return Met
	}
	var descs []string
	for _, filter := range filters {
		descs = append(descs, filter.desc)
	}
	return Expectation{
		Check:       check,
		Description: "no diagnostics " + strings.Join(descs, ", "),
	}
}

type flatDiagnostic struct {
	name string
	diag protocol.Diagnostic
}

func flattenDiagnostics(state State) []flatDiagnostic {
	var result []flatDiagnostic
	for name, diags := range state.diagnostics {
		for _, diag := range diags.Diagnostics {
			result = append(result, flatDiagnostic{name, diag})
		}
	}
	return result
}

// -- Diagnostic filters --

// A DiagnosticFilter filters the set of diagnostics, for assertion with
// Diagnostics or NoDiagnostics.
type DiagnosticFilter struct {
	desc  string
	check func(name string, _ protocol.Diagnostic) bool
}

// ForFile filters to diagnostics matching the sandbox-relative file name.
func ForFile(name string) DiagnosticFilter {
	return DiagnosticFilter{
		desc: fmt.Sprintf("for file %q", name),
		check: func(diagName string, _ protocol.Diagnostic) bool {
			return diagName == name
		},
	}
}

// AtRegexp filters to diagnostics in the file with sandbox-relative path name,
// at the first position matching the given regexp pattern.
//
// TODO(rfindley): pass in the editor to expectations, so that they may depend
// on editor state and AtRegexp can be a function rather than a method.
func (e *Env) AtRegexp(name, pattern string) DiagnosticFilter {
	loc := e.RegexpSearch(name, pattern)
	return DiagnosticFilter{
		desc: fmt.Sprintf("at the first position (%v) matching %#q in %q", loc.Range.Start, pattern, name),
		check: func(diagName string, d protocol.Diagnostic) bool {
			return diagName == name && d.Range.Start == loc.Range.Start
		},
	}
}
