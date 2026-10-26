// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// ModificationSource identifies the origin of a change.
type ModificationSource int

//go:generate go tool stringer -type=ModificationSource -linecomment

const (
	// FromDidOpen is from a didOpen notification.
	FromDidOpen = ModificationSource(iota) // opened files

	// FromDidChange is from a didChange notification.
	FromDidChange // changed files

	// FromDidChangeWatchedFiles is from didChangeWatchedFiles notification.
	FromDidChangeWatchedFiles // files changed on disk

	// FromDidSave is from a didSave notification.
	FromDidSave // saved files

	// FromDidClose is from a didClose notification.
	FromDidClose // close files

	// FromDidChangeConfiguration is from a didChangeConfiguration notification.
	FromDidChangeConfiguration // changed configuration

	// FromInitialWorkspaceLoad refers to the loading of all packages in the
	// workspace when the view is first created.
	FromInitialWorkspaceLoad // initial workspace load
)

var openRange = &protocol.Range{
	Start: protocol.Position{Line: 0, Character: 0},
	End:   protocol.Position{Line: 0, Character: 0},
}

func (s *server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	// TODO(myitcv): we need to report an error/problem/something in case the user opens a file
	// that is not part of the CUE module. For now we will not support that, because it massively
	// opens up a can of worms in terms of single-file support, ad hoc workspaces etc.
	//
	// TODO(ms) Compare with the upstream gopls which potentially adds a
	// new workspacefolder if there are currently none.

	mods := []file.Modification{{
		URI:            params.TextDocument.URI,
		Action:         file.Open,
		Version:        params.TextDocument.Version,
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Range: openRange, Text: params.TextDocument.Text}},
	}}
	return s.didModifyFiles(ctx, mods, FromDidOpen)
}

func (s *server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	mods := []file.Modification{{
		URI:            params.TextDocument.URI,
		Action:         file.Change,
		Version:        params.TextDocument.Version,
		ContentChanges: params.ContentChanges,
	}}
	return s.didModifyFiles(ctx, mods, FromDidChange)
}

func (s *server) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	modifications := make([]file.Modification, len(params.Changes))
	for i, change := range params.Changes {
		modifications[i] = file.Modification{
			URI:    change.URI,
			Action: ChangeTypeToFileAction(change.Type),
			OnDisk: true,
		}
	}
	return s.didModifyFiles(ctx, modifications, FromDidChangeWatchedFiles)
}

func (s *server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	mods := []file.Modification{{
		URI:    params.TextDocument.URI,
		Action: file.Close,
	}}
	return s.didModifyFiles(ctx, mods, FromDidClose)
}

func (s *server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	// In [server.Initialize], we explicitly tell the client to not
	// sent us the file text on save. So here we completely ignore
	// params.Text.
	mods := []file.Modification{{
		URI:    params.TextDocument.URI,
		Action: file.Save,
	}}
	return s.didModifyFiles(ctx, mods, FromDidSave)
}

func (s *server) didModifyFiles(ctx context.Context, modifications []file.Modification, cause ModificationSource) error {
	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(cause), "Calculating file diagnostics...")
		defer work.EndAsync(ctx, "Done.")
	}

	err := s.workspace.DidModifyFiles(ctx, modifications)
	if err != nil {
		// didModifyFiles is only called by notification handlers. So
		// any errors should be shown, rather than returned.
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
		return nil
	}

	// golang/go#50267: diagnostics should be re-sent after each change.

	// After any file modifications, we need to update our watched files,
	// in case something changed. Compute the new set of directories to watch,
	// and if it differs from the current set, send updated registrations.
	return s.UpdateWatchedFiles(ctx)
}

// DiagnosticWorkTitle returns the title of the diagnostic work resulting from a
// file change originating from the given cause.
func DiagnosticWorkTitle(cause ModificationSource) string {
	return fmt.Sprintf("diagnosing %v", cause)
}

// ChangeTypeToFileAction converts a [protocol.FileChangeType] to a
// [file.Action]. FileChangeTypes are used in watched file
// notifications, and file actions are used within the broader
// [file.Modification] type.
func ChangeTypeToFileAction(ct protocol.FileChangeType) file.Action {
	switch ct {
	case protocol.Created:
		return file.Create
	case protocol.Changed:
		return file.Change
	case protocol.Deleted:
		return file.Delete
	}
	return file.UnknownAction
}
