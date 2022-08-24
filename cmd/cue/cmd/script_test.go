// Copyright 2020 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/shlex"
	"github.com/rogpeppe/go-internal/goproxytest"
	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
)

const (
	homeDirName = ".user-home"
)

// TestLatest checks that the examples match the latest language standard,
// even if still valid in backwards compatibility mode.
func TestLatest(t *testing.T) {
	root := filepath.Join("testdata", "script")
	if err := filepath.WalkDir(root, func(fullpath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(fullpath, ".txtar") ||
			strings.HasPrefix(filepath.Base(fullpath), "fix") {
			return nil
		}

		a, err := txtar.ParseFile(fullpath)
		if err != nil {
			return err
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
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScript(t *testing.T) {
	srv, err := goproxytest.NewServer(filepath.Join("testdata", "mod"), "")
	if err != nil {
		t.Fatalf("cannot start proxy: %v", err)
	}
	p := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		UpdateScripts:       cuetest.UpdateGoldenFiles,
		RequireExplicitExec: true,
		Setup: func(e *testscript.Env) error {
			// Set up a home dir within work dir with a . prefix so that the
			// Go/CUE pattern ./... does not descend into it.
			home := filepath.Join(e.WorkDir, homeDirName)
			if err := os.Mkdir(home, 0777); err != nil {
				return err
			}

			e.Vars = append(e.Vars,
				"GOPROXY="+srv.URL,
				"GONOSUMDB=*", // GOPROXY is a private proxy
				homeEnvName()+"="+home,
			)
			return nil
		},
		Condition: cuetest.Condition,
	}
	if err := gotooltest.Setup(&p); err != nil {
		t.Fatal(err)
	}
	testscript.Run(t, p)
}

// TestScriptDebug takes a single testscript file and then runs it within the
// same process so it can be used for debugging. It runs the first cue command
// it finds.
//
// Usage Comment out t.Skip() and set file to test.
func TestX(t *testing.T) {
	t.Skip()
	const path = "./testdata/script/eval_e.txtar"

	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	tmpdir := t.TempDir()

	a, err := txtar.ParseFile(filepath.FromSlash(path))
	check(err)

	for _, f := range a.Files {
		name := filepath.Join(tmpdir, f.Name)
		check(os.MkdirAll(filepath.Dir(name), 0777))
		check(ioutil.WriteFile(name, f.Data, 0666))
	}

	cwd, err := os.Getwd()
	check(err)
	defer func() { _ = os.Chdir(cwd) }()
	_ = os.Chdir(tmpdir)

	for s := bufio.NewScanner(bytes.NewReader(a.Comment)); s.Scan(); {
		cmd := s.Text()
		cmd = strings.TrimLeft(cmd, "! ")
		if strings.HasPrefix(cmd, "exec ") {
			cmd = cmd[len("exec "):]
		}
		if !strings.HasPrefix(cmd, "cue ") {
			continue
		}

		args, err := shlex.Split(cmd)
		check(err)

		c, err := New(args[1:])
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
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": MainTest,
		"cue_stdinpipe": func() int {
			cwd, _ := os.Getwd()
			if err := mainTestStdinPipe(); err != nil {
				if err != ErrPrintedError { // print errors like Main
					errors.Print(os.Stderr, err, &errors.Config{
						Cwd:     cwd,
						ToSlash: inTest,
					})
				}
				return 1
			}
			return 0
		},
		"testcmd": func() int {
			if err := testCmd(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
		},
	}))
}

// homeEnvName extracts the logic from os.UserHomeDir to get the
// name of the environment variable that should be used when
// seting the user's home directory
func homeEnvName() string {
	switch goruntime.GOOS {
	case "windows":
		return "USERPROFILE"
	case "plan9":
		return "home"
	default:
		return "HOME"
	}
}

func mainTestStdinPipe() error {
	// Like MainTest, but sets stdin to a pipe,
	// to emulate stdin reads like a terminal.
	inTest = true
	cmd, err := New(os.Args[1:])
	if err != nil {
		return err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.SetInput(pr)
	_ = pw // we don't write to stdin at all, for now
	return cmd.Run(context.Background())
}

func testCmd() error {
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "concurrent":
		// Used to test that we support running tasks concurrently.
		// Run like `concurrent foo bar` and `concurrent bar foo`,
		// each command creates one file and waits for the other to exist.
		// If the commands are run sequentially, neither will succeed.
		if len(args) != 2 {
			return fmt.Errorf("usage: concurrent to_create to_wait\n")
		}
		toCreate := args[0]
		toWait := args[1]
		if err := os.WriteFile(toCreate, []byte("dummy"), 0o666); err != nil {
			return err
		}
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(toWait); err == nil {
				fmt.Printf("wrote %s and found %s\n", toCreate, toWait)
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("timed out waiting for %s to exist", toWait)
	default:
		return fmt.Errorf("unknown command: %q\n", cmd)
	}
}
