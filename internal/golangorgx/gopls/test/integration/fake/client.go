// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"context"
	"encoding/json"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake/glob"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
)

// ClientHooks are a set of optional hooks called during handling of
// the corresponding client method (see protocol.Client for the
// LSP server-to-client RPCs) in order to make test expectations
// awaitable.
type ClientHooks struct {
	OnLogMessage             func(context.Context, *protocol.LogMessageParams) error
	OnDiagnostics            func(context.Context, *protocol.PublishDiagnosticsParams) error
	OnWorkDoneProgressCreate func(context.Context, *protocol.WorkDoneProgressCreateParams) error
	OnProgress               func(context.Context, *protocol.ProgressParams) error
	OnShowMessage            func(context.Context, *protocol.ShowMessageParams) error
}

// Client is an implementation of the [protocol.Client] interface
// based on the test's fake [Editor]. It mostly delegates
// functionality to hooks that can be configured by tests.
type Client struct {
	editor *Editor
	hooks  ClientHooks
}

func (c *Client) CodeLensRefresh(context.Context) error { return nil }

func (c *Client) InlayHintRefresh(context.Context) error { return nil }

func (c *Client) DiagnosticRefresh(context.Context) error { return nil }

func (c *Client) FoldingRangeRefresh(context.Context) error { return nil }

func (c *Client) InlineValueRefresh(context.Context) error { return nil }

func (c *Client) SemanticTokensRefresh(context.Context) error { return nil }

func (c *Client) LogTrace(context.Context, *protocol.LogTraceParams) error { return nil }

func (c *Client) ShowMessage(ctx context.Context, params *protocol.ShowMessageParams) error {
	if c.hooks.OnShowMessage != nil {
		return c.hooks.OnShowMessage(ctx, params)
	}
	return nil
}

// ShowMessageRequest, [Client.ShowDocument] and [Client.ApplyEdit] return an
// error, as the server never calls them.
func (c *Client) ShowMessageRequest(context.Context, *protocol.ShowMessageRequestParams) (*protocol.MessageActionItem, error) {
	return nil, notImplemented("window/showMessageRequest")
}

func (c *Client) ShowDocument(context.Context, *protocol.ShowDocumentParams) (*protocol.ShowDocumentResult, error) {
	return nil, notImplemented("window/showDocument")
}

func (c *Client) ApplyEdit(context.Context, *protocol.ApplyWorkspaceEditParams) (*protocol.ApplyWorkspaceEditResult, error) {
	return nil, notImplemented("workspace/applyEdit")
}

func notImplemented(method string) error {
	return fmt.Errorf("%w: %q not implemented by the fake client", jsonrpc2.ErrMethodNotFound, method)
}

func (c *Client) LogMessage(ctx context.Context, params *protocol.LogMessageParams) error {
	if c.hooks.OnLogMessage != nil {
		return c.hooks.OnLogMessage(ctx, params)
	}
	return nil
}

func (c *Client) Event(ctx context.Context, event *interface{}) error {
	return nil
}

func (c *Client) PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error {
	if c.hooks.OnDiagnostics != nil {
		return c.hooks.OnDiagnostics(ctx, params)
	}
	return nil
}

func (c *Client) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	return []protocol.WorkspaceFolder{}, nil
}

func (c *Client) Configuration(_ context.Context, p *protocol.ParamConfiguration) ([]interface{}, error) {
	results := make([]interface{}, len(p.Items))
	for i, item := range p.Items {
		if item.ScopeURI != nil && *item.ScopeURI == "" {
			return nil, fmt.Errorf(`malformed ScopeURI ""`)
		}
		if item.Section == settings.ConfigSection {
			config := c.editor.Config()
			results[i] = makeSettings(c.editor.sandbox, config, item.ScopeURI)
		}
	}
	return results, nil
}

func (c *Client) RegisterCapability(ctx context.Context, params *protocol.RegistrationParams) error {
	// Update file watching patterns.
	//
	// TODO(rfindley): We could verify more here, like verify that the
	// registration ID is distinct, and that the capability is not currently
	// registered.
	for _, registration := range params.Registrations {
		if registration.Method == "workspace/didChangeWatchedFiles" {
			// Marshal and unmarshal to interpret RegisterOptions as
			// DidChangeWatchedFilesRegistrationOptions.
			raw, err := json.Marshal(registration.RegisterOptions)
			if err != nil {
				return fmt.Errorf("marshaling registration options: %v", err)
			}
			var opts protocol.DidChangeWatchedFilesRegistrationOptions
			if err := json.Unmarshal(raw, &opts); err != nil {
				return fmt.Errorf("unmarshaling registration options: %v", err)
			}
			var globs []*glob.Glob
			for _, watcher := range opts.Watchers {
				globPattern, _ := watcher.GlobPattern.Value.(protocol.Pattern)
				// TODO(rfindley): honor the watch kind.
				g, err := glob.Parse(globPattern)
				if err != nil {
					return fmt.Errorf("error parsing glob pattern %q: %v", watcher.GlobPattern, err)
				}
				globs = append(globs, g)
			}
			c.editor.mu.Lock()
			c.editor.watchPatterns = globs
			c.editor.mu.Unlock()
		}
	}
	return nil
}

func (c *Client) UnregisterCapability(context.Context, *protocol.UnregistrationParams) error {
	return nil
}

func (c *Client) Progress(ctx context.Context, params *protocol.ProgressParams) error {
	if c.hooks.OnProgress != nil {
		return c.hooks.OnProgress(ctx, params)
	}
	return nil
}

func (c *Client) WorkDoneProgressCreate(ctx context.Context, params *protocol.WorkDoneProgressCreateParams) error {
	if c.hooks.OnWorkDoneProgressCreate != nil {
		return c.hooks.OnWorkDoneProgressCreate(ctx, params)
	}
	return nil
}
