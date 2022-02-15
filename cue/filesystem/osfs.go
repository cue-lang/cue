package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
)

func containsAny(s, chars string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return true
			}
		}
	}
	return false
}

type OSFS struct {
	CWD string
}

func (fsys *OSFS) getAbsPath(path string) string {
	path = filepath.Clean(path)

	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(fsys.CWD, path))
	}

	return filepath.ToSlash(path)
}

func (fsys *OSFS) Open(name string) (fs.File, error) {
	name = fsys.getAbsPath(name)

	// Convert from standard path to OS specific path in FS
	f, err := os.Open(name)
	if err != nil {
		return nil, err // nil fs.File
	}
	return f, nil
}

func (fsys *OSFS) Stat(name string) (fs.FileInfo, error) {
	name = fsys.getAbsPath(name)

	// Convert from standard path to OS specific path in FS
	f, err := os.Stat(name)

	if err != nil {
		return nil, err
	}
	return f, nil
}
