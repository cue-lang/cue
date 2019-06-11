// Copyright 2019 CUE Authors
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

package kubernetes

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/internal/cuetest"
	"github.com/kylelemons/godebug/diff"
	"github.com/retr0h/go-gilt/copy"
)

var (
	update  = flag.Bool("update", false, "update test data")
	cleanup = flag.Bool("cleanup", true, "clean up generated files")
)

func TestTutorial(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// Read the tutorial.
	b, err := ioutil.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}

	// Copy test data and change the cwd to this directory.
	dir, err := ioutil.TempDir("", "tutorial")
	if err != nil {
		log.Fatal(err)
	}
	if *cleanup {
		defer os.RemoveAll(dir)
	} else {
		logf(t, "Temporary dir: %v", dir)
	}

	wd := filepath.Join(dir, "services")
	if err := copy.Dir(filepath.Join("testdata", "services"), wd); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	logf(t, "Tmp dir: %s", wd)

	// Execute the tutorial.
	for c := cuetest.NewChunker(t, b); c.Next("```", "```"); {
		for c := cuetest.NewChunker(t, c.Bytes()); c.Next("$ ", "\n"); {
			alt := c.Text()
			cmd := strings.Replace(alt, "<<EOF", "", -1)

			input := ""
			if cmd != alt {
				if !c.Next("", "EOF") {
					t.Fatalf("non-terminated <<EOF")
				}
				input = c.Text()
			}

			redirect := ""
			if p := strings.Index(cmd, " >"); p > 0 {
				redirect = cmd[p+1:]
				cmd = cmd[:p]
			}

			switch cmd = strings.TrimSpace(cmd); {
			case strings.HasPrefix(cmd, "cat"):
				if input == "" {
					break
				}
				var r *os.File
				var err error
				if strings.HasPrefix(redirect, ">>") {
					// Append input
					r, err = os.OpenFile(
						strings.TrimSpace(redirect[2:]),
						os.O_APPEND|os.O_CREATE|os.O_WRONLY,
						0666)
				} else { // strings.HasPrefix(redirect, ">")
					// Create new file with input
					r, err = os.Create(strings.TrimSpace(redirect[1:]))
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.WriteString(r, input)
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				if err != nil {
					t.Fatal(err)
				}

			case strings.HasPrefix(cmd, "cue "):
				logf(t, "$ %s", cmd)

				if strings.HasPrefix(cmd, "cue create") {
					// Don't execute the kubernetes dry run.
					break
				}

				cuetest.Run(t, wd, cmd, &cuetest.Config{
					Stdin:  strings.NewReader(input),
					Stdout: ioutil.Discard,
				})

			case strings.HasPrefix(cmd, "sed "),
				strings.HasPrefix(cmd, "touch "):
				logf(t, "$ %s", cmd)
				args := cuetest.SplitArgs(t, cmd)
				cx := exec.Command(args[0], args[1:]...)
				if input != "" {
					cx.Stdin = strings.NewReader(input)
					cx.Stdout = ioutil.Discard
				}
				if err := cx.Run(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	if err := os.Chdir(filepath.Join(cwd, "quick")); err != nil {
		t.Fatal(err)
	}

	if *update {
		// Remove all old cue files.
		filepath.Walk("", func(path string, info os.FileInfo, err error) error {
			if isCUE(path) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			return nil
		})

		filepath.Walk(wd, func(path string, info os.FileInfo, err error) error {
			if isCUE(path) {
				return copy.File(path, "services"+path[len(wd):])
			}
			return nil
		})
		return
	}

	// Compare the output in the temp directory with the quick output.
	err = filepath.Walk(wd, func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) != ".cue" {
			return nil
		}
		b1, err := ioutil.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b2, err := ioutil.ReadFile("services" + path[len(wd):])
		if err != nil {
			t.Fatal(err)
		}
		got, want := string(b1), string(b2)
		if got != want {
			t.Log(diff.Diff(got, want))
			return fmt.Errorf("file %q differs", path)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func isCUE(filename string) bool {
	return filepath.Ext(filename) == ".cue" && !strings.Contains(filename, "_tool")
}

func logf(t *testing.T, format string, args ...interface{}) {
	t.Logf(format, args...)
	log.Printf(format, args...)
}

// TODO:
// Test manual and quick: evaluation results in output of testdata directory.
