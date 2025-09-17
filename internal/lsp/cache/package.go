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
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
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

	// isDirty means that the package is in need of being reloaded.
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
	m.workspace.debugLog(fmt.Sprintf("%v Created", pkg))
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

// markDirty marks the current package as being dirty, along with any
// package that imports this package (recursively).
func (pkg *Package) markDirty() {
	if pkg.isDirty {
		return
	}

	pkg.isDirty = true
	for _, importer := range pkg.importedBy {
		importer.markDirty()
	}
}

// delete removes this package from its module.
func (pkg *Package) delete() {
	m := pkg.module
	delete(m.packages, pkg.importPath)

	w := m.workspace
	if modpkg := pkg.modpkg; modpkg != nil {
		for _, file := range modpkg.Files() {
			delete(w.mappers, file.Syntax.Pos().File())
		}
		for _, importedModpkg := range modpkg.Imports() {
			if isUnhandledPackage(importedModpkg) {
				continue
			}
			ip := normalizeImportPath(importedModpkg)
			modRootURI := moduleRootURI(importedModpkg)
			if importedPkg, found := w.findPackage(modRootURI, ip); found {
				importedPkg.RemoveImportedBy(pkg)
			}
		}
	}
	w.debugLog(fmt.Sprintf("%v Deleted", pkg))
	w.invalidateActiveFilesAndDirs()
}

// ErrBadPackage is returned by any method that cannot proceed because
// package is in an errored state (e.g. it's been deleted).
var ErrBadPackage = errors.New("bad package")

// update instructs the package to update its state based on the
// provided [modpkgload.Package]. If this modpkg is in error then the
// package is deleted and [ErrBadPackage] is returned.
func (pkg *Package) update(modpkg *modpkgload.Package) error {
	oldModpkg := pkg.modpkg
	pkg.modpkg = modpkg

	m := pkg.module
	w := m.workspace
	if err := modpkg.Error(); err != nil {
		w.debugLog(fmt.Sprintf("%v Error when reloading: %v", m, err))
		// It could be that the last file within this package was
		// deleted, so attempting to load it will create an error. So
		// the correct thing to do now is just remove any record of
		// the pkg.
		pkg.delete()
		return ErrBadPackage
	}
	w.debugLog(fmt.Sprintf("%v Reloaded", pkg))

	pkg.isDirty = false

	w.invalidateActiveFilesAndDirs()

	if oldModpkg != nil {
		for _, file := range oldModpkg.Files() {
			delete(w.mappers, file.Syntax.Pos().File())
		}
		for _, importedModpkg := range oldModpkg.Imports() {
			if isUnhandledPackage(importedModpkg) {
				continue
			}
			ip := normalizeImportPath(importedModpkg)
			modRootURI := moduleRootURI(importedModpkg)
			if importedPkg, found := w.findPackage(modRootURI, ip); found {
				importedPkg.RemoveImportedBy(pkg)
			}
		}
	}

	for _, importedModpkg := range modpkg.Imports() {
		if isUnhandledPackage(importedModpkg) {
			continue
		}
		ip := normalizeImportPath(importedModpkg)
		modRootURI := moduleRootURI(importedModpkg)
		if importedPkg, found := w.findPackage(modRootURI, ip); found {
			importedPkg.EnsureImportedBy(pkg)
		}
	}

	files := modpkg.Files()
	astFiles := make([]*ast.File, len(files))
	for i, f := range files {
		astFiles[i] = f.Syntax
		uri := m.rootURI + protocol.DocumentURI("/"+f.FilePath)
		tokFile := f.Syntax.Pos().File()
		w.mappers[tokFile] = protocol.NewMapper(uri, tokFile.Content())
		delete(m.dirtyFiles, uri)
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
	// here.
	pkg.definitions = definitions.Analyse(forPackage, astFiles...)

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

// Hover is very similar to Definiton. It attempts to treat the given
// uri and position as a file coordinate to some path element that can
// be resolved to one or more ast nodes, and returns the doc comments
// attached to those ast nodes.
func (pkg *Package) Hover(uri protocol.DocumentURI, pos protocol.Position) *protocol.Hover {
	tokFile, fdfns, srcMapper := pkg.definitionsForPosition(uri)
	if tokFile == nil {
		return nil
	}

	w := pkg.module.workspace

	var comments map[ast.Node][]*ast.CommentGroup
	offset, err := srcMapper.PositionOffset(pos)
	if err != nil {
		w.debugLog(err.Error())
		return nil
	}

	comments = fdfns.DocCommentsForOffset(offset)
	if len(comments) == 0 {
		return nil
	}

	// We sort comments by their location: comments within the same
	// file are sorted by offset, and across different files by
	// filepath, with the exception that comments from the current file
	// come last. The thinking here is that the comments from a remote
	// file are more likely to be not-already-on-screen.
	keys := slices.Collect(maps.Keys(comments))
	slices.SortFunc(keys, func(a, b ast.Node) int {
		aPos, bPos := a.Pos().Position(), b.Pos().Position()
		switch {
		case aPos.Filename == bPos.Filename:
			return cmp.Compare(aPos.Offset, bPos.Offset)
		case aPos.Filename == tokFile.Name():
			// The current file goes last.
			return 1
		case bPos.Filename == tokFile.Name():
			// The current file goes last.
			return -1
		default:
			return cmp.Compare(aPos.Filename, bPos.Filename)
		}
	})

	// Because in CUE docs can come from several files (and indeed
	// packages), it could be confusing if we smush them all together
	// without showing any provenance. So, for each non-empty comment,
	// we add a link to that comment as a section-footer. This can help
	// provide some context for each section of docs.
	var sb strings.Builder
	for _, key := range keys {
		addLink := false
		for _, cg := range comments[key] {
			text := cg.Text()
			text = strings.TrimRight(text, "\n")
			if text == "" {
				continue
			}
			fmt.Fprintln(&sb, text)
			addLink = true
		}
		if addLink {
			pos := key.Pos().Position()
			fmt.Fprintf(&sb, "([%s line %d](%s#L%d))\n\n", filepath.Base(pos.Filename), pos.Line, protocol.URIFromPath(pos.Filename), pos.Line)
		}
	}

	docs := strings.TrimRight(sb.String(), "\n")
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: docs,
		},
	}
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
