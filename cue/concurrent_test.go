// Copyright 2025 CUE Authors
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

package cue_test

import (
	"sync"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// TestConcurrentValueAccess tests that cue.Value read operations are race-free
// when called concurrently on a shared finalized value.
// This is a regression test for https://github.com/cue-lang/cue/issues/2733
func TestConcurrentValueAccess(t *testing.T) {
	ctx := cuecontext.New()

	// Create a value similar to what http.Serve would use
	v := ctx.CompileString(`
		listenAddr: "localhost:0"
		routing: {
			path: "/test/{id}"
			method: "GET"
		}
		request: {
			method: string
			url: string
			body: bytes
			form: [string]: [...string]
			header: [string]: [...string]
			pathValues: [string]: string
		}
		response: {
			body: *"default" | bytes
			statusCode: *200 | int
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	listenPath := cue.ParsePath("listenAddr")
	pathPath := cue.ParsePath("routing.path")
	requestPath := cue.ParsePath("request")
	respBodyPath := cue.ParsePath("response.body")

	// Simulate concurrent request handling by calling cue.Value methods
	// that would be used in the serve handler.
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				// Operations that http.Serve uses:
				// 1. LookupPath + String
				addr, err := v.LookupPath(listenPath).String()
				if err != nil {
					t.Error(err)
					return
				}
				if addr != "localhost:0" {
					t.Errorf("unexpected addr: %s", addr)
					return
				}

				// 2. LookupPath + Exists + String
				if p := v.LookupPath(pathPath); p.Exists() {
					s, err := p.String()
					if err != nil {
						t.Error(err)
						return
					}
					if s != "/test/{id}" {
						t.Errorf("unexpected path: %s", s)
						return
					}
				}

				// 3. Path()
				_ = v.Path()

				// 4. FillPath (creates new value)
				filled := v.FillPath(requestPath, map[string]any{
					"method": "GET",
					"url":    "/test/123",
					"body":   []byte("test body"),
				})
				if err := filled.Err(); err != nil {
					t.Error(err)
					return
				}

				// 5. LookupPath on filled value + Bytes
				body := filled.LookupPath(respBodyPath)
				if body.Exists() {
					_, err := body.Bytes()
					if err != nil {
						// Expected - default is string not bytes
					}
				}

				// 6. Default() - known race point
				resp := v.LookupPath(respBodyPath)
				if d, ok := resp.Default(); ok {
					_, _ = d.String()
				}
			}
		}()
	}
	wg.Wait()
}
