// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// This file defines server methods related to initialization,
// options, shutdown, and exit.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/debug"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/util/maps"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
)

func (s *server) Initialize(ctx context.Context, params *protocol.ParamInitialize) (*protocol.InitializeResult, error) {
	ctx, done := event.Start(ctx, "lsp.Server.initialize")
	defer done()

	s.stateMu.Lock()
	if s.state >= serverInitializing {
		defer s.stateMu.Unlock()
		return nil, fmt.Errorf("%w: initialize called while server in %v state", jsonrpc2.ErrInvalidRequest, s.state)
	}
	s.state = serverInitializing
	s.stateMu.Unlock()

	// Create a temp dir using the server PID. For uniqueness, use the server
	// PID rather than params.ProcessID (the client pid).
	pid := os.Getpid()
	s.tempDir = filepath.Join(os.TempDir(), fmt.Sprintf("cue-lsp-%d.%s", pid, s.session.ID()))
	err := os.Mkdir(s.tempDir, 0700)
	if err != nil {
		// MkdirTemp could fail due to permissions issues. This is a problem with
		// the user's environment, but should not block gopls otherwise behaving.
		// All usage of s.tempDir should be predicated on having a non-empty
		// s.tempDir.
		event.Error(ctx, "creating temp dir", err)
		s.tempDir = ""
	}

	// TODO(myitcv): need to better understand events, and the mechanisms that
	// hang off that. At least for now we know that the integration expectation
	// setup relies on the progress messaging titles to track things being done.
	s.progress.SetSupportsWorkDoneProgress(params.Capabilities.Window.WorkDoneProgress)

	// Clone the existing (default?) options, and update from the params. Defer setting
	// the options for readability reasons (rather than having that call "lost" below any
	// params-options related code below, it's easier to read locally here).
	options := s.Options().Clone()
	defer s.SetOptions(options)

	// TODO(myitcv): review and slim down option handling code
	if err := s.handleOptionResults(ctx, settings.SetOptions(options, params.InitializationOptions)); err != nil {
		return nil, err
	}
	options.ForClientCapabilities(params.ClientInfo, params.Capabilities)

	// An LSP WorkspaceFolder corresponds to a Session View. For now, we only
	// want to support a single WorkspaceFolder, to avoid any complex logic of
	// View handling. Therefore, error in case we get anything other than a
	// single WorkspaceFolder during Initialize. Also error when handling
	// didChangeWorkspaceFolders in case that state changes. We can then
	// prioritise work to support different clients etc based on bug reports.
	//
	// Ensure this logic is consistent with [server.DidChangeWorkspaceFolders].
	//
	// Note that (for now) we do not support a fallback to params.RootURI. We
	// might do this in the future.
	if l := len(params.WorkspaceFolders); l != 1 {
		return nil, fmt.Errorf("got %d WorkspaceFolders; expected 1", l)
	}

	folders := params.WorkspaceFolders
	for _, folder := range folders {
		if folder.URI == "" {
			return nil, fmt.Errorf("empty WorkspaceFolder.URI")
		}
		if _, err := protocol.ParseDocumentURI(folder.URI); err != nil {
			return nil, fmt.Errorf("invalid WorkspaceFolder.URI: %v", err)
		}
		s.pendingFolders = append(s.pendingFolders, folder)
	}

	// TODO(myitcv): later we should leverage the existing mechanism for getting
	// version information in cmd/cue. For now, let's just work around that.
	versionInfo := debug.VersionInfo()
	cueLspVersion, err := json.Marshal(versionInfo)
	if err != nil {
		return nil, err
	}

	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			DocumentFormattingProvider: &protocol.Or_ServerCapabilities_documentFormattingProvider{Value: true},
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				Change:    protocol.Incremental,
				OpenClose: true,
				Save: &protocol.SaveOptions{
					IncludeText: false,
				},
			},

			// Even though we don't support this for now, it's worth having the
			// feature enabled to that users run into the error of it not being
			// supported. It's a more obvious signal that the feature isn't
			// supported (yet), and a clear action to raise an issue if this is
			// something that they need.
			Workspace: &protocol.WorkspaceOptions{
				WorkspaceFolders: &protocol.WorkspaceFolders5Gn{
					Supported:           true,
					ChangeNotifications: "workspace/didChangeWorkspaceFolders",
				},
			},
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    "cue lsp",
			Version: string(cueLspVersion),
		},
	}, nil
}

func (s *server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.initialized")
	defer done()

	s.stateMu.Lock()
	if s.state >= serverInitialized {
		defer s.stateMu.Unlock()
		return fmt.Errorf("%w: initialized called while server in %v state", jsonrpc2.ErrInvalidRequest, s.state)
	}
	s.state = serverInitialized
	s.stateMu.Unlock()

	// TODO(myitcv): I _think_ that even in the case that we are using a daemon instance,
	// then the pid that the client sees is that of the ultimate LSP server. Which is correct.
	// Even though the client is connected through a forwarder, this is a dumb process that
	// shouldn't affect behaviour in any way. This TODO captures ensuring that is the case.
	// If so, this TODO can be removed.
	pid := os.Getpid()
	event.Log(ctx, fmt.Sprintf("cue lsp server pid: %d", pid))

	for _, not := range s.notifications {
		s.client.ShowMessage(ctx, not)
	}
	s.notifications = nil

	// Now create views for the pending folders
	s.addFolders(ctx, s.pendingFolders)
	s.pendingFolders = nil

	return nil
}

// addFolders gets called from Initialized to add the specified list of
// WorkspaceFolders to the session. There is no sense in returning an error
// here, because Initialized is a notification. As such, we report any errors
// in the loading process as shown messages to the end user.
//
// If we start to call addFolders from elsewhere, i.e. at another point during
// the lifetime of 'cue lsp', we will need to _very_ carefully consider the
// assumptions that are otherwise made in the code below.
//
// Precondition: each folder in folders must have a valid URI.
func (s *server) addFolders(ctx context.Context, folders []protocol.WorkspaceFolder) {
	originalViews := len(s.session.Views())
	viewErrors := make(map[protocol.URI]error)

	var ndiagnose sync.WaitGroup // number of unfinished diagnose calls

	// Do not remove this progress notification. It is a key part of integration
	// tests knowing when 'cue lsp' is "ready" post the Initialized-initiated
	// workspace load.
	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(FromInitialWorkspaceLoad), "Calculating diagnostics for initial workspace load...", nil, nil)
		defer func() {
			go func() {
				ndiagnose.Wait()
				work.End(ctx, "Done.")
			}()
		}()
	}

	// Only one view gets to have a workspace.
	var nsnapshots sync.WaitGroup // number of unfinished snapshot initializations
	for _, folder := range folders {
		uri, err := protocol.ParseDocumentURI(folder.URI)
		if err != nil {
			// Precondition on folders having valid URI violated
			panic(err)
		}

		work := s.progress.Start(ctx, "Setting up workspace", "Loading packages...", nil, nil)
		snapshot, release, err := s.addView(ctx, folder.Name, uri)
		if err != nil {
			viewErrors[folder.URI] = err
			work.End(ctx, fmt.Sprintf("Error loading packages: %s", err))
			continue
		}
		// Inv: release() must be called once.

		// Initialize snapshot asynchronously.
		initialized := make(chan struct{})
		nsnapshots.Add(1)
		go func() {
			snapshot.AwaitInitialized(ctx)
			work.End(ctx, "Finished loading packages.")
			nsnapshots.Done()
			close(initialized) // signal
		}()

		// Diagnose the newly created view asynchronously.
		ndiagnose.Add(1)
		go func() {
			s.diagnoseSnapshot(snapshot, nil, 0)
			<-initialized
			release()
			ndiagnose.Done()
		}()
	}

	// Wait for snapshots to be initialized so that all files are known.
	// (We don't need to wait for diagnosis to finish.)
	nsnapshots.Wait()

	// Register for file watching notifications, if they are supported.
	if err := s.updateWatchedDirectories(ctx); err != nil {
		event.Error(ctx, "failed to register for file watching notifications", err)
	}

	// Report any errors using the protocol.
	if len(viewErrors) > 0 {
		errMsg := fmt.Sprintf("Error loading workspace folders (expected %v, got %v)\n", len(folders), len(s.session.Views())-originalViews)
		for uri, err := range viewErrors {
			errMsg += fmt.Sprintf("failed to load view for %s: %v\n", uri, err)
		}
		showMessage(ctx, s.client, protocol.Error, errMsg)
	}
}

// updateWatchedDirectories compares the current set of directories to watch
// with the previously registered set of directories. If the set of directories
// has changed, we unregister and re-register for file watching notifications.
// updatedSnapshots is the set of snapshots that have been updated.
func (s *server) updateWatchedDirectories(ctx context.Context) error {
	patterns := s.session.FileWatchingGlobPatterns(ctx)

	s.watchedGlobPatternsMu.Lock()
	defer s.watchedGlobPatternsMu.Unlock()

	// Nothing to do if the set of workspace directories is unchanged.
	if maps.SameKeys(s.watchedGlobPatterns, patterns) {
		return nil
	}

	// If the set of directories to watch has changed, register the updates and
	// unregister the previously watched directories. This ordering avoids a
	// period where no files are being watched. Still, if a user makes on-disk
	// changes before these updates are complete, we may miss them for the new
	// directories.
	prevID := s.watchRegistrationCount - 1
	if err := s.registerWatchedDirectoriesLocked(ctx, patterns); err != nil {
		return err
	}
	if prevID >= 0 {
		return s.client.UnregisterCapability(ctx, &protocol.UnregistrationParams{
			Unregisterations: []protocol.Unregistration{{
				ID:     watchedFilesCapabilityID(prevID),
				Method: "workspace/didChangeWatchedFiles",
			}},
		})
	}
	return nil
}

func watchedFilesCapabilityID(id int) string {
	return fmt.Sprintf("workspace/didChangeWatchedFiles-%d", id)
}

// registerWatchedDirectoriesLocked sends the workspace/didChangeWatchedFiles
// registrations to the client and updates s.watchedDirectories.
// The caller must not subsequently mutate patterns.
func (s *server) registerWatchedDirectoriesLocked(ctx context.Context, patterns map[protocol.RelativePattern]unit) error {
	if !s.Options().DynamicWatchedFilesSupported {
		return nil
	}

	supportsRelativePatterns := s.Options().RelativePatternsSupported

	s.watchedGlobPatterns = patterns
	watchers := make([]protocol.FileSystemWatcher, 0, len(patterns)) // must be a slice
	val := protocol.WatchChange | protocol.WatchDelete | protocol.WatchCreate
	for pattern := range patterns {
		var value any
		if supportsRelativePatterns && pattern.BaseURI != "" {
			value = pattern
		} else {
			p := pattern.Pattern
			if pattern.BaseURI != "" {
				p = path.Join(filepath.ToSlash(pattern.BaseURI.Path()), p)
			}
			value = p
		}
		watchers = append(watchers, protocol.FileSystemWatcher{
			GlobPattern: protocol.GlobPattern{Value: value},
			Kind:        &val,
		})
	}

	if err := s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{
			ID:     watchedFilesCapabilityID(s.watchRegistrationCount),
			Method: "workspace/didChangeWatchedFiles",
			RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
				Watchers: watchers,
			},
		}},
	}); err != nil {
		return err
	}
	s.watchRegistrationCount++
	return nil
}

// Options returns the current server options.
//
// The caller must not modify the result.
func (s *server) Options() *settings.Options {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	return s.options
}

// SetOptions sets the current server options.
//
// The caller must not subsequently modify the options.
func (s *server) SetOptions(opts *settings.Options) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.options = opts
}

func (s *server) newFolder(ctx context.Context, folder protocol.DocumentURI, name string) (*cache.Folder, error) {
	opts := s.Options()
	if opts.ConfigurationSupported {
		scope := string(folder)
		configs, err := s.client.Configuration(ctx, &protocol.ParamConfiguration{
			Items: []protocol.ConfigurationItem{{
				ScopeURI: &scope,
				Section:  "cue lsp",
			}},
		},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace configuration from client (%s): %v", folder, err)
		}

		opts = opts.Clone()
		for _, config := range configs {
			if err := s.handleOptionResults(ctx, settings.SetOptions(opts, config)); err != nil {
				return nil, err
			}
		}
	}

	return &cache.Folder{
		Dir:     folder,
		Name:    name,
		Options: opts,
	}, nil
}

// fetchFolderOptions makes a workspace/configuration request for the given
// folder, and populates options with the result.
//
// If folder is "", fetchFolderOptions makes an unscoped request.
func (s *server) fetchFolderOptions(ctx context.Context, folder protocol.DocumentURI) (*settings.Options, error) {
	opts := s.Options()
	if !opts.ConfigurationSupported {
		return opts, nil
	}
	var scopeURI *string
	if folder != "" {
		scope := string(folder)
		scopeURI = &scope
	}
	configs, err := s.client.Configuration(ctx, &protocol.ParamConfiguration{
		Items: []protocol.ConfigurationItem{{
			ScopeURI: scopeURI,
			Section:  "gopls",
		}},
	},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace configuration from client (%s): %v", folder, err)
	}

	opts = opts.Clone()
	for _, config := range configs {
		if err := s.handleOptionResults(ctx, settings.SetOptions(opts, config)); err != nil {
			return nil, err
		}
	}
	return opts, nil
}

func (s *server) eventuallyShowMessage(ctx context.Context, msg *protocol.ShowMessageParams) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state == serverInitialized {
		return s.client.ShowMessage(ctx, msg)
	}
	s.notifications = append(s.notifications, msg)
	return nil
}

func (s *server) handleOptionResults(ctx context.Context, results settings.OptionResults) error {
	var warnings, errors []string
	for _, result := range results {
		switch result.Error.(type) {
		case nil:
			// nothing to do
		case *settings.SoftError:
			warnings = append(warnings, result.Error.Error())
		default:
			errors = append(errors, result.Error.Error())
		}
	}

	// Sort messages, but put errors first.
	//
	// Having stable content for the message allows clients to de-duplicate. This
	// matters because we may send duplicate warnings for clients that support
	// dynamic configuration: one for the initial settings, and then more for the
	// individual viewsettings.
	var msgs []string
	msgType := protocol.Warning
	if len(errors) > 0 {
		msgType = protocol.Error
		sort.Strings(errors)
		msgs = append(msgs, errors...)
	}
	if len(warnings) > 0 {
		sort.Strings(warnings)
		msgs = append(msgs, warnings...)
	}

	if len(msgs) > 0 {
		// Settings
		combined := "Invalid settings: " + strings.Join(msgs, "; ")
		params := &protocol.ShowMessageParams{
			Type:    msgType,
			Message: combined,
		}
		return s.eventuallyShowMessage(ctx, params)
	}

	return nil
}

// fileOf returns the file for a given URI and its snapshot.
// On success, the returned function must be called to release the snapshot.
func (s *server) fileOf(ctx context.Context, uri protocol.DocumentURI) (file.Handle, *cache.Snapshot, func(), error) {
	snapshot, release, err := s.session.SnapshotOf(ctx, uri)
	if err != nil {
		return nil, nil, nil, err
	}
	fh, err := snapshot.ReadFile(ctx, uri)
	if err != nil {
		release()
		return nil, nil, nil, err
	}
	return fh, snapshot, release, nil
}

// shutdown implements the 'shutdown' LSP handler. It releases resources
// associated with the server and waits for all ongoing work to complete.
func (s *server) Shutdown(ctx context.Context) error {
	ctx, done := event.Start(ctx, "lsp.Server.shutdown")
	defer done()

	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state < serverInitialized {
		event.Log(ctx, "server shutdown without initialization")
	}
	if s.state != serverShutDown {
		// drop all the active views
		s.session.Shutdown(ctx)
		s.state = serverShutDown
		if s.tempDir != "" {
			if err := os.RemoveAll(s.tempDir); err != nil {
				event.Error(ctx, "removing temp dir", err)
			}
		}
	}
	return nil
}

func (s *server) Exit(ctx context.Context) error {
	ctx, done := event.Start(ctx, "lsp.Server.exit")
	defer done()

	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	s.client.Close()

	if s.state != serverShutDown {
		// TODO: We should be able to do better than this.
		os.Exit(1)
	}
	// We don't terminate the process on a normal exit, we just allow it to
	// close naturally if needed after the connection is closed.
	return nil
}
