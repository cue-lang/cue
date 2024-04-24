package module

import (
	"fmt"
)

// A ModuleError indicates an error specific to a module.
type ModuleError struct {
	Path    string
	Version string
	Err     error
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
