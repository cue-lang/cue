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

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/extvalidator"
)

// ensureFile returns an existing [File] associated with uri if it
// exists, or creates a new association if not.
func (w *Workspace) ensureFile(uri protocol.DocumentURI) *File {
	f, found := w.files[uri]
	if !found {
		f = &File{
			workspace: w,
			uri:       uri,
		}
		// Unfortunately this is repeating work done in fscache, but due
		// to import cycles, there's no way we can attach the build.File
		// to the token.File (or ast.File). TODO - solve cycle somehow?
		bf, _ := filetypes.ParseFileAndType(uri.FilePath(), "", filetypes.Input)
		f.buildFile = bf

		w.files[uri] = f
	}
	return f
}

// closeFile calls [File.close] on a [File] associated with uri. If no
// such [File] exists, it is a noop.
func (w *Workspace) closeFile(uri protocol.DocumentURI) {
	f, found := w.files[uri]
	if !found {
		return
	}
	f.close()
}

// GetFile returns the [File] associated with uri if any. If no such
// [File] exists, it returns nil.
func (w *Workspace) GetFile(uri protocol.DocumentURI) *File {
	return w.files[uri]
}

// DocumentSymbol implements the LSP DocumentSymbols functionality.
func (w *Workspace) DocumentSymbols(fileUri protocol.DocumentURI) []protocol.DocumentSymbol {
	if f, found := w.files[fileUri]; found {
		return f.documentSymbols()
	}
	w.debugLogf("Document symbols requested for closed file: %v", fileUri)
	return nil
}

// publishDiagnostics sends to the client / editor all new diagnostic
// messages.
func (w *Workspace) publishDiagnostics(clearOnly bool) bool {
	errorsFound := false
	for _, f := range w.files {
		errorsFound = f.publishErrors(clearOnly) || errorsFound
	}
	return errorsFound
}

// File models a single CUE file. This file might be loaded by one or
// more packages, or might be loaded within [Standalone]. A File can
// only be deleted if it is not in use by the client / editor
// (i.e. it's closed), and no package or standalone is using it.
type File struct {
	workspace *Workspace
	uri       protocol.DocumentURI
	buildFile *build.File
	// isOpen records if this File is open within the client / editor.
	isOpen bool

	// syntax is the current AST for this file. Setting syntax via the
	// [File.setSyntax] method updates syntax, tokFile, mapper, and
	// symbols fields.
	syntax  *ast.File
	tokFile *token.File
	mapper  *protocol.Mapper
	symbols []protocol.DocumentSymbol

	extValidator *extvalidator.Validator

	// errors records both the current users of this File and any
	// errors they have reported. Because most of the time there will
	// be only a single user of a File, it is modelled using a slice
	// rather than a map.
	errors []userErrors
	// dirtyErrors records if the errors for this file have changed
	// since we last published them to the client.
	dirtyErrors bool
	// hasPublishedDiags records whether the most recent
	// PublishDiagnostics notification sent to the client for this
	// file contained a non-empty set of diagnostics. The client
	// retains diagnostics until they are explicitly cleared, so if
	// this is true, the client must be sent updates (or a clear)
	// even if this file is no longer open.
	hasPublishedDiags bool
}

type userErrors struct {
	user   any
	errors []error
}

// ensureUser records that this File is in use by user. The errs,
// which may be nil, contains any errors which this user has
// encountered with this file and which should be reported to the
// client via diagonstic notifications.
//
// user can be any value of any type. It is simply a handle which can
// be reliably used to update or remove errors encountered by the same
// user.
func (f *File) ensureUser(user any, errs ...error) {
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
func (f *File) removeUser(user any) {
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
	if f.hasPublishedDiags {
		// The client still holds diagnostics for this file. We are
		// about to lose all record of the file, so clear them now,
		// otherwise the client shows them indefinitely.
		f.hasPublishedDiags = false
		params := &protocol.PublishDiagnosticsParams{
			URI:         f.uri,
			Diagnostics: []protocol.Diagnostic{},
		}
		if f.tokFile != nil {
			params.Version = f.tokFile.Revision()
		}
		w.enqueue(func() {
			w.client.PublishDiagnostics(context.Background(), params)
		})
	}
	if tokFile := f.tokFile; tokFile != nil {
		delete(w.mappers, tokFile)
	}
	delete(w.files, f.uri)
}

// setSyntax updates this state with the provided syntax. All derived
// fields (tokFile, mapper, symbols etc) are also appropriately
// updated.
//
// syntax may be nil, when the file's current content cannot be
// parsed at all. In that case, content (which is otherwise ignored)
// is used to build the file's mapper, so that errors can still be
// converted to diagnostics for this file.
func (f *File) setSyntax(syntax *ast.File, content []byte) {
	w := f.workspace
	if oldTokFile := f.tokFile; oldTokFile != nil {
		delete(w.mappers, oldTokFile)
	}
	f.syntax = syntax
	var tokFile *token.File
	if syntax != nil {
		tokFile = syntax.Pos().File()
	}
	f.tokFile = tokFile
	if tokFile != nil {
		content = tokFile.Content()
	}
	if f.buildFile != nil {
		f.buildFile.Source = content
	}
	if content == nil {
		f.mapper = nil
	} else {
		f.mapper = protocol.NewMapper(f.uri, content)
		if tokFile != nil {
			w.mappers[tokFile] = f.mapper
		}
	}
	f.symbols = nil
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
			field, ok := n.(*ast.Field)
			if !ok {
				return true
			}
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
			return err == nil
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
//
// If clearOnly is true, diagnostics will only be sent if there are no
// errors - i.e. we're clearing old errors only and have no new errors
// to send. If clearOnly is true and the file does contain errors then
// true is returned, to indicate errors were found. In all other
// cases, false is returned.
func (f *File) publishErrors(clearOnly bool) bool {
	if !f.dirtyErrors || f.mapper == nil {
		return false
	}
	if !f.isOpen && !f.hasPublishedDiags {
		// The file is not open, and the client holds no diagnostics
		// for it: there is nothing to publish or clear.
		return false
	}

	// must not be nil!
	diags := []protocol.Diagnostic{}
	for _, errs := range f.errors {
		for _, err := range errs.errors {
			diags = f.errorToDiagnostics(err, diags)
		}
	}
	if len(diags) > 0 && clearOnly {
		// we have real errors to send, and we're not allowed to send them
		return true
	}

	f.dirtyErrors = false
	f.hasPublishedDiags = len(diags) > 0
	params := &protocol.PublishDiagnosticsParams{
		URI:         f.uri,
		Diagnostics: diags,
	}
	if f.tokFile != nil {
		params.Version = f.tokFile.Revision()
	}
	w := f.workspace
	w.enqueue(func() {
		w.client.PublishDiagnostics(context.Background(), params)
	})
	return false
}

// errorToDiagnostics converts cue errors to [protocol.Diagnostic]
// messages. It will extract positions from the errors, check they
// belong to this File, find corresponding ranges within this File,
// and append new Diagnostics to the provided accumulator.
//
// An error which carries no position information at all (it is not a
// [cueerrors.Error]) is reported at the start of the file: it was
// recorded against this File by one of its users, so it concerns
// this file even though it cannot be located within it. For example,
// the errors produced when extracting YAML are not cue errors.
func (f *File) errorToDiagnostics(err error, acc []protocol.Diagnostic) []protocol.Diagnostic {
	cueErr, ok := err.(cueerrors.Error)
	if !ok {
		r, rErr := f.mapper.OffsetRange(0, 0)
		if rErr != nil {
			return acc
		}
		return append(acc, protocol.Diagnostic{
			Range:    r,
			Severity: protocol.SeverityError,
			Source:   "cue",
			Message:  err.Error(),
		})
	}

	for _, e := range cueerrors.Errors(cueErr) {
		errPos := e.Position()
		if !errPos.IsValid() {
			continue
		}

		if protocol.DocumentURI(protocol.URIFromPath(errPos.Filename())) != f.uri {
			// This error is for a different file, skip it
			continue
		}

		startOffset, endOffset := errPos.Offset(), errPos.Offset()
		if f.syntax != nil {
			startOffset, endOffset = f.tokenRangeOffsets(errPos.Offset())
		}
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
