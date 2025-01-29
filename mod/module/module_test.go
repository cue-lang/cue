// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import (
	"testing"

	"cuelang.org/go/internal/cuetest"
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

type checkPathTest struct {
	path      string
	modErr    string
	importErr string
	fileErr   string
}

var checkPathTests = []checkPathTest{{
	path: `x.y/z`,
}, {
	path: `x.y`,
}, {
	path:      ``,
	modErr:    `empty string`,
	importErr: `malformed import path "": empty string`,
	fileErr:   `malformed file path "": empty string`,
}, {
	path:      "x.y/\xffz",
	modErr:    `invalid UTF-8`,
	importErr: `malformed import path "x.y/\xffz": invalid UTF-8`,
	fileErr:   `malformed file path "x.y/\xffz": invalid UTF-8`,
}, {
	path:      `/x.y/z`,
	modErr:    `empty path element`,
	importErr: `malformed import path "/x.y/z": empty path element`,
	fileErr:   `malformed file path "/x.y/z": empty path element`,
}, {
	path:      `x./z`,
	modErr:    `trailing '.' in path element`,
	importErr: `malformed import path "x./z": trailing dot in path element`,
	fileErr:   `malformed file path "x./z": trailing dot in path element`,
}, {
	path:   `.x/z`,
	modErr: `leading '.' in path element`,
}, {
	path:      `-x/z`,
	modErr:    `leading dash`,
	importErr: `malformed import path "-x/z": leading dash`,
}, {
	path:   `x..y/z`,
	modErr: `path does not conform to OCI repository name restrictions; see https://github.com/opencontainers/distribution-spec/blob/HEAD/spec.md#pulling-manifests`,
}, {
	path:      `x.y/z/../../w`,
	modErr:    `invalid path element ".."`,
	importErr: `malformed import path "x.y/z/../../w": invalid path element ".."`,
	fileErr:   `malformed file path "x.y/z/../../w": invalid path element ".."`,
}, {
	path:      `x.y//z`,
	modErr:    `double slash`,
	importErr: `malformed import path "x.y//z": double slash`,
	fileErr:   `malformed file path "x.y//z": double slash`,
}, {
	path:      `x.y/z//w`,
	modErr:    `double slash`,
	importErr: `malformed import path "x.y/z//w": double slash`,
	fileErr:   `malformed file path "x.y/z//w": double slash`,
}, {
	path:      `x.y/z/`,
	modErr:    `trailing slash`,
	importErr: `malformed import path "x.y/z/": trailing slash`,
	fileErr:   `malformed file path "x.y/z/": trailing slash`,
}, {
	path: `x.y/z/v0`,
}, {
	path: `x.y/z/v1`,
}, {
	path: `x.y/z/v2`,
}, {
	path: `x.y/z/v2.0`,
}, {
	path:   `X.y/z`,
	modErr: `invalid char 'X'`,
}, {
	path:      `!x.y/z`,
	modErr:    `invalid char '!'`,
	importErr: `malformed import path "!x.y/z": invalid char '!'`,
}, {
	path:   `_x.y/z`,
	modErr: `leading '_' in path element`,
}, {
	path:      `x.y!/z`,
	modErr:    `invalid char '!'`,
	importErr: `malformed import path "x.y!/z": invalid char '!'`,
}, {
	path:      `x.y"/z`,
	modErr:    `invalid char '"'`,
	importErr: `malformed import path "x.y\"/z": invalid char '"'`,
	fileErr:   `malformed file path "x.y\"/z": invalid char '"'`,
}, {
	path:      `x.y#/z`,
	modErr:    `invalid char '#'`,
	importErr: `malformed import path "x.y#/z": invalid char '#'`,
}, {
	path:      `x.y$/z`,
	modErr:    `invalid char '$'`,
	importErr: `malformed import path "x.y$/z": invalid char '$'`,
}, {
	path:      `x.y%/z`,
	modErr:    `invalid char '%'`,
	importErr: `malformed import path "x.y%/z": invalid char '%'`,
}, {
	path:      `x.y&/z`,
	modErr:    `invalid char '&'`,
	importErr: `malformed import path "x.y&/z": invalid char '&'`,
}, {
	path:      `x.y'/z`,
	modErr:    `invalid char '\''`,
	importErr: `malformed import path "x.y'/z": invalid char '\''`,
	fileErr:   `malformed file path "x.y'/z": invalid char '\''`,
}, {
	path:      `x.y(/z`,
	modErr:    `invalid char '('`,
	importErr: `malformed import path "x.y(/z": invalid char '('`,
}, {
	path:      `x.y)/z`,
	modErr:    `invalid char ')'`,
	importErr: `malformed import path "x.y)/z": invalid char ')'`,
}, {
	path:      `x.y*/z`,
	modErr:    `invalid char '*'`,
	importErr: `malformed import path "x.y*/z": invalid char '*'`,
	fileErr:   `malformed file path "x.y*/z": invalid char '*'`,
}, {
	path:   `x.y+/z`,
	modErr: `invalid char '+'`,
}, {
	path:      `x.y,/z`,
	modErr:    `invalid char ','`,
	importErr: `malformed import path "x.y,/z": invalid char ','`,
}, {
	path:   `x.y-/z`,
	modErr: `trailing '-' in path element`,
}, {
	path:      `x.y./zt`,
	modErr:    `trailing '.' in path element`,
	importErr: `malformed import path "x.y./zt": trailing dot in path element`,
	fileErr:   `malformed file path "x.y./zt": trailing dot in path element`,
}, {
	path:      `x.y:/z`,
	modErr:    `invalid char ':'`,
	importErr: `malformed import path "x.y:/z": invalid char ':'`,
	fileErr:   `malformed file path "x.y:/z": invalid char ':'`,
}, {
	path:      `x.y;/z`,
	modErr:    `invalid char ';'`,
	importErr: `malformed import path "x.y;/z": invalid char ';'`,
	fileErr:   `malformed file path "x.y;/z": invalid char ';'`,
}, {
	path:      `x.y</z`,
	modErr:    `invalid char '<'`,
	importErr: `malformed import path "x.y</z": invalid char '<'`,
	fileErr:   `malformed file path "x.y</z": invalid char '<'`,
}, {
	path:      `x.y=/z`,
	modErr:    `invalid char '='`,
	importErr: `malformed import path "x.y=/z": invalid char '='`,
}, {
	path:      `x.y>/z`,
	modErr:    `invalid char '>'`,
	importErr: `malformed import path "x.y>/z": invalid char '>'`,
	fileErr:   `malformed file path "x.y>/z": invalid char '>'`,
}, {
	path:      `x.y?/z`,
	modErr:    `invalid char '?'`,
	importErr: `malformed import path "x.y?/z": invalid char '?'`,
	fileErr:   `malformed file path "x.y?/z": invalid char '?'`,
}, {
	path:      `x.y@/z`,
	modErr:    `module path inappropriately contains version`,
	importErr: `malformed import path "x.y@/z": import paths can only contain a major version specifier`,
}, {
	path:      `x.y[/z`,
	modErr:    `invalid char '['`,
	importErr: `malformed import path "x.y[/z": invalid char '['`,
}, {
	path:      `x.y\/z`,
	modErr:    `invalid char '\\'`,
	importErr: `malformed import path "x.y\\/z": invalid char '\\'`,
	fileErr:   `malformed file path "x.y\\/z": invalid char '\\'`,
}, {
	path:      `x.y]/z`,
	modErr:    `invalid char ']'`,
	importErr: `malformed import path "x.y]/z": invalid char ']'`,
}, {
	path:      `x.y^/z`,
	modErr:    `invalid char '^'`,
	importErr: `malformed import path "x.y^/z": invalid char '^'`,
}, {
	path:   `x.y_/z`,
	modErr: `trailing '_' in path element`,
}, {
	path:      "x.y`/z",
	modErr:    "invalid char '`'",
	importErr: "malformed import path \"x.y`/z\": invalid char '`'",
	fileErr:   "malformed file path \"x.y`/z\": invalid char '`'",
}, {
	path:      `x.y{/z`,
	modErr:    `invalid char '{'`,
	importErr: `malformed import path "x.y{/z": invalid char '{'`,
}, {
	path:      `x.y}/z`,
	modErr:    `invalid char '}'`,
	importErr: `malformed import path "x.y}/z": invalid char '}'`,
}, {
	path:   `x.y~/z`,
	modErr: `invalid char '~'`,
}, {
	path:      `x.y/z!`,
	modErr:    `invalid char '!'`,
	importErr: `malformed import path "x.y/z!": invalid char '!'`,
}, {
	path:      `x.y/z"`,
	modErr:    `invalid char '"'`,
	importErr: `malformed import path "x.y/z\"": invalid char '"'`,
	fileErr:   `malformed file path "x.y/z\"": invalid char '"'`,
}, {
	path:      `x.y/z#`,
	modErr:    `invalid char '#'`,
	importErr: `malformed import path "x.y/z#": invalid char '#'`,
}, {
	path:      `x.y/z$`,
	modErr:    `invalid char '$'`,
	importErr: `malformed import path "x.y/z$": invalid char '$'`,
}, {
	path:      `x.y/z%`,
	modErr:    `invalid char '%'`,
	importErr: `malformed import path "x.y/z%": invalid char '%'`,
}, {
	path:      `x.y/z&`,
	modErr:    `invalid char '&'`,
	importErr: `malformed import path "x.y/z&": invalid char '&'`,
}, {
	path:      `x.y/z'`,
	modErr:    `invalid char '\''`,
	importErr: `malformed import path "x.y/z'": invalid char '\''`,
	fileErr:   `malformed file path "x.y/z'": invalid char '\''`,
}, {
	path:      `x.y/z(`,
	modErr:    `invalid char '('`,
	importErr: `malformed import path "x.y/z(": invalid char '('`,
}, {
	path:      `x.y/z)`,
	modErr:    `invalid char ')'`,
	importErr: `malformed import path "x.y/z)": invalid char ')'`,
}, {
	path:      `x.y/z*`,
	modErr:    `invalid char '*'`,
	importErr: `malformed import path "x.y/z*": invalid char '*'`,
	fileErr:   `malformed file path "x.y/z*": invalid char '*'`,
}, {
	path:   `x.y/z++`,
	modErr: `invalid char '+'`,
}, {
	path:      `x.y/z,`,
	modErr:    `invalid char ','`,
	importErr: `malformed import path "x.y/z,": invalid char ','`,
}, {
	path:   `x.y/z-`,
	modErr: `trailing '-' in path element`,
}, {
	path: `x.y/z.t`,
}, {
	path: `x.y/z/t`,
}, {
	path:    `x.y/z:`,
	modErr:  `invalid char ':'`,
	fileErr: `malformed file path "x.y/z:": invalid char ':'`,
}, {
	path:      `x.y/z;`,
	modErr:    `invalid char ';'`,
	importErr: `malformed import path "x.y/z;": invalid char ';'`,
	fileErr:   `malformed file path "x.y/z;": invalid char ';'`,
}, {
	path:      `x.y/z<`,
	modErr:    `invalid char '<'`,
	importErr: `malformed import path "x.y/z<": invalid char '<'`,
	fileErr:   `malformed file path "x.y/z<": invalid char '<'`,
}, {
	path:      `x.y/z=`,
	modErr:    `invalid char '='`,
	importErr: `malformed import path "x.y/z=": invalid char '='`,
}, {
	path:      `x.y/z>`,
	modErr:    `invalid char '>'`,
	importErr: `malformed import path "x.y/z>": invalid char '>'`,
	fileErr:   `malformed file path "x.y/z>": invalid char '>'`,
}, {
	path:      `x.y/z?`,
	modErr:    `invalid char '?'`,
	importErr: `malformed import path "x.y/z?": invalid char '?'`,
	fileErr:   `malformed file path "x.y/z?": invalid char '?'`,
}, {
	path:      `x.y/z@`,
	modErr:    `invalid char '@'`,
	importErr: `malformed import path "x.y/z@": invalid char '@'`,
}, {
	path:      `x.y/z[`,
	modErr:    `invalid char '['`,
	importErr: `malformed import path "x.y/z[": invalid char '['`,
}, {
	path:      `x.y/z\`,
	modErr:    `invalid char '\\'`,
	importErr: `malformed import path "x.y/z\\": invalid char '\\'`,
	fileErr:   `malformed file path "x.y/z\\": invalid char '\\'`,
}, {
	path:      `x.y/z]`,
	modErr:    `invalid char ']'`,
	importErr: `malformed import path "x.y/z]": invalid char ']'`,
}, {
	path:      `x.y/z^`,
	modErr:    `invalid char '^'`,
	importErr: `malformed import path "x.y/z^": invalid char '^'`,
}, {
	path:   `x.y/z_`,
	modErr: `trailing '_' in path element`,
}, {
	path:      "x.y/z`",
	modErr:    "invalid char '`'",
	importErr: "malformed import path \"x.y/z`\": invalid char '`'",
	fileErr:   "malformed file path \"x.y/z`\": invalid char '`'",
}, {
	path:      `x.y/z{`,
	modErr:    `invalid char '{'`,
	importErr: `malformed import path "x.y/z{": invalid char '{'`,
}, {
	path:      `x.y/z}`,
	modErr:    `invalid char '}'`,
	importErr: `malformed import path "x.y/z}": invalid char '}'`,
}, {
	path:   `x.y/z~`,
	modErr: `invalid char '~'`,
}, {
	path: `x.y/x.foo`,
}, {
	path:      `x.y/aux.foo`,
	modErr:    `"aux" disallowed as path element component on Windows`,
	importErr: `malformed import path "x.y/aux.foo": "aux" disallowed as path element component on Windows`,
	fileErr:   `malformed file path "x.y/aux.foo": "aux" disallowed as path element component on Windows`,
}, {
	path:      `x.y/prn`,
	modErr:    `"prn" disallowed as path element component on Windows`,
	importErr: `malformed import path "x.y/prn": "prn" disallowed as path element component on Windows`,
	fileErr:   `malformed file path "x.y/prn": "prn" disallowed as path element component on Windows`,
}, {
	path: `x.y/prn2`,
}, {
	path: `x.y/com`,
}, {
	path:      `x.y/com1`,
	modErr:    `"com1" disallowed as path element component on Windows`,
	importErr: `malformed import path "x.y/com1": "com1" disallowed as path element component on Windows`,
	fileErr:   `malformed file path "x.y/com1": "com1" disallowed as path element component on Windows`,
}, {
	path:      `x.y/com1.txt`,
	modErr:    `"com1" disallowed as path element component on Windows`,
	importErr: `malformed import path "x.y/com1.txt": "com1" disallowed as path element component on Windows`,
	fileErr:   `malformed file path "x.y/com1.txt": "com1" disallowed as path element component on Windows`,
}, {
	path: `x.y/calm1`,
}, {
	path:   `x.y/z~`,
	modErr: `invalid char '~'`,
}, {
	path:      `x.y/z~0`,
	modErr:    `invalid char '~'`,
	importErr: `malformed import path "x.y/z~0": trailing tilde and digits in path element`,
}, {
	path:      `x.y/z~09`,
	modErr:    `invalid char '~'`,
	importErr: `malformed import path "x.y/z~09": trailing tilde and digits in path element`,
}, {
	path: `x.y/z09`,
}, {
	path:   `x.y/z09~`,
	modErr: `invalid char '~'`,
}, {
	path:   `x.y/z09~09z`,
	modErr: `invalid char '~'`,
}, {
	path:      `x.y/z09~09z~09`,
	modErr:    `invalid char '~'`,
	importErr: `malformed import path "x.y/z09~09z~09": trailing tilde and digits in path element`,
}, {
	path:      `github.com/!123/logrus`,
	modErr:    `invalid char '!'`,
	importErr: `malformed import path "github.com/!123/logrus": invalid char '!'`,
}, {
	path:      `github.com/user/unicode/испытание`,
	modErr:    `invalid char 'и'`,
	importErr: `malformed import path "github.com/user/unicode/испытание": invalid char 'и'`,
}, {
	path:      `../x`,
	modErr:    `invalid path element ".."`,
	importErr: `malformed import path "../x": invalid path element ".."`,
	fileErr:   `malformed file path "../x": invalid path element ".."`,
}, {
	path:      `./y`,
	modErr:    `invalid path element "."`,
	importErr: `malformed import path "./y": invalid path element "."`,
	fileErr:   `malformed file path "./y": invalid path element "."`,
}, {
	path:    `x:y`,
	modErr:  `invalid char ':'`,
	fileErr: `malformed file path "x:y": invalid char ':'`,
}, {
	path:      `\temp\foo`,
	modErr:    `invalid char '\\'`,
	importErr: `malformed import path "\\temp\\foo": invalid char '\\'`,
	fileErr:   `malformed file path "\\temp\\foo": invalid char '\\'`,
}, {
	path:   `.gitignore`,
	modErr: `leading '.' in path element`,
}, {
	path:   `.github/ISSUE_TEMPLATE`,
	modErr: `leading '.' in path element`,
}, {
	path:      `x☺y`,
	modErr:    `invalid char '☺'`,
	importErr: `malformed import path "x☺y": invalid char '☺'`,
	fileErr:   `malformed file path "x☺y": invalid char '☺'`,
}, {
	path:      `bar.com/foo.`,
	modErr:    `trailing '.' in path element`,
	importErr: `malformed import path "bar.com/foo.": trailing dot in path element`,
	fileErr:   `malformed file path "bar.com/foo.": trailing dot in path element`,
}, {
	path:   `bar.com/_foo`,
	modErr: `leading '_' in path element`,
}, {
	path:   `bar.com/foo___x`,
	modErr: `path does not conform to OCI repository name restrictions; see https://github.com/opencontainers/distribution-spec/blob/HEAD/spec.md#pulling-manifests`,
}, {
	path:   `bar.com/Sushi`,
	modErr: `invalid char 'S'`,
}, {
	path:      `rsc io/quote`,
	modErr:    `invalid char ' '`,
	importErr: `malformed import path "rsc io/quote": invalid char ' '`,
}, {
	path:   `foo.com@v0`,
	modErr: `module path inappropriately contains version`,
}, {
	path: `foo.com/bar/baz`,
}}

func TestCheckPathWithoutVersion(t *testing.T) {
	cuetest.Run(t, checkPathTests, func(t *cuetest.T, test *checkPathTest) {
		t.Logf("path: `%s`", test.path)
		t.Equal(errStr(CheckPathWithoutVersion(test.path)), test.modErr)
	})
}

func TestCheckImportPath(t *testing.T) {
	cuetest.Run(t, checkPathTests, func(t *cuetest.T, test *checkPathTest) {
		t.Logf("path: `%s`", test.path)
		t.Equal(errStr(CheckImportPath(test.path)), test.importErr)
	})
}

func TestCheckFilePath(t *testing.T) {
	cuetest.Run(t, checkPathTests, func(t *cuetest.T, test *checkPathTest) {
		t.Logf("path: `%s`", test.path)
		t.Equal(errStr(CheckFilePath(test.path)), test.fileErr)
	})
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
}, {
	path:         "local",
	vers:         "",
	wantPath:     "local",
	wantBasePath: "local",
}, {
	path:      "local",
	vers:      "v0.1.2",
	wantError: `module 'local' cannot have version`,
}, {
	path:      "local@v1",
	vers:      "",
	wantError: `module 'local' cannot have version`,
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

var escapeVersionTests = []struct {
	v   string
	esc string // empty means same as path
}{
	{v: "v1.2.3-alpha"},
	{v: "v3"},
	{v: "v2.3.1-ABcD", esc: "v2.3.1-!a!bc!d"},
}

func TestEscapeVersion(t *testing.T) {
	for _, tt := range escapeVersionTests {
		esc, err := EscapeVersion(tt.v)
		if err != nil {
			t.Errorf("EscapeVersion(%q): unexpected error: %v", tt.v, err)
			continue
		}
		want := tt.esc
		if want == "" {
			want = tt.v
		}
		if esc != want {
			t.Errorf("EscapeVersion(%q) = %q, want %q", tt.v, esc, want)
		}
	}
}

func TestEscapePath(t *testing.T) {
	// Check invalid paths.
	for _, tt := range checkPathTests {
		if tt.modErr != "" {
			_, err := EscapePath(tt.path)
			if err == nil {
				t.Errorf("EscapePath(%q): succeeded, want error (invalid path)", tt.path)
			}
		}
	}
	path := "foo.com/bar"
	esc, err := EscapePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if esc != path {
		t.Fatalf("EscapePath(%q) = %q, want %q", path, esc, path)
	}
}

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
	wantCanonical: "foo.com/bar/.../blah",
}, {
	testName: "WithPatternAtEnd",
	path:     "foo.com/bar/...",
	want: ImportPath{
		Path:              "foo.com/bar/...",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "",
	},
	wantCanonical: "foo.com/bar/...",
}, {
	testName: "WithUnderscoreLastElement",
	path:     "foo.com/bar/_foo",
	want: ImportPath{
		Path:              "foo.com/bar/_foo",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "_foo",
	},
	wantCanonical: "foo.com/bar/_foo",
}, {
	testName: "WithHashLastElement",
	path:     "foo.com/bar/#foo",
	want: ImportPath{
		Path:              "foo.com/bar/#foo",
		Version:           "",
		ExplicitQualifier: false,
		Qualifier:         "",
	},
	wantCanonical: "foo.com/bar/#foo",
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
			qt.Assert(t, qt.Equals(parts.Canonical().String(), test.wantCanonical))
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

func errStr(err error) string {
	if err == nil {
		return ""
	}
	if e := err.Error(); e != "" {
		return e
	}
	panic("non-nil error with empty string")
}
