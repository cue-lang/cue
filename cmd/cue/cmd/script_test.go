package cmd

import (
	"os"
	"testing"

	"github.com/rogpeppe/testscript"
)

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:           "testdata/script",
		UpdateScripts: *update,
	})
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": Main,
	}))
}
