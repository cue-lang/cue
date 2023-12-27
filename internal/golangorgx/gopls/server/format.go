// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/source"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

func (s *server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	ctx, done := event.Start(ctx, "lsp.Server.formatting", tag.URI.Of(params.TextDocument.URI))
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.UnknownKind)
	defer release()
	if !ok {
		return nil, err
	}
	switch snapshot.FileKind(fh) {
	case file.Go:
		return source.Format(ctx, snapshot, fh)
	case file.CUE:
		return source.FormatCUE(ctx, snapshot, fh)
	default:
		// TODO warn that we did not know how to format that file... why was the
		// request routed to this LSP?
	}
	return nil, nil
}
