// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package server defines gopls' implementation of the LSP server
// interface, [protocol.Server]. Call [New] to create an instance.
package server

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"

	"cuelang.org/go/internal/golangorgx/gopls/progress"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/lsp/cache"
)

var serverIDCounter int64

type ServerWithID interface {
	protocol.Server

	// ID returns a unique, human-readable string for this server, for
	// the purpose of log messages and debugging.
	ID() string
}

// New creates an LSP server and binds it to handle incoming client
// messages on the supplied stream.
func New(cache *cache.Cache, client protocol.ClientCloser, options *settings.Options) ServerWithID {
	counter := atomic.AddInt64(&serverIDCounter, 1)

	return &server{
		id:     strconv.FormatInt(counter, 10),
		client: client,
		cache:  cache,

		state:   serverCreated,
		options: options,
	}
}

type serverState int

const (
	serverCreated      = serverState(iota)
	serverInitializing // set once the server has received "initialize" request
	serverInitialized  // set once the server has received "initialized" request
	serverShutDown
)

func (s serverState) String() string {
	switch s {
	case serverCreated:
		return "created"
	case serverInitializing:
		return "initializing"
	case serverInitialized:
		return "initialized"
	case serverShutDown:
		return "shutDown"
	}
	return fmt.Sprintf("(unknown state: %d)", int(s))
}

// server implements the protocol.server interface. The server is
// mainly concerned with the connection to the editor/client's
// lifecycle start and end. It does the initial [server.Initialize] /
// [server.Initialized] dance, then creating and configuring the
// workspace. Once the workspace is up and running, most messages from
// the editor/client (e.g. text modification notifications) go
// straight to the workspace. The server also takes care of shutdown,
// handling the [server.Shutdown] and [server.Exit] messages.
type server struct {
	id string

	client    protocol.ClientCloser
	cache     *cache.Cache
	workspace *cache.Workspace

	state   serverState
	options *settings.Options

	progress *progress.Tracker

	pendingMessages []*protocol.ShowMessageParams
	pendingFolders  map[protocol.WorkspaceFolder]protocol.DocumentURI

	// watchedGlobPatterns is the set of glob patterns that we have requested
	// the client watch on disk. It will be updated as the set of directories
	// that the server should watch changes.
	// The map field may be reassigned but the map itself is immutable.
	watchedGlobPatterns map[protocol.RelativePattern]struct{}
	watchingIDCounter   int
}

var _ ServerWithID = (*server)(nil)

func (s *server) ID() string { return s.id }

// Shutdown implements the 'shutdown' LSP handler. It releases resources
// associated with the server and waits for all ongoing work to complete.
//
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#shutdown
//
// Shutdown is a request from the client, to the server, and it gets a
// response. The server should not exit after the shutdown response is
// sent, instead it should wait for an exit message, which is
// asynchronous.
func (s *server) Shutdown(ctx context.Context) error {
	ctx, done := event.Start(ctx, "lsp.Server.shutdown")
	defer done()

	switch s.state {
	case serverInitialized:
		s.state = serverShutDown
		// It's very likely that eventually we'll need to tell the
		// workspace we're shutting down, and maybe wait for it to
		// finish making changes.

	case serverShutDown:
		return nil

	default:
		event.Log(ctx, "server shutdown without initialization")
	}

	return nil
}

// Exit implements the 'exit' LSP handler.
//
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#exit
//
// This is asynchronous - it does not get a response.
func (s *server) Exit(ctx context.Context) error {
	_, done := event.Start(ctx, "lsp.Server.exit")
	defer done()

	s.client.Close()

	if s.state != serverShutDown {
		// TODO: We should be able to do better than this.
		os.Exit(1)
	}
	// We don't terminate the process on a normal exit, we just allow it to
	// close naturally if needed after the connection is closed.
	return nil
}

// WorkDoneProgressCancel is a message from the editor/client
// requesting the cancellation of a long-running process.
func (s *server) WorkDoneProgressCancel(ctx context.Context, params *protocol.WorkDoneProgressCancelParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.workDoneProgressCancel")
	defer done()

	err := s.progress.Cancel(params.Token)
	// WorkDoneProgressCancel is a notification, so if there's an
	// error, we show it rather than return it.
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.Error, Message: err.Error()})
	}
	return nil
}

// eventuallyShowMessage (eventually) calls showMessage in the
// client. This can only be called once the server has completed
// initialization. So if the server is not yet initialized, then the
// message is buffered, and will be sent to the client later (when
// [server.MaybeShowPendingMessages] is called and the server
// completes initialization).
func (s *server) eventuallyShowMessage(ctx context.Context, msg *protocol.ShowMessageParams) error {
	if s.state == serverInitialized {
		return s.client.ShowMessage(ctx, msg)
	}
	s.pendingMessages = append(s.pendingMessages, msg)
	return nil
}

// maybeShowPendingMessages sends any pending showMessage messages to
// the client if the server has completed initialization. If the
// server has not completed initialization, this is a noop.
func (s *server) maybeShowPendingMessages(ctx context.Context) error {
	if s.state != serverInitialized {
		return nil
	}
	messages := s.pendingMessages
	s.pendingMessages = nil
	for _, msg := range messages {
		err := s.client.ShowMessage(ctx, msg)
		if err != nil {
			return err
		}
	}
	return nil
}

// debugLog sends a log message to the client, with type=debug. This
// is used extensively for testing, and debugging.
func (s *server) debugLog(msg string) {
	ctx := context.Background()
	s.client.LogMessage(ctx, &protocol.LogMessageParams{Type: protocol.Debug, Message: msg})
}

// eventuallyUseWorkspaceFolders records the folders that the server
// has received as part of the initialize message. Those folders
// aren't used until after initialization is complete.
func (s *server) eventuallyUseWorkspaceFolders(folders map[protocol.WorkspaceFolder]protocol.DocumentURI) {
	if s.state != serverInitializing {
		panic("Must not be called except when server is initializing")
	}

	s.pendingFolders = folders
}

// maybeUseWorkspaceFolders is used once server initialization is
// complete. It adds any workspace folders stashed in the server
// during initialization to the workspace.
func (s *server) maybeUseWorkspaceFolders(ctx context.Context) error {
	if s.state != serverInitialized {
		panic("Must not be called except once server is initialized")
	}

	folders := s.pendingFolders
	s.pendingFolders = nil

	return s.AddFolders(ctx, folders)
}
