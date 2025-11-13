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

package server

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/lsp/cache"
)

type Msg[T any] struct {
	Content        T
	resultReady    chan struct{}
	terminatedChan <-chan struct{}
}

func (msg *Msg[T]) WaitForReply() bool {
	select {
	case <-msg.resultReady:
		return true
	case <-msg.terminatedChan:
		// if they were both ready to recieve on, we could end up here
		// (it's random), so we need to test again to see whether
		// WaitChan can be received on:
		select {
		case <-msg.resultReady:
			return true
		default:
			return false
		}
	}
}

func (msg *Msg[T]) MarkProcessed() {
	close(msg.resultReady)
}

type MailboxWriter[T any] struct {
	ch             chan<- *Msg[T]
	TerminatedChan <-chan struct{}
}

func (w *MailboxWriter[T]) Send(msg *Msg[T], waitForReply bool) (success bool) {
	msg.terminatedChan = w.TerminatedChan
	msg.resultReady = make(chan struct{})
	select {
	case <-w.TerminatedChan:
		return false

	default:
		w.ch <- msg
		if waitForReply {
			return msg.WaitForReply()
		}
		return true
	}
}

type MailboxReader[T any] struct {
	ch             <-chan *Msg[T]
	terminatedChan chan struct{}
}

// Used by the reader to indicate the reader has exited. It is
// idempotent, but not concurrent-safe.
func (r *MailboxReader[T]) Terminate() {
	select {
	case <-r.terminatedChan:
	default:
		close(r.terminatedChan)
	}
}

func NewMailbox[T any]() (writer *MailboxWriter[T], reader *MailboxReader[T]) {
	ch := make(chan *Msg[T])
	terminatedChan := make(chan struct{})

	writer = &MailboxWriter[T]{
		ch:             ch,
		TerminatedChan: terminatedChan,
	}
	reader = &MailboxReader[T]{
		ch:             ch,
		terminatedChan: terminatedChan,
	}
	return writer, reader
}

var ErrServerTerminated = errors.New("server has terminated")

type serverFunc = func(*server)

// New creates an LSP server.
func New(cache *cache.Cache, client protocol.ClientCloser, options *settings.Options) ServerWithID {
	counter := atomic.AddInt64(&serverIDCounter, 1)

	server := &server{
		client: client,
		cache:  cache,

		state:   serverCreated,
		options: options,
	}

	w, r := NewMailbox[serverFunc]()
	go func() {
		defer r.Terminate()
		for {
			msg := <-r.ch
			msg.Content(server)
			msg.MarkProcessed()
		}
	}()
	return &serverActorClient{
		w:  w,
		id: strconv.FormatInt(counter, 10),
	}
}

type serverActorClient struct {
	w  *MailboxWriter[serverFunc]
	id string
}

var serverIDCounter int64

type ServerWithID interface {
	protocol.Server

	// ID returns a unique, human-readable string for this server, for
	// the purpose of log messages and debugging.
	ID() string
}

var _ ServerWithID = (*serverActorClient)(nil)

func (s *serverActorClient) ID() string { return s.id }

func (s *serverActorClient) sendAndWait(fun serverFunc) error {
	msg := &Msg[serverFunc]{
		Content: fun,
	}
	if s.w.Send(msg, true) {
		return nil
	}
	return ErrServerTerminated
}

func (s *serverActorClient) Progress(ctx context.Context, param *protocol.ProgressParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.Progress(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
}

func (s *serverActorClient) SetTrace(ctx context.Context, param *protocol.SetTraceParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.SetTrace(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
}

func (s *serverActorClient) IncomingCalls(ctx context.Context, param *protocol.CallHierarchyIncomingCallsParams) (result []protocol.CallHierarchyIncomingCall, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.IncomingCalls(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
}

func (s *serverActorClient) OutgoingCalls(ctx context.Context, param *protocol.CallHierarchyOutgoingCallsParams) (result []protocol.CallHierarchyOutgoingCall, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.OutgoingCalls(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // callHierarchy/outgoingCalls

func (s *serverActorClient) ResolveCodeAction(ctx context.Context, param *protocol.CodeAction) (result *protocol.CodeAction, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ResolveCodeAction(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // codeAction/resolve

func (s *serverActorClient) ResolveCodeLens(ctx context.Context, param *protocol.CodeLens) (result *protocol.CodeLens, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ResolveCodeLens(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // codeLens/resolve

func (s *serverActorClient) ResolveCompletionItem(ctx context.Context, param *protocol.CompletionItem) (result *protocol.CompletionItem, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ResolveCompletionItem(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // completionItem/resolve

func (s *serverActorClient) ResolveDocumentLink(ctx context.Context, param *protocol.DocumentLink) (result *protocol.DocumentLink, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ResolveDocumentLink(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // documentLink/resolve

func (s *serverActorClient) Exit(ctx context.Context) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.Exit(ctx)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // exit

func (s *serverActorClient) Initialize(ctx context.Context, param *protocol.ParamInitialize) (result *protocol.InitializeResult, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Initialize(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // initialize

func (s *serverActorClient) Initialized(ctx context.Context, param *protocol.InitializedParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.Initialized(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // initialized

func (s *serverActorClient) Resolve(ctx context.Context, param *protocol.InlayHint) (result *protocol.InlayHint, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Resolve(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // inlayHint/resolve

func (s *serverActorClient) DidChangeNotebookDocument(ctx context.Context, param *protocol.DidChangeNotebookDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidChangeNotebookDocument(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // notebookDocument/didChange

func (s *serverActorClient) DidCloseNotebookDocument(ctx context.Context, param *protocol.DidCloseNotebookDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidCloseNotebookDocument(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // notebookDocument/didClose

func (s *serverActorClient) DidOpenNotebookDocument(ctx context.Context, param *protocol.DidOpenNotebookDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidOpenNotebookDocument(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // notebookDocument/didOpen

func (s *serverActorClient) DidSaveNotebookDocument(ctx context.Context, param *protocol.DidSaveNotebookDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidSaveNotebookDocument(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // notebookDocument/didSave

func (s *serverActorClient) Shutdown(ctx context.Context) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.Shutdown(ctx)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // shutdown

func (s *serverActorClient) CodeAction(ctx context.Context, param *protocol.CodeActionParams) (result []protocol.CodeAction, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.CodeAction(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/codeAction

func (s *serverActorClient) CodeLens(ctx context.Context, param *protocol.CodeLensParams) (result []protocol.CodeLens, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.CodeLens(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/codeLens

func (s *serverActorClient) ColorPresentation(ctx context.Context, param *protocol.ColorPresentationParams) (result []protocol.ColorPresentation, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ColorPresentation(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/colorPresentation

func (s *serverActorClient) Completion(ctx context.Context, param *protocol.CompletionParams) (result *protocol.CompletionList, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Completion(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/completion

func (s *serverActorClient) Declaration(ctx context.Context, param *protocol.DeclarationParams) (result *protocol.Or_textDocument_declaration, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Declaration(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/declaration

func (s *serverActorClient) Definition(ctx context.Context, param *protocol.DefinitionParams) (result []protocol.Location, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Definition(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/definition

func (s *serverActorClient) Diagnostic(ctx context.Context, param *string) (result *string, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Diagnostic(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/diagnostic

func (s *serverActorClient) DidChange(ctx context.Context, param *protocol.DidChangeTextDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidChange(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // textDocument/didChange

func (s *serverActorClient) DidClose(ctx context.Context, param *protocol.DidCloseTextDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidClose(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // textDocument/didClose

func (s *serverActorClient) DidOpen(ctx context.Context, param *protocol.DidOpenTextDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidOpen(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // textDocument/didOpen

func (s *serverActorClient) DidSave(ctx context.Context, param *protocol.DidSaveTextDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidSave(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // textDocument/didSave

func (s *serverActorClient) DocumentColor(ctx context.Context, param *protocol.DocumentColorParams) (result []protocol.ColorInformation, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.DocumentColor(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/documentColor

func (s *serverActorClient) DocumentHighlight(ctx context.Context, param *protocol.DocumentHighlightParams) (result []protocol.DocumentHighlight, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.DocumentHighlight(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/documentHighlight

func (s *serverActorClient) DocumentLink(ctx context.Context, param *protocol.DocumentLinkParams) (result []protocol.DocumentLink, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.DocumentLink(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/documentLink

func (s *serverActorClient) DocumentSymbol(ctx context.Context, param *protocol.DocumentSymbolParams) (result []any, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.DocumentSymbol(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/documentSymbol

func (s *serverActorClient) FoldingRange(ctx context.Context, param *protocol.FoldingRangeParams) (result []protocol.FoldingRange, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.FoldingRange(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/foldingRange

func (s *serverActorClient) Formatting(ctx context.Context, param *protocol.DocumentFormattingParams) (result []protocol.TextEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Formatting(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/formatting

func (s *serverActorClient) Hover(ctx context.Context, param *protocol.HoverParams) (result *protocol.Hover, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Hover(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/hover

func (s *serverActorClient) Implementation(ctx context.Context, param *protocol.ImplementationParams) (result []protocol.Location, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Implementation(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/implementation

func (s *serverActorClient) InlayHint(ctx context.Context, param *protocol.InlayHintParams) (result []protocol.InlayHint, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.InlayHint(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/inlayHint

func (s *serverActorClient) InlineCompletion(ctx context.Context, param *protocol.InlineCompletionParams) (result *protocol.Or_Result_textDocument_inlineCompletion, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.InlineCompletion(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/inlineCompletion

func (s *serverActorClient) InlineValue(ctx context.Context, param *protocol.InlineValueParams) (result []protocol.InlineValue, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.InlineValue(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/inlineValue

func (s *serverActorClient) LinkedEditingRange(ctx context.Context, param *protocol.LinkedEditingRangeParams) (result *protocol.LinkedEditingRanges, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.LinkedEditingRange(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/linkedEditingRange

func (s *serverActorClient) Moniker(ctx context.Context, param *protocol.MonikerParams) (result []protocol.Moniker, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Moniker(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/moniker

func (s *serverActorClient) OnTypeFormatting(ctx context.Context, param *protocol.DocumentOnTypeFormattingParams) (result []protocol.TextEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.OnTypeFormatting(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/onTypeFormatting

func (s *serverActorClient) PrepareCallHierarchy(ctx context.Context, param *protocol.CallHierarchyPrepareParams) (result []protocol.CallHierarchyItem, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.PrepareCallHierarchy(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/prepareCallHierarchy

func (s *serverActorClient) PrepareRename(ctx context.Context, param *protocol.PrepareRenameParams) (result *protocol.PrepareRenameResult, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.PrepareRename(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/prepareRename

func (s *serverActorClient) PrepareTypeHierarchy(ctx context.Context, param *protocol.TypeHierarchyPrepareParams) (result []protocol.TypeHierarchyItem, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.PrepareTypeHierarchy(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/prepareTypeHierarchy

func (s *serverActorClient) RangeFormatting(ctx context.Context, param *protocol.DocumentRangeFormattingParams) (result []protocol.TextEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.RangeFormatting(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/rangeFormatting

func (s *serverActorClient) RangesFormatting(ctx context.Context, param *protocol.DocumentRangesFormattingParams) (result []protocol.TextEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.RangesFormatting(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/rangesFormatting

func (s *serverActorClient) References(ctx context.Context, param *protocol.ReferenceParams) (result []protocol.Location, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.References(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/references

func (s *serverActorClient) Rename(ctx context.Context, param *protocol.RenameParams) (result *protocol.WorkspaceEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Rename(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/rename

func (s *serverActorClient) SelectionRange(ctx context.Context, param *protocol.SelectionRangeParams) (result []protocol.SelectionRange, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.SelectionRange(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/selectionRange

func (s *serverActorClient) SemanticTokensFull(ctx context.Context, param *protocol.SemanticTokensParams) (result *protocol.SemanticTokens, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.SemanticTokensFull(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/semanticTokens/full

func (s *serverActorClient) SemanticTokensFullDelta(ctx context.Context, param *protocol.SemanticTokensDeltaParams) (result interface{}, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.SemanticTokensFullDelta(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/semanticTokens/full/delta

func (s *serverActorClient) SemanticTokensRange(ctx context.Context, param *protocol.SemanticTokensRangeParams) (result *protocol.SemanticTokens, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.SemanticTokensRange(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/semanticTokens/range

func (s *serverActorClient) SignatureHelp(ctx context.Context, param *protocol.SignatureHelpParams) (result *protocol.SignatureHelp, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.SignatureHelp(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/signatureHelp

func (s *serverActorClient) TypeDefinition(ctx context.Context, param *protocol.TypeDefinitionParams) (result []protocol.Location, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.TypeDefinition(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/typeDefinition

func (s *serverActorClient) WillSave(ctx context.Context, param *protocol.WillSaveTextDocumentParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.WillSave(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // textDocument/willSave

func (s *serverActorClient) WillSaveWaitUntil(ctx context.Context, param *protocol.WillSaveTextDocumentParams) (result []protocol.TextEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.WillSaveWaitUntil(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // textDocument/willSaveWaitUntil

func (s *serverActorClient) Subtypes(ctx context.Context, param *protocol.TypeHierarchySubtypesParams) (result []protocol.TypeHierarchyItem, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Subtypes(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // typeHierarchy/subtypes

func (s *serverActorClient) Supertypes(ctx context.Context, param *protocol.TypeHierarchySupertypesParams) (result []protocol.TypeHierarchyItem, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Supertypes(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // typeHierarchy/supertypes

func (s *serverActorClient) WorkDoneProgressCancel(ctx context.Context, param *protocol.WorkDoneProgressCancelParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.WorkDoneProgressCancel(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // window/workDoneProgress/cancel

func (s *serverActorClient) DiagnosticWorkspace(ctx context.Context, param *protocol.WorkspaceDiagnosticParams) (result *protocol.WorkspaceDiagnosticReport, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.DiagnosticWorkspace(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspace/diagnostic

func (s *serverActorClient) DidChangeConfiguration(ctx context.Context, param *protocol.DidChangeConfigurationParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidChangeConfiguration(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // workspace/didChangeConfiguration

func (s *serverActorClient) DidChangeWatchedFiles(ctx context.Context, param *protocol.DidChangeWatchedFilesParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidChangeWatchedFiles(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // workspace/didChangeWatchedFiles

func (s *serverActorClient) DidChangeWorkspaceFolders(ctx context.Context, param *protocol.DidChangeWorkspaceFoldersParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidChangeWorkspaceFolders(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // workspace/didChangeWorkspaceFolders

func (s *serverActorClient) DidCreateFiles(ctx context.Context, param *protocol.CreateFilesParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidCreateFiles(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // workspace/didCreateFiles

func (s *serverActorClient) DidDeleteFiles(ctx context.Context, param *protocol.DeleteFilesParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidDeleteFiles(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // workspace/didDeleteFiles

func (s *serverActorClient) DidRenameFiles(ctx context.Context, param *protocol.RenameFilesParams) (err error) {
	sendErr := s.sendAndWait(func(s *server) {
		err = s.DidRenameFiles(ctx, param)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
} // workspace/didRenameFiles

func (s *serverActorClient) ExecuteCommand(ctx context.Context, param *protocol.ExecuteCommandParams) (result interface{}, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ExecuteCommand(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspace/executeCommand

func (s *serverActorClient) Symbol(ctx context.Context, param *protocol.WorkspaceSymbolParams) (result []protocol.SymbolInformation, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.Symbol(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspace/symbol

func (s *serverActorClient) WillCreateFiles(ctx context.Context, param *protocol.CreateFilesParams) (result *protocol.WorkspaceEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.WillCreateFiles(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspace/willCreateFiles

func (s *serverActorClient) WillDeleteFiles(ctx context.Context, param *protocol.DeleteFilesParams) (result *protocol.WorkspaceEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.WillDeleteFiles(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspace/willDeleteFiles

func (s *serverActorClient) WillRenameFiles(ctx context.Context, param *protocol.RenameFilesParams) (result *protocol.WorkspaceEdit, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.WillRenameFiles(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspace/willRenameFiles

func (s *serverActorClient) ResolveWorkspaceSymbol(ctx context.Context, param *protocol.WorkspaceSymbol) (result *protocol.WorkspaceSymbol, err error) {
	sendErr := s.sendAndWait(func(s *server) {
		result, err = s.ResolveWorkspaceSymbol(ctx, param)
	})
	if sendErr != nil {
		return nil, sendErr
	}
	return result, err
} // workspaceSymbol/resolve
