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
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/mod/modpkgload"
)

type packageOrModule interface {
	MarkFileDirty(file protocol.DocumentURI)
	Encloses(file protocol.DocumentURI) bool
	// An "active" file is any file that we have loaded. This includes
	// modules loading cue.mod/module.cue files, along with all the cue
	// files loaded by each package that we've loaded.
	//
	// If a directory contains an active file then that directory is an
	// active directory, as are all of its ancestors, up to the module
	// root (inclusive).
	ActiveFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{})
}

type status uint8

const (
	// lots of code relies on the zero value being dirty. Do not change
	// this.
	dirty status = iota
	splendid
	deleted
)

type Package struct {
	// immutable: set once at construction
	module *Module
	// immutable: set once at construction
	// invariant: importPath is *always* canonical, and *always* with a non-empty version
	importPath ast.ImportPath

	// The dirURI is the "leaf" directory for the package. The package
	// consists of cue files in this directory and (optionally) any
	// ancestor directories only.
	//
	// immutable: set once at construction
	dirURI protocol.DocumentURI

	// mutable: updated whenever the package is reloaded
	pkg *modpkgload.Package
	// mutable: updated whenever the package is reloaded
	importedBy []*Package

	// mutable
	status status
}

func NewPackage(module *Module, importPath ast.ImportPath, dir protocol.DocumentURI) *Package {
	return &Package{
		module:     module,
		importPath: importPath,
		dirURI:     dir,
	}
}

func (pkg *Package) String() string {
	return fmt.Sprintf("Package dir=%v importPath=%v", pkg.dirURI, pkg.importPath)
}

func (pkg *Package) MarkFileDirty(file protocol.DocumentURI) {
	pkg.status = dirty
	pkg.module.dirtyFiles[file] = struct{}{}
}

func (pkg *Package) Encloses(file protocol.DocumentURI) bool {
	return pkg.dirURI.Encloses(file)
}

// See [packageOrModule.ActiveFilesAndDirs]
func (pkg *Package) ActiveFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	if pkg.pkg == nil {
		return
	}
	root := pkg.module.rootURI
	for _, file := range pkg.pkg.Files() {
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

// Idempotent
func (pkg *Package) EnsureImportedBy(importer *Package) {
	if slices.Contains(pkg.importedBy, importer) {
		return
	}
	pkg.importedBy = append(pkg.importedBy, importer)
}

// Idempotent
func (pkg *Package) RemoveImportedBy(importer *Package) {
	for i, p := range pkg.importedBy {
		if p == importer {
			pkg.importedBy = slices.Delete(pkg.importedBy, i, i+1)
			return
		}
	}
}
