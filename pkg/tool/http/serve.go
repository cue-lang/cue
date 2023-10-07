package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/task"
)

type serveCmd struct{}

func newServeCmd(v cue.Value) (task.Runner, error) {
	return &serveCmd{}, nil
}

func (c *serveCmd) Run(ctx *task.Context) (res interface{}, err error) {
	log.Printf("running http server")
	addr, err := ctx.Obj.Lookup("listenAddr").String()
	if err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	fmt.Printf("listening on %v\n", l.Addr())

	err = http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.serve(ctx, w, req)
	}))
	return struct{}{}, err
}

func (c *serveCmd) serve(ctx *task.Context, w http.ResponseWriter, req *http.Request) {
	v := ctx.Obj
	if req.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}

	v = v.FillPath(cue.MakePath(cue.Str("request"), cue.Str("query")), req.Form)
	v = v.FillPath(cue.MakePath(cue.Str("request"), cue.Str("header")), req.Header)
	data, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot read body: %v", err), http.StatusBadRequest)
		return
	}
	var body json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		http.Error(w, fmt.Sprintf("cannot decode body: %v", err), http.StatusBadRequest)
		return
	}
	v = v.FillPath(cue.MakePath(cue.Str("request"), cue.Str("body")), body)
	v = v.LookupPath(cue.MakePath(cue.Str("response")))
	respBody, err := v.MarshalJSON()
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot encode response: %v", err), http.StatusBadRequest)
		return
	}
	w.Write(respBody)
}
