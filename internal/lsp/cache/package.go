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
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/interpreter/embed"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/eval"
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

type embedding struct {
	*embed.Embed
	fileUri protocol.DocumentURI
	pkg     *Package
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

	embeds     map[token.Pos][]*embedding
	embeddedBy []*Package

	// isDirty means that the package needs reloading.
	isDirty bool

	// eval for the files in this package. This is updated
	// whenever the package status transitions to splendid.
	eval *eval.Evaluator

	isCue bool
	files []*File
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
		importedPkg := worklist[0]
		importedPkg.resetDefinitions()
		worklist = append(worklist[1:], importedPkg.imports...)
	}

	// downstream - packages that import us
	worklist = pkg.importedBy
	for len(worklist) > 0 {
		pkg := worklist[0]
		pkg.resetDefinitions()
		worklist = append(worklist[1:], pkg.importedBy...)
	}

	// upstream - files/pkgs we embed - no need for transitive
	for _, embeds := range pkg.embeds {
		for _, embed := range embeds {
			if embed.pkg != nil {
				embed.pkg.resetDefinitions()
			}
		}
	}
	// downstream - packages that embed us
	worklist = pkg.embeddedBy
	for len(worklist) > 0 {
		pkg := worklist[0]
		pkg.resetDefinitions()
		// yes really - initially embeddedBy but we switch here to
		// importedBy
		worklist = append(worklist[1:], pkg.importedBy...)
	}
}

func (pkg *Package) resetDefinitions() {
	if pkg.eval != nil {
		pkg.eval.Reset()
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
			w.GetFile(fileUri).removeUser(pkg)
		}
	}

	for _, importedPkg := range pkg.imports {
		importedPkg.RemoveImportedBy(pkg)
	}
	pkg.imports = nil

	for _, embeds := range pkg.embeds {
		for _, embed := range embeds {
			if embed.pkg != nil {
				embed.pkg.RemoveEmbeddedBy(pkg)
			}
		}
	}
	pkg.embeds = nil

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

	oldLspFiles := pkg.files
	pkg.modpkg = modpkg
	pkg.isDirty = false

	w.invalidateActiveFilesAndDirs()

	for _, importedPkg := range pkg.imports {
		importedPkg.RemoveImportedBy(pkg)
	}
	pkg.imports = pkg.imports[:0]

	for _, embeds := range pkg.embeds {
		for _, embed := range embeds {
			if embed.pkg != nil {
				embed.pkg.RemoveEmbeddedBy(pkg)
			}
		}
	}
	clear(pkg.embeds)

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
	lspFiles := make([]*File, len(files))
	isCue := true
	var embeds map[token.Pos][]*embedding

	for i, file := range files {
		astFiles[i] = file.Syntax
		fileUri := m.rootURI + protocol.DocumentURI("/"+file.FilePath)
		delete(m.dirtyFiles, fileUri)
		currentFiles[fileUri] = struct{}{}

		f := w.ensureFile(fileUri)
		lspFiles[i] = f
		isCue = isCue && strings.HasSuffix(string(fileUri), ".cue")
		syntax := file.Syntax
		f.setSyntax(syntax)

		var errs []error
		if file.SyntaxError != nil {
			errs = append(errs, file.SyntaxError)
		}

		if isCue && syntax != nil {
			attrsByField, err := runtime.ExtractFieldAttrsByKind(syntax, embedKind)
			if err != nil {
				errs = append(errs, err)
			}
			embeddedPaths, err := embed.EmbeddedPaths(file.FilePath, attrsByField)
			if err != nil {
				errs = append(errs, err)
			}
			if len(embeddedPaths) > 0 {
				if embeds == nil {
					embeds = make(map[token.Pos][]*embedding)
				}
				fs := w.overlayFS.IoFS(m.rootURI.Path())
				for _, embed := range embeddedPaths {
					attrPos := embed.Attribute.Pos
					filePaths := embed.Match(fs)
					embeddings := make([]*embedding, len(filePaths))
					for i, filePath := range filePaths {
						embeddings[i] = &embedding{
							Embed:   embed,
							fileUri: m.rootURI + "/" + protocol.DocumentURI(filePath),
						}
					}

					embeds[attrPos] = embeddings
				}
			}
		}

		f.ensureUser(pkg, errs...)
		w.standalone.deleteFile(fileUri)
	}
	pkg.isCue = isCue
	pkg.embeds = embeds

	for _, file := range oldLspFiles {
		if _, found := currentFiles[file.uri]; !found {
			file.removeUser(pkg)
		}
	}
	pkg.files = lspFiles

	config := eval.Config{
		IP:                     pkg.importPath,
		ImportCanonicalisation: importCanonicalisation,
		ForPackage:             pkg.forPackage,
		PkgImporters:           pkg.pkgImporters,
		ForEmbedAttribute:      pkg.forEmbedAttribute,
		PkgEmbedders:           pkg.pkgEmbedders,
		SupportsReferences:     isCue,
	}

	// eval.New does almost no work - calculation of resolutions is
	// done lazily. So no need to launch go-routines here.
	pkg.eval = eval.New(config, astFiles...)

	return nil
}

func (pkg *Package) linkEmbeddings() bool {
	if pkg.isCue {
		if len(pkg.embeds) == 0 {
			return false
		}

		byFileUri := make(map[protocol.DocumentURI][]*embedding)
		for _, embeddings := range pkg.embeds {
			for _, embedding := range embeddings {
				byFileUri[embedding.fileUri] = append(byFileUri[embedding.fileUri], embedding)
			}
		}
		for _, p := range pkg.module.packages {
			if p.isCue {
				continue
			}
			for _, file := range p.files {
				fileUri := file.uri
				for _, embedding := range byFileUri[fileUri] {
					embedding.pkg = p
				}
			}
		}

		m := pkg.module
		needsReload := false
		for _, embeddings := range pkg.embeds {
			for _, embedding := range embeddings {
				if embedding.pkg != nil {
					continue
				}
				fileUri := embedding.fileUri
				ip, dirUris, err := m.FindImportPathForFile(fileUri)
				if err != nil || ip == nil || len(dirUris) == 0 {
					continue
				}
				m.EnsurePackage(*ip, dirUris)
				needsReload = true
			}
		}
		if needsReload {
			pkg.markDirty()
		}
		return needsReload

	} else {
		// hmm
		return false
	}
}

func (pkg *Package) forPackage(importPath ast.ImportPath) *eval.Evaluator {
	for _, importedPkg := range pkg.imports {
		if importedPkg.importPath != importPath {
			continue
		}
		return importedPkg.eval
	}
	return nil
}

func (pkg *Package) pkgImporters() []*eval.Evaluator {
	if len(pkg.importedBy) == 0 {
		return nil
	}
	evals := make([]*eval.Evaluator, len(pkg.importedBy))
	for i, pkg := range pkg.importedBy {
		evals[i] = pkg.eval
	}
	return evals
}

func (pkg *Package) forEmbedAttribute(attr *ast.Attribute) []*eval.Evaluator {
	attrPos := attr.Pos()

	var evals []*eval.Evaluator

	embeds := pkg.embeds[attrPos]
	for _, embedding := range embeds {
		pkg := embedding.pkg
		if pkg == nil {
			continue
		}
		evals = append(evals, pkg.eval)
	}

	return evals
}

func (pkg *Package) pkgEmbedders() map[*eval.Evaluator][]*embed.Embed {
	fileUris := make(map[protocol.DocumentURI]struct{})
	for _, file := range pkg.files {
		fileUris[file.uri] = struct{}{}
	}
	embedders := make(map[*eval.Evaluator][]*embed.Embed)
	// We might be embedded by several different pkgs:
	for _, embedderPkg := range pkg.embeddedBy {
		// Each pkg might have several embed attributes:
		for _, embeds := range embedderPkg.embeds {
			// Each embed attribute might glob several files:
			for _, embedding := range embeds {
				if _, found := fileUris[embedding.fileUri]; !found {
					continue
				}
				embedders[embedderPkg.eval] = append(embedders[embedderPkg.eval], embedding.Embed)
			}
		}

	}
	return embedders
}

var embedKind = embed.New().Kind()

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

// EnsureEmbeddedBy ensures that embedder is recorded as a user of
// this package. This method is idempotent.
func (pkg *Package) EnsureEmbeddedBy(embedder *Package) {
	if slices.Contains(pkg.embeddedBy, embedder) {
		return
	}
	pkg.embeddedBy = append(pkg.embeddedBy, embedder)
}

// RemoveEmbeddedBy ensures that embedder is not recorded as a user of
// this package. This method is idempotent.
func (pkg *Package) RemoveEmbeddedBy(embedder *Package) {
	pkg.embeddedBy = slices.DeleteFunc(pkg.embeddedBy, func(p *Package) bool {
		return p == embedder
	})
}
