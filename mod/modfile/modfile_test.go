package modfile

import (
	"testing"

	"cuelang.org/go/cue/errors"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var tests = []struct {
	testName  string
	data      string
	wantError string
	want      *File
}{{
	testName: "NoDeps",
	data: `
module: "foo.com/bar@v0"
`,
	want: &File{
		Module: "foo.com/bar@v0",
	},
}, {
	testName: "WithDeps",
	data: `
module: "foo.com/bar@v0"
deps: "example.com@v1": v: "v1.2.3"
deps: "other.com/something@v0": v: "v0.2.3"
`,
	want: &File{
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
	testName: "InvalidDepVersion",
	data: `
module: "foo.com/bar@v1"
deps: "example.com@v1": v: "1.2.3"
`,
	wantError: `invalid module.cue file: cannot make version from module "example.com@v1", version "1.2.3": version "1.2.3" \(of module "example.com@v1"\) is not canonical`,
}, {
	testName: "NonCanonicalVersion",
	data: `
module: "foo.com/bar@v1"
deps: "example.com@v1": v: "v1.2"
`,
	wantError: `invalid module.cue file: cannot make version from module "example.com@v1", version "v1.2": version "v1.2" \(of module "example.com@v1"\) is not canonical`,
}, {
	testName: "NonCanonicalModule",
	data: `
module: "foo.com/bar"
`,
	wantError: `module path "foo.com/bar" does not contain major version`,
}, {
	testName: "NonCanonicalDep",
	data: `
module: "foo.com/bar@v1"
deps: "example.com": v: "v1.2.3"
`,
	wantError: `invalid module.cue file: no major version in "example.com"`,
}, {
	testName: "MismatchedMajorVersion",
	data: `
module: "foo.com/bar@v1"
deps: "example.com@v1": v: "v0.1.2"
`,
	wantError: `invalid module.cue file: cannot make version from module "example.com@v1", version "v0.1.2": mismatched major version suffix in "example.com@v1" \(version v0.1.2\)`,
}}

func TestParse(t *testing.T) {
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			f, err := Parse([]byte(test.data), "module.cue")
			if test.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err), qt.Commentf("details: %v", errors.Details(err, nil)))
			qt.Assert(t, qt.CmpEquals(f, test.want, cmpopts.IgnoreUnexported(File{})))
		})
	}
}
