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

package load

import (
	"cuelang.org/go/cue/ast"
	"errors"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// overlayFile is used to implement individual overlay files
// it implements both the fs.File and fs.DirInfo interfaces
type overlayFile struct {
	basename  string
	contents  []byte
	file      *ast.File
	modtime   time.Time
	isDir     bool
	readIndex int64
}

func (f *overlayFile) Name() string       { return f.basename }
func (f *overlayFile) Size() int64        { return int64(len(f.contents)) }
func (f *overlayFile) Mode() fs.FileMode  { return 0644 }
func (f *overlayFile) ModTime() time.Time { return f.modtime }
func (f *overlayFile) IsDir() bool        { return f.isDir }
func (f *overlayFile) Sys() interface{}   { return nil }

// Type allows Overlay file to be a fs.DirInfo
func (f *overlayFile) Type() fs.FileMode {
	return f.Mode()
}

func (f *overlayFile) Info() (fs.FileInfo, error) {
	return f, nil
}

func (f *overlayFile) Stat() (fs.FileInfo, error) {
	return f, nil
}

func (f *overlayFile) Read(b []byte) (int, error) {
	n := copy(b, f.contents[f.readIndex:])
	f.readIndex += int64(n)

	if f.readIndex == f.Size() {
		return n, io.EOF
	}

	return n, nil
}

func (f *overlayFile) Close() error {
	return nil // Sources have already been closed
}

func (f *overlayFile) Open() overlayFile {
	f.readIndex = 0

	// Pass by value to allow overlay file to have unique read index
	return *f
}

type overlayFS struct {
	overlayFiles map[string]*overlayFile
	cwd          string
}

// getAbsPath gets the absolute path, using CWD as the parent directory for
// relative paths
func (fsys *overlayFS) getAbsPath(path string) string {
	path = filepath.Clean(path)

	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(fsys.cwd, path))
	}

	return filepath.ToSlash(path)
}

func (fsys *overlayFS) init(overlay map[string]Source) error {
	fsys.overlayFiles = make(map[string]*overlayFile)

	// Creates an entire directory structure containing overlay files
	// if necessary. Allows directory traversal
	for path, source := range overlay {
		path = fsys.getAbsPath(path)

		b, file, err := source.contents()

		if err != nil {
			return err
		}

		fsys.overlayFiles[path] = &overlayFile{
			basename: pathpkg.Base(path),
			contents: b,
			file:     file,
			modtime:  time.Now(),
		}
		dir := path

		for {
			prevdir := dir
			dir := pathpkg.Dir(dir)
			if dir == prevdir || dir == "" {
				break
			}

			_, err := fs.ReadDir(fsys, dir)

			if err != nil {
				fsys.overlayFiles[dir] = &overlayFile{
					basename: pathpkg.Base(dir),
					modtime:  time.Now(),
					isDir:    true,
				}
			} else {
				break
			}
		}
	}

	return nil
}

// ReadDir implements the corresponding function from the io/fs interface
func (fsys *overlayFS) ReadDir(name string) (list []fs.DirEntry, err error) {
	// Modified from go1.17.7 src/io/fs/readdir.go to support Overlay

	for k, fi := range fsys.overlayFiles {
		if k == name {
			// Overlay directory cannot be child of itself
			continue
		}

		relPath, err := filepath.Rel(name, k)

		relPath = filepath.ToSlash(relPath)
		if err != nil || relPath == ".." || strings.HasPrefix(relPath, "../") { // Ignore all not relative overlay files
			continue
		}

		isImmediateChild := filepath.Base(relPath) == relPath

		if isImmediateChild {
			list = append(list, fi)
		}
	}

	// If the directory is an overlay directory, the filesystem should not be accessed
	if _, ok := fsys.overlayFiles[name]; ok {
		return list, nil
	}

	name = filepath.FromSlash(name)

	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dir, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not implemented")}
	}

	directoryList, err := dir.ReadDir(-1)
	sort.Slice(directoryList, func(i, j int) bool { return directoryList[i].Name() < directoryList[j].Name() })

	list = append(directoryList, list...)

	return list, err
}

// Open implements the corresponding function from the io/fs interface
func (fsys *overlayFS) Open(name string) (fs.File, error) {
	name = fsys.getAbsPath(name)

	if fsys.overlayFiles != nil {
		if fi, ok := fsys.overlayFiles[name]; ok {
			f := fi.Open()
			return &f, nil
		}
	}

	name = filepath.FromSlash(name)

	// Convert from standard path to OS specific path in FS
	f, err := os.Open(name)
	if err != nil {
		return nil, err // nil fs.File
	}
	return f, nil
}

// Stat implements the corresponding function from the io/fs interface
func (fsys *overlayFS) Stat(name string) (fs.FileInfo, error) {
	name = fsys.getAbsPath(name)

	if fsys.overlayFiles != nil {
		if fi, ok := fsys.overlayFiles[name]; ok {
			return fi, nil
		}
	}

	name = filepath.FromSlash(name)

	// Convert from standard path to OS specific path in FS
	f, err := os.Stat(name)

	if err != nil {
		return nil, err
	}
	return f, nil
}
