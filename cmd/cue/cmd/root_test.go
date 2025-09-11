// Copyright 2024 The CUE Authors
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

package cmd_test

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/internal/cueversion"
	"github.com/go-quicktest/qt"
)

// The cmd package exposes some APIs, which some users rely on.
// Ensure that they continue to work as advertised.

func TestCommand(t *testing.T) {
	ctx := context.Background()

	// Create one command and run it, only checking that it succeeds.
	c, err := cmd.New([]string{"help", "export"})
	qt.Assert(t, qt.IsNil(err))
	c.SetOutput(io.Discard)
	err = c.Run(ctx)
	qt.Assert(t, qt.IsNil(err))

	// Create another command and run it, expecting it to fail.
	c, err = cmd.New([]string{"help", "nosuchcommand"})
	qt.Assert(t, qt.IsNil(err))
	c.SetOutput(io.Discard)
	err = c.Run(ctx)
	qt.Assert(t, qt.IsNotNil(err))

	// Verify that SetInput and SetOutput work.
	c, err = cmd.New([]string{"export", "-"})
	qt.Assert(t, qt.IsNil(err))
	c.SetInput(strings.NewReader("foo: 123\n"))
	var buf bytes.Buffer
	c.SetOutput(&buf)
	err = c.Run(ctx)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(buf.String(), "{\n    \"foo\": 123\n}\n"))

	// Verify that we can use the API exposed by the embedded cobra command.
	c, err = cmd.New([]string{"fmt", "nosuchfile.cue"})
	qt.Assert(t, qt.IsNil(err))
	err = c.Execute()
	qt.Assert(t, qt.IsNotNil(err))

	// Verify that we can change the current directory via [testing.T.Chdir].
	// We test by ensuring we can load the module under testdata/files,
	// which uses the working directory as a starting point to find a module root.
	t.Run("Chdir", func(t *testing.T) {
		t.Chdir("testdata/module_broken")

		c, err = cmd.New([]string{"mod", "tidy", "--check"})
		qt.Assert(t, qt.IsNil(err))
		err = c.Execute()
		qt.Assert(t, qt.ErrorMatches(err, `^disallowed: field not allowed`))

		// Change the directory a second time, to ensure the global state is not sticky.
		t.Chdir("../module_ok")

		c, err = cmd.New([]string{"mod", "tidy", "--check"})
		qt.Assert(t, qt.IsNil(err))
		err = c.Execute()
		qt.Assert(t, qt.IsNil(err))
	})
}

func TestVersion(t *testing.T) {
	t.Parallel()

	if info, err := os.Lstat("../../../.git"); err != nil {
		t.Skip("cue version only includes VCS-derived information when building from a git clone")
	} else if !info.IsDir() {
		t.Skip("VCS information is not stamped for git worktrees due to https://go.dev/issue/58218")
	}

	// Test whether "cue version" reports the version information we expect.
	// Note that we can't use the test binary for this purpose,
	// given that binaries built via "go test" don't get stamped with version information.
	//
	// TODO: use "go tool cue", mimicking "go install ./cmd/cue && cue",
	// once https://go.dev/issue/75033 is resolved.
	// Until then, "go tool" never stamps the main module version,
	// and "go run" only does when -buildvcs is explicitly enabled.
	out, err := exec.Command("go", "run", "-buildvcs=true", "..", "version").CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("%s", out))

	got := string(out)

	for _, expr := range []string{
		`^cue version v0\.\d+\.\d+`,
		`\s+go version go1\.\d+`,
		`\s+-buildmode\s`,
		`\s+GOARCH\s`,
		`\s+GOARCH\s`,
		`\s+vcs git`,
		`\s+vcs.revision\s`,
		`\s+vcs.time\s`,
		`\s+cue\.lang\.version\s+` + regexp.QuoteMeta(cueversion.LanguageVersion()),
	} {
		// The output string is multi-line, and [qt.Matches] anchors the regular expression
		// like `^(expr)$`, so use `(?s)` to match newlines.
		qt.Assert(t, qt.Matches(got, `(?s).*`+expr+`.*`))
	}
}

// Test that we handle context cancellation via SIGINT correctly,
// never letting the cue tool hang around until it gets SIGKILLed.
func TestInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		// TODO: how could we support and test graceful cmd/cue cancellation on Windows?
		t.Skip("interrupt signals are not supported in Windows")
	}

	t.Parallel()

	// Run the same tool binary for each of the test cases.
	// This helps because we need to wait for the signal handler to be set up,
	// and having to also wait for `go build` to finish at the same time is messy.
	toolOut, err := exec.CommandContext(t.Context(), "go", "tool", "-n", "cue").Output()
	qt.Assert(t, qt.IsNil(err))
	toolPath := strings.TrimSpace(string(toolOut))

	// We set up a mock registry so the OAuth flow used by `cue login` is always pending.
	srv := newMockRegistryOauth("pending-forever")
	regURL, _ := url.Parse(srv.URL)
	t.Cleanup(srv.Close)

	for _, test := range []struct {
		args []string

		graceful       bool   // when false, we expect an abrupt stop
		gracefulStderr string // when empty, we expect a non-zero exit status
	}{
		// Test what happens when a command intentionally hangs forever
		// without handling context cancellation for a graceful stop.
		{args: []string{"exp", "internal-hang"}},

		// `cue mod registry` is a module registry for testing purposes,
		// and it can be gracefully stopped.
		{args: []string{"mod", "registry"}, graceful: true},

		// `cue login` waits for the user to perform the device flow interaction,
		// and should gracefully stop if interrupted while waiting.
		// TODO: this is currently an abrupt stop.
		{args: []string{"login"}},

		// Loading stdin as an input should gracefully stop when interrupted.
		// TODO: this is currently an abrupt stop.
		{args: []string{"export", "-"}},

		// TODO: Test more scenarios like the ones below.
		// It's perhaps significant work to add these, but they would be good
		// smoke tests to ensure that context cancellation is propagated correctly.
		//
		// * interrupt interactions with a fake registry,
		//   such as a `cue mod get` or `cue mod publish` which never finish.
		// * interrupt a long evaluation, but can that be done without burning CPU?
		// * interrupt a `cue cmd` command running an external process which is hung forever.
	} {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, toolPath, test.args...)
			cmd.Env = append(cmd.Environ(), "CUE_REGISTRY="+regURL.Host+"+insecure")

			// Hook up stdin to be present but block forever without having any bytes to read.
			pr, pw, err := os.Pipe()
			qt.Assert(t, qt.IsNil(err))
			cmd.Stdin = pr
			defer pw.Close()
			defer pr.Close()

			stderrBuf := new(bytes.Buffer)
			cmd.Stderr = stderrBuf
			err = cmd.Start()
			qt.Assert(t, qt.IsNil(err))

			// Give the cue tool enough time to set up the signal handler.
			// This is inherently racy, but the only alternative would be
			// to add some sort of debug mode to the cue tool to print a line
			// when it is ready to receive interrupt signals, which is also odd.
			//
			// It seems fine to test the real program in a way that works most of the time.
			time.Sleep(100 * time.Millisecond)

			err = cmd.Process.Signal(os.Interrupt)
			qt.Assert(t, qt.IsNil(err))

			err = cmd.Wait()
			t.Logf("error: %v", err)
			stderr := stderrBuf.String()
			t.Logf("stderr: %s", stderr)

			// When we expect an abrupt stop, we want the program to exit immediately
			// due to the lack of a signal handler, which is Go's default behavior.
			// Note that when a Go program is terminated immediately due to a signal
			// without a signal handler, [os.ProcessState.Exited] returns false.
			if !test.graceful {
				qt.Assert(t, qt.IsNotNil(err))
				qt.Assert(t, qt.Equals(cmd.ProcessState.Exited(), false))
				return
			}

			// We expect a graceful stop, we want the program to exit normally
			// thanks to a signal handler.
			// If we exited immediately due to the lack of a signal handler,
			// that means we raced and sent the signal before the handler was ready.
			if !cmd.ProcessState.Exited() {
				t.Skipf("the cue command was interrupted before the signal handler was ready")
			}
			if test.gracefulStderr == "" {
				// We expect the command to stop quietly and without failing.
				qt.Assert(t, qt.IsNil(err))
				qt.Assert(t, qt.Equals(stderr, ""))
			} else {
				// We expect the command to stop with an error.
				qt.Assert(t, qt.IsNotNil(err))
				qt.Assert(t, qt.Equals(stderr, test.gracefulStderr))
			}
		})
	}
}
