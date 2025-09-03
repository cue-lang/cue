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
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cuelang.org/go/cue"
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

	x, err := parser.ParseExpr("test", expr)
	if err != nil {
		t.Fatal(err)
	}
	v := internal.NewContext().BuildExpr(x)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	return value.UnifyBuiltin(v, kind)
}

func TestTLS(t *testing.T) {
	s := newTLSServer()
	t.Cleanup(s.Close)

	v1 := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "%s"}`, s.URL))
	_, err := (*httpCmd).Run(nil, &task.Context{Obj: v1})
	if err == nil {
		t.Fatal("http call should have failed")
	}

	v2 := parse(t, "tool/http.Get", fmt.Sprintf(`{url: "%s", tls: verify: false}`, s.URL))
	_, err = (*httpCmd).Run(nil, &task.Context{Obj: v2})
	if err != nil {
		t.Fatal(err)
	}

	publicKeyBlock := pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: s.Certificate().Raw,
	}
	publicKeyPem := pem.EncodeToMemory(&publicKeyBlock)

	v3 := parse(t, "tool/http.Get", fmt.Sprintf(`
	{
		url: "%s"
		tls: caCert: '''
%s
'''
	}`, s.URL, publicKeyPem))

	_, err = (*httpCmd).Run(nil, &task.Context{Obj: v3})
	if err != nil {
		t.Fatal(err)
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
			body:       ref("hello"),
		},
		{
			name:            "/a with explicit followRedirects: true",
			path:            "/a",
			statusCode:      200,
			followRedirects: ref(true),
			body:            ref("hello"),
		},
		{
			name:            "/a with explicit followRedirects: false",
			path:            "/a",
			statusCode:      302,
			followRedirects: ref(false),
		},
		{
			name:       "/b silent on redirects",
			path:       "/b",
			statusCode: 200,
			body:       ref("hello"),
		},
		{
			name:            "/b with explicit followRedirects: true",
			path:            "/b",
			statusCode:      200,
			followRedirects: ref(true),
			body:            ref("hello"),
		},
		{
			name:            "/b with explicit followRedirects: false",
			path:            "/b",
			statusCode:      200,
			followRedirects: ref(true),
			body:            ref("hello"),
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

			resp, err := (*httpCmd).Run(nil, &task.Context{Obj: v3})
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

func ref[T any](v T) *T {
	return &v
}
