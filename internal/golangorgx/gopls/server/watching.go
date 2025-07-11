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
	"path"
	"path/filepath"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func sameKeys[K comparable, V1, V2 any](x map[K]V1, y map[K]V2) bool {
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if _, ok := y[k]; !ok {
			return false
		}
	}
	return true
}

// UpdateWatchedFiles compares the current set of directories to watch
// with the previously registered set of directories. If the set of directories
// has changed, we unregister and re-register for file watching notifications.
// updatedSnapshots is the set of snapshots that have been updated.
func (s *server) UpdateWatchedFiles(ctx context.Context) error {
	patterns := s.workspace.FileWatchingGlobPatterns(ctx)

	options := s.Options()
	if !options.DynamicWatchedFilesSupported {
		return nil
	}

	// Nothing to do if the set of workspace directories is unchanged.
	if sameKeys(s.watchedGlobPatterns, patterns) {
		return nil
	}

	// If the set of directories to watch has changed, register the updates and
	// unregister the previously watched directories. This ordering avoids a
	// period where no files are being watched. Still, if a user makes on-disk
	// changes before these updates are complete, we may miss them for the new
	// directories.

	s.watchedGlobPatterns = patterns

	oldID := s.watchingIDCounter
	s.watchingIDCounter++
	curID := s.watchingIDCounter

	supportsRelativePatterns := options.RelativePatternsSupported
	kind := protocol.WatchChange | protocol.WatchDelete | protocol.WatchCreate
	watchers := make([]protocol.FileSystemWatcher, 0, len(patterns))

	for pattern := range patterns {
		var value any
		if supportsRelativePatterns && pattern.BaseURI != "" {
			value = pattern

		} else {
			p := pattern.Pattern
			if pattern.BaseURI != "" {
				p = path.Join(filepath.ToSlash(pattern.BaseURI.Path()), p)
			}
			value = p
		}

		watchers = append(watchers, protocol.FileSystemWatcher{
			GlobPattern: protocol.GlobPattern{Value: value},
			Kind:        &kind,
		})
	}

	err := s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{
			ID:     WatchedFilesCapabilityID(curID),
			Method: "workspace/didChangeWatchedFiles",
			RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
				Watchers: watchers,
			},
		}},
	})
	if err != nil {
		return err
	}

	if oldID > 0 {
		return s.client.UnregisterCapability(ctx, &protocol.UnregistrationParams{
			Unregisterations: []protocol.Unregistration{{
				ID:     WatchedFilesCapabilityID(oldID),
				Method: "workspace/didChangeWatchedFiles",
			}},
		})
	}

	return nil
}

func WatchedFilesCapabilityID(id int) string {
	return fmt.Sprintf("workspace/didChangeWatchedFiles-%d", id)
}
