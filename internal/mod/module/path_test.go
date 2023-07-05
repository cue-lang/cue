package module

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var checkPathTests = []struct {
	path     string
	pathOK   bool
	importOK bool
	fileOK   bool
}{
	{"x.y/z@v0", true, true, true},
	{"x.y@v0", true, true, true},
	{"x.y@v0.1", false, false, true},

	{"", false, false, false},
	{"x.y/\xFFz", false, false, false},
	{"/x.y/z@v0", false, false, false},
	{"x./z@v0", false, false, false},
	{".x/z@v0", false, true, true},
	{"-x/z@v0", false, false, true},
	{"x..y/z@v0", false, true, true},
	{"x.y/z/../../w@v0", false, false, false},
	{"x.y//z@v0", false, false, false},
	{"x.y/z//w@v0", false, false, false},
	{"x.y/z/@v0", false, false, true},

	{"x.y/z@v0", true, true, true},
	{"x.y/z@v1", true, true, true},
	{"x.y/z@v2", true, true, true},
	{"x.y/z/@v2", false, false, true},
	{"x.y/z/v2.0@v0", true, true, true},
	{"X.y/z@v0", false, true, true},

	{"!x.y/z@v0", false, false, true},
	{"_x.y/z@v0", false, true, true},
	{"x.y!/z@v0", false, false, true},
	{"x.y\"/z@v0", false, false, false},
	{"x.y#/z@v0", false, false, true},
	{"x.y$/z@v0", false, false, true},
	{"x.y%/z@v0", false, false, true},
	{"x.y&/z@v0", false, false, true},
	{"x.y'/z@v0", false, false, false},
	{"x.y(/z@v0", false, false, true},
	{"x.y)/z@v0", false, false, true},
	{"x.y*/z@v0", false, false, false},
	{"x.y+/z@v0", false, true, true},
	{"x.y,/z@v0", false, false, true},
	{"x.y-/z@v0", false, true, true}, // TODO should this be an invalid import path?
	{"x.y./zt@v0", false, false, false},
	{"x.y:/z@v0", false, false, false},
	{"x.y;/z@v0", false, false, false},
	{"x.y</z@v0", false, false, false},
	{"x.y=/z@v0", false, false, true},
	{"x.y>/z@v0", false, false, false},
	{"x.y?/z@v0", false, false, false},
	{"x.y@/z", false, false, true},
	{"x.y@/z@v0", false, false, true},
	{"x.y[/z@v0", false, false, true},
	{"x.y\\/z@v0", false, false, false},
	{"x.y]/z@v0", false, false, true},
	{"x.y^/z@v0", false, false, true},
	{"x.y_/z@v0", false, true, true},
	{"x.y`/z@v0", false, false, false},
	{"x.y{/z@v0", false, false, true},
	{"x.y}/z@v0", false, false, true},
	{"x.y~/z@v0", false, true, true}, // TODO should this be an invalid import path?
	{"x.y/z!@v0", false, false, true},
	{"x.y/z\"@v0", false, false, false},
	{"x.y/z#@v0", false, false, true},
	{"x.y/z$@v0", false, false, true},
	{"x.y/z%@v0", false, false, true},
	{"x.y/z&@v0", false, false, true},
	{"x.y/z'@v0", false, false, false},
	{"x.y/z(@v0", false, false, true},
	{"x.y/z)@v0", false, false, true},
	{"x.y/z*@v0", false, false, false},
	{"x.y/z++@v0", false, true, true},
	{"x.y/z,@v0", false, false, true},
	{"x.y/z-@v0", false, true, true},
	{"x.y/z.t@v0", true, true, true},
	{"x.y/z/t@v0", true, true, true},
	{"x.y/z:@v0", false, false, false},
	{"x.y/z;@v0", false, false, false},
	{"x.y/z<@v0", false, false, false},
	{"x.y/z=@v0", false, false, true},
	{"x.y/z>@v0", false, false, false},
	{"x.y/z?@v0", false, false, false},
	{"x.y/z@", false, false, true},
	{"x.y/z@@v0", false, false, true},
	{"x.y/z[@v0", false, false, true},
	{"x.y/z\\@v0", false, false, false},
	{"x.y/z]@v0", false, false, true},
	{"x.y/z^@v0", false, false, true},
	{"x.y/z_@v0", false, true, true},
	{"x.y/z`@v0", false, false, false},
	{"x.y/z{@v0", false, false, true},
	{"x.y/z}@v0", false, false, true},
	{"x.y/z~@v0", false, true, true},
	{"x.y/x.foo@v0", true, true, true},
	{"x.y/aux.foo@v0", false, false, false},
	{"x.y/prn/x@v0", false, false, false},
	{"x.y/prn", false, false, false},
	{"x.y/prn@v0", false, false, true},
	{"x.y/prn2", false, true, true},
	{"x.y/prn2@v0", true, true, true},
	{"x.y/com", false, true, true},
	{"x.y/com@v0", true, true, true},
	{"x.y/com1@v0", false, false, true},
	{"x.y/com1", false, false, false},
	{"x.y/com1.txt@v0", false, false, false},
	{"x.y/calm1@v0", true, true, true},
	{"x.y/z~", false, true, true},
	{"x.y/z~@v0", false, true, true},
	{"x.y/z~0", false, false, true},
	{"x.y/z~0@v0", false, false, true},
	{"x.y/z~09", false, false, true},
	{"x.y/z~09@v0", false, false, true},
	{"x.y/z09@v0", true, true, true},
	{"x.y/z09~@v0", false, true, true},
	{"x.y/z09~", false, true, true},
	{"x.y/z09~09z@v0", false, true, true},
	{"x.y/z09~09z", false, true, true},
	{"x.y/z09~09z~09@v0", false, false, true},
	{"x.y/z09~09z~09", false, false, true},
	{"github.com/!123/logrus", false, false, true},
	{"github.com/!123/logrus@v0", false, false, true},

	// TODO: CL 41822 allowed Unicode letters in old "go get"
	// without due consideration of the implications, and only on github.com (!).
	// For now, we disallow non-ASCII characters in module mode,
	// in both module paths and general import paths,
	// until we can get the implications right.
	// When we do, we'll enable them everywhere, not just for GitHub.
	{"github.com/user/unicode/испытание@v0", false, false, true},

	{"../x@v0", false, false, false},
	{"./y@v0", false, false, false},
	{"x:y@v0", false, false, false},
	{`\temp\foo`, false, false, false},
	{".gitignore@v0", false, true, true},
	{".github/ISSUE_TEMPLATE@v0", false, true, true},
	{"x☺y@v0", false, false, false},
}

func TestCheckPath(t *testing.T) {
	for _, tt := range checkPathTests {
		t.Run(tt.path, func(t *testing.T) {
			err := CheckPath(tt.path)
			if tt.pathOK && err != nil {
				t.Errorf("CheckPath(%q) = %v, wanted nil error", tt.path, err)
			} else if !tt.pathOK && err == nil {
				t.Errorf("CheckPath(%q) succeeded, wanted error", tt.path)
			}
		})
	}
}

func TestCheckFilePath(t *testing.T) {
	for _, tt := range checkPathTests {
		t.Run(tt.path, func(t *testing.T) {
			err := CheckFilePath(tt.path)
			if tt.fileOK && err != nil {
				t.Errorf("CheckFilePath(%q) = %v, wanted nil error", tt.path, err)
			} else if !tt.fileOK && err == nil {
				t.Errorf("CheckFilePath(%q) succeeded, wanted error", tt.path)
			}
		})
	}
}

func TestCheckImportPath(t *testing.T) {
	for _, tt := range checkPathTests {
		t.Run(tt.path, func(t *testing.T) {
			err := CheckImportPath(tt.path)
			if tt.importOK && err != nil {
				t.Errorf("CheckImportPath(%q) = %v, wanted nil error", tt.path, err)
			} else if !tt.importOK && err == nil {
				t.Errorf("CheckImportPath(%q) succeeded, wanted error", tt.path)
			}
		})
	}
}

var splitPathVersionTests = []struct {
	path        string
	wantPath    string
	wantVersion string
	wantOK      bool
}{{
	path:        "example.com/foo@v0",
	wantPath:    "example.com/foo",
	wantVersion: "v0",
	wantOK:      true,
}, {
	path:        "example.com/foo@v1234",
	wantPath:    "example.com/foo",
	wantVersion: "v1234",
	wantOK:      true,
}, {
	path:        "example.com/foo@v1",
	wantPath:    "example.com/foo",
	wantVersion: "v1",
	wantOK:      true,
}, {
	path:        "example.com@v1.2.3",
	wantPath:    "example.com",
	wantVersion: "v1.2.3",
	wantOK:      true,
}, {
	path: "example.com/foo",
}, {
	path: "@v1",
}, {
	path: "foo.com@v0123",
}, {
	path: "example.com@v0@v1",
}, {
	path: "example.com@",
}, {
	path: "example.com@x1",
}, {
	path: "example.com@v",
}, {
	path: "example.com@v1a",
}, {
	path: "example.com@vx1",
}}

func TestSplitPathVersion(t *testing.T) {
	for _, test := range splitPathVersionTests {
		t.Run(test.path, func(t *testing.T) {
			gotPath, gotVersion, gotOK := SplitPathVersion(test.path)
			qt.Assert(t, qt.Equals(gotOK, test.wantOK))
			qt.Check(t, qt.Equals(gotPath, test.wantPath))
			qt.Check(t, qt.Equals(gotVersion, test.wantVersion))
		})
	}
}
