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

	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
)

func TestCUEModSummary(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	r := newRegistry(t, `
-- example.com_v0.0.1/cue.mod/module.cue --
module: "example.com@v0"
deps: {
	"foo.com/bar/hello@v0": v: "v0.2.3"
	"bar.com@v0": v: "v0.5.0"
}
`)
	wantSummary := &modrequirements.ModFileSummary{
		Module: module.MustNewVersion("example.com", "v0.0.1"),
		Require: []module.Version{
			module.MustNewVersion("bar.com", "v0.5.0"),
			module.MustNewVersion("foo.com/bar/hello", "v0.2.3"),
		},
	}
	// Test two concurrent fetches both using the same directory.
	var wg sync.WaitGroup
	fetch := func(r ociregistry.Interface) {
		defer wg.Done()
		cr, err := New(r, dir)
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		summary, err := cr.CUEModSummary(ctx, module.MustNewVersion("example.com", "v0.0.1"))
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		if !qt.Check(t, qt.DeepEquals(summary, wantSummary)) {
			return
		}
		summary, err = cr.CUEModSummary(ctx, module.MustNewVersion("example.com", "v0.0.1"))
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		if !qt.Check(t, qt.DeepEquals(summary, wantSummary)) {
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
}

func TestFetch(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		removeAll(dir)
	})
	ctx := context.Background()
	registryContents := `
-- example.com_v0.0.1/cue.mod/module.cue --
module: "example.com@v0"
deps: {
	"foo.com/bar/hello@v0": v: "v0.2.3"
	"bar.com@v0": v: "v0.5.0"
}
-- example.com_v0.0.1/example.cue --
package example
-- example.com_v0.0.1/x/x.cue --
package x
`
	r := newRegistry(t, registryContents)
	wantContents, err := txtarContents(fsSub(txtarfs.FS(txtar.Parse([]byte(registryContents))), "example.com_v0.0.1"))
	qt.Assert(t, qt.IsNil(err))
	checkContents := func(t *testing.T, loc modpkgload.SourceLoc) bool {
		gotContents, err := txtarContents(fsSub(loc.FS, loc.Dir))
		if !qt.Check(t, qt.IsNil(err)) {
			return false
		}
		if !qt.Check(t, qt.Equals(string(gotContents), string(wantContents))) {
			return false
		}
		// Check that the location can be used to retrieve the OS file path.
		osrFS, ok := loc.FS.(OSRootFS)
		if !qt.Check(t, qt.IsTrue(ok)) {
			return false
		}
		root, ok := osrFS.OSRoot()
		if !qt.Check(t, qt.IsTrue(ok)) {
			return false
		}
		// Check that we can access a module file directly.
		data, err := os.ReadFile(filepath.Join(root, loc.Dir, "example.cue"))
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(data), "package example\n"))
		return true
	}
	var wg sync.WaitGroup
	fetch := func(r ociregistry.Interface) {
		defer wg.Done()
		cr, err := New(r, dir)
		if !qt.Check(t, qt.IsNil(err)) {
			return
		}
		loc, err := cr.Fetch(ctx, module.MustNewVersion("example.com", "v0.0.1"))
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
