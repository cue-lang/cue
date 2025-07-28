package fscache_test

import (
	iofs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	"github.com/go-quicktest/qt"
)

const fileContent = "package foo\n\nx: true"

func TestCUECacheFSURI(t *testing.T) {
	_, _, onDiskFilesAbs := setup(t)

	fs := fscache.NewCUECachedFS()
	for _, f := range onDiskFilesAbs {
		uri := protocol.URIFromPath(f)
		fh, err := fs.ReadFile(uri)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.DeepEquals(fh.Content(), []byte(fileContent)))
		ast, err := fh.ReadCUE(parser.NewConfig(parser.ImportsOnly))
		qt.Assert(t, qt.IsNil(err))
		// 2 decls: 1 for the package one for x, because the
		// [parser.ImportsOnly] mode is modified to
		// [parser.ParseComments].
		qt.Assert(t, qt.Equals(len(ast.Decls), 2))
	}
}

func TestCUECacheFS(t *testing.T) {
	dir, onDiskFiles, _ := setup(t)

	fs := fscache.NewCUECachedFS().IoFS(dir)
	err := fstest.TestFS(fs, onDiskFiles...)
	qt.Assert(t, qt.IsNil(err))
}

func TestOverlayFSURI(t *testing.T) {
	_, _, onDiskFilesAbs := setup(t)

	content := []byte("hello world")
	now := time.Now()

	fs := fscache.NewOverlayFS(fscache.NewCUECachedFS())
	err := fs.Update(func(txn *fscache.UpdateTxn) error {
		uri := protocol.URIFromPath(onDiskFilesAbs[0])
		_, err := txn.Get(uri)
		qt.Assert(t, qt.ErrorIs(err, iofs.ErrNotExist))

		_, err = txn.Set(uri, content, now, 7)
		qt.Assert(t, qt.IsNil(err))
		return nil
	})
	qt.Assert(t, qt.IsNil(err))

	err = fs.View(func(txn *fscache.ViewTxn) error {
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
			qt.Assert(t, qt.DeepEquals(fh.Content(), []byte(fileContent)))
			ast, err := fh.ReadCUE(parser.NewConfig(parser.ImportsOnly))
			qt.Assert(t, qt.IsNil(err))
			// 2 decls: 1 for the package one for x, because the
			// [parser.ImportsOnly] mode is modified to
			// [parser.ParseComments].
			qt.Assert(t, qt.Equals(len(ast.Decls), 2))
		}
	}
}

func TestOverlayFS(t *testing.T) {
	dir, onDiskFiles, onDiskFilesAbs := setup(t)

	overlayfs := fscache.NewOverlayFS(fscache.NewCUECachedFS())
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

	err = overlayfs.Update(func(txn *fscache.UpdateTxn) error {
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
	qt.Assert(t, qt.IsNil(err))

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
		writeFile(t, f, fileContent)
	}
	forceMFTUpdateOnWindows(t, dir)
	return dir, onDiskFiles, onDiskFilesAbs
}

func writeFile(t *testing.T, fpath string, content string) {
	err := os.MkdirAll(filepath.Dir(fpath), 0o777)
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(fpath, []byte(content), 0o666)
	qt.Assert(t, qt.IsNil(err))
}

// This code comes from Go's os/os_test.go file.
func forceMFTUpdateOnWindows(t *testing.T, path string) {
	t.Helper()

	if runtime.GOOS != "windows" {
		return
	}

	// On Windows, we force the MFT to update by reading the actual metadata from GetFileInformationByHandle and then
	// explicitly setting that. Otherwise it might get out of sync with FindFirstFile. See golang.org/issues/42637.
	if err := filepath.WalkDir(path, func(path string, d iofs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		info, err := d.Info()
		if err != nil {
			t.Fatal(err)
		}
		stat, err := os.Stat(path) // This uses GetFileInformationByHandle internally.
		if err != nil {
			t.Fatal(err)
		}
		if stat.ModTime() == info.ModTime() {
			return nil
		}
		if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
			t.Log(err) // We only log, not die, in case the test directory is not writable.
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
