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
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/cache"
)

func (s *server) Definition(ctx context.Context, params *protocol.DefinitionParams) (_ []protocol.Location, rerr error) {
	uri := params.TextDocument.URI
	mod, err := s.workspace.FindModuleForFile(uri)
	if err != nil {
		return nil, err
	} else if mod == nil {
		return nil, fmt.Errorf("no module found for %v", uri)
	}
	pkgs, err := mod.FindPackagesOrModulesForFile(uri)
	if err != nil {
		return nil, err
	} else if len(pkgs) == 0 {
		return nil, fmt.Errorf("no pkgs found for %v", uri)
	}
	// The first package will be the "most specific". I.e. the package
	// with root at the same directory as the file itself. There's
	// definitely an argument that we should be calling Definition for
	// all packages, and merging the results. This would find
	// definitions that exist due to ancestor imports. TODO
	pkg, ok := pkgs[0].(*cache.Package)
	if !ok {
		return nil, fmt.Errorf("no pkgs found for %v", uri)
	}
	return pkg.Definition(uri, params.Position), nil
}
