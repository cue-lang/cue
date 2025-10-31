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
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
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

// DocumentSymbol implements the LSP DocumentSymbols functionality.
func (w *Workspace) DocumentSymbols(fileUri protocol.DocumentURI) []protocol.DocumentSymbol {
	if f, found := w.files[fileUri]; found {
		return f.documentSymbols()
	}
	w.debugLogf("Document symbols requested for closed file: %v", fileUri)
	return nil
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
	syntax  *ast.File
	tokFile *token.File
	mapper  *protocol.Mapper
	symbols []protocol.DocumentSymbol

	// the current users of this File.
	users []packageOrModule
}

// ensureUser records that this File is in use by user.
func (f *File) ensureUser(user packageOrModule) {
	if slices.Contains(f.users, user) {
		return
	}
	f.users = append(f.users, user)
}

// removeUser records that user is no longer using this File.
func (f *File) removeUser(user packageOrModule) {
	f.users = slices.DeleteFunc(f.users, func(u packageOrModule) bool {
		return u == user
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
	if f.isOpen || len(f.users) > 0 {
		return
	}
	w := f.workspace
	if tokFile := f.tokFile; tokFile != nil {
		delete(w.mappers, tokFile)
	}
	delete(w.files, f.uri)
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
	f.tokFile = tokFile
	if tokFile == nil {
		f.mapper = nil
	} else {
		f.mapper = protocol.NewMapper(f.uri, tokFile.Content())
		w.mappers[tokFile] = f.mapper
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
