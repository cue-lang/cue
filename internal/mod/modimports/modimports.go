package modimports

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cueimports"
	"cuelang.org/go/mod/module"
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

// AllImports returns a sorted list of all the package paths
// imported by the module files produced by modFilesIter
// in canonical form.
func AllImports(modFilesIter func(func(ModuleFile, error) bool)) (_ []string, retErr error) {
	pkgPaths := make(map[string]bool)
	modFilesIter(func(mf ModuleFile, err error) bool {
		if err != nil {
			retErr = fmt.Errorf("cannot read %q: %v", mf.FilePath, err)
			return false
		}
		// TODO look at build tags and omit files with "ignore" tags.
		for _, imp := range mf.Syntax.Imports {
			pkgPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				// TODO location formatting
				retErr = fmt.Errorf("invalid import path %q in %s", imp.Path.Value, mf.FilePath)
				return false
			}
			// Canonicalize the path.
			pkgPath = module.ParseImportPath(pkgPath).Canonical().String()
			pkgPaths[pkgPath] = true
		}
		return true
	})
	if retErr != nil {
		return nil, retErr
	}
	// TODO use maps.Keys when we can.
	pkgPathSlice := make([]string, 0, len(pkgPaths))
	for p := range pkgPaths {
		pkgPathSlice = append(pkgPathSlice, p)
	}
	sort.Strings(pkgPathSlice)
	return pkgPathSlice, nil
}

// PackageFiles returns an iterator that produces all the CUE files
// inside the package with the given name at the given location.
// If pkgQualifier is "*", files from all packages in the directory will be produced.
func PackageFiles(fsys fs.FS, dir string, pkgQualifier string) func(func(ModuleFile, error) bool) {
	return func(yield func(ModuleFile, error) bool) {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			yield(ModuleFile{
				FilePath: dir,
			}, err)
			return
		}
		for _, e := range entries {
			if !yieldPackageFile(fsys, path.Join(dir, e.Name()), pkgQualifier, yield) {
				return
			}
		}
	}
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
			if !yieldPackageFile(fsys, fpath, "*", yield) {
				return fs.SkipAll
			}
			return nil
		})
	}
}

func yieldPackageFile(fsys fs.FS, fpath, pkgQualifier string, yield func(ModuleFile, error) bool) bool {
	if !strings.HasSuffix(fpath, ".cue") {
		return true
	}
	pf := ModuleFile{
		FilePath: fpath,
	}
	f, err := fsys.Open(fpath)
	if err != nil {
		return yield(pf, err)
	}
	defer f.Close()
	data, err := cueimports.Read(f)
	if err != nil {
		return yield(pf, err)
	}
	syntax, err := parser.ParseFile(fpath, data, parser.ParseComments)
	if err != nil {
		return yield(pf, err)
	}
	if pkgQualifier != "*" && syntax.PackageName() != pkgQualifier {
		return true
	}
	pf.Syntax = syntax
	return yield(pf, nil)
}
