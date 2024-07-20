package modload

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"
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
			reg := newRegistry(t, tfs)

			want, err := fs.ReadFile(tfs, "want")
			qt.Assert(t, qt.IsNil(err))

			versionsData, _ := fs.ReadFile(tfs, "versions")
			versions := strings.Fields(string(versionsData))

			var out strings.Builder
			mf, err := UpdateVersions(context.Background(), tfs, ".", reg, versions)
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
