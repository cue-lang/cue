package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"github.com/rogpeppe/testscript"
	"github.com/rogpeppe/testscript/txtar"
)

// TestLatest checks that the examples match the latest language standard,
// even if still valid in backwards compatibility mode.
func TestLatest(t *testing.T) {
	filepath.Walk("testdata/script", func(fullpath string, info os.FileInfo, err error) error {
		if !strings.HasSuffix(fullpath, ".txt") {
			return nil
		}

		a, err := txtar.ParseFile(fullpath)
		if err != nil {
			t.Error(err)
			return nil
		}
		if bytes.HasPrefix(a.Comment, []byte("!")) {
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
					w := &bytes.Buffer{}
					fmt.Fprintf(w, "\n%s:\n", fullpath)
					errors.Print(w, err, nil)
					t.Error(w.String())
				}
			})
		}
		return nil
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:           "testdata/script",
		UpdateScripts: *update,
	})
}

func TestMain(m *testing.M) {
	// Setting inTest causes filenames printed in error messages
	// to be normalized so the output looks the same on Unix
	// as Windows.
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": MainTest,
	}))
}
