package fscache_test

import (
	iofs "io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"cuelang.org/go/internal/fscache"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"github.com/go-quicktest/qt"
)

func TestCueCacheFSURI(t *testing.T) {
	_, _, onDiskFilesAbs := setup(t)

	fs := fscache.NewCueCachedFS()
	for _, f := range onDiskFilesAbs {
		uri := protocol.URIFromPath(f)
		fh, err := fs.ReadFile(uri)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.DeepEquals(fh.Content(), []byte(f)))
	}
}

func TestCueCacheFS(t *testing.T) {
	dir, onDiskFiles, _ := setup(t)

	fs := fscache.NewCueCachedFS().IoFS(dir)
	err := fstest.TestFS(fs, onDiskFiles...)
	qt.Assert(t, qt.IsNil(err))
}

func TestOverlayFSURI(t *testing.T) {
	_, _, onDiskFilesAbs := setup(t)

	content := []byte("hello world")
	now := time.Now()

	fs := fscache.NewOverlayFS(fscache.NewCueCachedFS())
	err := fs.Update(func(txn *fscache.OverlayFSRWTxn) error {
		uri := protocol.URIFromPath(onDiskFilesAbs[0])
		_, err := txn.Get(uri)
		qt.Assert(t, qt.ErrorIs(err, iofs.ErrNotExist))

		_, err = txn.Set(uri, content, now, 7)
		qt.Assert(t, qt.IsNil(err))
		return nil
	})
	qt.Assert(t, qt.IsNil(err))

	err = fs.View(func(txn *fscache.OverlayFSROTxn) error {
		uri := protocol.URIFromPath(onDiskFilesAbs[0])
		fh, err := txn.Get(uri)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.DeepEquals(fh.Content(), content))

		uri = protocol.URIFromPath(onDiskFilesAbs[1])
		_, err = txn.Get(uri)
		qt.Assert(t, qt.ErrorIs(err, iofs.ErrNotExist))
		return nil
	})
	qt.Assert(t, qt.IsNil(err))

	for i, f := range onDiskFilesAbs {
		uri := protocol.URIFromPath(f)
		fh, err := fs.ReadFile(uri)
		qt.Assert(t, qt.IsNil(err))
		if i == 0 {
			qt.Assert(t, qt.DeepEquals(fh.Content(), content))
		} else {
			qt.Assert(t, qt.DeepEquals(fh.Content(), []byte(f)))
		}
	}
}

func TestOverlayFS(t *testing.T) {
	dir, onDiskFiles, onDiskFilesAbs := setup(t)

	overlayfs := fscache.NewOverlayFS(fscache.NewCueCachedFS())
	fs := overlayfs.IoFS(dir)
	err := fstest.TestFS(fs, onDiskFiles...)
	qt.Assert(t, qt.IsNil(err))

	content := []byte("hello world")
	now := time.Now()

	extraFiles := []string{
		"foo/bar/c.cue",
		"foo/baz.cue/d.cue", // note the conversion of file to dir
	}
	extraFilesAbs := make([]string, len(extraFiles))
	for i, f := range extraFiles {
		extraFilesAbs[i] = filepath.Join(dir, filepath.FromSlash(f))
	}

	err = overlayfs.Update(func(txn *fscache.OverlayFSRWTxn) error {
		uri := protocol.URIFromPath(onDiskFilesAbs[0])
		_, err := txn.Set(uri, content, now, 7)
		qt.Assert(t, qt.IsNil(err))

		for _, f := range extraFilesAbs {
			uri := protocol.URIFromPath(f)
			_, err := txn.Set(uri, content, now, 7)
			qt.Assert(t, qt.IsNil(err))
		}

		return nil
	})

	// remove foo/baz.cue file from onDiskFiles
	onDiskFiles = append(slices.Delete(onDiskFiles, 2, 3), extraFiles...)
	err = fstest.TestFS(fs, onDiskFiles...)
	qt.Assert(t, qt.IsNil(err))
}

func setup(t *testing.T) (dir string, onDiskFiles, onDiskFilesAbs []string) {
	t.Helper()
	dir = t.TempDir()
	onDiskFiles = []string{
		"foo/bar/a.cue",
		"foo/bar/b.cue",
		"foo/baz.cue",
		"arble.cue",
	}
	onDiskFilesAbs = make([]string, len(onDiskFiles))
	for i, f := range onDiskFiles {
		onDiskFilesAbs[i] = filepath.Join(dir, filepath.FromSlash(f))
	}
	for _, f := range onDiskFilesAbs {
		writeFile(t, f, f)
	}
	return dir, onDiskFiles, onDiskFilesAbs
}

func writeFile(t *testing.T, fpath string, content string) {
	err := os.MkdirAll(filepath.Dir(fpath), 0o777)
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(fpath, []byte(content), 0o666)
	qt.Assert(t, qt.IsNil(err))
}
