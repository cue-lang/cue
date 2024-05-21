// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/cuelang"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/telemetry"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

func (s *server) Definition(ctx context.Context, params *protocol.DefinitionParams) (_ []protocol.Location, rerr error) {
	recordLatency := telemetry.StartLatencyTimer("definition")
	defer func() {
		recordLatency(ctx, rerr)
	}()

	ctx, done := event.Start(ctx, "lsp.Server.definition", tag.URI.Of(params.TextDocument.URI))
	defer done()

	// TODO(rfindley): definition requests should be multiplexed across all views.
	fh, snapshot, release, err := s.fileOf(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	defer release()
	switch kind := snapshot.FileKind(fh); kind {
	case file.CUE:
		return cuelang.Definition(ctx, snapshot, fh, params.Position)
	default:
		return nil, fmt.Errorf("can't find definitions for file type %s", kind)
	}
}
