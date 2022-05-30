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
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/goproxytest"
	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"

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
	filepath.Walk(root, func(fullpath string, info os.FileInfo, err error) error {
		if err != nil {
			t.Error(err)
			return nil
		}
		if !strings.HasSuffix(fullpath, ".txt") ||
			strings.HasPrefix(filepath.Base(fullpath), "fix") {
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

// testscriptSameProcess allows TestScript to run a single script,
// but running CUE commands in the same process to help debugging.
// This is done by running CUE via Params.Cmds; see the conditionals below.
// Also note that this makes the first CUE command fail and stop the script.
//
// Usage: TESTSCRIPT_SAME_PROCESS=true go test -run ScriptDebug/^eval_e$
var testscriptSameProcess = os.Getenv("TESTSCRIPT_SAME_PROCESS") == "true"

func TestScript(t *testing.T) {
	srv, err := goproxytest.NewServer(filepath.Join("testdata", "mod"), "")
	if err != nil {
		t.Fatalf("cannot start proxy: %v", err)
	}

	if testscriptSameProcess {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(cwd) }()
	}

	p := testscript.Params{
		Dir:           filepath.Join("testdata", "script"),
		UpdateScripts: cuetest.UpdateGoldenFiles,
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

			if testscriptSameProcess {
				_ = os.Chdir(e.WorkDir)
			}
			return nil
		},
		Condition: cuetest.Condition,
	}
	if testscriptSameProcess {
		p.Cmds = make(map[string]func(ts *testscript.TestScript, neg bool, args []string))
		p.Cmds["cue"] = func(ts *testscript.TestScript, neg bool, args []string) {
			c, err := New(args)
			ts.Check(err)
			b := &bytes.Buffer{}
			c.SetOutput(b)
			err = c.Run(context.Background())
			// Always create an error to show
			ts.Fatalf("%s: %s", err, b.String())
		}
	}
	if err := gotooltest.Setup(&p); err != nil {
		t.Fatal(err)
	}
	testscript.Run(t, p)
}

func TestMain(m *testing.M) {
	cmds := make(map[string]func() int)
	if !testscriptSameProcess {
		cmds["cue"] = MainTest
	}
	os.Exit(testscript.RunMain(m, cmds))
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
