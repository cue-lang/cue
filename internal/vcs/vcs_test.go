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
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/txtarfs"
	"golang.org/x/tools/txtar"
)

var testFS = txtarfs.FS(txtar.Parse([]byte(`
-- subdir/foo --
-- subdir/bar/baz --
-- bar.txt --
-- baz/something --
`)))

func TestGit(t *testing.T) {
	skipIfNoExecutable(t, "git")
	ctx := context.Background()
	dir := t.TempDir()
	err := copyFS(dir, testFS)
	qt.Assert(t, qt.IsNil(err))

	_, err = New("git", filepath.Join(dir, "subdir"))
	qt.Assert(t, qt.ErrorMatches(err, `git VCS not found in any parent of ".+"`))

	mustRunCmd(t, dir, "git", "init")
	v, err := New("git", filepath.Join(dir, "subdir"))
	qt.Assert(t, qt.IsNil(err))
	status, err := v.Status(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(status.Uncommitted))

	mustRunCmd(t, dir, "git", "add", ".")
	status, err = v.Status(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(status.Uncommitted))

	commitTime := time.Now().Truncate(time.Second)
	mustRunCmd(t, dir, "git",
		"-c", "user.email=cueckoo@gmail.com",
		"-c", "user.name=cueckoo",
		"commit", "-m", "something",
	)
	status, err = v.Status(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsFalse(status.Uncommitted))
	qt.Assert(t, qt.IsTrue(!status.CommitTime.Before(commitTime)))
	qt.Assert(t, qt.Matches(status.Revision, `[0-9a-f]+`))
	files, err := v.ListFiles(ctx, filepath.Join(dir, "subdir"))
	qt.Assert(t, qt.DeepEquals(files, []string{
		"bar/baz",
		"foo",
	}))
	files, err = v.ListFiles(ctx, dir)
	qt.Assert(t, qt.DeepEquals(files, []string{
		"bar.txt",
		"baz/something",
		"subdir/bar/baz",
		"subdir/foo",
	}))
}

func mustRunCmd(t *testing.T, dir string, exe string, args ...string) {
	c := exec.Command(exe, args...)
	c.Dir = dir
	data, err := c.CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %q", data))
}

func skipIfNoExecutable(t *testing.T, exeName string) {
	if _, err := exec.LookPath(exeName); err != nil {
		t.Skipf("cannot find %q executable: %v", exeName, err)
	}
}
