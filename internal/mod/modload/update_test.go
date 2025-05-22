package modload

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modfile"
)

func TestUpdateVersions(t *testing.T) {
	files, err := filepath.Glob("testdata/updateversions/*.txtar")
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

			versionsData, _ := fs.ReadFile(tfs, "versions")
			versions := strings.Fields(string(versionsData))

			var out strings.Builder
			mf, err := UpdateVersions(context.Background(), tfs, ".", reg, versions)
			if err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			} else {
				data, err := modfile.Format(mf)
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

func TestResolveAbsolutePackage(t *testing.T) {
	files, err := filepath.Glob("testdata/resolveabsolutepackage/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			ar, err := txtar.ParseFile(f)
			qt.Assert(t, qt.IsNil(err))
			tfs, err := txtar.FS(ar)
			qt.Assert(t, qt.IsNil(err))
			reg, cacheDir := newRegistry(t, tfs)

			testEntries, err := fs.ReadDir(tfs, "tests")
			qt.Assert(t, qt.IsNil(err))
			for _, e := range testEntries {
				t.Run(e.Name(), func(t *testing.T) {
					ctx := context.Background()
					pkgData, err := fs.ReadFile(tfs, path.Join("tests", e.Name(), "package"))
					qt.Assert(t, qt.IsNil(err))
					pkg := strings.TrimSpace(string(pkgData))

					wantData, err := fs.ReadFile(tfs, path.Join("tests", e.Name(), "want"))
					qt.Assert(t, qt.IsNil(err))
					testResolve := func(reg Registry, p string) {
						mv, loc, err := ResolveAbsolutePackage(ctx, reg, pkg)
						var got strings.Builder
						if err != nil {
							fmt.Fprintf(&got, "ERROR: %v\n", err)
						} else {
							modLoc, err := reg.Fetch(ctx, mv)
							qt.Assert(t, qt.IsNil(err))
							fmt.Fprintf(&got, "module: %v\n", mv)
							rel := strings.TrimPrefix(loc.Dir, modLoc.Dir)
							if rel == "" {
								rel = "."
							}
							fmt.Fprintf(&got, "loc: %s\n", rel)
						}
						qt.Assert(t, qt.Equals(got.String(), string(wantData)))
					}
					testResolve(reg, pkg)
					if strings.HasPrefix(string(wantData), "ERROR:") {
						return
					}
					if v := ast.ParseImportPath(pkg).Version; v == "" || semver.Canonical(v) != v {
						return
					}
					t.Logf("trying again with %v", pkg)
					// The version is canonical and the query succeeded, so we should be able to run
					// the same query again without hitting the registry.
					// Check that by creating another cache registry instance that points
					// to the same cache directory but has no backing network registry.
					reg1, err := modcache.New(nil, cacheDir)
					qt.Assert(t, qt.IsNil(err))
					testResolve(reg1, pkg)
				})
			}
		})
	}
}
