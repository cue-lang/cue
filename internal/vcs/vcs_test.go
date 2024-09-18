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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestGit(t *testing.T) {
	skipIfNoExecutable(t, "git")
	ctx := context.Background()
	dir := t.TempDir()

	testFS, err := txtar.FS(txtar.Parse([]byte(`
-- subdir/foo --
-- subdir/bar/baz --
-- bar.txt --
-- baz/something --
`)))
	qt.Assert(t, qt.IsNil(err))
	err = copyFS(dir, testFS)
	qt.Assert(t, qt.IsNil(err))

	// In the tests that follow, we are testing the scenario where a module is
	// present in $dir/subdir (the VCS is rooted at $dir). cue/load or similar
	// would establish the absolute path $dir/subdir is the CUE module root, and
	// as such we use that absolute path as an argument in the calls to the VCS
	// implementation.
	subdir := filepath.Join(dir, "subdir")

	_, err = New("git", subdir)
	qt.Assert(t, qt.ErrorMatches(err, `git VCS not found in any parent of ".+"`))

	env := TestEnv()
	mustRunCmd(t, dir, env, "git", "init")
	v, err := New("git", subdir)
	qt.Assert(t, qt.IsNil(err))

	// The status shows that we have uncommitted files
	// because we haven't yet added the files after doing
	// git init.
	statusuncommitted, err := v.Status(ctx, subdir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(statusuncommitted.Uncommitted))

	mustRunCmd(t, dir, env, "git", "add", ".")
	statusuncommitted, err = v.Status(ctx, subdir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(statusuncommitted.Uncommitted))

	commitTime := time.Now().Truncate(time.Second)
	mustRunCmd(t, dir, env, "git",
		"-c", "user.email=cueckoo@gmail.com",
		"-c", "user.name=cueckoo",
		"commit", "-m", "something",
	)
	status, err := v.Status(ctx, subdir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsFalse(status.Uncommitted))
	qt.Assert(t, qt.IsTrue(!status.CommitTime.Before(commitTime)))
	qt.Assert(t, qt.Matches(status.Revision, `[0-9a-f]+`))

	// Test various permutations of ListFiles
	var files []string
	allFiles := []string{
		"bar.txt",
		"baz/something",
		"subdir/bar/baz",
		"subdir/foo",
	}

	// Empty dir implies repo root, i.e. all files
	files, err = v.ListFiles(ctx, "")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, allFiles))

	// Explicit repo root
	files, err = v.ListFiles(ctx, dir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, allFiles))

	// Relative path file under repo root
	files, err = v.ListFiles(ctx, dir, "bar.txt")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, []string{"bar.txt"}))

	// Absolute path file under repo root
	files, err = v.ListFiles(ctx, dir, filepath.Join(dir, "bar.txt"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, []string{"bar.txt"}))

	// Relative path sub directory listed from root
	files, err = v.ListFiles(ctx, dir, "subdir")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, []string{
		"subdir/bar/baz",
		"subdir/foo",
	}))

	// Absolute path sub directory listed from root
	files, err = v.ListFiles(ctx, dir, filepath.Join(dir, "subdir"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, []string{
		"subdir/bar/baz",
		"subdir/foo",
	}))

	// Listing of files in sub directory
	files, err = v.ListFiles(ctx, subdir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, []string{
		"bar/baz",
		"foo",
	}))

	// Change a file that's not in subdir. The status in subdir should remain
	// the same.
	err = os.WriteFile(filepath.Join(dir, "bar.txt"), []byte("something else"), 0o666)
	qt.Assert(t, qt.IsNil(err))
	statuschanged, err := v.Status(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(statuschanged.Uncommitted))
	status1, err := v.Status(ctx, subdir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(status1, status))

	// Restore the file and ensure Status is clean
	err = os.WriteFile(filepath.Join(dir, "bar.txt"), nil, 0o666)
	qt.Assert(t, qt.IsNil(err))
	files, err = v.ListFiles(ctx, dir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, allFiles))
	status2, err := v.Status(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(status2, status))

	// Add an untracked file
	untracked := filepath.Join(dir, "untracked")
	err = os.WriteFile(untracked, nil, 0666)
	qt.Assert(t, qt.IsNil(err))
	files, err = v.ListFiles(ctx, dir) // Does not include untracked file
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, allFiles))
	statusuntracked, err := v.Status(ctx) // Status does now show uncommitted changes
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(statusuntracked.Uncommitted))

	// Remove the untracked file and ensure Status is clean
	err = os.Remove(untracked)
	qt.Assert(t, qt.IsNil(err))
	files, err = v.ListFiles(ctx, dir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, allFiles))
	status3, err := v.Status(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(status3, status))

	// // Remove a tracked file so that it is now "missing"
	err = os.Remove(filepath.Join(dir, "bar.txt"))
	qt.Assert(t, qt.IsNil(err))
	files, err = v.ListFiles(ctx, dir) // Still reports "missing" file
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(files, allFiles))
	statusmissing, err := v.Status(ctx) // Status does now show uncommitted changes
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(statusmissing.Uncommitted))
}

func mustRunCmd(t *testing.T, dir string, env []string, exe string, args ...string) {
	c := exec.Command(exe, args...)
	c.Dir = dir
	c.Env = env
	data, err := c.CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %q", data))
}

func skipIfNoExecutable(t *testing.T, exeName string) {
	if _, err := exec.LookPath(exeName); err != nil {
		t.Skipf("cannot find %q executable: %v", exeName, err)
	}
}
