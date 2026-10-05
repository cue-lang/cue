// Copyright 2026 CUE Authors
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

package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/lsp/cache"
)

// testClient is a client which accepts every message the server
// sends in these tests, other than those it is configured to fail.
type testClient struct {
	protocol.ClientCloser

	failRegisterCapability bool
	failConfiguration      bool
}

func (c *testClient) RegisterCapability(context.Context, *protocol.RegistrationParams) error {
	if c.failRegisterCapability {
		return errors.New("registerCapability rejected")
	}
	return nil
}

func (c *testClient) Configuration(_ context.Context, params *protocol.ParamConfiguration) ([]protocol.LSPAny, error) {
	if c.failConfiguration {
		return nil, errors.New("configuration rejected")
	}
	return make([]protocol.LSPAny, len(params.Items)), nil
}

func (c *testClient) ShowMessage(context.Context, *protocol.ShowMessageParams) error { return nil }
func (c *testClient) LogMessage(context.Context, *protocol.LogMessageParams) error   { return nil }
func (c *testClient) PublishDiagnostics(context.Context, *protocol.PublishDiagnosticsParams) error {
	return nil
}

// send delivers one message from the client to the handler, and
// describes how the server responds: "ok", "error" with a JSON-RPC
// error code, or "panic".
func send(handler jsonrpc2.Handler, req jsonrpc2.Request) (outcome string) {
	defer func() {
		if r := recover(); r != nil {
			outcome = "panic"
		}
	}()
	reply := func(_ context.Context, _ any, err error) error {
		var wireErr *jsonrpc2.WireError
		switch {
		case err == nil:
			outcome = "ok"
		case errors.As(err, &wireErr):
			outcome = fmt.Sprintf("error %d", wireErr.Code)
		default:
			outcome = fmt.Sprintf("error: %v", err)
		}
		return nil
	}
	if err := handler(context.Background(), reply, req); err != nil {
		return fmt.Sprintf("handler error: %v", err)
	}
	return outcome
}

// TestLifecycle checks how the server responds to sequences of
// messages, particularly those which arrive outside of the
// initialized state, or when the client fails a request made by the
// server.
func TestLifecycle(t *testing.T) {
	call := func(method string, params any) jsonrpc2.Request {
		req, err := jsonrpc2.NewCall(jsonrpc2.NewIntID(1), method, params)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	notify := func(method string, params any) jsonrpc2.Request {
		req, err := jsonrpc2.NewNotification(method, params)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}

	root := protocol.URIFromPath(t.TempDir())
	fileURI := root + "/a.cue"

	var initParams protocol.ParamInitialize
	initParams.RootURI = root
	initParams.Capabilities.Workspace.Configuration = true
	initParams.Capabilities.Workspace.DidChangeConfiguration.DynamicRegistration = true

	initialize := call("initialize", &initParams)
	initialized := notify("initialized", &protocol.InitializedParams{})
	shutdown := call("shutdown", nil)
	hover := call("textDocument/hover", &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: fileURI},
		},
	})
	didOpen := notify("textDocument/didOpen", &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: fileURI, Text: "x: 1\n"},
	})
	progressCancel := notify("window/workDoneProgress/cancel", &protocol.WorkDoneProgressCancelParams{})

	type step struct {
		req  jsonrpc2.Request
		want string
	}
	tests := []struct {
		name   string
		client testClient
		steps  []step
	}{{
		name: "RequestBeforeInitialize",
		steps: []step{
			{hover, "error -32002"},
			{initialize, "ok"},
		},
	}, {
		name: "NotificationBeforeInitialize",
		steps: []step{
			{progressCancel, "ok"},
			{didOpen, "ok"},
			{initialize, "ok"},
		},
	}, {
		name: "RequestBeforeInitialized",
		steps: []step{
			{initialize, "ok"},
			{hover, "error -32002"},
			{didOpen, "ok"},
			{initialized, "ok"},
			{hover, "ok"},
		},
	}, {
		name: "Initialized",
		steps: []step{
			{initialize, "ok"},
			{initialized, "ok"},
			{didOpen, "ok"},
			{hover, "ok"},
			{initialize, "error -32600"},
		},
	}, {
		name:   "RegisterCapabilityFails",
		client: testClient{failRegisterCapability: true},
		steps: []step{
			{initialize, "ok"},
			{initialized, "ok"},
			{didOpen, "ok"},
			{hover, "ok"},
		},
	}, {
		name: "RequestAfterShutdown",
		steps: []step{
			{initialize, "ok"},
			{initialized, "ok"},
			{shutdown, "ok"},
			{hover, "error -32600"},
			{didOpen, "ok"},
			{shutdown, "ok"},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := cache.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			s := New(c, &test.client, settings.DefaultOptions())
			// Nothing waits for the work which the server defers.
			s.SetEnqueuer(func(func()) {})
			handler := s.Handler(jsonrpc2.MethodNotFound)

			for i, step := range test.steps {
				if got := send(handler, step.req); got != step.want {
					t.Errorf("step %d, %s: got %q, want %q", i, step.req.Method(), got, step.want)
				}
			}
		})
	}
}
