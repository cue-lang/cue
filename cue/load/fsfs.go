// Copyright 2025 The CUE Authors
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
	stderrs "errors"
	"fmt"
	"io"
	"io/fs"
	iofs "io/fs"
	"path"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/mod/module"
)

// fsFilesystem implements [fileSystem] backed by an [io/fs.FS].
// All paths use forward slashes and may start with "/",
// which is stripped before passing to the underlying FS.
type fsFilesystem struct {
	fsys      fs.FS
	cwd       string
	fileCache *fileCache
}

func newFSFileSystem(cfg *Config) (*fsFilesystem, error) {
	return &fsFilesystem{
		fsys:      cfg.FS,
		cwd:       cfg.Dir,
		fileCache: newFileCache(cfg),
	}, nil
}

func (fs *fsFilesystem) readDir(p string) ([]iofs.DirEntry, errors.Error) {
	entries, err := iofs.ReadDir(fs.fsys, fs.fsPath(p))
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "readDir")
	}
	return entries, nil
}

func (fs *fsFilesystem) stat(p string) (iofs.FileInfo, errors.Error) {
	fi, err := iofs.Stat(fs.fsys, fs.fsPath(p))
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "stat")
	}
	return fi, nil
}

func (fs *fsFilesystem) openFile(p string) (io.ReadCloser, errors.Error) {
	f, err := fs.fsys.Open(fs.fsPath(p))
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "load")
	}
	return f, nil
}

func (fs *fsFilesystem) walk(root string, f walkFunc) error {
	root = fs.fsPath(root)
	return iofs.WalkDir(fs.fsys, root, func(p string, d iofs.DirEntry, err error) error {
		if p == root && err == nil && !d.IsDir() {
			return errors.Newf(token.NoPos, "path %q is not a directory", root)
		}
		var cueErr errors.Error
		if err != nil {
			cueErr = errors.Wrapf(err, token.NoPos, "walk")
		}
		walkErr := f(p, d, cueErr)
		if walkErr == skipDir {
			return iofs.SkipDir
		}
		return walkErr
	})
}

func (fs *fsFilesystem) getSource(cfg *Config, filename string) (any, error) {
	// Read the file contents because the encoding.NewDecoder would otherwise try
	// to open the file from the OS filesystem.
	// TODO provide encoding.NewDecoder with an fs.FS to avoid
	// this up-front read?
	return iofs.ReadFile(fs.fsys, fs.fsPath(filename))
}

func (fs *fsFilesystem) getCUESyntax(bf *build.File, cfg parser.Config) (*ast.File, error) {
	return fs.fileCache.getCUESyntax(bf, cfg)
}

func (fs *fsFilesystem) ioFS(root string, languageVersion string) fs.FS {
	sub, err := iofs.Sub(fs.fsys, fs.fsPath(root))
	if err != nil {
		panic(fmt.Errorf("invalid root directory %q: %v", root, err))
	}
	return &fsIOFS{
		wideFS:          sub.(wideFS),
		fileCache:       fs.fileCache,
		root:            root,
		languageVersion: languageVersion,
	}
}

// fsPath returns the [fs.FS] path to use for the given
// fileSystem path p.
func (fs *fsFilesystem) fsPath(p string) string {
	p = strings.TrimPrefix(fs.makeAbs(p), "/")
	if p == "" {
		p = "."
	}
	return p
}

func (fs *fsFilesystem) makeAbs(p string) string {
	if path.IsAbs(p) {
		return path.Clean(p)
	}
	return path.Join(fs.cwd, p)
}

// fsIOFS provides an [io/fs.FS] view for the fsFilesystem implementation.
// It implements the same set of interfaces as [overlayIOFS]
// except for [module.OSRootFS].
type fsIOFS struct {
	wideFS
	fileCache       *fileCache
	root            string
	languageVersion string
}

// wideFS declares the [fs] interfaces that
// [fs.Sub] implements.
type wideFS interface {
	fs.FS
	fs.ReadDirFS
	fs.ReadFileFS
	fs.ReadLinkFS
	fs.GlobFS
}

var _ interface {
	wideFS
	module.ReadCUEFS
} = (*fsIOFS)(nil)

// IsDirWithCUEFiles implements [module.ReadCUEFS].
func (fs *fsIOFS) IsDirWithCUEFiles(p string) (bool, error) {
	return false, stderrs.ErrUnsupported
}

// ReadCUEFile implements [module.ReadCUEFS].
func (fs *fsIOFS) ReadCUEFile(p string, cfg parser.Config) (*ast.File, error) {
	if !strings.HasSuffix(p, ".cue") {
		return nil, nil
	}
	absPath := path.Join(fs.root, p)
	key := fileCacheKey{cfg, absPath}
	cache := fs.fileCache
	cache.mu.Lock()
	entry, ok := cache.entries[key]
	cache.mu.Unlock()
	if ok {
		return entry.file, entry.err
	}
	data, err := iofs.ReadFile(fs.wideFS, p)
	if err != nil {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		cache.entries[key] = fileCacheEntry{nil, err}
		return nil, err
	}
	if fs.languageVersion != "" {
		cfg = cfg.Apply(parser.Version(fs.languageVersion))
	}
	return fs.fileCache.getCUESyntax(&build.File{
		Filename: absPath,
		Encoding: build.CUE,
		Source:   data,
	}, cfg)
}
