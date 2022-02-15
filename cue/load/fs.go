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
	"io/fs"
	pathpkg "path"
	"path/filepath"
)

func isDir(fsys fs.FS, path string) bool {
	fi, err := fs.Stat(fsys, path)

	return err == nil && fi.IsDir()
}

// Modified from /go1.17.7:src/io/fs/walk.go

// walkDir recursively descends path, calling walkDirFn.
func walkDir(fsys fs.FS, name string, d fs.DirEntry, walkDirFn fs.WalkDirFunc) error {

	name = filepath.ToSlash(name)
	if err := walkDirFn(name, d, nil); err != nil || !d.IsDir() {
		if err == fs.SkipDir && d.IsDir() {
			// Successfully skipped directory.
			err = nil
		}
		return err
	}

	// Symlinks will only work when reading from the OS file system.
	// Only use this to determine what directory to read from not as part of
	// name to allow for the symlinks to determine directory structure
	readDirName, err := filepath.EvalSymlinks(name)
	if err != nil {
		readDirName = name
	}

	dirs, err := fs.ReadDir(fsys, readDirName)
	if err != nil {
		// Second call, to report ReadDir error.
		err = walkDirFn(name, d, err)
		if err != nil {
			return err
		}
	}

	for _, d1 := range dirs {
		name1 := pathpkg.Join(name, d1.Name())
		if err := walkDir(fsys, name1, d1, walkDirFn); err != nil {
			if err == fs.SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

// WalkDirWithSymlinks walks the file tree rooted at root, calling fn for each file or
// directory in the tree, including root.
//
// All errors that arise visiting files and directories are filtered by fn:
// see the fs.WalkDirFunc documentation for details.
//
// The files are walked in lexical order, which makes the output deterministic
// but requires WalkDirWithSymlinks to read an entire directory into memory before proceeding
// to walk that directory.
//
// WalkDirWithSymlinks does follow symbolic links found in directories,
// which is a modification from the original functions
func WalkDirWithSymlinks(fsys fs.FS, root string, fn fs.WalkDirFunc) error {
	info, err := fs.Stat(fsys, root)
	if err != nil {
		err = fn(filepath.ToSlash(root), nil, err)
	} else {
		err = walkDir(fsys, root, &statDirEntry{info}, fn)
	}
	if err == fs.SkipDir {
		return nil
	}
	return err
}

type statDirEntry struct {
	info fs.FileInfo
}

func (d *statDirEntry) Name() string               { return d.info.Name() }
func (d *statDirEntry) IsDir() bool                { return d.info.IsDir() }
func (d *statDirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d *statDirEntry) Info() (fs.FileInfo, error) { return d.info, nil }
