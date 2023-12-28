// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parsego

import (
	"context"

	"cuelang.org/go/cue/parser"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

// Parse parses a buffer of Go source, repairing the tree if necessary.
//
// The provided ctx is used only for logging.
func Parse(ctx context.Context, uri protocol.DocumentURI, src []byte, options ...parser.Option) (res *File) {
	ctx, done := event.Start(ctx, "cache.ParseGoSrc", tag.File.Of(uri.Path()))
	defer done()

	file, parseErr := parser.ParseFile(uri.Path(), src, options...)
	tok := file.Pos().File()

	return &File{
		URI:      uri,
		Options:  options,
		Src:      src,
		File:     file,
		Tok:      tok,
		Mapper:   protocol.NewMapper(uri, src),
		ParseErr: parseErr,
	}
}
