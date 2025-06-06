// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/util/maps"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
)

// fileDiagnostics holds the current state of published diagnostics for a file.
type fileDiagnostics struct {
	publishedHash file.Hash // hash of the last set of diagnostics published for this URI
	mustPublish   bool      // if set, publish diagnostics even if they haven't changed

	// Orphaned file diagnostics are not necessarily associated with any *View
	// (since they are orphaned). Instead, keep track of the modification ID at
	// which they were orphaned (see server.lastModificationID).
	orphanedAt              uint64 // modification ID at which this file was orphaned.
	orphanedFileDiagnostics []*cache.Diagnostic

	// Files may have their diagnostics computed by multiple views, and so
	// diagnostics are organized by View. See the documentation for update for more
	// details about how the set of file diagnostics evolves over time.
	byView map[*cache.View]viewDiagnostics
}

// viewDiagnostics holds a set of file diagnostics computed from a given View.
type viewDiagnostics struct {
	snapshot    uint64 // snapshot sequence ID
	version     int32  // file version
	diagnostics []*cache.Diagnostic
}

// common types; for brevity
type (
	viewSet = map[*cache.View]unit
	diagMap = map[protocol.DocumentURI][]*cache.Diagnostic
)

// hashDiagnostics computes a hash to identify a diagnostic.
func hashDiagnostic(d *cache.Diagnostic) file.Hash {
	h := sha256.New()
	for _, t := range d.Tags {
		fmt.Fprintf(h, "tag: %s\n", t)
	}
	for _, r := range d.Related {
		fmt.Fprintf(h, "related: %s %s %s\n", r.Location.URI, r.Message, r.Location.Range)
	}
	fmt.Fprintf(h, "code: %s\n", d.Code)
	fmt.Fprintf(h, "codeHref: %s\n", d.CodeHref)
	fmt.Fprintf(h, "message: %s\n", d.Message)
	fmt.Fprintf(h, "range: %s\n", d.Range)
	fmt.Fprintf(h, "severity: %s\n", d.Severity)
	fmt.Fprintf(h, "source: %s\n", d.Source)
	if d.BundledFixes != nil {
		fmt.Fprintf(h, "fixes: %s\n", *d.BundledFixes)
	}
	var hash [sha256.Size]byte
	h.Sum(hash[:0])
	return hash
}

func sortDiagnostics(d []*cache.Diagnostic) {
	sort.Slice(d, func(i int, j int) bool {
		a, b := d[i], d[j]
		if r := protocol.CompareRange(a.Range, b.Range); r != 0 {
			return r < 0
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Message < b.Message
	})
}

func (s *server) diagnoseChangedViews(ctx context.Context, modID uint64, lastChange map[*cache.View][]protocol.DocumentURI, cause ModificationSource) {
	// Collect views needing diagnosis.
	s.modificationMu.Lock()
	needsDiagnosis := maps.Keys(s.viewsToDiagnose)
	s.modificationMu.Unlock()

	// Diagnose views concurrently.
	var wg sync.WaitGroup
	for _, v := range needsDiagnosis {
		v := v
		snapshot, release, err := v.Snapshot()
		if err != nil {
			s.modificationMu.Lock()
			// The View is shut down. Unlike below, no need to check
			// s.needsDiagnosis[v], since the view can never be diagnosed.
			delete(s.viewsToDiagnose, v)
			s.modificationMu.Unlock()
			continue
		}

		// Collect uris for fast diagnosis. We only care about the most recent
		// change here, because this is just an optimization for the case where the
		// user is actively editing a single file.
		uris := lastChange[v]
		if snapshot.Options().DiagnosticsTrigger == settings.DiagnosticsOnSave && cause == FromDidChange {
			// The user requested to update the diagnostics only on save.
			// Do not diagnose yet.
			release()
			continue
		}

		wg.Add(1)
		go func(snapshot *cache.Snapshot, uris []protocol.DocumentURI) {
			defer release()
			defer wg.Done()
			s.diagnoseSnapshot(snapshot, uris, snapshot.Options().DiagnosticsDelay)
			s.modificationMu.Lock()

			// Only remove v from s.viewsToDiagnose if the snapshot is not cancelled.
			// This ensures that the snapshot was not cloned before its state was
			// fully evaluated, and therefore avoids missing a change that was
			// irrelevant to an incomplete snapshot.
			//
			// See the documentation for s.viewsToDiagnose for details.
			if snapshot.BackgroundContext().Err() == nil && s.viewsToDiagnose[v] <= modID {
				delete(s.viewsToDiagnose, v)
			}
			s.modificationMu.Unlock()
		}(snapshot, uris)
	}

	wg.Wait()

	// Diagnose orphaned files for the session.
	orphanedFileDiagnostics, err := s.session.OrphanedFileDiagnostics(ctx)
	if err == nil {
		err = s.updateOrphanedFileDiagnostics(ctx, modID, orphanedFileDiagnostics)
	}
	if err != nil {
		if ctx.Err() == nil {
			event.Error(ctx, "warning: while diagnosing orphaned files", err)
		}
	}
}

// diagnoseSnapshot computes and publishes diagnostics for the given snapshot.
//
// If delay is non-zero, computing diagnostics does not start until after this
// delay has expired, to allow work to be cancelled by subsequent changes.
//
// If changedURIs is non-empty, it is a set of recently changed files that
// should be diagnosed immediately, and onDisk reports whether these file
// changes came from a change to on-disk files.
func (s *server) diagnoseSnapshot(snapshot *cache.Snapshot, changedURIs []protocol.DocumentURI, delay time.Duration) {
	ctx := snapshot.BackgroundContext()
	ctx, done := event.Start(ctx, "Server.diagnoseSnapshot", snapshot.Labels()...)
	defer done()

	allViews := s.session.Views()
	if delay > 0 {
		// 2-phase diagnostics.
		//
		// The first phase just parses and type-checks (but
		// does not analyze) packages directly affected by
		// file modifications.
		//
		// The second phase runs after the delay, and does everything.
		//
		// We wait a brief delay before the first phase, to allow higher priority
		// work such as autocompletion to acquire the type checking mutex (though
		// typically both diagnosing changed files and performing autocompletion
		// will be doing the same work: recomputing active packages).
		const minDelay = 20 * time.Millisecond
		select {
		case <-time.After(minDelay):
		case <-ctx.Done():
			return
		}

		if len(changedURIs) > 0 {
			diagnostics, err := s.diagnoseChangedFiles(ctx, snapshot, changedURIs)
			if err != nil {
				if ctx.Err() == nil {
					event.Error(ctx, "warning: while diagnosing changed files", err, snapshot.Labels()...)
				}
				return
			}
			s.updateDiagnostics(ctx, allViews, snapshot, diagnostics, false)
		}

		if delay < minDelay {
			delay = 0
		} else {
			delay -= minDelay
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}

	diagnostics, err := s.diagnose(ctx, snapshot)
	if err != nil {
		if ctx.Err() == nil {
			event.Error(ctx, "warning: while diagnosing snapshot", err, snapshot.Labels()...)
		}
		return
	}
	s.updateDiagnostics(ctx, allViews, snapshot, diagnostics, true)
}

func (s *server) diagnoseChangedFiles(ctx context.Context, snapshot *cache.Snapshot, uris []protocol.DocumentURI) (diagMap, error) {
	ctx, done := event.Start(ctx, "Server.diagnoseChangedFiles", snapshot.Labels()...)
	defer done()

	toDiagnose := make(map[metadata.ImportPath]*build.Instance)
	for _, uri := range uris {
		// If the file is not open, don't diagnose its package.
		//
		// We don't care about fast diagnostics for files that are no longer open,
		// because the user isn't looking at them. Also, explicitly requesting a
		// package can lead to "command-line-arguments" packages if the file isn't
		// covered by the current View. By avoiding requesting packages for e.g.
		// unrelated file movement, we can minimize these unnecessary packages.
		if !snapshot.IsOpen(uri) {
			continue
		}
		// If the file is not known to the snapshot (e.g., if it was deleted),
		// don't diagnose it.
		if snapshot.FindFile(uri) == nil {
			continue
		}

		insts, err := snapshot.MetadataForFile(ctx, uri)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// TODO(findleyr): we should probably do something with the error here,
			// but as of now this can fail repeatedly if load fails, so can be too
			// noisy to log (and we'll handle things later in the slow pass).
			continue
		}
		if len(insts) > 0 {
			// The results of snapshot.MetadataForFile are sorted, with
			// the instance with the fewest BuildFiles first. We want the
			// smallest/narrowest instance here.
			inst := insts[0]
			toDiagnose[metadata.ImportPath(inst.ImportPath)] = inst
		}
	}
	diags, err := snapshot.PackageDiagnostics(ctx, maps.Keys(toDiagnose)...)
	if err != nil {
		if ctx.Err() == nil {
			event.Error(ctx, "warning: diagnostics failed", err, snapshot.Labels()...)
		}
		return nil, err
	}
	// golang/go#59587: guarantee that we compute type-checking diagnostics
	// for every compiled package file, otherwise diagnostics won't be quickly
	// cleared following a fix.
	for _, inst := range toDiagnose {
		for _, file := range inst.BuildFiles {
			uri := protocol.URIFromPath(file.Filename)
			if _, ok := diags[uri]; !ok {
				diags[uri] = nil
			}
		}
	}
	return diags, nil
}

func (s *server) diagnose(ctx context.Context, snapshot *cache.Snapshot) (diagMap, error) {
	ctx, done := event.Start(ctx, "Server.diagnose", snapshot.Labels()...)
	defer done()

	var (
		diagnostics = make(diagMap)
	)

	return diagnostics, nil
}

// combineDiagnostics combines and filters list/parse/type diagnostics from
// tdiags with adiags, and appends the two lists to *outT and *outA,
// respectively.
//
// Type-error analyzers produce diagnostics that are redundant
// with type checker diagnostics, but more detailed (e.g. fixes).
// Rather than report two diagnostics for the same problem,
// we combine them by augmenting the type-checker diagnostic
// and discarding the analyzer diagnostic.
//
// If an analysis diagnostic has the same range and message as
// a list/parse/type diagnostic, the suggested fix information
// (et al) of the latter is merged into a copy of the former.
// This handles the case where a type-error analyzer suggests
// a fix to a type error, and avoids duplication.
//
// The use of out-slices, though irregular, allows the caller to
// easily choose whether to keep the results separate or combined.
//
// The arguments are not modified.
func combineDiagnostics(tdiags []*cache.Diagnostic, adiags []*cache.Diagnostic, outT, outA *[]*cache.Diagnostic) {

	// Build index of (list+parse+)type errors.
	type key struct {
		Range   protocol.Range
		message string
	}
	index := make(map[key]int) // maps (Range,Message) to index in tdiags slice
	for i, diag := range tdiags {
		index[key{diag.Range, diag.Message}] = i
	}

	// Filter out analysis diagnostics that match type errors,
	// retaining their suggested fix (etc) fields.
	for _, diag := range adiags {
		if i, ok := index[key{diag.Range, diag.Message}]; ok {
			copy := *tdiags[i]
			copy.SuggestedFixes = diag.SuggestedFixes
			copy.Tags = diag.Tags
			tdiags[i] = &copy
			continue
		}

		*outA = append(*outA, diag)
	}

	*outT = append(*outT, tdiags...)
}

// mustPublishDiagnostics marks the uri as needing publication, independent of
// whether the published contents have changed.
//
// This can be used for ensuring gopls publishes diagnostics after certain file
// events.
func (s *server) mustPublishDiagnostics(uri protocol.DocumentURI) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()

	if s.diagnostics[uri] == nil {
		s.diagnostics[uri] = new(fileDiagnostics)
	}
	s.diagnostics[uri].mustPublish = true
}

const WorkspaceLoadFailure = "Error loading workspace"

// updateCriticalErrorStatus updates the critical error progress notification
// based on err.
//
// If err is nil, or if there are no open files, it clears any existing error
// progress report.
func (s *server) updateCriticalErrorStatus(ctx context.Context, snapshot *cache.Snapshot, err *cache.InitializationError) {
	s.criticalErrorStatusMu.Lock()
	defer s.criticalErrorStatusMu.Unlock()

	// Remove all newlines so that the error message can be formatted in a
	// status bar.
	var errMsg string
	if err != nil {
		errMsg = strings.ReplaceAll(err.MainError.Error(), "\n", " ")
	}

	if s.criticalErrorStatus == nil {
		if errMsg != "" {
			event.Error(ctx, "errors loading workspace", err.MainError, snapshot.Labels()...)
			s.criticalErrorStatus = s.progress.Start(ctx, WorkspaceLoadFailure, errMsg, nil, nil)
		}
		return
	}

	// If an error is already shown to the user, update it or mark it as
	// resolved.
	if errMsg == "" {
		s.criticalErrorStatus.End(ctx, "Done.")
		s.criticalErrorStatus = nil
	} else {
		s.criticalErrorStatus.Report(ctx, errMsg, 0)
	}
}

// updateDiagnostics records the result of diagnosing a snapshot, and publishes
// any diagnostics that need to be updated on the client.
//
// The allViews argument should be the current set of views present in the
// session, for the purposes of trimming diagnostics produced by deleted views.
func (s *server) updateDiagnostics(ctx context.Context, allViews []*cache.View, snapshot *cache.Snapshot, diagnostics diagMap, final bool) {
	ctx, done := event.Start(ctx, "Server.publishDiagnostics")
	defer done()

	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()

	// Before updating any diagnostics, check that the context (i.e. snapshot
	// background context) is not cancelled.
	//
	// If not, then we know that we haven't started diagnosing the next snapshot,
	// because the previous snapshot is cancelled before the next snapshot is
	// returned from Invalidate.
	//
	// Therefore, even if we publish stale diagnostics here, they should
	// eventually be overwritten with accurate diagnostics.
	//
	// TODO(rfindley): refactor the API to force that snapshots are diagnosed
	// after they are created.
	if ctx.Err() != nil {
		return
	}

	viewMap := make(viewSet)
	for _, v := range allViews {
		viewMap[v] = unit{}
	}

	// updateAndPublish updates diagnostics for a file, checking both the latest
	// diagnostics for the current snapshot, as well as reconciling the set of
	// views.
	updateAndPublish := func(uri protocol.DocumentURI, f *fileDiagnostics, diags []*cache.Diagnostic) error {
		current, ok := f.byView[snapshot.View()]
		// Update the stored diagnostics if:
		//  1. we've never seen diagnostics for this view,
		//  2. diagnostics are for an older snapshot, or
		//  3. we're overwriting with final diagnostics
		//
		// In other words, we shouldn't overwrite existing diagnostics for a
		// snapshot with non-final diagnostics. This avoids the race described at
		// https://github.com/golang/go/issues/64765#issuecomment-1890144575.
		if !ok || current.snapshot < snapshot.SequenceID() || (current.snapshot == snapshot.SequenceID() && final) {
			fh, err := snapshot.ReadFile(ctx, uri)
			if err != nil {
				return err
			}
			current = viewDiagnostics{
				snapshot:    snapshot.SequenceID(),
				version:     fh.Version(),
				diagnostics: diags,
			}
			if f.byView == nil {
				f.byView = make(map[*cache.View]viewDiagnostics)
			}
			f.byView[snapshot.View()] = current
		}

		return s.publishFileDiagnosticsLocked(ctx, viewMap, uri, current.version, f)
	}

	seen := make(map[protocol.DocumentURI]bool)
	for uri, diags := range diagnostics {
		f, ok := s.diagnostics[uri]
		if !ok {
			f = new(fileDiagnostics)
			s.diagnostics[uri] = f
		}
		seen[uri] = true
		if err := updateAndPublish(uri, f, diags); err != nil {
			if ctx.Err() != nil {
				return
			} else {
				event.Error(ctx, "updateDiagnostics: failed to deliver diagnostics", err, tag.URI.Of(uri))
			}
		}
	}

	// TODO(rfindley): perhaps we should clean up files that have no diagnostics.
	// One could imagine a large operation generating diagnostics for a great
	// number of files, after which gopls has to do more bookkeeping into the
	// future.
	if final {
		for uri, f := range s.diagnostics {
			if !seen[uri] {
				if err := updateAndPublish(uri, f, nil); err != nil {
					if ctx.Err() != nil {
						return
					} else {
						event.Error(ctx, "updateDiagnostics: failed to deliver diagnostics", err, tag.URI.Of(uri))
					}
				}
			}
		}
	}
}

// updateOrphanedFileDiagnostics records and publishes orphaned file
// diagnostics as a given modification time.
func (s *server) updateOrphanedFileDiagnostics(ctx context.Context, modID uint64, diagnostics diagMap) error {
	views := s.session.Views()
	viewSet := make(viewSet)
	for _, v := range views {
		viewSet[v] = unit{}
	}

	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()

	for uri, diags := range diagnostics {
		f, ok := s.diagnostics[uri]
		if !ok {
			f = new(fileDiagnostics)
			s.diagnostics[uri] = f
		}
		if f.orphanedAt > modID {
			continue
		}
		f.orphanedAt = modID
		f.orphanedFileDiagnostics = diags
		// TODO(rfindley): the version of this file is potentially inaccurate;
		// nevertheless, it should be eventually consistent, because all
		// modifications are diagnosed.
		fh, err := s.session.ReadFile(ctx, uri)
		if err != nil {
			return err
		}
		if err := s.publishFileDiagnosticsLocked(ctx, viewSet, uri, fh.Version(), f); err != nil {
			return err
		}
	}

	// Clear any stale orphaned file diagnostics.
	for uri, f := range s.diagnostics {
		if f.orphanedAt < modID {
			f.orphanedFileDiagnostics = nil
		}
		fh, err := s.session.ReadFile(ctx, uri)
		if err != nil {
			return err
		}
		if err := s.publishFileDiagnosticsLocked(ctx, viewSet, uri, fh.Version(), f); err != nil {
			return err
		}
	}
	return nil
}

// publishFileDiagnosticsLocked publishes a fileDiagnostics value, while holding s.diagnosticsMu.
//
// If the publication succeeds, it updates f.publishedHash and f.mustPublish.
func (s *server) publishFileDiagnosticsLocked(ctx context.Context, views viewSet, uri protocol.DocumentURI, version int32, f *fileDiagnostics) error {
	// Check that the set of views is up-to-date, and de-dupe diagnostics
	// across views.
	var (
		diagHashes = make(map[file.Hash]unit) // unique diagnostic hashes
		hash       file.Hash                  // XOR of diagnostic hashes
		unique     []*cache.Diagnostic        // unique diagnostics
	)
	add := func(diag *cache.Diagnostic) {
		h := hashDiagnostic(diag)
		if _, ok := diagHashes[h]; !ok {
			diagHashes[h] = unit{}
			unique = append(unique, diag)
			hash.XORWith(h)
		}
	}
	for _, diag := range f.orphanedFileDiagnostics {
		add(diag)
	}
	for view, viewDiags := range f.byView {
		if _, ok := views[view]; !ok {
			delete(f.byView, view) // view no longer exists
			continue
		}
		if viewDiags.version != version {
			continue // a payload of diagnostics applies to a specific file version
		}
		for _, diag := range viewDiags.diagnostics {
			add(diag)
		}
	}
	sortDiagnostics(unique)

	// Publish, if necessary.
	if hash != f.publishedHash || f.mustPublish {
		if err := s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			Diagnostics: toProtocolDiagnostics(unique),
			URI:         uri,
			Version:     version,
		}); err != nil {
			return err
		}
		f.publishedHash = hash
		f.mustPublish = false
	}
	return nil
}

func toProtocolDiagnostics(diagnostics []*cache.Diagnostic) []protocol.Diagnostic {
	reports := []protocol.Diagnostic{}
	for _, diag := range diagnostics {
		pdiag := protocol.Diagnostic{
			// diag.Message might start with \n or \t
			Message:            strings.TrimSpace(diag.Message),
			Range:              diag.Range,
			Severity:           diag.Severity,
			Source:             string(diag.Source),
			Tags:               protocol.NonNilSlice(diag.Tags),
			RelatedInformation: diag.Related,
			Data:               diag.BundledFixes,
		}
		if diag.Code != "" {
			pdiag.Code = diag.Code
		}
		if diag.CodeHref != "" {
			pdiag.CodeDescription = &protocol.CodeDescription{Href: diag.CodeHref}
		}
		reports = append(reports, pdiag)
	}
	return reports
}

func (s *server) shouldIgnoreError(snapshot *cache.Snapshot, err error) bool {
	if err == nil { // if there is no error at all
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	// If the folder has no Go code in it, we shouldn't spam the user with a warning.
	// TODO(rfindley): surely it is not correct to walk the folder here just to
	// suppress diagnostics, every time we compute diagnostics.
	var hasGo bool
	_ = filepath.Walk(snapshot.Folder().Path(), func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		hasGo = true
		return errors.New("done")
	})
	return !hasGo
}
