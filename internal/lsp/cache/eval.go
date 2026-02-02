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
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/eval"
)

// Definition attempts to resolve the given position, within the file
// definitions, to one or more ast nodes, and returns the positions of
// the definitions of those nodes.
func (w *Workspace) Definition(file *File, fe *eval.FileEvaluator, srcMapper *protocol.Mapper, pos protocol.Position) []protocol.Location {
	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}

	targets := fe.DefinitionsForOffset(offset)
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

	// Although not required by the LSP spec, general advice seems to
	// be that results from the file from which the query was made
	// should come first; everything else follows, sorted by filename
	// then offset.
	slices.SortFunc(locations, func(a, b protocol.Location) int {
		return rangeStartCompare(srcMapper.URI, a, b)
	})

	return locations
}

func (w *Workspace) References(file *File, fe *eval.FileEvaluator, srcMapper *protocol.Mapper, params *protocol.ReferenceParams) []protocol.Location {
	// If UsagesForOffset returns no results, and if it's safe to
	// do so, we back off the Character offset (column number) by 1 and
	// try again. This can help when the caret symbol is a | (as
	// opposed to a block - i.e. it's *between* two characters rather
	// than *over* a single character) and is placed straight after the
	// end of a path element.
	offset, err := srcMapper.PositionOffset(params.Position)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}

	targets := fe.UsagesForOffset(offset, params.Context.IncludeDeclaration)
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

	// Although not required by the LSP spec, general advice seems to
	// be that results from the file from which the query was made
	// should come first; everything else follows, sorted by filename
	// then offset.
	slices.SortFunc(locations, func(a, b protocol.Location) int {
		return rangeStartCompare(srcMapper.URI, a, b)
	})
	return locations
}

// Hover is very similar to Definition. It attempts to resolve the
// given position, within the file definitions, to one or more ast
// nodes, and returns the doc comments attached to those ast nodes.
func (w *Workspace) Hover(file *File, fe *eval.FileEvaluator, srcMapper *protocol.Mapper, pos protocol.Position) *protocol.Hover {
	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}

	comments := fe.DocCommentsForOffset(offset)
	if len(comments) == 0 {
		return nil
	}

	tokFile := file.tokFile
	// We sort comments by their location: comments within the same
	// file are sorted by offset, and across different files by
	// filepath, with the exception that comments from the current file
	// come last. The thinking here is that the comments from a remote
	// file are more likely to be not-already-on-screen.
	keys := slices.Collect(maps.Keys(comments))
	slices.SortFunc(keys, func(a, b ast.Node) int {
		aPos, bPos := a.Pos().Position(), b.Pos().Position()
		switch c := cmp.Compare(aPos.Filename, bPos.Filename); {
		case c == 0:
			return cmp.Compare(aPos.Offset, bPos.Offset)
		case aPos.Filename == tokFile.Name():
			// The current file goes last.
			return 1
		case bPos.Filename == tokFile.Name():
			// The current file goes last.
			return -1
		default:
			return c
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

// Completion attempts to resolve the given position, within the file
// definitions, from which subsequent path elements can be suggested.
func (w *Workspace) Completion(file *File, fe *eval.FileEvaluator, srcMapper *protocol.Mapper, pos protocol.Position) *protocol.CompletionList {
	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}
	content := file.tokFile.Content()
	// The cursor can be after the last character of the file, hence
	// len(content), and not len(content)-1.
	offset = min(offset, len(content))

	completions := fe.CompletionsForOffset(offset)

	var completionItems []protocol.CompletionItem

	for completion, names := range completions {
		if len(names) == 0 {
			continue
		}

		completionRange, rangeErr := srcMapper.OffsetRange(completion.Start, completion.End)
		if rangeErr != nil {
			w.debugLog(rangeErr.Error())
		}

		// According to the LSP spec, TextEdits must be entirely on the
		// same line as offset (the cursor position), and must include
		// offset. If we're in the middle of a selector that's spread
		// over several lines (possibly accidentally), we can't perform
		// an edit.  E.g. (with the cursor position as | ):
		//
		//	x: a.|
		//	y: _
		//
		// Here, the parser will treat this as "x: a.y, _" (and raise an
		// error because it got a : where it expected a newline or ,
		// ). Completions that we offer here will want to try to replace
		// y, but the cursor is on the previous line. It's also very
		// unlikely this is what the user wants. So in this case, we
		// just treat it as a simple insert at the cursor position.

		// Testing shows, and general advice seems to be, that the
		// completions should have their range finish at the current
		// cursor position. This is not ideal as it makes replacing
		// existing tokens won't quite work correctly, but not trimming
		// this End causes problematic behaviour in popular editors.
		//
		// TODO: it's possible other fields with protocol.CompletionItem
		// can be used to get the behaviour we really want here.
		completionRange.End = pos

		for name := range names {
			if !ast.IsValidIdent(name) {
				name = strconv.Quote(name)
			}
			item := protocol.CompletionItem{
				Label: name,
				Kind:  completion.Kind,
			}
			if rangeErr == nil {
				item.TextEdit = &protocol.TextEdit{
					Range:   completionRange,
					NewText: name + completion.Suffix,
				}
			}
			completionItems = append(completionItems, item)
		}
	}

	slices.SortFunc(completionItems, func(a, b protocol.CompletionItem) int {
		if c := cmp.Compare(a.Label, b.Label); c != 0 {
			return c
		}
		return cmp.Compare(a.Kind, b.Kind)
	})

	return &protocol.CompletionList{
		Items: completionItems,
	}
}

func (w *Workspace) FileEvaluatorForURI(fileUri protocol.DocumentURI, loadAllPkgsInMod bool) (*File, *eval.FileEvaluator, *protocol.Mapper, error) {
	mod, err := w.FindModuleForFile(fileUri)
	if err != nil && err != errModuleNotFound {
		return nil, nil, nil, err
	}

	var e *eval.Evaluator

	if mod != nil {
		if loadAllPkgsInMod {
			mod.loadAllCuePackages()
		}
		ip, _, _ := mod.FindImportPathForFile(fileUri)
		if ip != nil {
			pkg := mod.Package(*ip)
			if pkg != nil {
				e = pkg.eval
			}
		}
	}

	if e == nil {
		if standalone, found := w.standalone.files[fileUri]; found {
			e = standalone.definitions
		} else {
			return nil, nil, nil, nil
		}
	}

	fe := e.ForFile(fileUri.Path())
	if fe == nil {
		return nil, nil, nil, nil
	}

	file := w.GetFile(fileUri)
	if file == nil {
		w.debugLog("file not found: " + string(fileUri))
		return nil, nil, nil, nil
	}
	srcMapper := w.mappers[file.tokFile]
	if srcMapper == nil {
		w.debugLog("mapper not found: " + string(fileUri))
		return nil, nil, nil, nil
	}

	return file, fe, srcMapper, nil
}

// rangeStartCompare compares a and b.
//
// * If they both come from the same uri, then they are ordered by
// their range starts.
// * Otherwise which ever comes from srcUri comes first.
// * Otherwise they're sorted by uri.
func rangeStartCompare(srcUri protocol.DocumentURI, a, b protocol.Location) int {
	switch c := cmp.Compare(a.URI, b.URI); {
	case c == 0:
		aStart, bStart := a.Range.Start, b.Range.Start
		if aStart.Line < bStart.Line || (aStart.Line == bStart.Line && aStart.Character < bStart.Character) {
			return -1
		}
		return 1
	case a.URI == srcUri:
		return -1
	case b.URI == srcUri:
		return 1
	default:
		return c
	}
}
