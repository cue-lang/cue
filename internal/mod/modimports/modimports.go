package modimports

import (
	"errors"
	"io/fs"
	"path"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cueimports"
)

type ModuleFile struct {
	// FilePath holds the path of the module file
	// relative to the root of the fs. This will be
	// valid even if there's an associated error.
	//
	// If there's an error, it might not a be CUE file.
	FilePath string

	// Syntax includes only the portion of the file up to and including
	// the imports. It will be nil if there was an error reading the file.
	Syntax *ast.File
}

// AllModuleFiles returns an iterator that produces all the CUE files inside the
// module at the given root.
func AllModuleFiles(fsys fs.FS, root string) func(func(ModuleFile, error) bool) {
	return func(yield func(ModuleFile, error) bool) {
		fs.WalkDir(fsys, root, func(fpath string, d fs.DirEntry, err error) (_err error) {
			if err != nil {
				if !yield(ModuleFile{
					FilePath: fpath,
				}, err) {
					return fs.SkipAll
				}
				return nil
			}
			if path.Base(fpath) == "cue.mod" {
				return fs.SkipDir
			}
			if d.IsDir() {
				if fpath == root {
					return nil
				}
				base := path.Base(fpath)
				if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
					return fs.SkipDir
				}
				_, err := fs.Stat(fsys, path.Join(fpath, "cue.mod"))
				if err == nil {
					// TODO is it enough to have a cue.mod directory
					// or should we look for cue.mod/module.cue too?
					return fs.SkipDir
				}
				if !errors.Is(err, fs.ErrNotExist) {
					// We haven't got a package file to produce with the
					// error here. Should we just ignore the error or produce
					// a ModuleFile with an empty path?
					yield(ModuleFile{}, err)
					return fs.SkipAll
				}
				return nil
			}
			if !strings.HasSuffix(fpath, ".cue") {
				return nil
			}
			if !yieldPackageFile(fsys, fpath, yield) {
				return fs.SkipAll
			}
			return nil
		})
	}
}

func yieldPackageFile(fsys fs.FS, path string, yield func(ModuleFile, error) bool) bool {
	pf := ModuleFile{
		FilePath: path,
	}
	f, err := fsys.Open(path)
	if err != nil {
		return yield(pf, err)
	}
	defer f.Close()
	data, err := cueimports.Read(f)
	if err != nil {
		return yield(pf, err)
	}
	syntax, err := parser.ParseFile(path, data, parser.ParseComments)
	if err != nil {
		return yield(pf, err)
	}
	pf.Syntax = syntax
	return yield(pf, nil)
}
