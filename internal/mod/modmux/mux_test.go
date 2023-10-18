package modmux

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"testing"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocifilter"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/modresolve"
	"cuelang.org/go/internal/mod/module"
	modzip "cuelang.org/go/internal/mod/zip"
	"cuelang.org/go/internal/registrytest"
)

const contents = `
-- r0/example.com_v0.0.1/cue.mod/module.cue --
module: "example.com@v0"

-- r0/example.com_v0.0.1/x.cue --
package x
"r0/example.com_v0.0.1"

-- r0/example.com_v0.0.2/cue.mod/module.cue --
module: "example.com@v0"

-- r0/example.com_v0.0.2/x.cue --
package x
"r0/example.com_v0.0.2"

-- r0/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"

-- r0/example.com_foo_v0.0.1/x.cue --
package x
"r0/example.com_foo_v0.0.1"

-- r1/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"

-- r1/example.com_foo_v0.0.1/x.cue --
package x
"r1/example.com_foo_v0.0.1"
`

func TestMux(t *testing.T) {
	rfs := registrytest.TxtarFS(txtar.Parse([]byte(contents)))
	const numRegistries = 2
	registries := make([]*registrytest.Registry, numRegistries)
	for i := 0; i < numRegistries; i++ {
		rfs1, _ := fs.Sub(rfs, fmt.Sprintf("r%d", i))
		// TODO non-empty prefixes.
		r, err := registrytest.New(rfs1, "")
		qt.Assert(t, qt.IsNil(err), qt.Commentf("r%d", i))
		registries[i] = r
		defer r.Close()
	}
	resolver, err := modresolve.ParseCUERegistry(fmt.Sprintf("example.com=%s,example.com/foo=%s", registries[0].Host(), registries[1].Host()), "fallback.registry/subdir")
	qt.Assert(t, qt.IsNil(err))

	fallback := ocimem.New()

	muxr := New(resolver, func(host string, insecure bool) (ociregistry.Interface, error) {
		if host == "fallback.registry" {
			return fallback, nil
		}
		return ociclient.New(host, &ociclient.Options{
			Insecure: insecure,
		})
	})
	ctx := context.Background()
	modc := modregistry.NewClient(muxr)

	qt.Assert(t, qt.StringContains(fetchXCUE(t, modc, "example.com", "v0.0.1"), `"r0/example.com_v0.0.1"`))
	qt.Assert(t, qt.StringContains(fetchXCUE(t, modc, "example.com/foo", "v0.0.1"), `"r1/example.com_foo_v0.0.1"`))

	versions, err := modc.ModuleVersions(ctx, "example.com@v0")
	qt.Assert(t, qt.DeepEquals(versions, []string{"v0.0.1", "v0.0.2"}))
	versions, err = modc.ModuleVersions(ctx, "example.com/foo@v0")
	qt.Assert(t, qt.DeepEquals(versions, []string{"v0.0.1"}))

	// Verify that we can put a module too.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	zw.AddFS(registrytest.TxtarFS(txtar.Parse([]byte(`
-- cue.mod/module.cue --
module: "other.com/a/b@v1"
-- x.cue --
package x
"other.com/a/b@v1.2.3"
`))))
	zw.Close()
	err = modc.PutModule(ctx, module.MustNewVersion("other.com/a/b", "v1.2.3"), bytes.NewReader(zbuf.Bytes()), int64(zbuf.Len()))
	qt.Assert(t, qt.IsNil(err))

	qt.Assert(t, qt.StringContains(fetchXCUE(t, modc, "other.com/a/b", "v1.2.3"), `"other.com/a/b@v1.2.3"`))

	// Check that the module we've just put ended up in the correct place.
	modc1 := modregistry.NewClient(ocifilter.Sub(fallback, "subdir"))

	qt.Assert(t, qt.StringContains(fetchXCUE(t, modc1, "other.com/a/b", "v1.2.3"), `"other.com/a/b@v1.2.3"`))
}

// fetchXCUE returns the contents of the x.cue file inside the
// module with the given path and version.
func fetchXCUE(t *testing.T, mclient *modregistry.Client, mpath string, vers string) string {
	ctx := context.Background()

	mv := module.MustNewVersion(mpath, vers)
	m, err := mclient.GetModule(ctx, mv)
	qt.Assert(t, qt.IsNil(err))
	mzipr, err := m.GetZip(ctx)
	qt.Assert(t, qt.IsNil(err))
	data, err := io.ReadAll(mzipr)
	qt.Assert(t, qt.IsNil(err))
	zipr, _, _, err := modzip.CheckZip(mv, bytes.NewReader(data), int64(len(data)))
	qt.Assert(t, qt.IsNil(err))
	f, err := zipr.Open("x.cue")
	qt.Assert(t, qt.IsNil(err))
	fdata, err := ioutil.ReadAll(f)
	qt.Assert(t, qt.IsNil(err))
	return string(fdata)
}

// zipAddFS adds all the files from fsys to zw.
// It's copied from zip.Writer.AddFS.
// TODO remove this when we can use go1.22's implementation
// directly.
func zipAddFS(w *zip.Writer, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("zip: cannot add non-regular file")
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = name
		h.Method = zip.Deflate
		fw, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := fsys.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})
}
