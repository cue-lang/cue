// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"
	"path/filepath"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/golang"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

func (s *server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	ctx, done := event.Start(ctx, "lsp.Server.rename", tag.URI.Of(params.TextDocument.URI))
	defer done()

	fh, snapshot, release, err := s.fileOf(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	defer release()

	if kind := snapshot.FileKind(fh); kind != file.Go {
		return nil, fmt.Errorf("cannot rename in file of type %s", kind)
	}

	// Because we don't handle directory renaming within golang.Rename, golang.Rename returns
	// boolean value isPkgRenaming to determine whether an DocumentChanges of type RenameFile should
	// be added to the return protocol.WorkspaceEdit value.
	edits, isPkgRenaming, err := golang.Rename(ctx, snapshot, fh, params.Position, params.NewName)
	if err != nil {
		return nil, err
	}

	docChanges := []protocol.DocumentChanges{} // must be a slice
	for uri, e := range edits {
		fh, err := snapshot.ReadFile(ctx, uri)
		if err != nil {
			return nil, err
		}
		docChanges = append(docChanges, documentChanges(fh, e)...)
	}
	if isPkgRenaming {
		// Update the last component of the file's enclosing directory.
		oldBase := filepath.Dir(fh.URI().Path())
		newURI := filepath.Join(filepath.Dir(oldBase), params.NewName)
		docChanges = append(docChanges, protocol.DocumentChanges{
			RenameFile: &protocol.RenameFile{
				Kind:   "rename",
				OldURI: protocol.URIFromPath(oldBase),
				NewURI: protocol.URIFromPath(newURI),
			},
		})
	}
	return &protocol.WorkspaceEdit{
		DocumentChanges: docChanges,
	}, nil
}

// PrepareRename implements the textDocument/prepareRename handler. It may
// return (nil, nil) if there is no rename at the cursor position, but it is
// not desirable to display an error to the user.
//
// TODO(rfindley): why wouldn't we want to show an error to the user, if the
// user initiated a rename request at the cursor?
func (s *server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (*protocol.PrepareRenamePlaceholder, error) {
	ctx, done := event.Start(ctx, "lsp.Server.prepareRename", tag.URI.Of(params.TextDocument.URI))
	defer done()

	fh, snapshot, release, err := s.fileOf(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	defer release()

	if kind := snapshot.FileKind(fh); kind != file.Go {
		return nil, fmt.Errorf("cannot rename in file of type %s", kind)
	}

	// Do not return errors here, as it adds clutter.
	// Returning a nil result means there is not a valid rename.
	item, usererr, err := golang.PrepareRename(ctx, snapshot, fh, params.Position)
	if err != nil {
		// Return usererr here rather than err, to avoid cluttering the UI with
		// internal error details.
		return nil, usererr
	}
	return &protocol.PrepareRenamePlaceholder{
		Range:       item.Range,
		Placeholder: item.Text,
	}, nil
}
