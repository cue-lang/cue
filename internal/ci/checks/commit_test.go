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

package main

import (
	"io/fs"
	"os/exec"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/vcs"
)

func TestCommits(t *testing.T) {
	archive, err := txtar.ParseFile("testdata/checks.txtar")
	qt.Assert(t, qt.IsNil(err))
	archiveFS, err := txtar.FS(archive)
	qt.Assert(t, qt.IsNil(err))

	setupCommit := func(t *testing.T, name string) string {
		commit, err := fs.ReadFile(archiveFS, name)
		qt.Assert(t, qt.IsNil(err))

		t.Logf("commit:\n%s", commit)

		dir := t.TempDir()
		env := vcs.TestEnv()
		mustRunCmd(t, dir, env, "git", "init")
		mustRunCmd(t, dir, env, "git",
			"-c", "user.email=cueckoo@gmail.com",
			"-c", "user.name=cueckoo",
			"commit", "--allow-empty", "-m", string(commit),
		)
		return dir
	}

	passFiles, err := fs.Glob(archiveFS, "pass-*")
	qt.Assert(t, qt.IsNil(err))
	for _, name := range passFiles {
		t.Run(name, func(t *testing.T) {
			dir := setupCommit(t, name)
			err = checkCommit(dir)
			t.Logf("error: %v", err)
			qt.Assert(t, qt.IsNil(err))
		})
	}

	failFiles, err := fs.Glob(archiveFS, "fail-*")
	qt.Assert(t, qt.IsNil(err))
	for _, name := range failFiles {
		t.Run(name, func(t *testing.T) {
			dir := setupCommit(t, name)
			err = checkCommit(dir)
			t.Logf("error: %v", err)
			qt.Assert(t, qt.IsNotNil(err))
		})
	}
}

func mustRunCmd(t *testing.T, dir string, env []string, exe string, args ...string) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = env
	data, err := cmd.CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %q", data))
}
