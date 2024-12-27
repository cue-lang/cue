package modimports

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
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
	slices.Sort(pkgPathSlice)
	return pkgPathSlice, nil
}

// PackageFiles returns an iterator that produces all the CUE files
// inside the package with the given name at the given location.
// If pkgQualifier is "*", files from all packages in the directory will be produced.
//
// TODO(mvdan): this should now be called InstanceFiles, to follow the naming from
// https://cuelang.org/docs/concept/modules-packages-instances/#instances.
func PackageFiles(fsys fs.FS, dir string, pkgQualifier string) func(func(ModuleFile, error) bool) {
	return func(yield func(ModuleFile, error) bool) {
		// Start at the target directory, but also include package files
		// from packages with the same name(s) in parent directories.
		// Stop the iteration when we find a cue.mod entry, signifying
		// the module root. If the location is inside a `cue.mod` directory
		// already, do not look at parent directories - this mimics historic
		// behavior.
		selectPackage := func(pkg string) bool {
			if pkgQualifier == "*" {
				return true
			}
			return pkg == pkgQualifier
		}
		inCUEMod := false
		if before, after, ok := strings.Cut(dir, "cue.mod"); ok {
			// We're underneath a cue.mod directory if some parent
			// element is cue.mod.
			inCUEMod =
				(before == "" || strings.HasSuffix(before, "/")) &&
					(after == "" || strings.HasPrefix(after, "/"))
		}
		var matchedPackages map[string]bool
		for {
			entries, err := fs.ReadDir(fsys, dir)
			if err != nil {
				yield(ModuleFile{
					FilePath: dir,
				}, err)
				return
			}
			inModRoot := false
			for _, e := range entries {
				if e.Name() == "cue.mod" {
					inModRoot = true
				}
				if e.IsDir() {
					// Directories are never package files, even when their filename ends with ".cue".
					continue
				}
				pkgName, cont := yieldPackageFile(fsys, path.Join(dir, e.Name()), selectPackage, yield)
				if !cont {
					return
				}
				if pkgName != "" {
					if matchedPackages == nil {
						matchedPackages = make(map[string]bool)
					}
					matchedPackages[pkgName] = true
				}
			}
			if inModRoot || inCUEMod {
				// We're at the module root or we're inside the cue.mod
				// directory. Don't go any further up the hierarchy.
				return
			}
			if matchedPackages == nil {
				// No packages possible in parent directories if there are
				// no matching package files in the package directory itself.
				return
			}
			selectPackage = func(pkgName string) bool {
				return matchedPackages[pkgName]
			}
			parent := path.Dir(dir)
			if len(parent) >= len(dir) {
				// No more parent directories.
				return
			}
			dir = parent
		}
	}
}

// AllModuleFiles returns an iterator that produces all the CUE files inside the
// module at the given root.
//
// The caller may assume that files from the same package are always adjacent.
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
	// Generate all entries for the package before moving onto packages
	// in subdirectories.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fpath := path.Join(fpath, entry.Name())
		if _, ok := yieldPackageFile(fsys, fpath, func(string) bool { return true }, yield); !ok {
			return false
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if name == "cue.mod" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		fpath := path.Join(fpath, name)
		if !yieldAllModFiles(fsys, fpath, false, yield) {
			return false
		}
	}
	return true
}

// yieldPackageFile invokes yield with the contents of the package file
// at the given path if selectPackage returns true for the file's
// package name.
//
// It returns the yielded package name (if any) and reports whether
// the iteration should continue.
func yieldPackageFile(fsys fs.FS, fpath string, selectPackage func(pkgName string) bool, yield func(ModuleFile, error) bool) (pkgName string, cont bool) {
	if !strings.HasSuffix(fpath, ".cue") {
		return "", true
	}
	pf := ModuleFile{
		FilePath: fpath,
	}
	var syntax *ast.File
	var err error
	if cueFS, ok := fsys.(module.ReadCUEFS); ok {
		// The FS implementation supports reading CUE syntax directly.
		// A notable FS implementation that does this is the one
		// provided by cue/load, allowing that package to cache
		// the parsed CUE.
		syntax, err = cueFS.ReadCUEFile(fpath)
		if err != nil && !errors.Is(err, errors.ErrUnsupported) {
			return "", yield(pf, err)
		}
	}
	if syntax == nil {
		// Either the FS doesn't implement [module.ReadCUEFS]
		// or the ReadCUEFile method returned ErrUnsupported,
		// so we need to acquire the syntax ourselves.

		f, err := fsys.Open(fpath)
		if err != nil {
			return "", yield(pf, err)
		}
		defer f.Close()

		// Note that we use cueimports.Read before parser.ParseFile as cue/parser
		// will always consume the whole input reader, which is often wasteful.
		//
		// TODO(mvdan): the need for cueimports.Read can go once cue/parser can work
		// on a reader in a streaming manner.
		data, err := cueimports.Read(f)
		if err != nil {
			return "", yield(pf, err)
		}
		// Add a leading "./" so that a parse error filename is consistent
		// with the other error filenames created elsewhere in the codebase.
		syntax, err = parser.ParseFile("./"+fpath, data, parser.ImportsOnly)
		if err != nil {
			return "", yield(pf, err)
		}
	}

	if !selectPackage(syntax.PackageName()) {
		return "", true
	}
	pf.Syntax = syntax
	return syntax.PackageName(), yield(pf, nil)
}
