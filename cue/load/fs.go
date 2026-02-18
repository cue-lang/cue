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
	"bytes"
	"cmp"
	stderrs "errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/mod/module"
	pkgpath "cuelang.org/go/pkg/path"
)

// fileSystem is an interface abstracting filesystem operations
// needed by the loader. There are two implementations:
//   - [overlayFileSystem] for the default OS filesystem with optional overlay
//   - [fsFilesystem] for loading from an [io/fs.FS]
type fileSystem interface {
	readDir(path string) ([]iofs.DirEntry, errors.Error)
	stat(path string) (iofs.FileInfo, errors.Error)
	openFile(path string) (io.ReadCloser, errors.Error)
	walk(root string, f walkFunc) error
	ioFS(root string, languageVersion string) iofs.FS
	getCUESyntax(bf *build.File, cfg parser.Config) (*ast.File, error)
	getSource(cfg *Config, path string) (any, error)
	makeAbs(path string) string
}

type overlayFile struct {
	basename string
	contents []byte
	file     *ast.File
	modtime  time.Time
	isDir    bool
}

func (f *overlayFile) Name() string { return f.basename }
func (f *overlayFile) Size() int64  { return int64(len(f.contents)) }
func (f *overlayFile) Mode() iofs.FileMode {
	if f.isDir {
		return iofs.ModeDir | 0o555
	}
	return 0o444
}
func (f *overlayFile) ModTime() time.Time { return f.modtime }
func (f *overlayFile) IsDir() bool        { return f.isDir }
func (f *overlayFile) Sys() interface{}   { return nil }

// overlayFileSystem implements [fileSystem] using the host OS filesystem
// with an optional overlay of in-memory files.
type overlayFileSystem struct {
	overlayDirs map[string]map[string]*overlayFile
	cwd         string
	pathOS      pkgpath.OS
	fileCache   *fileCache
}

func (fs *overlayFileSystem) getDir(dir string, create bool) map[string]*overlayFile {
	dir = pkgpath.Clean(dir, fs.pathOS)
	m, ok := fs.overlayDirs[dir]
	if !ok && create {
		m = map[string]*overlayFile{}
		fs.overlayDirs[dir] = m
	}
	return m
}

// ioFS returns an implementation of [io/fs.FS] that holds
// the contents of fs under the given filepath root.
//
// Note: we can't return an FS implementation that covers the
// entirety of fs because the overlay paths may not all share
// a common root.
//
// Note also: the returned FS also implements
// [modpkgload.OSRootFS] so that we can map
// the resulting source locations back to the filesystem
// paths required by most of the `cue/load` package
// implementation.
func (fs *overlayFileSystem) ioFS(root string, languageVersion string) iofs.FS {
	return &overlayIOFS{
		fs:              fs,
		root:            root,
		languageVersion: languageVersion,
	}
}

func newOverlayFS(cfg *Config) (*overlayFileSystem, error) {
	fs := &overlayFileSystem{
		cwd:         cfg.Dir,
		pathOS:      cfg.pathOS,
		overlayDirs: map[string]map[string]*overlayFile{},
	}

	// Organize overlay
	for filename, src := range cfg.Overlay {
		if !pkgpath.IsAbs(filename, fs.pathOS) {
			return nil, fmt.Errorf("non-absolute file path %q in overlay", filename)
		}
		// TODO: do we need to further clean the path or check that the
		// specified files are within the root/ absolute files?
		dir := pkgpath.Dir(filename, fs.pathOS)
		base := pkgpath.Base(filename, fs.pathOS)
		m := fs.getDir(dir, true)
		b, file, err := src.contents()
		if err != nil {
			return nil, err
		}
		m[base] = &overlayFile{
			basename: base,
			contents: b,
			file:     file,
			modtime:  time.Now(),
		}

		for {
			base = pkgpath.Base(dir, fs.pathOS)
			parent := pkgpath.Dir(dir, fs.pathOS)
			if parent == dir || parent == "" {
				break
			}
			m := fs.getDir(parent, true)
			if m[base] == nil {
				m[base] = &overlayFile{
					basename: base,
					modtime:  time.Now(),
					isDir:    true,
				}
			}
			dir = parent
		}
	}
	fs.fileCache = newFileCache(cfg)
	return fs, nil
}

func (fs *overlayFileSystem) makeAbs(path string) string {
	if pkgpath.IsAbs(path, fs.pathOS) {
		return path
	}
	return pkgpath.Join([]string{fs.cwd, path}, fs.pathOS)
}

func (fs *overlayFileSystem) readDir(path string) ([]iofs.DirEntry, errors.Error) {
	path = fs.makeAbs(path)
	m := fs.getDir(path, false)
	items, err := os.ReadDir(path)
	if err != nil {
		if !os.IsNotExist(err) || m == nil {
			return nil, errors.Wrapf(err, token.NoPos, "readDir")
		}
	}
	if m == nil {
		return items, nil
	}
	done := map[string]bool{}
	for i, fi := range items {
		done[fi.Name()] = true
		if o := m[fi.Name()]; o != nil {
			items[i] = iofs.FileInfoToDirEntry(o)
		}
	}
	for _, o := range m {
		if !done[o.Name()] {
			items = append(items, iofs.FileInfoToDirEntry(o))
		}
	}
	slices.SortFunc(items, func(a, b iofs.DirEntry) int {
		return cmp.Compare(a.Name(), b.Name())
	})
	return items, nil
}

func (fs *overlayFileSystem) getOverlay(path string) *overlayFile {
	dir := pkgpath.Dir(path, fs.pathOS)
	base := pkgpath.Base(path, fs.pathOS)
	if m := fs.getDir(dir, false); m != nil {
		return m[base]
	}
	return nil
}

func (fs *overlayFileSystem) stat(path string) (iofs.FileInfo, errors.Error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		return fi, nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "stat")
	}
	return fi, nil
}

func (fs *overlayFileSystem) lstat(path string) (iofs.FileInfo, errors.Error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		return fi, nil
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "stat")
	}
	return fi, nil
}

func (fs *overlayFileSystem) openFile(path string) (io.ReadCloser, errors.Error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		return io.NopCloser(bytes.NewReader(fi.contents)), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "load")
	}
	return f, nil
}

var skipDir = errors.Newf(token.NoPos, "skip directory")

type walkFunc func(path string, entry iofs.DirEntry, err errors.Error) errors.Error

func (fs *overlayFileSystem) walk(root string, f walkFunc) error {
	info, err := fs.lstat(root)
	entry := iofs.FileInfoToDirEntry(info)
	if err != nil {
		err = f(root, entry, err)
	} else if !info.IsDir() {
		return errors.Newf(token.NoPos, "path %q is not a directory", root)
	} else {
		err = fs.walkRec(root, entry, f)
	}
	if err == skipDir {
		return nil
	}
	return err

}

func (fs *overlayFileSystem) walkRec(path string, entry iofs.DirEntry, f walkFunc) errors.Error {
	if !entry.IsDir() {
		return f(path, entry, nil)
	}

	dir, err := fs.readDir(path)
	err1 := f(path, entry, err)

	// If err != nil, walk can't walk into this directory.
	// err1 != nil means walkFn want walk to skip this directory or stop walking.
	// Therefore, if one of err and err1 isn't nil, walk will return.
	if err != nil || err1 != nil {
		// The caller's behavior is controlled by the return value, which is decided
		// by walkFn. walkFn may ignore err and return nil.
		// If walkFn returns SkipDir, it will be handled by the caller.
		// So walk should return whatever walkFn returns.
		return err1
	}

	for _, entry := range dir {
		filename := pkgpath.Join([]string{path, entry.Name()}, fs.pathOS)
		err = fs.walkRec(filename, entry, f)
		if err != nil {
			if !entry.IsDir() || err != skipDir {
				return err
			}
		}
	}
	return nil
}

func (fs *overlayFileSystem) getSource(cfg *Config, path string) (any, error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		if fi.file != nil {
			return fi.file, nil
		}
		return fi.contents, nil
	}

	// If the input file is stdin or a non-regular file,
	// such as a named pipe or a device file, we can only read it once.
	// Given that later on we may consume the source multiple times,
	// such as first to only parse the imports and later to parse the whole file,
	// read the whole file here upfront and buffer the bytes.
	//
	// TODO(perf): this causes an upfront "stat" syscall for every input file,
	// which is wasteful given that in the majority of cases we deal with regular files.
	// Consider doing the buffering the first time we open the file later on.
	// whether the overlay provides the source.
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		// A regular file can be read with the [encoding.NewDecoder] logic
		// without the need to provide the source.
		return nil, nil
	}
	return os.ReadFile(path)
}

func (fs *overlayFileSystem) getCUESyntax(bf *build.File, cfg parser.Config) (*ast.File, error) {
	return fs.fileCache.getCUESyntax(bf, cfg)
}

var _ interface {
	iofs.FS
	iofs.ReadDirFS
	iofs.ReadFileFS
	module.OSRootFS
} = (*overlayIOFS)(nil)

type overlayIOFS struct {
	fs              *overlayFileSystem
	root            string
	languageVersion string
}

func (fs *overlayIOFS) OSRoot() string {
	return fs.root
}

func (fs *overlayIOFS) Open(name string) (iofs.File, error) {
	fpath, err := fs.absPathFromFSPath(name)
	if err != nil {
		return nil, err
	}
	r, err := fs.fs.openFile(fpath)
	if err != nil {
		return nil, err // TODO convert filepath in error to fs path
	}
	return &overlayIOFSFile{
		fs:   fs.fs,
		path: fpath,
		rc:   r,
	}, nil
}

func (fs *overlayIOFS) absPathFromFSPath(name string) (string, error) {
	if !iofs.ValidPath(name) {
		return "", fmt.Errorf("invalid io/fs path %q", name)
	}
	// Technically we should mimic Go's internal/safefilepath.fromFS
	// functionality here, but as we're using this in a relatively limited
	// context, we can just prohibit some characters.
	if strings.ContainsAny(name, ":\\") {
		return "", fmt.Errorf("invalid io/fs path %q", name)
	}
	return pkgpath.Join([]string{fs.root, name}, fs.fs.pathOS), nil
}

// ReadDir implements [io/fs.ReadDirFS].
func (fs *overlayIOFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	fpath, err := fs.absPathFromFSPath(name)
	if err != nil {
		return nil, err
	}
	return fs.fs.readDir(fpath)
}

// ReadFile implements [io/fs.ReadFileFS].
func (fs *overlayIOFS) ReadFile(name string) ([]byte, error) {
	fpath, err := fs.absPathFromFSPath(name)
	if err != nil {
		return nil, err
	}
	if fi := fs.fs.getOverlay(fpath); fi != nil {
		return bytes.Clone(fi.contents), nil
	}
	return os.ReadFile(fpath)
}

var _ module.ReadCUEFS = (*overlayIOFS)(nil)

// IsDirWithCUEFiles implements [module.ReadCUEFS]
func (fs *overlayIOFS) IsDirWithCUEFiles(path string) (bool, error) {
	return false, stderrs.ErrUnsupported
}

// ReadCUEFile implements [module.ReadCUEFS] by
// reading and updating the syntax file cache, which
// is shared with the cache used by the [fileSystem.getCUESyntax]
// method.
func (fs *overlayIOFS) ReadCUEFile(path string, cfg parser.Config) (*ast.File, error) {
	if !strings.HasSuffix(path, ".cue") {
		return nil, nil
	}
	fpath, err := fs.absPathFromFSPath(path)
	if err != nil {
		return nil, err
	}
	key := fileCacheKey{cfg, fpath}
	cache := fs.fs.fileCache
	cache.mu.Lock()
	entry, ok := cache.entries[key]
	cache.mu.Unlock()
	if ok {
		return entry.file, entry.err
	}
	var data []byte
	if fi := fs.fs.getOverlay(fpath); fi != nil {
		if fi.file != nil {
			// No need for a cache if we've got the contents in *ast.File
			// form already.
			return fi.file, nil
		}
		data = fi.contents
	} else {
		data, err = os.ReadFile(fpath)
		if err != nil {
			cache.mu.Lock()
			defer cache.mu.Unlock()
			cache.entries[key] = fileCacheEntry{nil, err}
			return nil, err
		}
	}
	if fs.languageVersion != "" {
		cfg = cfg.Apply(parser.Version(fs.languageVersion))
	}
	return fs.fs.getCUESyntax(&build.File{
		Filename: fpath,
		Encoding: build.CUE,
		//		Form:     build.Schema,
		Source: data,
	}, cfg)
}

// overlayIOFSFile implements [io/fs.File] for the overlay filesystem.
type overlayIOFSFile struct {
	fs      *overlayFileSystem
	path    string
	rc      io.ReadCloser
	entries []iofs.DirEntry
}

var _ interface {
	iofs.File
	iofs.ReadDirFile
} = (*overlayIOFSFile)(nil)

func (f *overlayIOFSFile) Stat() (iofs.FileInfo, error) {
	return f.fs.stat(f.path)
}

func (f *overlayIOFSFile) Read(buf []byte) (int, error) {
	return f.rc.Read(buf)
}

func (f *overlayIOFSFile) Close() error {
	return f.rc.Close()
}

func (f *overlayIOFSFile) ReadDir(n int) ([]iofs.DirEntry, error) {
	if f.entries == nil {
		entries, err := f.fs.readDir(f.path)
		if err != nil {
			return entries, err
		}
		if entries == nil {
			entries = []iofs.DirEntry{}
		}
		f.entries = entries
	}
	if n <= 0 {
		entries := f.entries
		f.entries = f.entries[len(f.entries):]
		return entries, nil
	}
	var err error
	if n >= len(f.entries) {
		n = len(f.entries)
		err = io.EOF
	}
	entries := f.entries[:n]
	f.entries = f.entries[n:]
	return entries, err
}

func newFileCache(c *Config) *fileCache {
	return &fileCache{
		config: encoding.Config{
			// Note: no need to pass Stdin, as we take care
			// always to pass a non-nil source when the file is "-".
			ParseFile: c.ParseFile,
		},
		ctx:     cuecontext.New(),
		entries: make(map[fileCacheKey]fileCacheEntry),
	}
}

// fileCache caches data derived from the file system.
type fileCache struct {
	config  encoding.Config
	ctx     *cue.Context
	mu      sync.Mutex
	entries map[fileCacheKey]fileCacheEntry
}

func (fc *fileCache) getCUESyntax(bf *build.File, cfg parser.Config) (*ast.File, error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if bf.Encoding != build.CUE {
		panic("getCUESyntax called with non-CUE file encoding")
	}
	key := fileCacheKey{cfg, bf.Filename}
	// When it's a regular CUE file with no funny stuff going on, we
	// check and update the syntax cache.
	useCache := bf.Form == "" && bf.Interpretation == ""
	if useCache {
		if syntax, ok := fc.entries[key]; ok {
			return syntax.file, syntax.err
		}
	}
	encodingCfg := fc.config
	encodingCfg.ParserConfig = cfg
	d := encoding.NewDecoder(fc.ctx, bf, &encodingCfg)
	defer d.Close()
	// Note: CUE files can never have multiple file parts.
	f, err := d.File(), d.Err()
	if useCache {
		fc.entries[key] = fileCacheEntry{f, err}
	}
	return f, err
}

type fileCacheKey struct {
	cfg  parser.Config
	path string
}

type fileCacheEntry struct {
	// TODO cache directory information too.

	// file caches the work involved when decoding a file into an *ast.File.
	// This can happen multiple times for the same file, for example when it is present in
	// multiple different build instances in the same directory hierarchy.
	file *ast.File
	err  error
}
