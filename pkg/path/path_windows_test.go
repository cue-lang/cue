// Copyright 2020 CUE Authors
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

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package path

import (
	"flag"
	"fmt"
	"io/ioutil"
	goos "os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestWinSplitListTestsAreValid(t *testing.T) {
	comspec := goos.Getenv("ComSpec")
	if comspec == "" {
		t.Fatal("%ComSpec% must be set")
	}

	for ti, tt := range winsplitlisttests {
		testWinSplitListTestIsValid(t, ti, tt, comspec)
	}
}

func testWinSplitListTestIsValid(t *testing.T, ti int, tt SplitListTest,
	comspec string) {

	const (
		cmdfile = `printdir.cmd`
		perm    = 0700
	)

	tmp := t.TempDir()

	for i, d := range tt.result {
		if d == "" {
			continue
		}
		if cd := Clean(d, Windows); VolumeName(cd, Windows) != "" ||
			cd[0] == '\\' || cd == ".." || (len(cd) >= 3 && cd[0:3] == `..\`) {
			t.Errorf("%d,%d: %#q refers outside working directory", ti, i, d)
			return
		}
		dd := Join([]string{tmp, d}, Windows)
		if _, err := goos.Stat(dd); err == nil {
			t.Errorf("%d,%d: %#q already exists", ti, i, d)
			return
		}
		if err := goos.MkdirAll(dd, perm); err != nil {
			t.Errorf("%d,%d: MkdirAll(%#q) failed: %v", ti, i, dd, err)
			return
		}
		fn, data := Join([]string{dd, cmdfile}, Windows), []byte("@echo "+d+"\r\n")
		if err := ioutil.WriteFile(fn, data, perm); err != nil {
			t.Errorf("%d,%d: WriteFile(%#q) failed: %v", ti, i, fn, err)
			return
		}
	}

	// on some systems, SystemRoot is required for cmd to work
	systemRoot := goos.Getenv("SystemRoot")

	for i, d := range tt.result {
		if d == "" {
			continue
		}
		exp := []byte(d + "\r\n")
		cmd := &exec.Cmd{
			Path: comspec,
			Args: []string{`/c`, cmdfile},
			Env:  []string{`Path=` + systemRoot + "/System32;" + tt.list, `SystemRoot=` + systemRoot},
			Dir:  tmp,
		}
		out, err := cmd.CombinedOutput()
		switch {
		case err != nil:
			t.Errorf("%d,%d: execution error %v\n%q", ti, i, err, out)
			return
		case !reflect.DeepEqual(out, exp):
			t.Errorf("%d,%d: expected %#q, got %#q", ti, i, exp, out)
			return
		default:
			// unshadow cmdfile in next directory
			err = goos.Remove(Join([]string{tmp, d, cmdfile}, Windows))
			if err != nil {
				t.Fatalf("Remove test command failed: %v", err)
			}
		}
	}
}

// checkVolume8dot3Setting runs "fsutil 8dot3name query c:" command
// (where c: is vol parameter) to discover "8dot3 name creation state".
// The state is combination of 2 flags. The global flag controls if it
// is per volume or global setting:
//
//	0 - Enable 8dot3 name creation on all volumes on the system
//	1 - Disable 8dot3 name creation on all volumes on the system
//	2 - Set 8dot3 name creation on a per volume basis
//	3 - Disable 8dot3 name creation on all volumes except the system volume
//
// If global flag is set to 2, then per-volume flag needs to be examined:
//
//	0 - Enable 8dot3 name creation on this volume
//	1 - Disable 8dot3 name creation on this volume
//
// checkVolume8dot3Setting verifies that "8dot3 name creation" flags
// are set to 2 and 0, if enabled parameter is true, or 2 and 1, if enabled
// is false. Otherwise checkVolume8dot3Setting returns error.
func checkVolume8dot3Setting(vol string, enabled bool) error {
	// It appears, on some systems "fsutil 8dot3name query ..." command always
	// exits with error. Ignore exit code, and look at fsutil output instead.
	out, _ := exec.Command("fsutil", "8dot3name", "query", vol).CombinedOutput()
	// Check that system has "Volume level setting" set.
	expected := "The registry state of NtfsDisable8dot3NameCreation is 2, the default (Volume level setting)"
	if !strings.Contains(string(out), expected) {
		// Windows 10 version of fsutil has different output message.
		expectedWindow10 := "The registry state is: 2 (Per volume setting - the default)"
		if !strings.Contains(string(out), expectedWindow10) {
			return fmt.Errorf("fsutil output should contain %q, but is %q", expected, string(out))
		}
	}
	// Now check the volume setting.
	expected = "Based on the above two settings, 8dot3 name creation is %s on %s"
	if enabled {
		expected = fmt.Sprintf(expected, "enabled", vol)
	} else {
		expected = fmt.Sprintf(expected, "disabled", vol)
	}
	if !strings.Contains(string(out), expected) {
		return fmt.Errorf("unexpected fsutil output: %q", string(out))
	}
	return nil
}

func setVolume8dot3Setting(vol string, enabled bool) error {
	cmd := []string{"fsutil", "8dot3name", "set", vol}
	if enabled {
		cmd = append(cmd, "0")
	} else {
		cmd = append(cmd, "1")
	}
	// It appears, on some systems "fsutil 8dot3name set ..." command always
	// exits with error. Ignore exit code, and look at fsutil output instead.
	out, _ := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if string(out) != "\r\nSuccessfully set 8dot3name behavior.\r\n" {
		// Windows 10 version of fsutil has different output message.
		expectedWindow10 := "Successfully %s 8dot3name generation on %s\r\n"
		if enabled {
			expectedWindow10 = fmt.Sprintf(expectedWindow10, "enabled", vol)
		} else {
			expectedWindow10 = fmt.Sprintf(expectedWindow10, "disabled", vol)
		}
		if string(out) != expectedWindow10 {
			return fmt.Errorf("%v command failed: %q", cmd, string(out))
		}
	}
	return nil
}

var runFSModifyTests = flag.Bool("run_fs_modify_tests", false, "run tests which modify filesystem parameters")
