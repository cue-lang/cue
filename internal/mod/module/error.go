// Copyright 2023 CUE Authors
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

package module

import (
	"errors"
	"fmt"
)

// A ModuleError indicates an error specific to a module.
type ModuleError struct {
	Path    string
	Version string
	Err     error
}

// VersionError returns a ModuleError derived from a Version and error,
// or err itself if it is already such an error.
func VersionError(v Version, err error) error {
	var mErr *ModuleError
	if errors.As(err, &mErr) && mErr.Path == v.Path() && mErr.Version == v.Version() {
		return err
	}
	return &ModuleError{
		Path:    v.Path(),
		Version: v.Version(),
		Err:     err,
	}
}

func (e *ModuleError) Error() string {
	if v, ok := e.Err.(*InvalidVersionError); ok {
		return fmt.Sprintf("%s@%s: invalid version: %v", e.Path, v.Version, v.Err)
	}
	if e.Version != "" {
		return fmt.Sprintf("%s@%s: %v", e.Path, e.Version, e.Err)
	}
	return fmt.Sprintf("module %s: %v", e.Path, e.Err)
}

func (e *ModuleError) Unwrap() error { return e.Err }

// An InvalidVersionError indicates an error specific to a version, with the
// module path unknown or specified externally.
//
// A ModuleError may wrap an InvalidVersionError, but an InvalidVersionError
// must not wrap a ModuleError.
type InvalidVersionError struct {
	Version string
	Err     error
}

func (e *InvalidVersionError) Error() string {
	return fmt.Sprintf("version %q invalid: %s", e.Version, e.Err)
}

func (e *InvalidVersionError) Unwrap() error { return e.Err }

// An InvalidPathError indicates a module, import, or file path doesn't
// satisfy all naming constraints. See CheckPath, CheckImportPath,
// and CheckFilePath for specific restrictions.
type InvalidPathError struct {
	Kind string // "module", "import", or "file"
	Path string
	Err  error
}

func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("malformed %s path %q: %v", e.Kind, e.Path, e.Err)
}

func (e *InvalidPathError) Unwrap() error { return e.Err }
