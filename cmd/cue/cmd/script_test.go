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
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociref"
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
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
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
				if !strings.HasSuffix(f.Name, ".cue") || path.Base(f.Name) == "invalid.cue" {
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
	srv := goproxytest.NewTestServer(t, filepath.Join("testdata", "mod"), "")
	p := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		UpdateScripts:       cuetest.UpdateGoldenFiles,
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
			// get-manifest writes the manifest for a given reference within an OCI
			// registry to a file in JSON format.
			"get-manifest": func(ts *testscript.TestScript, neg bool, args []string) {
				usage := func() {
					ts.Fatalf("usage: get-metadata OCI-ref@tag dest-file")
				}
				if neg {
					usage()
				}
				if len(args) != 2 {
					usage()
				}
				ref, err := ociref.Parse(args[0])
				if err != nil {
					ts.Fatalf("invalid OCI reference %q: %v", args[0], err)
				}
				if ref.Tag == "" {
					ts.Fatalf("no tag in OCI reference %q", args[0])
				}
				client, err := ociclient.New(ref.Host, &ociclient.Options{
					Insecure: true,
				})
				ts.Check(err)
				r, err := client.GetTag(context.Background(), ref.Repository, ref.Tag)
				ts.Check(err)
				data, err := io.ReadAll(r)
				ts.Check(err)
				err = os.WriteFile(ts.MkAbs(args[1]), data, 0o666)
				ts.Check(err)
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

				srv, err := registrytest.NewServer(ocimem.NewWithConfig(&ocimem.Config{ImmutableTags: true}), auth)
				if err != nil {
					ts.Fatalf("cannot start registrytest server: %v", err)
				}
				ts.Setenv(args[0], srv.Host())
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
				if neg || len(args) != 1 {
					ts.Fatalf("usage: oauthregistry <mode>")
				}
				ts.Setenv("CUE_EXPERIMENT", "modules")
				srv := newMockRegistryOauth(args[0])
				u, _ := url.Parse(srv.URL)
				ts.Setenv("CUE_REGISTRY", u.Host+"+insecure")
				ts.Defer(srv.Close)
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
					err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
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

				// The current language version which would be added by `cue mod init`, e.g. v0.10.0.
				"CUE_LANGUAGE_VERSION="+cueversion.LanguageVersion(),
				// A later language version which only increases the bugfix release, e.g. v0.10.99.
				"CUE_LANGUAGE_VERSION_BUGFIX="+semver.MajorMinor(cueversion.LanguageVersion())+".99",
			)
			entries, err := os.ReadDir(e.WorkDir)
			if err != nil {
				return fmt.Errorf("cannot read workdir: %v", err)
			}
			// As modules are enabled by default, we always want a cache directory.
			// Since os.UserCacheDir relies on OS-specific env vars that we don't set,
			// explicitly set up the cache directory somewhere predictable.
			e.Vars = append(e.Vars, "CUE_CACHE_DIR="+filepath.Join(e.WorkDir, ".tmp/cache"))
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
				registryDir := filepath.Join(e.WorkDir, entry.Name())
				prefix := ""
				if data, err := os.ReadFile(filepath.Join(e.WorkDir, "_registry"+regID+"_prefix")); err == nil {
					prefix = strings.TrimSpace(string(data))
				}
				useProxy := false
				proxyFile := filepath.Join(e.WorkDir, "_registry"+regID+"_proxy")
				if data, err := os.ReadFile(proxyFile); err == nil {
					useProxy, err = strconv.ParseBool(strings.TrimSpace(string(data)))
					if err != nil {
						return fmt.Errorf("invalid contents of proxy file %q: %v", proxyFile, err)
					}
				}
				reg, err := registrytest.New(os.DirFS(registryDir), prefix)
				if err != nil {
					return fmt.Errorf("cannot start test registry server: %v", err)
				}
				e.Defer(reg.Close)
				if prefix != "" {
					prefix = "/" + prefix
				}
				regHost := reg.Host()
				if useProxy {
					// Use a proxy for the registry, mirroring the way that the Central Registry
					// works.
					proxyClient, err := ociclient.New(regHost, &ociclient.Options{
						Insecure: true,
					})
					if err != nil {
						return fmt.Errorf("cannot create oci proxy client")
					}
					reg2 := httptest.NewServer(ociserver.New(proxyClient, nil))
					reg2URL, _ := url.Parse(reg2.URL)
					regHost = reg2URL.Host
					e.Defer(reg2.Close)
				}
				e.Vars = append(e.Vars,
					"CUE_REGISTRY"+regID+"="+regHost+prefix+"+insecure",
					// This enables some tests to construct their own malformed
					// CUE_REGISTRY values that still refer to the test registry.
					"DEBUG_REGISTRY"+regID+"_HOST="+regHost,
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
				ExpiresIn:   int64(time.Hour / time.Second), // 1h in seconds
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
