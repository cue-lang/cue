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
	"path/filepath"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// joinURI returns the canonical [protocol.DocumentURI] for the file
// or directory at the given (slash-separated) path relative to the
// given directory's URI.
//
// The result is canonicalized in the same way as URIs received from
// the client. This matters for any path containing characters which
// URIs escape (e.g. spaces): a URI built by plain string
// concatenation would never compare equal to the equivalent URI
// received from the client, splitting the workspace's state for that
// file in two.
func joinURI(dir protocol.DocumentURI, relPath string) protocol.DocumentURI {
	return protocol.URIFromPath(filepath.Join(dir.FilePath(), filepath.FromSlash(relPath)))
}

// checkPathValid performs an OS-specific path validity check. The
// implementation varies for filesystems that are case-insensitive
// (e.g. macOS, Windows), and for those that disallow certain file
// names (e.g. path segments ending with a period on Windows, or
// reserved names such as "com"; see
// https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file).
var checkPathValid = defaultCheckPathValid

func defaultCheckPathValid(path string) error {
	return nil
}

// CheckPathValid checks whether a directory is suitable as a workspace folder.
//
// This exists for use by tests, to check the [testing.TempDir] result
// is acceptable.
func CheckPathValid(path string) error {
	return checkPathValid(path)
}
