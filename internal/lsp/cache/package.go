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

// embedding couples an [embed.Embed] embed attribute with the results
// of its expansion.
type embedding struct {
	*embed.Embed
	results []*embeddingResult
}

type embeddingResult struct {
	// fileUri is immutable.
	fileUri protocol.DocumentURI
	// pkg may be nil for example if an embed attribute specifies a
	// file which does not exist. It may later be updated to non-nil.
	pkg *Package
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

	// importedBy contains the packages that directly import this
	// package.
	importedBy []*Package

	// imports contains the packages that are imported by this package.
	imports []*Package

	// embeddedBy contains the packages that directly embed this package.
	embeddedBy []*Package

	// embeddings contain the expansion of embed attributes extracted
	// from the files within this package.
	embeddings map[token.Pos]*embedding

	// isDirty means that the package needs reloading.
	isDirty bool

	// eval for the files in this package. This is updated
	// whenever the package status transitions to splendid.
	eval *eval.Evaluator

	// isCue records whether this package is made of regular ".cue"
	// files. If false, this is a package that could be embedded.
	isCue bool

	// files contains the files that make up this package.
	files map[protocol.DocumentURI]*File
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
	root := pkg.module.rootURI
	for fileUri := range pkg.files {
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

// markDirty marks the current package as being dirty.
func (pkg *Package) markDirty() {
	pkg.isDirty = true
}

// matchesUnknownEmbedding reports whether the current pkg should
// embed, but does not currently, the given fileUri.
//
// For example, if this pkg has an embed that uses a glob, and some
// time after this pkg is loaded, new file is created which also
// matches the glob, then this will method would return true.
func (pkg *Package) matchesUnknownEmbedding(fileUri protocol.DocumentURI) bool {
	filepath, wasCut := strings.CutPrefix(string(fileUri), string(pkg.module.rootURI)+"/")
	if !wasCut {
		return false
	}
	for _, embedding := range pkg.embeddings {
		if embedding.Matches(filepath) {
			for _, embedded := range embedding.results {
				if embedded.fileUri == fileUri {
					return false
				}
			}
			return true
		}
	}
	return false
}

// resetEval resets the package's own evaluator. If recursive is true,
// it also resets the evaluators of every upstream and downstream
// evaluator that could possibly contain objects (frames, navigables
// etc) from this package's evaluator.
func (pkg *Package) resetEval(recursive bool) {
	if pkg.eval != nil {
		pkg.eval.Reset()
	}

	if !recursive {
		return
	}

	// upstream - packges we import
	worklist := pkg.imports
	for len(worklist) > 0 {
		importedPkg := worklist[0]
		importedPkg.resetEval(false)
		worklist = append(worklist[1:], importedPkg.imports...)
	}

	// upstream - files/pkgs we embed - no need for transitive
	// treatment because an embedded pkg cannot embed or import
	// anything.
	for _, embedding := range pkg.embeddings {
		for _, embedded := range embedding.results {
			if embeddedPkg := embedded.pkg; embeddedPkg != nil {
				embeddedPkg.resetEval(false)
			}
		}
	}

	// downstream - packages that import (or embed) us
	worklist = append(pkg.importedBy, pkg.embeddedBy...)
	for len(worklist) > 0 {
		pkg := worklist[0]
		pkg.resetEval(false)
		worklist = append(worklist[1:], pkg.importedBy...)
	}
}

// delete removes this package from its module.
func (pkg *Package) delete() {
	pkg.resetEval(true)

	m := pkg.module
	delete(m.packages, pkg.importPath)

	w := m.workspace
	for fileUri := range pkg.files {
		w.standalone.reloadFile(fileUri)
		w.GetFile(fileUri).removeUser(pkg)
	}

	// upstream - packages we import
	for _, importedPkg := range pkg.imports {
		importedPkg.RemoveImportedBy(pkg)
	}

	// upstream - files/pkgs we embed
	for _, embedding := range pkg.embeddings {
		for _, embedded := range embedding.results {
			if embeddedPkg := embedded.pkg; embeddedPkg != nil {
				embeddedPkg.RemoveEmbeddedBy(pkg)
			}
		}
	}

	w.debugLogf("%v Deleted", pkg)
	pkg.files = nil
	pkg.isDirty = false
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

	oldFilesSet := pkg.files
	pkg.isDirty = false

	pkg.resetEval(true)

	for _, importedPkg := range pkg.imports {
		importedPkg.RemoveImportedBy(pkg)
	}

	for _, embedding := range pkg.embeddings {
		for _, embedded := range embedding.results {
			if embeddedPkg := embedded.pkg; embeddedPkg != nil {
				embeddedPkg.RemoveEmbeddedBy(pkg)
			}
		}
	}

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

	modpkgFiles := modpkg.Files()
	evalASTs := make([]*ast.File, len(modpkgFiles))
	filesSet := make(map[protocol.DocumentURI]*File, len(modpkgFiles))
	isCue := true
	var embeddings map[token.Pos]*embedding

	for i, modpkgFile := range modpkgFiles {
		evalASTs[i] = modpkgFile.Syntax
		fileUri := m.rootURI + protocol.DocumentURI("/"+modpkgFile.FilePath)
		delete(m.dirtyFiles, fileUri)

		isCue = isCue && strings.HasSuffix(string(fileUri), ".cue")

		file := w.ensureFile(fileUri)
		filesSet[fileUri] = file
		syntax := modpkgFile.Syntax
		file.setSyntax(syntax)

		var errs []error
		if modpkgFile.SyntaxError != nil {
			errs = append(errs, modpkgFile.SyntaxError)
		}

		if isCue && syntax != nil {
			// embedded files are "upstream" of us, in the same way that
			// imported packages are upstream. modpkgload discovers the
			// import graph for a given package. This includes all
			// (transitively) imported upstream packages, but no
			// downstream packages. We take the same approach for embeds,
			// so here we take a snapshot in time of the file system,
			// expanding embed attributes to file paths. Later on,
			// [Package.linkWithEmbeddedFiles] will be called which will
			// enable us to link these file paths to any packages which
			// have been found and loaded.
			//
			// There is one key difference between imported packages and
			// embedded packages: a glob embed attribute expands to
			// several packages, and so this can change over time as
			// files are added and removed from the file system. This is
			// not possible with imported packages: an import spec always
			// refers to exactly one package.
			attrsByField, err := runtime.ExtractFieldAttrsByKind(syntax, embedKind)
			if err != nil {
				errs = append(errs, err)
			}
			embeddedPaths, err := embed.EmbeddedPaths(modpkgFile.FilePath, attrsByField)
			if err != nil {
				errs = append(errs, err)
			}
			if len(embeddedPaths) > 0 {
				if embeddings == nil {
					embeddings = make(map[token.Pos]*embedding)
				}
				fs := w.overlayFS.IoFS(m.rootURI.Path())
				for _, embed := range embeddedPaths {
					embedding := &embedding{Embed: embed}
					embeddings[embed.Attribute.Pos] = embedding
					matches, err := embed.MatchAll(fs)
					if err != nil {
						errs = append(errs, err)
						continue
					}
					for _, filePath := range matches {
						embedding.results = append(embedding.results, &embeddingResult{
							fileUri: m.rootURI + "/" + protocol.DocumentURI(filePath),
						})
					}
				}
			}
		}

		file.ensureUser(pkg, errs...)
		w.standalone.deleteFile(fileUri)
	}
	pkg.isCue = isCue
	pkg.embeddings = embeddings

	for fileUri, file := range oldFilesSet {
		if _, found := filesSet[fileUri]; !found {
			file.removeUser(pkg)
		}
	}
	pkg.files = filesSet

	w.invalidateActiveFilesAndDirs()

	config := eval.Config{
		IP:                     pkg.importPath,
		ImportCanonicalisation: importCanonicalisation,
		ForPackage:             pkg.forPackage,
		PkgImporters:           pkg.pkgImporters,
		ForEmbedAttribute:      pkg.forEmbedAttribute,
		PkgEmbedders:           pkg.pkgEmbedders,
		PackageIsEmbedded:      !isCue,
	}

	// eval.New does almost no work - calculation of resolutions is
	// done lazily. So no need to launch go-routines here.
	pkg.eval = eval.New(config, evalASTs...)

	return nil
}

var embedKind = embed.New().Kind()

// linkWithEmbeddedFiles works through the pkg's embeddings,
// populating existing expansions of embed attributes with their
// corresponding package, on a best-effort basis.
func (pkg *Package) linkWithEmbeddedFiles() {
	activeFiles, _ := pkg.module.workspace.activeFilesAndDirs()

	for _, embedding := range pkg.embeddings {
		for _, embedded := range embedding.results {
			fileUri := embedded.fileUri
			if embedded.pkg != nil {
				continue
			}

			remotePkgs := activeFiles[fileUri]
			if len(remotePkgs) == 0 {
				continue
			}
			if l := len(remotePkgs); l > 1 {
				panic(fmt.Sprintf("Invariant failure: embedded file %q has %d packages. Must only have 1.", fileUri, l))
			}
			remotePkg := remotePkgs[0].(*Package)
			embedded.pkg = remotePkg
			remotePkg.EnsureEmbeddedBy(pkg)
		}
	}
}

// forPackage is a callback for the evaluator. See
// [eval.Config.ForPackage]
func (pkg *Package) forPackage(importPath ast.ImportPath) *eval.Evaluator {
	for _, importedPkg := range pkg.imports {
		if importedPkg.importPath != importPath {
			continue
		}
		return importedPkg.eval
	}
	return nil
}

// pkgImporters is a callback for the evaluator. See
// [eval.Config.PkgImporters]
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

// forEmbedAttribute is a callback for the evaluator. See
// [eval.Config.ForEmbedAttribute]
func (pkg *Package) forEmbedAttribute(attrPos token.Pos) (*embed.Embed, []*eval.Evaluator) {
	if !pkg.isCue {
		return nil, nil
	}

	// Scenario: we are a normal cue package, and we're trying to
	// resolve an embed attribute into a set of evaluators of those
	// remote embedded packages/files.

	embedding, found := pkg.embeddings[attrPos]
	if !found {
		return nil, nil
	}
	evals := make([]*eval.Evaluator, 0, len(embedding.results))
	for _, embedded := range embedding.results {
		if embeddedPkg := embedded.pkg; embeddedPkg != nil {
			evals = append(evals, embeddedPkg.eval)
		}
	}

	return embedding.Embed, evals
}

// pkgEmbedders is a callback for the evaluator. See
// [eval.Config.PkgEmbedders]
func (pkg *Package) pkgEmbedders() map[*eval.Evaluator][]*embed.Embed {
	if pkg.isCue {
		return nil
	}
	// Scenario: we are an embedded package, and we're trying to find
	// out which cue packages embed us.

	evals := make(map[*eval.Evaluator][]*embed.Embed)
	// For each package which embeds us,
	for _, embedderPkg := range pkg.embeddedBy {
		// look through all their embed attributes.
		for _, embedding := range embedderPkg.embeddings {
			// For each embed attribute, look at every pkg it expanded
			// to:
			for _, embedded := range embedding.results {
				if embedded.pkg != pkg {
					continue
				}
				evals[embedderPkg.eval] = append(evals[embedderPkg.eval], embedding.Embed)
				break
			}
		}
	}

	return evals
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
