// Copyright 2020 CUE Authors
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

package http

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/task"
	"cuelang.org/go/internal/value"
	"cuelang.org/go/pkg/internal"
)

func newTLSServer() *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"foo": "bar"}`
		w.Write([]byte(resp))
	}))
	// The TLS errors produced by TestTLS would otherwise print noise to stderr.
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	return server
}

func parse(t *testing.T, kind, expr string) cue.Value {
	t.Helper()

	return value.UnifyBuiltin(build(t, expr), kind)
}

// build builds a value without unifying it with any builtin schema,
// so that tests can exercise values which a schema itself rejects.
func build(t *testing.T, expr string) cue.Value {
	t.Helper()

	x, err := parser.ParseExpr("test", expr)
	if err != nil {
		t.Fatal(err)
	}
	v := internal.NewContext().BuildExpr(x)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	return v
}

// run runs an http.Do task on v, tied to the test's context.
func run(t *testing.T, v cue.Value) (any, error) {
	t.Helper()

	return (*httpCmd).Run(nil, &task.Context{Context: t.Context(), Obj: v})
}

func TestTLS(t *testing.T) {
	s := newTLSServer()
	t.Cleanup(s.Close)

	v1 := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "%s"}`, s.URL))
	_, err := run(t, v1)
	if err == nil {
		t.Fatal("http call should have failed")
	}

	v2 := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "%s", tls: verify: false}`, s.URL))
	_, err = run(t, v2)
	if err != nil {
		t.Fatal(err)
	}

	pemBlock := func(typ string, der []byte) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
	}
	for _, tc := range []struct {
		name   string
		caCert string
		// wantErr, when not empty, is a substring of the expected error.
		wantErr string
	}{{
		// The encoding which any certificate authority hands out.
		name:   "certificate",
		caCert: pemBlock("CERTIFICATE", s.Certificate().Raw),
	}, {
		// Not a certificate encoding, but supported for backwards compatibility.
		name:   "public key",
		caCert: pemBlock("PUBLIC KEY", s.Certificate().Raw),
	}, {
		name:    "not PEM",
		caCert:  "not a PEM file at all\n",
		wantErr: "invalid caCert: no certificates found",
	}, {
		name:    "unrelated PEM block",
		caCert:  pemBlock("PRIVATE KEY", []byte("not a certificate")),
		wantErr: "invalid caCert: no certificates found",
	}, {
		name:    "malformed certificate",
		caCert:  pemBlock("CERTIFICATE", []byte("not a certificate")),
		wantErr: "invalid caCert: x509:",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			v := parse(t, "tool/http.Get", fmt.Sprintf(`{url: %q, tls: caCert: %q}`, s.URL, tc.caCert))

			_, err := run(t, v)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v; want an error containing %q", err, tc.wantErr)
				}
				// The error points at caCert rather than at the whole task.
				want := v.LookupPath(cue.MakePath(cue.Str("tls"), cue.Str("caCert"))).Pos()
				if got := cueerrors.Positions(err); len(got) == 0 || got[0] != want {
					t.Errorf("got error positions %v; want %v", got, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseHeaders(t *testing.T) {
	req := `
	header: {
		"Accept-Language": "en,nl"
	}
	trailer: {
		"Accept-Language": "en,nl"
		User: "foo"
	}
	`
	testCases := []struct {
		req   string
		field string
		out   string
	}{{
		field: "header",
		out:   "nil",
	}, {
		req:   req,
		field: "non-exist",
		out:   "nil",
	}, {
		req:   req,
		field: "header",
		out:   "Accept-Language: en,nl\r\n",
	}, {
		req:   req,
		field: "trailer",
		out:   "Accept-Language: en,nl\r\nUser: foo\r\n",
	}, {
		req: `
		header: {
			"1": 'a'
		}
		`,
		field: "header",
		out:   "header.\"1\": cannot use value 'a' (type bytes) as list",
	}, {
		req: `
			header: 1
			`,
		field: "header",
		out:   "header: cannot use value 1 (type int) as struct",
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			ctx := internal.NewContext()
			v := ctx.CompileString(tc.req, cue.Filename("http headers"))
			if err := v.Err(); err != nil {
				t.Fatal(err)
			}

			h, err := parseHeaders(v, tc.field)

			b := &strings.Builder{}
			switch {
			case err != nil:
				fmt.Fprint(b, err)
			case h == nil:
				b.WriteString("nil")
			default:
				_ = h.Write(b)
			}

			got := b.String()
			if got != tc.out {
				t.Errorf("got %q; want %q", got, tc.out)
			}
		})
	}
}

// TestRedirect exercises the followRedirects configuration on an http.Do request
func TestRedirect(t *testing.T) {
	mux := http.NewServeMux()

	// In this test server, /a redirects to /b. /b serves "hello"
	mux.Handle("/a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusFound)
	}))
	mux.Handle("/b", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))

	server := httptest.NewUnstartedServer(mux)
	server.Start()
	t.Cleanup(server.Close)

	testCases := []struct {
		name            string
		path            string
		statusCode      int
		followRedirects *bool
		body            *string
	}{
		{
			name:       "/a silent on redirects",
			path:       "/a",
			statusCode: 200,
			body:       new("hello"),
		},
		{
			name:            "/a with explicit followRedirects: true",
			path:            "/a",
			statusCode:      200,
			followRedirects: new(true),
			body:            new("hello"),
		},
		{
			name:            "/a with explicit followRedirects: false",
			path:            "/a",
			statusCode:      302,
			followRedirects: new(false),
		},
		{
			name:       "/b silent on redirects",
			path:       "/b",
			statusCode: 200,
			body:       new("hello"),
		},
		{
			name:            "/b with explicit followRedirects: true",
			path:            "/b",
			statusCode:      200,
			followRedirects: new(true),
			body:            new("hello"),
		},
		{
			name:            "/b with explicit followRedirects: false",
			path:            "/b",
			statusCode:      200,
			followRedirects: new(true),
			body:            new("hello"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v3 := parse(t, "tool/http.Get", fmt.Sprintf(`
			{
				url: "%s%s"
			}`, server.URL, tc.path))

			if tc.followRedirects != nil {
				v3 = v3.FillPath(cue.ParsePath("followRedirects"), *tc.followRedirects)
			}

			resp, err := run(t, v3)
			if err != nil {
				t.Fatal(err)
			}

			// grab the response
			response := resp.(map[string]any)["response"].(map[string]any)

			if got := response["statusCode"]; got != tc.statusCode {
				t.Fatalf("status not as expected: wanted %d, got %d", got, tc.statusCode)
			}

			if tc.body != nil {
				want := *tc.body
				if got := response["body"]; got != want {
					t.Fatalf("body not as expected; wanted %q, got %q", got, want)
				}
			}
		})
	}
}

// TestRequestHeaders verifies that headers specified as either a single string
// or a list of strings are actually sent in the HTTP request.
func TestRequestHeaders(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	v := parse(t, "tool/http.Do", fmt.Sprintf(`{
		method: "GET"
		url: "%s"
		request: header: {
			"X-Single": "single-value"
			"X-List":   ["val-a", "val-b"]
		}
	}`, server.URL))

	_, err := run(t, v)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := gotHeaders.Get("X-Single"), "single-value"; got != want {
		t.Errorf("X-Single: got %q, want %q", got, want)
	}
	if got, want := gotHeaders.Values("X-List"), []string{"val-a", "val-b"}; !slices.Equal(got, want) {
		t.Errorf("X-List: got %v, want %v", got, want)
	}
}

func TestServeResponse(t *testing.T) {
	tests := []struct {
		name string
		expr string
		// wantErr, when not empty, is a substring of the expected error.
		// It also implies that nothing at all must be written to the response.
		wantErr  string
		wantCode int
		wantBody string
		// wantHeader holds the values expected for some response header keys.
		wantHeader http.Header
	}{{
		name:     "default status code",
		expr:     `{response: body: "ok"}`,
		wantCode: 200,
		wantBody: "ok",
	}, {
		name:     "explicit status code",
		expr:     `{response: {statusCode: 201, body: "ok"}}`,
		wantCode: 201,
		wantBody: "ok",
	}, {
		// The body is optional, so a response without one is not an error.
		name:     "no body",
		expr:     `{response: statusCode: 204}`,
		wantCode: 204,
	}, {
		// Every value of a multi-valued header is sent.
		name: "multi-valued header",
		expr: `{response: {
			header: "Set-Cookie": ["a=1", "b=2"]
			header: "X-Single":   "one"
			body: "ok"
		}}`,
		wantCode: 200,
		wantBody: "ok",
		wantHeader: http.Header{
			"Set-Cookie": {"a=1", "b=2"},
			"X-Single":   {"one"},
		},
	}, {
		// net/http panics when given a status code outside [100, 999].
		name:    "status code too low",
		expr:    `{response: {statusCode: 0, body: "ok"}}`,
		wantErr: "response status code 0 is not in the range [100, 999]",
	}, {
		name:    "status code too high",
		expr:    `{response: {statusCode: 1000, body: "ok"}}`,
		wantErr: "response status code 1000 is not in the range [100, 999]",
	}, {
		name:    "non-concrete status code",
		expr:    `{response: {statusCode: int, body: "ok"}}`,
		wantErr: "invalid response status code",
	}, {
		name:    "non-encodable body",
		expr:    `{response: {statusCode: 201, body: string}}`,
		wantErr: "cannot encode response",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c := &serveCmd{w: w}
			_, err := c.Run(&task.Context{Obj: build(t, tc.expr)})
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("got no error, want %q", tc.wantErr)
				}
				if got := err.Error(); !strings.Contains(got, tc.wantErr) {
					t.Fatalf("got error %q, want it to contain %q", got, tc.wantErr)
				}
				// A failure must leave the response untouched, so that the
				// caller can write a single error response of its own.
				if w.Body.Len() > 0 || w.Code != 200 {
					t.Fatalf("wrote a partial response: %d %q", w.Code, w.Body)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.wantCode {
				t.Errorf("got status code %d, want %d", w.Code, tc.wantCode)
			}
			if got := w.Body.String(); got != tc.wantBody {
				t.Errorf("got body %q, want %q", got, tc.wantBody)
			}
			for key, want := range tc.wantHeader {
				if got := w.Header().Values(key); !slices.Equal(got, want) {
					t.Errorf("header %s: got %q, want %q", key, got, want)
				}
			}
		})
	}
}

// TestTimeout verifies that the timeout field bounds how long a request
// may take, and that invalid timeouts are rejected.
func TestTimeout(t *testing.T) {
	// The handler never responds, until the client goes away or the test ends.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	v := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "%s", timeout: "10ms"}`, server.URL))
	_, err := run(t, v)
	if err == nil {
		t.Fatal("http call should have timed out")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v; want a deadline error", err)
	}

	// An invalid timeout is rejected before any request is made.
	for _, tc := range []struct{ timeout, want string }{
		{"nope", "invalid timeout"},
		{"0s", "timeout must be positive"},
		{"-1s", "timeout must be positive"},
	} {
		t.Run(tc.timeout, func(t *testing.T) {
			v := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "http://localhost", timeout: %q}`, tc.timeout))
			_, err := run(t, v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v; want an error containing %q", err, tc.want)
			}
		})
	}
}

// TestRequestContext verifies that a request is tied to the task context,
// so that cancelling the flow aborts a server which never responds.
func TestRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// The handler cancels the task context and then never responds,
	// until the client goes away or the test ends.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	v := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "%s"}`, server.URL))
	_, err := (*httpCmd).Run(nil, &task.Context{Context: ctx, Obj: v})
	if err == nil {
		t.Fatal("http call should have been cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v; want a cancellation error", err)
	}
}
