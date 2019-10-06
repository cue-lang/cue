package basics

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
	"github.com/rogpeppe/testscript"
)

var update = flag.Bool("update", false, "update the test files")

func TestScript(t *testing.T) {
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return nil
		}
		testscript.Run(t, testscript.Params{
			Dir:           path,
			UpdateScripts: *update,
		})
		return nil
	})
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": cmd.Main,
	}))
}
