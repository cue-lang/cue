package fscache

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"path"
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
	File
	dirEntry
} = (*overlayFileEntry)(nil)

// Implementing [File]
func (entry *overlayFileEntry) URI() protocol.DocumentURI { return entry.uri }

// Implementing [File]
func (entry *overlayFileEntry) ReadCUEFile(config parser.Config) (*ast.File, error) {
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
func (entry *overlayFileEntry) Version() int32 { return entry.version }

// Implementing [File]
func (entry *overlayFileEntry) Content() []byte { return slices.Clone(entry.content) }

// Implementing [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayFileEntry) Name() string { return entry.basename }

// Implementing [iofs.FileInfo]
func (entry *overlayFileEntry) Size() int64 { return int64(len(entry.content)) }

// Implementing [iofs.FileInfo]
func (entry *overlayFileEntry) Mode() iofs.FileMode { return 0o444 }

// Implementing [iofs.FileInfo]
func (entry *overlayFileEntry) ModTime() time.Time { return entry.modtime }

// Implementing [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayFileEntry) IsDir() bool { return false }

// Implementing [iofs.FileInfo]
func (entry *overlayFileEntry) Sys() any { return nil }

// Implementing [iofs.DirEntry]
func (entry *overlayFileEntry) Type() iofs.FileMode { return 0 }

// Implementing [iofs.DirEntry]
func (entry *overlayFileEntry) Info() (iofs.FileInfo, error) { return entry, nil }

func (entry *overlayFileEntry) open() *overlayFileHandle {
	return &overlayFileHandle{
		entry: entry,
		buf:   bytes.NewBuffer(entry.content),
	}
}

var _ iofs.File = (*overlayFileHandle)(nil)

type overlayFileHandle struct {
	entry *overlayFileEntry
	buf   *bytes.Buffer
}

// Implementing [iofs.File]
func (handle *overlayFileHandle) Stat() (iofs.FileInfo, error) { return handle.entry, nil }

// Implementing [iofs.File]
func (handle *overlayFileHandle) Read(buf []byte) (int, error) { return handle.buf.Read(buf) }

// Implementing [iofs.File]
func (handle *overlayFileHandle) Close() error { return nil }

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

// Implementing [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayDirEntry) Name() string { return entry.basename }

// Implementing [iofs.FileInfo]
func (entry *overlayDirEntry) Size() int64 { return 0 }

// Implementing [iofs.FileInfo]
func (entry *overlayDirEntry) Mode() iofs.FileMode { return iofs.ModeDir | 0o555 }

// Implementing [iofs.FileInfo]
func (entry *overlayDirEntry) ModTime() time.Time { return time.Time{} }

// Implementing [iofs.FileInfo] and  [iofs.DirEntry]
func (entry *overlayDirEntry) IsDir() bool { return true }

// Implementing [iofs.FileInfo]
func (entry *overlayDirEntry) Sys() any { return nil }

// Implementing [iofs.DirEntry]
func (entry *overlayDirEntry) Type() iofs.FileMode { return iofs.ModeDir }

// Implementing [iofs.DirEntry]
func (entry *overlayDirEntry) Info() (iofs.FileInfo, error) { return entry, nil }

func (entry *overlayDirEntry) open() *overlayDirHandle {
	return &overlayDirHandle{entry: entry}
}

var _ iofs.ReadDirFile = (*overlayDirHandle)(nil)

type overlayDirHandle struct {
	entry   *overlayDirEntry
	entries []iofs.DirEntry
}

// Implementing [iofs.File]
func (handle *overlayDirHandle) Stat() (iofs.FileInfo, error) { return handle.entry, nil }

// Implementing [iofs.File]
func (handle *overlayDirHandle) Read(buf []byte) (int, error) { return 0, errors.ErrUnsupported }

// Implementing [iofs.File]
func (handle *overlayDirHandle) Close() error { return nil }

// Implementing [iofs.ReadDirFile]
func (handle *overlayDirHandle) ReadDir(n int) ([]iofs.DirEntry, error) {
	// NB [iofs.ReadDirFile] does not require any sorting of entries.
	if handle.entries == nil {
		dirEntries := handle.entry.entries
		entries := make([]iofs.DirEntry, 0, len(dirEntries))
		// loop is necessary because we're changing type
		for _, entry := range dirEntries {
			entries = append(entries, entry)
		}
		handle.entries = entries
	}

	entries := handle.entries
	entriesLen := len(entries)
	switch {
	case n <= 0: // read everything, even if it's nothing
		handle.entries = entries[entriesLen:]
		return entries, nil

	case entriesLen == 0: // nothing to read
		return nil, io.EOF

	case n >= entriesLen: // read everything left over
		handle.entries = entries[entriesLen:]
		return entries, nil

	default: // read only n items
		handle.entries = entries[n:]
		return entries[:n], nil
	}
}

// OverlayFS extends [CueCacheFS] with an overlay facility. As with
// CueCacheFS, it provides both a URI-based API for use with LSP, and
// [iofs.FS] APIs for use with our module code.
type OverlayFS struct {
	mu          sync.RWMutex
	overlayRoot *overlayDirEntry
	delegatefs  *CueCacheFS
}

var _ RootableFS = (*OverlayFS)(nil)

func NewOverlayFS(fs *CueCacheFS) *OverlayFS {
	return &OverlayFS{
		overlayRoot: &overlayDirEntry{},
		delegatefs:  fs,
	}
}

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

// Requirements:
//   - fs.mu must already be held. It is not modified by this method.
//   - If create, then fs.mu must be held in write-mode. Otherwise read-mode is fine.
//   - If err == nil then the overlayDirEntry will be non-nil.
//   - If create, then error will not be [iofs.ErrNotExist].
//   - A conflict between an entry being a directory vs a file will
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

// Requirements:
//   - fs.mu will be taken (and released) in read-mode.
//   - if err == nil then dirEntry will be non-nil.
//   - A conflict between an entry being a directory vs a file
//     (i.e. the fs has a file for a component which is a directory in
//     components) will result in [iofs.ErrInvalid].
func (fs *OverlayFS) getEntry(components []string, entryName string) (dirEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.getEntryLocked(components, entryName)
}

// Requirements:
//   - fs.mu must already be held (in read or write-mode). It is not modified by this method.
//   - if err == nil then dirEntry will be non-nil.
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

func (fs *OverlayFS) ReadFile(uri protocol.DocumentURI) (File, error) {
	entry, err := fs.getEntry(fs.pathComponents(uri))

	if errors.Is(err, iofs.ErrNotExist) {
		return fs.delegatefs.ReadFile(uri)
	} else if err != nil {
		return nil, err
	}

	file, isFile := entry.(*overlayFileEntry)
	if !isFile {
		return nil, iofs.ErrInvalid
	}

	return file, nil
}

// Implementing [RootableFS]
//
// Note the root is GOOS-appropriate
func (fs *OverlayFS) IoFS(root string) CUEDirFS {
	return &RootedOverlayFS{
		overlayfs:  fs,
		delegatefs: fs.delegatefs.IoFS(root).(*RootedCueCacheFS),
	}
}

// View provides support for atomic read-only transactions on an
// OverlayFS. Multiple read-only transactions may occur in parallel,
// but read-only and read-write transactions are mutually
// exclusive. Note that nested transactions of any sort are not
// supported, and will probably deadlock.
func (fs *OverlayFS) View(fun func(txn *OverlayFSROTxn) error) error {
	fs.mu.RLock()
	txn := &OverlayFSROTxn{OverlayFS: fs}
	defer func() {
		fs.mu.RUnlock()
		// prevent subsequent use of the txn value
		txn.OverlayFS = nil
	}()
	return fun(txn)
}

// Update provides support for atomic read-write transactions on an
// OverlayFS. Only a single read-write transaction can occur at any
// time, and read-write transactions are mutually exclusive with
// read-only transactions. Essentially, this is the simple "one
// writer, many readers" model. Note that nested transactions of any
// sort are not supported, and will probably deadlock.
func (fs *OverlayFS) Update(fun func(txn *OverlayFSRWTxn) error) error {
	fs.mu.Lock()
	txn := &OverlayFSRWTxn{&OverlayFSROTxn{OverlayFS: fs}}
	defer func() {
		fs.mu.Unlock()
		// prevent subsequent use of the txn value
		txn.OverlayFSROTxn.OverlayFS = nil
		txn.OverlayFSROTxn = nil
	}()
	return fun(txn)
}

type OverlayFSROTxn struct {
	*OverlayFS
}

// Get *only* inspects the overlay itself. It does not, under any
// circumstances, access the underlying CueCacheFS.
func (txn *OverlayFSROTxn) Get(uri protocol.DocumentURI) (File, error) {
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

// WalkFiles walks over the overlay itself *only*. It does not,
// under any circumstances, access the underlying CueCacheFS. The fun
// only gets passed files, never directories. Note that within a
// directory, all files will be passed to fun before any directories
// are entered. No other ordering guarantees are made.
func (txn *OverlayFSROTxn) WalkFiles(fun func(File) error, uri protocol.DocumentURI) error {
	entry, err := txn.getEntryLocked(txn.pathComponents(uri))
	if err != nil {
		return err
	}
	return txn.walkFiles(fun, entry)
}

func (txn *OverlayFSROTxn) walkFiles(fun func(File) error, entry dirEntry) error {
	switch entry := entry.(type) {
	case *overlayFileEntry:
		if err := fun(entry); err != nil {
			return err
		}
	case *overlayDirEntry:
		var dirs []dirEntry
		for _, child := range entry.entries {
			if child.IsDir() {
				dirs = append(dirs, child)
			} else {
				if err := txn.walkFiles(fun, child); err != nil {
					return err
				}
			}
		}
		for _, dir := range dirs {
			if err := txn.walkFiles(fun, dir); err != nil {
				return err
			}
		}
	}
	return nil
}

type OverlayFSRWTxn struct {
	*OverlayFSROTxn
}

// Set updates the overlay, updating or creating a file with the given
// parameters. Any required parent directories will be silently
// created. Any existing file will be silently updated. However, if
// this action would require converting a file do a directory
// (i.e. the uri has a directory component that already exists in the
// overlay as a file), then an error will be returned.
//
// This modifies the overlay *only*. It does not, under any
// circumstances, access the underlying CueCacheFS.
func (txn *OverlayFSRWTxn) Set(uri protocol.DocumentURI, content []byte, mtime time.Time, version int32) (File, error) {
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
// directories will exist within the OverlayFS. Also note, Delete is
// idempotent: deleting a uri that does not exist, returns nil.
//
// This modifies the overlay *only*. It does not, under any
// circumstances, access the underlying CueCacheFS.
func (txn *OverlayFSRWTxn) Delete(uri protocol.DocumentURI) error {
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

// RootedOverlayFS is a wrapper over [OverlayFS] that provides
// implementations of [iofs.FS], [iofs.ReadDirFS], [iofs.ReadFileFS],
// [iofs.StatFS], [module.OSRootFS], and [module.ReadCUEFS].
type RootedOverlayFS struct {
	// The overlayfs always wins over the delegatefs: the delegatefs is
	// only used when overlayfs says nothing at all about the path in
	// question. The overlayfs can't currently be used to model file
	// deletions (it would need to implement some sort of
	// tombstone/blacklist) but that's ok - we don't need such
	// functionality for now.
	overlayfs  *OverlayFS
	delegatefs *RootedCueCacheFS
}

var _ CUEDirFS = (*RootedOverlayFS)(nil)

// Implementing [module.OSRootFS]
func (fs *RootedOverlayFS) OSRoot() string {
	return fs.delegatefs.OSRoot()
}

func (fs *RootedOverlayFS) pathComponents(name string) ([]string, string) {
	name = path.Join(fs.delegatefs.root, name)
	name = strings.TrimLeft(name, "/")
	components := strings.Split(name, "/")
	idx := len(components) - 1
	return components[:idx], components[idx]
}

// Implementing [iofs.FS]
func (fs *RootedOverlayFS) Open(name string) (iofs.File, error) {
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
				return &combinedDirHandle{
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

// Implementing [module.ReadCUEFS]
func (fs *RootedOverlayFS) ReadCUEFile(name string, config parser.Config) (*ast.File, error) {
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
		return file.ReadCUEFile(config)
	}

	return nil, iofs.ErrInvalid
}

// Implementing [iofs.ReadDirFS]
func (fs *RootedOverlayFS) ReadDir(name string) ([]iofs.DirEntry, error) {
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

// Invariant: as and bs must both be sorted by Name,
// ascending. Individually, as and bs must not contain duplicates.
//
// Note if an element in aIn has the same name as an element in bIn,
// then the element in aIn is selected in preference, and those
// entries in bIn are ignored.
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

// Implementing [iofs.ReadFileFS]
func (fs *RootedOverlayFS) ReadFile(name string) ([]byte, error) {
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

// Implementing [iofs.StatFS]
func (fs *RootedOverlayFS) Stat(name string) (iofs.FileInfo, error) {
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

type combinedDirHandle struct {
	overlayHandle  *overlayDirHandle
	delegateHandle iofs.ReadDirFile

	overlayEntryNames map[string]struct{}
}

var _ iofs.ReadDirFile = (*combinedDirHandle)(nil)

// Implementing [iofs.File]
func (handle *combinedDirHandle) Stat() (iofs.FileInfo, error) {
	return handle.overlayHandle.entry, nil
}

// Implementing [iofs.File]
func (handle *combinedDirHandle) Read(buf []byte) (int, error) { return 0, errors.ErrUnsupported }

// Implementing [iofs.File]
func (handle *combinedDirHandle) Close() error { return nil }

// Implementing [iofs.ReadDirFile]
func (handle *combinedDirHandle) ReadDir(n int) ([]iofs.DirEntry, error) {
	// NB [iofs.ReadDirFile] does not require the results to be sorted in any way.
	overlayEntryNames := handle.overlayEntryNames
	if overlayEntryNames == nil {
		overlayEntryNames = make(map[string]struct{})
		handle.overlayEntryNames = overlayEntryNames
	}

	if n <= 0 { // read everything
		overlayEntries, err := handle.overlayHandle.ReadDir(0)
		if err != nil {
			return nil, err
		}

		delegateEntries, err := handle.delegateHandle.ReadDir(0)
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
	overlayEntries, err := handle.overlayHandle.ReadDir(n)
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

		delegateEntries, err := handle.delegateHandle.ReadDir(remaining)
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
