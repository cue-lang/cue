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
	"encoding/json"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func (s *server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch params.Command {
	case "cuelsp.cuehubevaluate":
		args := params.Arguments
		if len(args) != 1 {
			break
		}
		var uri protocol.DocumentURI
		err := json.Unmarshal(args[0], &uri)
		if err != nil {
			return nil, err
		}

		err = s.workspace.CommandCueHubEval(ctx, uri, s.withServerLocked)
		return nil, err

	}
	return nil, notImplemented("ExecuteCommand")
}
