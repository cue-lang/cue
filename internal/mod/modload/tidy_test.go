package modload

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/modregistrytest"
)

func TestTidy(t *testing.T) {
	files, err := filepath.Glob("testdata/tidy/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			ar, err := txtar.ParseFile(f)
			qt.Assert(t, qt.IsNil(err))
			tfs, err := txtar.FS(ar)
			qt.Assert(t, qt.IsNil(err))
			reg, _ := newRegistry(t, tfs)

			want, err := fs.ReadFile(tfs, "want")
			qt.Assert(t, qt.IsNil(err))

			err = CheckTidy(context.Background(), tfs, ".", reg)
			wantCheckTidyError := stringFromFile(tfs, "tidy-check-error")
			if wantCheckTidyError == "" {
				qt.Check(t, qt.IsNil(err))
			} else {
				qt.Check(t, qt.ErrorMatches(err, wantCheckTidyError))
			}

			var out strings.Builder
			var tidyFile []byte
			mf, err := Tidy(context.Background(), tfs, ".", reg)
			if err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			} else {
				tidyFile, err = modfile.Format(mf)
				qt.Assert(t, qt.IsNil(err))
				out.Write(tidyFile)
			}
			if diff := cmp.Diff(string(want), out.String()); diff != "" {
				t.Log("actual result:\n", out.String())
				t.Fatalf("unexpected results (-want +got):\n%s", diff)
			}

			// Ensure that CheckTidy does not error after a successful Tidy.
			// We make a new txtar FS given that an FS is read-only.
			if len(tidyFile) > 0 {
				for i := range ar.Files {
					file := &ar.Files[i]
					if file.Name == "cue.mod/module.cue" {
						file.Data = []byte(out.String())
					}
				}
				tfs, err := txtar.FS(ar)
				qt.Assert(t, qt.IsNil(err))
				err = CheckTidy(context.Background(), tfs, ".", reg)
				qt.Check(t, qt.IsNil(err), qt.Commentf("CheckTidy after a successful Tidy should not fail"))
			}
		})
	}
}

func stringFromFile(fsys fs.FS, file string) string {
	data, _ := fs.ReadFile(fsys, file)
	return strings.TrimSpace(string(data))
}

func newRegistry(t *testing.T, fsys fs.FS) (Registry, string) {
	fsys, err := fs.Sub(fsys, "_registry")
	qt.Assert(t, qt.IsNil(err))
	regSrv, err := modregistrytest.New(fsys, "")
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(regSrv.Close)
	regOCI, err := ociclient.New(regSrv.Host(), &ociclient.Options{
		Insecure: true,
	})
	qt.Assert(t, qt.IsNil(err))
	cacheDir := t.TempDir()
	reg, err := modcache.New(modregistry.NewClient(regOCI), cacheDir)
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(func() {
		modcache.RemoveAll(cacheDir)
	})
	return reg, cacheDir
}
