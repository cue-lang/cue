package modcache

import (
	"io/fs"
	"os"
)

// OSRootFS can be implemented by an [fs.FS]
// implementation to return its root directory as
// an OS file path,
type OSRootFS interface {
	// OSRoot returns the root directory of the FS
	// as an OS file path, and reports whether that was possible.
	OSRoot() (string, bool)
}

func dirFS(p string) fs.FS {
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
	fs.ReadFileFS
	fs.ReadDirFS
}

type dirFSImpl struct {
	osRoot string
	augmentedFS
}

func (fsys dirFSImpl) OSRoot() (string, bool) {
	return fsys.osRoot, true
}
