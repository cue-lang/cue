// Copyright 2022 The CUE Authors
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

// Package filesystem provides io/fs compliant file systems for use in loading and decoding
package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
)

// OSFS is a io/fs compliant file system that is meant to behave identically
// to os file operations, with a current working directory at CWD
type OSFS struct {
	CWD string
}

// getAbsPath gets the absolute path, using CWD as the parent directory for
// relative paths
func (fsys *OSFS) getAbsPath(path string) string {
	path = filepath.Clean(path)

	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(fsys.CWD, path))
	}

	return filepath.ToSlash(path)
}

// Open implements the corresponding function from the io/fs interface
func (fsys *OSFS) Open(name string) (fs.File, error) {
	name = fsys.getAbsPath(name)

	// Convert from standard path to OS specific path in FS
	f, err := os.Open(name)
	if err != nil {
		return nil, err // nil fs.File
	}
	return f, nil
}

// Stat implements the corresponding function from the io/fs interface
func (fsys *OSFS) Stat(name string) (fs.FileInfo, error) {
	name = fsys.getAbsPath(name)

	// Convert from standard path to OS specific path in FS
	f, err := os.Stat(name)

	if err != nil {
		return nil, err
	}
	return f, nil
}
