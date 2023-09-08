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

package modregistry

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	"golang.org/x/tools/txtar"

	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"

	"cuelang.org/go/internal/mod/module"
	modzip "cuelang.org/go/internal/mod/zip"
)

func newTestClient(t *testing.T) *Client {
	srv := httptest.NewServer(ociserver.New(ocimem.New(), nil))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "")
	if err != nil {
		log.Fatal(err)
	}
	return c
}

func TestPutGetModule(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "example.com/module@v1"

-- x.cue --
x: 42
`
	ctx := context.Background()
	mv := module.MustParseVersion("example.com/module@v1.2.3")
	c := newTestClient(t)
	zipData := putModule(t, c, mv, testMod)

	m, err := c.GetModule(ctx, mv)
	qt.Assert(t, qt.IsNil(err))

	r, err := m.GetZip(ctx)
	qt.Assert(t, qt.IsNil(err))
	data, err := io.ReadAll(r)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(data, zipData))

	tags, err := c.ModuleVersions(ctx, mv.Path())
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(tags, []string{"v1.2.3"}))
}

func TestPutWithDependencies(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "example.com/module@v1"

module: "foo.com/bar@v0"
deps: "example.com@v1": v: "v1.2.3"
deps: "other.com/something@v0": v: "v0.2.3"

-- x.cue --
x: 42
`
}

var checkModuleTests = []struct {
	testName  string
	mv        module.Version
	content   string
	wantError string
}{{
	testName: "Minimal",
	mv:       module.MustNewVersion("foo.com/bar", "v0.1.2"),
	content: `
-- cue.mod/module.cue --
module: "foo.com/bar@v0"
`,
}, {
	testName: "MismatchedMajorVersion",
	mv:       module.MustNewVersion("foo.com/bar", "v0.1.2"),
	content: `
-- cue.mod/module.cue --
module: "foo.com/bar@v1"
`,
	wantError: `module.cue file check failed: module path "foo.com/bar@v1" found in cue.mod/module.cue does not match module path being published "foo.com/bar@v0"`,
}, {
	testName: "ModuleWithMinorVersion",
	mv:       module.MustNewVersion("foo.com/bar", "v1.2.3"),
	content: `
-- cue.mod/module.cue --
module: "foo@v1.2.3"
`,
	wantError: `module.cue file check failed: module path foo@v1.2.3 in "cue.mod/module.cue" should contain the major version only`,
}, {
	testName: "DependencyWithInvalidVersion",
	mv:       module.MustNewVersion("foo.com/bar", "v1.2.3"),
	content: `
-- cue.mod/module.cue --
module: "foo@v1"
deps: "foo.com/bar@v2": v: "invalid"
`,
	wantError: `module.cue file check failed: invalid module.cue file cue.mod/module.cue: cannot make version from module "foo.com/bar@v2", version "invalid": version "invalid" \(of module "foo.com/bar@v2"\) is not well formed`,
}}

func TestCheckModule(t *testing.T) {
	for _, test := range checkModuleTests {
		t.Run(test.testName, func(t *testing.T) {
			data := createZip(t, test.mv, test.content)
			m, err := CheckModule(test.mv, bytes.NewReader(data), int64(len(data)))
			if test.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Not(qt.IsNil(m)))
			qt.Assert(t, qt.DeepEquals(m.Version(), test.mv))
		})
	}
}

func putModule(t *testing.T, c *Client, mv module.Version, txtarData string) []byte {
	zipData := createZip(t, mv, txtarData)
	err := c.PutModule(context.Background(), mv, bytes.NewReader(zipData), int64(len(zipData)))
	qt.Assert(t, qt.IsNil(err))
	return zipData
}

func createZip(t *testing.T, mv module.Version, txtarData string) []byte {
	ar := txtar.Parse([]byte(txtarData))
	var zipContent bytes.Buffer
	err := modzip.Create[txtar.File](&zipContent, mv, ar.Files, txtarFileIO{})
	qt.Assert(t, qt.IsNil(err))
	return zipContent.Bytes()
}

type txtarFileIO struct{}

func (txtarFileIO) Path(f txtar.File) string {
	return f.Name
}

func (txtarFileIO) Lstat(f txtar.File) (os.FileInfo, error) {
	return fakeFileInfo{f}, nil
}

func (txtarFileIO) Open(f txtar.File) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.Data)), nil
}

func (txtarFileIO) Mode() os.FileMode {
	return 0o444
}

type fakeFileInfo struct {
	f txtar.File
}

func (fi fakeFileInfo) Name() string {
	return path.Base(fi.f.Name)
}

func (fi fakeFileInfo) Size() int64 {
	return int64(len(fi.f.Data))
}

func (fi fakeFileInfo) Mode() os.FileMode {
	return 0o644
}

func (fi fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi fakeFileInfo) IsDir() bool        { return false }
func (fi fakeFileInfo) Sys() interface{}   { return nil }
