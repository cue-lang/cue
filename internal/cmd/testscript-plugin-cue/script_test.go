package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cuecmd "cuelang.org/go/cmd/cue/cmd"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/testscript/plugin"
)

// TestMain registers a "cue" command so that scripts can run the cue tool via
// "exec cue". The plugin itself does not provide cue; a real test harness would
// supply its own cue executable on $PATH in the same way.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"cue": func() { os.Exit(cuecmd.Main()) },
	})
}

// TestScript runs the testdata scripts against the cue plugin. It builds the
// plugin binary and places it on $PATH as testscript-plugin-cue so that the
// plugin command can discover it.
func TestScript(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not available")
	}
	bindir := t.TempDir()
	bin := filepath.Join(bindir, "testscript-plugin-cue")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("cannot build plugin: %v\n%s", err, out)
	}
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := testscript.Params{
		Dir: "testdata",
	}
	cleanup, err := plugin.Setup(&p)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	testscript.Run(t, p)
}
