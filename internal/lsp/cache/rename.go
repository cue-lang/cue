// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/definitions"
)

// Rename implements the LSP Rename functionality.
func (w *Workspace) Rename(tokFile *token.File, fdfns *definitions.FileDefinitions, srcMapper *protocol.Mapper, params *protocol.RenameParams) *protocol.WorkspaceEdit {
	var targets []ast.Node
	for offset, err := range adjustedPositionsIter(params.Position, srcMapper) {
		if err != nil {
			w.debugLog(err.Error())
			continue
		}

		targets = fdfns.UsagesForOffset(offset, true)
		if len(targets) > 0 {
			break
		}
	}

	type versionedEdits struct {
		edits   []protocol.TextEdit
		version int32
	}

	changes := make(map[protocol.DocumentURI]*versionedEdits)
	for _, target := range targets {
		startPos := target.Pos().Position()
		endPos := target.End().Position()

		targetFile := target.Pos().File()
		targetMapper := w.mappers[targetFile]
		if targetMapper == nil {
			w.debugLog("mapper not found: " + targetFile.Name())
			return nil
		}
		r, err := targetMapper.OffsetRange(startPos.Offset, endPos.Offset)
		if err != nil {
			w.debugLog(err.Error())
			return nil
		}
		uri := targetMapper.URI
		name := params.NewName
		if lit, ok := target.(*ast.BasicLit); (ok && lit.Kind == token.STRING) || !ast.IsValidIdent(name) {
			name = literal.Label.Quote(name)
		}
		ve, found := changes[uri]
		if !found {
			ve = &versionedEdits{
				version: targetFile.Version(),
			}
			changes[uri] = ve
		}
		ve.edits = append(ve.edits, protocol.TextEdit{
			Range:   r,
			NewText: name,
		})
	}

	if len(changes) == 0 {
		return nil
	}

	var docChanges []protocol.DocumentChanges
	for uri, edits := range changes {
		docChanges = append(docChanges, protocol.TextEditsToDocumentChanges(uri, edits.version, edits.edits)...)
	}
	return &protocol.WorkspaceEdit{DocumentChanges: docChanges}
}

// Rename implements the LSP PrepareRename functionality.
func (w *Workspace) PrepareRename(tokFile *token.File, fdfns *definitions.FileDefinitions, srcMapper *protocol.Mapper, pos protocol.Position) *protocol.PrepareRenamePlaceholder {
	var targets []ast.Node
	for offset, err := range adjustedPositionsIter(pos, srcMapper) {
		if err != nil {
			w.debugLog(err.Error())
			continue
		}

		targets = fdfns.UsagesForOffset(offset, true)
		if len(targets) > 0 {
			break
		}
	}

	// The client (editor) has provided us with a single position
	// within a file, but it wants back a range so that it can fully
	// highlight the token that's going to get renamed. So from the
	// usages (and definitions), search for the one which contains pos
	// and return its range.
	posRange := protocol.Range{Start: pos, End: pos}
	for _, target := range targets {
		targetFile := target.Pos().File()
		if targetFile != tokFile {
			continue
		}

		startPos := target.Pos().Position()
		endPos := target.End().Position()

		r, err := srcMapper.OffsetRange(startPos.Offset, endPos.Offset)
		if err != nil {
			w.debugLog(err.Error())
			continue
		}
		if protocol.Intersect(r, posRange) {
			return &protocol.PrepareRenamePlaceholder{
				Range: r,
			}
		}
	}
	return nil
}
