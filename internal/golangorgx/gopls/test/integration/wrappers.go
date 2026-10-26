// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"encoding/json"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
)

// RemoveWorkspaceFile deletes a file on disk but does nothing in the
// editor. It calls t.Fatal on any error.
func (e *Env) RemoveWorkspaceFile(name string) {
	e.T.Helper()
	if err := e.Sandbox.Workdir.RemoveFile(e.Ctx, name); err != nil {
		e.T.Fatal(err)
	}
}

// WriteWorkspaceFile writes a file to disk but does nothing in the editor.
// It calls t.Fatal on any error.
func (e *Env) WriteWorkspaceFile(name, content string) {
	e.T.Helper()
	if err := e.Sandbox.Workdir.WriteFile(e.Ctx, name, content); err != nil {
		e.T.Fatal(err)
	}
}

// OpenFile opens a file in the editor, calling t.Fatal on any error.
func (e *Env) OpenFile(name string) {
	e.T.Helper()
	if err := e.Editor.OpenFile(e.Ctx, name); err != nil {
		e.T.Fatal(err)
	}
}

// CreateBuffer creates a buffer in the editor, calling t.Fatal on any error.
func (e *Env) CreateBuffer(name string, content string) {
	e.T.Helper()
	if err := e.Editor.CreateBuffer(e.Ctx, name, content); err != nil {
		e.T.Fatal(err)
	}
}

// BufferText returns the current buffer contents for the file with the given
// relative path, calling t.Fatal if the file is not open in a buffer.
func (e *Env) BufferText(name string) string {
	e.T.Helper()
	text, ok := e.Editor.BufferText(name)
	if !ok {
		e.T.Fatalf("buffer %q is not open", name)
	}
	return text
}

// CloseBuffer closes an editor buffer without saving, calling t.Fatal on any
// error.
func (e *Env) CloseBuffer(name string) {
	e.T.Helper()
	if err := e.Editor.CloseBuffer(e.Ctx, name); err != nil {
		e.T.Fatal(err)
	}
}

// EditBuffer applies edits to an editor buffer, calling t.Fatal on any error.
func (e *Env) EditBuffer(name string, edits ...protocol.TextEdit) {
	e.T.Helper()
	if err := e.Editor.EditBuffer(e.Ctx, name, edits); err != nil {
		e.T.Fatal(err)
	}
}

func (e *Env) SetBufferContent(name string, content string) {
	e.T.Helper()
	if err := e.Editor.SetBufferContent(e.Ctx, name, content); err != nil {
		e.T.Fatal(err)
	}
}

// RegexpSearch returns the starting position of the first match for re in the
// buffer specified by name, calling t.Fatal on any error. It first searches
// for the position in open buffers, then in workspace files.
func (e *Env) RegexpSearch(name, re string) protocol.Location {
	e.T.Helper()
	loc, err := e.Editor.RegexpSearch(name, re)
	if err == fake.ErrUnknownBuffer {
		loc, err = e.Sandbox.Workdir.RegexpSearch(name, re)
	}
	if err != nil {
		e.T.Fatalf("RegexpSearch: %v, %v for %q", name, err, re)
	}
	return loc
}

// RegexpReplace replaces the first group in the first match of regexpStr with
// the replace text, calling t.Fatal on any error.
func (e *Env) RegexpReplace(name, regexpStr, replace string) {
	e.T.Helper()
	if err := e.Editor.RegexpReplace(e.Ctx, name, regexpStr, replace); err != nil {
		e.T.Fatalf("RegexpReplace: %v", err)
	}
}

// SaveBuffer saves an editor buffer, calling t.Fatal on any error.
func (e *Env) SaveBuffer(name string) {
	e.T.Helper()
	if err := e.Editor.SaveBuffer(e.Ctx, name); err != nil {
		e.T.Fatal(err)
	}
}

// Definition goes to definitions in the editor, calling t.Fatal on any
// error. It returns the paths and positions of the resulting jump.
func (e *Env) Definition(loc protocol.Location) []protocol.Location {
	e.T.Helper()
	locs, err := e.Editor.Definition(e.Ctx, loc)
	if err != nil {
		e.T.Fatal(err)
	}
	return locs
}

// FormatBuffer formats the editor buffer, calling t.Fatal on any error.
func (e *Env) FormatBuffer(name string) {
	e.T.Helper()
	if err := e.Editor.FormatBuffer(e.Ctx, name); err != nil {
		e.T.Fatal(err)
	}
}

// ApplyCodeAction applies the given code action.
func (e *Env) ApplyCodeAction(action protocol.CodeAction) {
	e.T.Helper()
	if err := e.Editor.ApplyCodeAction(e.Ctx, action); err != nil {
		e.T.Fatal(err)
	}
}

// Hover in the editor, calling t.Fatal on any error.
func (e *Env) Hover(loc protocol.Location) (*protocol.MarkupContent, protocol.Location) {
	e.T.Helper()
	c, loc, err := e.Editor.Hover(e.Ctx, loc)
	if err != nil {
		e.T.Fatal(err)
	}
	return c, loc
}

// CheckForFileChanges triggers a manual poll of the workspace for any file
// changes since creation, or since last polling. It is a workaround for the
// lack of true file watching support in the fake workspace.
func (e *Env) CheckForFileChanges() {
	e.T.Helper()
	if err := e.Sandbox.Workdir.CheckForFileChanges(e.Ctx); err != nil {
		e.T.Fatal(err)
	}
}

// CodeLens calls textDocument/codeLens for the given path, calling t.Fatal on
// any error.
func (e *Env) CodeLens(path string) []protocol.CodeLens {
	e.T.Helper()
	lens, err := e.Editor.CodeLens(e.Ctx, path)
	if err != nil {
		e.T.Fatal(err)
	}
	return lens
}

func (e *Env) ExecuteCommand(params *protocol.ExecuteCommandParams, result interface{}) {
	e.T.Helper()
	response, err := e.Editor.ExecuteCommand(e.Ctx, params)
	if err != nil {
		e.T.Fatal(err)
	}
	if result == nil {
		return
	}
	// Hack: The result of an executeCommand request will be unmarshaled into
	// maps. Re-marshal and unmarshal into the type we expect.
	//
	// This could be improved by generating a jsonrpc2 command client from the
	// command.Interface, but that should only be done if we're consolidating
	// this part of the tsprotocol generation.
	data, err := json.Marshal(response)
	if err != nil {
		e.T.Fatal(err)
	}
	if err := json.Unmarshal(data, result); err != nil {
		e.T.Fatal(err)
	}
}

// References wraps Editor.References, calling t.Fatal on any error.
func (e *Env) References(loc protocol.Location) []protocol.Location {
	e.T.Helper()
	locations, err := e.Editor.References(e.Ctx, loc)
	if err != nil {
		e.T.Fatal(err)
	}
	return locations
}

// Rename wraps Editor.Rename, calling t.Fatal on any error.
func (e *Env) Rename(loc protocol.Location, newName string) {
	e.T.Helper()
	if err := e.Editor.Rename(e.Ctx, loc, newName); err != nil {
		e.T.Fatal(err)
	}
}

// Completion executes a completion request on the server.
func (e *Env) Completion(loc protocol.Location) *protocol.CompletionList {
	e.T.Helper()
	completions, err := e.Editor.Completion(e.Ctx, loc)
	if err != nil {
		e.T.Fatal(err)
	}
	return completions
}

// ChangeConfiguration updates the editor config, calling t.Fatal on any error.
func (e *Env) ChangeConfiguration(newConfig fake.EditorConfig) {
	e.T.Helper()
	if err := e.Editor.ChangeConfiguration(e.Ctx, newConfig); err != nil {
		e.T.Fatal(err)
	}
}

// ChangeWorkspaceFolders updates the editor workspace folders, calling t.Fatal
// on any error.
func (e *Env) ChangeWorkspaceFolders(newFolders ...string) {
	e.T.Helper()
	if err := e.Editor.ChangeWorkspaceFolders(e.Ctx, newFolders); err != nil {
		e.T.Fatal(err)
	}
}
