// Copyright 2018 The CUE Authors
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

package load

import (
	"fmt"
	"path/filepath"
	"strings"

	build "cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

func lastError(p *build.Instance) *packageError {
	if p == nil {
		return nil
	}
	switch v := p.Err.(type) {
	case *packageError:
		return v
	}
	return nil
}

func report(p *build.Instance, err *packageError) {
	if err != nil {
		p.ReportError(err)
	}
}

// shortPath returns an absolute or relative name for path, whatever is shorter.
func shortPath(cwd, path string) string {
	if cwd == "" {
		return path
	}
	if rel, err := filepath.Rel(cwd, path); err == nil && len(rel) < len(path) {
		return rel
	}
	return path
}

// A packageError describes an error loading information about a package.
type packageError struct {
	ImportStack    []string  // shortest path from package named on command line to this one
	Pos            token.Pos // position of error
	errors.Message           // the error itself
	IsImportCycle  bool      `json:"-"` // the error is an import cycle
	Hard           bool      `json:"-"` // whether the error is soft or hard; soft errors are ignored in some places
}

func (p *packageError) Position() token.Pos         { return p.Pos }
func (p *packageError) InputPositions() []token.Pos { return nil }
func (p *packageError) Path() []string              { return nil }

func (l *loader) errPkgf(importPos []token.Pos, format string, args ...interface{}) *packageError {
	err := &packageError{
		ImportStack: l.stk.Copy(),
		Message:     errors.NewMessage(format, args),
	}
	err.fillPos(l.cfg.Dir, importPos)
	return err
}

func (p *packageError) fillPos(cwd string, positions []token.Pos) {
	if len(positions) > 0 && !p.Pos.IsValid() {
		p.Pos = positions[0]
	}
}

// TODO(localize)
func (p *packageError) Error() string {
	// Import cycles deserve special treatment.
	if p.IsImportCycle {
		return fmt.Sprintf("%s\npackage %s\n", p.Message, strings.Join(p.ImportStack, "\n\timports "))
	}
	if p.Pos.IsValid() {
		// Omit import stack. The full path to the file where the error
		// is the most important thing.
		return p.Pos.String() + ": " + p.Message.Error()
	}
	if len(p.ImportStack) == 0 {
		return p.Message.Error()
	}
	return "package " + strings.Join(p.ImportStack, "\n\timports ") + ": " + p.Message.Error()
}

// noCUEError is the error used by Import to describe a directory
// containing no buildable Go source files. (It may still contain
// test files, files hidden by build tags, and so on.)
type noCUEError struct {
	Package *build.Instance

	Dir     string
	Ignored bool // whether any Go files were ignored due to build tags
}

func (e *noCUEError) Position() token.Pos         { return token.NoPos }
func (e *noCUEError) InputPositions() []token.Pos { return nil }
func (e *noCUEError) Path() []string              { return nil }

// TODO(localize)
func (e *noCUEError) Msg() (string, []interface{}) { return e.Error(), nil }

// TODO(localize)
func (e *noCUEError) Error() string {
	// Count files beginning with _, which we will pretend don't exist at all.
	dummy := 0
	for _, name := range e.Package.IgnoredCUEFiles {
		if strings.HasPrefix(name, "_") {
			dummy++
		}
	}

	// path := shortPath(e.Package.Root, e.Package.Dir)
	path := e.Package.DisplayPath

	if len(e.Package.IgnoredCUEFiles) > dummy {
		// CUE files exist, but they were ignored due to build constraints.
		msg := "build constraints exclude all CUE files in " + path + " (ignored: "
		files := e.Package.IgnoredCUEFiles
		if len(files) > 4 {
			files = append(files[:4], "...")
		}
		for i, f := range files {
			files[i] = filepath.ToSlash(f)
		}
		msg += strings.Join(files, ", ")
		msg += ")"
		return msg
	}
	// if len(e.Package.TestCUEFiles) > 0 {
	// 	// Test CUE files exist, but we're not interested in them.
	// 	// The double-negative is unfortunate but we want e.Package.Dir
	// 	// to appear at the end of error message.
	// 	return "no non-test CUE files in " + e.Package.Dir
	// }
	return "no CUE files in " + path
}

// multiplePackageError describes a directory containing
// multiple buildable Go source files for multiple packages.
type multiplePackageError struct {
	Dir      string   // directory containing files
	Packages []string // package names found
	Files    []string // corresponding files: Files[i] declares package Packages[i]
}

func (e *multiplePackageError) Position() token.Pos         { return token.NoPos }
func (e *multiplePackageError) InputPositions() []token.Pos { return nil }
func (e *multiplePackageError) Path() []string              { return nil }

func (e *multiplePackageError) Msg() (string, []interface{}) {
	return "found packages %s (%s) and %s (%s) in %s", []interface{}{
		e.Packages[0],
		e.Files[0],
		e.Packages[1],
		e.Files[1],
		e.Dir,
	}
}

func (e *multiplePackageError) Error() string {
	// Error string limited to two entries for compatibility.
	format, args := e.Msg()
	return fmt.Sprintf(format, args...)
}
