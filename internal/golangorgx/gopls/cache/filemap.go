// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"path/filepath"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/util/persistent"
)

// A fileMap maps files in the snapshot, with some additional bookkeeping:
// It keeps track of overlays as well as directories containing any observed
// file.
type fileMap struct {
	files    *persistent.Map[protocol.DocumentURI, file.Handle]
	overlays *persistent.Map[protocol.DocumentURI, *overlay] // the subset of files that are overlays
	dirs     *persistent.Set[string]                         // all dirs containing files; if nil, dirs have not been initialized
}

func newFileMap() *fileMap {
	return &fileMap{
		files:    new(persistent.Map[protocol.DocumentURI, file.Handle]),
		overlays: new(persistent.Map[protocol.DocumentURI, *overlay]),
		dirs:     new(persistent.Set[string]),
	}
}

// clone creates a copy of the fileMap, incorporating the changes specified by
// the changes map.
func (m *fileMap) clone(changes map[protocol.DocumentURI]file.Handle) *fileMap {
	m2 := &fileMap{
		files:    m.files.Clone(),
		overlays: m.overlays.Clone(),
	}
	if m.dirs != nil {
		m2.dirs = m.dirs.Clone()
	}

	// Handle file changes.
	//
	// Note, we can't simply delete the file unconditionally and let it be
	// re-read by the snapshot, as (1) the snapshot must always observe all
	// overlays, and (2) deleting a file forces directories to be reevaluated, as
	// it may be the last file in a directory. We want to avoid that work in the
	// common case where a file has simply changed.
	//
	// For that reason, we also do this in two passes, processing deletions
	// first, as a set before a deletion would result in pointless work.
	for uri, fh := range changes {
		if !fileExists(fh) {
			m2.delete(uri)
		}
	}
	for uri, fh := range changes {
		if fileExists(fh) {
			m2.set(uri, fh)
		}
	}
	return m2
}

func (m *fileMap) destroy() {
	m.files.Destroy()
	m.overlays.Destroy()
	if m.dirs != nil {
		m.dirs.Destroy()
	}
}

// get returns the file handle mapped by the given key, or (nil, false) if the
// key is not present.
func (m *fileMap) get(key protocol.DocumentURI) (file.Handle, bool) {
	return m.files.Get(key)
}

// foreach calls f for each (uri, fh) in the map.
func (m *fileMap) foreach(f func(uri protocol.DocumentURI, fh file.Handle)) {
	m.files.Range(f)
}

// set stores the given file handle for key, updating overlays and directories
// accordingly.
func (m *fileMap) set(key protocol.DocumentURI, fh file.Handle) {
	m.files.Set(key, fh, nil)

	// update overlays
	if o, ok := fh.(*overlay); ok {
		m.overlays.Set(key, o, nil)
	} else {
		// Setting a non-overlay must delete the corresponding overlay, to preserve
		// the accuracy of the overlay set.
		m.overlays.Delete(key)
	}

	// update dirs, if they have been computed
	if m.dirs != nil {
		m.addDirs(key)
	}
}

// addDirs adds all directories containing u to the dirs set.
func (m *fileMap) addDirs(u protocol.DocumentURI) {
	dir := filepath.Dir(u.Path())
	for dir != "" && !m.dirs.Contains(dir) {
		m.dirs.Add(dir)
		dir = filepath.Dir(dir)
	}
}

// delete removes a file from the map, and updates overlays and dirs
// accordingly.
func (m *fileMap) delete(key protocol.DocumentURI) {
	m.files.Delete(key)
	m.overlays.Delete(key)

	// Deleting a file may cause the set of dirs to shrink; therefore we must
	// re-evaluate the dir set.
	//
	// Do this lazily, to avoid work if there are multiple deletions in a row.
	if m.dirs != nil {
		m.dirs.Destroy()
		m.dirs = nil
	}
}

// getOverlays returns a new unordered array of overlay files.
func (m *fileMap) getOverlays() []*overlay {
	var overlays []*overlay
	m.overlays.Range(func(_ protocol.DocumentURI, o *overlay) {
		overlays = append(overlays, o)
	})
	return overlays
}

// getDirs reports returns the set of dirs observed by the fileMap.
//
// This operation mutates the fileMap.
// The result must not be mutated by the caller.
func (m *fileMap) getDirs() *persistent.Set[string] {
	if m.dirs == nil {
		m.dirs = new(persistent.Set[string])
		m.files.Range(func(u protocol.DocumentURI, _ file.Handle) {
			m.addDirs(u)
		})
	}
	return m.dirs
}
