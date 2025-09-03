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
	"strconv"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/definitions"
	"cuelang.org/go/internal/mod/modpkgload"
)

type packageOrModule interface {
	// MarkFileDirty records that the file is dirty. It is required
	// that the file is relevant (i.e. don't try to tell a package that
	// a file is dirty if that file has nothing to do with that
	// package)
	MarkFileDirty(file protocol.DocumentURI)
	// Encloses reports whether the file is enclosed by the package or
	// module.
	Encloses(file protocol.DocumentURI) bool
	// ActiveFilesAndDirs adds entries for the package or module's
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

	// pkg is the [modpkgload.Package] for this package, as loaded by
	// [modpkgload.LoadPackages].
	pkg *modpkgload.Package

	// importedBy contains the packages that directly import this
	// package.
	importedBy []*Package

	// status of this Package.
	status status

	// definitions for the files in this package. This is updated
	// whenever the package status transitions to splendid.
	definitions *definitions.Definitions
	// mappers is for converting between different file coordinate
	// systems. This is updated alongsite definitions.
	mappers map[*token.File]*protocol.Mapper
}

func NewPackage(module *Module, importPath ast.ImportPath, dirs []protocol.DocumentURI) *Package {
	slices.Sort(dirs)
	return &Package{
		module:     module,
		importPath: importPath,
		dirURIs:    dirs,
	}
}

func (pkg *Package) String() string {
	return fmt.Sprintf("Package dirs=%v importPath=%v", pkg.dirURIs, pkg.importPath)
}

// MarkFileDirty implements [packageOrModule]
func (pkg *Package) MarkFileDirty(file protocol.DocumentURI) {
	pkg.setStatus(dirty)
	pkg.module.dirtyFiles[file] = struct{}{}
}

// Encloses implements [packageOrModule]
func (pkg *Package) Encloses(file protocol.DocumentURI) bool {
	return slices.Contains(pkg.dirURIs, file.Dir())
}

// ActiveFilesAndDirs implements [packageOrModule]
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

// setStatus sets the package's status. If the status is transitioning
// to a splendid status, then definitions and mappers are created and
// stored in the package.
func (pkg *Package) setStatus(status status) {
	if pkg.status == status {
		return
	}
	pkg.status = status

	switch status {
	case dirty:
		for _, importer := range pkg.importedBy {
			importer.setStatus(dirty)
		}

	case splendid:
		w := pkg.module.workspace
		for file := range pkg.mappers {
			delete(w.mappers, file)
		}

		modpkg := pkg.pkg
		files := modpkg.Files()
		mappers := make(map[*token.File]*protocol.Mapper, len(files))
		astFiles := make([]*ast.File, len(files))
		for i, f := range files {
			astFiles[i] = f.Syntax
			uri := pkg.module.rootURI + protocol.DocumentURI("/"+f.FilePath)
			file := f.Syntax.Pos().File()
			mapper := protocol.NewMapper(uri, file.Content())
			mappers[file] = mapper
			w.mappers[file] = mapper
		}
		forPackage := func(importPath string) *definitions.Definitions {
			importPath = ast.ParseImportPath(importPath).Canonical().String()
			for _, imported := range modpkg.Imports() {
				if imported.ImportPath() != importPath {
					continue
				} else if isUnhandledPackage(imported) {
					// This includes stdlib packages, which we can't jump
					// into yet!. TODO
					return nil
				}
				ip := normalizeImportPath(imported)
				importedPkg, found := w.findPackage(moduleRootURI(imported), ip)
				if !found {
					return nil
				}
				return importedPkg.definitions
			}
			return nil
		}
		// definitions.Analyse does almost no work - calculation of
		// resolutions is done lazily. So no need to launch go-routines
		// here. Similarly, the creation of a mapper is lazy.
		pkg.mappers = mappers
		pkg.definitions = definitions.Analyse(forPackage, astFiles...)
	}
}

// Definition attempts to treat the given uri and position as a file
// coordinate to some path element that can be resolved to one or more
// ast nodes, and returns the positions of the definitions of those
// nodes.
func (pkg *Package) Definition(uri protocol.DocumentURI, pos protocol.Position) []protocol.Location {
	tokFile, fdfns, srcMapper := pkg.definitionsForPosition(uri)
	if tokFile == nil {
		return nil
	}

	w := pkg.module.workspace
	mappers := w.mappers

	var targets []ast.Node
	// If DefinitionsForOffset returns no results, and if it's safe to
	// do so, we back off the Character offset (column number) by 1 and
	// try again. This can help when the caret symbol is a | and is
	// placed straight after the end of a path element.
	posAdj := []uint32{0, 1}
	if pos.Character == 0 {
		posAdj = posAdj[:1]
	}
	for _, adj := range posAdj {
		pos := pos
		pos.Character -= adj
		offset, err := srcMapper.PositionOffset(pos)
		if err != nil {
			w.debugLog(err.Error())
			continue
		}

		targets = fdfns.DefinitionsForOffset(offset)
		if len(targets) > 0 {
			break
		}
	}
	if len(targets) == 0 {
		return nil
	}

	locations := make([]protocol.Location, len(targets))
	for i, target := range targets {
		startPos := target.Pos().Position()
		endPos := target.End().Position()

		targetFile := target.Pos().File()
		targetMapper := mappers[targetFile]
		if targetMapper == nil {
			w.debugLog("mapper not found: " + targetFile.Name())
			return nil
		}
		r, err := targetMapper.OffsetRange(startPos.Offset, endPos.Offset)
		if err != nil {
			w.debugLog(err.Error())
			return nil
		}

		locations[i] = protocol.Location{
			URI:   protocol.URIFromPath(startPos.Filename),
			Range: r,
		}
	}
	return locations
}

// Completion attempts to treat the given uri and position as a file
// coordinate to some path element, from which subsequent path
// elements can be suggested.
func (pkg *Package) Completion(uri protocol.DocumentURI, pos protocol.Position) *protocol.CompletionList {
	tokFile, fdfns, srcMapper := pkg.definitionsForPosition(uri)
	if tokFile == nil {
		return nil
	}

	w := pkg.module.workspace

	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}
	content := tokFile.Content()
	// The cursor can be after the last character of the file, hence
	// len(content), and not len(content)-1.
	offset = min(offset, len(content))

	// Use offset-1 because the cursor is always one beyond what we want.
	fields, embeds, startOffset, fieldEndOffset, embedEndOffset := fdfns.CompletionsForOffset(offset - 1)

	startOffset = min(startOffset, len(content))
	fieldEndOffset = min(fieldEndOffset, len(content))
	embedEndOffset = min(embedEndOffset, len(content))

	// According to the LSP spec, TextEdits must be on the same line as
	// offset (the cursor position), and must include offset. If we're
	// in the middle of a selector that's spread over several lines
	// (possibly accidentally), we can't perform an edit.  E.g. (with
	// the cursor position as | ):
	//
	//	x: a.|
	//	y: _
	//
	// Here, the parser will treat this as "x: a.y, _" (and raise an
	// error because it got a : where it expected a newline or ,
	// ). Completions that we offer here will want to try to replace y,
	// but the cursor is on the previous line. It's also very unlikely
	// this is what the user wants. So in this case, we just treat it
	// as a simple insert at the cursor position.
	if startOffset > offset {
		startOffset = offset
		fieldEndOffset = offset
		embedEndOffset = offset
	}

	totalLen := len(fields) + len(embeds)
	if totalLen == 0 {
		return nil
	}
	sortTextLen := len(fmt.Sprint(totalLen))

	completions := make([]protocol.CompletionItem, 0, totalLen)

	for _, cs := range []struct {
		completions   []string
		endOffset     int
		kind          protocol.CompletionItemKind
		newTextSuffix string
	}{
		{
			completions:   fields,
			endOffset:     fieldEndOffset,
			kind:          protocol.FieldCompletion,
			newTextSuffix: ":",
		},
		{
			completions: embeds,
			endOffset:   embedEndOffset,
			kind:        protocol.VariableCompletion,
		},
	} {
		if len(cs.completions) == 0 {
			continue
		}

		completionRange, rangeErr := srcMapper.OffsetRange(startOffset, cs.endOffset)
		if rangeErr != nil {
			w.debugLog(rangeErr.Error())
		}
		for _, name := range cs.completions {
			if !ast.IsValidIdent(name) {
				name = strconv.Quote(name)
			}
			item := protocol.CompletionItem{
				Label:    name,
				Kind:     cs.kind,
				SortText: fmt.Sprintf("%0*d", sortTextLen, len(completions)),
				// TODO: we can add in documentation for each item if we can
				// find it.
			}
			if rangeErr == nil {
				item.TextEdit = &protocol.TextEdit{
					Range:   completionRange,
					NewText: name + cs.newTextSuffix,
				}
			}
			completions = append(completions, item)
		}
	}

	return &protocol.CompletionList{
		Items: completions,
	}
}

func (pkg *Package) definitionsForPosition(uri protocol.DocumentURI) (*token.File, *definitions.FileDefinitions, *protocol.Mapper) {
	dfns := pkg.definitions
	if dfns == nil {
		return nil, nil, nil
	}

	w := pkg.module.workspace
	mappers := w.mappers

	fdfns := dfns.ForFile(uri.Path())
	if fdfns == nil {
		w.debugLog("file not found")
		return nil, nil, nil
	}

	tokFile := fdfns.File.Pos().File()
	srcMapper := mappers[tokFile]
	if srcMapper == nil {
		w.debugLog("mapper not found: " + string(uri))
		return nil, nil, nil
	}

	return tokFile, fdfns, srcMapper
}
