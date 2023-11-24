package modimports

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/internal/txtarfs"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"
)

func TestAllPackageFiles(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			ar, err := txtar.ParseFile(f)
			qt.Assert(t, qt.IsNil(err))
			tfs := txtarfs.FS(ar)
			want, err := fs.ReadFile(tfs, "want")
			qt.Assert(t, qt.IsNil(err))
			iter := AllPackageFiles(tfs, ".")
			var out strings.Builder
			iter(func(pf PkgFile, err error) bool {
				out.WriteString(pf.FilePath)
				if err != nil {
					fmt.Fprintf(&out, ": error: %v\n", err)
					return true
				}
				for _, imp := range pf.Syntax.Imports {
					fmt.Fprintf(&out, " %s", imp.Path.Value)
				}
				out.WriteString("\n")
				return true
			})
			if diff := cmp.Diff(string(want), out.String()); diff != "" {
				t.Fatalf("unexpected results (-want +got):\n%s", diff)
			}
		})
	}
}
