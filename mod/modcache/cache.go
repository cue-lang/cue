// Package modcache provides a file-based cache for modules.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package modcache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rogpeppe/go-internal/lockedfile"

	"cuelang.org/go/internal/robustio"
	"cuelang.org/go/mod/module"
)

var errNotCached = fmt.Errorf("not in cache")

// readDiskModFile reads a cached go.mod file from disk,
// returning the name of the cache file and the result.
// If the read fails, the caller can use
// writeDiskModFile(file, data) to write a new cache entry.
func (c *Cache) readDiskModFile(mv module.Version) (file string, data []byte, err error) {
	return c.readDiskCache(mv, "mod")
}

// writeDiskModFile writes a cue.mod/module.cue cache entry.
// The file name must have been returned by a previous call to readDiskModFile.
func (c *Cache) writeDiskModFile(ctx context.Context, file string, text []byte) error {
	return c.writeDiskCache(ctx, file, text)
}

// readDiskCache is the generic "read from a cache file" implementation.
// It takes the revision and an identifying suffix for the kind of data being cached.
// It returns the name of the cache file and the content of the file.
// If the read fails, the caller can use
// writeDiskCache(file, data) to write a new cache entry.
func (c *Cache) readDiskCache(mv module.Version, suffix string) (file string, data []byte, err error) {
	file, err = c.cachePath(mv, suffix)
	if err != nil {
		return "", nil, errNotCached
	}
	data, err = robustio.ReadFile(file)
	if err != nil {
		return file, nil, errNotCached
	}
	return file, data, nil
}

// writeDiskCache is the generic "write to a cache file" implementation.
// The file must have been returned by a previous call to readDiskCache.
func (c *Cache) writeDiskCache(ctx context.Context, file string, data []byte) error {
	if file == "" {
		return nil
	}
	// Make sure directory for file exists.
	if err := os.MkdirAll(filepath.Dir(file), 0777); err != nil {
		return err
	}

	// Write the file to a temporary location, and then rename it to its final
	// path to reduce the likelihood of a corrupt file existing at that final path.
	f, err := tempFile(ctx, filepath.Dir(file), filepath.Base(file), 0666)
	if err != nil {
		return err
	}
	defer func() {
		// Only call os.Remove on f.Name() if we failed to rename it: otherwise,
		// some other process may have created a new file with the same name after
		// the rename completed.
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := robustio.Rename(f.Name(), file); err != nil {
		return err
	}
	return nil
}

// downloadDir returns the directory for storing.
// An error will be returned if the module path or version cannot be escaped.
// An error satisfying [errors.Is](err, [fs.ErrNotExist]) will be returned
// along with the directory if the directory does not exist or if the directory
// is not completely populated.
func (c *Cache) downloadDir(m module.Version) (string, error) {
	if !m.IsCanonical() {
		return "", fmt.Errorf("non-semver module version %q", m.Version())
	}
	enc, err := module.EscapePath(m.BasePath())
	if err != nil {
		return "", err
	}
	encVer, err := module.EscapeVersion(m.Version())
	if err != nil {
		return "", err
	}

	// Check whether the directory itself exists.
	dir := filepath.Join(c.dir, "extract", enc+"@"+encVer)
	if fi, err := os.Stat(dir); os.IsNotExist(err) {
		return dir, err
	} else if err != nil {
		return dir, &downloadDirPartialError{dir, err}
	} else if !fi.IsDir() {
		return dir, &downloadDirPartialError{dir, errors.New("not a directory")}
	}

	// Check if a .partial file exists. This is created at the beginning of
	// a download and removed after the zip is extracted.
	partialPath, err := c.cachePath(m, "partial")
	if err != nil {
		return dir, err
	}
	if _, err := os.Stat(partialPath); err == nil {
		return dir, &downloadDirPartialError{dir, errors.New("not completely extracted")}
	} else if !os.IsNotExist(err) {
		return dir, err
	}
	return dir, nil
}

func (c *Cache) cachePath(m module.Version, suffix string) (string, error) {
	if !m.IsValid() || m.Version() == "" {
		return "", fmt.Errorf("non-semver module version %q", m)
	}
	esc, err := module.EscapePath(m.BasePath())
	if err != nil {
		return "", err
	}
	encVer, err := module.EscapeVersion(m.Version())
	if err != nil {
		return "", err
	}
	return filepath.Join(c.dir, "download", esc, "/@v", encVer+"."+suffix), nil
}

// downloadDirPartialError is returned by DownloadDir if a module directory
// exists but was not completely populated.
//
// downloadDirPartialError is equivalent to fs.ErrNotExist.
type downloadDirPartialError struct {
	Dir string
	Err error
}

func (e *downloadDirPartialError) Error() string     { return fmt.Sprintf("%s: %v", e.Dir, e.Err) }
func (e *downloadDirPartialError) Is(err error) bool { return err == fs.ErrNotExist }

// lockVersion locks a file within the module cache that guards the downloading
// and extraction of module data for the given module version.
func (c *Cache) lockVersion(mod module.Version) (unlock func(), err error) {
	path, err := c.cachePath(mod, "lock")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		return nil, err
	}
	return lockedfile.MutexAt(path).Lock()
}
