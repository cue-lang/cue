// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sort"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/golangorgx/gopls/cache/metadata"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/util/immutable"
	"cuelang.org/go/internal/golangorgx/gopls/util/pathutil"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"golang.org/x/tools/go/packages"
)

var loadID uint64 // atomic identifier for loads

// errNoInstances indicates that a load query returned no instances.
var errNoInstances = errors.New("no instances returned")

// load calls packages.Load for the given scopes, updating package metadata,
// import graph, and mapped files with the result.
//
// The resulting error may wrap the moduleErrorMap error type, representing
// errors associated with specific modules.
//
// If scopes contains a file scope there must be exactly one scope.
func (s *Snapshot) load(ctx context.Context, allowNetwork bool, scopes ...loadScope) (err error) {
	//id := atomic.AddUint64(&loadID, 1)
	//eventName := fmt.Sprintf("go/packages.Load #%d", id) // unique name for logging

	ctx, done := event.Start(ctx, "cache.snapshot.load")
	defer done()

	var insts []*build.Instance

	overlays := s.buildOverlay()

	for _, scope := range scopes {
		switch scope := scope.(type) {
		case fileLoadScope:
			uri := protocol.DocumentURI(scope)
			fh := s.FindFile(uri)
			if fh == nil || s.FileKind(fh) != file.CUE {
				// Don't try to load a file that doesn't exist, or isn't a go file.
				continue
			}
			_, err := fh.Content()
			if err != nil {
				continue
			}
			cfg := &load.Config{
				Dir:     filepath.Dir(uri.Path()),
				Overlay: overlays,
			}
			// We use ./... because we want to load all instances that
			// involve this particular file. This will include instances
			// from child directories that use the same package as our
			// file.
			//
			// TODO(ms): work out whether we can use the cfg.Package
			// field to limit us to a single package, and thus do less
			// work.
			insts = append(insts, load.Instances([]string{"./..."}, cfg)...)

		case packageLoadScope:
			cfg := &load.Config{
				Dir:     s.Folder().Path(),
				Overlay: overlays,
			}
			insts = append(insts, load.Instances([]string{string(scope)}, cfg)...)

		case moduleLoadScope:
			cfg := &load.Config{
				Module:  scope.modulePath,
				Package: "*",
				Dir:     scope.dir,
				Overlay: overlays,
			}
			insts = append(insts, load.Instances([]string{"./..."}, cfg)...)

		case viewLoadScope:
			// We're loading the workspace
			cfg := &load.Config{
				Package: "*",
				Dir:     s.Folder().Path(),
				Overlay: overlays,
			}
			// TODO(ms): In gopls, if the view is adhoc mode, here they
			// only load the directory and not ./... Why?!
			insts = append(insts, load.Instances([]string{"./..."}, cfg)...)

		default:
			panic(fmt.Sprintf("unknown scope type %T", scope))
		}
	}

	if len(insts) == 0 {
		return errNoInstances
	}

	byImportPath := make(map[ImportPath]*build.Instance)
	for _, inst := range insts {
		buildMetadata(byImportPath, inst)
	}

	s.mu.Lock()

	var files []protocol.DocumentURI // files to preload
	seenFiles := make(map[protocol.DocumentURI]struct{})
	updates := maps.Clone(byImportPath)
	for path, inst := range byImportPath {
		if _, exists := s.meta.Packages[metadata.ImportPath(inst.ImportPath)]; exists {
			delete(updates, path)
			continue
		}
		for _, path := range inst.BuildFiles {
			uri := protocol.URIFromPath(path.Filename)
			if _, seen := seenFiles[uri]; seen {
				continue
			}
			seenFiles[uri] = struct{}{}
			files = append(files, uri)
		}
		s.shouldLoad.Delete(ImportPath(inst.ImportPath))
	}

	s.meta = s.meta.Update(updates)

	s.mu.Unlock()

	s.preloadFiles(ctx, files)

	return nil
}

func buildMetadata(byImportPath map[ImportPath]*build.Instance, inst *build.Instance) {
	// Note that in cue, it is not considered that a child package
	// "imports" an ancestor package with the same package
	// name. E.g. cue/load.Instances of an importPath "foo/bar:x" will
	// automatically find and include "foo:x" if it exists. Further, we
	// will find the cue files from the parent directory in the
	// child-instance's BuildFiles. But it is not the case that the
	// parent package will appear in the child's Imports.
	importPath := ImportPath(inst.ImportPath)
	if _, seen := byImportPath[importPath]; seen {
		// think: diamond imports - A imports B, A imports C. B imports
		// D, C imports D. We'll see D twice then.
		return
	}
	byImportPath[importPath] = inst
	for _, inst := range inst.Imports {
		buildMetadata(byImportPath, inst)
	}
}

type moduleErrorMap struct {
	errs map[string][]packages.Error // module path -> errors
}

func (m *moduleErrorMap) Error() string {
	var paths []string // sort for stability
	for path, errs := range m.errs {
		if len(errs) > 0 { // should always be true, but be cautious
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d modules have errors:\n", len(paths))
	for _, path := range paths {
		fmt.Fprintf(&buf, "\t%s:%s\n", path, m.errs[path][0].Msg)
	}

	return buf.String()
}

// isWorkspacePackageLocked reports whether p is a workspace package for the
// snapshot s.
//
// Workspace packages are packages that we consider the user to be actively
// working on. As such, they are re-diagnosed on every keystroke, and searched
// for various workspace-wide queries such as references or workspace symbols.
//
// See the commentary inline for a description of the workspace package
// heuristics.
//
// s.mu must be held while calling this function.
func isWorkspacePackageLocked(s *Snapshot, meta *metadata.Graph, inst *build.Instance) bool {
	if metadata.IsCommandLineArguments(metadata.ImportPath(inst.ImportPath)) {
		// TODO(ms) original is more complex; decide what we're doing about commandlineargs in general.
		return false
	}

	// Apply filtering logic.
	//
	// Workspace packages must contain at least one non-filtered file.
	filterFunc := s.view.filterFunc()
	uris := make(map[protocol.DocumentURI]unit) // filtered package URIs
	for _, file := range inst.BuildFiles {
		uri := protocol.URIFromPath(file.Filename)
		if !filterFunc(uri) {
			uris[uri] = struct{}{}
		}
	}
	if len(uris) == 0 {
		return false // no non-filtered files
	}

	// For non-module views (of type GOPATH or AdHoc), or if
	// expandWorkspaceToModule is unset, workspace packages must be contained in
	// the workspace folder.
	//
	// For module views (of type GoMod or GoWork), packages must in any case be
	// in a workspace module (enforced below).
	if !s.view.moduleMode() || !s.Options().ExpandWorkspaceToModule {
		folder := s.view.folder.Dir.Path()
		inFolder := false
		for uri := range uris {
			if pathutil.InDir(folder, uri.Path()) {
				inFolder = true
				break
			}
		}
		if !inFolder {
			return false
		}
	}

	// In module mode, a workspace package must be contained in a workspace
	// module.
	if s.view.moduleMode() {
		if inst.Module == "" {
			return false
		}
		modURI := protocol.URIFromPath(inst.Root)
		_, ok := s.view.workspaceModFiles[modURI]
		return ok
	}

	return true // an ad-hoc package or GOPATH package
}

// containsOpenFileLocked reports whether any file referenced by inst
// is open in the snapshot s.
//
// s.mu must be held while calling this function.
func containsOpenFileLocked(s *Snapshot, inst *build.Instance) bool {
	for _, file := range inst.BuildFiles {
		fh, _ := s.files.get(protocol.URIFromPath(file.Filename))
		if _, open := fh.(*overlay); open {
			return true
		}
	}
	return false
}

// computeWorkspacePackagesLocked computes workspace packages in the
// snapshot s for the given metadata graph. The result does not
// contain intermediate test variants.
//
// s.mu must be held while calling this function.
func computeWorkspacePackagesLocked(s *Snapshot, meta *metadata.Graph) immutable.Map[ImportPath, unit] {
	workspacePackages := make(map[ImportPath]unit)
	for _, inst := range meta.Packages {
		if !isWorkspacePackageLocked(s, meta, inst) {
			continue
		}

		workspacePackages[ImportPath(inst.ImportPath)] = unit{}
	}
	return immutable.MapOf(workspacePackages)
}

// allFilesHaveRealPackages reports whether all files referenced by inst are
// contained in a "real" package (not command-line-arguments).
//
// If inst is valid but all "real" packages containing any file are invalid, this
// function returns false.
//
// If inst is not a command-line-arguments package, this is trivially true.
func allFilesHaveRealPackages(g *metadata.Graph, inst *build.Instance) bool {
checkURIs:
	for _, file := range inst.BuildFiles {
		for _, pkgPath := range g.FilesToPackage[protocol.URIFromPath(file.Filename)] {
			if !metadata.IsCommandLineArguments(pkgPath) {
				continue checkURIs
			}
		}
		return false
	}
	return true
}
