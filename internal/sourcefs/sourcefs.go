// Copyright 2019 CUE Authors
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

package sourcefs

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"time"

	"cuelang.org/go/internal/ospath"
)

type sourceFS struct {
	root *sourceFSFile
	// Could potentially have path->*sourceFSFile map here for fast lookup.
}

// sourceFSFile represents an entry in the file system.
// It implements [fs.FileInfo] and [fs.DirEntry].
type sourceFSFile struct {
	name         string // Single-level directory name of this entry.
	originalPath string // Entire path to original name.
	entries      []*sourceFSFile
	data         Source
}

type SourceFile interface {
	fs.File
	// OriginalPath returns the original path used for the file,
	// or the empty string if not known.
	OriginalPath() string
	// Source returns the Source for the file, or nil if not known.
	Source() Source
}

// NewOSSourceFS returns an [fs.FS]
// implementation given a map from OS-specific file paths to the
// source to use. The non-directory [io.File] entries returned by the filesystem implement
// [SourceFile].
//
// File names are canonicalized inside the file system. The [SourceFile.OriginalPath]
// method can be used to discover the original path used for a file.
func NewOSSourceFS(entries map[string]Source) (fs.FS, error) {
	return newOSSourceFS(entries, ospath.CurrentOS, filepath.Abs)
}

// newOSSourceFS is the internal version of NewOSSourceFS that exposes two extra parameters
// so we can test on different platforms. The OS is specified by the os parameter; the abs function is used to
// determine absolute paths.
func newOSSourceFS(entries map[string]Source, os ospath.OS, abs func(string) (string, error)) (fs.FS, error) {
	entries1 := make(map[string]Source)
	originalPaths := make(map[string]string)
	for name, src := range entries {
		name1, err := canonicalPath(name, os, abs)
		if err != nil {
			return nil, err
		}
		if old, ok := originalPaths[name1]; ok {
			return nil, fmt.Errorf("duplicate file overlay entry for %q (clashes with %q)", name, old)
		}
		originalPaths[name1] = name
		entries1[name1] = src
	}
	return newSourceFS0(entries1, originalPaths)
}

// NewSourceFS returns an [fs.FS] implementation that contains
// all the entries in the given map. The keys must all conform to
// [fs.ValidPath].
func NewSourceFS(entries map[string]Source) (fs.FS, error) {
	originalPaths := make(map[string]string)
	entries1 := make(map[string]Source)
	for p, src := range entries {
		if !fs.ValidPath(p) {
			return nil, fmt.Errorf("%q is not a valid io/fs.FS path", p)
		}
		p1 := path.Clean(p)
		entries1[p1] = src
		originalPaths[p1] = p
	}
	return newSourceFS0(entries1, originalPaths)
}

// newSourceFS0 is the underlying implementation of newSourceFS and newOSSourceFS.
// It assumes that all keys in entries have been canonicalized.
func newSourceFS0(entries map[string]Source, originalPaths map[string]string) (fs.FS, error) {
	srcFS := &sourceFS{
		root: &sourceFSFile{},
	}
	for name, src := range entries {
		if src == nil {
			// TODO Could panic instead?
			return nil, fmt.Errorf("%q has nil Source", originalPaths[name])
		}
		if err := srcFS.addFile(name, originalPaths[name], src); err != nil {
			return nil, err
		}
	}
	return srcFS, nil
}

// Open implements [fs.Open] by returning a file from the
// original map, or a directory from one of the parents of those
// entries. For regular files, the returned [fs.File] will implement
// [SourceFile].
func (sf *sourceFS) Open(name string) (fs.File, error) {
	name0 := name
	name = path.Clean(name)
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name0,
			Err:  fs.ErrInvalid,
		}
	}
	if name == "." {
		return sf.root.open()
	}
	dir := sf.root
	for {
		first, rest, hasMore := strings.Cut(name, "/")
		entry := dir.lookup(first)
		if entry == nil {
			return nil, &fs.PathError{
				Op:   "open",
				Path: name0,
				Err:  fs.ErrNotExist,
			}
		}
		if !hasMore {
			return entry.open()
		}
		if entry.data != nil {
			// Can't descend into a file.
			return nil, &fs.PathError{
				Op:   "open",
				Path: name0,
				Err:  fs.ErrNotExist,
			}
		}
		dir = entry
		name = rest
	}
}

// addFile adds the file with io/fs path name and contents src
// to the fs. It adds parent directory entries for all elements in the
// path if not already present.
func (fs *sourceFS) addFile(name, originalPath string, src Source) error {
	dir := fs.root
	for {
		first, rest, ok := strings.Cut(name, "/")
		if !ok {
			if entry := dir.lookup(first); entry != nil {
				if entry.data != nil {
					return fmt.Errorf("duplicate entry %q", originalPath)
				}
				return fmt.Errorf("file %q has another file nested within it", originalPath)
			}
			// We've reached the final directory.
			f := &sourceFSFile{
				name:         first,
				originalPath: originalPath,
				data:         src,
			}
			dir.entries = append(dir.entries, f)
			return nil
		}
		if entry := dir.lookup(first); entry != nil {
			if entry.data != nil {
				return fmt.Errorf("file %q has another file nested within it", entry.originalPath)
			}
			dir = entry
			name = rest
			continue
		}
		newDir := &sourceFSFile{
			name:    first,
			entries: []*sourceFSFile{},
		}
		dir.entries = append(dir.entries, newDir)
		dir = newDir
		name = rest
	}
}

var _ SourceFile = (*openFile)(nil)

func (f *sourceFSFile) open() (fs.File, error) {
	if f.IsDir() {
		return &openDir{
			entry: f,
		}, nil
	}
	// TODO fetch the data only when it's actually asked for.
	data, _, err := f.data.contents()
	if err != nil {
		return nil, err
	}
	return &openFile{
		entry:  f,
		Reader: bytes.NewReader(data),
	}, nil
}

func (f *sourceFSFile) lookup(name string) *sourceFSFile {
	for _, entry := range f.entries {
		if entry.name == name {
			return entry
		}
	}
	return nil
}

// Name implements [fs.DirEntry.Name] and [fs.FileInfo.Name].
func (f *sourceFSFile) Name() string {
	return f.name
}

// IsDir implements [fs.DirEntry.IsDir] and [fs.FileInfo.IsDir].
func (f *sourceFSFile) IsDir() bool {
	return f.data == nil
}

// Type implements [fs.DirEntry.Type].
func (f *sourceFSFile) Type() fs.FileMode {
	if f.IsDir() {
		return fs.ModeDir
	}
	return 0
}

// Info implements [fs.DirEntry.Info].
func (f *sourceFSFile) Info() (fs.FileInfo, error) {
	return f, nil
}

// Size implements [fs.FileInfo.Size]
func (f *sourceFSFile) Size() int64 {
	if f.IsDir() {
		return 0
	}
	data, _, err := f.data.contents()
	if err != nil {
		// TODO panic instead?
		return 0
	}
	return int64(len(data))
}

// Mode implements [fs.FileInfo.Mode].
func (f *sourceFSFile) Mode() fs.FileMode {
	if f.IsDir() {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

// ModTime implements [fs.FileInfo.ModTime].
func (f *sourceFSFile) ModTime() time.Time {
	return time.Time{}
}

// Sys implements [fs.FileInfo.Sys].
func (f *sourceFSFile) Sys() any {
	return nil
}

// openDir represents an open directory. It implements [fs.ReadDirFile].
type openDir struct {
	entry    *sourceFSFile
	dirEntry int
}

// ReadDir implements [fs.ReadDirFile.ReadDir].
func (f *openDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.dirEntry >= len(f.entry.entries) && n > 0 {
		return nil, io.EOF
	}
	entries := f.entry.entries[f.dirEntry:]
	if n >= 0 && len(entries) > n {
		entries = entries[:n]
	}
	entries1 := make([]fs.DirEntry, len(entries))
	for i, e := range entries {
		entries1[i] = e
	}
	f.dirEntry += len(entries)
	return entries1, nil
}

// Read implements [fs.ReadDirFile.Read].
func (f *openDir) Read([]byte) (int, error) {
	return 0, fmt.Errorf("cannot read bytes from a directory")
}

// Stat implements [fs.ReadDirFile.Stat].
func (f *openDir) Stat() (fs.FileInfo, error) {
	return f.entry, nil
}

// Close implements [fs.ReadDirFile.Close].
func (f *openDir) Close() error {
	// Note: we don't prevent further reads after the close, but that's probably
	// OK for what we're using it for.
	return nil
}

type openFile struct {
	entry *sourceFSFile
	// By embedding bytes.Reader, we're implementing [fs.File.Read]
	*bytes.Reader
}

// Stat implements [fs.File.Stat].
func (f *openFile) Stat() (fs.FileInfo, error) {
	return f.entry, nil
}

// OriginalPath implements [SourceFile.OriginalPath].
func (f *openFile) OriginalPath() string {
	return f.entry.originalPath
}

// Source implements [SourceFile.Source].
func (f *openFile) Source() Source {
	return f.entry.data
}

// Close implements [fs.File.Close].
func (f *openFile) Close() error {
	return nil
}

// canonicalPath returns an absolute "canonical" version
// of the path p. Under Windows, because there is no
// single root, but io/fs requires a single root, we map
// the volume name to a single element at the root.
// Any forward slashes in the volume name will be converted
// to backslashes, so, for example under Windows:
//
//	//foo/bar/baz
//
// will map to
//
//	\\foo\bar/baz
func canonicalPath(p string, os ospath.OS, abs func(string) (string, error)) (string, error) {
	p0 := p
	p, err := abs(p)
	if err != nil {
		return "", err
	}
	// Note: we're assuming that under Windows, ospath.OS.VolumeName
	// will always return a non-empty string, and that under non-Windows
	// systems, the path separator is /.
	if v := os.VolumeName(p); v != "" {
		// Replace forward slash with backslash so the resulting
		// path element conforms to fs.ValidPath.
		v1 := strings.ReplaceAll(v, "/", "\\")
		p = path.Join(v1, os.ToSlash(p[len(v):]))
	} else {
		// Remove the leading / that represents the root directory.
		// io/fs paths are unrooted.
		p = strings.TrimPrefix(p, "/")
	}
	if !fs.ValidPath(p) {
		panic(fmt.Errorf("canonicalPath produced an invalid path %q from %q", p, p0))
	}
	return p, nil
}
