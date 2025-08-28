package modimports

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

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
			tfs, err := txtar.FS(ar)
			qt.Assert(t, qt.IsNil(err))
			want, err := fs.ReadFile(tfs, "want")
			qt.Assert(t, qt.IsNil(err))
			iter := AllModuleFiles(tfs, ".")
			var out strings.Builder
			iter(func(pf ModuleFile, err error) bool {
				out.WriteString(pf.FilePath)
				if err != nil {
					fmt.Fprintf(&out, ": error: %v\n", err)
					return true
				}
				if pf.SyntaxError != nil {
					fmt.Fprintf(&out, ": error: %v\n", pf.SyntaxError)
					return true
				}
				for imp := range pf.Syntax.ImportSpecs() {
					fmt.Fprintf(&out, " %s", imp.Path.Value)
				}
				out.WriteString("\n")
				return true
			})
			if diff := cmp.Diff(string(want), out.String()); diff != "" {
				t.Fatalf("unexpected results (-want +got):\n%s", diff)
			}
			wantImports, err := fs.ReadFile(tfs, "want-imports")
			qt.Assert(t, qt.IsNil(err))
			out.Reset()
			imports, err := AllImports(AllModuleFiles(tfs, "."))
			if err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			} else {
				for _, imp := range imports {
					fmt.Fprintln(&out, imp)
				}
			}
			if diff := cmp.Diff(string(wantImports), out.String()); diff != "" {
				t.Fatalf("unexpected results for ImportsForModuleFiles (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPackageFiles(t *testing.T) {
	dirContents := txtar.Parse([]byte(`
-- x1.cue --
package x

import (
	"foo"
	"bar.com/baz"
)
-- x2.cue --
package x

import (
	"something.else:other"
	"bar.com/baz:baz"
)
-- y.cue --
package y

import (
	"something.that.is/imported/by/y"
)
-- nopkg.cue --
import (
	"something.that.is/imported/by/nopkg"
)
-- foo/y.cue --
import "other"
-- omitted.go --
-- omitted --
-- sub/x.cue --
package x
import (
	"imported-from-sub.com/foo"
)
`))
	tfs, err := txtar.FS(dirContents)
	qt.Assert(t, qt.IsNil(err))
	imps, err := AllImports(PackageFiles(tfs, ".", "*"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{
		"bar.com/baz",
		"foo",
		"something.else:other",
		"something.that.is/imported/by/nopkg",
		"something.that.is/imported/by/y",
	}))
	imps, err = AllImports(PackageFiles(tfs, ".", "x"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{
		"bar.com/baz",
		"foo",
		"something.else:other",
	}))
	imps, err = AllImports(PackageFiles(tfs, "foo", "*"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{"other"}))
	imps, err = AllImports(PackageFiles(tfs, "sub", "x"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{"bar.com/baz", "foo", "imported-from-sub.com/foo", "something.else:other"}))
}
