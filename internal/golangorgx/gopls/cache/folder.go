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

// NewWorkspaceFolder creates a new workspace folder. The name is
// entirely decorative and does not have any semantics attached to it.
func NewWorkspaceFolder(dir protocol.DocumentURI, name string) (*WorkspaceFolder, error) {
	dirPath := dir.Path()
	err := checkPathValid(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace folder path %v: %w; check that the spelling of the configured workspace folder path agrees with the spelling reported by the operating system", dirPath, err)
	}

	return &WorkspaceFolder{
		dir:  dir,
		name: name,
	}, nil
}

// UpdateOptions sets the folders options to opts. The caller should
// not modify the contents of opts after calling this method.
func (wf *WorkspaceFolder) UpdateOptions(opts *settings.Options) {
	wf.options = opts
}

// FileWatchingGlobPatterns adds a pattern for watching the folder to
// the given patterns map and reports whether this folder requires
// subdirectories to be watched explicitly.
func (wf *WorkspaceFolder) FileWatchingGlobPatterns(patterns map[protocol.RelativePattern]struct{}) bool {
	const watchCueFiles = "**/*.cue"

	patterns[protocol.RelativePattern{
		BaseURI: wf.dir,
		Pattern: watchCueFiles,
	}] = struct{}{}

	return wf.watchSubdirs()
}

func (s *WorkspaceFolder) watchSubdirs() bool {
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
		const vscodeName = "Visual Studio Code"
		if options.ClientInfo != nil && options.ClientInfo.Name == vscodeName {
			return true
		}
		return false
	default:
		return false
	}
}
