// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/golang"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/template"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

func (s *server) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	ctx, done := event.Start(ctx, "lsp.Server.documentHighlight", tag.URI.Of(params.TextDocument.URI))
	defer done()

	fh, snapshot, release, err := s.fileOf(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	defer release()

	switch snapshot.FileKind(fh) {
	case file.Tmpl:
		return template.Highlight(ctx, snapshot, fh, params.Position)
	case file.Go:
		rngs, err := golang.Highlight(ctx, snapshot, fh, params.Position)
		if err != nil {
			event.Error(ctx, "no highlight", err)
		}
		return toProtocolHighlight(rngs), nil
	}
	return nil, nil // empty result
}

func toProtocolHighlight(rngs []protocol.Range) []protocol.DocumentHighlight {
	result := make([]protocol.DocumentHighlight, 0, len(rngs))
	kind := protocol.Text
	for _, rng := range rngs {
		result = append(result, protocol.DocumentHighlight{
			Kind:  kind,
			Range: rng,
		})
	}
	return result
}
