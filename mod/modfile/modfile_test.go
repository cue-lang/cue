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

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/mod/module"
)

var parseTests = []struct {
	testName     string
	parse        func(modfile []byte, filename string) (*File, error)
	data         string
	wantError    string
	want         *File
	wantVersions []module.Version
	wantDefaults map[string]string
}{{
	testName: "NoDeps",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.8.0"
`,
	want: &File{
		Module: "foo.com/bar@v0",
		Language: &Language{
			Version: "v0.8.0",
		},
	},
	wantDefaults: map[string]string{
		"foo.com/bar": "v0",
	},
}, {
	testName: "WithDeps",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.8.1"
deps: "example.com@v1": {
	default: true
	v: "v1.2.3"
}
deps: "other.com/something@v0": v: "v0.2.3"
`,
	want: &File{
		Language: &Language{
			Version: "v0.8.1",
		},
		Module: "foo.com/bar@v0",
		Deps: map[string]*Dep{
			"example.com@v1": {
				Default: true,
				Version: "v1.2.3",
			},
			"other.com/something@v0": {
				Version: "v0.2.3",
			},
		},
	},
	wantVersions: parseVersions("example.com@v1.2.3", "other.com/something@v0.2.3"),
	wantDefaults: map[string]string{
		"foo.com/bar": "v0",
		"example.com": "v1",
	},
}, {
	testName: "WithSource",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.9.0-alpha.0"
source: kind: "git"
`,
	want: &File{
		Language: &Language{
			Version: "v0.9.0-alpha.0",
		},
		Module: "foo.com/bar@v0",
		Source: &Source{
			Kind: "git",
		},
	},
	wantDefaults: map[string]string{
		"foo.com/bar": "v0",
	},
}, {
	testName: "WithExplicitSource",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.9.0-alpha.0"
source: kind: "self"
`,
	want: &File{
		Language: &Language{
			Version: "v0.9.0-alpha.0",
		},
		Module: "foo.com/bar@v0",
		Source: &Source{
			Kind: "self",
		},
	},
	wantDefaults: map[string]string{
		"foo.com/bar": "v0",
	},
}, {
	testName: "WithUnknownSourceKind",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.9.0-alpha.0"
source: kind: "bad"
`,
	wantError: `source.kind: 2 errors in empty disjunction:(.|\n)+`,
}, {
	testName: "WithEarlierVersionAndSource",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.8.6"
source: kind: "git"
`,
	wantError: `source: conflicting values 1 and (.|\n)+`,
}, {
	testName: "AmbiguousDefaults",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.8.0"
deps: "example.com@v1": {
	default: true
	v: "v1.2.3"
}
deps: "example.com@v2": {
	default: true
	v: "v2.0.0"
}
`,
	wantError: `multiple default major versions found for example.com`,
}, {
	testName: "AmbiguousDefaultsWithMainModule",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.8.0"
deps: "foo.com/bar@v1": {
	default: true
	v: "v1.2.3"
}
`,
	wantError: `multiple default major versions found for foo.com/bar`,
}, {
	testName: "MisspelledLanguageVersionField",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
langugage: version: "v0.4.3"
`,
	wantError: `no language version declared in module.cue`,
}, {
	testName: "MissingLanguageVersionField",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
`,
	wantError: `no language version declared in module.cue`,
}, {
	testName: "InvalidLanguageVersion",
	parse:    Parse,
	data: `
language: version: "vblah"
module: "foo.com/bar@v0"`,
	wantError: `language version "vblah" in module.cue is not valid semantic version`,
}, {
	testName: "EmptyLanguageVersion",
	parse:    Parse,
	data: `
language: {}
module: "foo.com/bar@v0"`,
	wantError: `no language version declared in module.cue`,
}, {
	testName: "NonCanonicalLanguageVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v0"
language: version: "v0.8"
`,
	wantError: `language version v0.8 in module.cue is not canonical`,
}, {
	testName: "InvalidDepVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
language: version: "v0.8.0"
deps: "example.com@v1": v: "1.2.3"
`,
	wantError: `invalid module.cue file module.cue: cannot make version from module "example.com@v1", version "1.2.3": version "1.2.3" \(of module "example.com@v1"\) is not well formed`,
}, {
	testName: "NonCanonicalVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
language: version: "v0.8.0"
deps: "example.com@v1": v: "v1.2"
`,
	wantError: `invalid module.cue file module.cue: cannot make version from module "example.com@v1", version "v1.2": version "v1.2" \(of module "example.com@v1"\) is not canonical`,
}, {
	testName: "NonCanonicalModule",
	parse:    Parse,
	data: `
module: "foo.com/bar"
language: version: "v0.8.0"
`,
	wantError: `module path "foo.com/bar" in module.cue does not contain major version`,
}, {
	testName: "NonCanonicalDep",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
language: version: "v0.8.0"
deps: "example.com": v: "v1.2.3"
`,
	wantError: `invalid module.cue file module.cue: no major version in "example.com"`,
}, {
	testName: "MismatchedMajorVersion",
	parse:    Parse,
	data: `
module: "foo.com/bar@v1"
language: version: "v0.8.0"
deps: "example.com@v1": v: "v0.1.2"
`,
	wantError: `invalid module.cue file module.cue: cannot make version from module "example.com@v1", version "v0.1.2": mismatched major version suffix in "example.com@v1" \(version v0.1.2\)`,
}, {
	testName: "NonStrictNoMajorVersions",
	parse:    ParseNonStrict,
	data: `
module: "foo.com/bar"
language: version: "v0.8.0"
deps: "example.com": v: "v1.2.3"
`,
	want: &File{
		Module:   "foo.com/bar@v0",
		Language: &Language{Version: "v0.8.0"},
		Deps: map[string]*Dep{
			"example.com": {
				Version: "v1.2.3",
			},
		},
	},
	wantVersions: parseVersions("example.com@v1.2.3"),
	wantDefaults: map[string]string{
		"foo.com/bar": "v0",
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
	for _, test := range parseTests {
		t.Run(test.testName, func(t *testing.T) {
			f, err := test.parse([]byte(test.data), "module.cue")
			if test.wantError != "" {
				gotErr := strings.TrimSuffix(errors.Details(err, nil), "\n")
				qt.Assert(t, qt.Matches(gotErr, test.wantError), qt.Commentf("error %v", err))
				return
			}
			qt.Assert(t, qt.IsNil(err), qt.Commentf("details: %v", strings.TrimSuffix(errors.Details(err, nil), "\n")))
			qt.Assert(t, qt.CmpEquals(f, test.want, cmpopts.IgnoreUnexported(File{})))
			qt.Assert(t, qt.DeepEquals(f.DepVersions(), test.wantVersions))
			qt.Assert(t, qt.DeepEquals(f.DefaultMajorVersions(), test.wantDefaults))
		})
	}
}

func TestFormat(t *testing.T) {
	type formatTest struct {
		name      string
		file      *File
		wantError string
		want      string
	}
	tests := []formatTest{{
		name: "WithLanguage",
		file: &File{
			Language: &Language{
				Version: "v0.8.0",
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
		want: `module: "foo.com/bar@v0"
language: {
	version: "v0.8.0"
}
deps: {
	"example.com@v1": {
		v: "v1.2.3"
	}
	"other.com/something@v0": {
		v: "v0.2.3"
	}
}
`}, {
		name: "WithoutLanguage",
		file: &File{
			Module: "foo.com/bar@v0",
			Language: &Language{
				Version: "v0.8.0",
			},
		},
		want: `module: "foo.com/bar@v0"
language: {
	version: "v0.8.0"
}
`}, {
		name: "WithVersionTooEarly",
		file: &File{
			Module: "foo.com/bar@v0",
			Language: &Language{
				Version: "v0.4.3",
			},
		},
		wantError: `language version v0.4.3 is too early for module.cue \(need at least v0.8.0\)`,
	}, {
		name: "WithInvalidModuleVersion",
		file: &File{
			Module: "foo.com/bar@v0",
			Language: &Language{
				Version: "badversion--",
			},
		},
		wantError: `cannot round-trip module file: language version "badversion--" in module.cue is not valid semantic version`,
	}, {
		name: "WithNonNilEmptyDeps",
		file: &File{
			Module: "foo.com/bar@v0",
			Language: &Language{
				Version: "v0.8.0",
			},
			Deps: map[string]*Dep{},
		},
		want: `module: "foo.com/bar@v0"
language: {
	version: "v0.8.0"
}
`,
	}}
	cuetest.Run(t, tests, func(t *cuetest.T, test *formatTest) {
		data, err := test.file.Format()
		if test.wantError != "" {
			qt.Assert(t, qt.ErrorMatches(err, test.wantError))
			return
		}
		qt.Assert(t, qt.IsNil(err))
		t.Equal(string(data), test.want)

		// Check that it round-trips.
		f, err := Parse(data, "")
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.CmpEquals(f, test.file, cmpopts.IgnoreUnexported(File{}), cmpopts.EquateEmpty()))
	})
}

func parseVersions(vs ...string) []module.Version {
	vvs := make([]module.Version, 0, len(vs))
	for _, v := range vs {
		vvs = append(vvs, module.MustParseVersion(v))
	}
	return vvs
}
