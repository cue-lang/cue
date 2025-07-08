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

package server

import (
	"context"
	"errors"
	"fmt"
	"path"

	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/golangorgx/gopls/progress"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
)

// Initialize is a request from the client to initialize the
// workspace. It gets a response. Once the response is sent, the
// client needs to send an Initialized async message before any work
// starts.
//
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#initialize
func (s *Server) Initialize(ctx context.Context, params *protocol.ParamInitialize) (*protocol.InitializeResult, error) {
	ctx, done := event.Start(ctx, "lsp.Server.initialize")
	defer done()

	if s.state != serverCreated {
		return nil, fmt.Errorf("%w: initialize called while server in %v state", jsonrpc2.ErrInvalidRequest, s.state)
	}
	s.state = serverInitializing

	// TODO(myitcv): need to better understand events, and the mechanisms that
	// hang off that. At least for now we know that the integration expectation
	// setup relies on the progress messaging titles to track things being done.
	s.progress = progress.NewTracker(s.client, params.Capabilities.Window.WorkDoneProgress)

	// Clone the existing (default?) options, and update from the params.
	options := s.Options().Clone()

	// TODO(myitcv): review and slim down option handling code
	if err := s.HandleOptionResults(ctx, settings.SetOptions(options, params.InitializationOptions)); err != nil {
		return nil, err
	}
	options.ForClientCapabilities(params.ClientInfo, params.Capabilities)
	s.SetOptions(options)

	folders := params.WorkspaceFolders
	if len(folders) == 0 && params.RootURI != "" {
		folders = []protocol.WorkspaceFolder{{
			URI:  string(params.RootURI),
			Name: path.Base(params.RootURI.Path()),
		}}
	}

	for _, folder := range folders {
		if folder.URI == "" {
			return nil, fmt.Errorf("empty WorkspaceFolder.URI")
		}
		if _, err := protocol.ParseDocumentURI(folder.URI); err != nil {
			return nil, fmt.Errorf("invalid WorkspaceFolder.URI: %v", err)
		}
	}
	if len(folders) == 0 {
		return nil, errors.New("no WorkspaceFolders")
	}
	s.EventuallyUseWorkspaceFolders(folders)

	return &protocol.InitializeResult{
		ServerInfo: &protocol.ServerInfo{
			Name: "cuelsp",
			// NB, this is not strictly the same as "cue version" because
			// "cue version" allows the version to be set via ldflags.
			Version: cueversion.ModuleVersion(),
		},

		Capabilities: protocol.ServerCapabilities{
			DocumentFormattingProvider: &protocol.Or_ServerCapabilities_documentFormattingProvider{Value: true},
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				Change:    protocol.Incremental,
				OpenClose: true,
				Save: &protocol.SaveOptions{
					IncludeText: false,
				},
			},

			Workspace: &protocol.WorkspaceOptions{
				WorkspaceFolders: &protocol.WorkspaceFolders5Gn{
					Supported:           true,
					ChangeNotifications: "workspace/didChangeWorkspaceFolders",
				},
			},
		},
	}, nil
}

// This is the handler for the async message from the client. The
// client should send this only after it's received our
// InitializeResult message.
//
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#initialized
func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.initialized")
	defer done()

	if s.state != serverInitializing {
		return fmt.Errorf("%w: initialized called while server in %v state", jsonrpc2.ErrInvalidRequest, s.state)
	}
	s.state = serverInitialized

	s.MaybeShowPendingMessages(ctx)

	options := s.Options()
	if options.ConfigurationSupported && options.DynamicConfigurationSupported {
		err := s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
			Registrations: []protocol.Registration{{
				ID:     "workspace/didChangeConfiguration",
				Method: "workspace/didChangeConfiguration",
			}},
		})
		if err != nil {
			return err
		}
	}

	s.LaunchWorkspace()
	err := s.MaybeUseWorkspaceFolders(ctx)
	// Initialized is a notification, so if there's an error, we show it rather than return it
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
	}
	return nil
}
