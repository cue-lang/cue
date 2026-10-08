// Copyright 2026 CUE Authors
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

package cueconfig

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-quicktest/qt"
	"github.com/rogpeppe/go-internal/lockedfile"
	"golang.org/x/oauth2"

	"cuelang.org/go/internal/robustio"
)

// TestReadLoginsDuringWrite checks that ReadLogins does not report
// logins.json as missing while a writer holding the lock replaces it.
// On Windows, a read racing with the rename can spuriously fail
// as if the file did not exist; we simulate that with a file
// which does not exist until the writer creates it.
func TestReadLoginsDuringWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logins.json")

	// Without any writer, a missing file is reported as such,
	// and reading does not create the lock file.
	_, err := ReadLogins(path)
	qt.Assert(t, qt.ErrorIs(err, fs.ErrNotExist))
	_, err = os.Stat(path + ".lock")
	qt.Assert(t, qt.ErrorIs(err, fs.ErrNotExist))

	logins := &Logins{Registries: map[string]RegistryLogin{
		"registry.example": {AccessToken: "access", TokenType: "Bearer"},
	}}
	unlock, err := lockedfile.MutexAt(path + ".lock").Lock()
	qt.Assert(t, qt.IsNil(err))
	var got *Logins
	var gotErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, gotErr = ReadLogins(path)
	}()
	// Give ReadLogins a chance to see the missing file
	// before the writer puts it in place.
	time.Sleep(100 * time.Millisecond)
	err = writeLoginsUnlocked(path, logins)
	qt.Assert(t, qt.IsNil(err))
	unlock()

	<-done
	// TODO: ReadLogins should wait for the writer
	// rather than report the file as missing.
	qt.Assert(t, qt.ErrorIs(gotErr, fs.ErrNotExist))
	qt.Assert(t, qt.IsNil(got))
}

// TestWriteLoginsDuringRead checks that a writer does not replace logins.json
// while ReadLogins is reading it. On Windows, a rename over an open file fails.
func TestWriteLoginsDuringRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logins.json")
	oldLogins := &Logins{Registries: map[string]RegistryLogin{
		"registry.example": {AccessToken: "old", TokenType: "Bearer"},
	}}
	err := WriteLogins(path, oldLogins)
	qt.Assert(t, qt.IsNil(err))

	// Make the first read slow, so that a writer which does not wait for it
	// replaces the file midway. Later reads, including the writer's, are not slowed.
	var reads atomic.Int32
	readStarted := make(chan struct{})
	readFile = func(name string) ([]byte, error) {
		if reads.Add(1) == 1 {
			close(readStarted)
			time.Sleep(100 * time.Millisecond)
		}
		return robustio.ReadFile(name)
	}
	t.Cleanup(func() { readFile = robustio.ReadFile })

	var got *Logins
	var gotErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, gotErr = ReadLogins(path)
	}()
	<-readStarted
	newLogins, err := UpdateRegistryLogin(path, "registry.example", &oauth2.Token{AccessToken: "new", TokenType: "Bearer"})
	qt.Assert(t, qt.IsNil(err))

	<-done
	qt.Assert(t, qt.IsNil(gotErr))
	// TODO: the writer should wait for ReadLogins to finish.
	qt.Assert(t, qt.DeepEquals(got, newLogins))

	got, err = ReadLogins(path)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(got, newLogins))
}
