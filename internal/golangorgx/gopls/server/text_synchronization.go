// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/golangorgx/tools/xcontext"
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

	// FromRegenerateCgo refers to file modifications caused by regenerating
	// the cgo sources for the workspace.
	FromRegenerateCgo

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
	case FromRegenerateCgo:
		return "regenerate cgo"
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

func (s *server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didOpen", tag.URI.Of(params.TextDocument.URI))
	defer done()

	uri := params.TextDocument.URI

	// TODO(myitcv): we need to report an error/problem/something in case the user opens a file
	// that is not part of the CUE module. For now we will not support that, because it massively
	// opens up a can of worms in terms of single-file support, ad hoc workspaces etc.

	modifications := []file.Modification{{
		URI:        uri,
		Action:     file.Open,
		Version:    params.TextDocument.Version,
		Text:       []byte(params.TextDocument.Text),
		LanguageID: params.TextDocument.LanguageID,
	}}
	return s.didModifyFiles(ctx, modifications, FromDidOpen)
}

func (s *server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChange", tag.URI.Of(params.TextDocument.URI))
	defer done()

	uri := params.TextDocument.URI
	text, err := s.changedText(ctx, uri, params.ContentChanges)
	if err != nil {
		return err
	}
	c := file.Modification{
		URI:     uri,
		Action:  file.Change,
		Version: params.TextDocument.Version,
		Text:    text,
	}
	if err := s.didModifyFiles(ctx, []file.Modification{c}, FromDidChange); err != nil {
		return err
	}
	return nil
}

func (s *server) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChangeWatchedFiles")
	defer done()

	var modifications []file.Modification
	for _, change := range params.Changes {
		action := changeTypeToFileAction(change.Type)
		modifications = append(modifications, file.Modification{
			URI:    change.URI,
			Action: action,
			OnDisk: true,
		})
	}
	return s.didModifyFiles(ctx, modifications, FromDidChangeWatchedFiles)
}

func (s *server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didSave", tag.URI.Of(params.TextDocument.URI))
	defer done()

	c := file.Modification{
		URI:    params.TextDocument.URI,
		Action: file.Save,
	}
	if params.Text != nil {
		c.Text = []byte(*params.Text)
	}
	return s.didModifyFiles(ctx, []file.Modification{c}, FromDidSave)
}

func (s *server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didClose", tag.URI.Of(params.TextDocument.URI))
	defer done()

	return s.didModifyFiles(ctx, []file.Modification{
		{
			URI:     params.TextDocument.URI,
			Action:  file.Close,
			Version: -1,
			Text:    nil,
		},
	}, FromDidClose)
}

// This exists temporarily for facilitating integration tests. TODO(ms): remove when possible.
func logFilesToPackage(ctx context.Context, s *server, modifications []file.Modification) {
	uriToSnapshot := make(map[protocol.DocumentURI]map[protocol.DocumentURI][]metadata.ImportPath)
	for _, mod := range modifications {
		snapshot, release, err := s.session.SnapshotOf(ctx, mod.URI)
		if err != nil {
			continue
		}
		uriToSnapshot[mod.URI] = maps.Clone(snapshot.MetadataGraph().FilesToPackage)
		release()
	}
	bs, err := json.Marshal(uriToSnapshot)
	if err != nil {
		return
	}
	s.client.LogMessage(ctx, &protocol.LogMessageParams{
		Type:    protocol.Debug,
		Message: string(bs),
	})
}

func (s *server) didModifyFiles(ctx context.Context, modifications []file.Modification, cause ModificationSource) error {
	// wg guards two conditions:
	//  1. didModifyFiles is complete
	//  2. the goroutine diagnosing changes on behalf of didModifyFiles is
	//     complete, if it was started
	//
	// Both conditions must be satisfied for the purpose of testing: we don't
	// want to observe the completion of change processing until we have received
	// all diagnostics as well as all server->client notifications done on behalf
	// of this function.
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()

	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(cause), "Calculating file diagnostics...", nil, nil)
		go func() {
			wg.Wait()
			work.End(ctx, "Done.")
		}()
	}

	s.stateMu.Lock()
	if s.state >= serverShutDown {
		// This state check does not prevent races below, and exists only to
		// produce a better error message. The actual race to the cache should be
		// guarded by Session.viewMu.
		s.stateMu.Unlock()
		return errors.New("server is shut down")
	}
	s.stateMu.Unlock()

	// If the set of changes included directories, expand those directories
	// to their files.
	modifications = s.session.ExpandModificationsToDirectories(ctx, modifications)

	viewsToDiagnose, err := s.session.DidModifyFiles(ctx, modifications)
	if err != nil {
		return err
	}

	// golang/go#50267: diagnostics should be re-sent after each change.
	for _, mod := range modifications {
		s.mustPublishDiagnostics(mod.URI)
	}

	modCtx, modID := s.needsDiagnosis(ctx, viewsToDiagnose)

	wg.Add(1)
	go func() {
		s.diagnoseChangedViews(modCtx, modID, viewsToDiagnose, cause)
		wg.Done()
		// Temporary exposure of internal behaviour for testing only. TODO(ms): remove when possible.
		logFilesToPackage(ctx, s, modifications)
	}()

	// After any file modifications, we need to update our watched files,
	// in case something changed. Compute the new set of directories to watch,
	// and if it differs from the current set, send updated registrations.
	return s.updateWatchedDirectories(ctx)
}

// needsDiagnosis records the given views as needing diagnosis, returning the
// context and modification id to use for said diagnosis.
//
// Only the keys of viewsToDiagnose are used; the changed files are irrelevant.
func (s *server) needsDiagnosis(ctx context.Context, viewsToDiagnose map[*cache.View][]protocol.DocumentURI) (context.Context, uint64) {
	s.modificationMu.Lock()
	defer s.modificationMu.Unlock()
	if s.cancelPrevDiagnostics != nil {
		s.cancelPrevDiagnostics()
	}
	modCtx := xcontext.Detach(ctx)
	modCtx, s.cancelPrevDiagnostics = context.WithCancel(modCtx)
	s.lastModificationID++
	modID := s.lastModificationID

	for v := range viewsToDiagnose {
		if needs, ok := s.viewsToDiagnose[v]; !ok || needs < modID {
			s.viewsToDiagnose[v] = modID
		}
	}
	return modCtx, modID
}

// DiagnosticWorkTitle returns the title of the diagnostic work resulting from a
// file change originating from the given cause.
func DiagnosticWorkTitle(cause ModificationSource) string {
	return fmt.Sprintf("diagnosing %v", cause)
}

func (s *server) changedText(ctx context.Context, uri protocol.DocumentURI, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: no content changes provided", jsonrpc2.ErrInternal)
	}

	// Check if the client sent the full content of the file.
	// We accept a full content change even if the server expected incremental changes.
	if len(changes) == 1 && changes[0].Range == nil && changes[0].RangeLength == 0 {
		return []byte(changes[0].Text), nil
	}
	return s.applyIncrementalChanges(ctx, uri, changes)
}

func (s *server) applyIncrementalChanges(ctx context.Context, uri protocol.DocumentURI, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	fh, err := s.session.ReadFile(ctx, uri)
	if err != nil {
		return nil, err
	}
	content, err := fh.Content()
	if err != nil {
		return nil, fmt.Errorf("%w: file not found (%v)", jsonrpc2.ErrInternal, err)
	}
	for _, change := range changes {
		// TODO(adonovan): refactor to use diff.Apply, which is robust w.r.t.
		// out-of-order or overlapping changes---and much more efficient.

		// Make sure to update mapper along with the content.
		m := protocol.NewMapper(uri, content)
		if change.Range == nil {
			return nil, fmt.Errorf("%w: unexpected nil range for change", jsonrpc2.ErrInternal)
		}
		start, end, err := m.RangeOffsets(*change.Range)
		if err != nil {
			return nil, err
		}
		if end < start {
			return nil, fmt.Errorf("%w: invalid range for content change", jsonrpc2.ErrInternal)
		}
		var buf bytes.Buffer
		buf.Write(content[:start])
		buf.WriteString(change.Text)
		buf.Write(content[end:])
		content = buf.Bytes()
	}
	return content, nil
}

func changeTypeToFileAction(ct protocol.FileChangeType) file.Action {
	switch ct {
	case protocol.Changed:
		return file.Change
	case protocol.Created:
		return file.Create
	case protocol.Deleted:
		return file.Delete
	}
	return file.UnknownAction
}
