// Copyright 2021 The CUE Authors
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

// Package testing is a helper package for test packages in the CUE project.
// As such it should only be imported in _test.go files.
package cuetest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/internal/tdtest"
)

const (
	envUpdate = "CUE_UPDATE"

	// envNonIssues can be set to a regular expression which indicates what
	// issues we no longer consider issues, i.e. they should have been fixed.
	// This should generally result in tests that would otherwise be skipped no
	// longer being skipped.  e.g.  CUE_NON_ISSUES=. will cause all issue
	// tracker conditions (e.g. [golang.org/issues/1234]) to be considered
	// non-issues.
	envNonIssues = "CUE_NON_ISSUES"

	envFormatTxtar = "CUE_FORMAT_TXTAR"
)

var (
	// issuesConditions is a set of regular expressions that defines the set of
	// conditions that can be used to declare links to issues in various issue
	// trackers. e.g. in testscript condition form
	//
	//     [golang.org/issues/1234]
	//     [github.com/govim/govim/issues/4321]
	issuesConditions = []*regexp.Regexp{
		regexp.MustCompile(`^golang\.org/issues?/\d+$`),
		regexp.MustCompile(`^cuelang\.org/issues?/\d+$`),
	}
)

// UpdateGoldenFiles determines whether tests should update expected
// output in test files in the event of comparison failures (for example
// after a cmp failure in a testscript-based test). It is controlled by
// setting CUE_UPDATE to a non-empty string like "1" or "true". It
// corresponds to testscript.Params.UpdateGoldenFiles; see its docs for
// details.
//
// In some cases, tests might refuse to perform some updates by default.
// The special value "force" can be used to force updates in that situation.
//
// The special value "diff" does not update files but fails the test with
// a diff of the changes that would be applied; see [DiffGoldenFiles].
//
// These flags are functions rather than variables so that the environment
// is read while a test runs; see the comment on Init in internal/cueexperiment
// for why that matters to the go test cache.
func UpdateGoldenFiles() bool {
	return os.Getenv(envUpdate) != "" && os.Getenv(envUpdate) != "diff"
}

// ForceUpdateGoldenFiles determines whether tests should update
// expected output in test files even when they would not be updated
// usually (for example when there are test regressions).
func ForceUpdateGoldenFiles() bool {
	return os.Getenv(envUpdate) == "force"
}

// DiffGoldenFiles determines whether tests should fail with a diff wherever
// CUE_UPDATE=1 would write a file, without actually writing anything.
// It is controlled by setting CUE_UPDATE=diff, and is otherwise a regular
// test run: a diff run passes exactly when the tests pass and an update
// run would leave every file unchanged, which is what CI relies on.
//
// Output that a regular run never compares, such as documentary sections
// or unfilled placeholders, must therefore check [UpdateOrDiffGoldenFiles]
// rather than [UpdateGoldenFiles] and report a difference in diff mode,
// for instance via [WriteGoldenFile].
func DiffGoldenFiles() bool {
	return os.Getenv(envUpdate) == "diff"
}

// UpdateOrDiffGoldenFiles reports whether tests should work out what
// CUE_UPDATE=1 would write, either to write it ([UpdateGoldenFiles]) or to
// fail when it differs from what is stored ([DiffGoldenFiles]).
func UpdateOrDiffGoldenFiles() bool {
	return os.Getenv(envUpdate) != ""
}

// WriteGoldenFile writes data to path when updating golden files, creating
// parent directories as needed and skipping the write when the file already
// holds data so that its modification time, which the go test cache keys on,
// is left alone. Under CUE_UPDATE=diff it writes nothing and instead fails t
// with a diff when the file differs. Callers must be gated on
// [UpdateOrDiffGoldenFiles].
func WriteGoldenFile(t testing.TB, path string, data []byte) {
	t.Helper()
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if bytes.Equal(old, data) {
		return
	}
	if DiffGoldenFiles() {
		StaleGoldenFile(t, path, old, data)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o666); err != nil {
		t.Fatal(err)
	}
}

// StaleGoldenFile fails t under CUE_UPDATE=diff, reporting that CUE_UPDATE=1
// would rewrite the file or section name from old to new. It is for callers
// which cannot use [WriteGoldenFile], such as those updating part of a file.
func StaleGoldenFile(t testing.TB, name string, old, new []byte) {
	t.Helper()
	t.Errorf("%s is stale; CUE_UPDATE=1 would rewrite it: (-want +got)\n%s",
		name, cmp.Diff(string(old), string(new)))
}

// FormatTxtar ensures that .cue files in txtar test archives are well
// formatted, updating the archive as required prior to running a test.
// It is controlled by setting CUE_FORMAT_TXTAR to a non-empty string like "true".
func FormatTxtar() bool {
	return os.Getenv(envFormatTxtar) != ""
}

// Condition adds support for CUE-specific testscript conditions within
// testscript scripts. Supported conditions include:
//
// [golang.org/issue/N] - evaluates to true unless CUE_NON_ISSUES
// is set to a regexp that matches the condition, i.e. golang.org/issue/N
// in this case
//
// [cuelang.org/issue/N] - evaluates to true unless CUE_NON_ISSUES
// is set to a regexp that matches the condition, i.e. cuelang.org/issue/N
// in this case
func Condition(cond string) (bool, error) {
	isIssue, nonIssue, err := checkIssueCondition(cond)
	if err != nil {
		return false, err
	}
	if isIssue {
		return !nonIssue, nil
	}
	return false, fmt.Errorf("unknown condition %v", cond)
}

// T is an alias to tdtest.T
type T = tdtest.T

func init() {
	// Assign the function value; tdtest calls it while a test runs so the
	// CUE_UPDATE read is recorded by the go test cache.
	tdtest.UpdateTests = UpdateGoldenFiles
}

// Run creates a new table-driven test using the CUE testing defaults.
//
// TODO: move this wrapper out to cuetdtest. Users should either use the full
// version of tdtest directly, or use the cuetdtest wrapper.
func Run[TC any](t *testing.T, table []TC, fn func(t *T, tc *TC)) {
	tdtest.Run(t, table, fn)
}

// IssueSkip causes the test t to be skipped unless the issue identified
// by s is deemed to be a non-issue by CUE_NON_ISSUES.
func IssueSkip(t *testing.T, s string) {
	t.Helper()

	isIssue, nonIssue, err := checkIssueCondition(s)
	if err != nil {
		t.Fatal(err)
	}
	if !isIssue {
		t.Fatalf("issue %q does not match a known issue pattern", s)
	}
	if nonIssue {
		t.Skipf("issue %s", s)
	}
}

// checkIssueCondition examines s to determine whether it is an issue
// condition, in which case isIssue is true. If isIssue, then we check
// CUE_NON_ISSUES for a match, in which case nonIssue is true (a value of true
// indicates roughly that we don't believe issue s is an issue any more). In
// case of any errors err is set.
func checkIssueCondition(s string) (isIssue bool, nonIssue bool, err error) {
	var r *regexp.Regexp
	if v := os.Getenv(envNonIssues); v != "" {
		r, err = regexp.Compile(v)
		if err != nil {
			return false, false, fmt.Errorf("failed to compile regexp %q specified via %v: %v", v, envNonIssues, err)
		}
	}
	for _, c := range issuesConditions {
		if c.MatchString(s) {
			isIssue = true
		}
	}
	if !isIssue {
		return false, false, nil
	}
	return isIssue, r != nil && r.MatchString(s), nil
}

// DenyRegistryAccess sets $CUE_REGISTRY to "none" for the rest of the process,
// so that resolving a module fails rather than reaching the Central Registry
// over the network. Tests are meant to be hermetic, serving the modules they
// need from a registry of their own and naming it in an explicit environment
// such as [cuelang.org/go/cue/load.Config.Env]; this catches those which
// forget to.
//
// Call it from the init or TestMain of any test package whose tests can load
// CUE packages or run the cue command. It does not affect testscripts, which do
// not inherit the environment; see [cuelang.org/go/internal/cuetestscript.Setup].
func DenyRegistryAccess() {
	os.Setenv("CUE_REGISTRY", "none")
}
