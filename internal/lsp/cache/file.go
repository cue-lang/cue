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
	"context"
	"slices"
	"sync"

	"cuelang.org/go/cue/ast"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// ensureFile returns an existing [File] associated with uri if it
// exists, or creates a new association if not.
func (w *Workspace) ensureFile(uri protocol.DocumentURI) *File {
	w.filesMutex.Lock()
	f, found := w.files[uri]
	if !found {
		f = &File{
			workspace: w,
			uri:       uri,
		}
		w.files[uri] = f
	}
	w.filesMutex.Unlock()
	return f
}

// closeFile calls [File.close] on a [File] associated with uri. If no
// such [File] exists, it is a noop.
func (w *Workspace) closeFile(uri protocol.DocumentURI) {
	w.filesMutex.Lock()
	f, found := w.files[uri]
	w.filesMutex.Unlock()
	if !found {
		return
	}
	f.close()
}

// DocumentSymbol implements the LSP DocumentSymbols functionality.
func (w *Workspace) DocumentSymbols(fileUri protocol.DocumentURI) []protocol.DocumentSymbol {
	w.filesMutex.Lock()
	f, found := w.files[fileUri]
	w.filesMutex.Unlock()
	if found {
		return f.documentSymbols()
	}
	w.debugLogf("Document symbols requested for closed file: %v", fileUri)
	return nil
}

// publishDiagnostics sends to the client / editor all new diagnostic
// messages.
func (w *Workspace) publishDiagnostics() {
	w.filesMutex.Lock()
	for _, f := range w.files {
		f.publishErrors()
	}
	w.filesMutex.Unlock()
}

// File models a single CUE file. This file might be loaded by one or
// more packages, or might be loaded within [Standalone]. A File can
// only be deleted if it is not in use by the client / editor
// (i.e. it's closed), and no package or standalone is using it.
type File struct {
	workspace *Workspace
	uri       protocol.DocumentURI
	// isOpen records if this File is open within the client / editor.
	isOpen bool

	// syntax is the current AST for this file. Setting syntax via the
	// [File.setSyntax] method updates syntax, tokFile, mapper, and
	// symbols fields.
	syntax      *ast.File
	tokFile     *token.File
	mapper      *protocol.Mapper
	symbols     []protocol.DocumentSymbol
	tokFileLock sync.Mutex

	// errors records both the current users of this File and any
	// errors they have reported. Because most of the time there will
	// be only a single user of a File, it is modelled using a slice
	// rather than a map.
	errors []userErrors
	// dirtyErrors records if the errors for this file have changed
	// since we last published them to the client.
	dirtyErrors bool
}

type userErrors struct {
	user   packageOrModule
	errors []error
}

// ensureUser records that this File is in use by user. The errs,
// which may be nil, contains any errors which this user has
// encountered with this file and which should be reported to the
// client via diagonstic notifications.
func (f *File) ensureUser(user packageOrModule, errs ...error) {
	for i := range f.errors {
		existing := &f.errors[i]
		if existing.user != user {
			continue
		}

		f.dirtyErrors = f.dirtyErrors || len(errs) > 0 || len(existing.errors) > 0
		existing.errors = errs
		return
	}

	f.errors = append(f.errors, userErrors{
		user:   user,
		errors: errs,
	})
	f.dirtyErrors = f.dirtyErrors || len(errs) > 0
}

// removeUser records that user is no longer using this File.
func (f *File) removeUser(user packageOrModule) {
	f.errors = slices.DeleteFunc(f.errors, func(existing userErrors) bool {
		if existing.user != user {
			return false
		}
		f.dirtyErrors = f.dirtyErrors || len(existing.errors) > 0
		return true
	})
	f.maybeDelete()
}

// open marks this File as being open in the client / editor.
func (f *File) open() {
	f.isOpen = true
}

// close marks this File as not being open in the client / editor, and
// calls [File.maybeDelete].
func (f *File) close() {
	f.isOpen = false
	f.maybeDelete()
}

// maybeDelete removes this File from all the relevant collections if
// it is both not open in the client / editor, and it has no users.
func (f *File) maybeDelete() {
	if f.isOpen || len(f.errors) > 0 {
		return
	}
	w := f.workspace
	if tokFile := f.tokFile; tokFile != nil {
		delete(w.mappers, tokFile)
	}
	w.filesMutex.Lock()
	delete(w.files, f.uri)
	w.filesMutex.Unlock()
}

// setSyntax updates this state with the provided syntax. All derived
// fields (tokFile, mapper, symbols etc) are also appropriately
// updated.
func (f *File) setSyntax(syntax *ast.File) {
	w := f.workspace
	if oldTokFile := f.tokFile; oldTokFile != nil {
		delete(w.mappers, oldTokFile)
	}
	f.syntax = syntax
	tokFile := syntax.Pos().File()
	f.tokFileLock.Lock()
	f.tokFile = tokFile
	f.tokFileLock.Unlock()
	if tokFile == nil {
		f.mapper = nil
	} else {
		f.mapper = protocol.NewMapper(f.uri, tokFile.Content())
		w.mappers[tokFile] = f.mapper
	}
	f.symbols = nil
}

func (f *File) GetTokFileSafe() *token.File {
	f.tokFileLock.Lock()
	defer f.tokFileLock.Unlock()
	return f.tokFile
}

// documentSymbols returns the hierarchical document symbols for this
// file, calculating and caching them if they are currently unknown.
func (f *File) documentSymbols() []protocol.DocumentSymbol {
	if f.symbols != nil {
		return f.symbols
	}

	if f.syntax == nil || f.tokFile == nil || f.mapper == nil {
		return nil
	}

	stack := []*protocol.DocumentSymbol{{Kind: protocol.File}}

	peek := func() *protocol.DocumentSymbol {
		return stack[len(stack)-1]
	}

	push := func() *protocol.DocumentSymbol {
		parent := peek()
		i := len(parent.Children)
		parent.Children = append(parent.Children, protocol.DocumentSymbol{})
		child := &parent.Children[i]
		stack = append(stack, child)
		return child
	}

	pop := func() {
		stack = stack[:len(stack)-1]
	}

	mapper := f.mapper
	content := f.tokFile.Content()
	ast.Walk(f.syntax,
		func(n ast.Node) bool {
			if field, ok := n.(*ast.Field); ok {
				child := push()
				child.Kind = protocol.Field

				label := field.Label
				labelStartOffset, labelEndOffset := label.Pos().Offset(), label.End().Offset()
				child.Name = string(content[labelStartOffset:labelEndOffset])

				fieldStartOffset, fieldEndOffset := field.Pos().Offset(), field.End().Offset()
				var err error
				child.Range, err = mapper.OffsetRange(fieldStartOffset, fieldEndOffset)
				if err != nil {
					return false
				}
				child.SelectionRange, err = mapper.OffsetRange(labelStartOffset, labelEndOffset)
				if err != nil {
					return false
				}
			}
			return true
		},
		func(n ast.Node) {
			if _, ok := n.(*ast.Field); ok {
				pop()
			}
		})

	f.symbols = peek().Children
	return f.symbols
}

// tokenRangeOffsets searches through the file's AST for the range of
// the smallest token which contains the offset. In some cases there
// is no such token, for example some of the errors from the parser
// contain an offset which does not correspond to any token in the
// AST. In this case, the range of the first token beyond offset is
// returned. Failing that, the range of the entire file is returned.
func (f *File) tokenRangeOffsets(offset int) (start, end int) {
	start = f.syntax.Pos().Offset()
	end = f.syntax.End().Offset()
	if !(start <= offset && offset < end) {
		return
	}

	closestStart, closestEnd := end, end
	shrunkStartEnd := false
	ast.Walk(f.syntax, func(n ast.Node) bool {
		startOffset := n.Pos().Offset()
		endOffset := n.End().Offset()

		if startOffset > offset && (startOffset-offset < closestStart-offset) {
			closestStart, closestEnd = startOffset, endOffset
		}

		if startOffset <= offset && offset < endOffset {
			shrunkStartEnd = shrunkStartEnd || (endOffset-startOffset) < (end-start)
			start, end = startOffset, endOffset
			return true
		}

		return false
	}, nil)

	if !shrunkStartEnd && closestStart < closestEnd {
		return closestStart, closestEnd
	}

	return start, end
}

// publishErrors sends PublishDiagnostics notifications to the client
// if this File is open and has changed its errors since last
// publication.
func (f *File) publishErrors() {
	if !f.isOpen || !f.dirtyErrors || f.tokFile == nil || f.mapper == nil {
		return
	}
	f.dirtyErrors = false
	// must not be nil!
	diags := []protocol.Diagnostic{}
	for _, errs := range f.errors {
		for _, err := range errs.errors {
			diags = f.errorToDiagnostics(err, diags)
		}
	}

	params := &protocol.PublishDiagnosticsParams{
		URI:         f.uri,
		Version:     f.tokFile.Revision(),
		Diagnostics: diags,
	}
	f.workspace.client.PublishDiagnostics(context.Background(), params)
}

// errorToDiagnostics converts cue errors to [protocol.Diagnostic]
// messages. It will extract positions from the errors, check they
// belong to this File, find corresponding ranges within this File,
// and append new Diagnostics to the provided accumulator.
func (f *File) errorToDiagnostics(err error, acc []protocol.Diagnostic) []protocol.Diagnostic {
	err, ok := err.(cueerrors.Error)
	if !ok {
		return acc
	}

	for _, e := range cueerrors.Errors(err) {
		errPos := e.Position()
		if !errPos.IsValid() {
			continue
		}

		if protocol.DocumentURI(protocol.URIFromPath(errPos.Filename())) != f.uri {
			// This error is for a different file, skip it
			continue
		}

		startOffset, endOffset := f.tokenRangeOffsets(errPos.Offset())
		r, err := f.mapper.OffsetRange(startOffset, endOffset)
		if err != nil {
			continue
		}

		diag := protocol.Diagnostic{
			Range:    r,
			Severity: protocol.SeverityError,
			Source:   "cue",
			Message:  e.Error(),
		}

		acc = append(acc, diag)
	}

	return acc
}
