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
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"github.com/google/shlex"
	"github.com/rogpeppe/go-internal/goproxytest"
	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
	"golang.org/x/oauth2"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/registrytest"
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
	t.Cleanup(srv.Close)
	p := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		UpdateScripts:       cuetest.UpdateGoldenFiles,
		RequireExplicitExec: true,
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
			// memregistry starts an in-memory OCI server and sets the argument
			// environment variable name to its hostname.
			"memregistry": func(ts *testscript.TestScript, neg bool, args []string) {
				usage := func() {
					ts.Fatalf("usage: memregistry [-auth=username:password] <envvar-name>")
				}
				if neg {
					usage()
				}
				var auth *registrytest.AuthConfig
				if len(args) > 0 && strings.HasPrefix(args[0], "-") {
					userPass, ok := strings.CutPrefix(args[0], "-auth=")
					if !ok {
						usage()
					}
					user, pass, ok := strings.Cut(userPass, ":")
					if !ok {
						usage()
					}
					auth = &registrytest.AuthConfig{
						Username: user,
						Password: pass,
					}
					args = args[1:]
				}
				if len(args) != 1 {
					usage()
				}

				srv := httptest.NewServer(registrytest.AuthHandler(ociserver.New(ocimem.New(), nil), auth))
				u, _ := url.Parse(srv.URL)
				ts.Setenv(args[0], u.Host)
				ts.Defer(srv.Close)
			},
			// memregistry starts an HTTP server with enough endpoints to test `cue login`.
			// It takes a single argument to describe the oauth server's behavior:
			//
			// * device-code-expired: polling for a token with device_code
			//   always responds with [tokenErrorCodeExpired]
			// * pending-success: polling for a token with device_code
			//   responds with [tokenErrorCodePending] once, and then succeeds
			// * immediate-success: polling for a token with device_code succeeds right away
			"oauthregistry": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 1 {
					ts.Fatalf("usage: oauthregistry <mode>")
				}
				ts.Setenv("CUE_EXPERIMENT", "modules")
				srv := newMockRegistryOauth(args[0])
				u, _ := url.Parse(srv.URL)
				ts.Setenv("CUE_REGISTRY", u.Host+"+insecure")
				ts.Defer(srv.Close)
			},
		},
		Setup: func(e *testscript.Env) error {
			// If a testscript loads CUE packages but forgot to set up a cue.mod,
			// we might walk up to the system's temporary directory looking for cue.mod.
			// If /tmp/cue.mod exists for instance, this can lead to test failures
			// as our behavior when it comes to the module root and file paths changes.
			// Make the testscript.Params.WorkdirRoot directory a module,
			// ensuring consistent behavior no matter what parent directories contain.
			//
			// Note that creating the directory is enough for now,
			// and we ignore ErrExist since only the first test will succeed.
			// We can't create the directory before testscript.Run, as it sets up WorkdirRoot.
			workdirRoot := filepath.Dir(e.WorkDir)
			if err := os.Mkdir(filepath.Join(workdirRoot, "cue.mod"), 0o777); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}

			e.Vars = append(e.Vars,
				"GOPROXY="+srv.URL,
				"GONOSUMDB=*", // GOPROXY is a private proxy
			)
			entries, err := os.ReadDir(e.WorkDir)
			if err != nil {
				return fmt.Errorf("cannot read workdir: %v", err)
			}
			hasRegistry := false
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				regID, ok := strings.CutPrefix(entry.Name(), "_registry")
				if !ok {
					continue
				}
				// There's a _registry directory. Start a fake registry server to serve
				// the modules in it.
				hasRegistry = true
				registryDir := filepath.Join(e.WorkDir, entry.Name())
				prefix := ""
				if data, err := os.ReadFile(filepath.Join(e.WorkDir, "_registry"+regID+"_prefix")); err == nil {
					prefix = strings.TrimSpace(string(data))
				}
				reg, err := registrytest.New(os.DirFS(registryDir), prefix)
				if err != nil {
					return fmt.Errorf("cannot start test registry server: %v", err)
				}
				if prefix != "" {
					prefix = "/" + prefix
				}
				e.Vars = append(e.Vars,
					"CUE_REGISTRY"+regID+"="+reg.Host()+prefix+"+insecure",
					// This enables some tests to construct their own malformed
					// CUE_REGISTRY values that still refer to the test registry.
					"DEBUG_REGISTRY"+regID+"_HOST="+reg.Host(),
					// Some tests execute cue commands that need to write cache files.
					// Since os.UserCacheDir relies on OS-specific env vars that we don't set,
					// explicitly set up the cache directory somewhere predictable.
					"CUE_CACHE_DIR="+filepath.Join(e.WorkDir, ".tmp/cache"),
				)
				e.Defer(reg.Close)
			}
			if hasRegistry {
				e.Vars = append(e.Vars,
					"CUE_EXPERIMENT=modules",
				)
			}
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
		check(os.WriteFile(name, f.Data, 0666))
	}

	cwd, err := os.Getwd()
	check(err)
	defer func() { _ = os.Chdir(cwd) }()
	_ = os.Chdir(tmpdir)

	for s := bufio.NewScanner(bytes.NewReader(a.Comment)); s.Scan(); {
		cmd := s.Text()
		cmd = strings.TrimLeft(cmd, "! ")
		cmd = strings.TrimPrefix(cmd, "exec ")
		if !strings.HasPrefix(cmd, "cue ") {
			continue
		}

		args, err := shlex.Split(cmd)
		check(err)

		c, _ := New(args[1:])
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
		"cue": Main,
		// Until https://github.com/rogpeppe/go-internal/issues/93 is fixed,
		// or we have some other way to use "exec" without caring about success,
		// this is an easy way for us to mimic `? exec cue`.
		"cue_exitzero": func() int {
			Main()
			return 0
		},
		"cue_stdinpipe": func() int {
			cwd, _ := os.Getwd()
			if err := mainStdinPipe(); err != nil {
				if err != ErrPrintedError { // print errors like Main
					errors.Print(os.Stderr, err, &errors.Config{
						Cwd:     cwd,
						ToSlash: testing.Testing(),
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

func tsExpand(ts *testscript.TestScript, s string) string {
	return os.Expand(s, func(key string) string {
		return ts.Getenv(key)
	})
}

func mainStdinPipe() error {
	// Like Main, but sets stdin to a pipe,
	// to emulate stdin reads like a terminal.
	cmd, _ := New(os.Args[1:])
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

// newMockRegistryOauth starts a test HTTP server with the OAuth2 device flow endpoints
// used by `cue login` to obtain an access token.
// Note that this HTTP server isn't an OCI registry yet, as that isn't needed for now.
//
// TODO: once we support refresh tokens, add those endpoints and test them too.
func newMockRegistryOauth(mode string) *httptest.Server {
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	const (
		staticUserCode    = "user-code"
		staticDeviceCode  = "device-code-longer-string"
		staticAccessToken = "secret-access-token"
		intervalSecs      = 1 // 1s to keep the tests fast
	)
	// OAuth2 Device Authorization Request endpoint: https://datatracker.ietf.org/doc/html/rfc8628#section-3.1
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, oauth2.DeviceAuthResponse{
			DeviceCode: staticDeviceCode,
			UserCode:   staticUserCode,

			VerificationURI:         ts.URL + "/login/device",
			VerificationURIComplete: ts.URL + "/login/device?user_code=" + url.QueryEscape(staticUserCode),

			Expiry:   time.Now().Add(time.Minute),
			Interval: intervalSecs,
		})
	})
	// OAuth2 Token endpoint: https://datatracker.ietf.org/doc/html/rfc6749#section-3.2
	var tokenRequestCounter atomic.Int64
	mux.HandleFunc("/login/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		deviceCode := r.FormValue("device_code")
		if deviceCode != staticDeviceCode {
			writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodeDenied})
			return
		}
		switch mode {
		case "device-code-expired":
			writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodeExpired})
		case "pending-success":
			count := tokenRequestCounter.Add(1)
			if count == 1 {
				writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodePending})
				break
			}
			fallthrough
		case "immediate-success":
			writeJSON(w, http.StatusOK, oauth2.Token{
				AccessToken: staticAccessToken,
				TokenType:   "Bearer",
				Expiry:      time.Now().Add(time.Hour),
			})
		default:
			panic(fmt.Sprintf("unknown mode: %q", mode))
		}
	})
	return ts
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	b, err := json.Marshal(v)
	if err != nil { // should never happen
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(b)
}

const (
	// Device flow token error code strings from https://datatracker.ietf.org/doc/html/rfc8628#section-3.5
	tokenErrorCodePending  = "authorization_pending" // waiting for user
	tokenErrorCodeSlowDown = "slow_down"             // increase polling interval
	tokenErrorCodeDenied   = "access_denied"         // the user denied the request
	tokenErrorCodeExpired  = "expired_token"         // the device_code expired
)

// tokenError implements the error response structure defined by
// https://datatracker.ietf.org/doc/html/rfc6749#section-5.2
type tokenError struct {
	ErrorCode        string `json:"error"` // one of the constants above
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}
