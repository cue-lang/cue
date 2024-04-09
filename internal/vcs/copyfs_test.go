package vcs

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// copyFS is copied (and slightly adapted) from Go tip as of 315b6ae682a2a4e7718924a45b8b311a0fe10043.
// TODO use [os.CopyFS] when we can.

// copyFS copies the file system fsys into the directory dir,
// creating dir if necessary.
//
// Newly created directories and files have their default modes
// where any bits from the file in fsys that are not part of the
// standard read, write, and execute permissions will be zeroed
// out, and standard read and write permissions are set for owner,
// group, and others while retaining any existing execute bits from
// the file in fsys.
//
// Symbolic links in fsys are not supported, a *PathError with Err set
// to ErrInvalid is returned on symlink.
//
// Copying stops at and returns the first error encountered.
func copyFS(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// The original version calls safefilepath.Localize here
		// but we don't have access to that and the context is
		// limited enough that it shouldn't matter.
		newPath := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(newPath, 0777)
		}

		// TODO(panjf2000): handle symlinks with the help of fs.ReadLinkFS
		// 		once https://go.dev/issue/49580 is done.
		//		we also need safefilepath.IsLocal from https://go.dev/cl/564295.
		if !d.Type().IsRegular() {
			return &os.PathError{Op: "CopyFS", Path: path, Err: os.ErrInvalid}
		}

		r, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		info, err := r.Stat()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(newPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666|info.Mode()&0777)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, r); err != nil {
			w.Close()
			return &os.PathError{Op: "Copy", Path: newPath, Err: err}
		}
		return w.Close()
	})
}
