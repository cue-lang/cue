package modpkgload

import (
	"io/fs"
	"os"
)

// OSRootFS can be implemented by an [fs.FS]
// implementation to return its root directory as
// an OS file path.
type OSRootFS interface {
	fs.FS

	// OSRoot returns the root directory of the FS
	// as an OS file path. If it wasn't possible to do that,
	// it returns the empty string.
	OSRoot() string
}

// OSDirFS is like [os.DirFS] but the returned value implements
// [OSRootFS] by returning p.
func OSDirFS(p string) fs.FS {
	return dirFSImpl{
		augmentedFS: os.DirFS(p).(augmentedFS),
		osRoot:      p,
	}
}

var _ interface {
	augmentedFS
	fs.ReadFileFS
	fs.ReadDirFS
	OSRootFS
} = dirFSImpl{}

type augmentedFS interface {
	fs.FS
	fs.StatFS
	// Note: os.DirFS only started implementing ReadFileFS and
	// ReadDirFS in Go 1.21, so we can't include those here.
	// TODO add ReadDirFS and ReadFileFS when we can assume Go 1.21.
}

type dirFSImpl struct {
	osRoot string
	augmentedFS
}

func (fsys dirFSImpl) OSRoot() string {
	return fsys.osRoot
}

func (fsys dirFSImpl) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(fsys.augmentedFS, name)
}

func (fsys dirFSImpl) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(fsys.augmentedFS, name)
}
