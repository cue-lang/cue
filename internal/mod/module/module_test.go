// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var checkTests = []struct {
	path    string
	version string
	ok      bool
}{
	{"rsc.io/quote@v0", "0.1.0", false},
	{"rsc io/quote", "v1.0.0", false},

	{"github.com/go-yaml/yaml@v0", "v0.8.0", true},
	{"github.com/go-yaml/yaml@v1", "v1.0.0", true},
	{"github.com/go-yaml/yaml", "v2.0.0", false},
	{"github.com/go-yaml/yaml@v1", "v2.1.5", false},
	{"github.com/go-yaml/yaml@v3.0", "v3.0.0", false},

	{"github.com/go-yaml/yaml@v2", "v1.0.0", false},
	{"github.com/go-yaml/yaml@v2", "v2.0.0", true},
	{"github.com/go-yaml/yaml@v2", "v2.1.5", true},
	{"github.com/go-yaml/yaml@v2", "v3.0.0", false},

	{"rsc.io/quote", "v17.0.0", false},
}

func TestCheck(t *testing.T) {
	for _, tt := range checkTests {
		err := Check(tt.path, tt.version)
		if tt.ok && err != nil {
			t.Errorf("Check(%q, %q) = %v, wanted nil error", tt.path, tt.version, err)
		} else if !tt.ok && err == nil {
			t.Errorf("Check(%q, %q) succeeded, wanted error", tt.path, tt.version)
		}
	}
}

var checkPathWithoutVersionTests = []struct {
	path    string
	wantErr string
}{{
	path:    "rsc io/quote",
	wantErr: `invalid char ' '`,
}, {
	path:    "foo.com@v0",
	wantErr: `module path inappropriately contains major version`,
}, {
	path: "foo.com/bar/baz",
}}

func TestCheckPathWithoutVersion(t *testing.T) {
	for _, test := range checkPathWithoutVersionTests {
		t.Run(test.path, func(t *testing.T) {
			err := CheckPathWithoutVersion(test.path)
			if test.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantErr))
				return
			}
			qt.Assert(t, qt.IsNil(err))
		})
	}
}

var newVersionTests = []struct {
	path, vers   string
	wantError    string
	wantPath     string
	wantBasePath string
}{{
	path:         "github.com/foo/bar@v0",
	vers:         "v0.1.2",
	wantPath:     "github.com/foo/bar@v0",
	wantBasePath: "github.com/foo/bar",
}, {
	path:         "github.com/foo/bar",
	vers:         "v3.1.2",
	wantPath:     "github.com/foo/bar@v3",
	wantBasePath: "github.com/foo/bar",
}, {
	path:         "github.com/foo/bar@v1",
	vers:         "",
	wantPath:     "github.com/foo/bar@v1",
	wantBasePath: "github.com/foo/bar",
}, {
	path:      "github.com/foo/bar@v1",
	vers:      "v3.1.2",
	wantError: `mismatched major version suffix in "github.com/foo/bar@v1" \(version v3\.1\.2\)`,
}, {
	path:      "github.com/foo/bar@v1",
	vers:      "v3.1",
	wantError: `version "v3.1" \(of module "github.com/foo/bar@v1"\) is not canonical`,
}, {
	path:      "github.com/foo/bar@v1",
	vers:      "v3.10.4+build",
	wantError: `version "v3.10.4\+build" \(of module "github.com/foo/bar@v1"\) is not canonical`,
}, {
	path:      "something/bad@v1",
	vers:      "v1.2.3",
	wantError: `malformed module path "something/bad@v1": missing dot in first path element`,
}, {
	path:      "foo.com/bar",
	vers:      "",
	wantError: `path "foo.com/bar" has no major version`,
}, {
	path:      "x.com",
	vers:      "bad",
	wantError: `version "bad" \(of module "x.com"\) is not well formed`,
}}

func TestNewVersion(t *testing.T) {
	for _, test := range newVersionTests {
		t.Run(test.path+"@"+test.vers, func(t *testing.T) {
			v, err := NewVersion(test.path, test.vers)
			if test.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(v.Path(), test.wantPath))
			qt.Assert(t, qt.Equals(v.BasePath(), test.wantBasePath))
			qt.Assert(t, qt.Equals(v.Version(), test.vers))
		})
	}
}

var parseVersionTests = []struct {
	s         string
	wantError string
}{{
	s: "github.com/foo/bar@v0.1.2",
}}

func TestParseVersion(t *testing.T) {
	for _, test := range parseVersionTests {
		t.Run(test.s, func(t *testing.T) {
			v, err := ParseVersion(test.s)
			if test.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(v.String(), test.s))
		})
	}
}
