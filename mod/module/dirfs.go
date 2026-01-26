package module

import (
	"errors"
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

// ReadCUEFS can be implemented by an [fs.FS]
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
	//
	// This method may be called with paths which do not have a `.cue`
	// suffix. If the implementation is unable to read-and-convert (as
	// necessary) a path to a CUE AST, it should  return [ErrNotCUE].
	ReadCUEFile(path string, cfg parser.Config) (*ast.File, error)

	// IsDirWithCUEFiles reports whether the given path is a directory
	// which contains files for which this implementation would attempt
	// to read and parse, if its ReadCUEFile method were called.
	//
	// If this method is implemented, but the implementation does not
	// support examining directories, it should return
	// [errors.ErrUnsupported].
	IsDirWithCUEFiles(path string) (bool, error)
}

var ErrNotCUE = errors.New("Cannot convert file to CUE")

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
