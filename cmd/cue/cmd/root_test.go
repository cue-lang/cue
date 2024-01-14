// Copyright 2019 CUE Authors
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

package cmd

import (
	"context"
	"io"
	"testing"
)

func TestHelp(t *testing.T) {
	// TODO(mvdan): consider rewriting in testscript, capturing stdout/stderr
	run := func(args ...string) error {
		cmd, _ := New(args)
		cmd.SetOutput(io.Discard)
		ctx := context.Background()
		return cmd.Run(ctx)
	}
	if err := run("help"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("--help"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("-h"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("help", "cmd"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("cmd", "--help"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("cmd", "-h"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("help", "eval"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	if err := run("eval", "--help"); err != nil {
		t.Error("help command failed unexpectedly")
	}

	// os.Exit(1) is expected
	testOsExit(t, 1, []string{"help", "foo"}, run)

	testOsExit(t, 1, []string{"help", "cmd", "foo"}, run)
}

// testOsExit tests that os.Exit is called with the given code when running f.
func testOsExit(t *testing.T, code int, args []string, f func(args ...string) error) {
	t.Helper()
	oldExit := osExit
	defer func() { osExit = oldExit }()
	var status int
	exit := func(code int) {
		status = code
	}
	osExit = exit
	f(args...)
	if status != code {
		t.Errorf("os.Exit(%d) not called", code)
	}
}
