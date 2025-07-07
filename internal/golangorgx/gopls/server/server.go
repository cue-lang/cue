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
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/progress"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

// New creates an LSP server and binds it to handle incoming client
// messages on the supplied stream.
func New(client protocol.ClientCloser, options *settings.Options) protocol.Server {
	const concurrentAnalyses = 1
	// If this assignment fails to compile after a protocol
	// upgrade, it means that one or more new methods need new
	// stub declarations in unimplemented.go.
	return &server{
		watchedGlobPatterns: nil, // empty
		changedFiles:        make(map[protocol.DocumentURI]struct{}),
		client:              client,
		progress:            progress.NewTracker(client),
		options:             options,
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

// server implements the protocol.server interface.
type server struct {
	client protocol.ClientCloser

	stateMu sync.Mutex
	state   serverState
	// notifications generated before serverInitialized
	notifications []*protocol.ShowMessageParams

	tempDir string

	// changedFiles tracks files for which there has been a textDocument/didChange.
	changedFilesMu sync.Mutex
	changedFiles   map[protocol.DocumentURI]struct{}

	// folders is only valid between initialize and initialized, and holds the
	// set of folders to build views for when we are ready.
	// Each has a valid, non-empty 'file'-scheme URI.
	//
	// TODO(myitcv): it doesn't feel clean having this state at the "same level"
	// as other server state. This field is only relevant for the time between
	// the call to Initialize and notification of Initialized.
	pendingFolders []protocol.WorkspaceFolder

	// watchedGlobPatterns is the set of glob patterns that we have requested
	// the client watch on disk. It will be updated as the set of directories
	// that the server should watch changes.
	// The map field may be reassigned but the map is immutable.
	watchedGlobPatternsMu  sync.Mutex
	watchedGlobPatterns    map[protocol.RelativePattern]struct{}
	watchRegistrationCount int

	progress *progress.Tracker

	// When the workspace fails to load, we show its status through a progress
	// report with an error message.
	criticalErrorStatusMu sync.Mutex
	criticalErrorStatus   *progress.WorkDone

	// Track an ongoing CPU profile created with the StartProfile command and
	// terminated with the StopProfile command.
	ongoingProfileMu sync.Mutex
	ongoingProfile   *os.File // if non-nil, an ongoing profile is writing to this file

	// Track most recently requested options.
	optionsMu sync.Mutex
	options   *settings.Options

	lastModificationID uint64 // incrementing clock
}

func (s *server) WorkDoneProgressCancel(ctx context.Context, params *protocol.WorkDoneProgressCancelParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.workDoneProgressCancel")
	defer done()

	return s.progress.Cancel(params.Token)
}

func (s *server) Shutdown(context.Context) error { return nil }
func (s *server) Exit(context.Context) error     { return nil }
func (s *server) Initialize(context.Context, *protocol.ParamInitialize) (*protocol.InitializeResult, error) {
	return nil, nil
}
func (s *server) Initialized(context.Context, *protocol.InitializedParams) error {
	return nil
}
