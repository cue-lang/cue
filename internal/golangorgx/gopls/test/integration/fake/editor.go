// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake/glob"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2/servertest"
)

// Editor is a fake client editor.  It keeps track of client state and can be
// used for writing LSP tests.
type Editor struct {

	// Server, client, and sandbox are concurrency safe and written only
	// at construction time, so do not require synchronization.
	Server     protocol.Server
	cancelConn func()
	serverConn jsonrpc2.Conn
	client     *Client
	sandbox    *Sandbox

	// TODO(rfindley): buffers should be keyed by protocol.DocumentURI.
	mu                 sync.Mutex
	config             EditorConfig                // editor configuration
	buffers            map[string]buffer           // open buffers (relative path -> buffer content)
	serverCapabilities protocol.ServerCapabilities // capabilities / options
	watchPatterns      []*glob.Glob                // glob patterns to watch

	// Call metrics for the purpose of expectations. This is done in an ad-hoc
	// manner for now. Perhaps in the future we should do something more
	// systematic. Guarded with a separate mutex as calls may need to be accessed
	// asynchronously via callbacks into the Editor.
	callsMu sync.Mutex
	calls   CallCounts
}

// CallCounts tracks the number of protocol notifications of different types.
type CallCounts struct {
	DidOpen, DidChange, DidSave, DidChangeWatchedFiles, DidClose, DidChangeConfiguration uint64
}

// buffer holds information about an open buffer in the editor.
type buffer struct {
	version int              // monotonic version; incremented on edits
	path    string           // relative path in the workspace
	mapper  *protocol.Mapper // buffer content
	dirty   bool             // if true, content is unsaved (TODO(rfindley): rename this field)
}

func (b buffer) text() string {
	return string(b.mapper.Content)
}

// EditorConfig configures the editor's LSP session.
//
// The zero value for EditorConfig is the default configuration.
type EditorConfig struct {
	// WorkspaceFolders is the workspace folders to configure on the LSP server.
	// Each workspace folder is a file path relative to the sandbox workdir, or
	// a URI, used to test virtual folders with non-file URI schemes.
	//
	// As a special case, if WorkspaceFolders is nil the editor defaults to
	// configuring a single workspace folder corresponding to the workdir root.
	// To explicitly send no workspace folders, use an empty (non-nil) slice.
	WorkspaceFolders []string

	// RootURIAsDefaultFolder configures the RootURI initialization
	// parameter to be set to the first workspace folder, as VS Code does,
	// or to the default workdir root if there are no workspace folders.
	RootURIAsDefaultFolder bool

	// Settings holds user-provided configuration for the LSP server.
	Settings map[string]any

	// FolderSettings holds user-provided per-folder configuration, if any.
	//
	// It maps each folder (as a relative path to the sandbox workdir) to its
	// configuration mapping (like Settings).
	FolderSettings map[string]map[string]any

	// CapabilitiesJSON holds JSON client capabilities to overlay over the
	// editor's default client capabilities.
	//
	// Specifically, this JSON string will be unmarshalled into the editor's
	// client capabilities struct, before sending to the server.
	CapabilitiesJSON []byte
}

// NewEditor creates a new Editor.
func NewEditor(sandbox *Sandbox, config EditorConfig) *Editor {
	return &Editor{
		buffers: make(map[string]buffer),
		sandbox: sandbox,
		config:  config,
	}
}

// Connect configures the editor to communicate with an LSP server on conn. It
// is not concurrency safe, and should be called at most once, before using the
// editor.
//
// It returns the editor, so that it may be called as follows:
//
//	editor, err := NewEditor(s).Connect(ctx, conn, hooks)
func (e *Editor) Connect(ctx context.Context, connector servertest.Connector, hooks ClientHooks) (*Editor, error) {
	bgCtx, cancelConn := context.WithCancel(context.WithoutCancel(ctx))
	conn := connector.Connect(bgCtx)
	e.cancelConn = cancelConn

	e.serverConn = conn
	e.Server = protocol.ServerDispatcher(conn)
	e.client = &Client{editor: e, hooks: hooks}
	conn.Go(bgCtx,
		protocol.Handlers(
			protocol.ClientHandler(e.client,
				jsonrpc2.MethodNotFound)))

	if err := e.initialize(ctx); err != nil {
		return nil, err
	}
	e.sandbox.Workdir.AddWatcher(e.onFileChanges)
	return e, nil
}

func (e *Editor) Stats() CallCounts {
	e.callsMu.Lock()
	defer e.callsMu.Unlock()
	return e.calls
}

// Shutdown issues the 'shutdown' LSP notification.
func (e *Editor) Shutdown(ctx context.Context) error {
	if e.Server != nil {
		if err := e.Server.Shutdown(ctx); err != nil {
			return fmt.Errorf("Shutdown: %w", err)
		}
	}
	return nil
}

// Exit issues the 'exit' LSP notification.
func (e *Editor) Exit(ctx context.Context) error {
	if e.Server != nil {
		// Not all LSP clients issue the exit RPC, but we do so here to ensure that
		// we gracefully handle it on multi-session servers.
		if err := e.Server.Exit(ctx); err != nil {
			return fmt.Errorf("Exit: %w", err)
		}
	}
	return nil
}

// Close issues the shutdown and exit sequence an editor should.
func (e *Editor) Close(ctx context.Context) error {
	if err := e.Shutdown(ctx); err != nil {
		return err
	}
	if err := e.Exit(ctx); err != nil {
		return err
	}
	defer func() {
		e.cancelConn()
	}()

	// called close on the editor should result in the connection closing
	select {
	case <-e.serverConn.Done():
		// connection closed itself
		return nil
	case <-ctx.Done():
		return fmt.Errorf("connection not closed: %w", ctx.Err())
	}
}

// makeSettings builds the settings map for use in LSP settings RPCs.
func makeSettings(sandbox *Sandbox, config EditorConfig, scopeURI *protocol.URI) map[string]any {
	settings := map[string]any{
		// Use verbose progress reporting so that integration tests can assert on
		// asynchronous operations being completed (such as diagnosing a snapshot).
		"verboseWorkDoneProgress": true,
	}
	maps.Copy(settings, config.Settings)

	// If the server is requesting configuration for a specific scope, apply
	// settings for the nearest folder that has customized settings, if any.
	if scopeURI != nil {
		var (
			scopePath       = protocol.DocumentURI(*scopeURI).FilePath()
			closestDir      string         // longest dir with settings containing the scope, if any
			closestSettings map[string]any // settings for that dir, if any
		)
		for relPath, settings := range config.FolderSettings {
			dir := sandbox.Workdir.AbsPath(relPath)
			if strings.HasPrefix(scopePath+string(filepath.Separator), dir+string(filepath.Separator)) && len(dir) > len(closestDir) {
				closestDir = dir
				closestSettings = settings
			}
		}
		maps.Copy(settings, closestSettings)
	}

	return settings
}

func (e *Editor) initialize(ctx context.Context) error {
	config := e.Config()

	params := &protocol.ParamInitialize{}
	params.InitializationOptions = makeSettings(e.sandbox, config, nil)
	params.WorkspaceFolders = makeWorkspaceFolders(e.sandbox, config.WorkspaceFolders)
	if config.RootURIAsDefaultFolder {
		if len(params.WorkspaceFolders) > 0 {
			params.RootURI = params.WorkspaceFolders[0].URI
		} else {
			params.RootURI = string(e.sandbox.Workdir.URI(string(e.sandbox.Workdir.RelativeTo)))
		}
	}

	capabilities, err := clientCapabilities(config)
	if err != nil {
		return fmt.Errorf("unmarshalling EditorConfig.CapabilitiesJSON: %v", err)
	}
	params.Capabilities = capabilities

	trace := protocol.TraceValues("messages")
	params.Trace = &trace
	// TODO: support workspace folders.
	if e.Server != nil {
		resp, err := e.Server.Initialize(ctx, params)
		if err != nil {
			return fmt.Errorf("initialize: %w", err)
		}
		e.mu.Lock()
		e.serverCapabilities = resp.Capabilities
		e.mu.Unlock()

		if err := e.Server.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
			return fmt.Errorf("initialized: %w", err)
		}
	}
	// TODO: await initial configuration here, or expect the server to manage that?
	return nil
}

func clientCapabilities(cfg EditorConfig) (protocol.ClientCapabilities, error) {
	var capabilities protocol.ClientCapabilities
	// Set various client capabilities that are sought by the server.
	capabilities.Workspace.Configuration = true // support workspace/configuration
	capabilities.TextDocument.Completion.CompletionItem.TagSupport = &protocol.CompletionItemTagOptions{}
	capabilities.TextDocument.Completion.CompletionItem.TagSupport.ValueSet = []protocol.CompletionItemTag{protocol.ComplDeprecated}
	capabilities.TextDocument.Completion.CompletionItem.SnippetSupport = true
	capabilities.TextDocument.SemanticTokens.Requests.Full = &protocol.Or_ClientSemanticTokensRequestOptions_full{Value: true}
	capabilities.Window.WorkDoneProgress = true // support window/workDoneProgress
	capabilities.TextDocument.SemanticTokens.TokenTypes = []string{
		"namespace", "type", "class", "enum", "interface",
		"struct", "typeParameter", "parameter", "variable", "property", "enumMember",
		"event", "function", "method", "macro", "keyword", "modifier", "comment",
		"string", "number", "regexp", "operator",
	}
	capabilities.TextDocument.SemanticTokens.TokenModifiers = []string{
		"declaration", "definition", "readonly", "static",
		"deprecated", "abstract", "async", "modification", "documentation", "defaultLibrary",
	}
	// The LSP tests have historically enabled this flag,
	// but really we should test both ways for older editors.
	capabilities.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport = true
	// Glob pattern watching is enabled.
	capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration = true

	// Apply capabilities overlay.
	if cfg.CapabilitiesJSON != nil {
		if err := json.Unmarshal(cfg.CapabilitiesJSON, &capabilities); err != nil {
			return protocol.ClientCapabilities{}, fmt.Errorf("unmarshalling EditorConfig.CapabilitiesJSON: %v", err)
		}
	}
	return capabilities, nil
}

// makeWorkspaceFolders creates a slice of workspace folders to use for
// this editing session, based on the editor configuration.
func makeWorkspaceFolders(sandbox *Sandbox, paths []string) (folders []protocol.WorkspaceFolder) {
	if paths == nil {
		paths = []string{string(sandbox.Workdir.RelativeTo)}
	}

	for _, path := range paths {
		uri := path
		if !strings.Contains(path, "://") { // relative file path
			uri = string(sandbox.Workdir.URI(path))
		}
		folders = append(folders, protocol.WorkspaceFolder{
			URI:  uri,
			Name: filepath.Base(uri),
		})
	}

	return folders
}

// onFileChanges is registered to be called by the Workdir on any writes that
// go through the Workdir API. It is called synchronously by the Workdir.
func (e *Editor) onFileChanges(ctx context.Context, evts []protocol.FileEvent) {
	if e.Server == nil {
		return
	}

	// e may be locked when onFileChanges is called, but it is important that we
	// synchronously increment this counter so that we can subsequently assert on
	// the number of expected DidChangeWatchedFiles calls.
	e.callsMu.Lock()
	e.calls.DidChangeWatchedFiles++
	e.callsMu.Unlock()

	// Since e may be locked, we must run this mutation asynchronously.
	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, evt := range evts {
			// Always send an on-disk change, even for events that seem useless
			// because they're shadowed by an open buffer.
			path := e.sandbox.Workdir.URIToPath(evt.URI)
			if buf, ok := e.buffers[path]; ok {
				// Following VS Code, don't honor deletions or changes to dirty buffers.
				if buf.dirty || evt.Type == protocol.Deleted {
					continue
				}

				content, err := e.sandbox.Workdir.ReadFile(path)
				if err != nil {
					continue // A race with some other operation.
				}
				// No need to update if the buffer content hasn't changed.
				if string(content) == buf.text() {
					continue
				}
				// During shutdown, this call will fail. Ignore the error.
				_ = e.setBufferContentLocked(ctx, path, false, content, nil)
			}
		}
		var matchedEvts []protocol.FileEvent
		for _, evt := range evts {
			filename := filepath.ToSlash(evt.URI.FilePath())
			for _, g := range e.watchPatterns {
				if g.Match(filename) {
					matchedEvts = append(matchedEvts, evt)
					break
				}
			}
		}

		// TODO(rfindley): don't send notifications while locked.
		e.Server.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{
			Changes: matchedEvts,
		})
	}()
}

// OpenFile creates a buffer for the given workdir-relative file.
//
// If the file is already open, it is a no-op.
func (e *Editor) OpenFile(ctx context.Context, path string) error {
	if e.HasBuffer(path) {
		return nil
	}
	content, err := e.sandbox.Workdir.ReadFile(path)
	if err != nil {
		return err
	}
	return e.createBuffer(ctx, path, false, content)
}

// CreateBuffer creates a new unsaved buffer corresponding to the workdir path,
// containing the given textual content.
func (e *Editor) CreateBuffer(ctx context.Context, path, content string) error {
	return e.createBuffer(ctx, path, true, []byte(content))
}

func (e *Editor) createBuffer(ctx context.Context, path string, dirty bool, content []byte) error {
	e.mu.Lock()

	if _, ok := e.buffers[path]; ok {
		e.mu.Unlock()
		return fmt.Errorf("buffer %q already exists", path)
	}

	uri := e.sandbox.Workdir.URI(path)
	buf := buffer{
		version: 1,
		path:    path,
		mapper:  protocol.NewMapper(uri, content),
		dirty:   dirty,
	}
	e.buffers[path] = buf

	item := e.textDocumentItem(buf)
	e.mu.Unlock()

	return e.sendDidOpen(ctx, item)
}

// textDocumentItem builds a protocol.TextDocumentItem for the given buffer.
//
// Precondition: e.mu must be held.
func (e *Editor) textDocumentItem(buf buffer) protocol.TextDocumentItem {
	return protocol.TextDocumentItem{
		URI:        e.sandbox.Workdir.URI(buf.path),
		LanguageID: languageID(buf.path),
		Version:    int32(buf.version),
		Text:       buf.text(),
	}
}

func (e *Editor) sendDidOpen(ctx context.Context, item protocol.TextDocumentItem) error {
	if e.Server != nil {
		if err := e.Server.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
			TextDocument: item,
		}); err != nil {
			return fmt.Errorf("DidOpen: %w", err)
		}
		e.callsMu.Lock()
		e.calls.DidOpen++
		e.callsMu.Unlock()
	}
	return nil
}

// languageID returns the language identifier for the path p.
func languageID(p string) string {
	if path.Ext(p) == ".cue" {
		return "cue"
	}
	return ""
}

// CloseBuffer removes the current buffer (regardless of whether it is saved).
func (e *Editor) CloseBuffer(ctx context.Context, path string) error {
	e.mu.Lock()
	_, ok := e.buffers[path]
	if !ok {
		e.mu.Unlock()
		return ErrUnknownBuffer
	}
	delete(e.buffers, path)
	e.mu.Unlock()

	return e.sendDidClose(ctx, e.TextDocumentIdentifier(path))
}

func (e *Editor) sendDidClose(ctx context.Context, doc protocol.TextDocumentIdentifier) error {
	if e.Server != nil {
		if err := e.Server.DidClose(ctx, &protocol.DidCloseTextDocumentParams{
			TextDocument: doc,
		}); err != nil {
			return fmt.Errorf("DidClose: %w", err)
		}
		e.callsMu.Lock()
		e.calls.DidClose++
		e.callsMu.Unlock()
	}
	return nil
}

func (e *Editor) TextDocumentIdentifier(path string) protocol.TextDocumentIdentifier {
	return protocol.TextDocumentIdentifier{
		URI: e.sandbox.Workdir.URI(path),
	}
}

// SaveBuffer writes the content of the buffer specified by the given path to
// the filesystem.
func (e *Editor) SaveBuffer(ctx context.Context, path string) error {
	if err := e.OrganizeImports(ctx, path); err != nil {
		return fmt.Errorf("organizing imports before save: %w", err)
	}
	if err := e.FormatBuffer(ctx, path); err != nil {
		return fmt.Errorf("formatting before save: %w", err)
	}
	return e.SaveBufferWithoutActions(ctx, path)
}

func (e *Editor) SaveBufferWithoutActions(ctx context.Context, path string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	buf, ok := e.buffers[path]
	if !ok {
		return fmt.Errorf("unknown buffer: %q", path)
	}
	content := buf.text()
	includeText := false
	syncOptions, ok := e.serverCapabilities.TextDocumentSync.(protocol.TextDocumentSyncOptions)
	if ok {
		includeText = syncOptions.Save.IncludeText
	}

	docID := e.TextDocumentIdentifier(buf.path)
	if e.Server != nil {
		if err := e.Server.WillSave(ctx, &protocol.WillSaveTextDocumentParams{
			TextDocument: docID,
			Reason:       protocol.Manual,
		}); err != nil {
			return fmt.Errorf("WillSave: %w", err)
		}
	}
	if err := e.sandbox.Workdir.WriteFile(ctx, path, content); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}

	buf.dirty = false
	e.buffers[path] = buf

	if e.Server != nil {
		params := &protocol.DidSaveTextDocumentParams{
			TextDocument: docID,
		}
		if includeText {
			params.Text = &content
		}
		if err := e.Server.DidSave(ctx, params); err != nil {
			return fmt.Errorf("DidSave: %w", err)
		}
		e.callsMu.Lock()
		e.calls.DidSave++
		e.callsMu.Unlock()
	}
	return nil
}

// ErrNoMatch is returned if a regexp search fails.
var (
	ErrNoMatch       = errors.New("no match")
	ErrUnknownBuffer = errors.New("unknown buffer")
)

// regexpLocation returns the location of the first occurrence of either re
// or its singular subgroup. It returns ErrNoMatch if the regexp doesn't match.
func regexpLocation(mapper *protocol.Mapper, re string) (protocol.Location, error) {
	var start, end int
	rec, err := regexp.Compile(re)
	if err != nil {
		return protocol.Location{}, err
	}
	indexes := rec.FindSubmatchIndex(mapper.Content)
	if indexes == nil {
		return protocol.Location{}, ErrNoMatch
	}
	switch len(indexes) {
	case 2:
		// no subgroups: return the range of the regexp expression
		start, end = indexes[0], indexes[1]
	case 4:
		// one subgroup: return its range
		start, end = indexes[2], indexes[3]
	default:
		return protocol.Location{}, fmt.Errorf("invalid search regexp %q: expect either 0 or 1 subgroups, got %d", re, len(indexes)/2-1)
	}
	return mapper.OffsetLocation(start, end)
}

// RegexpSearch returns the Location of the first match for re in the buffer
// bufName. For convenience, RegexpSearch supports the following two modes:
//  1. If re has no subgroups, return the position of the match for re itself.
//  2. If re has one subgroup, return the position of the first subgroup.
//
// It returns an error re is invalid, has more than one subgroup, or doesn't
// match the buffer.
func (e *Editor) RegexpSearch(bufName, re string) (protocol.Location, error) {
	e.mu.Lock()
	buf, ok := e.buffers[bufName]
	e.mu.Unlock()
	if !ok {
		return protocol.Location{}, ErrUnknownBuffer
	}
	return regexpLocation(buf.mapper, re)
}

// RegexpReplace edits the buffer corresponding to path by replacing the first
// instance of re, or its first subgroup, with the replace text. See
// RegexpSearch for more explanation of these two modes.
// It returns an error if re is invalid, has more than one subgroup, or doesn't
// match the buffer.
func (e *Editor) RegexpReplace(ctx context.Context, path, re, replace string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	buf, ok := e.buffers[path]
	if !ok {
		return ErrUnknownBuffer
	}
	loc, err := regexpLocation(buf.mapper, re)
	if err != nil {
		return err
	}
	edits := []protocol.TextEdit{{
		Range:   loc.Range,
		NewText: replace,
	}}
	patched, _, err := protocol.ApplyEdits(buf.mapper, edits)
	if err != nil {
		return fmt.Errorf("editing %q: %v", path, err)
	}
	return e.setBufferContentLocked(ctx, path, true, patched, edits)
}

// EditBuffer applies the given test edits to the buffer identified by path.
func (e *Editor) EditBuffer(ctx context.Context, path string, edits []protocol.TextEdit) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.editBufferLocked(ctx, path, edits)
}

func (e *Editor) SetBufferContent(ctx context.Context, path, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setBufferContentLocked(ctx, path, true, []byte(content), nil)
}

// HasBuffer reports whether the file name is open in the editor.
func (e *Editor) HasBuffer(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.buffers[name]
	return ok
}

// BufferText returns the content of the buffer with the given name, or "" if
// the file at that path is not open. The second return value reports whether
// the file is open.
func (e *Editor) BufferText(name string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	buf, ok := e.buffers[name]
	if !ok {
		return "", false
	}
	return buf.text(), true
}

// BufferVersion returns the current version of the buffer corresponding to
// name (or 0 if it is not being edited).
func (e *Editor) BufferVersion(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buffers[name].version
}

func (e *Editor) editBufferLocked(ctx context.Context, path string, edits []protocol.TextEdit) error {
	buf, ok := e.buffers[path]
	if !ok {
		return fmt.Errorf("unknown buffer %q", path)
	}
	content, _, err := protocol.ApplyEdits(buf.mapper, edits)
	if err != nil {
		return fmt.Errorf("editing %q: %v; edits:\n%v", path, err, edits)
	}
	return e.setBufferContentLocked(ctx, path, true, content, edits)
}

func (e *Editor) setBufferContentLocked(ctx context.Context, path string, dirty bool, content []byte, fromEdits []protocol.TextEdit) error {
	buf, ok := e.buffers[path]
	if !ok {
		return fmt.Errorf("unknown buffer %q", path)
	}
	buf.mapper = protocol.NewMapper(buf.mapper.URI, content)
	buf.version++
	buf.dirty = dirty
	e.buffers[path] = buf

	// A simple heuristic: if there is only one edit, send it incrementally.
	// Otherwise, send the entire content.
	var evt protocol.TextDocumentContentChangeEvent
	if len(fromEdits) == 1 {
		evt.Range = &fromEdits[0].Range
		evt.Text = fromEdits[0].NewText
	} else {
		evt.Text = buf.text()
	}
	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			Version:                int32(buf.version),
			TextDocumentIdentifier: e.TextDocumentIdentifier(buf.path),
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{evt},
	}
	if e.Server != nil {
		if err := e.Server.DidChange(ctx, params); err != nil {
			return fmt.Errorf("DidChange: %w", err)
		}
		e.callsMu.Lock()
		e.calls.DidChange++
		e.callsMu.Unlock()
	}
	return nil
}

// GoToDefinition jumps to the definition of the symbol at the given position
// in an open buffer. It returns the location of the resulting jump.
func (e *Editor) Definition(ctx context.Context, loc protocol.Location) ([]protocol.Location, error) {
	if err := e.checkBufferLocation(loc); err != nil {
		return nil, err
	}
	params := &protocol.DefinitionParams{}
	params.TextDocument.URI = loc.URI
	params.Position = loc.Range.Start

	return e.Server.Definition(ctx, params)
}

// OrganizeImports requests and performs the source.organizeImports codeAction.
func (e *Editor) OrganizeImports(ctx context.Context, path string) error {
	if e.Server == nil {
		return nil
	}
	params := &protocol.CodeActionParams{}
	params.TextDocument.URI = e.sandbox.Workdir.URI(path)
	params.Context.Only = []protocol.CodeActionKind{protocol.SourceOrganizeImports}
	// A zero Range means the whole file.
	actions, err := e.Server.CodeAction(ctx, params)
	if err != nil {
		return err
	}
	for _, action := range actions {
		if action.Title == "" {
			return fmt.Errorf("empty title for code action")
		}
		if action.Kind != protocol.SourceOrganizeImports {
			continue
		}
		if err := e.ApplyCodeAction(ctx, action); err != nil {
			return err
		}
	}
	return nil
}

// ApplyCodeAction applies the given code action.
func (e *Editor) ApplyCodeAction(ctx context.Context, action protocol.CodeAction) error {
	// Resolve the code actions if necessary and supported.
	if action.Edit == nil {
		editSupport, err := e.EditResolveSupport()
		if err != nil {
			return err
		}
		if editSupport {
			ca, err := e.Server.ResolveCodeAction(ctx, &action)
			if err != nil {
				return err
			}
			action.Edit = ca.Edit
		}
	}

	if action.Edit != nil {
		for _, change := range action.Edit.DocumentChanges {
			if change.TextDocumentEdit != nil {
				path := e.sandbox.Workdir.URIToPath(change.TextDocumentEdit.TextDocument.URI)
				if int32(e.buffers[path].version) != change.TextDocumentEdit.TextDocument.Version {
					// Skip edits for old versions.
					continue
				}
				if err := e.EditBuffer(ctx, path, protocol.AsTextEdits(change.TextDocumentEdit.Edits)); err != nil {
					return fmt.Errorf("editing buffer %q: %w", path, err)
				}
			}
		}
	}
	// Execute any commands. The specification says that commands are
	// executed after edits are applied.
	if action.Command != nil {
		if _, err := e.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{
			Command:   action.Command.Command,
			Arguments: action.Command.Arguments,
		}); err != nil {
			return err
		}
	}
	// Some commands may edit files on disk.
	return e.sandbox.Workdir.CheckForFileChanges(ctx)
}

func (e *Editor) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (interface{}, error) {
	if e.Server == nil {
		return nil, nil
	}
	var match bool
	if e.serverCapabilities.ExecuteCommandProvider != nil {
		// Ensure that this command was actually listed as a supported command.
		if slices.Contains(e.serverCapabilities.ExecuteCommandProvider.Commands, params.Command) {
			match = true
		}
	}
	if !match {
		return nil, fmt.Errorf("unsupported command %q", params.Command)
	}
	result, err := e.Server.ExecuteCommand(ctx, params)
	if err != nil {
		return nil, err
	}
	// Some commands may write directly to disk.
	// For convenience, check for those changes.
	if err := e.sandbox.Workdir.CheckForFileChanges(ctx); err != nil {
		return nil, fmt.Errorf("checking for file changes: %v", err)
	}
	return result, nil
}

// FormatBuffer formats a file via textDocument/formatting.
func (e *Editor) FormatBuffer(ctx context.Context, path string) error {
	if e.Server == nil {
		return nil
	}
	e.mu.Lock()
	version := e.buffers[path].version
	e.mu.Unlock()
	params := &protocol.DocumentFormattingParams{}
	params.TextDocument.URI = e.sandbox.Workdir.URI(path)
	edits, err := e.Server.Formatting(ctx, params)
	if err != nil {
		return fmt.Errorf("textDocument/formatting: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if versionAfter := e.buffers[path].version; versionAfter != version {
		return fmt.Errorf("before receipt of formatting edits, buffer version changed from %d to %d", version, versionAfter)
	}
	if len(edits) == 0 {
		return nil
	}
	return e.editBufferLocked(ctx, path, edits)
}

func (e *Editor) checkBufferLocation(loc protocol.Location) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	path := e.sandbox.Workdir.URIToPath(loc.URI)
	buf, ok := e.buffers[path]
	if !ok {
		return fmt.Errorf("buffer %q is not open", path)
	}

	_, _, err := buf.mapper.RangeOffsets(loc.Range)
	return err
}

// CodeLens executes a codelens request on the server.
func (e *Editor) CodeLens(ctx context.Context, path string) ([]protocol.CodeLens, error) {
	if e.Server == nil {
		return nil, nil
	}
	e.mu.Lock()
	_, ok := e.buffers[path]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("buffer %q is not open", path)
	}
	params := &protocol.CodeLensParams{
		TextDocument: e.TextDocumentIdentifier(path),
	}
	lens, err := e.Server.CodeLens(ctx, params)
	if err != nil {
		return nil, err
	}
	return lens, nil
}

// Completion executes a completion request on the server.
func (e *Editor) Completion(ctx context.Context, loc protocol.Location) (*protocol.CompletionList, error) {
	if e.Server == nil {
		return nil, nil
	}
	path := e.sandbox.Workdir.URIToPath(loc.URI)
	e.mu.Lock()
	_, ok := e.buffers[path]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("buffer %q is not open", path)
	}
	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.LocationTextDocumentPositionParams(loc),
	}
	completions, err := e.Server.Completion(ctx, params)
	if err != nil {
		return nil, err
	}
	return completions, nil
}

// References returns references to the object at loc, as returned by
// the connected LSP server. If no server is connected, it returns (nil, nil).
func (e *Editor) References(ctx context.Context, loc protocol.Location) ([]protocol.Location, error) {
	if e.Server == nil {
		return nil, nil
	}
	path := e.sandbox.Workdir.URIToPath(loc.URI)
	e.mu.Lock()
	_, ok := e.buffers[path]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("buffer %q is not open", path)
	}
	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.LocationTextDocumentPositionParams(loc),
		Context: protocol.ReferenceContext{
			IncludeDeclaration: true,
		},
	}
	locations, err := e.Server.References(ctx, params)
	if err != nil {
		return nil, err
	}
	return locations, nil
}

// Rename performs a rename of the object at loc to newName, using the
// connected LSP server. If no server is connected, it returns nil.
func (e *Editor) Rename(ctx context.Context, loc protocol.Location, newName string) error {
	if e.Server == nil {
		return nil
	}
	path := e.sandbox.Workdir.URIToPath(loc.URI)

	// Verify that PrepareRename succeeds.
	prepareParams := &protocol.PrepareRenameParams{}
	prepareParams.TextDocument = e.TextDocumentIdentifier(path)
	prepareParams.Position = loc.Range.Start
	if _, err := e.Server.PrepareRename(ctx, prepareParams); err != nil {
		return fmt.Errorf("preparing rename: %v", err)
	}

	params := &protocol.RenameParams{
		TextDocument: e.TextDocumentIdentifier(path),
		Position:     loc.Range.Start,
		NewName:      newName,
	}
	wsEdits, err := e.Server.Rename(ctx, params)
	if err != nil {
		return err
	}
	for _, change := range wsEdits.DocumentChanges {
		if change.TextDocumentEdit == nil {
			return fmt.Errorf("unsupported document change: %+v", change)
		}
		if err := e.applyTextDocumentEdit(ctx, *change.TextDocumentEdit); err != nil {
			return err
		}
	}
	return nil
}

func (e *Editor) applyTextDocumentEdit(ctx context.Context, change protocol.TextDocumentEdit) error {
	path := e.sandbox.Workdir.URIToPath(change.TextDocument.URI)
	if ver := int32(e.BufferVersion(path)); ver != change.TextDocument.Version {
		return fmt.Errorf("buffer versions for %q do not match: have %d, editing %d", path, ver, change.TextDocument.Version)
	}
	if !e.HasBuffer(path) {
		err := e.OpenFile(ctx, path)
		if os.IsNotExist(err) {
			// TODO: it's unclear if this is correct. Here we create the buffer (with
			// version 1), then apply edits. Perhaps we should apply the edits before
			// sending the didOpen notification.
			e.CreateBuffer(ctx, path, "")
			err = nil
		}
		if err != nil {
			return err
		}
	}
	return e.EditBuffer(ctx, path, protocol.AsTextEdits(change.Edits))
}

// Config returns the current editor configuration.
func (e *Editor) Config() EditorConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

func (e *Editor) SetConfig(cfg EditorConfig) {
	e.mu.Lock()
	e.config = cfg
	e.mu.Unlock()
}

// ChangeConfiguration sets the new editor configuration, and if applicable
// sends a didChangeConfiguration notification.
//
// An error is returned if the change notification failed to send.
func (e *Editor) ChangeConfiguration(ctx context.Context, newConfig EditorConfig) error {
	e.SetConfig(newConfig)
	if e.Server != nil {
		var params protocol.DidChangeConfigurationParams // empty: the server ignores the Settings field
		if err := e.Server.DidChangeConfiguration(ctx, &params); err != nil {
			return err
		}
		e.callsMu.Lock()
		e.calls.DidChangeConfiguration++
		e.callsMu.Unlock()
	}
	return nil
}

// ChangeWorkspaceFolders sets the new workspace folders, and sends a
// didChangeWorkspaceFolders notification to the server.
//
// The given folders must all be unique.
func (e *Editor) ChangeWorkspaceFolders(ctx context.Context, folders []string) error {
	config := e.Config()

	// capture existing folders so that we can compute the change.
	oldFolders := makeWorkspaceFolders(e.sandbox, config.WorkspaceFolders)
	newFolders := makeWorkspaceFolders(e.sandbox, folders)
	config.WorkspaceFolders = folders
	e.SetConfig(config)

	if e.Server == nil {
		return nil
	}

	var params protocol.DidChangeWorkspaceFoldersParams

	// Keep track of old workspace folders that must be removed.
	toRemove := make(map[protocol.URI]protocol.WorkspaceFolder)
	for _, folder := range oldFolders {
		toRemove[folder.URI] = folder
	}

	// Sanity check: if we see a folder twice the algorithm below doesn't work,
	// so track seen folders to ensure that we panic in that case.
	seen := make(map[protocol.URI]protocol.WorkspaceFolder)
	for _, folder := range newFolders {
		if _, ok := seen[folder.URI]; ok {
			panic(fmt.Sprintf("folder %s seen twice", folder.URI))
		}

		// If this folder already exists, we don't want to remove it.
		// Otherwise, we need to add it.
		if _, ok := toRemove[folder.URI]; ok {
			delete(toRemove, folder.URI)
		} else {
			params.Event.Added = append(params.Event.Added, folder)
		}
	}

	for _, v := range toRemove {
		params.Event.Removed = append(params.Event.Removed, v)
	}

	return e.Server.DidChangeWorkspaceFolders(ctx, &params)
}

// CodeAction executes a codeAction request on the server.
// If loc.Range is zero, the whole file is implied.
func (e *Editor) CodeAction(ctx context.Context, loc protocol.Location, diagnostics []protocol.Diagnostic) ([]protocol.CodeAction, error) {
	if e.Server == nil {
		return nil, nil
	}
	path := e.sandbox.Workdir.URIToPath(loc.URI)
	e.mu.Lock()
	_, ok := e.buffers[path]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("buffer %q is not open", path)
	}
	params := &protocol.CodeActionParams{
		TextDocument: e.TextDocumentIdentifier(path),
		Context: protocol.CodeActionContext{
			Diagnostics: diagnostics,
		},
		Range: loc.Range, // may be zero
	}
	lens, err := e.Server.CodeAction(ctx, params)
	if err != nil {
		return nil, err
	}
	return lens, nil
}

func (e *Editor) EditResolveSupport() (bool, error) {
	capabilities, err := clientCapabilities(e.Config())
	if err != nil {
		return false, err
	}
	return capabilities.TextDocument.CodeAction.ResolveSupport != nil && slices.Contains(capabilities.TextDocument.CodeAction.ResolveSupport.Properties, "edit"), nil
}

// Hover triggers a hover at the given position in an open buffer.
func (e *Editor) Hover(ctx context.Context, loc protocol.Location) (*protocol.MarkupContent, protocol.Location, error) {
	if err := e.checkBufferLocation(loc); err != nil {
		return nil, protocol.Location{}, err
	}
	params := &protocol.HoverParams{}
	params.TextDocument.URI = loc.URI
	params.Position = loc.Range.Start

	resp, err := e.Server.Hover(ctx, params)
	if err != nil {
		return nil, protocol.Location{}, fmt.Errorf("hover: %w", err)
	}
	if resp == nil {
		return nil, protocol.Location{}, nil
	}
	return &resp.Contents, protocol.Location{URI: loc.URI, Range: resp.Range}, nil
}
