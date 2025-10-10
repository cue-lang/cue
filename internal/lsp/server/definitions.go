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

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func (s *server) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	uri := params.TextDocument.URI
	w := s.workspace
	tokFile, dfns, srcMapper, err := w.DefinitionsForURI(uri)
	if tokFile == nil || err != nil {
		return nil, err
	}
	return w.Definition(tokFile, dfns, srcMapper, params.Position), nil
}

func (s *server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	uri := params.TextDocument.URI
	w := s.workspace
	tokFile, dfns, srcMapper, err := w.DefinitionsForURI(uri)
	if tokFile == nil || err != nil {
		return nil, err
	}
	return w.Completion(tokFile, dfns, srcMapper, params.Position), nil
}

func (s *server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	uri := params.TextDocument.URI
	w := s.workspace
	tokFile, dfns, srcMapper, err := w.DefinitionsForURI(uri)
	if tokFile == nil || err != nil {
		return nil, err
	}
	return w.Hover(tokFile, dfns, srcMapper, params.Position), nil
}

func (s *server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	uri := params.TextDocument.URI
	w := s.workspace
	tokFile, dfns, srcMapper, err := w.DefinitionsForURI(uri)
	if tokFile == nil || err != nil {
		return nil, err
	}
	return w.References(tokFile, dfns, srcMapper, params.Position), nil
}
