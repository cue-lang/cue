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

func (s *server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) ([]any, error) {
	root := s.workspace.DocumentSymbols(params.TextDocument.URI)
	if len(root) == 0 {
		return nil, nil
	}
	roots := make([]any, len(root))
	for i, child := range root {
		roots[i] = child
	}
	return roots, nil
}
