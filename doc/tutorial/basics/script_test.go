package basics

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
)

// TestLatest checks that the examples match the latest language standard,
// even if still valid in backwards compatibility mode.
func TestLatest(t *testing.T) {
	if err := filepath.WalkDir(".", func(fullpath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(fullpath, ".txtar") {
			return nil
		}

		a, err := txtar.ParseFile(fullpath)
		if err != nil {
			return err
		}

		for _, f := range a.Files {
			t.Run(path.Join(fullpath, f.Name), func(t *testing.T) {
				if !strings.HasSuffix(f.Name, ".cue") {
					return
				}
				v := parser.FromVersion(parser.Latest)
				_, err := parser.ParseFile(f.Name, f.Data, v)
				if err != nil {
					t.Errorf("%v: %v", fullpath, err)
				}
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScript(t *testing.T) {
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if entry.IsDir() {
			return filepath.SkipDir
		}
		testscript.Run(t, testscript.Params{
			Dir:                 path,
			UpdateScripts:       cuetest.UpdateGoldenFiles,
			RequireExplicitExec: true,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": cmd.MainTest,
	}))
}
