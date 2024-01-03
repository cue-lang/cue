// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parsego

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/protocol"
)

// A File contains the results of parsing a Go file.
type File struct {
	URI     protocol.DocumentURI
	Options []parser.Option
	File    *ast.File
	Tok     *token.File
	// Source code used to build the AST. It may be different from the
	// actual content of the file if we have fixed the AST.
	Src []byte

	// Mapper is a mapping wrapper around Src
	Mapper *protocol.Mapper

	ParseErr error
}

// Fixed reports whether p was "Fixed", meaning that its source or positions
// may not correlate with the original file.
func (p File) Fixed() bool {
	return false
}

// -- go/token domain convenience helpers --

// PositionPos returns the token.Pos of protocol position p within the file.
func (pgf *File) PositionPos(p protocol.Position) (token.Pos, error) {
	offset, err := pgf.Mapper.PositionOffset(p)
	if err != nil {
		return token.NoPos, err
	}
	return pgf.Tok.Pos(offset, 0), nil
}

// PosRange returns a protocol Range for the token.Pos interval in this file.
func (pgf *File) PosRange(start, end token.Pos) (protocol.Range, error) {
	return pgf.Mapper.PosRangeCUE(pgf.Tok, start, end)
}

// PosMappedRange returns a MappedRange for the token.Pos interval in this file.
// A MappedRange can be converted to any other form.
func (pgf *File) PosMappedRange(start, end token.Pos) (protocol.MappedRange, error) {
	return pgf.Mapper.PosMappedRangeCUE(pgf.Tok, start, end)
}

// PosLocation returns a protocol Location for the token.Pos interval in this file.
func (pgf *File) PosLocation(start, end token.Pos) (protocol.Location, error) {
	return pgf.Mapper.PosLocationCUE(pgf.Tok, start, end)
}

// NodeRange returns a protocol Range for the ast.Node interval in this file.
func (pgf *File) NodeRange(node ast.Node) (protocol.Range, error) {
	return pgf.Mapper.NodeRangeCUE(pgf.Tok, node)
}

// NodeMappedRange returns a MappedRange for the ast.Node interval in this file.
// A MappedRange can be converted to any other form.
func (pgf *File) NodeMappedRange(node ast.Node) (protocol.MappedRange, error) {
	return pgf.Mapper.NodeMappedRangeCUE(pgf.Tok, node)
}

// NodeLocation returns a protocol Location for the ast.Node interval in this file.
func (pgf *File) NodeLocation(node ast.Node) (protocol.Location, error) {
	return pgf.Mapper.PosLocationCUE(pgf.Tok, node.Pos(), node.End())
}

// RangePos parses a protocol Range back into the go/token domain.
func (pgf *File) RangePos(r protocol.Range) (token.Pos, token.Pos, error) {
	start, end, err := pgf.Mapper.RangeOffsets(r)
	if err != nil {
		return token.NoPos, token.NoPos, err
	}
	return pgf.Tok.Pos(start, 0), pgf.Tok.Pos(end, 0), nil
}
