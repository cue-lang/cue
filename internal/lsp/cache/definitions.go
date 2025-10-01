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
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/definitions"
)

// Definition attempts to treat the given uri and position as a file
// coordinate to some path element that can be resolved to one or more
// ast nodes, and returns the positions of the definitions of those
// nodes.
func (w *Workspace) Definition(tokFile *token.File, fdfns *definitions.FileDefinitions, srcMapper *protocol.Mapper, pos protocol.Position) []protocol.Location {
	var targets []ast.Node
	// If DefinitionsForOffset returns no results, and if it's safe to
	// do so, we back off the Character offset (column number) by 1 and
	// try again. This can help when the caret symbol is a | and is
	// placed straight after the end of a path element.
	posAdj := []uint32{0, 1}
	if pos.Character == 0 {
		posAdj = posAdj[:1]
	}
	for _, adj := range posAdj {
		pos := pos
		pos.Character -= adj
		offset, err := srcMapper.PositionOffset(pos)
		if err != nil {
			w.debugLog(err.Error())
			continue
		}

		targets = fdfns.DefinitionsForOffset(offset)
		if len(targets) > 0 {
			break
		}
	}
	if len(targets) == 0 {
		return nil
	}

	locations := make([]protocol.Location, len(targets))
	for i, target := range targets {
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

		locations[i] = protocol.Location{
			URI:   protocol.URIFromPath(startPos.Filename),
			Range: r,
		}
	}
	return locations
}

// Hover is very similar to Definition. It attempts to treat the given
// uri and position as a file coordinate to some path element that can
// be resolved to one or more ast nodes, and returns the doc comments
// attached to those ast nodes.
func (w *Workspace) Hover(tokFile *token.File, fdfns *definitions.FileDefinitions, srcMapper *protocol.Mapper, pos protocol.Position) *protocol.Hover {
	var comments map[ast.Node][]*ast.CommentGroup
	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}

	comments = fdfns.DocCommentsForOffset(offset)
	if len(comments) == 0 {
		return nil
	}

	// We sort comments by their location: comments within the same
	// file are sorted by offset, and across different files by
	// filepath, with the exception that comments from the current file
	// come last. The thinking here is that the comments from a remote
	// file are more likely to be not-already-on-screen.
	keys := slices.Collect(maps.Keys(comments))
	slices.SortFunc(keys, func(a, b ast.Node) int {
		aPos, bPos := a.Pos().Position(), b.Pos().Position()
		switch {
		case aPos.Filename == bPos.Filename:
			return cmp.Compare(aPos.Offset, bPos.Offset)
		case aPos.Filename == tokFile.Name():
			// The current file goes last.
			return 1
		case bPos.Filename == tokFile.Name():
			// The current file goes last.
			return -1
		default:
			return cmp.Compare(aPos.Filename, bPos.Filename)
		}
	})

	// Because in CUE docs can come from several files (and indeed
	// packages), it could be confusing if we smush them all together
	// without showing any provenance. So, for each non-empty comment,
	// we add a link to that comment as a section-footer. This can help
	// provide some context for each section of docs.
	var sb strings.Builder
	for _, key := range keys {
		addLink := false
		for _, cg := range comments[key] {
			text := cg.Text()
			text = strings.TrimRight(text, "\n")
			if text == "" {
				continue
			}
			fmt.Fprintln(&sb, text)
			addLink = true
		}
		if addLink {
			pos := key.Pos().Position()
			fmt.Fprintf(&sb, "([%s line %d](%s#L%d))\n\n", filepath.Base(pos.Filename), pos.Line, protocol.URIFromPath(pos.Filename), pos.Line)
		}
	}

	docs := strings.TrimRight(sb.String(), "\n")
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: docs,
		},
	}
}

// Completion attempts to treat the given uri and position as a file
// coordinate to some path element, from which subsequent path
// elements can be suggested.
func (w *Workspace) Completion(tokFile *token.File, fdfns *definitions.FileDefinitions, srcMapper *protocol.Mapper, pos protocol.Position) *protocol.CompletionList {
	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}
	content := tokFile.Content()
	// The cursor can be after the last character of the file, hence
	// len(content), and not len(content)-1.
	offset = min(offset, len(content))

	// Use offset-1 because the cursor is always one beyond what we want.
	fields, embeds, startOffset, fieldEndOffset, embedEndOffset := fdfns.CompletionsForOffset(offset - 1)

	startOffset = min(startOffset, len(content))
	fieldEndOffset = min(fieldEndOffset, len(content))
	embedEndOffset = min(embedEndOffset, len(content))

	// According to the LSP spec, TextEdits must be on the same line as
	// offset (the cursor position), and must include offset. If we're
	// in the middle of a selector that's spread over several lines
	// (possibly accidentally), we can't perform an edit.  E.g. (with
	// the cursor position as | ):
	//
	//	x: a.|
	//	y: _
	//
	// Here, the parser will treat this as "x: a.y, _" (and raise an
	// error because it got a : where it expected a newline or ,
	// ). Completions that we offer here will want to try to replace y,
	// but the cursor is on the previous line. It's also very unlikely
	// this is what the user wants. So in this case, we just treat it
	// as a simple insert at the cursor position.
	if startOffset > offset {
		startOffset = offset
		fieldEndOffset = offset
		embedEndOffset = offset
	}

	totalLen := len(fields) + len(embeds)
	if totalLen == 0 {
		return nil
	}
	sortTextLen := len(fmt.Sprint(totalLen))

	completions := make([]protocol.CompletionItem, 0, totalLen)

	for _, cs := range []struct {
		completions   []string
		endOffset     int
		kind          protocol.CompletionItemKind
		newTextSuffix string
	}{
		{
			completions:   fields,
			endOffset:     fieldEndOffset,
			kind:          protocol.FieldCompletion,
			newTextSuffix: ":",
		},
		{
			completions: embeds,
			endOffset:   embedEndOffset,
			kind:        protocol.VariableCompletion,
		},
	} {
		if len(cs.completions) == 0 {
			continue
		}

		completionRange, rangeErr := srcMapper.OffsetRange(startOffset, cs.endOffset)
		if rangeErr != nil {
			w.debugLog(rangeErr.Error())
		}
		for _, name := range cs.completions {
			if !ast.IsValidIdent(name) {
				name = strconv.Quote(name)
			}
			item := protocol.CompletionItem{
				Label:    name,
				Kind:     cs.kind,
				SortText: fmt.Sprintf("%0*d", sortTextLen, len(completions)),
				// TODO: we can add in documentation for each item if we can
				// find it.
			}
			if rangeErr == nil {
				item.TextEdit = &protocol.TextEdit{
					Range:   completionRange,
					NewText: name + cs.newTextSuffix,
				}
			}
			completions = append(completions, item)
		}
	}

	return &protocol.CompletionList{
		Items: completions,
	}
}

func (w *Workspace) DefinitionsForUri(fileUri protocol.DocumentURI) (*token.File, *definitions.FileDefinitions, *protocol.Mapper, error) {
	mod, err := w.FindModuleForFile(fileUri)
	if err != nil {
		return nil, nil, nil, err
	}

	var dfns *definitions.Definitions

	if mod != nil {
		ip, _, _ := mod.FindImportPathForFile(fileUri)
		if ip != nil {
			pkg := mod.Package(*ip)
			if pkg != nil {
				dfns = pkg.definitions
			}
		}
	}

	if dfns == nil {
		if standalone, found := w.standalone.files[fileUri]; found {
			dfns = standalone.definitions
		} else {
			return nil, nil, nil, nil
		}
	}

	fdfns := dfns.ForFile(fileUri.Path())
	if fdfns == nil {
		return nil, nil, nil, nil
	}

	tokFile := fdfns.File.Pos().File()
	srcMapper := w.mappers[tokFile]
	if srcMapper == nil {
		w.debugLog("mapper not found: " + string(fileUri))
		return nil, nil, nil, nil
	}

	return tokFile, fdfns, srcMapper, nil
}
