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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	"golang.org/x/tools/txtar"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocimem"

	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

func newTestClient(t *testing.T) *Client {
	return NewClient(ocimem.NewWithConfig(&ocimem.Config{ImmutableTags: true}))
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

func TestNotFound(t *testing.T) {
	// Check that we get appropriate not-found behavior when the
	// HTTP response isn't entirely according to spec.
	// See https://cuelang.org/issue/2982 for an example.
	var writeResponse func(w http.ResponseWriter)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		writeResponse(w)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	reg, err := ociclient.New(u.Host, &ociclient.Options{
		Insecure: true,
	})
	qt.Assert(t, qt.IsNil(err))
	client := NewClient(reg)
	checkNotFound := func(writeResp func(w http.ResponseWriter)) {
		ctx := context.Background()
		writeResponse = writeResp
		mv := module.MustNewVersion("foo.com/bar@v1", "v1.2.3")
		_, err := client.GetModule(ctx, mv)
		qt.Assert(t, qt.ErrorIs(err, ErrNotFound))
		versions, err := client.ModuleVersions(ctx, "foo/bar")
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.HasLen(versions, 0))
	}

	checkNotFound(func(w http.ResponseWriter) {
		// issue 2982
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[{"code":"NOT_FOUND","message":"repository playground/cue/github.com not found"}]}`))
	})
	checkNotFound(func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`some other message`))
	})
}

func TestPutWithMetadata(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "foo.com/bar@v0"
language: version: "v0.8.0"

-- x.cue --
package bar
`
	ctx := context.Background()
	mv := module.MustParseVersion("foo.com/bar@v0.5.100")
	c := newTestClient(t)
	zipData := createZip(t, mv, testMod)
	meta := &Metadata{
		VCSType:       "git",
		VCSCommit:     "2ff5afa7cda41bf030654ab03caeba3fadf241ae",
		VCSCommitTime: time.Date(2024, 4, 23, 15, 16, 17, 0, time.UTC),
	}
	err := c.PutModuleWithMetadata(context.Background(), mv, bytes.NewReader(zipData), int64(len(zipData)), meta)
	qt.Assert(t, qt.IsNil(err))

	m, err := c.GetModule(ctx, mv)
	qt.Assert(t, qt.IsNil(err))

	gotMeta, err := m.Metadata()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(gotMeta, meta))
}

func TestPutWithInvalidMetadata(t *testing.T) {
	const testMod = `
-- cue.mod/module.cue --
module: "foo.com/bar@v0"
language: version: "v0.8.0"

-- x.cue --
package bar
`
	ctx := context.Background()
	mv := module.MustParseVersion("foo.com/bar@v0.5.100")
	c := newTestClient(t)
	zipData := createZip(t, mv, testMod)
	meta := &Metadata{
		// Missing VCSType field.
		VCSCommit:     "2ff5afa7cda41bf030654ab03caeba3fadf241ae",
		VCSCommitTime: time.Date(2024, 4, 23, 15, 16, 17, 0, time.UTC),
	}
	err := c.PutModuleWithMetadata(ctx, mv, bytes.NewReader(zipData), int64(len(zipData)), meta)
	qt.Assert(t, qt.ErrorMatches(err, `invalid metadata: empty metadata value for field "org.cuelang.vcs-type"`))
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
	// Note that we delete a tag below, so we want a mutable registry.
	reg := ocimem.NewWithConfig(&ocimem.Config{ImmutableTags: false})

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

	m, err := c.GetModuleWithManifest(mv, mdata, "application/json")
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
	err := modzip.Create(&zipContent, mv, ar.Files, txtarFileIO{})
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
