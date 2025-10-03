// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"context"
	"fmt"

	cueformat "cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/diff"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

// Formatting formats the params.TextDocument.URI as canonical cue.
//
// The file must be within a cue module.
//
// Formatting implements [protocol.Server]
func (s *server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	_, done := event.Start(ctx, "lsp.Server.formatting", tag.URI.Of(params.TextDocument.URI))
	defer done()

	uri := params.TextDocument.URI
	mod, err := s.workspace.FindModuleForFile(uri)
	if err != nil {
		return nil, err
	}

	parsedFile, config, fh, err := mod.ReadCUEFile(uri)
	if err != nil {
		s.client.ShowMessage(ctx, &protocol.ShowMessageParams{
			Type:    protocol.Info,
			Message: fmt.Sprintf("Cannot format file: %v", err),
		})
		return nil, nil
	} else if parsedFile == nil {
		s.debugLog(fmt.Sprintf("%v is not a CUE file", uri))
		return nil, nil
	} else if config.Mode != parser.ParseComments {
		s.debugLog(fmt.Sprintf("cannot format %v due to syntax errors", uri))
		return nil, nil
	}

	formatted, err := cueformat.Node(parsedFile)
	if err != nil {
		// TODO fix up the AST like gopls so we can do more with
		// partial/incomplete code.
		//
		// For now return early because there is nothing we can do.
		return nil, nil
	}

	src := fh.Content()
	if bytes.Equal(formatted, src) {
		return nil, nil
	}

	// Because of bugs in the formatter and/or parser, check that the
	// result of formatting can be parsed without error.
	_, err = parser.ParseFile(parsedFile.Filename, formatted, config)
	if err != nil {
		s.debugLog(fmt.Sprintf("%v: error when parsing newly formatted source: %v", uri, err))
		return nil, nil
	}

	mapper := protocol.NewMapper(fh.URI(), src)
	edits := diff.Strings(string(src), string(formatted))
	return protocol.EditsFromDiffEdits(mapper, edits)
}
