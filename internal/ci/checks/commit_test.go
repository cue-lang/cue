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
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestCommits(t *testing.T) {
	// We are removing the dependency on bash very soon.
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("cannot find bash: %v", err)
	}
	if runtime.GOOS != "linux" {
		t.Skipf("running only on Linux as others may ship older Bash")
	}

	scriptPath, err := filepath.Abs("commit.sh")
	qt.Assert(t, qt.IsNil(err))

	archive, err := txtar.ParseFile("testdata/checks.txtar")
	qt.Assert(t, qt.IsNil(err))
	archiveFS, err := txtar.FS(archive)
	qt.Assert(t, qt.IsNil(err))

	setupCommit := func(t *testing.T, name string) string {
		commit, err := fs.ReadFile(archiveFS, name)
		qt.Assert(t, qt.IsNil(err))

		t.Logf("commit:\n%s", commit)

		dir := t.TempDir()
		mustRunCmd(t, dir, "git", "init")
		mustRunCmd(t, dir, "git",
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
			cmd := exec.Command("bash", scriptPath)
			cmd.Dir = dir
			data, err := cmd.CombinedOutput()
			t.Logf("error: %v", err)
			qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %q", data))
		})
	}

	failFiles, err := fs.Glob(archiveFS, "fail-*")
	qt.Assert(t, qt.IsNil(err))
	for _, name := range failFiles {
		t.Run(name, func(t *testing.T) {
			dir := setupCommit(t, name)
			cmd := exec.Command("bash", scriptPath)
			cmd.Dir = dir
			err = cmd.Run()
			t.Logf("error: %v", err)
			qt.Assert(t, qt.IsNotNil(err))
		})
	}
}

func mustRunCmd(t *testing.T, dir string, exe string, args ...string) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	data, err := cmd.CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %q", data))
}
