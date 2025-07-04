package fscache

import (
	"errors"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/robustio"
	"cuelang.org/go/mod/module"
)

type File interface {
	URI() protocol.DocumentURI
	// Note that config is only used if there is no existing cached
	// [ast.File] value within the File. Therefore, it is the user's
	// responsibility to ensure that only one config value is used for
	// each file: if you change the config value and re-read the file,
	// you will not receive back an updated [ast.File].
	ReadCUEFile(config parser.Config) (*ast.File, error)
	Version() int32
	// The byte slice returned is a copy of the underlying file
	// content, and thus safe to be mutated. This matches the behaviour
	// of [iofs.ReadFileFS].
	Content() []byte
}

type diskFileEntry struct {
	uri     protocol.DocumentURI
	modTime time.Time

	// TODO: will need to add the means to get the buildFile out. And
	// probably refine the behavioul of err too.
	content   []byte
	buildFile *build.File

	mu  sync.Mutex
	ast *ast.File
}

var _ File = (*diskFileEntry)(nil)

// Implementing [File]
func (entry *diskFileEntry) URI() protocol.DocumentURI { return entry.uri }

// Implementing [File]
func (entry *diskFileEntry) ReadCUEFile(config parser.Config) (*ast.File, error) {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.ast != nil {
		return entry.ast, nil
	}

	bf := entry.buildFile
	if !(bf != nil && bf.Encoding == build.CUE && bf.Form == "" && bf.Interpretation == "") {
		return nil, nil
	}

	ast, err := parser.ParseFile(bf.Filename, bf.Source, config, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	entry.ast = ast

	return entry.ast, nil
}

// Implementing [File]
func (entry *diskFileEntry) Version() int32 { return -1 }

// Implementing [File]
func (entry *diskFileEntry) Content() []byte { return slices.Clone(entry.content) }

func (entry *diskFileEntry) clone() *diskFileEntry {
	// copy everything apart from the mutex
	return &diskFileEntry{
		uri:       entry.uri,
		modTime:   entry.modTime,
		content:   entry.content,
		buildFile: entry.buildFile,
		ast:       entry.ast,
	}
}

// CueCacheFS exists to cache [ast.File] values and thus amortize the
// cost of parsing cue files. It is not an overlay in any way. Its
// design is influenced by gopls's similar fs caching layer
// (cache/fs_memoized.go). CueCacheFS is also designed to bridge the
// API gap between LSP, in which everything is a URI, and our own
// module code (e.g. modpkgload) which is built around [iofs.FS] and
// related interfaces.
//
// Note that CueCacheFS will return errors for reading files which are
// not understood by [filetypes.ParseFileAndType].
type CueCacheFS struct {
	mu           sync.Mutex
	cueFilesByID map[robustio.FileID][]*diskFileEntry
}

var _ RootableFS = (*CueCacheFS)(nil)

func NewCueCachedFS() *CueCacheFS {
	return &CueCacheFS{
		cueFilesByID: make(map[robustio.FileID][]*diskFileEntry),
	}
}

func (fs *CueCacheFS) PurgeCacheUnder(uri protocol.DocumentURI) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for id, files := range fs.cueFilesByID {
		var kept []*diskFileEntry
		for i, file := range files {
			if file.uri == uri || uri.Encloses(file.uri) {
				if kept == nil {
					kept = files[:i]
				}
				continue

			} else {
				if kept != nil {
					kept = append(kept, file)
				}
			}
		}

		if kept == nil { // no files were dropped
			continue
		} else if len(kept) == 0 { // all files were dropped
			delete(fs.cueFilesByID, id)
		} else {
			fs.cueFilesByID[id] = kept
		}
	}
}

func (fs *CueCacheFS) ReadFile(uri protocol.DocumentURI) (File, error) {
	id, mtime, err := robustio.GetFileID(uri.Path())
	if err != nil {
		if errors.Is(iofs.ErrNotExist, err) {
			fs.PurgeCacheUnder(uri)
		}
		return nil, err
	}

	// The following comment taken from gopls's cache/fs_memoized.go file:
	//
	// We check if the file has changed by comparing modification times. Notably,
	// this is an imperfect heuristic as various systems have low resolution
	// mtimes (as much as 1s on WSL or s390x builders), so we only cache
	// filehandles if mtime is old enough to be reliable, meaning that we don't
	// expect a subsequent write to have the same mtime.
	//
	// The coarsest mtime precision we've seen in practice is 1s, so consider
	// mtime to be unreliable if it is less than 2s old. Capture this before
	// doing anything else.
	recentlyModified := time.Since(mtime) < 2*time.Second

	fs.mu.Lock()
	files, ok := fs.cueFilesByID[id]
	if ok && files[0].modTime.Equal(mtime) {
		var entry *diskFileEntry
		// We have already seen this file and it has not changed.
		for _, fh := range files {
			if fh.uri == uri {
				entry = fh
				break
			}
		}
		// No file handle for this exact URI. Create an alias, but share content.
		if entry == nil {
			entry := files[0].clone()
			entry.uri = uri
			files = append(files, entry)
			fs.cueFilesByID[id] = files
		}
		fs.mu.Unlock()
		return entry, nil
	}
	fs.mu.Unlock()

	// Unknown file, or file has changed. Read (or re-read) it.
	df, err := readFile(uri, mtime)

	fs.mu.Lock()
	// Only cache it if it's not been recentlyModified and it has no errors.
	if !recentlyModified && err == nil {
		fs.cueFilesByID[id] = []*diskFileEntry{df}
	} else {
		delete(fs.cueFilesByID, id)
	}
	fs.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return df, nil
}

func readFile(uri protocol.DocumentURI, mtime time.Time) (*diskFileEntry, error) {
	// The following comment taken from gopls's cache/fs_memoized.go file:
	//
	// It is possible that a race causes us to read a file with different file
	// ID, or whose mtime differs from the given mtime. However, in these cases
	// we expect the client to notify of a subsequent file change, and the file
	// content should be eventually consistent.

	// NB filePath is GOOS-appropriate (uri.Path() calls [filepath.FromSlash])
	filePath := uri.Path()
	content, err := os.ReadFile(filePath)
	if err != nil {
		content = nil // just in case
	}
	entry := &diskFileEntry{
		modTime: mtime,
		uri:     uri,
		content: content,
	}

	if err != nil {
		return nil, err
	}

	bf, err := filetypes.ParseFileAndType(filePath, "", filetypes.Input)
	if err != nil {
		return nil, err
	}
	bf.Source = content
	entry.buildFile = bf

	return entry, nil
}

// Implementing [RootableFS]
//
// Note the root is GOOS-appropriate.
func (fs *CueCacheFS) IoFS(root string) CUEDirFS {
	root = strings.TrimRight(root, string(os.PathSeparator))
	return &RootedCueCacheFS{
		cuecachefs: fs,
		delegatefs: os.DirFS(root).(DirFS),
		root:       filepath.ToSlash(root),
	}
}

type RootableFS interface {
	IoFS(root string) CUEDirFS
}

type DirFS interface {
	iofs.FS
	iofs.ReadDirFS
	iofs.ReadFileFS
	iofs.StatFS
}

type CUEDirFS interface {
	DirFS
	module.OSRootFS
	module.ReadCUEFS
}

// RootedCueCacheFS is a wrapper over [CueCacheFS] that provides
// implementations of [iofs.FS], [iofs.ReadDirFS], [iofs.ReadFileFS],
// [iofs.StatFS], [module.OSRootFS], and [module.ReadCUEFS]
type RootedCueCacheFS struct {
	cuecachefs *CueCacheFS
	delegatefs DirFS
	// NB root is in slash form, not GOOS
	root string
}

var _ CUEDirFS = (*RootedCueCacheFS)(nil)

// Implementing [module.OSRootFS]
func (fs *RootedCueCacheFS) OSRoot() string {
	return filepath.FromSlash(fs.root)
}

// Implementing [iofs.FS]
func (fs *RootedCueCacheFS) Open(name string) (iofs.File, error) { return fs.delegatefs.Open(name) }

// Implementing [module.ReadCUEFS]
func (fs *RootedCueCacheFS) ReadCUEFile(name string, config parser.Config) (*ast.File, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "ReadCUEFile", Path: name, Err: iofs.ErrInvalid}
	}

	uri := protocol.URIFromPath(path.Join(fs.root, name))
	fh, err := fs.cuecachefs.ReadFile(uri)
	if err != nil {
		return nil, err
	}
	return fh.ReadCUEFile(config)
}

// Implementing [iofs.ReadDirFS]
func (fs *RootedCueCacheFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	return fs.delegatefs.ReadDir(name)
}

// Implementing [iofs.ReadFileFS]
func (fs *RootedCueCacheFS) ReadFile(name string) ([]byte, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "ReadFile", Path: name, Err: iofs.ErrInvalid}
	}

	uri := protocol.URIFromPath(path.Join(fs.root, name))
	fh, err := fs.cuecachefs.ReadFile(uri)
	if err != nil {
		return nil, err
	}

	return fh.Content(), nil
}

// Implementing [iofs.StatFS]
func (fs *RootedCueCacheFS) Stat(name string) (iofs.FileInfo, error) {
	return fs.delegatefs.Stat(name)
}
