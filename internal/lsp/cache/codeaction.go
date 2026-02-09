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
	"cuelang.org/go/cue/token"
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
		w.debugLog(err.Error())
		return nil, nil
	}

	structLit, field := innermostStructFieldForOffset(f.syntax, offset)
	if structLit == nil || field == nil {
		return nil, nil
	}
	if len(structLit.Elts) != 1 && (structLit.Lbrace.HasAbsPos() || structLit.Rbrace.HasAbsPos()) {
		return nil, nil
	}

	content := f.tokFile.Content()
	lineStartOffsets := f.tokFile.Lines() // Lines is 0-based
	labelStart := field.Pos()
	lineNo := labelStart.Line() - 1 // Line is 1-based, hence the -1
	if lineNo < 0 || lineNo >= len(lineStartOffsets) {
		return nil, nil
	}

	lineEnding := extractLineEnding(content, lineStartOffsets)
	indent := extractIndent(content, lineStartOffsets[lineNo])

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
		}
		contentBuilder.Write(fieldLine)
		if isLastLine {
			contentBuilder.WriteString(lineEnding)
		}
	}

	contentBuilder.Write(indent)
	contentBuilder.WriteString("}")

	remaining := content[fieldEndOffset:]
	remaining = bytes.TrimLeft(remaining, ", \t")

	if bytes.HasPrefix(remaining, []byte(lineEnding)) {
		contentBuilder.Write(remaining)
	} else if len(remaining) > 0 {
		contentBuilder.WriteString(lineEnding)
		contentBuilder.Write(indent)
		contentBuilder.Write(remaining)
	}

	diffEdits := diff.Strings(string(content), contentBuilder.String())
	edits, err := protocol.EditsFromDiffEdits(f.mapper, diffEdits)
	if err != nil {
		return nil, nil
	}

	docChanges := protocol.TextEditsToDocumentChanges(params.TextDocument.URI, f.tokFile.Revision(), edits)
	return &protocol.WorkspaceEdit{DocumentChanges: docChanges}, nil
}

func (w *Workspace) CodeActionConvertFromStruct(ctx context.Context, params *protocol.CodeActionParams) (*protocol.WorkspaceEdit, error) {
	f := w.GetFile(params.TextDocument.URI)
	if f == nil || f.syntax == nil || f.mapper == nil || f.tokFile == nil {
		return nil, nil
	}

	offset, _, err := f.mapper.RangeOffsets(params.Range)
	if err != nil {
		w.debugLog(err.Error())
		return nil, nil
	}

	structLit, field := innermostStructFieldForOffset(f.syntax, offset)
	if structLit == nil || field == nil {
		return nil, nil
	}
	if len(structLit.Elts) != 1 || !structLit.Lbrace.HasAbsPos() || !structLit.Rbrace.HasAbsPos() {
		return nil, nil
	}

	content := f.tokFile.Content()
	lineStartOffsets := f.tokFile.Lines() // Lines is 0-based
	lineNo := structLit.Pos().Line() - 1  // Line is 1-based, hence the -1
	if lineNo < 0 || lineNo >= len(lineStartOffsets) {
		return nil, nil
	}

	lineEnding := extractLineEnding(content, lineStartOffsets)
	indent := extractIndent(content, lineStartOffsets[lineNo])

	var contentBuilder strings.Builder
	contentBuilder.Write(content[:structLit.Lbrace.Offset()])

	fieldLines := slices.Collect(bytes.Lines(content[field.Pos().Offset():field.End().Offset()]))
	for i, fieldLine := range fieldLines {
		if i > 0 {
			contentBuilder.Write(indent)
			contentBuilder.WriteString("\t")
		}
		fieldLine = bytes.TrimPrefix(fieldLine, indent)
		fieldLine = bytes.TrimRight(fieldLine, "\r\n")
		contentBuilder.Write(fieldLine)
		if isLastLine := i+1 == len(fieldLines); !isLastLine {
			contentBuilder.WriteString(lineEnding)
		}
	}

	remaining := content[structLit.Rbrace.Offset()+1:]
	remaining = bytes.TrimLeft(remaining, ", \t")

	if bytes.HasPrefix(remaining, []byte(lineEnding)) {
		contentBuilder.Write(remaining)
	} else if len(remaining) > 0 {
		contentBuilder.WriteString(lineEnding)
		contentBuilder.Write(indent)
		contentBuilder.Write(remaining)
	}

	diffEdits := diff.Strings(string(content), contentBuilder.String())
	edits, err := protocol.EditsFromDiffEdits(f.mapper, diffEdits)
	if err != nil {
		return nil, nil
	}

	docChanges := protocol.TextEditsToDocumentChanges(params.TextDocument.URI, f.tokFile.Revision(), edits)
	return &protocol.WorkspaceEdit{DocumentChanges: docChanges}, nil
}

// innermostStructFieldForOffset returns the innermost [ast.Field]
// (and its enclosing [ast.StructLit]) where the field's label
// contains the given offset.
func innermostStructFieldForOffset(syntax ast.Node, offset int) (structLit *ast.StructLit, field *ast.Field) {
	ast.Walk(syntax,
		// Walk over the AST, traversing into whatever contains the cursor position.
		func(node ast.Node) bool {
			start := node.Pos()
			end := node.End()

			if !start.HasAbsPos() || !end.HasAbsPos() {
				return false
			}
			if !token.WithinInclusive(offset, start, end) {
				return false
			}
			return true
		},

		// We want the inner-most matching field, so on the way back
		// out, capture the first suitable structlit+field.
		func(node ast.Node) {
			if structLit != nil {
				return
			}

			sl, ok := node.(*ast.StructLit)
			if !ok {
				return
			}

			for _, decl := range sl.Elts {
				f, ok := decl.(*ast.Field)
				if !ok {
					continue
				}

				lab := f.Label
				start := lab.Pos()
				end := lab.End()
				if !start.HasAbsPos() || !end.HasAbsPos() {
					continue
				}
				if !token.WithinInclusive(offset, start, end) {
					continue
				}

				structLit = sl
				field = f
				return
			}
		})

	return structLit, field
}

// extractIndent returns the run of space and horizontal tab
// characters in content that start at the lineStartOffset.
func extractIndent(content []byte, lineStartOffset int) []byte {
	for i := lineStartOffset; i < len(content); i++ {
		c := content[i]
		if c != ' ' && c != '\t' {
			return content[lineStartOffset:i]
		}
	}
	return nil
}

// extractLineEnding detects the line ending used in content by
// inspecting the end of the first line. If content only has one line
// then \n is returned.
func extractLineEnding(content []byte, lineStartOffsets []int) string {
	if len(lineStartOffsets) > 1 {
		// be careful: the very first line could be \n only
		offset := lineStartOffsets[1] - 2
		if offset > 0 && content[offset] == '\r' {
			return "\r\n"
		}
	}
	return "\n"
}
