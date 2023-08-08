package modregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"text/template"
	"time"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"github.com/go-quicktest/qt"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/mod/module"
	modzip "cuelang.org/go/mod/zip"
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

	data, err = m.ModuleFile(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(data), string(ar.Files[0].Data)))
}

type modspec struct {
	Name    string
	Deps    []string
	Imports []string
}

func TestPutWithDependencies(t *testing.T) {
	mods := []modspec{{
		Name: "example.com/f@v1.2.3",
		Deps: []string{
			"example.com/a@v1.9.9",
			"example.com/b@v3.0.0",
			"example.com/c@v0.0.0",
			"example.com/d@v0.1.2",
			"example.com/e@v2.3.4",
		},
		Imports: []string{
			"example.com/d",
			"example.com/e",
		},
	}, {
		Name: "example.com/e@v2.3.4",
	}, {
		Name: "example.com/d@v0.1.2",
		Deps: []string{
			"example.com/a@v1.9.9",
			"example.com/b@v3.0.0",
			"example.com/c@v0.0.0",
		},
		Imports: []string{
			"example.com/b",
			"example.com/c",
		},
	}, {
		Name: "example.com/c@v0.0.0",
		Deps: []string{
			"example.com/a@v1.9.9",
			"example.com/b@v3.0.0",
		},
		Imports: []string{
			"example.com/a",
			"example.com/b",
		},
	}, {
		Name: "example.com/a@v1.9.9",
	}, {
		Name: "example.com/b@v3.0.0",
	}}
	ctx := context.Background()
	c := newTestClient(t)
	zipData := make(map[module.Version][]byte)
	found := make(map[module.Version]bool)
	// In reverse order so we push deps first.
	for i := len(mods) - 1; i >= 0; i-- {
		m := mods[i]
		ar := txtar.Parse(txtarWithDeps(m))
		v := module.MustParseVersion(m.Name)
		zipData[v] = putModule(t, c, v, ar)
		if i > 0 {
			found[v] = false
		}
	}
	m, err := c.GetModule(ctx, module.MustParseVersion(mods[0].Name))
	qt.Assert(t, qt.IsNil(err))
	deps, err := m.Dependencies(ctx)
	qt.Assert(t, qt.HasLen(deps, len(mods)-1))

	for v, dep := range deps {
		qt.Assert(t, qt.Equals(v, dep.Version()))
		found[v] = true
		r, err := dep.GetZip(ctx)
		qt.Assert(t, qt.IsNil(err))
		data, err := io.ReadAll(r)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.DeepEquals(data, zipData[v]))
	}
	for v, ok := range found {
		qt.Assert(t, qt.Equals(ok, true), qt.Commentf("version %v", v))
	}
}

func TestGetNonExistentModule(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	m, err := c.GetModule(ctx, module.MustParseVersion("example.com/module@v1.2.3"))
	qt.Check(t, qt.ErrorMatches(err, `module "example.com/module@v1.2.3": module not found`))
	qt.Check(t, qt.ErrorIs(err, ErrNotFound))
	qt.Check(t, qt.IsNil(m))
}

func TestGetNonModuleArtifact(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(ociserver.New(ocimem.New(), nil))
	t.Cleanup(srv.Close)

	// Push a manifest that doesn't have the CUE module media type.
	repo := "cue/example.com/test"
	manifest := &ociregistry.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    pushBlob(t, srv.URL, repo, ocispec.MediaTypeImageManifest, "{}"),
	}
	data, err := json.Marshal(manifest)
	qt.Assert(t, qt.IsNil(err))
	_, err = ociclient.New(srv.URL, nil).PushManifest(ctx, repo, "v1.0.0", data, ocispec.MediaTypeImageManifest)
	qt.Assert(t, qt.IsNil(err))

	c, err := NewClient(srv.URL, "")
	if err != nil {
		log.Fatal(err)
	}
	mv := module.MustParseVersion("example.com/test@v1.0.0")
	_, err = c.GetModule(ctx, mv)
	qt.Assert(t, qt.ErrorMatches(err, `example.com/test@v1.0.0 does not resolve to a manifest of the correct type \(media type is "application/vnd.oci.image.manifest.v1\+json"\)`))
}

func pushBlob(t *testing.T, uri, repo, mediaType, content string) ociregistry.Descriptor {
	desc := ocispec.Descriptor{
		Digest:    digest.FromString(content),
		MediaType: mediaType,
		Size:      int64(len(content)),
	}
	c := ociclient.New(uri, nil)
	desc, err := c.PushBlob(context.Background(), repo, desc, strings.NewReader(content))
	qt.Assert(t, qt.IsNil(err))
	return desc
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

var depsTmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"basename": path.Base,
	"version":  module.MustParseVersion,
}).Parse(`
{{$vers := .Name | version -}}
-- cue.mod/module.cue --
module: {{printf "%q" $vers.Path}}
{{range $d := .Deps}}{{$dv := $d | version}}deps: {{printf "%q" $dv.Path}}: v: {{printf "%q" $dv.Version}}
{{end}}
-- x.cue --
package {{$vers.BasePath | basename}}
{{if .Deps}}
import (
{{range $d := .Deps}}	{{printf "%q" (version $d).BasePath}}
{{end}}
){{end}}
`))

// txtarWithDeps returns a txtar archive that contains the contents for
// a module with name@version m.name, depends on m.deps (which
// should include transitive deps) and imports m.imports.
func txtarWithDeps(m modspec) []byte {
	var buf bytes.Buffer
	err := depsTmpl.Execute(&buf, m)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}
