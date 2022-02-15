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

// Package path provides slash separated wrappers around filepath operations
// This package can be used instead of filepath to allow better Windows
// compatability
package path

import "path/filepath"

func IsAbs(path string) bool {
	return filepath.IsAbs(path)
}

func Abs(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	return filepath.ToSlash(absPath), err
}

func Clean(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func Rel(basepath string, targpath string) (string, error) {
	relPath, err := filepath.Rel(basepath, targpath)
	return filepath.ToSlash(relPath), err
}

func Join(elem ...string) string {
	return filepath.ToSlash(filepath.Join(elem...))
}

func Dir(path string) string {
	return filepath.ToSlash(filepath.Dir(path))
}
