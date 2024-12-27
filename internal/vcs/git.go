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
	"slices"
	"strconv"
	"strings"
	"time"
)

type gitVCS struct {
	root string
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
		root: root,
	}, nil
}

// Root implements [VCS.Root].
func (v gitVCS) Root() string {
	return v.root
}

// fixDir adjusts dir according to the semantics described in [VCS.ListFiles].
func fixDir(v VCS, dir string) string {
	if dir == "" {
		return v.Root()
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(v.Root(), dir)
	}
	return dir
}

// ListFiles implements [VCS.ListFiles].
func (v gitVCS) ListFiles(ctx context.Context, dir string, paths ...string) ([]string, error) {
	dir = fixDir(v, dir)

	// TODO should we use --recurse-submodules?
	gitargs := append([]string{"ls-files", "-z", "--"}, paths...)
	out, err := runCmd(ctx, dir, "git", gitargs...)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSuffix(out, "\x00")
	if out == "" {
		return nil, nil
	}
	files := strings.Split(out, "\x00")
	slices.Sort(files)
	return files, nil
}

// Status implements [VCS.Status].
func (v gitVCS) Status(ctx context.Context, paths ...string) (Status, error) {
	gitargs := append([]string{"status", "--porcelain", "--"}, paths...)
	out, err := runCmd(ctx, v.root, "git", gitargs...)
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
