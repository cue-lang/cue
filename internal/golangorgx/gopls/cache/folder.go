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

package cache

import (
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
)

// A WorkspaceFolder corresponds to an LSP Workspace Folder. A single
// workspace is configured with one or more workspace folders. Each
// folder can have its own options, for which the server can query the
// editor/client.
type WorkspaceFolder struct {
	dir     protocol.DocumentURI
	name    string // decorative name for UI; not necessarily unique
	options *settings.Options
}

func NewWorkspaceFolder(fetchFolderOptions func(folder protocol.DocumentURI) (*settings.Options, error), dir protocol.DocumentURI, name string) (*WorkspaceFolder, error) {
	dirPath := dir.Path()
	err := checkPathValid(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace folder path %v: %w; check that the spelling of the configured workspace folder path agrees with the spelling reported by the operating system", dirPath, err)
	}

	options, err := fetchFolderOptions(dir)
	if err != nil {
		return nil, err
	}
	return &WorkspaceFolder{
		dir:     dir,
		name:    name,
		options: options,
	}, nil
}

func (wf *WorkspaceFolder) UpdateOptions(fetchFolderOptions func(folder protocol.DocumentURI) (*settings.Options, error)) error {
	options, err := fetchFolderOptions(wf.dir)
	if err != nil {
		return err
	}
	wf.options = options
	return nil
}

func (wf *WorkspaceFolder) FileWatchingGlobPatterns(patterns map[protocol.RelativePattern]struct{}) bool {
	const watchCueFiles = "**/*.cue"
	// We always assume that patterns already contains a pattern for all module.cue files

	patterns[protocol.RelativePattern{
		BaseURI: wf.dir,
		Pattern: watchCueFiles,
	}] = struct{}{}

	return wf.WatchSubdirs()
}

func (s *WorkspaceFolder) WatchSubdirs() bool {
	options := s.options
	switch p := options.SubdirWatchPatterns; p {
	case settings.SubdirWatchPatternsOn:
		return true
	case settings.SubdirWatchPatternsOff:
		return false
	case settings.SubdirWatchPatternsAuto:
		// See the documentation of
		// [settings.InternalOptions.SubdirWatchPatterns] for an
		// explanation of why VS Code gets a different default value
		// here.
		//
		// Unfortunately, there is no authoritative list of client names, nor any
		// requirements that client names do not change. We should update the VS
		// Code extension to set a default value of "subdirWatchPatterns" to "on",
		// so that this workaround is only temporary.
		if options.ClientInfo != nil && options.ClientInfo.Name == "Visual Studio Code" {
			return true
		}
		return false
	default:
		return false
	}
}
