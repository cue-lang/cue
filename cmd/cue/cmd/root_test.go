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
	"os"
	"strings"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
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

	// Verify that we can change the current directory via [os.Chdir].
	// We test by ensuring we can load the module under testdata/files,
	// which uses the working directory as a starting point to find a module root.
	t.Run("Chdir", func(t *testing.T) {
		// TODO: use [testing.T.Chdir] once we can use Go 1.24 or later.
		origDir, err := os.Getwd()
		qt.Assert(t, qt.IsNil(err))

		qt.Assert(t, qt.IsNil(os.Chdir("testdata/module_broken")))
		t.Cleanup(func() {
			qt.Assert(t, qt.IsNil(os.Chdir(origDir)))
		})

		c, err = cmd.New([]string{"mod", "tidy", "--check"})
		qt.Assert(t, qt.IsNil(err))
		err = c.Execute()
		qt.Assert(t, qt.ErrorMatches(err, `^disallowed: field not allowed`))

		// Change the directory a second time, to ensure the global state is not sticky.
		qt.Assert(t, qt.IsNil(os.Chdir("../module_ok")))

		c, err = cmd.New([]string{"mod", "tidy", "--check"})
		qt.Assert(t, qt.IsNil(err))
		err = c.Execute()
		qt.Assert(t, qt.IsNil(err))
	})
}
