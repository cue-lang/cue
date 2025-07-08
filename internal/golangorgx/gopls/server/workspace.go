// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

func (s *server) LaunchWorkspace() {
	// If the workspace gains the need to shutdown then if s.workspace
	// != nil here, we'll need to shutdown the old workspace.
	s.workspace = cache.NewWorkspace(s.cache, s.DebugLog)
}

// AddFolders gets called from Initialized, and from
// DidChangeWorkspaceFolders, to add the specified list of
// WorkspaceFolders to the session.
//
// Precondition: each folder in folders must have a valid URI.
func (s *server) AddFolders(ctx context.Context, folders []protocol.WorkspaceFolder) error {
	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(FromInitialWorkspaceLoad), "Calculating diagnostics for initial workspace load...", nil, nil)
		defer func() {
			go func() {
				work.End(ctx, "Done.")
			}()
		}()
	}

	fetchFolderOptions := func(dir protocol.DocumentURI) (*settings.Options, error) {
		return s.FetchFolderOptions(ctx, dir)
	}
	folderErrs := make(map[protocol.DocumentURI]error)

	for _, folder := range folders {
		uri, err := protocol.ParseDocumentURI(folder.URI)
		if err != nil {
			// Precondition on folders having valid URI violated
			panic(err)
		}
		err = s.workspace.EnsureFolder(fetchFolderOptions, uri, folder.Name)
		if err != nil {
			folderErrs[uri] = err
			continue
		}
	}

	if len(folderErrs) > 0 {
		errMsg := "Error loading workspace folders:\n"
		for uri, err := range folderErrs {
			errMsg += fmt.Sprintf("failed to load view for %s: %v\n", uri, err)
		}
		return errors.New(errMsg)
	}

	// Register for file watching notifications, if they are supported.
	if err := s.UpdateWatchedFiles(ctx); err != nil {
		event.Error(ctx, "failed to register for file watching notifications", err)
	}

	return nil
}

func (s *server) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	for _, folder := range params.Event.Removed {
		dir, err := protocol.ParseDocumentURI(folder.URI)
		if err != nil {
			return fmt.Errorf("invalid folder %q: %v", folder.URI, err)
		}
		s.workspace.RemoveFolder(dir)
	}
	err := s.AddFolders(ctx, params.Event.Added)
	// DidChangeWorkspaceFoldersParams is a notification, so if there's an error, we show it rather than return it
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
	}
	return nil
}

func (s *server) DidChangeConfiguration(ctx context.Context, _ *protocol.DidChangeConfigurationParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChangeConfiguration")
	defer done()

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()
	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(FromDidChangeConfiguration), "Calculating diagnostics...", nil, nil)
		go func() {
			wg.Wait()
			work.End(ctx, "Done.")
		}()
	}

	// Apply any changes to the session-level settings.
	options, err := s.FetchFolderOptions(ctx, "")
	if err != nil {
		return err
	}
	s.SetOptions(options)

	fetchFolderOptions := func(dir protocol.DocumentURI) (*settings.Options, error) {
		return s.FetchFolderOptions(ctx, dir)
	}
	return s.workspace.UpdateFolderOptions(fetchFolderOptions)
}
