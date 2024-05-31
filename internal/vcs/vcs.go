// Copyright 2024 CUE Authors
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

// Package vcs provides access to operations on the version control
// systems supported by the source field in module.cue.
package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// VCS provides the operations on a particular instance of a VCS.
type VCS interface {
	// Root returns the root of the directory controlled by
	// the VCS (e.g. the directory containing .git).
	Root() string

	// ListFiles returns a list of files tracked by VCS, rooted at dir. The
	// optional args determine what should be listed. If no args are provided,
	// then all of the files under VCS control under dir are returned. It us up
	// to the caller to ensure that dir and args are contained by the VCS root
	// (some VCS implementations might return an error if an arg is provided
	// that is outside the VCS root). Filepaths are relative to dir and returned
	// in lexical order. They do not include directory names.
	//
	// Note that ListFiles is generally silent in the case an arg is provided
	// that does correspond to a VCS-controlled file. For example, calling
	// with an arg of "BANANA" where no such file is controlled by VCS will
	// result in no filepaths being returned.
	ListFiles(ctx context.Context, dir string, args ...string) ([]string, error)

	// Status returns the current state of the repository holding the given args
	// (files or directories). If args is not provided it implies the state of
	// the VCS repository in its entirety, including untracked files.
	Status(ctx context.Context, args ...string) (Status, error)
}

// Status is the current state of a local repository.
type Status struct {
	Revision    string    // Optional.
	CommitTime  time.Time // Optional.
	Uncommitted bool      // Required.
}

var vcsTypes = map[string]func(dir string) (VCS, error){
	"git": newGitVCS,
}

// New returns a new VCS value representing the
// version control system of the given type that
// controls the given directory.
//
// Status checks apply only to the given directory; other
// directories controlled by the VCS will not be considered.
//
// It returns an error if a VCS of the specified type
// cannot be found.
func New(vcsType string, dir string) (VCS, error) {
	vf := vcsTypes[vcsType]
	if vf == nil {
		return nil, fmt.Errorf("unrecognized VCS type %q", vcsType)
	}
	return vf(dir)
}

// findRoot inspects dir and its parents to find the VCS repository
// signified the presence of one of the given root names.
//
// If no repository is found, findRoot returns the empty string.
func findRoot(dir string, rootNames ...string) string {
	dir = filepath.Clean(dir)
	for {
		if isVCSRoot(dir, rootNames) {
			return dir
		}
		ndir := filepath.Dir(dir)
		if len(ndir) >= len(dir) {
			break
		}
		dir = ndir
	}
	return ""
}

// isVCSRoot identifies a VCS root by checking whether the directory contains
// any of the listed root names.
func isVCSRoot(dir string, rootNames []string) bool {
	for _, root := range rootNames {
		if _, err := os.Stat(filepath.Join(dir, root)); err == nil {
			// TODO return false if it's not the expected file type.
			// For now, this is only used by git which can use both
			// files and directories, so we'll allow either.
			return true
		}
	}
	return false
}

func runCmd(ctx context.Context, dir string, cmdName string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = dir

	fmt.Printf(">> dir: %s, %v\n", cmd.Dir, cmd)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running %q %q: %v", cmdName, args, err)
	}
	return string(out), nil
}

type vcsNotFoundError struct {
	kind string
	dir  string
}

func (e *vcsNotFoundError) Error() string {
	return fmt.Sprintf("%s VCS not found in any parent of %q", e.kind, e.dir)
}
