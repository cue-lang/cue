// Copyright 2023 CUE Authors
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

package modfile

import (
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var tests = []struct {
	testName  string
	parse     func(modfile []byte, filename string) (*File, error)
	data      string
	wantError string
	want      *File
}{{
	testName: "NoDeps",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
`,
	want: &File{
		Module: "foo.com/bar@v0",
	},
}, {
	testName: "WithDeps",
	parse:    Parse,
	data: `
language: version: "v0.4.3"
module: "foo.com/bar@v0"
deps: "example.com@v1": v: "v1.2.3"
deps: "other.com/something@v0": v: "v0.2.3"
`,
	want: &File{
		Language: Language{
			Version: "v0.4.3",
		},
		Module: "foo.com/bar@v0",
		Deps: map[string]*Dep{
			"example.com@v1": {
				Version: "v1.2.3",
			},
			"other.com/something@v0": {
				Version: "v0.2.3",
			},
		},
	},
}, {
	testName: "MisspelledLanguageVersionField",
	parse:    Parse,
	data: `
langugage: version: "v0.4.3"
module: "foo.com/bar@v0"
`,
	wantError: `langugage: field not allowed:
    cuelang.org/go/internal/mod/modfile/schema.cue:7:1
    cuelang.org/go/internal/mod/modfile/schema.cue:7:15
    cuelang.org/go/internal/mod/modfile/schema.cue:108:1
    cuelang.org/go/internal/mod/modfile/schema.cue:139:1
    module.cue:2:1`,
}, {
	testName: "InvalidLanguageVersion",
	parse:    Parse,
	data: `
language: version: "vblah"
module: "foo.com/bar@v0"`,
	wantError: `language version "vblah" in module.cue is not well formed`,
}, {
	testName: "InvalidDepVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
deps: "example.com@v1": v: "1.2.3"
`,
	wantError: `invalid module.cue file module.cue: cannot make version from module "example.com@v1", version "1.2.3": version "1.2.3" \(of module "example.com@v1"\) is not well formed`,
}, {
	testName: "NonCanonicalVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
deps: "example.com@v1": v: "v1.2"
`,
	wantError: `invalid module.cue file module.cue: cannot make version from module "example.com@v1", version "v1.2": version "v1.2" \(of module "example.com@v1"\) is not canonical`,
}, {
	testName: "NonCanonicalModule",
	parse:    Parse,
	data: `
module: "foo.com/bar"
`,
	wantError: `module path "foo.com/bar" in module.cue does not contain major version`,
}, {
	testName: "NonCanonicalDep",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
deps: "example.com": v: "v1.2.3"
`,
	wantError: `invalid module.cue file module.cue: no major version in "example.com"`,
}, {
	testName: "MismatchedMajorVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
deps: "example.com@v1": v: "v0.1.2"
`,
	wantError: `invalid module.cue file module.cue: cannot make version from module "example.com@v1", version "v0.1.2": mismatched major version suffix in "example.com@v1" \(version v0.1.2\)`,
}, {
	testName: "NonStrictNoMajorVersions",
	parse:    ParseNonStrict,
	data: `
module: "foo.com/bar"
deps: "example.com": v: "v1.2.3"
`,
	want: &File{
		Module: "foo.com/bar",
		Deps: map[string]*Dep{
			"example.com": {
				Version: "v1.2.3",
			},
		},
	},
}, {
	testName: "LegacyWithExtraFields",
	parse:    ParseLegacy,
	data: `
module: "foo.com/bar"
something: 4
cue: lang: "xxx"
`,
	want: &File{
		Module: "foo.com/bar",
	},
}}

func TestParse(t *testing.T) {
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			f, err := test.parse([]byte(test.data), "module.cue")
			if test.wantError != "" {
				gotErr := strings.TrimSuffix(errors.Details(err, nil), "\n")
				qt.Assert(t, qt.Matches(gotErr, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err), qt.Commentf("details: %v", errors.Details(err, nil)))
			qt.Assert(t, qt.CmpEquals(f, test.want, cmpopts.IgnoreUnexported(File{})))
		})
	}
}
