// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"errors"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

// ModificationSource identifies the origin of a change.
type ModificationSource int

const (
	// FromDidOpen is from a didOpen notification.
	FromDidOpen = ModificationSource(iota)

	// FromDidChange is from a didChange notification.
	FromDidChange

	// FromDidChangeWatchedFiles is from didChangeWatchedFiles notification.
	FromDidChangeWatchedFiles

	// FromDidSave is from a didSave notification.
	FromDidSave

	// FromDidClose is from a didClose notification.
	FromDidClose

	// FromDidChangeConfiguration is from a didChangeConfiguration notification.
	FromDidChangeConfiguration

	// FromInitialWorkspaceLoad refers to the loading of all packages in the
	// workspace when the view is first created.
	FromInitialWorkspaceLoad

	// FromCheckUpgrades refers to state changes resulting from the CheckUpgrades
	// command, which queries module upgrades.
	FromCheckUpgrades

	// FromResetGoModDiagnostics refers to state changes resulting from the
	// ResetGoModDiagnostics command.
	FromResetGoModDiagnostics

	// FromToggleGCDetails refers to state changes resulting from toggling
	// gc_details on or off for a package.
	FromToggleGCDetails
)

func (m ModificationSource) String() string {
	switch m {
	case FromDidOpen:
		return "opened files"
	case FromDidChange:
		return "changed files"
	case FromDidChangeWatchedFiles:
		return "files changed on disk"
	case FromDidSave:
		return "saved files"
	case FromDidClose:
		return "close files"
	case FromInitialWorkspaceLoad:
		return "initial workspace load"
	case FromCheckUpgrades:
		return "from check upgrades"
	case FromResetGoModDiagnostics:
		return "from resetting go.mod diagnostics"
	default:
		return "unknown file modification"
	}
}

var openRange = &protocol.Range{
	Start: protocol.Position{Line: 0, Character: 0},
	End:   protocol.Position{Line: 0, Character: 0},
}

func (s *server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didOpen", tag.URI.Of(params.TextDocument.URI))
	defer done()

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
		LanguageID:     params.TextDocument.LanguageID,
	}}
	return s.didModifyFiles(ctx, mods, FromDidOpen)
}

func (s *server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChange", tag.URI.Of(params.TextDocument.URI))
	defer done()

	mods := []file.Modification{{
		URI:            params.TextDocument.URI,
		Action:         file.Change,
		Version:        params.TextDocument.Version,
		ContentChanges: params.ContentChanges,
	}}
	return s.didModifyFiles(ctx, mods, FromDidChange)
}

func (s *server) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChangeWatchedFiles")
	defer done()

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
	ctx, done := event.Start(ctx, "lsp.Server.didClose", tag.URI.Of(params.TextDocument.URI))
	defer done()

	mods := []file.Modification{{
		URI:    params.TextDocument.URI,
		Action: file.Close,
	}}
	return s.didModifyFiles(ctx, mods, FromDidClose)
}

func (s *server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didSave", tag.URI.Of(params.TextDocument.URI))
	defer done()

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
		work := s.progress.Start(ctx, DiagnosticWorkTitle(cause), "Calculating file diagnostics...", nil, nil)
		defer work.EndAsync(ctx, "Done.")
	}

	if s.state == serverShutDown {
		// This state check does not prevent races below, and exists
		// only to produce a better error message.
		return errors.New("server is shut down")
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
