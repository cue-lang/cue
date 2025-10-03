// Copyright 2025 CUE Authors
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

package ast

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var parseImportPathTests = []struct {
	testName      string
	path          string
	want          ImportPath
	wantCanonical any // untyped nil to be equal to the path field
}{{
	testName: "StdlibLikeWithSlash",
	path:     "stdlib/path",
	want: ImportPath{
		Path:      "stdlib/path",
		Qualifier: "path",
	},
}, {
	testName: "StdlibLikeNoSlash",
	path:     "math",
	want: ImportPath{
		Path:      "math",
		Qualifier: "math",
	},
}, {
	testName: "StdlibLikeExplicitQualifier",
	path:     "stdlib/path:other",
	want: ImportPath{
		Path:              "stdlib/path",
		ExplicitQualifier: true,
		Qualifier:         "other",
	},
}, {
	testName: "ExplicitEmptyQualifier",
	path:     "stdlib/path:",
	want: ImportPath{
		Path:              "stdlib/path",
		ExplicitQualifier: true,
	},
}, {
	testName: "StdlibLikeExplicitQualifierNoSlash",
	path:     "math:other",
	want: ImportPath{
		Path:              "math",
		ExplicitQualifier: true,
		Qualifier:         "other",
	},
}, {
	testName: "WithMajorVersion",
	path:     "foo.com/bar@v0",
	want: ImportPath{
		Path:      "foo.com/bar",
		Version:   "v0",
		Qualifier: "bar",
	},
}, {
	testName: "WithFullVersion",
	path:     "foo.com/bar@v0.2.3:xxx",
	want: ImportPath{
		Path:              "foo.com/bar",
		Version:           "v0.2.3",
		Qualifier:         "xxx",
		ExplicitQualifier: true,
	},
}, {
	testName: "WithFullVersionNoQualifier",
	path:     "foo.com/bar@v0.2.3-foo",
	want: ImportPath{
		Path:      "foo.com/bar",
		Version:   "v0.2.3-foo",
		Qualifier: "bar",
	},
}, {
	testName: "WithLatest",
	path:     "foo.com/bar@latest",
	want: ImportPath{
		Path:      "foo.com/bar",
		Version:   "latest",
		Qualifier: "bar",
	},
}, {
	testName: "WithMajorVersionLatest",
	path:     "foo.com/bar@v1.latest",
	want: ImportPath{
		Path:      "foo.com/bar",
		Version:   "v1.latest",
		Qualifier: "bar",
	},
}, {
	testName: "WithMajorVersionNoSlash",
	path:     "main.test@v0",
	want: ImportPath{
		Path:      "main.test",
		Version:   "v0",
		Qualifier: "",
	},
}, {
	testName: "WithMajorVersionAndExplicitQualifier",
	path:     "foo.com/bar@v0:other",
	want: ImportPath{
		Path:              "foo.com/bar",
		Version:           "v0",
		ExplicitQualifier: true,
		Qualifier:         "other",
	},
}, {
	testName: "WithMajorVersionAndNoQualifier",
	path:     "foo.com/bar@v0",
	want: ImportPath{
		Path:      "foo.com/bar",
		Version:   "v0",
		Qualifier: "bar",
	},
}, {
	testName: "WithRedundantQualifier",
	path:     "foo.com/bar@v0:bar",
	want: ImportPath{
		Path:              "foo.com/bar",
		Version:           "v0",
		ExplicitQualifier: true,
		Qualifier:         "bar",
	},
	wantCanonical: "foo.com/bar@v0",
}, {
	testName: "WithPattern",
	path:     "foo.com/bar/.../blah",
	want: ImportPath{
		Path:              "foo.com/bar/.../blah",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "blah",
	},
}, {
	testName: "WithPatternAtEnd",
	path:     "foo.com/bar/...",
	want: ImportPath{
		Path:              "foo.com/bar/...",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "",
	},
}, {
	testName: "WithUnderscoreLastElement",
	path:     "foo.com/bar/_foo",
	want: ImportPath{
		Path:              "foo.com/bar/_foo",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "_foo",
	},
}, {
	testName: "WithHashLastElement",
	path:     "foo.com/bar/#foo",
	want: ImportPath{
		Path:              "foo.com/bar/#foo",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "",
	},
}, {
	testName: "StdlibPathWithQualifier",
	path:     "strings:strings",
	want: ImportPath{
		Path:              "strings",
		Version:           "",
		ExplicitQualifier: true,
		Qualifier:         "strings",
	},
	wantCanonical: "strings",
}, {
	testName: "DotWithQualifier",
	path:     ".:foo",
	want: ImportPath{
		Path:              ".",
		ExplicitQualifier: true,
		Qualifier:         "foo",
	},
}, {
	// Historically, `:pkgname` has been a short-hand for `.:pkgname`.
	testName: "JustQualifier",
	path:     ":foo",
	want: ImportPath{
		Path:              "",
		ExplicitQualifier: true,
		Qualifier:         "foo",
	},
}, {
	// Likely nonsensical, but keep track of what we return.
	testName: "Empty",
	path:     "",
	want: ImportPath{
		Path: "",
	},
}, {
	// Likely nonsensical, but keep track of what we return.
	testName: "Colon",
	path:     ":",
	want: ImportPath{
		Path:              "",
		ExplicitQualifier: true,
		Qualifier:         "",
	},
	wantCanonical: "",
}}

func TestParseImportPath(t *testing.T) {
	for _, test := range parseImportPathTests {
		t.Run(test.testName, func(t *testing.T) {
			parts := ParseImportPath(test.path)
			qt.Assert(t, qt.DeepEquals(parts, test.want))
			qt.Assert(t, qt.Equals(parts.String(), test.path))
			if test.wantCanonical == nil {
				test.wantCanonical = test.path
			}
			gotCanonical := parts.Canonical().String()
			qt.Assert(t, qt.Equals(gotCanonical, test.wantCanonical.(string)))
			// Make sure that the canonical version round-trips OK.
			qt.Assert(t, qt.Equals(ParseImportPath(gotCanonical).String(), gotCanonical))
		})
	}
}

var canonicalWithManuallyConstructedImportPathTests = []struct {
	testName   string
	ip         ImportPath
	want       ImportPath
	wantString string
}{{
	testName: "MissingQualifierIsAdded",
	ip: ImportPath{
		Path: "foo.com/bar",
	},
	want: ImportPath{
		Path:      "foo.com/bar",
		Qualifier: "bar",
	},
	wantString: "foo.com/bar",
}, {
	testName: "ExplicitQualifierIsSet",
	ip: ImportPath{
		Path:      "foo.com/bar",
		Qualifier: "other",
	},
	want: ImportPath{
		Path:              "foo.com/bar",
		Qualifier:         "other",
		ExplicitQualifier: true,
	},
	wantString: "foo.com/bar:other",
}, {
	testName: "HostOnly",
	ip: ImportPath{
		Path:      "foo.com",
		Qualifier: "bar",
	},
	want: ImportPath{
		Path:              "foo.com",
		Qualifier:         "bar",
		ExplicitQualifier: true,
	},
	wantString: "foo.com:bar",
}}

func TestCanonicalWithManuallyConstructedImportPath(t *testing.T) {
	// Test that Canonical works correctly on ImportPath values
	// that are in forms that would not be returned by ParseImportPath.
	for _, test := range canonicalWithManuallyConstructedImportPathTests {
		t.Run(test.testName, func(t *testing.T) {
			got := test.ip.Canonical()
			qt.Assert(t, qt.DeepEquals(got, test.want))
			qt.Assert(t, qt.Equals(got.String(), test.wantString))
		})
	}
}

func TestImportPathStringAddsQualifier(t *testing.T) {
	ip := ImportPath{
		Path:      "foo.com/bar",
		Version:   "v0",
		Qualifier: "baz",
	}
	qt.Assert(t, qt.Equals(ip.String(), "foo.com/bar@v0:baz"))
}

func TestImportPathStringAddsQualifierWhenNoVersion(t *testing.T) {
	ip := ImportPath{
		Path:      "foo.com/bar",
		Qualifier: "baz",
	}
	qt.Assert(t, qt.Equals(ip.String(), "foo.com/bar:baz"))
}
