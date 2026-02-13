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
	"path"
	"path/filepath"
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
)

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
func (f *overlayFile) Sys() any           { return nil }

// loadFSRoot is the synthetic absolute root used when loading from Config.FS.
// It is intentionally not an OS-absolute path to avoid platform-specific semantics.
// All loader paths are rooted here when FS is non-nil.
const loadFSRoot = "@fs"

func isLoaderAbs(p string) bool {
	return strings.HasPrefix(p, loadFSRoot)
}

func canonicalOverlayPath(p string) string {
	p = filepath.ToSlash(p)
	return path.Clean(p)
}

// loadFS provides access to the source filesystem used for loading CUE
// packages and modules. It abstracts over the host filesystem and
// virtual filesystems supplied via Config.FS.
//
// All direct os.* filesystem access must go through this type.
//
// loadFS operates exclusively on absolute loader paths.
// When fs != nil, all paths are rooted at loadFSRoot and use slash separators.
type loadFS struct {
	fs iofs.FS
}

type fakeDirInfo struct{}

func (fakeDirInfo) Name() string        { return "" }
func (fakeDirInfo) Size() int64         { return 0 }
func (fakeDirInfo) Mode() iofs.FileMode { return iofs.ModeDir | 0o555 }
func (fakeDirInfo) ModTime() time.Time  { return time.Time{} }
func (fakeDirInfo) IsDir() bool         { return true }
func (fakeDirInfo) Sys() any            { return nil }

type fakeRootFile struct{ fs iofs.FS }

func (f fakeRootFile) Stat() (iofs.FileInfo, error)           { return fakeDirInfo{}, nil }
func (f fakeRootFile) Read(p []byte) (int, error)             { return 0, io.EOF }
func (f fakeRootFile) Close() error                           { return nil }
func (f fakeRootFile) ReadDir(n int) ([]iofs.DirEntry, error) { return iofs.ReadDir(f.fs, ".") }

// isNotExist reports whether err indicates that a path does not exist.
//
// This helper exists to avoid using os.IsNotExist directly at call sites.
// The loader may operate on a virtual filesystem provided via Config.FS,
// whose errors are not guaranteed to satisfy os.IsNotExist. Instead, all
// filesystem access is normalized to io/fs semantics and tested using
// errors.Is(err, fs.ErrNotExist).
//
// All direct uses of os.* filesystem functions must be confined to loadFS.
func isNotExist(err error) bool {
	return errors.Is(err, iofs.ErrNotExist)
}

func stripFSRoot(path string) string {
	tr := func(p string) string {
		p = strings.ReplaceAll(p, `\`, `/`)
		p = strings.TrimPrefix(p, loadFSRoot)
		return strings.TrimPrefix(p, `/`)
	}

	for strings.Contains(path, loadFSRoot) {
		path = tr(path)
	}

	return path
}

func (l loadFS) Stat(name string) (iofs.FileInfo, error) {
	if l.fs == nil {
		return os.Stat(name)
	}

	strip := stripFSRoot(name)
	if strip == "" {
		return fakeDirInfo{}, nil
	}
	return iofs.Stat(l.fs, strip)
}

func (l loadFS) Lstat(name string) (iofs.FileInfo, error) {
	if l.fs == nil {
		return os.Lstat(name)
	}

	strip := stripFSRoot(name)
	if strip == "" {
		return fakeDirInfo{}, nil
	}
	// fs.FS has no concept of symlinks; fall back to Stat.
	return iofs.Stat(l.fs, strip)
}

func (l loadFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	if l.fs == nil {
		return os.ReadDir(name)
	}

	strip := stripFSRoot(name)
	if strip == "" {
		return iofs.ReadDir(l.fs, ".")
	}
	return iofs.ReadDir(l.fs, strip)
}

func (l loadFS) ReadFile(name string) ([]byte, error) {
	if l.fs == nil {
		return os.ReadFile(name)
	}
	return iofs.ReadFile(l.fs, stripFSRoot(name))
}

func (l loadFS) Open(name string) (iofs.File, error) {
	if l.fs == nil {
		return os.Open(name)
	}

	strip := stripFSRoot(name)
	if strip == "" {
		return fakeRootFile(l), nil
	}
	return l.fs.Open(strip)
}

func (l loadFS) IsAbs(p string) bool {
	if l.fs == nil {
		return filepath.IsAbs(p)
	}
	return isLoaderAbs(p)
}

func (l loadFS) Clean(p string) string {
	if l.fs == nil {
		return filepath.Clean(p)
	}
	return path.Clean(p)
}

func (l loadFS) Join(p ...string) string {
	if l.fs == nil {
		return filepath.Join(p...)
	}
	return path.Join(p...)
}

func (l loadFS) Split(p string) (string, string) {
	if l.fs == nil {
		return filepath.Split(p)
	}
	return path.Split(p)
}

func (l loadFS) Dir(p string) string {
	if l.fs == nil {
		return filepath.Dir(p)
	}
	return path.Dir(p)
}

// A fileSystem specifies the supporting context for a build.
type fileSystem struct {
	cwd string
	lfs loadFS

	fileCache   *fileCache
	overlayDirs map[string]map[string]*overlayFile
}

func (fs *fileSystem) getDir(dir string, create bool) map[string]*overlayFile {
	dir = fs.lfs.Clean(dir)
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
func (fs *fileSystem) ioFS(root string, languageVersion string) iofs.FS {
	return &ioFS{
		fs:              fs,
		root:            root,
		languageVersion: languageVersion,
	}
}

func newFileSystem(cfg *Config) (*fileSystem, error) {
	fs := &fileSystem{
		cwd:         cfg.Dir,
		lfs:         loadFS{fs: cfg.FS},
		overlayDirs: map[string]map[string]*overlayFile{},
	}

	// Organize overlay
	for filename, src := range cfg.Overlay {
		// Normalize overlay path to canonical loader-absolute form.
		filename = fs.makeAbs(filename)

		if !fs.lfs.IsAbs(filename) {
			return nil, fmt.Errorf("non-absolute file path %q in overlay", filename)
		}
		// TODO: do we need to further clean the path or check that the
		// specified files are within the root/ absolute files?
		dir, base := fs.lfs.Split(filename)
		m := fs.getDir(dir, true)
		b, file, err := src.contents()
		if err != nil {
			return nil, err
		}
		base = canonicalOverlayPath(base)
		m[base] = &overlayFile{
			basename: base,
			contents: b,
			file:     file,
			modtime:  time.Now(),
		}

		for {
			prevdir := dir
			dir, base = fs.lfs.Split(fs.lfs.Dir(dir))
			if dir == prevdir || dir == "" {
				break
			}
			m := fs.getDir(dir, true)
			if m[base] == nil {
				m[base] = &overlayFile{
					basename: base,
					modtime:  time.Now(),
					isDir:    true,
				}
			}
		}
	}
	fs.fileCache = newFileCache(cfg)
	return fs, nil
}

func (fs *fileSystem) makeAbs(p string) string {
	if fs.lfs.fs == nil {
		if fs.lfs.IsAbs(p) {
			return p
		}
		return fs.lfs.Join(fs.cwd, p)
	}

	// Normalize OS-rooted paths (Windows: "\foo", *NIX: "/foo")
	p = strings.TrimLeft(p, string(filepath.Separator))

	if isLoaderAbs(p) {
		return path.Clean(p)
	}
	return path.Join(loadFSRoot, p)
}

func (fs *fileSystem) readDir(path string) ([]iofs.DirEntry, errors.Error) {
	path = fs.makeAbs(path)
	m := fs.getDir(path, false)
	items, err := fs.lfs.ReadDir(path)
	if err != nil {
		if !isNotExist(err) || m == nil {
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

func (fs *fileSystem) getOverlay(path string) *overlayFile {
	path = canonicalOverlayPath(path)

	dir, base := fs.lfs.Split(path)
	if m := fs.getDir(dir, false); m != nil {
		return m[base]
	}
	return nil
}

func (fs *fileSystem) stat(path string) (iofs.FileInfo, errors.Error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		return fi, nil
	}
	fi, err := fs.lfs.Stat(path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "stat")
	}
	return fi, nil
}

func (fs *fileSystem) lstat(path string) (iofs.FileInfo, errors.Error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		return fi, nil
	}
	fi, err := fs.lfs.Lstat(path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "stat")
	}
	return fi, nil
}

type fileWithStat struct {
	io.ReadCloser
	info iofs.FileInfo
}

func (f fileWithStat) Stat() (iofs.FileInfo, error) {
	return f.info, nil
}

func (fs *fileSystem) openFileWithStat(path string) (iofs.File, error) {
	path = fs.makeAbs(path)

	if fi := fs.getOverlay(path); fi != nil {
		return fileWithStat{
			ReadCloser: io.NopCloser(bytes.NewReader(fi.contents)),
			info:       fi,
		}, nil
	}

	f, err := fs.lfs.Open(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (fs *fileSystem) openFile(path string) (io.ReadCloser, error) {
	path = fs.makeAbs(path)
	if fi := fs.getOverlay(path); fi != nil {
		return io.NopCloser(bytes.NewReader(fi.contents)), nil
	}

	f, err := fs.lfs.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "load")
	}
	return f, nil
}

var skipDir = errors.Newf(token.NoPos, "skip directory")

type walkFunc func(path string, entry iofs.DirEntry, err errors.Error) errors.Error

func (fs *fileSystem) walk(root string, f walkFunc) error {
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

func (fs *fileSystem) walkRec(path string, entry iofs.DirEntry, f walkFunc) errors.Error {
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
		filename := fs.lfs.Join(path, entry.Name())
		err = fs.walkRec(filename, entry, f)
		if err != nil {
			if !entry.IsDir() || err != skipDir {
				return err
			}
		}
	}
	return nil
}

var _ interface {
	iofs.FS
	iofs.ReadDirFS
	iofs.ReadFileFS
	module.OSRootFS
} = (*ioFS)(nil)

type ioFS struct {
	fs              *fileSystem
	root            string
	languageVersion string
}

func (fs *ioFS) OSRoot() string {
	return fs.root
}

func (fs *ioFS) Open(name string) (iofs.File, error) {
	fpath, err := fs.absPathFromFSPath(name)
	if err != nil {
		return nil, err
	}
	r, err := fs.fs.openFile(fpath)
	if err != nil {
		return nil, err // TODO convert filepath in error to fs path
	}
	return &ioFSFile{
		fs:   fs.fs,
		path: fpath,
		rc:   r,
	}, nil
}

func (fs *ioFS) absPathFromFSPath(name string) (string, error) {
	if !iofs.ValidPath(name) {
		return "", fmt.Errorf("invalid io/fs path %q", name)
	}
	// Technically we should mimic Go's internal/safefilepath.fromFS
	// functionality here, but as we're using this in a relatively limited
	// context, we can just prohibit some characters.
	if strings.ContainsAny(name, ":\\") {
		return "", fmt.Errorf("invalid io/fs path %q", name)
	}

	if fs.fs.lfs.fs != nil {
		if isLoaderAbs(name) {
			return name, nil
		}
		return path.Join(loadFSRoot, name), nil
	}
	return fs.fs.lfs.Join(fs.root, name), nil
}

// ReadDir implements [io/fs.ReadDirFS].
func (fs *ioFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	fpath, err := fs.absPathFromFSPath(name)
	if err != nil {
		return nil, err
	}
	return fs.fs.readDir(fpath)
}

// ReadFile implements [io/fs.ReadFileFS].
func (fs *ioFS) ReadFile(name string) ([]byte, error) {
	fpath, err := fs.absPathFromFSPath(name)
	if err != nil {
		return nil, err
	}
	if fi := fs.fs.getOverlay(fpath); fi != nil {
		return bytes.Clone(fi.contents), nil
	}
	return fs.fs.lfs.ReadFile(fpath)
}

var _ module.ReadCUEFS = (*ioFS)(nil)

// IsDirWithCUEFiles implements [module.ReadCUEFS]
func (fs *ioFS) IsDirWithCUEFiles(path string) (bool, error) {
	return false, stderrs.ErrUnsupported
}

// ReadCUEFile implements [module.ReadCUEFS] by
// reading and updating the syntax file cache, which
// is shared with the cache used by the [fileSystem.getCUESyntax]
// method.
func (fs *ioFS) ReadCUEFile(path string, cfg parser.Config) (*ast.File, error) {
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
		data, err = fs.fs.lfs.ReadFile(fpath)
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

// ioFSFile implements [io/fs.File] for the overlay filesystem.
type ioFSFile struct {
	fs      *fileSystem
	path    string
	rc      io.ReadCloser
	entries []iofs.DirEntry
}

var _ interface {
	iofs.File
	iofs.ReadDirFile
} = (*ioFSFile)(nil)

func (f *ioFSFile) Stat() (iofs.FileInfo, error) {
	return f.fs.stat(f.path)
}

func (f *ioFSFile) Read(buf []byte) (int, error) {
	return f.rc.Read(buf)
}

func (f *ioFSFile) Close() error {
	return f.rc.Close()
}

func (f *ioFSFile) ReadDir(n int) ([]iofs.DirEntry, error) {
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

func (fs *fileSystem) getCUESyntax(bf *build.File, cfg parser.Config) (*ast.File, error) {
	fs.fileCache.mu.Lock()
	defer fs.fileCache.mu.Unlock()
	if bf.Encoding != build.CUE {
		panic("getCUESyntax called with non-CUE file encoding")
	}
	key := fileCacheKey{cfg, bf.Filename}
	// When it's a regular CUE file with no funny stuff going on, we
	// check and update the syntax cache.
	useCache := bf.Form == "" && bf.Interpretation == ""
	if useCache {
		if syntax, ok := fs.fileCache.entries[key]; ok {
			return syntax.file, syntax.err
		}
	}
	encodingCfg := fs.fileCache.config
	encodingCfg.ParserConfig = cfg
	openFn := fs.openFileWithStat
	d := encoding.NewDecoderWithOpenFn(fs.fileCache.ctx, bf, &encodingCfg, openFn)
	defer d.Close()
	// Note: CUE files can never have multiple file parts.
	f, err := d.File(), d.Err()
	if useCache {
		fs.fileCache.entries[key] = fileCacheEntry{f, err}
	}
	return f, err
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
