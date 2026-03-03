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
	"context"
	"encoding/json"

	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/cuehub"
	cuehubproto "cuelang.org/go/unstable/lspaux/protocol"
)

func (w *Workspace) CodeActionCueHubEval(ctx context.Context, params *protocol.CodeActionParams) *protocol.CodeAction {
	f := w.GetFile(params.TextDocument.URI)
	if f == nil || f.cueHub == nil {
		return nil
	}

	if f.cueHub.IsDirty() {
		return &protocol.CodeAction{
			Title: "Validate with CueHub",
			Command: &protocol.Command{
				Title:     "Validate with CueHub",
				Command:   "cuelsp.cuehubevaluate",
				Arguments: []json.RawMessage{json.RawMessage(`"` + params.TextDocument.URI + `"`)},
			},
		}
	}

	return nil
}

func (w *Workspace) CommandCueHubEval(ctx context.Context, uri protocol.DocumentURI) error {
	f := w.GetFile(uri)
	if f == nil || f.cueHub == nil {
		return nil
	}

	eval := &cueHubEvaluation{
		workspace: w,
	}
	w.debugLog("cuehub: requesting evaluation")
	_, err := f.cueHub.RequestEvaluation(eval)
	return err
}

type cueHubEvaluation struct {
	workspace  *Workspace
	errsByFile map[protocol.DocumentURI][]error
}

func (eval *cueHubEvaluation) EvalResult(hubEval *cuehub.Evaluation, resultMsg *cuehubproto.EvalResultMsg) {
	errsByFile := eval.errsByFile
	if errsByFile == nil {
		errsByFile = make(map[protocol.DocumentURI][]error)
		eval.errsByFile = errsByFile
	}

	w := eval.workspace

	for _, err := range resultMsg.Errors {
		for _, coord := range err.Coordinates {
			if coord.Line == 0 || coord.Column == 0 {
				continue
			}
			uri := protocol.DocumentURI(coord.Path)
			f := w.GetFile(uri)
			if f == nil || f.tokFile == nil {
				continue
			}
			tokFile := f.tokFile
			content := tokFile.Content()
			lineStartOffsets := tokFile.Lines()

			lineNo := int(coord.Line) - 1
			if lineNo >= len(lineStartOffsets) {
				continue
			}
			lineStartOffset := lineStartOffsets[lineNo]
			if lineStartOffset >= len(content) {
				continue
			}
			lineEndOffset := len(content)
			if nextLineNo := int(lineNo) + 1; nextLineNo < len(lineStartOffsets) {
				lineEndOffset = lineStartOffsets[nextLineNo]
				if lineEndOffset >= len(content) {
					continue
				}
			}
			colNo := int(coord.Column) - 1
			offset := lineStartOffset + colNo
			if offset >= lineEndOffset {
				continue
			}
			pos := f.tokFile.Pos(offset, token.NoRelPos)
			errsByFile[uri] = append(errsByFile[uri], cueerrors.Newf(pos, "%s", err.Message))
		}
	}

	for uri, errs := range errsByFile {
		f := w.GetFile(uri)
		if f == nil {
			continue
		}
		f.ensureUser(eval, errs...)
	}

	w.publishDiagnostics()
}

func (eval *cueHubEvaluation) EvalFinished(*cuehub.Evaluation, *cuehubproto.EvalFinishedMsg) {
}

func (eval *cueHubEvaluation) Clear(*cuehub.Evaluation) {
	w := eval.workspace
	for fileUri := range eval.errsByFile {
		f := w.GetFile(fileUri)
		if f == nil {
			continue
		}
		f.removeUser(eval)
	}
	eval.errsByFile = nil
}
