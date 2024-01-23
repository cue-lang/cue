package load

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/slices"
)

func TestIOFS(t *testing.T) {
	dir := t.TempDir()
	onDiskFiles := []string{
		"foo/bar/a",
		"foo/bar/b",
		"foo/baz",
		"arble",
	}
	for _, f := range onDiskFiles {
		writeFile(t, filepath.Join(dir, f), f)
	}
	var fsys fileSystem
	overlayFiles := []string{
		"foo/bar/a",
		"foo/bar/c",
		"other/x",
	}
	overlay := map[string]Source{}
	for _, f := range overlayFiles {
		overlay[filepath.Join(dir, f)] = FromString(f + " overlay")
	}

	err := fsys.init(filepath.Join(dir, "foo"), overlay)
	qt.Assert(t, qt.IsNil(err))
	ffsys := fsys.ioFS(dir)
	err = fstest.TestFS(ffsys, append(slices.Clip(onDiskFiles), overlayFiles...)...)
	qt.Assert(t, qt.IsNil(err))
	checked := make(map[string]bool)
	for _, f := range overlayFiles {
		data, err := fs.ReadFile(ffsys, f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(data), f+" overlay"))
		checked[f] = true
	}
	for _, f := range onDiskFiles {
		if checked[f] {
			continue
		}
		data, err := fs.ReadFile(ffsys, f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(data), f))
	}
}

func writeFile(t *testing.T, fpath string, content string) {
	err := os.MkdirAll(filepath.Dir(fpath), 0o777)
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(fpath, []byte(content), 0o666)
	qt.Assert(t, qt.IsNil(err))
}
