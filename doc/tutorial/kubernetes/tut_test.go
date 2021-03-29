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
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/diff"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/copy"
	"cuelang.org/go/internal/cuetest"
)

var (
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
		defer logf(t, "Temporary dir: %v", dir)
	}

	wd := filepath.Join(dir, "services")
	if err := copy.Dir(filepath.Join("original", "services"), wd); err != nil {
		t.Fatal(err)
	}

	run(t, dir, "cue mod init", &config{
		// Stdin: strings.NewReader(input),
	})

	if cuetest.UpdateGoldenFiles {
		// The test environment won't work in all environments. We create
		// a fake go.mod so that Go will find the module root. By default
		// we won't set it.
		out := execute(t, dir, "go", "mod", "init", "cuelang.org/dummy")
		logf(t, "%s", out)
	} else {
		// We only fetch new kubernetes files with when updating.
		err := copy.Dir(load.GenPath("quick"), load.GenPath(dir))
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	logf(t, "Changed to directory: %s", wd)

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

			logf(t, "$ %s", cmd)
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
				if strings.HasPrefix(cmd, "cue create") {
					// Don't execute the kubernetes dry run.
					break
				}
				if strings.HasPrefix(cmd, "cue mod init") {
					// Already ran this at setup.
					break
				}

				if !cuetest.UpdateGoldenFiles && strings.HasPrefix(cmd, "cue get") {
					// Don't fetch stuff in normal mode.
					break
				}

				run(t, wd, cmd, &config{
					Stdin:  strings.NewReader(input),
					Stdout: os.Stdout,
				})

			case strings.HasPrefix(cmd, "sed "):
				c := cuetest.NewChunker(t, []byte(cmd))
				c.Next("s/", "/")
				re := regexp.MustCompile(c.Text())
				c.Next("", "/'")
				repl := c.Bytes()
				c.Next(" ", ".cue")
				file := c.Text() + ".cue"
				b, err := ioutil.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				b = re.ReplaceAll(b, repl)
				err = ioutil.WriteFile(file, b, 0644)
				if err != nil {
					t.Fatal(err)
				}

			case strings.HasPrefix(cmd, "touch "):
				logf(t, "$ %s", cmd)
				file := strings.TrimSpace(cmd[len("touch "):])
				err := ioutil.WriteFile(file, []byte(""), 0644)
				if err != nil {
					t.Fatal(err)
				}
			case strings.HasPrefix(cmd, "go "):
				if !cuetest.UpdateGoldenFiles && strings.HasPrefix(cmd, "go get") {
					// Don't fetch stuff in normal mode.
					break
				}

				out := execute(t, wd, splitArgs(t, cmd)...)
				logf(t, "%s", out)
			}
		}
	}

	if err := os.Chdir(filepath.Join(cwd, "quick")); err != nil {
		t.Fatal(err)
	}

	if cuetest.UpdateGoldenFiles {
		// Remove all old cue files.
		err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			if isCUE(path) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}

		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if isCUE(path) {
				dst := path[len(dir)+1:]
				err := os.MkdirAll(filepath.Dir(dst), 0755)
				if err != nil {
					return err
				}
				return copy.File(path, dst)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	// Compare the output in the temp directory with the quick output.
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) != ".cue" {
			return nil
		}
		b1, err := ioutil.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b2, err := ioutil.ReadFile(path[len(dir)+1:])
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

func TestEval(t *testing.T) {
	for _, dir := range []string{"quick", "manual"} {
		t.Run(dir, func(t *testing.T) {
			buf := &bytes.Buffer{}
			run(t, dir, "cue eval ./...", &config{
				Stdout: buf,
			})

			cwd, _ := os.Getwd()
			pattern := fmt.Sprintf("//.*%s.*", regexp.QuoteMeta(filepath.Join(cwd, dir)))
			re, err := regexp.Compile(pattern)
			if err != nil {
				t.Fatal(err)
			}
			got := re.ReplaceAll(buf.Bytes(), []byte{})
			got = bytes.TrimSpace(got)

			testfile := filepath.Join("testdata", dir+".out")

			if cuetest.UpdateGoldenFiles {
				err := ioutil.WriteFile(testfile, got, 0644)
				if err != nil {
					t.Fatal(err)
				}
				return
			}

			b, err := ioutil.ReadFile(testfile)
			if err != nil {
				t.Fatal(err)
			}

			if got, want := string(got), string(b); got != want {
				t.Log(got)
				t.Errorf("output differs for file %s in %s", testfile, cwd)
			}
		})
	}
}

type config struct {
	Stdin  io.Reader
	Stdout io.Writer
	Golden string
}

// execute executes the given command in the given directory
func execute(t *testing.T, dir string, args ...string) string {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { os.Chdir(old) }()

	logf(t, "Executing command: %s", strings.Join(args, " "))

	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run [%v] in %s: %v\n%s", cmd, dir, err, out)
	}
	return string(out)
}

// run executes the given command in the given directory and reports any
// errors comparing it to the gold standard.
func run(t *testing.T, dir, command string, cfg *config) {
	if cfg == nil {
		cfg = &config{}
	}

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { os.Chdir(old) }()

	logf(t, "Executing command: %s", command)

	command = strings.TrimSpace(command[4:])
	args := splitArgs(t, command)
	logf(t, "Args: %q", args)

	buf := &bytes.Buffer{}
	if cfg.Golden != "" {
		if cfg.Stdout != nil {
			t.Fatal("cannot set Golden and Stdout")
		}
		cfg.Stdout = buf
	}
	cmd, err := cmd.New(args)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Stdout != nil {
		cmd.SetOutput(cfg.Stdout)
	} else {
		cmd.SetOutput(buf)
	}
	if cfg.Stdin != nil {
		cmd.SetInput(cfg.Stdin)
	}
	if err = cmd.Run(context.Background()); err != nil {
		if cfg.Stdout == nil {
			logf(t, "Output:\n%s", buf.String())
		}
		logf(t, "Execution failed: %v", err)
	}

	if cfg.Golden == "" {
		return
	}

	pattern := fmt.Sprintf("//.*%s.*", regexp.QuoteMeta(dir))
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	got := re.ReplaceAllString(buf.String(), "")
	got = strings.TrimSpace(got)

	want := strings.TrimSpace(cfg.Golden)
	if got != want {
		t.Errorf("files differ:\n%s", diff.Diff(got, want))
	}
}

func logf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Logf(format, args...)
}

func splitArgs(t *testing.T, s string) (args []string) {
	c := cuetest.NewChunker(t, []byte(s))
	for {
		found := c.Find(" '")
		args = append(args, strings.Split(c.Text(), " ")...)
		if !found {
			break
		}
		c.Next("", "' ")
		args = append(args, c.Text())
	}
	return args
}
