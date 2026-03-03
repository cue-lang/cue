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
	"cuelang.org/go/internal/lsp/extvalidator"
	extproto "cuelang.org/go/unstable/lspaux/protocol"
)

func externalValidateCommand(uri protocol.DocumentURI) *protocol.Command {
	return &protocol.Command{
		Title:     "Run external validators",
		Command:   settings.ExternalValidateCommand,
		Arguments: []json.RawMessage{json.RawMessage(`"` + uri + `"`)},
	}
}

// CodeLensExternalValidate returns the currently available code lenses.
func (w *Workspace) CodeLensExternalValidate(ctx context.Context, params *protocol.CodeLensParams) *protocol.CodeLens {
	f := w.GetFile(params.TextDocument.URI)
	if f != nil && f.extValidator != nil && f.extValidator.IsDirty() {
		return &protocol.CodeLens{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			Command: externalValidateCommand(params.TextDocument.URI),
		}
	}

	return nil
}

// CommandExternalValidate begins external validation of the given
// uri, if available.
func (w *Workspace) CommandExternalValidate(ctx context.Context, uri protocol.DocumentURI) error {
	f := w.GetFile(uri)
	if f == nil || f.extValidator == nil {
		return nil
	}

	validation := &externalValidation{workspace: w}
	w.enqueue(func() {
		w.debugLog("extValidator: requesting evaluation")
		if err := f.extValidator.RequestEvaluation(validation); err != nil {
			w.debugLog(err.Error())
		}
	})
	return nil
}

type externalValidation struct {
	workspace  *Workspace
	errsByFile map[protocol.DocumentURI][]error
}

// EvalResult implements [extvalidator.EvalResponseHandler]
func (v *externalValidation) EvalResult(resultMsg *extproto.EvalResultMsg) {
	w := v.workspace
	w.enqueue(func() {
		errsByFile := v.errsByFile
		if errsByFile == nil {
			errsByFile = make(map[protocol.DocumentURI][]error)
			v.errsByFile = errsByFile
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
			f.ensureUser(v, errs...)
		}

		w.publishDiagnostics()
	})
}

// EvalFinished implements [extvalidator.EvalResponseHandler]
func (v *externalValidation) EvalFinished(*extproto.EvalFinishedMsg) {
}

// Clear implements [extvalidator.EvalResponseHandler]
func (v *externalValidation) Clear() {
	w := v.workspace
	w.enqueue(func() {
		errsByFile := v.errsByFile
		v.errsByFile = nil

		for fileUri := range errsByFile {
			f := w.GetFile(fileUri)
			if f == nil {
				continue
			}
			f.removeUser(v)
		}
	})
}

func (w *Workspace) extValidatorOnDirtyChanged(*extvalidator.ExtValidator) {
	w.enqueue(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := w.client.CodeLensRefresh(ctx); err != nil {
			w.debugLog(err.Error())
		}
	})
}
