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
	"fmt"
	"os"
	"regexp"
	"testing"
)

const (
	// envUpdate is used in the definition of UpdateGoldenFiles
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

// UpdateGoldenFiles determines whether testscript scripts should update txtar
// archives in the event of cmp failures. It corresponds to
// testscript.Params.UpdateGoldenFiles. See the docs for
// testscript.Params.UpdateGoldenFiles for more details.
var UpdateGoldenFiles = os.Getenv(envUpdate) != ""

// FormatTxtar ensures that .cue files in txtar test archives are well
// formatted, updating the archive as required prior to running a test.
var FormatTxtar = os.Getenv(envFormatTxtar) != ""

// Condition adds support for CUE-specific testscript conditions within
// testscript scripts. Supported conditions include:
//
// [long] - evalutates to true when the long build tag is specified
//
// [golang.org/issue/N] - evaluates to true unless CUE_NON_ISSUES
// is set to a regexp that matches the condition, i.e. golang.org/issue/N
// in this case
//
// [cuelang.org/issue/N] - evaluates to true unless CUE_NON_ISSUES
// is set to a regexp that matches the condition, i.e. cuelang.org/issue/N
// in this case
//
func Condition(cond string) (bool, error) {
	isIssue, nonIssue, err := checkIssueCondition(cond)
	if err != nil {
		return false, err
	}
	if isIssue {
		return !nonIssue, nil
	}
	switch cond {
	case "long":
		return Long, nil
	}
	return false, fmt.Errorf("unknown condition %v", cond)
}

// IssueSkip causes the test t to be skipped unless the issue identified
// by s is deemed to be a non-issue by CUE_NON_ISSUES.
func IssueSkip(t *testing.T, s string) {
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
