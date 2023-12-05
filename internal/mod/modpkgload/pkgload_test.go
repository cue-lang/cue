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

	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/txtarfs"
)

func TestLoadPackages(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		ar, err := txtar.ParseFile(f)
		qt.Assert(t, qt.IsNil(err))
		tfs := txtarfs.FS(ar)
		reg := testRegistry{tfs}
		testDirs, _ := fs.Glob(tfs, "test[0-9]*")
		for _, testDir := range testDirs {
			testName := strings.TrimSuffix(filepath.Base(f), ".txtar")
			t.Run(testName, func(t *testing.T) {
				t.Logf("test file: %v", f)
				readTestFile := func(name string) string {
					data, err := fs.ReadFile(tfs, path.Join(testDir, name))
					qt.Assert(t, qt.IsNil(err))
					return string(data)
				}

				initialRequirementsStr := strings.Fields(readTestFile("initial-requirements"))
				mainModulePath, moduleVersions := initialRequirementsStr[0], mapSlice(initialRequirementsStr[1:], module.MustParseVersion)
				initialRequirements := modrequirements.NewRequirements(mainModulePath, reg, moduleVersions)

				rootPackages := strings.Fields(readTestFile("root-packages"))
				want := readTestFile("want")

				var out strings.Builder
				printf := func(f string, a ...any) {
					fmt.Fprintf(&out, f, a...)
				}
				pkgs := LoadPackages(context.Background(), mainModulePath, SourceLoc{tfs, "."}, initialRequirements, reg, rootPackages)
				for _, pkg := range pkgs.All() {
					printf("%s\n", pkg.ImportPath())
					printf("\tflags: %v\n", pkg.Flags())
					if pkg.Error() != nil {
						printf("\terror: %v\n", pkg.Error())
						printf("\tmissing: %v\n", errors.As(pkg.Error(), new(*ImportMissingError)))
					} else {
						printf("\tmod: %v\n", pkg.Mod())
						printf("\tlocation: %v\n", pkg.Location().Dir)
						if imps := pkg.Imports(); len(imps) > 0 {
							printf("\timports:\n")
							for _, imp := range imps {
								printf("\t\t%v\n", imp.ImportPath())
							}
						}
					}
				}
				if diff := cmp.Diff(string(want), out.String()); diff != "" {
					t.Logf("actual result:\n%s", out.String())
					t.Fatalf("unexpected results (-want +got):\n%s", diff)
				}
			})
		}
	}
}

type testRegistry struct {
	fs fs.FS
}

func (r testRegistry) Fetch(ctx context.Context, m module.Version) (SourceLoc, error) {
	mpath := r.modpath(m)
	info, err := fs.Stat(r.fs, mpath)
	if err != nil || !info.IsDir() {
		return SourceLoc{}, fmt.Errorf("module %v not found at %v", m, mpath)
	}
	return SourceLoc{r.fs, mpath}, nil
}

func (r testRegistry) CUEModSummary(ctx context.Context, m module.Version) (*modrequirements.ModFileSummary, error) {
	mpath := path.Join(r.modpath(m), "cue.mod/module.cue")
	data, err := fs.ReadFile(r.fs, mpath)
	if err != nil {
		return nil, err
	}
	mf, err := modfile.Parse(data, mpath)
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", m, err)
	}
	return &modrequirements.ModFileSummary{
		Require: mf.DepVersions(),
		Module:  m,
	}, nil
}

func (r testRegistry) modpath(m module.Version) string {
	mpath, _, _ := module.SplitPathVersion(m.Path())
	return path.Join("_registry", strings.ReplaceAll(mpath, "/", "_")+"_"+m.Version())
}

func mapSlice[From, To any](ss []From, f func(From) To) []To {
	ts := make([]To, len(ss))
	for i := range ss {
		ts[i] = f(ss[i])
	}
	return ts
}
