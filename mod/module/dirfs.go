package module

import (
	"io/fs"
	"os"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
)

// SourceLoc represents the location of some CUE source code.
type SourceLoc struct {
	// FS is the filesystem containing the source.
	FS fs.FS
	// Dir is the directory within the above filesystem.
	Dir string
}

// ReadCUE can be implemented by an [fs.FS]
// to provide an optimized (cached) way of
// reading and parsing CUE syntax.
type ReadCUEFS interface {
	fs.FS

	// ReadCUEFile reads CUE syntax from the given path,
	// parsing it with the given configuration.
	//
	// If this method is implemented, but the implementation
	// does not support reading CUE files,
	// it should return [errors.ErrUnsupported].
	ReadCUEFile(path string, cfg parser.Config) (*ast.File, error)
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
