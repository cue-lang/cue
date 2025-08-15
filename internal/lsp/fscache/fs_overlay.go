package fscache

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

type dirEntry interface {
	iofs.FileInfo
	iofs.DirEntry
}

type overlayFileEntry struct {
	basename  string
	uri       protocol.DocumentURI
	content   []byte
	modtime   time.Time
	version   int32
	buildFile *build.File

	mu  sync.Mutex
	ast *ast.File
}

var _ interface {
	FileHandle
	dirEntry
} = (*overlayFileEntry)(nil)

// URI implements [FileHandle]
func (entry *overlayFileEntry) URI() protocol.DocumentURI { return entry.uri }

// ReadCUE implements [FileHandle]
func (entry *overlayFileEntry) ReadCUE(config parser.Config) (*ast.File, error) {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.ast != nil {
		return entry.ast, nil
	}

	bf := entry.buildFile
	if !(bf != nil && bf.Encoding == build.CUE && bf.Form == "" && bf.Interpretation == "") {
		return nil, nil
	}

	configCopy := config
	configCopy.Mode = parser.ParseComments
	ast, err := parser.ParseFile(bf.Filename, entry.content, configCopy)
	if err != nil && config.Mode != configCopy.Mode {
		ast, err = parser.ParseFile(bf.Filename, entry.content, config)
	}
	if err != nil {
		return nil, err
	}
	entry.ast = ast
	ast.Pos().File().SetContent(entry.content)

	return entry.ast, nil
}

// Version implements [FileHandle]
func (entry *overlayFileEntry) Version() int32 { return entry.version }

// Content implements [FileHandle]
func (entry *overlayFileEntry) Content() []byte { return slices.Clone(entry.content) }

// Name implements [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayFileEntry) Name() string { return entry.basename }

// Size implements [iofs.FileInfo]
func (entry *overlayFileEntry) Size() int64 { return int64(len(entry.content)) }

// Mode implements [iofs.FileInfo]
func (entry *overlayFileEntry) Mode() iofs.FileMode { return 0o444 }

// ModTime implements [iofs.FileInfo]
func (entry *overlayFileEntry) ModTime() time.Time { return entry.modtime }

// IsDir implements [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayFileEntry) IsDir() bool { return false }

// Sys implements [iofs.FileInfo]
func (entry *overlayFileEntry) Sys() any { return nil }

// Type implements [iofs.DirEntry]
func (entry *overlayFileEntry) Type() iofs.FileMode { return 0 }

// Info implements [iofs.DirEntry]
func (entry *overlayFileEntry) Info() (iofs.FileInfo, error) { return entry, nil }

func (entry *overlayFileEntry) open() *overlayFile {
	return &overlayFile{
		entry: entry,
		buf:   bytes.NewBuffer(entry.content),
	}
}

var _ iofs.File = (*overlayFile)(nil)

type overlayFile struct {
	entry *overlayFileEntry
	buf   *bytes.Buffer
}

// Stat implements [iofs.File]
func (file *overlayFile) Stat() (iofs.FileInfo, error) { return file.entry, nil }

// Read implements [iofs.File]
func (file *overlayFile) Read(buf []byte) (int, error) { return file.buf.Read(buf) }

// Close implements [iofs.File]
func (file *overlayFile) Close() error { return nil }

type overlayDirEntry struct {
	parent   *overlayDirEntry
	basename string
	entries  map[string]dirEntry
}

var _ dirEntry = (*overlayDirEntry)(nil)

func (entry *overlayDirEntry) ensureEntries() map[string]dirEntry {
	if entry.entries == nil {
		entry.entries = make(map[string]dirEntry)
	}
	return entry.entries
}

// Name implements [iofs.FileInfo] and [iofs.DirEntry]
func (entry *overlayDirEntry) Name() string { return entry.basename }

// Size implements [iofs.FileInfo]
func (entry *overlayDirEntry) Size() int64 { return 0 }

// Mode implements [iofs.FileInfo]
func (entry *overlayDirEntry) Mode() iofs.FileMode { return iofs.ModeDir | 0o555 }

// ModTime implements [iofs.FileInfo]
func (entry *overlayDirEntry) ModTime() time.Time { return time.Time{} }

// IsDir implements [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayDirEntry) IsDir() bool { return true }

// Sys implements [iofs.FileInfo]
func (entry *overlayDirEntry) Sys() any { return nil }

// Type implements [iofs.DirEntry]
func (entry *overlayDirEntry) Type() iofs.FileMode { return iofs.ModeDir }

// Info implements [iofs.DirEntry]
func (entry *overlayDirEntry) Info() (iofs.FileInfo, error) { return entry, nil }

func (entry *overlayDirEntry) open() *overlayDir {
	return &overlayDir{entry: entry}
}

var _ iofs.ReadDirFile = (*overlayDir)(nil)

type overlayDir struct {
	entry   *overlayDirEntry
	entries []iofs.DirEntry
}

// Stat implements [iofs.File]
func (dir *overlayDir) Stat() (iofs.FileInfo, error) { return dir.entry, nil }

// Read implements [iofs.File]
func (dir *overlayDir) Read(buf []byte) (int, error) { return 0, errors.ErrUnsupported }

// Close implements [iofs.File]
func (dir *overlayDir) Close() error { return nil }

// ReadDir implements [iofs.ReadDirFile]
func (dir *overlayDir) ReadDir(n int) ([]iofs.DirEntry, error) {
	// NB [iofs.ReadDirFile] does not require any sorting of entries.
	if dir.entries == nil {
		dirEntries := dir.entry.entries
		entries := make([]iofs.DirEntry, 0, len(dirEntries))
		// loop is necessary because we're changing type
		for _, entry := range dirEntries {
			entries = append(entries, entry)
		}
		dir.entries = entries
	}

	entries := dir.entries
	entriesLen := len(entries)
	switch {
	case n <= 0: // read everything, even if it's nothing
		dir.entries = entries[entriesLen:]
		return entries, nil

	case entriesLen == 0: // nothing to read
		return nil, io.EOF

	case n >= entriesLen: // read everything left over
		dir.entries = entries[entriesLen:]
		return entries, nil

	default: // read only n items
		dir.entries = entries[n:]
		return entries[:n], nil
	}
}

// OverlayFS extends [CUECacheFS] with an overlay facility. As with
// CUECacheFS, it provides both a URI-based API for use with LSP, and
// [iofs.FS] APIs for use with our module code.
type OverlayFS struct {
	mu          sync.RWMutex
	overlayRoot *overlayDirEntry
	delegatefs  *CUECacheFS
}

var _ RootableFS = (*OverlayFS)(nil)

func NewOverlayFS(fs *CUECacheFS) *OverlayFS {
	return &OverlayFS{
		overlayRoot: &overlayDirEntry{},
		delegatefs:  fs,
	}
}

// pathComponents splits uri into a slice of directory names (which
// may be empty), and the final basename.
func (fs *OverlayFS) pathComponents(uri protocol.DocumentURI) ([]string, string) {
	const fileSchemePrefix = "file:///"
	str := string(uri)
	if !strings.HasPrefix(str, fileSchemePrefix) {
		panic(fmt.Sprintf("%q is not a valid DocumentURI", str))
	}
	components := strings.Split(str[len(fileSchemePrefix):], "/")
	// strings.Split always returns a slice of at least 1 element
	idx := len(components) - 1
	return components[:idx], components[idx]
}

// getDirLocked searches the fs for a directory by following the path
// components.
//
//   - fs.mu must already be held. It is not modified by this method.
//   - If creating, then fs.mu must be held in write-mode. Otherwise read-mode is fine.
//   - If creating, then error will not be [iofs.ErrNotExist].
//   - If not creating, the error can be [iofs.ErrNotExist] if any
//     path component doesn't exist.
//   - A conflict between a component being a directory vs a file will
//     result in [iofs.ErrInvalid].
func (fs *OverlayFS) getDirLocked(components []string, create bool) (*overlayDirEntry, error) {
	dir := fs.overlayRoot

	for _, component := range components {
		entry, found := dir.entries[component]

		if found {
			childDir, isDir := entry.(*overlayDirEntry)
			if !isDir {
				// Within the overlay, we need to be consistent about
				// what's a directory and what's a file. We need a
				// directory here, but that's not what we've found.
				return nil, iofs.ErrInvalid
			}
			dir = childDir

		} else if create {
			childDir := &overlayDirEntry{
				parent:   dir,
				basename: component,
			}
			dirEntries := dir.ensureEntries()
			dirEntries[component] = childDir
			dir = childDir

		} else {
			return nil, iofs.ErrNotExist
		}
	}

	return dir, nil
}

// getEntry searches the fs for a directory entry by following the
// path components.
//
// Requirements:
//   - fs.mu will be taken (and released) in read-mode.
//   - A conflict between an entry being a directory vs a file
//     (i.e. the fs has a file for a component which is a directory in
//     components) will result in [iofs.ErrInvalid].
func (fs *OverlayFS) getEntry(components []string, entryName string) (dirEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.getEntryLocked(components, entryName)
}

// getEntryLocked searches the fs for a directory entry by following
// the path components.
//
//   - fs.mu must already be held (in read or write-mode). It is not modified by this method.
//   - A conflict between an entry being a directory vs a file
//     (i.e. the fs has a file for a component which is a directory in
//     components) will result in [iofs.ErrInvalid].
func (fs *OverlayFS) getEntryLocked(components []string, entryName string) (dirEntry, error) {
	dir, err := fs.getDirLocked(components, false)
	if err != nil {
		return nil, err
	}

	entry, found := dir.entries[entryName]
	if !found {
		return nil, iofs.ErrNotExist
	}
	return entry, nil
}

// ReadFile searches the overlays for the uri. If found, and is a
// file, the overlay is returned. If the uri is found in the overlays
// and is a directory, the error is [iofs.PathError]. If the uri is
// not found in the overlays, the underlying [CUECacheFS.ReadFile]
// method is called.
func (fs *OverlayFS) ReadFile(uri protocol.DocumentURI) (FileHandle, error) {
	entry, err := fs.getEntry(fs.pathComponents(uri))

	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.ReadFile(uri)
	} else if err != nil {
		return nil, err
	}

	file, isFile := entry.(*overlayFileEntry)
	if !isFile {
		return nil, &iofs.PathError{Op: "open", Path: uri.Path(), Err: iofs.ErrInvalid}
	}

	return file, nil
}

// IoFS implements [RootableFS]
func (fs *OverlayFS) IoFS(root string) CUEDirFS {
	return &rootedOverlayFS{
		overlayfs:  fs,
		delegatefs: fs.delegatefs.IoFS(root).(*rootedCUECacheFS),
	}
}

// View calls the given function once with a txn argument that can be
// used to atomically access read-only data from fs, and must not be
// used after the function returns.
//
// Multiple read-only transactions may run in parallel, but read-only
// and read-write transactions are mutually exclusive. Note that nested
// transactions of any sort are not supported, and will probably
// deadlock.
func (fs *OverlayFS) View(fun func(txn *ViewTxn) error) error {
	fs.mu.RLock()
	txn := &ViewTxn{OverlayFS: fs}
	defer func() {
		fs.mu.RUnlock()
		// prevent subsequent use of the txn value
		txn.OverlayFS = nil
	}()
	return fun(txn)
}

// Update calls the given function once with a txn argument that can be
// used to atomically access and mutate data from fs, and must not be
// used after the function returns.
//
// Multiple read-only transactions may run in parallel, but read-only
// and read-write transactions are mutually exclusive. Note that nested
// transactions of any sort are not supported, and will probably
// deadlock.
func (fs *OverlayFS) Update(fun func(txn *UpdateTxn) error) error {
	fs.mu.Lock()
	txn := &UpdateTxn{&ViewTxn{OverlayFS: fs}}
	defer func() {
		fs.mu.Unlock()
		// prevent subsequent use of the txn value
		txn.ViewTxn.OverlayFS = nil
		txn.ViewTxn = nil
	}()
	return fun(txn)
}

// ViewTxn provides methods to access the OverlayFS during a read-only
// transaction (via [OverlayFS.View]). A ViewTxn must not be used
// outside of a transaction.
type ViewTxn struct {
	*OverlayFS
}

// Get is like [OverlayFS.ReadFile] but it *only* returns a file if
// it's present in the overlay itself. It does not, under any
// circumstances, access the underlying [CUECacheFS].
func (txn *ViewTxn) Get(uri protocol.DocumentURI) (FileHandle, error) {
	entry, err := txn.getEntryLocked(txn.pathComponents(uri))
	if err != nil {
		return nil, err
	}

	file, isFile := entry.(*overlayFileEntry)
	if !isFile {
		return nil, iofs.ErrInvalid
	}

	return file, nil
}

// WalkFiles invokes fun on all the files present in the overlay
// only. It stops walking if fun returns an error.
//
// It does not, under any circumstances, access the underlying
// CUECacheFS. The function only gets passed files, never directories.
// All files in a directory will be passed to fun before any
// subdirectories; no other ordering guarantees are made.
func (txn *ViewTxn) WalkFiles(fun func(FileHandle) error, uri protocol.DocumentURI) error {
	entry, err := txn.getEntryLocked(txn.pathComponents(uri))
	if err != nil {
		return err
	}
	return txn.walkFiles(fun, entry)
}

func (txn *ViewTxn) walkFiles(fun func(FileHandle) error, entry dirEntry) error {
	switch entry := entry.(type) {
	case *overlayFileEntry:
		if err := fun(entry); err != nil {
			return err
		}
	case *overlayDirEntry:
		for _, child := range entry.entries {
			if child.IsDir() {
				continue
			}
			if err := txn.walkFiles(fun, child); err != nil {
				return err
			}
		}
		for _, child := range entry.entries {
			if !child.IsDir() {
				continue
			}
			if err := txn.walkFiles(fun, child); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateTxn provides methods to access the OverlayFS during a
// read-write transaction (via [OverlayFS.Update]). An UpdateTxn must
// not be used outside of a transaction.
type UpdateTxn struct {
	*ViewTxn
}

// Set updates the overlay, updating or creating a file with the given
// parameters. Any required parent directories will be silently
// created. Any existing file will be silently updated. However, if
// this action would require converting a file to a directory
// (i.e. the uri has a directory component that already exists in the
// overlay as a file), then an error will be returned.
//
// The version parameter is the file version, as defined by the LSP
// client, corresponding to the Version method of [FileHandle].
//
// This modifies the overlay *only*. It does not, under any
// circumstances, access the underlying CUECacheFS.
func (txn *UpdateTxn) Set(uri protocol.DocumentURI, content []byte, mtime time.Time, version int32) (FileHandle, error) {
	filePath := uri.Path()
	components, entryName := txn.pathComponents(uri)
	dir, err := txn.getDirLocked(components, true)
	if err != nil {
		return nil, err
	}

	bf, err := filetypes.ParseFileAndType(filePath, "", filetypes.Input)
	if err != nil {
		return nil, err
	}
	bf.Source = content

	file := &overlayFileEntry{
		basename:  entryName,
		uri:       uri,
		content:   content,
		modtime:   mtime,
		version:   version,
		buildFile: bf,
	}

	dirEntries := dir.ensureEntries()
	// silently overwrite any existing entry
	dirEntries[file.basename] = file

	return file, nil
}

// Delete updates the overlay, removing the entry specified by
// uri. This entry could be a file or a directory. If, after the
// removal, a parent directory is left empty, the parent directory is
// also removed (and this continues transitively). I.e. no empty
// directories will exist within the OverlayFS. Delete is idempotent:
// deleting a uri that does not exist, returns nil.
//
// This modifies the overlay *only*. It does not, under any
// circumstances, access the underlying CUECacheFS.
func (txn *UpdateTxn) Delete(uri protocol.DocumentURI) error {
	components, entryName := txn.pathComponents(uri)
	dir, err := txn.getDirLocked(components, false)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	for ; dir != nil; dir = dir.parent {
		_, found := dir.entries[entryName]
		if !found {
			return nil
		}

		delete(dir.entries, entryName)
		if len(dir.entries) > 0 {
			return nil
		}
		entryName = dir.basename
	}

	return nil
}

// rootedOverlayFS is a wrapper over [OverlayFS] that implements
// [iofs.FS], [iofs.ReadDirFS], [iofs.ReadFileFS], [iofs.StatFS],
// [module.OSRootFS], and [module.ReadCUEFS].
type rootedOverlayFS struct {
	// The overlayfs always wins over the delegatefs: the delegatefs is
	// only used when overlayfs says nothing at all about the path in
	// question. The overlayfs can't currently be used to model file
	// deletions (it would need to implement some sort of
	// tombstone/blacklist) but that's ok - we don't need such
	// functionality for now.
	overlayfs  *OverlayFS
	delegatefs *rootedCUECacheFS
}

var _ CUEDirFS = (*rootedOverlayFS)(nil)

// OSRoot implements [module.OSRootFS]
func (fs *rootedOverlayFS) OSRoot() string {
	return fs.delegatefs.OSRoot()
}

// pathComponents splits the name path into a slice of directory names
// (which may be empty), and the final basename. The name must be
// valid according to [iofs.ValidPath]
func (fs *rootedOverlayFS) pathComponents(name string) ([]string, string) {
	if name == "." {
		name = ""
	}
	components := strings.Split(fs.delegatefs.root, string(os.PathSeparator))
	components = append(components, strings.Split(name, "/")...)
	components = slices.DeleteFunc(components, func(component string) bool { return component == "" })

	idx := len(components) - 1
	return components[:idx], components[idx]
}

// Open implements [iofs.FS]
func (fs *rootedOverlayFS) Open(name string) (iofs.File, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "open", Path: name, Err: iofs.ErrInvalid}
	}

	entry, err := fs.overlayfs.getEntry(fs.pathComponents(name))
	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.Open(name)
	} else if err != nil {
		return nil, &iofs.PathError{Op: "open", Path: name, Err: err}
	}

	switch entry := entry.(type) {
	case *overlayFileEntry:
		// The overlay is a file. We don't look at the underlying delegatefs
		// to test whether or not it's a dir or file down there - the
		// overlay always wins.
		return entry.open(), nil

	case *overlayDirEntry:
		// The overlay is a dir. If the underlying delegatefs exists and is
		// also a dir, then we combine the two. Otherwise, the overlay
		// wins.
		delegateHandle, err := fs.delegatefs.Open(name)
		if err == nil {
			if st, err := delegateHandle.Stat(); err == nil && st.IsDir() {
				return &combinedDir{
					overlayHandle:  entry.open(),
					delegateHandle: delegateHandle.(iofs.ReadDirFile),
				}, nil
			}
			defer delegateHandle.Close()
		} else if errors.Is(err, iofs.ErrNotExist) {
			// something's really bad with the delegate.
			return nil, err
		}
		// either the delegate doesn't exist, or it does, but it's a
		// file. In both cases, the overlay wins.
		return entry.open(), nil

	default:
		panic("Unexpected entry type")
	}
}

// ReadCUEFile implements [module.ReadCUEFS]
func (fs *rootedOverlayFS) ReadCUEFile(name string, config parser.Config) (*ast.File, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "readcuefile", Path: name, Err: iofs.ErrInvalid}
	}

	entry, err := fs.overlayfs.getEntry(fs.pathComponents(name))

	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.ReadCUEFile(name, config)
	} else if err != nil {
		return nil, err
	}

	if file, isFile := entry.(*overlayFileEntry); isFile {
		return file.ReadCUE(config)
	}

	return nil, iofs.ErrInvalid
}

// ReadDir implements [iofs.ReadDirFS]
func (fs *rootedOverlayFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "readdir", Path: name, Err: iofs.ErrInvalid}
	}

	entry, err := fs.overlayfs.getEntry(fs.pathComponents(name))

	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.ReadDir(name)
	} else if err != nil {
		return nil, err
	}

	dir, isDir := entry.(*overlayDirEntry)
	if !isDir {
		return nil, iofs.ErrInvalid
	}

	// [iofs.ReadDirFS] requires that we sort the directory entries.
	overlayEntries, err := dir.open().ReadDir(0)
	if err != nil {
		return nil, err
	}

	delegateEntries, err := fs.delegatefs.ReadDir(name)
	if err != nil {
		// If there's any error at all, we ignore it: we know that there
		// is an overlay here, and overlays always win. Empty out
		// delegateEntries just to be sure.
		delegateEntries = nil
	}

	// delegateEntries will already be sorted. We need to sort the
	// overlayEntries, and then we can merge them both together.
	slices.SortFunc(
		overlayEntries,
		func(a, b iofs.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })

	// overlayEntries is the 1st arg because if names match, we want
	// the overlay entry to win.
	return mergeSort(overlayEntries, delegateEntries), nil
}

// mergeSort merges as and bs into a single slice sorted by name,
// discarding elements of bs when they're duplicated in as.
//
// Invariant: as and bs must both be sorted by Name,
// ascending. Individually, as and bs must not contain duplicates.
func mergeSort(as, bs []iofs.DirEntry) []iofs.DirEntry {
	if len(as) == 0 {
		return bs
	}
	if len(bs) == 0 {
		return as
	}

	out := make([]iofs.DirEntry, 0, len(as)+len(bs))
	for {
		aElem, bElem := as[0], bs[0]
		switch cmp.Compare(aElem.Name(), bElem.Name()) {
		case 0:
			bs = bs[1:]
			if len(bs) == 0 {
				return append(out, as...)
			}
			fallthrough

		case -1:
			out = append(out, aElem)
			as = as[1:]

			if len(as) == 0 {
				return append(out, bs...)
			}

		case 1:
			out = append(out, bElem)
			bs = bs[1:]
			if len(bs) == 0 {
				return append(out, as...)
			}
		}
	}
}

// ReadFile implements [iofs.ReadFileFS]
func (fs *rootedOverlayFS) ReadFile(name string) ([]byte, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "readfile", Path: name, Err: iofs.ErrInvalid}
	}

	entry, err := fs.overlayfs.getEntry(fs.pathComponents(name))

	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.ReadFile(name)
	} else if err != nil {
		return nil, err
	}

	if file, isFile := entry.(*overlayFileEntry); isFile {
		return file.Content(), nil
	}

	return nil, iofs.ErrInvalid
}

// Stat implements [iofs.StatFS]
func (fs *rootedOverlayFS) Stat(name string) (iofs.FileInfo, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "stat", Path: name, Err: iofs.ErrInvalid}
	}

	entry, err := fs.overlayfs.getEntry(fs.pathComponents(name))

	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.Stat(name)
	} else if err != nil {
		return nil, err
	}

	return entry.Info()
}

type combinedDir struct {
	overlayHandle  *overlayDir
	delegateHandle iofs.ReadDirFile

	overlayEntryNames map[string]struct{}
}

var _ iofs.ReadDirFile = (*combinedDir)(nil)

// Stat implements [iofs.File]
func (dir *combinedDir) Stat() (iofs.FileInfo, error) {
	return dir.overlayHandle.entry, nil
}

// Read implements [iofs.File]
func (dir *combinedDir) Read(buf []byte) (int, error) { return 0, errors.ErrUnsupported }

// Close implements [iofs.File]
func (dir *combinedDir) Close() error {
	var errDelegate, errOverlay error
	if delegateHandle := dir.delegateHandle; delegateHandle != nil {
		dir.delegateHandle = nil
		errDelegate = delegateHandle.Close()
	}
	if overlayHandle := dir.overlayHandle; overlayHandle != nil {
		dir.overlayHandle = nil
		errOverlay = overlayHandle.Close()
	}
	if errDelegate != nil {
		return errDelegate
	}
	return errOverlay
}

// ReadDir implements [iofs.ReadDirFile]
func (dir *combinedDir) ReadDir(n int) ([]iofs.DirEntry, error) {
	overlayHandle := dir.overlayHandle
	delegateHandle := dir.delegateHandle

	if overlayHandle == nil || delegateHandle == nil {
		return nil, iofs.ErrClosed
	}
	// NB [iofs.ReadDirFile] does not require the results to be sorted in any way.
	overlayEntryNames := dir.overlayEntryNames
	if overlayEntryNames == nil {
		overlayEntryNames = make(map[string]struct{})
		dir.overlayEntryNames = overlayEntryNames
	}

	if n <= 0 { // read everything
		overlayEntries, err := overlayHandle.ReadDir(0)
		if err != nil {
			return nil, err
		}

		delegateEntries, err := delegateHandle.ReadDir(0)
		if err != nil {
			return nil, err
		}

		// If we have a collision we want the overlay to win.
		for _, entry := range overlayEntries {
			overlayEntryNames[entry.Name()] = struct{}{}
		}

		entries := overlayEntries
		for _, entry := range delegateEntries {
			if _, overlayExists := overlayEntryNames[entry.Name()]; !overlayExists {
				entries = append(entries, entry)
			}
		}

		return entries, nil
	}

	// No need to do sorting here, so just use up the overlay first,
	// and then switch to the delegate. This way, we can be sure to never
	// emit a delegate entry with a name that matches an overlay entry.
	overlayEntries, err := overlayHandle.ReadDir(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	for _, entry := range overlayEntries {
		overlayEntryNames[entry.Name()] = struct{}{}
	}

	entries := overlayEntries

	for remaining := n - len(entries); remaining > 0; remaining = n - len(entries) {
		// we ran out of overlay entries so we now use the underlying
		// delegate, but we need to be careful to filter entries, hence the
		// loop.

		delegateEntries, err := delegateHandle.ReadDir(remaining)
		isEOF := errors.Is(err, io.EOF)
		if err != nil && !isEOF {
			return nil, err
		}

		for _, entry := range delegateEntries {
			if _, overlayExists := overlayEntryNames[entry.Name()]; !overlayExists {
				entries = append(entries, entry)
			}
		}

		if isEOF {
			break
		}
	}

	if len(entries) == 0 {
		return nil, io.EOF
	}

	return entries, nil
}
