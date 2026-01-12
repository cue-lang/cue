package fscache

import (
	"crypto/sha256"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/robustio"
	"cuelang.org/go/mod/module"
)

// A FileHandle represents the URI, content (including parsed CUE),
// and optional version of a file tracked by the LSP session.
//
// FileHandle content may be provided by the file system or from an
// overlay, for open files.
type FileHandle interface {
	// URI is the URI for this file handle.
	URI() protocol.DocumentURI
	// ReadCUE attempts to parse the file content as CUE. The config
	// supplied governs all parts of the parsing config apart from the
	// Mode. ReadCUE will forcibly set the Mode first to ParseComments,
	// and if that fails, to ImportsOnly. The returned config is the
	// first config that produced no error, or, failing that, the last
	// config attempted.
	ReadCUE(config parser.Config) (*ast.File, parser.Config, error)
	// Version returns the file version, as defined by the LSP client.
	Version() int32
	// Content returns the contents of a file. The byte slice returned
	// is a copy of the underlying file content, and thus safe to be
	// mutated. This matches the behaviour of [iofs.ReadFileFS].
	Content() []byte
}

type diskFileEntry struct {
	uri     protocol.DocumentURI
	modTime time.Time

	// cueFileParser is a pointer because it is shared between several
	// file entries (e.g. when symlinks mean you have several uris that
	// resolve to the same underlying file).
	*cueFileParser
}

var _ FileHandle = (*diskFileEntry)(nil)

// URI implements [FileHandle]
func (entry *diskFileEntry) URI() protocol.DocumentURI { return entry.uri }

type cueFileParser struct {
	// content and buildFile are immutable
	content   []byte
	buildFile *build.File
	// TODO: will need to add the means to get the buildFile out.

	mu     sync.Mutex
	config parser.Config
	ast    *ast.File
	err    error
}

// ReadCUE implements [FileHandle]
//
// ReadCUE attempts to parse the content first with
// [parser.ParseComments], and then [parser.ImportsOnly]. The first
// attempt that succeeds (nil error) is returned. It is useful to fall
// back to ImportsOnly if there are syntax errors further on in the
// CUE.
//
// Any non-nil AST returned will have an [ast.Package] decl in the
// root of the AST. If no package decl is present in the parsed AST,
// one is created and added, using a hash of the filename as the
// package name. Always ensuring a package name is present means that
// an import path can be made for every cue file within a module,
// which means that [modpkgload.LoadPackages] can always be used to
// load a package and resolve its imports.
func (p *cueFileParser) ReadCUE(config parser.Config) (syntax *ast.File, cfg parser.Config, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.config.IsValid() {
		return p.ast, p.config, p.err
	}

	bf := p.buildFile
	if bf == nil {
		return nil, parser.Config{}, nil
	}

	filename := bf.Filename
	content := p.content

	switch bf.Encoding {
	case build.CUE:
		parseComments := parser.NewConfig(config)
		parseComments.Mode = parser.ParseComments
		importsOnly := parser.NewConfig(config)
		importsOnly.Mode = parser.ImportsOnly

		for _, cfg = range []parser.Config{parseComments, importsOnly} {
			syntax, err = parser.ParseFile(filename, content, cfg)
			if syntax != nil {
				break
			}
		}

	case build.JSON:
		syntax = &ast.File{Filename: filename}
		var expr ast.Expr
		expr, err = json.Extract(filename, content)
		switch expr := expr.(type) {
		case nil: // unparsable
		case *ast.StructLit:
			syntax.Decls = expr.Elts
		default:
			syntax.Decls = []ast.Decl{&ast.EmbedDecl{Expr: expr}}
		}

	case build.YAML:
		syntax, err = yaml.Extract(filename, content)

	default:
		return nil, parser.Config{}, nil
	}

	if syntax != nil {
		file := syntax.Pos().File()
		if file != nil {
			file.SetContent(content)
		}
		var pkg *ast.Package
		decls := syntax.Decls
		for _, decl := range decls {
			if p, ok := decl.(*ast.Package); ok {
				pkg = p
				break
			}
		}
		if pkg == nil {
			pkg = &ast.Package{PackagePos: syntax.Pos()}
			if len(decls) == 0 {
				decls = append(decls, pkg)
			} else {
				decls = append(decls[:1], decls...)
				decls[0] = pkg
			}
			syntax.Decls = decls
		}
		if pkg.Name == nil || pkg.Name.Name == "" || pkg.Name.Name == "_" {
			// Important that this ident has no position.
			pkg.Name = ast.NewIdent(phantomPackageName(filename))
		}
	}

	p.config = cfg
	p.ast = syntax
	p.err = err

	return syntax, cfg, err
}

func phantomPackageName(filename string) string {
	return fmt.Sprintf("_%x", sha256.Sum256([]byte(filename)))
}

// phantomPackageNameLength contains the total length of the package
// name injected into files which do not have a package declaration.
var phantomPackageNameLength = len(phantomPackageName(""))

// IsPhantomPackage reports whether the package declaration was
// created to be injected into a file's AST for files which have no
// package declaration themselves.
func IsPhantomPackage(pkgDecl *ast.Package) bool {
	name := pkgDecl.Name
	return name != nil && !name.Pos().IsValid() && len(name.Name) == phantomPackageNameLength && name.Name[0] == '_'
}

// RemovePhantomPackageDecl removes any phantom package declaration
// from the provided AST.
func RemovePhantomPackageDecl(file *ast.File) ast.Node {
	pkgDecl, i := internal.Package(file)
	if pkgDecl == nil || !IsPhantomPackage(pkgDecl) {
		return file
	}

	decls := slices.Clone(file.Decls)
	decls = slices.Delete(decls, i, i+1)
	fileCopy := *file
	fileCopy.Decls = decls
	return &fileCopy
}

// Version implements [FileHandle]
func (entry *diskFileEntry) Version() int32 { panic("Should never be called") }

// Content implements [FileHandle]
func (entry *diskFileEntry) Content() []byte { return slices.Clone(entry.content) }

// CUECacheFS exists to cache [ast.File] values and thus amortize the
// cost of parsing cue files. It is not an overlay in any way. Its
// design is influenced by gopls's similar fs caching layer
// (cache/fs_memoized.go in the gopls repo). CUECacheFS is also
// designed to bridge the API gap between LSP, in which everything is
// a URI, and our own module code (e.g. modpkgload) which is built
// around [iofs.FS] and related interfaces.
//
// Note that CUECacheFS will return errors when reading files which
// are not understood by [filetypes.ParseFileAndType].
type CUECacheFS struct {
	mu sync.Mutex
	// Due to symlinks etc, multiple uris/paths may map to the same
	// file. A diskFileEntry has a specific URI, but cueFilesByID
	// allows us to group them together by file node id, which we then
	// use to amortize reading from disk.
	cueFilesByID map[robustio.FileID][]*diskFileEntry
}

var _ RootableFS = (*CUECacheFS)(nil)

func NewCUECachedFS() *CUECacheFS {
	return &CUECacheFS{
		cueFilesByID: make(map[robustio.FileID][]*diskFileEntry),
	}
}

// purgeCacheUnder removes from the cache entries that match or are
// enclosed by uri. It is allowed that uri here is a directory.
func (fs *CUECacheFS) purgeCacheUnder(uri protocol.DocumentURI) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for id, files := range fs.cueFilesByID {
		kept := slices.DeleteFunc(files, func(file *diskFileEntry) bool {
			return uri.Encloses(file.uri)
		})
		if len(kept) == len(files) { // no files were dropped
			// noop
		} else if len(kept) == 0 { // all files were dropped
			delete(fs.cueFilesByID, id)
		} else {
			fs.cueFilesByID[id] = kept
		}
	}
}

// ReadFile stats and (maybe) reads the file, updates the cache, and
// returns it. If uri does not exist, the error will be
// [iofs.ErrNotExist]. If uri is a directory, the error will be
// [iofs.PathError].
func (fs *CUECacheFS) ReadFile(uri protocol.DocumentURI) (FileHandle, error) {
	id, mtime, err := robustio.GetFileID(uri.Path())
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			// URI could have been a file, or a directory. In both cases
			// it's not on disk now, so we need to purge the cache of
			// everything enclosed by uri.
			fs.purgeCacheUnder(uri)
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
			entryCopy := *files[0]
			entryCopy.uri = uri
			entry = &entryCopy
			files = append(files, entry)
			fs.cueFilesByID[id] = files
		}
		fs.mu.Unlock()
		return entry, nil
	}
	fs.mu.Unlock()

	// Unknown file, or file has changed. Read (or re-read) it.
	//
	// The following comment taken from gopls's cache/fs_memoized.go file:
	//
	// It is possible that a race causes us to read a file with
	// different file ID, or whose mtime differs from our
	// mtime. However, in these cases we expect the client to notify of
	// a subsequent file change, and the file content should be
	// eventually consistent.
	df, err := readFile(uri, mtime)

	fs.mu.Lock()
	// Only cache it if it's not been recentlyModified and it has no errors.
	if !recentlyModified && err == nil {
		// It's possible that two goroutines attempt to read the same
		// file at the same time, and both find the cache for the id
		// either empty or invalid. They will both proceed and perform
		// the read from disk. At this point, they will race and one
		// will overwrite and throw away the cache content from the
		// other.
		//
		// However, any subsequent re-read of the file will make use of
		// the cache, and the benefit is that we allow concurrent reads
		// from disk: keeping the mutex whilst we do the readFile call
		// would prevent any concurrency when reading from disk. Thus we
		// make the argument that this is more important than rare
		// amounts of duplicated disk-reads.
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
	// NB filePath is GOOS-appropriate (uri.Path() calls [filepath.FromSlash])
	filePath := uri.Path()

	bf, err := filetypes.ParseFileAndType(filePath, "", filetypes.Input)
	if err != nil {
		// yes, throw the error away
		return &diskFileEntry{
			modTime:       mtime,
			uri:           uri,
			cueFileParser: &cueFileParser{},
		}, nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	bf.Source = content

	return &diskFileEntry{
		modTime: mtime,
		uri:     uri,
		cueFileParser: &cueFileParser{
			content:   content,
			buildFile: bf,
		},
	}, nil
}

// IoFS implements [RootableFS]
func (fs *CUECacheFS) IoFS(root string) CUEDirFS {
	root = strings.TrimRight(root, string(os.PathSeparator))
	return &rootedCUECacheFS{
		cuecachefs: fs,
		delegatefs: os.DirFS(root).(DirFS),
		root:       root,
	}
}

type RootableFS interface {
	// IoFS creates a CUEDirFS, for the tree of files rooted at the
	// directory root. Note the root is GOOS-appropriate.
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
	iofs.SubFS
}

// rootedCUECacheFS is a wrapper over [CUECacheFS] that implements
// [iofs.FS], [iofs.ReadDirFS], [iofs.ReadFileFS], [iofs.StatFS],
// [module.OSRootFS], and [module.ReadCUEFS]
type rootedCUECacheFS struct {
	cuecachefs *CUECacheFS
	delegatefs DirFS
	// NB root is GOOS-appropriate
	root string
}

var _ CUEDirFS = (*rootedCUECacheFS)(nil)

// OSRoot implements [module.OSRootFS]
func (fs *rootedCUECacheFS) OSRoot() string {
	return fs.root
}

// Sub implements [iofs.Sub]
func (fs *rootedCUECacheFS) Sub(dir string) (iofs.FS, error) {
	root := filepath.Join(fs.root, filepath.FromSlash(dir))
	return fs.cuecachefs.IoFS(root), nil
}

// Open implements [iofs.FS]
func (fs *rootedCUECacheFS) Open(name string) (iofs.File, error) { return fs.delegatefs.Open(name) }

// ReadCUEFile implements [module.ReadCUEFS]
func (fs *rootedCUECacheFS) ReadCUEFile(name string, config parser.Config) (*ast.File, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "ReadCUEFile", Path: name, Err: iofs.ErrInvalid}
	}
	name, err := filepath.Localize(name)
	if err != nil {
		return nil, &iofs.PathError{Op: "ReadCUEFile", Path: name, Err: err}
	}

	uri := protocol.URIFromPath(filepath.Join(fs.root, name))
	fh, err := fs.cuecachefs.ReadFile(uri)
	if err != nil {
		return nil, err
	}
	ast, _, err := fh.ReadCUE(config)
	return ast, err
}

func (fs *rootedCUECacheFS) IsDirWithCUEFiles(path string) (bool, error) {
	if !iofs.ValidPath(path) {
		return false, &iofs.PathError{Op: "IsDirWithCUEFiles", Path: path, Err: iofs.ErrInvalid}
	}
	path, err := filepath.Localize(path)
	if err != nil {
		return false, &iofs.PathError{Op: "IsDirWithCUEFiles", Path: path, Err: err}
	}
	path = filepath.Join(fs.root, path)

	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	for _, e := range entries {
		bf, err := filetypes.ParseFileAndType(e.Name(), "", filetypes.Input)
		if err != nil {
			continue
		}
		switch bf.Encoding {
		case build.CUE, build.JSON, build.YAML:
		default:
			continue
		}

		ftype := e.Type()
		if ftype&iofs.ModeSymlink != 0 {
			name, err := os.Readlink(filepath.Join(path, e.Name()))
			if err != nil {
				continue
			}
			if !filepath.IsAbs(name) {
				name = filepath.Join(path, name)
			}
			finfo, err := os.Stat(name)
			if err != nil {
				continue
			}
			ftype = finfo.Mode()
		}
		if !ftype.IsRegular() {
			continue
		}
		return true, nil
	}

	return false, nil
}

// ReadDir implements [iofs.ReadDirFS]
func (fs *rootedCUECacheFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	return fs.delegatefs.ReadDir(name)
}

// ReadFile implements [iofs.ReadFileFS]
func (fs *rootedCUECacheFS) ReadFile(name string) ([]byte, error) {
	if !iofs.ValidPath(name) {
		return nil, &iofs.PathError{Op: "ReadFile", Path: name, Err: iofs.ErrInvalid}
	}
	name, err := filepath.Localize(name)
	if err != nil {
		return nil, &iofs.PathError{Op: "ReadFile", Path: name, Err: err}
	}
	uri := protocol.URIFromPath(filepath.Join(fs.root, name))
	fh, err := fs.cuecachefs.ReadFile(uri)
	if err != nil {
		return nil, err
	}

	return fh.Content(), nil
}

// Stat implements [iofs.StatFS]
func (fs *rootedCUECacheFS) Stat(name string) (iofs.FileInfo, error) {
	return fs.delegatefs.Stat(name)
}
