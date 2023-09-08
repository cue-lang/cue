package registrytest

import (
	"bytes"
	"io"
	"os"
	"path"
	"time"

	"golang.org/x/tools/txtar"
)

type txtarFileIO struct{}

func (txtarFileIO) Path(f txtar.File) string {
	return f.Name
}

func (txtarFileIO) Lstat(f txtar.File) (os.FileInfo, error) {
	return fakeFileInfo{f}, nil
}

func (txtarFileIO) Open(f txtar.File) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.Data)), nil
}

func (txtarFileIO) Mode() os.FileMode {
	return 0o444
}

type fakeFileInfo struct {
	f txtar.File
}

func (fi fakeFileInfo) Name() string {
	return path.Base(fi.f.Name)
}

func (fi fakeFileInfo) Size() int64 {
	return int64(len(fi.f.Data))
}

func (fi fakeFileInfo) Mode() os.FileMode {
	return 0o644
}

func (fi fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi fakeFileInfo) IsDir() bool        { return false }
func (fi fakeFileInfo) Sys() interface{}   { return nil }
