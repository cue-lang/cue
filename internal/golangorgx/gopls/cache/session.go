// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/cache/typerefs"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/util/bug"
	"cuelang.org/go/internal/golangorgx/gopls/util/persistent"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/memoize"
	"cuelang.org/go/internal/golangorgx/tools/xcontext"
)

// NewSession creates a new gopls session with the given cache.
func NewSession(ctx context.Context, c *Cache) *Session {
	index := atomic.AddInt64(&sessionIndex, 1)
	s := &Session{
		id:         strconv.FormatInt(index, 10),
		cache:      c,
		overlayFS:  newOverlayFS(c),
		parseCache: newParseCache(1 * time.Minute), // keep recently parsed files for a minute, to optimize typing CPU
		viewMap:    make(map[protocol.DocumentURI]*View),
	}
	event.Log(ctx, "New session", KeyCreateSession.Of(s))
	return s
}

// A Session holds the state (views, file contents, parse cache,
// memoized computations) of a gopls server process.
//
// It implements the file.Source interface.
type Session struct {
	// Unique identifier for this session.
	id string

	// Immutable attributes shared across views.
	cache *Cache // shared cache

	viewMu  sync.Mutex
	views   []*View
	viewMap map[protocol.DocumentURI]*View // file->best view; nil after shutdown

	// snapshots is a counting semaphore that records the number
	// of unreleased snapshots associated with this session.
	// Shutdown waits for it to fall to zero.
	snapshotWG sync.WaitGroup

	parseCache *parseCache

	*overlayFS
}

// ID returns the unique identifier for this session on this server.
func (s *Session) ID() string     { return s.id }
func (s *Session) String() string { return s.id }

// Shutdown the session and all views it has created.
func (s *Session) Shutdown(ctx context.Context) {
	var views []*View
	s.viewMu.Lock()
	views = append(views, s.views...)
	s.views = nil
	s.viewMap = nil
	s.viewMu.Unlock()
	for _, view := range views {
		view.shutdown()
	}
	s.parseCache.stop()
	s.snapshotWG.Wait() // wait for all work on associated snapshots to finish
	event.Log(ctx, "Shutdown session", KeyShutdownSession.Of(s))
}

// Cache returns the cache that created this session, for debugging only.
func (s *Session) Cache() *Cache {
	return s.cache
}

// NewView creates a new View, returning it and its first snapshot. If a
// non-empty tempWorkspace directory is provided, the View will record a copy
// of its gopls workspace module in that directory, so that client tooling
// can execute in the same main module.  On success it also returns a release
// function that must be called when the Snapshot is no longer needed.
func (s *Session) NewView(ctx context.Context, folder *Folder) (*View, *Snapshot, func(), error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()

	// TODO(myitcv): when we shift to support multiple WorkspaceFolders, we
	// might need to introduce logic here that determines if we have an existing
	// view for a WorkspaceFolder we are adding.

	def, err := defineView(ctx, s, folder, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	view, snapshot, release := s.createView(ctx, def)
	s.views = append(s.views, view)
	// we always need to drop the view map
	s.viewMap = make(map[protocol.DocumentURI]*View)
	return view, snapshot, release, nil
}

// createView creates a new view, with an initial snapshot that retains the
// supplied context, detached from events and cancelation.
//
// The caller is responsible for calling the release function once.
func (s *Session) createView(ctx context.Context, def *viewDefinition) (*View, *Snapshot, func()) {
	index := atomic.AddInt64(&viewIndex, 1)

	// We want a true background context and not a detached context here
	// the spans need to be unrelated and no tag values should pollute it.
	baseCtx := event.Detach(xcontext.Detach(ctx))
	backgroundCtx, cancel := context.WithCancel(baseCtx)

	v := &View{
		id:                   strconv.FormatInt(index, 10),
		initialWorkspaceLoad: make(chan struct{}),
		initializationSema:   make(chan struct{}, 1),
		baseCtx:              baseCtx,
		parseCache:           s.parseCache,
		fs:                   s.overlayFS,
		viewDefinition:       def,
	}

	s.snapshotWG.Add(1)
	v.snapshot = &Snapshot{
		view:             v,
		backgroundCtx:    backgroundCtx,
		cancel:           cancel,
		store:            s.cache.store,
		refcount:         1, // Snapshots are born referenced.
		done:             s.snapshotWG.Done,
		packages:         new(persistent.Map[PackageID, *packageHandle]),
		meta:             new(metadata.Graph),
		files:            newFileMap(),
		activePackages:   new(persistent.Map[PackageID, *Package]),
		symbolizeHandles: new(persistent.Map[protocol.DocumentURI, *memoize.Promise]),
		shouldLoad:       new(persistent.Map[PackageID, []PackagePath]),
		unloadableFiles:  new(persistent.Set[protocol.DocumentURI]),
		pkgIndex:         typerefs.NewPackageIndex(),
	}

	// Snapshots must observe all open files, as there are some caching
	// heuristics that change behavior depending on open files.
	for _, o := range s.overlayFS.Overlays() {
		_, _ = v.snapshot.ReadFile(ctx, o.URI())
	}

	// Record the environment of the newly created view in the log.
	event.Log(ctx, viewEnv(v))

	// Initialize the view without blocking.
	initCtx, initCancel := context.WithCancel(xcontext.Detach(ctx))
	v.cancelInitialWorkspaceLoad = initCancel
	snapshot := v.snapshot

	// Pass a second reference to the background goroutine.
	bgRelease := snapshot.Acquire()
	go func() {
		defer bgRelease()
		snapshot.initialize(initCtx, true)
	}()

	// Return a third reference to the caller.
	return v, snapshot, snapshot.Acquire()
}

// RemoveView removes from the session the view rooted at the specified directory.
// It reports whether a view of that directory was removed.
func (s *Session) RemoveView(dir protocol.DocumentURI) bool {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	for _, view := range s.views {
		if view.folder.Dir == dir {
			i := s.dropView(view)
			if i == -1 {
				return false // can't happen
			}
			// delete this view... we don't care about order but we do want to make
			// sure we can garbage collect the view
			s.views = removeElement(s.views, i)
			return true
		}
	}
	return false
}

// View returns the view with a matching id, if present.
func (s *Session) View(id string) (*View, error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	for _, view := range s.views {
		if view.ID() == id {
			return view, nil
		}
	}
	return nil, fmt.Errorf("no view with ID %q", id)
}

// SnapshotOf returns a Snapshot corresponding to the given URI.
//
// In the case where the file can be  can be associated with a View by
// bestViewForURI (based on directory information alone, without package
// metadata), SnapshotOf returns the current Snapshot for that View. Otherwise,
// it awaits loading package metadata and returns a Snapshot for the first View
// containing a real (=not command-line-arguments) package for the file.
//
// If that also fails to find a View, SnapshotOf returns a Snapshot for the
// first view in s.views that is not shut down (i.e. s.views[0] unless we lose
// a race), for determinism in tests and so that we tend to aggregate the
// resulting command-line-arguments packages into a single view.
//
// SnapshotOf returns an error if a failure occurs along the way (most likely due
// to context cancellation), or if there are no Views in the Session.
//
// On success, the caller must call the returned function to release the snapshot.
func (s *Session) SnapshotOf(ctx context.Context, uri protocol.DocumentURI) (*Snapshot, func(), error) {
	// Fast path: if the uri has a static association with a view, return it.
	s.viewMu.Lock()
	v, err := s.viewOfLocked(ctx, uri)
	s.viewMu.Unlock()

	if err != nil {
		return nil, nil, err
	}

	if v != nil {
		snapshot, release, err := v.Snapshot()
		if err == nil {
			return snapshot, release, nil
		}
		// View is shut down. Forget this association.
		s.viewMu.Lock()
		if s.viewMap[uri] == v {
			delete(s.viewMap, uri)
		}
		s.viewMu.Unlock()
	}

	// Fall-back: none of the views could be associated with uri based on
	// directory information alone.
	//
	// Don't memoize the view association in viewMap, as it is not static: Views
	// may change as metadata changes.
	//
	// TODO(rfindley): we could perhaps optimize this case by peeking at existing
	// metadata before awaiting the load (after all, a load only adds metadata).
	// But that seems potentially tricky, when in the common case no loading
	// should be required.
	views := s.Views()

	for _, v := range views {
		snapshot, release, err := v.Snapshot()
		if err == nil {
			return snapshot, release, nil // first valid snapshot
		}
	}
	return nil, nil, errNoViews
}

// errNoViews is sought by orphaned file diagnostics, to detect the case where
// we have no view containing a file.
var errNoViews = errors.New("no views")

// viewOfLocked wraps bestViewForURI, memoizing its result.
//
// Precondition: caller holds s.viewMu lock.
//
// May return (nil, nil).
func (s *Session) viewOfLocked(ctx context.Context, uri protocol.DocumentURI) (*View, error) {
	v, hit := s.viewMap[uri]
	if !hit {
		// Cache miss: compute (and memoize) the best view.
		fh, err := s.ReadFile(ctx, uri)
		if err != nil {
			return nil, err
		}
		v, err = bestView(ctx, s, fh, s.views)
		if err != nil {
			return nil, err
		}
		if s.viewMap == nil {
			return nil, errors.New("session is shut down")
		}
		s.viewMap[uri] = v
	}
	return v, nil
}

func (s *Session) Views() []*View {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	result := make([]*View, len(s.views))
	copy(result, s.views)
	return result
}

// selectViewDefs constructs the best set of views covering the provided workspace
// folders and open files.
//
// This implements the zero-config algorithm of golang/go#57979.
func selectViewDefs(ctx context.Context, fs file.Source, folders []*Folder, openFiles []protocol.DocumentURI) ([]*viewDefinition, error) {
	var defs []*viewDefinition

	// First, compute a default view for each workspace folder.
	// TODO(golang/go#57979): technically, this is path dependent, since
	// DidChangeWorkspaceFolders could introduce a path-dependent ordering on
	// folders. We should keep folders sorted, or sort them here.
	for _, folder := range folders {
		def, err := defineView(ctx, fs, folder, nil)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}

	// Next, ensure that the set of views covers all open files contained in a
	// workspace folder.
	//
	// We only do this for files contained in a workspace folder, because other
	// open files are most likely the result of jumping to a definition from a
	// workspace file; we don't want to create additional views in those cases:
	// they should be resolved after initialization.

	folderForFile := func(uri protocol.DocumentURI) *Folder {
		var longest *Folder
		for _, folder := range folders {
			if (longest == nil || len(folder.Dir) > len(longest.Dir)) && folder.Dir.Encloses(uri) {
				longest = folder
			}
		}
		return longest
	}

checkFiles:
	for _, uri := range openFiles {
		folder := folderForFile(uri)
		if folder == nil || !folder.Options.ZeroConfig {
			continue // only guess views for open files
		}
		fh, err := fs.ReadFile(ctx, uri)
		if err != nil {
			return nil, err
		}
		def, err := bestView(ctx, fs, fh, defs)
		if err != nil {
			// We should never call selectViewDefs with a cancellable context, so
			// this should never fail.
			return nil, bug.Errorf("failed to find best view for open file: %v", err)
		}
		if def != nil {
			continue // file covered by an existing view
		}
		def, err = defineView(ctx, fs, folder, fh)
		if err != nil {
			// We should never call selectViewDefs with a cancellable context, so
			// this should never fail.
			return nil, bug.Errorf("failed to define view for open file: %v", err)
		}
		// It need not strictly be the case that the best view for a file is
		// distinct from other views, as the logic of getViewDefinition and
		// bestViewForURI does not align perfectly. This is not necessarily a bug:
		// there may be files for which we can't construct a valid view.
		//
		// Nevertheless, we should not create redundant views.
		for _, alt := range defs {
			if viewDefinitionsEqual(alt, def) {
				continue checkFiles
			}
		}
		defs = append(defs, def)
	}

	return defs, nil
}

// The viewDefiner interface allows the bestView algorithm to operate on both
// Views and viewDefinitions.
type viewDefiner interface{ definition() *viewDefinition }

// bestView returns the best View or viewDefinition that contains the
// given file, or (nil, nil) if no matching view is found.
//
// bestView only returns an error in the event of context cancellation.
//
// Making this function generic is convenient so that we can avoid mapping view
// definitions back to views inside Session.DidModifyFiles, where performance
// matters. It is, however, not the cleanest application of generics.
//
// Note: keep this function in sync with defineView.
func bestView[V viewDefiner](ctx context.Context, fs file.Source, fh file.Handle, views []V) (V, error) {
	var zero V

	if len(views) == 0 {
		return zero, nil // avoid the call to findRootPattern
	}
	uri := fh.URI()
	dir := uri.Dir()
	modURI, err := findRootPattern(ctx, dir, "cue.mod/module.cue", fs)
	if err != nil {
		return zero, err
	}

	// Prefer GoWork > GoMod > GOPATH > GoPackages > AdHoc.
	var (
		goPackagesViews []V // prefer longest
		workViews       []V // prefer longest
		modViews        []V // exact match
		gopathViews     []V // prefer longest
		adHocViews      []V // exact match
	)

	for _, view := range views {
		switch def := view.definition(); def.Type() {
		case CUEModView:
			if _, ok := def.workspaceModFiles[modURI]; ok {
				modViews = append(modViews, view)
			}
		case AdHocView:
			if def.root == dir {
				adHocViews = append(adHocViews, view)
			}
		}
	}

	// Now that we've collected matching views, choose the best match,
	// considering ports.
	//
	// We only consider one type of view, since the matching view created by
	// defineView should be of the best type.
	var bestViews []V
	switch {
	case len(workViews) > 0:
		bestViews = workViews
	case len(modViews) > 0:
		bestViews = modViews
	case len(gopathViews) > 0:
		bestViews = gopathViews
	case len(goPackagesViews) > 0:
		bestViews = goPackagesViews
	case len(adHocViews) > 0:
		bestViews = adHocViews
	default:
		return zero, nil
	}

	// TODO: we need to fix this
	return bestViews[0], nil
}

// updateViewLocked recreates the view with the given options.
//
// If the resulting error is non-nil, the view may or may not have already been
// dropped from the session.
func (s *Session) updateViewLocked(ctx context.Context, view *View, def *viewDefinition) (*View, error) {
	i := s.dropView(view)
	if i == -1 {
		return nil, fmt.Errorf("view %q not found", view.id)
	}

	view, _, release := s.createView(ctx, def)
	defer release()

	// substitute the new view into the array where the old view was
	s.views[i] = view
	s.viewMap = make(map[protocol.DocumentURI]*View)
	return view, nil
}

// removeElement removes the ith element from the slice replacing it with the last element.
// TODO(adonovan): generics, someday.
func removeElement(slice []*View, index int) []*View {
	last := len(slice) - 1
	slice[index] = slice[last]
	slice[last] = nil // aid GC
	return slice[:last]
}

// dropView removes v from the set of views for the receiver s and calls
// v.shutdown, returning the index of v in s.views (if found), or -1 if v was
// not found. s.viewMu must be held while calling this function.
func (s *Session) dropView(v *View) int {
	// we always need to drop the view map
	s.viewMap = make(map[protocol.DocumentURI]*View)
	for i := range s.views {
		if v == s.views[i] {
			// we found the view, drop it and return the index it was found at
			s.views[i] = nil
			v.shutdown()
			return i
		}
	}
	// TODO(rfindley): it looks wrong that we don't shutdown v in this codepath.
	// We should never get here.
	bug.Reportf("tried to drop nonexistent view %q", v.id)
	return -1
}

// ResetView resets the best view for the given URI.
func (s *Session) ResetView(ctx context.Context, uri protocol.DocumentURI) (*View, error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	v, err := s.viewOfLocked(ctx, uri)
	if err != nil {
		return nil, err
	}
	return s.updateViewLocked(ctx, v, v.viewDefinition)
}

// DidModifyFiles reports a file modification to the session. It returns
// the new snapshots after the modifications have been applied, paired with
// the affected file URIs for those snapshots.
// On success, it returns a release function that
// must be called when the snapshots are no longer needed.
//
// TODO(rfindley): what happens if this function fails? It must leave us in a
// broken state, which we should surface to the user, probably as a request to
// restart gopls.
func (s *Session) DidModifyFiles(ctx context.Context, modifications []file.Modification) (map[*View][]protocol.DocumentURI, error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()

	// Update overlays.
	//
	// This is done while holding viewMu because the set of open files affects
	// the set of views, and to prevent views from seeing updated file content
	// before they have processed invalidations.
	_, err := s.updateOverlays(ctx, modifications)
	if err != nil {
		return nil, err
	}

	// checkViews controls whether the set of views needs to be recomputed, for
	// example because a go.mod file was created or deleted, or a go.work file
	// changed on disk.
	checkViews := false

	changed := make(map[protocol.DocumentURI]file.Handle)
	for _, c := range modifications {
		fh := mustReadFile(ctx, s, c.URI)
		changed[c.URI] = fh
	}

	// We only want to run fast-path diagnostics (i.e. diagnoseChangedFiles) once
	// for each changed file, in its best view.
	viewsToDiagnose := map[*View][]protocol.DocumentURI{}
	for _, mod := range modifications {
		v, err := s.viewOfLocked(ctx, mod.URI)
		if err != nil {
			// bestViewForURI only returns an error in the event of context
			// cancellation. Since state changes should occur on an uncancellable
			// context, an error here is a bug.
			bug.Reportf("finding best view for change: %v", err)
			continue
		}
		if v != nil {
			viewsToDiagnose[v] = append(viewsToDiagnose[v], mod.URI)
		}
	}

	// ...but changes may be relevant to other views, for example if they are
	// changes to a shared package.
	for _, v := range s.views {
		_, release, needsDiagnosis := s.invalidateViewLocked(ctx, v, StateChange{Modifications: modifications, Files: changed})
		release()

		if needsDiagnosis || checkViews {
			if _, ok := viewsToDiagnose[v]; !ok {
				viewsToDiagnose[v] = nil
			}
		}
	}

	return viewsToDiagnose, nil
}

// ExpandModificationsToDirectories returns the set of changes with the
// directory changes removed and expanded to include all of the files in
// the directory.
func (s *Session) ExpandModificationsToDirectories(ctx context.Context, changes []file.Modification) []file.Modification {
	var snapshots []*Snapshot
	s.viewMu.Lock()
	for _, v := range s.views {
		snapshot, release, err := v.Snapshot()
		if err != nil {
			continue // view is shut down; continue with others
		}
		defer release()
		snapshots = append(snapshots, snapshot)
	}
	s.viewMu.Unlock()

	// Expand the modification to any file we could care about, which we define
	// to be any file observed by any of the snapshots.
	//
	// There may be other files in the directory, but if we haven't read them yet
	// we don't need to invalidate them.
	var result []file.Modification
	for _, c := range changes {
		expanded := make(map[protocol.DocumentURI]bool)
		for _, snapshot := range snapshots {
			for _, uri := range snapshot.filesInDir(c.URI) {
				expanded[uri] = true
			}
		}
		if len(expanded) == 0 {
			result = append(result, c)
		} else {
			for uri := range expanded {
				result = append(result, file.Modification{
					URI:        uri,
					Action:     c.Action,
					LanguageID: "",
					OnDisk:     c.OnDisk,
					// changes to directories cannot include text or versions
				})
			}
		}
	}
	return result
}

// updateOverlays updates the set of overlays and returns a map of any existing
// overlay values that were replaced.
//
// Precondition: caller holds s.viewMu lock.
// TODO(rfindley): move this to fs_overlay.go.
func (fs *overlayFS) updateOverlays(ctx context.Context, changes []file.Modification) (map[protocol.DocumentURI]*overlay, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	replaced := make(map[protocol.DocumentURI]*overlay)
	for _, c := range changes {
		o, ok := fs.overlays[c.URI]
		if ok {
			replaced[c.URI] = o
		}

		// If the file is not opened in an overlay and the change is on disk,
		// there's no need to update an overlay. If there is an overlay, we
		// may need to update the overlay's saved value.
		if !ok && c.OnDisk {
			continue
		}

		// Determine the file kind on open, otherwise, assume it has been cached.
		var kind file.Kind
		switch c.Action {
		case file.Open:
			kind = file.KindForLang(c.LanguageID)
		default:
			if !ok {
				return nil, fmt.Errorf("updateOverlays: modifying unopened overlay %v", c.URI)
			}
			kind = o.kind
		}

		// Closing a file just deletes its overlay.
		if c.Action == file.Close {
			delete(fs.overlays, c.URI)
			continue
		}

		// If the file is on disk, check if its content is the same as in the
		// overlay. Saves and on-disk file changes don't come with the file's
		// content.
		text := c.Text
		if text == nil && (c.Action == file.Save || c.OnDisk) {
			if !ok {
				return nil, fmt.Errorf("no known content for overlay for %s", c.Action)
			}
			text = o.content
		}
		// On-disk changes don't come with versions.
		version := c.Version
		if c.OnDisk || c.Action == file.Save {
			version = o.version
		}
		hash := file.HashOf(text)
		var sameContentOnDisk bool
		switch c.Action {
		case file.Delete:
			// Do nothing. sameContentOnDisk should be false.
		case file.Save:
			// Make sure the version and content (if present) is the same.
			if false && o.version != version { // Client no longer sends the version
				return nil, fmt.Errorf("updateOverlays: saving %s at version %v, currently at %v", c.URI, c.Version, o.version)
			}
			if c.Text != nil && o.hash != hash {
				return nil, fmt.Errorf("updateOverlays: overlay %s changed on save", c.URI)
			}
			sameContentOnDisk = true
		default:
			fh := mustReadFile(ctx, fs.delegate, c.URI)
			_, readErr := fh.Content()
			sameContentOnDisk = (readErr == nil && fh.Identity().Hash == hash)
		}
		o = &overlay{
			uri:     c.URI,
			version: version,
			content: text,
			kind:    kind,
			hash:    hash,
			saved:   sameContentOnDisk,
		}

		// NOTE: previous versions of this code checked here that the overlay had a
		// view and file kind (but we don't know why).

		fs.overlays[c.URI] = o
	}

	return replaced, nil
}

func mustReadFile(ctx context.Context, fs file.Source, uri protocol.DocumentURI) file.Handle {
	ctx = xcontext.Detach(ctx)
	fh, err := fs.ReadFile(ctx, uri)
	if err != nil {
		// ReadFile cannot fail with an uncancellable context.
		bug.Reportf("reading file failed unexpectedly: %v", err)
		return brokenFile{uri, err}
	}
	return fh
}

// A brokenFile represents an unexpected failure to read a file.
type brokenFile struct {
	uri protocol.DocumentURI
	err error
}

func (b brokenFile) URI() protocol.DocumentURI { return b.uri }
func (b brokenFile) Identity() file.Identity   { return file.Identity{URI: b.uri} }
func (b brokenFile) SameContentsOnDisk() bool  { return false }
func (b brokenFile) Version() int32            { return 0 }
func (b brokenFile) Content() ([]byte, error)  { return nil, b.err }

// FileWatchingGlobPatterns returns a set of glob patterns that the client is
// required to watch for changes, and notify the server of them, in order to
// keep the server's state up to date.
//
// This set includes
//  1. all go.mod and go.work files in the workspace; and
//  2. for each Snapshot, its modules (or directory for ad-hoc views). In
//     module mode, this is the set of active modules (and for VS Code, all
//     workspace directories within them, due to golang/go#42348).
//
// The watch for workspace go.work and go.mod files in (1) is sufficient to
// capture changes to the repo structure that may affect the set of views.
// Whenever this set changes, we reload the workspace and invalidate memoized
// files.
//
// The watch for workspace directories in (2) should keep each View up to date,
// as it should capture any newly added/modified/deleted Go files.
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
//
// TODO(golang/go#57979): we need to reset the memoizedFS when a view changes.
// Consider the case where we incidentally read a file, then it moved outside
// of an active module, and subsequently changed: we would still observe the
// original file state.
func (s *Session) FileWatchingGlobPatterns(ctx context.Context) map[protocol.RelativePattern]unit {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()

	// Always watch files that may change the set of views.
	patterns := map[protocol.RelativePattern]unit{
		{Pattern: "**/*.{mod,work}"}: {},
	}

	for _, view := range s.views {
		snapshot, release, err := view.Snapshot()
		if err != nil {
			continue // view is shut down; continue with others
		}
		for k, v := range snapshot.fileWatchingGlobPatterns() {
			patterns[k] = v
		}
		release()
	}
	return patterns
}

// OrphanedFileDiagnostics reports diagnostics describing why open files have
// no packages or have only command-line-arguments packages.
//
// If the resulting diagnostic is nil, the file is either not orphaned or we
// can't produce a good diagnostic.
//
// The caller must not mutate the result.
func (s *Session) OrphanedFileDiagnostics(ctx context.Context) (map[protocol.DocumentURI][]*Diagnostic, error) {
	// Note: diagnostics holds a slice for consistency with other diagnostic
	// funcs.
	diagnostics := make(map[protocol.DocumentURI][]*Diagnostic)

	byView := make(map[*View][]*overlay)
	for _, o := range s.Overlays() {
		uri := o.URI()
		snapshot, release, err := s.SnapshotOf(ctx, uri)
		if err != nil {
			// TODO(golang/go#57979): we have to use the .go suffix as an approximation for
			// file kind here, because we don't have access to Options if no View was
			// matched.
			//
			// But Options are really a property of Folder, not View, and we could
			// match a folder here.
			//
			// Refactor so that Folders are tracked independently of Views, and use
			// the correct options here to get the most accurate file kind.
			//
			// TODO(golang/go#57979): once we switch entirely to the zeroconfig
			// logic, we should use this diagnostic for the fallback case of
			// s.views[0] in the ViewOf logic.
			if errors.Is(err, errNoViews) {
				if strings.HasSuffix(string(uri), ".go") {
					if _, rng, ok := orphanedFileDiagnosticRange(ctx, s.parseCache, o); ok {
						diagnostics[uri] = []*Diagnostic{{
							URI:      uri,
							Range:    rng,
							Severity: protocol.SeverityWarning,
							Source:   ListError,
							Message:  fmt.Sprintf("No active builds contain %s: consider opening a new workspace folder containing it", uri.Path()),
						}}
					}
				}
				continue
			}
			return nil, err
		}
		v := snapshot.View()
		release()
		byView[v] = append(byView[v], o)
	}

	return diagnostics, nil
}
