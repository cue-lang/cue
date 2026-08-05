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

package cmd_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/goproxytest"
	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
	"golang.org/x/tools/txtar"

	cuecmd "cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetestscript"
)

// hostGoModCache returns the host's GOMODCACHE, letting testscripts reuse
// already downloaded modules; gotooltest gives each script an empty GOPATH.
var hostGoModCache = sync.OnceValues(func() (string, error) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMODCACHE: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
})

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
				if !strings.HasSuffix(f.Name, ".cue") || path.Base(f.Name) == "invalid.cue" {
					return
				}
				_, err := parser.ParseFile(f.Name, f.Data)
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
	p := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		UpdateScripts:       cuetest.UpdateGoldenFiles(),
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// env-fill rewrites its argument files to replace any environment variable
			// references with their values, using the same algorithm as cmpenv.
			"env-fill": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) == 0 {
					ts.Fatalf("usage: env-fill args...")
				}
				for _, arg := range args {
					path := ts.MkAbs(arg)
					data := ts.ReadFile(path)
					data = tsExpand(ts, data)
					ts.Check(os.WriteFile(path, []byte(data), 0o666))
				}
			},
			// env-read sets the content of a file as the value of an
			// environment variable.
			"env-read": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 2 {
					ts.Fatalf("usage: env-read env-var filepath")
				}
				key := args[0]
				path := ts.MkAbs(args[1])
				data := ts.ReadFile(path)
				ts.Setenv(key, data)
			},
			// mod-time prints the modification time of a file to stdout.
			// The time is displayed as nanoseconds since the Unix epoch.
			"mod-time": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 1 {
					ts.Fatalf("usage: mod-time PATH")
				}
				path := ts.MkAbs(args[0])
				fi, err := os.Stat(path)
				ts.Check(err)
				_, err = fmt.Fprint(ts.Stdout(), fi.ModTime().UnixNano())
				ts.Check(err)
			},
			// find-files recursively lists files under a directory, like `find -type f` on Linux.
			// It prints slash-separated paths relative to the root working directory of the testscript run,
			// for the sake of avoiding verbose and non-deterministic absolute paths.
			"find-files": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) == 0 {
					ts.Fatalf("usage: find-files args...")
				}
				out := ts.Stdout()
				workdir := ts.Getenv("WORK")
				for _, arg := range args {
					path := ts.MkAbs(arg)
					err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if d.Type().IsRegular() {
							rel, err := filepath.Rel(workdir, path)
							ts.Check(err)
							fmt.Fprintln(out, filepath.ToSlash(rel))
						}
						return nil
					})
					ts.Check(err)
				}
			},
			// strconv-unquote treats each argument as a go quoted string, unquotes it and prints
			// them, separated by newlines, to stdout
			"strconv-unquote": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg {
					ts.Fatalf("usage: strconv-unquote args...")
				}
				for _, quoted := range args {
					s, err := strconv.Unquote(quoted)
					ts.Check(err)
					fmt.Fprintln(ts.Stdout(), s)
				}
			},
			// curl is a simple HTTP client for testscripts.
			// Supports: -X METHOD, -H header, -d data, -L (follow redirects), -w format, -f (fail on error)
			"curl": func(ts *testscript.TestScript, neg bool, args []string) {
				method := "GET"
				var headers []string
				var data string
				followRedirects := false
				writeFormat := ""
				failOnError := false

				var reqURL string
				for i := 0; i < len(args); i++ {
					arg := args[i]
					switch {
					case arg == "-X" && i+1 < len(args):
						i++
						method = args[i]
					case arg == "-H" && i+1 < len(args):
						i++
						headers = append(headers, args[i])
					case arg == "-d" && i+1 < len(args):
						i++
						data = args[i]
						if method == "GET" {
							method = "POST"
						}
					case arg == "-L":
						followRedirects = true
					case arg == "-f":
						failOnError = true
					case arg == "-w" && i+1 < len(args):
						i++
						writeFormat = args[i]
					case !strings.HasPrefix(arg, "-"):
						reqURL = arg
					}
				}
				if reqURL == "" {
					ts.Fatalf("curl: no URL specified")
				}

				var body io.Reader
				if data != "" {
					body = strings.NewReader(data)
				}
				req, err := http.NewRequest(method, reqURL, body)
				ts.Check(err)

				for _, h := range headers {
					key, val, _ := strings.Cut(h, ":")
					req.Header.Add(strings.TrimSpace(key), strings.TrimSpace(val))
				}

				client := &http.Client{}
				if !followRedirects {
					client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
						return http.ErrUseLastResponse
					}
				}

				resp, err := client.Do(req)
				ts.Check(err)
				defer resp.Body.Close()

				_, err = io.Copy(ts.Stdout(), resp.Body)
				ts.Check(err)

				// Handle -w format (mainly for adding newline)
				if writeFormat != "" {
					fmt.Fprint(ts.Stdout(), strings.ReplaceAll(writeFormat, `\n`, "\n"))
				}

				// Check for HTTP errors when -f is used
				failed := failOnError && resp.StatusCode >= 400
				if failed && !neg {
					ts.Fatalf("curl: HTTP %d", resp.StatusCode)
				}
				if !failed && neg {
					ts.Fatalf("curl: expected failure but got HTTP %d", resp.StatusCode)
				}
			},
		},
		Setup: func(e *testscript.Env) error {
			// Set up the environment that the cue command expects: the
			// registries described by the script's _registry* directories,
			// the cache directory, and the language version variables.
			if err := cuetestscript.Setup(cuetestscript.SetupEnv(e)); err != nil {
				return err
			}
			// Let testscripts build Go code against this cuelang.org/go
			// checkout via a replace directive without network access.
			checkoutRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
			if err != nil {
				return err
			}
			goModCache, err := hostGoModCache()
			if err != nil {
				return err
			}
			e.Vars = append(e.Vars,
				"CUE_CHECKOUT_ROOT="+checkoutRoot,
				"HOST_GOMODCACHE="+goModCache,
			)
			return nil
		},
		Condition: cuetest.Condition,
	}
	// Add the commands that come with the cue testscript environment,
	// such as memregistry; see [cuetestscript.Cmds].
	maps.Copy(p.Cmds, cuetestscript.TestscriptCmds())
	if err := gotooltest.Setup(&p); err != nil {
		t.Fatal(err)
	}
	goproxytest.Setup(&p)
	testscript.Run(t, p)
}

// TestScriptDebug takes a single testscript file and then runs it within the
// same process so it can be used for debugging. It runs the first cue command
// it finds.
//
// Usage Comment out t.Skip() and set file to test.
func TestX(t *testing.T) {
	t.Skip()
	// adt.OpenGraphs = true
	// adt.DebugDeps = true
	cuedebug.Init()
	cuedebug.Flags.LogEval = 1
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
		check(os.WriteFile(name, f.Data, 0666))
	}

	t.Chdir(tmpdir)

	for s := bufio.NewScanner(bytes.NewReader(a.Comment)); s.Scan(); {
		cmd := s.Text()
		cmd = strings.TrimLeft(cmd, "! ")
		cmd = strings.TrimPrefix(cmd, "exec ")
		if !strings.HasPrefix(cmd, "cue ") {
			continue
		}

		args := splitArgs(cmd)

		c, _ := cuecmd.New(args[1:])
		b := &bytes.Buffer{}
		c.SetOutput(b)
		err = c.Run(context.Background())
		// Always create an error to show
		t.Error(err, "\n", b.String())
		return
	}
	t.Fatal("NO COMMAND FOUND")
}

// splitArgs splits a testscript command line into arguments: space-separated,
// with single quotes grouping text. Unlike general shell splitting it ignores
// double quotes, matching how testscript itself parses these lines.
//
// This is a simplified version of testscript's own parser; it omits the
// doubled-quote literal escape and env expansion, as neither is needed to
// replay the first "exec cue" command for debugging in TestX.
func splitArgs(line string) []string {
	var args []string
	var cur []byte
	inArg, quoted := false, false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\'':
			quoted, inArg = !quoted, true
		case !quoted && (c == ' ' || c == '\t'):
			if inArg {
				args = append(args, string(cur))
				cur, inArg = cur[:0], false
			}
		default:
			cur, inArg = append(cur, c), true
		}
	}
	if inArg {
		args = append(args, string(cur))
	}
	return args
}

func TestMain(m *testing.M) {
	check := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	testscript.Main(m, map[string]func(){
		"cue": func() { os.Exit(cuecmd.Main()) },
		// Until https://github.com/rogpeppe/go-internal/issues/93 is fixed,
		// or we have some other way to use "exec" without caring about success,
		// this is an easy way for us to mimic `? exec cue`.
		"cue_exitzero": func() { cuecmd.Main() },
		"cue_stdinpipe": func() {
			cwd, _ := os.Getwd()
			if err := mainStdinPipe(); err != nil {
				if err != cuecmd.ErrPrintedError { // print errors like Main
					errors.Print(os.Stderr, err, &errors.Config{
						Cwd:     cwd,
						ToSlash: testing.Testing(),
					})
				}
				os.Exit(1)
			}
		},
		"testcmd": testCmd,
		// These Unix-like commands are used by a few testscripts, especially when testing `cue cmd`.
		// They are not available on vanilla Windows, so add a simple version of them here.
		"false": func() {
			os.Exit(1)
		},
		"echo": func() {
			flag.Parse()
			args := flag.Args()
			for i, arg := range args {
				if i > 0 {
					fmt.Print(" ")
				}
				fmt.Print(arg)
			}
			fmt.Println()
		},
		"cat": func() {
			flag.Parse()
			args := flag.Args()
			if len(args) == 0 {
				_, err := io.Copy(os.Stdout, os.Stdin)
				check(err)
				return
			}
			for _, arg := range args {
				f, err := os.Open(arg)
				check(err)
				_, err = io.Copy(os.Stdout, f)
				f.Close()
				check(err)
			}
		},
		// Like `cue export`, but as a standalone Go program which doesn't
		// go through cmd/cue's setup of cuecontext and the evaluator.
		// Useful to check what the export behavior is for Go API users,
		// for example in relation to env vars like CUE_EXPERIMENT or CUE_DEBUG.
		// Only works with cue stdin and json stdout for simplicity.
		"cuectx_export": func() {
			input, err := io.ReadAll(os.Stdin)
			check(err)
			ctx := cuecontext.New()
			v := ctx.CompileBytes(input)
			err = v.Validate(cue.Concrete(true))
			check(err)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "    ")
			err = enc.Encode(v)
			check(err)
		},
	})
}

func tsExpand(ts *testscript.TestScript, s string) string {
	return os.Expand(s, func(key string) string {
		return ts.Getenv(key)
	})
}

func mainStdinPipe() error {
	// Like Main, but sets stdin to a pipe,
	// to emulate stdin reads like a terminal.
	cmd, _ := cuecmd.New(os.Args[1:])
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.SetInput(pr)
	_ = pw // we don't write to stdin at all, for now
	return cmd.Run(context.Background())
}

func testCmd() {
	check := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "concurrent":
		// Used to test that we support running tasks concurrently.
		// Run like `concurrent foo bar` and `concurrent bar foo`,
		// each command creates one file and waits for the other to exist.
		// If the commands are run sequentially, neither will succeed.
		if len(args) != 2 {
			check(fmt.Errorf("usage: concurrent to_create to_wait\n"))
		}
		toCreate := args[0]
		toWait := args[1]
		check(os.WriteFile(toCreate, []byte("dummy"), 0o666))
		for range 10 {
			if _, err := os.Stat(toWait); err == nil {
				fmt.Printf("wrote %s and found %s\n", toCreate, toWait)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		check(fmt.Errorf("timed out waiting for %s to exist", toWait))
	case "exitcode":
		// Used to test various non-zero exit status codes.
		if len(args) > 0 {
			code, err := strconv.Atoi(args[0])
			check(err)
			os.Exit(code)
		}
	case "sleep_and_print":
		// Used to test slow exec.Run commands in `cue cmd`.
		// It sleeps a given [time.Duration] string, and then prints messages one per line.
		// As a special case, the message UNIX_MILLI prints the current Unix time in milliseconds.
		if len(args) < 1 {
			check(fmt.Errorf("usage: sleep_and_print duration [msg...]\n"))
		}
		d, err := time.ParseDuration(args[0])
		check(err)
		time.Sleep(d)

		for _, msg := range args[1:] {
			if msg == "UNIX_MILLI" {
				fmt.Println(time.Now().UTC().UnixMilli())
			} else {
				fmt.Println(msg)
			}
		}
	case "expand":
		// Expands env vars like $FOO and ${BAR} in arguments, and prints them out.
		for _, arg := range args {
			fmt.Println(os.ExpandEnv(arg))
		}
	default:
		check(fmt.Errorf("unknown command: %q\n", cmd))
	}
}
