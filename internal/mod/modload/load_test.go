package modload

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
)

func TestLoad(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			ar, err := txtar.ParseFile(f)
			qt.Assert(t, qt.IsNil(err))
			tfs := txtarfs.FS(ar)
			reg := newRegistry(t, tfs, "_registry")

			want, err := fs.ReadFile(tfs, "want")
			qt.Assert(t, qt.IsNil(err))

			cueVers, _ := fs.ReadFile(tfs, "cue-version")

			var out strings.Builder
			mf, err := Tidy(context.Background(), tfs, ".", reg, strings.TrimSpace(string(cueVers)))
			if err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			} else {
				data, err := mf.Format()
				qt.Assert(t, qt.IsNil(err))
				out.Write(data)
			}
			if diff := cmp.Diff(string(want), out.String()); diff != "" {
				t.Log("actual result:\n", out.String())
				t.Fatalf("unexpected results (-want +got):\n%s", diff)
			}
		})
	}
}

func newRegistry(t *testing.T, fsys fs.FS, root string) Registry {
	fsys, err := fs.Sub(fsys, "_registry")
	qt.Assert(t, qt.IsNil(err))
	regSrv, err := registrytest.New(fsys, "")
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(regSrv.Close)
	regOCI, err := ociclient.New(regSrv.Host(), &ociclient.Options{
		Insecure: true,
	})
	qt.Assert(t, qt.IsNil(err))
	return &registryImpl{modregistry.NewClient(regOCI)}
}

type registryImpl struct {
	reg *modregistry.Client
}

func (r *registryImpl) CUEModSummary(ctx context.Context, mv module.Version) (*modrequirements.ModFileSummary, error) {
	m, err := r.reg.GetModule(ctx, mv)
	if err != nil {
		return nil, err
	}
	data, err := m.ModuleFile(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot get module file from %v: %v", m, err)
	}
	mf, err := modfile.Parse(data, mv.String())
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", m, err)
	}
	return &modrequirements.ModFileSummary{
		Require: mf.DepVersions(),
		Module:  mv,
	}, nil
}

// getModContents downloads the module with the given version
// and returns the directory where it's stored.
func (c *registryImpl) Fetch(ctx context.Context, mv module.Version) (modpkgload.SourceLoc, error) {
	m, err := c.reg.GetModule(ctx, mv)
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	r, err := m.GetZip(ctx)
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	defer r.Close()
	zipData, err := io.ReadAll(r)
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	zipr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	return modpkgload.SourceLoc{
		FS:  zipr,
		Dir: ".",
	}, nil
}

func (r *registryImpl) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	versions, err := r.reg.ModuleVersions(ctx, mpath)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain versions for module %q: %v", mpath, err)
	}
	return versions, nil
}
