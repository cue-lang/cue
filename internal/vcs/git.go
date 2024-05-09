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

package vcs

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type gitVCS struct {
	root   string
	subDir string
}

func newGitVCS(dir string) (VCS, error) {
	root := findRoot(dir, ".git")
	if root == "" {
		return nil, &vcsNotFoundError{
			kind: "git",
			dir:  dir,
		}
	}
	return gitVCS{
		root:   root,
		subDir: dir,
	}, nil
}

// Root implements [VCS.Root].
func (v gitVCS) Root() string {
	return v.root
}

// ListFiles implements [VCS.ListFiles].
func (v gitVCS) ListFiles(ctx context.Context, dir string) ([]string, error) {
	rel, err := filepath.Rel(v.root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("cannot list files from %q, outside VCS root %q", dir, v.root)
	}
	// TODO should we use --recurse-submodules?
	out, err := runCmd(ctx, dir, "git", "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	files := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	sort.Strings(files)
	return files, nil
}

// Status implements [VCS.Status].
func (v gitVCS) Status(ctx context.Context) (Status, error) {
	// We only care about the module's subdirectory status - if anything
	// else is dirty, it won't go into the module so we don't care.
	// TODO this will change if/when we include license files
	// from outside the module directory. It also over-reports dirtiness
	// because there might be nested modules that aren't included, but
	// are nonetheless included in the status check.
	out, err := runCmd(ctx, v.root, "git", "status", "--porcelain", v.subDir)
	if err != nil {
		return Status{}, err
	}
	uncommitted := len(out) > 0

	// "git status" works for empty repositories, but "git log" does not.
	// Assume there are no commits in the repo when "git log" fails with
	// uncommitted files and skip tagging revision / committime.
	var rev string
	var commitTime time.Time
	out, err = runCmd(ctx, v.root, "git",
		"-c", "log.showsignature=false",
		"log", "-1", "--format=%H:%ct",
	)
	if err != nil && !uncommitted {
		return Status{}, err
	}
	if err == nil {
		rev, commitTime, err = parseRevTime(out)
		if err != nil {
			return Status{}, err
		}
	}
	return Status{
		Revision:    rev,
		CommitTime:  commitTime,
		Uncommitted: uncommitted,
	}, nil
}

// parseRevTime parses commit details in "revision:seconds" format.
func parseRevTime(out string) (string, time.Time, error) {
	buf := strings.TrimSpace(out)

	rev, t, _ := strings.Cut(buf, ":")
	if rev == "" {
		return "", time.Time{}, fmt.Errorf("unrecognized VCS tool output %q", out)
	}

	secs, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("unrecognized VCS tool output: %v", err)
	}

	return rev, time.Unix(secs, 0), nil
}
