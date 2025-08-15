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
	"os/exec"
	"regexp"
	"strings"
	"testing"

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

	// The output string is multi-line, and [qt.Matches] anchors the regular expression
	// like `^(expr)$`, so use `(?s)` to match newlines.
	qt.Assert(t, qt.Matches(got, `(?s)cue version v0\.\d+\.\d+.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+go version go1\.\d+.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+-buildmode\s.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+GOARCH\s.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+GOARCH\s.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+vcs git.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+vcs.revision\s.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+vcs.time\s.*`))
	qt.Assert(t, qt.Matches(got, `(?s).*\s+cue\.lang\.version\s+`+regexp.QuoteMeta(cueversion.LanguageVersion())+`.*`))
}
