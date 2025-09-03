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
	"net/http"
	"regexp"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/task"
)

var (
	muxers = map[string]*http.ServeMux{}
)

func newServeCmd(v cue.Value) (task.Runner, error) {
	return &listenCmd{}, nil
}

type listenCmd struct {
	w    http.ResponseWriter
	body cue.Path
}

var m sync.Mutex

var (
	listenPath = cue.ParsePath("listenAddr")
	pathPath   = cue.ParsePath("routing.path")
	methodPath = cue.ParsePath("routing.method")

	reqMethodPath = cue.ParsePath("request.method")
	urlPath       = cue.ParsePath("request.url")
	formPath      = cue.ParsePath("request.form")
	headerPath    = cue.ParsePath("request.header")
	varsPath      = cue.ParsePath("request.pathValues")
	reqBodyPath   = cue.ParsePath("request.body")

	respBodyPath = cue.ParsePath("response.body")
	responsePath = cue.ParsePath("response")
)

func (c *listenCmd) Run(ctx *task.Context) (res interface{}, err error) {
	v := ctx.Obj
	addr, err := v.LookupPath(listenPath).String()

	if err != nil {
		return nil, err
	}

	m.Lock()
	mux := muxers[addr]
	if mux == nil {
		mux = http.NewServeMux()
		muxers[addr] = mux

		log.Printf("listening on %v\n", addr)

		// TODO: use Server at some point.
		go http.ListenAndServe(addr, mux)
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
		v := v
		err := req.ParseForm()
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot parse form: %v", err), http.StatusBadRequest)
			return
		}
		v = v.FillPath(reqMethodPath, req.Method)
		v = v.FillPath(urlPath, req.URL.String())
		v = v.FillPath(formPath, req.Form)
		v = v.FillPath(headerPath, req.Header)

		for _, variable := range vars {
			if s := req.PathValue(variable); s != "" {
				p := varsPath.Append(cue.Str(variable))
				v = v.FillPath(p, s)
			}
		}

		data, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot read body: %v", err), http.StatusBadRequest)
			return
		}
		v = v.FillPath(reqBodyPath, data)

		handle := &serveCmd{w: w}

		err = ctx.ForkRunLoop(req.Context(), path, v, handle)
		if err != nil {
			details := errors.Details(err, nil)
			http.Error(w, fmt.Sprintf("error handling request: %v", details), http.StatusConflict)
			return
		}
	})

	ctx.BackgroundTask()
	return nil, nil
}

// variableRegex is a regular expression to find all instances of {variableName} in a path.
// It captures the content inside the braces.
var variableRegex = regexp.MustCompile(`\{([^{}]+)\}`)

// extractPathVariables parses a URL pattern string and returns a slice of the variable names.
// For example, given "/users/{userID}/posts/{postID}", it returns ["userID", "postID"].
func extractPathVariables(pattern string) []string {
	matches := variableRegex.FindAllStringSubmatch(pattern, -1)
	if matches == nil {
		return nil
	}

	variables := make([]string, len(matches))
	for i, match := range matches {
		// The first submatch (index 1) is the captured group, which is the variable name.
		variables[i] = match[1]
	}
	return variables
}

type serveCmd struct {
	w    http.ResponseWriter
	body cue.Path
}

func (c *serveCmd) Run(ctx *task.Context) (res interface{}, err error) {
	v := ctx.Obj

	response := v.LookupPath(responsePath)
	headers, err := parseHeaders(response, "header")
	// TODO: error handling
	trailers, err := parseHeaders(response, "trailer")
	// TODO: error handling

	v = v.LookupPath(respBodyPath)

	b, err := v.Bytes()
	if err != nil {
		http.Error(c.w, fmt.Sprintf("cannot encode response: %v", err), http.StatusBadRequest)
	}

	for k, v := range headers {
		for _, v := range v {
			c.w.Header().Set(k, v)
		}
	}

	c.w.Write(b)

	for k, v := range trailers {
		for _, v := range v {
			c.w.Header().Set(k, v)
		}
	}

	return nil, nil
}
