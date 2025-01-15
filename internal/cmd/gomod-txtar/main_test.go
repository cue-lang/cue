// Copyright 2025 The CUE Authors
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
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestBasics(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		toggled string
	}{
		{
			input: "",
			toggled: `go mod edit -replace=cuelang.org/go=/path/to/somewhere
go mod tidy

-- go.mod --
module mod.example
-- deps.go --
//go:build deps
package deps
import _ "cuelang.org/go/cmd/cue"
`,
		},
		{
			input: `exec cat file.txt

-- file.txt --
`,
			toggled: `go mod edit -replace=cuelang.org/go=/path/to/somewhere
go mod tidy

exec cat file.txt

-- go.mod --
module mod.example
-- deps.go --
//go:build deps
package deps
import _ "cuelang.org/go/cmd/cue"
-- file.txt --
`,
		},
		{
			input: `! exec cue export file.txt
exec cue export file.txt

-- file.txt --
`,
			toggled: `go mod edit -replace=cuelang.org/go=/path/to/somewhere
go mod tidy

! exec go run cuelang.org/go/cmd/cue export file.txt
exec go run cuelang.org/go/cmd/cue export file.txt

-- go.mod --
module mod.example
-- deps.go --
//go:build deps
package deps
import _ "cuelang.org/go/cmd/cue"
-- file.txt --
`,
		},
	}

	const replTarget = "/path/to/somewhere"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ar := txtar.Parse([]byte(tc.input))

			// Sanity check a roundtrip to txtar and back
			assertArchiveEqualts(t, ar, tc.input)

			toggleArchive(ar, replTarget)

			// Assert we get the expected toggled state
			assertArchiveEqualts(t, ar, tc.toggled)

			// Toggle back and assert we get back to input
			toggleArchive(ar, replTarget)
			assertArchiveEqualts(t, ar, tc.input)
		})
	}
}

func assertArchiveEqualts(t *testing.T, ar *txtar.Archive, want string) {
	t.Helper()
	got := txtar.Format(ar)
	qt.Assert(t, qt.Equals(string(got), want))
}
