// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

// AddFolders gets called from Initialized, and from
// DidChangeWorkspaceFolders, to add the specified set of
// WorkspaceFolders to the session.
func (s *server) AddFolders(ctx context.Context, folders map[protocol.WorkspaceFolder]protocol.DocumentURI) error {
	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(FromInitialWorkspaceLoad), "Calculating diagnostics for initial workspace load...", nil, nil)
		defer work.EndAsync(ctx, "Done.")
	}

	folderErrs := make(map[protocol.DocumentURI]error)

	for folder, uri := range folders {
		wf, err := s.workspace.EnsureFolder(uri, folder.Name)
		if err != nil {
			folderErrs[uri] = err
			continue
		}
		options, err := s.fetchFolderOptions(ctx, uri)
		if err != nil {
			folderErrs[uri] = err
			continue
		}
		wf.UpdateOptions(options)
	}

	if len(folderErrs) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString("Error loading workspace folders:\n")
		for uri, err := range folderErrs {
			errMsg.WriteString(fmt.Sprintf("failed to load view for %s: %v\n", uri, err))
		}
		return errors.New(errMsg.String())
	}

	// Register for file watching notifications, if they are supported.
	return s.UpdateWatchedFiles(ctx)
}

func (s *server) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	for _, folder := range params.Event.Removed {
		dir, err := protocol.ParseDocumentURI(folder.URI)
		if err != nil {
			return fmt.Errorf("invalid folder %q: %v", folder.URI, err)
		}
		s.workspace.RemoveFolder(dir)
	}
	validFolders, err := validateWorkspaceFolders(params.Event.Added)
	if err == nil {
		err = s.AddFolders(ctx, validFolders)
	}
	// DidChangeWorkspaceFolders is a notification, so if there's an
	// error, we show it rather than return it.
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
	}
	return nil
}

func (s *server) DidChangeConfiguration(ctx context.Context, _ *protocol.DidChangeConfigurationParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChangeConfiguration")
	defer done()

	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(FromDidChangeConfiguration), "Calculating diagnostics...", nil, nil)
		defer work.EndAsync(ctx, "Done.")
	}

	// Apply any changes to the session-level settings.
	options, err := s.fetchFolderOptions(ctx, "")
	// DidChangeConfiguration is a notification, so if there's an
	// error, we show it rather than return it.
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
		return nil
	}
	s.SetOptions(options)

	fetchFolderOptions := func(dir protocol.DocumentURI) (*settings.Options, error) {
		return s.fetchFolderOptions(ctx, dir)
	}
	err = s.workspace.UpdateFolderOptions(fetchFolderOptions)
	// DidChangeConfiguration is a notification, so if there's an
	// error, we show it rather than return it.
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
		return nil
	}
	return nil
}
