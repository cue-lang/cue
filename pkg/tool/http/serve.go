// Copyright 2023 CUE Authors
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
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/task"
)

var (
	muxers = map[string]*http.ServeMux{}
	// listeners stores net.Listener for each address to enable error checking
	// before starting the serve goroutine.
	listeners = map[string]net.Listener{}
)

func newServeCmd(v cue.Value) (task.Runner, error) {
	return &listenCmd{}, nil
}

type listenCmd struct{}

// IsService indicates that http.Serve acts as a service.
// Other tasks can reference request fields (which are filled
// at runtime) without creating a dependency cycle error.
func (c *listenCmd) IsService() bool {
	return true
}

var m sync.Mutex

var (
	listenPath = cue.ParsePath("listenAddr")
	pathPath   = cue.ParsePath("routing.path")
	methodPath = cue.ParsePath("routing.method")

	requestPath  = cue.ParsePath("request")
	responsePath = cue.ParsePath("response")

	// Relative to responsePath.
	respBodyPath       = cue.ParsePath("body")
	respStatusCodePath = cue.ParsePath("statusCode")
)

// httpRequest represents the request data to fill into the CUE value.
type httpRequest struct {
	Method     string              `json:"method"`
	URL        string              `json:"url"`
	Body       []byte              `json:"body"`
	Form       map[string][]string `json:"form"`
	Header     map[string][]string `json:"header"`
	PathValues map[string]string   `json:"pathValues"`
}

func (c *listenCmd) Run(ctx *task.Context) (res any, err error) {
	v := ctx.Obj
	addr, err := v.LookupPath(listenPath).String()

	if err != nil {
		return nil, err
	}

	m.Lock()
	mux := muxers[addr]
	if mux == nil {
		// Create listener first to catch errors (e.g., port unavailable)
		// before starting the serve goroutine.
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			m.Unlock()
			return nil, fmt.Errorf("cannot listen on %s: %w", addr, err)
		}

		mux = http.NewServeMux()
		muxers[addr] = mux
		listeners[addr] = ln

		log.Printf("listening on %v\n", addr)

		// Only accept requests once all ready Serve tasks have run, as otherwise
		// a request could arrive before its handler is registered.
		// Until then, connections wait in the listener's backlog.
		// TODO: use Server at some point.
		ctx.BackgroundTask(func() { http.Serve(ln, mux) })
	}
	m.Unlock()

	url := "/"
	if p := v.LookupPath(pathPath); p.Exists() {
		url, err = p.String()
		if err != nil {
			return nil, err
		}
	}

	vars := extractPathVariables(url)

	if m := v.LookupPath(methodPath); m.Exists() {
		method, err := m.String()
		if err != nil {
			return nil, err
		}
		url = fmt.Sprintf("%s %s", method, url)
	}

	path := v.Path()

	log.Printf("adding handler for %v\n", url)
	mux.HandleFunc(url, func(w http.ResponseWriter, req *http.Request) {
		err := req.ParseForm()
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot parse form: %v", err), http.StatusBadRequest)
			return
		}

		data, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot read body: %v", err), http.StatusBadRequest)
			return
		}

		pathValues := make(map[string]string)
		for _, variable := range vars {
			if s := req.PathValue(variable); s != "" {
				pathValues[variable] = s
			}
		}

		reqValue := v.FillPath(requestPath, httpRequest{
			Method:     req.Method,
			URL:        req.URL.String(),
			Body:       data,
			Form:       req.Form,
			Header:     req.Header,
			PathValues: pathValues,
		})

		handle := &serveCmd{w: w}

		c := req.Context()
		controller := ctx.ForkRunLoop(c, path, reqValue, handle)

		if err := controller.Run(c); err != nil {
			cwd, _ := os.Getwd()
			details := errors.Details(err, &errors.Config{Cwd: cwd, ToSlash: true})
			// TODO: return JSON-formatted error response for consistency with
			// successful responses (e.g. {"error": "...", "details": "..."}).
			http.Error(w, fmt.Sprintf("error handling request: %v", details), http.StatusInternalServerError)
			return
		}
	})

	return nil, nil
}

// variableRegex is a regular expression to find all instances of {variableName} in a path.
// It captures the content inside the braces.
var variableRegex = regexp.MustCompile(`\{([^{}\.]+)(\.\.\.)?\}`)

// extractPathVariables parses a URL pattern string and returns a slice of the variable names.
// For example, given "/users/{userID}/posts/{postID}", it returns ["userID", "postID"].
// The special pattern {$} (exact path match in http.ServeMux) is excluded.
func extractPathVariables(pattern string) []string {
	matches := variableRegex.FindAllStringSubmatch(pattern, -1)
	if matches == nil {
		return nil
	}

	var variables []string
	for _, match := range matches {
		// The first submatch (index 1) is the captured group, which is the variable name.
		// Skip {$} which is a special http.ServeMux pattern for exact path matching.
		if name := match[1]; name != "$" {
			variables = append(variables, name)
		}
	}
	return variables
}

type serveCmd struct {
	w http.ResponseWriter
}

// IsService indicates that http.Serve should not be reported as part
// of task cycles during request handling via ForkRunLoop.
func (c *serveCmd) IsService() bool {
	return true
}

// Run builds the entire response before writing any of it, so that a failure
// leaves the response untouched. The caller, the handler set up by
// [listenCmd.Run], then turns the returned error into the sole error response.
func (c *serveCmd) Run(ctx *task.Context) (res any, err error) {
	v := ctx.Obj

	response := v.LookupPath(responsePath)
	headers, err := parseHeaders(response, "header")
	if err != nil {
		return nil, errors.Wrapf(err, response.Pos(), "cannot parse headers")
	}
	trailers, err := parseHeaders(response, "trailer")
	if err != nil {
		return nil, errors.Wrapf(err, response.Pos(), "cannot parse trailers")
	}

	// net/http panics when writing a status code outside the range of the
	// three-digit HTTP codes. [task.Context.TaskFunc] unifies every task value
	// with its builtin schema, and Serve bounds statusCode to that range, so
	// the check below only guards against that unification being bypassed.
	statusCode := int64(http.StatusOK)
	if sc := response.LookupPath(respStatusCodePath); sc.Exists() {
		statusCode, err = sc.Int64()
		if err != nil {
			return nil, errors.Wrapf(err, sc.Pos(), "invalid response status code")
		}
		if statusCode < 100 || statusCode > 999 {
			return nil, errors.Newf(sc.Pos(),
				"response status code %d is not in the range [100, 999]", statusCode)
		}
	}

	// The body is optional; an absent one results in an empty response.
	var b []byte
	if body := response.LookupPath(respBodyPath); body.Exists() {
		b, err = body.Bytes()
		if err != nil {
			return nil, errors.Wrapf(err, body.Pos(), "cannot encode response")
		}
	}

	for k, vs := range headers {
		for _, v := range vs {
			c.w.Header().Set(k, v)
		}
	}

	for k, vs := range trailers {
		for _, v := range vs {
			c.w.Header().Set(k, v)
		}
	}

	c.w.WriteHeader(int(statusCode))
	c.w.Write(b)

	return nil, nil
}
