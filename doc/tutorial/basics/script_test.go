package basics

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
)

// TestLatest checks that the examples match the latest language standard,
// even if still valid in backwards compatibility mode.
func TestLatest(t *testing.T) {
	filepath.Walk(".", func(fullpath string, info os.FileInfo, err error) error {
		if !strings.HasSuffix(fullpath, ".txt") {
			return nil
		}

		a, err := txtar.ParseFile(fullpath)
		if err != nil {
			t.Error(err)
			return nil
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
	})
}

func TestScript(t *testing.T) {
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return filepath.SkipDir
		}
		testscript.Run(t, testscript.Params{
			Dir:           path,
			UpdateScripts: cuetest.UpdateGoldenFiles,
		})
		return nil
	})
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": cmd.MainTest,
	}))
}
