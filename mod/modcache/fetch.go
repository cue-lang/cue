package modcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rogpeppe/go-internal/robustio"

	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/par"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

const logging = false // TODO hook this up to CUE_DEBUG

// New returns r wrapped inside a caching layer that
// stores persistent cached content inside the given
// OS directory, typically ${CUE_CACHE_DIR}.
//
// The `module.SourceLoc.FS` fields in the locations
// returned by the registry implement the `OSRootFS` interface,
// allowing a caller to find the native OS filepath where modules
// are stored.
func New(registry *modregistry.Client, dir string) (modload.Registry, error) {
	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", dir)
	}
	return &cache{
		dir: filepath.Join(dir, "mod"),
		reg: registry,
	}, nil
}

type cache struct {
	dir              string // typically ${CUE_CACHE_DIR}/mod
	reg              *modregistry.Client
	downloadZipCache par.ErrCache[module.Version, string]
	modFileCache     par.ErrCache[string, []byte]
}

func (c *cache) Requirements(ctx context.Context, mv module.Version) ([]module.Version, error) {
	data, err := c.downloadModFile(ctx, mv)
	if err != nil {
		return nil, err
	}
	mf, err := modfile.Parse(data, mv.String())
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", mv, err)
	}
	return mf.DepVersions(), nil
}

// Fetch returns the location of the contents for the given module
// version, downloading it if necessary.
func (c *cache) Fetch(ctx context.Context, mv module.Version) (module.SourceLoc, error) {
	dir, err := c.downloadDir(mv)
	if err == nil {
		// The directory has already been completely extracted (no .partial file exists).
		return c.dirToLocation(dir), nil
	}
	if dir == "" || !errors.Is(err, fs.ErrNotExist) {
		return module.SourceLoc{}, err
	}

	// To avoid cluttering the cache with extraneous files,
	// DownloadZip uses the same lockfile as Download.
	// Invoke DownloadZip before locking the file.
	zipfile, err := c.downloadZip(ctx, mv)
	if err != nil {
		return module.SourceLoc{}, err
	}

	unlock, err := c.lockVersion(mv)
	if err != nil {
		return module.SourceLoc{}, err
	}
	defer unlock()

	// Check whether the directory was populated while we were waiting on the lock.
	_, dirErr := c.downloadDir(mv)
	if dirErr == nil {
		return c.dirToLocation(dir), nil
	}
	_, dirExists := dirErr.(*downloadDirPartialError)

	// Clean up any partially extracted directories (indicated by
	// DownloadDirPartialError, usually because of a .partial file). This is only
	// safe to do because the lock file ensures that their writers are no longer
	// active.
	parentDir := filepath.Dir(dir)
	tmpPrefix := filepath.Base(dir) + ".tmp-"

	entries, _ := os.ReadDir(parentDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), tmpPrefix) {
			RemoveAll(filepath.Join(parentDir, entry.Name())) // best effort
		}
	}
	if dirExists {
		if err := RemoveAll(dir); err != nil {
			return module.SourceLoc{}, err
		}
	}

	partialPath, err := c.cachePath(mv, "partial")
	if err != nil {
		return module.SourceLoc{}, err
	}

	// Extract the module zip directory at its final location.
	//
	// To prevent other processes from reading the directory if we crash,
	// create a .partial file before extracting the directory, and delete
	// the .partial file afterward (all while holding the lock).
	//
	// A technique used previously was to extract to a temporary directory with a random name
	// then rename it into place with os.Rename. On Windows, this can fail with
	// ERROR_ACCESS_DENIED when another process (usually an anti-virus scanner)
	// opened files in the temporary directory.
	if err := os.MkdirAll(parentDir, 0777); err != nil {
		return module.SourceLoc{}, err
	}
	if err := os.WriteFile(partialPath, nil, 0666); err != nil {
		return module.SourceLoc{}, err
	}
	if err := modzip.Unzip(dir, mv, zipfile); err != nil {
		if rmErr := RemoveAll(dir); rmErr == nil {
			os.Remove(partialPath)
		}
		return module.SourceLoc{}, err
	}
	if err := os.Remove(partialPath); err != nil {
		return module.SourceLoc{}, err
	}
	makeDirsReadOnly(dir)
	return c.dirToLocation(dir), nil
}

// ModuleVersions implements [modload.Registry.ModuleVersions].
func (c *cache) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	// TODO should this do any kind of short-term caching?
	return c.reg.ModuleVersions(ctx, mpath)
}

func (c *cache) downloadZip(ctx context.Context, mv module.Version) (zipfile string, err error) {
	return c.downloadZipCache.Do(mv, func() (string, error) {
		zipfile, err := c.cachePath(mv, "zip")
		if err != nil {
			return "", err
		}

		// Return without locking if the zip file exists.
		if _, err := os.Stat(zipfile); err == nil {
			return zipfile, nil
		}
		logf("cue: downloading %s", mv)
		unlock, err := c.lockVersion(mv)
		if err != nil {
			return "", err
		}
		defer unlock()

		if err := c.downloadZip1(ctx, mv, zipfile); err != nil {
			return "", err
		}
		return zipfile, nil
	})
}

func (c *cache) downloadZip1(ctx context.Context, mod module.Version, zipfile string) (err error) {
	// Double-check that the zipfile was not created while we were waiting for
	// the lock in downloadZip.
	if _, err := os.Stat(zipfile); err == nil {
		return nil
	}

	// Create parent directories.
	if err := os.MkdirAll(filepath.Dir(zipfile), 0777); err != nil {
		return err
	}

	// Clean up any remaining tempfiles from previous runs.
	// This is only safe to do because the lock file ensures that their
	// writers are no longer active.
	tmpPattern := filepath.Base(zipfile) + "*.tmp"
	if old, err := filepath.Glob(filepath.Join(quoteGlob(filepath.Dir(zipfile)), tmpPattern)); err == nil {
		for _, path := range old {
			os.Remove(path) // best effort
		}
	}

	// From here to the os.Rename call below is functionally almost equivalent to
	// renameio.WriteToFile. We avoid using that so that we have control over the
	// names of the temporary files (see the cleanup above) and to avoid adding
	// renameio as an extra dependency.
	f, err := tempFile(ctx, filepath.Dir(zipfile), filepath.Base(zipfile), 0666)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()

	// TODO cache the result of GetModule so we don't have to do
	// an extra round trip when we've already fetched the module file.
	m, err := c.reg.GetModule(ctx, mod)
	if err != nil {
		return err
	}
	r, err := m.GetZip(ctx)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("failed to get module zip contents: %v", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), zipfile); err != nil {
		return err
	}
	// TODO should we check the zip file for well-formedness?
	// TODO: Should we make the .zip file read-only to discourage tampering?
	return nil
}

func (c *cache) downloadModFile(ctx context.Context, mod module.Version) ([]byte, error) {
	return c.modFileCache.Do(mod.String(), func() ([]byte, error) {
		modfile, data, err := c.readDiskModFile(mod)
		if err == nil {
			return data, nil
		}
		logf("cue: downloading %s", mod)
		unlock, err := c.lockVersion(mod)
		if err != nil {
			return nil, err
		}
		defer unlock()
		// Double-check that the file hasn't been created while we were
		// acquiring the lock.
		_, data, err = c.readDiskModFile(mod)
		if err == nil {
			return data, nil
		}
		return c.downloadModFile1(ctx, mod, modfile)
	})
}

func (c *cache) downloadModFile1(ctx context.Context, mod module.Version, modfile string) ([]byte, error) {
	m, err := c.reg.GetModule(ctx, mod)
	if err != nil {
		return nil, err
	}
	data, err := m.ModuleFile(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.writeDiskModFile(ctx, modfile, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *cache) dirToLocation(fpath string) module.SourceLoc {
	return module.SourceLoc{
		FS:  module.OSDirFS(fpath),
		Dir: ".",
	}
}

// makeDirsReadOnly makes a best-effort attempt to remove write permissions for dir
// and its transitive contents.
func makeDirsReadOnly(dir string) {
	type pathMode struct {
		path string
		mode fs.FileMode
	}
	var dirs []pathMode // in lexical order
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			info, err := d.Info()
			if err == nil && info.Mode()&0222 != 0 {
				dirs = append(dirs, pathMode{path, info.Mode()})
			}
		}
		return nil
	})

	// Run over list backward to chmod children before parents.
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Chmod(dirs[i].path, dirs[i].mode&^0222)
	}
}

// RemoveAll removes a directory written by the cache, first applying
// any permission changes needed to do so.
func RemoveAll(dir string) error {
	// Module cache has 0555 directories; make them writable in order to remove content.
	filepath.WalkDir(dir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return nil // ignore errors walking in file system
		}
		if info.IsDir() {
			os.Chmod(path, 0777)
		}
		return nil
	})
	return robustio.RemoveAll(dir)
}

// quoteGlob returns s with all Glob metacharacters quoted.
// We don't try to handle backslash here, as that can appear in a
// file path on Windows.
func quoteGlob(s string) string {
	if !strings.ContainsAny(s, `*?[]`) {
		return s
	}
	var sb strings.Builder
	for _, c := range s {
		switch c {
		case '*', '?', '[', ']':
			sb.WriteByte('\\')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

// tempFile creates a new temporary file with given permission bits.
func tempFile(ctx context.Context, dir, prefix string, perm fs.FileMode) (f *os.File, err error) {
	for i := 0; i < 10000; i++ {
		name := filepath.Join(dir, prefix+strconv.Itoa(rand.Intn(1000000000))+".tmp")
		f, err = os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if os.IsExist(err) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		break
	}
	return
}

func logf(f string, a ...any) {
	if logging {
		log.Printf(f, a...)
	}
}
