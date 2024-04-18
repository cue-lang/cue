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
	"fmt"
	"io"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	"golang.org/x/tools/txtar"

	"cuelabs.dev/go/oci/ociregistry/ocimem"

	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

func newTestClient(t *testing.T) *Client {
	return NewClient(ocimem.New())
}

func TestPutGetModule(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "example.com/module@v1"
language: version: "v0.8.0"

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

func TestModuleVersions(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)
	for _, v := range []string{"v1.0.0", "v2.3.3-alpha", "v1.2.3", "v0.23.676", "v3.2.1"} {
		mpath := "example.com/module@" + semver.Major(v)
		modContents := fmt.Sprintf(`
-- cue.mod/module.cue --
module: %q
language: version: "v0.8.0"

-- x.cue --
x: 42
`, mpath)
		putModule(t, c, module.MustParseVersion("example.com/module@"+v), modContents)
	}
	tags, err := c.ModuleVersions(ctx, "example.com/module")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(tags, []string{"v0.23.676", "v1.0.0", "v1.2.3", "v2.3.3-alpha", "v3.2.1"}))

	tags, err = c.ModuleVersions(ctx, "example.com/module@v1")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(tags, []string{"v1.0.0", "v1.2.3"}))
}

func TestPutGetWithDependencies(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "foo.com/bar@v0"
language: version: "v0.8.0"
deps: "example.com@v1": v: "v1.2.3"
deps: "other.com/something@v0": v: "v0.2.3"

-- x.cue --
package bar

import (
	a "example.com"
	"other.com/something"
)
x: a.foo + something.bar
`
	ctx := context.Background()
	mv := module.MustParseVersion("foo.com/bar@v0.5.100")
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
	qt.Assert(t, qt.DeepEquals(tags, []string{"v0.5.100"}))
}

func TestGetModuleWithManifest(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "foo.com/bar@v0"
language: version: "v0.8.0"
deps: "example.com@v1": v: "v1.2.3"
deps: "other.com/something@v0": v: "v0.2.3"

-- x.cue --
package bar

import (
	a "example.com"
	"other.com/something"
)
x: a.foo + something.bar
`
	ctx := context.Background()
	mv := module.MustParseVersion("foo.com/bar@v0.5.100")
	reg := ocimem.New()

	c := NewClient(reg)
	zipData := putModule(t, c, mv, testMod)

	mr, err := reg.GetTag(ctx, "foo.com/bar", "v0.5.100")
	qt.Assert(t, qt.IsNil(err))
	defer mr.Close()
	mdata, err := io.ReadAll(mr)
	qt.Assert(t, qt.IsNil(err))

	// Remove the tag so that we're sure it isn't
	// used for the GetModuleWithManifest call.
	err = reg.DeleteTag(ctx, "foo.com/bar", "v0.5.100")
	qt.Assert(t, qt.IsNil(err))

	m, err := c.GetModuleWithManifest(ctx, mv, mdata, "application/json")
	qt.Assert(t, qt.IsNil(err))

	r, err := m.GetZip(ctx)
	qt.Assert(t, qt.IsNil(err))
	data, err := io.ReadAll(r)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(data, zipData))
}

func TestPutWithInvalidDependencyVersion(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "foo.com/bar@v0"
language: version: "v0.8.0"
deps: "example.com@v1": v: "v1.2"

-- x.cue --
x: 42
`
	mv := module.MustParseVersion("foo.com/bar@v0.5.100")
	c := newTestClient(t)
	zipData := createZip(t, mv, testMod)
	err := c.PutModule(context.Background(), mv, bytes.NewReader(zipData), int64(len(zipData)))
	qt.Assert(t, qt.ErrorMatches(err, `module.cue file check failed: invalid module.cue file cue.mod/module.cue: cannot make version from module "example.com@v1", version "v1.2": version "v1.2" \(of module "example.com@v1"\) is not canonical`))
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
language: version: "v0.8.0"
`,
}, {
	testName: "MismatchedMajorVersion",
	mv:       module.MustNewVersion("foo.com/bar", "v0.1.2"),
	content: `
-- cue.mod/module.cue --
module: "foo.com/bar@v1"
language: version: "v0.8.0"
`,
	wantError: `module.cue file check failed: module path "foo.com/bar@v1" found in cue.mod/module.cue does not match module path being published "foo.com/bar@v0"`,
}, {
	testName: "ModuleWithMinorVersion",
	mv:       module.MustNewVersion("foo.com/bar", "v1.2.3"),
	content: `
-- cue.mod/module.cue --
module: "foo@v1.2.3"
language: version: "v0.8.0"
`,
	wantError: `module.cue file check failed: module path foo@v1.2.3 in "cue.mod/module.cue" should contain the major version only`,
}, {
	testName: "DependencyWithInvalidVersion",
	mv:       module.MustNewVersion("foo.com/bar", "v1.2.3"),
	content: `
-- cue.mod/module.cue --
module: "foo@v1"
language: version: "v0.8.0"
deps: "foo.com/bar@v2": v: "invalid"
`,
	wantError: `module.cue file check failed: invalid module.cue file cue.mod/module.cue: cannot make version from module "foo.com/bar@v2", version "invalid": version "invalid" \(of module "foo.com/bar@v2"\) is not well formed`,
}}

func TestCheckModule(t *testing.T) {
	for _, test := range checkModuleTests {
		t.Run(test.testName, func(t *testing.T) {
			data := createZip(t, test.mv, test.content)
			m, err := checkModule(test.mv, bytes.NewReader(data), int64(len(data)))
			if test.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Not(qt.IsNil(m)))
			qt.Assert(t, qt.DeepEquals(m.mv, test.mv))
		})
	}
}

func TestModuleVersionsOnNonExistentModule(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	tags, err := c.ModuleVersions(ctx, "not/there@v0")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.HasLen(tags, 0))

	// Bad names hit a slightly different code path, so make
	// sure they work OK too.
	tags, err = c.ModuleVersions(ctx, "bad--NAME-@v0")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.HasLen(tags, 0))
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
	return txtarFileInfo{f}, nil
}

func (txtarFileIO) Open(f txtar.File) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.Data)), nil
}

func (txtarFileIO) Mode() os.FileMode {
	return 0o444
}

type txtarFileInfo struct {
	f txtar.File
}

func (fi txtarFileInfo) Name() string {
	return path.Base(fi.f.Name)
}

func (fi txtarFileInfo) Size() int64 {
	return int64(len(fi.f.Data))
}

func (fi txtarFileInfo) Mode() os.FileMode {
	return 0o644
}

func (fi txtarFileInfo) ModTime() time.Time { return time.Time{} }
func (fi txtarFileInfo) IsDir() bool        { return false }
func (fi txtarFileInfo) Sys() interface{}   { return nil }
