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
	"maps"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// UpdateWatchedFiles compares the current set of glob patterns to watch
// with the previously registered set. If the set has changed, we
// unregister and re-register for file watching notifications.
func (s *server) UpdateWatchedFiles(ctx context.Context) error {
	patterns := s.workspace.FileWatchingGlobPatterns()

	options := s.Options()
	if !options.DynamicWatchedFilesSupported {
		return nil
	}

	// Nothing to do if the set of patterns is unchanged.
	if maps.Equal(s.watchedGlobPatterns, patterns) {
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

	kind := protocol.WatchChange | protocol.WatchDelete | protocol.WatchCreate
	watchers := make([]protocol.FileSystemWatcher, 0, len(patterns))

	for pattern := range patterns {
		watchers = append(watchers, protocol.FileSystemWatcher{
			GlobPattern: protocol.GlobPattern{Value: pattern},
			Kind:        &kind,
		})
	}

	s.enqueue(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := s.client.RegisterCapability(ctx,
			&protocol.RegistrationParams{
				Registrations: []protocol.Registration{{
					ID:     WatchedFilesCapabilityID(curID),
					Method: "workspace/didChangeWatchedFiles",
					RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
						Watchers: watchers,
					},
				}},
			})
		if err != nil {
			s.debugLog(err.Error())
		}
	})

	if oldID > 0 {
		s.enqueue(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := s.client.UnregisterCapability(ctx,
				&protocol.UnregistrationParams{
					Unregisterations: []protocol.Unregistration{{
						ID:     WatchedFilesCapabilityID(oldID),
						Method: "workspace/didChangeWatchedFiles",
					}},
				})
			if err != nil {
				s.debugLog(err.Error())
			}
		})
	}

	return nil
}

func WatchedFilesCapabilityID(id int) string {
	return fmt.Sprintf("workspace/didChangeWatchedFiles-%d", id)
}
