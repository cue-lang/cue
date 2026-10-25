// Copyright 2026 The CUE Authors
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
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cmd/cue/cmd"
)

// TestLoginDeviceFlow runs `cue login` with the OAuth2 device flow
// against [mockRegistryOauth]. It runs in a [synctest] bubble so that waiting
// for the polling interval takes no time, which requires serving the mock
// in memory: a client waiting on a real network connection isn't durably blocked.
//
// Login tests which never poll, such as with --token, are testscripts.
func TestLoginDeviceFlow(t *testing.T) {
	// Not parallel, as we replace [http.DefaultTransport] and set an env var.
	const (
		stdoutPrompt = `
Enter the code user-code via: http://registry.test/login/device
Or just open: http://registry.test/login/device?user_code=user-code

`
		// Timestamps are always stored in UTC and truncated to seconds.
		// The mock gives no refresh token, which is omitted.
		loginsFormat = `
{
	"registries": {
		"registry.test": {
			"access_token": "secret-access-token",
			"token_type": "Bearer",
			"expiry": "%s"
		}
	}
}
`
	)
	for _, test := range []struct {
		mode       string
		wantErr    string // a regular expression; empty when the login succeeds
		wantExpiry string // the stored token's expiry when the login succeeds
		wantPolls  string
	}{{
		mode: "immediate-success",
		// The bubble's clock starts at midnight UTC on 2000-01-01, and the token
		// expires an hour after the first poll, five seconds later.
		wantExpiry: "2000-01-01T01:00:05Z",
		wantPolls:  "poll\n",
	}, {
		mode:       "pending-success",
		wantExpiry: "2000-01-01T01:00:10Z",
		wantPolls:  "poll\npoll\n",
	}, {
		mode:      "device-code-expired",
		wantErr:   `^cannot obtain the OAuth2 token: oauth2: "expired_token"$`,
		wantPolls: "poll\n",
	}} {
		t.Run(test.mode, func(t *testing.T) {
			configDir := t.TempDir()
			loginsPath := filepath.Join(configDir, "logins.json")
			t.Setenv("CUE_CONFIG_DIR", configDir)

			var polls strings.Builder
			origTransport := http.DefaultTransport
			http.DefaultTransport = handlerTransport{mockRegistryOauth(test.mode, &polls)}
			t.Cleanup(func() { http.DefaultTransport = origTransport })

			synctest.Test(t, func(t *testing.T) {
				c, err := cmd.New([]string{"login", "registry.test+insecure"})
				qt.Assert(t, qt.IsNil(err))
				var stdout bytes.Buffer
				c.SetOut(&stdout)
				c.SetErr(io.Discard)
				err = c.Run(t.Context())

				wantStdout := stdoutPrompt[1:]
				logins, readErr := os.ReadFile(loginsPath)
				if test.wantErr != "" {
					qt.Assert(t, qt.ErrorMatches(err, test.wantErr))
					qt.Assert(t, qt.ErrorIs(readErr, fs.ErrNotExist))
				} else {
					qt.Assert(t, qt.IsNil(err))
					wantStdout += "Login for registry.test stored in " + loginsPath + "\n"
					qt.Assert(t, qt.IsNil(readErr))
					qt.Assert(t, qt.Equals(string(logins), fmt.Sprintf(loginsFormat[1:], test.wantExpiry)))
				}
				qt.Assert(t, qt.Equals(stdout.String(), wantStdout))
			})
			qt.Assert(t, qt.Equals(polls.String(), test.wantPolls))
		})
	}
}

// handlerTransport is an [http.RoundTripper] which serves each request
// in memory, in the calling goroutine.
type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}
