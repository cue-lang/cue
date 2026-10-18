package modimports

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
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
			iter := AllModuleFiles(tfs, ".", WalkOptions{})
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
			qt.Assert(t, qt.Equals(out.String(), string(want)))
			wantImports, err := fs.ReadFile(tfs, "want-imports")
			qt.Assert(t, qt.IsNil(err))
			out.Reset()
			imports, err := AllImports(AllModuleFiles(tfs, ".", WalkOptions{}))
			if err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			} else {
				for _, imp := range imports {
					fmt.Fprintln(&out, imp)
				}
			}
			qt.Assert(t, qt.Equals(out.String(), string(wantImports)),
				qt.Commentf("unexpected results for ImportsForModuleFiles"))
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
-- s/x.cue --
package x
import (
	"imported-from-s.com/foo"
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
	// A one-letter directory also includes package files from its parent.
	imps, err = AllImports(PackageFiles(tfs, "s", "x"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{"bar.com/baz", "foo", "imported-from-s.com/foo", "something.else:other"}))
}

// TestAllModuleFilesOptions checks the rules WalkOptions adds against the
// defaults, on a real directory so that symbolic links take part.
func TestAllModuleFilesOptions(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"a.cue",
		"_h.cue",
		".d/x.cue",
		"_u/y.cue",
		"testdata/t.cue",
		"nested/cue.mod/module.cue",
		"nested/n.cue",
		"sub/s.cue",
		"cue.mod/module.cue",
		"cue.mod/gen/g.cue",
		"cue.mod/pkg/_x/y.cue",
		"cue.mod/pkg/.h/z.cue",
		"cue.mod/pkg/v/cue.mod/module.cue",
		"cue.mod/pkg/v/v.cue",
		"cue.mod/usr/u/u.cue",
	} {
		p := filepath.Join(root, filepath.FromSlash(f))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(p), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(p, []byte("package p\n"), 0o666)))
	}
	links := runtime.GOOS != "windows"
	if links {
		qt.Assert(t, qt.IsNil(os.Symlink("a.cue", filepath.Join(root, "link.cue"))))
		qt.Assert(t, qt.IsNil(os.Symlink("sub", filepath.Join(root, "linkdir"))))
	}
	list := func(opts WalkOptions) []string {
		var got []string
		for f, err := range AllModuleFiles(os.DirFS(root), ".", opts) {
			qt.Assert(t, qt.IsNil(err))
			got = append(got, f.FilePath)
		}
		return got
	}

	// The defaults: hidden and underscore names out, cue.mod and nested
	// modules out, testdata in, a link to a file read through, a link to a
	// directory not followed.
	want := []string{"a.cue"}
	if links {
		want = append(want, "link.cue")
	}
	want = append(want, "sub/s.cue", "testdata/t.cue")
	qt.Assert(t, qt.DeepEquals(list(WalkOptions{}), want))

	// SkipDir sees the path relative to the root, so a rule can tell the
	// cue.mod trees from the main tree: underscore directories stay in
	// there, and a cue.mod below them is no boundary.
	want = []string{"a.cue"}
	if links {
		want = append(want, "link.cue")
	}
	want = append(want, "sub/s.cue",
		"cue.mod/gen/g.cue",
		"cue.mod/pkg/_x/y.cue",
		"cue.mod/pkg/v/v.cue",
		"cue.mod/usr/u/u.cue",
	)
	qt.Assert(t, qt.DeepEquals(list(WalkOptions{
		SkipDir: func(p string) bool {
			b := path.Base(p)
			if strings.HasPrefix(p, "cue.mod/") {
				return strings.HasPrefix(b, ".")
			}
			return isHidden(b) || b == "testdata"
		},
		IncludeCueModTrees: true,
	}), want))
}

// TestAllModuleFilesCueModRoots checks that SkipDir has its say on the
// cue.mod trees themselves and that a link in place of one is not
// followed.
func TestAllModuleFilesCueModRoots(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"a.cue",
		"cue.mod/module.cue",
		"cue.mod/gen/g.cue",
		"cue.mod/usr/u.cue",
		"elsewhere/p.cue",
	} {
		p := filepath.Join(root, filepath.FromSlash(f))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(p), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(p, []byte("package p\n"), 0o666)))
	}
	if runtime.GOOS != "windows" {
		qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "elsewhere"), filepath.Join(root, "cue.mod", "pkg"))))
	}
	var got []string
	for f, err := range AllModuleFiles(os.DirFS(root), ".", WalkOptions{
		SkipDir:            func(p string) bool { return p == "cue.mod/usr" || p == "elsewhere" },
		IncludeCueModTrees: true,
	}) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, f.FilePath)
	}
	qt.Assert(t, qt.DeepEquals(got, []string{"a.cue", "cue.mod/gen/g.cue"}))
}

// TestAllModuleFilesReportLinkedDirs checks that a symbolic link to a
// directory, in the main tree or in place of a cue.mod tree, is yielded as
// an error when asked for, and passed over otherwise.
func TestAllModuleFilesReportLinkedDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links")
	}
	base := t.TempDir()
	root := filepath.Join(base, "mod")
	for _, f := range []string{"mod/a.cue", "mod/cue.mod/module.cue", "mod/cue.mod/gen/g.cue", "ext/e.cue", "ext2/e2.cue"} {
		p := filepath.Join(base, filepath.FromSlash(f))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(p), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(p, []byte("package p\n"), 0o666)))
	}
	qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "ext"), filepath.Join(root, "sub"))))
	qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "ext"), filepath.Join(root, "_dep"))))
	qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "ext"), filepath.Join(root, ".hidden"))))
	qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "..", "ext2"), filepath.Join(root, "cue.mod", "pkg"))))
	list := func(opts WalkOptions) []string {
		var got []string
		for f, err := range AllModuleFiles(os.DirFS(root), ".", opts) {
			if err != nil {
				qt.Assert(t, qt.ErrorIs(err, ErrLinkedDir))
				got = append(got, f.FilePath+" (linked dir)")
				continue
			}
			got = append(got, f.FilePath)
		}
		return got
	}
	qt.Assert(t, qt.DeepEquals(list(WalkOptions{IncludeCueModTrees: true}), []string{"a.cue", "cue.mod/gen/g.cue"}))
	qt.Assert(t, qt.DeepEquals(list(WalkOptions{IncludeCueModTrees: true, ReportLinkedDirs: true}), []string{
		"a.cue", "sub (linked dir)", "cue.mod/gen/g.cue", "cue.mod/pkg (linked dir)",
	}))
	// A linked directory is judged by SkipDir as a directory is: with
	// underscore directories walked, a linked _dep is reported, while a
	// linked hidden directory is skipped either way.
	walkUnderscore := func(p string) bool { return path.Base(p)[0] == '.' }
	qt.Assert(t, qt.DeepEquals(list(WalkOptions{SkipDir: walkUnderscore, IncludeCueModTrees: true, ReportLinkedDirs: true}), []string{
		"_dep (linked dir)", "a.cue", "sub (linked dir)", "cue.mod/gen/g.cue", "cue.mod/pkg (linked dir)",
	}))

	// A link in place of cue.mod itself is the module's cue.mod, read
	// through as the loader reads it, not a linked directory to report.
	root2 := filepath.Join(base, "mod2")
	for _, f := range []string{"mod2/b.cue", "shared/module.cue", "shared/pkg/example.com/p/p.cue"} {
		p := filepath.Join(base, filepath.FromSlash(f))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(p), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(p, []byte("package p\n"), 0o666)))
	}
	qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "shared"), filepath.Join(root2, "cue.mod"))))
	var got []string
	for f, err := range AllModuleFiles(os.DirFS(root2), ".", WalkOptions{IncludeCueModTrees: true, ReportLinkedDirs: true}) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, f.FilePath)
	}
	qt.Assert(t, qt.DeepEquals(got, []string{"b.cue", "cue.mod/pkg/example.com/p/p.cue"}))
}
