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
	wantCanonical string
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
}}

func TestParseImportPath(t *testing.T) {
	for _, test := range parseImportPathTests {
		t.Run(test.testName, func(t *testing.T) {
			parts := ParseImportPath(test.path)
			qt.Assert(t, qt.DeepEquals(parts, test.want))
			qt.Assert(t, qt.Equals(parts.String(), test.path))
			if test.wantCanonical == "" {
				test.wantCanonical = test.path
			}
			gotCanonical := parts.Canonical().String()
			qt.Assert(t, qt.Equals(gotCanonical, test.wantCanonical))
			// Make sure that the canonical version round-trips OK.
			qt.Assert(t, qt.Equals(ParseImportPath(gotCanonical).String(), gotCanonical))
		})
	}
}

func TestCanonicalAddsQualifier(t *testing.T) {
	p := ImportPath{
		Path: "foo.com/bar",
	}.Canonical()
	qt.Assert(t, qt.DeepEquals(p, ImportPath{
		Path:      "foo.com/bar",
		Qualifier: "bar",
	}))
	qt.Assert(t, qt.Equals(p.String(), "foo.com/bar"))
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
