// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cache is the core of gopls: it is concerned with state
// management, dependency analysis, and invalidation; and it holds the
// machinery of type checking and modular static analysis. Its
// principal types are [Session], [Folder], [View], [Snapshot],
// [Cache], and [Package].
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
	"sort"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/util/maps"
	"cuelang.org/go/internal/golangorgx/gopls/util/slices"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/gocommand"
	"cuelang.org/go/internal/golangorgx/tools/xcontext"
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
}

// GoEnv holds the environment variables and data from the Go command that is
// required for operating on a workspace folder.
type GoEnv struct {
	// Go environment variables. These correspond directly with the Go env var of
	// the same name.
	GOOS        string
	GOARCH      string
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

	// baseCtx is the context handed to NewView. This is the parent of all
	// background contexts created for this view.
	baseCtx context.Context

	importsState *importsState

	// parseCache holds an LRU cache of recently parsed files.
	parseCache *parseCache

	// fs is the file source used to populate this view.
	fs *overlayFS

	// cancelInitialWorkspaceLoad can be used to terminate the view's first
	// attempt at initialization.
	cancelInitialWorkspaceLoad context.CancelFunc

	snapshotMu sync.Mutex
	snapshot   *Snapshot // latest snapshot; nil after shutdown has been called

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

	// Document filters are constructed once, in View.filterFunc.
	filterFuncOnce sync.Once
	_filterFunc    func(protocol.DocumentURI) bool // only accessed by View.filterFunc
}

// definition implements the viewDefiner interface.
func (v *View) definition() *viewDefinition { return v.viewDefinition }

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

	typ ViewType

	// root represents the directory root of the CUE module that contains
	// the WorkspaceFolder folder
	root protocol.DocumentURI

	// workspaceModFiles holds the set of mod files active in this snapshot.
	//
	// For a go.work workspace, this is the set of workspace modfiles. For a
	// go.mod workspace, this contains the go.mod file defining the workspace
	// root, as well as any locally replaced modules (if
	// "includeReplaceInWorkspace" is set).
	//
	// TODO(rfindley): should we just run `go list -m` to compute this set?
	workspaceModFiles    map[protocol.DocumentURI]struct{}
	workspaceModFilesErr error // error encountered computing workspaceModFiles

	// envOverlay holds additional environment to apply to this viewDefinition.
	envOverlay map[string]string
}

// definition implements the viewDefiner interface.
func (d *viewDefinition) definition() *viewDefinition { return d }

// Type returns the ViewType type, which determines how go/packages are loaded
// for this View.
func (d *viewDefinition) Type() ViewType { return d.typ }

// Root returns the view root, which determines where packages are loaded from.
func (d *viewDefinition) Root() protocol.DocumentURI { return d.root }

// EnvOverlay returns a new sorted slice of environment variables (in the form
// "k=v") for this view definition's env overlay.
func (d *viewDefinition) EnvOverlay() []string {
	var env []string
	for k, v := range d.envOverlay {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(env)
	return env
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
		x.root == y.root
}

// A ViewType describes how we load package information for a view.
//
// This is used for constructing the go/packages.Load query, and for
// interpreting missing packages, imports, or errors.
//
// See the documentation for individual ViewType values for details.
type ViewType int

const (
	// An AdHocView is a collection of files in a given directory, not in GOPATH
	// or a module.
	//
	// Load: . from the workspace folder.
	AdHocView ViewType = iota

	CUEModView
)

func (t ViewType) String() string {
	switch t {
	case AdHocView:
		return "AdHocView"
	case CUEModView:
		return "CUEModView"
	default:
		return "Unknown"
	}
}

// moduleMode reports whether the view uses Go modules.
func (w viewDefinition) moduleMode() bool {
	switch w.typ {
	case CUEModView:
		return true
	default:
		return false
	}
}

func (v *View) ID() string { return v.id }

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
// TODO(rfindley): rethink this function, or inline sole call.
func viewEnv(v *View) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `go info for %v
(view type %v)
(root dir %s)
(build flags: %v)
(env overlay: %v)
`,
		v.folder.Dir.Path(),
		v.typ,
		v.root.Path(),
		v.folder.Options.BuildFlags,
		v.envOverlay,
	)

	return buf.String()
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
func (v *View) filterFunc() func(protocol.DocumentURI) bool {
	v.filterFuncOnce.Do(func() {
		v._filterFunc = func(uri protocol.DocumentURI) bool {
			return false
		}
	})
	return v._filterFunc
}

// shutdown releases resources associated with the view.
func (v *View) shutdown() {
	// Cancel the initial workspace load if it is still running.
	v.cancelInitialWorkspaceLoad()

	v.snapshotMu.Lock()
	if v.snapshot != nil {
		v.snapshot.cancel()
		v.snapshot.decref()
		v.snapshot = nil
	}
	v.snapshotMu.Unlock()
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

// initialize loads the metadata (and currently, file contents, due to
// golang/go#57558) for the main package query of the View, which depends on
// the view type (see ViewType). If s.initialized is already true, initialize
// is a no op.
//
// The first attempt--which populates the first snapshot for a new view--must
// be allowed to run to completion without being cancelled.
//
// Subsequent attempts are triggered by conditions where gopls can't enumerate
// specific packages that require reloading, such as a change to a go.mod file.
// These attempts may be cancelled, and then retried by a later call.
//
// Postcondition: if ctx was not cancelled, s.initialized is true, s.initialErr
// holds the error resulting from initialization, if any, and s.metadata holds
// the resulting metadata graph.
func (s *Snapshot) initialize(ctx context.Context, firstAttempt bool) {
	// Acquire initializationSema, which is
	// (in effect) a mutex with a timeout.
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

	defer func() {
		if firstAttempt {
			close(s.view.initialWorkspaceLoad)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.initialized = true

	// TODO(myitcv): fix this?
	s.initialErr = nil
}

// A StateChange describes external state changes that may affect a snapshot.
//
// By far the most common of these is a change to file state, but a query of
// module upgrade information or vulnerabilities also affects gopls' behavior.
type StateChange struct {
	Modifications  []file.Modification // if set, the raw modifications originating this change
	Files          map[protocol.DocumentURI]file.Handle
	ModuleUpgrades map[protocol.DocumentURI]map[string]string
	GCDetails      map[metadata.PackageID]bool // package -> whether or not we want details
}

// InvalidateView processes the provided state change, invalidating any derived
// results that depend on the changed state.
//
// The resulting snapshot is non-nil, representing the outcome of the state
// change. The second result is a function that must be called to release the
// snapshot when the snapshot is no longer needed.
//
// An error is returned if the given view is no longer active in the session.
func (s *Session) InvalidateView(ctx context.Context, view *View, changed StateChange) (*Snapshot, func(), error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()

	if !slices.Contains(s.views, view) {
		return nil, nil, fmt.Errorf("view is no longer active")
	}
	snapshot, release, _ := s.invalidateViewLocked(ctx, view, changed)
	return snapshot, release, nil
}

// invalidateViewLocked invalidates the content of the given view.
// (See [Session.InvalidateView]).
//
// The resulting bool reports whether the View needs to be re-diagnosed.
// (See [Snapshot.clone]).
//
// s.viewMu must be held while calling this method.
func (s *Session) invalidateViewLocked(ctx context.Context, v *View, changed StateChange) (*Snapshot, func(), bool) {
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

	var needsDiagnosis bool
	s.snapshotWG.Add(1)
	v.snapshot, needsDiagnosis = prevSnapshot.clone(ctx, v.baseCtx, changed, s.snapshotWG.Done)

	// Remove the initial reference created when prevSnapshot was created.
	prevSnapshot.decref()

	// Return a second lease to the caller.
	return v.snapshot, v.snapshot.Acquire(), needsDiagnosis
}

// defineView computes the view definition for the provided workspace folder
// and URI.
//
// If forFile is non-empty, this view should be the best view including forFile.
// Otherwise, it is the default view for the folder. Per below TODO(myitcv), we
// need to better understand when this can happen, and what the preceding sentence
// actually means.
//
// defineView only returns an error in the event of context cancellation.
//
// gopls note: keep this function in sync with bestView.
func defineView(ctx context.Context, fs file.Source, folder *Folder, forFile file.Handle) (*viewDefinition, error) {
	if err := checkPathValid(folder.Dir.Path()); err != nil {
		return nil, fmt.Errorf("invalid workspace folder path: %w; check that the spelling of the configured workspace folder path agrees with the spelling reported by the operating system", err)
	}

	if forFile != nil {
		// TODO(myitcv): fix the implementation here. forFile != nil when we are trying
		// to compute the set of views given the set of open files/known folders. This is
		// part of the zero config approach in gopls, and we don't have anything like that
		// yet for 'cue lsp'.
		return nil, fmt.Errorf("defineView with forFile != nil; not yet supported")
	}

	def := new(viewDefinition)
	def.folder = folder
	def.root = folder.Dir

	targetFile := filepath.Join(folder.Dir.Path(), filepath.FromSlash("cue.mod/module.cue"))
	targetURI := protocol.URIFromPath(targetFile)
	modFile, err := fs.ReadFile(ctx, targetURI)
	if err != nil {
		return nil, err // cancelled
	}
	if !fileExists(modFile) {
		return nil, fmt.Errorf("WorkspaceFolder %s is not rooted at a CUE module", folder.Dir)
	}

	def.typ = CUEModView
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
		"GOOS":        &env.GOOS,
		"GOARCH":      &env.GOARCH,
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

func relPathExcludedByFilter(path string, filterer *Filterer) bool {
	path = strings.TrimPrefix(filepath.ToSlash(path), "/")
	return filterer.Disallow(path)
}
