package ociregistry

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
	ar := txtar.Parse([]byte(testMod))
	mv := module.MustParseVersion("example.com/module@v1.2.3")
	c := newTestClient(t)
	zipData := putModule(t, c, mv, ar)

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

func putModule(t *testing.T, c *Client, mv module.Version, content *txtar.Archive) []byte {
	var zipContent bytes.Buffer
	err := modzip.Create[txtar.File](&zipContent, mv, content.Files, txtarFileIO{})
	qt.Assert(t, qt.IsNil(err))
	zipData := zipContent.Bytes()
	err = c.PutModule(context.Background(), mv, bytes.NewReader(zipData), int64(len(zipData)))
	qt.Assert(t, qt.IsNil(err))
	return zipData
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
