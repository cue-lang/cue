// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"errors"
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/definitions"
	"cuelang.org/go/internal/mod/modpkgload"
)

type packageOrModule interface {
	// markFileDirty records that the file is dirty. It is required
	// that the file is relevant (i.e. don't try to tell a package that
	// a file is dirty if that file has nothing to do with that
	// package)
	markFileDirty(file protocol.DocumentURI)
	// encloses reports whether the file is enclosed by the package or
	// module.
	encloses(file protocol.DocumentURI) bool
	// activeFilesAndDirs adds entries for the package or module's
	// active files and directories to the given files or dirs maps
	// respectively.
	//
	// An "active" file is any file that we have loaded. This includes
	// modules loading cue.mod/module.cue files, along with all the cue
	// files loaded by each package that we've loaded.
	//
	// If a directory contains an active file then that directory is an
	// active directory, as are all of its ancestors, up to the module
	// root (inclusive).
	activeFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{})
}

// Package models a single CUE package within a CUE module.
type Package struct {
	// immutable fields: all set at construction only

	// module contains this package.
	module *Module
	// importPath for this package. It is always normalized and
	// canonical. It will always have a non-empty major version
	importPath ast.ImportPath

	// dirURIs are the "leaf" directories for the package. The package
	// consists of cue files in these directories and (optionally) any
	// ancestor directories only. It can contain more than one
	// directory only if the package is found using the old module
	// system (cue.mod/{gen|pkg|usr}).
	dirURIs []protocol.DocumentURI

	// mutable fields:

	// modpkg is the [modpkgload.Package] for this package, as loaded
	// by [modpkgload.LoadPackages].
	modpkg *modpkgload.Package

	// importedBy contains the packages that directly import this
	// package.
	importedBy []*Package

	// imports contains the packages that are imported by this package.
	imports []*Package

	// isDirty means that the package needs reloading.
	isDirty bool

	// definitions for the files in this package. This is updated
	// whenever the package status transitions to splendid.
	definitions *definitions.Definitions
}

// newPackage creates a new [Package] and adds it to the module.
func (m *Module) newPackage(importPath ast.ImportPath, dirs []protocol.DocumentURI) *Package {
	slices.Sort(dirs)
	pkg := &Package{
		module:     m,
		importPath: importPath,
		dirURIs:    dirs,
		isDirty:    true,
	}
	m.packages[importPath] = pkg
	m.workspace.debugLogf("%v Created", pkg)
	return pkg
}

func (pkg *Package) String() string {
	return fmt.Sprintf("Package dirs=%v importPath=%v", pkg.dirURIs, pkg.importPath)
}

// encloses implements [packageOrModule]
func (pkg *Package) encloses(file protocol.DocumentURI) bool {
	return slices.Contains(pkg.dirURIs, file.Dir())
}

// activeFilesAndDirs implements [packageOrModule]
func (pkg *Package) activeFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	if pkg.modpkg == nil {
		return
	}
	root := pkg.module.rootURI
	for _, file := range pkg.modpkg.Files() {
		fileUri := protocol.DocumentURI(string(root) + "/" + file.FilePath)
		files[fileUri] = append(files[fileUri], pkg)
		// the root will already be in dirs - the module will have seen
		// to that:
		for dir := fileUri.Dir(); dir != root; dir = dir.Dir() {
			if _, found := dirs[dir]; found {
				break
			}
			dirs[dir] = struct{}{}
		}
	}
}

// markFileDirty implements [packageOrModule]
func (pkg *Package) markFileDirty(file protocol.DocumentURI) {
	pkg.module.dirtyFiles[file] = struct{}{}
	pkg.module.pingHubAfterReload = true
	pkg.markDirty()
}

// markDirty marks the current package as being dirty, and resets both
// its own definitions and the definitions of every upstream and
// downstream package, recursively in both directions.
func (pkg *Package) markDirty() {
	if pkg.isDirty {
		return
	}

	pkg.isDirty = true
	pkg.resetDefinitions()

	// upstream - packges we import
	worklist := pkg.imports
	for len(worklist) > 0 {
		pkg := worklist[0]
		pkg.resetDefinitions()
		worklist = append(worklist[1:], pkg.imports...)
	}

	// downstream - packages that import us
	worklist = pkg.importedBy
	for len(worklist) > 0 {
		pkg := worklist[0]
		pkg.resetDefinitions()
		worklist = append(worklist[1:], pkg.importedBy...)
	}
}

func (pkg *Package) resetDefinitions() {
	if pkg.definitions != nil {
		pkg.definitions.Reset()
	}
}

// delete removes this package from its module.
func (pkg *Package) delete() {
	m := pkg.module
	delete(m.packages, pkg.importPath)

	w := m.workspace
	if modpkg := pkg.modpkg; modpkg != nil {
		for _, file := range modpkg.Files() {
			fileUri := m.rootURI + protocol.DocumentURI("/"+file.FilePath)
			w.standalone.reloadFile(fileUri)
			w.filesMutex.Lock()
			f := w.files[fileUri]
			w.filesMutex.Unlock()
			f.removeUser(pkg)
		}
	}
	for _, importedPkg := range pkg.imports {
		importedPkg.RemoveImportedBy(pkg)
	}
	pkg.imports = nil
	w.debugLogf("%v Deleted", pkg)
	w.invalidateActiveFilesAndDirs()
}

// ErrBadPackage is returned by any method that cannot proceed because
// package is in an errored state (e.g. it's been deleted).
var ErrBadPackage = errors.New("bad package")

// update instructs the package to update its state based on the
// provided [modpkgload.Package]. If this modpkg is in error then the
// package is deleted and [ErrBadPackage] is returned.
func (pkg *Package) update(modpkg *modpkgload.Package) error {
	m := pkg.module
	w := m.workspace
	if err := modpkg.Error(); err != nil {
		w.debugLogf("%v Error when reloading: %v", m, err)
		// It could be that the last file within this package was
		// deleted, so attempting to load it will create an error. So
		// the correct thing to do now is just remove any record of
		// the pkg.
		pkg.delete()
		return ErrBadPackage
	}
	w.debugLogf("%v Reloaded", pkg)

	oldModpkg := pkg.modpkg
	pkg.modpkg = modpkg
	pkg.isDirty = false

	w.invalidateActiveFilesAndDirs()

	for _, importedPkg := range pkg.imports {
		importedPkg.RemoveImportedBy(pkg)
	}
	pkg.imports = pkg.imports[:0]

	importCanonicalisation := make(map[string]ast.ImportPath)
	importCanonicalisation[modpkg.ImportPath()] = pkg.importPath
	importCanonicalisation[ast.ParseImportPath(modpkg.ImportPath()).Canonical().String()] = pkg.importPath

	for _, importedModpkg := range modpkg.Imports() {
		if isUnhandledPackage(importedModpkg) {
			continue
		}
		ip := normalizeImportPath(importedModpkg)
		modRootURI := moduleRootURI(importedModpkg)
		if importedPkg, found := w.findPackage(modRootURI, ip); found {
			importedPkg.EnsureImportedBy(pkg)
			pkg.imports = append(pkg.imports, importedPkg)
			importCanonicalisation[importedModpkg.ImportPath()] = importedPkg.importPath
			importCanonicalisation[ast.ParseImportPath(importedModpkg.ImportPath()).Canonical().String()] = importedPkg.importPath
		}
	}
	pkg.imports = slices.Clip(pkg.imports)

	currentFiles := make(map[protocol.DocumentURI]struct{})
	files := modpkg.Files()
	astFiles := make([]*ast.File, len(files))
	for i, file := range files {
		astFiles[i] = file.Syntax
		fileUri := m.rootURI + protocol.DocumentURI("/"+file.FilePath)
		delete(m.dirtyFiles, fileUri)
		currentFiles[fileUri] = struct{}{}
		f := w.ensureFile(fileUri)
		f.setSyntax(file.Syntax)
		if file.SyntaxError == nil {
			f.ensureUser(pkg)
		} else {
			f.ensureUser(pkg, file.SyntaxError)
		}
		w.standalone.deleteFile(fileUri)
	}
	if oldModpkg != nil {
		for _, file := range oldModpkg.Files() {
			fileUri := m.rootURI + protocol.DocumentURI("/"+file.FilePath)
			if _, found := currentFiles[fileUri]; !found {
				w.filesMutex.Lock()
				f := w.files[fileUri]
				w.filesMutex.Unlock()
				f.removeUser(pkg)
			}
		}
	}

	forPackage := func(importPath ast.ImportPath) *definitions.Definitions {
		for _, importedPkg := range pkg.imports {
			if importedPkg.importPath != importPath {
				continue
			}
			return importedPkg.definitions
		}
		return nil
	}

	pkgImporters := func() []*definitions.Definitions {
		if len(pkg.importedBy) == 0 {
			return nil
		}
		dfns := make([]*definitions.Definitions, len(pkg.importedBy))
		for i, pkg := range pkg.importedBy {
			dfns[i] = pkg.definitions
		}
		return dfns
	}

	// definitions.Analyse does almost no work - calculation of
	// resolutions is done lazily. So no need to launch go-routines
	// here.
	pkg.definitions = definitions.Analyse(pkg.importPath, importCanonicalisation, forPackage, pkgImporters, astFiles...)

	return nil
}

// EnsureImportedBy ensures that importer is recorded as a user of
// this package. This method is idempotent.
func (pkg *Package) EnsureImportedBy(importer *Package) {
	if slices.Contains(pkg.importedBy, importer) {
		return
	}
	pkg.importedBy = append(pkg.importedBy, importer)
}

// RemoveImportedBy ensures that importer is not recorded as a user of
// this package. This method is idempotent.
func (pkg *Package) RemoveImportedBy(importer *Package) {
	pkg.importedBy = slices.DeleteFunc(pkg.importedBy, func(p *Package) bool {
		return p == importer
	})
}
