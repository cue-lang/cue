// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cuelang.org/go/cue/ast"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/mod/module"
)

// Workspace corresponds to an LSP Workspace. Each LSP client/editor
// configures one workspace. A workspace may have several workspace
// folders [WorkspaceFolder].
type Workspace struct {
	registry  Registry // shared with other Workspaces
	fs        *fscache.CUECacheFS
	overlayFS *fscache.OverlayFS

	// debugLog sends the string msg to the client/editor as a log
	// message with type debug.
	debugLog func(msg string)

	// There is almost no relationship between workspace folders (which
	// are an LSP concept, and for which we request the editor watches
	// files/directorys within), and modules/packages (which are a CUE
	// concept).
	//
	// WorkspaceFolders can be nested. Modules can be nested. A single
	// module could span several WorkspaceFolders. A single
	// WorkspaceFolder could contain several modules. As much as
	// possible, we keep code that deals with workspace folders
	// separate from code which deals with cue modules+packages.
	folders  []*WorkspaceFolder
	modules  map[protocol.DocumentURI]*Module
	packages map[ast.ImportPath]*Package
	mappers  map[*token.File]*protocol.Mapper

	// These are cached values. Do not use these directly, instead, use
	// [Workspace.ActiveFilesAndDirs]
	activeFiles map[protocol.DocumentURI][]packageOrModule
	activeDirs  map[protocol.DocumentURI]struct{}
}

func NewWorkspace(cache *Cache, debugLog func(string)) *Workspace {
	return &Workspace{
		registry:  cache.registry,
		fs:        cache.fs,
		overlayFS: fscache.NewOverlayFS(cache.fs),
		debugLog:  debugLog,
		modules:   make(map[protocol.DocumentURI]*Module),
		packages:  make(map[ast.ImportPath]*Package),
		mappers:   make(map[*token.File]*protocol.Mapper),
	}
}

// EnsureFolder ensures that the folder at dir is a [WorkspaceFolder]
// within this workspace. The name is for display purposes only and
// does not have any semantics attached to it. This method is
// idempotent: if the workspace already includes a workspace folder at
// dir, then this method is a noop and returns nil.
func (w *Workspace) EnsureFolder(dir protocol.DocumentURI, name string) (*WorkspaceFolder, error) {
	inode1, err := os.Stat(dir.Path())
	if err != nil {
		return nil, err
	}
	for _, wf := range w.folders {
		if wf.dir == dir {
			return wf, nil
		}
		inode2, err := os.Stat(filepath.FromSlash(wf.dir.Path()))
		if err != nil {
			return nil, err
		}
		if os.SameFile(inode1, inode2) {
			return wf, nil
		}
	}

	folder, err := NewWorkspaceFolder(dir, name)
	if err != nil {
		return nil, err
	}
	w.folders = append(w.folders, folder)
	w.debugLog(fmt.Sprintf("Workspace folder added: %v", dir))
	return folder, nil
}

// RemoveFolder removes the folder at dir. This is idempotent.
//
// An LSP client/editor can dynamically reconfigure which workspace
// folders exist. RemoveFolder is used when the client changes its
// configuration and removes a folder.
func (w *Workspace) RemoveFolder(dir protocol.DocumentURI) {
	w.folders = slices.DeleteFunc(w.folders, func(wf *WorkspaceFolder) bool {
		if wf.dir == dir {
			w.debugLog(fmt.Sprintf("Workspace folder removed: %v", dir))
		}
		return wf.dir == dir
	})
}

// UpdateFolderOptions requests that the workspace refetches from the
// client/editor options for every workspace folder.
//
// An LSP client/editor can inform the server that its options have
// changed. It's up to the server to query the client for options for
// each workspace folder.
func (w *Workspace) UpdateFolderOptions(fetchFolderOptions func(folder protocol.DocumentURI) (*settings.Options, error)) error {
	for _, wf := range w.folders {
		options, err := fetchFolderOptions(wf.dir)
		if err != nil {
			return err
		}
		wf.UpdateOptions(options)
	}
	return nil
}

// FileWatchingGlobPatterns returns a set of glob patterns that the
// client is required to watch for changes and notify the server of
// them, in order to keep the server's state up to date.
//
// This set includes
//  1. all cue.mod/module.cue files in the workspace; and
//  2. for each WorkspaceFolder, its modules (or directory for ad-hoc views). In
//     module mode, this is the set of active modules (and for VS Code, all
//     workspace directories within them, due to golang/go#42348).
//
// The watch for workspace cue.mod/module.cue files in (1) is
// sufficient to capture changes to the repo structure that may affect
// the sets of modules and packages.  Whenever this set changes, we
// reload the workspace and invalidate memoized files.
//
// The watch for workspace directories in (2) should keep each Package
// up to date, as it should capture any newly added/modified/deleted
// cue files.
//
// Patterns are returned as a set of protocol.RelativePatterns, since they can
// always be later translated to glob patterns (i.e. strings) if the client
// lacks relative pattern support. By convention, any pattern returned with
// empty baseURI should be served as a glob pattern.
//
// In general, we prefer to serve relative patterns, as they work better on
// most clients that support both, and do not have issues with Windows driver
// letter casing:
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#relativePattern
func (w *Workspace) FileWatchingGlobPatterns(ctx context.Context) map[protocol.RelativePattern]struct{} {
	patterns := make(map[protocol.RelativePattern]struct{})

	// from golang/go#42348:
	//
	// VS Code requires that we watch directories explicitly -- a
	// deletion of a directory that contains Cue files will not give us
	// a notification if our glob pattern only contains Cue files
	// (e.g. "**/*.cue").
	//
	// We only care about deletions of directories, not about
	// creations. We care about deletions because deleting the
	// directory might also delete files that we care about. So, for every active file, if the dir

	var wfDirs []protocol.DocumentURI
	for _, wf := range w.folders {
		needsSubdirs := wf.FileWatchingGlobPatterns(patterns)
		if needsSubdirs {
			wfDirs = append(wfDirs, wf.dir)
		}
	}
	if len(wfDirs) == 0 {
		return patterns
	}

	// All these uris are absolute, so uris with fewer / separators are
	// higher up in the filesystem, so should be tested first - they
	// are more general.
	slices.SortFunc(wfDirs, func(a, b protocol.DocumentURI) int {
		return cmp.Compare(strings.Count(string(a), "/"), strings.Count(string(b), "/"))
	})

	// Overall, the number of workflow folders will not be huge, so the
	// number of uris in needingSubdirs will also not be huge. So
	// although this is not the most efficient approach, it should do
	// ok; plus most of the time, workflow folders will not be nested.
	_, activeDirs := w.activeFilesAndDirs()
	for activeDir := range activeDirs {
		for _, wfDir := range wfDirs {
			// NB: a.Encloses(b) returns true if a == b
			if wfDir.Encloses(activeDir) {
				patterns[protocol.RelativePattern{Pattern: protocol.Pattern(activeDir)}] = struct{}{}
				break
			}
		}
	}
	return patterns
}

// invalidateActiveFilesAndDirs clears the cached activeFiles and
// activeDirs values, ensuring that the next call to
// [Workspace.activeFilesAndDirs] will calculate fresh values.
func (w *Workspace) invalidateActiveFilesAndDirs() {
	w.activeFiles = nil
	w.activeDirs = nil
}

// activeFilesAndDirs gathers all the active files and directories
// from all the currently open modules and packages within the
// workspace.
//
// See [packageOrModule] for the definitions of active files, and
// active directories.
func (w *Workspace) activeFilesAndDirs() (files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	files = w.activeFiles
	dirs = w.activeDirs

	if files == nil || dirs == nil {
		files = make(map[protocol.DocumentURI][]packageOrModule)
		dirs = make(map[protocol.DocumentURI]struct{})
		w.reloadModules()
		for _, m := range w.modules {
			m.ActiveFilesAndDirs(files, dirs)
		}
		w.activeFiles = files
		w.activeDirs = dirs
	}

	return files, dirs
}

// DidModifyFiles is responsible for processing notifications of file
// modifications that are sent to us from the editor/client. There are
// two types of notification that we can receive, which are both
// catered for by the [file.Modification] type. 1) modifications that
// concern files/buffers that are open in the editor; 2) modifications
// that have happened on disk (e.g. by other tools) that the
// editor/client tells us about because of the watching globs that
// we've set up. Note that if a file is open in the editor, and there
// is a modification of that same file on disk, we should not make any
// assumption that the state of the editor has changed.
func (w *Workspace) DidModifyFiles(ctx context.Context, modifications []file.Modification) error {
	updatedFiles, err := w.updateOverlays(modifications)
	if err != nil {
		return err
	}

	activeFiles, activeDirs := w.activeFilesAndDirs()
	for uri, fh := range updatedFiles {
		// If it is in activeFiles then we know it's a file we have
		// loaded in the past.
		//
		// However, consider packages foo.com@v0:a and foo.com/b@v0:a
		// Assume both packages have at least one cue file. If we have
		// opened the foo.com module, and within it, the b:a package, we
		// may have loaded files from the parent directory (the ancestor
		// import pattern). Such files will appear in activeFiles.
		//
		// But if we have a update for a file in that parent directory
		// it might mean the editor is directly working on such a file,
		// and so we should ensure the parent package
		// (i.e. foo.com@v0:a) is loaded, and not just some (potentially
		// distant) descendent package.
		//
		// So if we know the file is open in the editor (i.e. fh != nil)
		// and we fail to find an existing package whose "leaf"
		// directory contains uri, then we must search for and open a
		// specific package for the file.
		//
		// !(fh != nil && !enclosingFound) <->
		//                 (fh == nil || enclosingFound) [De Morgan's]
		if pkgs, found := activeFiles[uri]; found {
			enclosingFound := false
			for _, pkg := range pkgs {
				pkg.MarkFileDirty(uri)
				enclosingFound = enclosingFound || pkg.Encloses(uri)
			}
			w.invalidateActiveFilesAndDirs()
			if fh == nil || enclosingFound {
				delete(updatedFiles, uri)
				continue
			}
		}

		if fh == nil {
			// This file is not open in the editor/client. But something
			// has changed about it. If there's another file in
			// activeFiles which is in the same directory, or is a
			// descendent, then we need to inspect this file.
			needsInspecting := false
			_, isDir := activeDirs[uri]
			if isDir {
				needsInspecting = true
			} else if _, found := activeDirs[uri.Dir()]; found {
				// it's possible that this is a new file which will
				// influence a sibling or descendent (neice/nephew?) file
				// by being in the same package (ancestor imports pattern)
				needsInspecting = true
			}

			if !needsInspecting {
				// sure, this might be a cue file, but it's not open in
				// the editor/client (because fh is nil), and it doesn't
				// seem to be able to influence any package we have
				// loaded. So we can ignore it.
				delete(updatedFiles, uri)
				continue
			}

			fh, err := w.fs.ReadFile(uri)
			if errors.Is(err, fs.ErrNotExist) {
				// This tells us that whatever it was (a file *or* a
				// directory), it no longer exists.
				if isDir {
					for activeUri, pkgs := range activeFiles {
						if uri.Encloses(activeUri) {
							for _, pkg := range pkgs {
								pkg.MarkFileDirty(activeUri)
							}
							w.invalidateActiveFilesAndDirs()
						}
					}
				}
				// If it wasn't a dir, well we also know it's not in
				// activeFiles (we would have "continued" before getting
				// to this point if it was in activeFiles), so it's safe
				// to ignore it.
				delete(updatedFiles, uri)
				continue

			} else if err != nil {
				// Given that we know it's not an active file, we choose
				// to assume from this error that we have had notification
				// of a newly created directory (hence you can't read it
				// as a file). So we swallow this error.
				delete(updatedFiles, uri)
				continue
			}

			updatedFiles[uri] = fh
		}
	}

	// By this point, updatedFiles only contains files (not
	// directories) that we don't know anything about, but we have
	// successfully read their contents (so the fh value is never nil)

	// Create any new modules we need
	for uri := range updatedFiles {
		if !strings.HasSuffix(string(uri), "/cue.mod/module.cue") {
			continue
		}
		delete(updatedFiles, uri)

		_ = w.newModule(uri)
	}

	// By now, all modules should be valid, and updatedFiles will only
	// have "normal" cue files in them (i.e. not /cue.mod/module.cue).
	for uri := range updatedFiles {
		// We can only parse a cue file if we know what module it belongs to.
		m, err := w.FindModuleForFile(uri)
		if err != nil {
			return err
		}
		if m == nil {
			w.debugLog(fmt.Sprintf("No module found for %v", uri))
			// TODO: something better
			continue
		}
		pkgs, err := m.FindPackagesOrModulesForFile(uri)
		if err != nil {
			if _, ok := err.(cueerrors.Error); ok {
				// Most likely a syntax error; ignore it.
				// TODO: this error might become a "diagnostics" message
				continue
			}
			return err
		}
		if len(pkgs) != 0 {
			for _, pkg := range pkgs {
				pkg.MarkFileDirty(uri)
			}
			w.invalidateActiveFilesAndDirs()
		}
		// if len(pkgs) == 0 and no error, then it means the file had no
		// package declaration. For the time being, we're ignoring
		// that. TODO something better
	}

	return w.reloadPackages()
}

func (w *Workspace) updateOverlays(modifications []file.Modification) (map[protocol.DocumentURI]fscache.FileHandle, error) {
	now := time.Now()
	updatedFiles := make(map[protocol.DocumentURI]fscache.FileHandle)

	err := w.overlayFS.Update(func(txn *fscache.UpdateTxn) error {
		// Process the non-on-disk changes first. These are changes that
		// correspond to the editor/client's open buffers. The state of
		// which we mirror in our overlayFS. These take priority over
		// the on-disk changes. In reality, I don't think a single set
		// of modifications can contain both types, but the typesigs
		// here allow it.
		for _, mod := range modifications {
			if mod.OnDisk {
				continue
			}

			var fh fscache.FileHandle
			var err error

			switch mod.Action {
			case file.Open:
				fh, err = txn.Set(mod.URI, []byte(mod.ContentChanges[0].Text), now, mod.Version)
				// TODO: should we nuke the w.fs cache here? (because it's
				// now masked by the overlayfs.)
				//
				// Also, if the file already exists in the w.fs cache, and
				// it has an ast.File, then we could reuse that here,
				// provided the content is the same. Is that really less
				// work though than just parsing the content we've
				// received from the editor/client?

			case file.Change:
				fh, err = txn.Get(mod.URI)

				if errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("updateOverlays: modifying unopened overlay %v", mod.URI)
				} else if err != nil {
					return err
				}

				if mod.Version <= fh.Version() {
					return fmt.Errorf("updateOverlays: modification from client for %v provides non-increasing version (existing: %d; supplied: %d)", mod.URI, fh.Version(), mod.Version)
				}

				content := fh.Content()
				content, err = changedText(mod.URI, content, mod.ContentChanges)
				if err != nil {
					return err
				}

				fh, err = txn.Set(mod.URI, content, now, mod.Version)

			case file.Close:
				err = txn.Delete(mod.URI)
				// If this was in the overlays then we should now attempt
				// to load it from disk. The disk version may or may not
				// be there, and if it is there, it might have completely
				// different content, including package, from the overlay
				// version.

			case file.Save:
				// a save is not the same as a close. Yes, it means the
				// content should be on disk, but it doesn't mean we can
				// now forget about it from the overlays: the file remains
				// open in the editor's/client's buffers.
				//
				// There is nothing to do here: a save notification does
				// not modify content.

			default:
				panic(fmt.Sprintf("Unsupported modification action: %v", mod.Action))
			}

			if err != nil {
				return err
			}

			// Add fh to the updateOverlays even if it's nil: if it's nil
			// then it's from a Close action, so the overlay has gone,
			// but we now need to go inspect what's actually on
			// disk. That's what a nil value in updatedFiles signifies.
			//
			// file.Save is really a noop for us, so don't include it in
			// updatedFiles.
			if mod.Action != file.Save {
				updatedFiles[mod.URI] = fh
			}
		}

		// We now process the on-disk modifications.
		for _, mod := range modifications {
			if !mod.OnDisk {
				continue
			} else if _, found := updatedFiles[mod.URI]; found {
				// This probably can't happen because as mentioned
				// earlier, modifications will either be all on-disk, or
				// all not on-disk.
				continue
			}

			// For all the possible actions, the behaviour is the same.
			// If the file exists in the overlays then the overlay trumps
			// the on-disk content (the overlay represents a file open in
			// the editor/client).
			//
			// Even for a create action, given it's now on disk, it's
			// entirely possible that that's just responding to the
			// editor doing the first save of a file that we already knew
			// about via the overlays.
			//
			// For a delete action, if the file is in the overlays we
			// keep using the overlay until the edtior/client closes that
			// buffer.
			_, err := txn.Get(mod.URI)

			if mod.Action == file.Delete && errors.Is(err, fs.ErrInvalid) {
				// this is fine: this error means the delete is a delete
				// of a directory, not a file. We should still add this to
				// updatedFiles because (a) just because it was a
				// directory in the overlays doesn't mean it's a directory
				// on disk; (b) if it's in updatedFiles then that'll force
				// us to read this uri from disk, and that'll purge caches
				// if it turns out this is/was a directory on disk.
				updatedFiles[mod.URI] = nil

			} else if errors.Is(err, fs.ErrNotExist) {
				// If the uri exists in the overlays then we ignore the
				// on-disk modification because the overlays always trumps
				// disk. But here, the uri does not exist in the
				// overlays. So we add to updatedFiles in order to force
				// inspection of the disk content later on.
				updatedFiles[mod.URI] = nil
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updatedFiles, nil
}

// FindModuleForFile attempts to find the most-specific
// (i.e. accommodating nested modules) module for the given file. This
// may result in new modules being added to the workspace.
//
// If no module can be found, this method returns nil, nil.
func (w *Workspace) FindModuleForFile(file protocol.DocumentURI) (*Module, error) {
	w.reloadModules()
	fileDir := file.Dir()
	var candidate *Module
	for _, m := range w.modules {
		if m.rootURI == fileDir {
			// could not be more specific
			return m, nil
		} else if m.rootURI.Encloses(file) {
			// cope with the possibility that modules can be nested
			if candidate == nil || candidate.rootURI.Encloses(m.rootURI) {
				candidate = m
			}
		}
	}

	// even if candidate is non-nil, there may be a more specific
	// module we haven't loaded yet.
	for ; fileDir != "file:///"; fileDir = fileDir.Dir() {
		if candidate != nil && fileDir.Encloses(candidate.rootURI) {
			return candidate, nil
		}
		fh, err := w.overlayFS.ReadFile(fileDir + "/cue.mod/module.cue")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		return w.newModule(fh.URI()), nil
	}

	return nil, nil
}

// ensureModule returns the existing module for the given module root,
// or if there is none, creates and returns a new module.
func (w *Workspace) ensureModule(rootUri protocol.DocumentURI) *Module {
	m, found := w.modules[rootUri]
	if !found {
		m = w.newModule(rootUri + "/cue.mod/module.cue")
		w.modules[rootUri] = m
	}
	return m
}

// newModule creates a new module, adding it to the set of modules
// within this workspace.
func (w *Workspace) newModule(modFileUri protocol.DocumentURI) *Module {
	m := NewModule(modFileUri, w)
	w.debugLog(fmt.Sprintf("%v Created", m))
	w.modules[m.rootURI] = m
	w.invalidateActiveFilesAndDirs()
	return m
}

// reloadModules reloads all modules in the workspace. If a module
// cannot be reloaded, it is removed from the workspace.
func (w *Workspace) reloadModules() {
	modules := w.modules
	changed := false
	for _, m := range modules {
		err := m.ReloadModule()
		if err != nil {
			changed = true
			delete(modules, m.rootURI)
		}
	}
	if changed {
		w.invalidateActiveFilesAndDirs()
	}
}

// reloadPackages reloads all dirty packages across all modules. A
// package may be dirty because one of its own files has changed, or
// because a module that it imports is dirty.
//
// The goal is to reach a point where all dirty files have been loaded
// into at least one (re)loaded package.
//
// If a previously-loaded package now cannot be loaded (perhaps all
// its files have been deleted) then the package will be deleted from
// its module. If a dirty file has changed package, that new package
// will be created and loaded. Imports are followed, and may result in
// new packages and even new modules, being added to the workspace.
func (w *Workspace) reloadPackages() error {
	modules := w.modules
	modulesChanged := false

	var loadedPkgs []*modpkgload.Package
	allDirtyFiles := make(map[protocol.DocumentURI]*Module)

	// A package in one module may import packages from different
	// modules. We need to process and manage all such packages and
	// modules. Therefore, whilst we ask the modules themselves to do
	// the (re)loading of any dirty packages, we accumulate all the
	// loaded packages here, and process them all together.
	for _, m := range modules {
		pkgs, err := m.loadDirtyPackages()
		if err != nil {
			modulesChanged = true
			delete(modules, m.rootURI)
			continue
		}
		if pkgs == nil {
			// No dirty packages within the module.
			continue
		}

		// We need to track all packages that are loaded, so we use
		// pkgs.All() and not pkgs.Roots().
		loadedPkgs = append(loadedPkgs, pkgs.All()...)
		for fileUri := range m.dirtyFiles {
			allDirtyFiles[fileUri] = m
		}
	}

	// Process the results of loading the all the dirty packages from
	// all the modules. We need to do this in two passes to ensure we
	// create all necessary packages before trying to build/update the
	// inverted import graph.
	processedPkgs := make(map[ast.ImportPath]struct{})
	pkgsImportsWorklist := make(map[*Package]*modpkgload.Package)
	repeatReload := false

	loadedPkgs = slices.DeleteFunc(loadedPkgs, (*modpkgload.Package).IsStdlibPackage)
	slices.SortFunc(loadedPkgs, func(a, b *modpkgload.Package) int {
		aExternal := a.FromExternalModule()
		switch {
		case aExternal == b.FromExternalModule():
			return 0
		case aExternal:
			return 1 // externals come last
		default:
			return -1
		}
	})

	for _, loadedPkg := range loadedPkgs {
		ip := normalizeImportPath(loadedPkg)

		// The same package can appear multiple times in loadedPkgs, for
		// several reasons. Firstly, two different packages in different
		// modules could each import the same third package.
		//
		// Secondly, even within the same module, we can get multiple
		// instances of the same package. Imagine some cue file in
		// package foo.com/x has "import foo.com/y" in it. Imagine that
		// we knew that both packages x and y are dirty, so when we
		// called modpkgload.LoadPackages, we had pkgPaths set to
		// [foo.com/x@v0, foo.com/y@v0]. Because modpkgload.LoadPackages
		// does not normalize import paths, we end up with two loadings
		// of y - one from the explicit pkgPath foo.com/y@v0, and one
		// from the import foo.com/y (the different spelling is the
		// critical thing).
		//
		// So we need to test whether we've already seen this package in
		// the results of this load. If we have, we can skip.
		if _, seen := processedPkgs[ip]; seen {
			continue
		}
		processedPkgs[ip] = struct{}{}

		modRoot := loadedPkg.ModRoot()
		modFS, ok := modRoot.FS.(module.OSRootFS)
		if !ok {
			panic(fmt.Sprintf("%v Unable to load module because fs is not an OSRootFS %v", loadedPkg.Mod().Path(), modRoot.FS))
		}
		modRootPath := filepath.Join(modFS.OSRoot(), filepath.FromSlash(modRoot.Dir))
		modRootURI := protocol.URIFromPath(modRootPath)

		m := w.ensureModule(modRootURI)
		if err := m.ReloadModule(); err != nil {
			modulesChanged = true
			delete(modules, modRootURI)
			continue
		}

		if loadedPkg.Error() != nil {
			// It could be that the last file within this package was
			// deleted, so attempting to load it will create an error. So
			// the correct thing to do now is just remove any record of
			// the pkg.
			//
			// TODO: if packages contains ip, then we should probably
			// look at its importedBy field as that would tell us about
			// packages that now have dangling imports.
			delete(m.packages, ip)
			delete(w.packages, ip)
			continue
		}

		pkg, found := m.packages[ip]
		if !found {
			// Every package contains cue sources from one "leaf"
			// directory and optionally any ancestor directory. Here we
			// determine that "leaf" directory:
			dirUri := protocol.DocumentURI("")
			for _, loc := range loadedPkg.Locations() {
				uri := protocol.DocumentURI(string(modRootURI) + "/" + loc.Dir)
				if dirUri == "" || dirUri.Encloses(uri) {
					dirUri = uri
				}
			}
			pkg = NewPackage(m, ip, dirUri)
			m.packages[ip] = pkg
			w.packages[ip] = pkg
		}
		// Capture the old loadedPkg (if it exists) so we can correct
		// the import graph later.
		pkgsImportsWorklist[pkg] = pkg.pkg
		pkg.pkg = loadedPkg
		w.debugLog(fmt.Sprintf("%v Loaded %v", m, pkg))

		if loadedPkg.FromExternalModule() {
			// We process all the non-external packages first, and we
			// don't process the same package (by ImportPath) twice. So
			// if we're here, we know that this is the first occurrence
			// of this ip in loadedPkgs, and that this package was not
			// directly loaded, because we can only have reached it via
			// the imports of some other package. This means it's not
			// necessarily dirty. If this is the first time we've created
			// a [Package] for it then it'll be dirty, and we must repeat
			// the (re)load because its files could be in the module
			// cache, and that code parses the package declaration and
			// imports only, so we can't trust the ASTs we would find via
			// loadedPkg.Files()[0].Syntax, thus we need to directly load
			// it.
			if pkg.status != splendid {
				repeatReload = true
			}
		} else {
			pkg.setStatus(splendid)

			if len(allDirtyFiles) != 0 {
				for _, file := range loadedPkg.Files() {
					fileUri := protocol.DocumentURI(string(modRootURI) + "/" + file.FilePath)
					m, found := allDirtyFiles[fileUri]
					if found {
						delete(allDirtyFiles, fileUri)
						delete(m.dirtyFiles, fileUri)
						if len(allDirtyFiles) == 0 {
							break
						}
					}
				}
			}
		}
	}

	if modulesChanged {
		w.invalidateActiveFilesAndDirs()
	}

	// 2nd pass: create/correct inverted import graph now that all
	// necessary Packages exist. pkgsImportsWorklist will only contain
	// local packages (i.e. packages within this module)
	imports := make(map[ast.ImportPath]struct{})
	for pkg, oldPkg := range pkgsImportsWorklist {
		clear(imports)
		if oldPkg != nil {
			for _, i := range oldPkg.Imports() {
				imports[normalizeImportPath(i)] = struct{}{}
			}
		}
		for _, i := range pkg.pkg.Imports() {
			ip := normalizeImportPath(i)
			if _, found := imports[ip]; found {
				// Both new and old pkgs import ip. Noop.
				delete(imports, ip)
			} else if importedPkg, found := w.packages[ip]; found {
				// Only new pkg imports ip. Add the back-pointer.
				importedPkg.EnsureImportedBy(pkg)
			}
		}
		for ip := range imports {
			if importedPkg, found := w.packages[ip]; found {
				// Only old pkg imports ip. Remove the back-pointer.
				importedPkg.RemoveImportedBy(pkg)
			}
		}
	}
	// Note that there's a potential memory leak here: we might load a
	// package "foo" because it's imported by "bar". If "bar" is edited
	// so that it no longer imports "foo" then we'll notice that, and
	// our "foo" Package will get updated so that its importedBy field
	// is empty. But we never currently remove the "foo" package from
	// its module. Ideally, we should keep track within each Package of
	// the number of its files open in the editor/client. If that drops
	// to zero, and the importedBy field is empty, then we should
	// remove the package from the module. TODO.

	// We need to watch out for when a dirty file moves package, either
	// to an existing package which we've not reloaded, or to a package
	// that we've never loaded. In both cases, the file will still be
	// within this module.
	for fileUri, m := range allDirtyFiles {
		pkgs, err := m.FindPackagesOrModulesForFile(fileUri)
		if err != nil {
			if _, ok := err.(cueerrors.Error); ok {
				// Most likely a syntax error; ignore it.
				// TODO: this error might become a "diagnostics" message
				continue
			}
			return err
		}
		if len(pkgs) != 0 {
			for _, pkg := range pkgs {
				pkg.MarkFileDirty(fileUri)
			}
			repeatReload = true
		}
		// if len(pkgs) == 0 and no error, then it means the file had no
		// package declaration. For the time being, we're ignoring
		// that. TODO something better
	}

	if repeatReload {
		return w.reloadPackages()
	}
	return nil
}

func changedText(uri protocol.DocumentURI, content []byte, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: no content changes provided", jsonrpc2.ErrInternal)
	}

	// Check if the client sent the full content of the file.
	// We accept a full content change even if the server expected incremental changes.
	if len(changes) == 1 && changes[0].Range == nil && changes[0].RangeLength == 0 {
		return []byte(changes[0].Text), nil
	}
	return applyIncrementalChanges(uri, content, changes)
}

func applyIncrementalChanges(uri protocol.DocumentURI, content []byte, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	for _, change := range changes {
		// TODO(adonovan): refactor to use diff.Apply, which is robust w.r.t.
		// out-of-order or overlapping changes---and much more efficient.

		// Make sure to update mapper along with the content.
		m := protocol.NewMapper(uri, content)
		if change.Range == nil {
			return nil, fmt.Errorf("%w: unexpected nil range for change", jsonrpc2.ErrInternal)
		}
		start, end, err := m.RangeOffsets(*change.Range)
		if err != nil {
			return nil, err
		}
		if end < start {
			return nil, fmt.Errorf("%w: invalid range for content change", jsonrpc2.ErrInternal)
		}
		var buf bytes.Buffer
		buf.Write(content[:start])
		buf.WriteString(change.Text)
		buf.Write(content[end:])
		content = buf.Bytes()
	}
	return content, nil
}
