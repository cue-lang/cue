// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build windows

package embed

import (
	"path/filepath"
	"syscall"
)

// isHidden checks if a file is hidden on Windows. We do not return an error
// if the file does not exist and will check that elsewhere.
func (c *compiler) isHidden(path string) bool {
	// To be safe, we also include dot files on Windows.
	base := filepath.Base(path)
	if base[0] == '.' {
		return true
	}

	path = filepath.FromSlash(path)
	path = filepath.Join(c.dir, path)

	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return false
	}

	return attributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
