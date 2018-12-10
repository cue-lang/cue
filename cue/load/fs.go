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
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// TODO: remove this file if we know we don't need it.

// A fileSystem specifies the supporting context for a build.
type fileSystem struct {
	// By default, Import uses the operating system's file system calls
	// to read directories and files. To read from other sources,
	// callers can set the following functions. They all have default
	// behaviors that use the local file system, so clients need only set
	// the functions whose behaviors they wish to change.

	// JoinPath joins the sequence of path fragments into a single path.
	// If JoinPath is nil, Import uses filepath.Join.
	JoinPath func(elem ...string) string

	// SplitPathList splits the path list into a slice of individual paths.
	// If SplitPathList is nil, Import uses filepath.SplitList.
	SplitPathList func(list string) []string

	// IsAbsPath reports whether path is an absolute path.
	// If IsAbsPath is nil, Import uses filepath.IsAbs.
	IsAbsPath func(path string) bool

	// IsDir reports whether the path names a directory.
	// If IsDir is nil, Import calls os.Stat and uses the result's IsDir method.
	IsDir func(path string) bool

	// HasSubdir reports whether dir is a subdirectory of
	// (perhaps multiple levels below) root.
	// If so, HasSubdir sets rel to a slash-separated path that
	// can be joined to root to produce a path equivalent to dir.
	// If HasSubdir is nil, Import uses an implementation built on
	// filepath.EvalSymlinks.
	HasSubdir func(root, dir string) (rel string, ok bool)

	// ReadDir returns a slice of os.FileInfo, sorted by Name,
	// describing the content of the named directory.
	// If ReadDir is nil, Import uses ioutil.ReadDir.
	ReadDir func(dir string) ([]os.FileInfo, error)

	// OpenFile opens a file (not a directory) for reading.
	// If OpenFile is nil, Import uses os.Open.
	OpenFile func(path string) (io.ReadCloser, error)
}

// JoinPath calls ctxt.JoinPath (if not nil) or else filepath.Join.
func (ctxt *fileSystem) joinPath(elem ...string) string {
	if f := ctxt.JoinPath; f != nil {
		return f(elem...)
	}
	return filepath.Join(elem...)
}

// splitPathList calls ctxt.SplitPathList (if not nil) or else filepath.SplitList.
func (ctxt *fileSystem) splitPathList(s string) []string {
	if f := ctxt.SplitPathList; f != nil {
		return f(s)
	}
	return filepath.SplitList(s)
}

// isAbsPath calls ctxt.IsAbsPath (if not nil) or else filepath.IsAbs.
func (ctxt *fileSystem) isAbsPath(path string) bool {
	if f := ctxt.IsAbsPath; f != nil {
		return f(path)
	}
	return filepath.IsAbs(path)
}

// isDir calls ctxt.IsDir (if not nil) or else uses os.Stat.
func (ctxt *fileSystem) isDir(path string) bool {
	if f := ctxt.IsDir; f != nil {
		return f(path)
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// hasSubdir calls ctxt.HasSubdir (if not nil) or else uses
// the local file system to answer the question.
func (ctxt *fileSystem) hasSubdir(root, dir string) (rel string, ok bool) {
	if f := ctxt.HasSubdir; f != nil {
		return f(root, dir)
	}

	// Try using paths we received.
	if rel, ok = hasSubdir(root, dir); ok {
		return
	}

	// Try expanding symlinks and comparing
	// expanded against unexpanded and
	// expanded against expanded.
	rootSym, _ := filepath.EvalSymlinks(root)
	dirSym, _ := filepath.EvalSymlinks(dir)

	if rel, ok = hasSubdir(rootSym, dir); ok {
		return
	}
	if rel, ok = hasSubdir(root, dirSym); ok {
		return
	}
	return hasSubdir(rootSym, dirSym)
}

func hasSubdir(root, dir string) (rel string, ok bool) {
	const sep = string(filepath.Separator)
	root = filepath.Clean(root)
	if !strings.HasSuffix(root, sep) {
		root += sep
	}
	dir = filepath.Clean(dir)
	if !strings.HasPrefix(dir, root) {
		return "", false
	}
	return filepath.ToSlash(dir[len(root):]), true
}

// ReadDir calls ctxt.ReadDir (if not nil) or else ioutil.ReadDir.
func (ctxt *fileSystem) readDir(path string) ([]os.FileInfo, error) {
	if f := ctxt.ReadDir; f != nil {
		return f(path)
	}
	return ioutil.ReadDir(path)
}

// openFile calls ctxt.OpenFile (if not nil) or else os.Open.
func (ctxt *fileSystem) openFile(path string) (io.ReadCloser, error) {
	if fn := ctxt.OpenFile; fn != nil {
		return fn(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err // nil interface
	}
	return f, nil
}

// isFile determines whether path is a file by trying to open it.
// It reuses openFile instead of adding another function to the
// list in Context.
func (ctxt *fileSystem) isFile(path string) bool {
	f, err := ctxt.openFile(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
