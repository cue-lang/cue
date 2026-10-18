package modimports

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/mod/module"
)

type ModuleFile struct {
	// FilePath holds the path of the module file
	// relative to the root of the fs. This will be
	// valid even if there's an associated error.
	//
	// If there's an error, it might not a be CUE file.
	FilePath string

	// Syntax (and SyntaxError) are the results from invoking
	// [parser.ParseFile]
	Syntax      *ast.File
	SyntaxError error
}

// AllImports returns a sorted list of all the package paths
// imported by the module files produced by modFilesIter
// in canonical form.
//
// If the modFilesIter yields an err then AllImports immediately stops
// and returns the accumulated package paths.
func AllImports(modFilesIter iter.Seq2[ModuleFile, error]) ([]string, error) {
	pkgPaths := make(map[string]bool)
	for mf, err := range modFilesIter {
		if err != nil {
			// The error usually names the file, so avoid repeating it here.
			return nil, err
		}
		// TODO look at build tags and omit files with "ignore" tags.
		for imp := range mf.Syntax.ImportSpecs() {
			pkgPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				// TODO location formatting
				return nil, fmt.Errorf("invalid import path %q in %s", imp.Path.Value, mf.FilePath)
			}
			// Canonicalize the path.
			pkgPath = ast.ParseImportPath(pkgPath).Canonical().String()
			pkgPaths[pkgPath] = true
		}
	}
	return slices.Sorted(maps.Keys(pkgPaths)), nil
}

// PackageFiles returns an iterator that produces all the CUE files
// inside the package with the given name at the given location.
// If pkgQualifier is "*", files from all packages in the directory will be produced.
//
// The iterator will yield an error if an I/O error is encountered
// when accessing the fsys.
//
// TODO(mvdan): this should now be called InstanceFiles, to follow the naming from
// https://cuelang.org/docs/concept/modules-packages-instances/#instances.
func PackageFiles(fsys fs.FS, dir string, pkgQualifier string) iter.Seq2[ModuleFile, error] {
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
				if isHidden(e.Name()) {
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
			if parent == dir {
				// No more parent directories; dir is "." or "/".
				return
			}
			dir = parent
		}
	}
}

// WalkOptions adjusts how [AllModuleFiles] walks the module. The zero value
// walks the packages the module's ./... pattern would, less its testdata
// rule.
type WalkOptions struct {
	// SkipDir decides whether the walk enters the directory at the given
	// path, slash-separated and relative to the root of the fs: when it
	// returns true the directory and everything below it are left out. It
	// is not called for the root itself, nor for a cue.mod directory, which
	// the walk of the main tree always leaves out. When nil, directories
	// whose name starts with "." or "_" are left out.
	SkipDir func(path string) bool

	// IncludeCueModTrees also yields, after the main tree, the files under
	// cue.mod/gen, cue.mod/pkg and cue.mod/usr, the trees the loader reaches
	// by import path. SkipDir applies there too, with paths under cue.mod. A
	// cue.mod directory below those trees is not a module boundary: a
	// vendored copy of a module may carry one, and the loader reaches the
	// files beside it all the same.
	IncludeCueModTrees bool

	// ReportLinkedDirs yields a symbolic link to a directory, which the walk
	// does not follow, as an error wrapping [ErrLinkedDir] with the link's
	// path, so that a caller can tell the files it is not seeing. By default
	// such a link is passed over in silence, as the loader's ./... passes
	// over it. A link in place of the module's own cue.mod is not reported:
	// it is read through, as the loader reads it, and the cue.mod trees are
	// walked behind it.
	ReportLinkedDirs bool
}

// ErrLinkedDir is the error [AllModuleFiles] yields for a symbolic link to
// a directory when [WalkOptions.ReportLinkedDirs] is set.
var ErrLinkedDir = errors.New("symbolic link to a directory is not followed")

// AllModuleFiles returns an iterator that produces all the CUE files inside the
// module at the given root, as opts selects them. A directory's files come
// before its subdirectories, each in name order; a directory below the root
// that holds its own cue.mod is a separate module and is left out with
// everything below it. A symbolic link is treated as the entry it resolves
// to, so a link to a CUE file is read through it; a link to a directory is
// not followed.
//
// The caller may assume that files from the same package are always adjacent.
func AllModuleFiles(fsys fs.FS, root string, opts WalkOptions) iter.Seq2[ModuleFile, error] {
	if opts.SkipDir == nil {
		opts.SkipDir = func(p string) bool { return isHidden(path.Base(p)) }
	}
	return func(yield func(ModuleFile, error) bool) {
		if !yieldAllModFiles(fsys, root, true, false, opts, yield) {
			return
		}
		if !opts.IncludeCueModTrees {
			return
		}
		// The trees are read as entries of cue.mod rather than stat'ed, so
		// that a symbolic link in place of one is seen as a link and not
		// followed, like any other link to a directory; and SkipDir has
		// its say on each tree as on any directory.
		cueMod := path.Join(root, "cue.mod")
		entries, err := fs.ReadDir(fsys, cueMod)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				yield(ModuleFile{FilePath: cueMod}, err)
			}
			return
		}
		for _, sub := range []string{"gen", "pkg", "usr"} {
			dir := path.Join(cueMod, sub)
			i := slices.IndexFunc(entries, func(e fs.DirEntry) bool { return e.Name() == sub })
			if i < 0 {
				continue
			}
			if opts.ReportLinkedDirs && entries[i].Type()&fs.ModeSymlink != 0 {
				if info, err := fs.Stat(fsys, dir); err == nil && info.IsDir() {
					if !yield(ModuleFile{FilePath: dir}, fmt.Errorf("%s: %w", dir, ErrLinkedDir)) {
						return
					}
					continue
				}
			}
			if !entries[i].IsDir() || opts.SkipDir(dir) {
				continue
			}
			if !yieldAllModFiles(fsys, dir, true, true, opts, yield) {
				return
			}
		}
	}
}

// yieldAllModFiles implements AllModuleFiles by recursing into directories.
//
// Note that we avoid [fs.WalkDir]; it yields directory entries in lexical order,
// so we would walk `foo/bar.cue` before walking `foo/cue.mod/` and realizing
// that `foo/` is a nested module that we should be ignoring entirely.
// That could be avoided via extra `fs.Stat` calls, but those are extra fs calls.
// Using [fs.ReadDir] avoids this issue entirely, as we can loop twice.
func yieldAllModFiles(fsys fs.FS, fpath string, topDir, inCueMod bool, opts WalkOptions, yield func(ModuleFile, error) bool) bool {
	entries, err := fs.ReadDir(fsys, fpath)
	if err != nil {
		if !yield(ModuleFile{
			FilePath: fpath,
		}, err) {
			return false
		}
	}
	// Skip nested submodules entirely, except under the cue.mod trees, where
	// a cue.mod is not a boundary (see WalkOptions.IncludeCueModTrees).
	if !topDir && !inCueMod {
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
		if entry.Name() == "cue.mod" && !inCueMod {
			// A link in place of cue.mod is the module's cue.mod all the
			// same: the loader reads through it, and so do the cue.mod
			// trees below, so it is no linked directory to report.
			continue
		}
		fpath := path.Join(fpath, entry.Name())
		if opts.ReportLinkedDirs && entry.Type()&fs.ModeSymlink != 0 {
			// A link to a directory is judged as the directory it stands
			// for, by SkipDir, and not by the file-name rules below: an
			// import reaches a linked _dep as it reaches a _dep directory.
			if info, err := fs.Stat(fsys, fpath); err == nil && info.IsDir() {
				if !opts.SkipDir(fpath) {
					if !yield(ModuleFile{FilePath: fpath}, fmt.Errorf("%s: %w", fpath, ErrLinkedDir)) {
						return false
					}
				}
				continue
			}
		}
		if isHidden(entry.Name()) {
			continue
		}
		if _, ok := yieldPackageFile(fsys, fpath, func(string) bool { return true }, yield); !ok {
			return false
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if name == "cue.mod" {
			continue
		}
		fpath := path.Join(fpath, name)
		if opts.SkipDir(fpath) {
			continue
		}
		if !yieldAllModFiles(fsys, fpath, false, inCueMod, opts, yield) {
			return false
		}
	}
	return true
}

// yieldPackageFile invokes yield with the contents of the package file
// at the given path if selectPackage returns true for the file's
// package name.
//
// yield is only invoked with a non-nil error if that error originates
// within fsys. In particular, errors from the parser are found via
// the [ModuleFile.SyntaxError] field.
//
// It returns the yielded package name (if any) and reports whether
// the iteration should continue.
func yieldPackageFile(fsys fs.FS, fpath string, selectPackage func(pkgName string) bool, yield func(ModuleFile, error) bool) (pkgName string, cont bool) {
	pf := ModuleFile{
		FilePath: fpath,
	}
	var syntax *ast.File
	var syntaxErr error
	if cueFS, ok := fsys.(module.ReadCUEFS); ok {
		// The FS implementation supports reading CUE syntax directly.
		// A notable FS implementation that does this is the one
		// provided by cue/load, allowing that package to cache
		// the parsed CUE.
		// TODO maybe we should make the options here match
		// the default parser options used by cue/load for better
		// cache behavior.
		//
		// No language version is set: an FS that owns one applies it itself,
		// and the fallback below has no module to take one from. Either way
		// ImportsOnly stops before any version-dependent syntax.
		syntax, syntaxErr = cueFS.ReadCUEFile(fpath, parser.NewConfig(parser.ImportsOnly))
		if syntax == nil {
			if syntaxErr == nil {
				// This file couldn't be read-and-converted to a CUE
				// AST. Make no further attempts on this file.
				return "", true
			} else if !errors.Is(syntaxErr, errors.ErrUnsupported) {
				return "", yield(pf, syntaxErr)
			}
		}
	}
	if syntax == nil {
		// Either the FS doesn't implement [module.ReadCUEFS]
		// or the ReadCUEFile method returned ErrUnsupported,
		// so we need to acquire the syntax ourselves.
		if !strings.HasSuffix(fpath, ".cue") {
			// This fallback only supports reading `.cue` files.
			return "", true
		}

		f, err := fsys.Open(fpath)
		if err != nil {
			return "", yield(pf, err)
		}
		defer f.Close()

		// Note that we use readImports before parser.ParseFile as cue/parser
		// will always consume the whole input reader, which is often wasteful.
		//
		// TODO(mvdan): the need for readImports can go once cue/parser can work
		// on a reader in a streaming manner.
		data, err := readImports(f)
		if err != nil {
			return "", yield(pf, err)
		}
		// Add a leading "./" so that a parse error filename is consistent
		// with the other error filenames created elsewhere in the codebase.
		syntax, syntaxErr = parser.ParseFile("./"+fpath, data, parser.ImportsOnly)
		if syntax == nil {
			return "", yield(pf, syntaxErr)
		}
	}

	if !selectPackage(syntax.PackageName()) {
		return "", true
	}
	pf.Syntax = syntax
	pf.SyntaxError = syntaxErr
	return syntax.PackageName(), yield(pf, nil)
}

func isHidden(name string) bool {
	return name == "" || name[0] == '.' || name[0] == '_'
}
