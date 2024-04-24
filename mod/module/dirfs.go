package module

import (
	"io/fs"
	"os"
)

// SourceLoc represents the location of some CUE source code.
type SourceLoc struct {
	// FS is the filesystem containing the source.
	FS fs.FS
	// Dir is the directory within the above filesystem.
	Dir string
}

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
	OSRootFS
} = dirFSImpl{}

type augmentedFS interface {
	fs.FS
	fs.StatFS
	fs.ReadDirFS
	fs.ReadFileFS
}

type dirFSImpl struct {
	osRoot string
	augmentedFS
}

func (fsys dirFSImpl) OSRoot() string {
	return fsys.osRoot
}
