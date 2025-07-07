// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

func (s *server) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	// Per the comment in [server.Initialize], we only support a single
	// WorkspaceFolder for now. More precisely, the call to Initialize must have
	// a single WorkspaceFolder. Therefore a notification via
	// DidChangeWorkspaceFolders must not cause that folder to change (because
	// that is the invariant we are maintaining for now).
	//
	// So for now we simply error in case there is any DidChangeWorkspaceFolders
	// notification, rather than trying to be smart and work out "has the folder
	// change?". If this proves to be too simplistic or restrictive, then we can
	// revisit as part of removing this constraint.
	//
	// When we do add such support, we need to be how/if/where logic for
	// deduping views comes in.
	//
	// Ensure this logic is consistent with [server.Initialize].
	return fmt.Errorf("cue lsp only supports a single WorkspaceFolder for now")
}

func (s *server) DidChangeConfiguration(ctx context.Context, _ *protocol.DidChangeConfigurationParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChangeConfiguration")
	defer done()

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()

	return nil
}
