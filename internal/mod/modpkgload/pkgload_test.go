package modpkgload

import (
	"context"
	"errors"
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
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

func TestLoadPackages(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		ar, err := txtar.ParseFile(f)
		qt.Assert(t, qt.IsNil(err))
		tfs, err := txtar.FS(ar)
		qt.Assert(t, qt.IsNil(err))
		reg := testRegistry{tfs}
		testDirs, _ := fs.Glob(tfs, "test[0-9]*")
		for _, testDir := range testDirs {
			testName := strings.TrimSuffix(filepath.Base(f), ".txtar") + "/" + testDir
			t.Run(testName, func(t *testing.T) {
				t.Logf("test file: %v", f)
				readTestFile := func(name string) string {
					data, err := fs.ReadFile(tfs, path.Join(testDir, name))
					qt.Assert(t, qt.IsNil(err))
					return string(data)
				}

				initialRequirementsStr := strings.Fields(readTestFile("initial-requirements"))
				mainModulePath, moduleVersions := initialRequirementsStr[0], mapSlice(initialRequirementsStr[1:], module.MustParseVersion)
				defaultMajorVersions := make(map[string]string)
				for f := range strings.FieldsSeq(readTestFile("default-major-versions")) {
					p, v, ok := strings.Cut(f, "@")
					qt.Assert(t, qt.IsTrue(ok))
					defaultMajorVersions[p] = v
				}
				initialRequirements := modrequirements.NewRequirements(mainModulePath, reg, moduleVersions, defaultMajorVersions)

				rootPackages := strings.Fields(readTestFile("root-packages"))
				want := readTestFile("want")

				var out strings.Builder
				printf := func(f string, a ...any) {
					fmt.Fprintf(&out, f, a...)
				}
				pkgs := LoadPackages(
					context.Background(),
					mainModulePath,
					module.SourceLoc{FS: tfs, Dir: "."},
					initialRequirements,
					reg,
					rootPackages,
					func(pkgPath string, mod module.Version, fsys fs.FS, mf modimports.ModuleFile) bool {
						return true
					},
				)
				for _, pkg := range pkgs.All() {
					printf("%s\n", pkg.ImportPath())
					printf("\tflags: %v\n", pkg.Flags())
					if pkg.Error() != nil {
						printf("\terror: %v\n", pkg.Error())
						printf("\tmissing: %v\n", errors.As(pkg.Error(), new(*ImportMissingError)))
					} else {
						printf("\tmod: %v\n", pkg.Mod())
						printf("\texternal: %v\n", pkg.FromExternalModule())
						// Sanity check that the module file is available at pkg.ModRoot.
						_, err := fs.Stat(pkg.ModRoot().FS, path.Join(pkg.ModRoot().Dir, "cue.mod/module.cue"))
						qt.Assert(t, qt.IsNil(err), qt.Commentf("pkg %q; mod root: %#v", pkg.ImportPath(), pkg.ModRoot()))
						for _, loc := range pkg.Locations() {
							printf("\tlocation: %v\n", loc.Dir)
						}
						for _, file := range pkg.Files() {
							printf("\tfile: %v: %v\n", file.FilePath, file.Syntax.PackageName())
						}
						if imps := pkg.Imports(); len(imps) > 0 {
							printf("\timports:\n")
							for _, imp := range imps {
								printf("\t\t%v\n", imp.ImportPath())
							}
						}
					}
				}
				if diff := cmp.Diff(want, out.String()); diff != "" {
					t.Logf("actual result:\n%s", out.String())
					t.Fatalf("unexpected results (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestFindPackageLocations(t *testing.T) {
	versionForModule := func(ctx context.Context, prefixPath string) (module.Version, error) {
		t.Logf("versionForModule %q", prefixPath)
		switch prefixPath {
		case "foo.bar":
			return module.Version{}, nil
		case "foo.bar/a":
			return module.MustNewVersion("foo.bar/a@v1", "v1.2.3"), nil
		case "foo.bar/a/b":
			return module.MustNewVersion("foo.bar/a/b@v0", "v0.2.4"), nil
		case "foo.bar/a/b/c":
			return module.MustNewVersion("foo.bar/a/b/c@v0", "v0.3.6"), nil
		case "foo.bar/a/b/c/d":
			return module.MustNewVersion("foo.bar/a/b/c/d@v5", "v5.10.20"), nil
		default:
			t.Errorf("unexpected call to versionForModule with prefix %q", prefixPath)
			return module.Version{}, fmt.Errorf("no version")
		}
	}
	tfs, err := txtar.FS(txtar.Parse([]byte(`
-- foo.bar_a/b/c/cue.mod/module.cue --
// This should cause foo.bar/a to be excluded from the list
// of possible candidates because c is a nested module.
module: "something"
-- foo.bar_a/b/c/d/x.cue --
package d
-- foo.bar_a_b/c/d/x.cue --
package C
-- foo.bar_a_b_c/d/x.cue --
package C
-- foo.bar_a_b_c_d/x.cue --
package C
`)))
	qt.Assert(t, qt.IsNil(err))
	fetch := func(ctx context.Context, m module.Version) (loc module.SourceLoc, isLocal bool, err error) {
		t.Logf("fetch %v", m)
		switch m.String() {
		case "foo.bar/a@v1.2.3":
			// Note: return true for isLocal to trigger the nested module
			// checking logic.
			return module.SourceLoc{
				FS:  tfs,
				Dir: "foo.bar_a",
			}, true, nil
		case "foo.bar/a/b@v0.2.4":
			return module.SourceLoc{
				FS:  tfs,
				Dir: "foo.bar_a_b",
			}, false, nil
		case "foo.bar/a/b/c@v0.3.6":
			return module.SourceLoc{
				FS:  tfs,
				Dir: "foo.bar_a_b_c",
			}, false, nil
		case "foo.bar/a/b/c/d@v5.10.20":
			return module.SourceLoc{
				FS:  tfs,
				Dir: "foo.bar_a_b_c_d",
			}, false, nil
		default:
			t.Errorf("unexpected call to versionForModule with module %q", m)
			return module.SourceLoc{}, false, fmt.Errorf("no module")
		}
	}
	locs, err := FindPackageLocations(context.Background(), "foo.bar/a/b/c/d", versionForModule, fetch)
	qt.Assert(t, qt.IsNil(err))
	var dirs []string
	for _, loc := range locs {
		dirs = append(dirs, loc.Locs[0].Dir)
	}
	qt.Assert(t, qt.DeepEquals(dirs, []string{
		"foo.bar_a_b_c_d",
		"foo.bar_a_b_c/d",
		"foo.bar_a_b/c/d",
	}))
}

type testRegistry struct {
	fs fs.FS
}

func (r testRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	mpath := r.modpath(m)
	info, err := fs.Stat(r.fs, mpath)
	if err != nil || !info.IsDir() {
		return module.SourceLoc{}, fmt.Errorf("module %v not found at %v", m, mpath)
	}
	return module.SourceLoc{
		FS:  r.fs,
		Dir: mpath,
	}, nil
}

func (r testRegistry) Requirements(ctx context.Context, m module.Version) ([]module.Version, error) {
	mpath := path.Join(r.modpath(m), "cue.mod/module.cue")
	data, err := fs.ReadFile(r.fs, mpath)
	if err != nil {
		return nil, err
	}
	mf, err := modfile.Parse(data, mpath)
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", m, err)
	}
	return mf.DepVersions(), nil
}

func (r testRegistry) modpath(m module.Version) string {
	mpath, _, _ := ast.SplitPackageVersion(m.Path())
	return path.Join("_registry", strings.ReplaceAll(mpath, "/", "_")+"_"+m.Version())
}

func mapSlice[From, To any](ss []From, f func(From) To) []To {
	ts := make([]To, len(ss))
	for i := range ss {
		ts[i] = f(ss[i])
	}
	return ts
}
