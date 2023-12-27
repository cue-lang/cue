// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cache implements the caching layer for gopls.
package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/util/maps"
	"cuelang.org/go/internal/golangorgx/gopls/util/pathutil"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/gocommand"
	"cuelang.org/go/internal/golangorgx/tools/imports"
	"cuelang.org/go/internal/golangorgx/tools/xcontext"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// A Folder represents an LSP workspace folder, together with its per-folder
// options and environment variables that affect build configuration.
//
// Folders (Name and Dir) are specified by the 'initialize' and subsequent
// 'didChangeWorkspaceFolders' requests; their options come from
// didChangeConfiguration.
//
// Folders must not be mutated, as they may be shared across multiple views.
type Folder struct {
	Dir     protocol.DocumentURI
	Name    string // decorative name for UI; not necessarily unique
	Options *settings.Options
	Env     *GoEnv
}

// GoEnv holds the environment variables and data from the Go command that is
// required for operating on a workspace folder.
type GoEnv struct {
	// Go environment variables. These correspond directly with the Go env var of
	// the same name.
	GOCACHE     string
	GOMODCACHE  string
	GOPATH      string
	GOPRIVATE   string
	GOFLAGS     string
	GO111MODULE string

	// Go version output.
	GoVersion       int    // The X in Go 1.X
	GoVersionOutput string // complete go version output

	// OS environment variables (notably not go env).
	GOWORK           string
	GOPACKAGESDRIVER string
}

// View represents a single build for a workspace.
//
// A View is a logical build (the viewDefinition) along with a state of that
// build (the Snapshot).
type View struct {
	id string // a unique string to identify this View in (e.g.) serialized Commands

	*viewDefinition // build configuration

	gocmdRunner *gocommand.Runner // limits go command concurrency

	// baseCtx is the context handed to NewView. This is the parent of all
	// background contexts created for this view.
	baseCtx context.Context

	importsState *importsState

	// parseCache holds an LRU cache of recently parsed files.
	parseCache *parseCache

	// fs is the file source used to populate this view.
	fs *overlayFS

	// ignoreFilter is used for fast checking of ignored files.
	ignoreFilter *ignoreFilter

	// initCancelFirstAttempt can be used to terminate the view's first
	// attempt at initialization.
	initCancelFirstAttempt context.CancelFunc

	// Track the latest snapshot via the snapshot field, guarded by snapshotMu.
	//
	// Invariant: whenever the snapshot field is overwritten, destroy(snapshot)
	// is called on the previous (overwritten) snapshot while snapshotMu is held,
	// incrementing snapshotWG. During shutdown the final snapshot is
	// overwritten with nil and destroyed, guaranteeing that all observed
	// snapshots have been destroyed via the destroy method, and snapshotWG may
	// be waited upon to let these destroy operations complete.
	snapshotMu sync.Mutex
	snapshot   *Snapshot      // latest snapshot; nil after shutdown has been called
	snapshotWG sync.WaitGroup // refcount for pending destroy operations

	// initialWorkspaceLoad is closed when the first workspace initialization has
	// completed. If we failed to load, we only retry if the go.mod file changes,
	// to avoid too many go/packages calls.
	initialWorkspaceLoad chan struct{}

	// initializationSema is used limit concurrent initialization of snapshots in
	// the view. We use a channel instead of a mutex to avoid blocking when a
	// context is canceled.
	//
	// This field (along with snapshot.initialized) guards against duplicate
	// initialization of snapshots. Do not change it without adjusting snapshot
	// accordingly.
	initializationSema chan struct{}
}

// A viewDefinition is a logical build, i.e. configuration (Folder) along with
// a build directory and possibly an environment overlay (e.g. GOWORK=off or
// GOOS, GOARCH=...) to affect the build.
//
// This type is immutable, and compared to see if the View needs to be
// reconstructed.
//
// Note: whenever modifying this type, also modify the equivalence relation
// implemented by viewDefinitionsEqual.
//
// TODO(golang/go#57979): viewDefinition should be sufficient for running
// go/packages. Enforce this in the API.
type viewDefinition struct {
	folder *Folder // pointer comparison is OK, as any new Folder creates a new def

	typ    ViewType
	root   protocol.DocumentURI // root directory; where to run the Go command
	gomod  protocol.DocumentURI // the nearest go.mod file, or ""
	gowork protocol.DocumentURI // the nearest go.work file, or ""

	// workspaceModFiles holds the set of mod files active in this snapshot.
	//
	// This is either empty, a single entry for the workspace go.mod file, or the
	// set of mod files used by the workspace go.work file.
	//
	// TODO(rfindley): should we just run `go list -m` to compute this set?
	workspaceModFiles    map[protocol.DocumentURI]struct{}
	workspaceModFilesErr error // error encountered computing workspaceModFiles

	// envOverlay holds additional environment to apply to this viewDefinition.
	envOverlay []string
}

// Type returns the ViewType type, which determines how go/packages are loaded
// for this View.
func (d viewDefinition) Type() ViewType { return d.typ }

// Root returns the view root, which determines where packages are loaded from.
func (d viewDefinition) Root() protocol.DocumentURI { return d.root }

// GoMod returns the nearest go.mod file for this view's root, or "".
func (d viewDefinition) GoMod() protocol.DocumentURI { return d.gomod }

// GoWork returns the nearest go.work file for this view's root, or "".
func (d viewDefinition) GoWork() protocol.DocumentURI { return d.gowork }

// adjustedGO111MODULE is the value of GO111MODULE to use for loading packages.
// It is adjusted to default to "auto" rather than "on", since if we are in
// GOPATH and have no module, we may as well allow a GOPATH view to work.
func (d viewDefinition) adjustedGO111MODULE() string {
	if d.folder.Env.GO111MODULE != "" {
		return d.folder.Env.GO111MODULE
	}
	return "auto"
}

// ModFiles are the go.mod files enclosed in the snapshot's view and known
// to the snapshot.
func (d viewDefinition) ModFiles() []protocol.DocumentURI {
	var uris []protocol.DocumentURI
	for modURI := range d.workspaceModFiles {
		uris = append(uris, modURI)
	}
	return uris
}

// viewDefinitionsEqual reports whether x and y are equivalent.
func viewDefinitionsEqual(x, y *viewDefinition) bool {
	if (x.workspaceModFilesErr == nil) != (y.workspaceModFilesErr == nil) {
		return false
	}
	if x.workspaceModFilesErr != nil {
		if x.workspaceModFilesErr.Error() != y.workspaceModFilesErr.Error() {
			return false
		}
	} else if !maps.SameKeys(x.workspaceModFiles, y.workspaceModFiles) {
		return false
	}
	if len(x.envOverlay) != len(y.envOverlay) {
		return false
	}
	for i, xv := range x.envOverlay {
		if xv != y.envOverlay[i] {
			return false
		}
	}
	return x.folder == y.folder &&
		x.typ == y.typ &&
		x.root == y.root &&
		x.gomod == y.gomod &&
		x.gowork == y.gowork
}

// A ViewType describes how we load package information for a view.
//
// This is used for constructing the go/packages.Load query, and for
// interpreting missing packages, imports, or errors.
//
// See the documentation for individual ViewType values for details.
type ViewType int

const (
	// GoPackagesDriverView is a view with a non-empty GOPACKAGESDRIVER
	// environment variable.
	//
	// Load: ./... from the workspace folder.
	GoPackagesDriverView ViewType = iota

	// GOPATHView is a view in GOPATH mode.
	//
	// I.e. in GOPATH, with GO111MODULE=off, or GO111MODULE=auto with no
	// go.mod file.
	//
	// Load: ./... from the workspace folder.
	GOPATHView

	// GoModView is a view in module mode with a single Go module.
	//
	// Load: <modulePath>/... from the module root.
	GoModView

	// GoWorkView is a view in module mode with a go.work file.
	//
	// Load: <modulePath>/... from the workspace folder, for each module.
	GoWorkView

	// An AdHocView is a collection of files in a given directory, not in GOPATH
	// or a module.
	//
	// Load: . from the workspace folder.
	AdHocView
)

// moduleMode reports whether the view uses Go modules.
func (w viewDefinition) moduleMode() bool {
	switch w.typ {
	case GoModView, GoWorkView:
		return true
	default:
		return false
	}
}

func (v *View) ID() string { return v.id }

// tempModFile creates a temporary go.mod file based on the contents
// of the given go.mod file. On success, it is the caller's
// responsibility to call the cleanup function when the file is no
// longer needed.
func tempModFile(modURI protocol.DocumentURI, gomod, gosum []byte) (tmpURI protocol.DocumentURI, cleanup func(), err error) {
	filenameHash := file.HashOf([]byte(modURI.Path()))
	tmpMod, err := os.CreateTemp("", fmt.Sprintf("go.%s.*.mod", filenameHash))
	if err != nil {
		return "", nil, err
	}
	defer tmpMod.Close()

	tmpURI = protocol.URIFromPath(tmpMod.Name())
	tmpSumName := sumFilename(tmpURI)

	if _, err := tmpMod.Write(gomod); err != nil {
		return "", nil, err
	}

	// We use a distinct name here to avoid subtlety around the fact
	// that both 'return' and 'defer' update the "cleanup" variable.
	doCleanup := func() {
		_ = os.Remove(tmpSumName)
		_ = os.Remove(tmpURI.Path())
	}

	// Be careful to clean up if we return an error from this function.
	defer func() {
		if err != nil {
			doCleanup()
			cleanup = nil
		}
	}()

	// Create an analogous go.sum, if one exists.
	if gosum != nil {
		if err := os.WriteFile(tmpSumName, gosum, 0655); err != nil {
			return "", nil, err
		}
	}

	return tmpURI, doCleanup, nil
}

// Folder returns the folder at the base of this view.
func (v *View) Folder() *Folder {
	return v.folder
}

// UpdateFolders updates the set of views for the new folders.
//
// Calling this causes each view to be reinitialized.
func (s *Session) UpdateFolders(ctx context.Context, newFolders []*Folder) error {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()

	overlays := s.Overlays()
	var openFiles []protocol.DocumentURI
	for _, o := range overlays {
		openFiles = append(openFiles, o.URI())
	}

	defs, err := selectViewDefs(ctx, s, newFolders, openFiles)
	if err != nil {
		return err
	}
	var newViews []*View
	for _, def := range defs {
		v, _, release := s.createView(ctx, def)
		release()
		newViews = append(newViews, v)
	}
	for _, v := range s.views {
		v.shutdown()
	}
	s.views = newViews
	return nil
}

// viewEnv returns a string describing the environment of a newly created view.
//
// It must not be called concurrently with any other view methods.
//
// TODO(golang/go#57979): revisit this function and its uses once the dust
// settles.
func viewEnv(v *View) string {
	env := v.folder.Options.EnvSlice()
	buildFlags := append([]string{}, v.folder.Options.BuildFlags...)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `go info for %v
(go dir %s)
(go version %s)
(valid build configuration = %v)
(build flags: %v)
`,
		v.folder.Dir.Path(),
		v.root.Path(),
		strings.TrimRight(v.folder.Env.GoVersionOutput, "\n"),
		v.snapshot.validBuildConfiguration(),
		buildFlags,
	)

	for _, v := range env {
		s := strings.SplitN(v, "=", 2)
		if len(s) != 2 {
			continue
		}
	}

	return buf.String()
}

// RunProcessEnvFunc runs fn with the process env for this snapshot's view.
// Note: the process env contains cached module and filesystem state.
func (s *Snapshot) RunProcessEnvFunc(ctx context.Context, fn func(context.Context, *imports.Options) error) error {
	return s.view.importsState.runProcessEnvFunc(ctx, s, fn)
}

// separated out from its sole use in locateTemplateFiles for testability
func fileHasExtension(path string, suffixes []string) bool {
	ext := filepath.Ext(path)
	if ext != "" && ext[0] == '.' {
		ext = ext[1:]
	}
	for _, s := range suffixes {
		if s != "" && ext == s {
			return true
		}
	}
	return false
}

// locateTemplateFiles ensures that the snapshot has mapped template files
// within the workspace folder.
func (s *Snapshot) locateTemplateFiles(ctx context.Context) {
	suffixes := s.Options().TemplateExtensions
	if len(suffixes) == 0 {
		return
	}

	searched := 0
	filterFunc := s.view.filterFunc()
	err := filepath.WalkDir(s.view.folder.Dir.Path(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if fileLimit > 0 && searched > fileLimit {
			return errExhausted
		}
		searched++
		if !fileHasExtension(path, suffixes) {
			return nil
		}
		uri := protocol.URIFromPath(path)
		if filterFunc(uri) {
			return nil
		}
		// Get the file in order to include it in the snapshot.
		// TODO(golang/go#57558): it is fundamentally broken to track files in this
		// way; we may lose them if configuration or layout changes cause a view to
		// be recreated.
		//
		// Furthermore, this operation must ignore errors, including context
		// cancellation, or risk leaving the snapshot in an undefined state.
		s.ReadFile(ctx, uri)
		return nil
	})
	if err != nil {
		event.Error(ctx, "searching for template files failed", err)
	}
}

// filterFunc returns a func that reports whether uri is filtered by the currently configured
// directoryFilters.
//
// TODO(rfindley): memoize this func or filterer, as it is invariant on the
// view.
func (v *View) filterFunc() func(protocol.DocumentURI) bool {
	folderDir := v.folder.Dir.Path()
	filterer := buildFilterer(folderDir, v.folder.Env.GOMODCACHE, v.folder.Options.DirectoryFilters)
	return func(uri protocol.DocumentURI) bool {
		// Only filter relative to the configured root directory.
		if pathutil.InDir(folderDir, uri.Path()) {
			return relPathExcludedByFilter(strings.TrimPrefix(uri.Path(), folderDir), filterer)
		}
		return false
	}
}

// shutdown releases resources associated with the view, and waits for ongoing
// work to complete.
func (v *View) shutdown() {
	// Cancel the initial workspace load if it is still running.
	v.initCancelFirstAttempt()

	v.snapshotMu.Lock()
	if v.snapshot != nil {
		v.snapshot.cancel()
		v.snapshot.decref()
		v.snapshot = nil
	}
	v.snapshotMu.Unlock()

	v.snapshotWG.Wait()
}

// IgnoredFile reports if a file would be ignored by a `go list` of the whole
// workspace.
//
// While go list ./... skips directories starting with '.', '_', or 'testdata',
// gopls may still load them via file queries. Explicitly filter them out.
func (s *Snapshot) IgnoredFile(uri protocol.DocumentURI) bool {
	// Fast path: if uri doesn't contain '.', '_', or 'testdata', it is not
	// possible that it is ignored.
	{
		uriStr := string(uri)
		if !strings.Contains(uriStr, ".") && !strings.Contains(uriStr, "_") && !strings.Contains(uriStr, "testdata") {
			return false
		}
	}

	return s.view.ignoreFilter.ignored(uri.Path())
}

// An ignoreFilter implements go list's exclusion rules via its 'ignored' method.
type ignoreFilter struct {
	prefixes []string // root dirs, ending in filepath.Separator
}

// newIgnoreFilter returns a new ignoreFilter implementing exclusion rules
// relative to the provided directories.
func newIgnoreFilter(dirs []string) *ignoreFilter {
	f := new(ignoreFilter)
	for _, d := range dirs {
		f.prefixes = append(f.prefixes, filepath.Clean(d)+string(filepath.Separator))
	}
	return f
}

func (f *ignoreFilter) ignored(filename string) bool {
	for _, prefix := range f.prefixes {
		if suffix := strings.TrimPrefix(filename, prefix); suffix != filename {
			if checkIgnored(suffix) {
				return true
			}
		}
	}
	return false
}

// checkIgnored implements go list's exclusion rules.
// Quoting “go help list”:
//
//	Directory and file names that begin with "." or "_" are ignored
//	by the go tool, as are directories named "testdata".
func checkIgnored(suffix string) bool {
	// Note: this could be further optimized by writing a HasSegment helper, a
	// segment-boundary respecting variant of strings.Contains.
	for _, component := range strings.Split(suffix, string(filepath.Separator)) {
		if len(component) == 0 {
			continue
		}
		if component[0] == '.' || component[0] == '_' || component == "testdata" {
			return true
		}
	}
	return false
}

// Snapshot returns the current snapshot for the view, and a
// release function that must be called when the Snapshot is
// no longer needed.
//
// The resulting error is non-nil if and only if the view is shut down, in
// which case the resulting release function will also be nil.
func (v *View) Snapshot() (*Snapshot, func(), error) {
	v.snapshotMu.Lock()
	defer v.snapshotMu.Unlock()
	if v.snapshot == nil {
		return nil, nil, errors.New("view is shutdown")
	}
	return v.snapshot, v.snapshot.Acquire(), nil
}

func (s *Snapshot) initialize(ctx context.Context, firstAttempt bool) {
	select {
	case <-ctx.Done():
		return
	case s.view.initializationSema <- struct{}{}:
	}

	defer func() {
		<-s.view.initializationSema
	}()

	s.mu.Lock()
	initialized := s.initialized
	s.mu.Unlock()

	if initialized {
		return
	}

	s.loadWorkspace(ctx, firstAttempt)
}

func (s *Snapshot) loadWorkspace(ctx context.Context, firstAttempt bool) (loadErr error) {
	// A failure is retryable if it may have been due to context cancellation,
	// and this is not the initial workspace load (firstAttempt==true).
	//
	// The IWL runs on a detached context with a long (~10m) timeout, so
	// if the context was canceled we consider loading to have failed
	// permanently.
	retryableFailure := func() bool {
		return loadErr != nil && ctx.Err() != nil && !firstAttempt
	}
	defer func() {
		if !retryableFailure() {
			s.mu.Lock()
			s.initialized = true
			s.mu.Unlock()
		}
		if firstAttempt {
			close(s.view.initialWorkspaceLoad)
		}
	}()

	// TODO(rFindley): we should only locate template files on the first attempt,
	// or guard it via a different mechanism.
	s.locateTemplateFiles(ctx)

	// Collect module paths to load by parsing go.mod files. If a module fails to
	// parse, capture the parsing failure as a critical diagnostic.
	var scopes []loadScope           // scopes to load
	var modDiagnostics []*Diagnostic // diagnostics for broken go.mod files
	addError := func(uri protocol.DocumentURI, err error) {
		modDiagnostics = append(modDiagnostics, &Diagnostic{
			URI:      uri,
			Severity: protocol.SeverityError,
			Source:   ListError,
			Message:  err.Error(),
		})
	}

	// TODO(rfindley): this should be predicated on the s.view.moduleMode().
	// There is no point loading ./... if we have an empty go.work.
	if len(s.view.workspaceModFiles) > 0 {
		for modURI := range s.view.workspaceModFiles {
			// Verify that the modfile is valid before trying to load it.
			//
			// TODO(rfindley): now that we no longer need to parse the modfile in
			// order to load scope, we could move these diagnostics to a more general
			// location where we diagnose problems with modfiles or the workspace.
			//
			// Be careful not to add context cancellation errors as critical module
			// errors.
			fh, err := s.ReadFile(ctx, modURI)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				addError(modURI, err)
				continue
			}
			parsed, err := s.ParseMod(ctx, fh)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				addError(modURI, err)
				continue
			}
			if parsed.File == nil || parsed.File.Module == nil {
				addError(modURI, fmt.Errorf("no module path for %s", modURI))
				continue
			}
			moduleDir := filepath.Dir(modURI.Path())
			// Previously, we loaded <modulepath>/... for each module path, but that
			// is actually incorrect when the pattern may match packages in more than
			// one module. See golang/go#59458 for more details.
			scopes = append(scopes, moduleLoadScope{dir: moduleDir, modulePath: parsed.File.Module.Mod.Path})
		}
	} else {
		scopes = append(scopes, viewLoadScope("LOAD_VIEW"))
	}

	// If we're loading anything, ensure we also load builtin,
	// since it provides fake definitions (and documentation)
	// for types like int that are used everywhere.
	if len(scopes) > 0 {
		scopes = append(scopes, packageLoadScope("builtin"))
	}
	loadErr = s.load(ctx, true, scopes...)

	if retryableFailure() {
		return loadErr
	}

	var criticalErr *CriticalError
	switch {
	case loadErr != nil && ctx.Err() != nil:
		event.Error(ctx, fmt.Sprintf("initial workspace load: %v", loadErr), loadErr)
		criticalErr = &CriticalError{
			MainError: loadErr,
		}
	case loadErr != nil:
		event.Error(ctx, "initial workspace load failed", loadErr)
		extractedDiags := s.extractGoCommandErrors(ctx, loadErr)
		criticalErr = &CriticalError{
			MainError:   loadErr,
			Diagnostics: maps.Group(append(modDiagnostics, extractedDiags...), byURI),
		}
	case len(modDiagnostics) == 1:
		criticalErr = &CriticalError{
			MainError:   fmt.Errorf(modDiagnostics[0].Message),
			Diagnostics: maps.Group(modDiagnostics, byURI),
		}
	case len(modDiagnostics) > 1:
		criticalErr = &CriticalError{
			MainError:   fmt.Errorf("error loading module names"),
			Diagnostics: maps.Group(modDiagnostics, byURI),
		}
	}

	// Lock the snapshot when setting the initialized error.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initializedErr = criticalErr
	return loadErr
}

// A StateChange describes external state changes that may affect a snapshot.
//
// By far the most common of these is a change to file state, but a query of
// module upgrade information or vulnerabilities also affects gopls' behavior.
type StateChange struct {
	Files          map[protocol.DocumentURI]file.Handle
	ModuleUpgrades map[protocol.DocumentURI]map[string]string
	GCDetails      map[metadata.PackageID]bool // package -> whether or not we want details
}

// Invalidate processes the provided state change, invalidating any derived
// results that depend on the changed state.
//
// The resulting snapshot is non-nil, representing the outcome of the state
// change. The second result is a function that must be called to release the
// snapshot when the snapshot is no longer needed.
//
// The resulting bool reports whether the new View needs to be re-diagnosed.
// See Snapshot.clone for more details.
func (v *View) Invalidate(ctx context.Context, changed StateChange) (*Snapshot, func(), bool) {
	// Detach the context so that content invalidation cannot be canceled.
	ctx = xcontext.Detach(ctx)

	// This should be the only time we hold the view's snapshot lock for any period of time.
	v.snapshotMu.Lock()
	defer v.snapshotMu.Unlock()

	prevSnapshot := v.snapshot

	if prevSnapshot == nil {
		panic("invalidateContent called after shutdown")
	}

	// Cancel all still-running previous requests, since they would be
	// operating on stale data.
	prevSnapshot.cancel()

	// Do not clone a snapshot until its view has finished initializing.
	//
	// TODO(rfindley): shouldn't we do this before canceling?
	prevSnapshot.AwaitInitialized(ctx)

	// Save one lease of the cloned snapshot in the view.
	var needsDiagnosis bool
	v.snapshot, needsDiagnosis = prevSnapshot.clone(ctx, v.baseCtx, changed)

	// Remove the initial reference created when prevSnapshot was created.
	prevSnapshot.decref()

	// Return a second lease to the caller.
	return v.snapshot, v.snapshot.Acquire(), needsDiagnosis
}

// defineView computes the view definition for the provided workspace folder
// and URI.
//
// If forURI is non-empty, this view should be the best view including forURI.
// Otherwise, it is the default view for the folder.
//
// defineView only returns an error in the event of context cancellation.
//
// TODO(rfindley): we should be able to remove the error return, as
// findModules is going away, and all other I/O is memoized.
//
// TODO(rfindley): pass in a narrower interface for the file.Source
// (e.g. fileExists func(DocumentURI) bool) to make clear that this
// process depends only on directory information, not file contents.
func defineView(ctx context.Context, fs file.Source, folder *Folder, forURI protocol.DocumentURI) (*viewDefinition, error) {
	if err := checkPathValid(folder.Dir.Path()); err != nil {
		return nil, fmt.Errorf("invalid workspace folder path: %w; check that the spelling of the configured workspace folder path agrees with the spelling reported by the operating system", err)
	}
	dir := folder.Dir.Path()
	if forURI != "" {
		dir = filepath.Dir(forURI.Path())
	}

	def := new(viewDefinition)
	def.folder = folder

	var err error
	dirURI := protocol.URIFromPath(dir)
	goworkFromEnv := false
	if folder.Env.GOWORK != "off" && folder.Env.GOWORK != "" {
		goworkFromEnv = true
		def.gowork = protocol.URIFromPath(folder.Env.GOWORK)
	} else {
		def.gowork, err = findRootPattern(ctx, dirURI, "go.work", fs)
		if err != nil {
			return nil, err
		}
	}

	// When deriving the best view for a given file, we only want to search
	// up the directory hierarchy for modfiles.
	//
	// If forURI is unset, we still use the legacy heuristic of scanning for
	// nested modules (this will be removed as part of golang/go#57979).
	if forURI != "" {
		def.gomod, err = findRootPattern(ctx, dirURI, "go.mod", fs)
		if err != nil {
			return nil, err
		}
	} else {
		// filterFunc is the path filter function for this workspace folder. Notably,
		// it is relative to folder (which is specified by the user), not root.
		filterFunc := relPathExcludedByFilterFunc(folder.Dir.Path(), folder.Env.GOMODCACHE, folder.Options.DirectoryFilters)
		def.gomod, err = findWorkspaceModFile(ctx, folder.Dir, fs, filterFunc)
		if err != nil {
			return nil, err
		}
	}

	// Determine how we load and where to load package information for this view
	//
	// Specifically, set
	//  - def.typ
	//  - def.root
	//  - def.workspaceModFiles, and
	//  - def.envOverlay.

	// If GOPACKAGESDRIVER is set it takes precedence.
	{
		// The value of GOPACKAGESDRIVER is not returned through the go command.
		gopackagesdriver := os.Getenv("GOPACKAGESDRIVER")
		// A user may also have a gopackagesdriver binary on their machine, which
		// works the same way as setting GOPACKAGESDRIVER.
		//
		// TODO(rfindley): remove this call to LookPath. We should not support this
		// undocumented method of setting GOPACKAGESDRIVER.
		tool, err := exec.LookPath("gopackagesdriver")
		if gopackagesdriver != "off" && (gopackagesdriver != "" || (err == nil && tool != "")) {
			def.typ = GoPackagesDriverView
			def.root = dirURI
			return def, nil
		}
	}

	// From go.dev/ref/mod, module mode is active if GO111MODULE=on, or
	// GO111MODULE=auto or "" and we are inside a module or have a GOWORK value.
	// But gopls is less strict, allowing GOPATH mode if GO111MODULE="", and
	// AdHoc views if no module is found.

	// Prefer a go.work file if it is available and contains the module relevant
	// to forURI.
	if def.adjustedGO111MODULE() != "off" && folder.Env.GOWORK != "off" && def.gowork != "" {
		def.typ = GoWorkView
		if goworkFromEnv {
			// The go.work file could be anywhere, which can lead to confusing error
			// messages.
			def.root = dirURI
		} else {
			// The go.work file could be anywhere, which can lead to confusing error
			def.root = def.gowork.Dir()
		}
		def.workspaceModFiles, def.workspaceModFilesErr = goWorkModules(ctx, def.gowork, fs)

		// If forURI is in a module but that module is not
		// included in the go.work file, use a go.mod view with GOWORK=off.
		if forURI != "" && def.workspaceModFilesErr == nil && def.gomod != "" {
			if _, ok := def.workspaceModFiles[def.gomod]; !ok {
				def.typ = GoModView
				def.root = def.gomod.Dir()
				def.workspaceModFiles = map[protocol.DocumentURI]unit{def.gomod: {}}
				def.envOverlay = []string{"GOWORK=off"}
			}
		}
		return def, nil
	}

	// Otherwise, use the active module, if in module mode.
	//
	// Note, we could override GO111MODULE here via envOverlay if we wanted to
	// support the case where someone opens a module with GO111MODULE=off. But
	// that is probably not worth worrying about (at this point, folks probably
	// shouldn't be setting GO111MODULE).
	if def.adjustedGO111MODULE() != "off" && def.gomod != "" {
		def.typ = GoModView
		def.root = def.gomod.Dir()
		def.workspaceModFiles = map[protocol.DocumentURI]struct{}{def.gomod: {}}
		return def, nil
	}

	// Check if the workspace is within any GOPATH directory.
	inGOPATH := false
	for _, gp := range filepath.SplitList(folder.Env.GOPATH) {
		if pathutil.InDir(filepath.Join(gp, "src"), dir) {
			inGOPATH = true
			break
		}
	}
	if def.adjustedGO111MODULE() != "on" && inGOPATH {
		def.typ = GOPATHView
		def.root = dirURI
		return def, nil
	}

	// We're not in a workspace, module, or GOPATH, so have no better choice than
	// an ad-hoc view.
	def.typ = AdHocView
	def.root = dirURI
	return def, nil
}

// FetchGoEnv queries the environment and Go command to collect environment
// variables necessary for the workspace folder.
func FetchGoEnv(ctx context.Context, folder protocol.DocumentURI, opts *settings.Options) (*GoEnv, error) {
	dir := folder.Path()
	// All of the go commands invoked here should be fast. No need to share a
	// runner with other operations.
	runner := new(gocommand.Runner)
	inv := gocommand.Invocation{
		WorkingDir: dir,
		Env:        opts.EnvSlice(),
	}

	var (
		env = new(GoEnv)
		err error
	)
	envvars := map[string]*string{
		"GOCACHE":     &env.GOCACHE,
		"GOPATH":      &env.GOPATH,
		"GOPRIVATE":   &env.GOPRIVATE,
		"GOMODCACHE":  &env.GOMODCACHE,
		"GOFLAGS":     &env.GOFLAGS,
		"GO111MODULE": &env.GO111MODULE,
	}
	if err := loadGoEnv(ctx, dir, opts.EnvSlice(), runner, envvars); err != nil {
		return nil, err
	}

	env.GoVersion, err = gocommand.GoVersion(ctx, inv, runner)
	if err != nil {
		return nil, err
	}
	env.GoVersionOutput, err = gocommand.GoVersionOutput(ctx, inv, runner)
	if err != nil {
		return nil, err
	}

	// The value of GOPACKAGESDRIVER is not returned through the go command.
	if driver, ok := opts.Env["GOPACKAGESDRIVER"]; ok {
		env.GOPACKAGESDRIVER = driver
	} else {
		env.GOPACKAGESDRIVER = os.Getenv("GOPACKAGESDRIVER")
		// A user may also have a gopackagesdriver binary on their machine, which
		// works the same way as setting GOPACKAGESDRIVER.
		//
		// TODO(rfindley): remove this call to LookPath. We should not support this
		// undocumented method of setting GOPACKAGESDRIVER.
		if env.GOPACKAGESDRIVER == "" {
			tool, err := exec.LookPath("gopackagesdriver")
			if err == nil && tool != "" {
				env.GOPACKAGESDRIVER = tool
			}
		}
	}

	// While GOWORK is available through the Go command, we want to differentiate
	// between an explicit GOWORK value and one which is implicit from the file
	// system. The former doesn't change unless the environment changes.
	if gowork, ok := opts.Env["GOWORK"]; ok {
		env.GOWORK = gowork
	} else {
		env.GOWORK = os.Getenv("GOWORK")
	}
	return env, nil
}

// loadGoEnv loads `go env` values into the provided map, keyed by Go variable
// name.
func loadGoEnv(ctx context.Context, dir string, configEnv []string, runner *gocommand.Runner, vars map[string]*string) error {
	// We can save ~200 ms by requesting only the variables we care about.
	args := []string{"-json"}
	for k := range vars {
		args = append(args, k)
	}

	inv := gocommand.Invocation{
		Verb:       "env",
		Args:       args,
		Env:        configEnv,
		WorkingDir: dir,
	}
	stdout, err := runner.Run(ctx, inv)
	if err != nil {
		return err
	}
	envMap := make(map[string]string)
	if err := json.Unmarshal(stdout.Bytes(), &envMap); err != nil {
		return fmt.Errorf("internal error unmarshaling JSON from 'go env': %w", err)
	}
	for key, ptr := range vars {
		*ptr = envMap[key]
	}

	return nil
}

// findWorkspaceModFile searches for a single go.mod file relative to the given
// folder URI, using the following algorithm:
//  1. if there is a go.mod file in a parent directory, return it
//  2. else, if there is exactly one nested module, return it
//  3. else, return ""
func findWorkspaceModFile(ctx context.Context, folderURI protocol.DocumentURI, fs file.Source, excludePath func(string) bool) (protocol.DocumentURI, error) {
	match, err := findRootPattern(ctx, folderURI, "go.mod", fs)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	if match != "" {
		return match, nil
	}

	// ...else we should check if there's exactly one nested module.
	all, err := findModules(folderURI, excludePath, 2)
	if err == errExhausted {
		// Fall-back behavior: if we don't find any modules after searching 10000
		// files, assume there are none.
		event.Log(ctx, fmt.Sprintf("stopped searching for modules after %d files", fileLimit))
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(all) == 1 {
		// range to access first element.
		for uri := range all {
			return uri, nil
		}
	}
	return "", nil
}

// findRootPattern looks for files with the given basename in dir or any parent
// directory of dir, using the provided FileSource. It returns the first match,
// starting from dir and search parents.
//
// The resulting string is either the file path of a matching file with the
// given basename, or "" if none was found.
//
// findRootPattern only returns an error in the case of context cancellation.
func findRootPattern(ctx context.Context, dirURI protocol.DocumentURI, basename string, fs file.Source) (protocol.DocumentURI, error) {
	dir := dirURI.Path()
	for dir != "" {
		target := filepath.Join(dir, basename)
		uri := protocol.URIFromPath(target)
		fh, err := fs.ReadFile(ctx, uri)
		if err != nil {
			return "", err // context cancelled
		}
		if fileExists(fh) {
			return uri, nil
		}
		// Trailing separators must be trimmed, otherwise filepath.Split is a noop.
		next, _ := filepath.Split(strings.TrimRight(dir, string(filepath.Separator)))
		if next == dir {
			break
		}
		dir = next
	}
	return "", nil
}

// checkPathValid performs an OS-specific path validity check. The
// implementation varies for filesystems that are case-insensitive
// (e.g. macOS, Windows), and for those that disallow certain file
// names (e.g. path segments ending with a period on Windows, or
// reserved names such as "com"; see
// https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file).
var checkPathValid = defaultCheckPathValid

// CheckPathValid checks whether a directory is suitable as a workspace folder.
func CheckPathValid(dir string) error { return checkPathValid(dir) }

func defaultCheckPathValid(path string) error {
	return nil
}

// IsGoPrivatePath reports whether target is a private import path, as identified
// by the GOPRIVATE environment variable.
func (s *Snapshot) IsGoPrivatePath(target string) bool {
	return globsMatchPath(s.view.folder.Env.GOPRIVATE, target)
}

// ModuleUpgrades returns known module upgrades for the dependencies of
// modfile.
func (s *Snapshot) ModuleUpgrades(modfile protocol.DocumentURI) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	upgrades := map[string]string{}
	orig, _ := s.moduleUpgrades.Get(modfile)
	for mod, ver := range orig {
		upgrades[mod] = ver
	}
	return upgrades
}

// GoVersion returns the effective release Go version (the X in go1.X) for this
// view.
func (v *View) GoVersion() int {
	return v.folder.Env.GoVersion
}

// GoVersionString returns the effective Go version string for this view.
//
// Unlike [GoVersion], this encodes the minor version and commit hash information.
func (v *View) GoVersionString() string {
	return gocommand.ParseGoVersionOutput(v.folder.Env.GoVersionOutput)
}

// GoVersionString is temporarily available from the snapshot.
//
// TODO(rfindley): refactor so that this method is not necessary.
func (s *Snapshot) GoVersionString() string {
	return s.view.GoVersionString()
}

// Copied from
// https://cs.opensource.google/go/go/+/master:src/cmd/go/internal/str/path.go;l=58;drc=2910c5b4a01a573ebc97744890a07c1a3122c67a
func globsMatchPath(globs, target string) bool {
	for globs != "" {
		// Extract next non-empty glob in comma-separated list.
		var glob string
		if i := strings.Index(globs, ","); i >= 0 {
			glob, globs = globs[:i], globs[i+1:]
		} else {
			glob, globs = globs, ""
		}
		if glob == "" {
			continue
		}

		// A glob with N+1 path elements (N slashes) needs to be matched
		// against the first N+1 path elements of target,
		// which end just before the N+1'th slash.
		n := strings.Count(glob, "/")
		prefix := target
		// Walk target, counting slashes, truncating at the N+1'th slash.
		for i := 0; i < len(target); i++ {
			if target[i] == '/' {
				if n == 0 {
					prefix = target[:i]
					break
				}
				n--
			}
		}
		if n > 0 {
			// Not enough prefix elements.
			continue
		}
		matched, _ := path.Match(glob, prefix)
		if matched {
			return true
		}
	}
	return false
}

var modFlagRegexp = regexp.MustCompile(`-mod[ =](\w+)`)

// TODO(rstambler): Consolidate modURI and modContent back into a FileHandle
// after we have a version of the workspace go.mod file on disk. Getting a
// FileHandle from the cache for temporary files is problematic, since we
// cannot delete it.
//
// TODO(rfindley): move this to snapshot.go.
func (s *Snapshot) vendorEnabled(ctx context.Context, modURI protocol.DocumentURI, modContent []byte) (bool, error) {
	// Legacy GOPATH workspace?
	if len(s.view.workspaceModFiles) == 0 {
		return false, nil
	}

	// Explicit -mod flag?
	matches := modFlagRegexp.FindStringSubmatch(s.view.folder.Env.GOFLAGS)
	if len(matches) != 0 {
		modFlag := matches[1]
		if modFlag != "" {
			// Don't override an explicit '-mod=vendor' argument.
			// We do want to override '-mod=readonly': it would break various module code lenses,
			// and on 1.16 we know -modfile is available, so we won't mess with go.mod anyway.
			return modFlag == "vendor", nil
		}
	}

	modFile, err := modfile.Parse(modURI.Path(), modContent, nil)
	if err != nil {
		return false, err
	}

	// No vendor directory?
	// TODO(golang/go#57514): this is wrong if the working dir is not the module
	// root.
	if fi, err := os.Stat(filepath.Join(s.view.root.Path(), "vendor")); err != nil || !fi.IsDir() {
		return false, nil
	}

	// Vendoring enabled by default by go declaration in go.mod?
	vendorEnabled := modFile.Go != nil && modFile.Go.Version != "" && semver.Compare("v"+modFile.Go.Version, "v1.14") >= 0
	return vendorEnabled, nil
}

// TODO(rfindley): clean up the redundancy of allFilesExcluded,
// pathExcludedByFilterFunc, pathExcludedByFilter, view.filterFunc...
func allFilesExcluded(files []string, filterFunc func(protocol.DocumentURI) bool) bool {
	for _, f := range files {
		uri := protocol.URIFromPath(f)
		if !filterFunc(uri) {
			return false
		}
	}
	return true
}

// relPathExcludedByFilterFunc returns a func that filters paths relative to the
// given folder according the given GOMODCACHE value and directory filters (see
// settings.BuildOptions.DirectoryFilters).
//
// The resulting func returns true if the directory should be skipped.
func relPathExcludedByFilterFunc(folder, gomodcache string, directoryFilters []string) func(string) bool {
	filterer := buildFilterer(folder, gomodcache, directoryFilters)
	return func(path string) bool {
		return relPathExcludedByFilter(path, filterer)
	}
}

func relPathExcludedByFilter(path string, filterer *Filterer) bool {
	path = strings.TrimPrefix(filepath.ToSlash(path), "/")
	return filterer.Disallow(path)
}

func buildFilterer(folder, gomodcache string, directoryFilters []string) *Filterer {
	var filters []string
	filters = append(filters, directoryFilters...)
	if pref := strings.TrimPrefix(gomodcache, folder); pref != gomodcache {
		modcacheFilter := "-" + strings.TrimPrefix(filepath.ToSlash(pref), "/")
		filters = append(filters, modcacheFilter)
	}
	return NewFilterer(filters)
}
