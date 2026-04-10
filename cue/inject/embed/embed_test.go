// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package embed

import (
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestFSGlob(t *testing.T) {
	// These test cases are the same for both Unix and Windows.
	testCases := []struct {
		testName  string
		paths     []string
		pattern   string
		want      []string
		wantError string
	}{{
		testName: "EmptyDirectory",
		paths:    []string{},
		pattern:  "*/*.txt",
		want:     nil,
	}, {
		testName: "DirectoryWithSingleDotFile",
		paths: []string{
			"foo/bar",
			"foo/bar.txt",
			"foo/.hello.txt",
		},
		pattern: "*/*.txt",
		want: []string{
			"foo/bar.txt",
		},
	}, {
		testName: "DotPrefixedDirectory",
		paths: []string{
			"foo/bar.txt",
			"foo/.hello.txt",
			"foo/baz.txt",
			".git/something.txt",
		},
		pattern: "*/*.txt",
		want: []string{
			"foo/bar.txt",
			"foo/baz.txt",
		},
	}, {
		testName: "DotPrefixedDirectoryWithExplicitDot",
		paths: []string{
			"foo/bar.txt",
			"foo/.hello.txt",
			"foo/baz.txt",
			".git/something.txt",
			".git/.otherthing.txt",
		},
		pattern: ".*/*.txt",
		want: []string{
			".git/something.txt",
		},
	}, {
		testName: "DotInCharacterClass",
		paths: []string{
			".foo",
		},
		pattern: "[.x]foo",
		want:    nil,
	}, {
		testName: "SlashInCharacterClass",
		paths: []string{
			"fooxbar",
			"foo/bar",
		},
		pattern:   "foo[/x]bar",
		wantError: `syntax error in pattern`,
	}, {
		testName: "EmptyPatternMatches",
		paths:    []string{},
		pattern:  "*.txt",
		want:     nil,
	}}
	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			m := make(fstest.MapFS)
			for _, p := range tc.paths {
				m[p] = &fstest.MapFile{
					Mode: 0o666,
				}
			}
			got, err := fsGlob(m, tc.pattern)
			if tc.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, tc.wantError))
				return
			}
			qt.Assert(t, qt.CmpEquals(got, tc.want, cmpopts.EquateEmpty()))
		})
	}
}
