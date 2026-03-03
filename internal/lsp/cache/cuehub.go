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
	"time"

	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/lsp/cuehub"
	cuehubproto "cuelang.org/go/unstable/lspaux/protocol"
)

func cueHubEvalCommand(uri protocol.DocumentURI) *protocol.Command {
	return &protocol.Command{
		Title:     "Validate with CueHub",
		Command:   settings.CueHubEvaluateCommand,
		Arguments: []json.RawMessage{json.RawMessage(`"` + uri + `"`)},
	}
}

func (w *Workspace) CodeActionCueHubEval(ctx context.Context, params *protocol.CodeActionParams) *protocol.CodeAction {
	f := w.GetFile(params.TextDocument.URI)
	if f != nil && f.cueHub != nil && f.cueHub.IsDirty() {
		return &protocol.CodeAction{
			Title:   "Validate with CueHub",
			Command: cueHubEvalCommand(params.TextDocument.URI),
		}
	}

	return nil
}

func (w *Workspace) CodeLensCueHubEval(ctx context.Context, params *protocol.CodeLensParams) *protocol.CodeLens {
	f := w.GetFile(params.TextDocument.URI)
	if f != nil && f.cueHub != nil && f.cueHub.IsDirty() {
		return &protocol.CodeLens{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			Command: cueHubEvalCommand(params.TextDocument.URI),
		}
	}

	return nil
}

func (w *Workspace) CommandCueHubEval(ctx context.Context, uri protocol.DocumentURI) error {
	f := w.GetFile(uri)
	if f == nil || f.cueHub == nil {
		return nil
	}

	eval := &cueHubEvaluation{workspace: w}
	// We want to try to ensure the client gets the reply to the
	// command before any new diagnostics etc.
	w.enqueue(func() {
		w.debugLog("cuehub: requesting evaluation")
		if err := f.cueHub.RequestEvaluation(eval); err != nil {
			w.debugLog(err.Error())
		}
	})
	return nil
}

type cueHubEvaluation struct {
	workspace  *Workspace
	errsByFile map[protocol.DocumentURI][]error
}

func (eval *cueHubEvaluation) EvalResult(resultMsg *cuehubproto.EvalResultMsg) {
	w := eval.workspace
	w.enqueue(func() {
		errsByFile := eval.errsByFile
		if errsByFile == nil {
			errsByFile = make(map[protocol.DocumentURI][]error)
			eval.errsByFile = errsByFile
		}

		for _, err := range resultMsg.Errors {
			for _, coord := range err.Coordinates {
				uri := protocol.DocumentURI(coord.Path)
				f := w.GetFile(uri)
				if f == nil || f.tokFile == nil {
					continue
				}
				pos := f.tokFile.Pos(int(coord.ByteOffset), token.NoRelPos)
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
	})
}

func (eval *cueHubEvaluation) EvalFinished(*cuehubproto.EvalFinishedMsg) {
}

func (eval *cueHubEvaluation) Clear() {
	w := eval.workspace
	w.enqueue(func() {
		errsByFile := eval.errsByFile
		eval.errsByFile = nil

		for fileUri := range errsByFile {
			f := w.GetFile(fileUri)
			if f == nil {
				continue
			}
			f.removeUser(eval)
		}
	})
}

func (w *Workspace) cueHubOnDirtyChanged(hub *cuehub.CueHub) {
	w.enqueue(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := w.client.CodeLensRefresh(ctx); err != nil {
			w.debugLog(err.Error())
		}
	})
}
