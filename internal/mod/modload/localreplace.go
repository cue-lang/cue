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

package modload

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/internal/mod/modfiledata"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// LocalReplacements handles resolution of local path replacements.
// It provides methods to resolve local paths (starting with ./ or ../)
// to absolute filesystem paths and to fetch module information from
// those local directories.
//
// A nil *LocalReplacements is valid and all methods are nil-safe,
// returning appropriate zero values or errors.
type LocalReplacements struct {
	mainModuleLoc module.SourceLoc
	replacements  map[string]modfiledata.Replacement
	resolvedRoot  string // Absolute OS path to the main module root, computed at creation time
}

// NewLocalReplacements creates a new LocalReplacements instance for the given
// main module location and replacement map. Returns (nil, nil) if there are no local
// path replacements (i.e., all replacements are remote module replacements).
//
// Returns an error if the main module location cannot be resolved to an absolute
// path (i.e., the filesystem doesn't implement OSRootFS and Dir is not absolute).
//
// The caller must not modify the replacements map after calling this function.
func NewLocalReplacements(mainModuleLoc module.SourceLoc, replacements map[string]modfiledata.Replacement) (*LocalReplacements, error) {
	hasLocal := false
	for _, r := range replacements {
		if r.LocalPath != "" {
			hasLocal = true
			break
		}
	}
	if !hasLocal {
		return nil, nil
	}

	// Resolve the root path at creation time to avoid fragile os.Getwd() calls later.
	var resolvedRoot string
	if osFS, ok := mainModuleLoc.FS.(module.OSRootFS); ok {
		// OSRootFS provides an absolute OS root path
		resolvedRoot = filepath.Join(osFS.OSRoot(), mainModuleLoc.Dir)
	} else if filepath.IsAbs(mainModuleLoc.Dir) {
		// Dir is already an absolute path
		resolvedRoot = mainModuleLoc.Dir
	} else {
		return nil, fmt.Errorf("cannot resolve local replacements: filesystem does not provide OS root and directory %q is not absolute", mainModuleLoc.Dir)
	}

	return &LocalReplacements{
		mainModuleLoc: mainModuleLoc,
		replacements:  replacements,
		resolvedRoot:  resolvedRoot,
	}, nil
}

// LocalPathFor returns the local path replacement for the given module path
// (which should include the major version, e.g., "example.com/foo@v0").
// Returns an empty string if no local replacement exists for the module.
func (lr *LocalReplacements) LocalPathFor(modulePath string) string {
	if lr == nil {
		return ""
	}
	if repl, ok := lr.replacements[modulePath]; ok && repl.LocalPath != "" {
		return repl.LocalPath
	}
	return ""
}

// ResolveToAbsPath resolves a local replacement path (e.g., "./local-dep" or
// "../sibling") to an absolute OS filesystem path, relative to the main module's
// location.
func (lr *LocalReplacements) ResolveToAbsPath(localPath string) (string, error) {
	if lr == nil {
		return "", fmt.Errorf("cannot resolve local path: no local replacements configured")
	}
	return filepath.Clean(filepath.Join(lr.resolvedRoot, localPath)), nil
}

// FetchSourceLoc returns a SourceLoc for a local path replacement.
// The returned SourceLoc points to the local directory and can be used
// to read module source files.
func (lr *LocalReplacements) FetchSourceLoc(localPath string) (module.SourceLoc, error) {
	absPath, err := lr.ResolveToAbsPath(localPath)
	if err != nil {
		return module.SourceLoc{}, err
	}

	// Validate the path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return module.SourceLoc{}, fmt.Errorf("replacement directory %q does not exist", localPath)
		}
		return module.SourceLoc{}, err
	}
	if !info.IsDir() {
		return module.SourceLoc{}, fmt.Errorf("replacement path %q is not a directory", localPath)
	}

	return module.SourceLoc{
		FS:  module.OSDirFS(absPath),
		Dir: ".",
	}, nil
}

// FetchRequirements reads the dependencies from a local module's cue.mod/module.cue
// file. Returns nil (not an error) if the local module has no module.cue file,
// indicating the module has no dependencies.
func (lr *LocalReplacements) FetchRequirements(localPath string) ([]module.Version, error) {
	absPath, err := lr.ResolveToAbsPath(localPath)
	if err != nil {
		return nil, err
	}

	// Read the module.cue file from the local path
	modFilePath := filepath.Join(absPath, "cue.mod", "module.cue")
	data, err := os.ReadFile(modFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No module.cue means the local module has no dependencies
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read module file from local replacement %q: %v", localPath, err)
	}

	mf, err := modfile.ParseNonStrict(data, modFilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from local replacement %q: %v", localPath, err)
	}

	return mf.DepVersions(), nil
}
