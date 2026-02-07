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

package server

import (
	"context"
	"slices"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func (s *server) getSupportedCodeActions() []protocol.CodeActionKind {
	allCodeActionKinds := make(map[protocol.CodeActionKind]struct{})
	for _, kinds := range s.Options().SupportedCodeActions {
		for kind := range kinds {
			allCodeActionKinds[kind] = struct{}{}
		}
	}
	var result []protocol.CodeActionKind
	for kind := range allCodeActionKinds {
		result = append(result, kind)
	}
	slices.Sort(result)
	return result
}

func (s *server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	var codeActions []protocol.CodeAction

	convertToStructEdit, err := s.workspace.CodeActionConvertToStruct(ctx, params)
	if err != nil {
		return nil, err
	}
	if convertToStructEdit != nil {
		codeActions = append(codeActions, protocol.CodeAction{
			Title: "Wrap field in struct",
			Kind:  protocol.RefactorRewriteConvertToStruct,
			Edit:  convertToStructEdit,
		})
	}

	return codeActions, nil
}
