package modimports

import (
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
		yieldAllModFiles(fsys, root, true, yield)
	}
}

// yieldAllModFiles implements AllModuleFiles by recursing into directories.
//
// Note that we avoid [fs.WalkDir]; it yields directory entries in lexical order,
// so we would walk `foo/bar.cue` before walking `foo/cue.mod/` and realizing
// that `foo/` is a nested module that we should be ignoring entirely.
// That could be avoided via extra `fs.Stat` calls, but those are extra fs calls.
// Using [fs.ReadDir] avoids this issue entirely, as we can loop twice.
func yieldAllModFiles(fsys fs.FS, fpath string, topDir bool, yield func(ModuleFile, error) bool) bool {
	entries, err := fs.ReadDir(fsys, fpath)
	if err != nil {
		if !yield(ModuleFile{
			FilePath: fpath,
		}, err) {
			return false
		}
	}
	// Skip nested submodules entirely.
	if !topDir {
		for _, entry := range entries {
			if entry.Name() == "cue.mod" {
				return true
			}
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		fpath := path.Join(fpath, name)
		if entry.IsDir() {
			if name == "cue.mod" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			if !yieldAllModFiles(fsys, fpath, false, yield) {
				return false
			}
		} else if !yieldPackageFile(fsys, fpath, "*", yield) {
			return false
		}
	}
	return true
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
