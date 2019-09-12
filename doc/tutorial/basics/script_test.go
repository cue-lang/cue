package basics

import (
	"flag"
	"os"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
	"github.com/rogpeppe/testscript"
)

var update = flag.Bool("update", false, "update the test files")

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:           "",
		UpdateScripts: *update,
	})
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": cmd.Main,
	}))
}
