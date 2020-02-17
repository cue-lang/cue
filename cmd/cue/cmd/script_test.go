package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/testscript"
	"github.com/rogpeppe/testscript/txtar"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
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

// TestScriptDebug takes a single testscript file and then runs it within the
// same process so it can be used for debugging. It runs the first cue command
// it finds.
//
// Usage Comment out t.Skip() and set file to test.
func TestX(t *testing.T) {
	t.Skip()
	const path = "./testdata/script/eval_context.txt"

	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	tmpdir, err := ioutil.TempDir("", "cue-script")
	check(err)
	defer os.Remove(tmpdir)

	a, err := txtar.ParseFile(filepath.FromSlash(path))
	check(err)

	for _, f := range a.Files {
		name := filepath.Join(tmpdir, f.Name)
		check(os.MkdirAll(filepath.Dir(name), 0777))
		check(ioutil.WriteFile(name, f.Data, 0666))
	}

	cwd, err := os.Getwd()
	check(err)
	defer os.Chdir(cwd)
	_ = os.Chdir(tmpdir)

	for s := bufio.NewScanner(bytes.NewReader(a.Comment)); s.Scan(); {
		cmd := s.Text()
		cmd = strings.TrimLeft(cmd, "! ")
		if !strings.HasPrefix(cmd, "cue ") {
			continue
		}

		// TODO: Ugly hack to get args. Do more principled parsing.
		var args []string
		for _, a := range strings.Split(cmd, " ")[1:] {
			args = append(args, strings.Trim(a, "'"))
		}

		c, err := New(args)
		check(err)
		b := &bytes.Buffer{}
		c.SetOutput(b)
		err = c.Run(context.Background())
		// Always create an error to show
		t.Error(err, "\n", b.String())
		return
	}
	t.Fatal("NO COMMAND FOUND")
}

func TestMain(m *testing.M) {
	// Setting inTest causes filenames printed in error messages
	// to be normalized so the output looks the same on Unix
	// as Windows.
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": MainTest,
	}))
}
