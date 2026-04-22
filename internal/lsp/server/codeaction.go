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
	"cmp"
	"context"
	"encoding/json"
	"slices"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func (s *server) getSupportedCodeActions() []protocol.CodeActionKind {
	var result []protocol.CodeActionKind
	for _, kinds := range s.Options().SupportedCodeActions {
		for kind := range kinds {
			result = append(result, kind)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func (s *server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	// delayEdit means the client supports a subsequent round-trip to
	// the server in order to resolve the `edit` property of a chosen
	// code action. This allows us to avoid potentially expensive
	// calculations of edits (diffs) before the user has chosen any
	// code action.
	delayEdit := slices.Contains(s.options.ClientOptions.CodeActionResolveOptions, "edit")
	var raw json.RawMessage
	if delayEdit {
		// The client will send this raw data back to us in any
		// subsequent call to ResolveCodeAction. For simplicity, we
		// reuse the params we've received.
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = json.RawMessage(data)
	}

	var codeActions []protocol.CodeAction

	convertToStructEdit, err := s.workspace.CodeActionConvertToStruct(ctx, params, delayEdit)
	if err != nil {
		return nil, err
	}
	if convertToStructEdit != nil {
		action := protocol.CodeAction{
			Title: "Add surrounding struct braces",
			Kind:  protocol.RefactorRewriteConvertToStruct,
		}
		if delayEdit {
			action.Data = &raw
		} else {
			action.Edit = convertToStructEdit

		}
		codeActions = append(codeActions, action)
	}

	convertFromStructEdit, err := s.workspace.CodeActionConvertFromStruct(ctx, params, delayEdit)
	if err != nil {
		return nil, err
	}
	if convertFromStructEdit != nil {
		action := protocol.CodeAction{
			Title: "Remove surrounding struct braces",
			Kind:  protocol.RefactorRewriteConvertFromStruct,
			// Mark it preferred so that if both "Add..." and "Remove..."
			// are available, then this "Remove..." action will be
			// prioritised by editors. This most likely matches the
			// user's needs.
			IsPreferred: true,
		}
		if delayEdit {
			action.Data = &raw
		} else {
			action.Edit = convertFromStructEdit
		}
		codeActions = append(codeActions, action)
	}

	// "Toggle surrounding struct braces" exists so it can be bound to a
	// single keystroke for the 0 <-> 1 brace cycle. If "Remove..." is
	// available at the cursor it is used; otherwise "Add..." is used.
	// The two primitives above remain available so users can still
	// deepen nesting explicitly.
	if toggleEdit := cmp.Or(convertFromStructEdit, convertToStructEdit); toggleEdit != nil {
		action := protocol.CodeAction{
			Title: "Toggle surrounding struct braces",
			Kind:  protocol.RefactorRewriteToggleStructBraces,
		}
		if delayEdit {
			action.Data = &raw
		} else {
			action.Edit = toggleEdit
		}
		codeActions = append(codeActions, action)
	}

	organizeImportsEdit, err := s.workspace.CodeActionOrganizeImports(ctx, params, delayEdit)
	if err != nil {
		return nil, err
	}
	if organizeImportsEdit != nil {
		action := protocol.CodeAction{
			Title: "Organize Imports",
			Kind:  protocol.SourceOrganizeImports,
		}
		if delayEdit {
			action.Data = &raw
		} else {
			action.Edit = organizeImportsEdit
		}
		codeActions = append(codeActions, action)
	}

	return codeActions, nil
}

func (s *server) ResolveCodeAction(ctx context.Context, action *protocol.CodeAction) (*protocol.CodeAction, error) {
	if action.Data == nil {
		return nil, nil
	}
	var params protocol.CodeActionParams
	err := json.Unmarshal(*action.Data, &params)
	if err != nil {
		return nil, err
	}

	switch action.Kind {
	case protocol.RefactorRewriteConvertToStruct:
		convertToStructEdit, err := s.workspace.CodeActionConvertToStruct(ctx, &params, false)
		if err != nil {
			return nil, err
		}
		action.Edit = convertToStructEdit
		return action, nil

	case protocol.RefactorRewriteConvertFromStruct:
		convertFromStructEdit, err := s.workspace.CodeActionConvertFromStruct(ctx, &params, false)
		if err != nil {
			return nil, err
		}
		action.Edit = convertFromStructEdit
		return action, nil

	case protocol.RefactorRewriteToggleStructBraces:
		convertFromStructEdit, err := s.workspace.CodeActionConvertFromStruct(ctx, &params, false)
		if err != nil {
			return nil, err
		}
		if convertFromStructEdit != nil {
			action.Edit = convertFromStructEdit
			return action, nil
		}
		convertToStructEdit, err := s.workspace.CodeActionConvertToStruct(ctx, &params, false)
		if err != nil {
			return nil, err
		}
		action.Edit = convertToStructEdit
		return action, nil

	case protocol.SourceOrganizeImports:
		organizeImportsEdit, err := s.workspace.CodeActionOrganizeImports(ctx, &params, false)
		if err != nil {
			return nil, err
		}
		action.Edit = organizeImportsEdit
		return action, nil

	default:
		return nil, nil
	}
}
