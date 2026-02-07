// Copyright 2026 The CUE Authors
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
	"bytes"
	"context"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/diff"
)

func (w *Workspace) CodeActionConvertToStruct(ctx context.Context, params *protocol.CodeActionParams) (*protocol.WorkspaceEdit, error) {
	f := w.GetFile(params.TextDocument.URI)
	if f == nil || f.syntax == nil || f.mapper == nil || f.tokFile == nil {
		return nil, nil
	}

	offset, _, err := f.mapper.RangeOffsets(params.Range)
	if err != nil {
		return nil, err
	}

	var structLit *ast.StructLit
	var field *ast.Field
	var label ast.Label

	ast.Walk(f.syntax,
		// Walk over the AST, traversing into whatever contains the cursor position.
		func(node ast.Node) bool {
			start := node.Pos()
			end := node.End()

			if !start.HasAbsPos() || !end.HasAbsPos() {
				return false
			}
			if !(start.Offset() <= offset && offset <= end.Offset()) {
				return false
			}
			return true
		},

		// We want the inner-most matching field, so on the way back
		// out, capture the first suitable structlit+field+label.
		func(node ast.Node) {
			if structLit != nil {
				return
			}

			sl, ok := node.(*ast.StructLit)
			if !ok || sl.Lbrace.HasAbsPos() || sl.Rbrace.HasAbsPos() || len(sl.Elts) != 1 {
				return
			}

			f, ok := sl.Elts[0].(*ast.Field)
			if !ok {
				return
			}
			l := f.Label
			start := l.Pos()
			end := l.End()

			if !start.HasAbsPos() || !end.HasAbsPos() {
				return
			}
			if !(start.Offset() <= offset && offset <= end.Offset() && start.Line() == sl.Pos().Line()) {
				return
			}

			structLit = sl
			field = f
			label = l
		})

	if structLit == nil {
		return nil, nil
	}

	content := f.tokFile.Content()
	lineStartOffsets := f.tokFile.Lines() // Lines is 0-based
	numLines := len(lineStartOffsets)
	labelStart := label.Pos()
	lineNo := labelStart.Line() - 1 // Line is 1-based, hence the -1
	if lineNo < 0 || lineNo >= len(lineStartOffsets) {
		return nil, nil
	}

	lineEnding := "\n"
	if numLines > 1 {
		if content[lineStartOffsets[1]-2] == '\r' {
			lineEnding = "\r\n"
		}
	}

	var indent []byte
	lineStartOffset := lineStartOffsets[lineNo]
	for i := lineStartOffset; i < len(content); i++ {
		c := content[i]
		if c != ' ' && c != '\t' {
			indent = content[lineStartOffset:i]
			break
		}
	}

	var contentBuilder strings.Builder
	contentBuilder.Write(content[:labelStart.Offset()])

	contentBuilder.WriteString("{")
	contentBuilder.WriteString(lineEnding)

	fieldEndOffset := field.End().Offset()
	fieldLines := slices.Collect(bytes.Lines(content[field.Pos().Offset():fieldEndOffset]))
	for i, fieldLine := range fieldLines {
		contentBuilder.Write(indent)
		contentBuilder.WriteString("\t")
		isLastLine := i+1 == len(fieldLines)
		if isLastLine {
			// The last field line may or may not have a trailing line
			// ending (because it might be the last line in the file).
			fieldLine = bytes.TrimRight(fieldLine, "\r\n")
			fieldLine = bytes.TrimRight(fieldLine, ", \t")
		}
		contentBuilder.Write(fieldLine)
		if isLastLine {
			contentBuilder.WriteString(lineEnding)
		}
	}

	contentBuilder.Write(indent)
	contentBuilder.WriteString("}")
	contentBuilder.WriteString(lineEnding)

	remaining := content[fieldEndOffset:]
	remaining = bytes.TrimLeft(remaining, ", \t")

	if bytes.HasPrefix(remaining, []byte(lineEnding)) {
		contentBuilder.Write(remaining)
	} else if len(remaining) > 0 {
		contentBuilder.Write(indent)
		contentBuilder.Write(remaining)
	}

	diffEdits := diff.Strings(string(content), contentBuilder.String())
	edits, err := protocol.EditsFromDiffEdits(f.mapper, diffEdits)
	if err != nil {
		return nil, nil
	}

	return &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			params.TextDocument.URI: edits,
		},
	}, nil
}
