package modcache

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

func TestRequirements(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	r := newRegistry(t, `
-- example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.8.0"
deps: {
	"foo.com/bar/hello@v0": v: "v0.2.3"
	"bar.com@v0": v: "v0.5.0"
}
`)
	wantRequirements := []module.Version{
		module.MustNewVersion("bar.com", "v0.5.0"),
		module.MustNewVersion("foo.com/bar/hello", "v0.2.3"),
	}
	// Test two concurrent fetches both using the same directory.
	var wg sync.WaitGroup
	fetch := func(r ociregistry.Interface) {
		defer wg.Done()
		cr, err := New(modregistry.NewClient(r), dir)
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		summary, err := cr.Requirements(ctx, module.MustNewVersion("example.com/foo", "v0.0.1"))
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		if !qt.Check(t, qt.DeepEquals(summary, wantRequirements)) {
			return
		}
		// Fetch again so that we test the in-memory cache-hit path.
		summary, err = cr.Requirements(ctx, module.MustNewVersion("example.com/foo", "v0.0.1"))
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		if !qt.Check(t, qt.DeepEquals(summary, wantRequirements)) {
			return
		}
	}
	wg.Add(2)
	go fetch(r)
	go fetch(r)
	wg.Wait()

	// Check that it still functions without a functional registry.
	wg.Add(1)
	fetch(nil)

	// Check that the file is stored in the expected place.
	data, err := os.ReadFile(filepath.Join(dir, "cache/download/example.com/foo/@v/v0.0.1.mod"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Matches(string(data), `(?s).*module: "example.com/foo@v0".*`))
}

func TestFetch(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		RemoveAll(dir)
	})
	ctx := context.Background()
	registryContents := `
-- example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.8.0"
deps: {
	"foo.com/bar/hello@v0": v: "v0.2.3"
	"bar.com@v0": v: "v0.5.0"
}
-- example.com_foo_v0.0.1/example.cue --
package example
-- example.com_foo_v0.0.1/x/x.cue --
package x
`
	r := newRegistry(t, registryContents)
	wantContents, err := txtarContents(fsSub(txtarfs.FS(txtar.Parse([]byte(registryContents))), "example.com_foo_v0.0.1"))
	qt.Assert(t, qt.IsNil(err))
	checkContents := func(t *testing.T, loc module.SourceLoc) bool {
		gotContents, err := txtarContents(fsSub(loc.FS, loc.Dir))
		if !qt.Check(t, qt.IsNil(err)) {
			return false
		}
		if !qt.Check(t, qt.Equals(string(gotContents), string(wantContents))) {
			return false
		}
		// Check that the location can be used to retrieve the OS file path.
		osrFS, ok := loc.FS.(module.OSRootFS)
		if !qt.Check(t, qt.IsTrue(ok)) {
			return false
		}
		root := osrFS.OSRoot()
		if !qt.Check(t, qt.Not(qt.Equals(root, ""))) {
			return false
		}
		// Check that we can access a module file directly.
		srcPath := filepath.Join(root, loc.Dir, "example.cue")
		data, err := os.ReadFile(srcPath)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(data), "package example\n"))
		// Check that the actual paths are as expected.
		qt.Check(t, qt.Equals(srcPath, filepath.Join(dir, "example.com", "foo@v0.0.1", "example.cue")))
		return true
	}
	var wg sync.WaitGroup
	fetch := func(r ociregistry.Interface) {
		defer wg.Done()
		cr, err := New(modregistry.NewClient(r), dir)
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		loc, err := cr.Fetch(ctx, module.MustNewVersion("example.com/foo", "v0.0.1"))
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		checkContents(t, loc)
	}
	wg.Add(2)
	go fetch(r)
	go fetch(r)
	wg.Wait()
	// Check that it still functions without a functional registry.
	wg.Add(1)
	fetch(nil)
}

func fsSub(fsys fs.FS, sub string) fs.FS {
	fsys, err := fs.Sub(fsys, sub)
	if err != nil {
		panic(err)
	}
	return fsys
}

// txtarContents returns the contents of fsys in txtar format.
// It assumes that all files end in a newline and do not contain
// a txtar separator.
func txtarContents(fsys fs.FS) ([]byte, error) {
	var buf bytes.Buffer
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&buf, "-- %s --\n", path)
		buf.Write(data)
		return nil
	})
	return buf.Bytes(), err
}

func newRegistry(t *testing.T, registryContents string) ociregistry.Interface {
	regSrv, err := registrytest.New(txtarfs.FS(txtar.Parse([]byte(registryContents))), "")
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(regSrv.Close)
	regOCI, err := ociclient.New(regSrv.Host(), &ociclient.Options{
		Insecure: true,
	})
	qt.Assert(t, qt.IsNil(err))
	return regOCI
}
