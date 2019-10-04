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
	// Setting inTest causes filenames printed in error messages
	// to be normalized so the output looks the same on Unix
	// as Windows.
	inTest = true
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": Main,
	}))
}
