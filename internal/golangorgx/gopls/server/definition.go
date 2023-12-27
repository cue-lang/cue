// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/source"
	"cuelang.org/go/internal/golangorgx/gopls/telemetry"
	"cuelang.org/go/internal/golangorgx/gopls/template"
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
	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.UnknownKind)
	defer release()
	if !ok {
		return nil, err
	}
	switch kind := snapshot.FileKind(fh); kind {
	case file.Tmpl:
		return template.Definition(snapshot, fh, params.Position)
	case file.Go:
		return source.Definition(ctx, snapshot, fh, params.Position)
	default:
		return nil, fmt.Errorf("can't find definitions for file type %s", kind)
	}
}

func (s *server) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) ([]protocol.Location, error) {
	ctx, done := event.Start(ctx, "lsp.Server.typeDefinition", tag.URI.Of(params.TextDocument.URI))
	defer done()

	// TODO(rfindley): type definition requests should be multiplexed across all views.
	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.Go)
	defer release()
	if !ok {
		return nil, err
	}
	switch kind := snapshot.FileKind(fh); kind {
	case file.Go:
		return source.TypeDefinition(ctx, snapshot, fh, params.Position)
	default:
		return nil, fmt.Errorf("can't find type definitions for file type %s", kind)
	}
}
