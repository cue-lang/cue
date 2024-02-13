// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package work

import (
	"bytes"
	"context"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"golang.org/x/mod/modfile"
)

func Hover(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle, position protocol.Position) (*protocol.Hover, error) {
	// We only provide hover information for the view's go.work file.
	if fh.URI() != snapshot.View().GoWork() {
		return nil, nil
	}

	ctx, done := event.Start(ctx, "work.Hover")
	defer done()

	// Get the position of the cursor.
	pw, err := snapshot.ParseWork(ctx, fh)
	if err != nil {
		return nil, fmt.Errorf("getting go.work file handle: %w", err)
	}
	offset, err := pw.Mapper.PositionOffset(position)
	if err != nil {
		return nil, fmt.Errorf("computing cursor offset: %w", err)
	}

	// Confirm that the cursor is inside a use statement, and then find
	// the position of the use statement's directory path.
	use, pathStart, pathEnd := usePath(pw, offset)

	// The cursor position is not on a use statement.
	if use == nil {
		return nil, nil
	}

	// Get the mod file denoted by the use.
	modfh, err := snapshot.ReadFile(ctx, modFileURI(pw, use))
	if err != nil {
		return nil, fmt.Errorf("getting modfile handle: %w", err)
	}
	pm, err := snapshot.ParseMod(ctx, modfh)
	if err != nil {
		return nil, fmt.Errorf("getting modfile handle: %w", err)
	}
	if pm.File.Module == nil {
		return nil, fmt.Errorf("modfile has no module declaration")
	}
	mod := pm.File.Module.Mod

	// Get the range to highlight for the hover.
	rng, err := pw.Mapper.OffsetRange(pathStart, pathEnd)
	if err != nil {
		return nil, err
	}
	options := snapshot.Options()
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  options.PreferredContentFormat,
			Value: mod.Path,
		},
		Range: rng,
	}, nil
}

func usePath(pw *cache.ParsedWorkFile, offset int) (use *modfile.Use, pathStart, pathEnd int) {
	for _, u := range pw.File.Use {
		path := []byte(u.Path)
		s, e := u.Syntax.Start.Byte, u.Syntax.End.Byte
		i := bytes.Index(pw.Mapper.Content[s:e], path)
		if i == -1 {
			// This should not happen.
			continue
		}
		// Shift the start position to the location of the
		// module directory within the use statement.
		pathStart, pathEnd = s+i, s+i+len(path)
		if pathStart <= offset && offset <= pathEnd {
			return u, pathStart, pathEnd
		}
	}
	return nil, 0, 0
}
