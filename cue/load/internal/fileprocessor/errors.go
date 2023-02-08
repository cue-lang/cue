package fileprocessor

import (
	"fmt"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/token"
)

// NoFilesError is the error used by Import to describe a directory
// containing no usable source files. (It may still contain
// tool files, files hidden by build tags, and so on.)
type NoFilesError struct {
	Package *build.Instance

	ignored bool // whether any Go files were ignored due to build tags
}

func (e *NoFilesError) Position() token.Pos         { return token.NoPos }
func (e *NoFilesError) InputPositions() []token.Pos { return nil }
func (e *NoFilesError) Path() []string              { return nil }

// TODO(localize)
func (e *NoFilesError) Msg() (string, []interface{}) { return e.Error(), nil }

// TODO(localize)
func (e *NoFilesError) Error() string {
	// Count files beginning with _, which we will pretend don't exist at all.
	dummy := 0
	for _, f := range e.Package.IgnoredFiles {
		if strings.HasPrefix(filepath.Base(f.Filename), "_") {
			dummy++
		}
	}

	// path := shortPath(e.Package.Root, e.Package.Dir)
	path := e.Package.DisplayPath

	if len(e.Package.IgnoredFiles) > dummy {
		b := strings.Builder{}
		b.WriteString("build constraints exclude all CUE files in ")
		b.WriteString(path)
		b.WriteString(":")
		// CUE files exist, but they were ignored due to build constraints.
		for _, f := range e.Package.IgnoredFiles {
			b.WriteString("\n    ")
			b.WriteString(filepath.ToSlash(e.Package.RelPath(f)))
			if f.ExcludeReason != nil {
				b.WriteString(": ")
				b.WriteString(f.ExcludeReason.Error())
			}
		}
		return b.String()
	}
	// if len(e.Package.TestCUEFiles) > 0 {
	// 	// Test CUE files exist, but we're not interested in them.
	// 	// The double-negative is unfortunate but we want e.Package.Dir
	// 	// to appear at the end of error message.
	// 	return "no non-test CUE files in " + e.Package.Dir
	// }
	return "no CUE files in " + path
}

// MultiplePackageError describes an attempt to build a package composed of
// CUE files from different packages.
type MultiplePackageError struct {
	Dir      string   // directory containing files
	Packages []string // package names found
	Files    []string // corresponding files: Files[i] declares package Packages[i]
}

func (e *MultiplePackageError) Position() token.Pos         { return token.NoPos }
func (e *MultiplePackageError) InputPositions() []token.Pos { return nil }
func (e *MultiplePackageError) Path() []string              { return nil }

func (e *MultiplePackageError) Msg() (string, []interface{}) {
	return "found packages %q (%s) and %s (%s) in %q", []interface{}{
		e.Packages[0],
		e.Files[0],
		e.Packages[1],
		e.Files[1],
		e.Dir,
	}
}

func (e *MultiplePackageError) Error() string {
	// Error string limited to two entries for compatibility.
	format, args := e.Msg()
	return fmt.Sprintf(format, args...)
}
